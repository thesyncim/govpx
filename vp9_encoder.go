package govpx

import (
	"errors"
	"image"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
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

	// fc carries the per-frame entropy context across frames.
	// Reset on every keyframe via ResetFrameContext.
	fc vp9dec.FrameContext

	// scratch is the reusable compressed-header staging buffer that
	// PackBitstream consults. Sized to 64KB so libvpx's
	// first_partition_size 16-bit cap can never overflow.
	scratch [65536]byte

	// aboveSegCtx / leftSegCtx are the partition-history arrays the
	// per-SB walker stamps. Sized to the frame's mi_cols at first
	// EncodeInto.
	aboveSegCtx []int8
	leftSegCtx  []int8
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

// EncodeInto packs the next frame into dst. The current shape covers
// the keyframe-only stub path: every block emits as a Block64x64
// DC-pred intra with skip=1, so the residue walker short-circuits and
// the output is a valid VP9 frame whose Y/UV planes decode to the
// DC predictor (gray). The compressed header rides the no-update
// path; counts-driven updates land when the encoder's tokenize loop
// exposes real per-frame counters.
//
// Returns the number of bytes written into dst. Caller sizes dst —
// the keyframe header + an empty body is well under 256 bytes for
// modest frame dimensions, but the caller should leave room for
// up to ~64 KB to match libvpx's worst-case header.
func (e *VP9Encoder) EncodeInto(_ *image.YCbCr, dst []byte) (int, error) {
	if e == nil || e.closed {
		return 0, ErrClosed
	}

	width := uint32(e.opts.Width)
	height := uint32(e.opts.Height)
	miCols := int((width + 7) >> 3)
	miRows := int((height + 7) >> 3)
	miColsAligned := alignToSb(miCols)
	if cap(e.aboveSegCtx) < miColsAligned {
		e.aboveSegCtx = make([]int8, miColsAligned)
	} else {
		e.aboveSegCtx = e.aboveSegCtx[:miColsAligned]
		for i := range e.aboveSegCtx {
			e.aboveSegCtx[i] = 0
		}
	}
	if cap(e.leftSegCtx) < common.MiBlockSize {
		e.leftSegCtx = make([]int8, common.MiBlockSize)
	} else {
		e.leftSegCtx = e.leftSegCtx[:common.MiBlockSize]
	}

	isKey := e.IsKeyFrameNext()
	if isKey {
		vp9dec.ResetFrameContext(&e.fc)
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		ShowFrame:             true,
		ErrorResilientMode:    e.opts.ErrorResilient,
		Width:                 width,
		Height:                height,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	// BaseQindex=1 dodges the lossless inference libvpx makes when
	// base_qindex + every delta_q are all zero. Lossless mode forces
	// tx_mode=ONLY_4X4 on the decoder side and skips the tx_mode
	// literal in the compressed header; staying out of lossless keeps
	// the wire layout consistent with the rest of the stub path.
	header.Quant.BaseQindex = 1
	if isKey {
		header.FrameType = common.KeyFrame
		header.RefreshFrameFlags = 0xff
	} else {
		// Inter / intra-only path not wired yet — fall back to an
		// intra-only frame so the decoder doesn't need ref-frame state.
		header.FrameType = common.InterFrame
		header.IntraOnly = true
		header.RefreshFrameFlags = 1
	}

	mi := &vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   1,
	}
	miGet := func(r, c int) *vp9dec.NeighborMi { return mi }
	var seg vp9dec.SegmentationParams // disabled — no map / no data update

	// libvpx swaps in vp9_kf_partition_probs (not fc->partition_prob)
	// for keyframe / intra-only frames — see set_partition_probs in
	// vp9/common/vp9_onyxc_int.h. The two tables have the same shape
	// but different probabilities, so the bool stream desyncs if the
	// encoder uses the wrong one.
	partitionProbs := tables.KfPartitionProbs

	args := encoder.PackBitstreamArgs{
		Dest:    dst,
		Scratch: e.scratch[:],
		Header:  &header,
		Comp: encoder.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			sbArgs := encoder.WriteModesSbArgs{
				AboveSegCtx:    e.aboveSegCtx,
				LeftSegCtx:     e.leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi:          miGet,
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					encoder.WriteKeyframeBlock(bw, encoder.WriteKeyframeBlockArgs{
						Seg:       &seg,
						Mi:        mi,
						TxMode:    common.Only4x4,
						SkipProbs: e.fc.SkipProbs,
					})
					encoder.WriteKeyframeUvMode(bw, common.DcPred, mi.Mode)
					// skip=1 → no residue walk.
				},
			}
			encoder.WriteModesTile(bw, encoder.WriteModesTileArgs{
				WriteModesSbArgs: sbArgs,
				MiRowStart:       0,
				MiRowEnd:         miRows,
				MiColStart:       0,
				MiColEnd:         miCols,
			})
			return nil
		},
	}

	n, err := encoder.PackBitstream(args)
	if err != nil {
		return n, err
	}
	e.frameIndex++
	return n, nil
}

// Encode is the alloc-returning wrapper around EncodeInto. Sizes
// dst at 64 KB upfront so EncodeInto can never overflow the
// compressed-header staging buffer for the stub keyframe body.
func (e *VP9Encoder) Encode(img *image.YCbCr) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(img, dst)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, dst[:n])
	return out, nil
}

func alignToSb(miCols int) int {
	const mask = common.MiBlockSize - 1
	return (miCols + mask) &^ mask
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
