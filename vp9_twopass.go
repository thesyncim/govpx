package govpx

import "math"

const (
	vp9DefaultTwoPassVBRBiasPct = 50
	vp9MinActiveArea            = 0.5
	vp9MaxActiveArea            = 1.0
	vp9ActiveAreaCorrection     = 0.5
)

type vp9TwoPassState struct {
	stats               []VP9FirstPassFrameStats
	totalStats          VP9FirstPassFrameStats
	bitsLeft            int64
	normalizedScoreLeft float64
	meanModScore        float64
	frameIndex          uint64
	currentTargetBits   int
	vbrBiasPct          int
	minPct              int
	maxPct              int
	minFrameBandwidth   int
	mbRows              int
}

func validateVP9TwoPassOptions(opts VP9EncoderOptions) error {
	if opts.TwoPassVBRBiasPct < 0 || opts.TwoPassMinPct < 0 ||
		opts.TwoPassMaxPct < 0 {
		return ErrInvalidConfig
	}
	if len(opts.TwoPassStats) == 0 {
		return nil
	}
	if !opts.RateControlModeSet ||
		(opts.RateControlMode != RateControlVBR &&
			opts.RateControlMode != RateControlCQ) {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP9Encoder) prepareVP9SecondPassFrameTarget(intraOnly bool, refreshFlags uint8) {
	e.vp9TwoPassFrameTarget = 0
	if e.twoPass.enabled() {
		if target := e.twoPass.frameTargetBits(e.rc.frameTargetBits); target > 0 {
			e.rc.frameTargetBits = target
			e.vp9TwoPassFrameTarget = target
			return
		}
	}
	e.rc.setOnePassVBRFrameTarget(intraOnly, refreshFlags)
}

func (t *vp9TwoPassState) configure(stats []VP9FirstPassFrameStats, bitsPerFrame int,
	biasPct int, minPct int, maxPct int, height int,
) {
	*t = vp9TwoPassState{}
	if len(stats) == 0 || bitsPerFrame <= 0 {
		return
	}
	t.stats, t.totalStats = normalizeVP9TwoPassStats(stats)
	if len(t.stats) == 0 {
		return
	}
	t.vbrBiasPct = biasPct
	if t.vbrBiasPct <= 0 {
		t.vbrBiasPct = vp9DefaultTwoPassVBRBiasPct
	}
	t.minPct = minPct
	t.maxPct = maxPct
	if t.maxPct <= 0 {
		t.maxPct = vp9DefaultVBRMaxSectionPct
	}
	t.minFrameBandwidth = vbrMinFrameBandwidthBits(bitsPerFrame, t.minPct)
	t.bitsLeft = int64(bitsPerFrame) * int64(len(t.stats))
	t.mbRows = (height + 15) >> 4
	if t.mbRows <= 0 {
		t.mbRows = 1
	}

	avErr := t.distributionAverageError()
	rawTotal := 0.0
	for i := range t.stats {
		rawTotal += t.modifiedFrameScore(t.stats[i], avErr)
	}
	t.meanModScore = rawTotal / nonZeroFloat(t.totalStats.Count)
	if t.meanModScore <= 0 {
		t.meanModScore = 1
	}
	for i := range t.stats {
		t.normalizedScoreLeft += t.normalizedFrameScore(t.stats[i], avErr)
	}
	if t.normalizedScoreLeft <= 0 {
		t.normalizedScoreLeft = float64(len(t.stats))
	}
}

func (t *vp9TwoPassState) enabled() bool {
	return len(t.stats) > 0
}

func (t *vp9TwoPassState) statsForFrame() VP9FirstPassFrameStats {
	if !t.enabled() || t.frameIndex >= uint64(len(t.stats)) {
		return VP9FirstPassFrameStats{}
	}
	return t.stats[t.frameIndex]
}

func (t *vp9TwoPassState) frameTargetBits(defaultTargetBits int) int {
	t.currentTargetBits = 0
	if !t.enabled() || t.frameIndex >= uint64(len(t.stats)) ||
		defaultTargetBits <= 0 {
		return 0
	}
	score := t.normalizedFrameScore(t.stats[t.frameIndex],
		t.distributionAverageError())
	if score <= 0 || t.normalizedScoreLeft <= 0 || t.bitsLeft <= 0 {
		return 0
	}
	target := int64(float64(t.bitsLeft) * score / t.normalizedScoreLeft)
	target += int64(t.minFrameBandwidth)
	maxBits := int64(defaultTargetBits) * int64(t.maxPct) / 100
	if maxBits > 0 && target > maxBits {
		target = maxBits
	}
	if target < int64(vp9FrameOverhead) {
		target = int64(vp9FrameOverhead)
	}
	if target > int64(maxInt()) {
		target = int64(maxInt())
	}
	t.currentTargetBits = int(target)
	return t.currentTargetBits
}

func (t *vp9TwoPassState) finishFrame() {
	if !t.enabled() || t.frameIndex >= uint64(len(t.stats)) {
		return
	}
	score := t.normalizedFrameScore(t.stats[t.frameIndex],
		t.distributionAverageError())
	t.normalizedScoreLeft -= score
	if t.normalizedScoreLeft < 0 {
		t.normalizedScoreLeft = 0
	}
	target := t.currentTargetBits
	if target <= 0 {
		target = vp9FrameOverhead
	}
	t.bitsLeft -= int64(target)
	if t.bitsLeft < 0 {
		t.bitsLeft = 0
	}
	t.frameIndex++
	t.currentTargetBits = 0
}

func (t *vp9TwoPassState) distributionAverageError() float64 {
	if t.totalStats.Count <= 0 {
		return 1
	}
	avgWeight := t.totalStats.Weight / t.totalStats.Count
	if avgWeight <= 0 {
		avgWeight = 1
	}
	avErr := (t.totalStats.CodedError * avgWeight) / t.totalStats.Count
	if avErr <= 0 {
		return 1
	}
	return avErr
}

func (t *vp9TwoPassState) modifiedFrameScore(row VP9FirstPassFrameStats, avErr float64) float64 {
	err := row.CodedError
	if err < 1 {
		err = 1
	}
	weight := row.Weight
	if weight <= 0 {
		weight = 1
	}
	score := avErr * math.Pow((err*weight)/nonZeroFloat(avErr),
		float64(t.vbrBiasPct)/100.0)
	score *= math.Pow(t.activeArea(row), vp9ActiveAreaCorrection)
	if score <= 0 || math.IsNaN(score) || math.IsInf(score, 0) {
		return 1
	}
	return score
}

func (t *vp9TwoPassState) normalizedFrameScore(row VP9FirstPassFrameStats, avErr float64) float64 {
	score := t.modifiedFrameScore(row, avErr) / nonZeroFloat(t.meanModScore)
	minScore := float64(t.minPct) / 100.0
	maxScore := float64(t.maxPct) / 100.0
	if maxScore <= 0 {
		maxScore = float64(vp9DefaultVBRMaxSectionPct) / 100.0
	}
	if score < minScore {
		score = minScore
	}
	if score > maxScore {
		score = maxScore
	}
	if score <= 0 || math.IsNaN(score) || math.IsInf(score, 0) {
		return 1
	}
	return score
}

func (t *vp9TwoPassState) activeArea(row VP9FirstPassFrameStats) float64 {
	active := 1.0 - ((row.IntraSkipPct / 2.0) +
		((row.InactiveZoneRows * 2.0) / float64(t.mbRows)))
	if active < vp9MinActiveArea {
		return vp9MinActiveArea
	}
	if active > vp9MaxActiveArea {
		return vp9MaxActiveArea
	}
	return active
}

func normalizeVP9TwoPassStats(stats []VP9FirstPassFrameStats) ([]VP9FirstPassFrameStats, VP9FirstPassFrameStats) {
	if len(stats) == 0 {
		return nil, VP9FirstPassFrameStats{}
	}
	if len(stats) > 1 {
		last := stats[len(stats)-1]
		if last.IsTotal {
			return stats[:len(stats)-1], last
		}
	}
	var total VP9FirstPassFrameStats
	for i := range stats {
		accumulateVP9FirstPassStats(&total, stats[i])
	}
	return stats, total
}

func nonZeroFloat(v float64) float64 {
	if v < 1e-12 && v > -1e-12 {
		return 1
	}
	return v
}
