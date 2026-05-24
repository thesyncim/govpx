package encoder

import "github.com/thesyncim/govpx/internal/vp9/common"

// ConditionalSkipIntra mirrors the directional intra-mode pruning used by
// libvpx's keyframe mode search.
func ConditionalSkipIntra(mode, bestMode common.PredictionMode) bool {
	switch mode {
	case common.D117Pred:
		return bestMode != common.VPred && bestMode != common.D135Pred
	case common.D63Pred:
		return bestMode != common.VPred && bestMode != common.D45Pred
	case common.D207Pred:
		return bestMode != common.HPred && bestMode != common.D45Pred
	case common.D153Pred:
		return bestMode != common.HPred && bestMode != common.D135Pred
	default:
		return false
	}
}

// RestorePlaneRect copies a packed saved rectangle back into a strided plane.
func RestorePlaneRect(data []byte, stride, x0, y0, w, h int, saved []byte) {
	for y := range h {
		copy(data[(y0+y)*stride+x0:(y0+y)*stride+x0+w],
			saved[y*w:(y+1)*w])
	}
}

func TransformBlockErrorShifted(coeffs, dqcoeffs []int16) uint64 {
	return TransformBlockError(coeffs, dqcoeffs, common.Tx4x4)
}

func TransformBlockEnergy(coeffs []int16, txSize common.TxSize) uint64 {
	var energy uint64
	for _, coeff := range coeffs {
		v := int64(coeff)
		energy += uint64(v * v)
	}
	if txSize != common.Tx32x32 {
		energy >>= 2
	}
	return energy
}

func ResidualSSE(residue []int16) uint64 {
	var sse uint64
	for _, diff := range residue {
		v := int64(diff)
		sse += uint64(v * v)
	}
	return sse
}

func TransformBlockError(coeffs, dqcoeffs []int16, txSize common.TxSize) uint64 {
	n := min(len(coeffs), len(dqcoeffs))
	var err uint64
	for i := range n {
		diff := int64(coeffs[i]) - int64(dqcoeffs[i])
		err += uint64(diff * diff)
	}
	if txSize != common.Tx32x32 {
		err >>= 2
	}
	return err
}
