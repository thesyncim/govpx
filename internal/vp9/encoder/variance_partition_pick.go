package encoder

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// This file contains the VP9 choose_partitioning picker body from libvpx
// (vp9/encoder/vp9_encodeframe.c:1253-1763). It consumes the local threshold
// helpers, fills the local variance tree, and writes partition decisions into
// the caller-supplied MI grid the same way libvpx writes into xd->mi[]->sb_type.
//
// libvpx features NOT yet ported and pinned off here (matching the default
// libvpx build configuration that govpx targets):
//
//   - SVC / spatial layering (cpi->use_svc).
//   - Temporal denoiser (CONFIG_VP9_TEMPORAL_DENOISING is off).
//   - Noise estimate (cpi->noise_estimate.enabled defaults to 0).
//   - copy_partitioning / scale_partitioning_svc / update_prev_partition
//     (cpi->sf.copy_partition_flag is off in REALTIME-only builds at speed
//     >= 6; govpx doesn't surface this knob yet).
//   - vp9_int_pro_motion_estimation: govpx callers pass the zero-MV LAST
//     predictor directly, which matches the libvpx output when
//     int_pro_motion returns the dummy_mv = {0,0} fallback (the common
//     case for flat / low-motion content driving the deferred fuzz seeds).
//   - vp9_build_inter_predictors_sb: handled by caller — govpx callers
//     pass the predictor slice (dst, dStride) constructed from the
//     zero-MV LAST/GOLDEN reference plane.
//   - Skin detection (cpi->use_skin_detection).
//   - CYCLIC_REFRESH_AQ segment boost (cyclic_refresh_segment_id_boosted).
//   - VP9_VAR_OFFS is the libvpx 128-fill predictor for keyframes
//     (vp9_encodeframe.c:70); govpx callers pass a per-call view via the
//     varOffs64 slice.
//
// Each gate is documented inline against the libvpx source line.

// varOffs64 is the libvpx VP9_VAR_OFFS constant
// (vp9/encoder/vp9_encodeframe.c:70-76) — a 64-byte vector of 128
// values. Used as the keyframe predictor (`d` in choose_partitioning) so
// fill_variance_*avg measures the source's variance against the
// neutral-gray plane.
var varOffs64 = [64]uint8{
	128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128,
	128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128,
}

// PosShift16x16 mirrors libvpx vp9_pickmode.c:56-58.
var PosShift16x16 = [4][4]int{
	{9, 10, 13, 14},
	{11, 12, 15, 16},
	{17, 18, 21, 22},
	{19, 20, 23, 24},
}

// minMax8x8 is the verbatim port of libvpx's vpx_minmax_8x8_c
// (vpx_dsp/avg.c:389-401). It returns (min, max) of |s[i,j] - d[i,j]|
// over an 8x8 luma block. libvpx initializes min=255, max=0.
func minMax8x8(s []uint8, sp int, d []uint8, dp int) (min, max int) {
	min = 255
	max = 0
	for i := range 8 {
		srcRow := s[i*sp:]
		dstRow := d[i*dp:]
		for j := range 8 {
			diff := int(srcRow[j]) - int(dstRow[j])
			if diff < 0 {
				diff = -diff
			}
			if diff < min {
				min = diff
			}
			if diff > max {
				max = diff
			}
		}
	}
	return min, max
}

// minMax8x8Clamped mirrors vpx_minmax_8x8 on edge-extended YV12 buffers
// while reading from Go's raw visible source and predictor planes.
func minMax8x8Clamped(s []uint8, sp int, d []uint8, dp int,
	x0, y0, pixelsWide, pixelsHigh int,
) (mn, mx int) {
	mn = 255
	mx = 0
	if pixelsWide <= 0 || pixelsHigh <= 0 {
		return mn, mx
	}
	maxX := pixelsWide - 1
	maxY := pixelsHigh - 1
	for r := range 8 {
		y := min(y0+r, maxY)
		srcRow := s[y*sp:]
		dstRow := d[y*dp:]
		for c := range 8 {
			x := min(x0+c, maxX)
			diff := int(srcRow[x]) - int(dstRow[x])
			if diff < 0 {
				diff = -diff
			}
			if diff < mn {
				mn = diff
			}
			if diff > mx {
				mx = diff
			}
		}
	}
	return mn, mx
}

// computeMinmax8x8 is the verbatim port of libvpx's compute_minmax_8x8
// (vp9/encoder/vp9_encodeframe.c:679-712). Walks the 4 8x8 sub-blocks of
// a 16x16 region, computing (max - min) of per-sub-block min/max ranges,
// and returns the difference between the largest and smallest of those.
//
// libvpx initializes minmax_min = 255 and minmax_max = 0; we mirror that.
// Out-of-frame sub-blocks contribute nothing (libvpx's `if (x8_idx <
// pixels_wide && y8_idx < pixels_high)` predicate).
func computeMinmax8x8(s []uint8, sp int, d []uint8, dp int,
	x16Idx, y16Idx, pixelsWide, pixelsHigh int,
) int {
	minmaxMax := 0
	minmaxMin := 255
	for k := range 4 {
		x8Idx := x16Idx + ((k & 1) << 3)
		y8Idx := y16Idx + ((k >> 1) << 3)
		if x8Idx < pixelsWide && y8Idx < pixelsHigh {
			mn, mx := minMax8x8Clamped(s, sp, d, dp, x8Idx, y8Idx,
				pixelsWide, pixelsHigh)
			if (mx - mn) > minmaxMax {
				minmaxMax = mx - mn
			}
			if (mx - mn) < minmaxMin {
				minmaxMin = mx - mn
			}
		}
	}
	return minmaxMax - minmaxMin
}

// ChoosePartitioningArgs bundles the inputs to ChoosePartitioning so
// the Go signature stays manageable. Mirrors libvpx's
// choose_partitioning(cpi, tile, x, mi_row, mi_col) cpi-derived state.
type ChoosePartitioningArgs struct {
	// MI grid the picker writes into. Indexed as MiGrid[row*MiCols+col].
	MiGrid []vp9dec.NeighborMi
	MiRows int
	MiCols int

	// Top-left mi coords of the 64x64 superblock to partition.
	MiRow int
	MiCol int

	// Frame width / height in luma pels.
	FrameWidth  int
	FrameHeight int

	// Source plane (luma) view. PlaneSrc[(y*SrcStride)+x] for x,y inside
	// the frame.
	PlaneSrc    []uint8
	PlaneSrcOff int // byte offset of the SB top-left in PlaneSrc.
	SrcStride   int

	// Predictor plane (luma). For inter frames this is the zero-MV LAST
	// (or GOLDEN) reference plane at the same coordinates as the source;
	// libvpx populates this via vp9_build_inter_predictors_sb after
	// running vp9_int_pro_motion_estimation. For keyframes (and SVC
	// key-fallback) callers should set IsKeyFrame=true and PlaneDst=nil;
	// the picker substitutes varOffs64 as the predictor.
	PlaneDst    []uint8
	PlaneDstOff int
	DstStride   int

	// libvpx flags / cpi-derived state.
	IsKeyFrame             bool
	UseSourceSAD           bool           // cpi->sf.use_source_sad
	NonRdKeyframe          bool           // cpi->sf.nonrd_keyframe
	Speed                  int            // cpi->oxcf.speed
	ContentState           ContentStateSB // x->content_state_sb
	HighSourceSAD          bool           // cpi->rc.high_source_sad
	NoiseEstimateEnabled   bool
	NoiseLevel             NoiseLevel
	VariancePartThreshMult int  // cpi->sf.variance_part_thresh_mult
	Disable16x16PartNonkey bool // cpi->sf.disable_16x16part_nonkey
	AvgFrameQIndexInter    int  // cpi->rc.avg_frame_qindex[INTER_FRAME]
	BaseQIndex             int  // cm->base_qindex
	ScreenContent          bool // cpi->oxcf.content == VP9E_CONTENT_SCREEN
	ZeroTempSADSource      bool // x->zero_temp_sad_source
	ShortCircuitLowTempVar int  // cpi->sf.short_circuit_low_temp_var
	PartitionRefFrame      int8 // ref_frame_partition
	PartitionMV            vp9dec.MV
	VarianceLow            *[25]uint8 // x->variance_low
	VarianceTree           *V64x64
	VarianceTreeLowRes     *[16]V16x16

	// CYCLIC_REFRESH boost predicate. Mirrors libvpx's
	// cyclic_refresh_segment_id_boosted(segment_id). When true, BaseQIndex
	// is already the segment qindex from vp9_get_qindex().
	CyclicRefreshSegmentIdBoosted bool
}

// ChoosePartitioning is the verbatim port of libvpx's choose_partitioning
// (vp9/encoder/vp9_encodeframe.c:1253-1763). It builds the variance tree
// for the 64x64 superblock rooted at (MiRow, MiCol), applies the
// per-resolution thresholds set up by setVBPThresholds / Aux, and
// writes the chosen partition tree into args.MiGrid[].SbType through
// setBlockSize / setVTPartitioning.
//
// Returns the libvpx `int` return: 0 on success (libvpx's normal exit and
// early returns from copy_partitioning fast paths). Currently always 0
// in govpx because the SVC / copy_partition fast paths are not wired.
//
// libvpx reference ranges, all of vp9_encodeframe.c:
//
//	1253-1297  setup, is_key_frame, vt2, force_split[21], vbp_thresholds copy.
//	1298-1336  is_key_frame override (scale/SVC paths), set_offsets,
//	           memset variance_low.
//	1338-1372  use_source_sad fast paths (svc_use_lowres_part,
//	           copy_partitioning). NOT YET PORTED.
//	1374-1389  set_vbp_thresholds dispatch + threshold_4x4avg.
//	1390-1395  pixels_wide / pixels_high clipping, source pointer init.
//	1396-1550  inter-frame predictor build (vp9_setup_pre_planes,
//	           vp9_build_inter_predictors_sb, y_sad copy fast paths) /
//	           keyframe d = VP9_VAR_OFFS.
//	1552-1630  4-level variance tree fill (fill_variance_8x8avg,
//	           fill_variance_tree, get_variance, force_split decisions).
//	1631-1694  noise level, 16x16 -> 32x32 -> 64x64 aggregation.
//	1696-1745  recursive set_vt_partitioning walk that stamps the tree.
//	1747-1762  post-walk hooks (copy_partitioning, svc_use_lowres_part,
//	           short_circuit_low_temp_var, chroma_check, vt2 free).
//
//nolint:gocyclo // verbatim libvpx body
func ChoosePartitioning(a ChoosePartitioningArgs) int {
	// libvpx: vp9_encodeframe.c:1258-1289 — scalar locals.
	vt := a.VarianceTree
	if vt == nil {
		vt = new(V64x64)
	}
	*vt = V64x64{}
	vt2 := a.VarianceTreeLowRes
	if vt2 == nil {
		vt2 = new([16]V16x16)
	}
	*vt2 = [16]V16x16{}
	var forceSplit [21]int
	var maxVar32x32 int
	minVar32x32 := math.MaxInt32
	var avg16x16 [4]int
	var maxvar16x16 [4]int
	var minvar16x16 [4]int
	for i := range 4 {
		minvar16x16[i] = math.MaxInt32
	}
	var threshold4x4avg int64
	noiseLevel := NoiseLevelLow
	contentState := a.ContentState
	computeMinmaxVariance := 1
	pixelsWide := 64
	pixelsHigh := 64

	// libvpx: vp9_encodeframe.c:1281-1282 — copy cpi->vbp_thresholds into
	// the local thresholds[4] array. govpx synthesizes the per-call
	// thresholds by calling setVBPThresholds with the picker inputs,
	// matching libvpx's set_vbp_thresholds invocation at line 1379.
	thresholds := setVBPThresholds(a.BaseQIndex, a.VariancePartThreshMult,
		a.Speed, a.FrameWidth, a.FrameHeight, a.IsKeyFrame, contentState,
		a.NoiseEstimateEnabled, a.NoiseLevel, a.AvgFrameQIndexInter,
		a.Disable16x16PartNonkey)
	aux := setVariancePartitionAuxThresholds(a.BaseQIndex,
		a.FrameWidth, a.FrameHeight, a.IsKeyFrame, a.HighSourceSAD)

	// libvpx: vp9_encodeframe.c:1283-1289 — scene_change_detected /
	// force_64_split.
	sceneChangeDetected := a.HighSourceSAD
	// force_64_split also fires for screen content with motion; govpx
	// single-layer compute_source_sad_onepass is represented by UseSourceSAD
	// plus a valid Last_Source feeding ZeroTempSADSource.
	force64Split := sceneChangeDetected
	if a.ScreenContent && a.UseSourceSAD && !a.ZeroTempSADSource {
		force64Split = true
	}

	// libvpx: vp9_encodeframe.c:1293-1297 — is_key_frame.
	isKeyFrame := a.IsKeyFrame

	// libvpx: vp9_encodeframe.c:1309-1311 — use_4x4_partition / low_res.
	use4x4Partition := isKeyFrame && !a.NonRdKeyframe
	lowRes := a.FrameWidth <= 352 && a.FrameHeight <= 288
	var variance4x4downsample [16]int

	// libvpx: vp9_encodeframe.c:1333-1334 — speed >= 8 disables minmax.
	if a.Speed >= 8 {
		computeMinmaxVariance = 0
	}

	// libvpx: vp9_encodeframe.c:1338-1372 — use_source_sad fast paths.
	// NOT YET PORTED (cpi->sf.copy_partition_flag, svc_use_lowres_part).
	// govpx falls through to the unconditional set_vbp_thresholds branch.

	// libvpx: vp9_encodeframe.c:1374-1380 — set_vbp_thresholds dispatch.
	// The CR_BOOST branch is represented by BaseQIndex already being the
	// segment qindex when CyclicRefreshSegmentIdBoosted is true.

	// libvpx: vp9_encodeframe.c:1383-1385 — screen-content 32x32
	// threshold decrease on scene-change / force_64_split.
	if a.ScreenContent && force64Split {
		thresholds[1] = (3 * thresholds[1]) >> 2
	}

	// libvpx: vp9_encodeframe.c:1388 — threshold_4x4avg.
	if a.Speed < 8 {
		threshold4x4avg = thresholds[1] << 1
	} else {
		threshold4x4avg = vbpThresholdMax
	}

	// libvpx: vp9_encodeframe.c:1390-1391 — pixels_wide/high clipping.
	if a.MiCol*8+64 > a.FrameWidth {
		pixelsWide = max(a.FrameWidth-a.MiCol*8, 0)
	}
	if a.MiRow*8+64 > a.FrameHeight {
		pixelsHigh = max(a.FrameHeight-a.MiRow*8, 0)
	}

	// libvpx: vp9_encodeframe.c:1393-1394 — source pointer.
	src := a.PlaneSrc[a.PlaneSrcOff:]
	sp := a.SrcStride
	// libvpx: vp9_encodeframe.c:1398 — force_split[0].
	if force64Split {
		forceSplit[0] = 1
	}

	// libvpx: vp9_encodeframe.c:1400-1550 — inter / keyframe predictor.
	var dst []uint8
	var dp int
	var ySAD uint64
	ySADValid := false
	if !isKeyFrame {
		// Inter frame: govpx callers pass the zero-MV LAST predictor in
		// args.PlaneDst, or the int-pro predictor when the caller ran
		// vp9_int_pro_motion_estimation. The y_sad short-circuit below
		// inspects the same source-vs-predictor surface libvpx stores in
		// xd->plane[0].dst after vp9_build_inter_predictors_sb.
		if len(a.PlaneDst) == 0 {
			// Without a predictor, fall through to keyframe-style
			// VP9_VAR_OFFS predictor and process the SB.
			dst = varOffs64[:]
			dp = 0
		} else {
			dst = a.PlaneDst[a.PlaneDstOff:]
			dp = a.DstStride
			bsize := choosePartitioningInterSADBSize(a.MiRows, a.MiCols,
				a.MiRow, a.MiCol)
			ySAD, ySADValid = choosePartitioningBlockSAD(src, sp, dst, dp,
				bsize, pixelsWide, pixelsHigh)
		}
	} else {
		// libvpx: vp9_encodeframe.c:1538-1539 — d = VP9_VAR_OFFS, dp = 0.
		dst = varOffs64[:]
		dp = 0
	}
	if ySADValid && !a.CyclicRefreshSegmentIdBoosted &&
		int64(ySAD) < aux.ThresholdSAD &&
		a.MiCol+4 < a.MiCols && a.MiRow+4 < a.MiRows {
		setBlockSize(a.MiGrid, a.MiRows, a.MiCols, a.MiRow, a.MiCol,
			common.Block64x64)
		if a.VarianceLow != nil {
			a.VarianceLow[0] = 1
		}
		return 0
	}

	// libvpx: vp9_encodeframe.c:1552-1553 — vt2 allocation for low_res
	// when threshold_4x4avg < INT64_MAX. govpx uses a fixed-size local
	// array; allocation always succeeds.
	useVT2 := lowRes && threshold4x4avg < vbpThresholdMax

	// libvpx: vp9_encodeframe.c:1556-1630 — 4-level tree fill.
	for i := range 4 {
		x32Idx := (i & 1) << 5
		y32Idx := (i >> 1) << 5
		i2 := i << 2
		forceSplit[i+1] = 0
		avg16x16[i] = 0
		maxvar16x16[i] = 0
		minvar16x16[i] = math.MaxInt32
		for j := range 4 {
			x16Idx := x32Idx + ((j & 1) << 4)
			y16Idx := y32Idx + ((j >> 1) << 4)
			splitIndex := 5 + i2 + j
			vst := &vt.Split[i].Split[j]
			forceSplit[splitIndex] = 0
			variance4x4downsample[i2+j] = 0
			if !isKeyFrame {
				fillVariance8x8Avg(src, sp, dst, dp, x16Idx, y16Idx,
					vst, pixelsWide, pixelsHigh, isKeyFrame)
				fillVarianceTreeV16x16(&vt.Split[i].Split[j])
				getVariance(&vt.Split[i].Split[j].PartVariances.None)
				avg16x16[i] += vt.Split[i].Split[j].PartVariances.None.Variance
				if vt.Split[i].Split[j].PartVariances.None.Variance < minvar16x16[i] {
					minvar16x16[i] = vt.Split[i].Split[j].PartVariances.None.Variance
				}
				if vt.Split[i].Split[j].PartVariances.None.Variance > maxvar16x16[i] {
					maxvar16x16[i] = vt.Split[i].Split[j].PartVariances.None.Variance
				}
				if int64(vt.Split[i].Split[j].PartVariances.None.Variance) > thresholds[2] {
					// 16x16 above threshold for split.
					forceSplit[splitIndex] = 1
					forceSplit[i+1] = 1
					forceSplit[0] = 1
				} else if computeMinmaxVariance != 0 &&
					int64(vt.Split[i].Split[j].PartVariances.None.Variance) > thresholds[1] &&
					!a.CyclicRefreshSegmentIdBoosted {
					minmax := computeMinmax8x8(src, sp, dst, dp,
						x16Idx, y16Idx, pixelsWide, pixelsHigh)
					threshMinmax := int(aux.ThresholdMinmax)
					if a.ContentState == ContentStateVeryHighSad {
						threshMinmax = threshMinmax << 1
					}
					if minmax > threshMinmax {
						forceSplit[splitIndex] = 1
						forceSplit[i+1] = 1
						forceSplit[0] = 1
					}
				}
			}
			if isKeyFrame ||
				(lowRes && int64(vt.Split[i].Split[j].PartVariances.None.Variance) >
					threshold4x4avg) {
				forceSplit[splitIndex] = 0
				variance4x4downsample[i2+j] = 1
				for k := range 4 {
					x8Idx := x16Idx + ((k & 1) << 3)
					y8Idx := y16Idx + ((k >> 1) << 3)
					var vst2 *V8x8
					if isKeyFrame {
						vst2 = &vst.Split[k]
					} else {
						vst2 = &vt2[i2+j].Split[k]
					}
					fillVariance4x4Avg(src, sp, dst, dp, x8Idx, y8Idx,
						vst2, pixelsWide, pixelsHigh, isKeyFrame)
				}
			}
		}
	}
	// libvpx: vp9_encodeframe.c:1631-1632 — noise level from
	// cpi->noise_estimate.
	if a.NoiseEstimateEnabled {
		noiseLevel = a.NoiseLevel
	}
	// libvpx: vp9_encodeframe.c:1634-1676 — 32x32 aggregation.
	avg32x32 := 0
	for i := range 4 {
		i2 := i << 2
		for j := range 4 {
			if variance4x4downsample[i2+j] == 1 {
				var vtemp *V16x16
				if !isKeyFrame {
					vtemp = &vt2[i2+j]
				} else {
					vtemp = &vt.Split[i].Split[j]
				}
				for m := range 4 {
					fillVarianceTreeV8x8(&vtemp.Split[m])
				}
				fillVarianceTreeV16x16(vtemp)
				getVariance(&vtemp.PartVariances.None)
				if int64(vtemp.PartVariances.None.Variance) > thresholds[2] {
					forceSplit[5+i2+j] = 1
					forceSplit[i+1] = 1
					forceSplit[0] = 1
				}
			}
		}
		fillVarianceTreeV32x32(&vt.Split[i])
		if forceSplit[i+1] == 0 {
			getVariance(&vt.Split[i].PartVariances.None)
			var32x32 := vt.Split[i].PartVariances.None.Variance
			if var32x32 > maxVar32x32 {
				maxVar32x32 = var32x32
			}
			if var32x32 < minVar32x32 {
				minVar32x32 = var32x32
			}
			if int64(vt.Split[i].PartVariances.None.Variance) > thresholds[1] ||
				(!isKeyFrame &&
					int64(vt.Split[i].PartVariances.None.Variance) > (thresholds[1]>>1) &&
					int64(vt.Split[i].PartVariances.None.Variance) > int64(avg16x16[i]>>1)) {
				forceSplit[i+1] = 1
				forceSplit[0] = 1
			} else if !isKeyFrame && noiseLevel < NoiseLevelLow &&
				a.FrameHeight <= 360 &&
				(maxvar16x16[i]-minvar16x16[i]) > int(thresholds[1]>>1) &&
				maxvar16x16[i] > int(thresholds[1]) {
				forceSplit[i+1] = 1
				forceSplit[0] = 1
			}
			avg32x32 += var32x32
		}
	}
	// libvpx: vp9_encodeframe.c:1677-1694 — 64x64 aggregation.
	if forceSplit[0] == 0 {
		fillVarianceTreeV64x64(vt)
		getVariance(&vt.PartVariances.None)
		if !isKeyFrame && noiseLevel >= NoiseLevelMedium &&
			vt.PartVariances.None.Variance > (9*avg32x32)>>5 {
			forceSplit[0] = 1
		} else if !isKeyFrame && noiseLevel < NoiseLevelMedium &&
			(maxVar32x32-minVar32x32) > int(3*(thresholds[0]>>3)) &&
			maxVar32x32 > int(thresholds[0]>>1) {
			forceSplit[0] = 1
		}
	}

	// libvpx: vp9_encodeframe.c:1696-1745 — recursive set_vt_partitioning.
	chromaOK := func(_ common.BlockSize) bool { return true }
	if a.MiCol+8 > a.MiCols || a.MiRow+8 > a.MiRows ||
		!setVTPartitioning(a.MiGrid, a.MiRows, a.MiCols, a.MiRow, a.MiCol,
			common.Block64x64, common.Block16x16, thresholds[0],
			forceSplit[0] != 0, isKeyFrame,
			setVTPartitioningArgs{V64: vt}, chromaOK) {
		for i := range 4 {
			x32Idx := (i & 1) << 2
			y32Idx := (i >> 1) << 2
			i2 := i << 2
			if !setVTPartitioning(a.MiGrid, a.MiRows, a.MiCols,
				a.MiRow+y32Idx, a.MiCol+x32Idx,
				common.Block32x32, common.Block16x16, thresholds[1],
				forceSplit[i+1] != 0, isKeyFrame,
				setVTPartitioningArgs{V32: &vt.Split[i]}, chromaOK) {
				for j := range 4 {
					x16Idx := (j & 1) << 1
					y16Idx := (j >> 1) << 1
					var vtemp *V16x16
					if !isKeyFrame && variance4x4downsample[i2+j] == 1 && useVT2 {
						vtemp = &vt2[i2+j]
					} else {
						vtemp = &vt.Split[i].Split[j]
					}
					bsizeMin := common.Block16x16
					if !aux.BsizeMin8x8 {
						bsizeMin = common.Block16x16
					} else {
						bsizeMin = common.Block8x8
					}
					if !setVTPartitioning(a.MiGrid, a.MiRows, a.MiCols,
						a.MiRow+y32Idx+y16Idx, a.MiCol+x32Idx+x16Idx,
						common.Block16x16, bsizeMin, thresholds[2],
						forceSplit[5+i2+j] != 0, isKeyFrame,
						setVTPartitioningArgs{V16: vtemp}, chromaOK) {
						for k := range 4 {
							x8Idx := k & 1
							y8Idx := k >> 1
							if use4x4Partition {
								if !setVTPartitioning(a.MiGrid, a.MiRows, a.MiCols,
									a.MiRow+y32Idx+y16Idx+y8Idx,
									a.MiCol+x32Idx+x16Idx+x8Idx,
									common.Block8x8, common.Block8x8,
									thresholds[3], false, isKeyFrame,
									setVTPartitioningArgs{V8: &vtemp.Split[k]}, chromaOK) {
									setBlockSize(a.MiGrid, a.MiRows, a.MiCols,
										a.MiRow+y32Idx+y16Idx+y8Idx,
										a.MiCol+x32Idx+x16Idx+x8Idx,
										common.Block4x4)
								}
							} else {
								setBlockSize(a.MiGrid, a.MiRows, a.MiCols,
									a.MiRow+y32Idx+y16Idx+y8Idx,
									a.MiCol+x32Idx+x16Idx+x8Idx,
									common.Block8x8)
							}
						}
					}
				}
			}
		}
	}
	// libvpx: vp9_encodeframe.c:1747-1762 — post-walk hooks
	// (copy_partitioning, svc_use_lowres_part, short_circuit_low_temp_var,
	// chroma_check). govpx callers run chroma decisions through the
	// existing pipeline so no in-picker chroma_check is required here.
	if a.ShortCircuitLowTempVar != 0 && a.VarianceLow != nil {
		setLowTempVarFlag(a, vt, thresholds)
	}
	return 0
}

func setLowTempVarFlag(a ChoosePartitioningArgs, vt *V64x64,
	thresholds [4]int64,
) {
	if vt == nil || a.VarianceLow == nil {
		return
	}
	if a.PartitionRefFrame != vp9dec.LastFrame {
		return
	}
	mvThr := int16(4)
	if a.FrameWidth > 640 {
		mvThr = 8
	}
	if a.ShortCircuitLowTempVar != 1 &&
		(a.PartitionMV.Col >= mvThr || a.PartitionMV.Col <= -mvThr ||
			a.PartitionMV.Row >= mvThr || a.PartitionMV.Row <= -mvThr) {
		return
	}
	root := varianceLowBlockSizeAt(a, a.MiRow, a.MiCol)
	switch root {
	case common.Block64x64:
		if vt.PartVariances.None.Variance < int(thresholds[0]>>1) {
			a.VarianceLow[0] = 1
		}
	case common.Block64x32:
		for i := range 2 {
			if vt.PartVariances.Horz[i].Variance < int(thresholds[0]>>2) {
				a.VarianceLow[i+1] = 1
			}
		}
	case common.Block32x64:
		for i := range 2 {
			if vt.PartVariances.Vert[i].Variance < int(thresholds[0]>>2) {
				a.VarianceLow[i+3] = 1
			}
		}
	default:
		idx := [4][2]int{{0, 0}, {0, 4}, {4, 0}, {4, 4}}
		for i := range 4 {
			miRow := a.MiRow + idx[i][0]
			miCol := a.MiCol + idx[i][1]
			if a.MiRows <= miRow || a.MiCols <= miCol {
				continue
			}
			sbType := varianceLowBlockSizeAt(a, miRow, miCol)
			if sbType == common.Block32x32 {
				threshold32x32 := thresholds[1] >> 1
				if a.ShortCircuitLowTempVar == 1 ||
					a.ShortCircuitLowTempVar == 3 {
					threshold32x32 = (5 * thresholds[1]) >> 3
				}
				if vt.Split[i].PartVariances.None.Variance < int(threshold32x32) {
					a.VarianceLow[i+5] = 1
				}
			} else if a.ShortCircuitLowTempVar >= 2 &&
				(sbType == common.Block16x16 ||
					sbType == common.Block32x16 ||
					sbType == common.Block16x32) {
				for j := range 4 {
					if vt.Split[i].Split[j].PartVariances.None.Variance <
						int(thresholds[2]>>8) {
						a.VarianceLow[(i<<2)+j+9] = 1
					}
				}
			}
		}
	}
}

func varianceLowBlockSizeAt(a ChoosePartitioningArgs, miRow, miCol int) common.BlockSize {
	if miRow < 0 || miCol < 0 || miRow >= a.MiRows || miCol >= a.MiCols ||
		a.MiCols <= 0 {
		return common.BlockInvalid
	}
	off := miRow*a.MiCols + miCol
	if off < 0 || off >= len(a.MiGrid) {
		return common.BlockInvalid
	}
	return a.MiGrid[off].SbType
}

func choosePartitioningInterSADBSize(miRows, miCols, miRow, miCol int) common.BlockSize {
	bsize := int(common.Block32x32)
	if miCol+4 < miCols {
		bsize += 2
	}
	if miRow+4 < miRows {
		bsize++
	}
	if bsize < int(common.Block32x32) || bsize > int(common.Block64x64) {
		return common.BlockInvalid
	}
	return common.BlockSize(bsize)
}

func choosePartitioningBlockSAD(src []uint8, sp int, dst []uint8, dp int,
	bsize common.BlockSize, pixelsWide, pixelsHigh int,
) (uint64, bool) {
	if bsize >= common.BlockSizes || sp <= 0 || dp < 0 ||
		len(src) == 0 || len(dst) == 0 ||
		pixelsWide <= 0 || pixelsHigh <= 0 {
		return 0, false
	}
	bw := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	bh := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	if bw <= 0 || bh <= 0 {
		return 0, false
	}
	if pixelsWide >= bw && pixelsHigh >= bh &&
		dp > 0 && len(src) >= (bh-1)*sp+bw && len(dst) >= (bh-1)*dp+bw {
		return BlockSADOffsets(src, 0, sp, dst, 0, dp, bw, bh, ^uint64(0)), true
	}
	if len(src) < (pixelsHigh-1)*sp+pixelsWide || dp <= 0 ||
		len(dst) < (pixelsHigh-1)*dp+pixelsWide {
		return 0, false
	}
	maxX := pixelsWide - 1
	maxY := pixelsHigh - 1
	var sad uint64
	for y := range bh {
		yy := min(y, maxY)
		srcRow := src[yy*sp:]
		dstRow := dst[yy*dp:]
		for x := range bw {
			xx := min(x, maxX)
			diff := int(srcRow[xx]) - int(dstRow[xx])
			if diff < 0 {
				diff = -diff
			}
			sad += uint64(diff)
		}
	}
	return sad, true
}
