# govpx tracker

Reference: libvpx v1.16.0. VP9 scope is documented in [UPSTREAM.md](UPSTREAM.md).

## Gates

- `make ci`
- `make verify-decoder-parity`
- `make verify-production`

## VP9 Scope

- Full profile 0 support only: 8-bit 4:2:0 raw packets and valid superframes.
- Profiles 1, 2, and 3 are out of scope.
- Do not claim support without an oracle case or conformance fixture.
