# Validation

Use the smallest gate that verifies your change, and run `make ci` before
merging behavior- or performance-sensitive changes. All `make` targets and
`scripts/go.sh` run with `go 1.27.4` and `GOEXPERIMENT=simd`.

## Local & CI Gates

| Gate | Purpose |
| --- | --- |
| `make pre-commit` | `gofmt`, fuzz seed naming, PGO fingerprint, and PGO build check (`make pgo-refresh` to update). |
| `scripts/go.sh test -short ./...` | Fast pure-Go unit, codec, and facade test sweep (no external binaries). |
| `scripts/go.sh test ./... -count=1` | Full default test suite across all packages. |
| `make test-noasm` / `make test-purego` | Verifies scalar fallback builds (`-tags purego`) without SIMD. |
| `GOARCH=amd64 scripts/go.sh vet ./...` | Cross-architecture compile/vet check for amd64 SIMD files. |
| `make test-trace` | Compile-only check for `govpx_oracle_trace` builds. |
| `make ci` | Standard CI gate: `pre-commit`, default tests, purego tests, trace compile, VP9 decoder conformance, VP9 quality fixtures, and VP9 BD-rate subset. |

## Oracle & Production Gates

Oracle tests build pinned `libvpx v1.16.0` tools under `internal/coracle/build`
and are gated behind `GOVPX_WITH_ORACLE=1`:

- `make test-oracle` — libvpx-backed VP8/VP9 oracle suite and tagged VP9
  internal oracle checks.
- `make test-vp9-internal-oracle` — tagged VP9 table/DSP/constant oracle checks.
- `make test-byte-parity` — strict byte-parity checks comparing govpx's pure-Go
  SIMD build against host-ISA `libvpx`.
- `make test-byte-parity-purec` — scalar-vs-scalar VP8 byte-parity lane
  (`-tags purego` vs `generic-gnu` `vpxenc-purec`).
- `make test-bdrate-vp8` / `make test-bdrate-vp9` — BD-rate quality gates
  against pinned `vpxenc`.
- `make verify-decoder-parity` — `make ci` plus decoder oracle checks.
- `make verify-production` — full release gate (`make ci`, oracle tests, byte
  parity, and parity reports).

Do not run `update-parity-baselines` or set `GOVPX_UPDATE_BASELINES=1` casually;
baseline updates require a dedicated reviewed change.

## Safety, Allocation & Fuzz Checks

```sh
# Enforce zero unsafe imports and zero hand-written assembly
git grep -l '"unsafe"' -- '*.go'
find . -name '*.s'

# Allocation guards
scripts/go.sh test ./... -run 'Alloc|Allocs' -count=1

# Optional build-tagged diagnostics stay zero-cost in default builds
scripts/go.sh test . -run 'TestVPxOracleTraceDisabled|TestVP8PhaseStatsDisabled' -count=1
scripts/go.sh test -tags govpx_phase_stats . -run 'TestVP8PhaseStatsEnabled' -count=1

# Focused fuzzing (run `make fuzz-rename` if new seeds are added)
scripts/go.sh test -run '^$' -fuzz FuzzVP8DecoderDecodeInto ./...
scripts/go.sh test -run '^$' -fuzz FuzzVP9DecoderDecodeInto ./...
scripts/go.sh test -run '^$' -fuzz FuzzVP8EncoderRuntimeControls ./...
scripts/go.sh test -run '^$' -fuzz FuzzVP9EncoderRuntimeControls ./...
```
