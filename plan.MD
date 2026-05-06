# govpx plan

This file is the small living plan for the current repo. The historical giant
spec has been retired; frozen upstream and licensing detail belong in
`UPSTREAM.md`, and performance notes belong in `docs/performance.md`.

## Goal

Build a pure-Go, VP8-only codec library based on libvpx v1.16.0 ideas with a
small Go API, no cgo dependency, and normal CI that stays fast.

## Current state

- The repo builds with Go 1.26.
- `make ci` is the fast gate: `gofmt` check plus `go test ./... -count=1`.
- GitHub Actions runs that same fast gate.
- The root package has decoder, encoder, stream-info, image, temporal, and
  rate-control APIs.
- Scalar VP8 decoder/encoder internals live under `internal/vp8`.
- Normal tests include checked-in smoke vectors and skip libvpx unless
  `GOVPX_WITH_ORACLE=1`.
- `make verify-production` remains the slower opt-in libvpx parity gate.

## Keep true

- VP8 only; do not add VP9, AV1, or WebM container logic to the codec package.
- Keep the codec package pure Go with no cgo or runtime libvpx dependency.
- Prefer correctness and oracle parity before unsafe, SIMD, or assembly work.
- Keep hot decode/encode paths allocation-conscious and benchmark visible.
- Keep CI simple and quick; put slow corpus/oracle work behind explicit targets.

## Next priorities

1. Tighten decoder conformance against the external VP8 corpus.
2. Improve encoder parity and quality guards against the pinned libvpx oracle.
3. Continue rate-control and temporal-layer behavior checks with focused tests.
4. Add performance work only after scalar correctness is covered by tests.
5. Keep README, `UPSTREAM.md`, and `docs/performance.md` current as behavior
   changes.
