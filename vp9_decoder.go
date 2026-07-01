package govpx

import (
	"errors"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

// VP9Decryptor mirrors libvpx v1.16.0 VPXD_SET_DECRYPTOR for VP9.
// It is an alias of VP8Decryptor because libvpx uses the same
// vpx_decrypt_cb callback shape for both codecs.
type VP9Decryptor = VP8Decryptor

// VP9ExternalFrameBuffer is the application-owned storage returned by
// VP9GetFrameBufferFunc. Data must have at least the requested minSize; Private
// is carried back unchanged to VP9ReleaseFrameBufferFunc.
type VP9ExternalFrameBuffer struct {
	Data    []byte
	Private any
}

// VP9GetFrameBufferFunc mirrors libvpx's vpx_get_frame_buffer_cb_fn_t for VP9.
// The decoder may call it more than once per Decode. The returned data does not
// need to be aligned, but newly allocated or grown buffers should be zeroed.
type VP9GetFrameBufferFunc func(minSize int) (VP9ExternalFrameBuffer, error)

// VP9ReleaseFrameBufferFunc mirrors libvpx's release callback for VP9 external
// frame buffers. Release is a notification; libvpx itself does not reliably
// surface release failures, so govpx follows that behavior with no error return.
type VP9ReleaseFrameBufferFunc func(VP9ExternalFrameBuffer)

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

	// ByteAlignment mirrors libvpx VP9_SET_BYTE_ALIGNMENT. Zero keeps the
	// decoder's legacy allocation behavior; non-zero values must be powers of
	// two in [32, 1024] and align the visible Y/U/V plane starts.
	ByteAlignment int

	// GetFrameBuffer and ReleaseFrameBuffer mirror libvpx's
	// vpx_codec_set_frame_buffer_functions external frame-buffer API for VP9.
	// Both callbacks must be set together, either in options or through
	// SetFrameBufferFunctions before the decoder has initialized on a real
	// stream.
	GetFrameBuffer     VP9GetFrameBufferFunc
	ReleaseFrameBuffer VP9ReleaseFrameBufferFunc

	// SVCSpatialLayerSet enables libvpx-style VP9 spatial-SVC superframe
	// filtering. When set, Decode decodes only frames 0..SVCSpatialLayer from a
	// VP9 superframe, matching VP9_DECODE_SVC_SPATIAL_LAYER. Zero leaves
	// superframes fully decoded.
	SVCSpatialLayerSet bool
	// SVCSpatialLayer is the highest VP9 spatial layer decoded from a
	// superframe when SVCSpatialLayerSet is true. Valid values are 0..7.
	SVCSpatialLayer uint8

	// ErrorConcealment enables libvpx-style concealment for corrupt interframes
	// after a clean frame has initialized references.
	ErrorConcealment bool
	// PostProcessFlags selects individual libvpx-style postprocess filters.
	// Zero disables postprocessing.
	PostProcessFlags PostProcessFlag
	// PostProcessNoiseLevel enables libvpx-style additive luma noise when
	// PostProcessAddNoise is set. Zero disables additive noise; valid range is
	// [0, 16].
	PostProcessNoiseLevel int

	// MaxWidth and MaxHeight cap the accepted frame dimensions.
	// Zero means no cap.
	MaxWidth  int
	MaxHeight int

	// Decryptor, when non-nil, decrypts compressed VP9 packet bytes before
	// superframe-index parsing, uncompressed-header parsing, compressed-header
	// parsing, tile-size reads, and tile token parsing. Mirrors libvpx's
	// VPXD_SET_DECRYPTOR control for VP9. govpx applies it once per Decode at
	// packet entry, matching the existing VP8 decoder contract while keeping
	// the VP9 parse/reconstruct hot path allocation-free after scratch growth.
	// DecryptorState is passed back as the callback's first argument.
	Decryptor      VP9Decryptor
	DecryptorState any

	// RejectResolutionChange, when true, makes Decode reject a coded
	// frame whose dimensions differ from the active stream.
	RejectResolutionChange bool

	// DecodeTileRowSet enables the libvpx VP9D_SET_DECODE_TILE_ROW
	// reconstruction filter. When false (the default), every tile row is
	// reconstructed.
	DecodeTileRowSet bool
	// DecodeTileRow is the active tile row when DecodeTileRowSet is true.
	// Negative values clear the filter (matching libvpx's "less than zero"
	// disable sentinel). Frames whose tile_rows are 1 always reconstruct
	// every tile regardless of the filter.
	DecodeTileRow int
	// DecodeTileColSet enables the libvpx VP9D_SET_DECODE_TILE_COL
	// reconstruction filter. When false (the default), every tile column is
	// reconstructed.
	DecodeTileColSet bool
	// DecodeTileCol is the active tile column when DecodeTileColSet is true.
	// Negative values clear the filter (matching libvpx's "less than zero"
	// disable sentinel). Frames whose tile_cols are 1 always reconstruct
	// every tile regardless of the filter.
	DecodeTileCol int

	// DecoderRowMT mirrors libvpx VP9D_SET_ROW_MT. When true and Threads > 1,
	// the tile-column decode body arms a per-SB-row wavefront sync primitive
	// so future per-row workers can be slotted in without changing the call
	// shape. The wavefront calls are no-ops in the single-goroutine
	// tile-column body but stay byte-identical to libvpx and provide the
	// foundation for actual per-row parallelism. Requires Threads > 1.
	DecoderRowMT bool

	// DecoderLoopFilterOpt mirrors libvpx VP9D_SET_LOOP_FILTER_OPT. When true
	// and Threads > 1, the deblock pass dispatches the U / V planes to the
	// loop-filter worker pool concurrently with the Y plane, matching
	// libvpx's pipelined loop-filter optimisation. When false, the deblock
	// pass runs serially even on a threaded decoder. Requires Threads > 1.
	DecoderLoopFilterOpt bool

	// SkipLoopFilter mirrors libvpx VP9_SET_SKIP_LOOP_FILTER. When true, the
	// decoder parses loop-filter syntax and reconstructs the frame normally but
	// skips the in-loop deblock pass before publishing or refreshing references.
	SkipLoopFilter bool

	// InvertTileDecodeOrder mirrors libvpx VP9_INVERT_TILE_DECODE_ORDER. When
	// true, each tile row is reconstructed from the rightmost tile column back
	// to the leftmost after the tile buffers have been parsed.
	InvertTileDecodeOrder bool
}

// VP9FrameInfo describes one decoded VP9 packet. Quantizer is the raw
// VP9 base qindex in [0, 255]; show-existing packets do not carry a
// quantizer and report zero.
type VP9FrameInfo struct {
	// Width and Height are the coded luma dimensions.
	Width  int
	Height int
	// RenderWidth and RenderHeight are the display dimensions carried by
	// VP9's render-size syntax and exposed by libvpx VP9D_GET_DISPLAY_SIZE.
	RenderWidth  int
	RenderHeight int
	// BitDepth is the VP9 bit depth reported by the uncompressed header.
	BitDepth int

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
	// Corrupted reports that the frame was recovered through decoder error
	// concealment instead of fully decoded from its packet.
	Corrupted bool
}

// ErrVP9NotImplemented is returned by VP9Decoder.Decode for valid VP9
// packets outside the implemented profile 0 reconstruction surface.
var ErrVP9NotImplemented = vpxerrors.ErrVP9NotImplemented

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
	// scratch for one 32x32 transform block; coefTokenCache mirrors
	// libvpx's decode_coefs token_cache scratch beside it.
	planes                [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	dqcoeff               [1024]int16
	coefTokenCache        [1024]uint8
	segIDPredictedScratch uint8

	// The first public reconstruction slice handles intra frames.
	// Unsupported frame classes keep parsing intact but stop before
	// publishing output.
	unsupportedReconstruct bool
	// predictLumaOnly skips chroma (U/V) reconstruction in the inter
	// predictor. Encoder-side motion-search and distortion measurement
	// only consult the luma plane, matching libvpx's
	// vp9_build_inter_predictors_sby fast path in nonrd_pickmode
	// (vp9/encoder/vp9_pickmode.c:2336). Skipping U/V cuts the per-
	// candidate convolution count by ~30-40% for 4:2:0 sources.
	predictLumaOnly bool
	// predictChromaOnly is the companion encoder prepass path for
	// variance-partition color sensitivity: luma prediction has already
	// been produced by choose_partitioning, so chroma_check only needs U/V.
	predictChromaOnly bool
	// predictChromaPlane narrows predictChromaOnly to a single chroma
	// plane (1 = U, 2 = V; 0 = both). libvpx's nonrd picker builds chroma
	// per plane via vp9_build_inter_predictors_sbp: the color-sensitivity
	// rate add only builds the sensitive plane(s)
	// (vp9/encoder/vp9_pickmode.c:2392-2398) and the
	// model_rd_for_sb_y_large UV skip test builds U first and only builds
	// V when U proved skippable (vp9/encoder/vp9_pickmode.c:578-605).
	predictChromaPlane  int8
	frameReady          bool
	lastFrame           Image
	lastInfo            VP9FrameInfo
	lastInfoValid       bool
	initialized         bool
	visibleFrames       int
	frameY              []byte
	frameU              []byte
	frameV              []byte
	frameYFull          []byte
	frameUFull          []byte
	frameVFull          []byte
	frameYOrigin        int
	frameUOrigin        int
	frameVOrigin        int
	frameExternal       *vp9ExternalFrameLease
	lastFrameExternal   *vp9ExternalFrameLease
	frameInternal       *vp9InternalFrameLease
	lastFrameInternal   *vp9InternalFrameLease
	internalFramePool   [common.RefFrames + common.RefsPerFrame + 1]vp9InternalFrameLease
	intraScratch        vp9dec.IntraPredictorScratch
	interPredictScratch []byte
	convolveScratch     dsp.Convolve8Scratch
	vp9DecoderPhaseStatsOptions
	refFrames       [common.RefFrames]vp9ReferenceFrame
	refFramesView   *[common.RefFrames]vp9ReferenceFrame
	prevFrameMvs    []vp9dec.MvRef
	curFrameMvs     []vp9dec.MvRef
	prevFrameMvRows int
	prevFrameMvCols int
	usePrevFrameMvs bool
	counts          vp9dec.FrameCounts

	// width and height carry the most recent decoded frame dimensions.
	// They stay zero until the first successful frame parse.
	width  int
	height int

	decryptedPacket []byte

	vp9LoopFilterPool     *vp9DecoderLoopFilterPool
	vp9TilePool           *vp9DecoderTileWorkerPool
	vp9TileDescs          []vp9DecoderTileDesc
	vp9LoopFilterMasks    []vp9LoopFilterMask
	vp9LoopFilterMaskRows int
	vp9LoopFilterMaskCols int

	// rowMTSync is set on the per-tile-column decode worker when
	// VP9D_SET_ROW_MT is active. parseVP9IntraModeTile and
	// parseVP9InterModeTile call read / write against it so the wavefront
	// primitive is fully exercised. Nil keeps the single-goroutine body
	// byte-identical to libvpx. Mirrors the encoder's vp9RowMTSync field.
	rowMTSync *vp9RowMTSync

	postSource      vp8common.FrameBuffer
	post            vp8common.FrameBuffer
	postModes       []vp8dec.MacroblockMode
	postprocScratch []byte
	postprocState   vp8dec.PostProcessState
}

type vp9ReferenceFrame struct {
	img          Image
	renderWidth  int
	renderHeight int
	bitDepth     int
	y            []byte
	u            []byte
	v            []byte
	external     *vp9ExternalFrameLease
	internal     *vp9InternalFrameLease
	valid        bool
}

type vp9ExternalFrameLease struct {
	buffer   VP9ExternalFrameBuffer
	refs     int
	released bool
}

type vp9InternalFrameLease struct {
	yFull []byte
	uFull []byte
	vFull []byte
	refs  int
}

func (f *vp9ReferenceFrame) store(src Image) {
	f.storeWithRender(src, src.Width, src.Height)
}

func (f *vp9ReferenceFrame) storeWithRender(src Image, renderWidth, renderHeight int) {
	f.storeWithRenderAndBitDepth(src, renderWidth, renderHeight, int(vp9dec.Bits8))
}

func (f *vp9ReferenceFrame) storeWithRenderAndBitDepth(src Image,
	renderWidth, renderHeight, bitDepth int,
) {
	f.y = buffers.EnsureCapacity(f.y, len(src.Y))
	f.u = buffers.EnsureCapacity(f.u, len(src.U))
	f.v = buffers.EnsureCapacity(f.v, len(src.V))
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
	f.renderWidth = renderWidth
	f.renderHeight = renderHeight
	f.bitDepth = bitDepth
	f.external = nil
	f.internal = nil
	f.valid = true
}

func (f *vp9ReferenceFrame) storeExternalWithRenderAndBitDepth(src Image,
	lease *vp9ExternalFrameLease, renderWidth, renderHeight, bitDepth int,
) {
	f.img = Image{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
	f.renderWidth = renderWidth
	f.renderHeight = renderHeight
	f.bitDepth = bitDepth
	f.external = lease
	f.internal = nil
	f.valid = true
}

func (f *vp9ReferenceFrame) storeInternalWithRenderAndBitDepth(src Image,
	lease *vp9InternalFrameLease, renderWidth, renderHeight, bitDepth int,
) {
	f.img = Image{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
	f.renderWidth = renderWidth
	f.renderHeight = renderHeight
	f.bitDepth = bitDepth
	f.external = nil
	f.internal = lease
	f.valid = true
}

func (f *vp9ReferenceFrame) renderSize() (int, int) {
	if f.renderWidth > 0 && f.renderHeight > 0 {
		return f.renderWidth, f.renderHeight
	}
	return f.img.Width, f.img.Height
}

func (f *vp9ReferenceFrame) bitDepthValue() int {
	if f.bitDepth > 0 {
		return f.bitDepth
	}
	return int(vp9dec.Bits8)
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
		if opts.DecoderRowMT && d.vp9TilePool != nil {
			d.vp9TilePool.armRowMT()
		}
	}
	return d, nil
}

func validateVP9DecoderOptions(opts VP9DecoderOptions) error {
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if err := validateVP9ByteAlignment(opts.ByteAlignment); err != nil {
		return err
	}
	if (opts.GetFrameBuffer == nil) != (opts.ReleaseFrameBuffer == nil) {
		return ErrInvalidConfig
	}
	if opts.SVCSpatialLayerSet && opts.SVCSpatialLayer >= VP9RTPMaxSpatialLayers {
		return ErrInvalidConfig
	}
	if opts.PostProcessFlags&^allPostProcessFlags != 0 {
		return ErrInvalidConfig
	}
	if uint(opts.PostProcessNoiseLevel) > 16 {
		return ErrInvalidConfig
	}
	if opts.PostProcessNoiseLevel > 0 &&
		opts.effectivePostProcessFlags()&PostProcessAddNoise == 0 {
		return ErrInvalidConfig
	}
	if opts.MaxWidth < 0 || opts.MaxHeight < 0 {
		return ErrInvalidConfig
	}
	if err := validateVP9DecodeTileFilter(opts.DecodeTileRowSet,
		opts.DecodeTileRow); err != nil {
		return err
	}
	if err := validateVP9DecodeTileFilter(opts.DecodeTileColSet,
		opts.DecodeTileCol); err != nil {
		return err
	}
	if (opts.DecoderRowMT || opts.DecoderLoopFilterOpt) && opts.Threads <= 1 {
		return ErrInvalidConfig
	}
	return nil
}

// validateVP9DecodeTileFilter mirrors libvpx's tile-decode control range:
// values must fit in [-1, vp9MaxTileLog2Bound] so the filter cannot exceed
// the maximum tile dimension permitted by the bitstream syntax.
func validateVP9DecodeTileFilter(set bool, value int) error {
	if !set {
		return nil
	}
	if value < -1 || value > vp9DecoderMaxTileFilter {
		return ErrInvalidConfig
	}
	return nil
}

// vp9DecoderMaxTileFilter caps DecodeTileRow/DecodeTileCol at the largest
// per-frame tile dimension libvpx can emit. VP9's log2 tile fields are
// 2 bits for rows (1<<3-1 = 7 is the spec ceiling) and up to 6 for columns
// (1<<6-1 = 63). Use the spec ceiling so the setter rejects clearly
// out-of-range values without locking out future expansions.
const vp9DecoderMaxTileFilter = 63

func validateVP9ByteAlignment(alignment int) error {
	if alignment == 0 {
		return nil
	}
	if alignment < 32 || alignment > 1024 || alignment&(alignment-1) != 0 {
		return ErrInvalidConfig
	}
	return nil
}

func (opts VP9DecoderOptions) effectivePostProcessFlags() PostProcessFlag {
	return opts.PostProcessFlags
}

func (opts VP9DecoderOptions) effectiveErrorConcealment() bool {
	return opts.ErrorConcealment
}

func (d *VP9Decoder) decryptVP9Packet(packet []byte) []byte {
	if d.opts.Decryptor == nil || len(packet) == 0 {
		return packet
	}
	d.decryptedPacket = buffers.EnsureLen(d.decryptedPacket, len(packet))
	d.opts.Decryptor(d.opts.DecryptorState, packet, d.decryptedPacket, len(packet))
	return d.decryptedPacket
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

// SetDecodeTileRow mirrors libvpx's VP9D_SET_DECODE_TILE_ROW control.
// Subsequent Decode calls only reconstruct the tile at the configured row
// (combined with the DecodeTileCol filter). Values < 0 clear the filter so
// the decoder reconstructs every tile row.
func (d *VP9Decoder) SetDecodeTileRow(row int) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if row > vp9DecoderMaxTileFilter {
		return ErrInvalidConfig
	}
	if row < 0 {
		d.opts.DecodeTileRowSet = false
		d.opts.DecodeTileRow = 0
		return nil
	}
	d.opts.DecodeTileRowSet = true
	d.opts.DecodeTileRow = row
	return nil
}

// SetDecodeTileCol mirrors libvpx's VP9D_SET_DECODE_TILE_COL control.
// Subsequent Decode calls only reconstruct the tile at the configured
// column (combined with the DecodeTileRow filter). Values < 0 clear the
// filter so the decoder reconstructs every tile column.
func (d *VP9Decoder) SetDecodeTileCol(col int) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if col > vp9DecoderMaxTileFilter {
		return ErrInvalidConfig
	}
	if col < 0 {
		d.opts.DecodeTileColSet = false
		d.opts.DecodeTileCol = 0
		return nil
	}
	d.opts.DecodeTileColSet = true
	d.opts.DecodeTileCol = col
	return nil
}

// vp9TileFilterMasksTile reports whether the configured DecodeTileRow /
// DecodeTileCol filter would mask the given (row, col) for a frame whose
// tile grid has tileRows × tileCols tiles. Returns true only when at least
// one filter is set, the frame has multiple tiles in the filtered axis,
// and the candidate tile falls outside the requested selection.
func (d *VP9Decoder) vp9TileFilterMasksTile(row, col, tileRows, tileCols int) bool {
	if d == nil {
		return false
	}
	if d.opts.DecodeTileRowSet && tileRows > 1 && row != d.opts.DecodeTileRow {
		return true
	}
	if d.opts.DecodeTileColSet && tileCols > 1 && col != d.opts.DecodeTileCol {
		return true
	}
	return false
}

// vp9TileFilterActive reports whether either of the per-tile decode
// filters has been configured.
func (d *VP9Decoder) vp9TileFilterActive() bool {
	if d == nil {
		return false
	}
	return d.opts.DecodeTileRowSet || d.opts.DecodeTileColSet
}

// SetRowMT mirrors libvpx VP9D_SET_ROW_MT. When enabled, the tile-column
// decode body arms a per-SB-row wavefront sync primitive so future per-row
// workers can be slotted in. Requires the decoder to have been constructed
// with Threads > 1; otherwise ErrInvalidConfig is returned.
func (d *VP9Decoder) SetRowMT(enabled bool) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if enabled && d.opts.Threads <= 1 {
		return ErrInvalidConfig
	}
	d.opts.DecoderRowMT = enabled
	if d.vp9TilePool != nil {
		if enabled {
			d.vp9TilePool.armRowMT()
		} else {
			d.vp9TilePool.releaseRowMTSync()
		}
	}
	return nil
}

// Active-map decoder controls: libvpx exposes the active-map feature
// only on the VP9 encoder side via VP8E_SET_ACTIVEMAP. The VP9 decoder
// (vp8dx.h) has no VP9D_SET_ACTIVE_MAP or equivalent — active maps
// describe which encoder superblocks to skip and are not transmitted
// in the VP9 bitstream. govpx therefore mirrors libvpx exactly: see
// VP9Encoder.SetActiveMap on the encoder side for the supported
// surface; the VP9Decoder does not expose a decoder-side active-map
// control because libvpx does not expose one.

// SetLoopFilterOpt mirrors libvpx VP9D_SET_LOOP_FILTER_OPT. When enabled, the
// deblock pass dispatches the U / V planes to the loop-filter worker pool
// concurrently with Y, matching libvpx's pipelined loop-filter optimisation.
// When disabled the deblock pass runs serially even on a threaded decoder.
// Requires the decoder to have been constructed with Threads > 1; otherwise
// ErrInvalidConfig is returned.
func (d *VP9Decoder) SetLoopFilterOpt(enabled bool) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if enabled && d.opts.Threads <= 1 {
		return ErrInvalidConfig
	}
	d.opts.DecoderLoopFilterOpt = enabled
	return nil
}

// SetSkipLoopFilter mirrors libvpx VP9_SET_SKIP_LOOP_FILTER. When enabled,
// subsequent frames skip the in-loop deblock pass even if their headers carry a
// non-zero loop-filter level.
func (d *VP9Decoder) SetSkipLoopFilter(enabled bool) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	d.opts.SkipLoopFilter = enabled
	return nil
}

// SetInvertTileDecodeOrder mirrors libvpx VP9_INVERT_TILE_DECODE_ORDER. When
// enabled, subsequent multi-tile frames process each tile row from the rightmost
// tile column to the leftmost tile column.
func (d *VP9Decoder) SetInvertTileDecodeOrder(enabled bool) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	d.opts.InvertTileDecodeOrder = enabled
	return nil
}

// SetByteAlignment mirrors libvpx VP9_SET_BYTE_ALIGNMENT. A value of zero
// keeps the decoder's legacy allocation behavior; non-zero values must be
// powers of two in [32, 1024] and apply to subsequently prepared output
// frames.
func (d *VP9Decoder) SetByteAlignment(alignment int) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if err := validateVP9ByteAlignment(alignment); err != nil {
		return err
	}
	d.opts.ByteAlignment = alignment
	return nil
}

// SetFrameBufferFunctions mirrors libvpx's
// vpx_codec_set_frame_buffer_functions for VP9. Both callbacks must be
// non-nil, and the control must be applied before the decoder initializes on a
// stream. Call Reset to return the decoder to a cold-start state before
// replacing callbacks.
func (d *VP9Decoder) SetFrameBufferFunctions(get VP9GetFrameBufferFunc,
	release VP9ReleaseFrameBufferFunc,
) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if get == nil || release == nil {
		return ErrInvalidConfig
	}
	if d.vp9FrameBufferCallbacksLocked() {
		return ErrInvalidConfig
	}
	d.opts.GetFrameBuffer = get
	d.opts.ReleaseFrameBuffer = release
	return nil
}

func (d *VP9Decoder) vp9FrameBufferCallbacksLocked() bool {
	if d == nil {
		return true
	}
	return d.initialized || d.lastHeaderValid || d.lastInfoValid ||
		d.width != 0 || d.height != 0 || d.visibleFrames != 0 ||
		d.frameReady
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
	packet = d.decryptVP9Packet(packet)
	return d.decodeVP9PacketWithPTS(packet, pts)
}

func (d *VP9Decoder) decodeVP9PacketWithPTS(packet []byte, pts uint64) error {
	sf, err := bitstream.ParseSuperframe(packet)
	if err != nil {
		if len(packet) == 0 && d.opts.effectiveErrorConcealment() &&
			d.canConcealVP9Frame() {
			return d.concealVP9Frame(nil, VP9FrameInfo{
				ShowFrame: true,
				PTS:       pts,
			}, pts)
		}
		return err
	}
	if sf.Count == 0 {
		return d.decodeVP9FrameWithPTS(packet, pts)
	}
	frameCount := d.vp9SVCFrameCount(sf.Count)
	for i := range frameCount {
		if err := d.decodeVP9FrameWithPTS(sf.Frames[i], pts); err != nil {
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
	err := d.decodeVP9FrameWithPTSStrict(packet, pts)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrVP9NotImplemented) || errors.Is(err, ErrFrameRejected) ||
		!d.opts.effectiveErrorConcealment() || !d.canConcealVP9Frame() {
		return err
	}
	hdr, uncSize, err := d.readVP9UncompressedHeader(packet)
	if err != nil {
		if len(packet) == 0 {
			return d.concealVP9Frame(nil, VP9FrameInfo{
				ShowFrame: true,
				PTS:       pts,
			}, pts)
		}
		return err
	}
	info, infoErr := d.vp9FrameInfoFromHeader(&hdr, pts)
	if infoErr != nil || hdr.FrameType == common.KeyFrame || hdr.IntraOnly ||
		hdr.ShowExistingFrame {
		if infoErr != nil {
			return infoErr
		}
		return err
	}
	if uncSize > len(packet) {
		return err
	}
	return d.concealVP9Frame(&hdr, info, pts)
}

func (d *VP9Decoder) decodeVP9FrameWithPTSStrict(packet []byte, pts uint64) (retErr error) {
	defer func() {
		if retErr != nil {
			d.releaseVP9ActiveExternalFrame()
			d.releaseVP9ActiveInternalFrame()
		}
	}()
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
		output, err := d.outputVP9FrameImage(&hdr, info, d.lastFrame)
		if err != nil {
			return err
		}
		d.lastFrame = output
		if d.opts.effectivePostProcessFlags() != 0 {
			d.replaceVP9LastFrameExternal(nil)
			if !d.transferVP9ActiveFrameToLastIfAliased(output) {
				d.replaceVP9LastFrameInternal(nil)
			}
		}
		d.finishVP9FrameInfo(info)
		return nil
	}

	compEnd := uncSize + int(hdr.FirstPartitionSize)
	if compEnd > len(packet) {
		return ErrInvalidVP9Data
	}
	if !vp9dec.SupportedOutputFormat(&hdr) {
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
		d.counts = vp9dec.FrameCounts{}
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
		CompoundRefAllowed:   vp9dec.CompoundReferenceAllowedForHeader(&hdr),
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
		d.unsupportedReconstruct = !vp9dec.SupportedOutputFormat(&hdr)
		if !d.unsupportedReconstruct {
			if err := d.prepareVP9OutputFrame(int(hdr.Width), int(hdr.Height)); err != nil {
				return err
			}
		}
		if err := d.parseVP9IntraModeTiles(packet[compEnd:], &hdr, compHeader); err != nil {
			return err
		}
	} else {
		d.unsupportedReconstruct = !vp9dec.SupportedOutputFormat(&hdr)
		if !d.unsupportedReconstruct {
			if err := d.prepareVP9OutputFrame(int(hdr.Width), int(hdr.Height)); err != nil {
				return err
			}
		}
		d.prepareVP9CurrentFrameMvs(&hdr)
		if err := d.parseVP9InterModeTiles(packet[compEnd:], &hdr, compHeader); err != nil {
			return err
		}
	}
	if !d.unsupportedReconstruct && hdr.Loopfilter.FilterLevel != 0 &&
		!d.opts.SkipLoopFilter {
		if !d.applyVP9LoopFilter(&hdr) {
			d.markVP9Unsupported()
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
			output, err := d.outputVP9FrameImage(&hdr, info, d.lastFrame)
			if err != nil {
				return err
			}
			d.lastFrame = output
			d.frameReady = true
			if d.opts.effectivePostProcessFlags() == 0 && d.frameExternal != nil {
				d.replaceVP9LastFrameExternal(d.frameExternal)
				d.frameExternal = nil
			} else {
				d.releaseVP9ActiveExternalFrame()
				d.replaceVP9LastFrameExternal(nil)
			}
			if !d.transferVP9ActiveFrameToLastIfAliased(output) {
				d.releaseVP9ActiveInternalFrame()
				d.replaceVP9LastFrameInternal(nil)
			}
		} else {
			d.releaseVP9ActiveExternalFrame()
			d.releaseVP9ActiveInternalFrame()
		}
		d.finishVP9FrameInfo(info)
		return nil
	}
	return ErrVP9NotImplemented
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
	packet = d.decryptVP9Packet(packet)
	sf, err := bitstream.ParseSuperframe(packet)
	if err != nil {
		if len(packet) == 0 && d.opts.effectiveErrorConcealment() &&
			d.canConcealVP9Frame() {
			if err := d.concealVP9Frame(nil, VP9FrameInfo{
				ShowFrame: true,
				PTS:       pts,
			}, pts); err != nil {
				return VP9FrameInfo{}, err
			}
			info := d.lastInfo
			if info.ShowFrame {
				if !dst.validForEncode(info.Width, info.Height) {
					return VP9FrameInfo{}, ErrInvalidConfig
				}
				copyVP9ImageToPublic(dst, d.lastFrame)
			}
			return info, nil
		}
		return VP9FrameInfo{}, err
	}
	if sf.Count != 0 {
		frameCount := d.vp9SVCFrameCount(sf.Count)
		for i := range frameCount {
			if err := d.decodeVP9FrameWithPTS(sf.Frames[i], pts); err != nil {
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
	if err := d.decodeVP9PacketWithPTS(packet, pts); err != nil {
		return VP9FrameInfo{}, err
	}
	if d.lastInfoValid {
		info = d.lastInfo
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
		info.RenderWidth, info.RenderHeight = d.refFrames[slot].renderSize()
		info.BitDepth = d.refFrames[slot].bitDepthValue()
		info.ShowFrame = true
		return info, nil
	}
	info.Width = int(hdr.Width)
	info.Height = int(hdr.Height)
	info.RenderWidth, info.RenderHeight = vp9dec.HeaderRenderSize(hdr)
	info.BitDepth = int(hdr.BitDepthColor.BitDepth)
	return info, nil
}

func (d *VP9Decoder) finishVP9FrameInfo(info VP9FrameInfo) {
	d.lastInfo = info
	d.lastInfoValid = true
	d.initialized = true
	if info.ShowFrame {
		d.visibleFrames++
	}
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
	return vp9dec.SupportedOutputFormat(hdr)
}

func (d *VP9Decoder) decodeVP9ShowExistingFrame(hdr *vp9dec.UncompressedHeader) error {
	slot := int(hdr.ExistingFrameSlot)
	if slot >= len(d.refFrames) || !d.refFrames[slot].valid {
		return ErrInvalidVP9Data
	}
	ref := &d.refFrames[slot]
	if err := d.publishVP9ReferenceFrame(ref); err != nil {
		return err
	}
	d.width = ref.img.Width
	d.height = ref.img.Height
	d.frameReady = true
	return nil
}

func (d *VP9Decoder) canConcealVP9Frame() bool {
	if !d.initialized || d.width <= 0 || d.height <= 0 {
		return false
	}
	for i := range d.refFrames {
		if d.refFrames[i].valid {
			return true
		}
	}
	return false
}

func (d *VP9Decoder) concealVP9Frame(hdr *vp9dec.UncompressedHeader,
	info VP9FrameInfo, pts uint64,
) error {
	ref := d.vp9ConcealmentReference(hdr)
	if ref == nil {
		return ErrInvalidVP9Data
	}
	info.Width = ref.img.Width
	info.Height = ref.img.Height
	info.RenderWidth, info.RenderHeight = ref.renderSize()
	info.BitDepth = ref.bitDepthValue()
	info.KeyFrame = false
	info.ShowFrame = true
	info.ShowExistingFrame = false
	info.ExistingFrameSlot = 0
	info.PTS = pts
	info.Corrupted = true
	if err := d.publishVP9ReferenceFrame(ref); err != nil {
		return err
	}
	output, err := d.outputVP9FrameImage(hdr, info, d.lastFrame)
	if err != nil {
		return err
	}
	d.lastFrame = output
	if d.opts.effectivePostProcessFlags() != 0 {
		d.replaceVP9LastFrameExternal(nil)
		if !d.transferVP9ActiveFrameToLastIfAliased(output) {
			d.replaceVP9LastFrameInternal(nil)
		}
	}
	d.width = ref.img.Width
	d.height = ref.img.Height
	d.frameReady = true
	d.finishVP9FrameInfo(info)
	return nil
}

func (d *VP9Decoder) publishVP9ReferenceFrame(ref *vp9ReferenceFrame) error {
	if ref == nil {
		return ErrInvalidVP9Data
	}
	if d.opts.ByteAlignment == 0 ||
		vp9ImagePlanesAligned(ref.img, d.opts.ByteAlignment) {
		d.lastFrame = ref.img
		d.retainVP9LastFrameExternal(ref.external)
		d.retainVP9LastFrameInternal(ref.internal)
		return nil
	}
	output, err := d.alignVP9PublishedFrame(ref.img)
	if err != nil {
		return err
	}
	d.lastFrame = output
	d.replaceVP9LastFrameExternal(nil)
	if !d.transferVP9ActiveFrameToLastIfAliased(output) {
		d.replaceVP9LastFrameInternal(nil)
	}
	return nil
}

func (d *VP9Decoder) alignVP9PublishedFrame(src Image) (Image, error) {
	if d == nil || d.opts.ByteAlignment == 0 {
		return src, nil
	}
	if err := d.prepareVP9InternalOutputFrame(src.Width, src.Height); err != nil {
		return Image{}, err
	}
	copyVP9ImageToPublic(&d.lastFrame, src)
	return d.lastFrame, nil
}

func (d *VP9Decoder) vp9ConcealmentReference(
	hdr *vp9dec.UncompressedHeader,
) *vp9ReferenceFrame {
	if hdr != nil {
		for i := range common.RefsPerFrame {
			slot := int(hdr.InterRef.RefIndex[i])
			if slot >= 0 && slot < len(d.refFrames) && d.refFrames[slot].valid {
				return &d.refFrames[slot]
			}
		}
	}
	for i := range d.refFrames {
		if d.refFrames[i].valid {
			return &d.refFrames[i]
		}
	}
	return nil
}

func (d *VP9Decoder) refreshVP9ReferenceFrames(hdr *vp9dec.UncompressedHeader) {
	flags := hdr.RefreshFrameFlags
	renderWidth, renderHeight := vp9dec.HeaderRenderSize(hdr)
	for slot := range d.refFrames {
		if flags&(1<<uint(slot)) != 0 {
			d.releaseVP9ReferenceFrame(slot)
			if d.frameExternal != nil {
				d.refFrames[slot].storeExternalWithRenderAndBitDepth(d.lastFrame,
					d.retainVP9ExternalFrame(d.frameExternal), renderWidth,
					renderHeight, int(hdr.BitDepthColor.BitDepth))
			} else if d.frameInternal != nil &&
				vp9ImageAliasesPlane(d.lastFrame, d.frameY) {
				d.refFrames[slot].storeInternalWithRenderAndBitDepth(d.lastFrame,
					d.retainVP9InternalFrame(d.frameInternal), renderWidth,
					renderHeight, int(hdr.BitDepthColor.BitDepth))
			} else {
				d.refFrames[slot].storeWithRenderAndBitDepth(d.lastFrame,
					renderWidth, renderHeight, int(hdr.BitDepthColor.BitDepth))
			}
		}
	}
}

func (d *VP9Decoder) prepareVP9CurrentFrameMvs(hdr *vp9dec.UncompressedHeader) {
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	need := miRows * miCols
	d.curFrameMvs = buffers.EnsureLenZeroed(d.curFrameMvs, need)
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
	buffers.CopyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	buffers.CopyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	buffers.CopyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
}

func vp9ImageAliasesPlane(img Image, plane []byte) bool {
	if len(img.Y) == 0 || len(plane) == 0 {
		return len(img.Y) == 0 && len(plane) == 0
	}
	return &img.Y[0] == &plane[0]
}

func (d *VP9Decoder) prepareVP9OutputFrame(width, height int) error {
	return d.prepareVP9OutputFrameWithExternal(width, height, true)
}

func (d *VP9Decoder) prepareVP9InternalOutputFrame(width, height int) error {
	return d.prepareVP9OutputFrameWithExternal(width, height, false)
}

func (d *VP9Decoder) prepareVP9OutputFrameWithExternal(width, height int,
	allowExternal bool,
) error {
	d.replaceVP9LastFrameExternal(nil)
	d.replaceVP9LastFrameInternal(nil)
	d.releaseVP9ActiveExternalFrame()
	d.releaseVP9ActiveInternalFrame()
	if allowExternal && d.opts.GetFrameBuffer != nil {
		return d.prepareVP9ExternalOutputFrame(width, height)
	}

	layout := common.NewDecoderFrameLayout(width, height, d.opts.ByteAlignment)
	align := common.DecoderFrameAlignment(d.opts.ByteAlignment)
	lease := d.acquireVP9InternalFrame()
	if lease == nil {
		return ErrInvalidConfig
	}
	lease.yFull = buffers.EnsureAlignedCapacity(lease.yFull,
		layout.YFullLen, align)
	lease.uFull = buffers.EnsureAlignedCapacity(lease.uFull,
		layout.UVFullLen, align)
	lease.vFull = buffers.EnsureAlignedCapacity(lease.vFull,
		layout.UVFullLen, align)
	d.frameInternal = lease
	d.frameYFull = lease.yFull
	d.frameUFull = lease.uFull
	d.frameVFull = lease.vFull
	layout = common.NewDecoderFrameLayoutForPlanes(width, height,
		d.opts.ByteAlignment, d.frameYFull, d.frameUFull, d.frameVFull)
	d.installVP9OutputFrameLayout(width, height, layout)
	return nil
}

func (d *VP9Decoder) prepareVP9ExternalOutputFrame(width, height int) error {
	layout := common.NewDecoderFrameLayout(width, height, d.opts.ByteAlignment)
	minSize := layout.YFullLen + 2*layout.UVFullLen + 31
	buffer, err := d.opts.GetFrameBuffer(minSize)
	if err != nil {
		return ErrInvalidConfig
	}
	if len(buffer.Data) < minSize {
		return ErrInvalidConfig
	}
	baseOff := buffers.AlignmentPadding(buffer.Data, 32)
	if len(buffer.Data)-baseOff < layout.YFullLen+2*layout.UVFullLen {
		return ErrInvalidConfig
	}
	base := buffer.Data[baseOff:]
	d.frameYFull = base[:layout.YFullLen]
	d.frameUFull = base[layout.YFullLen : layout.YFullLen+layout.UVFullLen]
	d.frameVFull = base[layout.YFullLen+layout.UVFullLen : layout.YFullLen+2*layout.UVFullLen]
	layout = common.NewDecoderFrameLayoutForPlanes(width, height,
		d.opts.ByteAlignment, d.frameYFull, d.frameUFull, d.frameVFull)
	d.frameExternal = &vp9ExternalFrameLease{buffer: buffer, refs: 1}
	d.installVP9OutputFrameLayout(width, height, layout)
	return nil
}

func (d *VP9Decoder) installVP9OutputFrameLayout(width, height int,
	layout common.FrameLayout,
) {
	buffers.Fill(d.frameYFull, 128)
	buffers.Fill(d.frameUFull, 128)
	buffers.Fill(d.frameVFull, 128)
	d.frameYOrigin = layout.YOrigin
	d.frameUOrigin = layout.UOrigin
	d.frameVOrigin = layout.VOrigin
	d.frameY = d.frameYFull[layout.YOrigin:]
	d.frameU = d.frameUFull[layout.UOrigin:]
	d.frameV = d.frameVFull[layout.VOrigin:]
	d.lastFrame = Image{
		Width:   width,
		Height:  height,
		Y:       d.frameY,
		U:       d.frameU,
		V:       d.frameV,
		YStride: layout.YStride,
		UStride: layout.UVStride,
		VStride: layout.UVStride,
	}
}

func (d *VP9Decoder) retainVP9ExternalFrame(
	lease *vp9ExternalFrameLease,
) *vp9ExternalFrameLease {
	if lease == nil || lease.released {
		return nil
	}
	lease.refs++
	return lease
}

func (d *VP9Decoder) releaseVP9ExternalFrame(lease *vp9ExternalFrameLease) {
	if lease == nil || lease.released {
		return
	}
	lease.refs--
	if lease.refs > 0 {
		return
	}
	lease.released = true
	if d != nil && d.opts.ReleaseFrameBuffer != nil {
		d.opts.ReleaseFrameBuffer(lease.buffer)
	}
}

func (d *VP9Decoder) acquireVP9InternalFrame() *vp9InternalFrameLease {
	for i := range d.internalFramePool {
		lease := &d.internalFramePool[i]
		if lease.refs == 0 {
			lease.refs = 1
			return lease
		}
	}
	return nil
}

func (d *VP9Decoder) retainVP9InternalFrame(
	lease *vp9InternalFrameLease,
) *vp9InternalFrameLease {
	if lease == nil || lease.refs <= 0 {
		return nil
	}
	lease.refs++
	return lease
}

func (d *VP9Decoder) releaseVP9InternalFrame(lease *vp9InternalFrameLease) {
	if lease == nil || lease.refs <= 0 {
		return
	}
	lease.refs--
}

func (d *VP9Decoder) releaseVP9ActiveInternalFrame() {
	if d == nil || d.frameInternal == nil {
		return
	}
	d.releaseVP9InternalFrame(d.frameInternal)
	d.frameInternal = nil
}

func (d *VP9Decoder) replaceVP9LastFrameInternal(
	lease *vp9InternalFrameLease,
) {
	if d == nil {
		return
	}
	if d.lastFrameInternal == lease {
		return
	}
	d.releaseVP9InternalFrame(d.lastFrameInternal)
	d.lastFrameInternal = lease
}

func (d *VP9Decoder) retainVP9LastFrameInternal(
	lease *vp9InternalFrameLease,
) {
	if d == nil || d.lastFrameInternal == lease {
		return
	}
	d.replaceVP9LastFrameInternal(d.retainVP9InternalFrame(lease))
}

func (d *VP9Decoder) releaseVP9ActiveExternalFrame() {
	if d == nil || d.frameExternal == nil {
		return
	}
	d.releaseVP9ExternalFrame(d.frameExternal)
	d.frameExternal = nil
}

func (d *VP9Decoder) transferVP9ActiveFrameToLastIfAliased(output Image) bool {
	if d == nil || d.frameInternal == nil || !vp9ImageAliasesPlane(output, d.frameY) {
		return false
	}
	d.replaceVP9LastFrameInternal(d.frameInternal)
	d.frameInternal = nil
	return true
}

func (d *VP9Decoder) replaceVP9LastFrameExternal(
	lease *vp9ExternalFrameLease,
) {
	if d == nil {
		return
	}
	if d.lastFrameExternal == lease {
		return
	}
	d.releaseVP9ExternalFrame(d.lastFrameExternal)
	d.lastFrameExternal = lease
}

func (d *VP9Decoder) retainVP9LastFrameExternal(lease *vp9ExternalFrameLease) {
	if d == nil || d.lastFrameExternal == lease {
		return
	}
	d.replaceVP9LastFrameExternal(d.retainVP9ExternalFrame(lease))
}

func (d *VP9Decoder) releaseVP9ReferenceExternalFrame(slot int) {
	if d == nil || slot < 0 || slot >= len(d.refFrames) {
		return
	}
	d.releaseVP9ExternalFrame(d.refFrames[slot].external)
	d.refFrames[slot].external = nil
}

func (d *VP9Decoder) releaseVP9ReferenceInternalFrame(slot int) {
	if d == nil || slot < 0 || slot >= len(d.refFrames) {
		return
	}
	d.releaseVP9InternalFrame(d.refFrames[slot].internal)
	d.refFrames[slot].internal = nil
}

func (d *VP9Decoder) releaseVP9ReferenceFrame(slot int) {
	d.releaseVP9ReferenceExternalFrame(slot)
	d.releaseVP9ReferenceInternalFrame(slot)
}

func (d *VP9Decoder) outputVP9FrameImage(hdr *vp9dec.UncompressedHeader,
	info VP9FrameInfo, src Image,
) (Image, error) {
	flags := d.opts.effectivePostProcessFlags()
	if flags == 0 {
		return src, nil
	}
	if err := d.prepareVP9PostProcess(src.Width, src.Height); err != nil {
		return Image{}, err
	}
	copyVP9ImageToPostSource(&d.postSource.Img, src)
	d.postSource.ExtendBorders()
	rows := (src.Height + 15) >> 4
	cols := (src.Width + 15) >> 4
	d.prepareVP9PostProcessModes(rows, cols)
	filterLevel := uint8(0)
	baseQIndex := info.Quantizer
	if hdr != nil {
		filterLevel = hdr.Loopfilter.FilterLevel
		baseQIndex = int(hdr.Quant.BaseQindex)
	}
	opts := vp8dec.PostProcessOptions{
		Deblock:         flags&PostProcessDeblock != 0,
		Demacroblock:    flags&PostProcessDemacroblock != 0,
		MFQE:            flags&PostProcessMFQE != 0,
		AddNoise:        flags&PostProcessAddNoise != 0 && d.opts.PostProcessNoiseLevel > 0,
		DeblockingLevel: vp8dec.DefaultPostProcessDeblockingLevel,
		NoiseLevel:      d.opts.PostProcessNoiseLevel,
		BaseQIndex:      baseQIndex,
		CurrentFrame:    d.visibleFrames + 1,
		KeyFrame:        info.KeyFrame,
		VP9:             true,
	}
	if opts.MFQE && len(d.miGrid) > 0 {
		// Use the libvpx-faithful VP9 SB-partition walker so MFQE
		// uses the VP9-specific decision (libvpx vp9_mfqe.c:198) and
		// per-block kernel (vp9_mfqe.c:159), not the VP8 mfqe_block
		// kernel (which has totally different SAD / variance / weight
		// thresholds and accepts intra and skipped blocks).
		opts.MFQEOverride = d.vp9MFQEFaithfulWalker
	}
	if err := vp8dec.ApplyPostProcessWithOptions(&d.postSource.Img, &d.post,
		rows, cols, d.postModes, filterLevel, d.postprocScratch, opts,
		&d.postprocState); err != nil {
		return Image{}, ErrInvalidVP9Data
	}
	output := publicImageFromVP8(&d.post.Img)
	if d.opts.ByteAlignment > 0 {
		if err := d.prepareVP9InternalOutputFrame(output.Width, output.Height); err != nil {
			return Image{}, err
		}
		copyVP9ImageToPublic(&d.lastFrame, output)
		return d.lastFrame, nil
	}
	return output, nil
}

func copyVP9ImageToPostSource(dst *vp8common.Image, src Image) {
	copyVP9PostPlane(dst.Y, dst.YStride, dst.CodedWidth, dst.CodedHeight,
		src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	codedUVWidth := (dst.CodedWidth + 1) >> 1
	codedUVHeight := (dst.CodedHeight + 1) >> 1
	copyVP9PostPlane(dst.U, dst.UStride, codedUVWidth, codedUVHeight,
		src.U, src.UStride, uvWidth, uvHeight)
	copyVP9PostPlane(dst.V, dst.VStride, codedUVWidth, codedUVHeight,
		src.V, src.VStride, uvWidth, uvHeight)
}

func copyVP9PostPlane(dst []byte, dstStride, codedWidth, codedHeight int,
	src []byte, srcStride, width, height int,
) {
	for y := range height {
		dstRow := dst[y*dstStride:]
		copy(dstRow[:width], src[y*srcStride:y*srcStride+width])
		if codedWidth > width {
			buffers.Fill(dstRow[width:codedWidth], dstRow[width-1])
		}
	}
	if height == 0 {
		return
	}
	lastRow := dst[(height-1)*dstStride:]
	for y := height; y < codedHeight; y++ {
		copy(dst[y*dstStride:y*dstStride+codedWidth], lastRow[:codedWidth])
	}
}

func (d *VP9Decoder) prepareVP9PostProcess(width, height int) error {
	if err := d.postSource.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidVP9Data
	}
	if err := d.post.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidVP9Data
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	count := rows * cols
	d.postModes = buffers.EnsureLen(d.postModes, count)
	scratchLen := cols * 24
	d.postprocScratch = buffers.EnsureLen(d.postprocScratch, scratchLen)
	flags := d.opts.effectivePostProcessFlags()
	if flags&PostProcessMFQE != 0 {
		if err := d.postprocState.EnsureMFQE(width, height); err != nil {
			return ErrInvalidVP9Data
		}
	}
	if flags&PostProcessAddNoise != 0 && d.opts.PostProcessNoiseLevel > 0 {
		d.postprocState.EnsureNoise(width)
	}
	return nil
}

func (d *VP9Decoder) prepareVP9PostProcessModes(rows, cols int) {
	for i := range d.postModes {
		d.postModes[i] = vp8dec.MacroblockMode{}
	}
	miCols := (d.lastFrame.Width + 7) >> 3
	miRows := (d.lastFrame.Height + 7) >> 3
	for mbRow := range rows {
		for mbCol := range cols {
			mode := &d.postModes[mbRow*cols+mbCol]
			mode.MBSkipCoeff = true
			for subRow := range 2 {
				miRow := mbRow*2 + subRow
				if miRow >= miRows {
					continue
				}
				for subCol := range 2 {
					miCol := mbCol*2 + subCol
					if miCol >= miCols {
						continue
					}
					idx := miRow*miCols + miCol
					if idx >= len(d.miGrid) {
						continue
					}
					mi := &d.miGrid[idx]
					if mi.Skip == 0 {
						mode.MBSkipCoeff = false
					}
					if mi.RefFrame[0] > vp9dec.IntraFrame {
						mode.RefFrame = vp8common.LastFrame
						mode.Mode = vp8common.ZeroMV
						mode.MV = vp8dec.MotionVector{
							Row: mi.Mv[0].Row,
							Col: mi.Mv[0].Col,
						}
					}
				}
			}
		}
	}
}

func vp9ImagePlanesAligned(img Image, align int) bool {
	if align <= 1 {
		return true
	}
	return buffers.ByteSliceAligned(img.Y, align) &&
		buffers.ByteSliceAligned(img.U, align) &&
		buffers.ByteSliceAligned(img.V, align)
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

// LastDisplaySize returns the VP9 render/display size for the most recently
// decoded frame, mirroring libvpx VP9D_GET_DISPLAY_SIZE. It returns (0, 0)
// before any successful frame metadata is available.
func (d *VP9Decoder) LastDisplaySize() (width, height int) {
	if d == nil || !d.lastInfoValid {
		return 0, 0
	}
	return d.lastInfo.RenderWidth, d.lastInfo.RenderHeight
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

// LastFrameCorrupted reports whether the most recently decoded VP9 frame was
// flagged as corrupted by the decoder. Mirrors libvpx's
// VP8D_GET_FRAME_CORRUPTED getter on the VP9 decoder control map. ok is false
// on a nil or closed decoder, and before the first successful Decode or
// DecodeInto call.
func (d *VP9Decoder) LastFrameCorrupted() (corrupted bool, ok bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return false, false
	}
	return d.lastInfo.Corrupted, true
}

// LastReferenceUpdates reports the VP9 reference-slot update bitmask from the
// most recently decoded frame. Mirrors libvpx's VP8D_GET_LAST_REF_UPDATES
// getter on the VP9 decoder control map. ok is false on a nil or closed
// decoder, and before the first successful Decode or DecodeInto call.
func (d *VP9Decoder) LastReferenceUpdates() (flags uint8, ok bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return 0, false
	}
	return d.lastInfo.RefreshFrameFlags, true
}

// LastBitDepth reports the VP9 bit depth from the most recently decoded frame,
// mirroring libvpx VP9D_GET_BIT_DEPTH. ok is false on a nil or closed decoder,
// and before the first successful Decode or DecodeInto call.
func (d *VP9Decoder) LastBitDepth() (bitDepth int, ok bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return 0, false
	}
	return d.lastInfo.BitDepth, true
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
	d.releaseVP9ActiveExternalFrame()
	d.replaceVP9LastFrameExternal(nil)
	d.releaseVP9ActiveInternalFrame()
	d.replaceVP9LastFrameInternal(nil)
	d.frameReady = false
	d.lastFrame = Image{}
	d.lastInfo = VP9FrameInfo{}
	d.lastInfoValid = false
	d.initialized = false
	d.visibleFrames = 0
	d.width = 0
	d.height = 0
	for i := range d.refFrames {
		d.releaseVP9ReferenceFrame(i)
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
	if d.vp9TileDescs != nil {
		d.vp9TileDescs = d.vp9TileDescs[:0]
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
	d.postSource.Reset()
	d.post.Reset()
	d.postprocState.Reset()
}

// Close releases internal state and marks the decoder as no longer
// usable. Subsequent calls to Decode return [ErrClosed]. Close is
// idempotent: calling it on an already-closed decoder returns
// [ErrClosed] without re-tearing-down the worker pools.
func (d *VP9Decoder) Close() error {
	if d == nil || d.closed {
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
