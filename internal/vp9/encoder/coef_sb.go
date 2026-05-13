package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 per-leaf coefficient walker. Ported from libvpx v1.16.0
// vp9/common/vp9_blockd.h — vp9_foreach_transformed_block_in_plane
// driving the same tokenize_b inner kernel pack_mb_tokens replays.
//
// The walker iterates the Y plane then the U / V planes; per plane it
// projects the luma block size onto the chroma subsampling, picks the
// transform size (luma_tx_size for Y, get_uv_tx_size for UV), and
// walks the tx-block grid in scan order. For each tx block the
// initial coefficient context (band-0 ctx) is recomputed from the
// above + left entropy-context cache, the residual is emitted via
// WriteCoefBlock, and the above + left bytes are stamped with the
// (eob > 0) flag so the next neighbor read sees the right state.
//
// Scan order picking is currently DCT_DCT-only — the inter path uses
// default scan unconditionally; the intra-only scan picker (row /
// col / default for tx_size < 32x32 keyed by Y_MODE) lands separately.

// WriteCoefSbArgs bundles the inputs WriteCoefSb consults across the
// three planes of one leaf block.
type WriteCoefSbArgs struct {
	BSize common.BlockSize
	// MiTxSize is mi->tx_size for the luma plane. Chroma plane tx size
	// is derived via GetUvTxSize against MiTxSize + per-plane
	// subsampling.
	MiTxSize common.TxSize

	IsInter int

	// Planes carries the per-plane macroblockd_plane shape (subsampling
	// + above/left entropy context buffers).
	Planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane

	// AboveOffsets / LeftOffsets are the entropy-context offsets the
	// caller has already advanced to for this leaf. AboveOffsets[p]
	// points at column 0 of plane p's residue context; LeftOffsets[p]
	// points at row 0 of plane p's residue context.
	AboveOffsets [vp9dec.MaxMbPlane]int
	LeftOffsets  [vp9dec.MaxMbPlane]int

	// PlaneDequant is the (DC, AC) dequant pair per plane.
	PlaneDequant [vp9dec.MaxMbPlane][2]int16

	// Fc is the active per-frame coefficient probability table.
	Fc *vp9dec.FrameCoefProbs

	// GetCoeffs is called per tx block to fetch the dequantized
	// coefficient buffer in raster (NOT scan) order, sized to
	// (1 << (2 * txSize)) entries. (r, c) are 4x4-unit indices into
	// the plane.
	GetCoeffs func(plane int, r, c int, txSize common.TxSize) []int16
}

// scanForTxSize returns the default scan/neighbors pair for `tx`.
// Inter blocks always take this branch; intra blocks pick row/col
// scans for non-32x32 sizes (not yet wired here).
func scanForTxSize(tx common.TxSize) (scan, neighbors []int16) {
	switch tx {
	case common.Tx4x4:
		return tables.DefaultScan4x4[:], tables.DefaultScan4x4Neighbors[:]
	case common.Tx8x8:
		return tables.DefaultScan8x8[:], tables.DefaultScan8x8Neighbors[:]
	case common.Tx16x16:
		return tables.DefaultScan16x16[:], tables.DefaultScan16x16Neighbors[:]
	default:
		return tables.DefaultScan32x32[:], tables.DefaultScan32x32Neighbors[:]
	}
}

// WriteCoefSb mirrors libvpx's per-block residue pack — the loop
// pack_mb_tokens replays after tokenize_b stages tokens for one
// leaf. Iterates the Y / U / V planes, walks each plane's tx-block
// grid in raster order, computes the initial coefficient context
// from the live above/left entropy context bytes, and emits each tx
// block's coefficient stream via WriteCoefBlock. Updates the
// above/left context bytes from (eob > 0) after each block so the
// next neighbor read sees the right state.
func WriteCoefSb(bw *bitstream.Writer, a WriteCoefSbArgs) error {
	for plane := 0; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &a.Planes[plane]
		planeType := 0
		if plane > 0 {
			planeType = 1
		}

		var txSize common.TxSize
		if plane == 0 {
			txSize = a.MiTxSize
		} else {
			// SbType is recovered from the leaf-size argument the
			// dispatcher already passed in: when the walker is invoked
			// for a leaf, BSize is the leaf bsize (not the SB root).
			txSize = vp9dec.GetUvTxSize(a.BSize, a.MiTxSize, pd)
		}

		planeBsize := vp9dec.GetPlaneBlockSize(a.BSize, pd)
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
		step := 1 << uint(txSize)
		scan, neighbors := scanForTxSize(txSize)
		dequant := a.PlaneDequant[plane]

		aboveBase := a.AboveOffsets[plane]
		leftBase := a.LeftOffsets[plane]

		for r := 0; r < num4x4H; r += step {
			for c := 0; c < num4x4W; c += step {
				aboveCtx := pd.AboveContext[aboveBase+c : aboveBase+c+step]
				leftCtx := pd.LeftContext[leftBase+r : leftBase+r+step]
				initCtx := vp9dec.GetEntropyContext(txSize, aboveCtx, leftCtx)

				coeffs := a.GetCoeffs(plane, r, c, txSize)
				if err := WriteCoefBlock(bw, WriteCoefBlockArgs{
					TxSize:    txSize,
					PlaneType: planeType,
					IsInter:   a.IsInter,
					DequantDC: dequant[0],
					DequantAC: dequant[1],
					Scan:      scan,
					Neighbors: neighbors,
					Coeffs:    coeffs,
					Fc:        a.Fc,
					InitCtx:   initCtx,
				}); err != nil {
					return err
				}

				eob := 0
				for i := 0; i < (1 << uint(2*txSize)); i++ {
					if coeffs[scan[i]] != 0 {
						eob = i + 1
					}
				}
				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				for j := 0; j < step; j++ {
					aboveCtx[j] = hasResidue
					leftCtx[j] = hasResidue
				}
			}
		}
	}
	return nil
}
