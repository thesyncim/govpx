# API

The only public import path is:

```go
import "github.com/thesyncim/govpx"
```

The root package exposes constructors, options, errors, `Image`, RTP/SDP
helpers, and codec handles. Implementation packages live under `internal/`.

## Decode

Use `NewVP8Decoder` for raw VP8 payloads and `NewVP9Decoder` for raw VP9
Profile 0 packets (including superframes). `Decode` publishes decoder-owned
output through `NextFrame`; `DecodeInto` / `DecodeIntoWithPTS` write into
caller-owned `Image` buffers.

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
    use(frame) // valid until the next Decode, Reset, or Close
}
```

Decoder options cover threading, error concealment, postprocess flags, max
dimensions, resolution-change rejection, and decryptor callbacks. VP9 also
exposes external frame buffers, byte alignment, tile/row-MT controls, and
spatial-SVC layer selection (`SetSVCSpatialLayer`).

## Encode

Use `NewVP8Encoder` or `NewVP9Encoder` with explicit dimensions, frame rate or
timebase, rate control, and deadline. `EncodeInto` writes into a caller-owned
byte slice and returns a result aliasing that buffer.

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
    CpuUsed:           -8,
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

Lookahead, auto-alt-ref, and two-pass encoders may queue frames and return
`ErrFrameNotReady`; drain remaining output with `FlushInto` at end of stream.

VP9 encoder APIs target Profile 0 (8-bit 4:2:0) and support superframes,
show-existing frames, intra-only frames, spatial-SVC signaling, tiles, row-MT,
color/render metadata, segmentation, lossless mode, AQ modes, and two-pass
stats.

## Runtime Controls

- `SetRealtimeTarget`: runtime bitrate, FPS, frame-drop, and in-place
  resolution updates (invalidates references and forces a keyframe).
- `SetRateControl` / `SetRateControlBuffer`: replace rate-control config or
  buffer parameters at runtime.
- `SetFrameDropAllowed` / `SetPostEncodeDrop`: toggle frame dropping.
- `SetTargetLevel`: VP9 level-constrained encode adaptation (`1` = auto,
  `255` = disabled).
- `ForceKeyFrame`: sticky PLI/FIR keyframe request (`EncodeForceKeyFrame` for
  a single frame).
- `SetReferenceFrame` / `CopyReferenceFrame`: direct `LAST`/`GOLDEN`/`ALTREF`
  buffer control.
- `SetScalingMode` (VP8): writes keyframe scale bits and forces a keyframe;
  callers supply frames at the coded size (no internal resampler).

## RTP, SDP, and Superframes

RTP helpers operate on RFC 7741 (VP8) and RFC 9628 (VP9) payload bodies. RTP
headers, sequence/loss policy, jitter buffering, SRTP, and signaling remain
caller-owned.

- **Stateful VP9 WebRTC senders**: use `VP9WebRTCPacketizer`
  (`PacketizeWebRTCNonFlexibleInto`, `PacketizeSpatialSVCWebRTCNonFlexibleInto`,
  or `PacketizeInto` for flexible mode). It manages 15-bit PictureIDs,
  temporal/spatial metadata, keyframe scalability structures, and PictureID
  gaps for dropped frames.
- **Recovery**: when `NeedsKeyFrame` is true (after a base/middle temporal drop
  or after calling `MarkEncodedAccessUnitUnsent` / `MarkAccessUnitUnsent` for
  locally withheld frames), call `ForceKeyFrame` before sending the next VP9
  frame.
- **SDP checks**: validate receiver capabilities and Profile 0 negotiation with
  `VP8SDPOffersReceiveFrame`, `VP8SDPAnswersSend`,
  `VP9SDPOffersProfile0ReceiveFrame`, and `VP9SDPAnswersProfile0Send`.
- **VP9 superframes**: `VP9SuperframeSize` and `PackVP9SuperframeInto` (Profile 0).

## Errors

Exported sentinel errors (safe with `errors.Is`):
`ErrInvalidData`, `ErrNeedKeyFrame`, `ErrFrameNotReady`, `ErrBufferTooSmall`,
`ErrFrameRejected`, `ErrInvalidConfig`, `ErrInvalidBitrate`,
`ErrInvalidQuantizer`, `ErrInvalidVP9Data`, `ErrVP9NotImplemented`, `ErrClosed`.

Zero-valued `DecoderOptions` is valid. Encoder options require positive
dimensions, frame rate or timebase, and a target bitrate when required by the
rate-control mode.
