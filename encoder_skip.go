package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c
// vp8cx_base_skip_false_prob. Entries are indexed by internal VP8 qindex.
var libvpxBaseSkipFalseProbs = [vp8common.QIndexRange]uint8{
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

func libvpxBaseSkipFalseProb(qIndex int) uint8 {
	if qIndex < 0 {
		qIndex = 0
	} else if qIndex >= len(libvpxBaseSkipFalseProbs) {
		qIndex = len(libvpxBaseSkipFalseProbs) - 1
	}
	return libvpxBaseSkipFalseProbs[qIndex]
}

func (e *VP8Encoder) baseSkipFalseProb(qIndex int) uint8 {
	if qIndex < 0 {
		qIndex = 0
	} else if qIndex >= len(libvpxBaseSkipFalseProbs) {
		qIndex = len(libvpxBaseSkipFalseProbs) - 1
	}
	if e == nil || e.baseSkipFalseProbs[qIndex] == 0 {
		return libvpxBaseSkipFalseProbs[qIndex]
	}
	return e.baseSkipFalseProbs[qIndex]
}

func skipFalseReferenceIndex(refreshGolden bool, refreshAltRef bool) int {
	if refreshAltRef {
		return 2
	}
	if refreshGolden {
		return 1
	}
	return 0
}

func clampInterAnalysisSkipFalseProb(prob uint8) uint8 {
	if prob < 5 {
		return 5
	}
	if prob > 250 {
		return 250
	}
	return prob
}

func (e *VP8Encoder) interFrameAnalysisSkipFalseProb(qIndex int, refreshGolden bool, refreshAltRef bool) uint8 {
	prob := e.baseSkipFalseProb(qIndex)
	if e != nil {
		if last := e.lastSkipFalseProbs[skipFalseReferenceIndex(refreshGolden, refreshAltRef)]; last != 0 {
			prob = last
		}
	}
	return clampInterAnalysisSkipFalseProb(prob)
}

func (e *VP8Encoder) commitInterFrameSkipFalseProb(attempt interFrameEncodeAttempt) {
	if e == nil || attempt.Config.ProbSkipFalse == 0 {
		return
	}
	prob := attempt.Config.ProbSkipFalse
	qIndex := int(attempt.Config.BaseQIndex)
	if qIndex < 0 {
		qIndex = 0
	} else if qIndex >= len(e.baseSkipFalseProbs) {
		qIndex = len(e.baseSkipFalseProbs) - 1
	}
	idx := skipFalseReferenceIndex(attempt.Config.RefreshGolden, attempt.Config.RefreshAltRef)
	e.probSkipFalse = prob
	e.lastSkipFalseProbs[idx] = prob
	if !attempt.Config.RefreshGolden && !attempt.Config.RefreshAltRef {
		e.baseSkipFalseProbs[qIndex] = prob
	}
}

func interFrameModeSkipFalseProbability(rows int, cols int, modes []vp8enc.InterFrameMacroblockMode, fallback uint8) uint8 {
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
	for i := 0; i < required; i++ {
		if modes[i].MBSkipCoeff {
			counts[1]++
		} else {
			counts[0]++
		}
	}
	return skipFalseProbabilityFromCounts(counts, fallback)
}

func skipFalseProbabilityFromCounts(counts [2]int, fallback uint8) uint8 {
	total := counts[0] + counts[1]
	if total <= 0 {
		if fallback == 0 {
			return 128
		}
		return fallback
	}
	prob := (counts[0]*256 + (total >> 1)) / total
	if prob <= 0 {
		return 1
	}
	if prob > 255 {
		return 255
	}
	return uint8(prob)
}
