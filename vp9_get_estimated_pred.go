package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// vp9_get_estimated_pred.go ports the get_estimated_pred orchestrator
// from libvpx v1.16.0 vp9/encoder/vp9_encodeframe.c:5103-5198 — the
// ML_BASED_PARTITION entry point that produces the per-SB 64x64 luma
// "estimated prediction" buffer consumed by nonrd_pick_partition.
//
// At this revision the helper is substrate-only; no live caller. Phase
// C wires it into the recursive partition picker.
//
// Verbatim ports cited inline:
//   - get_estimated_pred             (vp9_encodeframe.c:5103-5198)
//   - vp9_build_inter_predictors_sb  (vp9_reconinter.c:253-258 +
//                                     build_inter_predictors_for_planes
//                                     vp9_reconinter.c:210-237 +
//                                     build_inter_predictors
//                                     vp9_reconinter.c:127-208).
// Simplifications relative to libvpx — each documented and bounded:
//   1. Luma-only. get_estimated_pred only consumes x->est_pred (the
//      luma plane buffer); the chroma writes from build_inter_predictors_sb
//      land in xd->plane[1..2].dst and are never read again. Phase C
//      consumers do not need them — see libvpx vp9_encodeframe.c:5316
//      where only x->est_pred feeds nonrd_pick_partition's NN input.
//   2. No reference scaling. get_estimated_pred only fires when
//      cpi->sf.partition_search_type == ML_BASED_PARTITION which the
//      libvpx speed-features path gates to dynamic-resolution-off
//      configurations (vp9_speed_features.c:751-768 + 825-826) — there
//      are no scaled refs at the SB-pred level.
//   3. No high bit depth. Phase B targets the 8-bit path only; the
//      highbd memset variants for the keyframe fallback live behind
//      CONFIG_VP9_HIGHBITDEPTH in libvpx (vp9_encodeframe.c:5188-5197)
//      and are not part of govpx's 8-bit-only encoder.
//   4. No SVC scaled_ref_frame swap. ML_BASED_PARTITION runs after the
//      svc.use_gf_temporal_ref_current_layer gate has already routed
//      the right buffers into xd->plane[0].pre[0]; the substrate is
//      caller-supplied unscaled refs.

// vp9GetEstimatedPredKeyFrame ports the keyframe branch of
// get_estimated_pred (libvpx vp9_encodeframe.c:5198 — the
// memset(x->est_pred, 128, 64*64) for the 8-bit path). Writes 4096
// copies of 128 into estPred.
func vp9GetEstimatedPredKeyFrame(estPred []uint8) {
	for i := range 64 * 64 {
		estPred[i] = 128
	}
}

// vp9GetEstimatedPredInterInput is the per-SB substrate the inter
// branch of get_estimated_pred consumes. The caller is responsible for
// resolving:
//   - Bsize, the BLOCK_*x* sub-window the int_pro motion search runs
//     against (libvpx vp9_encodeframe.c:5113-5114). The libvpx formula
//     is:
//     bsize = BLOCK_32X32 + (mi_col + 4 < mi_cols) * 2
//   - (mi_row + 4 < mi_rows);
//     i.e. BLOCK_32X32 if the SB is in the bottom-right edge of the
//     frame (neither right neighbour SB nor bottom neighbour SB is
//     fully inside the frame), BLOCK_32X64 / BLOCK_64X32 if only one
//     axis fits, BLOCK_64X64 if both fit.
//   - The (src, refs) plane buffers, indexed at the SB origin.
//   - The cpi->oxcf.speed value (the GOLDEN-ref probe is gated to
//     speed < 8 in libvpx).
//   - The (haveGolden, haveAlt) flags + the ref-frame-flags bits.
//   - cpi->rc.is_src_frame_alt_ref + cpi->oxcf.lag_in_frames > 0 +
//     cpi->oxcf.rc_mode == VPX_VBR — the joint gate that hijacks LAST
//     and re-points it at ALTREF.
//   - sf.short_circuit_low_temp_var — the y_sad_g threshold tightener.
//
// On return, ChosenRef carries the libvpx ref slot (LAST=1, GOLDEN=2,
// ALTREF=3 in vp9_blockd.h's enum) the caller should record on
// mi->ref_frame[0].
type vp9GetEstimatedPredInterInput struct {
	Bsize common.BlockSize

	// SB-aligned source + reference plane windows. All three may
	// alias the same backing slice; the offsets distinguish them.
	Src       []uint8
	SrcOff    int
	SrcStride int

	LastRef       []uint8
	LastRefOff    int
	LastRefStride int

	HaveGolden      bool
	GoldenRef       []uint8
	GoldenRefOff    int
	GoldenRefStride int

	HaveAltRef   bool
	AltRef       []uint8
	AltRefOff    int
	AltRefStride int

	// Per-frame encoder state.
	Speed                   int
	OnePassSvcSpatialLayer  bool
	UseGfTemporalRefCurrent bool
	IsSrcFrameAltRef        bool
	LagInFrames             int
	RcModeIsVBR             bool
	RefFlagsGoldOn          bool
	ShortCircuitLowTempVar  bool

	// Full-pel UMV-window limits the int-pro motion search clamps
	// against (mirrored on libvpx's MACROBLOCK.mv_limits).
	MvLimits vp9MvLimits
}

// vp9RefFrameSlot mirrors libvpx's ref-frame enum
// (vp9/common/vp9_blockd.h MV_REFERENCE_FRAME). 0 is INTRA, 1 LAST,
// 2 GOLDEN, 3 ALTREF.
type vp9RefFrameSlot uint8

const (
	vp9RefIntra  vp9RefFrameSlot = 0
	vp9RefLast   vp9RefFrameSlot = 1
	vp9RefGolden vp9RefFrameSlot = 2
	vp9RefAlt    vp9RefFrameSlot = 3
)

// vp9GetEstimatedPredInter ports the non-keyframe body of
// get_estimated_pred (libvpx vp9_encodeframe.c:5111-5187).
//
// Returns the chosen ref slot + the int-pro MV (in 1/8-pel units).
// The caller writes the 64x64 luma prediction into estPred via
// vp9BuildEstimatedPredLuma64x64 — split out so each piece is unit-
// testable.
func vp9GetEstimatedPredInter(in *vp9GetEstimatedPredInterInput) (
	chosenRef vp9RefFrameSlot, intProMV vp9MV,
) {
	bsize := in.Bsize
	sdf := vp9SADForBsize(bsize)

	// libvpx (vp9_encodeframe.c:5121):
	//   if (!(is_one_pass_svc(cpi) && cpi->svc.spatial_layer_id) ||
	//       cpi->svc.use_gf_temporal_ref_current_layer) {
	//     yv12_g = get_ref_frame_buffer(cpi, GOLDEN_FRAME);
	//   }
	// We model the gate via the input flags. The "spatial_layer_id != 0"
	// path is guarded by OnePassSvcSpatialLayer; when set, GOLDEN is
	// suppressed unless UseGfTemporalRefCurrent overrides it.
	goldenVisible := !in.OnePassSvcSpatialLayer || in.UseGfTemporalRefCurrent

	// libvpx (vp9_encodeframe.c:5128):
	//   if (cpi->oxcf.speed < 8 && yv12_g && yv12_g != yv12 &&
	//       (cpi->ref_frame_flags & VP9_GOLD_FLAG)) { ... probe golden ... }
	var ySadGolden uint32 = ^uint32(0) // libvpx initialises to UINT_MAX.
	if in.Speed < 8 && goldenVisible && in.HaveGolden && in.RefFlagsGoldOn {
		ySadGolden = sdf(
			in.Src, in.SrcOff, in.SrcStride,
			in.GoldenRef, in.GoldenRefOff, in.GoldenRefStride,
		)
	}

	// libvpx (vp9_encodeframe.c:5142):
	//   if (cpi->oxcf.lag_in_frames > 0 && cpi->oxcf.rc_mode == VPX_VBR &&
	//       cpi->rc.is_src_frame_alt_ref) {
	//     // Hijack LAST to point at ALTREF instead.
	//     yv12 = get_ref_frame_buffer(cpi, ALTREF_FRAME);
	//     mi->ref_frame[0] = ALTREF_FRAME;
	//     y_sad_g = UINT_MAX;
	//   } else {
	//     // Normal LAST.
	//     mi->ref_frame[0] = LAST_FRAME;
	//   }
	useAltAsLast := in.LagInFrames > 0 && in.RcModeIsVBR && in.IsSrcFrameAltRef && in.HaveAltRef

	var motionRef []uint8
	var motionRefOff, motionRefStride int
	if useAltAsLast {
		motionRef = in.AltRef
		motionRefOff = in.AltRefOff
		motionRefStride = in.AltRefStride
		ySadGolden = ^uint32(0)
		chosenRef = vp9RefAlt
	} else {
		motionRef = in.LastRef
		motionRefOff = in.LastRefOff
		motionRefStride = in.LastRefStride
		chosenRef = vp9RefLast
	}

	// libvpx (vp9_encodeframe.c:5159-5167):
	//   const MV dummy_mv = { 0, 0 };
	//   y_sad = vp9_int_pro_motion_estimation(cpi, x, bsize, mi_row, mi_col,
	//                                         &dummy_mv);
	estIn := &vp9IntProEstimateInput{
		Bsize:     bsize,
		Src:       in.Src,
		SrcOff:    in.SrcOff,
		SrcStride: in.SrcStride,
		Ref:       motionRef,
		RefOff:    motionRefOff,
		RefStride: motionRefStride,
		RefMV:     vp9MV{Row: 0, Col: 0},
		MvLimits:  in.MvLimits,
	}
	ySad, mv := vp9IntProEstimate(estIn)
	intProMV = mv

	// libvpx (vp9_encodeframe.c:5170-5179):
	//   y_sad_thr = cpi->sf.short_circuit_low_temp_var ? (y_sad * 7) >> 3
	//                                                  : y_sad;
	//   if (y_sad_g < y_sad_thr) { ...pick GOLDEN... }
	//   else { x->pred_mv[LAST_FRAME] = mi->mv[0].as_mv; }
	ySadThr := ySad
	if in.ShortCircuitLowTempVar {
		ySadThr = (ySad * 7) >> 3
	}
	if ySadGolden < ySadThr {
		chosenRef = vp9RefGolden
		// libvpx zeroes mi->mv[0].as_int on the golden swap.
		intProMV = vp9MV{Row: 0, Col: 0}
	}

	return chosenRef, intProMV
}

// vp9GetEstimatedPredSubBsize ports the per-SB sub-bsize formula
// from libvpx vp9_encodeframe.c:5113-5114:
//
//	bsize = BLOCK_32X32 + (mi_col + 4 < mi_cols) * 2
//	                    + (mi_row + 4 < mi_rows);
//
// Returns one of BLOCK_32X32, BLOCK_32X64, BLOCK_64X32, BLOCK_64X64
// depending on whether the SB's right and bottom neighbours fit
// fully inside the frame.
func vp9GetEstimatedPredSubBsize(miRow, miCol, miRows, miCols int) common.BlockSize {
	// vp9_enums.h: BLOCK_32X32 = 7, BLOCK_32X64 = 8, BLOCK_64X32 = 9,
	// BLOCK_64X64 = 10. The arithmetic produces:
	//   neither fits  -> 7  (BLOCK_32X32)
	//   col only fits -> 9  (BLOCK_64X32) — (mi_col+4 < cols)*2 = 2.
	//   row only fits -> 8  (BLOCK_32X64) — (mi_row+4 < rows)   = 1.
	//   both fit      -> 10 (BLOCK_64X64).
	bs := int(common.Block32x32)
	if miCol+4 < miCols {
		bs += 2
	}
	if miRow+4 < miRows {
		bs += 1
	}
	return common.BlockSize(bs)
}

// vp9BuildEstimatedPredLuma64x64 ports the simplified
// vp9_build_inter_predictors_sb path that get_estimated_pred uses for
// the luma plane (libvpx vp9_reconinter.c:253-258 + 210-237 + 127-208).
//
// Scope of this implementation:
//   - Plane 0 only (luma). The chroma writes are skipped — get_estimated_pred
//     never reads them back.
//   - sb_type = BLOCK_64X64, so the sub-8x8 split path is dead.
//   - No reference scaling (xs = ys = 16, the no-scale path on
//     vp9_reconinter.c:184-187).
//   - Interp filter = BILINEAR — set explicitly on libvpx
//     vp9_encodeframe.c:5158 (mi->interp_filter = BILINEAR).
//   - Single ref (no compound) — get_estimated_pred sets
//     mi->ref_frame[1] = NO_REF_FRAME (vp9_encodeframe.c:5156).
//
// The MV is in 1/8-pel units. libvpx's
// clamp_mv_to_umv_border_sb converts luma MVs to Q4 by doubling them
// when subsampling is zero; the convolve path then derives subpel bits
// from that Q4 MV.
//
// estPred must have at least 64*64 entries; the result is written
// contiguously, row-major, with stride 64 (libvpx pins
// xd->plane[0].dst.stride = 64 on vp9_encodeframe.c:5185).
//
// MV-vs-buffer guard: the caller is responsible for ensuring the
// (refOff + mv-derived offset) stays inside the reference buffer.
// libvpx's clamp_mv_to_umv_border_sb (vp9_reconinter.c:93-109) does
// this with respect to xd->mb_to_*_edge; the substrate test below
// pins the no-MV identity case which is always in-bounds.
func vp9BuildEstimatedPredLuma64x64(
	estPred []uint8,
	ref []uint8, refOff, refStride int,
	mv vp9MV,
) {
	// scaled_mv.row / scaled_mv.col == mv.row / mv.col in the
	// no-scale luma path after clamp_mv_to_umv_border_sb maps 1/8-pel
	// MV units into Q4 (1/16-pel) units.
	mvQ4Row := int(mv.Row) << 1
	mvQ4Col := int(mv.Col) << 1
	subpelX := mvQ4Col & tables.SubpelMask
	subpelY := mvQ4Row & tables.SubpelMask
	pre := refOff + (mvQ4Row>>tables.SubpelBits)*refStride + (mvQ4Col >> tables.SubpelBits)

	// libvpx vp9_filter_kernels[BILINEAR] = &bilinear_filters
	// (vp9_filter.c:79-84). InterPredictor dispatches to
	// VpxConvolve8 / 8Horiz / 8Vert / Copy based on the subpel bits.
	decoder.InterPredictor(
		ref, refStride,
		estPred, 64,
		subpelX, subpelY,
		&tables.BilinearFilters,
		tables.SubpelShifts, tables.SubpelShifts,
		64, 64,
		0, // ref slot 0 (no compound).
		pre,
	)
}

// vp9GetEstimatedPred is the full orchestrator. Combines the keyframe
// memset, the inter int-pro motion search + ref selection, and the
// 64x64 luma inter-predictor convolve.
//
// is_key_frame is the libvpx frame_is_intra_only(cm) condition
// (vp9_encodeframe.c:5106). estPred is the per-SB scratch the caller
// allocates (libvpx x->est_pred, 64*64 bytes).
func vp9GetEstimatedPred(
	isKeyFrame bool, in *vp9GetEstimatedPredInterInput, estPred []uint8,
) (chosenRef vp9RefFrameSlot, mv vp9MV) {
	if isKeyFrame {
		vp9GetEstimatedPredKeyFrame(estPred)
		return vp9RefIntra, vp9MV{Row: 0, Col: 0}
	}

	chosenRef, mv = vp9GetEstimatedPredInter(in)

	var ref []uint8
	var refOff, refStride int
	switch chosenRef {
	case vp9RefAlt:
		ref = in.AltRef
		refOff = in.AltRefOff
		refStride = in.AltRefStride
	case vp9RefGolden:
		ref = in.GoldenRef
		refOff = in.GoldenRefOff
		refStride = in.GoldenRefStride
	default:
		// LAST is the default (vp9_encodeframe.c:5155).
		ref = in.LastRef
		refOff = in.LastRefOff
		refStride = in.LastRefStride
	}

	vp9BuildEstimatedPredLuma64x64(estPred, ref, refOff, refStride, mv)
	return chosenRef, mv
}

// vp9 dsp shape sanity, anchored to verify the file imports the dsp
// package even when the keyframe-only call path is taken at runtime.
// (vector_var is exercised through vp9IntProEstimate indirectly; the
// reference here keeps `dsp` honest as a dependency so the package
// compiles even with the keyframe-only branch.)
var _ = dsp.VpxVectorVar
