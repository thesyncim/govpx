# Upstream Baseline

govpx is pinned to libvpx v1.16.0.

- Upstream project: `chromium/webm/libvpx`
- Tag: `v1.16.0`
- Annotated tag object: `04def0a07f8bfa95785e30e6db95036cda17f9b2`
- Commit: `1024874c5919305883187e2953de8fcb4c3d7fa6`
- Release date: 2026-01-21
- Source URL: `https://chromium.googlesource.com/webm/libvpx/+/refs/tags/v1.16.0`

## Included Scope

The intended VP8-only porting scope is:

- `vp8/common/`
- `vp8/decoder/`
- `vp8/encoder/`
- `vpx/`
- `vpx_dsp/`
- `vpx_mem/`
- `vpx_ports/`
- `vpx_scale/`
- `vpx_util/`

The current repository contains public API scaffolding, upstream metadata,
internal parser/state ports, scalar VP8 decoder paths, encoder packet writers,
and initial encoder analysis paths.

## Excluded Scope

- `vp9/` is explicitly excluded.
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
| Public API | scaffolded |
| Upstream manifest | scaffolded |
| Oracle harness | `make verify-production` builds pinned libvpx helpers, fetches required corpora, and runs the full oracle gate; optional vpxdec smoke and libvpx checksum oracle scaffolded, including postprocess and error-concealment decode modes for targeted parity tests |
| Benchmark harness | synthetic govpx encoder JSON benchmark scaffolded with optional external libvpx vpxenc comparison, including reference output size, latency, bitrate, and decodable PSNR/SSIM metrics; decoder CLI and `benchmarks/` package cover govpx Decode/DecodeInto on the libvpx-authored smoke stream with optional libvpx checksum-oracle reference timing |
| IVF/test vectors | IVF parser and oracle checksum parser scaffolded; normal-test VP8/IVF smoke vectors, including libvpx-authored reference, token-partition, profile, loop-filter sharpness, and error-resilient streams plus NEWMV, subpixel NEWMV, luma/chroma intra-mode keyframe, and intra-macroblock interframe coverage, checksummed against libvpx v1.16.0; opt-in encoder oracle coverage includes B_PRED keyframes, intra B_PRED interframes, libvpx decode-acceptance/checksum agreement for both govpx-encoded and libvpx-encoded corpus clips, token-partition and segmentation feature assertions, aggregate and per-frame PSNR/SSIM floors, bitrate windows, and libvpx-vpxenc quality-gap guards; opt-in generated libvpx corpus covers profile 0/1/2/3, one/two/four/eight token partitions, active eight-partition row cycling, error-resilient cyclic-refresh segmentation, sharpness, and static-threshold streams with feature assertions; opt-in external VP8 IVF decoder conformance via `GOVPX_TEST_DATA_PATH`, invalid-stream rejection parity via `GOVPX_INVALID_TEST_DATA_PATH`, and external Y4M/YUV encoder source-corpus validation via `GOVPX_ENCODER_TEST_DATA_PATH`, all with required/minimum-count controls for CI corpus runs |
| Frame memory | macroblock-padded, border-addressable frame buffers scaffolded |
| Bool decoder/writer | bool decoder scaffolded |
| Header parsing | frame tag and uncompressed keyframe header scaffolded |
| Decoder state and reconstruction | state headers, segment dequant setup, macroblock residual transform, residual pixel add, intra predictor reference setup with libvpx-style row-edge extension, intra macroblock grid reconstruction, whole-block intra prediction/reconstruction, B_PRED 4x4 prediction/reconstruction, keyframe/inter reference refresh, version-specific inter prediction flags, extended-border whole-macroblock and SplitMV inter prediction/reconstruction, default deblock/demacroblock postprocess output, MFQE postprocess, optional ADDNOISE luma postprocess, and narrow frame output scaffolded |
| Token and mode parsing | tree reader, partition layout with two/four/eight token-reader row cycling, coefficient/mode probability state, macroblock coefficient token traversal/grid, keyframe/inter macroblock mode grids, near-MV selection, split-MV parsing, and motion-vector decoding scaffolded |
| Scalar DSP | clip/copy/reconstruction, SAD 16x16/16x8/8x16/8x8/4x4 with bounded 16x16 early-out, variance/SSE 16x16/16x8/8x16/8x8/8x4/4x8/4x4, bilinear subpel variance, bilinear/six-tap subpixel prediction, dequant, IDCT4x4, IWHT4x4, and intra predictors scaffolded |
| Loop filter | scalar edge primitives, limit table setup, and decoder frame traversal scaffolded |
| Encoder rate-control API | target bits, buffer model, bounded pre-commit quantizer feedback, post-frame quantizer feedback, bounded CBR frame dropping, and deterministic clip-level bitrate tracking tests scaffolded |
| VP8 constants and static tables | scaffolded; quant/dequant tables scaffolded |
| Encoder bitstream writer | bool writer, packet, tree-token, keyframe state, and interframe intra/inter mode primitives scaffolded |
| Encoder frame algorithms | neutral/coefficient keyframe packets, keyframe mode, zero/nonzero coefficient token grid writers, whole-block luma/chroma intra mode selection with libvpx-style RD rate costs, keyframe B_PRED 4x4 luma selection with context-aware mode costs, quantized residual token-rate RD scoring, keyframe/inter residual analysis with segment-aware quant/dequant reconstruction feedback, StaticThreshold encode-breakout with cyclic-refresh-style segmentation data, LAST/ZEROMV residual interframes with intra macroblock selection, last/golden/altref reference selection, LAST-only default inter refresh with golden/altref preservation, invisible-frame handling, libvpx-inspired NEWMV interframes with near-MV reuse, exhaustive full-pixel search plus libvpx iterative half/quarter-pel search with bilinear subpel variance and RD reconstruction candidate selection, opt-in reconstructed-frame loop filtering, forward transforms, segment-aware coefficient-builder quant setup, and fast block quantization scaffolded |
| SIMD/assembly | not started |

## Known Deviations

- `Decode` and `DecodeInto` cover supported VP8 versions, token partitions,
  keyframes, interframes, SplitMV, narrow error-resilient inter-frame
  concealment, default deblock/demacroblock post-processing, libvpx-style MFQE,
  and optional luma ADDNOISE using deterministic Go-side noise state. Reserved
  VP8 versions and caller-configured size/resolution limits return
  `ErrUnsupportedFeature`.
- `EncodeInto` can emit source-dependent whole-block luma/chroma intra keyframes,
  LAST/ZEROMV residual interframes, whole-block intra macroblocks inside interframes, and
  libvpx-inspired NEWMV interframes with last/golden/altref reference selection,
  near-MV reuse, libvpx-style full/subpel motion search, token partitions, LAST-only default
  inter refresh with golden/altref preservation, invisible-frame handling, plus opt-in reconstructed-frame
  loop filtering, but full libvpx cyclic/background segment selection and full
  libvpx rate-control heuristic parity are not complete yet. Encoder corpus
  validation currently acts as a regression guard; its libvpx-vpxenc aggregate
  and per-frame quality-gap gates, plus opt-in external Y4M/YUV source-corpus
  validation, document that quality and rate parity are still open correctness
  goals.
- The package exposes a small Go API, not the libvpx C API.
