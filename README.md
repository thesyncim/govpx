# govpx

[![CI](https://github.com/thesyncim/govpx/actions/workflows/ci.yml/badge.svg)](https://github.com/thesyncim/govpx/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thesyncim/govpx.svg)](https://pkg.go.dev/github.com/thesyncim/govpx)

Pure-Go VP8 encoder and decoder for raw VP8 frame payloads.

govpx is for Go programs that need VP8 without cgo and without a libvpx runtime
dependency. The package is intentionally VP8-only: no VP9, no AV1, no WebM
muxer, no RTP packetizer, and no libvpx C API compatibility layer.

The implementation is validated against a pinned libvpx v1.16.0 oracle. The API
uses Go types and methods; libvpx names appear only when they identify upstream
behavior, controls, or validation tooling.

## Install

Go 1.26 or newer is required. The module pins the default toolchain to Go
1.26.3 so local `go` commands use the same patch level as CI.

```sh
go get github.com/thesyncim/govpx
```

Use `-tags purego` when a compile-time scalar build is required. The tag
excludes govpx architecture assembly and selects Go fallbacks in
`internal/vp8/dsp` and `internal/vp8/encoder`.

## Decode

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

`NextFrame` returns decoder-owned storage that stays valid until the next
decode, reset, or close. Use `DecodeInto` / `DecodeIntoWithPTS` when the caller
owns the destination buffers.

Decoder controls include threading, error concealment, postprocess filters,
maximum dimensions, resolution-change rejection, frame metadata, and LAST /
GOLDEN / ALTREF reference-buffer set/copy methods.

## Encode

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
one raw VP8 payload per packet, not IVF or WebM.

Supported encoder controls include:

- Rate control: VBR, CBR, CQ, VPX_Q, one-pass and two-pass VBR, runtime bitrate
  and target updates, frame dropping, buffer sizing, min/max quantizers, and
  max intra bitrate.
- Realtime/WebRTC: error resilience, temporal scalability, keyframe forcing,
  CPU-used/deadline updates, VP8 RTC external-rate-control mode, and reference
  buffer set/copy methods.
- Quality and tools: adaptive keyframes, lookahead, automatic alt-ref, ARNR,
  denoise, token partitions, loop-filter sharpness, screen-content mode,
  static threshold, active maps, ROI maps, PSNR/SSIM tuning, and threading.

Lookahead and auto-alt-ref can make `EncodeInto` return `ErrFrameNotReady` while
frames are queued. Call `FlushInto` at end of stream until it returns no more
data.

## API Map

| Task | API |
| --- | --- |
| Decode one packet | `Decode`, then `NextFrame` |
| Decode into caller-owned buffers | `DecodeInto`, `DecodeIntoWithPTS` |
| Inspect a packet header | `PeekVP8StreamInfo` |
| Encode one frame | `EncodeInto` |
| Drain delayed encoder output | `FlushInto` |
| Force a keyframe | `ForceKeyFrame` or `EncodeForceKeyFrame` |
| Runtime bitrate/FPS update | `SetRealtimeTarget` |
| Change frame dropping only | `SetFrameDropAllowed` or `RealtimeTarget.FrameDrop` |
| Runtime rate-control replacement | `SetRateControl` |
| Two-pass encode | `CollectFirstPassStats`, `FinalizeFirstPassStats`, `SetTwoPassStats` |
| Reference buffer control | `SetReferenceFrame`, `CopyReferenceFrame` |
| Last decoded/encoded metadata | `LastFrameInfo`, `LastQuantizer`, `EncodeResult` |

## WebRTC Profile

For WebRTC senders, start with realtime CBR, error resilience, frame dropping,
and RTC external rate control:

```go
enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
	Width:                  1280,
	Height:                 720,
	FPS:                    30,
	RateControlMode:        govpx.RateControlCBR,
	TargetBitrateKbps:      2500,
	MinQuantizer:           4,
	MaxQuantizer:           56,
	BufferSizeMs:           600,
	BufferInitialSizeMs:    400,
	BufferOptimalSizeMs:    500,
	DropFrameAllowed:       true,
	DropFrameWaterMark:     60,
	Deadline:               govpx.DeadlineRealtime,
	ErrorResilient:         true,
	RTCExternalRateControl: true,
})
```

Use `ForceKeyFrame` or `EncodeForceKeyFrame` for PLI/FIR. Use
`SetRealtimeTarget` for bandwidth-estimation updates. The zero value of
`RealtimeTarget.FrameDrop` leaves frame dropping unchanged, so bitrate-only BWE
updates do not accidentally disable dropping.

See `examples/webrtc-vp8` for a separate-module demo that streams govpx VP8
through pion/webrtc to a browser.

## Validation

Fast local checks:

```sh
make ci
go test ./... -count=1
go test -tags purego ./... -count=1
go vet ./...
```

Production parity gate:

```sh
make verify-production
```

`verify-production` builds pinned libvpx tools under `internal/coracle/build`,
fetches VP8 conformance data, and runs decode plus encoder oracle tests. Encoder
output parity compares libvpx-decoded frame checksums, key/show decisions,
internal qindex, and packet-size ratchets for the covered realtime/WebRTC
settings.

Oracle trace and scoreboard code is behind the `govpx_oracle_trace` build tag
or lives in `*_test.go`; it is not linked into normal production builds.

## Benchmarking

```sh
go run ./cmd/govpx-bench
go run ./cmd/govpx-bench -decode -frames=120
go run ./cmd/govpx-bench -format=json
```

Encode benchmarks compare against libvpx by default when `cmd/govpx-bench` can
find `internal/coracle/build/vpxenc` or `vpxenc` on `PATH`. Pass
`-libvpx-vpxenc=/path/to/vpxenc` to force a binary or `-auto-libvpx=false` for a
govpx-only run. Decoder reference timing uses `govpx-vpx-oracle` only in
`-decode` mode. Use `-build-libvpx=true` only when you want the bench command to
build the pinned tools.

Plotting is optional and requires an ffmpeg binary with both the `libvpx`
encoder and `libvmaf` filter:

```sh
go run ./cmd/govpx-bench -plot benchmarks/govpx-vs-libvpx.svg -width=1280 -height=720 -frames=120 -fps=30 -bitrate=2500 -mode=realtime -threads=1
```

The plot path encodes the libvpx reference with `ffmpeg -c:v libvpx`, scores
govpx and libvpx with ffmpeg's `libvmaf`, `psnr`, and `ssim` filters, and writes
SVG plus sibling CSV/JSON files.

Do not treat README numbers as performance data. Measure on the Go version, CPU,
frame size, bitrate, deadline, thread count, and build tags that match your
workload.

`cmd/govpx-bench/default.pgo` is checked in intentionally. Go's default
`-pgo=auto` mode uses that profile when building the benchmark command and its
dependencies. Refresh it after material hot-path changes with:

```sh
make pgo-refresh
```

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

Normal encodes and decodes should not pay for oracle diagnostics. Keep trace
hooks build-tagged or test-only, and keep optional measurements behind explicit
caller-owned state.

Keep hot-path changes allocation-aware. The encoder is designed so steady-state
`EncodeInto` can reuse caller and encoder-owned buffers instead of allocating per
frame.

## License

BSD-3-Clause. See `LICENSE`, `NOTICE`, `LICENSE.libvpx`, `PATENTS.libvpx`, and
`UPSTREAM.md`.
