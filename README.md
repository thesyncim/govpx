# govpx

[![CI](https://github.com/thesyncim/govpx/actions/workflows/ci.yml/badge.svg)](https://github.com/thesyncim/govpx/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thesyncim/govpx.svg)](https://pkg.go.dev/github.com/thesyncim/govpx)

Pure-Go VP8 encoder and decoder for raw VP8 frame payloads.

govpx is for Go programs that need VP8 without cgo and without a libvpx
runtime dependency. It is intentionally VP8-only: no VP9, no AV1, no WebM
muxer, no RTP packetizer, no libvpx C API compatibility layer. Transport
framing is the caller's responsibility.

Behavior is validated against a pinned libvpx v1.16.0 oracle. The public
API uses Go types and methods; libvpx names appear only when they identify
upstream behavior, controls, or validation tooling.

## Install

Go 1.26 or newer is required. The module pins the default toolchain to Go
1.26.3 so local `go` commands use the same patch level as CI.

```sh
go get github.com/thesyncim/govpx
```

Build with `-tags purego` for a scalar build. The tag excludes govpx's
architecture-specific assembly and selects the Go fallbacks under
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
if frame, ok := dec.NextFrame(); ok {
    _ = frame // I420; planes alias decoder-owned storage until the next call.
}
```

`NextFrame` returns decoder-owned storage that stays valid until the next
`Decode`, `Reset`, or `Close`. Use `DecodeInto` / `DecodeIntoWithPTS` when
the caller owns the destination buffers.

Decoder features: configurable threading, error concealment, granular
postprocess (deblock, demacroblock, MFQE, additive noise), maximum
dimensions, resolution-change rejection, frame metadata, and LAST /
GOLDEN / ALTREF reference-buffer set/copy.

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

packet := make([]byte, 256*1024)
for i, src := range frames {
    res, err := enc.EncodeInto(packet, src, uint64(i), 1, 0)
    if err != nil {
        return err
    }
    if res.Dropped {
        continue
    }
    writePacket(res.Data) // res.Data aliases packet; copy if it must outlive packet.
}
```

Input images are planar 8-bit 4:2:0 (`Image{Y,U,V,*Stride}`). Output is
one raw VP8 payload per packet — not IVF, not WebM.

| Capability | Knobs |
| --- | --- |
| Rate control | `RateControlMode` (VBR / CBR / CQ / Q), one-pass + two-pass VBR, runtime bitrate and target updates, frame dropping, buffer model, min/max quantizers, max intra bitrate |
| Realtime / WebRTC | Error resilience, temporal scalability, keyframe forcing, runtime CPU-used / deadline, VP8 RTC external rate control, reference set/copy |
| Quality and tools | Adaptive keyframes, lookahead, auto alt-ref, ARNR, denoise, token partitions, loop-filter sharpness, screen-content mode, static threshold, active maps, ROI maps, PSNR/SSIM tuning, multi-threaded row encode |

Lookahead and auto-alt-ref can make `EncodeInto` return `ErrFrameNotReady`
while frames are queued. Call `FlushInto` at end of stream until it
returns no more data.

## API Map

| Task | API |
| --- | --- |
| Decode one packet | `Decode`, then `NextFrame` |
| Decode into caller-owned buffers | `DecodeInto`, `DecodeIntoWithPTS` |
| Inspect a packet header | `PeekVP8StreamInfo` |
| Encode one frame | `EncodeInto` |
| Drain delayed encoder output | `FlushInto` |
| Force a keyframe | `ForceKeyFrame` (sticky) or the `EncodeForceKeyFrame` flag (one frame) |
| Runtime bitrate/FPS update | `SetRealtimeTarget` |
| Toggle frame dropping only | `SetFrameDropAllowed` or `RealtimeTarget.FrameDrop` |
| Runtime rate-control replacement | `SetRateControl` |
| Two-pass encode | `CollectFirstPassStats`, `govpx.FinalizeFirstPassStats`, `SetTwoPassStats` |
| Reference buffer control | `SetReferenceFrame`, `CopyReferenceFrame` |
| Last decoded/encoded metadata | `LastFrameInfo`, `LastQuantizer`, `EncodeResult` |

## WebRTC profile

For WebRTC senders, start with the same one-stream VP8/libvpx profile
used by libwebrtc: realtime CBR, no lookahead, frame dropping, adaptive
denoising, a small realtime buffer, and a capped keyframe target:

```go
enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
    Width:                  1280,
    Height:                 720,
    FPS:                    30,
    RateControlMode:        govpx.RateControlCBR,
    TargetBitrateKbps:      2500,
    MinQuantizer:           2,
    MaxQuantizer:           56,
    UndershootPct:          100,
    OvershootPct:           15,
    BufferSizeMs:           1000,
    BufferInitialSizeMs:    500,
    BufferOptimalSizeMs:    600,
    DropFrameAllowed:       true,
    DropFrameWaterMark:     30,
    MaxIntraBitratePct:     900, // max(300, 600*30/20)
    KeyFrameInterval:       3000,
    Deadline:               govpx.DeadlineRealtime,
    CpuUsed:                -6,
    NoiseSensitivity:       4,
    StaticThreshold:        1,
})
```

- Use `ForceKeyFrame()` or the `EncodeForceKeyFrame` flag on the next
  `EncodeInto` for PLI/FIR.
- Use `SetRealtimeTarget` for bandwidth-estimation updates. The zero
  value of `RealtimeTarget.FrameDrop` leaves frame dropping unchanged, so
  bitrate-only BWE updates do not accidentally disable dropping.
- Drive caller-driven runtime resolution change through
  `SetRealtimeTarget` by setting a new `Width` / `Height` pair:
  size-dependent buffers are resized in place (capacity is reused), the
  `LAST` / `GOLDEN` / `ALTREF` references are invalidated, and the next
  encoded frame is forced to be a key frame at the new size. Mirrors
  libvpx's `vpx_codec_enc_config_set` with a new width / height. The
  spatial resampler (`VP8E_SET_SCALEMODE`, `rc_resize_*`) is out of
  scope. The decoder also handles key-frame resolution change; see
  `DecoderOptions.RejectResolutionChange`.

See `examples/webrtc-vp8` for a separate-module demo that streams govpx
VP8 through pion/webrtc to a browser.

## Validation

Fast local checks:

```sh
make ci                          # fmt + tests + purego tests
go test ./... -count=1
go test -tags purego ./... -count=1
go vet ./...
```

Parity gates:

```sh
make verify-decoder-parity   # cheaper: decoder-only oracle proof
make verify-production       # full encoder + decoder oracle gate
```

`verify-production` builds the pinned libvpx tools under
`internal/coracle/build`, fetches VP8 conformance data, and runs decoder
plus encoder oracle tests. Encoder parity compares libvpx-decoded frame
checksums, key/show decisions, internal qindex, and packet-size ratchets
for the covered realtime/WebRTC settings. Use
`make verify-decoder-parity` when only decoder changes need re-checking.

Oracle trace and scoreboard code lives behind the `govpx_oracle_trace`
build tag or in `*_test.go` files, so it is not linked into normal
production builds. `UPSTREAM.md` documents the parity scope and known
deviations in detail.

## Benchmarking

```sh
go run ./cmd/govpx-bench
go run ./cmd/govpx-bench -decode -frames=120
go run ./cmd/govpx-bench -format=json
```

By default the encode benchmark compares against libvpx when
`cmd/govpx-bench` can find `internal/coracle/build/vpxenc` or `vpxenc` on
`PATH`. Pass `-libvpx-vpxenc=/path/to/vpxenc` to pin a binary or
`-auto-libvpx=false` for a govpx-only run. Use `-build-libvpx=true` to
let the bench build the pinned tools when no binary is found. Decoder
reference timing uses `govpx-vpx-oracle` only in `-decode` mode.

Plotting is optional and requires an ffmpeg binary with both the
`libvpx` encoder and the `libvmaf` filter:

```sh
go run ./cmd/govpx-bench \
    -plot benchmarks/govpx-vs-libvpx.svg \
    -width=1280 -height=720 -frames=120 -fps=30 \
    -bitrate=2500 -mode=realtime -threads=1
```

The plot path encodes the libvpx reference with `ffmpeg -c:v libvpx`,
scores govpx and libvpx with ffmpeg's `libvmaf`, `psnr`, and `ssim`
filters, and writes an SVG plus sibling CSV/JSON files.

README numbers are not performance data. Measure on the Go version, CPU,
frame size, bitrate, deadline, thread count, and build tags that match
your workload.

`cmd/govpx-bench/default.pgo` is checked in intentionally so `go build`'s
default `-pgo=auto` picks it up for the benchmark command. Refresh it
after material hot-path changes. `make ci` runs `make pgo-check`, which
fails when VP8 benchmark hot-path sources changed without refreshing the
checked-in profile and source fingerprint:

```sh
make pgo-refresh
```

## Repository layout

```text
.                         public govpx package
internal/vp8/common       VP8 shared state, headers, loop filter, quant tables
internal/vp8/decoder      decoder internals
internal/vp8/encoder      packet, token, transform, quant, motion helpers
internal/vp8/dsp          scalar and architecture-specific pixel kernels
internal/coracle          pinned libvpx oracle build and comparison helpers
cmd/govpx-bench           encode/decode benchmark CLI
cmd/govpx-oracle          oracle wrapper command
cmd/scoreboard-report     oracle scoreboard runner
examples/webrtc-vp8       separate-module WebRTC example
testdata                  oracle scoreboard baselines
```

## Contributing

- Keep hot paths allocation-aware. Steady-state `EncodeInto` and `Decode`
  reuse caller- and codec-owned buffers; the test suite enforces this.
- Keep oracle diagnostics out of normal builds. Trace hooks are either
  build-tagged (`govpx_oracle_trace`) or test-only, and optional
  measurements live behind explicit caller-owned state.
- Run `make ci` before opening a PR. Run `make verify-production` when a
  change touches parity-sensitive code or oracle baselines.

## License

BSD-3-Clause. See `LICENSE`, `NOTICE`, `LICENSE.libvpx`, `PATENTS.libvpx`,
and `UPSTREAM.md`.
