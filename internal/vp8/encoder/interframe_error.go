package encoder

import (
	"encoding/binary"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	"github.com/thesyncim/govpx/internal/vpx/arith"
)

// Static inter encode-breakout helpers mirror libvpx v1.16.0
// vp8/encoder/rdopt.c and vp8/encoder/pickinter.c.

func MacroblockCoefficientsEmpty(coeffs *MacroblockCoefficients, is4x4 bool) bool {
	u0 := binary.LittleEndian.Uint64(coeffs.EOB[0:8])
	u1 := binary.LittleEndian.Uint64(coeffs.EOB[8:16])
	u2 := binary.LittleEndian.Uint64(coeffs.EOB[16:24])
	if !is4x4 {
		// Non-4x4 Y1 blocks skip DC (index 0), so EOB <= 1 is empty; EOB > 1
		// iff any bit above bit 0 is set.
		const mask = ^uint64(0x0101010101010101)
		return (u0&mask)|(u1&mask)|u2|uint64(coeffs.EOB[24]) == 0
	}
	return u0|u1|u2|uint64(coeffs.EOB[24]) == 0
}

func ClearMacroblockCoefficients(coeffs *MacroblockCoefficients) {
	*coeffs = MacroblockCoefficients{}
}

func StaticInterRDEncodeBreakout(src SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *MacroblockQuant, encodeBreakout int) bool {
	breakout, _ := StaticInterRDEncodeBreakoutDistortion(src, pred, mbRow, mbCol, quant, encodeBreakout)
	return breakout
}

// StaticInterRDEncodeBreakoutWithLuma evaluates the RD encode-breakout test
// using caller-provided luma variance/SSE from mode picking, avoiding a second
// 16x16 pixel.Stats pass over the luma plane.
func StaticInterRDEncodeBreakoutWithLuma(src SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *MacroblockQuant, encodeBreakout int, lumaVar int, lumaSSE int) bool {
	if encodeBreakout <= 0 || pred == nil || quant == nil {
		return false
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	if lumaSSE >= threshold {
		return false
	}
	y2DC := int(quant.Y2.Dequant[0])
	dcError := lumaSSE - lumaVar
	if dcError >= (y2DC*y2DC)>>4 && (lumaSSE/2 <= lumaVar || dcError >= 64) {
		return false
	}
	chromaSSE := MacroblockChromaSSE(src, pred, mbRow, mbCol)
	return chromaSSE*2 < threshold
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

// MacroblockChromaSSE returns the combined U/V SSE for the visible chroma
// block corresponding to the given macroblock, clamping partial-edge samples
// the same way libvpx v1.16.0's VP8 encoder does for inter analysis.
func MacroblockChromaSSE(src SourceImage, ref *vp8common.Image, mbRow int, mbCol int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth, uvHeight := SourceImageUVDimensions(src)
	refUVWidth := (ref.CodedWidth + 1) >> 1
	refUVHeight := (ref.CodedHeight + 1) >> 1
	// Uint-range collapse on each chroma dimension; the 4 src-dim and
	// ref-dim guards become 4 single compares (was 6 in the original
	// boolean chain).
	if uint(baseY) <= uint(uvHeight-8) && uint(baseX) <= uint(uvWidth-8) &&
		uint(baseY) <= uint(refUVHeight-8) && uint(baseX) <= uint(refUVWidth-8) {
		srcUOffset := baseY*src.UStride + baseX
		refUOffset := baseY*ref.UStride + baseX
		srcVOffset := baseY*src.VStride + baseX
		refVOffset := baseY*ref.VStride + baseX
		return dsp.SSE8x8Fast(src.U[srcUOffset:], src.UStride, ref.U[refUOffset:], ref.UStride) +
			dsp.SSE8x8Fast(src.V[srcVOffset:], src.VStride, ref.V[refVOffset:], ref.VStride)
	}

	sse := 0
	for row := range 8 {
		srcY := clampEncodeCoord(baseY+row, uvHeight)
		refY := clampEncodeCoord(baseY+row, refUVHeight)
		for col := range 8 {
			srcX := clampEncodeCoord(baseX+col, uvWidth)
			refX := clampEncodeCoord(baseX+col, refUVWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(ref.U[refY*ref.UStride+refX])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(ref.V[refY*ref.VStride+refX])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

// MacroblockLumaVarianceSSE returns VP8's luma variance and raw SSE for the
// given macroblock, using the SIMD path for full 16x16 blocks and the clamped
// visible-edge scalar path for partial-edge macroblocks.
func MacroblockLumaVarianceSSE(src SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) &&
		uint(baseY) <= uint(ref.CodedHeight-16) && uint(baseX) <= uint(ref.CodedWidth-16) {
		// Fused (sum, sse) read collapses Variance16x16 + SSE16x16 into one
		// SIMD pass (variance = sse - sum*sum/256).
		sum, sse := dsp.VarianceBlock16x16Fast(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[baseY*ref.YStride+baseX:], ref.YStride)
		return sse - ((sum * sum) >> 8), sse
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(baseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(baseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

func clampEncodeCoord(v int, limit int) int {
	return arith.ClampCoord(v, limit)
}

// ClampEncodeCoord clamps a VP8 encoder source/reference coordinate to the
// visible edge sample, matching libvpx v1.16.0 VP8 edge handling for partial
// macroblock analysis.
func ClampEncodeCoord(v int, limit int) int {
	return clampEncodeCoord(v, limit)
}
