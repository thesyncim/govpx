package codec

import (
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

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
	// PostProcessDeblockingLevel is the VP8_SET_POSTPROC deblocking strength
	// in [0, 16]. Zero uses libvpx's decoder default of 4 unless
	// PostProcessDeblockingLevelSet is true.
	PostProcessDeblockingLevel int
	// PostProcessDeblockingLevelSet makes a zero PostProcessDeblockingLevel
	// explicit instead of treating it as the default.
	PostProcessDeblockingLevelSet bool
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

// vp8DecoderState is the explicit lifecycle state of a VP8Decoder:
//
//	NeedKey (cold start / Reset) --keyframe--> Active <--> FrameReady
//	Any                          --Close()---> Closed (terminal)
type vp8DecoderState uint8

const (
	vp8DecoderNeedKey vp8DecoderState = iota
	vp8DecoderActive
	vp8DecoderFrameReady
	vp8DecoderClosed
)

// VP8Decoder decodes raw VP8 frame payloads.
// Pointer-containing fields are grouped at the top of the struct to minimize
// Go GC PtrBytes scanning overhead.
type VP8Decoder struct {
	opts            DecoderOptions
	lastFrame       Image
	refPool         [4]vp8common.FrameBuffer
	post            vp8common.FrameBuffer
	decryptedPacket []byte
	modes           []vp8dec.MacroblockMode
	prevModes       []vp8dec.MacroblockMode
	ecOverlaps      []vp8dec.ErrorConcealmentOverlap
	tokens          []vp8dec.MacroblockTokens
	tokenAbove      []vp8dec.EntropyContextPlanes
	segmentMap      []uint8
	postprocScratch []byte
	postprocState   vp8dec.PostProcessState
	modeReader      boolcoder.Decoder
	tokenReaders    [8]boolcoder.Decoder
	partitions      vp8dec.PartitionLayout

	// Pointer-free value fields below (never scanned by Go's GC).
	lifecycle          vp8DecoderState
	lastInfo           FrameInfo
	currentPTS         uint64
	visibleFrames      int
	ecActive           bool
	frameCorrupt       bool
	frameSuppress      bool
	modesCorrupt       int
	residualCorrupt    int
	frameWidth         int
	frameHeight        int
	curIdx             int
	lastIdx            int
	goldenIdx          int
	altIdx             int
	mbRows             int
	mbCols             int
	frameHeader        vp8dec.FrameHeader
	previousQuant      vp8dec.QuantHeader
	previousLoopFilter vp8dec.LoopFilterHeader
	state              vp8dec.StateHeader
	coefProbs          vp8tables.CoefficientProbs
	frameCoefProbs     vp8tables.CoefficientProbs
	modeProbs          vp8dec.ModeProbs
	frameModeProbs     vp8dec.ModeProbs
	loopInfo           vp8common.LoopFilterInfo
	dequantTables      vp8common.FrameDequantTables
	dequants           [vp8common.MaxMBSegments]vp8common.MacroblockDequant
	segmentationState  vp8dec.SegmentationHeader
	reconstructScratch vp8dec.IntraReconstructionScratch
}

func (d *VP8Decoder) curBuf() *vp8common.FrameBuffer    { return &d.refPool[d.curIdx] }
func (d *VP8Decoder) lastBuf() *vp8common.FrameBuffer   { return &d.refPool[d.lastIdx] }
func (d *VP8Decoder) goldenBuf() *vp8common.FrameBuffer { return &d.refPool[d.goldenIdx] }
func (d *VP8Decoder) altBuf() *vp8common.FrameBuffer    { return &d.refPool[d.altIdx] }

func (d *VP8Decoder) selectFreeCurrentSlot() {
	for i := range d.refPool {
		if i != d.lastIdx && i != d.goldenIdx && i != d.altIdx {
			d.curIdx = i
			return
		}
	}
}

func (d *VP8Decoder) detachReferenceSlot(ref ReferenceFrame) (*vp8common.FrameBuffer, bool) {
	var target *int
	var otherA, otherB int
	switch ref {
	case ReferenceLast:
		target, otherA, otherB = &d.lastIdx, d.goldenIdx, d.altIdx
	case ReferenceGolden:
		target, otherA, otherB = &d.goldenIdx, d.lastIdx, d.altIdx
	case ReferenceAltRef:
		target, otherA, otherB = &d.altIdx, d.lastIdx, d.goldenIdx
	default:
		return nil, false
	}
	if *target == otherA || *target == otherB || *target == d.curIdx {
		for i := range d.refPool {
			if i != otherA && i != otherB && i != d.curIdx {
				*target = i
				break
			}
		}
	}
	return &d.refPool[*target], true
}

func (d *VP8Decoder) isClosed() bool {
	return d == nil || d.lifecycle == vp8DecoderClosed
}

func (d *VP8Decoder) isActive() bool {
	return d != nil && (d.lifecycle == vp8DecoderActive || d.lifecycle == vp8DecoderFrameReady)
}

// NewVP8Decoder creates a VP8 decoder with validated options. The zero
// value of opts is valid: it produces a single-threaded decoder with no
// postprocessing, no error concealment, and no dimension caps.
func NewVP8Decoder(opts DecoderOptions) (*VP8Decoder, error) {
	if err := validateDecoderOptions(opts); err != nil {
		return nil, err
	}
	d := &VP8Decoder{
		opts:            opts,
		lifecycle:       vp8DecoderNeedKey,
		residualCorrupt: -1,
		curIdx:          0,
		lastIdx:         1,
		goldenIdx:       2,
		altIdx:          3,
		coefProbs:       vp8tables.DefaultCoefProbs,
		frameCoefProbs:  vp8tables.DefaultCoefProbs,
	}
	vp8dec.ResetModeProbs(&d.modeProbs)
	vp8dec.ResetModeProbs(&d.frameModeProbs)
	return d, nil
}

// SetPostProcess changes libvpx-style decoder postprocessing at runtime.
// flags must be a combination of PostProcess* constants. noiseLevel must be
// in [0, 16] and requires PostProcessAddNoise when non-zero. The update is
// atomic: invalid inputs leave the existing decoder options unchanged.
func (d *VP8Decoder) SetPostProcess(flags PostProcessFlag, noiseLevel int) error {
	if d.isClosed() {
		return ErrClosed
	}
	next := d.opts
	next.PostProcessFlags = flags
	next.PostProcessNoiseLevel = noiseLevel
	return d.setPostProcessOptions(next)
}

// SetPostProcessConfig changes the full VP8_SET_POSTPROC configuration at
// runtime, including deblocking strength. deblockingLevel and noiseLevel must
// be in [0, 16]. The update is atomic.
func (d *VP8Decoder) SetPostProcessConfig(flags PostProcessFlag, deblockingLevel int, noiseLevel int) error {
	if d.isClosed() {
		return ErrClosed
	}
	next := d.opts
	next.PostProcessFlags = flags
	next.PostProcessDeblockingLevel = deblockingLevel
	next.PostProcessDeblockingLevelSet = true
	next.PostProcessNoiseLevel = noiseLevel
	return d.setPostProcessOptions(next)
}

func (d *VP8Decoder) setPostProcessOptions(next DecoderOptions) error {
	if err := validateDecoderOptions(next); err != nil {
		return err
	}
	if d.frameWidth > 0 && d.frameHeight > 0 {
		flags := next.effectivePostProcessFlags()
		if flags&PostProcessMFQE != 0 {
			if err := d.postprocState.EnsureMFQE(d.frameWidth, d.frameHeight); err != nil {
				return ErrInvalidConfig
			}
		}
		if flags&PostProcessAddNoise != 0 && next.PostProcessNoiseLevel > 0 {
			d.postprocState.EnsureNoise(d.frameWidth)
		}
	}
	d.opts.PostProcessFlags = next.PostProcessFlags
	d.opts.PostProcessDeblockingLevel = next.PostProcessDeblockingLevel
	d.opts.PostProcessDeblockingLevelSet = next.PostProcessDeblockingLevelSet
	d.opts.PostProcessNoiseLevel = next.PostProcessNoiseLevel
	return nil
}

// SetDecryptor changes the VP8D_SET_DECRYPTOR callback and opaque state used
// before parsing compressed bytes. Passing nil disables decryption.
func (d *VP8Decoder) SetDecryptor(decryptor VP8Decryptor, state any) error {
	if d.isClosed() {
		return ErrClosed
	}
	d.opts.Decryptor = decryptor
	d.opts.DecryptorState = state
	return nil
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
	d.decryptedPacket = buffers.EnsureLen(d.decryptedPacket, len(packet))
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
	if d.isClosed() {
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
			d.lifecycle = vp8DecoderActive
			if frameInfo.ShowFrame {
				output, err := d.outputReferenceFrameImage(info, &d.curBuf().Img)
				if err != nil {
					return err
				}
				d.lastFrame = publicImageFromVP8(output)
				d.lifecycle = vp8DecoderFrameReady
			}
			return nil
		}
		return err
	}
	if d.lifecycle == vp8DecoderNeedKey && !info.KeyFrame {
		return ErrNeedKeyFrame
	}
	if err := d.validateStreamInfo(info); err != nil {
		return err
	}
	if err := d.decodeFramePacket(packet, frame, info); err != nil {
		if d.opts.effectiveErrorConcealment() && d.canConceal(info) {
			frameInfo := d.finishConcealedFrame(info, pts)
			d.lifecycle = vp8DecoderActive
			if frameInfo.ShowFrame {
				output, err := d.outputReferenceFrameImage(info, &d.lastBuf().Img)
				if err != nil {
					return err
				}
				d.lastFrame = publicImageFromVP8(output)
				d.lifecycle = vp8DecoderFrameReady
			}
			return nil
		}
		return err
	}

	d.finishFrame(info, pts)
	if !info.ShowFrame || d.frameSuppress {
		d.lifecycle = vp8DecoderActive
		return nil
	}
	output, err := d.outputFrameImage(info)
	if err != nil {
		return err
	}
	d.lastFrame = publicImageFromVP8(output)
	d.lifecycle = vp8DecoderFrameReady
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
	if d == nil || d.lifecycle != vp8DecoderFrameReady {
		return Image{}, false
	}
	d.lifecycle = vp8DecoderActive
	return d.lastFrame, true
}

// LastFrameInfo returns metadata for the most recently decoded frame. ok
// is false on a nil or closed decoder, and before the first successful
// Decode/DecodeInto call.
func (d *VP8Decoder) LastFrameInfo() (FrameInfo, bool) {
	if !d.isActive() {
		return FrameInfo{}, false
	}
	return d.lastInfo, true
}

// LastQuantizer returns the public 0..63 quantizer and internal VP8 qindex
// for the most recently decoded frame. Mirrors libvpx's
// VPXD_GET_LAST_QUANTIZER control (vp8/decoder/onyxd_if.c
// vp8dx_get_quantizer). ok is false on a nil or closed decoder, and before
// the first successful Decode/DecodeInto call.
func (d *VP8Decoder) LastQuantizer() (public int, internal int, ok bool) {
	if !d.isActive() {
		return 0, 0, false
	}
	return d.lastInfo.Quantizer, d.lastInfo.InternalQuantizer, true
}

// LastFrameCorrupted reports whether the most recently decoded frame was
// flagged as corrupted by the decoder. Mirrors libvpx's
// VP8D_GET_FRAME_CORRUPTED control (vp8/vp8_dx_iface.c). ok is false on a
// nil or closed decoder, and before the first successful Decode/DecodeInto
// call.
func (d *VP8Decoder) LastFrameCorrupted() (corrupted bool, ok bool) {
	if !d.isActive() {
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
	if !d.isActive() {
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
	if !d.isActive() {
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
	if d.isClosed() {
		return ErrClosed
	}
	if err := d.setReferenceFrameCheck(ref, src); err != nil {
		return err
	}
	fb, ok := d.detachReferenceSlot(ref)
	if !ok {
		return ErrInvalidConfig
	}
	copyPublicImageToVP8(&fb.Img, src)
	fb.ExtendBorders()
	return nil
}

func (d *VP8Decoder) setReferenceFrameCheck(ref ReferenceFrame, src Image) error {
	if _, ok := d.referenceFrameBuffer(ref); !ok || !d.referenceFramesInitialized() {
		return ErrInvalidConfig
	}
	if !src.ValidForEncode(d.frameWidth, d.frameHeight) {
		return ErrInvalidConfig
	}
	return nil
}

// CopyReferenceFrame copies ref into dst. ref must be ReferenceLast,
// ReferenceGolden, or ReferenceAltRef; dst must match the stream dimensions
// established by the most recently decoded key frame and provide valid
// I420 strides. Returns [ErrInvalidConfig] when no key frame has been
// decoded yet or when dimensions or strides do not match.
func (d *VP8Decoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error {
	if d.isClosed() {
		return ErrClosed
	}
	if dst == nil {
		return ErrInvalidConfig
	}
	fb, ok := d.referenceFrameBuffer(ref)
	if !ok || !d.referenceFramesInitialized() {
		return ErrInvalidConfig
	}
	if !dst.ValidForEncode(d.frameWidth, d.frameHeight) {
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
	if d.isClosed() {
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
			if !dst.ValidForEncode(outputWidth, outputHeight) {
				return FrameInfo{}, ErrInvalidConfig
			}
			frameInfo, err := d.concealMissingInterFrame(info, pts)
			if err != nil {
				return FrameInfo{}, err
			}
			d.lifecycle = vp8DecoderActive
			if frameInfo.ShowFrame {
				output, err := d.outputReferenceFrameImage(info, &d.curBuf().Img)
				if err != nil {
					return FrameInfo{}, err
				}
				copyVP8ImageToPublic(dst, output)
			}
			return frameInfo, nil
		}
		return FrameInfo{}, err
	}
	if d.lifecycle == vp8DecoderNeedKey && !info.KeyFrame {
		return FrameInfo{}, ErrNeedKeyFrame
	}
	if err := d.validateStreamInfo(info); err != nil {
		return FrameInfo{}, err
	}
	outputWidth, outputHeight := d.outputDimensions(info)
	if !dst.ValidForEncode(outputWidth, outputHeight) {
		return FrameInfo{}, ErrInvalidConfig
	}
	if err := d.decodeFramePacket(packet, frame, info); err != nil {
		if d.opts.effectiveErrorConcealment() && d.canConceal(info) {
			frameInfo := d.finishConcealedFrame(info, pts)
			d.lifecycle = vp8DecoderActive
			if frameInfo.ShowFrame {
				output, err := d.outputReferenceFrameImage(info, &d.lastBuf().Img)
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
	d.lifecycle = vp8DecoderActive
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
	d.lifecycle = vp8DecoderNeedKey
	d.lastFrame = Image{}
	d.lastInfo = FrameInfo{}
	d.currentPTS = 0
	d.visibleFrames = 0
	d.frameWidth = 0
	d.frameHeight = 0
	d.mbRows = 0
	d.mbCols = 0
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
	for i := range d.refPool {
		d.refPool[i].Reset()
	}
	d.curIdx = 0
	d.lastIdx = 1
	d.goldenIdx = 2
	d.altIdx = 3
	d.post.Reset()
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
	if d.isClosed() {
		return ErrClosed
	}
	d.Reset()
	d.lifecycle = vp8DecoderClosed
	return nil
}

func (d *VP8Decoder) decodeFramePacket(packet []byte, frame vp8dec.FrameHeader, info StreamInfo) error {
	if d.lifecycle == vp8DecoderFrameReady {
		// Invalidate any unconsumed NextFrame alias before mutating d.current.
		d.lifecycle = vp8DecoderActive
	}
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
	d.selectFreeCurrentSlot()
	if err := d.decodeModeGrid(info); err != nil {
		return err
	}
	if errorConcealment && d.modesCorrupt < d.mbRows*d.mbCols {
		d.ecOverlaps = buffers.EnsureLen(d.ecOverlaps, d.mbRows*d.mbCols)
		if err := vp8dec.EstimateMissingMotionVectorsWithScratch(d.modes, d.prevModes, d.mbRows, d.mbCols, d.modesCorrupt, d.ecOverlaps); err != nil {
			return ErrInvalidData
		}
		d.zeroCorruptMacroblockTokens(d.modesCorrupt)
		vp8dec.ApplyCorruptInterFrameRefresh(&d.state)
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
	if uint(opts.PostProcessDeblockingLevel) > 16 {
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

func (opts DecoderOptions) effectivePostProcessDeblockingLevel() int {
	if opts.PostProcessDeblockingLevelSet || opts.PostProcessDeblockingLevel != 0 {
		return opts.PostProcessDeblockingLevel
	}
	return vp8dec.DefaultPostProcessDeblockingLevel
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
	if d.isActive() && d.opts.RejectResolutionChange {
		if info.Width != d.lastInfo.Width || info.Height != d.lastInfo.Height {
			return ErrFrameRejected
		}
	}
	return nil
}

func (d *VP8Decoder) finishFrame(info StreamInfo, pts uint64) FrameInfo {
	d.currentPTS = pts
	d.lifecycle = vp8DecoderActive
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
	if info.ShowFrame && !d.frameSuppress {
		d.visibleFrames++
	}
	return frameInfo
}

func (d *VP8Decoder) canConceal(info StreamInfo) bool {
	return d.isActive() &&
		!info.KeyFrame &&
		d.frameWidth > 0 &&
		d.frameHeight > 0 &&
		d.lastBuf().BufferLen() != 0
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
	d.lifecycle = vp8DecoderActive
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
	d.ecOverlaps = buffers.EnsureLen(d.ecOverlaps, d.mbRows*d.mbCols)
	if err := vp8dec.EstimateMissingMotionVectorsWithScratch(d.modes, d.prevModes, d.mbRows, d.mbCols, 0, d.ecOverlaps); err != nil {
		return FrameInfo{}, ErrInvalidData
	}
	d.selectFreeCurrentSlot()
	if err := d.reconstructFrame(StreamInfo{Profile: 0}); err != nil {
		return FrameInfo{}, err
	}
	d.lastIdx = d.curIdx
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
	return d.outputReferenceFrameImage(info, &d.curBuf().Img)
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
		DeblockingLevel: d.opts.effectivePostProcessDeblockingLevel(),
		NoiseLevel:      d.opts.PostProcessNoiseLevel,
		BaseQIndex:      int(d.state.Quant.BaseQIndex),
		CurrentFrame:    d.visibleFrames,
		KeyFrame:        info.KeyFrame,
	}
	if d.post.Img.Width != src.Width || d.post.Img.Height != src.Height || d.post.BufferLen() == 0 {
		if err := d.post.Resize(src.Width, src.Height, 32, 32); err != nil {
			return nil, ErrInvalidData
		}
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
	if d.frameWidth == info.Width && d.frameHeight == info.Height && d.refPool[0].BufferLen() != 0 {
		return nil
	}
	for i := range d.refPool {
		if err := d.refPool[i].Resize(info.Width, info.Height, 32, 32); err != nil {
			return ErrInvalidData
		}
	}
	flags := d.opts.effectivePostProcessFlags()
	if flags != 0 {
		if err := d.post.Resize(info.Width, info.Height, 32, 32); err != nil {
			return ErrInvalidData
		}
	}
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
			vp8dec.ApplyCorruptInterFrameRefresh(&state)
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
	state.Segmentation = vp8dec.MergeSegmentationHeader(previousSegmentation, state.Segmentation)
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
	cur := d.curBuf()
	if d.opts.Threads >= 2 && d.mbRows > 1 {
		if err := vp8dec.ReconstructAndLoopFilterPipelined(
			&cur.Img, &d.lastBuf().Img, &d.goldenBuf().Img, &d.altBuf().Img,
			d.mbRows, d.mbCols, d.modes, d.tokens, &d.dequants, &d.reconstructScratch, cfg,
			info.KeyFrame,
			!skipLoopFilter,
			frameType, loopFilter, d.state.Segmentation, &d.loopInfo,
		); err != nil {
			return ErrInvalidData
		}
		cur.ExtendBorders()
		return nil
	}

	if info.KeyFrame {
		if err := vp8dec.ReconstructKeyFrameIntraGrid(&cur.Img, d.mbRows, d.mbCols, d.modes, d.tokens, &d.dequants, &d.reconstructScratch); err != nil {
			return ErrInvalidData
		}
	} else {
		if err := vp8dec.ReconstructInterFrameGridWithConfig(&cur.Img, &d.lastBuf().Img, &d.goldenBuf().Img, &d.altBuf().Img, d.mbRows, d.mbCols, d.modes, d.tokens, &d.dequants, &d.reconstructScratch, cfg); err != nil {
			return ErrInvalidData
		}
	}
	if !skipLoopFilter {
		vp8dec.ApplyLoopFilterUnchecked(&cur.Img, d.mbRows, d.mbCols, d.modes, frameType, loopFilter, d.state.Segmentation, &d.loopInfo)
	}
	cur.ExtendBorders()
	return nil
}

// refreshReferences applies the per-frame reference buffer copy/refresh
// flags parsed from the inter-frame header using zero-copy reference pool
// index swaps (mirroring libvpx's swap_frame_buffers).
func (d *VP8Decoder) refreshReferences() bool {
	suppress := false
	switch d.state.Refresh.CopyBufferToAltRef {
	case 1:
		d.altIdx = d.lastIdx
	case 2:
		d.altIdx = d.goldenIdx
	case 3:
		suppress = true
	}
	switch d.state.Refresh.CopyBufferToGolden {
	case 1:
		d.goldenIdx = d.lastIdx
	case 2:
		d.goldenIdx = d.altIdx
	case 3:
		suppress = true
	}
	if d.state.Refresh.RefreshLast {
		d.lastIdx = d.curIdx
	}
	if d.state.Refresh.RefreshGolden {
		d.goldenIdx = d.curIdx
	}
	if d.state.Refresh.RefreshAltRef {
		d.altIdx = d.curIdx
	}
	return suppress
}

// referenceFrameBuffer maps the public reference selector to the decoder-owned
// bordered buffer. Invalid selectors include combined ReferenceFlags values.
func (d *VP8Decoder) referenceFrameBuffer(ref ReferenceFrame) (*vp8common.FrameBuffer, bool) {
	switch ref {
	case ReferenceLast:
		return d.lastBuf(), true
	case ReferenceGolden:
		return d.goldenBuf(), true
	case ReferenceAltRef:
		return d.altBuf(), true
	default:
		return nil, false
	}
}

// referenceFramesInitialized reports whether set/copy reference controls can
// safely use the reference buffers. govpx requires a decoded key frame to
// establish stream dimensions before callers can replace decoder references.
func (d *VP8Decoder) referenceFramesInitialized() bool {
	return d.isActive() &&
		d.frameWidth > 0 &&
		d.frameHeight > 0 &&
		d.lastBuf().BufferLen() != 0
}

func copyVP8ImageToPublic(dst *Image, src *vp8common.Image) {
	buffers.CopyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	buffers.CopyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	buffers.CopyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
}

// copyPublicImageToVP8 copies only visible samples into a bordered VP8 image;
// callers that install a reference must extend borders afterwards.
func copyPublicImageToVP8(dst *vp8common.Image, src Image) {
	buffers.CopyPlane(dst.Y, dst.YStride, src.Y, src.YStride, dst.Width, dst.Height)
	uvWidth := (dst.Width + 1) >> 1
	uvHeight := (dst.Height + 1) >> 1
	buffers.CopyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	buffers.CopyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
}

func (d *VP8Decoder) ensureWorkspace(width int, height int) {
	cols := (width + 15) >> 4
	rows := (height + 15) >> 4
	count := rows * cols
	d.modes = buffers.EnsureLen(d.modes, count)
	if d.opts.ErrorConcealment {
		d.prevModes = buffers.EnsureLen(d.prevModes, count)
		d.ecOverlaps = buffers.EnsureLen(d.ecOverlaps, count)
	}
	d.tokens = buffers.EnsureLen(d.tokens, count)
	d.tokenAbove = buffers.EnsureLen(d.tokenAbove, cols)
	d.segmentMap = buffers.EnsureLen(d.segmentMap, count)
	scratchLen := cols * 24
	d.postprocScratch = buffers.EnsureLen(d.postprocScratch, scratchLen)
	flags := d.opts.effectivePostProcessFlags()
	if flags&PostProcessAddNoise != 0 && d.opts.PostProcessNoiseLevel > 0 {
		d.postprocState.EnsureNoise(width)
	}
	d.mbRows = rows
	d.mbCols = cols
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
