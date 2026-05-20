package encoder

// Ported from libvpx v1.16.0:
// - vp8/encoder/denoising.c
// - vp8/encoder/denoising.h

// libvpx denoiser constants (vp8/encoder/denoising.h).
const (
	denoiserSumDiffThreshold       = 512
	denoiserSumDiffThresholdHigh   = 600
	denoiserMotionMagnitudeThresh  = 8 * 3
	denoiserSumDiffThresholdUV     = 96
	denoiserSumDiffThresholdHighUV = 8 * 8 * 2
	denoiserSumDiffFromAvgThreshUV = 8 * 8 * 8
	denoiserMotionMagnitudeThrUV   = 8 * 3

	DenoiserSSEDiffThreshold = 16 * 16 * 20
	DenoiserSSEThreshold     = 16 * 16 * 40
	DenoiserSSEThresholdHigh = 16 * 16 * 80
	DenoiserMaxGFARFRange    = 8
)

// libvpx vp8_denoiser_decision (denoising.h).
const (
	DenoiserCopyBlock   = 0
	DenoiserFilterBlock = 1
)

// libvpx vp8_denoiser_filter_state (denoising.h).
const (
	DenoiserStateNoFilter      uint8 = 0
	DenoiserStateFilterZeroMV  uint8 = 1
	DenoiserStateFilterNonZero uint8 = 2
)

// libvpx vp8_denoiser_mode (denoising.h).
const (
	DenoiserOff = iota
	DenoiserOnYOnly
	DenoiserOnYUV
	DenoiserOnYUVAggressive
)

// DenoiseParams mirrors libvpx denoising.h denoise_params.
type DenoiseParams struct {
	ScaleSSEThresh      int
	ScaleMotionThresh   int
	ScaleIncreaseFilter int
	DenoiseMVBias       int
	PickmodeMVBias      int
	QPThresh            int
	ConsecZeroLast      int
	SpatialBlur         int
}

// DenoiserSetParameters mirrors vp8_denoiser_set_parameters: maps the
// denoiser_mode to the per-frame parameter block. mode 1 selects Y-only,
// 2 selects YUV, 3 selects YUV-aggressive (matching libvpx exactly).
func DenoiserSetParameters(mode int) (kind int, params DenoiseParams) {
	switch mode {
	case 1:
		kind = DenoiserOnYOnly
	case 2:
		kind = DenoiserOnYUV
	case 3:
		kind = DenoiserOnYUVAggressive
	default:
		kind = DenoiserOnYUV
	}
	if kind != DenoiserOnYUVAggressive {
		params = DenoiseParams{
			ScaleSSEThresh:      1,
			ScaleMotionThresh:   8,
			ScaleIncreaseFilter: 0,
			DenoiseMVBias:       95,
			PickmodeMVBias:      100,
			QPThresh:            0,
			ConsecZeroLast:      1<<31 - 1,
			SpatialBlur:         0,
		}
		return
	}
	params = DenoiseParams{
		ScaleSSEThresh:      2,
		ScaleMotionThresh:   16,
		ScaleIncreaseFilter: 1,
		DenoiseMVBias:       60,
		PickmodeMVBias:      75,
		QPThresh:            80,
		ConsecZeroLast:      15,
		SpatialBlur:         0,
	}
	return
}

// DenoiserModeForSensitivity maps the public NoiseSensitivity (1-6) to the
// libvpx denoiser_mode passed to vp8_denoiser_set_parameters. Levels 1, 2,
// and 3 select Y-only, YUV, and YUV-aggressive; levels 4..6 enter libvpx's
// default YUV path (level 4 may adapt later via process_denoiser_mode_change).
// 0 keeps the denoiser disabled.
func DenoiserModeForSensitivity(level int) int {
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

// DenoiserFilterY ports vp8_denoiser_filter_c (denoising.c). It returns the
// libvpx FILTER_BLOCK / COPY_BLOCK decision and writes the denoised running
// average into runningAvg when filtering succeeds. Caller must ensure the
// slices cover the 16x16 macroblock at their respective strides.
func DenoiserFilterY(mcRunningAvg []byte, mcStride int, runningAvg []byte, avgStride int, sig []byte, sigStride int, motionMagnitude uint32, increaseDenoising bool) int {
	if sumDiff, ok := denoiserFilterYFirstPassSIMD(mcRunningAvg, mcStride, runningAvg, avgStride, sig, sigStride, motionMagnitude, increaseDenoising); ok {
		thresh := denoiserSumDiffThreshold
		if increaseDenoising {
			thresh = denoiserSumDiffThresholdHigh
		}
		absMask := sumDiff >> intSignShift
		if (sumDiff^absMask)-absMask <= thresh {
			for r := range 16 {
				copy(sig[r*sigStride:r*sigStride+16], runningAvg[r*avgStride:r*avgStride+16])
			}
			return DenoiserFilterBlock
		}
	}
	return DenoiserFilterYScalar(mcRunningAvg, mcStride, runningAvg, avgStride, sig, sigStride, motionMagnitude, increaseDenoising)
}

func DenoiserFilterYScalar(mcRunningAvg []byte, mcStride int, runningAvg []byte, avgStride int, sig []byte, sigStride int, motionMagnitude uint32, increaseDenoising bool) int {
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
			diffMask := diff >> intSignShift
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
	absMask := sumDiff >> intSignShift
	abs := (sumDiff ^ absMask) - absMask
	if abs > thresh {
		delta := ((abs - thresh) >> 8) + 1
		if delta >= 4 {
			return DenoiserCopyBlock
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
				dMask := diff >> intSignShift
				adjustment := min((diff^dMask)-dMask, delta)
				// Branchless sign-aware delta: when dMask=0 (diff>=0)
				// signedDelta=-adjustment, when dMask=-1 signedDelta=+adjustment.
				// diff==0 collapses to adjustment=0, leaving avgRow and
				// colSum unchanged just like the original else-if-skip branch.
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
		absMask = sumDiff >> intSignShift
		abs = (sumDiff ^ absMask) - absMask
		if abs > thresh {
			return DenoiserCopyBlock
		}
	}
	for r := range 16 {
		copy(sig[r*sigStride:r*sigStride+16], runningAvg[r*avgStride:r*avgStride+16])
	}
	return DenoiserFilterBlock
}

// DenoiserFilterUV ports vp8_denoiser_filter_uv_c (denoising.c). It operates
// on one 8x8 chroma block.
func DenoiserFilterUV(mcRunningAvg []byte, mcStride int, runningAvg []byte, avgStride int, sig []byte, sigStride int, motionMagnitude uint32, increaseDenoising bool) int {
	if decision, ok := denoiserFilterUVSIMD(mcRunningAvg, mcStride, runningAvg, avgStride, sig, sigStride, motionMagnitude, increaseDenoising); ok {
		return decision
	}
	return DenoiserFilterUVScalar(mcRunningAvg, mcStride, runningAvg, avgStride, sig, sigStride, motionMagnitude, increaseDenoising)
}

func DenoiserFilterUVScalar(mcRunningAvg []byte, mcStride int, runningAvg []byte, avgStride int, sig []byte, sigStride int, motionMagnitude uint32, increaseDenoising bool) int {
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

	sumBlock := 0
	for r := range 8 {
		row := sig[r*sigStride:]
		for c := range 8 {
			sumBlock += int(row[c])
		}
	}
	{
		raw := sumBlock - 128*8*8
		rawMask := raw >> intSignShift
		if (raw^rawMask)-rawMask < denoiserSumDiffFromAvgThreshUV {
			return DenoiserCopyBlock
		}
	}

	sumDiff := 0
	for r := range 8 {
		mcRow := mcRunningAvg[r*mcStride:]
		sigRow := sig[r*sigStride:]
		avgRow := runningAvg[r*avgStride:]
		for c := range 8 {
			diff := int(mcRow[c]) - int(sigRow[c])
			dMask := diff >> intSignShift
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
	absMask := sumDiff >> intSignShift
	abs := (sumDiff ^ absMask) - absMask
	if abs > thresh {
		delta := ((abs - thresh) >> 8) + 1
		if delta >= 4 {
			return DenoiserCopyBlock
		}
		for r := range 8 {
			mcRow := mcRunningAvg[r*mcStride:]
			sigRow := sig[r*sigStride:]
			avgRow := runningAvg[r*avgStride:]
			for c := range 8 {
				diff := int(mcRow[c]) - int(sigRow[c])
				dMask := diff >> intSignShift
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
		absMask = sumDiff >> intSignShift
		abs = (sumDiff ^ absMask) - absMask
		if abs > thresh {
			return DenoiserCopyBlock
		}
	}
	for r := range 8 {
		copy(sig[r*sigStride:r*sigStride+8], runningAvg[r*avgStride:r*avgStride+8])
	}
	return DenoiserFilterBlock
}
