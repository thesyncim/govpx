package libgopx

import "errors"

var (
	ErrInvalidData        = errors.New("libgopx: invalid VP8 data")
	ErrUnsupportedFeature = errors.New("libgopx: unsupported VP8 feature")
	ErrNeedKeyFrame       = errors.New("libgopx: need VP8 keyframe")
	ErrFrameNotReady      = errors.New("libgopx: frame not ready")
	ErrBufferTooSmall     = errors.New("libgopx: output buffer too small")

	ErrInvalidConfig    = errors.New("libgopx: invalid config")
	ErrInvalidBitrate   = errors.New("libgopx: invalid bitrate")
	ErrInvalidQuantizer = errors.New("libgopx: invalid quantizer")
	ErrClosed           = errors.New("libgopx: codec is closed")
)
