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
// The current VP9 stack supports 8-bit 4:2:0 intra frames plus the
// first single-reference inter-frame reconstruction paths: zero-MV
// copy/residual blocks and direct non-scaled motion blocks whose
// interpolation window stays inside the stored reference. Other valid
// frame classes return [ErrVP9NotImplemented] at the current
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

	// RejectResolutionChange, when true, makes Decode reject a coded
	// frame whose dimensions differ from the active stream.
	RejectResolutionChange bool
}

// VP9FrameInfo describes one decoded VP9 packet. Quantizer is the raw
// VP9 base qindex in [0, 255]; show-existing packets do not carry a
// quantizer and report zero.
type VP9FrameInfo struct {
	// Width and Height are the visible output dimensions.
	Width  int
	Height int

	// KeyFrame reports whether the packet is a key frame.
	KeyFrame bool
	// ShowFrame reports whether the packet produced visible output.
	ShowFrame bool
	// ShowExistingFrame reports whether the packet displayed a stored
	// VP9 reference slot instead of carrying a frame body.
	ShowExistingFrame bool
	// ExistingFrameSlot is valid when ShowExistingFrame is true.
	ExistingFrameSlot uint8

	// Quantizer is the raw VP9 base qindex in [0, 255].
	Quantizer int
	// RefreshFrameFlags is the VP9 reference-slot update bitmask.
	RefreshFrameFlags uint8

	// PTS is the caller-provided presentation timestamp.
	PTS uint64
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

	// frameContexts mirrors VP9's four entropy-context slots. fc is the
	// active scratch copy selected by frame_context_idx for the current
	// frame; compressed-header updates are committed back only when the
	// header's refresh_frame_context bit allows it.
	frameContexts [common.FrameContexts]vp9dec.FrameContext
	fc            vp9dec.FrameContext

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
	planes                [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	dqcoeff               [1024]int16
	segIDPredictedScratch uint8

	// The first public reconstruction slice handles intra frames.
	// Unsupported frame classes keep parsing intact but stop before
	// publishing output.
	unsupportedReconstruct bool
	frameReady             bool
	lastFrame              Image
	lastInfo               VP9FrameInfo
	lastInfoValid          bool
	initialized            bool
	frameY                 []byte
	frameU                 []byte
	frameV                 []byte
	intraScratch           vp9dec.IntraPredictorScratch
	refFrames              [common.RefFrames]vp9ReferenceFrame

	// width and height carry the most recent decoded frame dimensions.
	// They stay zero until the first successful frame parse.
	width  int
	height int
}

type vp9ReferenceFrame struct {
	img   Image
	y     []byte
	u     []byte
	v     []byte
	valid bool
}

func (f *vp9ReferenceFrame) store(src Image) {
	f.y = ensureVP9PlaneCapacity(f.y, len(src.Y))
	f.u = ensureVP9PlaneCapacity(f.u, len(src.U))
	f.v = ensureVP9PlaneCapacity(f.v, len(src.V))
	copy(f.y, src.Y)
	copy(f.u, src.U)
	copy(f.v, src.V)
	f.img = Image{
		Width:   src.Width,
		Height:  src.Height,
		Y:       f.y,
		U:       f.u,
		V:       f.v,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
	f.valid = true
}

func ensureVP9PlaneCapacity(buf []byte, n int) []byte {
	if cap(buf) < n {
		return make([]byte, n)
	}
	return buf[:n]
}

// NewVP9Decoder creates a VP9 decoder with validated options. The
// zero value of opts is valid: it produces a single-threaded decoder
// with no dimension caps.
func NewVP9Decoder(opts VP9DecoderOptions) (*VP9Decoder, error) {
	if err := validateVP9DecoderOptions(opts); err != nil {
		return nil, err
	}
	d := &VP9Decoder{opts: opts}
	d.resetVP9FrameContexts()
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

// Decode is the VP9 entry point. It is equivalent to DecodeWithPTS
// with a zero presentation timestamp.
func (d *VP9Decoder) Decode(packet []byte) error {
	return d.DecodeWithPTS(packet, 0)
}

// DecodeWithPTS decodes one raw VP9 frame payload. The uncompressed and
// compressed headers plus tile mode-info/residual tokens are parsed and
// validated; malformed frames surface as [ErrInvalidVP9Data]. 8-bit 4:2:0
// intra frames and the currently supported single-reference inter frames
// decode to I420 output. Other valid packets return
// [ErrVP9NotImplemented] after parser state is updated. pts is echoed
// back through [VP9Decoder.LastFrameInfo].
//
// Side effects on a successful parse: the decoder's stored frame
// dimensions, loopfilter state, segmentation state, mode-info buffers,
// reference slots, and last-frame metadata are updated.
func (d *VP9Decoder) DecodeWithPTS(packet []byte, pts uint64) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	hdr, uncSize, err := d.readVP9UncompressedHeader(packet)
	if err != nil {
		return err
	}
	info, err := d.vp9FrameInfoFromHeader(&hdr, pts)
	if err != nil {
		return err
	}

	if hdr.ShowExistingFrame {
		if err := d.decodeVP9ShowExistingFrame(&hdr); err != nil {
			return err
		}
		d.finishVP9FrameInfo(info)
		return nil
	}

	compEnd := uncSize + int(hdr.FirstPartitionSize)
	if compEnd > len(packet) {
		return ErrInvalidVP9Data
	}

	frameContextIdx := d.prepareVP9FrameContext(&hdr)
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
		CompoundRefAllowed:   vp9CompoundReferenceAllowed(&hdr),
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
		d.unsupportedReconstruct = !vp9SupportedOutputFormat(&hdr)
		if !d.unsupportedReconstruct {
			d.prepareVP9OutputFrame(int(hdr.Width), int(hdr.Height))
		}
		if err := d.parseVP9InterModeTiles(packet[compEnd:], &hdr, compHeader); err != nil {
			return err
		}
	}
	d.commitVP9FrameContext(&hdr, frameContextIdx)

	d.lastHeader = hdr
	d.lastHeaderValid = true
	if !hdr.ShowExistingFrame {
		d.width = int(hdr.Width)
		d.height = int(hdr.Height)
	}
	d.frameReady = false
	if d.vp9CanPublishReconstructedFrame(&hdr) {
		d.refreshVP9ReferenceFrames(&hdr)
		if hdr.ShowFrame {
			d.frameReady = true
		}
		d.finishVP9FrameInfo(info)
		return nil
	}
	return ErrVP9NotImplemented
}

// DecodeInto decodes one raw VP9 frame payload. If the packet is a
// visible frame its decoded pixels are written into the caller-owned
// planes of dst; for hidden frames dst is left untouched.
func (d *VP9Decoder) DecodeInto(packet []byte, dst *Image) (VP9FrameInfo, error) {
	return d.DecodeIntoWithPTS(packet, dst, 0)
}

// DecodeIntoWithPTS is DecodeInto with an explicit presentation timestamp.
// pts is echoed back in the returned VP9FrameInfo.
func (d *VP9Decoder) DecodeIntoWithPTS(packet []byte, dst *Image, pts uint64) (VP9FrameInfo, error) {
	if d == nil || d.closed {
		return VP9FrameInfo{}, ErrClosed
	}
	if dst == nil {
		return VP9FrameInfo{}, ErrInvalidConfig
	}
	hdr, _, err := d.readVP9UncompressedHeader(packet)
	if err != nil {
		return VP9FrameInfo{}, err
	}
	info, err := d.vp9FrameInfoFromHeader(&hdr, pts)
	if err != nil {
		return VP9FrameInfo{}, err
	}
	if !dst.validForEncode(info.Width, info.Height) {
		return VP9FrameInfo{}, ErrInvalidConfig
	}
	if err := d.DecodeWithPTS(packet, pts); err != nil {
		return VP9FrameInfo{}, err
	}
	d.frameReady = false
	if info.ShowFrame {
		copyVP9ImageToPublic(dst, d.lastFrame)
	}
	return info, nil
}

func (d *VP9Decoder) readVP9UncompressedHeader(packet []byte) (vp9dec.UncompressedHeader, int, error) {
	if len(packet) == 0 {
		return vp9dec.UncompressedHeader{}, 0, ErrInvalidVP9Data
	}
	var br vp9dec.BitReader
	br.Init(packet)
	var prev *vp9dec.UncompressedHeader
	if d.lastHeaderValid {
		prev = &d.lastHeader
	}
	hdr, err := vp9dec.ReadUncompressedHeader(&br, prev, d.refDims)
	if err != nil {
		return vp9dec.UncompressedHeader{}, 0, ErrInvalidVP9Data
	}
	if hdr.ShowExistingFrame {
		return hdr, br.BytesRead(), nil
	}
	if hdr.Width == 0 || hdr.Height == 0 {
		return vp9dec.UncompressedHeader{}, 0, ErrInvalidVP9Data
	}
	if d.opts.MaxWidth > 0 && int(hdr.Width) > d.opts.MaxWidth {
		return vp9dec.UncompressedHeader{}, 0, ErrFrameRejected
	}
	if d.opts.MaxHeight > 0 && int(hdr.Height) > d.opts.MaxHeight {
		return vp9dec.UncompressedHeader{}, 0, ErrFrameRejected
	}
	if d.initialized && d.opts.RejectResolutionChange &&
		(int(hdr.Width) != d.width || int(hdr.Height) != d.height) {
		return vp9dec.UncompressedHeader{}, 0, ErrFrameRejected
	}
	return hdr, br.BytesRead(), nil
}

func (d *VP9Decoder) vp9FrameInfoFromHeader(hdr *vp9dec.UncompressedHeader, pts uint64) (VP9FrameInfo, error) {
	info := VP9FrameInfo{
		KeyFrame:          !hdr.ShowExistingFrame && hdr.FrameType == common.KeyFrame,
		ShowFrame:         hdr.ShowFrame,
		ShowExistingFrame: hdr.ShowExistingFrame,
		ExistingFrameSlot: hdr.ExistingFrameSlot,
		Quantizer:         int(hdr.Quant.BaseQindex),
		RefreshFrameFlags: hdr.RefreshFrameFlags,
		PTS:               pts,
	}
	if hdr.ShowExistingFrame {
		slot := int(hdr.ExistingFrameSlot)
		if slot >= len(d.refFrames) || !d.refFrames[slot].valid {
			return VP9FrameInfo{}, ErrInvalidVP9Data
		}
		ref := &d.refFrames[slot].img
		info.Width = ref.Width
		info.Height = ref.Height
		info.ShowFrame = true
		return info, nil
	}
	info.Width = int(hdr.Width)
	info.Height = int(hdr.Height)
	return info, nil
}

func (d *VP9Decoder) finishVP9FrameInfo(info VP9FrameInfo) {
	d.lastInfo = info
	d.lastInfoValid = true
	d.initialized = true
}

func (d *VP9Decoder) resetVP9FrameContexts() {
	for i := range d.frameContexts {
		vp9dec.ResetFrameContext(&d.frameContexts[i])
	}
	d.fc = d.frameContexts[0]
}

func (d *VP9Decoder) prepareVP9FrameContext(hdr *vp9dec.UncompressedHeader) int {
	idx := int(hdr.FrameContextIdx)
	if idx >= common.FrameContexts {
		idx = 0
	}
	if hdr.FrameType == common.KeyFrame || hdr.IntraOnly ||
		hdr.ErrorResilientMode || hdr.ResetFrameContext == 3 {
		d.resetVP9FrameContexts()
		idx = 0
	} else if hdr.ResetFrameContext == 2 {
		vp9dec.ResetFrameContext(&d.frameContexts[idx])
	}
	d.fc = d.frameContexts[idx]
	return idx
}

func (d *VP9Decoder) commitVP9FrameContext(hdr *vp9dec.UncompressedHeader, idx int) {
	if idx < 0 || idx >= common.FrameContexts || !hdr.RefreshFrameContext {
		return
	}
	d.frameContexts[idx] = d.fc
}

func (d *VP9Decoder) vp9CanPublishReconstructedFrame(hdr *vp9dec.UncompressedHeader) bool {
	if hdr.ShowExistingFrame || d.unsupportedReconstruct {
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

func vp9FrameRefSignBias(hdr *vp9dec.UncompressedHeader) [vp9dec.MaxRefFrames]uint8 {
	var signBias [vp9dec.MaxRefFrames]uint8
	for i := range common.RefsPerFrame {
		signBias[vp9dec.LastFrame+i] = hdr.InterRef.SignBias[i]
	}
	return signBias
}

func vp9CompoundReferenceAllowed(hdr *vp9dec.UncompressedHeader) bool {
	if hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
		return false
	}
	return vp9dec.CompoundReferenceAllowed(vp9FrameRefSignBias(hdr))
}

func (d *VP9Decoder) decodeVP9ShowExistingFrame(hdr *vp9dec.UncompressedHeader) error {
	slot := int(hdr.ExistingFrameSlot)
	if slot >= len(d.refFrames) || !d.refFrames[slot].valid {
		return ErrInvalidVP9Data
	}
	ref := &d.refFrames[slot]
	d.lastFrame = ref.img
	d.width = ref.img.Width
	d.height = ref.img.Height
	d.frameReady = true
	return nil
}

func (d *VP9Decoder) refreshVP9ReferenceFrames(hdr *vp9dec.UncompressedHeader) {
	flags := hdr.RefreshFrameFlags
	for slot := range d.refFrames {
		if flags&(1<<uint(slot)) != 0 {
			d.refFrames[slot].store(d.lastFrame)
		}
	}
}

func copyVP9ImageToPublic(dst *Image, src Image) {
	copyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	copyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	copyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
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

// refDims returns the dimensions of a stored VP9 reference slot. Frames
// that predate the reference manager fall back to the active stream size,
// matching the libvpx fast-path when every ref has the same dimensions.
func (d *VP9Decoder) refDims(slot uint8) (uint32, uint32) {
	idx := int(slot)
	if idx < len(d.refFrames) && d.refFrames[idx].valid {
		ref := &d.refFrames[idx].img
		return uint32(ref.Width), uint32(ref.Height)
	}
	return uint32(d.width), uint32(d.height)
}

// LastFrameSize returns the (width, height) of the last successfully
// decoded VP9 frame. Returns (0, 0) before any successful header parse.
func (d *VP9Decoder) LastFrameSize() (width, height int) {
	if d == nil {
		return 0, 0
	}
	return d.width, d.height
}

// LastFrameInfo returns metadata for the most recently decoded VP9 frame.
// ok is false on a nil or closed decoder, and before the first successful
// Decode/DecodeInto call.
func (d *VP9Decoder) LastFrameInfo() (VP9FrameInfo, bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return VP9FrameInfo{}, false
	}
	return d.lastInfo, true
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

// Reset returns the decoder to its cold-start state while retaining
// allocated buffers and validated VP9DecoderOptions for reuse.
func (d *VP9Decoder) Reset() {
	if d == nil {
		return
	}
	d.resetVP9FrameContexts()
	d.lastHeader = vp9dec.UncompressedHeader{}
	d.lastHeaderValid = false
	d.unsupportedReconstruct = false
	d.frameReady = false
	d.lastFrame = Image{}
	d.lastInfo = VP9FrameInfo{}
	d.lastInfoValid = false
	d.initialized = false
	d.width = 0
	d.height = 0
	for i := range d.refFrames {
		d.refFrames[i].img = Image{}
		d.refFrames[i].valid = false
	}
	if d.aboveSegCtx != nil {
		d.aboveSegCtx = d.aboveSegCtx[:0]
	}
	if d.leftSegCtx != nil {
		d.leftSegCtx = d.leftSegCtx[:0]
	}
	if d.miGrid != nil {
		d.miGrid = d.miGrid[:0]
	}
	if d.segMap != nil {
		d.segMap = d.segMap[:0]
	}
	if d.lastSegMap != nil {
		d.lastSegMap = d.lastSegMap[:0]
	}
}

// Close releases internal state and marks the decoder as no longer
// usable. Subsequent calls to Decode return [ErrClosed].
func (d *VP9Decoder) Close() error {
	if d == nil {
		return ErrClosed
	}
	d.Reset()
	d.closed = true
	return nil
}

// Codec reports the codec this decoder is built for.
func (d *VP9Decoder) Codec() Codec { return CodecVP9 }
