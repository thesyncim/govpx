# VP8 Performance Seven-Point Closure - 2026-05-11

This note records the concrete closure state for the seven performance work
items from `docs/libvpx_performance_gap_plan_2026-05-09.md`. The current result
does not claim single-thread parity with libvpx; it closes the identified
pipeline work items with source-backed changes, phase counters, and regression
tests so remaining work can be chosen from measured encoder phases instead of
assembly-count guesses.

## 1. Measure Pipeline Work, Not Assembly Coverage

Closed by the opt-in encoder phase report in `cmd/govpx-bench` and public
`EncoderPhaseStats` counters. The report now splits reconstruction, loop-filter
pick/apply, packet write, loop-filter trial copy/filter/SSE, coefficient-cache
reuse, token-record volume, and motion-search topology.

Verification:

```sh
go test ./cmd/govpx-bench -run TestRunBenchmarkPhaseTiming -count=1
```

## 2. Reuse Accepted RD Winner Coefficients Beyond FDCT

Closed for the safe accepted path by `interRDCoeffCacheState`: RD candidates can
stage DCTs plus reusable post-quant coefficient packages, and the accepted path
consumes the winner cache when quant identity, zbin, mode shape, MB position,
and collection flags match. Coefficient reuse is disabled for oracle/stat
collection and optimizer paths where replaying the accepted path is still the
correct behavior.

Verification:

```sh
go test ./... -run 'TestInterRDCoeffCache(ReusesPreparedCoefficients|CountsDCTReuse)' -count=1
```

## 3. Prepare Coefficient Packet Tokens During Accepted Reconstruction

Closed for the serial accepted-MB path. Accepted reconstruction now builds both
branch counts and compact coefficient token records in row-major order;
`InterFramePacket.Write` consumes `PrebuiltCoefCounts` and `PrebuiltCoefTokens`
when present instead of re-classifying coefficients for packet emission.

Verification:

```sh
go test ./internal/vp8/encoder ./cmd/govpx-bench -count=1
```

## 4. Reduce Loop-Filter Duplicate Work Without Changing LF Choice

Closed by keeping the exact picker walk while caching repeated level scores,
preserving the chosen trial luma for final apply reuse, and applying only chroma
after the picker-selected luma is reused. The phase report also exposes trial
count plus copy/filter/SSE timing so future LF work can target the right part of
the trial cost.

Verification:

```sh
go test ./... -run 'LoopFilter|LFTrial|ChromaOnly' -count=1
```

## 5. Make Motion-Search Topology Observable Before Changing Search Shape

Closed by pinning libvpx-shaped realtime search configuration and exposing
topology counters for full-pel SAD calls/candidates, 4-way SAD batches,
bounds rejects, early breaks, subpel candidates, subpel variance calls, cache
hits, and subpel early breaks. The counters are stack-local at call sites and
disabled on row-worker copies to avoid races.

Verification:

```sh
go test ./... -run 'InterAnalysisSearchConfig|MotionSearchStats|SubpixelSearchCandidateCount' -count=1
```

## 6. Fuse Small DSP/Quant Calls Instead Of Adding Tiny Wrappers

Closed for the current safe slices by using fused 4-way full-pel SAD evaluation,
subpel candidate caching, whole-MB residual gather plus batched FDCT, and luma
plus chroma `FastQuantizeBlockBatch` in the coefficient path. These reduce Go
loop and Go-to-SIMD wrapper churn without changing the selected modes or token
outputs.

Verification:

```sh
go test ./internal/vp8/dsp ./internal/vp8/encoder ./... -run 'FastQuant|MotionSearchStats|PredictedMacroblock' -count=1
```

## 7. Keep Row Threading Separate From Single-Thread Parity

Closed by the row-worker encoder path and allocation regression tests. The
single-thread benchmark remains the parity comparison; row threading is tracked
as a production wall-clock path and keeps the `Threads=1` path free of worker
pool state.

Verification:

```sh
go test ./... -run TestEncoderThreadsInterFrameAllocatesZero -count=1
go test -run '^$' -bench 'BenchmarkEncodeIntoThreadingMatrix/threads_8$' -benchmem -count=1
```

## 720p Snapshot

Command:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames 240 -fps 30 \
  -bitrate 2500 -mode realtime -threads 1 -encode-only -phase-timing -format json
```

Observed on 2026-05-11:

| Threads | govpx ns/frame | govpx fps | allocs/frame | libvpx ns/frame | ratio |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 12,758,942 | 78.38 | 0 | 5,942,870 | 2.15x |
| 4 | 7,400,479 | 135.13 | 0.05 | 3,719,166 | 1.99x |
| 8 | 6,627,324 | 150.89 | 0.12 | 3,716,208 | 1.78x |

The single-thread run produced 23,568,118 prepared coefficient token records,
1,289 loop-filter trials, 22,832,898 full-pel SAD candidates, and 4,936,710
subpel variance calls. That snapshot confirms the remaining gap is still in
semantic encoder work above leaf DSP, not missing assembly coverage.
