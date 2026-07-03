package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9InterZcoeffBlk holds the per-4x4-block (raster, blockIdx-by-step^2)
// zcoeff_blk decision for a committed inter leaf's Y plane: when an entry is
// true the writer forces that transform unit's eob to 0 (no tokens), mirroring
// libvpx encode_block (vp9/encoder/vp9_encodemb.c:580-588) consuming the
// x->zcoeff_blk[tx_size][block] flag the full-RD mode search committed.
type vp9InterZcoeffBlk struct {
	valid bool
	// flags is indexed by the Y-plane 4x4 raster index (rr*full4x4W+cc), the
	// same layout the writer's residual loop walks. Only the top-left 4x4 of
	// each transform unit is consulted.
	flags [256]bool
}

// vp9ComputeInterLeafZcoeffBlk reproduces libvpx block_rd_txfm's per-transform
// -block x->zcoeff_blk decision (vp9/encoder/vp9_rdopt.c:826-837) for the
// COMMITTED inter Y leaf at (miRow, miCol) and tx_size txSize. It is the
// commit-time replay of the same per-block rd1/rd2 the full-RD super_block_yrd
// producer (vp9FullRDInterYPlaneTxCandidate) already computes during the mode
// search:
//
//	rd1 = RDCOST(rdmult, rddiv, rate, dist)   // cost of coding the residual
//	rd2 = RDCOST(rdmult, rddiv, 0,    sse)    // cost of skipping it
//	zcoeff_blk[tx][block] = !eob || (sharpness == 0 && rd1 > rd2 && !lossless)
//
// libvpx commits zcoeff_blk into ctx during the search (vp9_rdopt.c:835, 4008)
// and replays it at encode time (update_state vp9_encodeframe.c:1846 →
// encode_block vp9_encodemb.c:580). govpx's writer instead recomputes it here
// from the SAME predictor the committed mode produced (already on the recon
// plane after predictVP9InterBlock), running the REGULAR quantizer
// (vp9_xform_quant) at the segment qindex exactly as block_rd_txfm. The
// predictor rect is snapshotted and restored so the writer's subsequent
// vp9_quantize_fp tokenize pass starts from the same predictor libvpx's
// encode_superblock rebuilds.
//
// Gated by the caller behind the deep full-RD use-partition flag; production
// (flag off) never calls it.
func (e *VP9Encoder) vp9ComputeInterLeafZcoeffBlk(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, txSize common.TxSize,
	segID uint8,
) (vp9InterZcoeffBlk, bool) {
	var out vp9InterZcoeffBlk
	if inter == nil || inter.dq == nil || inter.lossless {
		// lossless never zero-forces (the !xd->lossless guard); treat as no-op.
		return out, false
	}
	if e.opts.Sharpness != 0 {
		// libvpx's RD term `x->sharpness == 0` gates the rd1>rd2 force; with
		// sharpness != 0 the only zcoeff is !eob, which the FP-quant tokenize
		// already yields (eob==0 => no tokens), so the bitmap is a no-op.
		return out, false
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return out, false
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return out, false
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return out, false
	}
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
		bsize, pd, planeBsize)
	full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if maxEob > len(e.coefScratch) || bs*bs > len(e.residueScratch) {
		return out, false
	}
	if full4x4W*int(common.Num4x4BlocksHighLookup[planeBsize]) > len(out.flags) {
		return out, false
	}
	dequant := inter.dq.Y[segID]
	qindex := e.vp9SegmentQIndex(inter, segID)
	// rdmult mirrors the value the full-RD mode search committed the leaf with:
	// baseRdmult = e.rc.rdmult (the per-frame rd->RDMULT,
	// vp9_encoder_inter_modes.go:1799), falling back to
	// ComputeRDMultBasedOnQindex when rc.rdmult is unset. e.cbRdmult is reset to
	// 0 at the frame boundary (vp9_encoder_rd.go:141) so it cannot be used at
	// write time. TPL is off on the realtime cpu4 path, so no per-SB delta
	// applies. libvpx ref: x->rdmult feeding block_rd_txfm's RDCOST.
	rdmult := e.rc.rdmult
	if rdmult <= 0 {
		rdmult = encoder.ComputeRDMultBasedOnQindex(
			e.vp9EncoderModeDecisionQIndex(), encoder.RDFrameInter)
	}
	if rdmult <= 0 {
		rdmult = 1
	}
	// block_rd_txfm distortion domain (vp9_encodeframe.c:2041-2048); cpu4 forces
	// transform domain. The zcoeff rd1/rd2 must use the same domain block_rd_txfm
	// did when it set x->zcoeff_blk.
	useTxDomain := vp9InterUseDeepRDTxDomainDistortion &&
		e.vp9InterUseTransformDomainDistortion(inter, miRows, miCols, miRow, miCol,
			bsize)

	// Snapshot the predictor rect so the regular-quant inverse-add below can be
	// undone before the FP-quant tokenize pass (which re-adds the residual of
	// the surviving blocks).
	restoreW := full4x4W * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 || restoreW*restoreH > len(e.blockScratch) {
		return out, false
	}
	predictor := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(predictor[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}

	// Entropy contexts seed t_above/t_left exactly as vp9_get_entropy_contexts
	// (vp9_rdopt.c:872), the same seeding the producer uses.
	var aboveCtx [16]uint8
	var leftCtx [16]uint8
	aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
	leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
	if aboveLen <= len(aboveCtx) && leftLen <= len(leftCtx) &&
		len(pd.AboveContext) > 0 && len(pd.LeftContext) > 0 {
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if off := aboveOffsets[0]; off >= 0 && off+aboveLen <= len(pd.AboveContext) {
			copy(aboveCtx[:aboveLen], pd.AboveContext[off:off+aboveLen])
		}
		if off := leftOffsets[0]; off >= 0 && off+leftLen <= len(pd.LeftContext) {
			copy(leftCtx[:leftLen], pd.LeftContext[off:off+leftLen])
		}
	}

	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			coeffs := e.coefScratch[:maxEob]
			qcoeffs := e.qCoefScratch[:maxEob]
			for i := range coeffs {
				coeffs[i] = 0
				qcoeffs[i] = 0
			}
			initCtx := vp9dec.GetEntropyContextFull(txSize,
				aboveCtx[cc:cc+step], leftCtx[rr:rr+step])
			// Regular quantizer (vp9_xform_quant), segment qindex, inverse-add
			// into recon — exactly block_rd_txfm (vp9_rdopt.c:792-795).
			eob := e.prepareVP9InterTxResidueFullRD(inter, pd, txSize,
				miRow, miCol, rr, cc, dequant, qindex, initCtx, coeffs, qcoeffs)
			hasResidue := eob > 0

			// rd1/rd2 must consume the SAME distortion domain block_rd_txfm used
			// when it set zcoeff_blk. For cpu4 block_tx_domain==1 (transform
			// domain); otherwise pixel domain. Mirror the Y-producer choice so the
			// rd1>rd2 boundary matches libvpx (over-zeroing otherwise, e.g.
			// {0,1,1,0,1} frame-1 mi(7,5) blk0).
			var blockDist uint64
			var blockSSE uint64
			if useTxDomain && hasResidue {
				blockDist = encoder.TransformBlockError(e.txCoeffScratch[:maxEob],
					e.dqCoeffScratch[:maxEob], txSize)
				blockSSE = encoder.TransformBlockEnergy(e.txCoeffScratch[:maxEob],
					txSize)
			} else {
				bd, distOK := vp9FullRDInterTxBlockPixelSSE(src, srcStride,
					srcW, srcH, planeData, stride, baseX+cc*4, baseY+rr*4, bs)
				if !distOK {
					// Restore the predictor and bail; caller falls back to no-op.
					encoder.RestorePlaneRect(planeData, stride, baseX, baseY,
						restoreW, restoreH, predictor)
					return vp9InterZcoeffBlk{}, false
				}
				blockDist = bd * 16
				blockSSE = encoder.ResidualSSE(e.residueScratch[:bs*bs]) * 16
			}
			// cost_coeffs MUST use the INITIAL (pre-compressed-header-update)
			// coef probs the full-RD search built its token cost tables from —
			// libvpx fills x->token_costs from cm->fc.coef_probs at frame start
			// (fill_token_costs in vp9_initialize_rd_consts) and the zcoeff
			// decision (block_rd_txfm) consumes those. The compressed-header
			// write later applies vp9_cond_prob_diff_update deltas into e.fc, so
			// reading e.fc here would use the post-delta probs and flip the
			// rd1>rd2 boundary on the write pass. inter.selectFc is the snapshot
			// of e.fc at frame start (vp9_encoder_frame.go:449), unchanged by the
			// header update, so it gives the search-time probs in BOTH the count
			// pre-pass and the write pass.
			blockRate := e.vp9InterCoeffBlockRateCostQFcWithCosts(&inter.selectFc,
				e.vp9CoeffTokenCostTable(txSize, 0, 1), txSize, 0,
				dequant, coeffs, qcoeffs, initCtx, eob, true)

			rd1 := encoder.RDCost(rdmult, encoder.RDDivBits, blockRate, blockDist)
			rd2 := encoder.RDCost(rdmult, encoder.RDDivBits, 0, blockSSE)
			// vp9_rdopt.c:835 — zcoeff_blk = !eob || (sharpness==0 && rd1>rd2 &&
			// !lossless). sharpness==0 and !lossless already gated above.
			zb := !hasResidue || rd1 > rd2
			idx := rr*full4x4W + cc
			if idx >= 0 && idx < len(out.flags) {
				out.flags[idx] = zb
			}

			hasCtx := uint8(0)
			if hasResidue {
				hasCtx = 1
			}
			for i := 0; i < step && cc+i < aboveLen; i++ {
				aboveCtx[cc+i] = hasCtx
			}
			for i := 0; i < step && rr+i < leftLen; i++ {
				leftCtx[rr+i] = hasCtx
			}
		}
	}

	encoder.RestorePlaneRect(planeData, stride, baseX, baseY,
		restoreW, restoreH, predictor)
	out.valid = true
	return out, true
}
