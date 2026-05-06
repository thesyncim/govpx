package decoder

import "github.com/thesyncim/gopvx/internal/vp8/common"

// Ported macroblock mode metadata from libvpx v1.16.0 vp8/common/blockd.h.

type MacroblockMode struct {
	Mode     common.MBPredictionMode
	UVMode   common.MBPredictionMode
	RefFrame common.MVReferenceFrame
	MV       MotionVector

	Is4x4       bool
	MBSkipCoeff bool
	SegmentID   uint8
	Partition   uint8

	BModes  [16]common.BPredictionMode
	BlockMV [16]MotionVector
}
