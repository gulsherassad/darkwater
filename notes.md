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

## Reception model v2: DMA presence interpolated across gaps ≤ 15 min
- Bug found via SWAN ATLANTIC false alarm: DMA hears ships sporadically at its fringe → model only counted
  DMA's good-reception minutes → overestimated feed hearing chance (selection bias)
- 32% of present ship-minutes were missing from DMA (62,055 of 191,797 interpolated)
- Hearing chance 6.1%/min overall; 0.7% (10th pct) / 3.0% (median) / 18.9% (90th pct)
- False alarms, 3,924 gaps over 1,665 ship-hours:
  - naive ≥ 15 min: 213 vs model α=0.05 (flags at 15 min in good coverage): 55 → 74% fewer (matched)
  - naive ≥ 20 min: 132 vs α=0.01 (22 min): 15 → 89% fewer
  - naive ≥ 30 min: 40 vs α=0.001 (33 min): 5 → 88% fewer
- Feed-outage rule: silences spanning > 5 min of feed downtime aren't judged (removed 15 false flags in gapscan)
- gapscan at α=0.01: 3 of 150 judged ships flagged ≈ chance level (multiple testing) → live: watch α=0.01, alert α=0.001

## Live detector, overnight run (Nov 17 22:31 → Nov 18 12:23 PT)
- Laptop slept ~23:13–09:40: every scan "0 judged" → feed-outage rule prevented ~1,100 false alerts on wake
- Receiver survived repeated drops, network unreachable, and an IP change (moved networks); backoff capped at 1 min
- Steady state: ~110–160 ships judged per 30 s scan; watch 0–2, alert 0–2 (brief), mostly 0 — consistent with multiple-testing expectations
- Gap: monitor logs only counts → need per-ship alert event log to verify against DMA (Step 3)
- Recorded ~3 h of Nov 18 UTC overlap → rerun reception model with aisdk-2026-11-18 (~Nov 21)

## Live alerts UI + event log (test run, watch α=0.3)
- Map: amber/red ships, line + ring at projected position, clickable alerts list, HUD count
- Event log (data/alerts/*.jsonl): open / escalate / deescalate / reappeared / cleared, with missKm on reappearance
- Test run: 4 ships flagged, all reappeared within ~6 min; projection miss 5 m, 37 m, 112 m, 866 m
  → dead reckoning accurate over short gaps; ferry leaving harbour (turning/accelerating) had largest miss
- Double-ended ferries (DEUTSCHLAND): heading ≠ course by ~180° → projection must use course, never heading
- Pattern spotted: nearby ships going silent together → likely receiver hiccup (future: correlated-silence suppression)

## Spoofing scan (data/raw-server, Nov 2026)
- 28,608 positions / 1,552 ships → 2 teleports (1 ship), 0 identity clashes
- ALICE 244790715: 2 teleports at 2,509 kn = single bad fix out and back (GPS glitch)
- Future: tell clash vs glitch apart by checking whether the far positions form their own consistent track

## Encounter scan (3.2 h, data/raw-server)
- v1 mask (AIS "moored" status only): 548 encounters, 217 candidates/scan. Small harbours (FN/VG fishing boats) never report moored; n boats at one quay = n(n-1)/2 pairs
- v2 mask (3+ distinct ships < 0.5 kn per ~1 km square): 159 port squares, 86 candidates/scan, 32 encounters (-94%)
- Remaining 32 = rescue/pilot boats at 2-boat bases, same pairs both sessions. Indistinguishable from STS by position alone → need ShipStaticData (type, draught)
## Sparse GRU for the live feed (features sparse-v2)
- Live gaps (cmd/livegaps, 14 recordings, 30,210 gaps ≤ 2 h, 1,726 ships): p10 59 s / p25 98 s / median 189 s / p75 430 s / p90 20 min
- Dense GRU needs 10 fixes in 15 min → on live-density tracks it falls back to DR on 100% of questions
- Sparse recipe: last 6 fixes within 90 min, + gap since previous fix per point (47 inputs vs 65)
- Training: Nov 9 DMA thinned with live gaps (3 passes, seeds 1–3), truth from the full track; train 240k / val 48k / test 57k
- Same-day test (Nov 9 thinned) p90 vs DR: 15 min −9.3%, 30 min −15.2%, 60 min −17.6%; 5 min +31% (hybrid uses DR there)
- Cross-day, Go harness (Nov 12 18–24 UTC thinned, seed 99, 19,834 questions), hybrid p90 vs DR:
  | horizon | DR p50 / p90 | hybrid p50 / p90 | p90 change |
  |---|---|---|---|
  | 15 min | 0.21 / 1.84 km | 0.25 / 1.71 km | −7.2% |
  | 30 min | 0.65 / 5.14 km | 0.70 / 4.63 km | −10.0% |
  | 60 min | 2.15 / 13.29 km| 2.06 / 11.27 km| −15.2% |
- 0 model errors; sparse falls back to DR on 3,272 direct calls (fewer than 6 fixes in 90 min)
- Dense pipeline unchanged after refactor: Nov 9 eval reproduces −10.3 / −12.9 / −14.3% exactly
- Hybrid path coverage (same run; trajeval now prints this):
  | horizon | n | DR (<10 min) | dense GRU | sparse GRU | fallback (DR) |
  |---|---|---|---|---|---|
  | 5 min  | 5,351 | 100% | 0% | 0% | 0% |
  | 15 min | 5,178 | 0% | 0% | 83.4% | 16.6% |
  | 30 min | 4,899 | 0% | 0% | 83.6% | 16.4% |
  | 60 min | 4,406 | 0% | 0% | 83.5% | 16.5% |
- Only questions the sparse GRU answered (DR vs GRU on the same questions):
  | horizon | n | DR p50 / p90 | GRU p50 / p90 | p90 change |
  |---|---|---|---|---|
  | 15 min | 4,317 | 0.20 / 1.76 km | 0.25 / 1.58 km | −9.9% |
  | 30 min | 4,097 | 0.64 / 5.04 km | 0.69 / 4.41 km | −12.4% |
  | 60 min | 3,678 | 2.14 / 13.19 km | 2.04 / 10.82 km | −18.0% |
- Overall hybrid gains (−7.2 / −10.0 / −15.2%) are diluted by the ~16.5% fallback questions, which are also harder
  (DR p90 on them: 2.29 / 5.75 / 14.31 km vs 1.76 / 5.04 / 13.19 km where the GRU runs)
- Future work: K=4 or zero-padded sparse variant (new feature version + retrain) to cover the ~16.5% fallback questions

## Synthetic switch-off injection (cmd/inject, DMA Nov 12 + data/raw/2026-11-12T*.jsonl)
- Method: held-out ships (FNV split), reception model refit on fit-set ships only (prior 20, 6.1%/min overall);
  random switch-off time T per ship × 5 seeds where DMA shows ≥ 3 kn at T and presence for 2 h after; feed fixes after T deleted;
  detect.ScoreGap scored minute by minute from T, unchanged. Watch ends at end of feed data or a feed outage (> MaxFeedDownMin) = censored, not missed.
  Detection rates are Kaplan–Meier; n = injections detected by then or still under watch.
- Controls: 2,012 real feed gaps on 160 held-out ships where DMA shows present + moving (≥ 1 kn) throughout; 119.9 ship-hours watched
- n: 765 injections (153 eligible ships of 1,135); 496 censored at end of feed, 269 at the 17:48–18:53 feed outage, 0 watched the full 6 h; longest watch 81 min
  → ≤ 2 h rates and medians are NOT estimable from this data (needs a longer DMA/feed overlap: Nov 18)

| rule | alarms | per 100 h | ≤ 30 min (n) | ≤ 1 h (n) | median |
|---|---|---|---|---|---|
| model α=0.05   | 3  | 2.50  | 22.8% (296) | 31.0% (175) | not reached |
| model α=0.01   | 0  | 0     | 15.3% (285) | 24.8% (149) | not reached |
| model α=0.001  | 0  | 0     | 1.3% (278)  | 13.8% (120) | not reached |
| model α=0.0001 | 0  | 0     | 0.0% (276)  | 7.6% (110)  | not reached |
| naive ≥ 15 min | 36 | 30.03 | 100% (491)  | 100% (491)  | 13 min |
| naive ≥ 30 min | 5  | 4.17  | 100% (303)  | 100% (303)  | 28 min |
| naive ≥ 60 min | 0  | 0     | 0% (276)    | 100% (119)  | 58 min |
- 0 alarms in 119.9 ship-hours only bounds the rate at < 2.5 per 100 h (95%): controls too small to separate α ≤ 0.01 from naive ≥ 60 min
- Matched false alarms: α=0.05 (2.5/100 h) vs naive ≥ 30 (4.2/100 h): 31% vs 100% within 1 h; α ≤ 0.01 vs naive ≥ 60 (both 0 alarms): 8–25% vs 100%
- By reception at T (α=0.001, ≤ 1 h; terciles of cell hearing chance among judgeable cells):
  poor < 9.9%: 7.4% (n=12) · median 9.9–16.6%: 34.8% (25) · good ≥ 16.6%: 23.6% (28) · cell < 30 ship-min data: 6.1% (55)
- Main finding: 458 of 765 injections (60%) start in cells with < MinCellData ship-minutes in the fit-set model → those silences
  are mostly never judged. The reception model is data-starved (99 feed minutes; live model has ~2× the data, same problem)
- On this data the naive rule detects far more at equal measured false alarms; the model's false-alarm advantage (notes above:
  −74…−89% at matched sensitivity) can't show here because 120 control ship-hours can't measure rates < 2.5/100 h
- Next: rerun with aisdk-2026-11-18 + Nov 18 data/raw-server recordings (longer overlap); refit reception on more feed hours
  before trusting α-based detection rates
