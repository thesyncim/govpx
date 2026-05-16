package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const (
	vp9BPerMBNormBits   = 9
	vp9FrameOverhead    = 200
	vp9MinBPBFactor     = 0.005
	vp9MaxBPBFactor     = 50.0
	vp9MaxMBRateBits    = 250
	vp9MaxRate1080PBits = 4000000

	vp9RateFactorInterNormal = 0
	vp9RateFactorInterHigh   = 1
	vp9RateFactorGFARFLow    = 2
	vp9RateFactorGFARFStd    = 3
	vp9RateFactorKFStd       = 4
	vp9RateFactorLevels      = 5

	vp9MinGFInterval              = 4
	vp9MaxGFInterval              = 16
	vp9FixedGFInterval            = 8
	vp9DefaultAFRatioOnePassVBR   = 10
	vp9DefaultActiveWorstInterPct = 150
	vp9DefaultActiveWorstGFPct    = 100
	vp9DefaultVBRMaxSectionPct    = 2000
)

func vp9MacroblockCount(miRows, miCols int) int {
	return ((miRows + 1) >> 1) * ((miCols + 1) >> 1)
}

func (rc *vp9RateControlState) keyFrameTargetBits(_ int) int {
	if rc.mode != RateControlCBR {
		target := rc.onePassVBRKeyFrameTargetBits()
		return rc.applyVP9MaxIntraBound(target)
	}
	target := rc.perFrameBandwidthTargetBits()
	return rc.applyVP9MaxIntraBound(target)
}

func (rc *vp9RateControlState) interFrameTargetBits() int {
	return rc.perFrameBandwidthTargetBits()
}

// boostedInterFrameTargetBits applies the libvpx VP9 GF CBR boost on top of
// the per-frame bandwidth target, used for golden-frame refresh frames in
// CBR mode. Non-CBR refresh boosting is handled by
// onePassVBRInterFrameTargetBits.
func (rc *vp9RateControlState) boostedInterFrameTargetBits() int {
	target := rc.perFrameBandwidthTargetBits()
	target = rc.applyVP9GFCBRBoost(target)
	target = rc.applyVP9OvershootBound(target)
	return target
}

func (rc *vp9RateControlState) perFrameBandwidthTargetBits() int {
	target := rc.bitsPerFrame
	target = rc.applyVP9UndershootBound(target)
	target = rc.applyVP9OvershootBound(target)
	if target < vp9FrameOverhead {
		return vp9FrameOverhead
	}
	return target
}

func (rc *vp9RateControlState) setOnePassVBRFrameTarget(intraOnly bool, refreshFlags uint8) {
	if !rc.enabled || rc.mode == RateControlCBR {
		return
	}
	if intraOnly {
		rc.frameTargetBits = rc.onePassVBRKeyFrameTargetBits()
		return
	}
	rc.frameTargetBits = rc.onePassVBRInterFrameTargetBits(refreshFlags)
}

func (rc *vp9RateControlState) onePassVBRKeyFrameTargetBits() int {
	target := int64(rc.bitsPerFrame) * 25
	if target > int64(maxInt()) {
		target = int64(maxInt())
	}
	return rc.clampIFrameTargetBits(int(target))
}

func (rc *vp9RateControlState) onePassVBRInterFrameTargetBits(refreshFlags uint8) int {
	if !vp9BoostedInterRefresh(refreshFlags) {
		return rc.perFrameBandwidthTargetBits()
	}
	interval := int(rc.baselineGFInterval)
	if interval <= 0 {
		interval = (vp9MinGFInterval + vp9MaxGFInterval) >> 1
	}
	afRatio := int(rc.afRatioOnePassVBR)
	if afRatio <= 0 {
		afRatio = vp9DefaultAFRatioOnePassVBR
	}
	den := interval + afRatio - 1
	if den <= 0 {
		return rc.clampPFrameTargetBits(rc.bitsPerFrame)
	}
	target := int64(rc.bitsPerFrame) * int64(interval) * int64(afRatio)
	target /= int64(den)
	if target > int64(maxInt()) {
		target = int64(maxInt())
	}
	return rc.clampPFrameTargetBits(int(target))
}

func (rc *vp9RateControlState) clampPFrameTargetBits(target int) int {
	minTarget := rc.minFrameBandwidth
	if minTarget < vp9FrameOverhead {
		minTarget = vp9FrameOverhead
	}
	if rc.bitsPerFrame > 0 && rc.bitsPerFrame>>5 > minTarget {
		minTarget = rc.bitsPerFrame >> 5
	}
	if target < minTarget {
		target = minTarget
	}
	if rc.maxFrameBandwidth > 0 && target > rc.maxFrameBandwidth {
		target = rc.maxFrameBandwidth
	}
	return target
}

func (rc *vp9RateControlState) clampIFrameTargetBits(target int) int {
	if rc.maxFrameBandwidth > 0 && target > rc.maxFrameBandwidth {
		return rc.maxFrameBandwidth
	}
	if target < vp9FrameOverhead {
		return vp9FrameOverhead
	}
	return target
}

func (rc *vp9RateControlState) cbrQuantizer(intraOnly bool, refreshFlags uint8, frameIndex int, macroblocks int) int {
	if !rc.enabled || rc.mode != RateControlCBR || macroblocks <= 0 {
		return int(rc.bestQuality)
	}
	activeBest, activeWorst := rc.cbrActiveQuantizerBounds(intraOnly, refreshFlags, frameIndex)
	q := vp9RegulatedQuantizer(intraOnly, rc.frameTargetBits, macroblocks,
		activeBest, activeWorst, rc.rateCorrectionFactor(intraOnly, refreshFlags))
	return rc.adjustCBRQuantizer(q, refreshFlags)
}

func (rc *vp9RateControlState) cbrQuantizerWithBounds(intraOnly bool, refreshFlags uint8, frameIndex int, macroblocks int) (q int, activeBest int, activeWorst int, correctionFactor float64) {
	if !rc.enabled || rc.mode != RateControlCBR || macroblocks <= 0 {
		best := int(rc.bestQuality)
		return best, best, int(rc.worstQuality), 1
	}
	activeBest, activeWorst = rc.cbrActiveQuantizerBounds(intraOnly, refreshFlags, frameIndex)
	correctionFactor = rc.rateCorrectionFactor(intraOnly, refreshFlags)
	q = vp9RegulatedQuantizer(intraOnly, rc.frameTargetBits, macroblocks,
		activeBest, activeWorst, correctionFactor)
	return rc.adjustCBRQuantizer(q, refreshFlags), activeBest, activeWorst, correctionFactor
}

func (rc *vp9RateControlState) vbrQuantizer(intraOnly bool, refreshFlags uint8, frameIndex int, macroblocks int) int {
	q, _, _, _ := rc.vbrQuantizerWithBounds(intraOnly, refreshFlags,
		frameIndex, macroblocks)
	return q
}

func (rc *vp9RateControlState) vbrQuantizerWithBounds(intraOnly bool, refreshFlags uint8, frameIndex int, macroblocks int) (q int, activeBest int, activeWorst int, correctionFactor float64) {
	if !rc.enabled || rc.mode == RateControlCBR || macroblocks <= 0 {
		best := int(rc.bestQuality)
		return best, best, int(rc.worstQuality), 1
	}
	activeBest, activeWorst = rc.vbrActiveQuantizerBounds(intraOnly,
		refreshFlags, frameIndex)
	correctionFactor = rc.rateCorrectionFactor(intraOnly, refreshFlags)
	if rc.mode == RateControlQ {
		return activeBest, activeBest, activeWorst, correctionFactor
	}
	q = vp9RegulatedQuantizer(intraOnly, rc.frameTargetBits, macroblocks,
		activeBest, activeWorst, correctionFactor)
	return q, activeBest, activeWorst, correctionFactor
}

func (rc *vp9RateControlState) onePassRecodeAllowed() bool {
	// libvpx VP9 forces DISALLOW_RECODE for pass 0, which is the realtime
	// helper path used by the current byte-parity oracle.
	return false
}

func (rc *vp9RateControlState) cbrActiveQuantizerBounds(intraOnly bool, refreshFlags uint8, frameIndex int) (int, int) {
	best := int(rc.bestQuality)
	worst := int(rc.worstQuality)
	activeWorst := rc.cbrActiveWorstQuantizer(intraOnly, frameIndex)
	if activeWorst < best {
		activeWorst = best
	}
	if activeWorst > worst {
		activeWorst = worst
	}

	activeBest := best
	if intraOnly {
		activeBest = best
		if frameIndex > 0 {
			activeBest = vp9KFActiveQuality(int(rc.avgFrameQIndexKey))
			if int64(rc.codedWidth)*int64(rc.codedHeight) <= 352*288 {
				activeBest += vp9ComputeQDelta(best, worst, activeBest, 75, 100)
			}
		}
	} else {
		qBasis := activeWorst
		if frameIndex > 1 && int(rc.avgFrameQIndexInter) < activeWorst {
			qBasis = int(rc.avgFrameQIndexInter)
		} else if frameIndex <= 1 && int(rc.avgFrameQIndexKey) < activeWorst {
			qBasis = int(rc.avgFrameQIndexKey)
		}
		activeBest = vp9RTCMINQ(qBasis)
	}
	if activeBest < best {
		activeBest = best
	}
	if activeBest > worst {
		activeBest = worst
	}
	if activeWorst < activeBest {
		activeWorst = activeBest
	}
	return activeBest, activeWorst
}

func (rc *vp9RateControlState) vbrActiveQuantizerBounds(intraOnly bool, refreshFlags uint8, frameIndex int) (int, int) {
	best := int(rc.bestQuality)
	worst := int(rc.worstQuality)
	activeWorst := rc.vbrActiveWorstQuantizer(intraOnly, refreshFlags,
		frameIndex)
	if activeWorst < best {
		activeWorst = best
	}
	if activeWorst > worst {
		activeWorst = worst
	}
	cqLevel := rc.activeCQLevelOnePass()
	activeBest := best
	if intraOnly {
		if rc.mode == RateControlQ {
			activeBest = cqLevel + vp9ComputeQDelta(best, worst, cqLevel,
				1, 4)
			if activeBest < best {
				activeBest = best
			}
		} else {
			activeBest = vp9KFActiveQuality(int(rc.avgFrameQIndexKey))
			if int64(rc.codedWidth)*int64(rc.codedHeight) <= 352*288 {
				activeBest += vp9ComputeQDelta(best, worst,
					activeBest, 75, 100)
			}
		}
	} else if vp9BoostedInterRefresh(refreshFlags) {
		qBasis := int(rc.avgFrameQIndexKey)
		if rc.framesSinceKey > 1 {
			qBasis = min(int(rc.avgFrameQIndexInter), activeWorst)
		}
		switch rc.mode {
		case RateControlCQ:
			if qBasis < cqLevel {
				qBasis = cqLevel
			}
			activeBest = (vp9GFActiveQuality(qBasis) * 15) >> 4
		case RateControlQ:
			num := 1
			den := 2
			if refreshFlags&(1<<vp9AltRefSlot) != 0 {
				num = 2
				den = 5
			}
			activeBest = cqLevel + vp9ComputeQDelta(best, worst,
				cqLevel, num, den)
			if activeBest < best {
				activeBest = best
			}
		default:
			activeBest = vp9GFActiveQuality(qBasis)
		}
	} else if rc.mode == RateControlQ {
		num, den := vp9PublicQModeInterRate(frameIndex)
		activeBest = cqLevel + vp9ComputeQDelta(best, worst, cqLevel,
			num, den)
		if activeBest < best {
			activeBest = best
		}
	} else {
		if frameIndex > 1 {
			activeBest = vp9InterMINQ(min(int(rc.avgFrameQIndexInter),
				activeWorst))
		} else {
			activeBest = vp9InterMINQ(int(rc.avgFrameQIndexKey))
		}
		if rc.mode == RateControlCQ && activeBest < cqLevel {
			activeBest = cqLevel
		}
	}

	if activeBest < best {
		activeBest = best
	}
	if activeBest > worst {
		activeBest = worst
	}
	if activeWorst < activeBest {
		activeWorst = activeBest
	}
	if intraOnly && frameIndex != 0 {
		activeWorst += vp9ComputeQDeltaByRate(best, worst, intraOnly,
			activeWorst, 2, 1)
		if activeWorst < activeBest {
			activeWorst = activeBest
		}
	} else if !intraOnly && vp9BoostedInterRefresh(refreshFlags) {
		activeWorst += vp9ComputeQDeltaByRate(best, worst, intraOnly,
			activeWorst, 7, 4)
		if activeWorst < activeBest {
			activeWorst = activeBest
		}
	}
	if activeWorst > worst {
		activeWorst = worst
	}
	return activeBest, activeWorst
}

func (rc *vp9RateControlState) vbrActiveWorstQuantizer(intraOnly bool, refreshFlags uint8, frameIndex int) int {
	worst := int(rc.worstQuality)
	if intraOnly {
		if frameIndex == 0 {
			return worst
		}
		return min(int(rc.lastQKey)<<1, worst)
	}
	if vp9BoostedInterRefresh(refreshFlags) {
		if frameIndex == 1 {
			return min((int(rc.lastQKey)*5)>>2, worst)
		}
		return min(int(rc.lastQInter)*int(rc.facActiveWorstGF)/100, worst)
	}
	if frameIndex == 1 {
		return min(int(rc.lastQKey)<<1, worst)
	}
	return min(int(rc.avgFrameQIndexInter)*int(rc.facActiveWorstInter)/100,
		worst)
}

func (rc *vp9RateControlState) activeCQLevelOnePass() int {
	level := int(rc.cqLevel)
	if rc.mode == RateControlCQ && rc.totalTargetBits > 0 {
		adjusted := (int64(level) * rc.totalActualBits * 10) /
			rc.totalTargetBits
		if adjusted < int64(level) {
			level = int(adjusted)
		}
	}
	return level
}

func (rc *vp9RateControlState) onePassVBRGoldenRefreshDue() bool {
	return rc.enabled && rc.mode != RateControlCBR && rc.framesTillGF == 0
}

func (rc *vp9RateControlState) setRuntimeOnePassVBRGoldenCadence(prev vp9RateControlState) {
	if rc == nil || !rc.enabled || rc.mode == RateControlCBR {
		return
	}
	if prev.enabled && prev.mode != RateControlCBR {
		rc.framesTillGF = prev.framesTillGF
		return
	}
	if rc.framesTillGF == 0 {
		rc.framesTillGF = rc.runtimeOnePassVBRGoldenInterval()
	}
}

func (rc *vp9RateControlState) runtimeOnePassVBRGoldenInterval() uint8 {
	interval := rc.baselineGFInterval
	if interval == 0 {
		interval = (vp9MinGFInterval + vp9MaxGFInterval) >> 1
	}
	return interval
}

func (rc *vp9RateControlState) postOnePassVBRRefresh(refreshFlags uint8) {
	if !rc.enabled || rc.mode == RateControlCBR {
		return
	}
	if refreshFlags&(1<<vp9GoldenRefSlot) != 0 && rc.framesTillGF == 0 {
		rc.framesTillGF = rc.runtimeOnePassVBRGoldenInterval()
	}
	if refreshFlags&(1<<vp9GoldenRefSlot) != 0 ||
		refreshFlags&(1<<vp9AltRefSlot) == 0 {
		if rc.framesTillGF > 0 {
			rc.framesTillGF--
		}
	}
}

func (rc *vp9RateControlState) cbrActiveWorstQuantizer(intraOnly bool, frameIndex int) int {
	worst := int(rc.worstQuality)
	if intraOnly {
		return worst
	}
	bufferLevel := rc.bufferLevelBits
	criticalLevel := rc.bufferOptimalBits >> 3
	ambientQP := int(rc.avgFrameQIndexInter)
	if frameIndex < 5 {
		if int(rc.avgFrameQIndexKey) < ambientQP {
			ambientQP = int(rc.avgFrameQIndexKey)
		}
	}
	activeWorst := (ambientQP * 5) >> 2
	if activeWorst > worst {
		activeWorst = worst
	}
	if bufferLevel > rc.bufferOptimalBits {
		maxAdjustmentDown := activeWorst / 3
		if maxAdjustmentDown > 0 {
			step := (rc.bufferSizeBits - rc.bufferOptimalBits) / maxAdjustmentDown
			adjustment := 0
			if step > 0 {
				adjustment = (bufferLevel - rc.bufferOptimalBits) / step
			}
			activeWorst -= adjustment
		}
	} else if bufferLevel > criticalLevel {
		if criticalLevel > 0 {
			step := rc.bufferOptimalBits - criticalLevel
			if step > 0 {
				activeWorst = ambientQP + int((int64(worst-ambientQP)*int64(rc.bufferOptimalBits-bufferLevel))/int64(step))
			}
		}
	} else {
		activeWorst = worst
	}
	return activeWorst
}

func (rc *vp9RateControlState) adjustCBRQuantizer(q int, refreshFlags uint8) int {
	if rc.rc1Frame*rc.rc2Frame == -1 && rc.q1Frame != rc.q2Frame && refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) == 0 {
		low := int(rc.q1Frame)
		high := int(rc.q2Frame)
		if low > high {
			low, high = high, low
		}
		qClamp := min(max(q, low), high)
		if rc.rc1Frame == -1 && q > qClamp {
			q = (q + qClamp) >> 1
		} else {
			q = qClamp
		}
	}
	best := int(rc.bestQuality)
	worst := int(rc.worstQuality)
	return min(max(q, best), worst)
}

func vp9RegulatedQuantizer(intraOnly bool, targetBits int, macroblocks int, activeBest int, activeWorst int, correctionFactor float64) int {
	if macroblocks <= 0 || targetBits <= 0 {
		return activeBest
	}
	targetBitsPerMB := int((uint64(targetBits) << vp9BPerMBNormBits) / uint64(macroblocks))
	q := activeWorst
	lastError := maxInt()
	for i := activeBest; i <= activeWorst; i++ {
		bitsPerMB := vp9RCBitsPerMB(intraOnly, i, correctionFactor)
		diffBits := targetBitsPerMB - bitsPerMB
		if bitsPerMB <= targetBitsPerMB {
			if diffBits <= lastError {
				q = i
			} else {
				q = i - 1
			}
			break
		}
		lastError = -diffBits
	}
	return min(max(q, activeBest), activeWorst)
}

func vp9RCBitsPerMB(intraOnly bool, qindex int, correctionFactor float64) int {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	q := vp9QIndexToQ(int16(qindex))
	enumerator := 1800000
	if intraOnly {
		enumerator = 2700000
	}
	enumerator += int(float64(enumerator)*q) >> 12
	return int(float64(enumerator) * normalizedVP9RateCorrectionFactor(correctionFactor) / q)
}

func vp9EstimatedBitsAtQ(intraOnly bool, qindex int, macroblocks int, correctionFactor float64) int {
	if macroblocks <= 0 {
		return 0
	}
	bpm := vp9RCBitsPerMB(intraOnly, qindex, correctionFactor)
	bits := int((uint64(bpm) * uint64(macroblocks)) >> vp9BPerMBNormBits)
	if bits < vp9FrameOverhead {
		return vp9FrameOverhead
	}
	return bits
}

func vp9QIndexToQ(qindex int16) float64 {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	return float64(tables.AcQLookup8[qindex]) / 4.0
}

func vp9RTCMINQ(qindex int) int {
	return vp9MinQIndex(qindex, 0.00000271, -0.00113, 0.70)
}

func vp9InterMINQ(qindex int) int {
	return vp9MinQIndex(qindex, 0.00000271, -0.00113, 0.70)
}

func vp9KFActiveQuality(qindex int) int {
	return vp9ActiveQuality(qindex, 2000, 300, 4800,
		0.000001, -0.0004, 0.150,
		0.0000021, -0.00125, 0.45)
}

func vp9GFActiveQuality(qindex int) int {
	return vp9ActiveQuality(qindex, 2000, 400, 2000,
		0.0000015, -0.0009, 0.30,
		0.0000021, -0.00125, 0.55)
}

func vp9ActiveQuality(qindex int, boost int, low int, high int, lowX3 float64, lowX2 float64, lowX1 float64, highX3 float64, highX2 float64, highX1 float64) int {
	if boost > high {
		return vp9MinQIndex(qindex, lowX3, lowX2, lowX1)
	}
	if boost < low {
		return vp9MinQIndex(qindex, highX3, highX2, highX1)
	}
	lowMotion := vp9MinQIndex(qindex, lowX3, lowX2, lowX1)
	highMotion := vp9MinQIndex(qindex, highX3, highX2, highX1)
	gap := high - low
	offset := high - boost
	qdiff := highMotion - lowMotion
	adjustment := ((offset * qdiff) + (gap >> 1)) / gap
	return lowMotion + adjustment
}

func vp9MinQIndex(qindex int, x3 float64, x2 float64, x1 float64) int {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	maxq := vp9QIndexToQ(int16(qindex))
	minqTarget := (((x3*maxq)+x2)*maxq + x1) * maxq
	if minqTarget > maxq {
		minqTarget = maxq
	}
	if minqTarget <= 2 {
		return 0
	}
	targetAC := minqTarget * 4
	for i, q := range tables.AcQLookup8 {
		if targetAC <= float64(q) {
			return i
		}
	}
	return 255
}

func vp9ComputeQDeltaByRate(best, worst int, intraOnly bool, qindex int, ratioNum int, ratioDen int) int {
	if ratioNum <= 0 || ratioDen <= 0 {
		return 0
	}
	qindex = min(max(qindex, best), worst)
	baseBitsPerMB := vp9RCBitsPerMB(intraOnly, qindex, 1)
	targetBitsPerMB := (int64(baseBitsPerMB) * int64(ratioNum)) /
		int64(ratioDen)
	targetIndex := worst
	for i := best; i < worst; i++ {
		if int64(vp9RCBitsPerMB(intraOnly, i, 1)) <= targetBitsPerMB {
			targetIndex = i
			break
		}
	}
	return targetIndex - qindex
}

func (rc *vp9RateControlState) rateFactorLevel(intraOnly bool, refreshFlags uint8) int {
	if intraOnly {
		return vp9RateFactorKFStd
	}
	if vp9BoostedInterRefresh(refreshFlags) && rc.mode != RateControlCBR {
		return vp9RateFactorGFARFStd
	}
	return vp9RateFactorInterNormal
}

func (rc *vp9RateControlState) rateCorrectionFactor(intraOnly bool, refreshFlags uint8) float64 {
	level := rc.rateFactorLevel(intraOnly, refreshFlags)
	return normalizedVP9RateCorrectionFactor(rc.rateCorrectionFactors[level])
}

func (rc *vp9RateControlState) setRateCorrectionFactor(intraOnly bool, refreshFlags uint8, factor float64) {
	level := rc.rateFactorLevel(intraOnly, refreshFlags)
	rc.rateCorrectionFactors[level] = min(max(factor, vp9MinBPBFactor), vp9MaxBPBFactor)
}

func (rc *vp9RateControlState) updateRateCorrectionFactor(actualBits int, qindex int, intraOnly bool, refreshFlags uint8, macroblocks int) {
	if actualBits <= 0 || macroblocks <= 0 {
		return
	}
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	level := rc.rateFactorLevel(intraOnly, refreshFlags)
	rateCorrectionFactor := rc.rateCorrectionFactor(intraOnly, refreshFlags)
	projectedBits := vp9EstimatedBitsAtQ(intraOnly, qindex, macroblocks, rateCorrectionFactor)
	correctionFactor := 100
	if projectedBits > vp9FrameOverhead {
		correctionFactor = int((100 * int64(actualBits)) / int64(projectedBits))
	}
	adjustmentLimit := 1.0
	if rc.dampedAdjustment[level] {
		adjustmentLimit = 0.25 + 0.5*math.Min(1, math.Abs(math.Log10(0.01*float64(correctionFactor))))
	} else {
		rc.dampedAdjustment[level] = true
	}

	rc.q2Frame = rc.q1Frame
	rc.q1Frame = uint8(qindex)
	rc.rc2Frame = rc.rc1Frame
	if correctionFactor > 110 {
		rc.rc1Frame = -1
	} else if correctionFactor < 90 {
		rc.rc1Frame = 1
	} else {
		rc.rc1Frame = 0
	}
	if rc.rc1Frame == -1 && rc.rc2Frame == 1 && correctionFactor > 1000 {
		rc.rc2Frame = 0
	}

	if correctionFactor > 102 {
		correctionFactor = int(100 + float64(correctionFactor-100)*adjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
	} else if correctionFactor < 99 {
		correctionFactor = int(100 - float64(100-correctionFactor)*adjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
	}
	rc.setRateCorrectionFactor(intraOnly, refreshFlags, rateCorrectionFactor)
}

func vp9BoostedInterRefresh(refreshFlags uint8) bool {
	return refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) != 0
}

func vp9DefaultMinGFInterval(timing timingState) int {
	num := int64(timing.timebaseDen)
	den := int64(timing.timebaseNum) * int64(timing.frameDuration) * 8
	interval := vp9RoundedRatio(num, den)
	if interval < vp9MinGFInterval {
		return vp9MinGFInterval
	}
	if interval > vp9MaxGFInterval {
		return vp9MaxGFInterval
	}
	return interval
}

func vp9DefaultMaxGFInterval(timing timingState, minInterval int) int {
	num := int64(timing.timebaseDen) * 3
	den := int64(timing.timebaseNum) * int64(timing.frameDuration) * 4
	interval := vp9RoundedRatio(num, den)
	if interval > vp9MaxGFInterval {
		interval = vp9MaxGFInterval
	}
	interval += interval & 1
	if interval < minInterval {
		interval = minInterval
	}
	return interval
}

func vp9RoundedRatio(num int64, den int64) int {
	if num <= 0 || den <= 0 {
		return 0
	}
	v := (num + (den >> 1)) / den
	if v > int64(maxInt()) {
		return maxInt()
	}
	return int(v)
}

func (rc *vp9RateControlState) updateQHistory(qindex int, intraOnly bool, refreshFlags uint8, showFrame bool) {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	q := uint8(qindex)
	rc.q1Frame = q
	if intraOnly {
		rc.lastQKey = q
		rc.avgFrameQIndexKey = uint8((3*int(rc.avgFrameQIndexKey) + qindex + 2) >> 2)
		rc.lastBoostedQIndex = q
		rc.framesSinceKey = 0
	} else if refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) == 0 {
		rc.lastQInter = q
		rc.avgFrameQIndexInter = uint8((3*int(rc.avgFrameQIndexInter) + qindex + 2) >> 2)
	}
	if !intraOnly && refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) != 0 {
		rc.lastBoostedQIndex = q
	}
	if showFrame {
		rc.incrementFramesSinceKey()
	}
}

func (rc *vp9RateControlState) incrementFramesSinceKey() {
	if rc.framesSinceKey != ^uint16(0) {
		rc.framesSinceKey++
	}
}

func normalizedVP9RateCorrectionFactor(factor float64) float64 {
	if factor <= 0 {
		return 1
	}
	if factor < vp9MinBPBFactor {
		return vp9MinBPBFactor
	}
	if factor > vp9MaxBPBFactor {
		return vp9MaxBPBFactor
	}
	return factor
}
