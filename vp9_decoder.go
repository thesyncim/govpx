package govpx

import (
	"errors"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9DecoderOptions configures a VP9 decoder. Mirrors the VP8 shape
// so call sites can switch codecs by swapping the constructor.
//
// The current VP9 stack is parser-only (see internal/vp9 — uncompressed
// header, compressed header, mode info, detokenize, and DSP kernels
// are byte-parity tested against libvpx v1.16.0 but the per-block
// reconstruct pipeline isn't wired yet). VP9Decoder.Decode therefore
// returns [ErrVP9NotImplemented] until the residual + reconstruct
// loops land. Constructors and accessors are usable today.
type VP9DecoderOptions struct {
	// Threads selects the decoder worker count for parallel tile
	// rows. 0 and 1 use the serial path. The threaded path mirrors
	// the libvpx row-pipeline and will be enabled when the
	// reconstruct loop lands.
	Threads int

	// MaxWidth and MaxHeight cap the accepted frame dimensions.
	// Zero means no cap.
	MaxWidth  int
	MaxHeight int

	// RejectResolutionChange, when true, makes Decode reject a key
	// frame whose dimensions differ from the active stream.
	RejectResolutionChange bool
}

// ErrVP9NotImplemented is returned by VP9Decoder.Decode while the
// per-block reconstruct pipeline is still being ported.
var ErrVP9NotImplemented = errors.New("govpx: VP9 reconstruct pipeline not yet implemented")

// VP9Decoder is the public entry point for VP9 stream decoding. The
// internal/vp9 package carries the parser and DSP kernels; this type
// holds the per-frame context (FrameContext, SegmentationParams,
// LoopfilterParams, dequant tables) the parser needs across frames.
type VP9Decoder struct {
	opts   VP9DecoderOptions
	closed bool

	// fc carries the probability tables the compressed header walks
	// and updates each frame. Seeded with libvpx's default tables
	// at construction; reset to defaults on every keyframe.
	fc vp9dec.FrameContext

	// seg carries the segmentation header. Preserved across
	// non-update frames per libvpx's setup_segmentation contract.
	seg vp9dec.SegmentationParams

	// lf carries the loopfilter header — filter_level + sharpness +
	// mode/ref deltas.
	lf vp9dec.LoopfilterParams

	// lfi is the per-(seg, ref, mode) loopfilter level table built
	// by LoopFilterFrameInit on every key/show frame.
	lfi vp9dec.LoopFilterInfoN

	// dq carries the per-segment dequant tables built by
	// SetupSegmentationDequant.
	dq vp9dec.DequantTables

	// width and height carry the last keyframe's frame dimensions.
	// Reset on every keyframe; non-zero only after the first
	// successful keyframe parse.
	width  int
	height int
}

// NewVP9Decoder creates a VP9 decoder with validated options. The
// zero value of opts is valid: it produces a single-threaded decoder
// with no dimension caps.
func NewVP9Decoder(opts VP9DecoderOptions) (*VP9Decoder, error) {
	if err := validateVP9DecoderOptions(opts); err != nil {
		return nil, err
	}
	d := &VP9Decoder{opts: opts}
	d.lfi = vp9dec.NewLoopFilterInfoN()
	return d, nil
}

func validateVP9DecoderOptions(opts VP9DecoderOptions) error {
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if opts.MaxWidth < 0 || opts.MaxHeight < 0 {
		return ErrInvalidConfig
	}
	return nil
}

// Decode is the VP9 entry point. While the reconstruct pipeline is
// still under construction it returns [ErrVP9NotImplemented]; the
// parser layer is byte-parity tested separately in
// internal/vp9/decoder.
func (d *VP9Decoder) Decode(packet []byte) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	_ = packet
	return ErrVP9NotImplemented
}

// Close releases internal state and marks the decoder as no longer
// usable. Subsequent calls to Decode return [ErrClosed].
func (d *VP9Decoder) Close() error {
	if d == nil {
		return ErrClosed
	}
	d.closed = true
	return nil
}

// Codec reports the codec this decoder is built for.
func (d *VP9Decoder) Codec() Codec { return CodecVP9 }
