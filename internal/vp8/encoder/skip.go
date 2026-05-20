package encoder

import common "github.com/thesyncim/govpx/internal/vp8/common"

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c
// vp8cx_base_skip_false_prob. Entries are indexed by internal VP8 qindex.
var DefaultBaseSkipFalseProbs = [common.QIndexRange]uint8{
	255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255,
	251, 248, 244, 240, 236, 232, 229, 225,
	221,
	217, 213, 208, 204, 199, 194, 190, 187,
	183, 179, 175, 172, 168, 164, 160, 157,
	153, 149, 145, 142, 138, 134, 130, 127,
	124, 120, 117, 114, 110, 107, 104, 101,
	98, 95, 92, 89, 86, 83, 80, 77,
	74, 71, 68, 65, 62, 59, 56, 53,
	50, 47, 44, 41, 38, 35, 32, 30,
	28, 26, 24, 22, 20, 18, 16,
}

func BaseSkipFalseProb(qIndex int) uint8 {
	return DefaultBaseSkipFalseProbs[min(max(qIndex, 0), len(DefaultBaseSkipFalseProbs)-1)]
}

func SkipFalseReferenceIndex(refreshGolden bool, refreshAltRef bool) int {
	if refreshAltRef {
		return 2
	}
	if refreshGolden {
		return 1
	}
	return 0
}

func ClampInterAnalysisSkipFalseProb(prob uint8) uint8 {
	return min(max(prob, 5), 250)
}

func InterFrameModeSkipFalseProbability(rows int, cols int, modes []InterFrameMacroblockMode, fallback uint8) uint8 {
	if rows <= 0 || cols <= 0 {
		if fallback == 0 {
			return 128
		}
		return fallback
	}
	required := rows * cols
	if required <= 0 || len(modes) < required {
		if fallback == 0 {
			return 128
		}
		return fallback
	}
	var counts [2]int
	for i := range required {
		if modes[i].MBSkipCoeff {
			counts[1]++
		} else {
			counts[0]++
		}
	}
	return SkipFalseProbabilityFromCounts(counts, fallback)
}

func SkipFalseProbabilityFromCounts(counts [2]int, fallback uint8) uint8 {
	total := counts[0] + counts[1]
	if total <= 0 {
		if fallback == 0 {
			return 128
		}
		return fallback
	}
	return uint8(min(max(counts[0]*256/total, 1), 255))
}
