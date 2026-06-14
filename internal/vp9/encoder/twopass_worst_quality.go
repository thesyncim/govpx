package encoder

import "math"

const (
	SectionNoiseDefault = 250.0
	NoiseFactorMin      = 0.9
	NoiseFactorMax      = 1.1
)

var qPowTerm = [(256 >> 5) + 1]float64{
	0.65, 0.70, 0.75, 0.85, 0.90, 0.90, 0.90, 1.00, 1.25,
}

// TwoPassWorstQualityInputs carries the state read by
// get_twopass_worst_quality.
type TwoPassWorstQualityInputs struct {
	SectionError           float64
	InactiveZone           float64
	SectionNoise           float64
	SectionTargetBandwidth int

	BestQuality  int
	WorstQuality int
	CQLevel      int
	IsCQ         bool

	AvgFrameBandwidth  int
	MinFrameBandwidth  int
	MaxFrameBandwidth  int
	MaxInterBitratePct int

	Macroblocks int
	Speed       int
	BPMFactor   float64
	Width       int
	Height      int
}

// TwoPassWorstQuality ports libvpx get_twopass_worst_quality.
func TwoPassWorstQuality(in TwoPassWorstQualityInputs) int {
	best := clampInt(in.BestQuality, 0, 255)
	worst := clampInt(in.WorstQuality, best, 255)
	targetRate := clampTwoPassPFrameTarget(in)
	if targetRate <= 0 {
		return worst
	}
	noise := in.SectionNoise
	if noise <= 0 {
		noise = SectionNoiseDefault
	}
	noiseFactor := math.Sqrt(noise / SectionNoiseDefault)
	noiseFactor = clampFloat(noiseFactor, NoiseFactorMin, NoiseFactorMax)
	inactiveZone := clampFloat(in.InactiveZone, 0, 1)
	activePct := math.Max(0.01, 1.0-inactiveZone)
	mbs := in.Macroblocks
	if mbs <= 0 {
		mbs = MacroblockCount((in.Height+7)>>3, (in.Width+7)>>3)
	}
	if mbs <= 0 {
		return worst
	}
	activeMBs := int(math.Max(1, float64(mbs)*activePct))
	avErrPerMB := in.SectionError / activePct
	speedTerm := 1.0 + 0.04*float64(in.Speed)
	bpmFactor := in.BPMFactor
	if bpmFactor <= 0 {
		bpmFactor = 1.0
	}
	targetNormBitsPerMB := (uint64(targetRate) << bPerMBNormBits) /
		uint64(activeMBs)
	q := best
	for ; q < worst; q++ {
		factor := CalcTwoPassCorrectionFactor(avErrPerMB,
			WorstQualityErrorDivisor(in.Width, in.Height), q)
		bitsPerMB := BitsPerMB(false, q,
			factor*speedTerm*bpmFactor*noiseFactor)
		if uint64(bitsPerMB) <= targetNormBitsPerMB {
			break
		}
	}
	if in.IsCQ && q < in.CQLevel {
		q = in.CQLevel
	}
	return clampInt(q, best, worst)
}

func clampTwoPassPFrameTarget(in TwoPassWorstQualityInputs) int {
	target := in.SectionTargetBandwidth
	minTarget := in.MinFrameBandwidth
	if shift := in.AvgFrameBandwidth >> 5; shift > minTarget {
		minTarget = shift
	}
	if minTarget < FrameOverhead {
		minTarget = FrameOverhead
	}
	if target < minTarget {
		target = minTarget
	}
	if in.MaxFrameBandwidth > 0 && target > in.MaxFrameBandwidth {
		target = in.MaxFrameBandwidth
	}
	if in.MaxInterBitratePct > 0 && in.AvgFrameBandwidth > 0 {
		maxRate := int64(in.AvgFrameBandwidth) *
			int64(in.MaxInterBitratePct) / 100
		if int64(target) > maxRate {
			target = int(maxRate)
		}
	}
	return target
}

// CalcTwoPassCorrectionFactor ports calc_correction_factor.
func CalcTwoPassCorrectionFactor(errPerMB, errDivisor float64, q int) float64 {
	errorTerm := errPerMB / doubleDivideCheck(errDivisor)
	index := q >> 5
	if index < 0 {
		index = 0
	}
	if index >= len(qPowTerm)-1 {
		index = len(qPowTerm) - 2
	}
	powerTerm := qPowTerm[index] +
		((qPowTerm[index+1]-qPowTerm[index])*float64(q%32))/32.0
	return clampFloat(math.Pow(errorTerm, powerTerm), 0.05, 5.0)
}

// WorstQualityErrorDivisor ports wq_err_divisor.
func WorstQualityErrorDivisor(width, height int) float64 {
	area := width * height
	switch {
	case area <= 640*360:
		return 115.0
	case area < 1280*720:
		return 125.0
	case area <= 1920*1080:
		return 130.0
	case area < 3840*2160:
		return 150.0
	default:
		return 200.0
	}
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
