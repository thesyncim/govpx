package decoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

// Tightened whole-MB inter predictor builder. The public per-MB API
// (ReconstructWholeMVInterMacroblock) is preserved unchanged for
// correctness and external callers; the grid loop (driven from
// ReconstructInterFrameGridWithConfig) instead routes through the
// precomputed-state path below to amortize per-MB plane lookups,
// codedWidth/Height resolution, and config branches.
//
// Mirrors the per-MB dispatch shape of libvpx v1.16.0
// vp8/common/reconinter.c vp8_build_inter_predictors_mb /
// vp8_build_inter16x16_predictors_mb (with libvpx's clamp_mv_to_umv_border
// and the chroma +1|sign / fullpixel_mask MV derivation). The Y MV path
// matches vp8_build_inter16x16_predictors_mb and the UV MV path mirrors
// the inline UV adjustment performed by the same function in libvpx.
//
// All output is byte-identical to the original scalar path; the
// per-MB validation gates remain in place and any failure reverts to
// returning false so the caller can surface ErrUnsupportedInterReconstructionMode.

// frameInterRefState caches per-reference-frame plane addresses,
// strides, borders, and coded dimensions so the inner loop only does
// per-MB work. Built once per ReconstructInterFrameGridWithConfig
// reference image.
type frameInterRefState struct {
	yPlane []byte
	uPlane []byte
	vPlane []byte

	yStride int
	uStride int
	vStride int

	yOrigin  int
	uOrigin  int
	vOrigin  int
	yBorder  int
	uvBorder int

	codedWidth  int
	codedHeight int

	uvWidth  int
	uvHeight int

	mbRows int
	mbCols int

	// Cached bounds limits used by imageHasReferenceBlock. These are
	// constant per-reference and per-plane so we evaluate them once
	// and re-use across all 3600 macroblocks of a 720p frame.
	yMaxRowFor16  int // codedHeight + yBorder - 16
	yMaxColFor16  int // codedWidth  + yBorder - 16
	yMaxRowFor21  int // codedHeight + yBorder - 21
	yMaxColFor21  int // codedWidth  + yBorder - 21
	yMaxRowFor17  int // codedHeight + yBorder - 17 (bilinear)
	yMaxColFor17  int // codedWidth  + yBorder - 17 (bilinear)
	yMinRow       int // -yBorder
	yMinCol       int // -yBorder
	uvMaxRowFor8  int
	uvMaxColFor8  int
	uvMaxRowFor13 int
	uvMaxColFor13 int
	uvMaxRowFor9  int
	uvMaxColFor9  int
	uvMinRow      int
	uvMinCol      int

	useBilinear bool
	fullPixel   bool
}

func newFrameInterRefState(ref *common.Image, cfg InterPredictionConfig) frameInterRefState {
	yPlane, yOrigin, yBorder := referencePlane(ref.Y, ref.YFull, ref.YOrigin, ref.YBorder)
	uPlane, uOrigin, uvBorder := referencePlane(ref.U, ref.UFull, ref.UOrigin, ref.UVBorder)
	vPlane, vOrigin, _ := referencePlane(ref.V, ref.VFull, ref.VOrigin, ref.UVBorder)
	cw := codedImageWidth(ref)
	ch := codedImageHeight(ref)
	uvW := (cw + 1) >> 1
	uvH := (ch + 1) >> 1
	return frameInterRefState{
		yPlane:      yPlane,
		uPlane:      uPlane,
		vPlane:      vPlane,
		yStride:     ref.YStride,
		uStride:     ref.UStride,
		vStride:     ref.VStride,
		yOrigin:     yOrigin,
		uOrigin:     uOrigin,
		vOrigin:     vOrigin,
		yBorder:     yBorder,
		uvBorder:    uvBorder,
		codedWidth:  cw,
		codedHeight: ch,
		uvWidth:     uvW,
		uvHeight:    uvH,
		mbRows:      ch >> 4,
		mbCols:      cw >> 4,

		yMaxRowFor16:  ch + yBorder - 16,
		yMaxColFor16:  cw + yBorder - 16,
		yMaxRowFor21:  ch + yBorder - 21,
		yMaxColFor21:  cw + yBorder - 21,
		yMaxRowFor17:  ch + yBorder - 17,
		yMaxColFor17:  cw + yBorder - 17,
		yMinRow:       -yBorder,
		yMinCol:       -yBorder,
		uvMaxRowFor8:  uvH + uvBorder - 8,
		uvMaxColFor8:  uvW + uvBorder - 8,
		uvMaxRowFor13: uvH + uvBorder - 13,
		uvMaxColFor13: uvW + uvBorder - 13,
		uvMaxRowFor9:  uvH + uvBorder - 9,
		uvMaxColFor9:  uvW + uvBorder - 9,
		uvMinRow:      -uvBorder,
		uvMinCol:      -uvBorder,

		useBilinear: cfg.UseBilinear,
		fullPixel:   cfg.FullPixel,
	}
}

// reconstructWholeMVInterMacroblockFast is the fast-path equivalent of
// ReconstructWholeMVInterMacroblock for use inside the grid loop where
// precomputed reference state is available. Returns false on validation
// failure so the caller can fall back / surface the error.
//
//go:nosplit
func reconstructWholeMVInterMacroblockFast(state *frameInterRefState, mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual, mbRow int, mbCol int) bool {
	// Validation gates are intentionally kept identical to the
	// pre-existing ReconstructWholeMVInterMacroblock to ensure
	// byte-identical behavior on inputs the slow path would reject.
	if mode.RefFrame == common.IntraFrame || mode.Is4x4 || !common.IsWholeInterMacroblockMode(mode.Mode) {
		return false
	}
	if mode.Mode == common.ZeroMV && !mode.MV.IsZero() {
		return false
	}
	if mode.Mode == common.ZeroMV {
		if !copyZeroMVInterMacroblockFast(state, y, yStride, u, uStride, v, vStride, mbRow, mbCol) {
			return false
		}
		if mode.MBSkipCoeff {
			return true
		}
		dequantIDCTAddMacroblock(tokens, dequant, false, y, yStride, u, uStride, v, vStride)
		return true
	}

	mvRow := int(mode.MV.Row)
	mvCol := int(mode.MV.Col)
	// libvpx v1.16.0 vp8/common/reconinter.c vp8_build_inter16x16_predictors_mb
	// applies xd->fullpixel_mask ONLY to the chroma MV (lines 333-334) after
	// the +1|sign / divide-by-2 derivation. The luma MV is consumed as-is by
	// subpixel_predict16x16. We mirror that here: leave mvRow/mvCol untouched
	// for luma, and apply &^=7 only inside the chroma derivation below.
	// Inline clampMotionVectorToUMVBorder using the cached coded
	// dimensions (and the precomputed mbRows/mbCols). Identical to
	// macroblockMotionVectorEdges -> clampUMVComponent.
	mbRows := state.mbRows
	mbCols := state.mbCols
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	if mvRow < top-(19<<3) {
		mvRow = top - (16 << 3)
	} else if mvRow > bottom+(18<<3) {
		mvRow = bottom + (16 << 3)
	}
	if mvCol < left-(19<<3) {
		mvCol = left - (16 << 3)
	} else if mvCol > right+(18<<3) {
		mvCol = right + (16 << 3)
	}

	// --- Y plane offset & predict ---
	yMVRow := mvRow >> 3
	yMVCol := mvCol >> 3
	yXOffset := mvCol & 7
	yYOffset := mvRow & 7
	yRow := mbRow*16 + yMVRow
	yCol := mbCol*16 + yMVCol

	// Fast bounds check using the per-frame cached limits. The
	// reference frame state was constructed with positive width/height
	// border/origin/stride relations validated; the only per-MB
	// variables are the (potentially MV-shifted) row/col, plus the
	// pre-known shape (16x16, 21x21 sixtap-tap window, or 17x17
	// bilinear window).
	yRow2, yCol2 := yRow, yCol
	var yMaxRow, yMaxCol int
	if (yXOffset | yYOffset) == 0 {
		yMaxRow = state.yMaxRowFor16
		yMaxCol = state.yMaxColFor16
	} else if state.useBilinear {
		yMaxRow = state.yMaxRowFor17
		yMaxCol = state.yMaxColFor17
	} else {
		yRow2 -= 2
		yCol2 -= 2
		yMaxRow = state.yMaxRowFor21
		yMaxCol = state.yMaxColFor21
	}
	if yRow2 < state.yMinRow || yCol2 < state.yMinCol || yRow2 > yMaxRow || yCol2 > yMaxCol {
		return false
	}
	yOff := state.yOrigin + yRow2*state.yStride + yCol2

	// --- UV plane offset & predict ---
	// chromaMotionVectorComponent inlined. (mv +/- 1) is folded into a
	// single +1 with a sign-keyed -2 correction so the divide isn't
	// gated by a branch:
	//   mask = -1 if mv<0 else 0; (mv + 1 + 2*mask) == (mv-1) when mv<0
	//   and (mv+1) when mv>=0.
	uvMVRow := (mvRow + 1 + 2*(mvRow>>intSignShiftDec)) / 2
	uvMVCol := (mvCol + 1 + 2*(mvCol>>intSignShiftDec)) / 2
	if state.fullPixel {
		uvMVRow &^= 7
		uvMVCol &^= 7
	}
	uvRow := mbRow*8 + (uvMVRow >> 3)
	uvCol := mbCol*8 + (uvMVCol >> 3)
	uvXOffset := uvMVCol & 7
	uvYOffset := uvMVRow & 7

	// Fast UV bounds. The per-frame limits for 8/9/13-pixel windows
	// are precomputed; chroma planes share dimensions, so one
	// comparison set covers both U and V. Different strides between
	// U and V are extremely rare; the explicit length-bound check
	// later catches any plane that's smaller than expected.
	uvRow2, uvCol2 := uvRow, uvCol
	var uvMaxRow, uvMaxCol int
	if (uvXOffset | uvYOffset) == 0 {
		uvMaxRow = state.uvMaxRowFor8
		uvMaxCol = state.uvMaxColFor8
	} else if state.useBilinear {
		uvMaxRow = state.uvMaxRowFor9
		uvMaxCol = state.uvMaxColFor9
	} else {
		uvRow2 -= 2
		uvCol2 -= 2
		uvMaxRow = state.uvMaxRowFor13
		uvMaxCol = state.uvMaxColFor13
	}
	if uvRow2 < state.uvMinRow || uvCol2 < state.uvMinCol || uvRow2 > uvMaxRow || uvCol2 > uvMaxCol {
		return false
	}
	uOff := state.uOrigin + uvRow2*state.uStride + uvCol2
	vOff := state.vOrigin + uvRow2*state.vStride + uvCol2

	// --- Predict (inlined dispatch). Hoisting useBilinear above the
	// per-plane subpel branch lets the compiler share the table-load
	// of state.useBilinear across the 3 dispatches.
	ySrc := state.yPlane[yOff:]
	uSrc := state.uPlane[uOff:]
	vSrc := state.vPlane[vOff:]
	if state.useBilinear {
		if (yXOffset | yYOffset) == 0 {
			dsp.Copy16x16(ySrc, state.yStride, y, yStride)
		} else {
			dsp.BilinearPredict16x16(ySrc, state.yStride, yXOffset, yYOffset, y, yStride)
		}
		if (uvXOffset | uvYOffset) == 0 {
			dsp.Copy8x8(uSrc, state.uStride, u, uStride)
			dsp.Copy8x8(vSrc, state.vStride, v, vStride)
		} else {
			dsp.BilinearPredict8x8(uSrc, state.uStride, uvXOffset, uvYOffset, u, uStride)
			dsp.BilinearPredict8x8(vSrc, state.vStride, uvXOffset, uvYOffset, v, vStride)
		}
	} else {
		if (yXOffset | yYOffset) == 0 {
			dsp.Copy16x16(ySrc, state.yStride, y, yStride)
		} else {
			dsp.SixTapPredict16x16(ySrc, state.yStride, yXOffset, yYOffset, y, yStride)
		}
		if (uvXOffset | uvYOffset) == 0 {
			dsp.Copy8x8(uSrc, state.uStride, u, uStride)
			dsp.Copy8x8(vSrc, state.vStride, v, vStride)
		} else {
			dsp.SixTapPredict8x8Pair(
				uSrc, state.uStride,
				vSrc, state.vStride,
				uvXOffset, uvYOffset,
				u, uStride,
				v, vStride,
			)
		}
	}

	if mode.MBSkipCoeff {
		return true
	}
	dequantIDCTAddMacroblock(tokens, dequant, false, y, yStride, u, uStride, v, vStride)
	return true
}

func reconstructSplitMVInterMacroblockFast(state *frameInterRefState, mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual, mbRow int, mbCol int, cfg InterPredictionConfig) bool {
	if mode.RefFrame == common.IntraFrame || mode.Mode != common.SplitMV || !mode.Is4x4 {
		return false
	}
	if mode.Partition < 3 {
		for _, block := range [...]int{0, 2, 8, 10} {
			mv := clampMotionVectorToUMVBorderFast(state, mode.BlockMV[block], mbRow, mbCol)
			if !predictSplitMVLuma8x8(state.yPlane, state.yStride, state.codedWidth, state.codedHeight,
				state.yOrigin, state.yBorder, y, yStride, mbRow, mbCol, block, mv,
				cfg) {
				return false
			}
		}
	} else {
		for block := 0; block < 16; block += 2 {
			mv0 := clampMotionVectorToUMVBorderFast(state, mode.BlockMV[block], mbRow, mbCol)
			mv1 := clampMotionVectorToUMVBorderFast(state, mode.BlockMV[block+1], mbRow, mbCol)
			if mv0 == mv1 {
				if !predictSplitMVLuma8x4(state.yPlane, state.yStride, state.codedWidth, state.codedHeight,
					state.yOrigin, state.yBorder, y, yStride, mbRow, mbCol, block, mv0,
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
			offset, ok := wholeMVPlaneOffset(state.yPlane, state.yStride, state.codedWidth, state.codedHeight,
				srcRow, srcCol, 4, 4, xOffset, yOffset, state.yOrigin, state.yBorder, cfg)
			if !ok {
				return false
			}
			predictInter4x4(state.yPlane[offset:], state.yStride, xOffset, yOffset,
				y[yBlockOffset(block, yStride):], yStride, cfg)

			block1 := block + 1
			blockRow = block1 >> 2
			blockCol = block1 & 3
			srcRow = mbRow*16 + blockRow*4 + int(mv1.Row>>3)
			srcCol = mbCol*16 + blockCol*4 + int(mv1.Col>>3)
			xOffset = int(mv1.Col) & 7
			yOffset = int(mv1.Row) & 7
			offset, ok = wholeMVPlaneOffset(state.yPlane, state.yStride, state.codedWidth, state.codedHeight,
				srcRow, srcCol, 4, 4, xOffset, yOffset, state.yOrigin, state.yBorder, cfg)
			if !ok {
				return false
			}
			predictInter4x4(state.yPlane[offset:], state.yStride, xOffset, yOffset,
				y[yBlockOffset(block1, yStride):], yStride, cfg)
		}
	}

	if !predictSplitMVInterChromaFast(state, mode, u, uStride, v, vStride, mbRow, mbCol, cfg) {
		return false
	}
	if mode.MBSkipCoeff {
		return true
	}
	dequantIDCTAddMacroblock(tokens, dequant, true, y, yStride, u, uStride, v, vStride)
	return true
}

func predictSplitMVInterChromaFast(state *frameInterRefState, mode *MacroblockMode, u []byte, uStride int, v []byte, vStride int, mbRow int, mbCol int, cfg InterPredictionConfig) bool {
	var mv [4]splitMVChromaVector
	for block := range mv {
		mvRow, mvCol := splitChromaMotionVector(mode, block)
		mvRow, mvCol = fullPixelChromaMotionVector(mvRow, mvCol, cfg)
		mvRow, mvCol = clampChromaMotionVectorToUMVBorderFast(state, mvRow, mvCol, mbRow, mbCol)
		mv[block] = splitMVChromaVector{row: mvRow, col: mvCol}
	}
	for block := 0; block < 4; block += 2 {
		if mv[block] == mv[block+1] {
			if !predictSplitMVChroma8x4(state.uPlane, state.uStride, state.uvWidth, state.uvHeight,
				state.uOrigin, state.uvBorder, u, uStride, mbRow, mbCol, block, mv[block],
				cfg) {
				return false
			}
			if !predictSplitMVChroma8x4(state.vPlane, state.vStride, state.uvWidth, state.uvHeight,
				state.vOrigin, state.uvBorder, v, vStride, mbRow, mbCol, block, mv[block],
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
			uOffset, ok := wholeMVPlaneOffset(state.uPlane, state.uStride, state.uvWidth, state.uvHeight,
				srcRow, srcCol, 4, 4, xOffset, yOffset, state.uOrigin, state.uvBorder, cfg)
			if !ok {
				return false
			}
			vOffset, ok := wholeMVPlaneOffset(state.vPlane, state.vStride, state.uvWidth, state.uvHeight,
				srcRow, srcCol, 4, 4, xOffset, yOffset, state.vOrigin, state.uvBorder, cfg)
			if !ok {
				return false
			}
			predictInter4x4(state.uPlane[uOffset:], state.uStride, xOffset, yOffset,
				u[uvBlockOffset(sub, uStride):], uStride, cfg)
			predictInter4x4(state.vPlane[vOffset:], state.vStride, xOffset, yOffset,
				v[uvBlockOffset(sub, vStride):], vStride, cfg)
		}
	}
	return true
}

func clampMotionVectorToUMVBorderFast(state *frameInterRefState, mv MotionVector, mbRow int, mbCol int) MotionVector {
	top, bottom, left, right := macroblockMotionVectorEdgesFromGrid(mbRow, mbCol, state.mbRows, state.mbCols)
	return MotionVector{
		Row: int16(clampUMVComponent(int(mv.Row), top, bottom)),
		Col: int16(clampUMVComponent(int(mv.Col), left, right)),
	}
}

func clampChromaMotionVectorToUMVBorderFast(state *frameInterRefState, row int, col int, mbRow int, mbCol int) (int, int) {
	top, bottom, left, right := macroblockMotionVectorEdgesFromGrid(mbRow, mbCol, state.mbRows, state.mbCols)
	return clampChromaUMVComponent(row, top, bottom), clampChromaUMVComponent(col, left, right)
}

func macroblockMotionVectorEdgesFromGrid(mbRow int, mbCol int, mbRows int, mbCols int) (int, int, int, int) {
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return top, bottom, left, right
}

func copyZeroMVInterMacroblockFast(state *frameInterRefState, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, mbRow int, mbCol int) bool {
	if uint(mbRow) >= uint(state.mbRows) || uint(mbCol) >= uint(state.mbCols) {
		return false
	}
	yOff := state.yOrigin + mbRow*16*state.yStride + mbCol*16
	uOff := state.uOrigin + mbRow*8*state.uStride + mbCol*8
	vOff := state.vOrigin + mbRow*8*state.vStride + mbCol*8
	if !planeHasOffsetBlock(state.yPlane, yOff, state.yStride, 16, 16) ||
		!planeHasOffsetBlock(state.uPlane, uOff, state.uStride, 8, 8) ||
		!planeHasOffsetBlock(state.vPlane, vOff, state.vStride, 8, 8) ||
		!planeHasOffsetBlock(y, 0, yStride, 16, 16) ||
		!planeHasOffsetBlock(u, 0, uStride, 8, 8) ||
		!planeHasOffsetBlock(v, 0, vStride, 8, 8) {
		return false
	}
	dsp.Copy16x16(state.yPlane[yOff:], state.yStride, y, yStride)
	dsp.Copy8x8(state.uPlane[uOff:], state.uStride, u, uStride)
	dsp.Copy8x8(state.vPlane[vOff:], state.vStride, v, vStride)
	return true
}

func copyZeroMVInterMacroblockRunFast(state *frameInterRefState, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, mbRow int, mbCol int, mbCount int) bool {
	if mbCount <= 0 || mbCount > state.mbCols || mbCol < 0 || mbCol > state.mbCols-mbCount || uint(mbRow) >= uint(state.mbRows) {
		return false
	}
	yWidth := mbCount * 16
	uvWidth := mbCount * 8
	yOff := state.yOrigin + mbRow*16*state.yStride + mbCol*16
	uOff := state.uOrigin + mbRow*8*state.uStride + mbCol*8
	vOff := state.vOrigin + mbRow*8*state.vStride + mbCol*8
	if !planeHasOffsetBlock(state.yPlane, yOff, state.yStride, yWidth, 16) ||
		!planeHasOffsetBlock(state.uPlane, uOff, state.uStride, uvWidth, 8) ||
		!planeHasOffsetBlock(state.vPlane, vOff, state.vStride, uvWidth, 8) ||
		!planeHasOffsetBlock(y, 0, yStride, yWidth, 16) ||
		!planeHasOffsetBlock(u, 0, uStride, uvWidth, 8) ||
		!planeHasOffsetBlock(v, 0, vStride, uvWidth, 8) {
		return false
	}
	copyPlaneRows(state.yPlane[yOff:], state.yStride, y, yStride, yWidth, 16)
	copyPlaneRows(state.uPlane[uOff:], state.uStride, u, uStride, uvWidth, 8)
	copyPlaneRows(state.vPlane[vOff:], state.vStride, v, vStride, uvWidth, 8)
	return true
}

func copyPlaneRows(src []byte, srcStride int, dst []byte, dstStride int, width int, height int) {
	for y := range height {
		copy(dst[y*dstStride:y*dstStride+width], src[y*srcStride:y*srcStride+width])
	}
}

func planeHasOffsetBlock(plane []byte, offset int, stride int, width int, height int) bool {
	if offset < 0 || width < 0 || height < 0 || stride < width {
		return false
	}
	if height == 0 {
		return true
	}
	need := offset + (height-1)*stride + width
	return need >= offset && need <= len(plane)
}
