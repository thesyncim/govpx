package encoder

import "github.com/thesyncim/govpx/internal/pixel"

// Arithmetic follows libvpx v1.16.0 vp8/encoder/denoising.c.
func denoiserFilterYFirstPassSIMD(mc []byte, ms int, avg []byte, as int, sig []byte, ss int, motion uint32, increase bool) (int, bool) {
	if !pixel.Enabled {
		return 0, false
	}
	inc, level := 0, 3
	if motion <= denoiserMotionMagnitudeThresh {
		level = 4
		if increase {
			inc = 1
			level = 5
		}
	}
	return pixel.DenoiseFirst(mc, ms, avg, as, sig, ss, 16, inc, level), true
}
func denoiserFilterUVSIMD(mc []byte, ms int, avg []byte, as int, sig []byte, ss int, motion uint32, increase bool) (int, bool) {
	if !pixel.Enabled {
		return 0, false
	}
	v := pixel.Sum(sig, ss, 8, 8) - 128*64
	if max(v, -v) < denoiserSumDiffFromAvgThreshUV {
		return DenoiserCopyBlock, true
	}
	inc, level := 0, 3
	if motion <= denoiserMotionMagnitudeThrUV {
		level = 4
		if increase {
			inc = 1
			level = 5
		}
	}
	sum := pixel.DenoiseFirst(mc, ms, avg, as, sig, ss, 8, inc, level)
	thresh := denoiserSumDiffThresholdUV
	if increase {
		thresh = denoiserSumDiffThresholdHighUV
	}
	if max(sum, -sum) > thresh {
		return DenoiserFilterUVScalar(mc, ms, avg, as, sig, ss, motion, increase), true
	}
	for y := range 8 {
		copy(sig[y*ss:y*ss+8], avg[y*as:y*as+8])
	}
	return DenoiserFilterBlock, true
}
