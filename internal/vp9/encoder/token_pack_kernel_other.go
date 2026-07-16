//go:build !arm64 || purego

package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func packTokenWindowKernel(
	bw *bitstream.Writer, tokens []TokenExtra, fc *vp9dec.FrameCoefProbs,
) (hasResidue bool, consumed int, ok bool, handled bool) {
	return false, 0, false, false
}
