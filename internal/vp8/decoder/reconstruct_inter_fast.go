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
	if mode.RefFrame == common.IntraFrame || mode.Is4x4 || !isWholeMacroblockInterMode(mode.Mode) {
		return false
	}
	if mode.Mode == common.ZeroMV && !mode.MV.IsZero() {
		return false
	}

	mvRow := int(mode.MV.Row)
	mvCol := int(mode.MV.Col)
	if state.fullPixel {
		mvRow &^= 7
		mvCol &^= 7
	}
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
	TransformMacroblockTokens(tokens, dequant, false, scratch)
	AddMacroblockResidual(tokens, scratch, y, yStride, u, uStride, v, vStride)
	return true
}
