package govpx

import (
	"errors"
	"fmt"
	"os"

	"github.com/thesyncim/govpx/internal/vp8/mem"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9DecoderOptions configures a VP9 decoder. Mirrors the VP8 shape
// so call sites can switch codecs by swapping the constructor.
//
// The VP9 implementation scope is full profile 0 support: 8-bit 4:2:0
// raw VP9 packets, including valid superframes. Profiles 1, 2, and 3
// are intentionally outside the package scope and return
// [ErrVP9NotImplemented] when their headers are otherwise valid.
type VP9DecoderOptions struct {
	// Threads selects the decoder worker count. 0 and 1 use the serial path.
	// Values >= 2 enable persistent VP9 tile-mode and loop-filter worker pools.
	// Tile-mode workers are used only for frame-parallel, multi-tile frames with
	// one tile row, so entropy-count adaptation and row dependencies remain in
	// libvpx order.
	Threads int

	// SVCSpatialLayerSet enables libvpx-style VP9 spatial-SVC superframe
	// filtering. When set, Decode decodes only frames 0..SVCSpatialLayer from a
	// VP9 superframe, matching VP9_DECODE_SVC_SPATIAL_LAYER. Zero leaves
	// superframes fully decoded.
	SVCSpatialLayerSet bool
	// SVCSpatialLayer is the highest VP9 spatial layer decoded from a
	// superframe when SVCSpatialLayerSet is true. Valid values are 0..7.
	SVCSpatialLayer uint8

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
// packets outside the implemented profile 0 reconstruction surface.
var ErrVP9NotImplemented = errors.New("govpx: VP9 packet outside implemented profile 0 scope")

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
	segMap       []uint8
	lastSegMap   []uint8
	segMapMiRows int
	segMapMiCols int

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
	frameYFull             []byte
	frameUFull             []byte
	frameVFull             []byte
	frameYOrigin           int
	frameUOrigin           int
	frameVOrigin           int
	intraScratch           vp9dec.IntraPredictorScratch
	interPredictScratch    []byte
	refFrames              [common.RefFrames]vp9ReferenceFrame
	prevFrameMvs           []vp9MvRef
	curFrameMvs            []vp9MvRef
	prevFrameMvRows        int
	prevFrameMvCols        int
	usePrevFrameMvs        bool
	counts                 vp9FrameCounts

	// width and height carry the most recent decoded frame dimensions.
	// They stay zero until the first successful frame parse.
	width  int
	height int

	vp9LoopFilterPool *vp9DecoderLoopFilterPool
	vp9TilePool       *vp9DecoderTileWorkerPool
}

type vp9ReferenceFrame struct {
	img   Image
	y     []byte
	u     []byte
	v     []byte
	valid bool
}

type vp9MvRef struct {
	RefFrame [2]int8
	Mv       [2]vp9dec.MV
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
	vp9dec.LoopFilterInit(&d.lfi, 0)
	if opts.Threads > 1 {
		d.vp9LoopFilterPool = newVP9DecoderLoopFilterPool(opts.Threads)
		d.vp9TilePool = newVP9DecoderTileWorkerPool(opts.Threads)
	}
	return d, nil
}

func validateVP9DecoderOptions(opts VP9DecoderOptions) error {
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if opts.SVCSpatialLayerSet && opts.SVCSpatialLayer >= VP9RTPMaxSpatialLayers {
		return ErrInvalidConfig
	}
	if opts.MaxWidth < 0 || opts.MaxHeight < 0 {
		return ErrInvalidConfig
	}
	return nil
}

// SetSVCSpatialLayer enables libvpx-style VP9 spatial-SVC superframe
// filtering. Subsequent Decode calls decode only frames 0..layer from a VP9
// superframe, matching VP9_DECODE_SVC_SPATIAL_LAYER.
func (d *VP9Decoder) SetSVCSpatialLayer(layer uint8) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if layer >= VP9RTPMaxSpatialLayers {
		return ErrInvalidConfig
	}
	d.opts.SVCSpatialLayerSet = true
	d.opts.SVCSpatialLayer = layer
	return nil
}

// ClearSVCSpatialLayer disables VP9 spatial-SVC superframe filtering so
// subsequent Decode calls decode every frame listed in a VP9 superframe.
func (d *VP9Decoder) ClearSVCSpatialLayer() error {
	if d == nil || d.closed {
		return ErrClosed
	}
	d.opts.SVCSpatialLayerSet = false
	d.opts.SVCSpatialLayer = 0
	return nil
}

// Decode is the VP9 entry point. It is equivalent to DecodeWithPTS
// with a zero presentation timestamp.
func (d *VP9Decoder) Decode(packet []byte) error {
	return d.DecodeWithPTS(packet, 0)
}

// DecodeWithPTS decodes one raw VP9 packet payload in the profile 0 scope.
// Packets with a valid VP9 superframe index are split and decoded in order.
// The uncompressed and compressed headers plus tile mode-info/residual tokens
// are parsed and validated; malformed frames surface as [ErrInvalidVP9Data].
// Supported profile 0 frames decode to I420 output. Valid packets outside
// profile 0 return [ErrVP9NotImplemented]. pts is echoed back through
// [VP9Decoder.LastFrameInfo].
//
// Side effects on a successful parse: the decoder's stored frame
// dimensions, loopfilter state, segmentation state, mode-info buffers,
// reference slots, and last-frame metadata are updated.
func (d *VP9Decoder) DecodeWithPTS(packet []byte, pts uint64) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	sf, err := vp9ParseSuperframe(packet)
	if err != nil {
		return err
	}
	if sf.count == 0 {
		return d.decodeVP9FrameWithPTS(packet, pts)
	}
	frameCount := d.vp9SVCFrameCount(sf.count)
	for i := 0; i < frameCount; i++ {
		if err := d.decodeVP9FrameWithPTS(sf.frames[i], pts); err != nil {
			return err
		}
	}
	return nil
}

func (d *VP9Decoder) vp9SVCFrameCount(frameCount int) int {
	if !d.opts.SVCSpatialLayerSet {
		return frameCount
	}
	limit := int(d.opts.SVCSpatialLayer) + 1
	if limit < frameCount {
		return limit
	}
	return frameCount
}

func (d *VP9Decoder) decodeVP9FrameWithPTS(packet []byte, pts uint64) error {
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
	if !vp9SupportedOutputFormat(&hdr) {
		d.lastHeader = hdr
		d.lastHeaderValid = true
		if !hdr.ShowExistingFrame {
			d.width = int(hdr.Width)
			d.height = int(hdr.Height)
		}
		d.frameReady = false
		d.finishVP9FrameInfo(info)
		return ErrVP9NotImplemented
	}

	frameContextIdx := d.prepareVP9FrameContext(&hdr)
	if !hdr.FrameParallelDecoding {
		d.counts = vp9FrameCounts{}
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
		d.prepareVP9CurrentFrameMvs(&hdr)
		if err := d.parseVP9InterModeTiles(packet[compEnd:], &hdr, compHeader); err != nil {
			return err
		}
	}
	if !d.unsupportedReconstruct && hdr.Loopfilter.FilterLevel != 0 {
		if !d.applyVP9LoopFilter(&hdr) {
			d.traceVP9Unsupported("loopfilter")
			d.unsupportedReconstruct = true
		}
	}
	d.adaptVP9FrameContext(&hdr, compHeader, frameContextIdx)
	d.commitVP9FrameContext(&hdr, frameContextIdx)

	d.lastHeader = hdr
	d.lastHeaderValid = true
	if !hdr.ShowExistingFrame {
		d.width = int(hdr.Width)
		d.height = int(hdr.Height)
	}
	d.finishVP9CurrentFrameMvs(&hdr)
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

type vp9SuperframeIndex struct {
	frames [8][]byte
	count  int
}

func vp9ParseSuperframe(packet []byte) (vp9SuperframeIndex, error) {
	var sf vp9SuperframeIndex
	if len(packet) == 0 {
		return sf, ErrInvalidVP9Data
	}
	marker := packet[len(packet)-1]
	if marker&0xe0 != 0xc0 {
		return sf, nil
	}

	frames := int(marker&0x7) + 1
	sizeBytes := int((marker>>3)&0x3) + 1
	indexSize := 2 + frames*sizeBytes
	if len(packet) < indexSize {
		return sf, ErrInvalidVP9Data
	}
	indexStart := len(packet) - indexSize
	if packet[indexStart] != marker {
		return sf, ErrInvalidVP9Data
	}

	offset := 0
	sizeOffset := indexStart + 1
	for i := 0; i < frames; i++ {
		frameSize := 0
		for j := 0; j < sizeBytes; j++ {
			frameSize |= int(packet[sizeOffset+i*sizeBytes+j]) << (8 * j)
		}
		if frameSize <= 0 || frameSize > indexStart-offset {
			return sf, ErrInvalidVP9Data
		}
		sf.frames[i] = packet[offset : offset+frameSize]
		offset += frameSize
	}
	if offset != indexStart {
		return sf, ErrInvalidVP9Data
	}
	sf.count = frames
	return sf, nil
}

// DecodeInto decodes one raw VP9 Profile 0 packet payload. If the packet is a
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
	sf, err := vp9ParseSuperframe(packet)
	if err != nil {
		return VP9FrameInfo{}, err
	}
	if sf.count != 0 {
		frameCount := d.vp9SVCFrameCount(sf.count)
		for i := 0; i < frameCount; i++ {
			if err := d.decodeVP9FrameWithPTS(sf.frames[i], pts); err != nil {
				return VP9FrameInfo{}, err
			}
		}
		info := d.lastInfo
		if !d.lastInfoValid {
			return VP9FrameInfo{}, ErrInvalidVP9Data
		}
		d.frameReady = false
		if info.ShowFrame {
			if !dst.validForEncode(info.Width, info.Height) {
				return VP9FrameInfo{}, ErrInvalidConfig
			}
			copyVP9ImageToPublic(dst, d.lastFrame)
		}
		return info, nil
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
	if hdr.FrameType == common.KeyFrame ||
		hdr.ErrorResilientMode || hdr.ResetFrameContext == 3 {
		d.resetVP9FrameContexts()
		idx = 0
	} else if hdr.IntraOnly && hdr.ResetFrameContext == 2 {
		vp9dec.ResetFrameContext(&d.frameContexts[idx])
		idx = 0
	} else if hdr.IntraOnly {
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
	if hdr.Profile != common.Profile0 ||
		hdr.BitDepthColor.BitDepth != vp9dec.Bits8 ||
		hdr.BitDepthColor.SubsamplingX != 1 ||
		hdr.BitDepthColor.SubsamplingY != 1 {
		return false
	}
	return true
}

func (d *VP9Decoder) traceVP9Unsupported(reason string) {
	if os.Getenv("GOVPX_TRACE_UNSUPPORTED") != "" {
		fmt.Fprintf(os.Stderr, "vp9 unsupported: %s\n", reason)
	}
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

func (d *VP9Decoder) prepareVP9CurrentFrameMvs(hdr *vp9dec.UncompressedHeader) {
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	need := miRows * miCols
	if cap(d.curFrameMvs) < need {
		d.curFrameMvs = make([]vp9MvRef, need)
	} else {
		d.curFrameMvs = d.curFrameMvs[:need]
		for i := range d.curFrameMvs {
			d.curFrameMvs[i] = vp9MvRef{}
		}
	}
	d.usePrevFrameMvs = d.lastHeaderValid &&
		!hdr.ErrorResilientMode &&
		hdr.Width == d.lastHeader.Width &&
		hdr.Height == d.lastHeader.Height &&
		!d.lastHeader.IntraOnly &&
		d.lastHeader.ShowFrame &&
		d.lastHeader.FrameType != common.KeyFrame &&
		d.prevFrameMvRows == miRows &&
		d.prevFrameMvCols == miCols &&
		len(d.prevFrameMvs) >= need
}

func (d *VP9Decoder) finishVP9CurrentFrameMvs(hdr *vp9dec.UncompressedHeader) {
	if hdr.ShowExistingFrame || hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
		d.usePrevFrameMvs = false
		return
	}
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	if len(d.curFrameMvs) < miRows*miCols {
		d.usePrevFrameMvs = false
		return
	}
	d.prevFrameMvs, d.curFrameMvs = d.curFrameMvs, d.prevFrameMvs
	d.prevFrameMvRows = miRows
	d.prevFrameMvCols = miCols
	d.usePrevFrameMvs = false
}

func copyVP9ImageToPublic(dst *Image, src Image) {
	copyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	copyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	copyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
}

func (d *VP9Decoder) prepareVP9OutputFrame(width, height int) {
	layout := vp9FrameBufferLayout(width, height)
	d.frameYFull = ensureVP9AlignedPlaneCapacity(d.frameYFull, layout.yFullLen)
	d.frameUFull = ensureVP9AlignedPlaneCapacity(d.frameUFull, layout.uvFullLen)
	d.frameVFull = ensureVP9AlignedPlaneCapacity(d.frameVFull, layout.uvFullLen)
	fillVP9Plane(d.frameYFull, 128)
	fillVP9Plane(d.frameUFull, 128)
	fillVP9Plane(d.frameVFull, 128)
	d.frameYOrigin = layout.yOrigin
	d.frameUOrigin = layout.uvOrigin
	d.frameVOrigin = layout.uvOrigin
	d.frameY = d.frameYFull[layout.yOrigin:]
	d.frameU = d.frameUFull[layout.uvOrigin:]
	d.frameV = d.frameVFull[layout.uvOrigin:]
	d.lastFrame = Image{
		Width:   width,
		Height:  height,
		Y:       d.frameY,
		U:       d.frameU,
		V:       d.frameV,
		YStride: layout.yStride,
		UStride: layout.uvStride,
		VStride: layout.uvStride,
	}
}

type vp9FrameLayout struct {
	yStride   int
	uvStride  int
	yWidth    int
	yHeight   int
	uvWidth   int
	uvHeight  int
	yOrigin   int
	uvOrigin  int
	yFullLen  int
	uvFullLen int
}

func vp9FrameBufferLayout(width, height int) vp9FrameLayout {
	const border = 32 // VP9_DEC_BORDER_IN_PIXELS in libvpx vpx_scale/yv12config.h.
	alignedWidth := vp9AlignTo(width, 8)
	alignedHeight := vp9AlignTo(height, 8)
	yStride := vp9AlignTo(alignedWidth+2*border, 32)
	uvWidth := alignedWidth >> 1
	uvHeight := alignedHeight >> 1
	uvStride := yStride >> 1
	uvBorder := border >> 1
	return vp9FrameLayout{
		yStride:   yStride,
		uvStride:  uvStride,
		yWidth:    alignedWidth,
		yHeight:   alignedHeight,
		uvWidth:   uvWidth,
		uvHeight:  uvHeight,
		yOrigin:   border*yStride + border,
		uvOrigin:  uvBorder*uvStride + uvBorder,
		yFullLen:  yStride * (alignedHeight + 2*border),
		uvFullLen: uvStride * (uvHeight + 2*uvBorder),
	}
}

func ensureVP9AlignedPlaneCapacity(buf []byte, n int) []byte {
	if cap(buf) < n {
		return mem.NewAligned(n, 32)
	}
	return buf[:n]
}

func vp9AlignTo(v, align int) int {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
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
	d.usePrevFrameMvs = false
	d.prevFrameMvRows = 0
	d.prevFrameMvCols = 0
	if d.prevFrameMvs != nil {
		d.prevFrameMvs = d.prevFrameMvs[:0]
	}
	if d.curFrameMvs != nil {
		d.curFrameMvs = d.curFrameMvs[:0]
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
	d.segMapMiRows = 0
	d.segMapMiCols = 0
	if d.interPredictScratch != nil {
		d.interPredictScratch = d.interPredictScratch[:0]
	}
}

// Close releases internal state and marks the decoder as no longer
// usable. Subsequent calls to Decode return [ErrClosed].
func (d *VP9Decoder) Close() error {
	if d == nil {
		return ErrClosed
	}
	d.Reset()
	if d.vp9LoopFilterPool != nil {
		d.vp9LoopFilterPool.shutdown()
		d.vp9LoopFilterPool = nil
	}
	if d.vp9TilePool != nil {
		d.vp9TilePool.shutdown()
		d.vp9TilePool = nil
	}
	d.closed = true
	return nil
}

// Codec reports the codec this decoder is built for.
func (d *VP9Decoder) Codec() Codec { return CodecVP9 }
