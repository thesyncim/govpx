package encoder

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	"github.com/thesyncim/govpx/internal/vpx/arith"
)

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
		return dsp.SSE8x8PtrFast(&src.U[srcUOffset], src.UStride, &ref.U[refUOffset], ref.UStride) +
			dsp.SSE8x8PtrFast(&src.V[srcVOffset], src.VStride, &ref.V[refVOffset], ref.VStride)
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
		sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, &ref.Y[baseY*ref.YStride+baseX], ref.YStride)
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
