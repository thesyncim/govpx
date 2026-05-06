package gopvx

import "errors"

var (
	ErrInvalidData        = errors.New("gopvx: invalid VP8 data")
	ErrUnsupportedFeature = errors.New("gopvx: unsupported VP8 feature")
	ErrNeedKeyFrame       = errors.New("gopvx: need VP8 keyframe")
	ErrFrameNotReady      = errors.New("gopvx: frame not ready")
	ErrBufferTooSmall     = errors.New("gopvx: output buffer too small")

	ErrInvalidConfig    = errors.New("gopvx: invalid config")
	ErrInvalidBitrate   = errors.New("gopvx: invalid bitrate")
	ErrInvalidQuantizer = errors.New("gopvx: invalid quantizer")
	ErrClosed           = errors.New("gopvx: codec is closed")
)
