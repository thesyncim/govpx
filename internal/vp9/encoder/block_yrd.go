package encoder

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

// rateTabQ10 ports rate_tab_q10 (vp9_rd.c:462-471) verbatim — the
// normalized rate table indexed by the 4-msb bucket of xsq_q10.
var rateTabQ10 = [...]int{
	65536, 6086, 5574, 5275, 5063, 4899, 4764, 4651, 4553, 4389, 4255, 4142, 4044,
	3958, 3881, 3811, 3748, 3635, 3538, 3453, 3376, 3307, 3244, 3186, 3133, 3037,
	2952, 2877, 2809, 2747, 2690, 2638, 2589, 2501, 2423, 2353, 2290, 2232, 2179,
	2130, 2084, 2001, 1928, 1862, 1802, 1748, 1698, 1651, 1608, 1530, 1460, 1398,
	1342, 1290, 1243, 1199, 1159, 1086, 1021, 963, 911, 864, 821, 781, 745,
	680, 623, 574, 530, 490, 455, 424, 395, 345, 304, 269, 239, 213,
	190, 171, 154, 126, 104, 87, 73, 61, 52, 44, 38, 28, 21,
	16, 12, 10, 8, 6, 5, 3, 2, 1, 1, 1, 0, 0,
}

// distTabQ10 ports dist_tab_q10 (vp9_rd.c:480-489) verbatim — the
// normalized distortion table indexed by the same bucket.
var distTabQ10 = [...]int{
	0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 4, 5, 5,
	6, 7, 7, 8, 9, 11, 12, 13, 15, 16, 17, 18, 21,
	24, 26, 29, 31, 34, 36, 39, 44, 49, 54, 59, 64, 69,
	73, 78, 88, 97, 106, 115, 124, 133, 142, 151, 167, 184, 200,
	215, 231, 245, 260, 274, 301, 327, 351, 375, 397, 418, 439, 458,
	495, 528, 559, 587, 613, 637, 659, 680, 717, 749, 777, 801, 823,
	842, 859, 874, 899, 919, 936, 949, 960, 969, 977, 983, 994, 1001,
	1006, 1010, 1013, 1015, 1017, 1018, 1020, 1022, 1022, 1023, 1023, 1023, 1024,
}

// xsqIqQ10 ports xsq_iq_q10 (vp9_rd.c:490-503) — the bucket-edge table
// used to convert the bucket index back to xsq_q10 for interpolation.
var xsqIqQ10 = [...]int{
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

// maxXsqQ10 ports MAX_XSQ_Q10 (vp9_rd.c:516).
const maxXsqQ10 = 245727

// getMsb ports get_msb (vpx_ports/bitops.h:39-42 / 69-85) — returns
// floor(log2(n)). Caller guarantees n > 0.
func getMsb(n uint32) int {
	return 31 - bits.LeadingZeros32(n)
}

// ModelRdNorm ports model_rd_norm (vp9_rd.c:505-514) verbatim.
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
func ModelRdNorm(xsqQ10 int) (rQ10, dQ10 int) {
	tmp := (xsqQ10 >> 2) + 8
	// get_msb is undefined for n == 0; libvpx callers guarantee xsq_q10 >= 0
	// (and 0 is handled in vp9_model_rd_from_var_lapndz before invoking us).
	k := getMsb(uint32(tmp)) - 3
	xq := (k << 3) + ((tmp >> k) & 0x7)
	const oneQ10 = 1 << 10
	aQ10 := ((xsqQ10 - xsqIqQ10[xq]) << 10) >> (2 + k)
	bQ10 := oneQ10 - aQ10
	rQ10 = (rateTabQ10[xq]*bQ10 + rateTabQ10[xq+1]*aQ10) >> 10
	dQ10 = (distTabQ10[xq]*bQ10 + distTabQ10[xq+1]*aQ10) >> 10
	return
}

// ModelRDFromVarLapndz ports vp9_model_rd_from_var_lapndz (vp9_rd.c:518-539):
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
// VP9_PROB_COST_SHIFT == 9 (govpx VP9ProbCostShift). The shift
// `10 - 9 == 1` lines up with libvpx.
func ModelRDFromVarLapndz(varN uint32, nLog2 uint, qstep uint32) (rate int, dist int64) {
	if varN == 0 {
		return 0, 0
	}
	xsqQ1064 := ((uint64(qstep) * uint64(qstep)) << (nLog2 + 10)) + uint64(varN>>1)
	xsqQ1064 /= uint64(varN)
	var xsqQ10 int
	if xsqQ1064 > maxXsqQ10 {
		xsqQ10 = maxXsqQ10
	} else {
		xsqQ10 = int(xsqQ1064)
	}
	rQ10, dQ10 := ModelRdNorm(xsqQ10)
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

// SkipTxfmFlag mirrors libvpx's SKIP_TXFM_* enum (vp9_blockd.h):
//
//	SKIP_TXFM_NONE   = 0
//	SKIP_TXFM_AC_DC  = 1
//	SKIP_TXFM_AC_ONLY = 2
type SkipTxfmFlag uint8

const (
	SkipTxfmNone   SkipTxfmFlag = 0
	SkipTxfmAcDc   SkipTxfmFlag = 1
	SkipTxfmAcOnly SkipTxfmFlag = 2
)

const BlockYrdUnknownSSE = uint64(1<<63 - 1)

// ModelRdForSbYArgs is the libvpx model_rd_for_sb_y input surface used by
// realtime non-RD mode picking.
type ModelRdForSbYArgs struct {
	BSize   common.BlockSize
	QIndex  int
	Dequant [2]int16
	VarY    uint64
	SSEY    uint64

	// IsIntra controls calculate_tx_size's var_thresh branch. The inter
	// picker passes false; the non-RD intra fallback passes true.
	IsIntra bool

	TxMode          common.TxMode
	SourceVariance  uint64
	SegmentID       uint8
	CyclicRefreshAQ bool
	ScreenContent   bool
}

// ModelRdForSbUVArgs is the chroma-plane companion to ModelRdForSbY. It
// mirrors libvpx model_rd_for_sb_uv's scalar inputs after the caller has
// built the U/V predictors and measured each plane's variance/SSE.
type ModelRdForSbUVArgs struct {
	BSize     common.BlockSize
	Sensitive [2]bool
	Var       [2]uint64
	SSE       [2]uint64
	Dequant   [2][2]int16
	VarY      uint64
	SSEY      uint64
}

// ModelRdForSbUV ports model_rd_for_sb_uv from vp9_pickmode.c. For each
// color-sensitive chroma plane it adds the same DC and AC Laplace-model RD
// costs used by model_rd_for_sb_y, and returns the libvpx-mutated total
// variance/SSE accumulators.
func ModelRdForSbUV(args ModelRdForSbUVArgs) (
	rate int, dist int64, totalVar uint64, totalSSE uint64,
) {
	totalVar = args.VarY
	totalSSE = args.SSEY
	if args.BSize >= common.BlockSizes {
		return 0, 0, totalVar, totalSSE
	}
	nLog2 := uint(common.NumPelsLog2Lookup[args.BSize])
	for plane := range 2 {
		if !args.Sensitive[plane] {
			continue
		}
		variance := args.Var[plane]
		sse := args.SSE[plane]
		if sse < variance {
			continue
		}
		totalVar += variance
		totalSSE += sse

		dequant := args.Dequant[plane]
		dcQuant := uint32(dequant[0])
		acQuant := uint32(dequant[1])
		dcRate, dcDist := ModelRDFromVarLapndz(uint32(sse-variance), nLog2,
			dcQuant>>3)
		rate += dcRate >> 1
		dist += dcDist << 3

		acRate, acDist := ModelRDFromVarLapndz(uint32(variance), nLog2,
			acQuant>>3)
		rate += acRate
		dist += acDist << 4
	}
	return rate, dist, totalVar, totalSSE
}

// CalculateTxSizeArgs is the explicit Go form of libvpx
// calculate_tx_size(cpi, bsize, xd, var, sse, ac_thr, source_variance,
// is_intra) from vp9_pickmode.c:363-393.
type CalculateTxSizeArgs struct {
	BSize           common.BlockSize
	TxMode          common.TxMode
	VarY            uint64
	SSEY            uint64
	ACThreshold     int64
	SourceVariance  uint64
	IsIntra         bool
	CyclicRefreshAQ bool
	SegmentID       uint8
	ScreenContent   bool
}

// TxSizeForcesArgs describes the post-candidate transform-size forces in
// calculate_tx_size: cyclic-refresh boosted segments, the Tx16x16 cap, and
// screen-content Tx4x4 forcing.
type TxSizeForcesArgs struct {
	TxSize          common.TxSize
	BSize           common.BlockSize
	VarY            uint64
	ACThreshold     int64
	LimitTx         bool
	CyclicRefreshAQ bool
	SegmentID       uint8
	ScreenContent   bool
}

type ModelRdForSbYLargeArgs struct {
	BSize   common.BlockSize
	Dequant [2]int16

	Src        []byte
	SrcStride  int
	SrcX       int
	SrcY       int
	Pred       []byte
	PredStride int
	PredX      int
	PredY      int

	TxMode            common.TxMode
	SourceVariance    uint64
	SegmentID         uint8
	CyclicRefreshAQ   bool
	ScreenContent     bool
	ZeroTempSADSource bool

	Speed  int
	Width  int
	Height int
}

type ModelRdForSbYLargeResult struct {
	Rate     int
	Dist     int64
	VarY     uint64
	SSEY     uint64
	SkipTxfm SkipTxfmFlag
	TxSize   common.TxSize
	Valid    bool
}

type ModelRdForSbYLargeEarlyTermArgs struct {
	UVBSize  common.BlockSize
	UVTxSize common.TxSize
	Dequant  [2][2]int16
	Var      [2]uint64
	SSE      [2]uint64
}

// ModelRdForSbYLarge ports libvpx model_rd_for_sb_y_large
// (vp9_pickmode.c:439-643) for the 8-bit Y plane. The large-block kernel
// differs from ModelRdForSbY by testing every 8x8/16x16/32x32 transform unit
// before declaring the whole Y plane skippable; that prevents a single
// high-energy tile from being hidden by a low whole-block average.
func ModelRdForSbYLarge(args ModelRdForSbYLargeArgs) ModelRdForSbYLargeResult {
	var res ModelRdForSbYLargeResult
	if args.BSize < common.Block8x8 || args.BSize >= common.BlockSizes ||
		args.SrcStride <= 0 || args.PredStride <= 0 {
		return res
	}
	bwLog2 := int(common.BWidthLog2Lookup[args.BSize])
	bhLog2 := int(common.BHeightLog2Lookup[args.BSize])
	blockW := 4 << uint(bwLog2)
	blockH := 4 << uint(bhLog2)
	if !modelRdWindowFits(args.Src, args.SrcStride, args.SrcX, args.SrcY,
		blockW, blockH) ||
		!modelRdWindowFits(args.Pred, args.PredStride, args.PredX, args.PredY,
			blockW, blockH) {
		return res
	}

	var sse8x8 [64]uint32
	var sum8x8 [64]int
	var var8x8 [64]uint32
	sse, sum, ok := modelRdBlockVariance8x8(args.Src, args.SrcStride,
		args.SrcX, args.SrcY, args.Pred, args.PredStride, args.PredX,
		args.PredY, blockW, blockH, sse8x8[:], sum8x8[:], var8x8[:])
	if !ok {
		return res
	}

	sumSqr := uint32((int64(sum) * int64(sum)) >> uint(bwLog2+bhLog2+4))
	varY := modelRdAbsDiff32(sse, sumSqr)
	res.VarY = uint64(varY)
	res.SSEY = uint64(sse)

	dcQuant := uint32(args.Dequant[0])
	acQuant := uint32(args.Dequant[1])
	dcThr := int64(dcQuant*dcQuant) >> 6
	acThr := int64(acQuant*acQuant) >> 6
	acThr *= int64(modelRdACThresholdFactor(args.Speed, args.Width,
		args.Height, modelRdAbsInt(sum)>>uint(bwLog2+bhLog2)))

	txSize := CalculateTxSize(CalculateTxSizeArgs{
		BSize:           args.BSize,
		TxMode:          args.TxMode,
		VarY:            uint64(varY),
		SSEY:            uint64(sse),
		ACThreshold:     acThr,
		SourceVariance:  args.SourceVariance,
		CyclicRefreshAQ: args.CyclicRefreshAQ,
		SegmentID:       args.SegmentID,
		ScreenContent:   args.ScreenContent,
	})
	if txSize < common.Tx8x8 {
		txSize = common.Tx8x8
	}
	res.TxSize = txSize

	if args.ScreenContent && args.ZeroTempSADSource && args.SourceVariance == 0 {
		dcThr <<= 1
	}

	num8x8 := 1 << uint(bwLog2+bhLog2-2)
	sseTx := sse8x8[:]
	varTx := var8x8[:]
	numTx := num8x8
	var sse16x16 [16]uint32
	var sum16x16 [16]int
	var var16x16 [16]uint32
	if txSize >= common.Tx16x16 {
		modelRdAggregateVariance(bwLog2, bhLog2, common.Tx8x8,
			sse8x8[:], sum8x8[:], var16x16[:], sse16x16[:], sum16x16[:])
		sseTx = sse16x16[:]
		varTx = var16x16[:]
		numTx = num8x8 >> 2
	}
	var sse32x32 [4]uint32
	var sum32x32 [4]int
	var var32x32 [4]uint32
	if txSize == common.Tx32x32 {
		modelRdAggregateVariance(bwLog2, bhLog2, common.Tx16x16,
			sse16x16[:], sum16x16[:], var32x32[:], sse32x32[:], sum32x32[:])
		sseTx = sse32x32[:]
		varTx = var32x32[:]
		numTx = num8x8 >> 4
	}

	skipDc := false
	res.SkipTxfm = modelRdLargeSkipTxfm(sse, varY, sseTx, varTx, numTx,
		acThr, dcThr)
	if res.SkipTxfm == SkipTxfmNone &&
		modelRdLargeDCSkippable(sse, varY, sseTx, varTx, numTx, dcThr) {
		skipDc = true
	}

	if res.SkipTxfm == SkipTxfmAcDc {
		res.Rate = 0
		res.Dist = int64(uint64(sse) << 4)
		res.Valid = true
		return res
	}

	nLog2 := uint(common.NumPelsLog2Lookup[args.BSize])
	if !skipDc {
		dcRate, dcDist := ModelRDFromVarLapndz(sse-varY, nLog2, dcQuant>>3)
		res.Rate = dcRate >> 1
		res.Dist = dcDist << 3
	} else {
		res.Rate = 0
		res.Dist = int64(uint64(sse-varY) << 4)
	}
	acRate, acDist := ModelRDFromVarLapndz(varY, nLog2, acQuant>>3)
	res.Rate += acRate
	res.Dist += acDist << 4
	res.Valid = true
	return res
}

// ModelRdForSbYLargeEarlyTerm ports the UV transform-skip test at the
// end of libvpx model_rd_for_sb_y_large. The caller has already proved the
// Y plane is SKIP_TXFM_AC_DC and supplied the U/V variances after building
// the chroma predictors for the same candidate.
func ModelRdForSbYLargeEarlyTerm(args ModelRdForSbYLargeEarlyTermArgs) bool {
	if args.UVBSize >= common.BlockSizes || args.UVTxSize >= common.TxSizes {
		return false
	}
	unitSize := common.TxsizeToBsize[args.UVTxSize]
	uvBW := int(common.BWidthLog2Lookup[args.UVBSize])
	uvBH := int(common.BHeightLog2Lookup[args.UVBSize])
	unitBW := int(common.BWidthLog2Lookup[unitSize])
	unitBH := int(common.BHeightLog2Lookup[unitSize])
	scaleShift := (uvBW - unitBW) + (uvBH - unitBH)
	if scaleShift < 0 || scaleShift > 6 {
		return false
	}
	thresholdShift := uint(6 - scaleShift)
	for plane := range 2 {
		dequant := args.Dequant[plane]
		dcThr := uint64(int64(dequant[0])*int64(dequant[0])) >> thresholdShift
		acThr := uint64(int64(dequant[1])*int64(dequant[1])) >> thresholdShift
		variance := args.Var[plane]
		sse := args.SSE[plane]
		if sse < variance {
			return false
		}
		if !((variance < acThr || variance == 0) &&
			(sse-variance < dcThr || sse == variance)) {
			return false
		}
	}
	return true
}

func modelRdWindowFits(buf []byte, stride, x, y, w, h int) bool {
	if len(buf) == 0 || stride <= 0 || x < 0 || y < 0 || w <= 0 || h <= 0 {
		return false
	}
	if x+w > stride {
		return false
	}
	return (y+h)*stride <= len(buf)
}

func modelRdBlockVariance8x8(src []byte, srcStride, srcX, srcY int,
	pred []byte, predStride, predX, predY, w, h int,
	sse8x8 []uint32, sum8x8 []int, var8x8 []uint32,
) (uint32, int, bool) {
	if w%8 != 0 || h%8 != 0 {
		return 0, 0, false
	}
	k := 0
	var totalSSE uint32
	totalSum := 0
	for y := 0; y < h; y += 8 {
		for x := 0; x < w; x += 8 {
			if k >= len(sse8x8) || k >= len(sum8x8) || k >= len(var8x8) {
				return 0, 0, false
			}
			var blockSSE uint32
			blockSum := 0
			for yy := range 8 {
				srcRow := src[(srcY+y+yy)*srcStride+srcX+x:]
				predRow := pred[(predY+y+yy)*predStride+predX+x:]
				for xx := range 8 {
					diff := int(srcRow[xx]) - int(predRow[xx])
					blockSum += diff
					blockSSE += uint32(diff * diff)
				}
			}
			sse8x8[k] = blockSSE
			sum8x8[k] = blockSum
			sumSqr := uint32((int64(blockSum) * int64(blockSum)) >> 6)
			var8x8[k] = modelRdAbsDiff32(blockSSE, sumSqr)
			totalSSE += blockSSE
			totalSum += blockSum
			k++
		}
	}
	return totalSSE, totalSum, true
}

func modelRdAggregateVariance(bwLog2, bhLog2 int, txSize common.TxSize,
	inSSE []uint32, inSum []int, outVar []uint32, outSSE []uint32, outSum []int,
) {
	unitSize := common.TxsizeToBsize[txSize]
	nw := 1 << uint(bwLog2-int(common.BWidthLog2Lookup[unitSize]))
	nh := 1 << uint(bhLog2-int(common.BHeightLog2Lookup[unitSize]))
	k := 0
	for y := 0; y < nh; y += 2 {
		for x := 0; x < nw; x += 2 {
			idx := y*nw + x
			sse := inSSE[idx] + inSSE[idx+1] +
				inSSE[idx+nw] + inSSE[idx+nw+1]
			sum := inSum[idx] + inSum[idx+1] +
				inSum[idx+nw] + inSum[idx+nw+1]
			outSSE[k] = sse
			outSum[k] = sum
			shift := int(common.BWidthLog2Lookup[unitSize]) +
				int(common.BHeightLog2Lookup[unitSize]) + 6
			sumSqr := uint32((int64(sum) * int64(sum)) >> uint(shift))
			outVar[k] = modelRdAbsDiff32(sse, sumSqr)
			k++
		}
	}
}

func modelRdLargeSkipTxfm(sse, varY uint32, sseTx, varTx []uint32, num int,
	acThr, dcThr int64,
) SkipTxfmFlag {
	acTest := true
	dcTest := true
	for k := range num {
		if !(int64(varTx[k]) < acThr || varY == 0) {
			acTest = false
			break
		}
	}
	for k := range num {
		if !(int64(sseTx[k]-varTx[k]) < dcThr || sse == varY) {
			dcTest = false
			break
		}
	}
	if acTest {
		if dcTest {
			return SkipTxfmAcDc
		}
		return SkipTxfmAcOnly
	}
	return SkipTxfmNone
}

func modelRdLargeDCSkippable(sse, varY uint32, sseTx, varTx []uint32,
	num int, dcThr int64,
) bool {
	for k := range num {
		if !(int64(sseTx[k]-varTx[k]) < dcThr || sse == varY) {
			return false
		}
	}
	return true
}

func modelRdACThresholdFactor(speed, width, height int, normSum int) int {
	if speed >= 8 && normSum < 5 {
		if width <= 640 && height <= 480 {
			return 4
		}
		return 2
	}
	return 1
}

func modelRdAbsDiff32(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func modelRdAbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// CalculateTxSize ports libvpx vp9_pickmode.c:363-393. It is shared by the
// non-RD model_rd_for_sb_y path and the root encoder's later tx-size picker so
// the AQ/screen-content transform-size rules do not drift apart.
func CalculateTxSize(args CalculateTxSizeArgs) common.TxSize {
	if args.BSize >= common.BlockSizes {
		return common.Tx4x4
	}
	txMode := args.TxMode
	if txMode >= common.TxModes {
		txMode = common.TxModeSelect
	}
	maxTx := common.MaxTxsizeLookup[args.BSize]
	biggestForMode := common.TxModeToBiggestTxSize[txMode]
	if maxTx > biggestForMode {
		maxTx = biggestForMode
	}
	varThresh := uint64(1)
	if args.IsIntra {
		varThresh = 0
		if args.ACThreshold > 0 {
			varThresh = uint64(args.ACThreshold)
		}
	}
	limitTx := true
	if args.CyclicRefreshAQ &&
		(args.SourceVariance == 0 || args.VarY < varThresh) {
		limitTx = false
	}
	if txMode != common.TxModeSelect {
		return maxTx
	}
	txSize := common.Tx8x8
	if args.SSEY > args.VarY<<2 {
		txSize = maxTx
	}
	return ApplyTxSizeForces(TxSizeForcesArgs{
		TxSize:          txSize,
		BSize:           args.BSize,
		VarY:            args.VarY,
		ACThreshold:     args.ACThreshold,
		LimitTx:         limitTx,
		CyclicRefreshAQ: args.CyclicRefreshAQ,
		SegmentID:       args.SegmentID,
		ScreenContent:   args.ScreenContent,
	})
}

// ApplyTxSizeForces ports the force/cap tail of libvpx calculate_tx_size
// (vp9_pickmode.c:380-388) for callers that have already selected a candidate
// transform size by another scoring path.
func ApplyTxSizeForces(args TxSizeForcesArgs) common.TxSize {
	txSize := args.TxSize
	if args.CyclicRefreshAQ && args.LimitTx &&
		CyclicRefreshSegmentIDBoosted(args.SegmentID) {
		txSize = common.Tx8x8
	} else if txSize > common.Tx16x16 && args.LimitTx {
		txSize = common.Tx16x16
	}
	acThr := uint64(0)
	if args.ACThreshold > 0 {
		acThr = uint64(args.ACThreshold)
	}
	if args.ScreenContent && txSize == common.Tx8x8 &&
		args.BSize <= common.Block16x16 && (args.VarY>>5) > acThr {
		txSize = common.Tx4x4
	}
	return txSize
}

// ModelRdForSbY ports model_rd_for_sb_y (vp9_pickmode.c:645-726) verbatim.
// It returns libvpx's (rate, dist) tuple for the RDCOST comparison, the
// SKIP_TXFM_* flag, and the tx_size chosen by calculate_tx_size.
//
// dc_thr/ac_thr derive from `zbin[0|1]^2 >> 6` (the libvpx p->quant_thred
// initializer at vp9_quantize.c:264-265 sets quant_thred = zbin*zbin; the
// shift by 6 in model_rd_for_sb_y normalizes it). govpx mirrors the qzbin
// factor and ROUND_POWER_OF_TWO setup from vp9_quantize.c:209-211.
//
// The shifted-domain returned distortion is `sse << 4` when the Y-plane
// is AC_DC-skippable; otherwise it is `(sse-var) << 4` plus the model_rd
// AC contribution `(dist << 4)`. Rates are in libvpx prob-cost units
// (bits << VP9_PROB_COST_SHIFT), matching the units the rest of the
// picker rate-cost helpers (InterModeRateCost, ref_frame_cost, ...)
// emit.
//
// Block-size note: callers pass bsize directly; this function expects
// bsize >= BLOCK_8X8 (the realtime picker never invokes mode decision
// at sub-8x8). For BLOCK_4X4 the n_log2 == 4 path is still well-defined
// but tx_size would clamp to TX_4X4 — not exercised here.
func ModelRdForSbY(args ModelRdForSbYArgs) (outRateSum int, outDistSum int64,
	skipTxfm SkipTxfmFlag, txSize common.TxSize,
) {
	// libvpx: dc_thr = p->quant_thred[0] >> 6; ac_thr = p->quant_thred[1] >> 6;
	// quant_thred[i] = zbin[i]*zbin[i] (vp9_quantize.c:264-265).
	dcQuant := uint32(args.Dequant[0])
	acQuant := uint32(args.Dequant[1])
	dcThr, acThr := ModelRdQuantThresholds(args.QIndex, args.Dequant)

	txSize = CalculateTxSize(CalculateTxSizeArgs{
		BSize:           args.BSize,
		TxMode:          args.TxMode,
		VarY:            args.VarY,
		SSEY:            args.SSEY,
		ACThreshold:     acThr,
		SourceVariance:  args.SourceVariance,
		IsIntra:         args.IsIntra,
		CyclicRefreshAQ: args.CyclicRefreshAQ,
		SegmentID:       args.SegmentID,
		ScreenContent:   args.ScreenContent,
	})

	// libvpx: skippable test on the per-tx-unit (var_tx, sse_tx) — pre-
	// divided by num_blk via the num_blk_log2 shift. govpx mirrors the
	// shifts exactly.
	unitSize := common.TxsizeToBsize[txSize]
	numBlkLog2 := (common.BWidthLog2Lookup[args.BSize] - common.BWidthLog2Lookup[unitSize]) +
		(common.BHeightLog2Lookup[args.BSize] - common.BHeightLog2Lookup[unitSize])
	sseTx := args.SSEY >> uint(numBlkLog2)
	varTx := args.VarY >> uint(numBlkLog2)

	skipTxfm = SkipTxfmNone
	skipDc := false
	// libvpx vp9_pickmode.c:682-689 — ac quantizable to zero?
	if varTx < uint64(acThr) || args.VarY == 0 {
		skipTxfm = SkipTxfmAcOnly
		// dc quantizable to zero?
		if sseTx-varTx < uint64(dcThr) || args.SSEY == args.VarY {
			skipTxfm = SkipTxfmAcDc
		}
	} else if sseTx-varTx < uint64(dcThr) || args.SSEY == args.VarY {
		skipDc = true
	}

	if skipTxfm == SkipTxfmAcDc {
		// libvpx vp9_pickmode.c:692-696 — full Y skip.
		outRateSum = 0
		outDistSum = int64(args.SSEY << 4)
		return
	}

	nLog2 := uint(common.NumPelsLog2Lookup[args.BSize])
	if !skipDc {
		// libvpx: vp9_model_rd_from_var_lapndz(sse - var, n_log2, dc_quant >> 3, ...);
		dcRate, dcDist := ModelRDFromVarLapndz(uint32(args.SSEY-args.VarY), nLog2, dcQuant>>3)
		outRateSum = dcRate >> 1
		outDistSum = dcDist << 3
	} else {
		outRateSum = 0
		outDistSum = int64((args.SSEY - args.VarY) << 4)
	}

	// libvpx: vp9_model_rd_from_var_lapndz(var, n_log2, ac_quant >> 3, ...);
	acRate, acDist := ModelRDFromVarLapndz(uint32(args.VarY), nLog2, acQuant>>3)
	outRateSum += acRate
	outDistSum += acDist << 4
	return
}

func ModelRdQuantThresholds(qindex int, dequant [2]int16) (dcThr, acThr int64) {
	qzbinFactor := modelRdQzbinFactor(qindex)
	for i, dq := range dequant {
		dq64 := int64(dq)
		if dq64 <= 0 {
			continue
		}
		zbin := modelRdRoundPowerOfTwo(int64(qzbinFactor)*dq64, 7)
		thr := (zbin * zbin) >> 6
		if i == 0 {
			dcThr = thr
		} else {
			acThr = thr
		}
	}
	return dcThr, acThr
}

func modelRdQzbinFactor(qindex int) int {
	if qindex == 0 {
		return 64
	}
	if int(common.DcQuant(qindex, 0, common.Bits8)) < 148 {
		return 84
	}
	return 80
}

func modelRdRoundPowerOfTwo(value int64, n uint) int64 {
	return (value + int64(1)<<(n-1)) >> n
}

// EncodeBreakoutTest ports encode_breakout_test (vp9_pickmode.c:942-1045)
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
func EncodeBreakoutTest(bsize common.BlockSize, dequant [2]int16,
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
		threshAc = min(
			// thresh_ac = clamp(thresh_ac, min_thresh, max_thresh);
			max(

				uint64(int64(dequant[1])*int64(dequant[1]))>>3, minThresh), maxThresh)
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

// blockYrdMaxTxUnits bounds the per-call eob array. BLOCK_64X64 with
// TX_4X4 produces 256 tx units; libvpx clamps tx_size to TX_16X16 in the
// realtime nonrd path (vp9/encoder/vp9_pickmode.c:2361) so the actual cap
// is 16, but we size for the worst case so the function stays usable from
// future callers (e.g. the block_yrd at vp9_pickmode.c:1083 / 2610 which
// passes the un-clamped mi->tx_size).
const blockYrdMaxTxUnits = 256

// BlockYrdResult bundles the libvpx block_yrd outputs:
//   - rate, dist: refined (rate, dist) tuple after Hadamard + quantize_fp +
//     SATD scoring. Both in libvpx's prob-cost / shifted-distortion units
//     (rate is bits << VP9_PROB_COST_SHIFT, dist is sum-of-squared-error
//     in the shifted domain block_yrd produces — see the (*sse<<6)>>2
//     scale at vp9_pickmode.c:823, equivalent to sse << 4).
//   - skippable: true when every transform unit's eob came back as zero
//     (i.e. the Y residual quantizes entirely to zero); libvpx sets the
//     SKIP_TXFM_AC_DC bit and forces this_rdc.dist = sse in that case.
//   - sse: the shifted-domain sum-of-squares the picker compares against
//     RDCOST(0, sse) for the skip-vs-non-skip override.
type BlockYrdResult struct {
	Rate      int
	Dist      int64
	SSE       int64
	Skippable bool
	Valid     bool
}

// BlockYrd ports libvpx v1.16.0 block_yrd (vp9/encoder/vp9_pickmode.c:728-854)
// verbatim. It refines the (rate, dist) tuple model_rd_for_sb_y produces by
// running the actual Hadamard + quantize_fp + SATD path on the Y-plane
// residual, mirroring the realtime nonrd kernel that scores BLOCK_32X32+
// candidates after model_rd. Inputs:
//
//   - src/srcStride/srcX/srcY: source plane and the top-left pixel of the
//     bw×bh block in source space.
//   - dst/dstStride/dstX/dstY: prediction plane (the inter-predictor output
//     written by predictVP9InterBlock) and its top-left pixel. govpx's
//     pickVP9InterReferenceModeNonRD already calls
//     vp9InterPredictionVarianceSSE which fills the reconstruction plane;
//     this kernel reads from it directly so no extra prediction pass is
//     needed.
//   - bsize: the picker-decided block size (BLOCK_32X32 / BLOCK_64X32 /
//     BLOCK_32X64 / BLOCK_64X64).
//   - txSize: the transform size to score with. Realtime nonrd passes
//     min(mi->tx_size, TX_16X16) (vp9_pickmode.c:2361). The libvpx assert
//     at line 764 guarantees tx_size != TX_32X32 here.
//   - dequant: Y-plane (dc, ac) dequantizers (inter.dq.Y[segId]).
//   - sseIn: the (sse_y) value model_rd_for_sb_y produced; libvpx writes
//     this directly to *sse before scaling by ((*sse << 6) >> 2) and the
//     skippable-vs-non-skippable branch.
//   - residueScratch: int16 scratch the kernel uses for the bw*bh src_diff
//     buffer plus the per-tx coeff / qcoeff / dqcoeff. Caller-supplied so
//     we don't allocate per-candidate.
//
// libvpx flow (verbatim):
//
//	*skippable = 1;
//	subtract bw×bh src_diff = src - pred;
//	for each tx-unit (r, c) inside max_blocks_high × max_blocks_wide {
//	  hadamard(src_diff + (r*bw + c)<<2, bw, coeff);   // 4x4 / 8x8 / 16x16
//	  quantize_fp(coeff, n, qcoeff, dqcoeff, dequant, &eob, scan_order);
//	  skippable &= (eob == 0);
//	  eob_cost += 1;
//	}
//	*sse = (sseIn << 6) >> 2;            // = sseIn << 4
//	if (skippable) {
//	  this_rdc.dist = *sse;
//	  this_rdc.rate = (eob_cost << VP9_PROB_COST_SHIFT);    // post-shift below
//	  return;
//	}
//	this_rdc.dist = 0;
//	this_rdc.rate = 0;
//	for each tx-unit {
//	  rate += (eob == 1) ? abs(qcoeff[0]) : vpx_satd(qcoeff, n);
//	  dist += vp9_block_error_fp(coeff, dqcoeff, n) >> 2;
//	}
//	this_rdc.rate <<= (2 + VP9_PROB_COST_SHIFT);
//	this_rdc.rate += (eob_cost << VP9_PROB_COST_SHIFT);
//
// Limitations vs libvpx:
//
//   - High-bit-depth (CONFIG_VP9_HIGHBITDEPTH) is not handled; govpx is
//     8-bit only.
//   - The xd->mb_to_right_edge / mb_to_bottom_edge clamping is folded into
//     the caller's bw/bh (it always passes the visible block extents); a
//     follow-up port can surface the edge clamp explicitly if a fuzz seed
//     ever hits a sub-tile-edge inter block at BLOCK_32X32+.
func BlockYrd(src []byte, srcStride int, srcX, srcY int,
	dst []byte, dstStride int, dstX, dstY int,
	bw, bh int, txSize common.TxSize, dequant [2]int16,
	sseIn uint64, residueScratch []int16,
) BlockYrdResult {
	var res BlockYrdResult

	// libvpx asserts tx_size != TX_32X32 (vp9_pickmode.c:764). govpx clamps
	// at the call site (txSize is always min(mi->tx_size, TX_16X16)); fall
	// back rather than panic if a caller violates it.
	if txSize > common.Tx16x16 {
		return res
	}

	// libvpx: vp9_pickmode.c:736-737
	//   step = 1 << (tx_size << 1)            (libvpx walks block += step
	//                                          through p->coeff; govpx
	//                                          uses local per-tx slabs
	//                                          so step is implicit.)
	//   block_step = 1 << tx_size              (in 4x4 units)
	blockStep := 1 << uint(txSize) // in 4x4 units along one axis
	txDim := blockStep * 4         // tx unit pixel dimension
	nCoeffs := txDim * txDim       // 16/64/256
	num4x4W := bw >> 2
	num4x4H := bh >> 2
	maxBlocksWide := num4x4W
	maxBlocksHigh := num4x4H

	// libvpx: vp9_pickmode.c:771-777 — vpx_subtract_block fills p->src_diff
	// with src - dst (the prediction). govpx mirrors verbatim.
	srcDiffLen := bw * bh
	if len(residueScratch) < srcDiffLen {
		return res
	}
	srcDiff := residueScratch[:srcDiffLen]

	// Bounds-check the source/dst windows before reading. If the block runs
	// off the buffer (sub-frame-edge), the picker should be using a smaller
	// bw/bh — refuse rather than over-read.
	if srcX < 0 || srcY < 0 || dstX < 0 || dstY < 0 ||
		srcX+bw > srcStride || dstX+bw > dstStride {
		return res
	}
	if (srcY+bh)*srcStride > len(src) || (dstY+bh)*dstStride > len(dst) {
		return res
	}
	for y := range bh {
		srcRow := src[(srcY+y)*srcStride+srcX : (srcY+y)*srcStride+srcX+bw]
		dstRow := dst[(dstY+y)*dstStride+dstX : (dstY+y)*dstStride+dstX+bw]
		out := srcDiff[y*bw:]
		for x := range bw {
			out[x] = int16(int(srcRow[x]) - int(dstRow[x]))
		}
	}

	// libvpx: vp9_pickmode.c:784 scan_order = &vp9_default_scan_orders[tx_size]
	scanOrder := common.DefaultScanOrders[txSize]
	scan := scanOrder.Scan
	iscan := scanOrder.IScan

	// libvpx: vp9_quantize.c:209-210 — derive (round_fp, quant_fp) from dequant
	// using vp9_init_quantizer's recipe. nonrd_pickmode runs with
	// sf->use_quant_fp=1, so these are the same tables the realtime tokenizer
	// later consumes. qrounding_factor_fp == 64 at q==0; otherwise 48 (dc)
	// and 42 (ac). govpx routes through QuantizeFPLibvpx which
	// already takes (roundFP, quantFP, dequant) so we mirror the recipe
	// inline.
	var roundFP, quantFP [2]int16
	if int(dequant[0]) <= 0 || int(dequant[1]) <= 0 {
		return res
	}
	roundFP[0] = int16((48 * int(dequant[0])) >> 7)
	roundFP[1] = int16((42 * int(dequant[1])) >> 7)
	quantFP[0] = int16((1 << 16) / int(dequant[0]))
	quantFP[1] = int16((1 << 16) / int(dequant[1]))

	// libvpx: vp9_pickmode.c:778 *skippable = 1; (set true initially)
	skippable := true
	eobCost := 0

	// Tx-unit pointer scratch: track per-tx eob/coeff/qcoeff/dqcoeff so the
	// second pass can re-use them for the SATD / block_error_fp accumulation
	// without re-running Hadamard+quantize.
	maxTxUnits := (num4x4W / blockStep) * (num4x4H / blockStep)
	if maxTxUnits <= 0 {
		return res
	}
	// Per-tx scratch slabs: each transform writes nCoeffs entries. The
	// caller's residueScratch is sized for the realtime worst case
	// (BLOCK_64X64 + TX_16X16 = 4096 + 16 × 256 × 3 = 16384). For TX_8X8
	// at BLOCK_64X64 we have 64 tx units × 64 × 3 = 12288 + 4096 = 16384;
	// for TX_4X4 it would be 256 × 16 × 3 + 4096 = 16384 — all fits.
	if len(residueScratch) < srcDiffLen+3*nCoeffs*maxTxUnits {
		return res
	}
	perTxBase := srcDiffLen
	coeffsAll := residueScratch[perTxBase : perTxBase+nCoeffs*maxTxUnits]
	qcoeffAll := residueScratch[perTxBase+nCoeffs*maxTxUnits : perTxBase+2*nCoeffs*maxTxUnits]
	dqcoeffAll := residueScratch[perTxBase+2*nCoeffs*maxTxUnits : perTxBase+3*nCoeffs*maxTxUnits]

	// First pass: Hadamard / fdct4x4 + quantize_fp. libvpx:
	// vp9_pickmode.c:781-819. The eobs cap is bounded by the realtime
	// schedule: BLOCK_64X64 + TX_4X4 -> 256 tx units, which fits the
	// blockYrdMaxTxUnits ceiling below. govpx clamps tx_size <=
	// TX_16X16 (vp9_pickmode.c:2361) so the realistic max is 16.
	txIdx := 0
	var eobsBuf [blockYrdMaxTxUnits]int
	if maxTxUnits > len(eobsBuf) {
		return res
	}
	_ = eobsBuf
	for r := 0; r < maxBlocksHigh; r += blockStep {
		for c := 0; c < num4x4W; c += blockStep {
			if c >= maxBlocksWide {
				continue
			}
			// libvpx: vp9_pickmode.c:791
			//   src_diff = &p->src_diff[(r * diff_stride + c) << 2];
			// diff_stride == bw (== num_4x4_w * 4).
			srcOff := (r*bw + c) << 2
			coeffSlot := coeffsAll[txIdx*nCoeffs : (txIdx+1)*nCoeffs]
			qcoeffSlot := qcoeffAll[txIdx*nCoeffs : (txIdx+1)*nCoeffs]
			dqcoeffSlot := dqcoeffAll[txIdx*nCoeffs : (txIdx+1)*nCoeffs]

			// libvpx: vp9_pickmode.c:796-813 — Hadamard dispatch by tx_size.
			switch txSize {
			case common.Tx16x16:
				hadamard16x16Into(srcDiff[srcOff:], bw, coeffSlot)
			case common.Tx8x8:
				hadamard8x8Into(srcDiff[srcOff:], bw, coeffSlot)
			default:
				// libvpx: vp9_pickmode.c:809 — x->fwd_txfm4x4 is the forward
				// 4x4 DCT (vpx_fdct4x4). govpx's ForwardDCT4x4Into
				// is the verbatim libvpx port.
				ForwardDCT4x4Into(srcDiff[srcOff:], bw, coeffSlot)
			}
			// libvpx: vp9_pickmode.c:799-811 — vp9_quantize_fp_c.
			eob := QuantizeFPLibvpx(coeffSlot, nCoeffs, roundFP, quantFP,
				dequant, scan, iscan, qcoeffSlot, dqcoeffSlot)
			eobsBuf[txIdx] = eob
			// libvpx: vp9_pickmode.c:814 *skippable &= (*eob == 0);
			if eob != 0 {
				skippable = false
			}
			eobCost++
			txIdx++
		}
	}

	// libvpx: vp9_pickmode.c:822-828 — *sse = (sseIn << 6) >> 2 only
	// when the caller provided a finite SSE. The nonrd keyframe intra
	// picker passes INT64_MAX, which intentionally bypasses the early
	// skippable return so the caller can clobber the final rate itself.
	sseKnown := sseIn < BlockYrdUnknownSSE
	if sseKnown {
		res.SSE = int64(sseIn << 4) // (<<6)>>2 == <<4
	}

	if skippable && sseKnown {
		// libvpx: vp9_pickmode.c:821 sets `this_rdc->rate = 0;` then the
		// skippable branch (vp9_pickmode.c:824-826) returns BEFORE the
		// non-skippable second-pass rate accumulation and BEFORE the
		// `this_rdc->rate <<= (2 + VP9_PROB_COST_SHIFT); this_rdc->rate +=
		// (eob_cost << VP9_PROB_COST_SHIFT);` finalization at lines
		// 852-853. The "If skippable is set, rate gets clobbered later"
		// comment at line 851 refers to the caller (vp9_pickmode.c:2364
		// overwrites this_rdc.rate with vp9_cost_bit(skip_prob, 1)). So
		// block_yrd's output on the skippable path is rate=0, dist=*sse.
		res.Dist = res.SSE
		res.Skippable = true
		res.Rate = 0
		_ = eobCost // unused on the skippable branch — see libvpx note above.
		res.Valid = true
		return res
	}

	// Second pass: SATD + block_error_fp. libvpx: vp9_pickmode.c:830-849.
	var rate int
	var dist int64
	for i := 0; i < txIdx; i++ {
		coeffSlot := coeffsAll[i*nCoeffs : (i+1)*nCoeffs]
		qcoeffSlot := qcoeffAll[i*nCoeffs : (i+1)*nCoeffs]
		dqcoeffSlot := dqcoeffAll[i*nCoeffs : (i+1)*nCoeffs]
		eob := eobsBuf[i]

		// libvpx: vp9_pickmode.c:840-843 — rate accumulation.
		if eob == 1 {
			// abs(qcoeff[0]).
			q0 := int(qcoeffSlot[0])
			if q0 < 0 {
				q0 = -q0
			}
			rate += q0
		} else if eob > 1 {
			// vpx_satd over n coefficients.
			satd := 0
			for j := range nCoeffs {
				q := int(qcoeffSlot[j])
				if q < 0 {
					q = -q
				}
				satd += q
			}
			rate += satd
		}

		// libvpx: vp9_pickmode.c:845 — vp9_block_error_fp(coeff, dqcoeff, n) >> 2.
		// The >> 2 is caller-side (the helper itself returns the raw
		// sum-of-squared-diffs — libvpx vp9_rdopt.c:334-345).
		dist += int64(BlockErrorFP(coeffSlot, dqcoeffSlot)) >> 2
	}

	// libvpx: vp9_pickmode.c:852-853 — final rate scaling.
	//   this_rdc.rate <<= (2 + VP9_PROB_COST_SHIFT);
	//   this_rdc.rate += (eob_cost << VP9_PROB_COST_SHIFT);
	res.Rate = (rate << (2 + VP9ProbCostShift)) +
		(eobCost << VP9ProbCostShift)
	res.Dist = dist
	res.Skippable = skippable
	res.Valid = true
	return res
}

// BlockErrorFP ports libvpx vp9_rdopt.c:334-345 verbatim:
//
//	int64_t vp9_block_error_fp_c(const tran_low_t *coeff,
//	                             const tran_low_t *dqcoeff,
//	                             int block_size) {
//	  int i;
//	  int64_t error = 0;
//	  for (i = 0; i < block_size; i++) {
//	    const int diff = coeff[i] - dqcoeff[i];
//	    error += diff * diff;
//	  }
//	  return error;
//	}
//
// In libvpx's 8-bit build tran_low_t is int16_t (vpx_dsp_common.h:45), so
// diff fits in int and diff*diff fits in int. The accumulator is int64_t
// for headroom against 1024-coeff TX_32X32 blocks. govpx is 8-bit only
// (no CONFIG_VP9_HIGHBITDEPTH path) so int16 + int (== int64 on 64-bit
// Go) preserves identical semantics. The return type is uint64 because
// the sum of squared diffs is non-negative; the caller (block_yrd second
// pass, libvpx vp9_pickmode.c:845) widens to int64 and applies >> 2
// itself — that shift is NOT part of this helper.
//
// Caller currently in govpx: vp9_block_yrd.go second-pass loop. The
// historical scoreVP9KeyframeTxBlockRD caller (vp9_encoder.go) was
// removed when the keyframe RD picker switched to cost_coeffs (commit
// a2f325c); this helper remains as the verbatim block_yrd dependency.
func BlockErrorFP(coeff, dqcoeff []int16) uint64 {
	n := min(len(coeff), len(dqcoeff))
	if n == 16 {
		// Keep the TX_4X4 hot path in the exported wrapper so the
		// dispatch indirection used for larger blocks cannot regress it.
		d0 := int(coeff[0]) - int(dqcoeff[0])
		d1 := int(coeff[1]) - int(dqcoeff[1])
		d2 := int(coeff[2]) - int(dqcoeff[2])
		d3 := int(coeff[3]) - int(dqcoeff[3])
		d4 := int(coeff[4]) - int(dqcoeff[4])
		d5 := int(coeff[5]) - int(dqcoeff[5])
		d6 := int(coeff[6]) - int(dqcoeff[6])
		d7 := int(coeff[7]) - int(dqcoeff[7])
		d8 := int(coeff[8]) - int(dqcoeff[8])
		d9 := int(coeff[9]) - int(dqcoeff[9])
		d10 := int(coeff[10]) - int(dqcoeff[10])
		d11 := int(coeff[11]) - int(dqcoeff[11])
		d12 := int(coeff[12]) - int(dqcoeff[12])
		d13 := int(coeff[13]) - int(dqcoeff[13])
		d14 := int(coeff[14]) - int(dqcoeff[14])
		d15 := int(coeff[15]) - int(dqcoeff[15])
		return uint64(d0*d0) + uint64(d1*d1) +
			uint64(d2*d2) + uint64(d3*d3) +
			uint64(d4*d4) + uint64(d5*d5) +
			uint64(d6*d6) + uint64(d7*d7) +
			uint64(d8*d8) + uint64(d9*d9) +
			uint64(d10*d10) + uint64(d11*d11) +
			uint64(d12*d12) + uint64(d13*d13) +
			uint64(d14*d14) + uint64(d15*d15)
	}
	return blockErrorFPDispatch(coeff, dqcoeff, n)
}

func blockErrorFPScalar(coeff, dqcoeff []int16, n int) uint64 {
	var err uint64
	if n == 16 {
		// TX_4X4 blocks are common enough that avoiding loop overhead matters.
		d0 := int(coeff[0]) - int(dqcoeff[0])
		d1 := int(coeff[1]) - int(dqcoeff[1])
		d2 := int(coeff[2]) - int(dqcoeff[2])
		d3 := int(coeff[3]) - int(dqcoeff[3])
		d4 := int(coeff[4]) - int(dqcoeff[4])
		d5 := int(coeff[5]) - int(dqcoeff[5])
		d6 := int(coeff[6]) - int(dqcoeff[6])
		d7 := int(coeff[7]) - int(dqcoeff[7])
		d8 := int(coeff[8]) - int(dqcoeff[8])
		d9 := int(coeff[9]) - int(dqcoeff[9])
		d10 := int(coeff[10]) - int(dqcoeff[10])
		d11 := int(coeff[11]) - int(dqcoeff[11])
		d12 := int(coeff[12]) - int(dqcoeff[12])
		d13 := int(coeff[13]) - int(dqcoeff[13])
		d14 := int(coeff[14]) - int(dqcoeff[14])
		d15 := int(coeff[15]) - int(dqcoeff[15])
		return uint64(d0*d0) + uint64(d1*d1) +
			uint64(d2*d2) + uint64(d3*d3) +
			uint64(d4*d4) + uint64(d5*d5) +
			uint64(d6*d6) + uint64(d7*d7) +
			uint64(d8*d8) + uint64(d9*d9) +
			uint64(d10*d10) + uint64(d11*d11) +
			uint64(d12*d12) + uint64(d13*d13) +
			uint64(d14*d14) + uint64(d15*d15)
	}
	i := 0
	for ; i+7 < n; i += 8 {
		d0 := int(coeff[i+0]) - int(dqcoeff[i+0])
		d1 := int(coeff[i+1]) - int(dqcoeff[i+1])
		d2 := int(coeff[i+2]) - int(dqcoeff[i+2])
		d3 := int(coeff[i+3]) - int(dqcoeff[i+3])
		d4 := int(coeff[i+4]) - int(dqcoeff[i+4])
		d5 := int(coeff[i+5]) - int(dqcoeff[i+5])
		d6 := int(coeff[i+6]) - int(dqcoeff[i+6])
		d7 := int(coeff[i+7]) - int(dqcoeff[i+7])
		err += uint64(d0*d0) + uint64(d1*d1) +
			uint64(d2*d2) + uint64(d3*d3) +
			uint64(d4*d4) + uint64(d5*d5) +
			uint64(d6*d6) + uint64(d7*d7)
	}
	for ; i < n; i++ {
		diff := int(coeff[i]) - int(dqcoeff[i])
		err += uint64(diff * diff)
	}
	return err
}
