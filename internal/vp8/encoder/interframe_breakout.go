package encoder

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

// Static inter encode-breakout helpers mirror libvpx v1.16.0
// vp8/encoder/rdopt.c and vp8/encoder/pickinter.c.

func MacroblockCoefficientsEmpty(coeffs *MacroblockCoefficients, is4x4 bool) bool {
	if coeffs.EOB[24] != 0 {
		return false
	}
	for i := range 16 {
		if (is4x4 && coeffs.EOB[i] != 0) || (!is4x4 && coeffs.EOB[i] > 1) {
			return false
		}
	}
	for i := 16; i < 24; i++ {
		if coeffs.EOB[i] != 0 {
			return false
		}
	}
	return true
}

func ClearMacroblockCoefficients(coeffs *MacroblockCoefficients) {
	*coeffs = MacroblockCoefficients{}
}

func StaticInterRDEncodeBreakout(src SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *MacroblockQuant, encodeBreakout int) bool {
	breakout, _ := StaticInterRDEncodeBreakoutDistortion(src, pred, mbRow, mbCol, quant, encodeBreakout)
	return breakout
}

func StaticInterRDEncodeBreakoutDistortion(src SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *MacroblockQuant, encodeBreakout int) (bool, int) {
	if encodeBreakout <= 0 || pred == nil || quant == nil {
		return false, 0
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	lumaVar, lumaSSE := MacroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
	if lumaSSE >= threshold {
		return false, 0
	}
	y2DC := int(quant.Y2.Dequant[0])
	dcError := lumaSSE - lumaVar
	if dcError >= (y2DC*y2DC)>>4 && (lumaSSE/2 <= lumaVar || dcError >= 64) {
		return false, 0
	}
	chromaSSE := MacroblockChromaSSE(src, pred, mbRow, mbCol)
	// libvpx vp8/encoder/rdopt.c:1627 - the UV-SSE breakout compare uses
	// `threshold` (= max(yAC^2>>4, x->encode_breakout)), not the raw
	// encode_breakout. The fast picker (pickinter.c:463) compares against
	// `x->encode_breakout` instead; that asymmetry is intentional in libvpx
	// and mirrored in StaticInterFastEncodeBreakout below.
	return chromaSSE*2 < threshold, lumaSSE + chromaSSE
}

func StaticInterFastEncodeBreakout(src SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *InterFrameMacroblockMode, quant *MacroblockQuant, encodeBreakout int, lumaSSE int) bool {
	if encodeBreakout <= 0 || ref == nil || mode == nil || quant == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return false
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	if lumaSSE >= threshold {
		return false
	}
	chromaSSE, ok := MacroblockChromaMotionSSE(src, ref, mbRow, mbCol, mode)
	return ok && chromaSSE*2 < encodeBreakout
}

func MacroblockChromaMotionSSE(src SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *InterFrameMacroblockMode) (int, bool) {
	if ref == nil || mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return 0, false
	}
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth, uvHeight := SourceImageUVDimensions(src)
	// Uint range collapses (base<0) + (base+8>dim) into one compare per
	// dimension. The original positive-form '+8 > dim' becomes
	// 'base > dim-8' which uint-cast handles in one branch.
	if uint(baseY) > uint(uvHeight-8) || uint(baseX) > uint(uvWidth-8) {
		return 0, false
	}
	srcUOff := baseY*src.UStride + baseX
	srcVOff := baseY*src.VStride + baseX
	if srcUOff < 0 || srcVOff < 0 ||
		srcUOff+7*src.UStride+7 >= len(src.U) ||
		srcVOff+7*src.VStride+7 >= len(src.V) {
		return 0, false
	}

	mvRow := chromaMotionVectorComponent(mode.MV.Row)
	mvCol := chromaMotionVectorComponent(mode.MV.Col)
	refY := baseY + (mvRow >> 3)
	refX := baseX + (mvCol >> 3)
	xOffset := mvCol & 7
	yOffset := mvRow & 7
	uPlane, uOrigin := referenceChromaPlane(ref.U, ref.UFull, ref.UOrigin)
	vPlane, vOrigin := referenceChromaPlane(ref.V, ref.VFull, ref.VOrigin)
	uOff, ok := referencePlaneBlockOffset(uPlane, ref.UStride, uOrigin, refY, refX, 8, 8, xOffset|yOffset != 0)
	if !ok {
		return 0, false
	}
	vOff, ok := referencePlaneBlockOffset(vPlane, ref.VStride, vOrigin, refY, refX, 8, 8, xOffset|yOffset != 0)
	if !ok {
		return 0, false
	}
	if xOffset|yOffset == 0 {
		return dsp.SSE8x8(uPlane[uOff:], ref.UStride, src.U[srcUOff:], src.UStride) +
			dsp.SSE8x8(vPlane[vOff:], ref.VStride, src.V[srcVOff:], src.VStride), true
	}
	_, uSSE := dsp.SubpelVariance8x8(uPlane[uOff:], ref.UStride, xOffset, yOffset, src.U[srcUOff:], src.UStride)
	_, vSSE := dsp.SubpelVariance8x8(vPlane[vOff:], ref.VStride, xOffset, yOffset, src.V[srcVOff:], src.VStride)
	return uSSE + vSSE, true
}

func chromaMotionVectorComponent(v int16) int {
	c := int(v)
	// (c-1)/2 when c<0, (c+1)/2 otherwise. Sign-mask folds the offset
	// into one straight-line expression.
	mask := c >> intSignShift
	return (c + 1 + 2*mask) / 2
}

func referenceChromaPlane(visible []byte, full []byte, origin int) ([]byte, int) {
	if len(full) != 0 {
		return full, origin
	}
	return visible, 0
}

func referencePlaneBlockOffset(plane []byte, stride int, origin int, y int, x int, width int, height int, subpel bool) (int, bool) {
	if len(plane) == 0 || min(min(stride, width), height) <= 0 {
		return 0, false
	}
	if subpel {
		maxInt := int(^uint(0) >> 1)
		if width == maxInt || height == maxInt {
			return 0, false
		}
		width++
		height++
	}
	if width > stride {
		return 0, false
	}
	rowOffset, ok := checkedIntMul(y, stride)
	if !ok {
		return 0, false
	}
	off, ok := checkedIntAdd(origin, rowOffset)
	if !ok {
		return 0, false
	}
	off, ok = checkedIntAdd(off, x)
	if !ok {
		return 0, false
	}
	lastRowOffset, ok := checkedIntMul(height-1, stride)
	if !ok {
		return 0, false
	}
	last, ok := checkedIntAdd(off, lastRowOffset)
	if !ok {
		return 0, false
	}
	last, ok = checkedIntAdd(last, width-1)
	if !ok {
		return 0, false
	}
	if uint(off) >= uint(len(plane)) || uint(last) >= uint(len(plane)) {
		return 0, false
	}
	return off, true
}

func checkedIntAdd(a, b int) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if b > 0 && a > maxInt-b {
		return 0, false
	}
	if b < 0 && a < minInt-b {
		return 0, false
	}
	return a + b, true
}

func checkedIntMul(a, b int) (int, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if a > 0 {
		if b > 0 {
			if a > maxInt/b {
				return 0, false
			}
		} else if b < minInt/a {
			return 0, false
		}
	} else {
		if b > 0 {
			if a < minInt/b {
				return 0, false
			}
		} else if a != 0 && b < maxInt/a {
			return 0, false
		}
	}
	return a * b, true
}
