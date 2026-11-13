"""Sanity check: load the exported data and reproduce dead reckoning's error.

If these numbers roughly match Go's trajeval output, Go and Python agree
on the data format. Do this before training anything.
"""
import json

import numpy as np

meta = json.load(open("data/ml/meta.json"))
cols = meta["columns"]
col = {name: i for i, name in enumerate(cols)}


def load(split):
    x = np.fromfile(f"data/ml/{split}.bin", dtype="<f4")  # little-endian float32
    return x.reshape(-1, len(cols))


for split in ["train", "val", "test"]:
    data = load(split)
    print(f"{split:5s}: {data.shape[0]:7d} rows, {data.shape[1]} columns, NaNs: {np.isnan(data).sum()}")

test = load("test")
dr_err = np.hypot(
    test[:, col["dr_dx_km"]] - test[:, col["target_dx_km"]],
    test[:, col["dr_dy_km"]] - test[:, col["target_dy_km"]],
)
horizon = test[:, col["horizon_min"]]

print("\ndead reckoning on the exported test set (median / p90):")
for h in [5, 15, 30, 60]:
    e = dr_err[np.abs(horizon - h) <= 1.5]  # actual horizon can be off by up to 60 s
    print(f"  {h:2d} min  n={len(e):6d}   {np.median(e):6.2f} km / {np.percentile(e, 90):6.2f} km")