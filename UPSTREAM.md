# Upstream

govpx is pinned to libvpx `v1.16.0`.

- Project: `chromium/webm/libvpx`
- Tag object: `04def0a07f8bfa95785e30e6db95036cda17f9b2`
- Commit: `1024874c5919305883187e2953de8fcb4c3d7fa6`
- Release date: 2026-01-21
- Source: `https://chromium.googlesource.com/webm/libvpx/+/refs/tags/v1.16.0`

## Compatibility Scope

govpx uses libvpx as a pinned oracle, not as a runtime dependency.
Compatibility targets are VP8 codec bitstreams, VP9 Profile 0 codec
bitstreams, and RTP/WebRTC payload bodies for both codecs. The libvpx C ABI is
not a compatibility target.

VP9 scope is Profile 0 only: 8-bit 4:2:0 raw packets and valid Profile 0
superframes. Profiles 1-3, alpha, high bit depth, and non-4:2:0 chroma are out
of scope. Valid non-Profile-0 VP9 packets return `ErrVP9NotImplemented`.

## Optimization Scope

Architecture-specific kernels exist for selected hot paths, and the `purego`
build tag selects scalar fallbacks. New optimization work must preserve scalar
correctness, oracle coverage, allocation behavior, and public API
compatibility.

## Go SIMD source copies

Selected kernels in `internal/pixel` copy or adapt Go SIMD dataflow from
`thesyncim/goav1` at `ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4`; their source
comments identify the extracted helpers. The sixteen-column residual layout
also adapts `residualBlockSIMD` in `internal/av1/encoder/rdstats_gosimd.go` at
`0f40b2bd848d8c5c7bdbaa016f2ced3085f7fc7a`.

These are source copies with govpx-specific bounds, dispatch and fallback
contracts, without a Go module dependency. The associated license, notices
and patent text are preserved in `third_party/goav1/`.

## Gates

- `make verify-decoder-parity`: decoder oracle checks, including VP9
  Profile 0 IVF coverage.
- `make verify-production`: supported encoder and decoder oracle checks.
- `fetch-vp9-test-data`: pinned official VP9 Profile 0 decoder subset.

`GOVPX_VP9_TEST_DATA_STRICT=1` keeps valid VP9 Profile 0 fixtures from being
skipped as unsupported.

## License

libvpx is BSD-3-Clause with an additional patent grant. See
`LICENSE.libvpx` and `PATENTS.libvpx`.
