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
	energy := transformBlockEnergyDispatch(coeffs)
	if txSize != common.Tx32x32 {
		energy >>= 2
	}
	return energy
}

func TransformBlockErrorWithEnergy(coeffs, dqcoeffs []int16, txSize common.TxSize) (err, energy uint64) {
	err, energy = blockErrorFPWithEnergyDispatch(coeffs, dqcoeffs)
	if txSize != common.Tx32x32 {
		err >>= 2
		energy >>= 2
	}
	return err, energy
}

func transformBlockEnergyScalar(coeffs []int16) uint64 {
	return squareSumScalar(coeffs)
}

func ResidualSSE(residue []int16) uint64 {
	return residualSSEDispatch(residue)
}

func residualSSEScalar(residue []int16) uint64 {
	return squareSumScalar(residue)
}

func squareSumScalar(values []int16) uint64 {
	var sum uint64
	for _, value := range values {
		v := int64(value)
		sum += uint64(v * v)
	}
	return sum
}

func TransformBlockError(coeffs, dqcoeffs []int16, txSize common.TxSize) uint64 {
	err := transformBlockErrorDispatch(coeffs, dqcoeffs)
	if txSize != common.Tx32x32 {
		err >>= 2
	}
	return err
}

func transformBlockErrorScalar(coeffs, dqcoeffs []int16) uint64 {
	n := min(len(coeffs), len(dqcoeffs))
	var err uint64
	for i := range n {
		diff := int64(coeffs[i]) - int64(dqcoeffs[i])
		err += uint64(diff * diff)
	}
	return err
}
