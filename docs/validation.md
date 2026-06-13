# Validation

Use the smallest gate that proves the change, then run the stricter gate before
a safe-point commit when codec behavior or hot paths can be affected.

## Local Gates

`make pre-commit`

Checks Go formatting, fuzz seed names, and the VP8 benchmark PGO source
fingerprint. If it reports a PGO mismatch after touching tracked hot-path
sources, run `make pgo-refresh` and rerun the gate.

`go test ./... -count=1`

Minimum gate for structural changes, file splits, root facade changes, internal
package moves, and test-suite cleanup.

`make test-purego`

Verifies scalar fallback builds. Run it when touching DSP dispatch,
architecture-tagged files, or shared code used by both SIMD and pure-Go paths.

`make test-trace`

Compile-only oracle-trace gate. Run it after moving tagged trace files,
parity report helpers, or oracle-only instrumentation.

## CI Gate

`make ci`

Runs formatting and fuzz-seed checks, PGO freshness, default `go test ./...`,
purego `go test ./...`, trace-tag compile checks, VP9 decoder conformance
against pinned libvpx Profile 0 data, VP9 quality fixtures, and the small VP9
BD-rate subset.

Run `make ci` before committing changes that touch:

- public codec APIs;
- decoder or encoder behavior;
- RTP packetization or assembly;
- stream-info parsing;
- rate control, target levels, frame flags, reference semantics, or
  postprocess controls;
- PGO-tracked hot-path source files.

## Oracle And Production Gates

Libvpx-backed tests should use helpers from `internal/testutil/vp8test` and
`internal/testutil/vp9test` for binary resolution, trace projection, and oracle
process plumbing. The lower-level `internal/coracle/coracletest` package is
compiled only under `govpx_oracle_trace`. Root test files are expected to
describe public behavior and codec parity; reusable harness code belongs
outside root.

`make verify-decoder-parity`

Runs `make ci` plus decoder oracle checks. Use this for decoder syntax,
reconstruction, threading, reference, error-concealment, or postprocess changes.

`make verify-production`

Runs `make ci`, oracle tests, byte-parity checks, and parity reports. This is the
final integration gate for behavior-sensitive release work.

`make test-oracle`

Runs the libvpx-backed oracle suite. Use this for behavior-sensitive
encoder/decoder changes when full production verification would be excessive.
It also runs the tagged VP9 internal source/blob oracle checks, so default
`go test ./...` stays free of libvpx checkout probes and oracle-asset skips.

`make test-vp9-internal-oracle`

Runs only the tagged VP9 internal oracle checks for generated tables, DSP
kernels, token costs, and source-derived constants. Use this when touching
`internal/vp9` tables, DSP kernels, probability costs, or intra-extension
metadata without needing the whole root oracle suite.

`make test-byte-parity`

Runs strict byte-parity gates under the oracle trace build. This compares
govpx's host-ISA assembly build against the host-ISA (NEON/SSE) libvpx oracle —
the fair asm-vs-asm lane.

`make test-byte-parity-purec`

Fair pure-Go-vs-pure-C VP8 byte-parity lane. govpx is built with `-tags purego`
(no assembly) and the oracle is `vpxenc-purec`, a SIMD-disabled (`generic-gnu`)
libvpx built by `internal/coracle/build_vpxenc_purec.sh`. Both sides are scalar,
so a divergence here is an algorithm difference rather than a SIMD-rounding
artifact. Keep the two lanes separate: pure-Go vs generic-gnu here; Go+asm vs
host-ISA in `test-byte-parity`. Scoped to VP8 oracle tests that use the plain
vpxenc oracle (the pure-C binary carries no frameflags/VP9 patch).

As of 2026-06-13 this lane is green, including the formerly red positive
`cpu_used=8` realtime timing and extended cases. Treat any future red result
here as an algorithmic scalar-vs-scalar regression first, not a SIMD artifact.

As of 2026-06-13 the stats-backed VP9 two-pass q setup is also covered by:

```sh
GOVPX_WITH_ORACLE=1 go test -tags govpx_oracle_trace . -run 'TestVP9OracleTwoPass(Stream|Constant)ByteParity|TestVP9RecodeSeed1_1_1_1_0' -count=1 -v
```

The constant two-pass case now matches libvpx q exactly. The panning stream
still lacks byte parity because GF refresh scheduling and inter allocation
remain ahead of the q path, but the opening keyframe q moved from the old
one-pass fallback (q=122) to q=16 vs libvpx q=17.

`make test-parity-report`

Runs parity reports through the report wrapper without updating baselines.

`make test-bdrate-vp9`

Runs the slower VP9 BD-rate quality sweep. Use it for VP9 AltRef, ARNR, TPL,
AQ, loop-filter, and quality-affecting encode changes.

`make test-bdrate-vp8`

Runs VP8 BD-rate quality gates against the pinned libvpx `vpxenc` oracle. Use it
for VP8 quality, rate-control, loop-filter, segmentation, denoise, ARNR, or
temporal encode changes.

## Fuzz Gates

Use focused fuzz targets when a change touches parser or runtime-control input
surfaces:

```sh
go test -run '^$' -fuzz FuzzVP8DecoderDecodeInto ./...
go test -run '^$' -fuzz FuzzVP9DecoderDecodeInto ./...
go test -run '^$' -fuzz FuzzVP8EncoderRuntimeControls ./...
go test -run '^$' -fuzz FuzzVP9EncoderRuntimeControls ./...
```

After a fuzz run writes new seeds, run `make fuzz-rename` before
`make pre-commit`.

## Allocation And Performance

Hot-path changes must preserve existing allocation behavior. Run focused
allocation tests when touching encode/decode loops, optional instrumentation,
oracle hooks, debug counters, tracing, or caller-owned buffer paths:

```sh
go test ./... -run 'Alloc|Allocs' -count=1
```

For VP9 CBR post-encode drop behavior, pair the feature tests with the runtime
allocation gate so the optional path does not charge ordinary `EncodeInto`
calls:

```sh
go test . -run 'PostEncodeDrop|RuntimeControlsAllocationGate' -count=1
```

For disabled tracing or testing hooks, prove the default production build first:
no `govpx_oracle_trace` tag and no `GOVPX_*` oracle or trace environment
variables. Then compile the trace-tag build:

```sh
go list -f '{{.TestImports}}' . ./benchmarks
go test . -run 'TestVPxOracleTraceDisabled' -count=1
go test . -run 'TestVP8PhaseStatsDisabled' -count=1
go test -tags govpx_oracle_trace ./... -run '^$' -count=1
```

VP8 phase timing is an opt-in build shape so the default encoder hot path and
default `EncoderOptions` surface have no runtime stats branches or fields.
When changing PhaseStats plumbing or `cmd/govpx-bench -phase-timing`, also
compile and run the tagged shape:

```sh
go test -tags govpx_phase_stats . -run 'TestVP8PhaseStatsEnabled' -count=1
go test -tags govpx_phase_stats ./cmd/govpx-bench/benchcmd -run 'TestRunBenchmarkPhaseTiming' -count=1
```

For method-shape or ownership rewrites in hot code, collect compiler evidence
with package-scoped `-gcflags` so standard-library diagnostics do not bury the
signal:

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

Use representative benchmarks for measured performance work:

```sh
go test ./... -bench . -run '^$'
```

Document approved non-zero allocations or measured tradeoffs here before
merging the final integration sweep.

## Baseline Rules

Do not run `update-parity-baselines`, set `GOVPX_UPDATE_BASELINES=1`, or update
parity baselines just to make tests pass. Baseline changes require a separate
explicit packet whose purpose is to update the baseline after the behavior
change has been reviewed.
