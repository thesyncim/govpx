// Ported from libvpx v1.16.0 vp8/common/reconinter.c.
package decoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

// PredictWholeMVInterLumaWithState writes the Y predictor without constructing
// unused chroma predictors. It retains whole-MV reference validation, including
// chroma, so callers can keep their existing fallback on unsupported references.
// No tokens, residual scratch, or chroma destination are read or written.
func PredictWholeMVInterLumaWithState(state *InterFrameRefState, mode *MacroblockMode, y []byte, yStride int, mbRow int, mbCol int) bool {
	if state == nil || !state.ready || mode == nil || mode.RefFrame == common.IntraFrame ||
		mode.Is4x4 || !common.IsWholeInterMacroblockMode(mode.Mode) ||
		!planeHasOffsetBlock(y, 0, yStride, 16, 16) {
		return false
	}
	if mode.Mode == common.ZeroMV {
		if !mode.MV.IsZero() || uint(mbRow) >= uint(state.mbRows) || uint(mbCol) >= uint(state.mbCols) {
			return false
		}
		yOff := state.yOrigin + mbRow*16*state.yStride + mbCol*16
		uOff := state.uOrigin + mbRow*8*state.uStride + mbCol*8
		vOff := state.vOrigin + mbRow*8*state.vStride + mbCol*8
		if !planeHasOffsetBlock(state.yPlane, yOff, state.yStride, 16, 16) ||
			!planeHasOffsetBlock(state.uPlane, uOff, state.uStride, 8, 8) ||
			!planeHasOffsetBlock(state.vPlane, vOff, state.vStride, 8, 8) {
			return false
		}
		dsp.Copy16x16(state.yPlane[yOff:], state.yStride, y, yStride)
		return true
	}

	mv := clampMotionVectorToUMVBorderFast(state, mode.MV, mbRow, mbCol)
	mvRow, mvCol := int(mv.Row), int(mv.Col)
	yXOffset, yYOffset := mvCol&7, mvRow&7
	cfg := InterPredictionConfig{UseBilinear: state.useBilinear, FullPixel: state.fullPixel}
	yOff, ok := wholeMVPlaneOffset(state.yPlane, state.yStride, state.codedWidth, state.codedHeight,
		mbRow*16+(mvRow>>3), mbCol*16+(mvCol>>3), 16, 16, yXOffset, yYOffset,
		state.yOrigin, state.yBorder, cfg)
	if !ok {
		return false
	}
	// Match libvpx reconinter.c: fullpixel_mask applies to chroma only.
	uvMVRow := (mvRow + 1 + 2*(mvRow>>intSignShiftDec)) / 2
	uvMVCol := (mvCol + 1 + 2*(mvCol>>intSignShiftDec)) / 2
	uvMVRow, uvMVCol = fullPixelChromaMotionVector(uvMVRow, uvMVCol, cfg)
	uvRow, uvCol := mbRow*8+(uvMVRow>>3), mbCol*8+(uvMVCol>>3)
	uvXOffset, uvYOffset := uvMVCol&7, uvMVRow&7
	if _, ok := wholeMVPlaneOffset(state.uPlane, state.uStride, state.uvWidth, state.uvHeight,
		uvRow, uvCol, 8, 8, uvXOffset, uvYOffset, state.uOrigin, state.uvBorder, cfg); !ok {
		return false
	}
	if _, ok := wholeMVPlaneOffset(state.vPlane, state.vStride, state.uvWidth, state.uvHeight,
		uvRow, uvCol, 8, 8, uvXOffset, uvYOffset, state.vOrigin, state.uvBorder, cfg); !ok {
		return false
	}
	if yXOffset|yYOffset == 0 {
		dsp.Copy16x16(state.yPlane[yOff:], state.yStride, y, yStride)
	} else if state.useBilinear {
		dsp.BilinearPredict16x16(state.yPlane[yOff:], state.yStride, yXOffset, yYOffset, y, yStride)
	} else {
		dsp.SixTapPredict16x16(state.yPlane[yOff:], state.yStride, yXOffset, yYOffset, y, yStride)
	}
	return true
}
