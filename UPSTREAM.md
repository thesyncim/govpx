# Upstream Baseline

govpx is pinned to libvpx v1.16.0.

- Upstream project: `chromium/webm/libvpx`
- Tag: `v1.16.0`
- Annotated tag object: `04def0a07f8bfa95785e30e6db95036cda17f9b2`
- Commit: `1024874c5919305883187e2953de8fcb4c3d7fa6`
- Release date: 2026-01-21
- Source URL: `https://chromium.googlesource.com/webm/libvpx/+/refs/tags/v1.16.0`

## Scope

govpx ports the libvpx VP8 surface and the VP9 profile 0 codec surface.

VP9 scope is deliberately limited to full profile 0 support: 8-bit 4:2:0
raw VP9 packets, including valid superframes. Profiles 1, 2, and 3,
high bit depth, non-4:2:0 chroma, alpha, WebM/container behavior, AV1, and
the libvpx C API are out of scope.

The parity bar is 100% byte parity with libvpx on supported configurations:
bit-identical decoded pixels for decoders and bit-identical packets or
decoder-visible output for encoder paths where that is the supported proof.

## Status

| Area | Current state |
| --- | --- |
| VP8 decoder | No known behavioral parity gap for the supported surface covered by `make verify-decoder-parity`. |
| VP8 encoder | Functional and oracle-guarded for the documented realtime/WebRTC and analysis paths. Remaining exact heuristic parity work is tracked by root oracle tests and scoreboards, not by this document. |
| VP9 decoder | Profile 0 raw packets and valid superframes are the supported target. The decoder handles the profile 0 paths covered by generated vpxdec oracle cases plus the pinned official VP90 IVF subset. Valid non-profile0 packets return `ErrVP9NotImplemented` by design. |
| VP9 encoder | Scaffolded for profile 0 packet emission and vpxdec structural acceptance. Full VP9 encoder parity is separate from the decoder parity target. |
| SIMD/threading | Deferred until parity gates are stable. |

## Oracle Gates

- `make verify-decoder-parity` runs decoder oracle proofs, including strict
  VP9 profile 0 IVF byte parity.
- `make verify-production` runs the full encoder and decoder oracle gate.
- `fetch-vp9-test-data` fetches the pinned official VP9 decoder subset:
  7 valid VP90 IVF streams, 17 invalid VP90 IVF streams, and 11
  non-profile0 profile-family WebM streams.
- `GOVPX_VP9_TEST_DATA_STRICT=1` is part of the make gates so valid VP90
  streams cannot be skipped as unsupported.

VP9 decoder byte evidence currently comes from:

- `TestVP9DecoderVpxdecOracleMatches*` generated profile 0 reconstruction
  streams.
- `TestVP9DecoderOfficialIVFTestDataMatchesLibvpx` against the pinned
  official VP90 profile 0 IVF subset.
- `TestVP9DecoderOfficialInvalidIVFTestDataRejectedLikeLibvpx`.
- `TestVP9DecoderOfficialProfileWebMTestDataReturnsUnsupported`.

## License

libvpx is distributed under a BSD 3-Clause license with an additional patent
grant. This repository keeps libvpx license and patent notices in
`LICENSE.libvpx` and `PATENTS.libvpx`.
