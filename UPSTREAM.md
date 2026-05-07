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
| Public API | scaffolded; decoder options expose libvpx-style error concealment and granular postprocess controls; runtime encoder controls cover bitrate, rate-control config, CQLevel, max-intra bitrate, GF-CBR boost, token partitions, sharpness, screen-content mode, static threshold, realtime targets, temporal scalability, temporal layer ID, deadline/CPU-used, keyframe interval, force-keyframe, force-Golden/AltRef frame flags, and reset; encoder results report libvpx-style temporal layer IDs, sync, TL0PICIDX, droppable metadata, incremental/cumulative layer bitrate targets, per-layer buffer state, and per-layer input/encoded/cumulative bit counters; zero-allocation hot-path coverage exists for runtime controls |
| Upstream manifest | scaffolded |
| Oracle harness | `make verify-production` builds pinned libvpx helpers, including vpxenc, vpxdec, and the VP8 temporal SVC example encoder, fetches required corpora, and runs the full oracle gate; optional vpxdec smoke and libvpx checksum oracle scaffolded, including postprocess and error-concealment decode modes for targeted parity tests |
| Benchmark harness | synthetic govpx encoder JSON benchmark scaffolded with optional external libvpx vpxenc comparison, including reference output size, latency, bitrate, and decodable PSNR/SSIM metrics; decoder CLI and `benchmarks/` package cover govpx Decode/DecodeInto on the libvpx-authored smoke stream with optional libvpx checksum-oracle reference timing |
| Hot-path allocation guards | Decode, DecodeInto, NextFrame, EncodeInto, temporal EncodeInto, runtime controls, ForceKeyFrame, Reset, Close, and core parser/DSP helpers have zero-allocation regression tests after initialization |
| IVF/test vectors | IVF parser and oracle checksum parser scaffolded; normal-test VP8/IVF smoke vectors, including libvpx-authored reference, token-partition, profile, loop-filter sharpness, and error-resilient streams plus NEWMV, subpixel NEWMV, luma/chroma intra-mode keyframe, and intra-macroblock interframe coverage, checksummed against libvpx v1.16.0; opt-in encoder oracle coverage includes B_PRED keyframes, intra B_PRED interframes, temporal SVC base-layer and full-sequence streams, external libvpx temporal SVC example layer streams plus layer accounting checks, CQLevel streams, libvpx decode-acceptance/checksum agreement for both govpx-encoded and libvpx-encoded corpus clips, token-partition and segmentation feature assertions, aggregate and per-frame PSNR/SSIM floors, bitrate windows, and libvpx-vpxenc quality-gap guards; normal temporal SVC coverage checks packet refresh/entropy flags across all libvpx example patterns; opt-in generated libvpx corpus covers profile 0/1/2/3, one/two/four/eight token partitions, active eight-partition row cycling, error-resilient cyclic-refresh segmentation, sharpness, and static-threshold streams with feature assertions; production external VP8 IVF decoder conformance requires the full valid VP8 subset currently available through the pinned libvpx v1.16.0 test-data list (58 VP80 IVF vectors plus 2 invalid rejection vectors; four non-invalid `vp80-03-segmentation-0*.ivf` fixtures are I420 IVF resources), with invalid-stream rejection parity via `GOVPX_INVALID_TEST_DATA_PATH` and two required external Y4M/YUV encoder source-corpus clips via `GOVPX_ENCODER_TEST_DATA_PATH`, all with required/minimum-count controls for CI corpus runs |
| Frame memory | macroblock-padded, border-addressable frame buffers scaffolded |
| Bool decoder/writer | bool decoder scaffolded |
| Header parsing | frame tag and uncompressed keyframe header scaffolded |
| Decoder state and reconstruction | state headers, segment dequant setup, macroblock residual transform, residual pixel add, intra predictor reference setup with libvpx-style row-edge extension, intra macroblock grid reconstruction, whole-block intra prediction/reconstruction, B_PRED 4x4 prediction/reconstruction, keyframe/inter reference refresh, keyframe resolution-change buffer reinitialization, version-specific inter prediction flags, extended-border whole-macroblock and SplitMV inter prediction/reconstruction, default and granular deblock/demacroblock postprocess output, MFQE postprocess, optional ADDNOISE luma postprocess, and narrow frame output scaffolded |
| Token and mode parsing | tree reader, partition layout with two/four/eight token-reader row cycling, coefficient/mode probability state, macroblock coefficient token traversal/grid, keyframe/inter macroblock mode grids, near-MV selection, split-MV parsing, and motion-vector decoding scaffolded |
| Scalar DSP | clip/copy/reconstruction, SAD 16x16/16x8/8x16/8x8/4x4 with bounded 16x16 early-out, variance/SSE 16x16/16x8/8x16/8x8/8x4/4x8/4x4, bilinear subpel variance, bilinear/six-tap subpixel prediction, dequant, IDCT4x4, IWHT4x4, and intra predictors scaffolded |
| Loop filter | scalar edge primitives, limit table setup, and decoder frame traversal scaffolded |
| Encoder rate-control API | target bits, buffer model, libvpx one-pass keyframe target sizing/boosts, libvpx-style droppable encoded-frame metadata, max-intra-bitrate keyframe target cap, CBR golden-frame refresh/boost with libvpx cyclic-refresh cadence and LAST/ZEROMV refresh gating, libvpx cyclic-refresh MB cadence by temporal-layer count including screen-content mode cadence, low-buffer debt clamp, and screen-content inter-Q drop limiting, libvpx temporal layer ID override, temporal input/encoded/cumulative bit counters, and cumulative layer-buffer updates reported from encode results, libvpx bits-per-macroblock quantizer regulation plus buffer-fullness target scaling, initialized/reset rolling bit monitors and correction factors, non-show-frame overhead accounting including temporal-layer buffers, negative CBR buffer-debt/drop threshold handling, temporal-layer frame-size bounds, runtime bitrate buffer preservation, and bounded correction-factor feedback for one-pass CBR frames, libvpx frame-size bounds for bounded pre-commit quantizer feedback, post-frame quantizer feedback, bounded CBR frame dropping, CQLevel constrained-quality quantizer floor/control, and deterministic clip-level bitrate tracking tests scaffolded |
| VP8 constants and static tables | scaffolded; quant/dequant tables scaffolded |
| Encoder bitstream writer | bool writer, packet, tree-token, keyframe state, token-partition payloads, and interframe intra/inter mode primitives scaffolded |
| Encoder frame algorithms | neutral/coefficient keyframe packets, keyframe mode, zero/nonzero coefficient token grid writers, whole-block luma/chroma intra mode selection with libvpx-style RD rate costs, keyframe B_PRED 4x4 luma selection with context-aware mode costs, quantized residual token-rate RD scoring, keyframe/inter residual analysis with skip/reference/inter-mode/MV/token bit costs and segment-aware quant/dequant reconstruction feedback, StaticThreshold encode-breakout with rotating cyclic-refresh-style segmentation data using libvpx default CBR/error-resilient enablement, screen-content cadence/disable rules, Q/2-Q ALT_Q boost, refresh-map cooldown, count-derived segment tree probabilities, and base temporal-layer gating, LAST/ZEROMV residual interframes with intra macroblock selection, last/golden/altref reference selection, LAST-only default inter refresh with golden/altref preservation, force-Golden/AltRef refresh flags, invisible-frame handling, explicit ZEROMV/NEARESTMV/NEARMV/NEWMV inter RD candidates, libvpx-inspired NEWMV interframes with near-MV reuse, exhaustive full-pixel search plus libvpx iterative half/quarter-pel search with bilinear subpel variance and RD reconstruction candidate selection, SPLITMV candidate selection/bitstream emission across VP8 split partition shapes, default reconstructed-frame loop filtering with libvpx-style base-q initial level and keyframe sharpness reset, forward transforms, segment-aware coefficient-builder quant setup, and fast block quantization scaffolded |
| SIMD/assembly | not started |

## Known Deviations

- `Decode` and `DecodeInto` cover VP8 versions 0-7, token partitions,
  keyframes, interframes, SplitMV, narrow error-resilient inter-frame
  concealment, default and granular deblock/demacroblock post-processing,
  libvpx-style MFQE, and optional luma ADDNOISE using deterministic Go-side
  noise state. Full active error-concealment missing-MV estimation for corrupt
  interframes is still incomplete. Caller-configured size/resolution limits
  return `ErrUnsupportedFeature`.
- `EncodeInto` can emit source-dependent whole-block luma/chroma intra keyframes,
  LAST/ZEROMV residual interframes, whole-block intra macroblocks inside interframes,
  libvpx-inspired NEWMV interframes with last/golden/altref reference selection,
  explicit ZEROMV/NEARESTMV/NEARMV/NEWMV candidate scoring with VP8
  skip/reference/inter-mode/MV/token rate costs, near-MV reuse, libvpx-style
  full/subpel motion search, SPLITMV interframes across
  VP8 split partition shapes, token partitions, LAST-only default
  inter refresh with golden/altref preservation, invisible-frame handling,
  default reconstructed-frame loop filtering, and initial CQLevel constrained-quality mode. One-pass CBR now starts frames from libvpx's bits-per-macroblock quantizer regulator, updates key/inter/golden-frame correction factors from encoded frame size, and tracks the current external 90p vpxenc validation clip closely, but full libvpx
  SPLITMV RD/mode-cost parity, exact cyclic/background segment selection, exact constrained-quality/golden-frame bitrate heuristics, and full
  libvpx rate-control heuristic parity are not complete yet. Encoder corpus
  validation currently acts as a regression guard; its libvpx-vpxenc aggregate
  and per-frame quality-gap gates, plus opt-in external Y4M/YUV source-corpus
  validation, document that quality and rate parity are still open correctness
  goals.
- The package exposes a small Go API, not the libvpx C API.
