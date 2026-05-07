package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
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

// denoiserModeForSensitivity maps the public NoiseSensitivity (1-6) to a
// libvpx denoiser_mode. Levels 1, 2, and 3+ select Y-only, YUV, and
// YUV-aggressive respectively (libvpx onyx_if.c set_internal_size). 0 keeps
// the denoiser disabled.
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
	return 3
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
	for r := 0; r < 16; r++ {
		mcRow := mcRunningAvg[r*mcStride:]
		sigRow := sig[r*sigStride:]
		avgRow := runningAvg[r*avgStride:]
		for c := 0; c < 16; c++ {
			diff := int(mcRow[c]) - int(sigRow[c])
			absdiff := diff
			if absdiff < 0 {
				absdiff = -absdiff
			}
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
				val := int(sigRow[c]) + adjustment
				if val > 255 {
					val = 255
				}
				avgRow[c] = byte(val)
				colSum[c] += adjustment
			} else {
				val := int(sigRow[c]) - adjustment
				if val < 0 {
					val = 0
				}
				avgRow[c] = byte(val)
				colSum[c] -= adjustment
			}
		}
	}
	sumDiff := 0
	for c := 0; c < 16; c++ {
		if colSum[c] >= 128 {
			colSum[c] = 127
		}
		sumDiff += colSum[c]
	}
	thresh := denoiserSumDiffThreshold
	if increaseDenoising {
		thresh = denoiserSumDiffThresholdHigh
	}
	abs := sumDiff
	if abs < 0 {
		abs = -abs
	}
	if abs > thresh {
		delta := ((abs - thresh) >> 8) + 1
		if delta >= 4 {
			return denoiserCopyBlock
		}
		// Apply weaker fallback temporal filter.
		for r := 0; r < 16; r++ {
			mcRow := mcRunningAvg[r*mcStride:]
			sigRow := sig[r*sigStride:]
			avgRow := runningAvg[r*avgStride:]
			for c := 0; c < 16; c++ {
				diff := int(mcRow[c]) - int(sigRow[c])
				adjustment := diff
				if adjustment < 0 {
					adjustment = -adjustment
				}
				if adjustment > delta {
					adjustment = delta
				}
				if diff > 0 {
					val := int(avgRow[c]) - adjustment
					if val < 0 {
						val = 0
					}
					avgRow[c] = byte(val)
					colSum[c] -= adjustment
				} else if diff < 0 {
					val := int(avgRow[c]) + adjustment
					if val > 255 {
						val = 255
					}
					avgRow[c] = byte(val)
					colSum[c] += adjustment
				}
			}
		}
		sumDiff = 0
		for c := 0; c < 16; c++ {
			if colSum[c] >= 128 {
				colSum[c] = 127
			}
			sumDiff += colSum[c]
		}
		abs = sumDiff
		if abs < 0 {
			abs = -abs
		}
		if abs > thresh {
			return denoiserCopyBlock
		}
	}
	// Filter accepted: copy running_avg back into the source frame.
	for r := 0; r < 16; r++ {
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
	for r := 0; r < 8; r++ {
		row := sig[r*sigStride:]
		for c := 0; c < 8; c++ {
			sumBlock += int(row[c])
		}
	}
	if abs := sumBlock - 128*8*8; (abs < 0 && -abs < denoiserSumDiffFromAvgThreshUV) || (abs >= 0 && abs < denoiserSumDiffFromAvgThreshUV) {
		return denoiserCopyBlock
	}
	sumDiff := 0
	for r := 0; r < 8; r++ {
		mcRow := mcRunningAvg[r*mcStride:]
		sigRow := sig[r*sigStride:]
		avgRow := runningAvg[r*avgStride:]
		for c := 0; c < 8; c++ {
			diff := int(mcRow[c]) - int(sigRow[c])
			absdiff := diff
			if absdiff < 0 {
				absdiff = -absdiff
			}
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
				val := int(sigRow[c]) + adjustment
				if val > 255 {
					val = 255
				}
				avgRow[c] = byte(val)
				sumDiff += adjustment
			} else {
				val := int(sigRow[c]) - adjustment
				if val < 0 {
					val = 0
				}
				avgRow[c] = byte(val)
				sumDiff -= adjustment
			}
		}
	}
	thresh := denoiserSumDiffThresholdUV
	if increaseDenoising {
		thresh = denoiserSumDiffThresholdHighUV
	}
	abs := sumDiff
	if abs < 0 {
		abs = -abs
	}
	if abs > thresh {
		delta := ((abs - thresh) >> 8) + 1
		if delta >= 4 {
			return denoiserCopyBlock
		}
		for r := 0; r < 8; r++ {
			mcRow := mcRunningAvg[r*mcStride:]
			sigRow := sig[r*sigStride:]
			avgRow := runningAvg[r*avgStride:]
			for c := 0; c < 8; c++ {
				diff := int(mcRow[c]) - int(sigRow[c])
				adjustment := diff
				if adjustment < 0 {
					adjustment = -adjustment
				}
				if adjustment > delta {
					adjustment = delta
				}
				if diff > 0 {
					val := int(avgRow[c]) - adjustment
					if val < 0 {
						val = 0
					}
					avgRow[c] = byte(val)
					sumDiff -= adjustment
				} else if diff < 0 {
					val := int(avgRow[c]) + adjustment
					if val > 255 {
						val = 255
					}
					avgRow[c] = byte(val)
					sumDiff += adjustment
				}
			}
		}
		abs = sumDiff
		if abs < 0 {
			abs = -abs
		}
		if abs > thresh {
			return denoiserCopyBlock
		}
	}
	for r := 0; r < 8; r++ {
		copy(sig[r*sigStride:r*sigStride+8], runningAvg[r*avgStride:r*avgStride+8])
	}
	return denoiserFilterBlock
}

// denoiserPickmodeMVBias returns the libvpx pickmode_mv_bias multiplier for
// the configured noise sensitivity, or 100 (no bias) when the denoiser is
// off. Used by the fast-mode RD path to scale ZEROMV-LAST scores when the
// denoiser is in YUV-aggressive mode.
func (e *VP8Encoder) denoiserPickmodeMVBias() int {
	if e == nil || e.opts.NoiseSensitivity <= 0 {
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
	for i := range d.state {
		d.state[i] = 0
	}
	d.allocated = false
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
	if e == nil || e.opts.NoiseSensitivity <= 0 || !e.denoiser.allocated {
		return
	}
	for i := range e.denoiser.runningAvg {
		copySourceToFrameBuffer(&e.denoiser.runningAvg[i], source)
	}
	for i := range e.denoiser.state {
		e.denoiser.state[i] = denoiserStateNoFilter
	}
}

// applyDenoiserToInterFrame runs the libvpx per-MB denoiser after inter
// reconstruction. For each macroblock we use the encoded reconstruction
// (e.analysis.Img) as the motion-compensated running average, run
// denoiserFilterY (and UV when the mode includes chroma), and write the
// filtered or copied pixels into runningAvg[INTRA]. The per-MB FILTER /
// COPY / kNoFilter state is recorded for next-frame bias decisions.
func (e *VP8Encoder) applyDenoiserToInterFrame(source vp8enc.SourceImage, rows int, cols int) {
	if e == nil || e.opts.NoiseSensitivity <= 0 || !e.denoiser.allocated {
		return
	}
	if rows <= 0 || cols <= 0 {
		return
	}
	avg := &e.denoiser.runningAvg[denoiserAvgIntra]
	mcSrc := &e.analysis.Img
	uvWidth := (e.opts.Width + 1) >> 1
	uvHeight := (e.opts.Height + 1) >> 1
	doYUV := e.denoiser.mode != denoiserOnYOnly
	required := rows * cols
	if len(e.interFrameModes) < required || len(e.denoiser.state) < required {
		return
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			mode := e.interFrameModes[index]
			yOff := row*16*source.YStride + col*16
			uOff := row*8*source.UStride + col*8
			vOff := row*8*source.VStride + col*8
			yMcOff := row*16*mcSrc.YStride + col*16
			uMcOff := row*8*mcSrc.UStride + col*8
			vMcOff := row*8*mcSrc.VStride + col*8
			yAvgOff := row*16*avg.Img.YStride + col*16
			uAvgOff := row*8*avg.Img.UStride + col*8
			vAvgOff := row*8*avg.Img.VStride + col*8

			// Intra blocks have no motion-compensated reference; libvpx
			// treats them as always COPY (no denoise).
			if mode.RefFrame == 0 /* IntraFrame */ {
				e.copyDenoiserMacroblockSource(source, avg, row, col, yOff, uOff, vOff, yAvgOff, uAvgOff, vAvgOff, uvWidth, uvHeight, doYUV)
				e.denoiser.state[index] = denoiserStateNoFilter
				continue
			}

			motionMag := uint32(int(mode.MV.Row)*int(mode.MV.Row) + int(mode.MV.Col)*int(mode.MV.Col))
			increase := motionMag < uint32(e.denoiser.params.scaleIncreaseFilter)*denoiserNoiseMotionThreshold
			decision := denoiserFilterY(
				mcSrc.Y[yMcOff:], mcSrc.YStride,
				avg.Img.Y[yAvgOff:], avg.Img.YStride,
				source.Y[yOff:], source.YStride,
				motionMag, increase,
			)
			if decision == denoiserFilterBlock {
				if motionMag > 0 {
					e.denoiser.state[index] = denoiserStateFilterNonZero
				} else {
					e.denoiser.state[index] = denoiserStateFilterZeroMV
				}
			} else {
				e.denoiser.state[index] = denoiserStateNoFilter
				copyMacroblockY(avg.Img.Y[yAvgOff:], avg.Img.YStride, source.Y[yOff:], source.YStride)
			}
			if doYUV && motionMag == 0 && decision == denoiserFilterBlock {
				_ = denoiserFilterUV(
					mcSrc.U[uMcOff:], mcSrc.UStride,
					avg.Img.U[uAvgOff:], avg.Img.UStride,
					source.U[uOff:], source.UStride,
					motionMag, increase,
				)
				_ = denoiserFilterUV(
					mcSrc.V[vMcOff:], mcSrc.VStride,
					avg.Img.V[vAvgOff:], avg.Img.VStride,
					source.V[vOff:], source.VStride,
					motionMag, increase,
				)
			} else if doYUV {
				copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
				copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
			}
		}
	}
	avg.ExtendBorders()
}

// copyDenoiserMacroblockSource copies the source macroblock pixels into the
// running_avg[INTRA] buffer for blocks that the denoiser declines to filter
// (intra and COPY decisions).
func (e *VP8Encoder) copyDenoiserMacroblockSource(source vp8enc.SourceImage, avg *vp8common.FrameBuffer, row int, col int, yOff int, uOff int, vOff int, yAvgOff int, uAvgOff int, vAvgOff int, uvWidth int, uvHeight int, doYUV bool) {
	copyMacroblockY(avg.Img.Y[yAvgOff:], avg.Img.YStride, source.Y[yOff:], source.YStride)
	if doYUV {
		copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
		copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
	}
}

func copyMacroblockY(dst []byte, dstStride int, src []byte, srcStride int) {
	for r := 0; r < 16; r++ {
		copy(dst[r*dstStride:r*dstStride+16], src[r*srcStride:r*srcStride+16])
	}
}

func copyMacroblock8x8(dst []byte, dstStride int, src []byte, srcStride int) {
	for r := 0; r < 8; r++ {
		copy(dst[r*dstStride:r*dstStride+8], src[r*srcStride:r*srcStride+8])
	}
}

// copyDenoiserAvgForRefresh mirrors update_reference_frames' denoiser branch:
// after the encoded frame is committed, copy running_avg[INTRA] into the
// per-reference running_avg buffers that this frame's refresh policy updates,
// keeping the denoiser's parallel reference stream in sync with the encoder's
// references.
func (e *VP8Encoder) copyDenoiserAvgForRefresh(refreshLast bool, refreshGolden bool, refreshAltRef bool) {
	if e == nil || e.opts.NoiseSensitivity <= 0 || !e.denoiser.allocated {
		return
	}
	intra := &e.denoiser.runningAvg[denoiserAvgIntra]
	if refreshLast {
		copyFrameImage(&e.denoiser.runningAvg[denoiserAvgLast].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgLast].ExtendBorders()
	}
	if refreshGolden {
		copyFrameImage(&e.denoiser.runningAvg[denoiserAvgGolden].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgGolden].ExtendBorders()
	}
	if refreshAltRef {
		copyFrameImage(&e.denoiser.runningAvg[denoiserAvgAltRef].Img, &intra.Img)
		e.denoiser.runningAvg[denoiserAvgAltRef].ExtendBorders()
	}
}

// denoiserNoiseMotionThreshold mirrors libvpx's NOISE_MOTION_THRESHOLD
// (denoising.c) and is used to scale the increase_denoising decision.
const denoiserNoiseMotionThreshold = 25 * 25
