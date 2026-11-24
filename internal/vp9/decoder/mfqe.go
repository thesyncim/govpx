package decoder

import "github.com/thesyncim/govpx/internal/vp9/common"

const (
	// MFQEPrecision mirrors libvpx vp9_postproc.h: weights live in 4-bit
	// fixed point ([0,16]).
	MFQEPrecision = 4
	// MFQEMvLenSquareThreshold is the libvpx vp9_mfqe.c:203 squared
	// MV-length cap, in 1/8-pel units, below which MFQE is admitted on an
	// inter block.
	MFQEMvLenSquareThreshold = 100
	// MFQEQDiffThreshold is the libvpx vp9_postproc.c:32 SB-level MFQE
	// q-difference precondition.
	MFQEQDiffThreshold = 20
	// MFQELastQThreshold is the libvpx vp9_postproc.c:33 previous-frame
	// quantizer precondition.
	MFQELastQThreshold = 170
)

// MFQEDecision mirrors libvpx vp9_mfqe.c:198. Block must be inter
// (mode >= NEARESTMV), at least 16x16, and have a small enough MV
// (squared L2 <= 100 in 1/8-pel units).
func MFQEDecision(mi *NeighborMi, curBs common.BlockSize) bool {
	row := int(mi.Mv[0].Row)
	col := int(mi.Mv[0].Col)
	mvLenSquare := row*row + col*col
	return mi.Mode >= common.NearestMv &&
		curBs >= common.Block16x16 &&
		mvLenSquare <= MFQEMvLenSquareThreshold
}

// MFQEThresholds returns the libvpx vp9_mfqe.c:147 block-size-conditioned
// SAD and vdiff thresholds for the MFQE block test.
func MFQEThresholds(bs common.BlockSize, qdiff int) (sadThr int, vdiffThr int) {
	adj := qdiff >> MFQEPrecision
	switch bs {
	case common.Block16x16:
		sadThr = 7 + adj
	case common.Block32x32:
		sadThr = 6 + adj
	default: // Block64x64
		sadThr = 5 + adj
	}
	vdiffThr = 125 + qdiff
	return
}

// MFQESum2D returns sum and sum-of-squares-of-diffs over a width x height
// window.
func MFQESum2D(a []byte, aStride int, b []byte, bStride int, width int, height int) (sum int, sse int, sad int) {
	for r := range height {
		aRow := a[r*aStride:]
		bRow := b[r*bStride:]
		for c := range width {
			diff := int(aRow[c]) - int(bRow[c])
			sum += diff
			sse += diff * diff
			if diff < 0 {
				sad += -diff
			} else {
				sad += diff
			}
		}
	}
	return
}

// MFQEBlockMetrics returns the libvpx-faithful vdiff and sad pair for a square
// block of side pixels, using the normalization in vp9_mfqe.c:168-177.
func MFQEBlockMetrics(side int, a []byte, aStride int, b []byte, bStride int) (vdiff int, sad int) {
	sum, sse, sadRaw := MFQESum2D(a, aStride, b, bStride, side, side)
	pels := side * side
	variance := sse - (sum*sum)/pels
	var round int
	var shift int
	switch side {
	case 16:
		round = 128
		shift = 8
	case 32:
		round = 512
		shift = 10
	default: // 64
		round = 2048
		shift = 12
	}
	vdiff = (variance + round) >> shift
	sad = (sadRaw + round) >> shift
	return
}
