package encoder

import "math"

// VP9 two-pass keyframe-group setup ported from libvpx
// vp9_firstpass.c:3288 find_next_key_frame.
const (
	MinKFTotalBoost             = 300
	DefaultScanFramesForKFBoost = 32
	MaxScanFramesForKFBoost     = 48
	MinScanFramesForKFBoost     = 32
	KFAbsZoomThresh             = 6.0
	KFMinFrameBoost             = 40.0
	KFMaxFrameBoost             = 96.0
	MaxKFTotalBoost             = 5400
)

// KeyFrameGroupInputs carries the libvpx state consumed by find_next_key_frame.
type KeyFrameGroupInputs struct {
	Stats []FirstPassFrameStats

	StartShowIdx      int
	KeyFrameFrequency int
	AutoKey           bool
	MinGFInterval     int

	BitsLeft            int64
	NormalizedScoreLeft float64
	MaxFrameBandwidth   int

	MeanModScore float64
	AvErr        float64
	MBRows       int

	TwoPassVBRBiasPct    int
	TwoPassVBRMinSection int
	TwoPassVBRMaxSection int

	CurrentVideoFrame   int
	AvgFrameQIndexInter int
	FrameWidth          int
	FrameHeight         int
	BoostParams         ARFBoostParams
}

// KeyFrameGroupResult mirrors the key fields find_next_key_frame leaves in
// RATE_CONTROL, TWO_PASS, and GF_GROUP before define_gf_group runs.
type KeyFrameGroupResult struct {
	FramesToKey        int
	NextKeyFrameForced bool
	KeyFrameBoost      int
	KeyFrameBits       int
	KFGroupBitsLeft    int64
	KFGroupErrorLeft   float64
	KFZeroMotionPct    int
}

// PrepareKeyFrameGroup ports the bit/boost subset of find_next_key_frame that
// feeds VP9 second-pass q selection and GF-group allocation.
func PrepareKeyFrameGroup(in KeyFrameGroupInputs) KeyFrameGroupResult {
	if in.StartShowIdx < 0 || in.StartShowIdx >= len(in.Stats) {
		return KeyFrameGroupResult{}
	}
	if in.KeyFrameFrequency <= 0 {
		in.KeyFrameFrequency = MaxStaticGFGroupLength
	}
	if in.MinGFInterval <= 0 {
		in.MinGFInterval = MinGFInterval
	}
	if in.MBRows <= 0 {
		in.MBRows = 1
	}
	if in.BoostParams == (ARFBoostParams{}) {
		in.BoostParams = DefaultARFBoostParams(in.MBRows)
	}

	framesToKey := GetFramesToNextKey(in)
	if framesToKey <= 0 {
		return KeyFrameGroupResult{}
	}
	keyFrameStats := in.Stats[in.StartShowIdx]
	keyFrameModScore := CalcNormFrameScoreConfig(keyFrameStats,
		in.MeanModScore, in.AvErr, in.MBRows,
		in.TwoPassVBRBiasPct, in.TwoPassVBRMinSection,
		in.TwoPassVBRMaxSection)
	kfGroupErr := 0.0
	for i := 0; i < framesToKey; i++ {
		idx := in.StartShowIdx + i
		if idx < 0 || idx >= len(in.Stats) {
			break
		}
		kfGroupErr += CalcNormFrameScoreConfig(in.Stats[idx],
			in.MeanModScore, in.AvErr, in.MBRows,
			in.TwoPassVBRBiasPct, in.TwoPassVBRMinSection,
			in.TwoPassVBRMaxSection)
	}

	kfGroupBits := int64(0)
	if in.BitsLeft > 0 && in.NormalizedScoreLeft > 0 && kfGroupErr > 0 {
		kfGroupBits = int64(float64(in.BitsLeft) *
			(kfGroupErr / in.NormalizedScoreLeft))
		if in.MaxFrameBandwidth > 0 {
			maxGroupBits := int64(in.MaxFrameBandwidth) * int64(framesToKey)
			if kfGroupBits > maxGroupBits {
				kfGroupBits = maxGroupBits
			}
		}
		if kfGroupBits < 0 {
			kfGroupBits = 0
		}
	}

	boost, zeroMotionPct := ComputeKeyFrameBoost(in, framesToKey,
		keyFrameStats.IntraError)
	kfBits := CalculateBoostBits(framesToKey-1, boost, kfGroupBits)
	if kfGroupErr > 0 {
		kfBits += int(float64(kfGroupBits-int64(kfBits)) *
			(keyFrameModScore / kfGroupErr))
	}
	maxKFBits := kfGroupBits - int64(framesToKey-1)*int64(FrameOverhead)
	if maxKFBits < 0 {
		maxKFBits = 0
	}
	if maxKFBits > int64(maxInt()) {
		maxKFBits = int64(maxInt())
	}
	if int64(kfBits) > maxKFBits {
		kfBits = int(maxKFBits)
	}
	if kfBits < 0 {
		kfBits = 0
	}

	return KeyFrameGroupResult{
		FramesToKey:        framesToKey,
		NextKeyFrameForced: framesToKey >= in.KeyFrameFrequency,
		KeyFrameBoost:      boost,
		KeyFrameBits:       kfBits,
		KFGroupBitsLeft:    kfGroupBits - int64(kfBits),
		KFGroupErrorLeft:   kfGroupErr - keyFrameModScore,
		KFZeroMotionPct:    zeroMotionPct,
	}
}

// GetFramesToNextKey ports vp9_get_frames_to_next_key.
func GetFramesToNextKey(in KeyFrameGroupInputs) int {
	if in.StartShowIdx < 0 || in.StartShowIdx >= len(in.Stats) {
		return 0
	}
	keyFreq := in.KeyFrameFrequency
	if keyFreq <= 0 {
		keyFreq = MaxStaticGFGroupLength
	}
	maxFramesToKey := len(in.Stats) - in.StartShowIdx
	if maxFramesToKey > keyFreq {
		maxFramesToKey = keyFreq
	}
	if maxFramesToKey <= 0 {
		return 0
	}
	if !in.AutoKey {
		return maxFramesToKey
	}
	params := in.BoostParams
	if params == (ARFBoostParams{}) {
		params = DefaultARFBoostParams(max(in.MBRows, 1))
	}
	minGF := in.MinGFInterval
	if minGF <= 0 {
		minGF = MinGFInterval
	}
	framesToKey := 1
	recentLoopDecay := [8]float64{1, 1, 1, 1, 1, 1, 1, 1}
	for framesToKey < maxFramesToKey {
		if in.StartShowIdx+framesToKey+1 < len(in.Stats) {
			nextFrame := in.Stats[in.StartShowIdx+framesToKey+1]
			if TestCandidateKeyFrame(in.Stats, in.StartShowIdx+framesToKey) {
				break
			}
			loopDecayRate := GetPredictionDecayRate(nextFrame,
				params.SRDiffFactor, params.SRDefaultDecayLimit,
				params.ZMFactor)
			recentLoopDecay[(framesToKey-1)%len(recentLoopDecay)] = loopDecayRate
			decayAccumulator := 1.0
			for _, rate := range recentLoopDecay {
				decayAccumulator *= rate
			}
			if (framesToKey-1) > minGF && loopDecayRate >= 0.999 &&
				decayAccumulator < 0.9 {
				stillInterval := keyFreq - (framesToKey - 1)
				showIdx := in.StartShowIdx + framesToKey
				if CheckTransitionToStill(in.Stats, showIdx, stillInterval,
					params) {
					break
				}
			}
		}
		framesToKey++
	}
	return framesToKey
}

// ComputeKeyFrameBoost ports the boost-score walk inside find_next_key_frame.
func ComputeKeyFrameBoost(in KeyFrameGroupInputs, framesToKey int, keyRawErr float64) (int, int) {
	if framesToKey <= 1 || in.StartShowIdx+1 >= len(in.Stats) {
		return MinKFTotalBoost, 100
	}
	params := in.BoostParams
	if params == (ARFBoostParams{}) {
		params = DefaultARFBoostParams(max(in.MBRows, 1))
	}

	zeroMotionAccumulator := 1.0
	zeroMotionSum := 0.0
	motionCompensableSum := 0.0
	numFrames := 0
	scanLimit := min(MaxScanFramesForKFBoost, framesToKey-1)
	for i := 0; i < scanLimit; i++ {
		idx := in.StartShowIdx + 1 + i
		if idx < 0 || idx >= len(in.Stats) {
			break
		}
		nextFrame := in.Stats[idx]
		zeroMotionSum += nextFrame.PcntInter - nextFrame.PcntMotion
		motionCompensableSum += 1 - nextFrame.CodedError/
			doubleDivideCheck(nextFrame.IntraError)
		numFrames++
	}

	kfBoostScanFrames := DefaultScanFramesForKFBoost
	if numFrames >= MinScanFramesForKFBoost {
		zeroMotionAvg := zeroMotionSum / float64(numFrames)
		motionCompensableAvg := motionCompensableSum / float64(numFrames)
		kfBoostScanFrames = int(math.Max(64*zeroMotionAvg-16,
			160*motionCompensableAvg-112))
		kfBoostScanFrames = min(max(kfBoostScanFrames,
			MinScanFramesForKFBoost), MaxScanFramesForKFBoost)
	}

	boostScore := 0.0
	srAccumulator := 0.0
	absMvInOutAccumulator := 0.0
	kfErrPerMB := DefaultKFErrPerMB(in.FrameWidth, in.FrameHeight)
	for i := 0; i < framesToKey-1; i++ {
		idx := in.StartShowIdx + 1 + i
		if idx < 0 || idx >= len(in.Stats) {
			break
		}
		nextFrame := in.Stats[idx]
		if i > kfBoostScanFrames && zeroMotionAccumulator < 0.99 {
			break
		}
		if i > 0 {
			zeroMotionAccumulator = math.Min(zeroMotionAccumulator,
				GetZeroMotionFactor(nextFrame, params.ZMFactor))
		} else {
			zeroMotionAccumulator = nextFrame.PcntInter - nextFrame.PcntMotion
		}
		zmFactor := 0.75 + zeroMotionAccumulator/2.0
		if i < 2 {
			srAccumulator = 0
		}
		frameBoost := CalcKFFrameBoost(nextFrame, &srAccumulator,
			kfErrPerMB, max(in.MBRows, 1), in.AvgFrameQIndexInter,
			0, zmFactor, in.CurrentVideoFrame == 0)
		boostScore += frameBoost
		absMvInOutAccumulator += math.Abs(nextFrame.MVInOutCount *
			nextFrame.PcntMotion)
		if frameBoost < 25.0 || absMvInOutAccumulator > KFAbsZoomThresh ||
			srAccumulator > keyRawErr*1.50 {
			break
		}
	}

	zeroMotionPct := int(zeroMotionAccumulator * 100.0)
	boost := 0
	if zeroMotionAccumulator > 0.99 && framesToKey > 8 {
		boost = MaxKFTotalBoost
	} else {
		boost = max(int(boostScore), framesToKey*3)
		boost = max(boost, MinKFTotalBoost)
		boost = min(boost, MaxKFTotalBoost)
	}
	return boost, zeroMotionPct
}

// CalcKFFrameBoost ports libvpx calc_kf_frame_boost.
func CalcKFFrameBoost(frame FirstPassFrameStats, srAccumulator *float64,
	kfErrPerMB float64, mbRows int, avgFrameQIndex int,
	thisFrameMvInOut float64, zmFactor float64, firstFrame bool,
) float64 {
	lq := ConvertQIndexToQ(avgFrameQIndex)
	boostQCorrection := 0.50 + lq*0.015
	if boostQCorrection > 2.0 {
		boostQCorrection = 2.0
	}
	activeArea := CalculateActiveArea(mbRows, frame)
	frameBoost := (kfErrPerMB * activeArea) /
		doubleDivideCheck(frame.CodedError+*srAccumulator)
	*srAccumulator += frame.SRCodedError - frame.CodedError
	if *srAccumulator < 0 {
		*srAccumulator = 0
	}
	if thisFrameMvInOut > 0 {
		frameBoost += frameBoost * (thisFrameMvInOut * 2.0)
	}
	frameBoost = (frameBoost + KFMinFrameBoost) * boostQCorrection
	maxBoost := KFMaxFrameBoost
	if !firstFrame {
		maxBoost = KFMaxFrameBoost
	}
	maxBoost *= zmFactor * boostQCorrection
	if frameBoost < maxBoost {
		return frameBoost
	}
	return maxBoost
}

// DefaultKFErrPerMB mirrors setup_two_pass_state's resolution-dependent
// kf_err_per_mb defaults.
func DefaultKFErrPerMB(width, height int) float64 {
	area := width * height
	if area < 1280*720 {
		return 2000.0
	}
	if area < 1920*1080 {
		return 500.0
	}
	return 250.0
}

func TestCandidateKeyFrame(stats []FirstPassFrameStats, showIdx int) bool {
	if showIdx <= 0 || showIdx+1 >= len(stats) {
		return false
	}
	lastFrame := stats[showIdx-1]
	thisFrame := stats[showIdx]
	nextFrame := stats[showIdx+1]
	pcntIntra := 1.0 - thisFrame.PcntInter
	if DetectFlashFromFrameStats(&thisFrame) ||
		DetectFlashFromFrameStats(&nextFrame) ||
		thisFrame.PcntSecondRef >= 0.2 {
		return false
	}
	if !(thisFrame.PcntInter < 0.05 ||
		slideTransition(thisFrame, lastFrame, nextFrame) ||
		intraStepTransition(thisFrame, lastFrame, nextFrame) ||
		(thisFrame.CodedError > nextFrame.CodedError*1.2 &&
			thisFrame.CodedError > lastFrame.CodedError*1.2 &&
			pcntIntra > 0.25 &&
			pcntIntra+thisFrame.PcntNeutral > 0.5 &&
			thisFrame.IntraError/doubleDivideCheck(thisFrame.CodedError) < 2.5)) {
		return false
	}

	boostScore := 0.0
	oldBoostScore := 0.0
	decayAccumulator := 1.0
	i := 0
	for ; i < 16; i++ {
		idx := showIdx + 1 + i
		if idx >= len(stats) {
			break
		}
		frame := stats[idx]
		nextIIRatio := 12.5 * frame.IntraError /
			doubleDivideCheck(frame.CodedError)
		if nextIIRatio > 128.0 {
			nextIIRatio = 128.0
		}
		if frame.PcntInter > 0.85 {
			decayAccumulator *= frame.PcntInter
		} else {
			decayAccumulator *= (0.85 + frame.PcntInter) / 2.0
		}
		boostScore += decayAccumulator * nextIIRatio
		if frame.PcntInter < 0.05 || nextIIRatio < 1.5 ||
			(frame.PcntInter-frame.PcntNeutral < 0.20 && nextIIRatio < 3.0) ||
			boostScore-oldBoostScore < 3.0 ||
			frame.IntraError < 0.5 {
			break
		}
		oldBoostScore = boostScore
		if idx == len(stats)-1 {
			break
		}
	}
	return boostScore > 30.0 && i > 3
}

func slideTransition(thisFrame, lastFrame, nextFrame FirstPassFrameStats) bool {
	return thisFrame.IntraError < thisFrame.CodedError*1.5 &&
		thisFrame.CodedError > lastFrame.CodedError*5.0 &&
		thisFrame.CodedError > nextFrame.CodedError*5.0
}

func intraStepTransition(thisFrame, lastFrame, nextFrame FirstPassFrameStats) bool {
	lastIIRatio := lastFrame.IntraError / doubleDivideCheck(lastFrame.CodedError)
	thisIIRatio := thisFrame.IntraError / doubleDivideCheck(thisFrame.CodedError)
	nextIIRatio := nextFrame.IntraError / doubleDivideCheck(nextFrame.CodedError)
	lastPcntIntra := 1.0 - lastFrame.PcntInter
	thisPcntIntra := 1.0 - thisFrame.PcntInter
	nextPcntIntra := 1.0 - nextFrame.PcntInter
	modThisIntra := thisPcntIntra + thisFrame.PcntNeutral
	if thisIIRatio < 2.0 && lastIIRatio > 2.25 &&
		nextIIRatio > 2.25 && thisPcntIntra > 3*lastPcntIntra &&
		thisPcntIntra > 3*nextPcntIntra &&
		(thisPcntIntra > 0.075 || modThisIntra > 0.85) {
		return true
	}
	return thisIIRatio < 1.25 && modThisIntra > 0.85 &&
		thisIIRatio < lastIIRatio*0.9 &&
		thisIIRatio < nextIIRatio*0.9
}
