package govpx

import vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"

// vp8ActivityAvgMin is the libvpx activity floor used by SSIM activity
// masking. It keeps very flat macroblocks from producing zero RD multipliers.
const vp8ActivityAvgMin = 64

// prepareTuningActivityMap builds the per-frame macroblock activity map used
// by TuneSSIM. The default TunePSNR path returns without allocating.
func (e *VP8Encoder) prepareTuningActivityMap(src vp8enc.SourceImage, rows int, cols int) error {
	if e.opts.Tuning != TuneSSIM {
		e.activityMapValid = false
		return nil
	}
	required := rows * cols
	if required <= 0 {
		e.activityMapValid = false
		return ErrInvalidConfig
	}
	if cap(e.activityMap) < required {
		e.activityMap = make([]uint32, required)
	} else {
		e.activityMap = e.activityMap[:required]
	}

	var sum uint64
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			activity := ssimActivityMeasure(src, row, col)
			e.activityMap[index] = activity
			sum += uint64(activity)
		}
	}
	avg := max(uint32(sum/uint64(required)), vp8ActivityAvgMin)
	e.activityAvg = avg
	e.activityMapValid = true
	return nil
}

// ssimActivityMeasure returns a libvpx-style luma variance proxy for a
// macroblock, with edge clamping for partial frame-edge macroblocks.
func ssimActivityMeasure(src vp8enc.SourceImage, mbRow int, mbCol int) uint32 {
	baseY := mbRow * 16
	baseX := mbCol * 16
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			diff := int(src.Y[srcY*src.YStride+srcX]) - 128
			sum += diff
			sse += diff * diff
		}
	}
	variance := max(sse-((sum*sum)>>8), 0)
	activity := variance << 4
	if activity < 8<<12 && activity >= 5<<12 {
		activity = 5 << 12
	}
	if activity < vp8ActivityAvgMin {
		activity = vp8ActivityAvgMin
	}
	return uint32(activity)
}

// tunedRDModeScoreWithZbin applies TuneSSIM's per-macroblock RD multiplier
// adjustment. Callers keep the default path outside this helper so PSNR mode
// does not pay the helper call inside per-MB loops.
func (e *VP8Encoder) tunedRDModeScoreWithZbin(qIndex int, zbinOverQuant int, mbRow int, mbCol int, rate int, distortion int) int {
	if !e.activityMapValid {
		return rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, distortion)
	}
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult = e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

// tunedRDMultiplier mirrors libvpx's activity masking multiplier: textured
// blocks tolerate more distortion, while flat blocks receive a lower
// multiplier.
func (e *VP8Encoder) tunedRDMultiplier(rdMult int, mbRow int, mbCol int) int {
	if !e.activityMapValid {
		return rdMult
	}
	activity, ok := e.activityAt(mbRow, mbCol)
	if !ok {
		return rdMult
	}
	avg := max(int64(e.activityAvg), vp8ActivityAvgMin)
	act := int64(activity)
	a := act + 2*avg
	b := 2*act + avg
	if a <= 0 {
		return rdMult
	}
	adjusted := (int64(rdMult)*b + (a >> 1)) / a
	if adjusted < 1 {
		return 1
	}
	if adjusted > int64(maxInt()) {
		return maxInt()
	}
	return int(adjusted)
}

// tunedZbinOverQuant mirrors libvpx's activity-adjusted zero-bin bias. The
// returned value is clamped to the regulator's legal zbin-over-quant range.
func (e *VP8Encoder) tunedZbinOverQuant(zbinOverQuant int, mbRow int, mbCol int) int {
	if !e.activityMapValid {
		return zbinOverQuant
	}
	activity, ok := e.activityAt(mbRow, mbCol)
	if !ok {
		return zbinOverQuant
	}
	avg := max(int64(e.activityAvg), vp8ActivityAvgMin)
	act := int64(activity)
	a := act + 4*avg
	b := 4*act + avg
	if min(a, b) <= 0 {
		return zbinOverQuant
	}
	adjustment := 0
	if act > avg {
		adjustment = int((b+(a>>1))/a) - 1
	} else {
		adjustment = 1 - int((a+(b>>1))/b)
	}
	zbinOverQuant += adjustment
	if zbinOverQuant < 0 {
		return 0
	}
	if zbinOverQuant > libvpxZbinOverQuantMax {
		return libvpxZbinOverQuantMax
	}
	return zbinOverQuant
}

// activityAt returns the cached macroblock activity value for TuneSSIM.
func (e *VP8Encoder) activityAt(mbRow int, mbCol int) (uint32, bool) {
	if mbRow < 0 || mbCol < 0 || !e.activityMapValid {
		return 0, false
	}
	cols := encoderMacroblockCols(e.opts.Width)
	index := mbRow*cols + mbCol
	if uint(index) >= uint(len(e.activityMap)) {
		return 0, false
	}
	return e.activityMap[index], true
}
