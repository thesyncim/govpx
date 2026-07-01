package govpx

import "github.com/thesyncim/govpx/internal/vp9/encoder"

const (
	// VP9TargetLevelUnspecified is the Go zero value and maps to libvpx's
	// LEVEL_UNKNOWN value. It applies no fixed target-level constraints.
	VP9TargetLevelUnspecified = 0
	// VP9TargetLevelAuto maps to libvpx's LEVEL_AUTO value.
	VP9TargetLevelAuto = 1
	// VP9TargetLevelUnconstrained maps to libvpx's LEVEL_MAX value.
	VP9TargetLevelUnconstrained = 255
)

// vp9TargetLevelSpec mirrors the libvpx v1.16.0 vp9_level_defs fields that
// config_target_level feeds back into encoder configuration.
type vp9TargetLevelSpec struct {
	level                 int
	maxLumaPictureSize    int
	maxLumaPictureBreadth int
	averageBitrateKbps    int
	maxColTiles           int
	minAltRefDistance     int
}

var vp9TargetLevelSpecs = [...]vp9TargetLevelSpec{
	// level, picture size, breadth, average bitrate, max column tiles,
	// min alt-ref distance.
	{10, 36864, 512, 200, 1, 4},
	{11, 73728, 768, 800, 1, 4},
	{20, 122880, 960, 1800, 1, 4},
	{21, 245760, 1344, 3600, 2, 4},
	{30, 552960, 2048, 7200, 4, 4},
	{31, 983040, 2752, 12000, 4, 4},
	{40, 2228224, 4160, 18000, 4, 4},
	{41, 2228224, 4160, 30000, 4, 5},
	{50, 8912896, 8384, 60000, 8, 6},
	{51, 8912896, 8384, 120000, 8, 10},
	{52, 8912896, 8384, 180000, 8, 10},
	{60, 35651584, 16832, 180000, 16, 10},
	{61, 35651584, 16832, 240000, 16, 10},
	{62, 35651584, 16832, 480000, 16, 10},
}

func validateVP9TargetLevel(level int) error {
	if level == VP9TargetLevelUnspecified || level == VP9TargetLevelAuto ||
		level == VP9TargetLevelUnconstrained {
		return nil
	}
	if _, ok := vp9FixedTargetLevelSpec(level); ok {
		return nil
	}
	return ErrInvalidConfig
}

func vp9FixedTargetLevelSpec(level int) (vp9TargetLevelSpec, bool) {
	for i := range vp9TargetLevelSpecs {
		spec := vp9TargetLevelSpecs[i]
		if spec.level == level {
			return spec, true
		}
	}
	return vp9TargetLevelSpec{}, false
}

func vp9AutoTargetLevelSpec(width, height int) (vp9TargetLevelSpec, bool) {
	if width <= 0 || height <= 0 {
		return vp9TargetLevelSpec{}, false
	}
	picture := uint64(width) * uint64(height)
	breadth := max(width, height)
	for i := range vp9TargetLevelSpecs {
		spec := vp9TargetLevelSpecs[i]
		if picture <= uint64(spec.maxLumaPictureSize) &&
			breadth <= spec.maxLumaPictureBreadth {
			return spec, true
		}
	}
	return vp9TargetLevelSpec{}, false
}

func vp9TargetLevelClampBitrateKbps(level, kbps int) int {
	if kbps <= 0 {
		return kbps
	}
	spec, ok := vp9FixedTargetLevelSpec(level)
	if !ok {
		return kbps
	}
	maxKbps := spec.averageBitrateKbps * 8 / 10
	if maxKbps > 0 && kbps > maxKbps {
		return maxKbps
	}
	return kbps
}

func vp9TargetLevelClampOvershootPct(level, effectiveKbps int, configured uint8) uint8 {
	spec, ok := vp9FixedTargetLevelSpec(level)
	if !ok || effectiveKbps <= 0 {
		return configured
	}
	maxAverageBits := float64(spec.averageBitrateKbps) * 800.0
	targetBits := float64(effectiveKbps) * 1000.0
	maxPct := max(int(((maxAverageBits*1.10)-targetBits)*100.0/targetBits), 0)
	if int(configured) > maxPct {
		return uint8(maxPct)
	}
	return configured
}

func vp9TargetLevelWorstQuality(level, worst int) int {
	if _, ok := vp9FixedTargetLevelSpec(level); !ok {
		return worst
	}
	return encoder.PublicQuantizerToQIndex(encoder.MaxPublicQuantizer)
}

func vp9TargetLevelGFIntervals(level, width, height, minGF, maxGF int) (int, int) {
	spec, ok := vp9FixedTargetLevelSpec(level)
	if !ok && level == VP9TargetLevelAuto {
		spec, ok = vp9AutoTargetLevelSpec(width, height)
		if ok && minGF <= spec.minAltRefDistance {
			minGF = spec.minAltRefDistance
			if maxGF != 0 && maxGF < minGF {
				maxGF = minGF
			}
		}
		return minGF, maxGF
	}
	if !ok {
		return minGF, maxGF
	}
	floor := spec.minAltRefDistance + 1
	if minGF <= spec.minAltRefDistance {
		minGF = floor
		if maxGF != 0 && maxGF < minGF {
			maxGF = minGF
		}
	}
	return minGF, maxGF
}

func vp9TargetLevelClampLog2TileCols(level, width, height, minLog2, log2Cols int) int {
	spec, ok := vp9FixedTargetLevelSpec(level)
	if !ok && level == VP9TargetLevelAuto {
		spec, ok = vp9AutoTargetLevelSpec(width, height)
	}
	if !ok || spec.maxColTiles <= 0 {
		return log2Cols
	}
	for log2Cols > minLog2 && (1<<uint(log2Cols)) > spec.maxColTiles {
		log2Cols--
	}
	return log2Cols
}
