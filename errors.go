package govpx

import "errors"

var (
	// ErrInvalidData reports malformed or unsupported VP8 bitstream or RTP
	// payload data.
	ErrInvalidData = errors.New("govpx: invalid VP8 data")
	// ErrUnsupportedFeature is reserved for future use to report a valid
	// VP8 feature that govpx does not implement. No public API path
	// currently returns it; it is retained as a stable sentinel so future
	// support can be added without breaking callers that compare with
	// errors.Is.
	ErrUnsupportedFeature = errors.New("govpx: unsupported VP8 feature")
	// ErrNeedKeyFrame reports an inter frame before reference state has been
	// initialized by a key frame.
	ErrNeedKeyFrame = errors.New("govpx: need VP8 keyframe")
	// ErrFrameNotReady reports that a lookahead encoder accepted input but has
	// not emitted an output packet yet.
	ErrFrameNotReady = errors.New("govpx: frame not ready")
	// ErrBufferTooSmall reports that a caller-provided output buffer cannot
	// hold the requested encoded packet or payload wrapper.
	ErrBufferTooSmall = errors.New("govpx: output buffer too small")
	// ErrFrameRejected reports a frame rejected by configured decoder limits.
	ErrFrameRejected = errors.New("govpx: VP8 frame rejected by decoder options")

	// ErrInvalidConfig reports an invalid option, runtime control, image shape,
	// or reference selector.
	ErrInvalidConfig = errors.New("govpx: invalid config")
	// ErrInvalidBitrate reports a bitrate or buffer-model value outside the
	// supported encoder range.
	ErrInvalidBitrate = errors.New("govpx: invalid bitrate")
	// ErrInvalidQuantizer reports a public quantizer outside [0, 63] or outside
	// the active min/max range.
	ErrInvalidQuantizer = errors.New("govpx: invalid quantizer")
	// ErrClosed reports use of a nil or closed encoder/decoder.
	ErrClosed = errors.New("govpx: codec is closed")

	// ErrInvalidVP9Data reports malformed or unsupported VP9
	// bitstream or RTP payload data.
	// The internal VP9 stack returns a more specific error
	// (vp9/decoder.ErrInvalidHeader); the public surface flattens
	// to this sentinel so callers don't take a dependency on the
	// internal types.
	ErrInvalidVP9Data = errors.New("govpx: invalid VP9 data")
)
