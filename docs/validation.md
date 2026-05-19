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

`go test ./... -count=1`

Minimum gate for structural changes, same-package file splits, root facade
changes, and internal package moves.

`go test -tags purego ./... -count=1`

Verifies the scalar fallback build. Run this when touching DSP dispatch,
architecture-tagged files, or common code used by both SIMD and pure-Go paths.

`go test -tags govpx_oracle_trace ./... -run '^$' -count=1`

Compile-only oracle-trace gate. Run it after moving tagged trace files,
scoreboard helpers, or oracle-only instrumentation.

## CI Gate

`make ci`

Runs formatting/fuzz-seed checks, PGO freshness, default `go test ./...`,
purego `go test ./...`, VP9 decoder conformance smoke against pinned libvpx
Profile 0 data, VP9 quality fixtures, and the small VP9 BD-rate smoke subset.

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

`make verify-bd-rate`

Runs the slower VP9 BD-rate feature sweep. Use it for VP9 AltRef, ARNR, TPL,
AQ, loop-filter, and quality-affecting encode changes.

`make verify-bd-rate-vp8`

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

Do not run `scoreboard-update`, set `GOVPX_UPDATE_BASELINES=1`, or update parity
baselines just to make tests pass. Baseline changes require a separate explicit
packet whose purpose is to update the baseline after the behavior change has
been reviewed.
