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