package codecapi

import (
	"runtime"
	"time"
)

var monotonicOrigin = time.Now()

// Nanotime returns the monotonic clock in nanoseconds without unsafe linkname.
func Nanotime() int64 {
	return time.Since(monotonicOrigin).Nanoseconds()
}

// RuntimeProcYield performs a brief non-preemptive CPU spin pause during
// wavefront/worker waits.
//
//go:noinline
func RuntimeProcYield(cycles uint32) {
	for i := range cycles {
		if i == ^uint32(0) {
			runtime.Gosched()
		}
	}
}

type TimingState struct {
	TimebaseNum   int
	TimebaseDen   int
	FrameDuration int
	FrameRate     float64
}

func OutputFrameRate(timing TimingState) float64 {
	if timing.FrameRate > 0 {
		return timing.FrameRate
	}
	if min(min(timing.TimebaseNum, timing.TimebaseDen), timing.FrameDuration) <= 0 {
		return 0
	}
	return float64(timing.TimebaseDen) / (float64(timing.TimebaseNum) * float64(timing.FrameDuration))
}

const (
	LibvpxTimestampTicksPerSecond = int64(10000000)
	LibvpxInitialFirstTimestamp   = int64(0x7fffffff)
	MaxInt64Value                 = int64(^uint64(0) >> 1)
)

type EncoderSourceTimestampState struct {
	RatioNum           int64
	RatioDen           int64
	FirstTimestampEver int64
	LastTimestampSeen  int64
	LastEndSeen        int64
	RefFrameRate       float64
}

func NewEncoderSourceTimestampState(timing TimingState) EncoderSourceTimestampState {
	ratioNum := int64(timing.TimebaseNum) * LibvpxTimestampTicksPerSecond
	ratioDen := int64(timing.TimebaseDen)
	if ratioNum <= 0 || ratioDen <= 0 {
		ratioNum = LibvpxTimestampTicksPerSecond
		ratioDen = 30
	}
	g := Int64GCD(AbsInt64(ratioNum), AbsInt64(ratioDen))
	if g > 1 {
		ratioNum /= g
		ratioDen /= g
	}
	return EncoderSourceTimestampState{
		RatioNum:           ratioNum,
		RatioDen:           ratioDen,
		FirstTimestampEver: LibvpxInitialFirstTimestamp,
		RefFrameRate:       LibvpxClampFrameRate(OutputFrameRate(timing)),
	}
}

func (s EncoderSourceTimestampState) TimestampTicks(v uint64) int64 {
	if s.RatioDen <= 0 {
		return 0
	}
	if v > uint64(MaxInt64Value/s.RatioNum) {
		return MaxInt64Value
	}
	return int64(v) * s.RatioNum / s.RatioDen
}

func LibvpxClampFrameRate(frameRate float64) float64 {
	if frameRate < 0.1 {
		return 30
	}
	if frameRate > 1000 {
		return 1000
	}
	return frameRate
}

func SaturatingAddUint64(a, b uint64) uint64 {
	sum := a + b
	if sum < a {
		return ^uint64(0)
	}
	return sum
}

func AbsInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func Int64GCD(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
