package encoder

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

// Ported from libvpx v1.16.0 vp8/encoder/encodeframe.c and
// vp8/encoder/encodemb.c intra residual transform/quantization flow, limited
// to DC-predicted keyframe macroblocks against a neutral predictor.

type SourceImage struct {
	Width  int
	Height int

	Y []byte
	U []byte
	V []byte

	YStride int
	UStride int
	VStride int
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

	// QCoeff is [25][16]int16 — contiguous in memory. Re-view as a
	// single [25*16]int16 so the batched fast-quantize writes the
	// per-block qcoeffs directly into the MacroblockCoefficients layout.
	qAll := unsafe.Slice((*int16)(unsafe.Pointer(&coeffs.QCoeff[0][0])), 25*16)
	FastQuantizeBlockBatch(dct[:16*16], &quant.Y1DC, qAll[:16*16], dqAll[:16*16], eobs[:16], 16)
	FastQuantizeBlockBatch(dct[16*16:24*16], &quant.UV, qAll[16*16:24*16], dqAll[16*16:24*16], eobs[16:24], 8)
	FastQuantizeBlockBatch(dct[24*16:25*16], &quant.Y2, qAll[24*16:25*16], dqAll[24*16:25*16], eobs[24:25], 1)

	// Commit per-block EOBs into the coefficient layout. Using the
	// loop variable bound to 25 lets the Go compiler hoist the
	// SetBlockEOB bounds checks (eob slice and EOB array share the
	// same length).
	for i := range 25 {
		coeffs.SetBlockEOB(i, int(eobs[i]))
	}
}

// fillResidual4x4Slice writes a source block into a caller-supplied slice so
// all 24 4x4 blocks land in one contiguous buffer ready for ForwardDCT4x4Batch.
func fillResidual4x4Slice(plane []byte, stride int, width int, height int, x int, y int, out []int16) {
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
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	if src.YStride < src.Width || src.UStride < uvWidth || src.VStride < uvWidth {
		return false
	}
	if len(src.Y) < sourcePlaneLen(src.YStride, src.Height, src.Width) {
		return false
	}
	if len(src.U) < sourcePlaneLen(src.UStride, uvHeight, uvWidth) {
		return false
	}
	if len(src.V) < sourcePlaneLen(src.VStride, uvHeight, uvWidth) {
		return false
	}
	return true
}

func sourcePlaneLen(stride int, rows int, visibleWidth int) int {
	if rows <= 0 {
		return 0
	}
	return stride*(rows-1) + visibleWidth
}

func clampCoord(v int, limit int) int {
	return min(v, limit-1)
}
