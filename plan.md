# govpx tracker

Reference: libvpx v1.16.0. VP9 scope is documented in [UPSTREAM.md](UPSTREAM.md).

## Gates

- `make ci`
- `make verify-decoder-parity`
- `make verify-production`

## VP9 Scope

- Full profile 0 only: 8-bit 4:2:0 raw VP9 packets and valid
  superframes.
- Out of scope: VP9 profiles 1, 2, and 3; high bit depth; non-4:2:0; alpha;
  containers; AV1; libvpx C ABI compatibility.
- RTP/WebRTC payload compatibility is in scope for both VP8 and VP9.
- Valid non-profile0 VP9 packets return `ErrVP9NotImplemented`.
