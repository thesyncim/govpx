package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// denoiserPickmodeMVBias returns the libvpx pickmode_mv_bias multiplier from
// the allocated denoiser state, or 100 (no bias) when the denoiser is off.
// Runtime nonzero noise-sensitivity controls leave libvpx's active denoiser
// parameters sticky, so this must not be recalculated from oxcf every frame.
func (e *VP8Encoder) denoiserPickmodeMVBias() int {
	if e.opts.NoiseSensitivity <= 0 {
		return 100
	}
	if e.denoiser.allocated && e.denoiser.mode != vp8enc.DenoiserOff {
		return e.denoiser.params.PickmodeMVBias
	}
	_, params := vp8enc.DenoiserSetParameters(vp8enc.DenoiserModeForSensitivity(e.opts.NoiseSensitivity))
	return params.PickmodeMVBias
}

// denoiserState carries the libvpx-style running-average buffers, mode, and
// per-MB filter-state map that survives across frames.
type denoiserState struct {
	mode      int
	params    vp8enc.DenoiseParams
	allocated bool
	width     int
	height    int

	// Running averages for each reference: index 0 is INTRA (the in-progress
	// frame's running average that becomes LAST), 1=LAST, 2=GOLDEN, 3=ALTREF.
	runningAvg [4]vp8common.FrameBuffer
	refStates  [4]vp8dec.InterFrameRefState
	mcRunning  vp8common.FrameBuffer
	source     vp8common.FrameBuffer
	mcY        [16 * 16]byte
	mcU        [8 * 8]byte
	mcV        [8 * 8]byte

	state []uint8
}

func (d *denoiserState) ensureAllocated(width int, height int) error {
	if d.allocated && d.width == width && d.height == height {
		return nil
	}
	for i := range d.runningAvg {
		if err := d.runningAvg[i].Resize(width, height, 32, 32); err != nil {
			return ErrInvalidConfig
		}
		d.refStates[i] = vp8dec.PrepareInterFrameRefState(&d.runningAvg[i].Img, vp8dec.InterPredictionConfig{})
	}
	if err := d.mcRunning.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := d.source.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	stateLen := rows * cols
	if cap(d.state) < stateLen {
		d.state = make([]uint8, stateLen)
	} else {
		d.state = d.state[:stateLen]
		clear(d.state)
	}
	d.allocated = true
	d.width = width
	d.height = height
	return nil
}

func (d *denoiserState) reset() {
	for i := range d.runningAvg {
		d.runningAvg[i].Reset()
		d.refStates[i] = vp8dec.InterFrameRefState{}
	}
	d.mcRunning.Reset()
	d.source.Reset()
	for i := range d.state {
		d.state[i] = 0
	}
	d.allocated = false
}

type denoiserMacroblockDecision struct {
	bestSSE              uint32
	zeroMVSSE            uint32
	bestMV               vp8enc.MotionVector
	bestReferenceFrame   vp8common.MVReferenceFrame
	bestMode             vp8common.MBPredictionMode
	zeroMVReferenceFrame vp8common.MVReferenceFrame
	useSkinGate          bool
}

func newDenoiserMacroblockDecision() denoiserMacroblockDecision {
	const maxUint32 = ^uint32(0)
	return denoiserMacroblockDecision{
		bestReferenceFrame:   vp8common.IntraFrame,
		zeroMVReferenceFrame: vp8common.IntraFrame,
		bestSSE:              maxUint32,
		zeroMVSSE:            maxUint32,
	}
}

// Inactive active-map MBs still enter libvpx's denoiser as skipped inter
// candidates, so the denoiser must see the zero-MV reference rather than a
// no-filter intra sentinel.
func (d *denoiserMacroblockDecision) recordInactiveInterCandidate(ref vp8common.MVReferenceFrame, mode vp8common.MBPredictionMode, mv vp8enc.MotionVector) {
	d.bestReferenceFrame = ref
	d.bestMode = mode
	d.bestMV = mv
	d.bestSSE = 0
	if mode == vp8common.ZeroMV {
		d.zeroMVReferenceFrame = ref
		d.zeroMVSSE = 0
	}
}

func (e *VP8Encoder) denoiserReferenceTooOld(ref vp8common.MVReferenceFrame) bool {
	if ref == vp8common.LastFrame || ref <= vp8common.IntraFrame || ref >= vp8common.MaxRefFrames {
		return false
	}
	return e.frameCount > e.referenceFrameNumbers[ref] &&
		e.frameCount-e.referenceFrameNumbers[ref] > vp8enc.DenoiserMaxGFARFRange
}

func denoiserReferenceAvgIndexForMVRef(ref vp8common.MVReferenceFrame) (int, bool) {
	switch ref {
	case vp8common.LastFrame:
		return denoiserAvgLast, true
	case vp8common.GoldenFrame:
		return denoiserAvgGolden, true
	case vp8common.AltRefFrame:
		return denoiserAvgAltRef, true
	default:
		return 0, false
	}
}

// Indices into denoiserState.runningAvg matching libvpx's reference order:
// INTRA is the in-progress denoised buffer that becomes LAST (or GOLDEN/
// ALTREF) at frame end via copyDenoiserAvgForRefresh.
const (
	denoiserAvgIntra = iota
	denoiserAvgLast
	denoiserAvgGolden
	denoiserAvgAltRef
)

// initDenoiserAvgFromKeyFrame mirrors update_reference_frames' key-frame
// branch (onyx_if.c): seed every running_avg buffer from the key-frame
// source so subsequent inter frames have a defined reference for
// motion-compensated denoising.
func (e *VP8Encoder) initDenoiserAvgFromKeyFrame(source vp8enc.SourceImage) {
	if e.opts.NoiseSensitivity <= 0 || !e.denoiser.allocated {
		return
	}
	for i := range e.denoiser.runningAvg {
		vp8enc.CopySourceToFrameBuffer(&e.denoiser.runningAvg[i], source)
	}
	for i := range e.denoiser.state {
		e.denoiser.state[i] = vp8enc.DenoiserStateNoFilter
	}
}

func (e *VP8Encoder) applyDenoiserToInterMacroblock(source vp8enc.SourceImage, filtered vp8enc.SourceImage, rows int, cols int, row int, col int, decision *interFrameModeDecision) {
	if e.opts.NoiseSensitivity <= 0 || !e.denoiser.allocated || decision == nil {
		return
	}
	if rows <= 0 || cols <= 0 || row < 0 || row >= rows || col < 0 || col >= cols {
		return
	}
	index := row*cols + col
	if len(e.denoiser.state) <= index {
		return
	}
	d := decision.denoise
	if d.zeroMVReferenceFrame == vp8common.IntraFrame {
		e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
		return
	}

	frame := d.bestReferenceFrame
	mode := d.bestMode
	mv := d.bestMV
	bestSSE := d.bestSSE
	zeroSSE := uint32(uint64(d.zeroMVSSE) * uint64(e.denoiser.params.DenoiseMVBias) / 100)
	sseDiff := int64(zeroSSE) - int64(bestSSE)
	motionMag := uint32(int(mv.Row)*int(mv.Row) + int(mv.Col)*int(mv.Col))
	sseDiffThresh := 0
	if motionMag <= denoiserNoiseMotionThreshold {
		sseDiffThresh = vp8enc.DenoiserSSEDiffThreshold
	}
	denoiseZeroMV := frame == vp8common.IntraFrame || sseDiff <= int64(sseDiffThresh)
	if denoiseZeroMV {
		frame = d.zeroMVReferenceFrame
		mode = vp8common.ZeroMV
		mv = vp8enc.MotionVector{}
		bestSSE = zeroSSE
		motionMag = 0
	}

	avgIndex, ok := denoiserReferenceAvgIndexForMVRef(frame)
	if !ok {
		e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
		return
	}
	increase := motionMag < uint32(e.denoiser.params.ScaleIncreaseFilter)*denoiserNoiseMotionThreshold
	sseThresh := uint32(e.denoiser.params.ScaleSSEThresh * vp8enc.DenoiserSSEThreshold)
	if increase {
		sseThresh = uint32(e.denoiser.params.ScaleSSEThresh * vp8enc.DenoiserSSEThresholdHigh)
	}
	motionThresh := uint32(e.denoiser.params.ScaleMotionThresh) * denoiserNoiseMotionThreshold
	skinBlocksFilter := d.useSkinGate && e.denoiserSkinGateBlocksFilter(row, col, cols, index, motionMag)
	if bestSSE > sseThresh || motionMag > motionThresh || skinBlocksFilter {
		e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
		return
	}

	mcMode := vp8enc.InterFrameMacroblockMode{
		RefFrame:    frame,
		Mode:        mode,
		MV:          mv,
		UVMode:      vp8common.DCPred,
		MBSkipCoeff: true,
	}
	var decMode vp8dec.MacroblockMode
	vp8enc.ConvertInterFrameMode(&mcMode, &decMode)
	var zeroTokens vp8dec.MacroblockTokens
	mcY := e.denoiser.mcY[:]
	mcU := e.denoiser.mcU[:]
	mcV := e.denoiser.mcV[:]
	mcYStride, mcUStride, mcVStride := 16, 8, 8
	if !vp8dec.ReconstructWholeMVInterMacroblockWithState(
		&e.denoiser.refStates[avgIndex], &decMode, &zeroTokens, &e.dequants[0],
		mcY, mcYStride, mcU, mcUStride, mcV, mcVStride,
		&e.reconstructScratch.Residual, row, col,
	) {
		if !reconstructInterAnalysisMacroblockWithState(&e.denoiser.mcRunning.Img, &e.denoiser.runningAvg[avgIndex].Img, &e.denoiser.refStates[avgIndex], row, col, &decMode, &zeroTokens, &e.dequants[0], &e.reconstructScratch) {
			e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
			return
		}
		yMcOff := row*16*e.denoiser.mcRunning.Img.YStride + col*16
		uMcOff := row*8*e.denoiser.mcRunning.Img.UStride + col*8
		vMcOff := row*8*e.denoiser.mcRunning.Img.VStride + col*8
		mcY = e.denoiser.mcRunning.Img.Y[yMcOff:]
		mcU = e.denoiser.mcRunning.Img.U[uMcOff:]
		mcV = e.denoiser.mcRunning.Img.V[vMcOff:]
		mcYStride = e.denoiser.mcRunning.Img.YStride
		mcUStride = e.denoiser.mcRunning.Img.UStride
		mcVStride = e.denoiser.mcRunning.Img.VStride
	}

	avg := &e.denoiser.runningAvg[denoiserAvgIntra]
	yOff := row*16*source.YStride + col*16
	uOff := row*8*source.UStride + col*8
	vOff := row*8*source.VStride + col*8
	ySigOff := row*16*filtered.YStride + col*16
	uSigOff := row*8*filtered.UStride + col*8
	vSigOff := row*8*filtered.VStride + col*8
	yAvgOff := row*16*avg.Img.YStride + col*16
	uAvgOff := row*8*avg.Img.UStride + col*8
	vAvgOff := row*8*avg.Img.VStride + col*8
	filteredSourceY := sourceImagePlaneMatches(source.Y, source.YStride, filtered.Y, filtered.YStride)
	filteredSourceU := sourceImagePlaneMatches(source.U, source.UStride, filtered.U, filtered.UStride)
	filteredSourceV := sourceImagePlaneMatches(source.V, source.VStride, filtered.V, filtered.VStride)

	filterDecision := vp8enc.DenoiserFilterY(
		mcY, mcYStride,
		avg.Img.Y[yAvgOff:], avg.Img.YStride,
		filtered.Y[ySigOff:], filtered.YStride,
		motionMag, increase,
	)
	if filterDecision == vp8enc.DenoiserFilterBlock {
		if motionMag > 0 {
			e.denoiser.state[index] = vp8enc.DenoiserStateFilterNonZero
		} else {
			e.denoiser.state[index] = vp8enc.DenoiserStateFilterZeroMV
		}
	} else {
		e.denoiser.state[index] = vp8enc.DenoiserStateNoFilter
		copyMacroblockY(avg.Img.Y[yAvgOff:], avg.Img.YStride, source.Y[yOff:], source.YStride)
		if !filteredSourceY {
			copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, source.Y[yOff:], source.YStride)
		}
	}

	applySpatialFilter := func() {
		if e.applyDenoiserSpatialLoopFilter(filtered, avg, row, col, cols, index, ySigOff, yAvgOff) {
			copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, avg.Img.Y[yAvgOff:], avg.Img.YStride)
		}
	}
	if e.denoiser.mode == vp8enc.DenoiserOnYOnly {
		applySpatialFilter()
		return
	}
	if motionMag == 0 && filterDecision == vp8enc.DenoiserFilterBlock {
		if vp8enc.DenoiserFilterUV(
			mcU, mcUStride,
			avg.Img.U[uAvgOff:], avg.Img.UStride,
			filtered.U[uSigOff:], filtered.UStride,
			motionMag, false,
		) == vp8enc.DenoiserCopyBlock {
			copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
			if !filteredSourceU {
				copyMacroblock8x8(filtered.U[uSigOff:], filtered.UStride, source.U[uOff:], source.UStride)
			}
		}
		if vp8enc.DenoiserFilterUV(
			mcV, mcVStride,
			avg.Img.V[vAvgOff:], avg.Img.VStride,
			filtered.V[vSigOff:], filtered.VStride,
			motionMag, false,
		) == vp8enc.DenoiserCopyBlock {
			copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
			if !filteredSourceV {
				copyMacroblock8x8(filtered.V[vSigOff:], filtered.VStride, source.V[vOff:], source.VStride)
			}
		}
		applySpatialFilter()
		return
	}
	copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
	copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
	if !filteredSourceU {
		copyMacroblock8x8(filtered.U[uSigOff:], filtered.UStride, source.U[uOff:], source.UStride)
	}
	if !filteredSourceV {
		copyMacroblock8x8(filtered.V[vSigOff:], filtered.VStride, source.V[vOff:], source.VStride)
	}
	applySpatialFilter()
}

func (e *VP8Encoder) applyDenoiserSpatialLoopFilter(filtered vp8enc.SourceImage, avg *vp8common.FrameBuffer, row int, col int, cols int, index int, ySigOff int, yAvgOff int) bool {
	const filterLevel = 48
	currentState := e.denoiser.state[index]
	applyFilterCol := false
	applyFilterRow := false
	if col > 0 {
		left := e.denoiser.state[index-1]
		applyFilterCol = !(currentState == left && currentState != vp8enc.DenoiserStateFilterNonZero)
	}
	if row > 0 {
		above := e.denoiser.state[index-cols]
		applyFilterRow = !(currentState == above && currentState != vp8enc.DenoiserStateFilterNonZero)
	}
	if !applyFilterCol && !applyFilterRow {
		return false
	}

	hev := e.loopInfo.HEVThresh[e.loopInfo.HEVThreshLUT[vp8common.InterFrame][filterLevel]]
	mblim := e.loopInfo.MBLimit[filterLevel]
	lim := e.loopInfo.Limit[filterLevel]
	y := avg.Img.Y
	stride := avg.Img.YStride
	if applyFilterCol {
		dsp.MBLoopFilterVerticalEdge(y[yAvgOff-4:], stride, mblim, lim, hev, 2)
	}
	if applyFilterRow {
		dsp.MBLoopFilterHorizontalEdge(y[yAvgOff-4*stride:], stride, mblim, lim, hev, 2)
	}
	return len(filtered.Y) > ySigOff
}

func (e *VP8Encoder) denoiserSkinGateBlocksFilter(row int, col int, cols int, index int, motionMag uint32) bool {
	if !e.macroblockIsSkin(row, col, cols) {
		return false
	}
	consecZeroLastMVBias := 0
	if uint(index) < uint(len(e.consecZeroLastMVBias)) {
		consecZeroLastMVBias = int(e.consecZeroLastMVBias[index])
	}
	return consecZeroLastMVBias < 2 || motionMag > 0
}

func (e *VP8Encoder) copyDenoiserNoFilterMacroblock(source vp8enc.SourceImage, filtered vp8enc.SourceImage, row int, col int, cols int, index int) {
	avg := &e.denoiser.runningAvg[denoiserAvgIntra]
	yOff := row*16*source.YStride + col*16
	uOff := row*8*source.UStride + col*8
	vOff := row*8*source.VStride + col*8
	ySigOff := row*16*filtered.YStride + col*16
	uSigOff := row*8*filtered.UStride + col*8
	vSigOff := row*8*filtered.VStride + col*8
	yAvgOff := row*16*avg.Img.YStride + col*16
	uAvgOff := row*8*avg.Img.UStride + col*8
	vAvgOff := row*8*avg.Img.VStride + col*8
	copyMacroblockY(avg.Img.Y[yAvgOff:], avg.Img.YStride, source.Y[yOff:], source.YStride)
	if !sourceImagePlaneMatches(source.Y, source.YStride, filtered.Y, filtered.YStride) {
		copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, source.Y[yOff:], source.YStride)
	}
	if e.denoiser.mode != vp8enc.DenoiserOnYOnly {
		copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
		copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
		if !sourceImagePlaneMatches(source.U, source.UStride, filtered.U, filtered.UStride) {
			copyMacroblock8x8(filtered.U[uSigOff:], filtered.UStride, source.U[uOff:], source.UStride)
		}
		if !sourceImagePlaneMatches(source.V, source.VStride, filtered.V, filtered.VStride) {
			copyMacroblock8x8(filtered.V[vSigOff:], filtered.VStride, source.V[vOff:], source.VStride)
		}
	}
	e.denoiser.state[index] = vp8enc.DenoiserStateNoFilter
	if e.applyDenoiserSpatialLoopFilter(filtered, avg, row, col, cols, index, ySigOff, yAvgOff) {
		copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, avg.Img.Y[yAvgOff:], avg.Img.YStride)
	}
}

func sourceImagePlaneMatches(a []byte, aStride int, b []byte, bStride int) bool {
	if aStride != bStride || len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &b[0]
}

func copyMacroblockY(dst []byte, dstStride int, src []byte, srcStride int) {
	for r := range 16 {
		copy(dst[r*dstStride:r*dstStride+16], src[r*srcStride:r*srcStride+16])
	}
}

func copyMacroblock8x8(dst []byte, dstStride int, src []byte, srcStride int) {
	for r := range 8 {
		copy(dst[r*dstStride:r*dstStride+8], src[r*srcStride:r*srcStride+8])
	}
}

// copyDenoiserAvgForRefresh mirrors update_reference_frames' denoiser branch:
// after the encoded frame is committed, copy running_avg[INTRA] into the
// per-reference running_avg buffers that this frame's refresh/copy policy
// updates, keeping the denoiser's parallel reference stream in sync with the
// encoder's references.
func (e *VP8Encoder) copyDenoiserAvgForRefresh(cfg vp8enc.InterFrameStateConfig) {
	if e.opts.NoiseSensitivity <= 0 || !e.denoiser.allocated {
		return
	}
	intra := &e.denoiser.runningAvg[denoiserAvgIntra]
	intra.ExtendBorders()
	if cfg.RefreshLast {
		vp8common.CopyImage(&e.denoiser.runningAvg[denoiserAvgLast].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgLast].ExtendBorders()
	}
	if cfg.RefreshGolden || cfg.CopyBufferToGolden != 0 {
		vp8common.CopyImage(&e.denoiser.runningAvg[denoiserAvgGolden].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgGolden].ExtendBorders()
	}
	if cfg.RefreshAltRef || cfg.CopyBufferToAltRef != 0 {
		vp8common.CopyImage(&e.denoiser.runningAvg[denoiserAvgAltRef].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgAltRef].ExtendBorders()
	}
}

// denoiserNoiseMotionThreshold mirrors libvpx's NOISE_MOTION_THRESHOLD
// (denoising.c) and is used to scale the increase_denoising decision.
const denoiserNoiseMotionThreshold = 25 * 25
