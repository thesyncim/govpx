# govpx parity tracker

Reference: libvpx v1.16.0. Scope details live in [UPSTREAM.md](UPSTREAM.md).

## Gates

- Full production gate: `make verify-production`
- Decoder-only gate: `make verify-decoder-parity`
- Standard CI gate: `make ci`
- Safe points should end with a clean `git status --short`.

## Current Status

- VP8 decoder: no known supported-surface parity gap under
  `make verify-decoder-parity`.
- VP9 decoder: current target is full profile 0 support only. Strict VP90 IVF
  byte parity and generated vpxdec oracle cases are wired into
  `make verify-decoder-parity` and `make verify-production`.
- VP8 encoder: functional and oracle-guarded across the covered
  realtime/WebRTC and analysis paths, with remaining exact heuristic parity
  tracked by scoreboards.
- VP9 encoder: scaffolded for profile 0 structural acceptance; full encoder
  parity is not part of the VP9 decoder parity target.
- Performance, SIMD, and threading work are deferred until correctness gates
  are stable.

## Active Work Order

1. Keep decoder oracle gates green.
2. Close VP8 encoder parity scoreboards in priority order.
3. Expand oracle coverage only when it protects a supported surface.
4. Start dispatch/SIMD/threading/performance work after parity-sensitive
   behavior is stable.

## Notes

- Do not treat non-profile0 VP9 reconstruction as future work. It is outside
  scope.
- Do not claim support without an oracle case or a conformance fixture.
- Keep historical investigations in commits, issues, or scoreboards, not in
  this tracker.
