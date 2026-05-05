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

The current repository contains only API scaffolding and metadata. No libvpx C
algorithms have been ported yet.

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
| Oracle harness | not started |
| IVF/test vectors | not started |
| Frame memory | scaffolded |
| Bool decoder/writer | bool decoder scaffolded |
| Header parsing | frame tag and uncompressed keyframe header scaffolded |
| Decoder state and reconstruction | state headers scaffolded; reconstruction not started |
| Token and mode parsing | tree reader scaffolded |
| Scalar DSP | clip/copy/reconstruction primitives scaffolded |
| Loop filter | not started |
| Encoder rate-control API | scaffolded |
| VP8 constants and static tables | scaffolded |
| Encoder bitstream writer | not started |
| Encoder frame algorithms | not started |
| SIMD/assembly | not started |

## Known Deviations

- `Decode`, `DecodeInto`, and `EncodeInto` validate basic inputs but return
  `ErrUnsupportedFeature` because VP8 algorithms are not ported yet.
- The package exposes a small Go API, not the libvpx C API.
