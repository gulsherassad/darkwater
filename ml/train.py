"""Train a GRU that corrects dead reckoning, then export it to ONNX.

Run from the repo root with the venv active:
    python ml/train.py
"""
import copy
import json
import os
import time

import numpy as np
import onnxruntime as ort
import torch
import torch.nn as nn
from torch.utils.data import DataLoader, TensorDataset

SEED = 42
EPOCHS = 30
PATIENCE = 4  # stop after this many epochs without val improvement
BATCH = 1024
LR = 1e-3
HIDDEN = 64
HORIZONS = [5, 15, 30, 60]
MODEL_PATH = "models/traj_gru.onnx"
METRICS_PATH = "models/traj_gru.metrics.json"

torch.manual_seed(SEED)
np.random.seed(SEED)

# ---- Data ----
meta = json.load(open("data/ml/meta.json"))
NCOL = len(meta["columns"])
NI = meta["num_inputs"]  # 65 inputs, then 2 targets
H = meta["hist_len"]  # 10 history points
F = meta["step_features"]  # 6 numbers per point
SEQ = H * F  # first 60 inputs are the sequence; the last 5 are context


def load(split):
    arr = np.fromfile(f"data/ml/{split}.bin", dtype="<f4").reshape(-1, NCOL)
    return arr[:, :NI].copy(), arr[:, NI : NI + 2].copy()


Xtr, Ytr = load("train")
Xva, Yva = load("val")
Xte, Yte = load("test")

# Normalization stats from TRAIN ONLY (val/test must not influence anything)
seq_tr = Xtr[:, :SEQ].reshape(-1, H, F)
seq_mean = seq_tr.mean(axis=(0, 1))
seq_std = seq_tr.std(axis=(0, 1)) + 1e-6
ctx_mean = Xtr[:, SEQ:].mean(axis=0)
ctx_std = Xtr[:, SEQ:].std(axis=0) + 1e-6


# ---- Model ----
class TrajGRU(nn.Module):
    def __init__(self):
        super().__init__()
        # Buffers are saved with the model (and exported to ONNX) but not trained
        self.register_buffer("seq_mean", torch.tensor(seq_mean, dtype=torch.float32))
        self.register_buffer("seq_std", torch.tensor(seq_std, dtype=torch.float32))
        self.register_buffer("ctx_mean", torch.tensor(ctx_mean, dtype=torch.float32))
        self.register_buffer("ctx_std", torch.tensor(ctx_std, dtype=torch.float32))
        self.gru = nn.GRU(F, HIDDEN, batch_first=True)
        self.head = nn.Sequential(nn.Linear(HIDDEN + 5, 64), nn.ReLU(), nn.Linear(64, 2))

    def forward(self, x):
        seq = x[:, :SEQ].reshape(-1, H, F)
        ctx = x[:, SEQ:]
        dr = ctx[:, 3:5]  # dead reckoning's guess (dx, dy) in km

        seq_n = (seq - self.seq_mean) / self.seq_std
        ctx_n = (ctx - self.ctx_mean) / self.ctx_std
        _, h = self.gru(seq_n)  # h = memory after reading all 10 points
        correction = self.head(torch.cat([h[-1], ctx_n], dim=1))
        return dr + correction  # predicted displacement in km


# ---- Evaluation: median and p90 error per horizon ----
def metrics(pred, Y, X):
    err = np.hypot(pred[:, 0] - Y[:, 0], pred[:, 1] - Y[:, 1])
    horizon = X[:, SEQ]  # first context column = horizon in minutes
    out = {}
    for h in HORIZONS:
        e = err[np.abs(horizon - h) <= 1.5]
        out[h] = (float(np.median(e)), float(np.percentile(e, 90)), int(len(e)))
    return out


def predict(model, X):
    model.eval()
    with torch.no_grad():
        return model(torch.from_numpy(X)).numpy()


def mean_p90(m):
    return float(np.mean([m[h][1] for h in HORIZONS]))


# ---- Training ----
model = TrajGRU()
opt = torch.optim.Adam(model.parameters(), lr=LR)
loss_fn = nn.SmoothL1Loss(beta=0.5)  # Huber loss, in km
loader = DataLoader(TensorDataset(torch.from_numpy(Xtr), torch.from_numpy(Ytr)), batch_size=BATCH, shuffle=True)

dr_val = metrics(Xva[:, SEQ + 3 : SEQ + 5], Yva, Xva)
print(f"val mean p90 — dead reckoning: {mean_p90(dr_val):.3f} km (the score to beat)\n")

best_score, best_state, bad_epochs = float("inf"), None, 0
for epoch in range(1, EPOCHS + 1):
    t0 = time.time()
    model.train()
    total = 0.0
    for xb, yb in loader:
        opt.zero_grad()
        loss = loss_fn(model(xb), yb)
        loss.backward()
        opt.step()
        total += loss.item() * len(xb)

    score = mean_p90(metrics(predict(model, Xva), Yva, Xva))
    improved = score < best_score
    if improved:
        best_score, best_state, bad_epochs = score, copy.deepcopy(model.state_dict()), 0
    else:
        bad_epochs += 1
    print(f"epoch {epoch:2d}  train loss {total / len(Xtr):.4f}  val mean p90 {score:.3f} km"
          f"  {'*' if improved else ' '}  ({time.time() - t0:.1f}s)")
    if bad_epochs >= PATIENCE:
        print("validation stopped improving: early stop")
        break

model.load_state_dict(best_state)

# ---- Final test: the locked exam ----
gru_te = metrics(predict(model, Xte), Yte, Xte)
dr_te = metrics(Xte[:, SEQ + 3 : SEQ + 5], Yte, Xte)
print("\nTEST (18:00–24:00 UTC), median / p90 in km:")
print(f"{'horizon':8s} {'n':>6s}   {'dead reckoning':>18s}   {'GRU':>18s}   p90 change")
for h in HORIZONS:
    dm, dp, n = dr_te[h]
    gm, gp, _ = gru_te[h]
    print(f"{h:3d} min  {n:6d}   {dm:7.2f} / {dp:7.2f}   {gm:7.2f} / {gp:7.2f}   {100 * (gp - dp) / dp:+6.1f}%")

# ---- Export to ONNX ----
os.makedirs("models", exist_ok=True)
model.eval()
example = torch.from_numpy(Xte[:8])
export_args = dict(input_names=["inputs"], output_names=["pred"],
                   dynamic_axes={"inputs": {0: "batch"}, "pred": {0: "batch"}})
try:
    torch.onnx.export(model, (example,), MODEL_PATH, opset_version=17, dynamo=False, **export_args)
except Exception as e:
    print(f"\nclassic ONNX exporter failed ({e}); trying the newer one")
    torch.onnx.export(model, (example,), MODEL_PATH, dynamo=True, **export_args)

# Check that ONNX Runtime gives the same answers as PyTorch
sess = ort.InferenceSession(MODEL_PATH)
in_name = sess.get_inputs()[0].name
onnx_out = sess.run(None, {in_name: Xte[:2000]})[0]
torch_out = predict(model, Xte[:2000])
diff = float(np.abs(onnx_out - torch_out).max())
print(f"\nexported {MODEL_PATH} ({os.path.getsize(MODEL_PATH) / 1024:.0f} KB)")
print(f"ONNX input '{in_name}' {sess.get_inputs()[0].shape}, output '{sess.get_outputs()[0].name}'")
print(f"max difference PyTorch vs ONNX Runtime: {diff:.2e} km")
assert diff < 1e-3, "ONNX output doesn't match PyTorch!"

json.dump({
    "test": {str(h): {"gru_median": gru_te[h][0], "gru_p90": gru_te[h][1],
                      "dr_median": dr_te[h][0], "dr_p90": dr_te[h][1], "n": gru_te[h][2]} for h in HORIZONS},
    "best_val_mean_p90": best_score,
    "seed": SEED, "hidden": HIDDEN, "source": meta["source"],
}, open(METRICS_PATH, "w"), indent=2)
print(f"saved {METRICS_PATH}")