# Upstream Baseline

govpx is pinned to libvpx v1.16.0.

- Upstream project: `chromium/webm/libvpx`
- Tag: `v1.16.0`
- Annotated tag object: `04def0a07f8bfa95785e30e6db95036cda17f9b2`
- Commit: `1024874c5919305883187e2953de8fcb4c3d7fa6`
- Release date: 2026-01-21
- Source URL: `https://chromium.googlesource.com/webm/libvpx/+/refs/tags/v1.16.0`

## Included Scope

The intended porting scope is both VP8 and VP9 from libvpx:

- `vp8/common/`
- `vp8/decoder/`
- `vp8/encoder/`
- `vp9/common/`
- `vp9/decoder/`
- `vp9/encoder/`
- `vpx/`
- `vpx_dsp/`
- `vpx_mem/`
- `vpx_ports/`
- `vpx_scale/`
- `vpx_util/`

The current repository contains a complete VP8 port and an in-progress VP9
port; VP9 status is tracked separately below.

The parity bar for both codecs is 100% byte parity with libvpx on the
supported configurations: bit-identical encoded packets out of the
encoder, bit-identical decoded pixels out of the decoder, validated by
the oracle harness against the pinned libvpx v1.16.0 build.

## Excluded Scope

- `av1/` is explicitly excluded.
- `examples/` and `tools/` are excluded except as optional references.
- WebM mux/demux code is excluded from the codec package.
- cgo is excluded from the Go package.

## License Obligations

libvpx is distributed under a BSD 3-Clause license with an additional patent
grant. This repository keeps libvpx license and patent notices in
`LICENSE.libvpx` and `PATENTS.libvpx`.

## VP8 Subsystem Status

| Subsystem | Status |
| --- | --- |
| Public API | scaffolded. Decoder options expose libvpx-style error concealment and granular postprocess. Runtime encoder controls cover bitrate, rate-control config, CQLevel, GF/AltRef flags, token partitions, sharpness, screen-content, PSNR/SSIM tuning, VP8 RTC external-rate-control mode, static threshold, noise sensitivity, ARNR, two-pass stats, realtime targets, temporal scalability/layer ID, deadline/CpuUsed, keyframe interval, adaptive keyframes, force-keyframe, flush, reset. Encoder results report temporal IDs, sync, TL0PICIDX, droppable metadata, lookahead depth, preprocessing flags, scene-cut keyframe promotion, first-pass stats, two-pass targets, per-layer rate/buffer/bit counters. Zero-allocation regression coverage on hot paths after initialization. |
| Upstream manifest | scaffolded |
| Oracle harness | `make verify-production` builds pinned vpxenc/vpxdec and the VP8 temporal SVC example encoder, fetches corpora, runs the full oracle gate. `make verify-decoder-parity` runs the decoder-only proof. Optional vpxdec smoke and libvpx checksum oracle support postprocess, ADDNOISE, and error-concealment decode modes. |
| Benchmark harness | synthetic govpx encoder JSON bench with optional libvpx-vpxenc reference (output size, latency, bitrate, PSNR/SSIM). Decoder CLI and `benchmarks/` package cover Decode/DecodeInto on the libvpx-authored smoke stream with optional checksum-oracle reference timing. |
| Hot-path allocation guards | Decode, DecodeInto, NextFrame, EncodeInto, temporal EncodeInto, runtime controls, ForceKeyFrame, Reset, Close, and core parser/DSP helpers — zero-allocation after init. |
| IVF/test vectors | IVF parser and oracle checksum parser scaffolded. Decoder parity gate covers the full pinned libvpx v1.16.0 valid VP8 subset (58 VP80 IVF vectors + 2 invalid rejection vectors; four `vp80-03-segmentation-0*.ivf` fixtures are I420), checksumming both `Decode` and `DecodeInto`. Generated libvpx feature streams cover profile 0–3, 1/2/4/8 token partitions, error-resilient cyclic-refresh segmentation, sharpness, static-threshold, postprocess, error-concealment, and keyframe resolution change. Opt-in encoder oracle covers B_PRED keyframes, intra B_PRED interframes, temporal SVC, CQLevel, govpx- and libvpx-encoded corpus decode acceptance, token-partition/segmentation assertions, aggregate and per-frame PSNR/SSIM floors, bitrate windows, and libvpx-vpxenc quality-gap guards. Production external Y4M/YUV source corpus is wired through `GOVPX_ENCODER_TEST_DATA_PATH`. |
| Frame memory | macroblock-padded, border-addressable frame buffers scaffolded |
| Bool decoder/writer | bool decoder scaffolded |
| Header parsing | frame tag and uncompressed keyframe header scaffolded |
| Decoder state and reconstruction | state headers, segment dequant setup, residual transform/add, intra predictor reference setup with row-edge extension, whole-block + B_PRED 4x4 prediction/reconstruction, keyframe/inter reference refresh, keyframe resolution-change reinitialization, version-specific inter prediction flags, extended-border whole-MB and SplitMV inter prediction/reconstruction, active error-concealment prediction for corrupt residuals, default and granular deblock/demacroblock postprocess, MFQE, optional luma ADDNOISE, narrow-frame output. |
| Token and mode parsing | tree reader, partition layout with 2/4/8 token-reader row cycling, coef/mode probability state, MB coefficient/mode grids including error-concealment corruption tracking, near-MV selection, split-MV parsing, MV decoding, active error-concealment missing-MV estimation. |
| Scalar DSP | clip/copy/reconstruction, SAD (16x16/16x8/8x16/8x8/4x4) with bounded 16x16 early-out, variance/SSE (all common sizes), bilinear subpel variance, bilinear/six-tap subpel prediction, dequant, IDCT4x4, IWHT4x4, intra predictors. |
| Loop filter | scalar edge primitives, limit table setup, decoder frame traversal. |
| Encoder rate-control API | one-pass keyframe target sizing/boosts, scene-cut keyframe promotion (one-pass + two-pass), first-pass stats collection, two-pass VBR targeting, droppable encoded-frame metadata, max-intra-bitrate KF cap, CBR GF refresh/boost with cyclic-refresh cadence (incl. screen-content cadence), low-buffer debt clamp, screen-content inter-Q drop limiting, temporal layer ID override, per-layer bit counters and cumulative buffer updates, bits-per-MB quantizer regulation with buffer-fullness scaling, rolling bit monitors and correction factors, non-show-frame overhead accounting, negative CBR buffer-debt drop, runtime bitrate buffer preservation, bounded correction-factor feedback (CBR/CQ/VBR/Q), boosted-golden branching, frame-size bounds for pre/post-commit Q feedback, bounded CBR drop, CQLevel floor. |
| VP8 constants and static tables | scaffolded; quant/dequant tables scaffolded |
| Encoder bitstream writer | bool writer, packet, tree-token, keyframe state, token-partition payloads, interframe intra/inter mode primitives. |
| Encoder frame algorithms | keyframe packets, keyframe mode, zero/nonzero coefficient token grid writers, lookahead, ARNR-style temporal filtering, spatial/temporal denoising. Whole-block luma/chroma intra mode selection with RD rate costs; keyframe B_PRED 4x4 luma with context-aware mode costs; residual analysis with skip/ref/inter-mode/MV/token bit costs and segment-aware quant/dequant reconstruction. StaticThreshold encode-breakout, rotating cyclic-refresh segmentation (default CBR/error-resilient enablement, screen-content cadence/disable, Q/2-Q ALT_Q boost, refresh-map cooldown, count-derived segment tree probs, base temporal-layer gating, consecutive-LAST/ZEROMV counters, skin-block classification). LAST/ZEROMV residual interframes; LAST/GOLDEN/ALTREF selection with LAST-only default refresh and force-GF/ARF flags; invisible-frame handling. Explicit ZEROMV/NEARESTMV/NEARMV/NEWMV inter RD candidates with near-MV reuse, full/half/quarter-pel motion search with bilinear subpel variance and RD reconstruction; SPLITMV candidate selection/bitstream emission across VP8 split shapes. Default reconstructed-frame loop filtering with base-Q initial level and keyframe sharpness reset; forward transforms; segment-aware coefficient-builder quant setup; fast block quantization. |
| SIMD/assembly | not started |

## Known Deviations

- `Decode` and `DecodeInto` have no known behavioral parity gap for the
  supported VP8 surface covered by `make verify-decoder-parity`: VP8 versions
  0-7, token partitions, keyframes, interframes, SplitMV, error-resilient and
  active error concealment, keyframe resolution changes, default and granular
  deblock/demacroblock post-processing, libvpx-style MFQE, and optional luma
  ADDNOISE using libvpx-compatible libc rand streams for the supported oracle
  platforms. Caller-configured size/resolution rejections return
  `ErrFrameRejected`; `ErrUnsupportedFeature` is retained only for API
  compatibility. This is an oracle-backed empirical parity claim, not a formal
  proof over every possible malformed byte stream.
- `EncodeInto` emits keyframes, LAST/ZEROMV interframes, intra macroblocks
  inside interframes, NEWMV interframes (LAST/GOLDEN/ALTREF selection with
  ZEROMV/NEARESTMV/NEARMV/NEWMV RD candidate scoring, near-MV reuse,
  libvpx-style full/subpel motion search), SPLITMV across VP8 split shapes,
  token partitions, LAST-only default refresh with GF/ARF preservation,
  invisible-frame handling, default reconstructed-frame loop filtering, and
  opt-in lookahead / ARNR / spatial-denoise preprocessing. Two-pass VBR with
  libvpx scene-cut placement, one-pass scene-cut keyframe promotion including
  post-inter auto-key recode, and CQLevel constrained-quality are wired.
  One-pass CBR/CQ/VBR start frames from libvpx's bits-per-MB Q regulator and
  update KF/inter/GF correction factors from encoded frame size.
- Open encoder gaps: exact cyclic/background segment selection, exact
  constrained-quality / GF bitrate heuristics, full libvpx rate-control
  heuristic parity, and the remaining oracle-scoreboard deltas tracked by the
  root oracle tests and `testdata/*_baseline.json`. Encoder corpus validation
  acts as a regression guard, not a parity proof.
- The package exposes a small Go API, not the libvpx C API.

## VP9 Subsystem Status

The VP9 port is in progress on the `vp9-port` branch. Tracking layout:

| Subsystem | Status |
| --- | --- |
| Public API (VP9Decoder / VP9Encoder) | not started |
| Module scaffolding | in progress (`internal/vp9/{bitstream,common,decoder,dsp,encoder,mem,tables}`) |
| Bitstream (range coder reader + writer) | not started |
| Common tables (entropy / scan / quant / MV / pred) | not started |
| DSP (idct, intra pred, inter convolve, loop filter, SAD, variance) | not started |
| Decoder (uncompressed + compressed header, tile, detok, recon) | not started |
| Encoder | not started |
| Oracle harness (vp90 IVF corpus + vpxenc/vpxdec gates) | not started |
| Byte parity gate | not started |
| Hot-path allocation guards | not started |
| SIMD/assembly | not started |

The VP9 port follows the same philosophy as the VP8 port:

- 100% byte parity with libvpx on supported configurations is the gate.
- Hot paths must be zero-allocation after init; allocation tests guard
  steady-state `Decode`/`Encode` paths.
- Inner kernels must remain leaf-callable and inlinable; the public API
  takes caller-owned buffers and aliases into them where libvpx aliases
  into caller-provided storage.
- Stack-only data on hot paths. Heap allocations live in setup / Reset
  only, not in steady-state per-frame work.
- Doc comments name libvpx call sites for any non-obvious code so the
  upstream behavior is traceable.
