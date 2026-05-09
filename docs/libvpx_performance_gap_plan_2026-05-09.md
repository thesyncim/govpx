# govpx vs libvpx VP8 performance gap plan

Date: 2026-05-09  
Architectural owner: current Codex investigation  
Scope: VP8 realtime CBR encode, Apple Silicon arm64, libvpx v1.16.0 parity flags

## Executive summary

The current gap is real and reproducible, but it is no longer the old
"missing SIMD" problem. This checkout already has NEON/AVX2/SSE2 coverage for
the former top DSP leaves. The remaining single-thread gap is a pipeline and
control-flow problem: govpx does more full-frame and per-MB work around mode
decision, loop-filter search, coefficient accounting, and packetization than
libvpx's C implementation.

Local measurements from this investigation:

| workload | govpx | libvpx | gap |
| --- | ---: | ---: | ---: |
| `320x240`, 600 frames, 800 kbps, realtime, cpu-used=8, threads=1 | 1.796 ms/frame | 0.688 ms/frame | govpx 2.61x slower |
| `1280x720`, 120 frames, 2500 kbps, realtime, cpu-used=8, threads=1 | 16.033 ms/frame | 5.436 ms/frame | govpx 2.95x slower |

Quality and bitrate are not the blocker in these runs. The 720p run was
bitrate-matched within 0.31% output bytes and govpx was +0.35 dB PSNR /
+0.020 SSIM. The speed gap is therefore not explained by govpx doing a
substantially better encode.

Important distinction: the reproduced `~3x` gap is single-thread on both
sides (`cmd/govpx-bench` passes `--threads=1` to libvpx). Missing real
`EncoderOptions.Threads > 1` matters for production wall-clock parity, but it
does not explain the reproduced single-thread number.

## Reproduce

Use the project-built libvpx tools. `cmd/govpx-bench` will auto-locate them
under `internal/coracle/build` and run `make oracle-tools` if needed.

```sh
GOCACHE=/Users/thesyncim/GolandProjects/govpx/.gocache \
GOTOOLCHAIN=go1.26.1 \
go run ./cmd/govpx-bench \
  -width 320 -height 240 -frames 600 -fps 30 -bitrate 800 \
  -mode realtime -threads 1 -format json \
  -cpuprofile /private/tmp/govpx-bench-320x240.cpu
```

```sh
GOCACHE=/Users/thesyncim/GolandProjects/govpx/.gocache \
GOTOOLCHAIN=go1.26.1 \
go run ./cmd/govpx-bench \
  -width 1280 -height 720 -frames 120 -fps 30 -bitrate 2500 \
  -mode realtime -threads 1 -format json \
  -cpuprofile /private/tmp/govpx-bench-1280x720.cpu
```

Focused encoder-only benchmark:

```sh
GOCACHE=/Users/thesyncim/GolandProjects/govpx/.gocache \
GOTOOLCHAIN=go1.26.1 \
go test . -run '^$' \
  -bench 'BenchmarkEncodeIntoThreadingMatrix/threads_1$' \
  -benchmem -benchtime=20x \
  -cpuprofile /private/tmp/govpx-thread1-720.cpu
```

Threading no-op sanity check:

```sh
GOCACHE=/Users/thesyncim/GolandProjects/govpx/.gocache \
GOTOOLCHAIN=go1.26.1 \
go test . -run '^$' -bench 'BenchmarkEncodeIntoThreadingMatrix' \
  -benchtime=5x -benchmem
```

## Current evidence

Primary benchmark surface:

- `cmd/govpx-bench/main.go` measures govpx in-process `EncodeInto`, runs
  libvpx through `vpxenc`, parses vpxenc's encode-time progress line, and
  reports govpx/libvpx ratios.
- `cmd/govpx-bench/main.go` uses parity flags for CBR, q-range, keyframe
  cadence, single pass, zero lag, `--threads`, `--rt`, and `--cpu-used=8`.
- `README.md` currently records the public headline as `2-3x slower`.

720p whole-bench pprof (`/private/tmp/govpx-bench-1280x720.cpu`) shows the
bench itself consumes non-trivial time in quality metrics and libvpx setup, so
do not use raw whole-process percentages as the final encoder breakdown.
Still, focused on `EncodeInto`, the large cumulative nodes are:

| area | pprof evidence | meaning |
| --- | ---: | --- |
| Inter-frame build/reconstruct | `buildReconstructingInterFrameCoefficientsWithSegmentation` 37.7% cum | Main frame pipeline dominates |
| Loop-filter level picking | `pickLoopFilterLevelFull` / `loopFilterTrialLumaSSE` 13.8% cum | Multiple trial copy/filter/SSE passes before final filter |
| Mode decision | `selectFastInterFrameModeDecision` 18.6% cum | Still significant after DSP SIMD landed |
| Candidate RD coefficient work | `buildPredictedMacroblockCoefficientsRD` 11.4% cum | Transform/quant/rate/distortion during candidate scoring |
| Token/probability writing | `WriteCoefficientInterFrameWithProbabilityBaseScratch` 9.4% cum | Full coefficient/probability pass after RD |
| Final loop filter | `applyReconstructionLoopFilter` 5.2% cum | Separate pass after LF search |
| SIMD leaves | `varianceBlock16x16NEON`, loopfilter NEON, SAD NEON, six-tap NEON | Covered but still called often |

Encoder-only `BenchmarkEncodeIntoThreadingMatrix/threads_1` confirms the
720p steady-state cost is about `15-16 ms/op` with no useful scaling from
`Threads`. The alloc count in that benchmark includes the synthetic test frame
created inside the loop; existing encode hot-path allocation tests still guard
zero steady-state `EncodeInto` allocations.

## Why the gap remains

### 1. Full loop-filter search is too expensive

`encodeInterFrameAttempt` calls `pickLoopFilterLevel`, then applies the chosen
filter to the real analysis frame. The full picker tries multiple candidate
levels; each trial copies luma into `loopFilterPick`, applies a candidate
filter, and computes source-vs-filtered SSE. Then the accepted level filters
the real frame.

Relevant files:

- `encoder.go`: `encodeInterFrameAttempt`
- `encoder.go`: `pickLoopFilterLevelFull`
- `encoder.go`: `loopFilterTrialLumaSSE`
- `encoder.go`: `applyReconstructionLoopFilter`

This is parity-sensitive because LF picker bias and trial order already caused
visible oracle divergence. Optimize by reducing repeated work or parallelizing
trial scoring, not by casually changing the chosen level.

### 2. Mode decision still does expensive candidate RD work

For candidate residual scoring, `estimateInterResidualRDAccounting` reconstructs
the predictor into `e.analysis.Img`, then runs
`buildPredictedMacroblockCoefficientsRD`, which gathers residuals, does batched
DCT, quantizes block-by-block, computes token rate, and computes distortion.
After the mode is chosen, the main encode path does final coefficient build and
reconstruction again.

Relevant files:

- `encoder_reconstruct.go`: `estimateInterResidualRDAccounting`
- `encoder_reconstruct.go`: `buildPredictedMacroblockCoefficientsRD`
- `encoder_reconstruct.go`: final accepted-mode path around
  `buildPredictedMacroblockCoefficients` and `reconstructInterAnalysisMacroblock`

The likely win is reuse: carry enough accepted-candidate scratch forward to
avoid rebuilding coefficients/reconstruction for the chosen winner when the
candidate accounting already computed exactly the same data.

### 3. Realtime speed=4 still uses NSTEP at 720p and smaller

The code explicitly documents that the previous NSTEP plus iterative subpel
path remains for 720p and below, while HEX search is enabled at speed >= 4 only
for `>=1920x1080` or generally for speed > 4. The comment says the old path
does about 70 SAD calls per NEWMV while libvpx's realtime path uses hex
topology and often breaks early.

Relevant files:

- `encoder_reconstruct.go`: `interAnalysisSearchConfig`
- `encoder_reconstruct.go`: `subpelSearchCtx.subpelVarianceForQuarterMV`

Do not simply flip 720p to HEX; previous attempts broke oracle distribution
gates. The task is to make a libvpx-shaped HEX path pass the 128/256/720p
inter-mode and EOB scoreboards, or to prove call count is no longer a top
contributor after LF/candidate reuse work.

### 4. Coefficient data is walked repeatedly

After reconstruction, packetization builds coefficient probability updates over
the frame, adapts mode probabilities, writes modes, and writes coefficient
tokens. Projected-size accounting can also scan coefficients for entropy
savings. RD mode decision already computed token rates for candidate scoring,
but final packet output walks the data again.

Relevant files:

- `internal/vp8/encoder/interframe.go`:
  `BuildInterCoefficientProbabilityUpdates`, `WriteInterCoefficientTokenGrid`
- `encoder.go`: projected frame size / entropy savings helpers

Some repeated walks are necessary because bitstream output needs final adapted
probabilities. The architectural goal is to cache branch counts, entropy
savings, and token context side-data per accepted attempt so the frame pays for
each semantic pass once.

### 5. Remaining DSP gaps are now second-order, not the whole answer

There are still useful low-level wins:

- `SixTapPredict16x8` and `SixTapPredict8x16` fall through to scalar on arm64
  and amd64.
- `subpelVariance` still stages through stack buffers and dispatches multiple
  tiny kernels per candidate.
- `FastQuantizeBlockBatch` exists but the reconstructing inter path still calls
  single-block `FastQuantizeBlock` from `quantizeEncodedBlock`.

These should be tackled after or alongside pipeline work, but they are not
enough by themselves to close 3x. The largest leaves from the old no-SIMD plan
are already gone.

### 6. Missing row threading is a separate production gap

`EncoderOptions.Threads` accepts values > 1, but the current encoder collapses
them onto the same serial macroblock loop. `BenchmarkEncodeIntoThreadingMatrix`
shows no useful scaling. This does not explain the single-thread libvpx
comparison because the bench passes `--threads=1`, but production users will
expect libvpx-like scaling when they ask for threads.

Relevant files:

- `encoder.go`: `EncoderOptions.Threads` comment
- `encoder_threading.go`: `effectiveThreadCount`
- `encoder_row_worker.go`: row worker pool scaffold
- `encoder_threading_test.go`: scaling benchmark

## Work lanes

### Lane A: measurement contract and dashboards

Owner type: benchmark/instrumentation agent.

1. Add an encode-only JSON mode or flag to `cmd/govpx-bench` that skips
   `benchmarkQualityMetrics`, reference decode, and quality computation while
   preserving the libvpx comparison.
2. Add optional internal phase timers around:
   mode decision/reconstruction, LF picker, final LF, coefficient probability
   updates, token writing, projected-size accounting, denoise, frame copies.
3. Emit a compact per-phase table for govpx only; do not put timers in hot
   paths unless the flag is enabled.
4. Re-baseline 320x240, 720p, and 1080p with `-count` or repeated runs.

Acceptance:

- Report includes govpx/libvpx ns/frame plus govpx phase percentages.
- Numbers are reproducible within normal laptop noise across three runs.
- Default bench behavior remains backward compatible.

### Lane B: loop-filter picker cost

Owner type: encoder pipeline agent.

1. Count how many LF trial levels are evaluated per frame at 320x240, 720p,
   and 1080p.
2. Split `loopFilterTrialLumaSSE` time into copy, candidate filter, and SSE.
3. Investigate reusing a single copied source plane and applying trials into
   rotating scratch buffers rather than copying full luma for every trial.
4. Consider parallel trial scoring only after serial scratch reuse is proven;
   keep `Threads=1` deterministic and unchanged.
5. Preserve exact chosen LF level by comparing `TestOracleLFTrialDiag` and the
   reconstruction/loop-filter scoreboards.

Acceptance:

- Same LF choice and oracle outputs on scoreboards.
- 720p `pickLoopFilterLevelFull` cumulative time reduced materially.
- No steady-state allocation regression.

### Lane C: accepted-candidate reuse

Owner type: mode-decision/reconstruction agent.

1. Add instrumentation to count how often the chosen inter mode already had a
   full `estimateInterResidualRDAccounting` coefficient result.
2. Design an `interCandidateScratch` that can hold predictor identity,
   coefficients, EOBs, token contexts, distortion, and rate for the winning
   candidate without aliasing later candidates.
3. In the accepted inter path, reuse the winning candidate's coefficient data
   instead of calling `buildPredictedMacroblockCoefficients` again.
4. If safe, avoid the predictor-only reconstruct followed by final reconstruct
   for whole-MB inter modes by preserving the predictor and applying residuals
   once.
5. Start with single-thread only; row-threading can own scratch duplication
   later.

Acceptance:

- `TestEncodeIntoMultiResolutionAllocatesZero` stays zero allocation.
- `make scoreboard` remains green.
- 720p `buildPredictedMacroblockCoefficientsRD` plus final coefficient build
  cumulative time drops.

### Lane D: coefficient traversal consolidation

Owner type: bitstream/probability agent.

1. Trace exact consumers of coefficient branch counts, coefficient probability
   updates, entropy savings, and token writing.
2. Store per-frame branch counts produced during final coefficient build or
   first packetization pass.
3. Reuse those counts for `BuildInterCoefficientProbabilityUpdates` and
   projected-size savings where the math is identical.
4. Keep final token writing separate unless a safe writer-side cache emerges;
   bitstream order and probability adaptation are correctness-critical.

Acceptance:

- Packet bytes remain identical for existing tests.
- `WriteCoefficientInterFrameWithProbabilityBaseScratch` and projected-size
  helpers show lower cumulative time.
- No public API changes.

### Lane E: 720p realtime search topology

Owner type: motion-search parity agent.

1. Add a call-count instrument for SAD, subpel variance, full-pel candidates,
   and subpel candidates per NEWMV.
2. Compare current govpx counts against libvpx oracle traces for 320x240,
   720p, and 1080p.
3. Prototype HEX at 720p behind a private flag and identify which oracle gates
   fail: mode distribution, EOB sum, size ratio, or reconstruction.
4. Adjust tie-breaks, search start, thresholds, or step-subpel gating until
   720p HEX is libvpx-shaped enough to pass existing scoreboards.
5. Only then remove or lower the `>=1920x1080` gate.

Acceptance:

- 720p bench improves without regressing `TestOracleInterModeDistribution`,
  `TestOracleCandidateRateScoreboard`, and reconstruction scoreboards.
- Call counts demonstrate a real reduction, not just a reshuffle.

### Lane F: remaining DSP finish work

Owner type: DSP/SIMD agent.

1. Add arm64 and amd64 SIMD for `SixTapPredict16x8` and `SixTapPredict8x16`.
2. Explore fused subpel variance kernels for the common 16x16 case to reduce
   stack staging and Go wrapper overhead.
3. Thread `FastQuantizeBlockBatch` into the reconstructing inter path where
   contexts allow it. The 16 Y blocks and 8 UV blocks share quant tables, but
   token contexts still need scan-order handling.
4. Keep scalar references and randomized SIMD parity tests.

Acceptance:

- SIMD parity tests cover every x/y offset and edge stride.
- Microbench wins translate into `cmd/govpx-bench`, not just isolated kernels.

### Lane G: real row threading

Owner type: concurrency/encoder architecture agent.

1. Treat this as a separate feature from the single-thread gap.
2. Use `rowWorkerPool` and `rowProgress`; do not use per-MB channels.
3. Give each worker private mode-decision scratch, token contexts, and adaptive
   RD-threshold state, mirroring libvpx's per-worker `MACROBLOCK` copies.
4. Decide product contract: threaded bitstream may differ from single-thread
   like libvpx, or deterministic merge must replay adaptive state after rows.
5. Add threaded scoreboards or separate baselines if output differs.

Acceptance:

- `BenchmarkEncodeIntoThreadingMatrix` shows scaling for `Threads=2/4/8`.
- Race detector is clean for targeted tests.
- Public docs clarify deterministic vs libvpx-style threaded output.

## Prioritized sequence

1. Lane A first. Without phase timers, agents will optimize whatever pprof
   happens to sample.
2. Lane B next. LF picker is the largest obvious non-mode-decision full-frame
   multiplier and has a clear preservation contract.
3. Lane C in parallel with B if there is an experienced encoder agent.
4. Lane D after C, because accepted-candidate reuse changes what coefficient
   data can be cached.
5. Lane E after B/C show whether motion search remains high enough to justify
   parity risk.
6. Lane F continuously when a DSP agent is available, but do not let it block
   the pipeline work.
7. Lane G after single-thread work unless the product requirement is
   multi-core throughput now.

## Eight-scout correction, 2026-05-09

The latest scout pass changes the working theory:

- Quality and output bitrate are mandatory gates. Encode-only numbers are
  profiling data only.
- `cmd/govpx-bench` must score PSNR/SSIM from the same govpx packets used for
  timing and byte counts. Re-encoding for quality can hide wall-clock autospeed
  drift.
- Current 720p realtime `cpu-used=8` parity runs use the fast partial LF
  picker, not full-frame LF search. Do not chase the old "full LF search at
  720p" hypothesis without proving it from `lf_trial` rows.
- The strongest extra-call candidate is coefficient/probability accounting:
  accepted coefficients are scanned for entropy savings, scanned again for
  probability updates, then walked again for token writing.
- Output-invisible oracle sidecars (`OracleY1DCEOB1`, stale Y2 snapshots) must
  be gated behind oracle tracing. They are useful diagnostics, not production
  work.
- Mode-decision pruning is viable only when side effects are preserved:
  threshold mutation, static-breakout behavior, and oracle candidate traces
  make broad RD lower-bound skips risky.
- Runtime profiles show stack pressure and benchmark fixture allocation noise.
  Do not treat `BenchmarkEncodeIntoThreadingMatrix` allocs as steady-state
  encoder heap allocations unless the fixture is prebuilt.
- Large positional helper signatures should be replaced by named parameter
  structs or smaller helpers before adding more booleans.

Latest quality-safe local result after the harness/oracle-sidecar cleanup:

```text
1280x720, 120 frames, 2500 kbps, realtime, threads=1
govpx: 13.925 ms/frame
libvpx: 5.307 ms/frame
slowdown: 2.62x
bytes ratio: 0.9991
PSNR delta: +0.441 dB
SSIM delta: +0.0226
allocs/frame: 0
```

This is progress, not closure. The next agents should prioritize a
byte-identical coefficient branch-count cache that feeds entropy savings and
probability updates from one accepted-frame count pass, then benchmark whether
token writing still dominates.

## Gates before claiming progress

Fast local gates:

```sh
GOCACHE=/Users/thesyncim/GolandProjects/govpx/.gocache \
GOTOOLCHAIN=go1.26.1 \
go test . -run 'TestEncodeInto.*AllocatesZero|TestLibvpx.*Speed|TestOracle.*Scoreboard' -count=1
```

Production gate:

```sh
make verify-production
```

Benchmark gate:

```sh
GOCACHE=/Users/thesyncim/GolandProjects/govpx/.gocache \
GOTOOLCHAIN=go1.26.1 \
go run ./cmd/govpx-bench \
  -width 1280 -height 720 -frames 120 -fps 30 -bitrate 2500 \
  -mode realtime -threads 1 -format json |
jq -e '
  .comparison_vs_reference.bitrate_ratio_vs_reference >= 0.99 and
  .comparison_vs_reference.bitrate_ratio_vs_reference <= 1.01 and
  .comparison_vs_reference.psnr_delta_db >= 0 and
  .comparison_vs_reference.ssim_delta >= 0 and
  .dropped_frames == 0 and
  .quality_frames == 120 and
  .reference.quality_frames == 120
'
```

Claim a win only when:

- 720p `ns_per_frame_ratio_vs_reference` moves down by at least 10% relative
  to the current 2.95x baseline.
- bitrate ratio stays within 1%.
- PSNR/SSIM do not regress beyond existing parity tolerance.
- allocs/frame remains effectively zero in the steady-state encode path.

## Notes for future agents

- Current 2026-05-09 follow-up: `cmd/govpx-bench` now exposes
  `-autospeed-calibration` for the old deterministic parity timing model.
  Leaving calibration off measures production wall-clock autospeed and moves
  the 30-frame 720p encode-only ratio from about 3.76x to about 2.32x on the
  M4 Max test host, but it does **not** satisfy the win gate: full-quality
  720p output drifted to about 1.25x libvpx bytes and -0.37 dB PSNR. Treat
  autospeed calibration as a measurement/control knob, not a closed perf fix.
  The parity-preserving hot path remains full loop-filter selection plus
  coefficient packing under Speed=4.
- A temporary LF-only experiment (`loopFilterUsesFastSearch` returning true at
  realtime Speed=4) is also not a global fix. It moved 320p encode-only to
  about 1.75x with bytes within 0.2%, but 720p stayed around 3.25x and bytes
  drifted about 2.45%. If revisited, scope it behind a small-frame policy and
  prove PSNR/SSIM plus bitrate before changing the libvpx speed-feature gate.
- Do not resurrect the old `docs/beat_libvpx_plan.md` assumption that there is
  "no SIMD anywhere"; that was true for an older checkout and is false now.
- Be skeptical of whole-bench pprof profiles because they include govpx
  quality encode/decode, libvpx subprocess setup, file writes, and SSIM/PSNR.
- Be equally skeptical of isolated DSP microbench wins. The current problem is
  the number of passes and candidates, not just nanoseconds per primitive.
- Preserve oracle parity first. Several fast-looking changes already have
  comments documenting why they broke distribution or reconstruction gates.
