package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// vp9_fullrd_inter_sub8x8_segment.go ports encode_inter_mb_segment
// (vp9/encoder/vp9_rdopt.c:1608-1734): the per-label residual RD for the
// sub-8x8 joint search. For label i it builds the inter predictor over the
// label's pw×ph footprint with mi.bmi[i].as_mv[0], subtracts the source, and
// walks the footprint in 4x4 transform units running fdct4x4 → vpx_quantize_b →
// vp9_block_error → cost_coeffs, accumulating thisrate/thisdistortion/thissse
// with the running this_rd early-exit. Distortion and sse are reported >> 2
// (vp9_rdopt.c:1729-1731).

// vp9Sub8x8SegmentEntropy holds the t_above[2]/t_left[2] entropy context the
// sub-8x8 segment carries from label to label (vp9_rdopt.c:2103,2120-2121,
// 2372-2373). It is the 8x8 block's 2-wide/2-high above/left context.
type vp9Sub8x8SegmentEntropy struct {
	above [2]uint8
	left  [2]uint8
}

// vp9Sub8x8SegmentRD is encode_inter_mb_segment's output for one label.
type vp9Sub8x8SegmentRD struct {
	byrate int    // *labelyrate (cost_coeffs sum)
	dist   uint64 // *distortion (thisdistortion >> 2)
	sse    uint64 // *sse (thissse >> 2)
	rdcost uint64 // RDCOST(rdmult, rddiv, labelyrate, distortion)
	eob    int    // p->eobs[block] (the label's first 4x4 eob, for the probe)
	anyEob bool   // any 4x4 unit in the label had eob > 0 (→ not skippable)
}

// encodeInterMbSegment ports encode_inter_mb_segment (vp9_rdopt.c:1608-1734)
// for the single-reference Y plane. mi.bmi[block].as_mv[0] is the label MV
// (already written by set_and_cost_bmi_mvs). ent is updated in place. The
// returned rdcost is RDCOST(labelyrate, distortion); the caller adds the
// mode/MV-rate RDCOST and the early-exit budget is bestYrd.
func (e *VP9Encoder) encodeInterMbSegment(inter *vp9InterEncodeState,
	in vp9Sub8x8Input, ent *vp9Sub8x8SegmentEntropy, bsize common.BlockSize,
	block int, refFrame int8, mi *vp9dec.NeighborMi, bestYrd uint64,
) (vp9Sub8x8SegmentRD, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return vp9Sub8x8SegmentRD{}, false
	}
	// plane_bsize width/height = label footprint pw×ph (vp9_rdopt.c:1618-1620).
	width := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	height := int(common.Num4x4BlocksHighLookup[bsize]) * 4

	// Sub-block raster position inside the 8x8 (vp9_rdopt.c:1636-1638):
	// b_width_log2_lookup[BLOCK_8X8] == 1 → h = 4*(i>>1), w = 4*(i&1).
	x0Block := in.miCol*common.MiSize + 4*(block&1)
	y0Block := in.miRow*common.MiSize + 4*(block>>1)

	pre, preStride, preOriginX, preOriginY, _, _, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if !refOK || len(pre) == 0 || preStride <= 0 {
		return vp9Sub8x8SegmentRD{}, false
	}
	mv := mi.Bmi[block].AsMv[0]
	filterIdx := int(mi.InterpFilter)
	if filterIdx < 0 || filterIdx >= int(vp9dec.InterpSwitchable) {
		return vp9Sub8x8SegmentRD{}, false
	}
	predBuf := e.vp9Sub8x8PredScratch(width * height)
	if predBuf == nil {
		return vp9Sub8x8SegmentRD{}, false
	}
	// vp9_build_inter_predictor with MV_PRECISION_Q3 (vp9_reconinter.c:
	// mv_q4 = mv*2; subpel = mv_q4 & SUBPEL_MASK(15); integer = mv_q4 >> 4).
	// encode_inter_mb_segment calls it directly with the raw (unclamped) MV.
	mvQ4Col := int(mv.Col) * 2
	mvQ4Row := int(mv.Row) * 2
	preX := x0Block + (mvQ4Col >> vp9dec.SubpelBitsConst)
	preY := y0Block + (mvQ4Row >> vp9dec.SubpelBitsConst)
	bufX := preOriginX + preX
	bufY := preOriginY + preY
	preRows := len(pre) / preStride
	if bufX < 0 || bufY < 0 || bufX+width+1 > preStride || bufY+height+1 > preRows {
		return vp9Sub8x8SegmentRD{}, false
	}
	preOff := bufY*preStride + bufX
	subpelX := mvQ4Col & (vp9dec.SubpelShifts - 1)
	subpelY := mvQ4Row & (vp9dec.SubpelShifts - 1)
	vp9dec.InterPredictor(pre, preStride, predBuf, width, subpelX, subpelY,
		tables.FilterKernels[filterIdx], vp9dec.SubpelShifts, vp9dec.SubpelShifts,
		width, height, 0, preOff)

	dequant := inter.dq.Y[0]
	qindex := inter.baseQindex
	scan := common.DefaultScanOrders[common.Tx4x4]

	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	var diff [16]int16

	var thisrate int
	var thisdistortion uint64
	var thissse uint64
	firstEob := 0
	anyEob := false

	w4 := width / 4
	h4 := height / 4
	// k accumulates exactly as libvpx (vp9_rdopt.c:1689-1699).
	k := block
	for idy := 0; idy < h4; idy++ {
		for idx := 0; idx < w4; idx++ {
			k += idy*2 + idx
			col := k & 1
			row := k >> 1
			// combine_entropy_contexts(ta[k&1], tl[k>>1]) for TX_4X4
			// (vp9_rdopt.c:1700): (above!=0) + (left!=0).
			coeffCtx := vp9dec.GetEntropyContext(common.Tx4x4,
				ent.above[col:col+1], ent.left[row:row+1])

			ux := x0Block + 4*idx
			uy := y0Block + 4*idy
			if !vp9Sub8x8GatherResidual(src, srcStride, srcW, srcH, predBuf, width,
				ux, uy, 4*idx, 4*idy, diff[:]) {
				return vp9Sub8x8SegmentRD{}, false
			}
			encoder.ForwardDCT4x4Into(diff[:], 4, coeff[:])
			eob := encoder.QuantizeBWithQ(coeff[:], qindex, dequant, scan.Scan,
				qcoeff[:], dqcoeff[:])

			// vp9_block_error: error = sum(coeff-dqcoeff)^2, ssz = sum(coeff^2).
			thisdistortion += encoder.BlockErrorFP(coeff[:], dqcoeff[:])
			thissse += vp9Sub8x8SumSquares(coeff[:])

			thisrate += e.vp9InterCoeffBlockRateCostQ(common.Tx4x4, 0, dequant,
				coeff[:], qcoeff[:], coeffCtx)

			hasCtx := uint8(0)
			if eob > 0 {
				hasCtx = 1
				anyEob = true
			}
			ent.above[col] = hasCtx
			ent.left[row] = hasCtx
			if idy == 0 && idx == 0 {
				firstEob = eob
			}

			// rd1 = RDCOST(rate, dist>>2); rd2 = RDCOST(0, sse>>2);
			// rd = min(rd1, rd2); if rd >= best_yrd return INT64_MAX.
			rd1 := encoder.RDCost(in.rdmult, encoder.RDDivBits, thisrate,
				thisdistortion>>2)
			rd2 := encoder.RDCost(in.rdmult, encoder.RDDivBits, 0, thissse>>2)
			rd := rd1
			if rd2 < rd {
				rd = rd2
			}
			if bestYrd != rdCostMaxLocal && rd >= bestYrd {
				return vp9Sub8x8SegmentRD{}, false
			}
		}
	}

	dist := thisdistortion >> 2
	sse := thissse >> 2
	return vp9Sub8x8SegmentRD{
		byrate: thisrate,
		dist:   dist,
		sse:    sse,
		rdcost: encoder.RDCost(in.rdmult, encoder.RDDivBits, thisrate, dist),
		eob:    firstEob,
		anyEob: anyEob,
	}, true
}

// vp9Sub8x8SumSquares returns sum(coeff[i]^2) (the ssz term of vp9_block_error,
// vp9_rdopt.c:327).
func vp9Sub8x8SumSquares(coeff []int16) uint64 {
	var s uint64
	for _, c := range coeff[:min(16, len(coeff))] {
		s += uint64(int64(c) * int64(c))
	}
	return s
}

// vp9Sub8x8GatherResidual writes the (src - predictor) 4x4 residual for one
// transform unit into diff (raster, stride 4).
func vp9Sub8x8GatherResidual(src []byte, srcStride, srcW, srcH int,
	predBuf []byte, predStride, srcX, srcY, predX, predY int, diff []int16,
) bool {
	if srcX < 0 || srcY < 0 || srcX+4 > srcW || srcY+4 > srcH {
		return false
	}
	if predX < 0 || predY < 0 || predX+4 > predStride ||
		(predY+4)*predStride > len(predBuf) {
		return false
	}
	for y := 0; y < 4; y++ {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		predRow := predBuf[(predY+y)*predStride+predX:]
		dRow := diff[y*4:]
		for x := 0; x < 4; x++ {
			dRow[x] = int16(int(srcRow[x]) - int(predRow[x]))
		}
	}
	return true
}

// vp9Sub8x8PredScratch returns a reusable byte scratch buffer for the label
// predictor.
func (e *VP9Encoder) vp9Sub8x8PredScratch(n int) []byte {
	if n <= 0 {
		return nil
	}
	if cap(e.sub8x8PredScratch) < n {
		e.sub8x8PredScratch = make([]byte, n)
	}
	return e.sub8x8PredScratch[:n]
}

// vp9Sub8x8SeedEntropy seeds the t_above[2]/t_left[2] segment entropy context
// from the 8x8 block's plane[0] above/left context (vp9_rdopt.c:2120-2121).
func (e *VP9Encoder) vp9Sub8x8SeedEntropy(ent *vp9Sub8x8SegmentEntropy,
	miRow, miCol int,
) {
	pd := &e.planes[0]
	aboveOff, leftOff := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	if ao := aboveOff[0]; ao >= 0 && ao+2 <= len(pd.AboveContext) {
		ent.above[0] = pd.AboveContext[ao]
		ent.above[1] = pd.AboveContext[ao+1]
	}
	if lo := leftOff[0]; lo >= 0 && lo+2 <= len(pd.LeftContext) {
		ent.left[0] = pd.LeftContext[lo]
		ent.left[1] = pd.LeftContext[lo+1]
	}
}

// vp9Sub8x8StampEntropy writes the committed 8x8 block's segment entropy context
// back into the plane[0] above/left context. This is the entropy-context half of
// libvpx's encode_sb / encode_superblock → vp9_foreach_transformed_block →
// vp9_set_contexts (vp9/encoder/vp9_encodeframe.c:4163-4166, the split children
// with pc_tree->index != 3): after a sub-8x8 leaf is committed during the SPLIT
// recursion, its plane entropy context is stamped so the next sibling 8x8's
// rd_pick_best_sub8x8_mode seed (memcpy(t_above, pd->above_context),
// vp9_rdopt.c:2120-2121) reads it. ent.above[0..1]/left[0..1] are the running
// t_above[2]/t_left[2] at segment end (vp9_rdopt.c:2398-2399).
func (e *VP9Encoder) vp9Sub8x8StampEntropy(ent *vp9Sub8x8SegmentEntropy,
	miRow, miCol int,
) {
	pd := &e.planes[0]
	aboveOff, leftOff := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	if ao := aboveOff[0]; ao >= 0 && ao+2 <= len(pd.AboveContext) {
		pd.AboveContext[ao] = ent.above[0]
		pd.AboveContext[ao+1] = ent.above[1]
	}
	if lo := leftOff[0]; lo >= 0 && lo+2 <= len(pd.LeftContext) {
		pd.LeftContext[lo] = ent.left[0]
		pd.LeftContext[lo+1] = ent.left[1]
	}
}
