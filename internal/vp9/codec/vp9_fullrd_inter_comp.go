package codec

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_comp.go ports libvpx's COMPOUND-prediction inter RD — the
// is_comp_pred branch of handle_inter_mode (vp9/encoder/vp9_rdopt.c:2811) and
// its compound-specific pieces — as a standalone, verified producer returning
// the best {ref_pair, mv0, mv1, rate, dist, rd} for a two-reference inter
// candidate.
//
// ============================ USAGE FINDING ===============================
// The three full-RD long-fixture seeds {0,2,0,0,2} / {0,1,1,0,1} /
// {1,1,1,1,0} DO NOT exercise compound prediction at all. Confirmed via a
// private $TMPDIR vpxenc-vp9 build with fprintf probes (reverted; the shared
// oracle binaries' md5 stayed 758eb784… / 16ddb772…):
//
//	FIXTURE {0,1,1,0,1}: ref_mode=SINGLE_REFERENCE allow_comp=0  HANDLE_COMP=0
//	FIXTURE {1,1,1,1,0}: ref_mode=SINGLE_REFERENCE allow_comp=0  HANDLE_COMP=0
//	FIXTURE {0,2,0,0,2}: ref_mode=SINGLE_REFERENCE allow_comp=0  HANDLE_COMP=0
//
// For every inter frame of all three seeds cm->ref_frame_sign_bias is 0/0/0/0
// (no ALTREF with opposite sign bias: --auto-alt-ref=0 --lag-in-frames=0), so
// vp9_compound_reference_allowed (vp9/common/vp9_pred_common.c:16) returns 0,
// vp9_encode_frame sets cpi->allow_comp_inter_inter=0
// (vp9/encoder/vp9_encodeframe.c:5842) and cm->reference_mode=SINGLE_REFERENCE
// (vp9_encodeframe.c:5866). Compound candidates ARE enumerated in the
// vp9_rd_pick_inter_mode_sb mode loop, but every one continues at
// vp9_rdopt.c:3757 (`if (!cpi->allow_comp_inter_inter) continue;`) BEFORE
// handle_inter_mode is ever reached — so the compound RD path is dead for
// these seeds. The single-reference full-RD inter port
// (vp9_fullrd_inter_thisrd.go etc.) is what those seeds need.
//
// To exercise — and pin — compound RD this file uses a DERIVED 2-ref fixture
// that DOES reach the compound handle_inter_mode: the same 64x64 panning clip
// encoded good-quality with a real future ALTREF:
//
//	vpxenc --codec=vp9 --good --lag-in-frames=16 --auto-alt-ref=1
//	       --end-usage=vbr --target-bitrate=700 --cpu-used=0 (or 2)
//	       --kf-min-dist=0 --kf-max-dist=30 --aq-mode=0 --timebase=1/30 ...
//
// Once the ARF arrives, cm->ref_frame_sign_bias=0/0/0/1 (ALTREF=1),
// allow_comp_inter_inter=1, comp_fixed_ref=ALTREF, comp_var_ref=LAST/GOLDEN,
// reference_mode=REFERENCE_MODE_SELECT, and handle_inter_mode runs the
// compound branch thousands of times (HANDLE_COMP=3640 at cpu0). The
// libvpx-ground-truth pin below is captured from this fixture.
//
// ========================= COMPOUND RD ALGORITHM ==========================
// handle_inter_mode compound branch, verbatim:
//
//   - is_comp_pred = has_second_ref(mi)                       (vp9_rdopt.c:2824)
//   - NEWMV: joint_motion_search (vp9_rdopt.c:2888-2916):
//       num_iters = get_joint_search_iters(sf_level, bsize)   (:2892, :1896)
//       frame_mv[refs[0/1]] = single_newmv[refs[0/1]]         (:2896-2897)
//       if num_iters: joint_motion_search refines both MVs    (:2903, :1907)
//       else rate_mv = vp9_mv_bit_cost(mv0,refmv0)
//                    + vp9_mv_bit_cost(mv1,refmv1)            (:2909-2914)
//       *rate2 += rate_mv                                      (:2916)
//   - cost_mv_ref(this_mode, mode_context[refs[0]])           (:2970-2977)
//   - rs (switchable interp filter rate) added if SWITCHABLE  (:3164)
//   - predictor: the two-reference ROUND_POWER_OF_TWO average is built by
//     the SAME vp9_build_inter_predictors path the single-ref RD uses; for a
//     mi with ref_frame[1] > INTRA_FRAME refIdx==1 averages into the dst
//     (govpx reconstructVP9InterPredictBlock, avg:=refIdx==1).
//   - super_block_yrd / super_block_uvrd on the comp residual (:3176, :3192)
//   - skip-vs-non-skip pick in the caller (:3896-3930), then
//     ref_costs_comp[ref_frame] added (:3891), and
//     this_rd = RDCOST(rdmult, rddiv, rate2, distortion2)      (:3929).
//
// joint_motion_search (vp9_rdopt.c:1907-2075): an iterative single-ref-fixed
// two-ref MV search. For ite in [0..num_iters): id=ite%2 (even searches ref0,
// odd ref1), build second_pred from the OTHER ref's current MV
// (vp9_build_inter_predictor, :2010), small-range full-pel
// vp9_refining_search_8p_c against the SAD-averaged target (:2027), then
// vp9_get_mvpred_av_var (:2031) + find_fractional_mv_step with second_pred
// (:2039); accept only if bestsme improved (:2049). skip_iters
// (vp9_rdopt.c:1837) breaks when the searched ref's full-pel MV and the other
// ref's MV both repeat from 2 iterations back. rate_mv (:2061-2074) is the
// sum of the two final MVs' vp9_mv_bit_cost against ref_mvs[refs[ref]][0].
//
// ===================== libvpx GROUND TRUTH (pinned) =======================
// Derived 2-ref fixture (above), cpu0, frame 2, SB0 root, BLOCK_32X32,
// NEWMV, refs GOLDEN(2)+ALTREF(3), captured with TEMPORARY fprintf (reverted):
//
//	joint search: iters=4 single0=(21,10) single1=(20,14)
//	              refmv0=(5,20) refmv1=(-5,-20) -> jms0=(21,10) jms1=(20,14)
//	              rate_mv=14223 (joint search did not move the MVs here)
//	pre-Y rate2 = rate_mv(14223) + cost_mv_ref(400) + rs(1069) = 15692
//	super_block_yrd  -> rate_y=2724984 dist_y(in dist2)
//	super_block_uvrd -> rate_uv=836545
//	ref_costs_comp[GOLDEN]=512  skip_cost0(no-skip flag)=23  skip2=0
//	rate2 = 15692 + 2724984 + 836545 + 512 + 23 = 3577756
//	dist2 = 48096  total_sse = 27113520
//	this_rd = RDCOST(rdmult=5442, rddiv=7, 3577756, 48096) = 44183921
//
// The compound RD ASSEMBLY (rate2 composition + skip-pick + RDCOST) and the
// joint-search iteration-count / skip logic are pinned to these exact libvpx
// values in vp9_fullrd_inter_comp_parity_test.go. (rate_y / rate_uv / rate_mv
// depend on frame-2's reconstructed reference buffers and per-frame entropy
// state, which a standalone unit cannot reconstruct; they enter the assembly
// pin as the libvpx-captured components, exactly as vp9_fullrd_inter_thisrd.go
// documents its own super_block_yrd/uvrd ground truth.)

// vp9GetJointSearchIters ports get_joint_search_iters (vp9_rdopt.c:1896-1905)
// verbatim: the number of joint compound MV-search iterations as a function of
// the comp_inter_joint_search_iter_level speed feature and block size.
//
// sf_level is cpi->sf.comp_inter_joint_search_iter_level: 1 for the
// good-quality/VOD default (vp9_speed_features.c:247), 2 for speeds >=3
// (:335,:534) and the RT path (:941 sets it 0 — but RT never reaches the
// compound full-RD branch). MAX_JOINT_MV_SEARCH_ITERS == 4 (vp9_rdopt.c:1895).
func vp9GetJointSearchIters(sfLevel int, bsize common.BlockSize) int {
	numIters := vp9MaxJointMvSearchIters // sf_level == 0
	if sfLevel >= 2 {
		numIters = 0
	} else if sfLevel >= 1 {
		if bsize < common.Block8x8 {
			numIters = 0
		} else if bsize <= common.Block16x16 {
			numIters = 2
		} else {
			numIters = vp9MaxJointMvSearchIters
		}
	}
	return numIters
}

// vp9MaxJointMvSearchIters mirrors MAX_JOINT_MV_SEARCH_ITERS (vp9_rdopt.c:1895).
const vp9MaxJointMvSearchIters = 4

// vp9JointSearchSkipIters ports skip_iters (vp9_rdopt.c:1837-1847) verbatim: the
// joint-search early-out test. It returns true when, two iterations back, the
// OTHER reference's MV is unchanged AND the searched reference's full-pixel MV
// (>>3) is unchanged — i.e. the search has converged for this id.
//
// iterMvs[ite][ref] holds the per-iteration MV pair (int_mv iter_mvs in libvpx).
func vp9JointSearchSkipIters(iterMvs [][2]vp9dec.MV, ite, id int) bool {
	if ite < 2 || ite >= len(iterMvs) {
		return false
	}
	other := 1 - id
	if iterMvs[ite-2][other] != iterMvs[ite][other] {
		return false
	}
	// Full-pixel (>>3) comparison of the searched ref's MV.
	curRow := iterMvs[ite][id].Row >> 3
	curCol := iterMvs[ite][id].Col >> 3
	prevRow := iterMvs[ite-2][id].Row >> 3
	prevCol := iterMvs[ite-2][id].Col >> 3
	return curRow == prevRow && curCol == prevCol
}

// vp9FullRDInterCompCostMvRef ports the mode-rate addition at
// vp9_rdopt.c:2970-2977 (cost_mv_ref, :1551-1554) for a compound candidate. The
// compound mode cost reads the SAME inter_mode_cost table as a single-reference
// candidate, indexed by mode_context[refs[0]].
//
// The discount_newmv_test VPXMIN against NEARESTMV (:2970-2974) is NOT gated on
// !is_comp_pred — discount_newmv_test (vp9_rdopt.c, non-CONFIG_NON_GREEDY_MV
// branch) reads mode_mv[NEARESTMV/NEARMV][refs[0]] with the single ref refs[0]
// and fires for a compound NEWMV too when !is_src_frame_alt_ref, mv0!=0, and
// both the NEARESTMV and NEARMV candidate MVs for refs[0] are 0 or INVALID. The
// caller evaluates discount_newmv_test (as vp9FullRDInterThisRD does via
// vp9FullRDInterDiscountNewMv) and passes the result as `discount`; when set,
// the charge is VPXMIN(cost_mv_ref(this_mode), cost_mv_ref(NEARESTMV)).
func vp9FullRDInterCompCostMvRef(fc *vp9dec.FrameContext, interCtx int,
	mode common.PredictionMode, discount bool,
) int {
	cost := encoder.CostMvRef(fc, interCtx, mode)
	if discount && mode == common.NewMv {
		if nearest := encoder.CostMvRef(fc, interCtx, common.NearestMv); nearest < cost {
			cost = nearest
		}
	}
	return cost
}

// vp9FullRDInterCompAssemble is the pure RD-composition core of the compound
// branch, separated so the libvpx component values (rate_y/rate_uv/rate_mv from
// the captured ground truth) can be fed directly to pin the assembly verbatim
// without reconstructing frame-2's reference buffers. It mirrors
// vp9_rdopt.c:2916/2970-2977/3164/3186-3203 + caller :3891-3929.
//
// This is the compound sibling of the vp9FullRDInterThisRD skip-pick assembly:
// rate2 = rate_mv + cost_mv_ref + rs + rate_y + rate_uv + ref_costs_comp, then
// the skippable / non-skip pick, then this_rd = RDCOST(rdmult, rddiv, rate2,
// dist2). skip0/skip1 are vp9_cost_bit(skip_prob, 0/1).
func vp9FullRDInterCompAssemble(rateMv, costMvRef, rs, rateY, rateUV, refCostsComp,
	skip0, skip1 int, distY, distUV, sseY, sseUV uint64,
	skippableY, skippableUV, lossless, sharpness bool, rdmult int,
) (rate2 int, dist2 uint64, totalSSE uint64, skip2 bool, thisRD uint64) {
	rddiv := encoder.RDDivBits
	preRate := rateMv + costMvRef + rs
	rate2 = preRate + rateY + rateUV + refCostsComp
	dist2 = distY + distUV
	totalSSE = sseY + sseUV
	skippable := skippableY && skippableUV
	if skippable {
		rate2 -= rateY + rateUV
		rate2 += skip1
	} else if !lossless && !sharpness {
		noSkip := encoder.RDCost(rdmult, rddiv, rateY+rateUV+skip0, dist2)
		skip := encoder.RDCost(rdmult, rddiv, skip1, totalSSE)
		if noSkip < skip {
			rate2 += skip0
		} else {
			rate2 += skip1
			dist2 = totalSSE
			rate2 -= rateY + rateUV
			skip2 = true
		}
	} else {
		rate2 += skip0
	}
	thisRD = encoder.RDCost(rdmult, rddiv, rate2, dist2)
	return rate2, dist2, totalSSE, skip2, thisRD
}
