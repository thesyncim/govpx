# Upstream

govpx is pinned to libvpx `v1.16.0`.

- Project: `chromium/webm/libvpx`
- Tag object: `04def0a07f8bfa95785e30e6db95036cda17f9b2`
- Commit: `1024874c5919305883187e2953de8fcb4c3d7fa6`
- Release date: 2026-01-21
- Source: `https://chromium.googlesource.com/webm/libvpx/+/refs/tags/v1.16.0`

## Scope

govpx uses libvpx as a pinned oracle, not as a runtime dependency.
Compatibility targets are VP8/VP9 codec bitstreams and RTP/WebRTC payload
compatibility, not the libvpx C ABI.

VP9 support is full Profile 0 only: 8-bit 4:2:0 raw packets and valid
superframes. VP9 profiles 1-3, alpha, high-bit-depth/deep-color, and
non-4:2:0 chroma variants are out of scope. RTP/WebRTC payload compatibility is
in scope for both VP8 and VP9. Valid non-Profile-0 VP9 packets return
`ErrVP9NotImplemented`.

## Gates

- `make verify-decoder-parity`: decoder oracle checks, including VP9
  Profile 0 IVF coverage.
- `make verify-production`: supported encoder and decoder oracle checks.
- `fetch-vp9-test-data`: pinned official VP9 decoder subset.

`GOVPX_VP9_TEST_DATA_STRICT=1` keeps valid VP9 Profile 0 fixtures from being
skipped as unsupported.

## License

libvpx is BSD-3-Clause with an additional patent grant. See
`LICENSE.libvpx` and `PATENTS.libvpx`.
