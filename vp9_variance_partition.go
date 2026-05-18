package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// vp9_variance_partition.go contains the verbatim libvpx VP9
// VAR_BASED_PARTITION threshold infrastructure (set_vbp_thresholds /
// vp9_set_variance_partition_thresholds).
//
// libvpx ref: vp9/encoder/vp9_encodeframe.c:549-676.
//
// The thresholds drive the variance-tree picker (choose_partitioning,
// vp9_encodeframe.c:1253-1640). The picker itself is still being ported
// incrementally and is currently absent from govpx — the existing
// pickVP9CBRVariancePartitionBlockSize and pickVP9KeyframeVariancePartitionBlockSize
// adapters compute a per-call threshold derived from these constants instead
// of consuming a precomputed thresholds[4] array. This file lifts those
// constants into a libvpx-shaped function so the eventual choose_partitioning
// port (vp9_encodeframe.c:1253) can reference them directly without
// re-deriving each level. No callers yet — the thresholds plumb-through
// commit is deliberately landed before the picker rewrite so the picker can
// land as a self-contained change.

// vp9ContentStateSB mirrors libvpx's CONTENT_STATE_SB enum
// (vp9/encoder/vp9_encoder.h:138-146).
type vp9ContentStateSB int

const (
	vp9ContentStateInvalid            vp9ContentStateSB = 0
	vp9ContentStateLowSadLowSumdiff   vp9ContentStateSB = 1
	vp9ContentStateLowSadHighSumdiff  vp9ContentStateSB = 2
	vp9ContentStateHighSadLowSumdiff  vp9ContentStateSB = 3
	vp9ContentStateHighSadHighSumdiff vp9ContentStateSB = 4
	vp9ContentStateLowVarHighSumdiff  vp9ContentStateSB = 5
	vp9ContentStateVeryHighSad        vp9ContentStateSB = 6
)

// vp9NoiseLevel mirrors libvpx's NOISE_LEVEL enum
// (vp9/encoder/vp9_noise_estimate.h:28):
//
//	typedef enum noise_level { kLowLow, kLow, kMedium, kHigh } NOISE_LEVEL;
//
// The encoder wires this from cpi->noise_estimate for variance partitioning;
// direct helper tests may still pass a fixed level explicitly.
type vp9NoiseLevel int

const (
	vp9NoiseLevelLowLow vp9NoiseLevel = 0
	vp9NoiseLevelLow    vp9NoiseLevel = 1
	vp9NoiseLevelMedium vp9NoiseLevel = 2
	vp9NoiseLevelHigh   vp9NoiseLevel = 3
)

// vp9YDequant returns the libvpx cpi->y_dequant[q][1] (luma AC dequant) for
// the given 8-bit profile-0 qindex. libvpx populates the table from
// ac_qlookup at vp9/encoder/vp9_quantize.c vp9_init_quantizer; the [1] slot
// is the AC value. govpx's tables.AcQLookup8 is the byte-identical
// libvpx table (validated by internal/vp9/tables/oracle_test.go).
//
// libvpx ref: vp9/encoder/vp9_quantize.c vp9_init_quantizer.
func vp9YDequantAC(qindex int) int16 {
	if qindex < 0 {
		qindex = 0
	}
	if qindex >= len(tables.AcQLookup8) {
		qindex = len(tables.AcQLookup8) - 1
	}
	return tables.AcQLookup8[qindex]
}

// vp9ScalePartThreshSumdiff is the libvpx scale_part_thresh_sumdiff
// verbatim port (vp9/encoder/vp9_encodeframe.c:549-567).
func vp9ScalePartThreshSumdiff(thresholdBase int64, speed, width, height int,
	contentState vp9ContentStateSB,
) int64 {
	if speed >= 8 {
		if width <= 640 && height <= 480 {
			return (5 * thresholdBase) >> 2
		}
		if contentState == vp9ContentStateLowSadLowSumdiff ||
			contentState == vp9ContentStateHighSadLowSumdiff ||
			contentState == vp9ContentStateLowVarHighSumdiff {
			return (5 * thresholdBase) >> 2
		}
	} else if speed == 7 {
		if contentState == vp9ContentStateLowSadLowSumdiff ||
			contentState == vp9ContentStateHighSadLowSumdiff ||
			contentState == vp9ContentStateLowVarHighSumdiff {
			return (5 * thresholdBase) >> 2
		}
	}
	return thresholdBase
}

// vp9SetVBPThresholds is the libvpx set_vbp_thresholds verbatim port
// (vp9/encoder/vp9_encodeframe.c:573-635).
//
// Parameters mirror libvpx's cpi-local references:
//   - q           = cpi->common.base_qindex (or per-segment override)
//   - variancePartThreshMult = cpi->sf.variance_part_thresh_mult
//   - speed       = cpi->oxcf.speed
//   - width/height= cm->width / cm->height
//   - isKeyFrame  = frame_is_intra_only(cm)
//   - contentState= caller-provided CONTENT_STATE_SB
//   - noiseLevel  = vp9_noise_estimate_extract_level(&cpi->noise_estimate)
//   - noiseEstimateEnabled = cpi->noise_estimate.enabled
//   - avgFrameQIndexInter  = cpi->rc.avg_frame_qindex[INTER_FRAME]
//   - disable16x16PartNonkey = cpi->sf.disable_16x16part_nonkey
//
// Returns the libvpx thresholds[4] array.
func vp9SetVBPThresholds(q, variancePartThreshMult, speed, width, height int,
	isKeyFrame bool, contentState vp9ContentStateSB,
	noiseEstimateEnabled bool, noiseLevel vp9NoiseLevel,
	avgFrameQIndexInter int, disable16x16PartNonkey bool,
) [4]int64 {
	var thresholds [4]int64
	thresholdMultiplier := variancePartThreshMult
	if isKeyFrame {
		thresholdMultiplier = 20
	}
	thresholdBase := int64(thresholdMultiplier) * int64(vp9YDequantAC(q))

	if isKeyFrame {
		thresholds[0] = thresholdBase
		thresholds[1] = thresholdBase >> 2
		thresholds[2] = thresholdBase >> 2
		thresholds[3] = thresholdBase << 2
		return thresholds
	}

	// Inter frames.
	// Increase base variance threshold based on estimated noise level.
	if noiseEstimateEnabled && width >= 640 && height >= 480 {
		switch {
		case noiseLevel == vp9NoiseLevelHigh:
			thresholdBase = 3 * thresholdBase
		case noiseLevel == vp9NoiseLevelMedium:
			thresholdBase = thresholdBase << 1
		case noiseLevel < vp9NoiseLevelLow:
			thresholdBase = (7 * thresholdBase) >> 3
		}
	}
	// CONFIG_VP9_TEMPORAL_DENOISING is off in the vpx_codec_get_caps default
	// libvpx build, so we always take the scale_part_thresh_sumdiff branch.
	thresholdBase = vp9ScalePartThreshSumdiff(thresholdBase, speed, width,
		height, contentState)
	thresholds[0] = thresholdBase
	thresholds[2] = thresholdBase << uint(speed)
	if width >= 1280 && height >= 720 && speed < 7 {
		thresholds[2] = thresholds[2] << 1
	}
	if width <= 352 && height <= 288 {
		thresholds[0] = thresholdBase >> 3
		thresholds[1] = thresholdBase >> 1
		thresholds[2] = thresholdBase << 3
		if avgFrameQIndexInter > 220 {
			thresholds[2] = thresholds[2] << 2
		} else if avgFrameQIndexInter > 200 {
			thresholds[2] = thresholds[2] << 1
		}
	} else if width < 1280 && height < 720 {
		thresholds[1] = (5 * thresholdBase) >> 2
	} else if width < 1920 && height < 1080 {
		thresholds[1] = thresholdBase << 1
	} else {
		thresholds[1] = (5 * thresholdBase) >> 1
	}
	if disable16x16PartNonkey {
		thresholds[2] = vp9VBPThresholdMax
	}
	return thresholds
}

// vp9VBPThresholdMax mirrors libvpx's INT64_MAX sentinel used in
// set_vbp_thresholds when sf->disable_16x16part_nonkey is set.
const vp9VBPThresholdMax = int64(1<<63 - 1)

// vp9SetVariancePartitionAuxThresholds is the libvpx
// vp9_set_variance_partition_thresholds verbatim port for the auxiliary
// thresholds (vbp_threshold_sad, vbp_threshold_copy, vbp_bsize_min,
// vbp_threshold_minmax). It lives next to vp9SetVBPThresholds because the
// libvpx caller (vp9_set_variance_partition_thresholds) computes both blocks
// together (vp9/encoder/vp9_encodeframe.c:637-676).
//
// The Block8x8 / Block16x16 minimum is returned as an integer so callers can
// downcast without importing common.
type vp9VBPAuxThresholds struct {
	ThresholdSAD    int64
	ThresholdCopy   int64
	ThresholdMinmax int64
	BsizeMin8x8     bool // true => BLOCK_8X8; false => BLOCK_16X16
}

func vp9SetVariancePartitionAuxThresholds(q, width, height int,
	isKeyFrame bool, highSourceSAD bool,
) vp9VBPAuxThresholds {
	var aux vp9VBPAuxThresholds
	yDequant := int64(vp9YDequantAC(q))
	if isKeyFrame {
		aux.ThresholdSAD = 0
		aux.ThresholdCopy = 0
		aux.BsizeMin8x8 = true // BLOCK_8X8
	} else {
		if width <= 352 && height <= 288 {
			aux.ThresholdSAD = 10
		} else {
			t := yDequant << 1
			aux.ThresholdSAD = max(t, 1000)
		}
		aux.BsizeMin8x8 = false // BLOCK_16X16
		switch {
		case width <= 352 && height <= 288:
			aux.ThresholdCopy = 4000
		case width <= 640 && height <= 360:
			aux.ThresholdCopy = 8000
		default:
			t := yDequant << 3
			aux.ThresholdCopy = max(t, 8000)
		}
		if highSourceSAD {
			aux.ThresholdSAD = 0
			aux.ThresholdCopy = 0
		}
	}
	aux.ThresholdMinmax = int64(15 + (q >> 3))
	return aux
}
