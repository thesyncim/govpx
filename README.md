# govpx — VP8 & VP9 in pure Go

[![CI](https://github.com/thesyncim/govpx/actions/workflows/ci.yml/badge.svg)](https://github.com/thesyncim/govpx/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thesyncim/govpx.svg)](https://pkg.go.dev/github.com/thesyncim/govpx)
[![Go Report Card](https://goreportcard.com/badge/github.com/thesyncim/govpx)](https://goreportcard.com/report/github.com/thesyncim/govpx)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

**A production-grade VP8 and VP9 Profile 0 encoder + decoder written entirely
in Go — validated byte-for-byte against libvpx, with no cgo and no runtime
dependencies.**

govpx is for Go programs that need real VP8/VP9 support — WebRTC senders,
SFUs, media servers, transcoders, recorders — without linking C. It produces
and consumes raw VP8 frame payloads and raw VP9 Profile 0 packets for
RTP/WebRTC-compatible transport, and ships the full realtime toolbox libvpx
users expect: CBR rate control, frame dropping, temporal denoising, temporal
and spatial SVC, ROI/active maps, two-pass encoding, and multithreaded
encode/decode.

## Why govpx

- **Correct, provably.** Development is oracle-driven against pinned libvpx
  v1.16.0: VP8 encoder output is **byte-identical** to libvpx across the
  repository's pinned parity matrices (rate control, dropping, denoising,
  segmentation, SVC, error resilience, …). The VP9 decoder passes the full
  official conformance corpus, and VP9 realtime encode tracks libvpx
  byte-exactly on the pinned production fixtures. Every hot-path change lands
  behind byte-parity gates.
- **Pure Go.** No cgo, no shared libraries, no build headaches. Cross-compile
  a static media server for any GOOS/GOARCH with a plain `go build`. A
  `-tags purego` build drops even govpx's own assembly.
- **Fast.** Hand-written NEON (arm64) and SSE (amd64) kernels, profile-guided
  optimization, allocation-free steady state, and libvpx-shaped threading:
  VP8 row multithreading, VP9 tile-column + row-based encoder MT, and
  row-based decoder MT with a threaded loop filter. See the numbers below.
- **Realtime-first.** The encoder mirrors the libvpx configuration WebRTC
  stacks actually use, and the library includes RFC 7741 / RFC 9628 RTP
  packetizers and assemblers, SDP negotiation helpers, and a stateful VP9
  WebRTC packetizer that keeps browsers' dependency tracking happy across
  dropped frames.

## Performance

720p realtime, 30 fps, 2.5 Mbps CBR, `cpu-used=8`, Apple M4 Max, Go 1.26.3,
measured 2026-07-16 against libvpx v1.16.0's own reported encode times
(identical output bytes and drop topology in every encode row):

| Workload | govpx | libvpx (C + asm) | gap |
| --- | --- | --- | --- |
| VP8 encode, 1 thread | 7.3 ms/frame | 5.4 ms/frame | 1.36× |
| VP9 encode, 1 thread | 10.3 ms/frame | 5.5 ms/frame | 1.87× |
| VP9 encode, 8 threads row-MT | 2.6 ms/frame | 1.3 ms/frame | 2.0× |
| VP8 decode | 1.8 ms/frame | 1.6 ms/frame | 1.17× |
| VP9 decode | 1.9 ms/frame | 1.4 ms/frame | 1.31× |

Quality on the same runs: VP8 PSNR/SSIM deltas are exactly 0 (byte-identical
streams); VP9 measures −0.05 dB PSNR / +0.00002 SSIM at identical output size
and identical encoded/dropped topology.

Benchmark numbers depend on hardware, content, and configuration — reproduce
them for your workload with the bundled tool (see
[Benchmarking](#benchmarking)). The speed gap is an active engineering front;
see `docs/perf-phase3-structural-plan.md` for the measured optimization
program.

## Install

Go 1.26 or newer is required. The module pins the default toolchain to Go
1.26.3 so local `go` commands use the same patch level as CI.

```sh
go get github.com/thesyncim/govpx
```

Build with `-tags purego` for a scalar build. The tag excludes govpx's
architecture-specific assembly and selects the Go fallbacks under
`internal/vp8/dsp`, `internal/vp8/encoder`, `internal/vp9/dsp`, and
`internal/vp9/encoder`.

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

Use `NewVP9Decoder` for raw VP9 Profile 0 packets. A VP9 packet may
contain a superframe index; the decoder consumes each contained Profile 0
frame in packet order and publishes the final visible output through `NextFrame`.
Valid non-Profile-0 VP9 packets return `ErrVP9NotImplemented`.

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

Input images are planar 8-bit 4:2:0 (`Image{Y,U,V,*Stride}`). VP8 output is
one raw VP8 payload per packet -- not IVF, not WebM. VP9 encoder APIs emit
Profile 0 packets and valid Profile 0 superframes only.

| Capability | Knobs |
| --- | --- |
| Rate control | `RateControlMode` (VBR / CBR / CQ / Q), one-pass + two-pass VBR, runtime bitrate and target updates, VP9 target-level constraints, frame dropping, buffer model, min/max quantizers, max intra bitrate |
| Realtime controls | Error resilience, temporal/spatial scalability signaling, keyframe forcing, runtime CPU-used / deadline, VP8 RTC external rate control, reference set/copy. RTP/WebRTC payload compatibility is covered below. |
| Quality and tools | Adaptive keyframes, lookahead, auto alt-ref, ARNR, denoise, token partitions, loop-filter sharpness, screen-content mode, static threshold, active maps, ROI maps, PSNR/SSIM tuning, VP9 lossless via `VP9EncoderOptions.Lossless` / `SetLossless`, multi-threaded VP8 row encode, VP9 tile-column and row-MT threading controls |

Lookahead and auto-alt-ref can make `EncodeInto` return `ErrFrameNotReady`
while frames are queued. Call `FlushInto` at end of stream until it
returns no more data.

## Correctness and validation

govpx treats libvpx v1.16.0 as a pinned oracle (`UPSTREAM.md` is the
authoritative scope statement). The repository gates enforce:

- **VP8 encoder byte parity** — pinned stream-SHA matrices across rate
  control, frame dropping, denoising, segmentation, cyclic refresh, temporal
  SVC, error resilience, and runtime-control sequences, all compared against
  an instrumented libvpx vpxenc oracle.
- **VP9 decoder conformance** — the official Profile 0 vector corpus decodes
  bit-exactly across thread counts {1, 2, 4, 8}, including tile and row-MT
  paths, invalid-stream rejection, and unsupported-profile handling.
- **VP9 encoder parity** — realtime (non-RD) encode paths are byte-exact on
  pinned production fixtures across thread counts; quality fixtures gate
  PSNR/SSIM against libvpx on every CI run.
- **Determinism** — multithreaded encode output is pinned byte-identical
  across thread counts on the production option grid.
- **Allocation discipline** — steady-state `EncodeInto` / `Decode` paths are
  enforced allocation-free by tests.
- **Fuzzing** — differential fuzzers run govpx against libvpx for both
  decoders and the VP8 boolean coder; corpus regressions are pinned as seeds.

Run `make ci` locally for the standard gate, `make verify-production` for the
full oracle/parity lane. See `docs/validation.md` for the complete gate map.

## API Map

| Task | API |
| --- | --- |
| Decode one packet | `Decode`, then `NextFrame` |
| Decode into caller-owned buffers | `DecodeInto`, `DecodeIntoWithPTS` |
| Configure VP9 decode recovery/postprocess | `VP9DecoderOptions.ErrorConcealment`, `VP9DecoderOptions.PostProcessFlags`, `VP9DecoderOptions.PostProcessNoiseLevel` |
| Select VP9 decoded spatial-SVC layer | `SetSVCSpatialLayer`, `ClearSVCSpatialLayer` |
| Inspect a packet header | `PeekVP8StreamInfo`, `PeekVP9StreamInfo` |
| Encode one frame | `EncodeInto`, `EncodeIntoWithFlags` (VP9 Profile 0 flag subset), `EncodeIntraOnlyFrameInto`, `EncodeShowExistingFrameInto` |
| Encode a VP9 spatial-SVC access unit | `NewVP9SpatialSVCEncoder`, `VP9SpatialSVCEncoder.EncodeIntoWithResult` |
| Signal VP9 encoded spatial layer | `VP9EncoderOptions.SpatialScalability`, `SetSpatialScalability`, `SetSpatialLayerID` |
| Validate VP9 WebRTC SDP/profile negotiation | `VP9SDPNegotiatesProfile0`, `VP9SDPOffersProfile0Receive`, `VP9SDPOffersProfile0ReceiveFrame`, `VP9SDPAnswersProfile0Send`, `VP9SDPFmtpContainsProfile0`, `ParseVP9SDPFmtp`, `VP9SDPReceiverCapabilities` |
| Validate VP8 WebRTC SDP negotiation | `VP8SDPNegotiates`, `VP8SDPOffersReceive`, `VP8SDPOffersReceiveFrame`, `VP8SDPAnswersSend`, `ParseVP8SDPFmtp`, `VP8SDPReceiverCapabilities` |
| Packetize, assemble, pack, or inspect VP8 RTP payload bodies | `VP8RTPFramePacketizationSize`, `PacketizeVP8RTPFrameInto`, `PacketizeVP8RTPFrame`, `VP8RTPFrameAssemblySize`, `AssembleVP8RTPFrameInto`, `AssembleVP8RTPFrame`, `VP8RTPPayloadDescriptor`, `ParseVP8RTPPayloadDescriptor`, `PackVP8RTPPayloadInto`, `PackVP8RTPPayload` |
| Pack VP9 superframes | `VP9SuperframeSize`, `PackVP9SuperframeInto` |
| Packetize, assemble, pack, or inspect VP9 RTP payload bodies | `VP9RTPFramePacketizationSize`, `PacketizeVP9RTPFrameInto`, `PacketizeVP9RTPFrame`, `VP9RTPFrameAssemblySize`, `AssembleVP9RTPFrameInto`, `AssembleVP9RTPFrame`, `VP9RTPPayloadDescriptor`, `ParseVP9RTPPayloadDescriptor`, `PackVP9RTPPayloadInto`, `PackVP9RTPPayload` |
| Packetize plain VP9 for long-lived WebRTC senders | `VP9WebRTCPacketizer.PacketizeWebRTCNonFlexibleInto`, `VP9WebRTCPacketizer.PacketizeWebRTCNonFlexible`, `VP9WebRTCPacketizer.PacketizeInto`, `VP9WebRTCPacketizer.Packetize` |
| Packetize VP9 spatial SVC for long-lived WebRTC senders | `VP9WebRTCPacketizer.PacketizeSpatialSVCWebRTCNonFlexibleInto`, `VP9WebRTCPacketizer.PacketizeSpatialSVCWebRTCNonFlexible`, `VP9WebRTCPacketizer.PacketizeSpatialSVCWebRTCInto`, `VP9WebRTCPacketizer.PacketizeSpatialSVCWebRTC` |
| Build one VP9 WebRTC RTP access unit only when caller owns all sender state | `VP9EncodeResult.PacketizeWebRTCRTPInto`, `VP9EncodeResult.PacketizeWebRTCRTP`, `VP9SpatialSVCEncodeResult.PacketizeWebRTCRTPInto`, `VP9SpatialSVCEncodeResult.PacketizeWebRTCRTP` |
| Drain delayed encoder output | `FlushInto` |
| Force a keyframe | `ForceKeyFrame` (VP8/VP9 sticky) or `EncodeForceKeyFrame` (VP8/VP9 one frame) |
| Runtime bitrate/FPS/size update | `SetRealtimeTarget` (VP8 and VP9 Profile 0; VP9 explicit CBR updates bitrate/FPS/size and frame-drop state) |
| Toggle frame dropping | `SetFrameDropAllowed` or `RealtimeTarget.FrameDrop` |
| Toggle VP9 CBR post-encode drops | `SetPostEncodeDrop` |
| Runtime rate-control replacement | `SetRateControl` |
| Two-pass encode | `CollectFirstPassStats`, `govpx.FinalizeFirstPassStats`, `SetTwoPassStats` |
| Reference buffer control | `SetReferenceFrame`, `CopyReferenceFrame` |
| Last decoded/encoded metadata | `LastFrameInfo`, `LastQuantizer`, `EncodeResult` |

## RTP/WebRTC Compatibility

govpx's RTP/WebRTC contract is codec-payload compatibility for VP8 and VP9
Profile 0. VP8 and VP9 expose payload-descriptor helpers plus MTU-aware
packetizers and assemblers for RFC 7741 and RFC 9628 payload bodies.
Packetizers return payload bodies plus marker bits; assemblers consume ordered
payload bodies plus marker bits. RTP headers, sequence/loss policy, jitter
buffering, SRTP, and signaling remain caller-owned. Use
`VP8SDPOffersReceiveFrame` or `VP9SDPOffersProfile0ReceiveFrame` before
sending to peers that advertise receiver caps, so an under-cap decoder is
rejected before it can look like a clean-RTP frozen video path. VP9 helpers carry
picture IDs, layer indices, flexible-mode references, and scalability
structures through packetization and assembly. Use `VP9SDPOffersProfile0Receive`
and `VP9SDPAnswersProfile0Send` around offer/answer handling before sending
VP9 Profile 0 RTP to a peer; profile or direction mismatches can otherwise
look like a frozen decoder even when RTP loss counters stay at zero. For
WebRTC VP9 senders, prefer
`VP9WebRTCPacketizer`: it wraps the result packetizers, forces 15-bit PictureID,
preserves temporal metadata, emits keyframe scalability-structure data on the
first payload, and advances PictureID after packetizing a frame/access unit or
consuming an encoder-dropped temporal slot. Encoder-dropped frames return no
RTP payloads but leave a PictureID gap, which keeps the RTP PictureID timeline
aligned with the encoder timeline.
`PacketizationSize` consumes dropped-frame slots for the same reason, since
dropped frames need no follow-up payload write. After a dropped base or middle
temporal layer, `NeedsKeyFrame` reports that the sender must force a TL0
keyframe before emitting more VP9 RTP payloads; continuing with inter frames can
leave WebRTC's VP9 dependency finder waiting for references that will never
arrive. If an application intentionally withholds a coded VP9 frame/access unit
after encoding but before successful packetization, call
`VP9WebRTCPacketizer.MarkEncodedAccessUnitUnsent`; if packetization already
succeeded, call `VP9WebRTCPacketizer.MarkAccessUnitUnsent`. Then force a
keyframe before sending more VP9 RTP. That local pacing/backpressure drop does
not show up as RTP loss, but later VP9 inter frames can otherwise reference a
PictureID the receiver never saw. With pion/webrtc, write the payloads from
`VP9WebRTCPacketizer` to a `TrackLocalStaticRTP`; do not pass govpx VP9 SVC
superframes to `TrackLocalStaticSample`, because Pion's generic VP9 payloader
cannot reconstruct govpx's temporal/spatial dependency metadata from raw VP9
bytes. The generic VP9 RTP packetizers remain available for callers that
already own their full PictureID, dependency, recovery, and descriptor policy.
They are not the production long-lived WebRTC sender path. The VP9 decoder also
exposes libvpx-style spatial-SVC
superframe filtering with `SetSVCSpatialLayer`; the VP9 encoder exposes spatial
layer signaling through `SetSpatialScalability`.

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

- Use `ForceKeyFrame()` for sticky PLI/FIR. Use `EncodeForceKeyFrame`
  on `EncodeInto` (VP8) or `EncodeIntoWithFlags` (VP9) for a one-frame request.
- VP9 `EncodeIntoWithFlags` is Profile-0-only and supports the VP9-compatible
  keyframe, visibility, reference, and entropy hints documented by
  `EncodeFlags`.
- For plain single-spatial/single-temporal VP9 over WebRTC, call
  `EncodeIntoWithResult`, then
  `VP9WebRTCPacketizer.PacketizeWebRTCNonFlexibleInto` or
  `PacketizeWebRTCNonFlexible` for the browser-oriented realtime RTP shape
  with TL0PICIDX and keyframe GOF metadata. `PacketizeInto` / `Packetize`
  remain available when a sender explicitly wants flexible-mode VP9 reference
  diffs. Both stateful modes leave a PictureID gap for CBR-dropped frames. If
  the packetizer's `NeedsKeyFrame` becomes true after a dropped frame, call
  `ForceKeyFrame` before sending another VP9 frame.
- If your sender drops or withholds a coded VP9 frame/access unit locally after
  encode but before packetization, call
  `VP9WebRTCPacketizer.MarkEncodedAccessUnitUnsent`. If packetization already
  succeeded, call `VP9WebRTCPacketizer.MarkAccessUnitUnsent`. Then force a
  keyframe before the next VP9 send; otherwise the browser can freeze with no
  RTP loss while waiting on an app-local missing reference.
- `EncodeIntraOnlyFrameInto` plus `EncodeShowExistingFrameInto` covers the VP9
  hidden intra-only refresh / show-existing packet pattern used by payload-level
  refresh flows.
- Use `SetRealtimeTarget` for bandwidth-estimation updates. The zero value
  of `RealtimeTarget.FrameDrop` leaves VP8 frame dropping unchanged, so
  bitrate-only BWE updates do not accidentally disable dropping. VP9 explicit
  CBR accepts bitrate/FPS/size, frame-drop, and public quantizer runtime
  updates.
- Use `SetTargetLevel` when a VP9 stream must fit a fixed level. Fixed levels
  adapt encoder decisions in the libvpx style; they are not constructor-time
  geometry rejection gates. Use level `1` for libvpx-style auto level
  decisions and `255` for no fixed level constraint.
- Drive caller-driven runtime resolution change through
  `SetRealtimeTarget` by setting a new `Width` / `Height` pair:
  size-dependent buffers are resized in place (capacity is reused), the
  `LAST` / `GOLDEN` / `ALTREF` references are invalidated, and the next
  encoded frame is forced to be a key frame at the new size. Mirrors
  libvpx's `vpx_codec_enc_config_set` with a new width / height. The
  VP8 `SetScalingMode` writes keyframe scale bits and forces a keyframe, but
  govpx does not run libvpx's internal source resampler or `rc_resize_*`
  watermarks. The decoder also handles key-frame resolution change; see
  `DecoderOptions.RejectResolutionChange`.

See `examples/webrtc-vp8` for a VP8 separate-module demo that streams govpx
VP8 through pion/webrtc to a browser.

## More

- `docs/api.md`: public API guide and examples.
- `docs/architecture.md`: package ownership and data flow.
- `docs/codec-status.md`: supported VP8/VP9 scope and out-of-scope features.
- `docs/validation.md`: local, CI, oracle, fuzz, allocation, and performance
  gates.
- `docs/perf-phase3-structural-plan.md`: the live speed-vs-libvpx engineering
  program with per-change measurements.
- `UPSTREAM.md`: pinned libvpx baseline and compatibility scope.

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

The table in [Performance](#performance) was produced with this tool at
`-width=1280 -height=720 -fps=30 -bitrate=2500 -mode=realtime -cpu-used=8`
(VP9 MT rows add `-threads=8 -row-mt -noise-sensitivity=0`). Reproduce on
the Go version, CPU, frame size, bitrate, deadline, thread count, and build
tags that match your workload before drawing conclusions.

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

`cmd/govpx-bench/default.pgo` is checked in intentionally so `go build`'s
default `-pgo=auto` picks it up for the benchmark command. Refresh it
after material hot-path changes. `make ci` runs `make pgo-check`, which
fails when VP8 benchmark hot-path sources changed without refreshing the
checked-in profile and source fingerprint:

```sh
make pgo-refresh
make pre-commit
```

## Repository layout

```text
.                         public govpx package
internal/vp8/common       VP8 shared state, headers, loop filter, quant tables
internal/vp8/decoder      VP8 decoder internals
internal/vp8/encoder      VP8 packet, token, transform, quant, motion helpers
internal/vp8/dsp          VP8 scalar and architecture-specific pixel kernels
internal/vp9/bitstream    VP9 boolean range coder (reader + writer)
internal/vp9/common       VP9 shared state, headers, partition tree, references
internal/vp9/decoder      VP9 decoder internals
internal/vp9/encoder      VP9 header, mode, coefficient, probability, and pack writers
internal/vp9/dsp          VP9 scalar and architecture-specific pixel kernels
internal/vp9/tables       VP9 entropy / scan / quant / probability constants
internal/coracle          pinned libvpx oracle build and comparison helpers
cmd/govpx-bench           encode/decode benchmark CLI
cmd/govpx-oracle          oracle wrapper command
cmd/parity-report         oracle parity report runner
examples/webrtc-vp8       separate-module WebRTC example
testdata                  oracle parity baselines
```

## Contributing

- Keep hot paths allocation-aware. Steady-state `EncodeInto` and `Decode`
  reuse caller- and codec-owned buffers; the test suite enforces this.
- Keep oracle diagnostics out of normal builds. Trace hooks are either
  build-tagged (`govpx_oracle_trace`) or test-only, and optional VP8 phase
  timing is compiled in only with the `govpx_phase_stats` tag.
- Run `make ci` before opening a PR. Run `make verify-production` when a
  change touches parity-sensitive code or oracle baselines.

## License

BSD-3-Clause. See `LICENSE`, `NOTICE`, `LICENSE.libvpx`, `PATENTS.libvpx`,
and `UPSTREAM.md`.
