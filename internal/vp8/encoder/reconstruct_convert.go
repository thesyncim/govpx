package encoder

import (
	"unsafe"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

// Reconstruction conversion helpers bridge VP8 encoder decisions into the
// decoder-shaped macroblock state used for local reconstruction, mirroring
// libvpx v1.16.0's VP8 encode/reconstruct data flow.

func ConvertKeyFrameMode(src *KeyFrameMacroblockMode, dst *vp8dec.MacroblockMode) {
	*dst = vp8dec.MacroblockMode{
		SegmentID: src.SegmentID,
		RefFrame:  vp8common.IntraFrame,
		Mode:      src.YMode,
		UVMode:    src.UVMode,
		Is4x4:     src.YMode == vp8common.BPred,
		BModes:    src.BModes,
	}
}

func ConvertInterFrameMode(src *InterFrameMacroblockMode, dst *vp8dec.MacroblockMode) {
	*dst = vp8dec.MacroblockMode{
		SegmentID:   src.SegmentID,
		RefFrame:    ConvertInterFrameReference(src),
		Mode:        src.Mode,
		UVMode:      src.UVMode,
		Is4x4:       InterFrameModeUses4x4Tokens(src.Mode),
		BModes:      src.BModes,
		MV:          vp8dec.MotionVector{Row: src.MV.Row, Col: src.MV.Col},
		MBSkipCoeff: src.MBSkipCoeff,
		Partition:   src.Partition,
	}
	// MotionVector structs are layout-identical, so copy the full block-MV
	// array at once instead of assigning each element on the reconstruction path.
	dst.BlockMV = *(*[16]vp8dec.MotionVector)(unsafe.Pointer(&src.BlockMV))
}

func ConvertInterFrameReference(mode *InterFrameMacroblockMode) vp8common.MVReferenceFrame {
	// DCPred==0, BPred==4; single uint compare folds the dual-bound test.
	if uint(mode.Mode) <= uint(vp8common.BPred) {
		return vp8common.IntraFrame
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return vp8common.LastFrame
	}
	return mode.RefFrame
}

func ConvertMacroblockCoefficients(src *MacroblockCoefficients, is4x4 bool, dst *vp8dec.MacroblockTokens) {
	if !is4x4 {
		eob := src.EOB[24]
		dst.EOB[24] = eob
		copyQCoeffForEOB(&src.QCoeff[24], eob, &dst.QCoeff[24])
		for i := range 16 {
			eob := max(src.EOB[i], 1)
			dst.EOB[i] = eob
			copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
		}
	} else {
		dst.EOB[24] = 0
		for i := range 16 {
			eob := src.EOB[i]
			dst.EOB[i] = eob
			copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
		}
	}
	for i := 16; i < 24; i++ {
		eob := src.EOB[i]
		dst.EOB[i] = eob
		copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
	}
}

func InterFrameModeUses4x4Tokens(mode vp8common.MBPredictionMode) bool {
	return mode == vp8common.BPred || mode == vp8common.SplitMV
}

func copyQCoeffForEOB(src *[16]int16, eob uint8, dst *[16]int16) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dst[0] = src[0]
		return
	}
	*dst = *src
}
