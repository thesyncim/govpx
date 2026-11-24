// Package errors holds shared sentinel errors for internal VPx packages.
package errors

import "errors"

var (
	ErrInvalidData       = errors.New("govpx: invalid VP8 data")
	ErrNeedKeyFrame      = errors.New("govpx: need VP8 keyframe")
	ErrFrameNotReady     = errors.New("govpx: frame not ready")
	ErrBufferTooSmall    = errors.New("govpx: output buffer too small")
	ErrFrameRejected     = errors.New("govpx: VP8 frame rejected by decoder options")
	ErrInvalidConfig     = errors.New("govpx: invalid config")
	ErrInvalidBitrate    = errors.New("govpx: invalid bitrate")
	ErrInvalidQuantizer  = errors.New("govpx: invalid quantizer")
	ErrClosed            = errors.New("govpx: codec is closed")
	ErrInvalidVP9Data    = errors.New("govpx: invalid VP9 data")
	ErrVP9NotImplemented = errors.New("govpx: VP9 packet outside implemented profile 0 scope")
)
