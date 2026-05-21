package encoder

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

// Predicted residual gather helpers mirror libvpx v1.16.0
// vp8/encoder/encodemb.c subtract flows while packing into the block-major
// coefficient layout used by the Go encoder.

func GatherMacroblockYResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	// Uint range collapses (base >= 0) + (base+16 <= dim) into one
	// compare per dimension when dim >= 16; smaller dims fall through
	// to the scalar gather path.
	if uint(baseY) <= uint(height-16) && uint(baseX) <= uint(width-16) {
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
		FillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

func gatherMacroblockYResiduals4x4Unchecked(src *byte, srcStride int, pred *byte, predStride int, baseX int, baseY int, out *int16) {
	srcBase := (*byte)(unsafe.Add(unsafe.Pointer(src), baseY*srcStride+baseX))
	predBase := (*byte)(unsafe.Add(unsafe.Pointer(pred), baseY*predStride+baseX))
	dsp.ResidualGather16x16PtrFast(srcBase, srcStride, predBase, predStride, out)
}

// GatherMacroblockUVResiduals4x4 writes the 4 chroma 4x4 residuals of the
// 8x8 MB chroma block at top-left (baseX,baseY) into out (4 blocks, 16 int16
// per block in scan order). Same fast/slow split as the Y gatherer.
func GatherMacroblockUVResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if uint(baseY) <= uint(height-8) && uint(baseX) <= uint(width-8) {
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
		FillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

func gatherMacroblockUVResiduals4x4Unchecked(src *byte, srcStride int, pred *byte, predStride int, baseX int, baseY int, out *int16) {
	srcBase := (*byte)(unsafe.Add(unsafe.Pointer(src), baseY*srcStride+baseX))
	predBase := (*byte)(unsafe.Add(unsafe.Pointer(pred), baseY*predStride+baseX))
	dsp.ResidualGather8x8PtrFast(srcBase, srcStride, predBase, predStride, out)
}

// GatherMacroblockYResiduals4x4FromPredBuffer computes the 16 4x4 Y residuals
// (src - pred) into out (16 blocks of 16 int16) for the macroblock at
// (mbBaseX, mbBaseY) in src coordinates, against a 16x16 pred buffer in its
// own local (0..15, 0..15) coordinate space with stride predStride.
func GatherMacroblockYResiduals4x4FromPredBuffer(src []byte, srcStride int, width int, height int, pred []byte, predStride int, mbBaseX int, mbBaseY int, out []int16) {
	for block := range 16 {
		blockX := (block & 3) * 4
		blockY := (block >> 2) * 4
		dst := out[block*16 : block*16+16]
		for row := range 4 {
			sampleY := clampEncodeCoord(mbBaseY+blockY+row, height)
			for col := range 4 {
				sampleX := clampEncodeCoord(mbBaseX+blockX+col, width)
				dst[row*4+col] = int16(int(src[sampleY*srcStride+sampleX]) - int(pred[(blockY+row)*predStride+blockX+col]))
			}
		}
	}
}

func FillPredictedResidual4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, x int, y int, out *[16]int16) {
	for row := range 4 {
		sampleY := clampEncodeCoord(y+row, height)
		for col := range 4 {
			sampleX := clampEncodeCoord(x+col, width)
			out[row*4+col] = int16(int(src[sampleY*srcStride+sampleX]) - int(pred[(y+row)*predStride+x+col]))
		}
	}
}

// FillPredictedResidual4x4Slice mirrors FillPredictedResidual4x4 but writes
// into a caller-supplied slice.
func FillPredictedResidual4x4Slice(src []byte, srcStride int, width int, height int, pred []byte, predStride int, x int, y int, out []int16) {
	for row := range 4 {
		sampleY := clampEncodeCoord(y+row, height)
		for col := range 4 {
			sampleX := clampEncodeCoord(x+col, width)
			out[row*4+col] = int16(int(src[sampleY*srcStride+sampleX]) - int(pred[(y+row)*predStride+x+col]))
		}
	}
}
