package encoder

import "github.com/thesyncim/govpx/internal/vp8/dsp"

// Analysis block helpers mirror libvpx v1.16.0 VP8 encoder transform-error
// and inverse-transform add flows used by rdopt.c / encodemb.c.

// TransformBlockError returns the VP8 transform-domain squared error for one
// coefficient block against its dequantized reconstruction.
func TransformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	return dsp.TransformBlockError(coeff, dqcoeff)
}

// AddQuantizedBlockResidual applies one quantized 4x4 residual block to dst.
// It uses the DC-only inverse transform fast path for eob==1, matching libvpx
// v1.16.0's VP8 reconstruction flow.
func AddQuantizedBlockResidual(eob int, dq *[16]int16, dst []byte, stride int) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dsp.DCOnlyIDCT4x4Add(dq[0], dst, stride, dst, stride)
		return
	}
	dsp.IDCT4x4Add(dq, dst, stride, dst, stride)
}

func AnalysisYBlockOffset(block int, stride int) int {
	return (block>>2)*4*stride + (block&3)*4
}

func AnalysisUVBlockOffset(block int, stride int) int {
	return (block>>1)*4*stride + (block&1)*4
}
