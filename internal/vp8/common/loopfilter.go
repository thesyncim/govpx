package common

// Ported from libvpx v1.16.0 vp8/common/loopfilter.h and
// vp8/common/vp8_loopfilter.c.

const (
	MaxLoopFilter        = 63
	PartialFrameFraction = 8
)

type LoopFilterInfo struct {
	MBLimit [MaxLoopFilter + 1]byte
	BLimit  [MaxLoopFilter + 1]byte
	Limit   [MaxLoopFilter + 1]byte

	HEVThresh    [4]byte
	HEVThreshLUT [2][MaxLoopFilter + 1]byte
	ModeLFLUT    [MBModeCount]byte

	Level          [MaxMBSegments][MaxRefFrames][MaxModeLFDeltas]byte
	SharpnessLevel byte
}

type LoopFilterFrameConfig struct {
	SegmentationEnabled bool
	SegmentAbsDelta     bool
	SegmentLF           [MaxMBSegments]int8

	ModeRefDeltaEnabled bool
	RefDeltas           [MaxRefLFDeltas]int8
	ModeDeltas          [MaxModeLFDeltas]int8
}

func InitLoopFilterInfo(lfi *LoopFilterInfo, sharpnessLevel int) {
	UpdateLoopFilterSharpness(lfi, sharpnessLevel)
	initLoopFilterLUT(lfi)
	for i := range 4 {
		lfi.HEVThresh[i] = byte(i)
	}
}

func UpdateLoopFilterSharpness(lfi *LoopFilterInfo, sharpnessLevel int) {
	sharpness := clampInt(sharpnessLevel, 0, 7)
	for level := 0; level <= MaxLoopFilter; level++ {
		blockInsideLimit := level
		if sharpness > 0 {
			blockInsideLimit >>= 1
		}
		if sharpness > 4 {
			blockInsideLimit >>= 1
		}
		if sharpness > 0 && blockInsideLimit > 9-sharpness {
			blockInsideLimit = 9 - sharpness
		}
		if blockInsideLimit < 1 {
			blockInsideLimit = 1
		}

		lfi.Limit[level] = byte(blockInsideLimit)
		lfi.BLimit[level] = byte(2*level + blockInsideLimit)
		lfi.MBLimit[level] = byte(2*(level+2) + blockInsideLimit)
	}
	lfi.SharpnessLevel = byte(sharpness)
}

func InitLoopFilterFrame(lfi *LoopFilterInfo, defaultFilterLevel int, cfg LoopFilterFrameConfig) {
	for seg := range MaxMBSegments {
		levelSeg := defaultFilterLevel
		if cfg.SegmentationEnabled {
			if cfg.SegmentAbsDelta {
				levelSeg = int(cfg.SegmentLF[seg])
			} else {
				levelSeg += int(cfg.SegmentLF[seg])
			}
			levelSeg = clampLoopFilterLevel(levelSeg)
		}

		if !cfg.ModeRefDeltaEnabled {
			level := byte(clampLoopFilterLevel(levelSeg))
			for ref := range int(MaxRefFrames) {
				for mode := range MaxModeLFDeltas {
					lfi.Level[seg][ref][mode] = level
				}
			}
			continue
		}

		for ref := range int(MaxRefFrames) {
			for mode := range MaxModeLFDeltas {
				lfi.Level[seg][ref][mode] = 0
			}
		}

		levelRef := levelSeg + int(cfg.RefDeltas[IntraFrame])
		levelMode := levelRef + int(cfg.ModeDeltas[0])
		lfi.Level[seg][IntraFrame][0] = byte(clampLoopFilterLevel(levelMode))
		lfi.Level[seg][IntraFrame][1] = byte(clampLoopFilterLevel(levelRef))

		for ref := LastFrame; ref < MaxRefFrames; ref++ {
			levelRef = levelSeg + int(cfg.RefDeltas[ref])
			for mode := 1; mode < MaxModeLFDeltas; mode++ {
				levelMode = levelRef + int(cfg.ModeDeltas[mode])
				lfi.Level[seg][ref][mode] = byte(clampLoopFilterLevel(levelMode))
			}
		}
	}
}

func initLoopFilterLUT(lfi *LoopFilterInfo) {
	for level := 0; level <= MaxLoopFilter; level++ {
		if level >= 40 {
			lfi.HEVThreshLUT[KeyFrame][level] = 2
			lfi.HEVThreshLUT[InterFrame][level] = 3
		} else if level >= 20 {
			lfi.HEVThreshLUT[KeyFrame][level] = 1
			lfi.HEVThreshLUT[InterFrame][level] = 2
		} else if level >= 15 {
			lfi.HEVThreshLUT[KeyFrame][level] = 1
			lfi.HEVThreshLUT[InterFrame][level] = 1
		} else {
			lfi.HEVThreshLUT[KeyFrame][level] = 0
			lfi.HEVThreshLUT[InterFrame][level] = 0
		}
	}

	lfi.ModeLFLUT[DCPred] = 1
	lfi.ModeLFLUT[VPred] = 1
	lfi.ModeLFLUT[HPred] = 1
	lfi.ModeLFLUT[TMPred] = 1
	lfi.ModeLFLUT[BPred] = 0
	lfi.ModeLFLUT[ZeroMV] = 1
	lfi.ModeLFLUT[NearestMV] = 2
	lfi.ModeLFLUT[NearMV] = 2
	lfi.ModeLFLUT[NewMV] = 2
	lfi.ModeLFLUT[SplitMV] = 3
}

func clampLoopFilterLevel(level int) int {
	if level < 0 {
		return 0
	}
	if level > MaxLoopFilter {
		return MaxLoopFilter
	}
	return level
}

func clampInt(v int, low int, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
