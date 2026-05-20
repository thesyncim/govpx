package govpx

const (
	libvpxTimestampTicksPerSecond = int64(10000000)
	libvpxInitialFirstTimestamp   = int64(0x7fffffff)
)

type encoderSourceTimestampState struct {
	ratioNum int64
	ratioDen int64

	firstTimestampEver int64
	lastTimestampSeen  int64
	lastEndSeen        int64

	refFrameRate float64
}

func timingFromEncoderOptions(opts EncoderOptions) timingState {
	timing := timingState{frameDuration: 1}
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		timing.timebaseNum = opts.TimebaseNum
		timing.timebaseDen = opts.TimebaseDen
	} else if opts.FPS > 0 {
		timing.timebaseNum = 1
		timing.timebaseDen = opts.FPS
	} else {
		timing.timebaseNum = 1
		timing.timebaseDen = 30
	}
	return timing
}

func newEncoderSourceTimestampState(timing timingState) encoderSourceTimestampState {
	ratioNum := int64(timing.timebaseNum) * libvpxTimestampTicksPerSecond
	ratioDen := int64(timing.timebaseDen)
	if ratioNum <= 0 || ratioDen <= 0 {
		ratioNum = libvpxTimestampTicksPerSecond
		ratioDen = 30
	}
	g := int64GCD(absInt64(ratioNum), absInt64(ratioDen))
	if g > 1 {
		ratioNum /= g
		ratioDen /= g
	}
	return encoderSourceTimestampState{
		ratioNum:           ratioNum,
		ratioDen:           ratioDen,
		firstTimestampEver: libvpxInitialFirstTimestamp,
		refFrameRate:       libvpxClampFrameRate(outputFrameRate(timing)),
	}
}

func (e *VP8Encoder) updateSourceFrameRateFromTimestamp(pts, duration uint64, showFrame bool) {
	if !showFrame || e.sourceTS.ratioNum <= 0 || e.sourceTS.ratioDen <= 0 {
		return
	}
	start := e.sourceTS.timestampTicks(pts)
	end := max(e.sourceTS.timestampTicks(saturatingAddUint64(pts, duration)), start)

	if start < e.sourceTS.firstTimestampEver {
		e.sourceTS.firstTimestampEver = start
		e.sourceTS.lastEndSeen = start
	}

	var thisDuration int64
	step := 0
	if start == e.sourceTS.firstTimestampEver {
		thisDuration = end - start
		step = 1
	} else {
		thisDuration = end - e.sourceTS.lastEndSeen
		lastDuration := e.sourceTS.lastEndSeen - e.sourceTS.lastTimestampSeen
		if thisDuration > maxInt64Value/10 {
			thisDuration = maxInt64Value / 10
		}
		if lastDuration != 0 {
			step = int((thisDuration - lastDuration) * 10 / lastDuration)
		}
	}

	if thisDuration != 0 {
		if step != 0 {
			e.sourceTS.refFrameRate = float64(libvpxTimestampTicksPerSecond) / float64(thisDuration)
		} else {
			interval := float64(end - e.sourceTS.firstTimestampEver)
			if interval > float64(libvpxTimestampTicksPerSecond) {
				interval = float64(libvpxTimestampTicksPerSecond)
			}
			avgDuration := float64(libvpxTimestampTicksPerSecond) / e.sourceTS.refFrameRate
			avgDuration *= interval - avgDuration + float64(thisDuration)
			avgDuration /= interval
			e.sourceTS.refFrameRate = float64(libvpxTimestampTicksPerSecond) / avgDuration
		}
		e.timing = timingState{
			timebaseNum:   1,
			timebaseDen:   int(e.sourceTS.refFrameRate + 0.5),
			frameDuration: 1,
			frameRate:     libvpxClampFrameRate(e.sourceTS.refFrameRate),
		}
		e.rc.refreshFrameRate(e.timing, e.opts.TwoPassMinPct)
	}

	e.sourceTS.lastTimestampSeen = start
	e.sourceTS.lastEndSeen = end
}

func (s encoderSourceTimestampState) timestampTicks(v uint64) int64 {
	if s.ratioDen <= 0 {
		return 0
	}
	if v > uint64(maxInt64Value/s.ratioNum) {
		return maxInt64Value
	}
	return int64(v) * s.ratioNum / s.ratioDen
}

func libvpxClampFrameRate(frameRate float64) float64 {
	if frameRate < 0.1 {
		return 30
	}
	return frameRate
}

func saturatingAddUint64(a, b uint64) uint64 {
	c := a + b
	if c < a {
		return ^uint64(0)
	}
	return c
}

func int64GCD(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

const maxInt64Value = int64(^uint64(0) >> 1)
