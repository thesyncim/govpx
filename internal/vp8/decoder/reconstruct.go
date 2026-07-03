package decoder

import (
	"errors"
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/decodeframe.c macroblock inverse transform setup
// - vp8/common/invtrans.h inverse-transform dispatch
// - vp8/common/setupintrarecon.c intra edge setup
// - vp8/common/reconinter.c whole-macroblock inter predictor offsets
// - vp8/common/findnearmv.h motion-vector border clamping
// - vp8/common/extend.c row-edge extension for intra prediction

var (
	ErrReconstructGridBufferTooSmall      = errors.New("govpx: VP8 reconstruction grid buffer too small")
	ErrUnsupportedIntraReconstructionMode = errors.New("govpx: unsupported VP8 intra reconstruction mode")
	ErrUnsupportedInterReconstructionMode = errors.New("govpx: unsupported VP8 inter reconstruction mode")
)

type MacroblockResidual struct {
	DQCoeff [25 * 16]int16
}

type IntraPredictorRefs struct {
	YAbove []byte
	YLeft  []byte
	UAbove []byte
	ULeft  []byte
	VAbove []byte
	VLeft  []byte

	YTopLeft byte
	UTopLeft byte
	VTopLeft byte

	UpAvailable   bool
	LeftAvailable bool
}

type IntraPredictorScratch struct {
	YAbove [20]byte
	YLeft  [16]byte
	UAbove [8]byte
	ULeft  [8]byte
	VAbove [8]byte
	VLeft  [8]byte
}

type IntraReconstructionScratch struct {
	Refs     IntraPredictorScratch
	Residual MacroblockResidual
}

func (r *MacroblockResidual) Block(index int) *[16]int16 {
	return (*[16]int16)(r.DQCoeff[index*16 : index*16+16])
}

func BuildIntraPredictorRefs(img *common.Image, mbRow int, mbCol int, scratch *IntraPredictorScratch) IntraPredictorRefs {
	yRow := mbRow * 16
	yCol := mbCol * 16
	uvRow := mbRow * 8
	uvCol := mbCol * 8
	upAvailable := mbRow > 0
	leftAvailable := mbCol > 0
	codedWidth := codedImageWidth(img)
	codedHeight := codedImageHeight(img)

	buildAbove(scratch.YAbove[:], img.Y, img.YFull, img.YOrigin, img.YStride, codedWidth, yRow, yCol, img.YBorder, upAvailable)
	buildLeft(scratch.YLeft[:], img.Y, img.YFull, img.YOrigin, img.YStride, codedHeight, yRow, yCol, img.YBorder, leftAvailable)
	uvWidth := (codedWidth + 1) >> 1
	uvHeight := (codedHeight + 1) >> 1
	buildAbove(scratch.UAbove[:], img.U, img.UFull, img.UOrigin, img.UStride, uvWidth, uvRow, uvCol, img.UVBorder, upAvailable)
	buildLeft(scratch.ULeft[:], img.U, img.UFull, img.UOrigin, img.UStride, uvHeight, uvRow, uvCol, img.UVBorder, leftAvailable)
	buildAbove(scratch.VAbove[:], img.V, img.VFull, img.VOrigin, img.VStride, uvWidth, uvRow, uvCol, img.UVBorder, upAvailable)
	buildLeft(scratch.VLeft[:], img.V, img.VFull, img.VOrigin, img.VStride, uvHeight, uvRow, uvCol, img.UVBorder, leftAvailable)

	return IntraPredictorRefs{
		YAbove:        scratch.YAbove[:],
		YLeft:         scratch.YLeft[:],
		UAbove:        scratch.UAbove[:],
		ULeft:         scratch.ULeft[:],
		VAbove:        scratch.VAbove[:],
		VLeft:         scratch.VLeft[:],
		YTopLeft:      topLeftSample(img.Y, img.YFull, img.YOrigin, img.YStride, yRow, yCol, img.YBorder, upAvailable, leftAvailable),
		UTopLeft:      topLeftSample(img.U, img.UFull, img.UOrigin, img.UStride, uvRow, uvCol, img.UVBorder, upAvailable, leftAvailable),
		VTopLeft:      topLeftSample(img.V, img.VFull, img.VOrigin, img.VStride, uvRow, uvCol, img.UVBorder, upAvailable, leftAvailable),
		UpAvailable:   upAvailable,
		LeftAvailable: leftAvailable,
	}
}

// BuildIntraPredictorRefsLuma gathers only the Y-plane neighbor stripes.
// The encoder's fast intra picker scores candidate 16x16 modes on luma
// alone (libvpx vp8/encoder/pickinter.c uses
// vp8_build_intra_predictors_mby_s in the mode loop and picks the UV mode
// separately for the winner), so it never reads the U/V stripes. When the
// above row is already contiguous in the visible or extended frame buffer, the
// returned YAbove slice aliases it directly like libvpx's pointer-based setup;
// edge-fill cases still fall back to scratch.
func BuildIntraPredictorRefsLuma(img *common.Image, mbRow int, mbCol int, scratch *IntraPredictorScratch) IntraPredictorRefs {
	yRow := mbRow * 16
	yCol := mbCol * 16
	upAvailable := mbRow > 0
	leftAvailable := mbCol > 0
	codedWidth := codedImageWidth(img)
	codedHeight := codedImageHeight(img)

	yAbove := scratch.YAbove[:]
	if !upAvailable {
		buildAbove(yAbove, img.Y, img.YFull, img.YOrigin, img.YStride, codedWidth, yRow, yCol, img.YBorder, upAvailable)
	} else if hasFullIntraSampleRange(img.YFull, img.YOrigin, img.YStride, codedWidth, img.YBorder, yRow-1, yCol, len(yAbove)) {
		start := img.YOrigin + (yRow-1)*img.YStride + yCol
		yAbove = img.YFull[start : start+len(yAbove)]
	} else if img.YStride > 0 && yCol+len(yAbove) <= codedWidth {
		start := (yRow-1)*img.YStride + yCol
		if start >= 0 && start+len(yAbove) <= len(img.Y) {
			yAbove = img.Y[start : start+len(yAbove)]
		} else {
			buildAbove(yAbove, img.Y, img.YFull, img.YOrigin, img.YStride, codedWidth, yRow, yCol, img.YBorder, upAvailable)
		}
	} else {
		buildAbove(yAbove, img.Y, img.YFull, img.YOrigin, img.YStride, codedWidth, yRow, yCol, img.YBorder, upAvailable)
	}
	buildLeft(scratch.YLeft[:], img.Y, img.YFull, img.YOrigin, img.YStride, codedHeight, yRow, yCol, img.YBorder, leftAvailable)

	return IntraPredictorRefs{
		YAbove:        yAbove,
		YLeft:         scratch.YLeft[:],
		YTopLeft:      topLeftSample(img.Y, img.YFull, img.YOrigin, img.YStride, yRow, yCol, img.YBorder, upAvailable, leftAvailable),
		UpAvailable:   upAvailable,
		LeftAvailable: leftAvailable,
	}
}

func TransformMacroblockTokens(tokens *MacroblockTokens, dequant *common.MacroblockDequant, is4x4 bool, out *MacroblockResidual) {
	transformMacroblockTokensLuma(tokens, dequant, is4x4, out)
	transformMacroblockTokensChroma(tokens, dequant, out)
}

func transformMacroblockTokensLuma(tokens *MacroblockTokens, dequant *common.MacroblockDequant, is4x4 bool, out *MacroblockResidual) {
	hasY2 := !is4x4 && tokens.EOB[24] > 0
	if hasY2 {
		clearYResidualBlocks(out)
		var y2 [16]int16
		if tokens.EOB[24] > 1 {
			dsp.DequantizeBlock(&tokens.QCoeff[24], &dequant.Y2, &y2)
			dsp.InverseWalsh4x4(&y2, out.DQCoeff[:])
		} else {
			y2[0] = tokens.QCoeff[24][0] * dequant.Y2[0]
			dsp.DCOnlyInverseWalsh4x4(y2[0], out.DQCoeff[:])
		}
	}

	yDequant := &dequant.Y1
	if !is4x4 {
		yDequant = &dequant.Y1DC
	}
	for i := range 16 {
		eob := tokens.EOB[i]
		if eob == 0 {
			continue
		}
		block := out.Block(i)
		if !hasY2 {
			if eob == 1 {
				block[0] = 0
			} else {
				clearResidualBlock(block)
			}
		}
		dequantizeInto(&tokens.QCoeff[i], yDequant, eob, block)
	}
}

func transformMacroblockTokensChroma(tokens *MacroblockTokens, dequant *common.MacroblockDequant, out *MacroblockResidual) {
	for i := 16; i < 24; i++ {
		eob := tokens.EOB[i]
		if eob == 0 {
			continue
		}
		block := out.Block(i)
		if eob == 1 {
			block[0] = 0
		} else {
			clearResidualBlock(block)
		}
		dequantizeInto(&tokens.QCoeff[i], &dequant.UV, eob, block)
	}
}

func AddMacroblockResidual(tokens *MacroblockTokens, residual *MacroblockResidual, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int) {
	AddMacroblockResidualWithDequant(tokens, residual, nil, y, yStride, u, uStride, v, vStride)
}

// dequantIDCTAddMacroblock is the fused dequant+IDCT+add path used by the
// decoder reconstruction hot paths. It mirrors libvpx v1.16.0
// vp8/decoder/decodeframe.c decode_macroblock composed with
// vp8/common/arm/neon/idct_blk_neon.c vp8_dequant_idct_add_y_block_neon and
// vp8_dequant_idct_add_uv_block_neon: for non-4x4 macroblocks the inverse
// Walsh transform seeds the per-block DC coefficients directly into
// qcoeff[i*16] (dequant.Y1DC[0] == 1 preserves them through the fused
// multiply), and the residual is then dequantized and transformed in
// horizontally adjacent block pairs straight from the token coefficients —
// no intermediate residual buffer.
func dequantIDCTAddMacroblock(tokens *MacroblockTokens, dequant *common.MacroblockDequant, is4x4 bool, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int) {
	yDequant := &dequant.Y1
	seeded := false
	if !is4x4 {
		yDequant = &dequant.Y1DC
		if tokens.EOB[24] > 0 {
			seedLumaDCFromY2(tokens, dequant)
			seeded = true
		}
	}
	dequantIDCTAddLumaBlocks(tokens, yDequant, seeded, y, yStride)
	if seeded {
		unseedLumaDC(tokens)
	}
	dequantIDCTAddChromaBlocks(tokens, &dequant.UV, u, uStride, v, vStride)
}

// DequantIDCTAddMacroblock exposes the fused reconstruction to encoder
// analysis callers. libvpx v1.16.0's encoder reconstructs accepted
// macroblocks with the same kernels as the decoder:
// vp8cx_encode_inter_macroblock (vp8/encoder/encodeframe.c:1288-1291) calls
// vp8_inverse_transform_mby (vp8/common/invtrans.h — inverse Walsh + eob
// promotion + vp8_dequant_idct_add_y_block) followed by
// vp8_dequant_idct_add_uv_block, so routing encoder recon through this
// fused path is the verbatim port of the encoder-side inverse transform.
//
// Token-buffer contract (audited for the encoder convert pipeline): every
// caller must guarantee that a block with EOB >= 2 has zeros in all
// coefficient slots at or beyond its EOB. The encoder quantizers
// (QuantizeBlockWithZbinAndActivity, FastQuantizeBlock, the trellis
// optimizer and ResetLibvpxSmallSecondOrderCoefficients) all write every
// one of the 16 slots, and ConvertMacroblockCoefficients copies the full
// array for EOB >= 2, so the contract holds there. EOB <= 1 blocks may
// carry arbitrary stale slots: the DC-pair path only reads slot 0 gated on
// EOB != 0 (or a Y2-seeded DC), and sanitizeDCOnlyBlock scrubs any EOB <= 1
// partner that rides the full pair kernel (see 354668f2).
func DequantIDCTAddMacroblock(tokens *MacroblockTokens, dequant *common.MacroblockDequant, is4x4 bool, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int) {
	dequantIDCTAddMacroblock(tokens, dequant, is4x4, y, yStride, u, uStride, v, vStride)
}

// seedLumaDCFromY2 mirrors libvpx decode_macroblock's second-order handling:
// vp8_short_inv_walsh4x4 (or the DC-only variant) writes the 16 per-block DC
// values into the luma coefficient blocks at stride 16 (dequant.Y1DC[0] == 1
// carries them through the fused multiply unchanged, exactly as libvpx).
func seedLumaDCFromY2(tokens *MacroblockTokens, dequant *common.MacroblockDequant) {
	qflat := unsafe.Slice(&tokens.QCoeff[0][0], 16*16)
	if tokens.EOB[24] > 1 {
		var y2 [16]int16
		dsp.DequantizeBlock(&tokens.QCoeff[24], &dequant.Y2, &y2)
		dsp.InverseWalsh4x4(&y2, qflat)
	} else {
		dsp.DCOnlyInverseWalsh4x4(tokens.QCoeff[24][0]*dequant.Y2[0], qflat)
	}
}

// unseedLumaDC restores the luma DC coefficient slots to their token-decode
// state after the fused reconstruction consumed the seeded Walsh DCs. Under
// the second-order transform the token pass never writes position 0
// (skip_dc), so the original value is always zero; zeroing keeps the tokens
// pristine for callers that reconstruct the same coefficients more than once
// (encoder analysis) and keeps the dirty-mask clearing contract unchanged.
func unseedLumaDC(tokens *MacroblockTokens) {
	for i := range 16 {
		tokens.QCoeff[i][0] = 0
	}
}

// sanitizeDCOnlyBlock zeroes the coefficient slots an EOB <= 1 block does
// not own before the block rides the fused pair kernel. The decoder's
// dirty-clear contract already guarantees these slots are zero, but encoder
// analysis callers reuse token buffers across macroblocks and only maintain
// the first EOB coefficients (libvpx's own encoder never routes its analysis
// reconstruction through the NEON pair kernels, and the pre-fusion
// addOneChromaBlock explicitly rebuilt "a fresh DC-only block" for the same
// reason). An EOB == 0 block owns nothing (unless the Y2 inverse Walsh
// seeded its DC, which is real reconstruction data); an EOB == 1 block owns
// only its DC.
func sanitizeDCOnlyBlock(block *[16]int16, eob uint8, seeded bool) {
	if eob == 0 && !seeded {
		block[0] = 0
	}
	for i := 1; i < 16; i++ {
		block[i] = 0
	}
}

// dequantIDCTAddLumaBlocks mirrors vp8_dequant_idct_add_y_block_neon: the 16
// luma blocks dispatch in horizontally adjacent pairs. A pair with any
// EOB > 1 runs the full fused kernel (int16 wrapping dequant of both
// blocks); otherwise the DC-only pair path applies ((q*dq)+4)>>3 computed in
// int precision, matching idct_dequant_0_2x_neon.
func dequantIDCTAddLumaBlocks(tokens *MacroblockTokens, yDequant *[16]int16, seeded bool, y []byte, yStride int) {
	qflat := unsafe.Slice(&tokens.QCoeff[0][0], 25*16)
	for row := range 4 {
		rowBlock := row * 4
		rowOff := row * 4 * yStride
		for col := 0; col < 4; col += 2 {
			b := rowBlock + col
			eob0 := tokens.EOB[b]
			eob1 := tokens.EOB[b+1]
			if eob0 == 0 && eob1 == 0 {
				continue
			}
			off := rowOff + col*4
			if eob0 > 1 || eob1 > 1 {
				if eob0 <= 1 {
					sanitizeDCOnlyBlock((*[16]int16)(qflat[b*16:b*16+16]), eob0, seeded)
				}
				if eob1 <= 1 {
					sanitizeDCOnlyBlock((*[16]int16)(qflat[b*16+16:b*16+32]), eob1, seeded)
				}
				dsp.DequantIDCTAddFull2x((*[32]int16)(qflat[b*16:]), yDequant, y[off:], yStride)
				continue
			}
			var dc0, dc1 int32
			if eob0 != 0 || seeded {
				dc0 = int32(tokens.QCoeff[b][0]) * int32(yDequant[0])
			}
			if eob1 != 0 || seeded {
				dc1 = int32(tokens.QCoeff[b+1][0]) * int32(yDequant[0])
			}
			dsp.DCOnlyIDCT4x4AddPairInt32(dc0, dc1, y[off:], yStride, y[off:], yStride)
		}
	}
}

// dequantIDCTAddChromaBlocks mirrors vp8_dequant_idct_add_uv_block_neon:
// chroma dispatches in the pairs (16,17), (18,19), (20,21), (22,23) with the
// same full-vs-DC pair policy as luma.
func dequantIDCTAddChromaBlocks(tokens *MacroblockTokens, uvDequant *[16]int16, u []byte, uStride int, v []byte, vStride int) {
	dequantIDCTAddChromaPair(tokens, uvDequant, 16, u, uStride)
	dequantIDCTAddChromaPair(tokens, uvDequant, 18, u[4*uStride:], uStride)
	dequantIDCTAddChromaPair(tokens, uvDequant, 20, v, vStride)
	dequantIDCTAddChromaPair(tokens, uvDequant, 22, v[4*vStride:], vStride)
}

func dequantIDCTAddChromaPair(tokens *MacroblockTokens, uvDequant *[16]int16, b int, dst []byte, stride int) {
	eob0 := tokens.EOB[b]
	eob1 := tokens.EOB[b+1]
	if eob0 == 0 && eob1 == 0 {
		return
	}
	if eob0 > 1 || eob1 > 1 {
		qflat := unsafe.Slice(&tokens.QCoeff[0][0], 25*16)
		if eob0 <= 1 {
			sanitizeDCOnlyBlock((*[16]int16)(qflat[b*16:b*16+16]), eob0, false)
		}
		if eob1 <= 1 {
			sanitizeDCOnlyBlock((*[16]int16)(qflat[b*16+16:b*16+32]), eob1, false)
		}
		dsp.DequantIDCTAddFull2x((*[32]int16)(qflat[b*16:]), uvDequant, dst, stride)
		return
	}
	var dc0, dc1 int32
	if eob0 != 0 {
		dc0 = int32(tokens.QCoeff[b][0]) * int32(uvDequant[0])
	}
	if eob1 != 0 {
		dc1 = int32(tokens.QCoeff[b+1][0]) * int32(uvDequant[0])
	}
	dsp.DCOnlyIDCT4x4AddPairInt32(dc0, dc1, dst, stride, dst, stride)
}

// AddMacroblockResidualWithDequant adds the dequantized residual onto the
// already-predicted Y/U/V samples. When dequant is non-nil and a DC-only
// (eob==1) block is encountered, the DC product is recomputed in int32
// precision instead of relying on the int16 residual buffer. This mirrors
// libvpx v1.16.0 NEON idct_dequant_0_2x_neon which performs
// ((q[0] * dq) + 4) >> 3 in int precision and only truncates to int16 after
// the shift, so wide chroma DC coefficients (e.g. 334 * 132 = 44088) survive
// the dequantize-add path without int16 wraparound. When dequant is nil the
// historical int16 path is used (preserves callers that have already
// dequantized through a wider intermediate or that operate on encoder-side
// coefficient ranges which never overflow).
//
// For Y blocks the DC dequant value is dequant.Y1DC[0] when the MB uses the
// Y2 second-order transform (Y1DC[0]==1 in that mode, so the addition is
// equivalent to keeping the Walsh-derived DC) and dequant.Y1[0] when the MB
// is a 4x4 block layout (B_PRED / SPLITMV) where qcoeff[0] carries the
// per-block DC directly. The 4x4 fix is wired through the BPred and SplitMV
// callers; for non-4x4 Y blocks Y1DC[0] is 1 by construction so the wider
// product never differs from the int16 product.
func AddMacroblockResidualWithDequant(tokens *MacroblockTokens, residual *MacroblockResidual, dequant *common.MacroblockDequant, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int) {
	addYResidualBlocks(tokens, residual, y, yStride)
	addChromaResidualWithDequant(tokens, residual, dequant, u, uStride, v, vStride)
}

func addYResidualBlocks(tokens *MacroblockTokens, residual *MacroblockResidual, y []byte, yStride int) {
	for row := range 4 {
		rowBlock := row * 4
		rowOff := row * 4 * yStride
		for col := 0; col < 4; col += 2 {
			block := rowBlock + col
			eob0 := tokens.EOB[block]
			eob1 := tokens.EOB[block+1]
			off := rowOff + col*4
			if eob0 == 1 && eob1 == 1 {
				dc0 := int32(residual.Block(block)[0])
				dc1 := int32(residual.Block(block + 1)[0])
				dsp.DCOnlyIDCT4x4AddPairInt32(dc0, dc1, y[off:], yStride, y[off:], yStride)
				continue
			}
			if eob0 != 0 {
				addTransformBlock(eob0, residual.Block(block), y[off:], yStride)
			}
			if eob1 != 0 {
				addTransformBlock(eob1, residual.Block(block+1), y[off+4:], yStride)
			}
		}
	}
}

func addChromaResidualWithDequant(tokens *MacroblockTokens, residual *MacroblockResidual, dequant *common.MacroblockDequant, u []byte, uStride int, v []byte, vStride int) {
	// libvpx v1.16.0 vp8/common/arm/neon/idct_blk_neon.c
	// vp8_dequant_idct_add_uv_block_neon (lines 256-295) dispatches by PAIRS of
	// chroma blocks: (16,17), (18,19), (20,21), (22,23). When BOTH EOBs in a
	// pair are <= 1, idct_dequant_0_2x_neon is used (DC-only, the (int16_t)a0
	// cast at line 27 is safe for realistic |q[0]*dq| <= ~32767*8). When ANY
	// EOB in the pair is > 1, idct_dequant_full_2x_neon is used and the
	// dequant multiplication wraps to int16 via vmulq_s16 (line 115). The
	// wrap matters precisely when |q[0]*dq| >= 32768 — e.g. the
	// frame-7 MB(1,1) chroma U-block-18 has q[0]=289 * UVDC=132 = 38148
	// which wraps to int16 -27388, yielding all-zero post-clip pixels rather
	// than the all-255 produced by an int32 DC-only path.
	//
	// Mirror libvpx's pair-based dispatch: when a partner block has EOB > 1,
	// route the EOB==1 block through the full int16 IDCT path so its DC
	// product wraps identically. Otherwise keep the int32 DC-only fast path
	// (matches NEON's clean (int16_t)a0 cast for in-range a0).
	addUChromaResidualPair(tokens, residual, dequant, 0, u, uStride)
	addUChromaResidualPair(tokens, residual, dequant, 2, u, uStride)
	addVChromaResidualPair(tokens, residual, dequant, 0, v, vStride)
	addVChromaResidualPair(tokens, residual, dequant, 2, v, vStride)
}

func addUChromaResidualPair(tokens *MacroblockTokens, residual *MacroblockResidual, dequant *common.MacroblockDequant, base int, u []byte, uStride int) {
	eob0 := tokens.EOB[16+base]
	eob1 := tokens.EOB[16+base+1]
	if eob0 == 1 && eob1 == 1 && dequant != nil {
		dc0 := int32(tokens.QCoeff[16+base][0]) * int32(dequant.UV[0])
		dc1 := int32(tokens.QCoeff[16+base+1][0]) * int32(dequant.UV[0])
		off := uvBlockOffset(base, uStride)
		dsp.DCOnlyIDCT4x4AddPairInt32(dc0, dc1, u[off:], uStride, u[off:], uStride)
		return
	}
	pairHasFull := eob0 > 1 || eob1 > 1
	if eob0 != 0 {
		addOneChromaBlock(tokens, residual, dequant, 16+base, base, pairHasFull, u, uStride)
	}
	if eob1 != 0 {
		addOneChromaBlock(tokens, residual, dequant, 16+base+1, base+1, pairHasFull, u, uStride)
	}
}

func addVChromaResidualPair(tokens *MacroblockTokens, residual *MacroblockResidual, dequant *common.MacroblockDequant, base int, v []byte, vStride int) {
	eob0 := tokens.EOB[20+base]
	eob1 := tokens.EOB[20+base+1]
	if eob0 == 1 && eob1 == 1 && dequant != nil {
		dc0 := int32(tokens.QCoeff[20+base][0]) * int32(dequant.UV[0])
		dc1 := int32(tokens.QCoeff[20+base+1][0]) * int32(dequant.UV[0])
		off := uvBlockOffset(base, vStride)
		dsp.DCOnlyIDCT4x4AddPairInt32(dc0, dc1, v[off:], vStride, v[off:], vStride)
		return
	}
	pairHasFull := eob0 > 1 || eob1 > 1
	if eob0 != 0 {
		addOneChromaBlock(tokens, residual, dequant, 20+base, base, pairHasFull, v, vStride)
	}
	if eob1 != 0 {
		addOneChromaBlock(tokens, residual, dequant, 20+base+1, base+1, pairHasFull, v, vStride)
	}
}

func addOneChromaBlock(tokens *MacroblockTokens, residual *MacroblockResidual, dequant *common.MacroblockDequant, blockIndex int, subBlock int, pairHasFull bool, dst []byte, stride int) {
	eob := tokens.EOB[blockIndex]
	off := uvBlockOffset(subBlock, stride)
	if pairHasFull && eob == 1 && dequant != nil {
		// libvpx routes the entire pair through idct_dequant_full_2x_neon when
		// any partner has EOB > 1. The full path's vmulq_s16(q, dq) wraps the
		// dequantized coefficients to int16. Recompute a DC-only block with
		// int16 multiplication (matching vmulq_s16) and route through the
		// full IDCT, so |q[0]*dq| >= 32768 wraps identically. The pre-existing
		// residual scratch only zeros block[0] when EOB==1, leaving stale
		// AC slots — populate a fresh DC-only block here.
		var block [16]int16
		block[0] = tokens.QCoeff[blockIndex][0] * dequant.UV[0]
		dsp.IDCT4x4Add(&block, dst[off:], stride, dst[off:], stride)
		return
	}
	if eob == 1 && dequant != nil {
		// DC-only NEON (idct_dequant_0_2x_neon) path: (int) math then
		// (int16_t)a0 cast. For realistic |q*dq| this matches int32 precision.
		dc := int32(tokens.QCoeff[blockIndex][0]) * int32(dequant.UV[0])
		dsp.DCOnlyIDCT4x4AddInt32(dc, dst[off:], stride, dst[off:], stride)
		return
	}
	if dequant != nil {
		var block [16]int16
		dequantizeInto(&tokens.QCoeff[blockIndex], &dequant.UV, eob, &block)
		dsp.IDCT4x4Add(&block, dst[off:], stride, dst[off:], stride)
		return
	}
	addTransformBlock(eob, residual.Block(blockIndex), dst[off:], stride)
}

func PredictIntraY16x16(mode common.MBPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte, upAvailable bool, leftAvailable bool) bool {
	switch mode {
	case common.DCPred:
		dsp.IntraDCPredict16x16(dst, stride, above, left, upAvailable, leftAvailable)
	case common.VPred:
		dsp.IntraVerticalPredict16x16(dst, stride, above)
	case common.HPred:
		dsp.IntraHorizontalPredict16x16(dst, stride, left)
	case common.TMPred:
		dsp.IntraTMPredict16x16(dst, stride, above, left, topLeft)
	default:
		return false
	}
	return true
}

func PredictIntraUV8x8(mode common.MBPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte, upAvailable bool, leftAvailable bool) bool {
	switch mode {
	case common.DCPred:
		dsp.IntraDCPredict8x8(dst, stride, above, left, upAvailable, leftAvailable)
	case common.VPred:
		dsp.IntraVerticalPredict8x8(dst, stride, above)
	case common.HPred:
		dsp.IntraHorizontalPredict8x8(dst, stride, left)
	case common.TMPred:
		dsp.IntraTMPredict8x8(dst, stride, above, left, topLeft)
	default:
		return false
	}
	return true
}

func PredictIntraY4x4(modes *[16]common.BPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte) bool {
	for block := range 16 {
		if ok := predictIntraY4x4Block((*modes)[block], dst, stride, above, left, topLeft, block); !ok {
			return false
		}
	}
	return true
}

func ReconstructWholeBlockIntraMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, refs IntraPredictorRefs, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual) bool {
	if mode.Is4x4 || mode.Mode == common.BPred {
		return false
	}
	if !PredictIntraY16x16(mode.Mode, y, yStride, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !PredictIntraUV8x8(mode.UVMode, u, uStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !PredictIntraUV8x8(mode.UVMode, v, vStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if mode.MBSkipCoeff {
		return true
	}
	dequantIDCTAddMacroblock(tokens, dequant, false, y, yStride, u, uStride, v, vStride)
	return true
}

func ReconstructIntraMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, refs IntraPredictorRefs, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual) bool {
	if mode.Is4x4 || mode.Mode == common.BPred {
		return ReconstructBPredIntraMacroblock(mode, tokens, dequant, refs, y, yStride, u, uStride, v, vStride, scratch)
	}
	return ReconstructWholeBlockIntraMacroblock(mode, tokens, dequant, refs, y, yStride, u, uStride, v, vStride, scratch)
}

func ReconstructKeyFrameIntraGrid(img *common.Image, rows int, cols int, modes []MacroblockMode, tokens []MacroblockTokens, dequants *[common.MaxMBSegments]common.MacroblockDequant, scratch *IntraReconstructionScratch) error {
	if rows < 0 || cols < 0 {
		return ErrReconstructGridBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrReconstructGridBufferTooSmall
	}
	required := rows * cols
	if img == nil || dequants == nil || scratch == nil || len(modes) < required || len(tokens) < required {
		return ErrReconstructGridBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrReconstructGridBufferTooSmall
	}

	for row := range rows {
		if err := reconstructKeyFrameIntraGridRow(img, row, cols, modes, tokens, dequants, scratch); err != nil {
			return err
		}
	}
	return nil
}

func reconstructKeyFrameIntraGridRow(img *common.Image, row int, cols int, modes []MacroblockMode, tokens []MacroblockTokens, dequants *[common.MaxMBSegments]common.MacroblockDequant, scratch *IntraReconstructionScratch) error {
	yRow := row * 16 * img.YStride
	uRow := row * 8 * img.UStride
	vRow := row * 8 * img.VStride
	for col := range cols {
		index := row*cols + col
		mode := &modes[index]
		if mode.SegmentID >= common.MaxMBSegments {
			return ErrUnsupportedIntraReconstructionMode
		}
		refs := BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
		yOff := yRow + col*16
		uOff := uRow + col*8
		vOff := vRow + col*8
		if !ReconstructIntraMacroblock(mode, &tokens[index], &(*dequants)[mode.SegmentID], refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual) {
			return ErrUnsupportedIntraReconstructionMode
		}
	}
	extendIntraRightEdgeForRow(img, row)
	return nil
}

func ReconstructInterFrameGrid(img *common.Image, last *common.Image, golden *common.Image, alt *common.Image, rows int, cols int, modes []MacroblockMode, tokens []MacroblockTokens, dequants *[common.MaxMBSegments]common.MacroblockDequant, scratch *IntraReconstructionScratch) error {
	return ReconstructInterFrameGridWithConfig(img, last, golden, alt, rows, cols, modes, tokens, dequants, scratch, InterPredictionConfig{})
}

func ReconstructInterFrameGridWithConfig(img *common.Image, last *common.Image, golden *common.Image, alt *common.Image, rows int, cols int, modes []MacroblockMode, tokens []MacroblockTokens, dequants *[common.MaxMBSegments]common.MacroblockDequant, scratch *IntraReconstructionScratch, cfg InterPredictionConfig) error {
	if rows < 0 || cols < 0 {
		return ErrReconstructGridBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrReconstructGridBufferTooSmall
	}
	required := rows * cols
	if img == nil || last == nil || golden == nil || alt == nil || dequants == nil || scratch == nil || len(modes) < required || len(tokens) < required {
		return ErrReconstructGridBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) || !imageHasMacroblockGrid(last, rows, cols) || !imageHasMacroblockGrid(golden, rows, cols) || !imageHasMacroblockGrid(alt, rows, cols) {
		return ErrReconstructGridBufferTooSmall
	}

	// Cache frame-level reference state once per reference image to
	// amortize codedWidth/Height resolution, plane lookups, and
	// origin/border math out of the per-MB inter-predictor builder.
	lastState := newFrameInterRefState(last, cfg)
	goldenState := newFrameInterRefState(golden, cfg)
	altState := newFrameInterRefState(alt, cfg)

	for row := range rows {
		if err := reconstructInterFrameGridRow(img, last, golden, alt, &lastState, &goldenState, &altState, row, cols, modes, tokens, dequants, scratch, cfg); err != nil {
			return err
		}
	}
	return nil
}

func reconstructInterFrameGridRow(img *common.Image, last *common.Image, golden *common.Image, alt *common.Image, lastState *frameInterRefState, goldenState *frameInterRefState, altState *frameInterRefState, row int, cols int, modes []MacroblockMode, tokens []MacroblockTokens, dequants *[common.MaxMBSegments]common.MacroblockDequant, scratch *IntraReconstructionScratch, cfg InterPredictionConfig) error {
	yRow := row * 16 * img.YStride
	uRow := row * 8 * img.UStride
	vRow := row * 8 * img.VStride
	rowBase := row * cols
	rowModes := modes[rowBase : rowBase+cols]
	rowTokens := tokens[rowBase : rowBase+cols]
	for col := 0; col < cols; col++ {
		mode := &rowModes[col]
		if mode.SegmentID >= common.MaxMBSegments {
			return ErrUnsupportedInterReconstructionMode
		}
		yOff := yRow + col*16
		uOff := uRow + col*8
		vOff := vRow + col*8
		if mode.RefFrame == common.IntraFrame {
			refs := BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
			if !ReconstructIntraMacroblock(mode, &rowTokens[col], &(*dequants)[mode.SegmentID], refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual) {
				return ErrUnsupportedInterReconstructionMode
			}
			continue
		}

		var refState *frameInterRefState
		var ref *common.Image
		switch mode.RefFrame {
		case common.LastFrame:
			refState = lastState
			ref = last
		case common.GoldenFrame:
			refState = goldenState
			ref = golden
		case common.AltRefFrame:
			refState = altState
			ref = alt
		default:
			return ErrUnsupportedInterReconstructionMode
		}
		if ref != img && mode.Mode == common.ZeroMV && mode.MBSkipCoeff && !mode.Is4x4 && mode.MV.IsZero() {
			run := zeroMVSkippedRunLength(rowModes, col, cols, mode.RefFrame)
			if run > 1 {
				if copyZeroMVInterMacroblockRunFast(refState, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, row, col, run) {
					col += run - 1
					continue
				}
			}
			if run == 1 && copyZeroMVInterMacroblockFast(refState, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, row, col) {
				continue
			}
		}
		if mode.Mode == common.SplitMV {
			if !reconstructSplitMVInterMacroblockFast(refState, mode, &rowTokens[col], &(*dequants)[mode.SegmentID], img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, cfg) {
				return ErrUnsupportedInterReconstructionMode
			}
			continue
		}
		if !reconstructWholeMVInterMacroblockFast(refState, mode, &rowTokens[col], &(*dequants)[mode.SegmentID], img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col) {
			return ErrUnsupportedInterReconstructionMode
		}
	}
	extendIntraRightEdgeForRow(img, row)
	return nil
}

func zeroMVSkippedRunLength(rowModes []MacroblockMode, start int, cols int, ref common.MVReferenceFrame) int {
	if start < 0 || start >= cols {
		return 0
	}
	// The caller already validated rowModes[start]. Start scanning at the next
	// MB so the hot skipped-ZeroMV path does not re-check the current mode.
	run := 1
	for col := start + 1; col < cols; col++ {
		mode := &rowModes[col]
		if mode.SegmentID >= common.MaxMBSegments ||
			mode.RefFrame != ref ||
			mode.Mode != common.ZeroMV ||
			mode.Is4x4 ||
			!mode.MBSkipCoeff ||
			!mode.MV.IsZero() {
			break
		}
		run++
	}
	return run
}

func ReconstructWholeMVInterMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, ref *common.Image, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual, mbRow int, mbCol int, cfg InterPredictionConfig) bool {
	if ref == nil {
		return false
	}
	state := newFrameInterRefState(ref, cfg)
	return reconstructWholeMVInterMacroblockFast(&state, mode, tokens, dequant, y, yStride, u, uStride, v, vStride, scratch, mbRow, mbCol)
}

func ReconstructSplitMVInterMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, ref *common.Image, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual, mbRow int, mbCol int, cfg InterPredictionConfig) bool {
	if mode.RefFrame == common.IntraFrame || mode.Mode != common.SplitMV || !mode.Is4x4 {
		return false
	}
	yPlane, yOrigin, yBorder := referencePlane(ref.Y, ref.YFull, ref.YOrigin, ref.YBorder)
	codedWidth := codedImageWidth(ref)
	codedHeight := codedImageHeight(ref)
	if mode.Partition < 3 {
		for _, block := range [...]int{0, 2, 8, 10} {
			mv := mode.BlockMV[block]
			mv = clampMotionVectorToUMVBorder(mv, mbRow, mbCol, codedWidth, codedHeight)
			if !predictSplitMVLuma8x8(yPlane, ref.YStride, codedWidth, codedHeight,
				yOrigin, yBorder, y, yStride, mbRow, mbCol, block, mv,
				cfg) {
				return false
			}
		}
	} else {
		for block := 0; block < 16; block += 2 {
			mv0 := mode.BlockMV[block]
			mv1 := mode.BlockMV[block+1]
			mv0 = clampMotionVectorToUMVBorder(mv0, mbRow, mbCol, codedWidth, codedHeight)
			mv1 = clampMotionVectorToUMVBorder(mv1, mbRow, mbCol, codedWidth, codedHeight)
			if mv0 == mv1 {
				if !predictSplitMVLuma8x4(yPlane, ref.YStride, codedWidth, codedHeight,
					yOrigin, yBorder, y, yStride, mbRow, mbCol, block, mv0,
					cfg) {
					return false
				}
				continue
			}
			blockRow := block >> 2
			blockCol := block & 3
			srcRow := mbRow*16 + blockRow*4 + int(mv0.Row>>3)
			srcCol := mbCol*16 + blockCol*4 + int(mv0.Col>>3)
			xOffset := int(mv0.Col) & 7
			yOffset := int(mv0.Row) & 7
			offset, ok := wholeMVPlaneOffset(yPlane, ref.YStride, codedWidth, codedHeight,
				srcRow, srcCol, 4, 4, xOffset, yOffset, yOrigin, yBorder, cfg)
			if !ok {
				return false
			}
			predictInter4x4(yPlane[offset:], ref.YStride, xOffset, yOffset,
				y[yBlockOffset(block, yStride):], yStride, cfg)

			block1 := block + 1
			blockRow = block1 >> 2
			blockCol = block1 & 3
			srcRow = mbRow*16 + blockRow*4 + int(mv1.Row>>3)
			srcCol = mbCol*16 + blockCol*4 + int(mv1.Col>>3)
			xOffset = int(mv1.Col) & 7
			yOffset = int(mv1.Row) & 7
			offset, ok = wholeMVPlaneOffset(yPlane, ref.YStride, codedWidth, codedHeight,
				srcRow, srcCol, 4, 4, xOffset, yOffset, yOrigin, yBorder, cfg)
			if !ok {
				return false
			}
			predictInter4x4(yPlane[offset:], ref.YStride, xOffset, yOffset,
				y[yBlockOffset(block1, yStride):], yStride, cfg)
		}
	}

	if !PredictSplitMVInterChroma(mode, ref, u, uStride, v, vStride, mbRow, mbCol, cfg) {
		return false
	}

	if mode.MBSkipCoeff {
		return true
	}
	dequantIDCTAddMacroblock(tokens, dequant, true, y, yStride, u, uStride, v, vStride)
	return true
}

// PredictSplitMVInterChroma writes only the inter chroma predictor for a
// SPLITMV macroblock.
func PredictSplitMVInterChroma(mode *MacroblockMode, ref *common.Image, u []byte, uStride int, v []byte, vStride int, mbRow int, mbCol int, cfg InterPredictionConfig) bool {
	if mode == nil || ref == nil || mode.RefFrame == common.IntraFrame || mode.Mode != common.SplitMV || !mode.Is4x4 {
		return false
	}
	uPlane, uOrigin, uvBorder := referencePlane(ref.U, ref.UFull, ref.UOrigin, ref.UVBorder)
	vPlane, vOrigin, _ := referencePlane(ref.V, ref.VFull, ref.VOrigin, ref.UVBorder)
	codedWidth := codedImageWidth(ref)
	codedHeight := codedImageHeight(ref)
	uvWidth := (codedWidth + 1) >> 1
	uvHeight := (codedHeight + 1) >> 1
	var mv [4]splitMVChromaVector
	for block := range mv {
		mvRow, mvCol := splitChromaMotionVector(mode, block)
		mvRow, mvCol = fullPixelChromaMotionVector(mvRow, mvCol, cfg)
		mvRow, mvCol = clampChromaMotionVectorToUMVBorder(mvRow, mvCol, mbRow, mbCol, codedWidth, codedHeight)
		mv[block] = splitMVChromaVector{row: mvRow, col: mvCol}
	}
	for block := 0; block < 4; block += 2 {
		if mv[block] == mv[block+1] {
			if !predictSplitMVChroma8x4(uPlane, ref.UStride, uvWidth, uvHeight,
				uOrigin, uvBorder, u, uStride, mbRow, mbCol, block, mv[block],
				cfg) {
				return false
			}
			if !predictSplitMVChroma8x4(vPlane, ref.VStride, uvWidth, uvHeight,
				vOrigin, uvBorder, v, vStride, mbRow, mbCol, block, mv[block],
				cfg) {
				return false
			}
			continue
		}
		for sub := block; sub <= block+1; sub++ {
			blockRow := sub >> 1
			blockCol := sub & 1
			srcRow := mbRow*8 + blockRow*4 + (mv[sub].row >> 3)
			srcCol := mbCol*8 + blockCol*4 + (mv[sub].col >> 3)
			xOffset := mv[sub].col & 7
			yOffset := mv[sub].row & 7
			uOffset, ok := wholeMVPlaneOffset(uPlane, ref.UStride, uvWidth, uvHeight,
				srcRow, srcCol, 4, 4, xOffset, yOffset, uOrigin, uvBorder, cfg)
			if !ok {
				return false
			}
			vOffset, ok := wholeMVPlaneOffset(vPlane, ref.VStride, uvWidth, uvHeight,
				srcRow, srcCol, 4, 4, xOffset, yOffset, vOrigin, uvBorder, cfg)
			if !ok {
				return false
			}
			predictInter4x4(uPlane[uOffset:], ref.UStride, xOffset, yOffset,
				u[uvBlockOffset(sub, uStride):], uStride, cfg)
			predictInter4x4(vPlane[vOffset:], ref.VStride, xOffset, yOffset,
				v[uvBlockOffset(sub, vStride):], vStride, cfg)
		}
	}
	return true
}

type splitMVChromaVector struct {
	row int
	col int
}

func referencePlane(visible []byte, full []byte, origin int, border int) ([]byte, int, int) {
	if len(full) == 0 {
		return visible, 0, 0
	}
	return full, origin, border
}

func wholeMVPlaneOffset(plane []byte, stride int, codedWidth int, codedHeight int, row int, col int, width int, height int, xOffset int, yOffset int, origin int, border int, cfg InterPredictionConfig) (int, bool) {
	if xOffset|yOffset != 0 {
		if cfg.UseBilinear {
			width++
			height++
		} else {
			row -= 2
			col -= 2
			width += 5
			height += 5
		}
	}
	if !imageHasReferenceBlock(plane, stride, codedWidth, codedHeight, row, col, width, height, origin, border) {
		return 0, false
	}
	return origin + row*stride + col, true
}

func predictSplitMVLuma8x4(plane []byte, planeStride int, codedWidth int, codedHeight int,
	origin int, border int, dst []byte, dstStride int, mbRow int, mbCol int,
	block int, mv MotionVector, cfg InterPredictionConfig,
) bool {
	blockRow := block >> 2
	blockCol := block & 3
	srcRow := mbRow*16 + blockRow*4 + int(mv.Row>>3)
	srcCol := mbCol*16 + blockCol*4 + int(mv.Col>>3)
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	offset, ok := wholeMVPlaneOffset(plane, planeStride, codedWidth, codedHeight,
		srcRow, srcCol, 8, 4, xOffset, yOffset, origin, border, cfg)
	if !ok {
		return false
	}
	predictInter8x4(plane[offset:], planeStride, xOffset, yOffset,
		dst[yBlockOffset(block, dstStride):], dstStride, cfg)
	return true
}

func predictSplitMVLuma8x8(plane []byte, planeStride int, codedWidth int, codedHeight int,
	origin int, border int, dst []byte, dstStride int, mbRow int, mbCol int,
	block int, mv MotionVector, cfg InterPredictionConfig,
) bool {
	blockRow := block >> 2
	blockCol := block & 3
	srcRow := mbRow*16 + blockRow*4 + int(mv.Row>>3)
	srcCol := mbCol*16 + blockCol*4 + int(mv.Col>>3)
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	offset, ok := wholeMVPlaneOffset(plane, planeStride, codedWidth, codedHeight,
		srcRow, srcCol, 8, 8, xOffset, yOffset, origin, border, cfg)
	if !ok {
		return false
	}
	predictInter8x8(plane[offset:], planeStride, xOffset, yOffset,
		dst[yBlockOffset(block, dstStride):], dstStride, cfg)
	return true
}

func predictSplitMVChroma8x4(plane []byte, planeStride int, codedWidth int, codedHeight int,
	origin int, border int, dst []byte, dstStride int, mbRow int, mbCol int,
	block int, mv splitMVChromaVector, cfg InterPredictionConfig,
) bool {
	blockRow := block >> 1
	blockCol := block & 1
	srcRow := mbRow*8 + blockRow*4 + (mv.row >> 3)
	srcCol := mbCol*8 + blockCol*4 + (mv.col >> 3)
	xOffset := mv.col & 7
	yOffset := mv.row & 7
	offset, ok := wholeMVPlaneOffset(plane, planeStride, codedWidth, codedHeight,
		srcRow, srcCol, 8, 4, xOffset, yOffset, origin, border, cfg)
	if !ok {
		return false
	}
	predictInter8x4(plane[offset:], planeStride, xOffset, yOffset,
		dst[uvBlockOffset(block, dstStride):], dstStride, cfg)
	return true
}

func predictInter8x8(src []byte, srcStride int, xOffset int, yOffset int, dst []byte, dstStride int, cfg InterPredictionConfig) {
	if xOffset|yOffset == 0 {
		dsp.Copy8x8(src, srcStride, dst, dstStride)
		return
	}
	if cfg.UseBilinear {
		dsp.BilinearPredict8x8(src, srcStride, xOffset, yOffset, dst, dstStride)
		return
	}
	dsp.SixTapPredict8x8(src, srcStride, xOffset, yOffset, dst, dstStride)
}

func predictInter8x4(src []byte, srcStride int, xOffset int, yOffset int, dst []byte, dstStride int, cfg InterPredictionConfig) {
	if xOffset|yOffset == 0 {
		dsp.Copy8x4(src, srcStride, dst, dstStride)
		return
	}
	if cfg.UseBilinear {
		dsp.BilinearPredict8x4(src, srcStride, xOffset, yOffset, dst, dstStride)
		return
	}
	dsp.SixTapPredict8x4(src, srcStride, xOffset, yOffset, dst, dstStride)
}

func predictInter4x4(src []byte, srcStride int, xOffset int, yOffset int, dst []byte, dstStride int, cfg InterPredictionConfig) {
	if xOffset|yOffset == 0 {
		copyInter4x4(src, srcStride, dst, dstStride)
		return
	}
	if cfg.UseBilinear {
		dsp.BilinearPredict4x4(src, srcStride, xOffset, yOffset, dst, dstStride)
		return
	}
	dsp.SixTapPredict4x4(src, srcStride, xOffset, yOffset, dst, dstStride)
}

func copyInter4x4(src []byte, srcStride int, dst []byte, dstStride int) {
	for row := range 4 {
		copy(dst[row*dstStride:row*dstStride+4], src[row*srcStride:row*srcStride+4])
	}
}

func splitChromaMotionVector(mode *MacroblockMode, block int) (int, int) {
	yBlock := (block>>1)*8 + (block&1)*2
	row := splitChromaMotionVectorComponent(
		mode.BlockMV[yBlock].Row,
		mode.BlockMV[yBlock+1].Row,
		mode.BlockMV[yBlock+4].Row,
		mode.BlockMV[yBlock+5].Row,
	)
	col := splitChromaMotionVectorComponent(
		mode.BlockMV[yBlock].Col,
		mode.BlockMV[yBlock+1].Col,
		mode.BlockMV[yBlock+4].Col,
		mode.BlockMV[yBlock+5].Col,
	)
	return row, col
}

func splitChromaMotionVectorComponent(a int16, b int16, c int16, d int16) int {
	sum := int(a) + int(b) + int(c) + int(d)
	// Branchless rounding away from zero by 4 before the /8: sign-mask
	// flips +4 to -4 when sum is negative.
	mask := sum >> intSignShiftDec
	return (sum + 4 + 8*mask) / 8
}

func fullPixelChromaMotionVector(row int, col int, cfg InterPredictionConfig) (int, int) {
	if !cfg.FullPixel {
		return row, col
	}
	return row &^ 7, col &^ 7
}

func clampMotionVectorToUMVBorder(mv MotionVector, mbRow int, mbCol int, codedWidth int, codedHeight int) MotionVector {
	top, bottom, left, right := macroblockMotionVectorEdges(mbRow, mbCol, codedWidth, codedHeight)
	return MotionVector{
		Row: int16(clampUMVComponent(int(mv.Row), top, bottom)),
		Col: int16(clampUMVComponent(int(mv.Col), left, right)),
	}
}

func clampChromaMotionVectorToUMVBorder(row int, col int, mbRow int, mbCol int, codedWidth int, codedHeight int) (int, int) {
	top, bottom, left, right := macroblockMotionVectorEdges(mbRow, mbCol, codedWidth, codedHeight)
	return clampChromaUMVComponent(row, top, bottom), clampChromaUMVComponent(col, left, right)
}

func macroblockMotionVectorEdges(mbRow int, mbCol int, codedWidth int, codedHeight int) (int, int, int, int) {
	mbRows := codedHeight >> 4
	mbCols := codedWidth >> 4
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return top, bottom, left, right
}

func clampUMVComponent(v int, lowEdge int, highEdge int) int {
	if v < lowEdge-(19<<3) {
		return lowEdge - (16 << 3)
	}
	if v > highEdge+(18<<3) {
		return highEdge + (16 << 3)
	}
	return v
}

func clampChromaUMVComponent(v int, lowEdge int, highEdge int) int {
	if 2*v < lowEdge-(19<<3) {
		return (lowEdge - (16 << 3)) >> 1
	}
	if 2*v > highEdge+(18<<3) {
		return (highEdge + (16 << 3)) >> 1
	}
	return v
}

func ReconstructBPredIntraMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, refs IntraPredictorRefs, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual) bool {
	if !mode.Is4x4 || mode.Mode != common.BPred {
		return false
	}
	if !PredictIntraUV8x8(mode.UVMode, u, uStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !PredictIntraUV8x8(mode.UVMode, v, vStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if mode.MBSkipCoeff {
		return PredictIntraY4x4(&mode.BModes, y, yStride, refs.YAbove, refs.YLeft, refs.YTopLeft)
	}

	transformMacroblockTokensLuma(tokens, dequant, true, scratch)
	for block := range 16 {
		if ok := predictIntraY4x4Block(mode.BModes[block], y, yStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block); !ok {
			return false
		}
		if eob := tokens.EOB[block]; eob != 0 {
			if eob == 1 {
				dc := int32(tokens.QCoeff[block][0]) * int32(dequant.Y1[0])
				dsp.DCOnlyIDCT4x4AddInt32(dc, y[yBlockOffset(block, yStride):], yStride, y[yBlockOffset(block, yStride):], yStride)
			} else {
				addTransformBlock(eob, scratch.Block(block), y[yBlockOffset(block, yStride):], yStride)
			}
		}
	}
	dequantIDCTAddChromaBlocks(tokens, &dequant.UV, u, uStride, v, vStride)
	return true
}

func clearYResidualBlocks(out *MacroblockResidual) {
	clear(out.DQCoeff[:16*16])
}

func clearResidualBlock(block *[16]int16) {
	*block = [16]int16{}
}

func dequantizeInto(qcoeff *[16]int16, dequant *[16]int16, eob uint8, out *[16]int16) {
	if eob == 1 {
		out[0] += qcoeff[0] * dequant[0]
		return
	}
	limit := min(int(eob), 16)
	for i := range limit {
		index := int(tables.DefaultZigZag1D[i])
		out[index] += qcoeff[index] * dequant[index]
	}
}

// ExtendIntraRightEdgeForRow replicates the right-edge predictor samples after
// reconstructing one macroblock row.
func ExtendIntraRightEdgeForRow(img *common.Image, mbRow int) {
	extendIntraRightEdgeForRow(img, mbRow)
}

func extendIntraRightEdgeForRow(img *common.Image, mbRow int) {
	if img == nil {
		return
	}
	codedWidth := codedImageWidth(img)
	codedHeight := codedImageHeight(img)
	extendIntraRightEdgeRows(img.YFull, img.YOrigin, img.YStride, codedWidth, codedHeight, img.YBorder, mbRow*16+14, 2)

	uvWidth := (codedWidth + 1) >> 1
	uvHeight := (codedHeight + 1) >> 1
	extendIntraRightEdgeRows(img.UFull, img.UOrigin, img.UStride, uvWidth, uvHeight, img.UVBorder, mbRow*8+6, 2)
	extendIntraRightEdgeRows(img.VFull, img.VOrigin, img.VStride, uvWidth, uvHeight, img.UVBorder, mbRow*8+6, 2)
}

func extendIntraRightEdgeRows(full []byte, origin int, stride int, width int, height int, border int, startRow int, count int) {
	if len(full) == 0 || origin < 0 || stride <= 0 || width <= 0 || height <= 0 || border < 4 || count <= 0 {
		return
	}
	if stride < width+border {
		return
	}
	for i := range count {
		row := startRow + i
		if uint(row) >= uint(height) {
			continue
		}
		start := origin + row*stride + width
		if start <= 0 || start+4 > len(full) {
			continue
		}
		edge := full[start-1]
		full[start+0] = edge
		full[start+1] = edge
		full[start+2] = edge
		full[start+3] = edge
	}
}

func buildAbove(out []byte, plane []byte, full []byte, origin int, stride int, width int, row int, col int, border int, available bool) {
	if !available {
		for i := range out {
			out[i] = 127
		}
		return
	}
	if hasFullIntraSampleRange(full, origin, stride, width, border, row-1, col, len(out)) {
		copy(out, full[origin+(row-1)*stride+col:])
		return
	}
	src := (row-1)*stride + col
	for i := range out {
		if col+i < width {
			out[i] = plane[src+i]
		} else {
			out[i] = 127
		}
	}
}

func buildLeft(out []byte, plane []byte, full []byte, origin int, stride int, height int, row int, col int, border int, available bool) {
	if !available {
		for i := range out {
			out[i] = 129
		}
		return
	}
	if hasFullIntraVerticalRange(full, origin, stride, height, border, row, col-1, len(out)) {
		src := origin + row*stride + col - 1
		for i := range out {
			out[i] = full[src+i*stride]
		}
		return
	}
	src := row*stride + col - 1
	for i := range out {
		if row+i < height {
			out[i] = plane[src+i*stride]
		} else {
			out[i] = 129
		}
	}
}

func topLeftSample(plane []byte, full []byte, origin int, stride int, row int, col int, border int, upAvailable bool, leftAvailable bool) byte {
	if !upAvailable {
		return 127
	}
	if !leftAvailable {
		return 129
	}
	if hasFullIntraSampleRange(full, origin, stride, col, border, row-1, col-1, 1) {
		return full[origin+(row-1)*stride+col-1]
	}
	return plane[(row-1)*stride+col-1]
}

func hasFullIntraSampleRange(full []byte, origin int, stride int, width int, border int, row int, col int, count int) bool {
	if count < 0 || len(full) == 0 || origin < 0 || stride <= 0 || border <= 0 || row < 0 || col < 0 {
		return false
	}
	if col+count > width+border {
		return false
	}
	start := origin + row*stride + col
	return start >= 0 && start+count <= len(full)
}

func hasFullIntraVerticalRange(full []byte, origin int, stride int, height int, border int, row int, col int, count int) bool {
	if count < 0 || len(full) == 0 || origin < 0 || stride <= 0 || border <= 0 || row < 0 || col < 0 {
		return false
	}
	if row+count > height+border {
		return false
	}
	start := origin + row*stride + col
	return start >= 0 && start+(count-1)*stride < len(full)
}

func imageHasMacroblockGrid(img *common.Image, rows int, cols int) bool {
	if img.YStride <= 0 || img.UStride <= 0 || img.VStride <= 0 {
		return false
	}
	yWidth := cols * 16
	yHeight := rows * 16
	uvWidth := cols * 8
	uvHeight := rows * 8
	if codedImageWidth(img) < yWidth || codedImageHeight(img) < yHeight {
		return false
	}
	return planeHasBlock(img.Y, img.YStride, yWidth, yHeight) &&
		planeHasBlock(img.U, img.UStride, uvWidth, uvHeight) &&
		planeHasBlock(img.V, img.VStride, uvWidth, uvHeight)
}

func planeHasBlock(plane []byte, stride int, width int, height int) bool {
	if width < 0 || height < 0 || stride < width {
		return false
	}
	if height == 0 {
		return true
	}
	need := (height-1)*stride + width
	return need <= len(plane)
}

func imageHasReferenceBlock(plane []byte, stride int, codedWidth int, codedHeight int, row int, col int, width int, height int, origin int, border int) bool {
	if width < 0 || height < 0 || border < 0 || origin < 0 || stride < codedWidth+border*2 {
		return false
	}
	if width > codedWidth+border*2 || height > codedHeight+border*2 {
		return false
	}
	if row < -border || col < -border {
		return false
	}
	if col > codedWidth+border-width || row > codedHeight+border-height {
		return false
	}
	if height == 0 {
		return true
	}
	start := origin + row*stride + col
	if start < 0 {
		return false
	}
	need := start + (height-1)*stride + width
	return need <= len(plane)
}

func codedImageWidth(img *common.Image) int {
	if img.CodedWidth > 0 {
		return img.CodedWidth
	}
	return img.Width
}

func codedImageHeight(img *common.Image) int {
	if img.CodedHeight > 0 {
		return img.CodedHeight
	}
	return img.Height
}

func predictIntraY4x4Block(mode common.BPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte, block int) bool {
	blockRow := block >> 2
	blockCol := block & 3
	y := blockRow * 4
	x := blockCol * 4
	var blockAbove [8]byte
	var blockLeft [4]byte

	if blockRow == 0 {
		copy(blockAbove[:], above[x:x+8])
	} else {
		aboveOff := (y-1)*stride + x
		copy(blockAbove[:4], dst[aboveOff:aboveOff+4])
		if blockCol < 3 {
			copy(blockAbove[4:], dst[aboveOff+4:aboveOff+8])
		} else {
			copy(blockAbove[4:], above[16:20])
		}
	}

	if blockCol == 0 {
		copy(blockLeft[:], left[y:y+4])
	} else {
		for i := range 4 {
			blockLeft[i] = dst[(y+i)*stride+x-1]
		}
	}

	blockTopLeft := topLeft
	switch {
	case blockRow == 0 && blockCol == 0:
	case blockRow == 0:
		blockTopLeft = above[x-1]
	case blockCol == 0:
		blockTopLeft = left[y-1]
	default:
		blockTopLeft = dst[(y-1)*stride+x-1]
	}

	return dsp.Intra4x4Predict(dst[y*stride+x:], stride, mode, blockAbove[:], blockLeft[:], blockTopLeft)
}

func addTransformBlock(eob uint8, coeff *[16]int16, dst []byte, stride int) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dsp.DCOnlyIDCT4x4Add(coeff[0], dst, stride, dst, stride)
		return
	}
	dsp.IDCT4x4Add(coeff, dst, stride, dst, stride)
}

func yBlockOffset(block int, stride int) int {
	return (block>>2)*4*stride + (block&3)*4
}

func uvBlockOffset(block int, stride int) int {
	return (block>>1)*4*stride + (block&1)*4
}
