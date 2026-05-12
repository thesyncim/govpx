// Package govpx is a pure-Go VP8 and VP9 encoder and decoder.
//
// The package scope is the libvpx codec surface only: no AV1, no WebM muxer,
// no RTP packetizer, and no libvpx C API compatibility layer. It produces and
// consumes raw VP8 or VP9 frame payloads — one frame per packet — and leaves
// transport framing to the caller.
//
// govpx targets two main consumers: low-latency realtime senders (WebRTC,
// SFU edges, screen capture) and offline encoders that want a pure-Go
// dependency. Behavior is validated against a pinned libvpx v1.16.0 oracle;
// see [UpstreamLibvpxVersion]. The parity bar is 100% byte parity with
// libvpx on the supported configurations: bit-identical encoded packets and
// bit-identical decoded pixels.
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
// return [ErrFrameNotReady]; call [VP8Encoder.FlushInto] at end of stream
// to drain queued frames.
//
// # Errors and zero values
//
// Sentinel errors live in this package: [ErrInvalidData], [ErrNeedKeyFrame],
// [ErrFrameNotReady], [ErrBufferTooSmall], [ErrFrameRejected],
// [ErrInvalidConfig], [ErrInvalidBitrate], [ErrInvalidQuantizer], and
// [ErrClosed]. Compare with errors.Is. [ErrUnsupportedFeature] is
// reserved for future use and is not currently returned by any path.
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
// libvpx parity scope.
package govpx
