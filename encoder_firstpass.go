package govpx

import (
	"math"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

const (
	libvpxMinGFInterval = 4
	libvpxIIKFactor1    = 1.40
	libvpxRMax          = 14.0
)

type FirstPassFrameStats struct {
	Frame               uint64
	IntraError          float64
	CodedError          float64
	SSIMWeightedPredErr float64
	PcntInter           float64
	PcntMotion          float64
	PcntSecondRef       float64
	PcntNeutral         float64
	MVr                 float64
	MVrAbs              float64
	MVc                 float64
	MVcAbs              float64
	MVrv                float64
	MVcv                float64
	MVInOutCount        float64
	NewMVCount          float64
	Duration            float64
	Count               float64
}

func (e *VP8Encoder) CollectFirstPassStats(src Image, pts uint64, duration uint64, flags EncodeFlags) (FirstPassFrameStats, error) {
	if e == nil || e.closed {
		return FirstPassFrameStats{}, ErrClosed
	}
	if !src.validForEncode(e.opts.Width, e.opts.Height) {
		return FirstPassFrameStats{}, ErrInvalidConfig
	}
	if err := validateEncodeFlags(flags); err != nil {
		return FirstPassFrameStats{}, err
	}
	_ = pts
	stats := e.computeFirstPassStats(sourceImageFromImage(src), duration)
	copySourceToFrameBuffer(&e.firstPassLastRef, sourceImageFromImage(src))
	if e.firstPassCount == 0 || stats.PcntInter < 0.05 {
		copyFrameImage(&e.firstPassGoldenRef.Img, &e.firstPassLastRef.Img)
		e.firstPassGoldenRef.ExtendBorders()
	}
	e.firstPassCount++
	return stats, nil
}

func (e *VP8Encoder) computeFirstPassStats(src vp8enc.SourceImage, duration uint64) FirstPassFrameStats {
	rows := encoderMacroblockRows(src.Height)
	cols := encoderMacroblockCols(src.Width)
	mbs := rows * cols
	if mbs <= 0 {
		return FirstPassFrameStats{Frame: e.firstPassCount, Count: 1}
	}
	const intraPenalty = 1000
	intraError := int64(0)
	codedError := int64(0)
	interCount := 0
	secondRefCount := 0
	neutralCount := 0
	hasLast := e.firstPassCount > 0 && e.firstPassLastRef.Img.Width == src.Width && e.firstPassLastRef.Img.Height == src.Height
	hasGolden := e.firstPassCount > 1 && e.firstPassGoldenRef.Img.Width == src.Width && e.firstPassGoldenRef.Img.Height == src.Height
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			intra := macroblockMeanLumaSSE(src, row, col) + intraPenalty
			best := intra
			lastErr := maxInt()
			if hasLast {
				lastErr = macroblockLumaSSE(src, &e.firstPassLastRef.Img, row, col, vp8enc.MotionVector{}) + 128
				if lastErr < best {
					best = lastErr
					interCount++
				}
				if ((intra-intraPenalty)*9 <= lastErr*10) && intra < 2*intraPenalty {
					neutralCount++
				}
			}
			if hasGolden {
				goldenErr := macroblockLumaSSE(src, &e.firstPassGoldenRef.Img, row, col, vp8enc.MotionVector{}) + 128
				if goldenErr < lastErr && goldenErr < intra {
					secondRefCount++
				}
			}
			intraError += int64(intra)
			codedError += int64(best)
		}
	}
	stats := FirstPassFrameStats{
		Frame:               e.firstPassCount,
		IntraError:          float64(intraError >> 8),
		CodedError:          float64(codedError >> 8),
		SSIMWeightedPredErr: float64(codedError >> 8),
		PcntInter:           float64(interCount) / float64(mbs),
		PcntSecondRef:       float64(secondRefCount) / float64(mbs),
		PcntNeutral:         float64(neutralCount) / float64(mbs),
		Duration:            float64(duration),
		Count:               1,
	}
	if stats.Duration == 0 {
		stats.Duration = 1
	}
	return stats
}

type twoPassState struct {
	stats       []FirstPassFrameStats
	bitsLeft    int64
	errorLeft   float64
	frameIndex  uint64
	vbrBiasPct  int
	minPct      int
	maxPct      int
	lastKeySeen uint64
}

func (t *twoPassState) configure(stats []FirstPassFrameStats, bitsPerFrame int, biasPct int, minPct int, maxPct int) {
	*t = twoPassState{}
	if len(stats) == 0 || bitsPerFrame <= 0 {
		return
	}
	t.stats = stats
	t.bitsLeft = int64(bitsPerFrame) * int64(len(stats))
	t.vbrBiasPct = biasPct
	if t.vbrBiasPct <= 0 {
		t.vbrBiasPct = 50
	}
	t.minPct = minPct
	if t.minPct <= 0 {
		t.minPct = 50
	}
	t.maxPct = maxPct
	if t.maxPct <= 0 {
		t.maxPct = 200
	}
	for i := range stats {
		t.errorLeft += twoPassModifiedError(stats[i], t.vbrBiasPct)
	}
}

func (t *twoPassState) enabled() bool {
	return len(t.stats) > 0
}

func (t *twoPassState) statsForFrame(frame uint64) FirstPassFrameStats {
	if !t.enabled() || frame >= uint64(len(t.stats)) {
		return FirstPassFrameStats{}
	}
	return t.stats[frame]
}

func (t *twoPassState) shouldKeyFrame(frame uint64, framesSinceKeyFrame int, keyFrameInterval int) bool {
	if !t.enabled() || frame == 0 || frame+1 >= uint64(len(t.stats)) {
		return false
	}
	if framesSinceKeyFrame < libvpxMinGFInterval {
		return false
	}
	if keyFrameInterval > 0 && framesSinceKeyFrame >= keyFrameInterval {
		return true
	}
	return libvpxTestCandidateKeyFrame(t.stats, int(frame))
}

func (t *twoPassState) frameTargetBits(frame uint64, keyFrame bool, defaultTargetBits int) int {
	if !t.enabled() || frame >= uint64(len(t.stats)) || defaultTargetBits <= 0 {
		return 0
	}
	modErr := twoPassModifiedError(t.stats[frame], t.vbrBiasPct)
	if modErr <= 0 || t.errorLeft <= 0 || t.bitsLeft <= 0 {
		return defaultTargetBits
	}
	target := int64(float64(t.bitsLeft) * modErr / t.errorLeft)
	minTarget := int64(defaultTargetBits) * int64(t.minPct) / 100
	maxTarget := int64(defaultTargetBits) * int64(t.maxPct) / 100
	if keyFrame {
		maxTarget *= 4
		target *= 3
	}
	if target < minTarget {
		target = minTarget
	}
	if target > maxTarget {
		target = maxTarget
	}
	if target < 1 {
		target = 1
	}
	if target > int64(maxInt()) {
		return maxInt()
	}
	return int(target)
}

func (t *twoPassState) finishFrame(actualBits int) {
	if !t.enabled() {
		return
	}
	if t.frameIndex < uint64(len(t.stats)) {
		t.errorLeft -= twoPassModifiedError(t.stats[t.frameIndex], t.vbrBiasPct)
		if t.errorLeft < 0 {
			t.errorLeft = 0
		}
	}
	t.bitsLeft -= int64(actualBits)
	if t.bitsLeft < 0 {
		t.bitsLeft = 0
	}
	t.frameIndex++
}

func (t *twoPassState) markKeyFrame(frame uint64) {
	if t.enabled() {
		t.lastKeySeen = frame
	}
}

func twoPassModifiedError(stats FirstPassFrameStats, biasPct int) float64 {
	err := stats.CodedError
	if stats.SSIMWeightedPredErr > 0 {
		err = stats.SSIMWeightedPredErr
	}
	if err < 1 {
		err = 1
	}
	pow := float64(biasPct) / 100.0
	if pow <= 0 {
		return err
	}
	return math.Pow(err, pow)
}

func libvpxTestCandidateKeyFrame(stats []FirstPassFrameStats, idx int) bool {
	if idx <= 0 || idx+1 >= len(stats) {
		return false
	}
	lastFrame := stats[idx-1]
	thisFrame := stats[idx]
	nextFrame := stats[idx+1]
	if thisFrame.PcntSecondRef >= 0.10 || nextFrame.PcntSecondRef >= 0.10 {
		return false
	}
	if !((thisFrame.PcntInter < 0.05) ||
		(((thisFrame.PcntInter - thisFrame.PcntNeutral) < 0.25) &&
			((thisFrame.IntraError / doubleDivideCheck(thisFrame.CodedError)) < 2.5) &&
			((math.Abs(lastFrame.CodedError-thisFrame.CodedError)/doubleDivideCheck(thisFrame.CodedError) > 0.40) ||
				(math.Abs(lastFrame.IntraError-thisFrame.IntraError)/doubleDivideCheck(thisFrame.IntraError) > 0.40) ||
				((nextFrame.IntraError / doubleDivideCheck(nextFrame.CodedError)) > 3.5)))) {
		return false
	}
	boostScore := 0.0
	oldBoostScore := 0.0
	decayAccumulator := 1.0
	i := 0
	for ; i < 16 && idx+1+i < len(stats); i++ {
		localNext := stats[idx+1+i]
		nextIIRatio := libvpxIIKFactor1 * localNext.IntraError / doubleDivideCheck(localNext.CodedError)
		if nextIIRatio > libvpxRMax {
			nextIIRatio = libvpxRMax
		}
		if localNext.PcntInter > 0.85 {
			decayAccumulator *= localNext.PcntInter
		} else {
			decayAccumulator *= (0.85 + localNext.PcntInter) / 2.0
		}
		boostScore += decayAccumulator * nextIIRatio
		if localNext.PcntInter < 0.05 ||
			nextIIRatio < 1.5 ||
			(((localNext.PcntInter - localNext.PcntNeutral) < 0.20) && nextIIRatio < 3.0) ||
			((boostScore - oldBoostScore) < 0.5) ||
			localNext.IntraError < 200 {
			break
		}
		oldBoostScore = boostScore
	}
	return boostScore > 5.0 && i > 3
}

func doubleDivideCheck(v float64) float64 {
	if v < 0 {
		return v - 0.000001
	}
	return v + 0.000001
}
