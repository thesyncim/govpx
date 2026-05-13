# Upstream

govpx is pinned to libvpx `v1.16.0`.

- Project: `chromium/webm/libvpx`
- Tag object: `04def0a07f8bfa95785e30e6db95036cda17f9b2`
- Commit: `1024874c5919305883187e2953de8fcb4c3d7fa6`
- Release date: 2026-01-21
- Source: `https://chromium.googlesource.com/webm/libvpx/+/refs/tags/v1.16.0`

## Scope

govpx uses libvpx as an oracle and baseline, not as a runtime dependency.
The public package API is Go-native and does not try to mirror the libvpx C ABI.

VP9 support is full profile 0 only: 8-bit 4:2:0 raw packets and valid
superframes.

Out of scope: VP9 profiles 1, 2, and 3; high bit depth; non-4:2:0 chroma
variants; alpha; containers; AV1; and libvpx C ABI compatibility.

RTP/WebRTC payload compatibility is in scope for both VP8 and VP9.

Valid non-profile0 VP9 packets return `ErrVP9NotImplemented`.

## Gates

- `make verify-decoder-parity`: decoder oracle checks, including strict VP9
  profile 0 IVF parity.
- `make verify-production`: full supported oracle gate.
- `fetch-vp9-test-data`: pinned official VP9 decoder subset.

`GOVPX_VP9_TEST_DATA_STRICT=1` keeps valid VP90 profile 0 fixtures from being
skipped as unsupported.

## License

libvpx is BSD-3-Clause with an additional patent grant. See
`LICENSE.libvpx` and `PATENTS.libvpx`.
