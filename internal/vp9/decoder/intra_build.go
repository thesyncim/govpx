package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
)

// VP9 intra-prediction build driver. Ported from libvpx v1.16.0
// vp9/common/vp9_reconintra.c — the 8-bit-profile half of
// build_intra_predictors and the vp9_predict_intra_block dispatch
// table.
//
// The DSP intra-predictor kernels in internal/vp9/dsp/intrapred*.go
// expect:
//   - dst   : destination block buffer, raster order with `stride`.
//   - above : `bs` bytes of above-row predictor pixels (some directional
//             modes read 2*bs).
//   - left  : `bs` bytes of left-column predictor pixels.
//
// BuildIntraPredictors materializes the (above, left) borders from
// the source frame's reconstructed pixels (or the 127/129 sentinel
// fills when a neighbor is missing) before dispatching the kernel.

// IntraPredFn matches the DSP intra-predictor signature.
type IntraPredFn func(dst []uint8, stride int, above, left []uint8)

// IntraDcPredTable indexes the DC variant by (leftAvailable,
// aboveAvailable, tx_size). Mirrors libvpx's dc_pred[2][2][TX_SIZES]
// dispatch table:
//
//	[0][0]  Dc128 (no neighbors).
//	[0][1]  DcTop (only above available).
//	[1][0]  DcLeft (only left available).
//	[1][1]  Dc (both — the standard average).
var IntraDcPredTable = [2][2][common.TxSizes]IntraPredFn{
	{
		{dsp.VpxDc128Predictor4x4, dsp.VpxDc128Predictor8x8, dsp.VpxDc128Predictor16x16, dsp.VpxDc128Predictor32x32},
		{dsp.VpxDcTopPredictor4x4, dsp.VpxDcTopPredictor8x8, dsp.VpxDcTopPredictor16x16, dsp.VpxDcTopPredictor32x32},
	},
	{
		{dsp.VpxDcLeftPredictor4x4, dsp.VpxDcLeftPredictor8x8, dsp.VpxDcLeftPredictor16x16, dsp.VpxDcLeftPredictor32x32},
		{dsp.VpxDcPredictor4x4, dsp.VpxDcPredictor8x8, dsp.VpxDcPredictor16x16, dsp.VpxDcPredictor32x32},
	},
}

// IntraPredTable indexes the non-DC kernels by (mode, tx_size).
// Mirrors libvpx's pred[INTRA_MODES][TX_SIZES] dispatch table.
// Slot [DcPred] is unused — DC variants are picked from
// IntraDcPredTable.
var IntraPredTable = [common.IntraModes][common.TxSizes]IntraPredFn{
	{}, // DcPred — unused
	{dsp.VpxVPredictor4x4, dsp.VpxVPredictor8x8, dsp.VpxVPredictor16x16, dsp.VpxVPredictor32x32},
	{dsp.VpxHPredictor4x4, dsp.VpxHPredictor8x8, dsp.VpxHPredictor16x16, dsp.VpxHPredictor32x32},
	{dsp.VpxD45Predictor4x4, dsp.VpxD45Predictor8x8, dsp.VpxD45Predictor16x16, dsp.VpxD45Predictor32x32},
	{dsp.VpxD135Predictor4x4, dsp.VpxD135Predictor8x8, dsp.VpxD135Predictor16x16, dsp.VpxD135Predictor32x32},
	{dsp.VpxD117Predictor4x4, dsp.VpxD117Predictor8x8, dsp.VpxD117Predictor16x16, dsp.VpxD117Predictor32x32},
	{dsp.VpxD153Predictor4x4, dsp.VpxD153Predictor8x8, dsp.VpxD153Predictor16x16, dsp.VpxD153Predictor32x32},
	{dsp.VpxD207Predictor4x4, dsp.VpxD207Predictor8x8, dsp.VpxD207Predictor16x16, dsp.VpxD207Predictor32x32},
	{dsp.VpxD63Predictor4x4, dsp.VpxD63Predictor8x8, dsp.VpxD63Predictor16x16, dsp.VpxD63Predictor32x32},
	{dsp.VpxTmPredictor4x4, dsp.VpxTmPredictor8x8, dsp.VpxTmPredictor16x16, dsp.VpxTmPredictor32x32},
}

// IntraEdgeRefs collects the neighbor-pixel slices the caller
// extracts from the source frame for the current block. Decouples
// the C pointer arithmetic from the Go port.
//
//   - Above        : `2*bs` bytes starting at (row-1, col). For modes
//     that don't read above-right, only the first `bs`
//     are consulted.
//   - AboveLeft    : the single byte at (row-1, col-1). Only consulted
//     when both UpAvailable and LeftAvailable are true.
//   - Left         : `bs` bytes at (row+i, col-1) for i=0..bs-1.
//
// Caller is responsible for ensuring the slices have the right
// length when the corresponding need flag is set.
type IntraEdgeRefs struct {
	Above     []uint8
	AboveLeft uint8
	Left      []uint8
}

// BuildIntraPredictorsArgs bundles every input the libvpx
// build_intra_predictors driver consults. Caller-supplied fields
// mirror the fragments libvpx pulls out of MACROBLOCKD / xd->plane.
type BuildIntraPredictorsArgs struct {
	Dst            []uint8
	DstStride      int
	Mode           common.PredictionMode
	TxSize         common.TxSize
	Edges          IntraEdgeRefs
	UpAvailable    bool
	LeftAvailable  bool
	RightAvailable bool
	FrameWidth     int
	FrameHeight    int
	X0, Y0         int
	MbToRightEdge  int
	MbToBottomEdge int
}

// BuildIntraPredictors mirrors libvpx's build_intra_predictors
// (8-bit profile). Materializes the above-row and left-column border
// pixels into stack-allocated buffers and dispatches the matching
// DSP kernel. Border-extension matches libvpx byte-for-byte: 127 for
// missing above, 129 for missing left, edge replication when the
// block reaches into the frame margin.
//
// Output: a.Dst is overwritten with the prediction; libvpx writes
// `bs` rows of `bs` bytes each at stride a.DstStride.
func BuildIntraPredictors(a BuildIntraPredictorsArgs) {
	// aboveData[16] is the slot libvpx writes for above_row[-1]; we
	// expose it as topLeft and pass aboveRow = aboveData[17:] to the
	// kernel. Kernels that need the top-left byte read above[-1]; in
	// Go we synthesize a buffer where index 0 is the corner byte and
	// index 1..2*bs are the row.
	var leftCol [32]uint8
	var aboveBuf [1 + 64]uint8 // [0]=topLeft, [1:]=above row up to 2*bs
	topLeft := uint8(127)
	bs := 4 << uint(a.TxSize)

	need := ExtendModes[a.Mode]

	if need&NeedLeft != 0 {
		if a.LeftAvailable {
			ref := a.Edges.Left
			if a.MbToBottomEdge < 0 {
				if a.Y0+bs <= a.FrameHeight {
					copy(leftCol[:bs], ref[:bs])
				} else {
					extendBottom := min(a.FrameHeight-a.Y0, len(ref))
					copy(leftCol[:extendBottom], ref[:extendBottom])
					last := uint8(0)
					if extendBottom > 0 {
						last = ref[extendBottom-1]
					}
					for i := extendBottom; i < bs; i++ {
						leftCol[i] = last
					}
				}
			} else {
				copy(leftCol[:bs], ref[:bs])
			}
		} else {
			fillByte(leftCol[:bs], 129)
		}
	}

	if need&(NeedAbove|NeedAboveRight) != 0 {
		row := aboveBuf[1:] // exposed as the "above" slice; index 0 mirrors above_row[-1].
		need2bs := need&NeedAboveRight != 0
		span := bs
		if need2bs {
			span = 2 * bs
		}

		if a.UpAvailable {
			ref := a.Edges.Above
			if a.MbToRightEdge < 0 {
				switch {
				case a.X0+span <= a.FrameWidth:
					copy(row[:span], ref[:span])
				case a.X0+bs <= a.FrameWidth:
					if need2bs {
						r := a.FrameWidth - a.X0
						if a.RightAvailable && bs == 4 {
							copy(row[:r], ref[:r])
							fillByte(row[r:span], row[r-1])
						} else {
							copy(row[:bs], ref[:bs])
							fillByte(row[bs:span], row[bs-1])
						}
					} else {
						r := a.FrameWidth - a.X0
						copy(row[:r], ref[:r])
						fillByte(row[r:span], row[r-1])
					}
				case a.X0 <= a.FrameWidth:
					r := a.FrameWidth - a.X0
					copy(row[:r], ref[:r])
					if r > 0 {
						fillByte(row[r:span], row[r-1])
					}
				}
			} else {
				if need2bs {
					if bs == 4 && a.RightAvailable && a.LeftAvailable {
						copy(row[:span], ref[:span])
					} else {
						copy(row[:bs], ref[:bs])
						if bs == 4 && a.RightAvailable {
							copy(row[bs:span], ref[bs:span])
						} else {
							fillByte(row[bs:span], row[bs-1])
						}
					}
				} else {
					copy(row[:bs], ref[:bs])
				}
			}
			if a.LeftAvailable {
				topLeft = a.Edges.AboveLeft
			} else {
				topLeft = 129
			}
		} else {
			fillByte(row[:span], 127)
			topLeft = 127
		}
	}

	aboveBuf[0] = topLeft
	// DSP intra-pred kernels expect a slice where [0] is the
	// top-left corner byte and [1..] is the above row. The kernels
	// internally do `above[1:]` to strip the corner. We hand them
	// the corner-prefixed slice directly.
	aboveSlice := aboveBuf[0 : 1+2*bs]
	leftSlice := leftCol[:bs]

	if a.Mode == common.DcPred {
		li := 0
		if a.LeftAvailable {
			li = 1
		}
		ai := 0
		if a.UpAvailable {
			ai = 1
		}
		IntraDcPredTable[li][ai][a.TxSize](a.Dst, a.DstStride, aboveSlice, leftSlice)
		return
	}
	IntraPredTable[a.Mode][a.TxSize](a.Dst, a.DstStride, aboveSlice, leftSlice)
}

// fillByte memsets `b` to `v` without runtime.memclr — keeps the
// hot path inlineable.
func fillByte(b []uint8, v uint8) {
	for i := range b {
		b[i] = v
	}
}
