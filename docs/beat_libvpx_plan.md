# Plan: beat libvpx VP8 in govpx-bench

Baseline (320x240, 60 frames, 30 fps, 800 kbps CBR, realtime, cpu-used=8,
captured from `cmd/govpx-bench` with parity flags shipped to vpxenc 1.16.0):

| metric | govpx | libvpx | gap |
| --- | ---: | ---: | ---: |
| encode fps | 91.6 | 1517 | govpx 16.6x slower |
| ns/frame (encode-only) | 10,916,070 | 659,183 | 16.6x |
| PSNR (dB) | 34.86 | 37.28 | govpx -2.42 dB |
| SSIM | 0.9810 | 0.9740 | govpx +0.007 |
| output bytes | 188,541 | 185,134 | govpx +1.84% |
| bitrate error | -5.7% | -7.4% | both undershoot |

Two gaps to close: speed (16x) and PSNR (-2.4 dB). Bitrate matches well,
SSIM is already ahead. The framing below assumes the bench harness from
`cmd/govpx-bench` is the success metric.

## What we have today

- **No SIMD anywhere.** No `.s` files, no `_arm64.go`/`_amd64.go` in the
  tree. `internal/vp8/dsp/sad.go:SAD16x16` is a 256-iteration scalar
  byte loop; libvpx does the same in ~12 NEON instructions (or 4-6
  AVX2). This single fact accounts for most of the speed gap.
- **Speed tiers are wired.** `encoder.go` and `encoder_reconstruct.go`
  branch on `CpuUsed`; `libvpxInterFrameFirstStep` /
  `libvpxInterFrameSpeedAdjust` follow libvpx's schedule. Algorithmic
  skipping is roughly aligned with libvpx speed-8.
- **Encoder is large**: `encoder_reconstruct.go` is 5854 lines / 276
  funcs of mode-decision and RD logic. The Go code is the work; SIMD is
  the missing layer below it.
- **22 DSP entry points** across SAD, intra, IDCT, loop filter,
  subpixel, variance, walsh. Each is a candidate for a NEON+SSE
  pack.
- **Quality is RD-efficiency, not coding tools.** SSIM ahead, PSNR
  behind, bitrate within 2% - we're spending bits in the wrong places,
  not missing a tool.

## Path A - speed (close 16x)

### Baseline profile (2026-05-07, arm64 / Apple Silicon)

Captured via `govpx-bench -width 320 -height 240 -frames 600 -fps 30
-bitrate 800 -mode realtime -cpuprofile=...`. 17.87 s of samples over
600 frames. Top 10 flat (self-time) leaves:

| % flat | function | category |
| ---: | --- | --- |
| 12.59 | `dsp.varianceBlock` | subpel variance |
| 11.14 | `dsp.varFilterBlock2DBilinearSecondPass` | subpel variance bilinear |
| 10.63 | `dsp.varFilterBlock2DBilinearFirstPass` | subpel variance bilinear |
|  8.45 | `dsp.sixTapPredict` | inter subpel reconstruction |
|  7.55 | `dsp.sadBlockLimit` | motion search SAD |
|  3.13 | `encoder.FastQuantizeBlock` | quantization |
|  2.57 | `encoder.findTreeToken` | entropy lookup |
|  2.46 | `treeTokenCost` | RD cost |
|  2.24 | `macroblockSADLimited` | motion search wrapper |
|  1.57 | `dsp.subpelVariance` | subpel variance wrapper |

Top-3 functions are all subpel variance work; together they are
**34.4% of total CPU**. Adding `sixTapPredict` (8.5%) and `sadBlockLimit`
(7.5%) brings the SIMDable top-5 to **50.4%**. IDCT, loop filter, and
intra prediction are each <1.5% - **lower priority than the original
plan assumed**.

Cumulative: `selectFastInterFrameModeDecision` at 59.4% drives most of
the work; mode decision calls into subpel variance to refine motion
vectors, which is where the bilinear/variance hot path lives.
`benchmarkQualityMetrics` (34.4% cum) is the bench encoding the same
frames a second time for PSNR/SSIM - production usage would not pay
this.

### Reordered priorities

1. **Subpel variance bilinear NEON (arm64) + SSE2 (amd64).** Three
   functions: `varFilterBlock2DBilinearFirstPass`,
   `varFilterBlock2DBilinearSecondPass`, `varianceBlock`. Together
   34.4% of CPU. This is the single highest-leverage SIMD target.
   Anticipated win: 3-5x on these functions = ~1.5-2x overall.
2. **Six-tap NEON.** `dsp.sixTapPredict` is 8.5% flat. Same shape as
   bilinear - vertical pass + horizontal pass + clamping. Used in
   inter reconstruction once the MV is locked.
3. **SAD NEON (arm64) + SSE2 (amd64).** `sadBlockLimit` (with the
   early-exit limit) is 7.5%. Implement the limit-aware variant; the
   plain SAD16x16 wraps it. Anticipated win: 4-6x on the function.
4. **Quantize NEON.** `FastQuantizeBlock` is 3.1% - a per-coefficient
   round + threshold + zigzag pack, vectorizes well into 8x16-bit
   lanes.
5. **Tighten scalar fallback.** Bounds-check elision (single
   `_ = src[len-1]` hint at the top of inner loops), `unsafe.Add`
   for stride math, avoid `[]byte` reslicing in inner loops. Apply
   to `varianceBlock`, `sixTapPredict`, `sadBlockLimit`,
   `findTreeToken`, `treeTokenCost`. Anticipated win: 1.2-1.5x even
   without SIMD, by letting the Go compiler keep the inner loop in
   registers.
6. **Mode-decision pruning.** `selectFastInterFrameModeDecision` is
   59.4% cum but only 0.7% flat - the work happens in its callees.
   After steps 1-3, re-profile and look for whether any single
   candidate MV explores too aggressively at speed=8. libvpx's
   `vp8_mv_bit_cost` early-exit is in `mvCost`; verify ours matches.
7. **Loop filter / IDCT / intra.** Each <1.5% flat. Skip until the
   first-pass SIMD work lands.

Realistic combined ceiling: scalar tightening 1.2-1.5x, SIMD on
items 1-4 carries another 3-5x of those 50% of cycles, so overall
~3-5x. That puts govpx in the 280-460 fps range at 320x240, still
short of libvpx's 1517 but materially closer. The remaining gap is
in the long tail; reaching parity needs the full DSP pack and likely
mode-decision pruning.

## Path B - quality (close -2.4 dB)

1. **Subpixel-filter parity test.** `internal/vp8/dsp/subpixel.go` was
   recently edited; bit-exact match to libvpx's 6-tap on every (xoff,
   yoff) pair is mandatory for inter PSNR. Add a parity oracle test
   alongside `internal/vp8/dsp/subpixel_test.go` that compares
   per-pixel against a libvpx-generated reference for all 16
   sub-pel positions on a corpus of source blocks.
2. **Quantizer / zbin parity.** Confirm govpx's `dc_qlookup`,
   `ac_qlookup`, zbin, and rounding tables match libvpx exactly at
   every q-index. A single off-by-one in zbin produces measurable PSNR
   loss that is uniform across the frame (which would explain the
   "high SSIM, lower PSNR" symptom).
3. **Trellis / RDOQ.** `encoder_viterbi_test.go` exists - verify the
   trellis is configured the same way libvpx is at speed=8 (typically
   off for realtime). Both encoders should be making the same
   trade-off; if govpx has the trellis on while libvpx has it off,
   govpx is paying compute for no PSNR; if both are off, look
   elsewhere.
4. **Skip-block decision.** `encoder_skip.go` decides when to emit a
   skipped block. False-positive skips are a direct PSNR loss with no
   bit savings to show for it. Audit threshold against libvpx's
   `vp8_skip_decision` at the same q-index.
5. **Cyclic refresh / AQ-mode 3.** libvpx realtime CBR uses
   segment-level QP boosts on dirty MBs. `encoder_segmentation.go`
   probably has hooks; verify they fire on realtime CBR. Missing
   cyclic refresh is consistent with the SSIM-up-PSNR-down profile
   (govpx may be smoothing instead of selectively boosting fidelity).
6. **Q-distribution audit.** Bench's quantizer histogram shows
   q in [10..50] mean 41.15 for govpx. Compare against libvpx on the
   same input - if libvpx picks a tighter spread (say 38-44), the
   bitrate budget is being misallocated.

Anticipated path: items 1-3 are cheap to verify and likely close
1.0-1.5 dB on their own if any of them is wrong. Items 4-5 close the
remainder.

## Path C - methodology

1. **Real content corpus.** Synthetic gradient frames have trivial
   motion - speed numbers there overstate libvpx's real-world lead.
   Add a Derf-collection corpus (akiyo, bus, foreman, station) to the
   bench so quality numbers reflect realistic content.
2. **Multiple resolutions.** Today: 320x240. Add 640x360 and 1280x720
   to verify SIMD wins scale - some hot functions only show up
   meaningfully at the higher resolutions.
3. **Bench-as-CI gate.** Make `make bench` reproducible and emit a JSON
   diff vs the last committed baseline so regressions are caught
   automatically.
4. **`-cpuprofile` / `-memprofile` flags** in `cmd/govpx-bench`. The
   bench is the right harness for profiling; the unit tests aren't.

## Ordered task list

1. ~~Add `-cpuprofile`/`-memprofile` flags to `cmd/govpx-bench` and
   capture the baseline flame graph.~~ **Done 2026-05-07** - see the
   "Baseline profile" section above.
2. Subpel-variance NEON (arm64) + SSE2 (amd64): `varianceBlock`,
   `varFilterBlock2DBilinearFirstPass`,
   `varFilterBlock2DBilinearSecondPass`. 34.4% of CPU; highest leverage.
3. Six-tap NEON: `sixTapPredict` (8.5%).
4. SAD NEON: `sadBlockLimit` (limit-aware variant; 7.5%).
5. Quantize NEON: `FastQuantizeBlock` (3.1%).
6. Tighten scalar fallbacks (BCE hints + `unsafe.Add` strides) for
   the same hot functions, so non-arm64/non-amd64 builds also gain.
7. Subpixel-filter parity oracle vs libvpx. Fix any mismatches.
8. Quantizer/zbin parity test against libvpx's tables.
9. Audit `encoder_skip.go` thresholds and `encoder_segmentation.go`
   cyclic-refresh wiring.
10. Add Derf-collection corpus + 640x360 / 1280x720 bench targets.
11. Repeat profile + iterate. After steps 2-5 the next biggest leaf
    likely shifts; expect to add IDCT/loop-filter/intra NEON only if
    they climb back into the top 10.

## Caveats

- libvpx ships AVX2 on x86 alongside NEON on arm64. A realistic first
  milestone is "match libvpx on Apple Silicon"; AVX2 parity on x86 is a
  second pass.
- Go assembly via `avo` (amd64) and direct NEON `.s` files (arm64) is
  tedious but tractable; libvpx's own `.asm`/`.S` files are a useful
  structural reference even though the calling convention differs.
- Quality fixes interact - expect 2-3 iterations on items B.1-B.5
  before the PSNR gap closes. Don't chase a single dB delta to ground
  before the corpus is in place; synthetic content will mislead.

## Rough timeline

- Profile + scalar tightening: 1-2 days
- SAD / Subpel / IDCT NEON: 3-5 days
- Loop filter + variance NEON: 2-3 days
- Quality investigation and RD fixes: 3-5 days
- Methodology, corpus, regression bench: 1-2 days

Total: 2-3 weeks of focused work to potentially match-or-exceed libvpx
in realtime CBR at 320x240 on Apple Silicon.
