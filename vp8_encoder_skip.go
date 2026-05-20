package govpx

import vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"

func (e *VP8Encoder) baseSkipFalseProb(qIndex int) uint8 {
	qIndex = min(max(qIndex, 0), len(vp8enc.DefaultBaseSkipFalseProbs)-1)
	if e.baseSkipFalseProbs[qIndex] == 0 {
		return vp8enc.DefaultBaseSkipFalseProbs[qIndex]
	}
	return e.baseSkipFalseProbs[qIndex]
}

func (e *VP8Encoder) interFrameAnalysisSkipFalseProb(qIndex int, refreshGolden bool, refreshAltRef bool, singleLayerSrcAltRef bool) uint8 {
	prob := e.baseSkipFalseProb(qIndex)
	if last := e.lastSkipFalseProbs[vp8enc.SkipFalseReferenceIndex(refreshGolden, refreshAltRef)]; last != 0 {
		prob = last
	}
	prob = vp8enc.ClampInterAnalysisSkipFalseProb(prob)
	if singleLayerSrcAltRef {
		return 1
	}
	return prob
}

func (e *VP8Encoder) commitInterFrameSkipFalseProb(attempt interFrameEncodeAttempt) {
	if attempt.Config.ProbSkipFalse == 0 {
		return
	}
	prob := attempt.Config.ProbSkipFalse
	qIndex := min(max(int(attempt.Config.BaseQIndex), 0), len(e.baseSkipFalseProbs)-1)
	idx := vp8enc.SkipFalseReferenceIndex(attempt.Config.RefreshGolden, attempt.Config.RefreshAltRef)
	e.probSkipFalse = prob
	e.lastSkipFalseProbs[idx] = prob
	if !attempt.Config.RefreshGolden && !attempt.Config.RefreshAltRef {
		e.baseSkipFalseProbs[qIndex] = prob
	}
}
