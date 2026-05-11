# govpx

[![CI](https://github.com/thesyncim/govpx/actions/workflows/ci.yml/badge.svg)](https://github.com/thesyncim/govpx/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thesyncim/govpx.svg)](https://pkg.go.dev/github.com/thesyncim/govpx)

Pure-Go VP8 encoder and decoder.

govpx is for Go programs that need VP8 without cgo or a libvpx runtime
dependency. It is VP8-only: no VP9, no AV1, no WebM muxer, and no libvpx C API
compatibility layer.

The implementation is tested against a pinned libvpx v1.16.0 oracle. The codec
package exposes Go types and methods; libvpx naming is kept only where it
identifies upstream behavior, parity tests, or oracle tooling.

## Requirements

Go 1.26 or newer.

```sh
go get github.com/thesyncim/govpx
```

## Decode VP8

```go
dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
if err != nil {
	return err
}
defer dec.Close()

if err := dec.Decode(packet); err != nil {
	return err
}
frame, ok := dec.NextFrame()
if !ok {
	return nil
}
_ = frame // I420: Y, U, V plus per-plane strides.
```

`NextFrame` returns an image backed by decoder-owned storage. It is valid until
the next decode, reset, or close. Use `DecodeInto` / `DecodeIntoWithPTS` when
the caller owns the destination buffers.

Decoder options include error concealment, postprocess flags, max dimensions,
and resolution-change rejection.

## Encode VP8

```go
enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
	Width:             640,
	Height:            480,
	FPS:               30,
	RateControlMode:   govpx.RateControlCBR,
	TargetBitrateKbps: 1500,
	Deadline:          govpx.DeadlineRealtime,
})
if err != nil {
	return err
}
defer enc.Close()

dst := make([]byte, 256*1024)
for i, src := range frames {
	res, err := enc.EncodeInto(dst, src, uint64(i), 1, 0)
	if err != nil {
		return err
	}
	if res.Dropped {
		continue
	}
	writePacket(res.Data) // res.Data aliases dst; copy it if it must outlive dst.
}
```

Input images are planar 8-bit 4:2:0 (`Image{Y,U,V,*Stride}`). Encoded output is
a raw VP8 frame payload, not IVF or WebM.

Encoder support includes CBR, CQ, one-pass and two-pass VBR controls, temporal
layers, token partitions, adaptive keyframes, lookahead, automatic alt-ref,
ARNR, denoise, screen-content mode, active maps, and realtime control methods
such as `SetBitrateKbps`, `SetRateControl`, `SetRealtimeTarget`, `SetDeadline`,
and `SetCPUUsed`.

For lookahead or auto-alt-ref, `EncodeInto` can return `ErrFrameNotReady` while
frames are queued. Call `FlushInto` at end of stream until it returns no more
data.

## Validation

Fast local checks:

```sh
make ci
go test ./... -count=1
go vet ./...
```

Oracle checks:

```sh
make verify-production
```

`verify-production` builds the pinned libvpx oracle tools under
`internal/coracle/build`, fetches VP8 conformance data, and runs decode plus
encoder parity gates. The oracle trace harness is behind the
`govpx_oracle_trace` build tag and is off in normal builds.

## Benchmark

```sh
go run ./cmd/govpx-bench
go run ./cmd/govpx-bench -decode -frames=120
go run ./cmd/govpx-bench -format=json
```

The benchmark can compare govpx with the pinned libvpx tools when they are
available. By default it looks for `internal/coracle/build/vpxenc` and
`internal/coracle/build/govpx-vpx-oracle`, building them with `make
oracle-tools` if needed. Use `-auto-libvpx=false` for govpx-only runs.

Do not treat README numbers as performance data. Run the benchmark on the target
machine, Go version, CPU, frame size, bitrate, deadline, and thread count that
matter for your workload.

## Repository Layout

```text
.                         public govpx package
internal/vp8/common       VP8 shared state, headers, loop filter, quant tables
internal/vp8/decoder      decoder internals
internal/vp8/encoder      packet, token, transform, quant, and motion helpers
internal/vp8/dsp          scalar and architecture-specific pixel kernels
internal/coracle          pinned libvpx oracle build and comparison helpers
cmd/govpx-bench           encode/decode benchmark CLI
cmd/govpx-oracle          oracle wrapper command
cmd/scoreboard-report     oracle scoreboard runner
examples/webrtc-vp8       separate WebRTC example module
testdata                  oracle scoreboard baselines
```

## Development Notes

Production builds should not pay for diagnostics. Trace and oracle-only code is
build-tagged out by default; opt-in phase timing only runs when
`EncoderOptions.PhaseStats` is non-nil.

Keep hot-path changes allocation-aware. The encoder is designed so steady-state
`EncodeInto` can reuse caller and encoder-owned buffers instead of allocating
per frame.

## License

BSD-3-Clause. See `LICENSE`, `NOTICE`, `LICENSE.libvpx`, `PATENTS.libvpx`, and
`UPSTREAM.md`.
