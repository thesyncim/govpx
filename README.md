# govpx

`govpx` is a pure-Go VP8 codec library inspired by the frozen libvpx v1.16.0
baseline. It is VP8-only, has no cgo dependency, and exposes a small Go-style
API instead of mirroring the libvpx C API.

## Current status

This is an active scalar port, not a finished production codec yet.

Implemented paths include a growing VP8 decoder and encoder surface: keyframes,
token partitions, loop filtering, postprocess options, error-resilient decode
handling, interframes with LAST/GOLDEN/ALTREF reference selection, temporal
metadata, rate-control controls, smoke vectors, and opt-in libvpx parity tests.

The broad external VP8 corpus and encoder quality/parity checks run in GitHub
Actions through the full parity gate.

## Non-goals

- VP9 or AV1
- WebM muxing/demuxing in the codec package
- cgo or runtime libvpx linkage
- Full libvpx C API compatibility

## Requirements

- Go 1.26 or newer
- A POSIX shell for the Makefile targets

## Fast local checks

```sh
make ci
```

`make ci` runs `gofmt` checking and `go test ./... -count=1`. The oracle tests
skip unless explicitly enabled, so this path does not build libvpx or download
external corpora.

## Full parity gate

```sh
make verify-production
```

This slower opt-in target builds the pinned libvpx v1.16.0 oracle, `vpxenc`,
and `vpxdec`; downloads the VP8 IVF/source corpora under
`internal/coracle/build/test-data/`; and runs the `TestOracle*` suite with the
required corpus checks enabled.

GitHub Actions runs this full parity gate for pushes and pull requests.

## Benchmarks

```sh
go run ./cmd/govpx-bench
go test ./benchmarks -bench Decode -benchmem -json
```

Set `GOVPX_VPXENC` or `GOVPX_ORACLE` to compare against locally built libvpx
helpers. `make oracle-tools` builds those helpers without running the full
parity gate.

## Layout

- Root package: public VP8 decoder/encoder API
- `internal/vp8`: scalar VP8 port internals
- `internal/testutil`: IVF, checksum, and conformance helpers
- `internal/coracle`: optional libvpx oracle build scripts
- `cmd/govpx-bench`: synthetic encode/decode benchmark tool
- `docs/performance.md`: local scalar performance notes
