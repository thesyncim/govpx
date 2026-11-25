package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// Ported from libvpx v1.16.0 vp8/encoder/encodeframe.c and
// vp8/encoder/encodemb.c intra residual transform/quantization flow, limited
// to DC-predicted keyframe macroblocks against a neutral predictor.

type SourceImage struct {
	Y        []byte
	U        []byte
	V        []byte
	Width    int
	Height   int
	UVWidth  int
	UVHeight int
	YStride  int
	UStride  int
	VStride  int
}

func BuildNeutralPredictorKeyFrameCoefficients(src SourceImage, qIndex int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients) error {
	return buildNeutralPredictorKeyFrameCoefficients(src, qIndex, common.QuantDeltas{}, SegmentationConfig{}, false, modes, coeffs)
}

func BuildNeutralPredictorKeyFrameCoefficientsWithQuantDeltas(src SourceImage, qIndex int, deltas common.QuantDeltas, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients) error {
	return buildNeutralPredictorKeyFrameCoefficients(src, qIndex, deltas, SegmentationConfig{}, false, modes, coeffs)
}

func BuildNeutralPredictorKeyFrameCoefficientsWithSegmentation(src SourceImage, qIndex int, segmentation SegmentationConfig, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients) error {
	return buildNeutralPredictorKeyFrameCoefficients(src, qIndex, common.QuantDeltas{}, segmentation, true, modes, coeffs)
}

func buildNeutralPredictorKeyFrameCoefficients(src SourceImage, qIndex int, deltas common.QuantDeltas, segmentation SegmentationConfig, preserveSegmentID bool, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients) error {
	if !validSourceImage(src) || qIndex < common.MinQ || qIndex > common.MaxQ {
		return ErrInvalidPacketConfig
	}
	rows := (src.Height + 15) >> 4
	cols := (src.Width + 15) >> 4
	required := rows * cols
	if len(modes) < required || len(coeffs) < required {
		return ErrModeBufferTooSmall
	}

	var quants [common.MaxMBSegments]MacroblockQuant
	if err := InitSegmentMacroblockQuants(qIndex, deltas, segmentation, &quants); err != nil {
		return err
	}

	for row := range rows {
		for col := range cols {
			index := row*cols + col
			segmentID := uint8(0)
			if preserveSegmentID {
				segmentID = modes[index].SegmentID
				if segmentID >= common.MaxMBSegments {
					return ErrInvalidPacketConfig
				}
			}
			modes[index] = KeyFrameMacroblockMode{SegmentID: segmentID, YMode: common.DCPred, UVMode: common.DCPred}
			// MaxMBSegments=4 (pow2) and segmentID was just bounded to
			// [0,4); AND-mask with 3 elides the bounds check on quants.
			buildNeutralPredictorMacroblockCoefficients(src, row, col, &quants[segmentID&3], &coeffs[index])
		}
	}
	return nil
}

func buildNeutralPredictorMacroblockCoefficients(src SourceImage, mbRow int, mbCol int, quant *MacroblockQuant, coeffs *MacroblockCoefficients) {
	// Whole-MB transform/quantize pipeline mirroring libvpx v1.16.0
	// vp8_transform_mb / vp8_quantize_mb call sequence: build the
	// contiguous 24-block (16 Y + 8 UV) residual buffer, run the 4x4
	// forward DCT batched across all 24 blocks, lift the 16 Y DCs
	// into the Y2 block (with the Y AC slot zeroed), Walsh-transform
	// Y2, and batch-quantize Y (Y1DC quant), Y2 (one block), and UV
	// (UV quant) using the shared per-plane BlockQuant. Each batched
	// dispatch amortizes the Go<->asm boundary that
	// libvpx hides via short_fdct8x4 / vp8_quantize_mby.
	var residuals [24 * 16]int16
	var dct [25 * 16]int16
	var dqAll [25 * 16]int16
	var eobs [25]uint8
	var y2Coeff [16]int16
	var y2Input [16]int16

	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	for block := range 16 {
		x := mbCol*16 + (block&3)*4
		y := mbRow*16 + (block>>2)*4
		fillResidual4x4Slice(src.Y, src.YStride, src.Width, src.Height, x, y, residuals[block*16:block*16+16])
	}
	for block := range 4 {
		x := mbCol*8 + (block&1)*4
		y := mbRow*8 + (block>>1)*4
		fillResidual4x4Slice(src.U, src.UStride, uvWidth, uvHeight, x, y, residuals[(16+block)*16:(16+block)*16+16])
		fillResidual4x4Slice(src.V, src.VStride, uvWidth, uvHeight, x, y, residuals[(20+block)*16:(20+block)*16+16])
	}

	// Single dispatched 24-block 4x4 forward DCT (Y0..Y15, U0..U3, V0..V3).
	ForwardDCT4x4Batch(residuals[:], dct[:24*16], 24)

	// Lift 16 Y DCs into the Y2 input, then zero the Y AC slot 0
	// (matches libvpx's build_dcblock + the y_no_dc tokenize start).
	for block := range 16 {
		y2Input[block] = dct[block*16]
		dct[block*16] = 0
	}
	ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
	copy(dct[24*16:], y2Coeff[:])

	var qAll [25 * 16]int16
	FastQuantizeBlockBatch(dct[:16*16], &quant.Y1DC, qAll[:16*16], dqAll[:16*16], eobs[:16], 16)
	FastQuantizeBlockBatch(dct[16*16:24*16], &quant.UV, qAll[16*16:24*16], dqAll[16*16:24*16], eobs[16:24], 8)
	FastQuantizeBlockBatch(dct[24*16:25*16], &quant.Y2, qAll[24*16:25*16], dqAll[24*16:25*16], eobs[24:25], 1)

	for i := range 25 {
		copy(coeffs.QCoeff[i][:], qAll[i*16:])
		coeffs.SetBlockEOB(i, int(eobs[i]))
	}
}

// fillResidual4x4Slice writes a source block into a caller-supplied slice so
// all 24 4x4 blocks land in one contiguous buffer ready for ForwardDCT4x4Batch.
func fillResidual4x4Slice(plane []byte, stride int, width int, height int, x int, y int, out []int16) {
	if x+4 <= width && y+4 <= height {
		for row := range 4 {
			src := (y+row)*stride + x
			dst := row * 4
			out[dst+0] = int16(int(plane[src+0]) - 128)
			out[dst+1] = int16(int(plane[src+1]) - 128)
			out[dst+2] = int16(int(plane[src+2]) - 128)
			out[dst+3] = int16(int(plane[src+3]) - 128)
		}
		return
	}
	for row := range 4 {
		sampleY := clampCoord(y+row, height)
		for col := range 4 {
			sampleX := clampCoord(x+col, width)
			out[row*4+col] = int16(int(plane[sampleY*stride+sampleX]) - 128)
		}
	}
}

func validSourceImage(src SourceImage) bool {
	if src.Width <= 0 || src.Height <= 0 {
		return false
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(src.Width, src.Height)
	if src.YStride < src.Width || src.UStride < uvWidth || src.VStride < uvWidth {
		return false
	}
	if len(src.Y) < buffers.PlaneLen(src.YStride, src.Height, src.Width) {
		return false
	}
	if len(src.U) < buffers.PlaneLen(src.UStride, uvHeight, uvWidth) {
		return false
	}
	if len(src.V) < buffers.PlaneLen(src.VStride, uvHeight, uvWidth) {
		return false
	}
	return true
}

func clampCoord(v int, limit int) int {
	return min(v, limit-1)
}

// Predicted residual gather helpers mirror libvpx v1.16.0
// vp8/encoder/encodemb.c subtract flows while packing into the block-major
// coefficient layout used by the Go encoder.

func GatherMacroblockYResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if uint(baseY) <= uint(height-16) && uint(baseX) <= uint(width-16) {
		srcEnd := (baseY+15)*srcStride + baseX + 15
		predEnd := (baseY+15)*predStride + baseX + 15
		if srcStride > 0 && predStride > 0 && srcEnd < len(src) && predEnd < len(pred) && len(out) >= 16*16 {
			dsp.ResidualGather16x16Fast(src[baseY*srcStride+baseX:], srcStride, pred[baseY*predStride+baseX:], predStride, out)
			return
		}
	}
	for block := range 16 {
		x := baseX + (block&3)*4
		y := baseY + (block>>2)*4
		FillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

// GatherMacroblockUVResiduals4x4 writes the 4 chroma 4x4 residuals of the
// 8x8 MB chroma block at top-left (baseX,baseY) into out (4 blocks, 16 int16
// per block in scan order).
func GatherMacroblockUVResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if uint(baseY) <= uint(height-8) && uint(baseX) <= uint(width-8) {
		srcEnd := (baseY+7)*srcStride + baseX + 7
		predEnd := (baseY+7)*predStride + baseX + 7
		if srcStride > 0 && predStride > 0 && srcEnd < len(src) && predEnd < len(pred) && len(out) >= 4*16 {
			dsp.ResidualGather8x8Fast(src[baseY*srcStride+baseX:], srcStride, pred[baseY*predStride+baseX:], predStride, out)
			return
		}
	}
	for block := range 4 {
		x := baseX + (block&1)*4
		y := baseY + (block>>1)*4
		FillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

// GatherMacroblockYResiduals4x4FromPredBuffer computes the 16 4x4 Y residuals
// (src - pred) into out (16 blocks of 16 int16) for the macroblock at
// (mbBaseX, mbBaseY) in src coordinates, against a 16x16 pred buffer in its
// own local (0..15, 0..15) coordinate space with stride predStride.
func GatherMacroblockYResiduals4x4FromPredBuffer(src []byte, srcStride int, width int, height int, pred []byte, predStride int, mbBaseX int, mbBaseY int, out []int16) {
	if width >= 16 && height >= 16 && uint(mbBaseX) <= uint(width-16) && uint(mbBaseY) <= uint(height-16) && srcStride >= width && predStride >= 16 && len(out) >= 256 && len(src) >= mbBaseX+16 && len(pred) >= 16 && mbBaseY+15 <= (len(src)-mbBaseX-16)/srcStride && predStride <= (len(pred)-16)/15 {
		dsp.ResidualGather16x16Fast(src[mbBaseY*srcStride+mbBaseX:], srcStride, pred, predStride, out)
		return
	}

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
	FillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[:])
}

// FillPredictedResidual4x4Slice mirrors FillPredictedResidual4x4 but writes
// into a caller-supplied slice.
func FillPredictedResidual4x4Slice(src []byte, srcStride int, width int, height int, pred []byte, predStride int, x int, y int, out []int16) {
	if width >= 4 && height >= 4 && uint(x) <= uint(width-4) && uint(y) <= uint(height-4) && srcStride >= width && predStride >= x+4 && len(out) >= 16 && len(src) >= x+4 && len(pred) >= x+4 && y+3 <= (len(src)-x-4)/srcStride && y+3 <= (len(pred)-x-4)/predStride {
		pixel.Subtract(src[y*srcStride+x:], srcStride, pred[y*predStride+x:], predStride, out, 4, 4, 4)
		return
	}

	for row := range 4 {
		sampleY := clampEncodeCoord(y+row, height)
		for col := range 4 {
			sampleX := clampEncodeCoord(x+col, width)
			out[row*4+col] = int16(int(src[sampleY*srcStride+sampleX]) - int(pred[(y+row)*predStride+x+col]))
		}
	}
}
