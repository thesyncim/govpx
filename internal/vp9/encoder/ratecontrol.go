package encoder

import "github.com/thesyncim/govpx/internal/vp9/tables"

const (
	bPerMBNormBits = 9

	// FrameOverhead is libvpx VP9's minimum frame target in bits.
	FrameOverhead = 200

	// MinBPBFactor and MaxBPBFactor bound VP9 rate-correction factors.
	MinBPBFactor = 0.005
	MaxBPBFactor = 50.0

	// MaxMBRateBits and MaxRate1080PBits bound one-pass VP9 frame bandwidth.
	MaxMBRateBits    = 250
	MaxRate1080PBits = 4000000

	RateFactorInterNormal = 0
	RateFactorInterHigh   = 1
	RateFactorGFARFLow    = 2
	RateFactorGFARFStd    = 3
	RateFactorKFStd       = 4
	RateFactorLevels      = 5

	// MinGFInterval and MaxGFInterval mirror libvpx's one-pass VP9 GF bounds.
	MinGFInterval   = 4
	MaxGFInterval   = 16
	FixedGFInterval = 8

	DefaultAFRatioOnePassVBR   = 10
	DefaultActiveWorstInterPct = 150
	DefaultActiveWorstGFPct    = 100
	DefaultVBRMaxSectionPct    = 2000
)

func maxInt() int {
	return int(^uint(0) >> 1)
}

// MacroblockCount returns the number of 16x16 macroblocks covering a VP9 mi
// grid. VP9 mi units are 8x8, so odd row/column counts round up.
func MacroblockCount(miRows, miCols int) int {
	return ((miRows + 1) >> 1) * ((miCols + 1) >> 1)
}

// RegulatedQuantizer selects the qindex whose projected bits-per-macroblock
// best meets targetBits inside [activeBest, activeWorst].
func RegulatedQuantizer(intraOnly bool, targetBits int, macroblocks int, activeBest int, activeWorst int, correctionFactor float64) int {
	if macroblocks <= 0 || targetBits <= 0 {
		return activeBest
	}
	targetBitsPerMB := int((uint64(targetBits) << bPerMBNormBits) / uint64(macroblocks))
	q := activeWorst
	lastError := maxInt()
	for i := activeBest; i <= activeWorst; i++ {
		bitsPerMB := BitsPerMB(intraOnly, i, correctionFactor)
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

// BitsPerMB estimates VP9 bits per macroblock for qindex and frame type.
func BitsPerMB(intraOnly bool, qindex int, correctionFactor float64) int {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	q := QIndexToQ(int16(qindex))
	enumerator := 1800000
	if intraOnly {
		enumerator = 2700000
	}
	enumerator += int(float64(enumerator)*q) >> 12
	return int(float64(enumerator) * NormalizeRateCorrectionFactor(correctionFactor) / q)
}

// EstimatedBitsAtQ estimates a frame's encoded size at qindex.
func EstimatedBitsAtQ(intraOnly bool, qindex int, macroblocks int, correctionFactor float64) int {
	if macroblocks <= 0 {
		return 0
	}
	bpm := BitsPerMB(intraOnly, qindex, correctionFactor)
	bits := int((uint64(bpm) * uint64(macroblocks)) >> bPerMBNormBits)
	if bits < FrameOverhead {
		return FrameOverhead
	}
	return bits
}

// QIndexToQ returns libvpx's VP9 active AC quantizer for qindex.
func QIndexToQ(qindex int16) float64 {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	return float64(tables.AcQLookup8[qindex]) / 4.0
}

// RTCMinQ returns libvpx's realtime minimum qindex for the active worst Q.
func RTCMinQ(qindex int) int {
	return minQIndex(qindex, 0.00000271, -0.00113, 0.70)
}

// InterMinQ returns libvpx's inter-frame minimum qindex for the active worst Q.
func InterMinQ(qindex int) int {
	return minQIndex(qindex, 0.00000271, -0.00113, 0.70)
}

// KFActiveQuality returns the default key-frame active-best qindex.
func KFActiveQuality(qindex int) int {
	return KFActiveQualityWithBoost(qindex, 2000)
}

// KFActiveQualityWithBoost returns the key-frame active-best qindex for boost.
func KFActiveQualityWithBoost(qindex, boost int) int {
	return activeQuality(qindex, boost, 300, 4800,
		0.000001, -0.0004, 0.150,
		0.0000021, -0.00125, 0.45)
}

// GFActiveQuality returns the default golden/alt-ref active-best qindex.
func GFActiveQuality(qindex int) int {
	return GFActiveQualityWithBoost(qindex, 2000)
}

// GFActiveQualityWithBoost returns the golden/alt-ref active-best qindex for boost.
func GFActiveQualityWithBoost(qindex, boost int) int {
	return activeQuality(qindex, boost, 400, 2000,
		0.0000015, -0.0009, 0.30,
		0.0000021, -0.00125, 0.55)
}

// GFLowMotionActiveQuality returns the low-motion golden-frame qindex floor.
func GFLowMotionActiveQuality(qindex int) int {
	return minQIndex(qindex, 0.0000015, -0.0009, 0.30)
}

// GFHighMotionActiveQuality returns the high-motion golden-frame qindex floor.
func GFHighMotionActiveQuality(qindex int) int {
	return minQIndex(qindex, 0.0000021, -0.00125, 0.55)
}

func activeQuality(qindex int, boost int, low int, high int, lowX3 float64, lowX2 float64, lowX1 float64, highX3 float64, highX2 float64, highX1 float64) int {
	if boost > high {
		return minQIndex(qindex, lowX3, lowX2, lowX1)
	}
	if boost < low {
		return minQIndex(qindex, highX3, highX2, highX1)
	}
	lowMotion := minQIndex(qindex, lowX3, lowX2, lowX1)
	highMotion := minQIndex(qindex, highX3, highX2, highX1)
	gap := high - low
	offset := high - boost
	qdiff := highMotion - lowMotion
	adjustment := ((offset * qdiff) + (gap >> 1)) / gap
	return lowMotion + adjustment
}

func minQIndex(qindex int, x3 float64, x2 float64, x1 float64) int {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	maxq := QIndexToQ(int16(qindex))
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

// ComputeQDeltaByRate returns the qindex delta needed to scale the estimated
// bits-per-macroblock by ratioNum/ratioDen.
func ComputeQDeltaByRate(best, worst int, intraOnly bool, qindex int, ratioNum int, ratioDen int) int {
	if ratioNum <= 0 || ratioDen <= 0 {
		return 0
	}
	qindex = min(max(qindex, best), worst)
	baseBitsPerMB := BitsPerMB(intraOnly, qindex, 1)
	targetBitsPerMB := (int64(baseBitsPerMB) * int64(ratioNum)) /
		int64(ratioDen)
	targetIndex := worst
	for i := best; i < worst; i++ {
		if int64(BitsPerMB(intraOnly, i, 1)) <= targetBitsPerMB {
			targetIndex = i
			break
		}
	}
	return targetIndex - qindex
}

// RoundedRatio returns num/den rounded to nearest int, saturating at MaxInt.
func RoundedRatio(num int64, den int64) int {
	if num <= 0 || den <= 0 {
		return 0
	}
	v := (num + (den >> 1)) / den
	if v > int64(maxInt()) {
		return maxInt()
	}
	return int(v)
}

// NormalizeRateCorrectionFactor clamps factor to libvpx's valid VP9 range.
func NormalizeRateCorrectionFactor(factor float64) float64 {
	if factor <= 0 {
		return 1
	}
	if factor < MinBPBFactor {
		return MinBPBFactor
	}
	if factor > MaxBPBFactor {
		return MaxBPBFactor
	}
	return factor
}
