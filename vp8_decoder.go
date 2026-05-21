package govpx

import (
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// PostProcessFlag selects optional libvpx-style decoder postprocessing.
type PostProcessFlag uint32

const (
	// PostProcessDeblock enables VP8 deblocking postprocess.
	PostProcessDeblock PostProcessFlag = 1 << iota
	// PostProcessDemacroblock enables block-edge smoothing postprocess.
	PostProcessDemacroblock
	// PostProcessAddNoise enables luma noise restoration postprocess.
	PostProcessAddNoise
	// PostProcessMFQE enables multi-frame quality enhancement.
	PostProcessMFQE

	allPostProcessFlags = PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise | PostProcessMFQE
)

// VP8Decryptor mirrors libvpx v1.16.0 vpx_decrypt_cb (vpx/vpx_decoder.h).
// The callback receives the encrypted byte slice and writes count
// plaintext bytes into dst. state is the caller-supplied opaque value
// from [DecoderOptions.DecryptorState]. Mirrors the callback shape
// invoked by libvpx's VP8D_SET_DECRYPTOR control path.
//
// Implementations must not retain references to src or dst beyond the
// call. govpx invokes the callback once per Decode with the full
// packet bytes (a coarser granularity than libvpx's per-boolean-fill
// invocation, which would force the boolean decoder's internal scratch
// onto the heap under Go's escape-analysis rules and regress the
// alloc-free hot path that TestDecoderHotPathAllocs guards). Callbacks
// that ignore offsets (the common DRM-frame-key case) behave
// identically to libvpx; callbacks that depend on per-fill granularity
// must wrap the full-packet bytes themselves.
type VP8Decryptor func(state any, src []byte, dst []byte, count int)

// DecoderOptions configures a VP8 decoder.
type DecoderOptions struct {
	// Threads selects the decoder worker count. 0 and 1 use the serial
	// path; values >=2 enable the two-stage row pipeline when the frame
	// has more than one macroblock row. Negative values are rejected
	// with [ErrInvalidConfig].
	Threads int

	// ErrorConcealment enables libvpx-style concealment for corrupt interframes
	// after a clean keyframe has initialized references.
	ErrorConcealment bool
	// PostProcessFlags selects individual libvpx-style postprocess filters.
	// Zero disables postprocessing.
	PostProcessFlags PostProcessFlag
	// PostProcessNoiseLevel enables libvpx-style additive luma noise when
	// PostProcessAddNoise is set. Zero disables additive noise; valid range is
	// [0, 16].
	PostProcessNoiseLevel int

	// MaxWidth and MaxHeight reject key frames larger than the configured
	// dimensions when non-zero.
	MaxWidth  int
	MaxHeight int

	// Decryptor, when non-nil, decrypts compressed packet bytes before
	// the boolean decoder reads them. Mirrors libvpx's VP8D_SET_DECRYPTOR
	// control (vp8/vp8_dx_iface.c vp8_set_decryptor); the callback is
	// invoked by each VP8 partition's boolean decoder during fill()
	// with the encrypted slice and a destination buffer to write the
	// plaintext into. DecryptorState is passed back as the callback's
	// first argument. Re-use the same callback + state across
	// VP8Decoder instances when a per-decoder key is desired.
	Decryptor      VP8Decryptor
	DecryptorState any

	// RejectResolutionChange, when true, makes Decode return
	// [ErrFrameRejected] on a key frame whose dimensions differ from the
	// active stream. When false (the default) the decoder reallocates
	// its internal frame buffers on the resolution-change key frame.
	RejectResolutionChange bool
}

// VP8Decoder decodes raw VP8 frame payloads.
type VP8Decoder struct {
	opts            DecoderOptions
	closed          bool
	needKey         bool
	frameReady      bool
	lastFrame       Image
	lastInfo        FrameInfo
	lastInfoValid   bool
	currentPTS      uint64
	visibleFrames   int
	initialized     bool
	ecActive        bool
	frameCorrupt    bool
	frameSuppress   bool
	modesCorrupt    int
	residualCorrupt int

	frameWidth  int
	frameHeight int
	current     vp8common.FrameBuffer
	post        vp8common.FrameBuffer
	lastRef     vp8common.FrameBuffer
	goldenRef   vp8common.FrameBuffer
	altRef      vp8common.FrameBuffer

	decryptedPacket []byte

	mbRows             int
	mbCols             int
	modes              []vp8dec.MacroblockMode
	prevModes          []vp8dec.MacroblockMode
	ecOverlaps         []vp8dec.ErrorConcealmentOverlap
	tokens             []vp8dec.MacroblockTokens
	tokenAbove         []vp8dec.EntropyContextPlanes
	frameHeader        vp8dec.FrameHeader
	previousQuant      vp8dec.QuantHeader
	previousLoopFilter vp8dec.LoopFilterHeader
	state              vp8dec.StateHeader
	partitions         vp8dec.PartitionLayout
	modeReader         boolcoder.Decoder
	tokenReaders       [8]boolcoder.Decoder
	coefProbs          vp8tables.CoefficientProbs
	frameCoefProbs     vp8tables.CoefficientProbs
	modeProbs          vp8dec.ModeProbs
	frameModeProbs     vp8dec.ModeProbs
	loopInfo           vp8common.LoopFilterInfo
	dequantTables      vp8common.FrameDequantTables
	dequants           [vp8common.MaxMBSegments]vp8common.MacroblockDequant
	segmentationState  vp8dec.SegmentationHeader
	segmentMap         []uint8
	postprocScratch    []byte
	postprocState      vp8dec.PostProcessState
	reconstructScratch vp8dec.IntraReconstructionScratch
}

// NewVP8Decoder creates a VP8 decoder with validated options. The zero
// value of opts is valid: it produces a single-threaded decoder with no
// postprocessing, no error concealment, and no dimension caps.
func NewVP8Decoder(opts DecoderOptions) (*VP8Decoder, error) {
	if err := validateDecoderOptions(opts); err != nil {
		return nil, err
	}
	d := &VP8Decoder{
		opts:           opts,
		needKey:        true,
		coefProbs:      vp8tables.DefaultCoefProbs,
		frameCoefProbs: vp8tables.DefaultCoefProbs,
	}
	vp8dec.ResetModeProbs(&d.modeProbs)
	vp8dec.ResetModeProbs(&d.frameModeProbs)
	return d, nil
}

// decryptPacket applies the registered Decryptor callback to packet,
// returning a plaintext slice for the bool-decoder pipeline. Mirrors
// libvpx's VP8D_SET_DECRYPTOR contract (vp8/vp8_dx_iface.c
// vp8_set_decryptor): the callback transforms encrypted compressed
// data into plaintext before parsing. govpx applies the callback once
// per Decode at packet entry rather than per boolean-decoder fill
// window — see the [VP8Decryptor] doc for the rationale.
//
// When no decryptor is configured this returns the input slice
// unchanged. The decrypted-buffer scratch lives on VP8Decoder so
// repeated Decode calls do not allocate on the hot path.
func (d *VP8Decoder) decryptPacket(packet []byte) []byte {
	if d.opts.Decryptor == nil || len(packet) == 0 {
		return packet
	}
	if cap(d.decryptedPacket) < len(packet) {
		d.decryptedPacket = make([]byte, len(packet))
	} else {
		d.decryptedPacket = d.decryptedPacket[:len(packet)]
	}
	d.opts.Decryptor(d.opts.DecryptorState, packet, d.decryptedPacket, len(packet))
	return d.decryptedPacket
}

// Decode decodes one raw VP8 frame payload. The first packet supplied to
// a fresh or reset decoder must be a key frame; otherwise
// [ErrNeedKeyFrame] is returned.
//
// Visible frames are queued for the next call to NextFrame. Hidden
// frames (such as alt-refs) update reference buffers but produce no
// NextFrame output.
func (d *VP8Decoder) Decode(packet []byte) error {
	return d.DecodeWithPTS(packet, 0)
}

// DecodeWithPTS is Decode with an explicit presentation timestamp. pts is
// echoed back through [VP8Decoder.LastFrameInfo].
func (d *VP8Decoder) DecodeWithPTS(packet []byte, pts uint64) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	packet = d.decryptPacket(packet)
	frame, info, err := peekVP8FrameHeader(packet)
	if err != nil {
		if d.shouldConcealMissingFrameTag(packet) {
			info := missingFrameConcealmentInfo()
			frameInfo, err := d.concealMissingInterFrame(info, pts)
			if err != nil {
				return err
			}
			d.frameReady = false
			if frameInfo.ShowFrame {
				output, err := d.outputReferenceFrameImage(info, &d.current.Img)
				if err != nil {
					return err
				}
				d.lastFrame = publicImageFromVP8(output)
				d.frameReady = true
			}
			return nil
		}
		return err
	}
	if d.needKey && !info.KeyFrame {
		return ErrNeedKeyFrame
	}
	if err := d.validateStreamInfo(info); err != nil {
		return err
	}
	if err := d.decodeFramePacket(packet, frame, info); err != nil {
		if d.opts.effectiveErrorConcealment() && d.canConceal(info) {
			frameInfo := d.finishConcealedFrame(info, pts)
			d.frameReady = false
			if frameInfo.ShowFrame {
				output, err := d.outputReferenceFrameImage(info, &d.lastRef.Img)
				if err != nil {
					return err
				}
				d.lastFrame = publicImageFromVP8(output)
				d.frameReady = true
			}
			return nil
		}
		return err
	}

	d.finishFrame(info, pts)
	if !info.ShowFrame || d.frameSuppress {
		d.frameReady = false
		return nil
	}
	output, err := d.outputFrameImage(info)
	if err != nil {
		return err
	}
	d.lastFrame = publicImageFromVP8(output)
	d.frameReady = true
	return nil
}

// NextFrame returns the most recent visible decoded frame and consumes it.
// Subsequent calls return false until the next visible frame is decoded.
// The returned image aliases decoder-owned storage; that storage stays
// valid until the next Decode, Reset, or Close call. Copy the planes if
// they must outlive that boundary.
//
// Hidden VP8 frames (ShowFrame == false, including alt-refs) do not
// produce a NextFrame result; only their reference-buffer updates take
// effect.
func (d *VP8Decoder) NextFrame() (Image, bool) {
	if d == nil || d.closed || !d.frameReady {
		return Image{}, false
	}
	d.frameReady = false
	return d.lastFrame, true
}

// LastFrameInfo returns metadata for the most recently decoded frame. ok
// is false on a nil or closed decoder, and before the first successful
// Decode/DecodeInto call.
func (d *VP8Decoder) LastFrameInfo() (FrameInfo, bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return FrameInfo{}, false
	}
	return d.lastInfo, true
}

// LastFrameCorrupted reports whether the most recently decoded frame was
// flagged as corrupted by the decoder. Mirrors libvpx's
// VP8D_GET_FRAME_CORRUPTED control (vp8/vp8_dx_iface.c). ok is false on a
// nil or closed decoder, and before the first successful Decode/DecodeInto
// call.
func (d *VP8Decoder) LastFrameCorrupted() (corrupted bool, ok bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return false, false
	}
	return d.lastInfo.Corrupted, true
}

// LastReferenceUpdates reports which reference buffers the most recently
// decoded frame refreshed. Mirrors libvpx's VP8D_GET_LAST_REF_UPDATES
// control (vp8/vp8_dx_iface.c vp8_get_last_ref_updates). ok is false on a
// nil or closed decoder, and before the first successful Decode/DecodeInto
// call.
func (d *VP8Decoder) LastReferenceUpdates() (flags ReferenceFlags, ok bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return 0, false
	}
	return d.lastInfo.RefUpdates, true
}

// LastReferencesUsed reports which reference buffers were referenced by
// inter prediction in the most recently decoded frame. Mirrors libvpx's
// VP8D_GET_LAST_REF_USED control (vp8/vp8_dx_iface.c vp8_get_last_ref_frame,
// vp8/decoder/onyxd_if.c vp8dx_references_buffer). Key frames report 0. ok
// is false on a nil or closed decoder, and before the first successful
// Decode/DecodeInto call.
func (d *VP8Decoder) LastReferencesUsed() (flags ReferenceFlags, ok bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return 0, false
	}
	return d.lastInfo.RefUsed, true
}

// SetReferenceFrame replaces ref with src. ref must be ReferenceLast,
// ReferenceGolden, or ReferenceAltRef; src must match the stream dimensions
// established by the most recently decoded key frame and provide valid
// I420 strides. Returns [ErrInvalidConfig] when no key frame has been
// decoded yet or when dimensions or strides do not match. The decoder
// extends reference borders after copying.
func (d *VP8Decoder) SetReferenceFrame(ref ReferenceFrame, src Image) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	fb, ok := d.referenceFrameBuffer(ref)
	if !ok || !d.referenceFramesInitialized() {
		return ErrInvalidConfig
	}
	if !src.validForEncode(d.frameWidth, d.frameHeight) {
		return ErrInvalidConfig
	}
	copyPublicImageToVP8(&fb.Img, src)
	fb.ExtendBorders()
	return nil
}

// CopyReferenceFrame copies ref into dst. ref must be ReferenceLast,
// ReferenceGolden, or ReferenceAltRef; dst must match the stream dimensions
// established by the most recently decoded key frame and provide valid
// I420 strides. Returns [ErrInvalidConfig] when no key frame has been
// decoded yet or when dimensions or strides do not match.
func (d *VP8Decoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if dst == nil {
		return ErrInvalidConfig
	}
	fb, ok := d.referenceFrameBuffer(ref)
	if !ok || !d.referenceFramesInitialized() {
		return ErrInvalidConfig
	}
	if !dst.validForEncode(d.frameWidth, d.frameHeight) {
		return ErrInvalidConfig
	}
	copyVP8ImageToPublic(dst, &fb.Img)
	return nil
}

// DecodeInto decodes one raw VP8 frame payload. If the packet is a visible
// frame its decoded pixels are written into the caller-owned planes of
// dst; for hidden frames dst is left untouched. dst must be non-nil and
// match the dimensions of the most recently decoded key frame (or of
// the packet itself, when it is a key frame).
func (d *VP8Decoder) DecodeInto(packet []byte, dst *Image) (FrameInfo, error) {
	return d.DecodeIntoWithPTS(packet, dst, 0)
}

// DecodeIntoWithPTS is DecodeInto with an explicit presentation timestamp.
// pts is echoed back in the returned FrameInfo.
func (d *VP8Decoder) DecodeIntoWithPTS(packet []byte, dst *Image, pts uint64) (FrameInfo, error) {
	if d == nil || d.closed {
		return FrameInfo{}, ErrClosed
	}
	if dst == nil {
		return FrameInfo{}, ErrInvalidConfig
	}
	packet = d.decryptPacket(packet)
	frame, info, err := peekVP8FrameHeader(packet)
	if err != nil {
		if d.shouldConcealMissingFrameTag(packet) {
			info := missingFrameConcealmentInfo()
			outputWidth, outputHeight := d.outputDimensions(info)
			if !dst.validForEncode(outputWidth, outputHeight) {
				return FrameInfo{}, ErrInvalidConfig
			}
			frameInfo, err := d.concealMissingInterFrame(info, pts)
			if err != nil {
				return FrameInfo{}, err
			}
			d.frameReady = false
			if frameInfo.ShowFrame {
				output, err := d.outputReferenceFrameImage(info, &d.current.Img)
				if err != nil {
					return FrameInfo{}, err
				}
				copyVP8ImageToPublic(dst, output)
			}
			return frameInfo, nil
		}
		return FrameInfo{}, err
	}
	if d.needKey && !info.KeyFrame {
		return FrameInfo{}, ErrNeedKeyFrame
	}
	if err := d.validateStreamInfo(info); err != nil {
		return FrameInfo{}, err
	}
	outputWidth, outputHeight := d.outputDimensions(info)
	if !dst.validForEncode(outputWidth, outputHeight) {
		return FrameInfo{}, ErrInvalidConfig
	}
	if err := d.decodeFramePacket(packet, frame, info); err != nil {
		if d.opts.effectiveErrorConcealment() && d.canConceal(info) {
			frameInfo := d.finishConcealedFrame(info, pts)
			d.frameReady = false
			if frameInfo.ShowFrame {
				output, err := d.outputReferenceFrameImage(info, &d.lastRef.Img)
				if err != nil {
					return FrameInfo{}, err
				}
				copyVP8ImageToPublic(dst, output)
			}
			return frameInfo, nil
		}
		return FrameInfo{}, err
	}
	frameInfo := d.finishFrame(info, pts)
	d.frameReady = false
	if !info.ShowFrame || d.frameSuppress {
		if d.frameSuppress {
			// Mirror libvpx vp8/decoder/onyxd_if.c swap_frame_buffers
			// err=-1 path: the frame is decoded but vp8_get_frame yields
			// no image, so the public ShowFrame bit is cleared in the
			// returned FrameInfo to signal callers that dst was not
			// written.
			frameInfo.ShowFrame = false
		}
		return frameInfo, nil
	}
	output, err := d.outputFrameImage(info)
	if err != nil {
		return FrameInfo{}, err
	}
	copyVP8ImageToPublic(dst, output)
	return frameInfo, nil
}

// Reset returns the decoder to its cold-start state while retaining
// allocated buffers and validated DecoderOptions for reuse. The next
// Decode must be a key frame; reference buffers, postprocess state, and
// queued NextFrame output are cleared.
func (d *VP8Decoder) Reset() {
	if d == nil {
		return
	}
	d.needKey = true
	d.frameReady = false
	d.lastFrame = Image{}
	d.lastInfo = FrameInfo{}
	d.lastInfoValid = false
	d.currentPTS = 0
	d.visibleFrames = 0
	d.initialized = false
	d.ecActive = false
	d.frameCorrupt = false
	d.frameSuppress = false
	d.modesCorrupt = 0
	d.residualCorrupt = -1
	d.previousQuant = vp8dec.QuantHeader{}
	d.previousLoopFilter = vp8dec.LoopFilterHeader{}
	d.state = vp8dec.StateHeader{}
	d.segmentationState = vp8dec.SegmentationHeader{}
	d.frameHeader = vp8dec.FrameHeader{}
	d.partitions = vp8dec.PartitionLayout{}
	d.current.Reset()
	d.post.Reset()
	d.lastRef.Reset()
	d.goldenRef.Reset()
	d.altRef.Reset()
	d.postprocState.Reset()
	d.coefProbs = vp8tables.DefaultCoefProbs
	d.frameCoefProbs = vp8tables.DefaultCoefProbs
	for i := range d.segmentMap {
		d.segmentMap[i] = 0
	}
	for i := range d.prevModes {
		d.prevModes[i] = vp8dec.MacroblockMode{}
	}
	vp8dec.ResetModeProbs(&d.modeProbs)
	vp8dec.ResetModeProbs(&d.frameModeProbs)
}

// Close releases decoder state. After Close, methods that return an
// error return [ErrClosed]; [VP8Decoder.NextFrame] and
// [VP8Decoder.LastFrameInfo] report not-ready instead. Calling Close on
// a nil or already-closed decoder returns [ErrClosed].
func (d *VP8Decoder) Close() error {
	if d == nil || d.closed {
		return ErrClosed
	}
	d.Reset()
	d.closed = true
	return nil
}

func (d *VP8Decoder) decodeFramePacket(packet []byte, frame vp8dec.FrameHeader, info StreamInfo) error {
	errorConcealment := d.opts.effectiveErrorConcealment() && d.canConceal(info)
	if errorConcealment {
		d.ecActive = true
	}
	d.frameCorrupt = false
	d.frameSuppress = false
	d.modesCorrupt = 0
	d.residualCorrupt = -1
	if err := d.parseState(packet, frame, errorConcealment); err != nil {
		return err
	}
	if err := d.ensureFrameBuffers(info); err != nil {
		return err
	}
	if err := d.decodeModeGrid(info); err != nil {
		return err
	}
	if errorConcealment && d.modesCorrupt < d.mbRows*d.mbCols {
		if err := vp8dec.EstimateMissingMotionVectorsWithScratch(d.modes, d.prevModes, d.mbRows, d.mbCols, d.modesCorrupt, d.ecOverlaps); err != nil {
			return ErrInvalidData
		}
		d.zeroCorruptMacroblockTokens(d.modesCorrupt)
		applyCorruptInterFrameRefresh(&d.state)
		d.frameCorrupt = true
	}
	if d.frameCorrupt {
		if d.modesCorrupt > 0 {
			if err := d.decodeTokenGrid(errorConcealment); err != nil {
				return err
			}
		}
		d.zeroCorruptMacroblockTokens(d.modesCorrupt)
	} else if err := d.decodeTokenGrid(errorConcealment); err != nil {
		return err
	}
	if d.residualCorrupt >= 0 {
		d.zeroCorruptMacroblockTokens(d.residualCorrupt)
	}
	if err := d.reconstructFrame(info); err != nil {
		return err
	}
	d.saveErrorConcealmentModes()
	d.frameSuppress = d.refreshReferences()
	if !d.frameCorrupt {
		d.commitParsedState(info)
	}
	return nil
}

func validateDecoderOptions(opts DecoderOptions) error {
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if opts.PostProcessFlags&^allPostProcessFlags != 0 {
		return ErrInvalidConfig
	}
	if min(opts.MaxWidth, opts.MaxHeight) < 0 {
		return ErrInvalidConfig
	}
	if uint(opts.PostProcessNoiseLevel) > 16 {
		return ErrInvalidConfig
	}
	if opts.PostProcessNoiseLevel > 0 && opts.effectivePostProcessFlags()&PostProcessAddNoise == 0 {
		return ErrInvalidConfig
	}
	if opts.MaxWidth > maxVP8Dimension || opts.MaxHeight > maxVP8Dimension {
		return ErrInvalidConfig
	}
	return nil
}

func (opts DecoderOptions) effectivePostProcessFlags() PostProcessFlag {
	return opts.PostProcessFlags
}

func (opts DecoderOptions) effectiveErrorConcealment() bool {
	return opts.ErrorConcealment
}

func missingFrameConcealmentInfo() StreamInfo {
	return StreamInfo{ShowFrame: true}
}

func (d *VP8Decoder) shouldConcealMissingFrameTag(packet []byte) bool {
	return len(packet) < 3 &&
		d.opts.effectiveErrorConcealment() &&
		d.ecActive &&
		d.canConceal(missingFrameConcealmentInfo())
}

func (d *VP8Decoder) validateStreamInfo(info StreamInfo) error {
	if !vp8dec.IsSupportedVersion(info.Profile) {
		return ErrInvalidData
	}
	if !info.KeyFrame {
		return nil
	}
	if min(info.Width, info.Height) <= 0 {
		return ErrInvalidData
	}
	if d.opts.MaxWidth > 0 && info.Width > d.opts.MaxWidth {
		return ErrFrameRejected
	}
	if d.opts.MaxHeight > 0 && info.Height > d.opts.MaxHeight {
		return ErrFrameRejected
	}
	if d.initialized && d.opts.RejectResolutionChange {
		if info.Width != d.lastInfo.Width || info.Height != d.lastInfo.Height {
			return ErrFrameRejected
		}
	}
	return nil
}

func (d *VP8Decoder) finishFrame(info StreamInfo, pts uint64) FrameInfo {
	d.currentPTS = pts
	d.initialized = true
	if info.KeyFrame {
		d.needKey = false
	}
	width, height := d.outputDimensions(info)
	frameInfo := FrameInfo{
		Width:             width,
		Height:            height,
		KeyFrame:          info.KeyFrame,
		ShowFrame:         info.ShowFrame,
		Corrupted:         d.frameCorrupt,
		Quantizer:         vp8common.QIndexToPublicQuantizer(int(d.state.Quant.BaseQIndex)),
		InternalQuantizer: int(d.state.Quant.BaseQIndex),
		RefUpdates:        referenceFlagsFromRefresh(d.state.Refresh),
		RefUsed:           d.referenceFlagsUsed(info),
		PTS:               pts,
	}
	d.lastInfo = frameInfo
	d.lastInfoValid = true
	if info.ShowFrame && !d.frameSuppress {
		d.visibleFrames++
	}
	return frameInfo
}

func (d *VP8Decoder) canConceal(info StreamInfo) bool {
	return d.initialized &&
		!info.KeyFrame &&
		d.frameWidth > 0 &&
		d.frameHeight > 0 &&
		d.lastRef.BufferLen() != 0
}

func (d *VP8Decoder) finishConcealedFrame(info StreamInfo, pts uint64) FrameInfo {
	d.currentPTS = pts
	frameInfo := FrameInfo{
		Width:             d.frameWidth,
		Height:            d.frameHeight,
		KeyFrame:          false,
		ShowFrame:         info.ShowFrame,
		Corrupted:         true,
		Quantizer:         vp8common.QIndexToPublicQuantizer(int(d.previousQuant.BaseQIndex)),
		InternalQuantizer: int(d.previousQuant.BaseQIndex),
		RefUpdates:        referenceFlagsFromRefresh(d.state.Refresh),
		RefUsed:           d.referenceFlagsUsed(info),
		PTS:               pts,
	}
	d.lastInfo = frameInfo
	d.lastInfoValid = true
	if info.ShowFrame {
		d.visibleFrames++
	}
	return frameInfo
}

func referenceFlagsFromRefresh(refresh vp8dec.RefreshHeader) ReferenceFlags {
	var flags ReferenceFlags
	if refresh.RefreshLast {
		flags |= ReferenceFlagLast
	}
	if refresh.RefreshGolden {
		flags |= ReferenceFlagGolden
	}
	if refresh.RefreshAltRef {
		flags |= ReferenceFlagAltRef
	}
	return flags
}

func (d *VP8Decoder) referenceFlagsUsed(info StreamInfo) ReferenceFlags {
	if info.KeyFrame {
		return 0
	}
	var flags ReferenceFlags
	for i := 0; i < len(d.modes); i++ {
		switch d.modes[i].RefFrame {
		case vp8common.LastFrame:
			flags |= ReferenceFlagLast
		case vp8common.GoldenFrame:
			flags |= ReferenceFlagGolden
		case vp8common.AltRefFrame:
			flags |= ReferenceFlagAltRef
		}
	}
	return flags
}

func (d *VP8Decoder) concealMissingInterFrame(info StreamInfo, pts uint64) (FrameInfo, error) {
	d.state = vp8dec.StateHeader{}
	d.state.Quant = d.previousQuant
	d.state.Refresh.RefreshLast = true
	d.frameHeader = vp8dec.FrameHeader{FrameType: vp8common.InterFrame, Profile: 0, ShowFrame: true}
	for i := range d.tokens {
		d.tokens[i] = vp8dec.MacroblockTokens{}
	}
	if err := vp8dec.EstimateMissingMotionVectorsWithScratch(d.modes, d.prevModes, d.mbRows, d.mbCols, 0, d.ecOverlaps); err != nil {
		return FrameInfo{}, ErrInvalidData
	}
	if err := d.reconstructFrame(StreamInfo{Profile: 0}); err != nil {
		return FrameInfo{}, err
	}
	vp8common.CopyExtendedImage(&d.lastRef.Img, &d.current.Img)
	d.saveErrorConcealmentModes()
	return d.finishConcealedFrame(info, pts), nil
}

func (d *VP8Decoder) saveErrorConcealmentModes() {
	if !d.opts.effectiveErrorConcealment() || len(d.prevModes) < len(d.modes) {
		return
	}
	vp8dec.PrepareErrorConcealmentModes(d.modes)
	copy(d.prevModes, d.modes)
}

func (d *VP8Decoder) outputFrameImage(info StreamInfo) (*vp8common.Image, error) {
	return d.outputReferenceFrameImage(info, &d.current.Img)
}

func (d *VP8Decoder) outputReferenceFrameImage(info StreamInfo, src *vp8common.Image) (*vp8common.Image, error) {
	flags := d.opts.effectivePostProcessFlags()
	if flags == 0 {
		return src, nil
	}
	loopFilter := vp8dec.LoopFilterHeaderForVersion(info.Profile, d.state.LoopFilter)
	opts := vp8dec.PostProcessOptions{
		Deblock:         flags&PostProcessDeblock != 0,
		Demacroblock:    flags&PostProcessDemacroblock != 0,
		MFQE:            flags&PostProcessMFQE != 0,
		AddNoise:        flags&PostProcessAddNoise != 0 && d.opts.PostProcessNoiseLevel > 0,
		DeblockingLevel: vp8dec.DefaultPostProcessDeblockingLevel,
		NoiseLevel:      d.opts.PostProcessNoiseLevel,
		BaseQIndex:      int(d.state.Quant.BaseQIndex),
		CurrentFrame:    d.visibleFrames,
		KeyFrame:        info.KeyFrame,
	}
	if err := vp8dec.ApplyPostProcessWithOptions(src, &d.post, d.mbRows, d.mbCols, d.modes, loopFilter.Level, d.postprocScratch, opts, &d.postprocState); err != nil {
		return nil, ErrInvalidData
	}
	return &d.post.Img, nil
}

func (d *VP8Decoder) outputDimensions(info StreamInfo) (int, int) {
	if info.KeyFrame {
		return info.Width, info.Height
	}
	return d.frameWidth, d.frameHeight
}

func (d *VP8Decoder) ensureFrameBuffers(info StreamInfo) error {
	if !info.KeyFrame {
		return nil
	}
	if d.frameWidth == info.Width && d.frameHeight == info.Height && d.current.BufferLen() != 0 {
		return nil
	}
	if err := d.current.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	if err := d.post.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	if err := d.lastRef.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	if err := d.goldenRef.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	if err := d.altRef.Resize(info.Width, info.Height, 32, 32); err != nil {
		return ErrInvalidData
	}
	flags := d.opts.effectivePostProcessFlags()
	if flags&PostProcessMFQE != 0 {
		if err := d.postprocState.EnsureMFQE(info.Width, info.Height); err != nil {
			return ErrInvalidData
		}
	}
	d.ensureWorkspace(info.Width, info.Height)
	d.frameWidth = info.Width
	d.frameHeight = info.Height
	return nil
}

func (d *VP8Decoder) parseState(packet []byte, frameHeader vp8dec.FrameHeader, errorConcealment bool) error {
	frameProbs := d.coefProbs
	frameModeProbs := d.modeProbs
	var frame vp8dec.FrameHeader
	var state vp8dec.StateHeader
	var modeReader boolcoder.Decoder
	var err error
	var stateCorrupted bool
	if errorConcealment {
		frame, state, modeReader, stateCorrupted, err = vp8dec.ParseStateHeaderFromFrameWithErrorConcealment(packet, frameHeader, d.previousQuant, d.previousLoopFilter, &frameProbs, &frameModeProbs)
	} else {
		frame, state, modeReader, err = vp8dec.ParseStateHeaderFromFrameWithReaderAndProbsAndLoopFilter(packet, frameHeader, d.previousQuant, d.previousLoopFilter, &frameProbs, &frameModeProbs)
	}
	if err != nil {
		return ErrInvalidData
	}
	if errorConcealment && !frame.KeyFrame() && frame.HeaderSize <= len(packet) && frame.FirstPartitionSize > len(packet)-frame.HeaderSize {
		stateCorrupted = true
	}
	if stateCorrupted {
		if !frame.KeyFrame() {
			applyCorruptInterFrameRefresh(&state)
		}
		d.frameCorrupt = true
		d.modesCorrupt = 0
	} else {
		d.modesCorrupt = d.mbRows * d.mbCols
	}
	previousSegmentation := d.segmentationState
	if frame.KeyFrame() {
		previousSegmentation = vp8dec.SegmentationHeader{}
	}
	state.Segmentation = mergeSegmentationHeader(previousSegmentation, state.Segmentation)
	var partitions vp8dec.PartitionLayout
	var partitionErr error
	if errorConcealment {
		partitionErr = vp8dec.ParsePartitionLayoutWithErrorConcealment(packet, frame, state.TokenPartition, &partitions)
	} else {
		partitionErr = vp8dec.ParsePartitionLayout(packet, frame, state.TokenPartition, &partitions)
	}
	if partitionErr != nil {
		return ErrInvalidData
	}
	for i := 0; i < partitions.TokenCount; i++ {
		if err := d.tokenReaders[i].Init(partitions.Tokens[i]); err != nil {
			return ErrInvalidData
		}
	}
	d.frameHeader = frame
	d.state = state
	d.partitions = partitions
	d.modeReader = modeReader
	d.frameCoefProbs = frameProbs
	d.frameModeProbs = frameModeProbs
	vp8dec.InitSegmentDequants(state.Quant, &state.Segmentation, &d.dequantTables, &d.dequants)
	return nil
}

func applyCorruptInterFrameRefresh(state *vp8dec.StateHeader) {
	state.Refresh.RefreshGolden = false
	state.Refresh.RefreshAltRef = false
	state.Refresh.CopyBufferToGolden = 0
	state.Refresh.CopyBufferToAltRef = 0
	state.Refresh.RefreshEntropyProbs = false
	state.Refresh.RefreshLast = true
}

func (d *VP8Decoder) commitParsedState(info StreamInfo) {
	if d.state.Refresh.RefreshEntropyProbs {
		d.coefProbs = d.frameCoefProbs
	} else if info.KeyFrame {
		d.coefProbs = vp8tables.DefaultCoefProbs
	}
	if d.state.Refresh.RefreshEntropyProbs {
		d.modeProbs = d.frameModeProbs
	} else if info.KeyFrame {
		vp8dec.ResetModeProbs(&d.modeProbs)
	}
	d.previousQuant = d.state.Quant
	d.previousLoopFilter = d.state.LoopFilter
	if info.KeyFrame {
		d.segmentationState = vp8dec.SegmentationHeader{}
	}
	if d.state.Segmentation.Enabled {
		d.segmentationState = d.state.Segmentation
		d.segmentationState.UpdateMap = false
		d.segmentationState.UpdateData = false
	}
	d.commitSegmentMap()
}

func (d *VP8Decoder) decodeModeGrid(info StreamInfo) error {
	if info.KeyFrame {
		d.clearSegmentMap()
	}
	d.restoreSegmentMap()
	reader := d.modeReader
	if info.KeyFrame {
		if err := vp8dec.DecodeKeyFrameModeGrid(&reader, d.mbRows, d.mbCols, &d.state.Segmentation, d.state.Mode, d.modes); err != nil {
			return ErrInvalidData
		}
	} else {
		if d.opts.effectiveErrorConcealment() && d.ecActive {
			firstCorrupt, err := vp8dec.DecodeInterModeGridWithErrorConcealment(&reader, d.mbRows, d.mbCols, &d.state.Segmentation, d.state.Mode, &d.frameModeProbs, d.referenceSignBias(), d.modes)
			if err != nil {
				return ErrInvalidData
			}
			if firstCorrupt < d.modesCorrupt {
				d.modesCorrupt = firstCorrupt
				d.frameCorrupt = true
			}
		} else {
			if err := vp8dec.DecodeInterModeGrid(&reader, d.mbRows, d.mbCols, &d.state.Segmentation, d.state.Mode, &d.frameModeProbs, d.referenceSignBias(), d.modes); err != nil {
				return ErrInvalidData
			}
		}
	}
	if reader.Err() != nil && !(d.opts.effectiveErrorConcealment() && d.ecActive && !info.KeyFrame) {
		return ErrInvalidData
	}
	d.modeReader = reader
	return nil
}

func (d *VP8Decoder) referenceSignBias() [vp8common.MaxRefFrames]bool {
	var signBias [vp8common.MaxRefFrames]bool
	signBias[vp8common.GoldenFrame] = d.state.Refresh.GoldenSignBias
	signBias[vp8common.AltRefFrame] = d.state.Refresh.AltRefSignBias
	return signBias
}

func (d *VP8Decoder) decodeTokenGrid(errorConcealment bool) error {
	readers := d.tokenReaders[:d.partitions.TokenCount]
	if errorConcealment {
		firstCorrupt, err := d.decodeTokenGridWithErrorConcealment(readers)
		if err != nil {
			return err
		}
		if firstCorrupt < d.mbRows*d.mbCols {
			d.frameCorrupt = true
			if d.residualCorrupt < 0 || firstCorrupt < d.residualCorrupt {
				d.residualCorrupt = firstCorrupt
			}
		}
		return nil
	}
	if _, err := vp8dec.DecodeTokenGrid(readers, d.mbRows, d.mbCols, &d.frameCoefProbs, d.modes, d.tokenAbove, d.tokens); err != nil {
		return ErrInvalidData
	}
	for i := range readers {
		if readers[i].Err() != nil {
			return ErrInvalidData
		}
	}
	return nil
}

func (d *VP8Decoder) decodeTokenGridWithErrorConcealment(readers []boolcoder.Decoder) (int, error) {
	_, firstCorrupt, err := vp8dec.DecodeTokenGridWithErrorConcealment(readers, d.mbRows, d.mbCols, &d.frameCoefProbs, d.modes, d.tokenAbove, d.tokens)
	if err != nil {
		return 0, ErrInvalidData
	}
	return firstCorrupt, nil
}

func (d *VP8Decoder) zeroCorruptMacroblockTokens(first int) {
	if first < 0 {
		first = 0
	}
	if first > len(d.tokens) {
		return
	}
	for i := first; i < len(d.tokens); i++ {
		d.tokens[i] = vp8dec.MacroblockTokens{}
		d.modes[i].MBSkipCoeff = true
	}
}

func (d *VP8Decoder) reconstructFrame(info StreamInfo) error {
	frameType := vp8common.KeyFrame
	if !info.KeyFrame {
		frameType = vp8common.InterFrame
	}
	cfg := vp8dec.InterPredictionConfigForVersion(info.Profile)
	skipLoopFilter := vp8dec.VersionSkipsLoopFilter(info.Profile)
	loopFilter := vp8dec.LoopFilterHeaderForVersion(info.Profile, d.state.LoopFilter)

	// Threads >= 2 enables the libvpx-style two-stage row pipeline (recon
	// producer / loop-filter consumer). The output is byte-identical to
	// the serial path; see internal/vp8/decoder/threading.go for the
	// dependency analysis. Tiny frames (rows <= 1) gain nothing from the
	// pipeline so we keep the inline serial walk for them.
	if d.opts.Threads >= 2 && d.mbRows > 1 {
		if err := vp8dec.ReconstructAndLoopFilterPipelined(
			&d.current.Img, &d.lastRef.Img, &d.goldenRef.Img, &d.altRef.Img,
			d.mbRows, d.mbCols, d.modes, d.tokens, &d.dequants, &d.reconstructScratch, cfg,
			info.KeyFrame,
			!skipLoopFilter,
			frameType, loopFilter, d.state.Segmentation, &d.loopInfo,
		); err != nil {
			return ErrInvalidData
		}
		d.current.ExtendBorders()
		return nil
	}

	if info.KeyFrame {
		if err := vp8dec.ReconstructKeyFrameIntraGrid(&d.current.Img, d.mbRows, d.mbCols, d.modes, d.tokens, &d.dequants, &d.reconstructScratch); err != nil {
			return ErrInvalidData
		}
	} else {
		if err := vp8dec.ReconstructInterFrameGridWithConfig(&d.current.Img, &d.lastRef.Img, &d.goldenRef.Img, &d.altRef.Img, d.mbRows, d.mbCols, d.modes, d.tokens, &d.dequants, &d.reconstructScratch, cfg); err != nil {
			return ErrInvalidData
		}
	}
	if !skipLoopFilter {
		vp8dec.ApplyLoopFilterUnchecked(&d.current.Img, d.mbRows, d.mbCols, d.modes, frameType, loopFilter, d.state.Segmentation, &d.loopInfo)
	}
	d.current.ExtendBorders()
	return nil
}

// refreshReferences applies the per-frame reference buffer copy/refresh
// flags parsed from the inter-frame header. Ported from libvpx v1.16.0
// vp8/decoder/onyxd_if.c swap_frame_buffers (lines 213-268): the
// CopyBufferToAltRef and CopyBufferToGolden fields are 2-bit literals
// (range 0..3) but only values 0/1/2 are defined. Value 3 is invalid;
// libvpx's swap_frame_buffers sets err=-1 in that case, which propagates
// to vp8dx_receive_compressed_data (onyxd_if.c:339-342) — it sets
// VPX_CODEC_ERROR and jumps to decode_exit *before* clearing
// ready_for_new_data. The subsequent vp8dx_get_raw_frame (onyxd_if.c:380)
// returns -1 because ready_for_new_data is still 1, so vp8_get_frame
// yields no image: the frame is decoded but suppressed from output.
//
// refreshReferences returns true when the frame should be suppressed
// (CopyBufferToAltRef or CopyBufferToGolden equals 3). Callers must skip
// publishing the current frame via NextFrame/DecodeInto in that case.
// Reference state remains unchanged for invalid copy values, mirroring
// libvpx's behavior where the alias points at an unchanged buffer slot.
func (d *VP8Decoder) refreshReferences() bool {
	suppress := false
	switch d.state.Refresh.CopyBufferToAltRef {
	case 1:
		vp8common.CopyExtendedImage(&d.altRef.Img, &d.lastRef.Img)
	case 2:
		vp8common.CopyExtendedImage(&d.altRef.Img, &d.goldenRef.Img)
	case 3:
		suppress = true
	}
	switch d.state.Refresh.CopyBufferToGolden {
	case 1:
		vp8common.CopyExtendedImage(&d.goldenRef.Img, &d.lastRef.Img)
	case 2:
		vp8common.CopyExtendedImage(&d.goldenRef.Img, &d.altRef.Img)
	case 3:
		suppress = true
	}
	if d.state.Refresh.RefreshLast {
		vp8common.CopyExtendedImage(&d.lastRef.Img, &d.current.Img)
	}
	if d.state.Refresh.RefreshGolden {
		vp8common.CopyExtendedImage(&d.goldenRef.Img, &d.current.Img)
	}
	if d.state.Refresh.RefreshAltRef {
		vp8common.CopyExtendedImage(&d.altRef.Img, &d.current.Img)
	}
	return suppress
}

// referenceFrameBuffer maps the public reference selector to the decoder-owned
// bordered buffer. Invalid selectors include combined ReferenceFlags values.
func (d *VP8Decoder) referenceFrameBuffer(ref ReferenceFrame) (*vp8common.FrameBuffer, bool) {
	switch ref {
	case ReferenceLast:
		return &d.lastRef, true
	case ReferenceGolden:
		return &d.goldenRef, true
	case ReferenceAltRef:
		return &d.altRef, true
	default:
		return nil, false
	}
}

// referenceFramesInitialized reports whether set/copy reference controls can
// safely use the reference buffers. govpx requires a decoded key frame to
// establish stream dimensions before callers can replace decoder references.
func (d *VP8Decoder) referenceFramesInitialized() bool {
	return d.initialized &&
		d.frameWidth > 0 &&
		d.frameHeight > 0 &&
		d.lastRef.BufferLen() != 0
}

func publicImageFromVP8(src *vp8common.Image) Image {
	return Image{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
}

func copyVP8ImageToPublic(dst *Image, src *vp8common.Image) {
	copyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	copyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	copyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
}

// copyPublicImageToVP8 copies only visible samples into a bordered VP8 image;
// callers that install a reference must extend borders afterwards.
func copyPublicImageToVP8(dst *vp8common.Image, src Image) {
	copyPlane(dst.Y, dst.YStride, src.Y, src.YStride, dst.Width, dst.Height)
	uvWidth := (dst.Width + 1) >> 1
	uvHeight := (dst.Height + 1) >> 1
	copyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	copyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
}

func copyPlane(dst []byte, dstStride int, src []byte, srcStride int, width int, height int) {
	for row := range height {
		copy(dst[row*dstStride:row*dstStride+width], src[row*srcStride:row*srcStride+width])
	}
}

func avgPlane(dst []byte, dstStride int, src []byte, srcStride int, width int, height int) {
	for row := range height {
		dstRow := dst[row*dstStride:]
		srcRow := src[row*srcStride:]
		for col := range width {
			dstRow[col] = byte((int(dstRow[col]) + int(srcRow[col]) + 1) >> 1)
		}
	}
}

func (d *VP8Decoder) ensureWorkspace(width int, height int) {
	cols := (width + 15) >> 4
	rows := (height + 15) >> 4
	count := rows * cols
	if cap(d.modes) < count {
		d.modes = make([]vp8dec.MacroblockMode, count)
	} else {
		d.modes = d.modes[:count]
	}
	if cap(d.prevModes) < count {
		d.prevModes = make([]vp8dec.MacroblockMode, count)
	} else {
		d.prevModes = d.prevModes[:count]
	}
	// Persistent error-concealment overlap buffer (libvpx pbi->overlaps).
	// Sized once per workspace resize so concealed inter frames don't
	// allocate per Decode call (see TestDecodeErrorConcealmentAllocatesZero).
	if cap(d.ecOverlaps) < count {
		d.ecOverlaps = make([]vp8dec.ErrorConcealmentOverlap, count)
	} else {
		d.ecOverlaps = d.ecOverlaps[:count]
	}
	if cap(d.tokens) < count {
		d.tokens = make([]vp8dec.MacroblockTokens, count)
	} else {
		d.tokens = d.tokens[:count]
	}
	if cap(d.tokenAbove) < cols {
		d.tokenAbove = make([]vp8dec.EntropyContextPlanes, cols)
	} else {
		d.tokenAbove = d.tokenAbove[:cols]
	}
	if cap(d.segmentMap) < count {
		d.segmentMap = make([]uint8, count)
	} else {
		d.segmentMap = d.segmentMap[:count]
	}
	scratchLen := cols * 24
	if cap(d.postprocScratch) < scratchLen {
		d.postprocScratch = make([]byte, scratchLen)
	} else {
		d.postprocScratch = d.postprocScratch[:scratchLen]
	}
	flags := d.opts.effectivePostProcessFlags()
	if flags&PostProcessAddNoise != 0 && d.opts.PostProcessNoiseLevel > 0 {
		d.postprocState.EnsureNoise(width)
	}
	d.mbRows = rows
	d.mbCols = cols
}

func mergeSegmentationHeader(previous vp8dec.SegmentationHeader, current vp8dec.SegmentationHeader) vp8dec.SegmentationHeader {
	if !current.Enabled {
		return current
	}
	if !current.UpdateData {
		current.AbsDelta = previous.AbsDelta
		current.FeatureData = previous.FeatureData
	}
	if !current.UpdateMap {
		current.TreeProbs = previous.TreeProbs
	}
	return current
}

func (d *VP8Decoder) restoreSegmentMap() {
	if !d.state.Segmentation.Enabled || d.state.Segmentation.UpdateMap {
		return
	}
	for i := range d.modes {
		d.modes[i].SegmentID = d.segmentMap[i]
	}
}

func (d *VP8Decoder) commitSegmentMap() {
	if !d.state.Segmentation.Enabled {
		return
	}
	for i := range d.modes {
		d.segmentMap[i] = d.modes[i].SegmentID
	}
}

func (d *VP8Decoder) clearSegmentMap() {
	for i := range d.segmentMap {
		d.segmentMap[i] = 0
	}
}
