// Package govpx is a pure-Go VP8 and VP9 profile 0 codec package.
//
// It produces and consumes raw VP8 frame payloads and raw VP9 Profile 0
// packets; VP9 packets may be superframes. RTP/WebRTC payload compatibility is
// in scope for both VP8 and VP9. VP8 and VP9 RTP payload descriptor helpers and
// MTU-aware payload packetizers and assemblers are provided; RTP headers, SRTP,
// SDP, signaling, sequence/loss handling, and transport policy stay
// caller-owned.
//
// VP9 scope is full profile 0 support only: 8-bit 4:2:0 raw packets and valid
// superframes. VP9 profiles 1-3, alpha, high-bit-depth/deep-color, and
// non-4:2:0 chroma variants are out of scope. Valid non-profile-0 VP9 packets
// return [ErrVP9NotImplemented].
//
// govpx targets two main consumers: low-latency realtime senders (WebRTC,
// SFU edges, screen capture) and offline encoders that want a pure-Go
// dependency. Behavior is validated against a pinned libvpx v1.16.0 oracle;
// see [UpstreamLibvpxVersion]. VP9 oracle coverage is profile 0 only.
//
// # Decoding
//
// Construct a [VP8Decoder] once and feed it raw VP8 packets:
//
//	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
//	if err != nil {
//	    return err
//	}
//	defer dec.Close()
//
//	if err := dec.Decode(packet); err != nil {
//	    return err
//	}
//	if frame, ok := dec.NextFrame(); ok {
//	    // frame is I420; planes alias decoder-owned storage until the next
//	    // Decode, Reset, or Close call.
//	    _ = frame
//	}
//
// Use [VP8Decoder.DecodeInto] or [VP8Decoder.DecodeIntoWithPTS] when the
// caller owns the output buffers and wants them filled in place.
//
// # Encoding
//
// Construct a [VP8Encoder] once and reuse a single output buffer across
// frames. EncodeResult.Data aliases the caller-provided destination buffer.
//
//	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
//	    Width:             1280,
//	    Height:            720,
//	    FPS:               30,
//	    RateControlMode:   govpx.RateControlCBR,
//	    TargetBitrateKbps: 2500,
//	    Deadline:          govpx.DeadlineRealtime,
//	})
//	if err != nil {
//	    return err
//	}
//	defer enc.Close()
//
//	packet := make([]byte, 256*1024)
//	res, err := enc.EncodeInto(packet, img, pts, duration, 0)
//	if err != nil {
//	    return err
//	}
//	if !res.Dropped {
//	    send(res.Data) // copy if res.Data must outlive packet.
//	}
//
// Lookahead, auto-alt-ref, and two-pass encoders may withhold output and
// return [ErrFrameNotReady]; call [VP8Encoder.FlushInto] or
// [VP9Encoder.FlushIntoWithResult] at end of stream to drain queued frames.
//
// # Errors and zero values
//
// Sentinel errors live in this package: [ErrInvalidData], [ErrNeedKeyFrame],
// [ErrFrameNotReady], [ErrBufferTooSmall], [ErrFrameRejected],
// [ErrInvalidConfig], [ErrInvalidBitrate], [ErrInvalidQuantizer], and
// [ErrClosed]. Compare with errors.Is.
//
// The zero value of [EncoderOptions] is not a valid configuration: Width,
// Height, FPS (or TimebaseNum/Den), and TargetBitrateKbps must be set. The
// zero value of [DecoderOptions] is valid and produces a single-threaded
// decoder with no postprocessing.
//
// # Build tags
//
//   - The default build links the native architecture's pixel kernels.
//   - The "purego" build tag forces scalar Go fallbacks across
//     internal/vp8/dsp, internal/vp8/encoder, internal/vp9/dsp, and
//     internal/vp9/encoder.
//   - The "govpx_oracle_trace" build tag links the encoder oracle trace
//     hooks; normal builds leave them out.
//
// See the repository README for build, benchmarking, and validation
// commands, the WebRTC demo under examples/webrtc-vp8, and UPSTREAM.md for
// the libvpx baseline and VP9 scope.
package govpx
