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