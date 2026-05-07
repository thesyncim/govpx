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

The order below is pprof-leaf-driven; each step is conditional on the
previous profile pass confirming it is still the top hot leaf.

1. **Profile first.** Add a `-cpuprofile` flag to `cmd/govpx-bench` and
   capture a 10-second flame graph at the baseline config. Document the
   top 10 leaves before writing any assembly. Expected suspects:
   `SAD16x16`, `SixTapPredict{16x16,8x8}`, `IDCT4x4Add`,
   `MBLoopFilter*Edge`, `Variance16x16`, `IntraDCPredict16x16`,
   `BilinearPredict*`, quantize/dequant inner loops.
2. **SAD NEON (arm64) + SSE2 (amd64).** Highest-leverage single
   function. Start with `SAD16x16`, then 8x16/16x8/8x8/4x4. Keep the
   pure-Go path as the fallback under build-tag selection.
   Anticipated win: 3-5x on motion-heavy frames.
3. **Sub-pel NEON.** `SixTapPredict16x16/8x8/8x16/16x8/8x4/4x4` and the
   bilinear variants - hottest functions in inter prediction.
   Anticipated win: 2-4x on inter-coded macroblocks.
4. **IDCT NEON.** `IDCT4x4Add` + `DCOnlyIDCT4x4Add`. Called per coded
   block; cheap to vectorize, big aggregate.
5. **Loop filter NEON.** `MBLoopFilter{Horizontal,Vertical}Edge` and
   `LoopFilter{Horizontal,Vertical}Edge`, including the simple
   variants. Loop filter is non-trivial fraction of post-encode time.
6. **Variance / Walsh / Intra16x16.** Lower priority once 2-5 land,
   but each is a few hundred lines of straightforward NEON.
7. **Tighten the Go fallback.** Bounds-check elision (single
   `_ = src[len-1]` at the top of inner loops), `unsafe.Add` for stride
   arithmetic, eliminate `append` on hot paths, `sync.Pool` for
   residual/token/tx buffers if the profile shows allocation pressure
   (current `allocs_per_frame` is 1.1, fine, but internal pools may
   churn beneath that number).

Realistic target: scalar tightening 1.3-1.7x, SIMD on the top 5
functions 5-8x. Combined ~7-12x, putting govpx in the 600-1100 fps
range at 320x240. Reaching parity (1500 fps) likely needs the full
DSP pack, not just the top 5.

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

1. Add `-cpuprofile`/`-memprofile` flags to `cmd/govpx-bench` and
   capture the baseline flame graph. Document the top 10 leaves in
   this file as the profile-driven plan-of-record.
2. SAD16x16 NEON (arm64). Build-tag fallback to existing Go.
3. SixTapPredict16x16/8x8 NEON.
4. IDCT4x4Add + DCOnlyIDCT4x4Add NEON.
5. MBLoopFilter*Edge NEON (all four edges).
6. Variance16x16 + Walsh4x4 NEON.
7. Subpixel-filter parity oracle vs libvpx. Fix any mismatches.
8. Quantizer/zbin parity test against libvpx's tables.
9. Audit `encoder_skip.go` thresholds and `encoder_segmentation.go`
   cyclic-refresh wiring.
10. Add Derf-collection corpus + 640x360 / 1280x720 bench targets.
11. Repeat profile + iterate until govpx >= libvpx encode-fps and PSNR
    within +-0.3 dB on the corpus.

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
