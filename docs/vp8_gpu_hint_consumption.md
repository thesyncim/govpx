# VP8 GPU Hint Consumption: Parity-Breaking Speedups

This document describes the design for letting the VP8 encoder consume
the GPU analyzer's per-macroblock hints to skip work and run faster. It
is a **parity-breaking** code path — the encoded VP8 bitstream will
differ from the no-analysis baseline — so it is gated behind an
explicit config opt-in and documented per-optimization.

## Scope

This is **NOT** the analyzer (`gpuanalysis` package), which is
observation-only and parity-preserving. This is the *consumer* on the
encoder side that reads the analyzer's `FrameAnalysis` and changes
encode decisions based on it.

## Configuration

`VP8AnalysisConfig.UseEncodeHints` is a new bool, default `false`.

When `false`:
- `ByteParityRequired` is forced `true` by `Normalize`.
- The encoder never reads `LastFrameAnalysis()` for decision purposes.
- The bitstream matches `VP8AnalysisOff` byte-for-byte.

When `true`:
- `ByteParityRequired` is forced `false` by `Normalize` (parity cannot
  hold once hints feed decisions).
- The encoder routes through the optimizations listed below.
- The bitstream **will** differ from the no-analysis baseline.
- Quality impact is documented per-optimization.

Callers must explicitly opt in; the field is not on by default.

## Hard rules

- `UseEncodeHints` must be set together with `Mode: VP8AnalysisObserveCPU`
  or `VP8AnalysisObserveGPU`. Setting `UseEncodeHints=true` with
  `Mode: VP8AnalysisOff` produces no consumer-side effect (there are no
  hints to consume) and is a programmer error.
- The encoder must validate hint freshness: the `FrameAnalysis.FrameIndex`
  must match the frame being encoded; stale hints must be ignored.
- No hint-driven optimization may change the output container shape
  (frame count, packet count, packet size class). It may only change
  per-MB mode / motion-vector decisions inside the bitstream.

## Planned optimizations

Each optimization below has an injection point identified in the
encoder source. The actual code change is a separate follow-up PR per
optimization; this document records the design so reviewers can audit
the parity tradeoff before any of it lands.

### 1. Motion-search early-exit on `FlagStatic` MBs

**What it changes:** For MBs the GPU has flagged `FlagStatic`
(ZeroSAD ≤ 32 against the previous source), the encoder skips the
full motion-search loop and commits to `ZEROMV-LAST` with `MV=(0,0)`
without exploring other candidates.

**Where it lands:** `selectFastInterFrameModeDecisionHot` in
`encoder_inter_modes_fast.go`. Before the per-mode loop at line 146,
add a check: if `e.hintForMB(mbRow, mbCol).Flags & FlagStatic != 0`,
short-circuit to a forced `ZEROMV-LAST` decision and return.

**Expected speedup:** 10–25% encode time on low-motion content
(screen capture, talking heads). Near zero on high-motion content
(sports, action footage) because few MBs are flagged static.

**Quality impact:**
- Best case (already-converged static MBs): identical PSNR, identical
  decision.
- Worst case (encoder would have found a small motion that gave
  slightly lower SAD against the reconstructed reference): ≤ 0.1 dB
  PSNR loss per affected MB. Almost always below visual threshold.
- Recommendation: validate on consumer corpus before production.

### 2. Motion-search radius reduction on `SearchRadius` hint

**What it changes:** The diamond/nstep motion search currently uses a
fixed maximum radius. With hints, the radius is set to
`hint.SearchRadius` (the value the GPU computed from ZeroSAD / variance
classification: 1 for static, 4 for moderate motion, 8 for high motion).

**Where it lands:** `newFullPelMotionSearch` in
`encoder_motion_search.go:137`. Multiply the `bounds.HalfRange` by a
hint-derived scale before constructing the search.

**Expected speedup:** 5–15% encode time. Lower than #1 but covers all
inter MBs, not just the static ones.

**Quality impact:**
- For static MBs the radius is reduced from 16 to 1; behaviour is
  identical to #1.
- For moderate-motion MBs the radius is reduced from default to 4. May
  miss a slightly-better MV on diagonal motion; PSNR loss expected
  ≤ 0.2 dB on action content, undetectable on regular content.

### 3. Skip-MB on `FlagSkipLikely` (force MB skip)

**What it changes:** For MBs flagged `FlagSkipLikely` (static + flat),
the encoder emits a "skip" token for the entire MB without computing
residual coefficients.

**Where it lands:** Same as #1 (mode decision entry), but the override
is "skip everything" rather than "ZEROMV-LAST".

**Expected speedup:** 15–30% encode time on highly static + flat
content (gradient backgrounds, slide presentations).

**Quality impact:**
- Worst case: faint banding on slow gradients where the encoder would
  otherwise have added a small residual. Visible only on critical
  inspection.
- Recommendation: gate this further behind a per-frame keyframe-distance
  check so post-keyframe artifacts have time to wash out.

### 4. RD-cost variance from GPU (parity-PRESERVING)

This one preserves parity but is listed here for completeness because
it overlaps with the hint-consumption infrastructure. See
`docs/vp8_gpu_analysis_future.md` for the GPU side.

**What it changes:** The encoder's per-MB true-variance computation
(`dsp.Variance16x16`) is replaced with a GPU-precomputed value. The GPU
must compute true variance (sum of squared deviations, not the L1 proxy
the current analyzer uses).

**Where it lands:** Audit `dsp.Variance16x16` call sites in
`encoder_inter_modes_fast.go` and `encoder_inter_breakout.go`; route
each through a cache backed by GPU output.

**Expected speedup:** 3–8% encode time. Smaller than the parity-breaking
optimizations because variance is a smaller share of CPU time.

**Quality impact:** None by construction (parity-preserving).

## How quality regression is monitored

Before any of these optimizations land, a regression suite must check:

1. **Frame-level SSIM** vs the no-analysis baseline, on a corpus of at
   least 10 representative VP8 clips spanning:
   - Talking-head / videoconferencing
   - Screen capture / slide deck
   - Action / sports / panning camera
   - Animation
2. **Bitrate parity check**: total stream size with hints on must be
   within ±2% of stream size with hints off at matched-quality
   configurations.
3. **Visual diff spot-checks**: at least one keyframe and one P-frame
   per clip diffed by a human reviewer.

This regression suite is **not** in `make ci` because it requires the
corpus checkout; it lives under `validation/hint-quality/` and is run
manually before merging hint-consumption optimizations.

## Why parity-breaking is the only practical path for big speedups

The encoder is already integer / fixed-point. The expensive work it
does at `cpu_used=8` is **already optimized away in the parity-preserving
sense**: motion search is bounded, mode decision is fast-path, entropy
coding is mature. The remaining CPU cost is unavoidable *if* you want
the encoder to make the same decisions it would have without hints.

To go faster, the encoder has to make *different* decisions. The hint
infrastructure makes those different decisions *more informed* than
naive shortcutting (e.g. just lowering quality knobs) by giving the
encoder a precomputed signal about which MBs are safe to short-circuit.

So the hint-consumption path is the architectural complement to the
GPU analyzer: GPU produces signal, encoder uses signal to skip work,
quality is preserved because the GPU signal correctly identifies the
MBs where the encoder's own work would have been redundant.

## Status

- Config plumbing: **landed** (this commit).
- Encoder consumers (#1, #2, #3, #4 above): **not yet landed**. Each is
  a separate follow-up PR with its own benchmark + quality validation.

The order I recommend landing them:

1. **#4 (RD-cost variance, parity-preserving)** — proves the encoder
   side can consume GPU output without breaking parity. Foundation for
   everything else.
2. **#1 (motion-search early-exit)** — biggest parity-breaking win,
   localized code change.
3. **#2 (radius reduction)** — broader coverage than #1.
4. **#3 (force-skip)** — most aggressive, save for last after #1 and
   #2 are validated in production.
