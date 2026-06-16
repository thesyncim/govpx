# API

The public import path is:

```go
import "github.com/thesyncim/govpx"
```

The root package exposes constructors, options, errors, image buffers, RTP
helpers, and codec handles. Codec implementation details live under `internal/`
or are being moved there; callers should only depend on the root package.

## Decode

Use `NewVP8Decoder` for raw VP8 frame payloads and `NewVP9Decoder` for raw VP9
Profile 0 packets. `Decode` publishes decoder-owned output through
`NextFrame`; `DecodeInto` writes into caller-owned `Image` buffers.

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
if ok {
    use(frame)
}
```

`NextFrame` returns storage owned by the decoder. It remains valid until the
next `Decode`, `Reset`, or `Close` call. Use `DecodeInto` or
`DecodeIntoWithPTS` when the caller owns the destination buffers.

VP8 and VP9 decoder options include threading, error concealment, postprocess
flags, maximum dimensions, resolution-change policy, and decryptor callbacks.
VP9 also exposes external frame buffers, byte alignment, tile/row controls, and
spatial-SVC layer selection.

## Encode

Use `NewVP8Encoder` or `NewVP9Encoder` with explicit dimensions, frame rate or
timebase, rate control, and deadline. `EncodeInto` writes into a caller-owned
byte buffer and returns a result whose `Data` slice aliases that buffer.

```go
enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
    Width:             1280,
    Height:            720,
    FPS:               30,
    RateControlMode:   govpx.RateControlCBR,
    TargetBitrateKbps: 2500,
    MinQuantizer:      2,
    MaxQuantizer:      56,
    Deadline:          govpx.DeadlineRealtime,
    CpuUsed:           -6,
})
if err != nil {
    return err
}
defer enc.Close()

packet := make([]byte, 256*1024)
res, err := enc.EncodeInto(packet, src, pts, duration, 0)
if err != nil {
    return err
}
if !res.Dropped {
    send(res.Data)
}
```

Lookahead, auto-alt-ref, and two-pass encoders may accept a frame without
emitting a packet. In that case `EncodeInto` returns `ErrFrameNotReady`. At end
of stream, call `FlushInto` until no more output is available.

VP9 uses `VP9EncoderOptions` and the same caller-owned-buffer contract. VP9
encoder APIs are limited to Profile 0: 8-bit 4:2:0 packets, Profile 0
superframes, show-existing frames, intra-only packets, spatial-layer signaling,
tile settings, row-MT settings, color/render metadata, segmentation, lossless
mode, AQ modes, and first-pass/two-pass stats.

VP9 rate control reports the requested `TargetBitrateKbps`, but its internal
frame budgets and CBR buffers mirror libvpx's raw-target-rate cap for Profile 0
sources and fixed target-level bitrate caps. Runtime `SetRateControlBuffer`
accepts literal zero values: max and optimal buffers fall back to
`target_bandwidth/8`, while the initial buffer can be zero.

## Runtime Controls

Common runtime controls include:

- `SetRealtimeTarget` for bitrate, FPS, frame-drop, and caller-driven size
  changes.
- `SetRateControl` for replacing the public rate-control configuration.
- `SetFrameDropAllowed` for runtime frame-drop policy.
- `SetPostEncodeDrop` for VP9 CBR post-encode drops when a packed inter frame
  would underflow the CBR buffer.
- `SetTargetLevel` for VP9 level-constrained encode decisions. Fixed levels
  adapt internal bitrate, overshoot, quantizer, GF interval, and tile-column
  limits instead of rejecting otherwise valid source geometry; level `1`
  selects libvpx-style auto level decisions, and `255` disables fixed level
  constraints.
- `ForceKeyFrame` for sticky PLI/FIR-style requests.
- `SetReferenceFrame` and `CopyReferenceFrame` for LAST/GOLDEN/ALTREF buffers.

VP8 `SetScalingMode` writes VP8 keyframe scale bits and forces a keyframe. It
does not run libvpx's internal source resampler; callers provide frames at the
coded size they want govpx to encode.

## RTP And Superframes

VP8 and VP9 RTP helpers operate on payload bodies, not RTP sessions. The
package provides payload descriptor parsing/packing, MTU-aware packetization,
ordered frame assembly, marker-bit results, and VP9 scalability-structure
support. RTP headers, sequence/loss policy, jitter buffering, SRTP, SDP, and
signaling stay caller-owned.

For VP9 WebRTC senders, use `VP9WebRTCPacketizer` around the WebRTC-specific
encoder-result packetizers. It sets 15-bit PictureID, preserves temporal-layer
metadata, emits keyframe scalability-structure data with WebRTC GOF
dependencies, and advances PictureID after successful packetization or after
consuming an encoder-dropped temporal slot. If VP9 CBR intentionally drops a
frame, `VP9WebRTCPacketizer.Packetize` returns `sent=false` with no RTP
payloads and leaves a PictureID gap so the receiver's non-flexible VP9 GOF
index remains aligned. `PacketizationSize` has the same dropped-frame consume
behavior because dropped frames need no follow-up payload write. The lower-level
VP9 RTP packetizers are still available when a caller deliberately owns
descriptor policy.

VP9 superframe helpers are Profile 0 only:

- `VP9SuperframeSize`
- `PackVP9SuperframeInto`

## Errors

Sentinel errors are exported from the root package and are safe to compare with
`errors.Is`: `ErrInvalidData`, `ErrNeedKeyFrame`, `ErrFrameNotReady`,
`ErrBufferTooSmall`, `ErrFrameRejected`, `ErrInvalidConfig`,
`ErrInvalidBitrate`, `ErrInvalidQuantizer`, `ErrInvalidVP9Data`,
`ErrVP9NotImplemented`, and `ErrClosed`.

The zero value of `DecoderOptions` is valid. Encoder options require at least a
positive width, height, frame rate or timebase, and target bitrate where the
selected rate-control mode needs one.
