package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9FrameCountsFromEncoder produces a decoder-shaped FrameCounts from
// the encoder's FrameCounts so the decoder-owned AdaptFrameContextWithCounts
// helper can drive non-frame-parallel adaptation on the encoder side.
//
// All mode / inter / mv / tx / skip / partition fields map 1:1. The
// coefficient histogram is reconstructed from the encoder's per-node
// branch stats: at node 0 the (EOB, not-EOB) counts are folded back into
// CoefCounts.EobBranch (total tests) and Coef[...][eobModelToken] (EOB
// count); the ZERO / ONE / TWO+ token counts come straight from nodes 1
// and 2. This mirrors the encoder-side test in
// internal/vp9/encoder/coef_block_test.go which asserts the inverse
// equality.
func vp9FrameCountsFromEncoder(src *encoder.FrameCounts) vp9dec.FrameCounts {
	var dst vp9dec.FrameCounts
	if src == nil {
		return dst
	}
	dst.YMode = src.YMode
	dst.Partition = src.Partition
	dst.SwitchableInterp = src.SwitchableInterp
	dst.InterMode = src.InterMode
	dst.IntraInter = src.IntraInter
	dst.CompInter = src.ReferenceMode.CompInter
	dst.SingleRef = src.ReferenceMode.SingleRef
	dst.CompRef = src.ReferenceMode.CompRef
	dst.Skip = src.Skip
	dst.Tx = vp9TxCountsFromEncoder(src.TxMode)
	dst.Mv = vp9NmvCountsFromEncoder(src.Mv)
	dst.Coef = vp9CoefCountsFromEncoderBranchStats(&src.CoefBranchStats)
	return dst
}

// vp9TxCountsFromEncoder lifts encoder.TxModeCounts into the
// decoder-shaped TxCounts. Both shapes are the per-context histogram
// of selected tx sizes for the 8x8 / 16x16 / 32x32 max-tx sub-tables.
func vp9TxCountsFromEncoder(src encoder.TxModeCounts) vp9dec.TxCounts {
	var dst vp9dec.TxCounts
	for ctx := range vp9dec.TxSizeContexts {
		dst.P8x8[ctx][0] = src.P8x8[ctx][0]
		dst.P8x8[ctx][1] = src.P8x8[ctx][1]
		dst.P16x16[ctx][0] = src.P16x16[ctx][0]
		dst.P16x16[ctx][1] = src.P16x16[ctx][1]
		dst.P16x16[ctx][2] = src.P16x16[ctx][2]
		dst.P32x32[ctx][0] = src.P32x32[ctx][0]
		dst.P32x32[ctx][1] = src.P32x32[ctx][1]
		dst.P32x32[ctx][2] = src.P32x32[ctx][2]
		dst.P32x32[ctx][3] = src.P32x32[ctx][3]
	}
	return dst
}

// vp9NmvCountsFromEncoder lifts encoder.NmvContextCounts into the
// decoder-shaped NmvContextCounts. Joints + per-axis component slabs
// have identical shape on both sides.
func vp9NmvCountsFromEncoder(src encoder.NmvContextCounts) vp9dec.NmvContextCounts {
	var dst vp9dec.NmvContextCounts
	dst.Joints = src.Joints
	for i := range 2 {
		dst.Comps[i].Sign = src.Comps[i].Sign
		dst.Comps[i].Classes = src.Comps[i].Classes
		dst.Comps[i].Class0 = src.Comps[i].Class0
		dst.Comps[i].Bits = src.Comps[i].Bits
		dst.Comps[i].Class0Fp = src.Comps[i].Class0Fp
		dst.Comps[i].Fp = src.Comps[i].Fp
		dst.Comps[i].Class0Hp = src.Comps[i].Class0Hp
		dst.Comps[i].Hp = src.Comps[i].Hp
	}
	return dst
}

// vp9CoefCountsFromEncoderBranchStats reconstructs the decoder's
// token-count CoefCounts from the encoder's per-node branch stats.
// The encoder records, per (tx, plane, ref, band, ctx) and per
// UnconstrainedNode (0..2):
//
//	node 0 (EOB):       [EOB count, not-EOB count]
//	node 1 (ZERO):      [ZERO count, non-zero count]
//	node 2 (PIVOT/ONE): [ONE count, TWO+ count]
//
// CoefCounts.Coef[...][zeroToken=0] = ZERO count = stats[1][0]
// CoefCounts.Coef[...][oneToken=1]  = ONE count  = stats[2][0]
// CoefCounts.Coef[...][twoToken=2]  = TWO+ count = stats[2][1]
// CoefCounts.Coef[...][eobModelToken=3] = EOB count = stats[0][0]
// CoefCounts.EobBranch[...] = total EOB tests = stats[0][0] + stats[0][1]
//
// Mirrors the inverse assertion in
// internal/vp9/encoder/coef_block_test.go's
// assertCoefPrefixStatsMatchDecoderCounts.
func vp9CoefCountsFromEncoderBranchStats(src *encoder.FrameCoefBranchStats) vp9dec.CoefCounts {
	var dst vp9dec.CoefCounts
	if src == nil {
		return dst
	}
	for tx := common.Tx4x4; tx <= common.Tx32x32; tx++ {
		for plane := range vp9dec.CoefPlaneTypes {
			for ref := range vp9dec.CoefRefTypes {
				for band := range vp9dec.CoefBands {
					ctxCount := vp9dec.BandCoefContexts(band)
					for ctx := range ctxCount {
						node := &src[tx][plane][ref][band][ctx]
						eob := node[0][0]
						neob := node[0][1]
						dst.Coef[tx][plane][ref][band][ctx][0] = node[1][0]
						dst.Coef[tx][plane][ref][band][ctx][1] = node[2][0]
						dst.Coef[tx][plane][ref][band][ctx][2] = node[2][1]
						dst.Coef[tx][plane][ref][band][ctx][3] = eob
						dst.EobBranch[tx][plane][ref][band][ctx] = eob + neob
					}
				}
			}
		}
	}
	return dst
}
