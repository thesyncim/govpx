package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// This file contains the libvpx VP9 VAR_BASED_PARTITION threshold
// infrastructure (set_vbp_thresholds / vp9_set_variance_partition_thresholds).
//
// libvpx ref: vp9/encoder/vp9_encodeframe.c:549-676.
//
// The thresholds drive ChoosePartitioning's variance-tree walk. Keep the
// helpers close to the picker so the package owns the codec-specific tuning
// without exposing it through the root facade.

// ContentStateSB mirrors libvpx's CONTENT_STATE_SB enum
// (vp9/encoder/vp9_encoder.h:138-146).
type ContentStateSB int

const (
	ContentStateInvalid            ContentStateSB = 0
	ContentStateLowSadLowSumdiff   ContentStateSB = 1
	ContentStateLowSadHighSumdiff  ContentStateSB = 2
	ContentStateHighSadLowSumdiff  ContentStateSB = 3
	ContentStateHighSadHighSumdiff ContentStateSB = 4
	ContentStateLowVarHighSumdiff  ContentStateSB = 5
	ContentStateVeryHighSad        ContentStateSB = 6
)

// NoiseLevel mirrors libvpx's NOISE_LEVEL enum
// (vp9/encoder/vp9_noise_estimate.h:28):
//
//	typedef enum noise_level { kLowLow, kLow, kMedium, kHigh } NOISE_LEVEL;
//
// The encoder wires this from cpi->noise_estimate for variance partitioning;
// direct helper tests may still pass a fixed level explicitly.
type NoiseLevel int

const (
	NoiseLevelLowLow NoiseLevel = 0
	NoiseLevelLow    NoiseLevel = 1
	NoiseLevelMedium NoiseLevel = 2
	NoiseLevelHigh   NoiseLevel = 3
)

// ChromaCheckArgs describes the libvpx chroma_check inputs after the caller
// has measured the luma and chroma SAD values for the partition prepass
// predictor.
type ChromaCheckArgs struct {
	YSAD  uint64
	UVSAD [2]uint64

	IsKeyFrame          bool
	Speed               int
	ScreenContent       bool
	SceneChangeDetected bool

	BaseQIndex             int
	VariancePartThreshMult int
	Width                  int
	Height                 int
	ContentState           ContentStateSB
	NoiseEstimateEnabled   bool
	NoiseLevel             NoiseLevel
	AvgFrameQIndexInter    int
	Disable16x16PartNonkey bool
}

// ChromaCheck ports vp9_encodeframe.c::chroma_check. It intentionally lives
// with the variance-partition code because libvpx derives color_sensitivity
// from the same prepass predictor and threshold state.
func ChromaCheck(args ChromaCheckArgs) [2]bool {
	var sensitive [2]bool
	if args.IsKeyFrame {
		return sensitive
	}
	if args.Speed > 8 {
		thresholds := setVBPThresholds(args.BaseQIndex,
			args.VariancePartThreshMult, args.Speed, args.Width, args.Height,
			false, args.ContentState, args.NoiseEstimateEnabled,
			args.NoiseLevel, args.AvgFrameQIndexInter,
			args.Disable16x16PartNonkey)
		if args.YSAD > uint64(thresholds[1]) &&
			(!args.NoiseEstimateEnabled || args.NoiseLevel < NoiseLevelMedium) {
			return sensitive
		}
	}

	shift := uint(2)
	if args.ScreenContent && args.SceneChangeDetected {
		shift = 5
	}
	limit := args.YSAD >> shift
	for plane := range sensitive {
		sensitive[plane] = args.UVSAD[plane] > limit
	}
	return sensitive
}

// yDequantAC returns the libvpx cpi->y_dequant[q][1] (luma AC dequant) for
// the given 8-bit profile-0 qindex. libvpx populates the table from
// ac_qlookup at vp9/encoder/vp9_quantize.c vp9_init_quantizer; the [1] slot
// is the AC value. govpx's tables.AcQLookup8 is the byte-identical
// libvpx table (validated by internal/vp9/tables/oracle_test.go).
//
// libvpx ref: vp9/encoder/vp9_quantize.c vp9_init_quantizer.
func yDequantAC(qindex int) int16 {
	if qindex < 0 {
		qindex = 0
	}
	if qindex >= len(tables.AcQLookup8) {
		qindex = len(tables.AcQLookup8) - 1
	}
	return tables.AcQLookup8[qindex]
}

// scalePartThreshSumdiff is the libvpx scale_part_thresh_sumdiff
// verbatim port (vp9/encoder/vp9_encodeframe.c:549-567).
func scalePartThreshSumdiff(thresholdBase int64, speed, width, height int,
	contentState ContentStateSB,
) int64 {
	if speed >= 8 {
		if width <= 640 && height <= 480 {
			return (5 * thresholdBase) >> 2
		}
		if contentState == ContentStateLowSadLowSumdiff ||
			contentState == ContentStateHighSadLowSumdiff ||
			contentState == ContentStateLowVarHighSumdiff {
			return (5 * thresholdBase) >> 2
		}
	} else if speed == 7 {
		if contentState == ContentStateLowSadLowSumdiff ||
			contentState == ContentStateHighSadLowSumdiff ||
			contentState == ContentStateLowVarHighSumdiff {
			return (5 * thresholdBase) >> 2
		}
	}
	return thresholdBase
}

// setVBPThresholds is the libvpx set_vbp_thresholds verbatim port
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
func setVBPThresholds(q, variancePartThreshMult, speed, width, height int,
	isKeyFrame bool, contentState ContentStateSB,
	noiseEstimateEnabled bool, noiseLevel NoiseLevel,
	avgFrameQIndexInter int, disable16x16PartNonkey bool,
) [4]int64 {
	var thresholds [4]int64
	thresholdMultiplier := variancePartThreshMult
	if isKeyFrame {
		thresholdMultiplier = 20
	}
	thresholdBase := int64(thresholdMultiplier) * int64(yDequantAC(q))

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
		case noiseLevel == NoiseLevelHigh:
			thresholdBase = 3 * thresholdBase
		case noiseLevel == NoiseLevelMedium:
			thresholdBase = thresholdBase << 1
		case noiseLevel < NoiseLevelLow:
			thresholdBase = (7 * thresholdBase) >> 3
		}
	}
	// CONFIG_VP9_TEMPORAL_DENOISING is off in the vpx_codec_get_caps default
	// libvpx build, so we always take the scale_part_thresh_sumdiff branch.
	thresholdBase = scalePartThreshSumdiff(thresholdBase, speed, width,
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
		thresholds[2] = vbpThresholdMax
	}
	return thresholds
}

// vbpThresholdMax mirrors libvpx's INT64_MAX sentinel used in
// set_vbp_thresholds when sf->disable_16x16part_nonkey is set.
const vbpThresholdMax = int64(1<<63 - 1)

// setVariancePartitionAuxThresholds is the libvpx
// vp9_set_variance_partition_thresholds verbatim port for the auxiliary
// thresholds (vbp_threshold_sad, vbp_threshold_copy, vbp_bsize_min,
// vbp_threshold_minmax). It lives next to setVBPThresholds because the
// libvpx caller (vp9_set_variance_partition_thresholds) computes both blocks
// together (vp9/encoder/vp9_encodeframe.c:637-676).
//
// The Block8x8 / Block16x16 minimum is returned as an integer so callers can
// downcast without importing common.
type vbpAuxThresholds struct {
	ThresholdSAD    int64
	ThresholdCopy   int64
	ThresholdMinmax int64
	BsizeMin8x8     bool // true => BLOCK_8X8; false => BLOCK_16X16
}

func setVariancePartitionAuxThresholds(q, width, height int,
	isKeyFrame bool, highSourceSAD bool,
) vbpAuxThresholds {
	var aux vbpAuxThresholds
	yDequant := int64(yDequantAC(q))
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
