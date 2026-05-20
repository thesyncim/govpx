package govpx

type timingState struct {
	timebaseNum   int
	timebaseDen   int
	frameDuration int
	frameRate     float64
}

func outputFrameRate(timing timingState) float64 {
	if timing.frameRate > 0 {
		return timing.frameRate
	}
	if min(min(timing.timebaseNum, timing.timebaseDen), timing.frameDuration) <= 0 {
		return 0
	}
	return float64(timing.timebaseDen) / (float64(timing.timebaseNum) * float64(timing.frameDuration))
}
