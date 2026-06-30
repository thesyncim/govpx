package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/arith"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// vp9EncoderBestInterRefMvs returns the per-ref MV that the bitstream MV coder
// (WriteInterBlock / countVP9NewMv) differences each NEWMV against. libvpx's
// pack_inter_mode_mvs writes BOTH a whole-block NEWMV and every sub-8x8 NEWMV
// sub-block against the block-level NEAREST candidate
// x->mbmi_ext->ref_mvs[ref_frame][0] (vp9/encoder/vp9_bitstream.c:328-330 for
// the sub-8x8 idx loop and :337-339 for the whole-block NEWMV) — i.e. always
// the index-[0] (NEAREST) candidate of vp9_find_mv_refs, irrespective of the
// committed block mode. The decoder mirrors this exactly: for a NEWMV sub-block
// it seeds best_ref_mvs from dec_find_mv_refs(NEWMV, sb_type, block=-1)[0]
// (vp9/decoder/vp9_decodemv.c:748-752, reproduced in vp9_decoder_modes.go:607-616),
// and for a whole-block NEWMV from ref_mvs[ref][refmv_count-1==0 for NEWMV's
// early-break] (vp9_decodemv.c:776-781).
//
// Therefore the write/count reference MV must be computed with mode == NEWMV
// (the index-[0] NEAREST), NOT with the committed block mode mi.Mode. Passing
// mi.Mode was wrong for a sub-8x8 leaf whose block-level mode is NEARMV
// (mi.Mode == bmi[3].as_mode): InterModeMvCandidate(.., NEARMV) returns the
// NEAR candidate ref_mvs[1] instead of the NEAREST ref_mvs[0], so every NEWMV
// sub-block of that leaf was differenced against the wrong reference and the
// decoder reconstructed each MV shifted by (ref_mvs[1] - ref_mvs[0]). That
// corruption then propagated into the NEAREST chain of subsequent blocks. Using
// NEWMV here makes the encoder's write reference byte-identical to the decoder's
// read reference for both sub-8x8 and whole-block NEWMV. (For NEARESTMV/NEWMV
// blocks the result is unchanged: both already resolve to ref_mvs[0]. For
// NEARMV/ZEROMV whole blocks no MV is written, so the value is unused.)
func (e *VP9Encoder) vp9EncoderBestInterRefMvs(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, allowHP bool, signBias [vp9dec.MaxRefFrames]uint8,
) [2]vp9dec.MV {
	var best [2]vp9dec.MV
	if mi == nil || mi.Mode == common.ZeroMv || mi.RefFrame[0] <= vp9dec.IntraFrame {
		return best
	}
	halves := 1
	if mi.RefFrame[1] > vp9dec.IntraFrame {
		halves = 2
	}
	for ref := 0; ref < halves; ref++ {
		if cand, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, common.NewMv, mi.RefFrame[ref], allowHP,
			signBias); ok {
			best[ref] = cand
		}
	}
	return best
}

func (e *VP9Encoder) vp9EncoderInterModeCandidateMv(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, allowHP bool,
	signBias [vp9dec.MaxRefFrames]uint8,
) (vp9dec.MV, bool) {
	if mode == common.ZeroMv || refFrame <= vp9dec.IntraFrame {
		return vp9dec.MV{}, false
	}
	// Pass the five MV-ref-scan inputs directly; previously this site
	// allocated a ~29kB VP9Decoder per call just to populate five fields.
	// libvpx: vp9/common/vp9_mvref_common.c — find_mv_refs_idx reads the
	// flat fields off VP9_COMMON/MACROBLOCKD without an intermediate
	// composite.
	refList, refCount := vp9dec.FindInterMvRefsFields(e.miGrid,
		e.useVP9EncoderPrevFrameMvs(miRows, miCols),
		e.prevFrameMvs, e.prevFrameMvRows, e.prevFrameMvCols,
		tile, miRows, miCols, miRow, miCol, bsize, mode, refFrame,
		signBias, -1)
	if mode == common.NearMv {
		if refCount <= 1 {
			return vp9dec.MV{}, false
		}
	} else if refCount == 0 {
		return vp9dec.MV{}, false
	}
	mv := vp9dec.InterModeMvCandidate(refList, refCount, mode)
	vp9dec.LowerMvPrecision(&mv, allowHP)
	return mv, true
}

func (e *VP9Encoder) vp9FindInterMvRefsForBlock(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	mode common.PredictionMode,
	refFrame int8,
	signBias [vp9dec.MaxRefFrames]uint8,
	block int,
) ([2]vp9dec.MV, int) {
	return vp9dec.FindInterMvRefsFields(e.miGrid,
		e.useVP9EncoderPrevFrameMvs(miRows, miCols),
		e.prevFrameMvs, e.prevFrameMvRows, e.prevFrameMvCols,
		tile, miRows, miCols, miRow, miCol, bsize, mode, refFrame,
		signBias, block)
}

func (e *VP9Encoder) vp9AppendSub8x8MvsForIdx(mi *vp9dec.NeighborMi,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	mode common.PredictionMode,
	block, ref int,
	refFrame int8,
	signBias [vp9dec.MaxRefFrames]uint8,
) vp9dec.MV {
	if mi == nil {
		return vp9dec.MV{}
	}
	refList, refCount := e.vp9FindInterMvRefsForBlock(tile, miRows, miCols,
		miRow, miCol, bsize, mode, refFrame, signBias, block)
	switch block {
	case 0:
		if refCount > 0 {
			return refList[refCount-1]
		}
	case 1, 2:
		if mode == common.NearestMv {
			return mi.Bmi[0].AsMv[ref]
		}
		for i := range refList {
			if refList[i] != mi.Bmi[0].AsMv[ref] {
				return refList[i]
			}
		}
	case 3:
		if mode == common.NearestMv {
			return mi.Bmi[2].AsMv[ref]
		}
		if mi.Bmi[2].AsMv[ref] != mi.Bmi[1].AsMv[ref] {
			return mi.Bmi[1].AsMv[ref]
		}
		if mi.Bmi[2].AsMv[ref] != mi.Bmi[0].AsMv[ref] {
			return mi.Bmi[0].AsMv[ref]
		}
		for i := range refList {
			if refList[i] != mi.Bmi[2].AsMv[ref] {
				return refList[i]
			}
		}
	}
	return vp9dec.MV{}
}

func (e *VP9Encoder) predictVP9InterBlock(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) bool {
	return e.predictVP9InterBlockOpts(inter, miRows, miCols, miRow, miCol,
		bsize, mi, false, false)
}

// predictVP9InterBlockLumaOnly reconstructs only the luma plane for the
// given inter prediction. Encoder motion-search SAD only reads luma, so
// skipping chroma cuts ~30-40% of convolve8 work per candidate.
// libvpx: vp9/encoder/vp9_pickmode.c:2336 (vp9_build_inter_predictors_sby
// in nonrd_pickmode does luma only).
func (e *VP9Encoder) predictVP9InterBlockLumaOnly(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) bool {
	return e.predictVP9InterBlockOpts(inter, miRows, miCols, miRow, miCol,
		bsize, mi, true, false)
}

// predictVP9InterBlockChromaOnly reconstructs only U/V for callers that have
// already built or scored luma. The variance-partition chroma_check path only
// needs chroma SAD, matching libvpx's use of pd->dst.buf after the luma
// partition prepass predictor is available.
func (e *VP9Encoder) predictVP9InterBlockChromaOnly(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) bool {
	return e.predictVP9InterBlockOpts(inter, miRows, miCols, miRow, miCol,
		bsize, mi, false, true)
}

func (e *VP9Encoder) predictVP9InterBlockOpts(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, lumaOnly bool, chromaOnly bool,
) bool {
	if inter == nil || inter.ref == nil || !inter.ref.valid {
		return false
	}
	if mi == nil || mi.RefFrame[0] <= vp9dec.IntraFrame {
		return false
	}
	predictor := &e.interPredictor
	predictor.planes = e.planes
	predictor.frameY = e.reconY
	predictor.frameU = e.reconU
	predictor.frameV = e.reconV
	predictor.lastFrame = e.reconFrame
	predictor.interPredictScratch = e.interPredictScratch
	predictor.refFramesView = &e.refFrames
	predictor.unsupportedReconstruct = false
	predictor.predictLumaOnly = lumaOnly
	predictor.predictChromaOnly = chromaOnly
	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(e.opts.Width),
		Height: uint32(e.opts.Height),
		InterRef: vp9dec.InterRefBlock{
			RefIndex: e.vp9InterRefIndexForFrame(),
			SignBias: [3]uint8{
				vp9InterSignBias(inter)[vp9dec.LastFrame],
				vp9InterSignBias(inter)[vp9dec.GoldenFrame],
				vp9InterSignBias(inter)[vp9dec.AltrefFrame],
			},
		},
		AllowHighPrecisionMv: true,
		InterpFilter:         vp9InterFrameInterpFilter(inter),
	}
	ok := predictor.reconstructVP9InterPredictBlock(&hdr, mi, miRow, miCol, bsize)
	e.interPredictScratch = predictor.interPredictScratch
	predictor.refFramesView = nil
	// Reset flags so subsequent callers that don't explicitly select planes get
	// the full 3-plane reconstruction.
	predictor.predictLumaOnly = false
	predictor.predictChromaOnly = false
	return ok && !predictor.unsupportedReconstruct
}

func clearVP9TxCoeffOutputs(out, qOut []int16, maxEob int) {
	if maxEob <= 0 {
		return
	}
	if len(out) >= maxEob {
		clear(out[:maxEob])
	}
	if qOut != nil && len(qOut) >= maxEob {
		clear(qOut[:maxEob])
	}
}

func (e *VP9Encoder) prepareVP9KeyframeTxResidue(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int, dequant [2]int16,
	qindex int, out, qOut []int16,
) bool {
	return e.prepareVP9KeyframeTxResidueWithQ(key, pd, plane, mode, txSize,
		tile, miRows, miCols, miRow, miCol, bsize, blockRow4x4, blockCol4x4,
		dequant, qindex, out, qOut)
}

// prepareVP9KeyframeTxResidueWithQ mirrors prepareVP9KeyframeTxResidue and
// additionally emits the signed quantized coefficients into qOut when
// non-nil so the cost_coeffs port can consume libvpx-equivalent qcoeff
// values. libvpx vp9_rdopt.c:367,392,405 reads qcoeff from
// p->qcoeff = ctx->qcoeff_pbuf; recovery from dqcoeff drifts whenever
// q*dequant overflows int16 (notably Tx32x32 high-frequency bands where
// dequant[1] can reach ~1828 at high qindex).
func (e *VP9Encoder) prepareVP9KeyframeTxResidueWithQ(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int, dequant [2]int16,
	qindex int, out, qOut []int16,
) bool {
	ok, _ := e.prepareVP9KeyframeTxResidueWithQEOB(key, pd, plane, mode, txSize,
		tile, miRows, miCols, miRow, miCol, bsize, blockRow4x4, blockCol4x4,
		dequant, qindex, out, qOut)
	return ok
}

func (e *VP9Encoder) prepareVP9KeyframeTxResidueWithQEOB(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int, dequant [2]int16,
	qindex int, out, qOut []int16,
) (bool, int) {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	dst, stride, x0, y0, ok := e.predictVP9KeyframeTx(key.hdr, pd, plane, mode,
		txSize, tile, miRows, miCols, miRow, miCol, bsize, blockRow4x4, blockCol4x4)
	if !ok {
		clearVP9TxCoeffOutputs(out, qOut, maxEob)
		return false, 0
	}
	txType := common.DctDct
	if plane == 0 && txSize != common.Tx32x32 && !key.lossless {
		txType = common.IntraModeToTxType[mode]
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		clearVP9TxCoeffOutputs(out, qOut, maxEob)
		return false, 0
	}
	eob := e.quantizeVP9TxResidualWithQTrellis(dst, stride, txSize, txType, dequant, qindex,
		out, qOut, key.lossless, false, false, nil)
	if eob == 0 {
		clearVP9TxCoeffOutputs(out, qOut, maxEob)
		return false, 0
	}
	return true, eob
}

func (e *VP9Encoder) prepareVP9InterTxResidue(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, txSize common.TxSize,
	miRow, miCol int, blockRow4x4, blockCol4x4 int, dequant [2]int16, out []int16,
) bool {
	return e.prepareVP9InterTxResidueWithQ(inter, pd, plane, txSize, miRow, miCol,
		blockRow4x4, blockCol4x4, dequant, out, nil)
}

// prepareVP9InterTxResidueWithQ is the qcoeff-emitting sibling of
// prepareVP9InterTxResidue. See prepareVP9KeyframeTxResidueWithQ for the
// libvpx cost_coeffs rationale.
func (e *VP9Encoder) prepareVP9InterTxResidueWithQ(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, txSize common.TxSize,
	miRow, miCol int, blockRow4x4, blockCol4x4 int, dequant [2]int16, out, qOut []int16,
) bool {
	ok, _ := e.prepareVP9InterTxResidueWithQEOB(inter, pd, plane, txSize,
		miRow, miCol, blockRow4x4, blockCol4x4, dequant, out, qOut)
	return ok
}

func (e *VP9Encoder) prepareVP9InterTxResidueWithQEOB(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, txSize common.TxSize,
	miRow, miCol int, blockRow4x4, blockCol4x4 int, dequant [2]int16, out, qOut []int16,
) (bool, int) {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	dst, stride, x0, y0, ok := e.vp9EncoderTxDst(pd, plane, txSize,
		miRow, miCol, blockRow4x4, blockCol4x4)
	if !ok {
		clearVP9TxCoeffOutputs(out, qOut, maxEob)
		return false, 0
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		clearVP9TxCoeffOutputs(out, qOut, maxEob)
		return false, 0
	}
	// KNOWN DIVERGENCE (cpu0-3 inter coefficient parity): this hardcodes the FP
	// quantizer (useFastQuant=true). libvpx's encode_block selects the quantizer
	// on x->quant_fp = sf.use_quant_fp (vp9/encoder/vp9_encodemb.c:590-625,
	// vp9_encodeframe.c:5665) — FP only when set, else the zbin "b" quantizer.
	// use_quant_fp is 0 by default (vp9_speed_features.c:954) and set !is_keyframe
	// only at REALTIME speed>=4 (vp9_speed_features.c:573). So cpu>=4 (e.g. the
	// byte-exact {0,1,1,0,1} cpu4 seed) correctly uses FP here, but cpu0-3 should
	// use B: FP drops AC coefficients the B quantizer keeps (e.g. {0,2,0,0,2}
	// frame-1 mi(0,1) sub-block 2: FP eob 1 vs libvpx's DC 180@0 + AC dq 235@3).
	// Gating this on `e.sf.UseQuantFp != 0` is the correct fix but cascades: this
	// fn also feeds the deep-search entropy-context stamp, and the {0,2,0,0,2}
	// cpu0 deep mode-pins were calibrated on the FP recon — so the gate must land
	// together with re-deriving those pins toward byte parity. See
	// docs/vp9_cpu0_quant_fp_gap.md.
	eob := e.quantizeVP9TxResidualWithQTrellis(dst, stride, txSize, common.DctDct, dequant, 0,
		out, qOut, inter.lossless, true, false, nil)
	if eob == 0 {
		clearVP9TxCoeffOutputs(out, qOut, maxEob)
		return false, 0
	}
	return true, eob
}

func (e *VP9Encoder) gatherVP9TxResidual(src []byte, srcStride, srcW, srcH int,
	dst []byte, dstStride, x0, y0 int, txSize common.TxSize,
) bool {
	bs := 4 << uint(txSize)
	if bs*bs > len(e.residueScratch) || len(src) == 0 || srcStride <= 0 ||
		srcW <= 0 || srcH <= 0 {
		return false
	}
	diffMask := 0
	if x0 >= 0 && y0 >= 0 && x0+bs <= srcW && y0+bs <= srcH {
		if nonZero, ok := vp9dsp.SubtractBlockNonZero(src,
			y0*srcStride+x0, srcStride, dst, 0, dstStride,
			e.residueScratch[:], 0, bs, bs, bs); ok {
			return nonZero
		}
		for y := range bs {
			srcRow := src[(y0+y)*srcStride+x0:]
			dstRow := dst[y*dstStride:]
			for x := range bs {
				diff := int(srcRow[x]) - int(dstRow[x])
				e.residueScratch[y*bs+x] = int16(diff)
				diffMask |= diff
			}
		}
		return diffMask != 0
	}
	for y := range bs {
		sy := arith.ClampCoord(y0+y, srcH)
		srcRow := src[sy*srcStride:]
		dstRow := dst[y*dstStride:]
		for x := range bs {
			sx := arith.ClampCoord(x0+x, srcW)
			diff := int(srcRow[sx]) - int(dstRow[x])
			e.residueScratch[y*bs+x] = int16(diff)
			diffMask |= diff
		}
	}
	return diffMask != 0
}

func vp9CopySourceRectClamped(dst []byte, dstStride int, src []byte,
	srcStride, srcW, srcH int, x0, y0, w, h int,
) {
	if len(dst) == 0 || dstStride <= 0 || len(src) == 0 ||
		srcStride <= 0 || srcW <= 0 || srcH <= 0 || w <= 0 || h <= 0 {
		return
	}
	dstRows := len(dst) / dstStride
	if x0 < 0 || y0 < 0 || x0 >= dstStride || y0 >= dstRows {
		return
	}
	if x0+w > dstStride {
		w = dstStride - x0
	}
	if y0+h > dstRows {
		h = dstRows - y0
	}
	if w <= 0 || h <= 0 {
		return
	}
	if x0+w <= srcW && y0+h <= srcH {
		for y := range h {
			copy(dst[(y0+y)*dstStride+x0:(y0+y)*dstStride+x0+w],
				src[(y0+y)*srcStride+x0:(y0+y)*srcStride+x0+w])
		}
		return
	}
	for y := range h {
		sy := arith.ClampCoord(y0+y, srcH)
		dstRow := dst[(y0+y)*dstStride+x0:]
		srcRow := src[sy*srcStride:]
		for x := range w {
			sx := arith.ClampCoord(x0+x, srcW)
			dstRow[x] = srcRow[sx]
		}
	}
}

func vp9PredictionSSEClamped(src []byte, srcStride, srcW, srcH int,
	pred []byte, predStride, x0, y0, bs int,
) uint64 {
	if len(src) == 0 || srcStride <= 0 || srcW <= 0 || srcH <= 0 ||
		len(pred) == 0 || predStride <= 0 || bs <= 0 {
		return 0
	}
	var score uint64
	if x0 >= 0 && y0 >= 0 && x0+bs <= srcW && y0+bs <= srcH {
		for y := range bs {
			srcRow := src[(y0+y)*srcStride+x0:]
			predRow := pred[y*predStride:]
			for x := range bs {
				diff := int(srcRow[x]) - int(predRow[x])
				score += uint64(diff * diff)
			}
		}
		return score
	}
	for y := range bs {
		sy := arith.ClampCoord(y0+y, srcH)
		srcRow := src[sy*srcStride:]
		predRow := pred[y*predStride:]
		for x := range bs {
			sx := arith.ClampCoord(x0+x, srcW)
			diff := int(srcRow[sx]) - int(predRow[x])
			score += uint64(diff * diff)
		}
	}
	return score
}

// quantizeVP9TxResidualWithQ mirrors quantizeVP9TxResidual and additionally
// emits the signed quantized coefficients into qOut when non-nil. libvpx's
// cost_coeffs (vp9_rdopt.c:367,392,405,438) reads qcoeff directly so
// callers in the second-tier RD chain pass qOut and avoid recovering q
// from int16-wrapped dqcoeff. libvpx file:line: vpx_dsp/quantize.c:42-77
// (b) and 216-275 (b_32x32); vp9/encoder/vp9_quantize.c:26-56 (fp) and
// 92-123 (fp_32x32) all write qcoeff_ptr + dqcoeff_ptr in lockstep.
func (e *VP9Encoder) quantizeVP9TxResidualWithQ(dst []byte, stride int,
	txSize common.TxSize, txType common.TxType, dequant [2]int16, qindex int,
	out, qOut []int16, lossless bool, useFastQuant bool, useLp32x32RD bool,
) bool {
	return e.quantizeVP9TxResidualWithQTrellis(dst, stride, txSize, txType,
		dequant, qindex, out, qOut, lossless, useFastQuant, useLp32x32RD, nil) > 0
}

// quantizeVP9TxResidualWithQTrellis is quantizeVP9TxResidualWithQ with an
// optional trellis hook (libvpx block_rd_txfm runs vp9_optimize_b between
// vp9_xform_quant and the inverse transform / dist_block, vp9_rdopt.c:792-795).
// When trellis is non-nil it is called after quantization with the pre-quant
// forward-transform coefficients, the quantizer's qcoeff/dqcoeff, and the
// pre-trellis eob, and must return the optimised eob (mutating qcoeff/dqcoeff
// in place). The inverse transform and the out/qOut copies then reflect the
// optimised coefficients, exactly as dist_block / cost_coeffs consume them.
// Returns the final optimized EOB, or zero when the block has no coded residue.
// When trellis is nil this is byte-identical to quantizeVP9TxResidualWithQ.
func (e *VP9Encoder) quantizeVP9TxResidualWithQTrellis(dst []byte, stride int,
	txSize common.TxSize, txType common.TxType, dequant [2]int16, qindex int,
	out, qOut []int16, lossless bool, useFastQuant bool, useLp32x32RD bool,
	trellis func(coeff, qcoeff, dqcoeff []int16, eob int) int,
) int {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if txType >= common.TxTypes || maxEob > vp9EncoderTxCoeffSlots ||
		dequant[0] == 0 || dequant[1] == 0 || len(out) < maxEob {
		return 0
	}
	if qOut != nil && len(qOut) < maxEob {
		return 0
	}
	if lossless && txSize != common.Tx4x4 {
		return 0
	}
	if txSize == common.Tx32x32 && txType != common.DctDct {
		return 0
	}
	wantQ := qOut != nil
	// Valid VP9 forward transforms overwrite every coefficient slot, and the
	// quantizers either clear or fully write q/dq outputs. Keep the hot path
	// to one producer per scratch buffer instead of pre-clearing the same memory.
	if lossless {
		txType = common.DctDct
		encoder.ForwardWHT4x4Into(e.residueScratch[:], 4,
			e.txCoeffScratch[:maxEob])
	} else {
		switch txSize {
		case common.Tx4x4:
			encoder.ForwardHT4x4Into(e.residueScratch[:], 4, txType,
				e.txCoeffScratch[:maxEob])
		case common.Tx8x8:
			encoder.ForwardHT8x8Into(e.residueScratch[:], 8, txType,
				e.txCoeffScratch[:maxEob])
		case common.Tx16x16:
			encoder.ForwardHT16x16Into(e.residueScratch[:], 16, txType,
				e.txCoeffScratch[:maxEob])
		case common.Tx32x32:
			// libvpx vp9/encoder/vp9_encodemb.c:331-337,396 routes the
			// 32x32 forward DCT through the rate-distortion-loop variant
			// (vpx_fdct32x32_rd_c) whenever MACROBLOCK::use_lp32x32fdct is
			// set, which mirrors the speed feature sf.use_lp32x32fdct. The
			// RD variant is the FP-path companion in libvpx, so it only
			// applies when this caller is on the fast (vp9_quantize_fp)
			// branch.
			//
			// useLp32x32RD overrides this for the full-RD MODE-SELECTION path:
			// rd_pick_sb_modes (vp9_encodeframe.c:1994) unconditionally sets
			// x->use_lp32x32fdct = 1 ("lower precision, but faster, 32x32 fdct
			// for mode selection") before super_block_yrd, regardless of the
			// speed feature. The final encode pass (encode_superblock) restores
			// x->use_lp32x32fdct = sf->use_lp32x32fdct (vp9_encodeframe.c:6063).
			if useLp32x32RD || (useFastQuant && e.sf.UseLp32x32Fdct != 0) {
				encoder.ForwardDCT32x32RDInto(e.residueScratch[:], 32, e.txCoeffScratch[:maxEob])
			} else {
				encoder.ForwardDCT32x32Into(e.residueScratch[:], 32, e.txCoeffScratch[:maxEob])
			}
		default:
			return 0
		}
	}
	scanOrder := common.ScanOrders[txSize][txType]
	if lossless {
		scanOrder = common.DefaultScanOrders[txSize]
	}
	scan := scanOrder.Scan
	eob := 0
	// libvpx writes both qcoeff and dqcoeff inside the quantize kernels
	// (vpx_dsp/quantize.c:71-72, 261,269; vp9/encoder/vp9_quantize.c:50-51,
	// 116-117). govpx mirrors this when qOut is requested so the
	// cost_coeffs path consumes qcoeff directly instead of recovering it
	// from int16-wrapped dqcoeff.
	var qBuf []int16
	if wantQ {
		qBuf = e.qCoeffScratch[:maxEob]
	}
	if txSize == common.Tx32x32 {
		if !useFastQuant {
			eob = encoder.QuantizeB32x32WithQScanOrder(e.txCoeffScratch[:maxEob], qindex,
				dequant, scanOrder, qBuf, e.dqCoeffScratch[:maxEob])
		} else {
			eob = encoder.QuantizeFP32x32WithQ(e.txCoeffScratch[:maxEob],
				dequant, scan, qBuf, e.dqCoeffScratch[:maxEob])
		}
	} else {
		if !useFastQuant {
			eob = encoder.QuantizeBWithQScanOrder(e.txCoeffScratch[:maxEob], qindex,
				dequant, scanOrder, qBuf, e.dqCoeffScratch[:maxEob])
		} else {
			eob = encoder.QuantizeFPWithQScanOrder(e.txCoeffScratch[:maxEob], dequant,
				scanOrder, qBuf, e.dqCoeffScratch[:maxEob])
		}
	}
	if eob == 0 {
		return 0
	}
	// libvpx block_rd_txfm runs vp9_optimize_b (trellis) here, between
	// vp9_xform_quant and the inverse transform consumed by dist_block
	// (vp9_rdopt.c:793-795). The trellis mutates qcoeff/dqcoeff (in scratch)
	// and returns the optimised eob; both the inverse transform below and the
	// out/qOut copies then reflect the optimised coefficients. Requires qcoeff
	// (qBuf), so callers wanting trellis must request qOut.
	if trellis != nil && wantQ {
		eob = trellis(e.txCoeffScratch[:maxEob], e.qCoeffScratch[:maxEob],
			e.dqCoeffScratch[:maxEob], eob)
		if eob == 0 {
			return 0
		}
	}
	copy(out[:maxEob], e.dqCoeffScratch[:maxEob])
	if wantQ {
		copy(qOut[:maxEob], e.qCoeffScratch[:maxEob])
	}
	vp9dec.InverseTransformBlock(out[:maxEob],
		dst, stride, txSize, txType, eob, lossless)
	return eob
}

func (e *VP9Encoder) predictVP9KeyframeTx(hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	return e.predictVP9KeyframeTxGeneric(hdr, pd, plane, mode, txSize, tile,
		miRows, miCols, miRow, miCol, bsize, blockRow4x4, blockCol4x4,
		planeData, stride, planeData, stride, 0, 0, 0, 0)
}

func (e *VP9Encoder) predictVP9KeyframeTxGeneric(hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
	dstData []byte, dstStride int, refData []byte, refStride int,
	dstOriginX, dstOriginY, refOriginX, refOriginY int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	stride = dstStride
	if dstStride <= 0 || len(dstData) == 0 || refStride <= 0 ||
		len(refData) == 0 || int(mode) >= common.IntraModes {
		return nil, 0, 0, 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return nil, 0, 0, 0, false
	}
	rows := len(dstData) / dstStride
	refRows := len(refData) / refStride
	alignedWidth := buffers.Align(int(hdr.Width), 8)
	alignedHeight := buffers.Align(int(hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 = baseX + blockCol4x4*4
	y0 = baseY + blockRow4x4*4
	localX := x0 - (dstOriginX >> pd.SubsamplingX)
	localY := y0 - (dstOriginY >> pd.SubsamplingY)
	refLocalX := x0 - (refOriginX >> pd.SubsamplingX)
	refLocalY := y0 - (refOriginY >> pd.SubsamplingY)

	bs := 4 << uint(txSize)
	if localX < 0 || localY < 0 || refLocalX < 0 || refLocalY < 0 ||
		localX+bs > dstStride || localY+bs > rows ||
		refLocalX+bs > refStride || refLocalY+bs > refRows {
		return nil, 0, 0, 0, false
	}

	bounds := vp9dec.BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	left := e.intraScratch.Left[:bs]
	if leftAvailable {
		if refLocalX <= 0 {
			return nil, 0, 0, 0, false
		}
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			refY := sy - (refOriginY >> pd.SubsamplingY)
			left[i] = refData[refY*refStride+refLocalX-1]
		}
	}

	edges := vp9dec.IntraEdgeRefs{
		AboveLeft: 127,
		Left:      left,
	}
	upAvailable := blockRow4x4 != 0 || miRow > 0
	if upAvailable {
		if refLocalY <= 0 {
			return nil, 0, 0, 0, false
		}
		edges.Above = refData[(refLocalY-1)*refStride+refLocalX:]
		if leftAvailable {
			edges.AboveLeft = refData[(refLocalY-1)*refStride+refLocalX-1]
		}
	}
	planeBlock4x4W := vp9IntraPredictWidth4x4(bsize, planeBsize, pd)
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W
	dst = dstData[localY*dstStride+localX:]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            dst,
		DstStride:      dstStride,
		Mode:           mode,
		TxSize:         txSize,
		Edges:          edges,
		UpAvailable:    upAvailable,
		LeftAvailable:  leftAvailable,
		RightAvailable: rightAvailable,
		FrameWidth:     planeWidth,
		FrameHeight:    planeHeight,
		X0:             x0,
		Y0:             y0,
		MbToRightEdge:  bounds.MbToRightEdge,
		MbToBottomEdge: bounds.MbToBottomEdge,
	}, &e.intraScratch)
	return dst, dstStride, x0, y0, true
}

func (e *VP9Encoder) predictVP9KeyframeTxScratchLive(hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
	dstData []byte, dstStride int, dstOriginX, dstOriginY int,
	refData []byte, refStride int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	stride = dstStride
	if hdr == nil || pd == nil || dstStride <= 0 || len(dstData) == 0 ||
		refStride <= 0 || len(refData) == 0 || int(mode) >= common.IntraModes {
		return nil, 0, 0, 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return nil, 0, 0, 0, false
	}
	rows := len(dstData) / dstStride
	refRows := len(refData) / refStride
	alignedWidth := buffers.Align(int(hdr.Width), 8)
	alignedHeight := buffers.Align(int(hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 = baseX + blockCol4x4*4
	y0 = baseY + blockRow4x4*4
	originX := dstOriginX >> pd.SubsamplingX
	originY := dstOriginY >> pd.SubsamplingY
	localX := x0 - originX
	localY := y0 - originY

	bs := 4 << uint(txSize)
	if localX < 0 || localY < 0 || localX+bs > dstStride || localY+bs > rows {
		return nil, 0, 0, 0, false
	}

	readEdge := func(px, py int) uint8 {
		if lx, ly := px-originX, py-originY; lx >= 0 && ly >= 0 &&
			lx < dstStride && ly < rows {
			return dstData[ly*dstStride+lx]
		}
		if py < 0 {
			py = 0
		} else if py >= refRows {
			py = refRows - 1
		}
		if px < 0 {
			px = 0
		} else if px >= refStride {
			px = refStride - 1
		}
		return refData[py*refStride+px]
	}

	bounds := vp9dec.BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	left := e.intraScratch.Left[:bs]
	if leftAvailable {
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			left[i] = readEdge(x0-1, sy)
		}
	}

	upAvailable := blockRow4x4 != 0 || miRow > 0
	above := e.intraScratch.Above[1 : 1+2*bs]
	aboveLeft := uint8(127)
	if upAvailable {
		for i := range 2 * bs {
			above[i] = readEdge(x0+i, y0-1)
		}
		if leftAvailable {
			aboveLeft = readEdge(x0-1, y0-1)
		}
	}

	planeBlock4x4W := vp9IntraPredictWidth4x4(bsize, planeBsize, pd)
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W
	dst = dstData[localY*dstStride+localX:]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            dst,
		DstStride:      dstStride,
		Mode:           mode,
		TxSize:         txSize,
		Edges:          vp9dec.IntraEdgeRefs{Above: above, AboveLeft: aboveLeft, Left: left},
		UpAvailable:    upAvailable,
		LeftAvailable:  leftAvailable,
		RightAvailable: rightAvailable,
		FrameWidth:     planeWidth,
		FrameHeight:    planeHeight,
		X0:             x0,
		Y0:             y0,
		MbToRightEdge:  bounds.MbToRightEdge,
		MbToBottomEdge: bounds.MbToBottomEdge,
	}, &e.intraScratch)
	return dst, dstStride, x0, y0, true
}

func (e *VP9Encoder) vp9EncoderTxDst(pd *vp9dec.MacroblockdPlane,
	plane int, txSize common.TxSize,
	miRow, miCol int, blockRow4x4, blockCol4x4 int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	if stride <= 0 || len(planeData) == 0 {
		return nil, 0, 0, 0, false
	}
	rows := len(planeData) / stride
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 = baseX + blockCol4x4*4
	y0 = baseY + blockRow4x4*4
	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, 0, 0, false
	}
	return planeData[y0*stride+x0:], stride, x0, y0, true
}

func (e *VP9Encoder) vp9BlockCoeffs(plane int,
	bsize common.BlockSize, r, c int, tx common.TxSize,
) []int16 {
	coeffs := e.coefScratch[:vp9dec.MaxEobForTxSize(tx)]
	for i := range coeffs {
		coeffs[i] = 0
	}
	if plane < 0 || plane >= vp9dec.MaxMbPlane {
		return coeffs
	}
	pd := &e.planes[plane]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return coeffs
	}
	full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	coeffBase := (r*full4x4W + c) * vp9EncoderTxCoeffSlots
	maxEob := vp9dec.MaxEobForTxSize(tx)
	if maxEob <= vp9EncoderTxCoeffSlots && coeffBase >= 0 &&
		coeffBase+maxEob <= len(e.blockCoeffs[plane]) {
		copy(coeffs, e.blockCoeffs[plane][coeffBase:coeffBase+maxEob])
	}
	return coeffs
}

func (e *VP9Encoder) vp9BlockQCoeffs(plane int,
	bsize common.BlockSize, r, c int, tx common.TxSize,
) []int16 {
	qcoeffs := e.qCoefScratch[:vp9dec.MaxEobForTxSize(tx)]
	for i := range qcoeffs {
		qcoeffs[i] = 0
	}
	if plane < 0 || plane >= vp9dec.MaxMbPlane {
		return qcoeffs
	}
	pd := &e.planes[plane]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return qcoeffs
	}
	full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	coeffBase := (r*full4x4W + c) * vp9EncoderTxCoeffSlots
	maxEob := vp9dec.MaxEobForTxSize(tx)
	if maxEob <= vp9EncoderTxCoeffSlots && coeffBase >= 0 &&
		coeffBase+maxEob <= len(e.blockQCoeffs[plane]) {
		copy(qcoeffs, e.blockQCoeffs[plane][coeffBase:coeffBase+maxEob])
	}
	return qcoeffs
}

func (e *VP9Encoder) vp9BlockEOB(plane int,
	bsize common.BlockSize, r, c int, tx common.TxSize,
) (int, bool) {
	if plane < 0 || plane >= vp9dec.MaxMbPlane || tx >= common.TxSizes {
		return 0, false
	}
	pd := &e.planes[plane]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, false
	}
	full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	idx := r*full4x4W + c
	if idx < 0 || idx >= len(e.blockEOBs[plane]) {
		return 0, false
	}
	eob := int(e.blockEOBs[plane][idx])
	maxEob := vp9dec.MaxEobForTxSize(tx)
	if eob < 0 || eob > maxEob {
		return 0, false
	}
	return eob, true
}

func (e *VP9Encoder) vp9EncoderReconPlane(plane int) ([]byte, int) {
	switch plane {
	case 0:
		return e.reconY, e.reconFrame.YStride
	case 1:
		return e.reconU, e.reconFrame.UStride
	case 2:
		return e.reconV, e.reconFrame.VStride
	default:
		return nil, 0
	}
}

func vp9EncoderSourcePlane(img *image.YCbCr, plane int) (
	pixels []byte, stride, width, height int,
) {
	if img == nil {
		return nil, 0, 0, 0
	}
	switch plane {
	case 0:
		return img.Y, img.YStride, img.Rect.Dx(), img.Rect.Dy()
	case 1:
		return img.Cb, img.CStride, (img.Rect.Dx() + 1) >> 1, (img.Rect.Dy() + 1) >> 1
	case 2:
		return img.Cr, img.CStride, (img.Rect.Dx() + 1) >> 1, (img.Rect.Dy() + 1) >> 1
	default:
		return nil, 0, 0, 0
	}
}

func vp9ReferenceVisiblePlane(ref *vp9ReferenceFrame, plane int) (
	pixels []byte, stride, width, height int,
) {
	if ref == nil || !ref.valid {
		return nil, 0, 0, 0
	}
	pixels, stride = vp9ReferencePlane(ref, plane)
	switch plane {
	case 0:
		return pixels, stride, ref.img.Width, ref.img.Height
	case 1, 2:
		return pixels, stride, (ref.img.Width + 1) >> 1, (ref.img.Height + 1) >> 1
	default:
		return nil, 0, 0, 0
	}
}

func (e *VP9Encoder) vp9MiAt(miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	off := r*miCols + c
	if off < 0 || off >= len(e.miGrid) {
		return nil
	}
	return &e.miGrid[off]
}
