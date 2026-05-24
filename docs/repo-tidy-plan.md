# Repository Tidy Plan

The cleanup target is a Go-style codec library with
`github.com/thesyncim/govpx` as the public import path and implementation
ownership under internal packages.

## Non-Negotiables

- Keep the root package as the public facade: options, errors, images, RTP
  types, constructors, and stable codec handles.
- Put VP8-only implementation under `internal/vp8` and VP9-only implementation
  under `internal/vp9`.
- Share only mechanical VPx helpers under `internal/vpx`; do not force codec
  symmetry where libvpx behavior differs.
- Treat the module as unreleased. Remove bad names, compatibility shims,
  staging aliases, historical wrappers, and old internal call shapes instead of
  preserving them.
- Keep disabled tracing, oracle hooks, debug counters, and test-only plumbing
  allocation-free and absent from production hot loops.
- Preserve hot-path inlining, escape behavior, and allocation profiles unless a
  measured tradeoff is approved.
- Use the pinned libvpx version in `UPSTREAM.md` as the oracle source of truth.
  Do not update baselines just to make a gate pass.

## Active Passes

Current lane split: this session owns VP8 and VP9 cleanup work again. Parallel
lanes may still take narrow packets, but every safe point must keep the
cross-codec package boundaries, tests, and documentation coherent.

1. Inventory and guardrails: maintain file clusters, large-file exceptions,
   protected gates, and no-overlap ownership notes.
2. Public facade: keep user-facing APIs small, explicit, documented, and
   idiomatic for Go callers.
3. Mechanical splits: shrink large hand-authored implementation and test files
   before package moves.
4. Codec package moves: move private VP8/VP9 encoder and decoder internals to
   codec-owned internal packages, leaving root wrappers only where they are the
   final API.
5. Real deduplication: run an explicit VP8/VP9 reusable-mechanics audit before
   each move wave. Move packet assembly scaffolding, buffer sizing, validation,
   timebase math, bounded arithmetic, rate-control value objects, and test
   harness utilities to `internal/vpx` only when both codecs truly share the
   mechanics. Keep bitstream syntax, prediction semantics, probability models,
   reference behavior, and codec-specific controls in their codec packages.
   Shared helpers need focused unit tests that do not require libvpx binaries.
6. Oracle harness extraction: move libvpx path resolution, process wrappers,
   trace parsing, parity reporting helpers, and fixture plumbing into
   `internal/coracle` or `internal/vpx/testharness`. Root tests should describe
   public behavior, not carry oracle implementation details.
7. Test suite hygiene: rename historical task/audit tests into objective
   behavior names, merge duplicate coverage, remove unexplained diagnostics,
   and keep reusable helpers outside root.
8. Tracing and performance hygiene: prove disabled observability paths have no
   allocations, no clock reads, no atomics, no interface dispatch, and no hot
   loop branches unless measurements prove the cost is noise.
9. Dead code removal: delete unused helpers, stale compatibility shapes, and
   unreferenced diagnostics after focused tests prove coverage remains.
10. Documentation rewrite: keep README short; put API, architecture, codec
    status, validation, and hard-to-read parity notes under `docs/`.
11. Parity and feature-gap improvement: after the structure is stable, use the
    cleaner package and test boundaries to close the highest-value VP8/VP9
    parity gaps documented by the current parity reports.

## Safe-Point Gate

At each safe point, run the smallest focused tests that prove the packet, then
run:

```sh
go test ./... -count=1
make pre-commit
make ci
```

Run `make verify-production` for final integration and for codec/parity changes
that need the full oracle suite.
