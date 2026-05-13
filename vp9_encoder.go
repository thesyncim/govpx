package govpx

import (
	"errors"
	"image"
)

// VP9EncoderOptions configures a VP9 encoder. Mirrors the subset of
// VP8 EncoderOptions that's wire-relevant once the VP9 encoder is
// implemented. The current build only validates options and emits
// ErrVP9NotImplemented from Encode/EncodeInto.
type VP9EncoderOptions struct {
	// Width and Height are the fixed visible dimensions accepted by
	// EncodeInto. Must both be positive.
	Width  int
	Height int

	// FPS sets a 1/FPS timebase when TimebaseNum and TimebaseDen are
	// both zero. Defaults to 30 if all three are unset.
	FPS int

	// TimebaseNum is the numerator of the caller timebase.
	TimebaseNum int
	// TimebaseDen is the denominator of the caller timebase.
	TimebaseDen int

	// Threads selects the worker-goroutine count for the inter-frame
	// tile-threaded macroblock pipeline. Zero or 1 use the serial
	// reference path; >=2 enables tile-parallel encode when the
	// frame is large enough. Negative values return ErrInvalidConfig.
	Threads int

	// TargetBitrateKbps is the total target bitrate in kbps. Required
	// for rate-controlled modes; for VPX_Q / Q-mode encodes the
	// quantizer is taken from Quantizer.
	TargetBitrateKbps int

	// Quantizer selects a fixed VPX_Q-mode quantizer in [0, 255].
	// Zero defers to TargetBitrateKbps + RateControlMode.
	Quantizer int

	// MaxKeyframeInterval bounds the gap between key frames. Zero
	// uses libvpx's default (kf_max_dist=128).
	MaxKeyframeInterval int

	// ErrorResilient enables the libvpx error-resilient bit on every
	// frame header.
	ErrorResilient bool
}

// ErrVP9EncoderNotImplemented is returned by VP9Encoder.Encode /
// EncodeInto until the encoder bitstream path lands.
var ErrVP9EncoderNotImplemented = errors.New("govpx: VP9 encoder not yet implemented")

// VP9Encoder is the public entry point for VP9 stream encoding.
// Encode/EncodeInto currently return ErrVP9EncoderNotImplemented;
// construction + option validation + the IsKeyFrameNext predicate
// are usable today so callers can plumb the surface.
type VP9Encoder struct {
	opts   VP9EncoderOptions
	closed bool

	// frameIndex tracks the frame number for the key-frame cadence
	// gate. Mirrors libvpx's cpi->common.current_video_frame.
	frameIndex int
}

// NewVP9Encoder creates a VP9 encoder with validated options.
// Width and Height must be positive; Threads / Quantizer /
// TargetBitrateKbps / MaxKeyframeInterval must be non-negative.
func NewVP9Encoder(opts VP9EncoderOptions) (*VP9Encoder, error) {
	if err := validateVP9EncoderOptions(opts); err != nil {
		return nil, err
	}
	return &VP9Encoder{opts: opts}, nil
}

func validateVP9EncoderOptions(opts VP9EncoderOptions) error {
	if opts.Width <= 0 || opts.Height <= 0 {
		return ErrInvalidConfig
	}
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if opts.TargetBitrateKbps < 0 || opts.Quantizer < 0 || opts.MaxKeyframeInterval < 0 {
		return ErrInvalidConfig
	}
	if opts.Quantizer > 255 {
		return ErrInvalidQuantizer
	}
	if opts.FPS < 0 {
		return ErrInvalidConfig
	}
	if (opts.TimebaseNum < 0) || (opts.TimebaseDen < 0) {
		return ErrInvalidConfig
	}
	// Either FPS xor both timebase components must be set, or all
	// three may be zero (defaults to 30 fps in libvpx).
	if (opts.TimebaseNum != 0) != (opts.TimebaseDen != 0) {
		return ErrInvalidConfig
	}
	return nil
}

// IsKeyFrameNext reports whether the next call to EncodeInto would
// emit a key frame. The first frame is always a key; subsequent
// frames key on MaxKeyframeInterval boundaries.
func (e *VP9Encoder) IsKeyFrameNext() bool {
	if e == nil || e.closed {
		return false
	}
	if e.frameIndex == 0 {
		return true
	}
	cadence := e.opts.MaxKeyframeInterval
	if cadence <= 0 {
		cadence = 128 // libvpx default kf_max_dist
	}
	return e.frameIndex%cadence == 0
}

// EncodeInto is the planned EncodeInto entry. It currently returns
// ErrVP9EncoderNotImplemented while the bitstream emit path is being
// ported. The signature mirrors VP8Encoder.EncodeInto so callers can
// switch codecs by swapping the constructor.
func (e *VP9Encoder) EncodeInto(_ *image.YCbCr, _ []byte) (int, error) {
	if e == nil || e.closed {
		return 0, ErrClosed
	}
	return 0, ErrVP9EncoderNotImplemented
}

// Encode is the planned alloc-returning entry. It currently returns
// ErrVP9EncoderNotImplemented.
func (e *VP9Encoder) Encode(_ *image.YCbCr) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	return nil, ErrVP9EncoderNotImplemented
}

// Close releases internal state and marks the encoder as no longer
// usable. Subsequent Encode / EncodeInto calls return [ErrClosed].
func (e *VP9Encoder) Close() error {
	if e == nil {
		return ErrClosed
	}
	e.closed = true
	return nil
}

// Codec reports the codec this encoder targets.
func (e *VP9Encoder) Codec() Codec { return CodecVP9 }
