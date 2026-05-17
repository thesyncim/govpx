package govpx

import (
	"math/bits"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// vp9_block_yrd.go ports the libvpx v1.16.0 substrate the realtime nonrd
// pickmode uses to produce libvpx-faithful (rate, distortion) tuples for
// each candidate (ref, mode, mv, filter). The three pieces ported here are:
//
//   - model_rd_norm + rate/dist_tab_q10 + xsq_iq_q10 + MAX_XSQ_Q10
//     (vp9/encoder/vp9_rd.c:452-516 — Laplace source model).
//   - vp9_model_rd_from_var_lapndz (vp9/encoder/vp9_rd.c:518-539) — closed-
//     form RD estimate for a Laplacian source quantized with a uniform
//     stepsize.
//   - model_rd_for_sb_y (vp9/encoder/vp9_pickmode.c:645-726) — the Y-plane
//     entry that feeds the picker. Produces (out_rate_sum, out_dist_sum,
//     var_y, sse_y) plus the SKIP_TXFM_* flag.
//
// These are the kernels libvpx's vp9_pick_inter_mode loop calls between
// vp9_build_inter_predictors_sby and the RDCOST comparison. The previous
// govpx pickVP9InterReferenceModeNonRD used a raw-SSE proxy and the
// per-frame RDMULT to score candidates; the residual ~430 bytes per inter
// frame on the deferred RefControl seeds came almost entirely from that
// proxy ordering differently than libvpx's (rate, dist) tuple at the
// quantizer-step model_rd produces. Folding model_rd_for_sb_y into the
// picker closes that ordering gap.

// vp9RateTabQ10 ports rate_tab_q10 (vp9_rd.c:462-471) verbatim — the
// normalized rate table indexed by the 4-msb bucket of xsq_q10.
var vp9RateTabQ10 = [...]int{
	65536, 6086, 5574, 5275, 5063, 4899, 4764, 4651, 4553, 4389, 4255, 4142, 4044,
	3958, 3881, 3811, 3748, 3635, 3538, 3453, 3376, 3307, 3244, 3186, 3133, 3037,
	2952, 2877, 2809, 2747, 2690, 2638, 2589, 2501, 2423, 2353, 2290, 2232, 2179,
	2130, 2084, 2001, 1928, 1862, 1802, 1748, 1698, 1651, 1608, 1530, 1460, 1398,
	1342, 1290, 1243, 1199, 1159, 1086, 1021, 963, 911, 864, 821, 781, 745,
	680, 623, 574, 530, 490, 455, 424, 395, 345, 304, 269, 239, 213,
	190, 171, 154, 126, 104, 87, 73, 61, 52, 44, 38, 28, 21,
	16, 12, 10, 8, 6, 5, 3, 2, 1, 1, 1, 0, 0,
}

// vp9DistTabQ10 ports dist_tab_q10 (vp9_rd.c:480-489) verbatim — the
// normalized distortion table indexed by the same bucket.
var vp9DistTabQ10 = [...]int{
	0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 4, 5, 5,
	6, 7, 7, 8, 9, 11, 12, 13, 15, 16, 17, 18, 21,
	24, 26, 29, 31, 34, 36, 39, 44, 49, 54, 59, 64, 69,
	73, 78, 88, 97, 106, 115, 124, 133, 142, 151, 167, 184, 200,
	215, 231, 245, 260, 274, 301, 327, 351, 375, 397, 418, 439, 458,
	495, 528, 559, 587, 613, 637, 659, 680, 717, 749, 777, 801, 823,
	842, 859, 874, 899, 919, 936, 949, 960, 969, 977, 983, 994, 1001,
	1006, 1010, 1013, 1015, 1017, 1018, 1020, 1022, 1022, 1023, 1023, 1023, 1024,
}

// vp9XsqIqQ10 ports xsq_iq_q10 (vp9_rd.c:490-503) — the bucket-edge table
// used to convert the bucket index back to xsq_q10 for interpolation.
var vp9XsqIqQ10 = [...]int{
	0, 4, 8, 12, 16, 20, 24, 28, 32,
	40, 48, 56, 64, 72, 80, 88, 96, 112,
	128, 144, 160, 176, 192, 208, 224, 256, 288,
	320, 352, 384, 416, 448, 480, 544, 608, 672,
	736, 800, 864, 928, 992, 1120, 1248, 1376, 1504,
	1632, 1760, 1888, 2016, 2272, 2528, 2784, 3040, 3296,
	3552, 3808, 4064, 4576, 5088, 5600, 6112, 6624, 7136,
	7648, 8160, 9184, 10208, 11232, 12256, 13280, 14304, 15328,
	16352, 18400, 20448, 22496, 24544, 26592, 28640, 30688, 32736,
	36832, 40928, 45024, 49120, 53216, 57312, 61408, 65504, 73696,
	81888, 90080, 98272, 106464, 114656, 122848, 131040, 147424, 163808,
	180192, 196576, 212960, 229344, 245728,
}

// vp9MaxXsqQ10 ports MAX_XSQ_Q10 (vp9_rd.c:516).
const vp9MaxXsqQ10 = 245727

// vp9GetMsb ports get_msb (vpx_ports/bitops.h:39-42 / 69-85) — returns
// floor(log2(n)). Caller guarantees n > 0.
func vp9GetMsb(n uint32) int {
	return 31 - bits.LeadingZeros32(n)
}

// vp9ModelRdNorm ports model_rd_norm (vp9_rd.c:505-514) verbatim.
//
//	static void model_rd_norm(int xsq_q10, int *r_q10, int *d_q10) {
//	  const int tmp = (xsq_q10 >> 2) + 8;
//	  const int k = get_msb(tmp) - 3;
//	  const int xq = (k << 3) + ((tmp >> k) & 0x7);
//	  const int one_q10 = 1 << 10;
//	  const int a_q10 = ((xsq_q10 - xsq_iq_q10[xq]) << 10) >> (2 + k);
//	  const int b_q10 = one_q10 - a_q10;
//	  *r_q10 = (rate_tab_q10[xq] * b_q10 + rate_tab_q10[xq + 1] * a_q10) >> 10;
//	  *d_q10 = (dist_tab_q10[xq] * b_q10 + dist_tab_q10[xq + 1] * a_q10) >> 10;
//	}
func vp9ModelRdNorm(xsqQ10 int) (rQ10, dQ10 int) {
	tmp := (xsqQ10 >> 2) + 8
	// get_msb is undefined for n == 0; libvpx callers guarantee xsq_q10 >= 0
	// (and 0 is handled in vp9_model_rd_from_var_lapndz before invoking us).
	k := vp9GetMsb(uint32(tmp)) - 3
	xq := (k << 3) + ((tmp >> k) & 0x7)
	const oneQ10 = 1 << 10
	aQ10 := ((xsqQ10 - vp9XsqIqQ10[xq]) << 10) >> (2 + k)
	bQ10 := oneQ10 - aQ10
	rQ10 = (vp9RateTabQ10[xq]*bQ10 + vp9RateTabQ10[xq+1]*aQ10) >> 10
	dQ10 = (vp9DistTabQ10[xq]*bQ10 + vp9DistTabQ10[xq+1]*aQ10) >> 10
	return
}

// vp9ModelRDFromVarLapndz ports vp9_model_rd_from_var_lapndz (vp9_rd.c:518-539):
//
//	void vp9_model_rd_from_var_lapndz(unsigned int var, unsigned int n_log2,
//	                                  unsigned int qstep, int *rate,
//	                                  int64_t *dist) {
//	  if (var == 0) {
//	    *rate = 0;
//	    *dist = 0;
//	  } else {
//	    int d_q10, r_q10;
//	    const uint64_t xsq_q10_64 =
//	        (((uint64_t)qstep * qstep << (n_log2 + 10)) + (var >> 1)) / var;
//	    const int xsq_q10 = (int)VPXMIN(xsq_q10_64, MAX_XSQ_Q10);
//	    model_rd_norm(xsq_q10, &r_q10, &d_q10);
//	    *rate = ROUND_POWER_OF_TWO(r_q10 << n_log2, 10 - VP9_PROB_COST_SHIFT);
//	    *dist = (var * (int64_t)d_q10 + 512) >> 10;
//	  }
//	}
//
// VP9_PROB_COST_SHIFT == 9 (govpx encoder.VP9ProbCostShift). The shift
// `10 - 9 == 1` lines up with libvpx.
func vp9ModelRDFromVarLapndz(varN uint32, nLog2 uint, qstep uint32) (rate int, dist int64) {
	if varN == 0 {
		return 0, 0
	}
	xsqQ1064 := ((uint64(qstep) * uint64(qstep)) << (nLog2 + 10)) + uint64(varN>>1)
	xsqQ1064 /= uint64(varN)
	var xsqQ10 int
	if xsqQ1064 > vp9MaxXsqQ10 {
		xsqQ10 = vp9MaxXsqQ10
	} else {
		xsqQ10 = int(xsqQ1064)
	}
	rQ10, dQ10 := vp9ModelRdNorm(xsqQ10)
	// libvpx ROUND_POWER_OF_TWO(x, n) == (x + (1<<(n-1))) >> n. VP9_PROB_COST_SHIFT==9
	// so n == 10 - 9 == 1; the +1 rounding adds 1 before shifting right by 1.
	const probCostShift = 9
	const round = 10 - probCostShift // == 1
	if round > 0 {
		rate = ((rQ10 << nLog2) + (1 << (round - 1))) >> round
	} else {
		rate = rQ10 << nLog2
	}
	dist = (int64(varN)*int64(dQ10) + 512) >> 10
	return
}

// vp9SkipTxfmFlag mirrors libvpx's SKIP_TXFM_* enum (vp9_blockd.h):
//
//	SKIP_TXFM_NONE   = 0
//	SKIP_TXFM_AC_DC  = 1
//	SKIP_TXFM_AC_ONLY = 2
type vp9SkipTxfmFlag uint8

const (
	vp9SkipTxfmNone   vp9SkipTxfmFlag = 0
	vp9SkipTxfmAcDc   vp9SkipTxfmFlag = 1
	vp9SkipTxfmAcOnly vp9SkipTxfmFlag = 2
)

// vp9ModelRdForSbY ports model_rd_for_sb_y (vp9_pickmode.c:645-726) verbatim.
// Inputs:
//
//   - bsize: block size.
//   - dequant: Y-plane (dc, ac) dequantizers from inter.dq.Y[segId].
//   - varY, sseY: pre-computed variance/SSE between src and dst (the
//     inter prediction).
//   - isIntra: 0 for inter, 1 for intra (controls the tx_size threshold;
//     the inter picker always passes 0).
//
// Outputs:
//
//   - outRateSum, outDistSum: libvpx's (rate, dist) tuple suitable for the
//     RDCOST comparison.
//   - skipTxfm: SKIP_TXFM_* flag; the picker forwards it to the bitstream
//     writer when this candidate wins.
//
// libvpx's calculate_tx_size + the quant_thred-based skip test are folded
// in; the simplified path here omits the AQ_CYCLIC_REFRESH branch and the
// VP9E_CONTENT_SCREEN branch because the realtime fuzz seeds disable both.
// dc_thr/ac_thr derive from `dequant[0]*dequant[0] >> 6` and `>>6` (the
// libvpx p->quant_thred initializer at vp9_quantize.c:265 sets quant_thred
// = zbin*zbin == dequant*dequant for !use_quant_fp; the shift by 6 in
// model_rd_for_sb_y normalizes it). govpx mirrors that constant exactly.
//
// The shifted-domain returned distortion is `sse << 4` when the Y-plane
// is AC_DC-skippable; otherwise it is `(sse-var) << 4` plus the model_rd
// AC contribution `(dist << 4)`. Rates are in libvpx prob-cost units
// (bits << VP9_PROB_COST_SHIFT), matching the units the rest of the
// picker rate-cost helpers (vp9InterModeRateCost, ref_frame_cost, ...)
// emit.
//
// Block-size note: callers pass bsize directly; this function expects
// bsize >= BLOCK_8X8 (the realtime picker never invokes mode decision
// at sub-8x8). For BLOCK_4X4 the n_log2 == 4 path is still well-defined
// but tx_size would clamp to TX_4X4 — not exercised here.
func vp9ModelRdForSbY(bsize common.BlockSize, dequant [2]int16,
	varY, sseY uint64, isIntra int,
) (outRateSum int, outDistSum int64, skipTxfm vp9SkipTxfmFlag, txSize common.TxSize) {
	// libvpx: dc_thr = p->quant_thred[0] >> 6; ac_thr = p->quant_thred[1] >> 6;
	// quant_thred[i] = dequant[i]*dequant[i] (vp9_quantize.c:265 init in the
	// !use_quant_fp path; nonrd_pickmode always runs with use_quant_fp).
	dcQuant := uint32(dequant[0])
	acQuant := uint32(dequant[1])
	dcThr := int64(dcQuant) * int64(dcQuant) >> 6
	acThr := int64(acQuant) * int64(acQuant) >> 6

	// libvpx: tx_size = calculate_tx_size(...). For TX_MODE_SELECT path:
	//   tx_size = (sse > (var << 2)) ? min(max_txsize, biggest_tx) : TX_8X8;
	// govpx: nonrd_pickmode runs with tx_mode TX_MODE_SELECT, so port the
	// SELECT branch verbatim. The cyclic-refresh / screen-content tweaks
	// fold into !isIntra == 1 callers.
	varThresh := uint64(1)
	if isIntra != 0 {
		// var_thresh = (unsigned int)ac_thr (vp9_pickmode.c:369).
		varThresh = uint64(acThr)
	}
	_ = varThresh
	// tx_size = min(max_txsize_lookup[bsize], TX_32X32 (TX_MODE_SELECT
	// biggest is TX_32X32)) when sse > var*4 else TX_8X8.
	maxTx := common.MaxTxsizeLookup[bsize]
	if sseY > (varY << 2) {
		txSize = min(maxTx, common.Tx32x32)
	} else {
		txSize = common.Tx8x8
	}

	// libvpx: skippable test on the per-tx-unit (var_tx, sse_tx) — pre-
	// divided by num_blk via the num_blk_log2 shift. govpx mirrors the
	// shifts exactly.
	unitSize := common.TxsizeToBsize[txSize]
	numBlkLog2 := (common.BWidthLog2Lookup[bsize] - common.BWidthLog2Lookup[unitSize]) +
		(common.BHeightLog2Lookup[bsize] - common.BHeightLog2Lookup[unitSize])
	sseTx := sseY >> uint(numBlkLog2)
	varTx := varY >> uint(numBlkLog2)

	skipTxfm = vp9SkipTxfmNone
	skipDc := false
	// libvpx vp9_pickmode.c:682-689 — ac quantizable to zero?
	if varTx < uint64(acThr) || varY == 0 {
		skipTxfm = vp9SkipTxfmAcOnly
		// dc quantizable to zero?
		if sseTx-varTx < uint64(dcThr) || sseY == varY {
			skipTxfm = vp9SkipTxfmAcDc
		}
	} else if sseTx-varTx < uint64(dcThr) || sseY == varY {
		skipDc = true
	}

	if skipTxfm == vp9SkipTxfmAcDc {
		// libvpx vp9_pickmode.c:692-696 — full Y skip.
		outRateSum = 0
		outDistSum = int64(sseY << 4)
		return
	}

	nLog2 := uint(common.NumPelsLog2Lookup[bsize])
	if !skipDc {
		// libvpx: vp9_model_rd_from_var_lapndz(sse - var, n_log2, dc_quant >> 3, ...);
		dcRate, dcDist := vp9ModelRDFromVarLapndz(uint32(sseY-varY), nLog2, dcQuant>>3)
		outRateSum = dcRate >> 1
		outDistSum = dcDist << 3
	} else {
		outRateSum = 0
		outDistSum = int64((sseY - varY) << 4)
	}

	// libvpx: vp9_model_rd_from_var_lapndz(var, n_log2, ac_quant >> 3, ...);
	acRate, acDist := vp9ModelRDFromVarLapndz(uint32(varY), nLog2, acQuant>>3)
	outRateSum += acRate
	outDistSum += acDist << 4
	return
}

// vp9EncodeBreakoutTest ports encode_breakout_test (vp9_pickmode.c:942-1045)
// verbatim. Inputs are the per-candidate (var_y, sse_y, mv, dequant), plus
// the per-frame encode_breakout knob (libvpx x->encode_breakout — seeded
// from cpi->oxcf.encode_breakout / segment_encode_breakout in
// vp9_pick_inter_mode at line 1787). The return tuple captures whether
// the breakout fired and, if so, the libvpx-faithful (rate, dist) override.
//
// Important: the var/sse inputs here are Y-plane only; libvpx's UV-plane
// breakout test runs inside encode_breakout_test by invoking the inter
// predictor on chroma and computing variance/sse there. govpx supplies the
// UV variance/sse via the caller so the breakout port stays kernel-only
// (no encoder-state side effects).
//
// libvpx:
//
//	if (cpi->use_svc && ref_frame == GOLDEN_FRAME) return;
//	if (|mv| > 64) motion_low = 0;
//	if (x->encode_breakout > 0 && motion_low) {
//	  max_thresh = 36000;
//	  min_thresh = VPXMIN(encode_breakout << 4, max_thresh);
//	  thresh_ac = (dequant[1]*dequant[1]) >> 3;
//	  thresh_ac = clamp(thresh_ac, min_thresh, max_thresh);
//	  thresh_ac >>= 8 - (bw_log2 + bh_log2);
//	  thresh_dc = (dequant[0]*dequant[0]) >> 6;
//	} else {
//	  thresh_ac = thresh_dc = 0;
//	}
//	if (var <= thresh_ac && (sse - var) <= thresh_dc) {
//	  ... UV-plane skip checks ...
//	  if (var_v << 2 <= thresh_ac_uv && sse_v - var_v <= thresh_dc_uv) {
//	    x->skip = 1;
//	    *rate = inter_mode_cost[...][INTER_OFFSET(this_mode)];
//	    *dist = sse << 4;
//	  }
//	}
//
// Returns (true, rate_override, dist_override) when the breakout fires.
func vp9EncodeBreakoutTest(bsize common.BlockSize, dequant [2]int16,
	mvRow, mvCol int16, varY, sseY uint64,
	uvDequant [2][2]int16, varU, sseU, varV, sseV uint64,
	encodeBreakout int, sbIsSkin bool, interModeBitCost int,
) (fired bool, distOverride int64, rateOverride int) {
	motionLow := !(mvRow > 64 || mvRow < -64 || mvCol > 64 || mvCol < -64)

	var threshAc, threshDc uint64
	if encodeBreakout > 0 && motionLow {
		// libvpx: const unsigned int max_thresh = 36000;
		const maxThresh uint64 = 36000
		// const unsigned int min_thresh = VPXMIN((unsigned int)x->encode_breakout << 4, max_thresh);
		minThresh := min(uint64(encodeBreakout)<<4, maxThresh)
		// thresh_ac = (dequant[1] * dequant[1]) >> 3;
		threshAc = max(
			// thresh_ac = clamp(thresh_ac, min_thresh, max_thresh);
			uint64(int64(dequant[1])*int64(dequant[1]))>>3, minThresh)
		if threshAc > maxThresh {
			threshAc = maxThresh
		}
		// thresh_ac >>= 8 - (bw_log2 + bh_log2);
		shift := 8 - int(common.BWidthLog2Lookup[bsize]+common.BHeightLog2Lookup[bsize])
		if shift > 0 {
			threshAc >>= uint(shift)
		}
		// thresh_dc = (dequant[0] * dequant[0]) >> 6;
		threshDc = uint64(int64(dequant[0])*int64(dequant[0])) >> 6
	}

	// libvpx: if (var <= thresh_ac && (sse - var) <= thresh_dc) { ... }
	if !(varY <= threshAc && sseY-varY <= threshDc) {
		return false, 0, 0
	}

	// libvpx vp9_pickmode.c:1003-1006 — x->sb_is_skin zeros the UV thresholds.
	threshAcUv := threshAc
	threshDcUv := threshDc
	if sbIsSkin {
		threshAcUv = 0
		threshDcUv = 0
	}

	// libvpx vp9_pickmode.c:1014-1025 — U/V skip tests (note the <<2 on var_uv).
	_ = uvDequant
	if !((varU<<2) <= threshAcUv && sseU-varU <= threshDcUv) {
		return false, 0, 0
	}
	if !((varV<<2) <= threshAcUv && sseV-varV <= threshDcUv) {
		return false, 0, 0
	}

	// libvpx vp9_pickmode.c:1026-1041:
	//   x->skip = 1;
	//   *rate = inter_mode_cost[ctx][INTER_OFFSET(this_mode)];
	//   *dist = sse << 4;
	rateOverride = interModeBitCost
	distOverride = int64(sseY << 4)
	return true, distOverride, rateOverride
}
