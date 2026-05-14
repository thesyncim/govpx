package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const (
	vp9BPerMBNormBits = 9
	vp9FrameOverhead  = 200
	vp9MinBPBFactor   = 0.005
	vp9MaxBPBFactor   = 50.0

	vp9DefaultUndershootPct = 25
	vp9DefaultOvershootPct  = 25

	vp9RateFactorInterNormal = 0
	vp9RateFactorInterHigh   = 1
	vp9RateFactorGFARFLow    = 2
	vp9RateFactorGFARFStd    = 3
	vp9RateFactorKFStd       = 4
	vp9RateFactorLevels      = 5
)

func vp9MacroblockCount(miRows, miCols int) int {
	return ((miRows + 1) >> 1) * ((miCols + 1) >> 1)
}

func (rc *vp9RateControlState) keyFrameTargetBits(frameIndex int) int {
	if frameIndex == 0 {
		return rc.clampFrameTarget(rc.bufferInitialBits >> 1)
	}
	kfBoost := 32
	framerate := rc.frameRate()
	boostFromRate := int(math.Round(2*framerate - 16))
	if boostFromRate > kfBoost {
		kfBoost = boostFromRate
	}
	if framerate > 0 && float64(rc.framesSinceKey) < framerate/2 {
		kfBoost = int(math.Round(float64(kfBoost) * float64(rc.framesSinceKey) / (framerate / 2)))
	}
	return rc.clampFrameTarget(int(((int64(16+kfBoost) * int64(rc.bitsPerFrame)) >> 4)))
}

func (rc *vp9RateControlState) interFrameTargetBits() int {
	target := int64(rc.bitsPerFrame)
	diff := int64(rc.bufferOptimalBits - rc.bufferLevelBits)
	onePctBits := int64(1 + rc.bufferOptimalBits/100)
	if diff > 0 {
		pctLow := int(diff / onePctBits)
		if pctLow > vp9DefaultUndershootPct {
			pctLow = vp9DefaultUndershootPct
		}
		target -= (target * int64(pctLow)) / 200
	} else if diff < 0 {
		pctHigh := int((-diff) / onePctBits)
		if pctHigh > vp9DefaultOvershootPct {
			pctHigh = vp9DefaultOvershootPct
		}
		target += (target * int64(pctHigh)) / 200
	}
	if target > int64(maxInt()) {
		target = int64(maxInt())
	}
	minTarget := rc.bitsPerFrame >> 4
	if minTarget < vp9FrameOverhead {
		minTarget = vp9FrameOverhead
	}
	if int(target) < minTarget {
		return minTarget
	}
	return int(target)
}

func (rc *vp9RateControlState) frameRate() float64 {
	if rc.frameRateNum <= 0 || rc.frameRateDen <= 0 {
		return 30
	}
	return float64(rc.frameRateNum) / float64(rc.frameRateDen)
}

func (rc *vp9RateControlState) clampFrameTarget(target int) int {
	if target < 0 {
		return 0
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

func (rc *vp9RateControlState) cbrActiveWorstQuantizer(intraOnly bool, frameIndex int) int {
	worst := int(rc.worstQuality)
	if intraOnly {
		return worst
	}
	bufferLevel := rc.preEncodeBufferLevel()
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

func (rc *vp9RateControlState) preEncodeBufferLevel() int {
	level := saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	if level > rc.bufferSizeBits {
		return rc.bufferSizeBits
	}
	return level
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

func vp9KFActiveQuality(qindex int) int {
	return vp9ActiveQuality(qindex, 2000, 300, 4800,
		0.000001, -0.0004, 0.150,
		0.0000021, -0.00125, 0.45)
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

func (rc *vp9RateControlState) rateCorrectionFactor(intraOnly bool, refreshFlags uint8) float64 {
	level := vp9RateFactorInterNormal
	if intraOnly {
		level = vp9RateFactorKFStd
	} else if refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) != 0 {
		level = vp9RateFactorInterNormal
	}
	return normalizedVP9RateCorrectionFactor(rc.rateCorrectionFactors[level])
}

func (rc *vp9RateControlState) setRateCorrectionFactor(intraOnly bool, refreshFlags uint8, factor float64) {
	level := vp9RateFactorInterNormal
	if intraOnly {
		level = vp9RateFactorKFStd
	} else if refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) != 0 {
		level = vp9RateFactorInterNormal
	}
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
	level := vp9RateFactorInterNormal
	if intraOnly {
		level = vp9RateFactorKFStd
	}
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
	if !intraOnly && refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) != 0 &&
		(qindex < int(rc.lastBoostedQIndex) || rc.lastBoostedQIndex == 0) {
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
