# govpx

[![CI](https://github.com/thesyncim/govpx/actions/workflows/ci.yml/badge.svg)](https://github.com/thesyncim/govpx/actions/workflows/ci.yml)

`govpx` is a pure-Go VP8 codec library inspired by the frozen libvpx v1.16.0
baseline. It is VP8-only, does not use cgo, and exposes a compact Go API rather
than mirroring the libvpx C API.

This repository is an active scalar port. It is useful for development,
conformance work, and experimentation, but it is not yet a production-ready VP8
replacement.

## What Works

- Raw VP8 frame parsing through `PeekVP8StreamInfo`
- VP8 decode APIs with borrowed-frame and caller-owned-buffer paths
- VP8 encode APIs with realtime-oriented CBR/CQ rate control, temporal metadata, and
  keyframe/interframe support
- Whole-block, B_PRED, NEWMV, and SPLITMV encoder mode paths, including VP8
  split partition-shape selection
- Token partitions, loop filtering, granular postprocess flags, runtime token
  partition/sharpness/static-threshold controls, error-concealment decode
  handling, LAST/GOLDEN/ALTREF reference selection, and entropy update controls
- Checked-in smoke vectors plus an exhaustive libvpx-backed parity gate

## Scope

`govpx` deliberately stays narrow:

- VP8 only; no VP9 or AV1
- No WebM muxing or demuxing in the codec package
- No cgo or runtime libvpx dependency
- No full libvpx C API compatibility

The codec package works with VP8 frame packets. IVF support lives in
tests/tools, and WebRTC/WebM/container integration belongs outside the core
package.

## Install

```sh
go get github.com/thesyncim/govpx
```

Requirements:

- Go 1.26 or newer
- A POSIX shell for the Makefile targets
- `curl`, `make`, and a C toolchain for the optional libvpx oracle gate

## Quick Taste

```go
package main

import "github.com/thesyncim/govpx"

func decodePacket(packet []byte) (govpx.Image, bool, error) {
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		return govpx.Image{}, false, err
	}
	defer dec.Close()

	if err := dec.Decode(packet); err != nil {
		return govpx.Image{}, false, err
	}
	frame, ok := dec.NextFrame()
	return frame, ok, nil
}
```

The returned `Image` is planar 8-bit 4:2:0. Borrowed decoder image data remains
valid until the next decode, reset, or close.

## Development Checks

Fast local gate:

```sh
make ci
```

This runs `gofmt` checking and `go test ./... -count=1`. Oracle tests skip in
this path, so it does not build libvpx or download external corpora.

Exhaustive parity gate:

```sh
make verify-production
```

This builds pinned libvpx v1.16.0 oracle helpers (`govpx-vpx-oracle`, `vpxenc`,
and `vpxdec`), downloads the VP8 IVF/source corpora under
`internal/coracle/build/test-data/`, and runs the root `TestOracle*` suite with
required corpus checks enabled.

GitHub Actions runs `make verify-production` for pushes and pull requests.

## Benchmarks

```sh
go run ./cmd/govpx-bench
go test ./benchmarks -bench Decode -benchmem -json
```

Set `GOVPX_VPXENC` or pass `-libvpx-vpxenc` to include a local libvpx encoder
comparison. Set `GOVPX_ORACLE` or pass `-libvpx-oracle` to time the pinned
libvpx checksum oracle for decode comparison.

## Repository Layout

- Root package: public VP8 decoder/encoder API
- `internal/vp8`: scalar VP8 port internals
- `internal/testutil`: IVF, checksum, and conformance helpers
- `internal/coracle`: optional libvpx oracle build scripts
- `cmd/govpx-bench`: synthetic encode/decode benchmark tool
- `cmd/govpx-oracle`: wrapper for the optional checksum oracle
- `examples/webrtc-vp8`: separate example module for browser playback
- `docs/performance.md`: scalar performance notes

## Licensing

See `LICENSE`, `NOTICE`, `LICENSE.libvpx`, `PATENTS.libvpx`, and `UPSTREAM.md`
for project and upstream libvpx licensing details.
