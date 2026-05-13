package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// libvpx denoiser constants (vp8/encoder/denoising.h).
const (
	denoiserSumDiffThreshold       = 512
	denoiserSumDiffThresholdHigh   = 600
	denoiserMotionMagnitudeThresh  = 8 * 3
	denoiserSumDiffThresholdUV     = 96
	denoiserSumDiffThresholdHighUV = 8 * 8 * 2
	denoiserSumDiffFromAvgThreshUV = 8 * 8 * 8
	denoiserMotionMagnitudeThrUV   = 8 * 3
	denoiserSSEDiffThreshold       = 16 * 16 * 20
	denoiserSSEThreshold           = 16 * 16 * 40
	denoiserSSEThresholdHigh       = 16 * 16 * 80
	denoiserMaxGFARFRange          = 8
)

// libvpx vp8_denoiser_decision (denoising.h).
const (
	denoiserCopyBlock   = 0
	denoiserFilterBlock = 1
)

// libvpx vp8_denoiser_filter_state (denoising.h).
const (
	denoiserStateNoFilter      uint8 = 0
	denoiserStateFilterZeroMV  uint8 = 1
	denoiserStateFilterNonZero uint8 = 2
)

// libvpx vp8_denoiser_mode (denoising.h). govpx maps NoiseSensitivity 1-6 to
// these modes via denoiserModeForSensitivity.
const (
	denoiserOff = iota
	denoiserOnYOnly
	denoiserOnYUV
	denoiserOnYUVAggressive
)

// denoiseParams mirrors libvpx denoising.h denoise_params; chosen at frame
// start by denoiserSetParameters.
type denoiseParams struct {
	scaleSSEThresh      int
	scaleMotionThresh   int
	scaleIncreaseFilter int
	denoiseMVBias       int
	pickmodeMVBias      int
	qpThresh            int
	consecZeroLast      int
	spatialBlur         int
}

// denoiserSetParameters mirrors vp8_denoiser_set_parameters: maps the
// denoiser_mode to the per-frame parameter block. mode 1 selects Y-only,
// 2 selects YUV, 3 selects YUV-aggressive (matching libvpx exactly).
func denoiserSetParameters(mode int) (kind int, params denoiseParams) {
	switch mode {
	case 1:
		kind = denoiserOnYOnly
	case 2:
		kind = denoiserOnYUV
	case 3:
		kind = denoiserOnYUVAggressive
	default:
		kind = denoiserOnYUV
	}
	if kind != denoiserOnYUVAggressive {
		params = denoiseParams{
			scaleSSEThresh:      1,
			scaleMotionThresh:   8,
			scaleIncreaseFilter: 0,
			denoiseMVBias:       95,
			pickmodeMVBias:      100,
			qpThresh:            0,
			consecZeroLast:      1<<31 - 1,
			spatialBlur:         0,
		}
		return
	}
	params = denoiseParams{
		scaleSSEThresh:      2,
		scaleMotionThresh:   16,
		scaleIncreaseFilter: 1,
		denoiseMVBias:       60,
		pickmodeMVBias:      75,
		qpThresh:            80,
		consecZeroLast:      15,
		spatialBlur:         0,
	}
	return
}

// denoiserModeForSensitivity maps the public NoiseSensitivity (1-6) to the
// libvpx denoiser_mode passed to vp8_denoiser_set_parameters. Levels 1, 2,
// and 3 select Y-only, YUV, and YUV-aggressive; levels 4..6 enter libvpx's
// default YUV path (level 4 may adapt later via process_denoiser_mode_change).
// 0 keeps the denoiser disabled.
func denoiserModeForSensitivity(level int) int {
	if level <= 0 {
		return 0
	}
	if level == 1 {
		return 1
	}
	if level == 2 {
		return 2
	}
	if level == 3 {
		return 3
	}
	return 2
}

// denoiserFilterY ports vp8_denoiser_filter_c (denoising.c). Returns the
// libvpx FILTER_BLOCK / COPY_BLOCK decision and writes the denoised running
// average into runningAvg when filtering succeeds. Caller must ensure the
// slices cover the 16x16 macroblock at their respective strides.
func denoiserFilterY(mcRunningAvg []byte, mcStride int, runningAvg []byte, avgStride int, sig []byte, sigStride int, motionMagnitude uint32, increaseDenoising bool) int {
	adj := [3]int{3, 4, 6}
	shiftInc1 := 0
	shiftInc2 := 1
	if motionMagnitude <= denoiserMotionMagnitudeThresh {
		if increaseDenoising {
			shiftInc1 = 1
			shiftInc2 = 2
		}
		adj[0] += shiftInc2
		adj[1] += shiftInc2
		adj[2] += shiftInc2
	}

	var colSum [16]int
	for r := range 16 {
		// Sub-slice each row to len 16 (with full cap) so the inner
		// loop's mcRow[c]/sigRow[c]/avgRow[c] accesses are statically
		// bounded by len(slice) = 16 and the per-iter BCE elides.
		mcRow := mcRunningAvg[r*mcStride : r*mcStride+16 : r*mcStride+16]
		sigRow := sig[r*sigStride : r*sigStride+16 : r*sigStride+16]
		avgRow := runningAvg[r*avgStride : r*avgStride+16 : r*avgStride+16]
		for c := range 16 {
			diff := int(mcRow[c]) - int(sigRow[c])
			diffMask := diff >> mvKernelSignShift
			absdiff := (diff ^ diffMask) - diffMask
			if absdiff <= 3+shiftInc1 {
				avgRow[c] = mcRow[c]
				colSum[c] += diff
				continue
			}
			var adjustment int
			switch {
			case absdiff >= 4+shiftInc1 && absdiff <= 7:
				adjustment = adj[0]
			case absdiff >= 8 && absdiff <= 15:
				adjustment = adj[1]
			default:
				adjustment = adj[2]
			}
			if diff > 0 {
				val := min(int(sigRow[c])+adjustment, 255)
				avgRow[c] = byte(val)
				colSum[c] += adjustment
			} else {
				val := max(int(sigRow[c])-adjustment, 0)
				avgRow[c] = byte(val)
				colSum[c] -= adjustment
			}
		}
	}
	sumDiff := 0
	for c := range 16 {
		colSum[c] = min(colSum[c], 127)
		sumDiff += colSum[c]
	}
	thresh := denoiserSumDiffThreshold
	if increaseDenoising {
		thresh = denoiserSumDiffThresholdHigh
	}
	absMask := sumDiff >> mvKernelSignShift
	abs := (sumDiff ^ absMask) - absMask
	if abs > thresh {
		delta := ((abs - thresh) >> 8) + 1
		if delta >= 4 {
			return denoiserCopyBlock
		}
		// Apply weaker fallback temporal filter.
		for r := range 16 {
			// Bound rows to len 16 (full-cap) so the inner-loop indexed
			// accesses are statically bounded and the per-pixel BCE
			// elides, matching the primary 16x16 loop above.
			mcRow := mcRunningAvg[r*mcStride : r*mcStride+16 : r*mcStride+16]
			sigRow := sig[r*sigStride : r*sigStride+16 : r*sigStride+16]
			avgRow := runningAvg[r*avgStride : r*avgStride+16 : r*avgStride+16]
			for c := range 16 {
				diff := int(mcRow[c]) - int(sigRow[c])
				dMask := diff >> mvKernelSignShift
				adjustment := min((diff^dMask)-dMask, delta)
				// Branchless sign-aware delta: when dMask=0 (diff>=0)
				// signedDelta=-adjustment, when dMask=-1 signedDelta=+adjustment.
				// diff==0 collapses to adjustment=0 → signedDelta=0, leaving
				// avgRow and colSum unchanged just like the original
				// else-if-skip branch.
				signedDelta := (adjustment&dMask)<<1 - adjustment
				val := min(max(int(avgRow[c])+signedDelta, 0), 255)
				avgRow[c] = byte(val)
				colSum[c] += signedDelta
			}
		}
		sumDiff = 0
		for c := range 16 {
			colSum[c] = min(colSum[c], 127)
			sumDiff += colSum[c]
		}
		absMask = sumDiff >> mvKernelSignShift
		abs = (sumDiff ^ absMask) - absMask
		if abs > thresh {
			return denoiserCopyBlock
		}
	}
	// Filter accepted: copy running_avg back into the source frame.
	for r := range 16 {
		copy(sig[r*sigStride:r*sigStride+16], runningAvg[r*avgStride:r*avgStride+16])
	}
	return denoiserFilterBlock
}

// denoiserFilterUV ports vp8_denoiser_filter_uv_c (denoising.c). 8x8 block
// version for the chroma planes.
func denoiserFilterUV(mcRunningAvg []byte, mcStride int, runningAvg []byte, avgStride int, sig []byte, sigStride int, motionMagnitude uint32, increaseDenoising bool) int {
	adj := [3]int{3, 4, 6}
	shiftInc1 := 0
	shiftInc2 := 1
	if motionMagnitude <= denoiserMotionMagnitudeThrUV {
		if increaseDenoising {
			shiftInc1 = 1
			shiftInc2 = 2
		}
		adj[0] += shiftInc2
		adj[1] += shiftInc2
		adj[2] += shiftInc2
	}
	// Skip denoising of near-neutral colour blocks (libvpx avoids the filter
	// when chroma is close to 128).
	sumBlock := 0
	for r := range 8 {
		row := sig[r*sigStride:]
		for c := range 8 {
			sumBlock += int(row[c])
		}
	}
	{
		raw := sumBlock - 128*8*8
		rawMask := raw >> mvKernelSignShift
		if (raw^rawMask)-rawMask < denoiserSumDiffFromAvgThreshUV {
			return denoiserCopyBlock
		}
	}
	sumDiff := 0
	for r := range 8 {
		mcRow := mcRunningAvg[r*mcStride:]
		sigRow := sig[r*sigStride:]
		avgRow := runningAvg[r*avgStride:]
		for c := range 8 {
			diff := int(mcRow[c]) - int(sigRow[c])
			dMask := diff >> mvKernelSignShift
			absdiff := (diff ^ dMask) - dMask
			if absdiff <= 3+shiftInc1 {
				avgRow[c] = mcRow[c]
				sumDiff += diff
				continue
			}
			var adjustment int
			switch {
			case absdiff >= 4 && absdiff <= 7:
				adjustment = adj[0]
			case absdiff >= 8 && absdiff <= 15:
				adjustment = adj[1]
			default:
				adjustment = adj[2]
			}
			if diff > 0 {
				val := min(int(sigRow[c])+adjustment, 255)
				avgRow[c] = byte(val)
				sumDiff += adjustment
			} else {
				val := max(int(sigRow[c])-adjustment, 0)
				avgRow[c] = byte(val)
				sumDiff -= adjustment
			}
		}
	}
	thresh := denoiserSumDiffThresholdUV
	if increaseDenoising {
		thresh = denoiserSumDiffThresholdHighUV
	}
	absMask := sumDiff >> mvKernelSignShift
	abs := (sumDiff ^ absMask) - absMask
	if abs > thresh {
		delta := ((abs - thresh) >> 8) + 1
		if delta >= 4 {
			return denoiserCopyBlock
		}
		for r := range 8 {
			mcRow := mcRunningAvg[r*mcStride:]
			sigRow := sig[r*sigStride:]
			avgRow := runningAvg[r*avgStride:]
			for c := range 8 {
				diff := int(mcRow[c]) - int(sigRow[c])
				dMask := diff >> mvKernelSignShift
				adjustment := min((diff^dMask)-dMask, delta)
				// Same branchless sign-aware delta as the 16-wide luma loop:
				// signedDelta is -adjustment for diff>=0 and +adjustment for
				// diff<0, collapsing the diff==0 case via adjustment==0.
				signedDelta := (adjustment&dMask)<<1 - adjustment
				val := min(max(int(avgRow[c])+signedDelta, 0), 255)
				avgRow[c] = byte(val)
				sumDiff += signedDelta
			}
		}
		absMask = sumDiff >> mvKernelSignShift
		abs = (sumDiff ^ absMask) - absMask
		if abs > thresh {
			return denoiserCopyBlock
		}
	}
	for r := range 8 {
		copy(sig[r*sigStride:r*sigStride+8], runningAvg[r*avgStride:r*avgStride+8])
	}
	return denoiserFilterBlock
}

// denoiserPickmodeMVBias returns the libvpx pickmode_mv_bias multiplier for
// the configured noise sensitivity, or 100 (no bias) when the denoiser is
// off. Used by the fast-mode RD path to scale ZEROMV-LAST scores when the
// denoiser is in YUV-aggressive mode.
func (e *VP8Encoder) denoiserPickmodeMVBias() int {
	if e.opts.NoiseSensitivity <= 0 {
		return 100
	}
	_, params := denoiserSetParameters(denoiserModeForSensitivity(e.opts.NoiseSensitivity))
	return params.pickmodeMVBias
}

// denoiserState carries the libvpx-style running-average buffers, mode, and
// per-MB filter-state map that survives across frames.
type denoiserState struct {
	mode      int
	params    denoiseParams
	allocated bool
	width     int
	height    int

	// Running averages for each reference: index 0 is INTRA (the in-progress
	// frame's running average that becomes LAST), 1=LAST, 2=GOLDEN, 3=ALTREF.
	runningAvg [4]vp8common.FrameBuffer
	mcRunning  vp8common.FrameBuffer
	source     vp8common.FrameBuffer

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
	}
	if err := d.mcRunning.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := d.source.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	d.state = make([]uint8, rows*cols)
	d.allocated = true
	d.width = width
	d.height = height
	return nil
}

func (d *denoiserState) reset() {
	for i := range d.runningAvg {
		d.runningAvg[i].Reset()
	}
	d.mcRunning.Reset()
	d.source.Reset()
	for i := range d.state {
		d.state[i] = 0
	}
	d.allocated = false
}

type denoiserMacroblockDecision struct {
	bestReferenceFrame   vp8common.MVReferenceFrame
	bestMode             vp8common.MBPredictionMode
	bestMV               vp8enc.MotionVector
	bestSSE              uint32
	zeroMVReferenceFrame vp8common.MVReferenceFrame
	zeroMVSSE            uint32
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

func (e *VP8Encoder) denoiserReferenceTooOld(ref vp8common.MVReferenceFrame) bool {
	if ref == vp8common.LastFrame || ref <= vp8common.IntraFrame || ref >= vp8common.MaxRefFrames {
		return false
	}
	return e.frameCount > e.referenceFrameNumbers[ref] &&
		e.frameCount-e.referenceFrameNumbers[ref] > denoiserMaxGFARFRange
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
		copySourceToFrameBuffer(&e.denoiser.runningAvg[i], source)
	}
	for i := range e.denoiser.state {
		e.denoiser.state[i] = denoiserStateNoFilter
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
	zeroSSE := uint32(uint64(d.zeroMVSSE) * uint64(e.denoiser.params.denoiseMVBias) / 100)
	sseDiff := int64(zeroSSE) - int64(bestSSE)
	motionMag := uint32(int(mv.Row)*int(mv.Row) + int(mv.Col)*int(mv.Col))
	sseDiffThresh := 0
	if motionMag <= denoiserNoiseMotionThreshold {
		sseDiffThresh = denoiserSSEDiffThreshold
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
	increase := motionMag < uint32(e.denoiser.params.scaleIncreaseFilter)*denoiserNoiseMotionThreshold
	sseThresh := uint32(e.denoiser.params.scaleSSEThresh * denoiserSSEThreshold)
	if increase {
		sseThresh = uint32(e.denoiser.params.scaleSSEThresh * denoiserSSEThresholdHigh)
	}
	motionThresh := uint32(e.denoiser.params.scaleMotionThresh) * denoiserNoiseMotionThreshold
	if bestSSE > sseThresh || motionMag > motionThresh || e.denoiserSkinGateBlocksFilter(row, col, cols, index, motionMag) {
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
	convertInterFrameMode(&mcMode, &decMode)
	var zeroTokens vp8dec.MacroblockTokens
	if !reconstructInterAnalysisMacroblock(&e.denoiser.mcRunning.Img, &e.denoiser.runningAvg[avgIndex].Img, row, col, &decMode, &zeroTokens, &e.dequants[0], &e.reconstructScratch) {
		e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
		return
	}

	avg := &e.denoiser.runningAvg[denoiserAvgIntra]
	yOff := row*16*source.YStride + col*16
	uOff := row*8*source.UStride + col*8
	vOff := row*8*source.VStride + col*8
	ySigOff := row*16*filtered.YStride + col*16
	uSigOff := row*8*filtered.UStride + col*8
	vSigOff := row*8*filtered.VStride + col*8
	yMcOff := row*16*e.denoiser.mcRunning.Img.YStride + col*16
	uMcOff := row*8*e.denoiser.mcRunning.Img.UStride + col*8
	vMcOff := row*8*e.denoiser.mcRunning.Img.VStride + col*8
	yAvgOff := row*16*avg.Img.YStride + col*16
	uAvgOff := row*8*avg.Img.UStride + col*8
	vAvgOff := row*8*avg.Img.VStride + col*8

	filterDecision := denoiserFilterY(
		e.denoiser.mcRunning.Img.Y[yMcOff:], e.denoiser.mcRunning.Img.YStride,
		avg.Img.Y[yAvgOff:], avg.Img.YStride,
		filtered.Y[ySigOff:], filtered.YStride,
		motionMag, increase,
	)
	if filterDecision == denoiserFilterBlock {
		if motionMag > 0 {
			e.denoiser.state[index] = denoiserStateFilterNonZero
		} else {
			e.denoiser.state[index] = denoiserStateFilterZeroMV
		}
	} else {
		e.denoiser.state[index] = denoiserStateNoFilter
		copyMacroblockY(avg.Img.Y[yAvgOff:], avg.Img.YStride, source.Y[yOff:], source.YStride)
		copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, source.Y[yOff:], source.YStride)
	}

	applySpatialFilter := func() {
		if e.applyDenoiserSpatialLoopFilter(filtered, avg, row, col, cols, index, ySigOff, yAvgOff) {
			copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, avg.Img.Y[yAvgOff:], avg.Img.YStride)
		}
	}
	if e.denoiser.mode == denoiserOnYOnly {
		applySpatialFilter()
		return
	}
	if motionMag == 0 && filterDecision == denoiserFilterBlock {
		if denoiserFilterUV(
			e.denoiser.mcRunning.Img.U[uMcOff:], e.denoiser.mcRunning.Img.UStride,
			avg.Img.U[uAvgOff:], avg.Img.UStride,
			filtered.U[uSigOff:], filtered.UStride,
			motionMag, false,
		) == denoiserCopyBlock {
			copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
			copyMacroblock8x8(filtered.U[uSigOff:], filtered.UStride, source.U[uOff:], source.UStride)
		}
		if denoiserFilterUV(
			e.denoiser.mcRunning.Img.V[vMcOff:], e.denoiser.mcRunning.Img.VStride,
			avg.Img.V[vAvgOff:], avg.Img.VStride,
			filtered.V[vSigOff:], filtered.VStride,
			motionMag, false,
		) == denoiserCopyBlock {
			copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
			copyMacroblock8x8(filtered.V[vSigOff:], filtered.VStride, source.V[vOff:], source.VStride)
		}
		applySpatialFilter()
		return
	}
	copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
	copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
	copyMacroblock8x8(filtered.U[uSigOff:], filtered.UStride, source.U[uOff:], source.UStride)
	copyMacroblock8x8(filtered.V[vSigOff:], filtered.VStride, source.V[vOff:], source.VStride)
	applySpatialFilter()
}

func (e *VP8Encoder) applyDenoiserSpatialLoopFilter(filtered vp8enc.SourceImage, avg *vp8common.FrameBuffer, row int, col int, cols int, index int, ySigOff int, yAvgOff int) bool {
	const filterLevel = 48
	currentState := e.denoiser.state[index]
	applyFilterCol := false
	applyFilterRow := false
	if col > 0 {
		leftState := e.denoiser.state[index-1]
		applyFilterCol = !(currentState == leftState && currentState != denoiserStateFilterNonZero)
	}
	if row > 0 {
		aboveState := e.denoiser.state[index-cols]
		applyFilterRow = !(currentState == aboveState && currentState != denoiserStateFilterNonZero)
	}
	if !applyFilterCol && !applyFilterRow {
		return false
	}

	var lfi vp8common.LoopFilterInfo
	vp8common.InitLoopFilterInfo(&lfi, int(e.opts.Sharpness))
	hev := lfi.HEVThresh[lfi.HEVThreshLUT[vp8common.InterFrame][filterLevel]]
	mblim := lfi.MBLimit[filterLevel]
	lim := lfi.Limit[filterLevel]
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
	copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, source.Y[yOff:], source.YStride)
	if e.denoiser.mode != denoiserOnYOnly {
		copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
		copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
		copyMacroblock8x8(filtered.U[uSigOff:], filtered.UStride, source.U[uOff:], source.UStride)
		copyMacroblock8x8(filtered.V[vSigOff:], filtered.VStride, source.V[vOff:], source.VStride)
	}
	e.denoiser.state[index] = denoiserStateNoFilter
	if e.applyDenoiserSpatialLoopFilter(filtered, avg, row, col, cols, index, ySigOff, yAvgOff) {
		copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, avg.Img.Y[yAvgOff:], avg.Img.YStride)
	}
}

// copyDenoiserMacroblockSource copies the source macroblock pixels into the
// running_avg[INTRA] buffer for blocks that the denoiser declines to filter
// (intra and COPY decisions).
func (e *VP8Encoder) copyDenoiserMacroblockSource(source vp8enc.SourceImage, avg *vp8common.FrameBuffer, yOff int, uOff int, vOff int, yAvgOff int, uAvgOff int, vAvgOff int, doYUV bool) {
	copyMacroblockY(avg.Img.Y[yAvgOff:], avg.Img.YStride, source.Y[yOff:], source.YStride)
	if doYUV {
		copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
		copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
	}
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
		copyFrameImage(&e.denoiser.runningAvg[denoiserAvgLast].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgLast].ExtendBorders()
	}
	if cfg.RefreshGolden || cfg.CopyBufferToGolden != 0 {
		copyFrameImage(&e.denoiser.runningAvg[denoiserAvgGolden].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgGolden].ExtendBorders()
	}
	if cfg.RefreshAltRef || cfg.CopyBufferToAltRef != 0 {
		copyFrameImage(&e.denoiser.runningAvg[denoiserAvgAltRef].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgAltRef].ExtendBorders()
	}
}

// denoiserNoiseMotionThreshold mirrors libvpx's NOISE_MOTION_THRESHOLD
// (denoising.c) and is used to scale the increase_denoising decision.
const denoiserNoiseMotionThreshold = 25 * 25
