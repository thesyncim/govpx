package govpx

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

func gatherMacroblockYResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if baseY >= 0 && baseX >= 0 && baseY+16 <= height && baseX+16 <= width {
		srcEnd := (baseY+15)*srcStride + baseX + 15
		predEnd := (baseY+15)*predStride + baseX + 15
		if srcStride > 0 && predStride > 0 && srcEnd < len(src) && predEnd < len(pred) && len(out) >= 16*16 {
			gatherMacroblockYResiduals4x4Unchecked(unsafe.SliceData(src), srcStride, unsafe.SliceData(pred), predStride, baseX, baseY, unsafe.SliceData(out))
			return
		}
	}
	for block := range 16 {
		x := baseX + (block&3)*4
		y := baseY + (block>>2)*4
		fillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

func gatherMacroblockYResiduals4x4Unchecked(src *byte, srcStride int, pred *byte, predStride int, baseX int, baseY int, out *int16) {
	srcBase := (*byte)(unsafe.Add(unsafe.Pointer(src), baseY*srcStride+baseX))
	predBase := (*byte)(unsafe.Add(unsafe.Pointer(pred), baseY*predStride+baseX))
	dsp.ResidualGather16x16PtrFast(srcBase, srcStride, predBase, predStride, out)
}

// gatherMacroblockUVResiduals4x4 writes the 4 chroma 4x4 residuals of
// the 8x8 MB chroma block at top-left (baseX,baseY) into out (4 blocks,
// 16 int16 per block in scan order). Same fast/slow split as the Y
// gatherer.
func gatherMacroblockUVResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if baseY >= 0 && baseX >= 0 && baseY+8 <= height && baseX+8 <= width {
		srcEnd := (baseY+7)*srcStride + baseX + 7
		predEnd := (baseY+7)*predStride + baseX + 7
		if srcStride > 0 && predStride > 0 && srcEnd < len(src) && predEnd < len(pred) && len(out) >= 4*16 {
			gatherMacroblockUVResiduals4x4Unchecked(unsafe.SliceData(src), srcStride, unsafe.SliceData(pred), predStride, baseX, baseY, unsafe.SliceData(out))
			return
		}
	}
	for block := range 4 {
		x := baseX + (block&1)*4
		y := baseY + (block>>1)*4
		fillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

func gatherMacroblockUVResiduals4x4Unchecked(src *byte, srcStride int, pred *byte, predStride int, baseX int, baseY int, out *int16) {
	srcBase := (*byte)(unsafe.Add(unsafe.Pointer(src), baseY*srcStride+baseX))
	predBase := (*byte)(unsafe.Add(unsafe.Pointer(pred), baseY*predStride+baseX))
	dsp.ResidualGather8x8PtrFast(srcBase, srcStride, predBase, predStride, out)
}
