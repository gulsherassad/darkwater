# darkwater — measured findings

## DMA ground truth (aisdk-2026-11-09, Danish shore network)
- 16.1M rows/day, 3,016 ships, 9.3M rows kept after cleaning
- 33% of rows are duplicates (same broadcast logged by several shore stations)
- How often ships really transmit (gap between messages from the same ship):
  - 92% of gaps < 30 s; median falls in the 10–30 s bucket
  - 3.15% in 2–5 min (anchored/moored ships report less often, per the AIS standard)
  - only 0.06% of gaps > 15 min
- Parser: 2.7 GB CSV in 12 s (~1.3M rows/s), constant memory (streams row by row)

## AISStream feed (live, Denmark bounding box)
- ~2.8 messages/s ≈ 240k/day vs DMA 16.1M/day → feed delivers ~1.5% of transmissions (areas not identical, rough figure)
- Typical gap per ship in the feed: 1–3 min (vs 10–30 s real) → feed passes on roughly 1 in 5–10 messages
- Gaps cluster at round minutes (1m, 2m, 3m) → feed appears to throttle per ship
- Conclusion: silences in the feed are mostly the feed, not the ships

## Live map
- Viewport filtering: snapshot 19.3 KB (zoomed out) → 2.7 KB (Copenhagen) = 86% smaller

## DMA vs feed cross-check (2026-11-09, 19:35–19:40 UTC)
- Clock: offsets peak at 0s / -1s → DMA timestamps are UTC (DMA truncates to whole seconds, hence -1s)
- 76.6% of moving feed positions also appear in DMA; 148 of 566 feed ships absent from DMA entirely
  → DMA covers Danish waters only; Swedish/German/Norwegian parts of the bounding box are outside its coverage
  → DMA is ground truth only where DMA itself has coverage; reception model must account for this
- 5-minute window: DMA saw 1,899 ships, feed 566, both 418 → feed saw 22% of DMA's ships

## H3 grid (res 6, ~36 km² cells), DMA 2026-11-09 vs all feed recordings so far
- 4,130 cells with data: both 614, DMA only 3,465, feed only 51
- NOT a fair coverage comparison yet: DMA = 24 h, feed = a few short sessions
- 51 feed-only cells = waters outside DMA's coverage (Sweden/Germany/Norway), confirms Step 3
- Busiest cells: ~100–112 distinct ships/day in DMA

## Coverage map (H3 res 6)
- DMA-only cells trace the traffic separation schemes (Skagerrak, Kattegat, Great Belt, Bornholm)
- Hard western edge of DMA coverage in the North Sea = range limit of DMA shore receivers
- Feed-only cells at the fringes: southern Norway, Karlskrona, Wadden Sea

- 80-min run (2026-11-12, ~18:53–20:15 UTC): ship count plateaus at ~1,160 after ~1 h;
  median per-ship gap stabilizes at ~3–4 min (early readings biased low by censoring)
  → vs DMA's 10–30 s, feed passes on roughly 1 in 10–20 messages per ship
- Rate steady at ~3 msg/s throughout → looks like a fixed cap on AISStream's side

## Trajectory baselines (DMA 2026-11-09, test = after 18:00 UTC, moving ships ≥ 3 kn)
| horizon | n | stay-put median / p90 | dead reckoning median / p90 |
|---|---|---|---|
| 5 min  | 13,816 | 1.32 / 2.01 km  | 0.02 / 0.31 km |
| 15 min | 13,283 | 4.46 / 6.64 km  | 0.15 / 1.93 km |
| 30 min | 12,562 | 9.01 / 13.28 km | 0.59 / 5.28 km |
| 60 min | 11,266 | 17.70 / 26.17 km| 2.63 / 13.48 km |
- Error is heavily skewed: median tiny, p90 10–20× larger → turning ships cause most error
- Target metric for ML: p90, not median
- Stay-put 60-min median 17.7 km ⇒ typical moving ship ~9.6 kn (sanity check)

## Trajectory training data (DMA 2026-11-09)
- Splits by time, no leakage across boundaries: train 00–15 UTC (264k), val 15–18 (52k), test 18–24 (47.5k)
- 10-point history (≤ 15 min), positions relative to current point, course as sin/cos; target = displacement at horizon
- Exported set covers ~93% of harness questions (needs full history); Python reproduces Go's dead-reckoning numbers → format verified

## Residual GRU (hidden 64, Huber loss, early stop on val p90; trained 00–15 UTC Nov 9)
| horizon | DR median / p90 | GRU median / p90 | p90 change |
|---|---|---|---|
| 5 min  | 0.02 / 0.26 km | 0.11 / 0.36 km | +37.6% |
| 15 min | 0.14 / 1.80 km | 0.25 / 1.62 km | −10.4% |
| 30 min | 0.56 / 5.18 km | 0.72 / 4.53 km | −12.5% |
| 60 min | 2.52 / 13.47 km| 2.57 / 11.30 km| −16.1% |
- GRU wins the tail (turns) at ≥ 15 min, loses at 5 min (adds small noise to near-perfect straight-line predictions)
- Decision: hybrid — dead reckoning < 10 min, GRU ≥ 10 min
- ONNX export verified against PyTorch (max diff 6.7e-6 km at batch 2000), 76 KB
- Caveat: same-day train/test; cross-day test pending (Nov 12 data)

## Official trajectory results (Go harness, full test set 18–24 UTC Nov 9, 50,927 questions)
| horizon | DR median / p90 | hybrid median / p90 | p90 change |
|---|---|---|---|
| 5 min  | 0.02 / 0.31 km | 0.02 / 0.31 km | 0% (DR) |
| 15 min | 0.15 / 1.93 km | 0.26 / 1.73 km | −10.3% |
| 30 min | 0.59 / 5.28 km | 0.75 / 4.60 km | −12.9% |
| 60 min | 2.63 / 13.48 km| 2.64 / 11.55 km| −14.3% |
- Hybrid = dead reckoning < 10 min, GRU ≥ 10 min; never worse than DR
- Go ONNX results match Python within ~2 points → no training/serving skew; 0 model errors
- GRU falls back to DR when history is incomplete (3,676 calls)
- Caveat: same-day test; cross-day test pending

## Cross-day test (model trained Nov 9, tested Nov 12 18–24 UTC, 44,273 questions)
| horizon | same day (Nov 9) | unseen day (Nov 12) |
|---|---|---|
| 15 min | −10.3% | −7.6% |
| 30 min | −12.9% | −12.3% |
| 60 min | −14.3% | −13.8% |
- Gains hold on an unseen day → model learned general turning patterns, not one day's voyages
- 0 model errors; Nov 12 DMA stats nearly identical to Nov 9 (15.97M rows, 2,825 ships, 92.3% gaps < 30 s)

## Reception model (Nov 12, 99 feed-alive minutes, H3 res 6, prior strength 20)
- Split ships by FNV hash of MMSI (parity split was biased: big ships' MMSIs end in zeros → even)
- Overall hearing chance 8.6%/min; per-cell spread 1.4% (10th pct) → 6.2% (median) → 20.9% (90th pct)
- Silence needed to flag (good / typical / poor): α=0.05 → 13 / 48 / 206 min; α=0.01 → 20 / 73 / 316 min
- False alarms on eval ships (all confirmed transmitting by DMA), 3,004 gaps over 1,145 ship-hours:
  - naive ≥ 20 min: 35 vs model α=0.01: 3 → 91% fewer at same good-coverage sensitivity
  - naive ≥ 15 min: 72 vs model α=0.05: 22 → 69% fewer while flagging sooner (13 min)
- Limitation: can only detect short dark periods where feed coverage is good
- Preliminary: small sample; re-run with more recorded hours