package govpx

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9DecoderOptions configures a VP9 decoder. Mirrors the VP8 shape
// so call sites can switch codecs by swapping the constructor.
//
// The current VP9 stack supports 8-bit 4:2:0 intra frames: mode-info
// and residual tokens are parsed, transform blocks are reconstructed
// from their intra predictors plus inverse transform/add, and other
// valid frame classes return [ErrVP9NotImplemented] at the current
// reconstruct boundary.
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

// ErrVP9NotImplemented is returned by VP9Decoder.Decode for valid VP9
// frames whose reconstruction path has not been ported yet.
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

	// lastHeader carries the previous frame's uncompressed-header
	// state so the parser can seed the fields VP9's wire format
	// preserves across frames (loopfilter + segmentation in
	// particular).
	lastHeader      vp9dec.UncompressedHeader
	lastHeaderValid bool

	// lfi is the per-(seg, ref, mode) loopfilter level table built
	// by LoopFilterFrameInit on every key/show frame.
	lfi vp9dec.LoopFilterInfoN

	// dq carries the per-segment dequant tables built by
	// SetupSegmentationDequant.
	dq vp9dec.DequantTables

	// aboveSegCtx / leftSegCtx are the partition-history arrays the
	// tile mode-info pass stamps while walking SBs. miGrid mirrors the
	// decoder-visible MODE_INFO grid at 8x8 granularity so above/left
	// mode contexts match libvpx across SB and tile edges.
	aboveSegCtx []int8
	leftSegCtx  []int8
	miGrid      []vp9dec.NeighborMi

	// segMap is the current-frame segmentation map used by the
	// mode-info readers; lastSegMap is kept for copy/predicted segment
	// id paths on later frames.
	segMap     []uint8
	lastSegMap []uint8

	// planes carries the per-plane coefficient entropy contexts the
	// residual token pass updates. dqcoeff is stack-equivalent decoder
	// scratch for one 32x32 transform block.
	planes  [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	dqcoeff [1024]int16

	// The first public reconstruction slice handles intra frames.
	// Unsupported frame classes keep parsing intact but stop before
	// publishing output.
	unsupportedReconstruct bool
	frameReady             bool
	lastFrame              Image
	frameY                 []byte
	frameU                 []byte
	frameV                 []byte
	intraScratch           vp9dec.IntraPredictorScratch

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
	vp9dec.ResetFrameContext(&d.fc)
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

// Decode is the VP9 entry point. The uncompressed and compressed
// headers plus intra-only tile mode-info/residual tokens are parsed
// and validated; malformed frames surface as [ErrInvalidVP9Data].
// 8-bit 4:2:0 intra frames decode to I420 output. Other valid
// packets return [ErrVP9NotImplemented] after parser state is updated.
//
// Side effects on a successful parse: the decoder's stored frame
// dimensions, loopfilter state, segmentation state, and mode-info
// buffers are updated so LastFrameSize reflects the latest frame.
func (d *VP9Decoder) Decode(packet []byte) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if len(packet) == 0 {
		return ErrInvalidVP9Data
	}
	var br vp9dec.BitReader
	br.Init(packet)
	var prev *vp9dec.UncompressedHeader
	if d.lastHeaderValid {
		prev = &d.lastHeader
	}
	hdr, err := vp9dec.ReadUncompressedHeader(&br, prev, d.refDims)
	if err != nil {
		return ErrInvalidVP9Data
	}
	if d.opts.MaxWidth > 0 && int(hdr.Width) > d.opts.MaxWidth {
		return ErrFrameRejected
	}
	if d.opts.MaxHeight > 0 && int(hdr.Height) > d.opts.MaxHeight {
		return ErrFrameRejected
	}

	if !hdr.ShowExistingFrame {
		uncSize := br.BytesRead()
		compEnd := uncSize + int(hdr.FirstPartitionSize)
		if compEnd > len(packet) {
			return ErrInvalidVP9Data
		}

		if hdr.FrameType == common.KeyFrame || hdr.IntraOnly || hdr.ErrorResilientMode {
			vp9dec.ResetFrameContext(&d.fc)
		}
		var cr bitstream.Reader
		if err := cr.Init(packet[uncSize:compEnd]); err != nil {
			return ErrInvalidVP9Data
		}
		compHeader := vp9dec.ReadCompressedHeader(&cr, &d.fc, vp9dec.ReadCompressedHeaderArgs{
			Lossless:             hdr.Quant.Lossless,
			IntraOnly:            hdr.IntraOnly,
			KeyFrame:             hdr.FrameType == common.KeyFrame,
			InterpFilter:         hdr.InterpFilter,
			AllowHighPrecisionMv: hdr.AllowHighPrecisionMv,
			CompoundRefAllowed:   false,
		})
		if cr.HasError() {
			return ErrInvalidVP9Data
		}

		vp9dec.SetupSegmentationDequant(&hdr.Seg, vp9dec.SetupSegmentationDequantArgs{
			BaseQindex: int(hdr.Quant.BaseQindex),
			YDcDeltaQ:  int(hdr.Quant.YDcDeltaQ),
			UvDcDeltaQ: int(hdr.Quant.UvDcDeltaQ),
			UvAcDeltaQ: int(hdr.Quant.UvAcDeltaQ),
			BitDepth:   vp9dec.BitDepth(hdr.BitDepthColor.BitDepth),
		}, &d.dq)
		vp9dec.LoopFilterFrameInit(&d.lfi, &hdr.Loopfilter, &hdr.Seg,
			int(hdr.Loopfilter.FilterLevel))

		if hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
			d.unsupportedReconstruct = !vp9SupportedOutputFormat(&hdr)
			if !d.unsupportedReconstruct {
				d.prepareVP9OutputFrame(int(hdr.Width), int(hdr.Height))
			}
			if err := d.parseVP9IntraModeTiles(packet[compEnd:], &hdr, compHeader); err != nil {
				return err
			}
		} else {
			d.unsupportedReconstruct = true
		}
	}

	d.lastHeader = hdr
	d.lastHeaderValid = true
	if !hdr.ShowExistingFrame {
		d.width = int(hdr.Width)
		d.height = int(hdr.Height)
	}
	d.frameReady = false
	if d.vp9CanPublishReconstructedFrame(&hdr) {
		if hdr.ShowFrame {
			d.frameReady = true
		}
		return nil
	}
	return ErrVP9NotImplemented
}

func (d *VP9Decoder) vp9CanPublishReconstructedFrame(hdr *vp9dec.UncompressedHeader) bool {
	if hdr.ShowExistingFrame || d.unsupportedReconstruct {
		return false
	}
	if hdr.FrameType != common.KeyFrame && !hdr.IntraOnly {
		return false
	}
	return vp9SupportedOutputFormat(hdr)
}

func vp9SupportedOutputFormat(hdr *vp9dec.UncompressedHeader) bool {
	if hdr.BitDepthColor.BitDepth != vp9dec.Bits8 ||
		hdr.BitDepthColor.SubsamplingX != 1 ||
		hdr.BitDepthColor.SubsamplingY != 1 {
		return false
	}
	return true
}

func (d *VP9Decoder) prepareVP9OutputFrame(width, height int) {
	yStride, yRows := vp9PaddedPlaneDims(width, height, 0, 0)
	uvStride, uvRows := vp9PaddedPlaneDims(width, height, 1, 1)
	yLen := planeLen(yStride, yRows, yStride)
	uLen := planeLen(uvStride, uvRows, uvStride)
	if cap(d.frameY) < yLen {
		d.frameY = make([]byte, yLen)
	} else {
		d.frameY = d.frameY[:yLen]
	}
	if cap(d.frameU) < uLen {
		d.frameU = make([]byte, uLen)
	} else {
		d.frameU = d.frameU[:uLen]
	}
	if cap(d.frameV) < uLen {
		d.frameV = make([]byte, uLen)
	} else {
		d.frameV = d.frameV[:uLen]
	}
	fillVP9Plane(d.frameY, 128)
	fillVP9Plane(d.frameU, 128)
	fillVP9Plane(d.frameV, 128)
	d.lastFrame = Image{
		Width:   width,
		Height:  height,
		Y:       d.frameY,
		U:       d.frameU,
		V:       d.frameV,
		YStride: yStride,
		UStride: uvStride,
		VStride: uvStride,
	}
}

func vp9PaddedPlaneDims(width, height int, ssX, ssY uint8) (stride, rows int) {
	miCols := (width + common.MiSize - 1) >> common.MiSizeLog2
	miRows := (height + common.MiSize - 1) >> common.MiSizeLog2
	yStride := miCols * common.MiSize
	yRows := miRows * common.MiSize
	stride = (yStride + (1 << ssX) - 1) >> ssX
	rows = (yRows + (1 << ssY) - 1) >> ssY
	return stride, rows
}

func fillVP9Plane(buf []byte, value byte) {
	for i := range buf {
		buf[i] = value
	}
}

// refDims is the placeholder ring-slot dimension lookup. The full
// implementation will return the (width, height) of the reference
// frame at the given ring slot; until the frame-buffer manager
// lands we return the current frame's dimensions, matching the
// libvpx fast-path when every ref is the same size.
func (d *VP9Decoder) refDims(uint8) (uint32, uint32) {
	return uint32(d.width), uint32(d.height)
}

// LastFrameSize returns the (width, height) of the last successfully
// parsed key/inter frame. Returns (0, 0) before any successful
// header parse.
func (d *VP9Decoder) LastFrameSize() (width, height int) {
	if d == nil {
		return 0, 0
	}
	return d.width, d.height
}

// NextFrame returns the most recent visible VP9 frame decoded by the
// currently supported reconstruction path and consumes it. Subsequent
// calls return false until the next visible frame is decoded.
//
// The returned image aliases decoder-owned storage. That storage stays
// valid until the next Decode or Close call.
func (d *VP9Decoder) NextFrame() (Image, bool) {
	if d == nil || d.closed || !d.frameReady {
		return Image{}, false
	}
	d.frameReady = false
	return d.lastFrame, true
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
