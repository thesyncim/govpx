//go:build amd64 && !purego

package encoder

import "unsafe"

// libvpx v1.16.0 baseline: vp8/encoder/x86/denoising_sse2.c.

const denoiserSSE2ByteRepeat = 0x0101010101010101

// denoiserFilterYFirstPassSSE2 ports the common first pass of libvpx
// v1.16.0 vp8/encoder/x86/denoising_sse2.c
// vp8_denoiser_filter_sse2. It writes the candidate running average and
// returns the saturated signed adjustment sum used by the accept/copy
// threshold.
//
//go:noescape
func denoiserFilterYFirstPassSSE2(mc *byte, mcStride int, avg *byte, avgStride int, sig *byte, sigStride int, level1Adjustment uint64, level1Threshold uint64, sumOut *int32)

//go:noescape
func denoiserFilterUVFirstPassSSE2(mc *byte, mcStride int, avg *byte, avgStride int, sig *byte, sigStride int, level1Adjustment uint64, level1Threshold uint64, sumOut *int32)

func repeatedDenoiserByte(v uint64) uint64 {
	return v * denoiserSSE2ByteRepeat
}

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
	denoiserFilterYFirstPassSSE2(
		unsafe.SliceData(mcRunningAvg),
		mcStride,
		unsafe.SliceData(runningAvg),
		avgStride,
		unsafe.SliceData(sig),
		sigStride,
		repeatedDenoiserByte(level1Adjustment),
		repeatedDenoiserByte(level1Threshold),
		&sum,
	)
	return int(sum), true
}

func denoiserFilterUVSIMD(mcRunningAvg []byte, mcStride int, runningAvg []byte, avgStride int, sig []byte, sigStride int, motionMagnitude uint32, increaseDenoising bool) (int, bool) {
	sumBlock := 0
	for r := range 8 {
		row := sig[r*sigStride:]
		for c := range 8 {
			sumBlock += int(row[c])
		}
	}
	raw := sumBlock - 128*8*8
	rawMask := raw >> intSignShift
	if (raw^rawMask)-rawMask < denoiserSumDiffFromAvgThreshUV {
		return DenoiserCopyBlock, true
	}

	shiftInc := uint64(0)
	if increaseDenoising && motionMagnitude <= denoiserMotionMagnitudeThrUV {
		shiftInc = 1
	}
	level1Adjustment := uint64(3)
	if motionMagnitude <= denoiserMotionMagnitudeThrUV {
		level1Adjustment = 4 + shiftInc
	}
	level1Threshold := 4 + shiftInc
	var sum int32
	denoiserFilterUVFirstPassSSE2(
		unsafe.SliceData(mcRunningAvg),
		mcStride,
		unsafe.SliceData(runningAvg),
		avgStride,
		unsafe.SliceData(sig),
		sigStride,
		repeatedDenoiserByte(level1Adjustment),
		repeatedDenoiserByte(level1Threshold),
		&sum,
	)
	sumDiff := int(sum)
	thresh := denoiserSumDiffThresholdUV
	if increaseDenoising {
		thresh = denoiserSumDiffThresholdHighUV
	}
	absMask := sumDiff >> intSignShift
	if (sumDiff^absMask)-absMask > thresh {
		return 0, false
	}
	for r := range 8 {
		copy(sig[r*sigStride:r*sigStride+8], runningAvg[r*avgStride:r*avgStride+8])
	}
	return DenoiserFilterBlock, true
}
