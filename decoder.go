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

	allPostProcessFlags    = PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise | PostProcessMFQE
	legacyPostProcessFlags = PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE
)

// DecoderOptions configures a VP8 decoder.
type DecoderOptions struct {
	// Threads selects decoder worker count. Values greater than one enable the
	// row pipeline where supported; zero uses the serial path.
	Threads int

	// ErrorConcealment enables libvpx-style concealment for corrupt interframes
	// after a clean keyframe has initialized references.
	ErrorConcealment bool
	// ErrorResilient is kept as a compatibility alias for ErrorConcealment.
	ErrorResilient bool
	// PostProcess enables the legacy libvpx-style postprocess chain:
	// deblock, demacroblock, and MFQE. Prefer PostProcessFlags for new code.
	PostProcess bool
	// PostProcessFlags selects individual libvpx-style postprocess filters.
	// Zero disables postprocessing unless PostProcess is set.
	PostProcessFlags PostProcessFlag
	// PostProcessNoiseLevel enables libvpx-style additive luma noise when
	// PostProcess is true or PostProcessAddNoise is set. Zero disables
	// additive noise; valid range is [0, 16].
	PostProcessNoiseLevel int

	// MaxWidth and MaxHeight reject key frames larger than the configured
	// dimensions when non-zero.
	MaxWidth  int
	MaxHeight int

	// If true, Decode returns an explicit error when resolution changes.
	// If false, decoder may reallocate internal frame buffers on keyframe
	// resolution change.
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
	modesCorrupt    int
	residualCorrupt int

	frameWidth  int
	frameHeight int
	current     vp8common.FrameBuffer
	post        vp8common.FrameBuffer
	lastRef     vp8common.FrameBuffer
	goldenRef   vp8common.FrameBuffer
	altRef      vp8common.FrameBuffer

	mbRows             int
	mbCols             int
	modes              []vp8dec.MacroblockMode
	prevModes          []vp8dec.MacroblockMode
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

// NewVP8Decoder creates a VP8 decoder with validated options.
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

// Decode decodes one raw VP8 frame payload and queues visible output for
// NextFrame.
func (d *VP8Decoder) Decode(packet []byte) error {
	return d.DecodeWithPTS(packet, 0)
}

// DecodeWithPTS decodes one raw VP8 frame payload and records pts in the
// resulting FrameInfo.
func (d *VP8Decoder) DecodeWithPTS(packet []byte, pts uint64) error {
	if d == nil || d.closed {
		return ErrClosed
	}
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
	if !info.ShowFrame {
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

// NextFrame returns the most recent visible decoded frame, if one is queued.
// The returned image aliases decoder-owned storage until the next Decode,
// Reset, or Close.
func (d *VP8Decoder) NextFrame() (Image, bool) {
	if d == nil || d.closed || !d.frameReady {
		return Image{}, false
	}
	d.frameReady = false
	return d.lastFrame, true
}

// LastFrameInfo returns metadata for the most recently decoded frame.
func (d *VP8Decoder) LastFrameInfo() (FrameInfo, bool) {
	if d == nil || d.closed || !d.lastInfoValid {
		return FrameInfo{}, false
	}
	return d.lastInfo, true
}

// SetReferenceFrame replaces an initialized decoder reference buffer with src.
// The source image must match the current stream dimensions.
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

// CopyReferenceFrame copies an initialized decoder reference buffer into dst.
// The destination image must match the current stream dimensions.
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

// DecodeInto decodes one raw VP8 frame payload into caller-owned output
// storage when the packet is visible.
func (d *VP8Decoder) DecodeInto(packet []byte, dst *Image) (FrameInfo, error) {
	return d.DecodeIntoWithPTS(packet, dst, 0)
}

// DecodeIntoWithPTS decodes one raw VP8 frame payload into caller-owned output
// storage and records pts in the returned FrameInfo.
func (d *VP8Decoder) DecodeIntoWithPTS(packet []byte, dst *Image, pts uint64) (FrameInfo, error) {
	if d == nil || d.closed {
		return FrameInfo{}, ErrClosed
	}
	if dst == nil {
		return FrameInfo{}, ErrInvalidConfig
	}
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
	if !info.ShowFrame {
		return frameInfo, nil
	}
	output, err := d.outputFrameImage(info)
	if err != nil {
		return FrameInfo{}, err
	}
	copyVP8ImageToPublic(dst, output)
	return frameInfo, nil
}

// Reset returns the decoder to its cold-start state while retaining allocated
// buffers for reuse.
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

// Close releases decoder state. Further method calls return ErrClosed or no
// output.
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
		if err := vp8dec.EstimateMissingMotionVectors(d.modes, d.prevModes, d.mbRows, d.mbCols, d.modesCorrupt); err != nil {
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
	d.refreshReferences()
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
	if opts.MaxWidth < 0 || opts.MaxHeight < 0 {
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
	flags := opts.PostProcessFlags
	if flags == 0 && opts.PostProcess {
		flags = legacyPostProcessFlags
		if opts.PostProcessNoiseLevel > 0 {
			flags |= PostProcessAddNoise
		}
	}
	return flags
}

func (opts DecoderOptions) effectiveErrorConcealment() bool {
	return opts.ErrorConcealment || opts.ErrorResilient
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
	if info.Width <= 0 || info.Height <= 0 {
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
		Quantizer:         libvpxQIndexToPublicQuantizer(int(d.state.Quant.BaseQIndex)),
		InternalQuantizer: int(d.state.Quant.BaseQIndex),
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
		Quantizer:         libvpxQIndexToPublicQuantizer(int(d.previousQuant.BaseQIndex)),
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
	if err := vp8dec.EstimateMissingMotionVectors(d.modes, d.prevModes, d.mbRows, d.mbCols, 0); err != nil {
		return FrameInfo{}, ErrInvalidData
	}
	if err := d.reconstructFrame(StreamInfo{Profile: 0}); err != nil {
		return FrameInfo{}, err
	}
	copyExtendedFrameImage(&d.lastRef.Img, &d.current.Img)
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

func (d *VP8Decoder) refreshReferences() {
	switch d.state.Refresh.CopyBufferToAltRef {
	case 1:
		copyExtendedFrameImage(&d.altRef.Img, &d.lastRef.Img)
	case 2:
		copyExtendedFrameImage(&d.altRef.Img, &d.goldenRef.Img)
	}
	switch d.state.Refresh.CopyBufferToGolden {
	case 1:
		copyExtendedFrameImage(&d.goldenRef.Img, &d.lastRef.Img)
	case 2:
		copyExtendedFrameImage(&d.goldenRef.Img, &d.altRef.Img)
	}
	if d.state.Refresh.RefreshLast {
		copyExtendedFrameImage(&d.lastRef.Img, &d.current.Img)
	}
	if d.state.Refresh.RefreshGolden {
		copyExtendedFrameImage(&d.goldenRef.Img, &d.current.Img)
	}
	if d.state.Refresh.RefreshAltRef {
		copyExtendedFrameImage(&d.altRef.Img, &d.current.Img)
	}
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

func copyExtendedFrameImage(dst *vp8common.Image, src *vp8common.Image) {
	copy(dst.YFull, src.YFull)
	copy(dst.UFull, src.UFull)
	copy(dst.VFull, src.VFull)
}

func copyFrameImage(dst *vp8common.Image, src *vp8common.Image) {
	copy(dst.Y, src.Y)
	copy(dst.U, src.U)
	copy(dst.V, src.V)
}

func copyFrameImageLuma(dst *vp8common.Image, src *vp8common.Image) {
	if dst == nil || src == nil {
		return
	}
	width := min(dst.CodedWidth, src.CodedWidth)
	height := min(dst.CodedHeight, src.CodedHeight)
	if width <= 0 || height <= 0 {
		return
	}
	if dst.YStride == src.YStride && width == dst.YStride {
		copy(dst.Y[:height*dst.YStride], src.Y[:height*src.YStride])
		return
	}
	for row := range height {
		copy(dst.Y[row*dst.YStride:row*dst.YStride+width], src.Y[row*src.YStride:row*src.YStride+width])
	}
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
