package govpx

import vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"

var (
	// ErrInvalidData reports malformed or unsupported VP8 bitstream or RTP
	// payload data.
	ErrInvalidData = vpxerrors.ErrInvalidData
	// ErrNeedKeyFrame reports an inter frame before reference state has been
	// initialized by a key frame.
	ErrNeedKeyFrame = vpxerrors.ErrNeedKeyFrame
	// ErrFrameNotReady reports that a lookahead encoder accepted input but has
	// not emitted an output packet yet.
	ErrFrameNotReady = vpxerrors.ErrFrameNotReady
	// ErrBufferTooSmall reports that a caller-provided output buffer cannot
	// hold the requested encoded packet or payload wrapper.
	ErrBufferTooSmall = vpxerrors.ErrBufferTooSmall
	// ErrFrameRejected reports a frame rejected by configured decoder limits.
	ErrFrameRejected = vpxerrors.ErrFrameRejected

	// ErrInvalidConfig reports an invalid option, runtime control, image shape,
	// or reference selector.
	ErrInvalidConfig = vpxerrors.ErrInvalidConfig
	// ErrInvalidBitrate reports a bitrate or buffer-model value outside the
	// supported encoder range.
	ErrInvalidBitrate = vpxerrors.ErrInvalidBitrate
	// ErrInvalidQuantizer reports a public quantizer outside [0, 63] or outside
	// the active min/max range.
	ErrInvalidQuantizer = vpxerrors.ErrInvalidQuantizer
	// ErrClosed reports use of a nil or closed encoder/decoder.
	ErrClosed = vpxerrors.ErrClosed

	// ErrInvalidVP9Data reports malformed or unsupported VP9
	// bitstream or RTP payload data.
	// The internal VP9 stack returns a more specific error
	// (vp9/decoder.ErrInvalidHeader); the public surface flattens
	// to this sentinel so callers don't take a dependency on the
	// internal types.
	ErrInvalidVP9Data = vpxerrors.ErrInvalidVP9Data
)
