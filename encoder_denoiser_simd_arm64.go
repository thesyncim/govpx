//go:build arm64 && !purego

package govpx

import "unsafe"

// denoiserFilterYFirstPassNEON ports the common first pass of libvpx
// v1.16.0 vp8/encoder/arm/neon/denoising_neon.c
// vp8_denoiser_filter_neon. It writes the candidate running average and
// returns the saturated signed adjustment sum used by the accept/copy
// threshold.
//
//go:noescape
func denoiserFilterYFirstPassNEON(mc *byte, mcStride int, avg *byte, avgStride int, sig *byte, sigStride int, level1Adjustment uint64, level1Threshold uint64, sumOut *int32)

func denoiserFilterYFirstPassSIMD(mcRunningAvg []byte, mcStride int, runningAvg []byte, avgStride int, sig []byte, sigStride int, motionMagnitude uint32, increaseDenoising bool) (int, bool) {
	shiftInc := uint64(0)
	if increaseDenoising && motionMagnitude <= denoiserMotionMagnitudeThresh {
		shiftInc = 1
	}
	level1Adjustment := uint64(3)
	if motionMagnitude <= denoiserMotionMagnitudeThresh {
		level1Adjustment = 4 + shiftInc
	}
	level1Threshold := 4 + shiftInc
	var sum int32
	denoiserFilterYFirstPassNEON(
		unsafe.SliceData(mcRunningAvg),
		mcStride,
		unsafe.SliceData(runningAvg),
		avgStride,
		unsafe.SliceData(sig),
		sigStride,
		level1Adjustment,
		level1Threshold,
		&sum,
	)
	return int(sum), true
}
