# Upstream Baseline

libgopx is pinned to libvpx v1.16.0.

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
internal parser/state scaffolding, and initial scalar VP8 algorithm ports.

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
| Oracle harness | optional vpxdec smoke and libvpx checksum oracle scaffolded |
| IVF/test vectors | IVF parser and oracle checksum parser scaffolded; normal-test VP8/IVF smoke vectors, including a libvpx-authored reference stream plus NEWMV, luma/chroma intra-mode keyframe, and intra-macroblock interframe coverage, checksummed against libvpx v1.16.0 |
| Frame memory | macroblock-padded, border-addressable frame buffers scaffolded |
| Bool decoder/writer | bool decoder scaffolded |
| Header parsing | frame tag and uncompressed keyframe header scaffolded |
| Decoder state and reconstruction | state headers, segment dequant setup, macroblock residual transform, residual pixel add, intra predictor reference setup with libvpx-style row-edge extension, intra macroblock grid reconstruction, whole-block intra prediction/reconstruction, B_PRED 4x4 prediction/reconstruction, keyframe/inter reference refresh, version-specific inter prediction flags, extended-border whole-macroblock and SplitMV inter prediction/reconstruction, and narrow frame output scaffolded |
| Token and mode parsing | tree reader, partition layout, coefficient/mode probability state, macroblock coefficient token traversal/grid, keyframe/inter macroblock mode grids, near-MV selection, split-MV parsing, and motion-vector decoding scaffolded |
| Scalar DSP | clip/copy/reconstruction, SAD 16x16/16x8/8x16/8x8/4x4, variance/SSE 16x16/16x8/8x16/8x8/4x4, bilinear/six-tap subpixel, dequant, IDCT4x4, IWHT4x4, and intra predictors scaffolded |
| Loop filter | scalar edge primitives, limit table setup, and decoder frame traversal scaffolded |
| Encoder rate-control API | scaffolded |
| VP8 constants and static tables | scaffolded; quant/dequant tables scaffolded |
| Encoder bitstream writer | bool writer, packet, tree-token, keyframe state, and interframe intra/inter mode primitives scaffolded |
| Encoder frame algorithms | neutral/coefficient keyframe packets, keyframe mode, zero/nonzero coefficient token grid writers, whole-block luma/chroma intra mode selection, keyframe residual analysis with reconstruction feedback, LAST/ZEROMV residual interframes with intra macroblock selection, last/golden/altref reference selection and refresh control, invisible-frame handling, libvpx-inspired full-pixel NEWMV interframes with near-MV reuse, hex-ring motion candidates, bounded diamond refinement, opt-in reconstructed-frame loop filtering, forward transforms, and fast block quantization scaffolded |
| SIMD/assembly | not started |

## Known Deviations

- `Decode` and `DecodeInto` can expose narrow supported-version keyframe and
  inter-frame scaffolds, but error concealment, post-processing, and many VP8
  features still return `ErrUnsupportedFeature`.
- `EncodeInto` can emit source-dependent whole-block luma/chroma intra keyframes,
  LAST/ZEROMV residual interframes, whole-block intra macroblocks inside interframes, and
  libvpx-inspired full-pixel NEWMV interframes with last/golden/altref reference
  selection, near-MV reuse, and reference refresh control, invisible-frame handling,
  plus opt-in reconstructed-frame loop
  filtering, but full prediction mode analysis, subpixel motion search, and
  rate-control feedback are not complete yet.
- The package exposes a small Go API, not the libvpx C API.
