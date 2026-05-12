package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func macroblockCoefficientsEmpty(coeffs *vp8enc.MacroblockCoefficients, is4x4 bool) bool {
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

func clearMacroblockCoefficients(coeffs *vp8enc.MacroblockCoefficients) {
	*coeffs = vp8enc.MacroblockCoefficients{}
}

func staticInterRDEncodeBreakout(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, encodeBreakout int) bool {
	breakout, _ := staticInterRDEncodeBreakoutDistortion(src, pred, mbRow, mbCol, quant, encodeBreakout)
	return breakout
}

func staticInterRDEncodeBreakoutDistortion(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, encodeBreakout int) (bool, int) {
	if encodeBreakout <= 0 || pred == nil || quant == nil {
		return false, 0
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	lumaVar, lumaSSE := macroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
	if lumaSSE >= threshold {
		return false, 0
	}
	y2DC := int(quant.Y2.Dequant[0])
	dcError := lumaSSE - lumaVar
	if dcError >= (y2DC*y2DC)>>4 && (lumaSSE/2 <= lumaVar || dcError >= 64) {
		return false, 0
	}
	chromaSSE := macroblockChromaSSE(src, pred, mbRow, mbCol)
	return chromaSSE*2 < encodeBreakout, lumaSSE + chromaSSE
}

func staticInterFastEncodeBreakout(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode, quant *vp8enc.MacroblockQuant, encodeBreakout int, lumaSSE int) bool {
	if encodeBreakout <= 0 || ref == nil || mode == nil || quant == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return false
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	if lumaSSE >= threshold {
		return false
	}
	chromaSSE, ok := macroblockChromaMotionSSE(src, ref, mbRow, mbCol, mode)
	return ok && chromaSSE*2 < encodeBreakout
}

func macroblockChromaMotionSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode) (int, bool) {
	if ref == nil || mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return 0, false
	}
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	if baseY < 0 || baseX < 0 || baseY+8 > uvHeight || baseX+8 > uvWidth {
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
	mask := c >> mvKernelSignShift
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
		width++
		height++
	}
	off := origin + y*stride + x
	last := off + (height-1)*stride + width - 1
	// Uint range collapses (off<0)+(off>=len) and (last<0)+(last>=len) into
	// one compare each. The implicit last<off "overflow" case is also
	// covered because a wrapped-negative last has uint() >= uint(len).
	if uint(off) >= uint(len(plane)) || uint(last) >= uint(len(plane)) {
		return 0, false
	}
	return off, true
}
