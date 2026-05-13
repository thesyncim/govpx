# govpx tracker

Reference: libvpx v1.16.0. VP9 scope is documented in [UPSTREAM.md](UPSTREAM.md).

## Gates

- `make ci`
- `make verify-decoder-parity`
- `make verify-production`

## VP9 Scope

Authoritative scope lives in [UPSTREAM.md](UPSTREAM.md). Current target: VP9
full profile 0 only; no VP9 profiles 1-3, alpha, high-bit-depth/deep-color, or
non-4:2:0 variants. RTP/WebRTC payload compatibility remains in scope for both
VP8 and VP9.
