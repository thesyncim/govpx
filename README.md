# govpx — VP8 & VP9 in pure Go

[![CI](https://github.com/thesyncim/govpx/actions/workflows/ci.yml/badge.svg)](https://github.com/thesyncim/govpx/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thesyncim/govpx.svg)](https://pkg.go.dev/github.com/thesyncim/govpx)
[![Go Report Card](https://goreportcard.com/badge/github.com/thesyncim/govpx)](https://goreportcard.com/report/github.com/thesyncim/govpx)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

**A memory-safe VP8 and VP9 (Profiles 0–3) encoder + decoder written in pure Go —
zero `cgo`, zero `unsafe`, zero hand-written assembly, and competitive with
`libvpx`.**

govpx targets Go applications that need production VP8/VP9 support — WebRTC
senders, SFUs, media servers, transcoders, and recorders — without linking C.
It produces and consumes raw VP8 frame payloads and raw VP9 packets across all
four VP9 profiles (8-bit, 10-bit, and 12-bit; 4:2:0, 4:2:2, 4:4:0, 4:4:4, and
sRGB) with CBR/VBR/CQ/two-pass rate control, frame dropping, temporal
denoising, temporal and spatial SVC, ROI/active maps, and multithreaded
encode/decode.

## Why govpx

- **Memory-safe pure Go.** Zero `cgo`, zero `unsafe`, and zero `.s` assembly
  files. Cross-compile a static binary with a plain `go build`.
- **Fast.** Pure-Go `simd/archsimd` kernels (arm64 NEON / amd64 AVX2),
  profile-guided optimization, allocation-free steady state (`0.00 allocs/frame`
  on decode, GC-invisible `noscan` buffers), VP8 row multithreading, and VP9
  tile-column + row-MT encode/decode.
- **Oracle-validated.** Verified against pinned `libvpx v1.16.0`: VP8 encoder
  output is byte-identical on pinned non-HD parity matrices, the VP9 decoder
  passes the official Profile 0 conformance corpus across thread counts and
  matches `libvpx-vp9` byte-for-byte on Profiles 1–3, and encode workloads are
  quality-gated (`-quality-gate`) against `libvpx`.
- **WebRTC-ready.** Includes RFC 7741 (VP8) and RFC 9628 (VP9) RTP payload
  packetizers/assemblers, SDP profile/cap helpers, and `VP9WebRTCPacketizer`
  for stateful PictureID and dependency tracking across dropped frames.

## Performance

Measured on Apple M4 Max with Go 1.27.4 (`GOEXPERIMENT=simd`, checked-in PGO)
against pinned `libvpx v1.16.0` (C + NEON assembly) using `cmd/govpx-bench`
(`-content panning`, `-drop-frame=0`, pinned VP8 `-cpu-used -8`, `-runs 3`
alternating execution order, independent medians, codec-internal timers
`vpxenc-stats` / `oracle-bench` / `vpxdec-summary`, and 8×8 windowed SSIM).
Ratio is `govpx / libvpx` core time (`< 1.00×` = govpx faster):

| Workload | govpx | libvpx (C + asm) | Ratio (core) | Notes |
| --- | ---: | ---: | ---: | --- |
| **Standard suite geomean** (`-suite standard -suite-runs 3`) | — | — | **0.84×** | 1.20× encode fps, 0/0 drops |
| — VP8 realtime 720p, 1T (`rt-720p-2m-60f`) | **7.19 ms** (139.0 fps) | 6.88 ms (145.4 fps) | **1.05×** | PSNR −0.13 dB, SSIM −0.0036 |
| — VP8 good 1080p, 1T (`good-1080p-8m-60f`) | **11.50 ms** (87.0 fps) | 17.19 ms (58.2 fps) | **0.67×** | 0.58× rate, PSNR +1.91 dB |
| VP8 realtime 720p, 4T (`-cpu-used -8`) | **1.77 ms** (564.6 fps) | 2.06 ms (485.2 fps) | **0.86×** | PSNR +0.24 dB, 0.07 allocs/frame |
| VP8 realtime 720p, 1T (`-cpu-used -8`, denoise off) | **5.20 ms** (192.4 fps) | 5.39 ms (185.5 fps) | **0.96×** | PSNR +1.44 dB, 0.91× bytes |
| VP9 good 720p, 1T (`-cpu-used 4`) | **22.15 ms** (45.1 fps) | 53.17 ms (18.8 fps) | **0.42×** | PSNR +0.55 dB, SSIM +0.0003 |
| VP9 realtime 720p, 1T (`-cpu-used 8`) | **8.61 ms** (116.1 fps) | 8.95 ms (111.7 fps) | **0.96×** | PSNR −4.32 dB vs libvpx on panning |
| VP9 realtime 720p, 4T `-row-mt` (`-cpu-used 8`, 4 Mbps) | **4.67 ms** (214.3 fps) | 4.51 ms (221.5 fps) | **1.03×** | PSNR −3.93 dB, zero-spin workers |
| VP8 decode 720p, 1T (120 frames) | **1.20 ms** (832.7 fps) | 0.96 ms (1,046.2 fps) | **1.25×** | p50 1.19 ms, 0.00 allocs/frame |
| VP9 decode 720p, 1T (120 frames) | **0.82 ms** (1,214.6 fps) | 0.65 ms (1,533.4 fps) | **1.26×** | p50 0.76 ms, 0.00 allocs/frame |

Performance depends on CPU, content, resolution, bitrate, and mode — reproduce
on synthetic patterns or your own `.y4m` / `.ivf` files with `cmd/govpx-bench`
(see [Benchmarking](#benchmarking)).

## Install

Requires **Go 1.27.4+**.

```sh
go get github.com/thesyncim/govpx
```

Enable Go's native SIMD kernels with `GOEXPERIMENT=simd`:

```sh
GOEXPERIMENT=simd go build ./...
```

`make` targets and `scripts/go.sh` set `GOEXPERIMENT=simd` automatically. On
amd64, AVX2 kernels are guarded by runtime CPU detection; arm64 uses mandatory
ASIMD. Plain `go build` or `-tags purego` selects scalar Go fallbacks.

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

- `NextFrame` returns decoder-owned `Image` storage valid until the next
  `Decode`, `Reset`, or `Close` (`Y`/`U`/`V` for 8-bit Profiles 0–1;
  `Y16`/`U16`/`V16` with `BitDepth` 10 or 12 for Profiles 2–3). Use
  `DecodeInto` / `DecodeIntoWithPTS` for caller-owned buffers.
- Use `NewVP9Decoder` for raw VP9 Profiles 0–3 packets (including superframes).
- Supported controls: threading, error concealment, postprocess flags, max
  dimensions, resolution-change rejection, decryptor callbacks, and reference
  buffer set/copy.

## Encode

```go
enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
    Width:             1280,
    Height:            720,
    FPS:               30,
    RateControlMode:   govpx.RateControlCBR,
    TargetBitrateKbps: 2500,
    Deadline:          govpx.DeadlineRealtime,
    CpuUsed:           -8,
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
    writePacket(res.Data) // aliases packet; copy if it must outlive packet.
}
```

Inputs are planar 8-bit `Image{Y,U,V,*Stride}` (VP8 and VP9 Profiles 0–1) or
10/12-bit `Image{Y16,U16,V16,*Stride,BitDepth}` via `EncodeImage` /
`EncodeImageInto` (VP9 Profiles 2–3). Outputs are raw VP8 payloads or VP9
packets/superframes (not IVF or WebM). When lookahead or auto-alt-ref queues
frames, `EncodeInto` returns `ErrFrameNotReady`; drain with `FlushInto` at end
of stream.

| Area | Controls |
| --- | --- |
| Rate control | `RateControlMode` (VBR / CBR / CQ / Q), one-pass and two-pass VBR, runtime `SetRealtimeTarget` / `SetRateControl`, VP9 target levels, frame dropping, buffer model, min/max quantizers, max intra bitrate |
| Realtime & SVC | Error resilience, temporal/spatial scalability, `ForceKeyFrame` / `EncodeForceKeyFrame`, runtime `CpuUsed` / `Deadline`, external rate control, reference set/copy |
| Quality & threading | Adaptive keyframes, lookahead, auto-alt-ref, ARNR, denoise, token partitions, screen-content mode, static threshold, active/ROI maps, PSNR/SSIM tuning, VP9 lossless, VP8 row-MT, VP9 tile-column + row-MT |

## WebRTC & RTP

govpx provides RFC 7741 (VP8) and RFC 9628 (VP9) payload descriptors, MTU-aware
packetizers, ordered frame assemblers, and SDP profile/capability helpers. RTP
headers, sequence/loss policy, jitter buffering, SRTP, and signaling remain
caller-owned.

- **SDP negotiation**: check `VP8SDPOffersReceiveFrame` /
  `VP9SDPOffersProfile0ReceiveFrame` and `VP8SDPAnswersSend` /
  `VP9SDPAnswersProfile0Send` before sending to peers with `max-fr` / `max-fs`
  or profile constraints.
- **VP9 WebRTC senders**: prefer `VP9WebRTCPacketizer` (`PacketizeWebRTCNonFlexibleInto`
  or `PacketizeSpatialSVCWebRTCNonFlexibleInto`) and write payloads directly to
  `pion/webrtc`'s `TrackLocalStaticRTP`. It manages 15-bit PictureIDs,
  temporal/spatial metadata, keyframe scalability structures, and PictureID
  gaps for dropped frames.
- **Recovery**: when `VP9WebRTCPacketizer.NeedsKeyFrame` becomes true (or if
  you locally withhold a coded/packetized access unit via
  `MarkEncodedAccessUnitUnsent` / `MarkAccessUnitUnsent`), call `ForceKeyFrame`
  before sending the next VP9 frame.
- **Runtime resolution change**: update `Width` / `Height` via
  `SetRealtimeTarget` to resize buffers in place and force a keyframe.

See [examples/webrtc-vp8](examples/webrtc-vp8) and
[examples/webrtc-vp9](examples/webrtc-vp9) for complete Pion WebRTC demos.

## Benchmarking

```sh
scripts/go.sh run ./cmd/govpx-bench -suite standard -suite-runs 3 -quality-gate
scripts/go.sh run ./cmd/govpx-bench -codec vp9 -mode realtime -content panning -width 1280 -height 720 -frames 60 -bitrate 4000 -cpu-used 8 -threads 4 -row-mt -drop-frame=0 -runs 3
scripts/go.sh run ./cmd/govpx-bench -decode -codec vp9 -content panning -width 1280 -height 720 -frames 120 -bitrate 2500 -threads 1 -runs 3
scripts/go.sh run ./cmd/govpx-bench -input clip.y4m -mode realtime -bitrate 2500 -cpu-used -8 -drop-frame=0 -runs 3
```

By default, `govpx-bench` auto-locates `internal/coracle/build/vpxenc` or
`vpxenc` on `PATH` (pass `-auto-libvpx=false` for govpx-only runs, or
`-build-libvpx=true` to build the pinned libvpx tools). Pass `-content`
(`panning`, `checker`, or `ramp`) for synthetic sources or `-input` for a real
`.y4m` (encode) or `.ivf` (decode) file, `-runs N` for alternating-order
median repeats, and `-drop-frame=0` for equal-work comparisons. Optional
VMAF/PSNR/SSIM plotting is available via `-plot` when `ffmpeg` includes
`libvpx` and `libvmaf`.

`cmd/govpx-bench/default.pgo` is checked in for `-pgo=auto`. Refresh it after
hot-path edits:

```sh
make pgo-refresh && make pre-commit
```

## Documentation

- [docs/api.md](docs/api.md) — public API guide and runtime controls
- [docs/architecture.md](docs/architecture.md) — package ownership and data flow
- [docs/codec-status.md](docs/codec-status.md) — supported VP8/VP9 scope
- [docs/validation.md](docs/validation.md) — local, CI, oracle, fuzz, and allocation gates
- [UPSTREAM.md](UPSTREAM.md) — pinned `libvpx v1.16.0` baseline and compatibility scope

## Repository layout

```text
.                         public govpx API, encoders, decoders, RTP/SDP facade
internal/pixel            pure-Go simd/archsimd pixel, transform, quant, and filter kernels
internal/vp8/*            VP8 common, boolcoder, decoder, encoder, dsp, rtp, scale, tables
internal/vp9/*            VP9 bitstream, common, decoder, encoder, dsp, rtp, tables
internal/vpx/*            codec-neutral arith, buffers, conformance, errors, geometry, ivf, ratecontrol, rtp
internal/coracle          pinned libvpx oracle build scripts and trace comparison helpers
cmd/govpx-bench           encode/decode benchmark and quality-gate CLI
cmd/govpx-oracle          oracle wrapper CLI
cmd/parity-report         oracle parity report runner
examples/webrtc-vp8       Pion WebRTC VP8 simulcast demo (separate module)
examples/webrtc-vp9       Pion WebRTC VP9 spatial-SVC demo (separate module)
testdata                  oracle parity baselines and fuzz seeds
```

## License

BSD-3-Clause. See [LICENSE](LICENSE), [NOTICE](NOTICE),
[LICENSE.libvpx](LICENSE.libvpx), [PATENTS.libvpx](PATENTS.libvpx), and
[UPSTREAM.md](UPSTREAM.md).
