//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

// Complete luma decisions follow libvpx v1.16.0 vp8/encoder/denoising.c.

import "github.com/thesyncim/govpx/internal/pixel"

func denoiserFilterYCompleteSIMD(mc []byte, ms int, avg []byte, as int, sig []byte, ss int, motion uint32, increase bool) (int, bool) {
	return denoiserFilterYCompleteSIMDWork(mc, ms, avg, as, sig, ss, motion, increase, true)
}

func denoiserFilterYCompleteAverageSIMD(mc []byte, ms int, avg []byte, as int, sig []byte, ss int, motion uint32, increase bool) (int, bool) {
	return denoiserFilterYCompleteSIMDWork(mc, ms, avg, as, sig, ss, motion, increase, false)
}

func denoiserFilterYCompleteSIMDWork(mc []byte, ms int, avg []byte, as int, sig []byte, ss int, motion uint32, increase bool, updateSignal bool) (int, bool) {
	if !pixel.Enabled {
		return 0, false
	}
	inc, level, threshold := 0, 3, denoiserSumDiffThreshold
	if increase {
		threshold = denoiserSumDiffThresholdHigh
	}
	if motion <= denoiserMotionMagnitudeThresh {
		level = 4
		if increase {
			inc, level = 1, 5
		}
	}
	var filtered, ok bool
	if updateSignal {
		filtered, ok = pixel.DenoiseLuma16(mc, ms, avg, as, sig, ss, inc, level, threshold)
	} else {
		filtered, ok = pixel.DenoiseLuma16Average(mc, ms, avg, as, sig, ss, inc, level, threshold)
	}
	if filtered {
		return DenoiserFilterBlock, ok
	}
	return DenoiserCopyBlock, ok
}
