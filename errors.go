package govpx

import "errors"

var (
	// ErrInvalidData reports malformed or unsupported VP8 bitstream data.
	ErrInvalidData = errors.New("govpx: invalid VP8 data")
	// ErrUnsupportedFeature reports a valid VP8 feature that govpx does not
	// implement yet.
	ErrUnsupportedFeature = errors.New("govpx: unsupported VP8 feature")
	// ErrNeedKeyFrame reports an inter frame before reference state has been
	// initialized by a key frame.
	ErrNeedKeyFrame = errors.New("govpx: need VP8 keyframe")
	// ErrFrameNotReady reports that a lookahead encoder accepted input but has
	// not emitted an output packet yet.
	ErrFrameNotReady = errors.New("govpx: frame not ready")
	// ErrBufferTooSmall reports that the caller-provided encoded output buffer
	// cannot hold the next VP8 frame.
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
)
