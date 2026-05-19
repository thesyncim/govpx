# Validation

Use the smallest gate that proves the class of change, then run the stricter
gate before a safe-point commit when the touched code can affect codec behavior.

Keep validation entry points boring. Prefer named Makefile targets and focused
package tests over copying long environment-variable command lines into docs,
PRs, or tickets. If a gate needs libvpx paths, corpus paths, or strict oracle
settings, put that wiring behind a make target and document the target name.

## Local Gates

`make pre-commit`

Checks Go formatting, fuzz seed names, and the VP8 benchmark PGO source
fingerprint. If it reports a PGO mismatch after touching tracked hot-path
sources, run `make pgo-refresh` and rerun the gate.

`make test`

Minimum gate for structural changes, same-package file splits, root facade
changes, and internal package moves.

`make test-purego`

Verifies the scalar fallback build. Run this when touching DSP dispatch,
architecture-tagged files, or common code used by both SIMD and pure-Go paths.

`make test-trace`

Compile-only oracle-trace gate. Run it after moving tagged trace files,
scoreboard helpers, or oracle-only instrumentation.

## Test Hygiene

Test consolidation must preserve behavior coverage, not just line coverage.
When combining duplicated task, audit, fuzz, oracle, or regression files, carry
each distinct seed, fixture, edge case, and assertion into the new suite before
deleting the old file.

Prefer normal Go test shapes: table tests for repeated contract checks,
`t.Helper` helpers for shared setup, package-local tests beside internal
implementation, `internal/testutil` for cross-package fixtures, and `testdata`
for durable corpus files. Avoid environment-heavy recipes in ordinary test
docs; put slow oracle/corpus wiring behind named Makefile gates.

## CI Gate

`make ci`

Runs formatting/fuzz-seed checks, PGO freshness, default `go test ./...`,
purego `go test ./...`, trace-tag compile checks, VP9 decoder conformance
smoke against pinned libvpx Profile 0 data, VP9 quality fixtures, and the small
VP9 BD-rate smoke subset.

Run `make ci` before committing changes that touch:

- public codec APIs;
- decoder or encoder behavior;
- RTP packetization or assembly;
- stream-info parsing;
- rate control, frame flags, reference semantics, or postprocess controls;
- PGO-tracked hot-path source files.

## Oracle And Production Gates

`make verify-decoder-parity`

Runs `make ci` plus decoder oracle checks. Use this for decoder syntax,
reconstruction, threading, reference, error-concealment, or postprocess changes.

`make verify-production`

Runs `make ci`, oracle tests, byte-parity checks, and scoreboards. This is the
final integration gate before the tidy branch is ready to leave draft.

`make test-oracle`

Runs the libvpx-backed oracle regression suite. Use this for behavior-sensitive
encoder/decoder changes when full production verification would be excessive.

`make test-byte-parity`

Runs strict byte-parity gates under the oracle trace build.

`make test-scoreboard`

Runs parity scoreboards through the report wrapper without updating baselines.

`make test-bdrate-vp9`

Runs the slower VP9 BD-rate feature sweep. Use it for VP9 AltRef, ARNR, TPL,
AQ, loop-filter, and quality-affecting encode changes.

`make test-bdrate-vp8`

Runs VP8 BD-rate quality gates against the pinned libvpx `vpxenc` oracle. Use it
for VP8 quality, rate-control, loop-filter, AQ-like segmentation, denoise, ARNR,
or temporal encode changes.

## Fuzz And Regression Gates

Use focused fuzz targets when a change touches parser or runtime-control input
surfaces:

```sh
go test -run '^$' -fuzz FuzzVP8DecoderDecodeInto ./...
go test -run '^$' -fuzz FuzzVP9DecoderDecodeInto ./...
go test -run '^$' -fuzz FuzzVP8EncoderRuntimeControls ./...
go test -run '^$' -fuzz FuzzVP9EncoderRuntimeControls ./...
```

After a fuzz run writes new seeds, run `make fuzz-rename` before `make
pre-commit`.

## Allocation And Performance

Hot-path changes must preserve existing allocation behavior. Run focused
allocation tests when touching encode/decode loops, optional instrumentation,
oracle hooks, debug counters, tracing, or caller-owned buffer paths:

```sh
go test ./... -run 'Alloc|Allocs' -count=1
```

For disabled tracing or testing hooks, also collect compiler evidence from the
touched package and confirm trace builds still compile:

```sh
go test -run '^$' -gcflags='github.com/thesyncim/govpx=-m=2' .
go test -tags govpx_oracle_trace ./... -run '^$' -count=1
```

The disabled-path proof is the default production build: no
`govpx_oracle_trace` tag and no `GOVPX_*` oracle or trace environment
variables. Run allocation or benchmark checks on the touched encode/decode path
in that shape first, then run the trace compile gate to prove the tagged
instrumentation still builds. Put any external oracle or corpus wiring behind a
named Makefile gate instead of copying long environment recipes into docs or
PRs.

For method-shape or ownership rewrites in hot code, collect compiler evidence
before and after the edit with package-scoped `-gcflags` so the signal is not
buried under standard-library diagnostics:

```sh
go test -run '^$' -gcflags='github.com/thesyncim/govpx=-m=2' .
go test -run '^$' -gcflags='github.com/thesyncim/govpx/internal/vp8/encoder=-m=2' ./internal/vp8/encoder
go test -run '^$' -gcflags='github.com/thesyncim/govpx/internal/vp9/encoder=-m=2' ./internal/vp9/encoder
go test -run '^$' -gcflags='github.com/thesyncim/govpx/internal/vp9/decoder=-m=2' ./internal/vp9/decoder
```

Treat a method conversion as acceptable only when the receiver expresses real
state ownership and the before/after evidence preserves inlining, escape
behavior, and allocation contracts. Keep pure arithmetic, syntax writers, and
DSP kernels as free functions unless measurement proves otherwise.

Use representative benchmarks for measured performance work rather than
assuming a change is free:

```sh
go test ./... -bench . -run '^$'
```

Document any approved non-zero allocation or measured tradeoff in this file
before merging the final integration sweep.

## Baseline Rules

Do not run `update-scoreboards`, set `GOVPX_UPDATE_BASELINES=1`, or update
parity baselines just to make tests pass. Baseline changes require a separate
explicit packet whose purpose is to update the baseline after the behavior
change has been reviewed.
