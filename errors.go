package govpx

import "errors"

var (
	ErrInvalidData        = errors.New("govpx: invalid VP8 data")
	ErrUnsupportedFeature = errors.New("govpx: unsupported VP8 feature")
	ErrNeedKeyFrame       = errors.New("govpx: need VP8 keyframe")
	ErrFrameNotReady      = errors.New("govpx: frame not ready")
	ErrBufferTooSmall     = errors.New("govpx: output buffer too small")

	ErrInvalidConfig    = errors.New("govpx: invalid config")
	ErrInvalidBitrate   = errors.New("govpx: invalid bitrate")
	ErrInvalidQuantizer = errors.New("govpx: invalid quantizer")
	ErrClosed           = errors.New("govpx: codec is closed")
)
