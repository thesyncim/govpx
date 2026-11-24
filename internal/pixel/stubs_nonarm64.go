//go:build !goexperiment.simd || !arm64 || purego

package pixel

const (
	fillFixedRows     = false
	IntraDCFillNative = false
	NativeLFByAxis    = false
	TMNative          = false
)

func Intra4x4DCFill(dst []byte, ds int, above, left []byte) bool { return false }

func IntraDCFill(dst []byte, ds int, above, left []byte, n int, up, lf bool) bool { return false }

func CanLoopFilterMacroHorizontal16(p []byte, pitch int, mblimit, blimit byte) bool                 { return false }
func LoopFilterMacroHorizontal16Unchecked(p []byte, pitch int, mblimit, blimit, limit, thresh byte) {}

func CanLoopFilterMacroVertical16(p []byte, pitch int, mblimit, blimit byte) bool                 { return false }
func LoopFilterMacroVertical16Unchecked(p []byte, pitch int, mblimit, blimit, limit, thresh byte) {}

func ResidualSSEActivity(_ []byte, _ int, _ []byte, _ int, _, _ int) (uint64, uint64, bool) {
	return 0, 0, false
}

// TMPredict4Native retains libvpx v1.16.0 vpx_dsp/intrapred.c semantics
// through the existing generic pixel implementation.
func TMPredict4Native(dst []byte, ds int, above, left []byte, top byte, publicChecked bool) {
	if publicChecked {
		_ = above[3]
		_ = left[3]
		_ = dst[3*ds+3]
	}
	TMPredict(dst, ds, above, left, top, 4)
}

func TMPredictBytes(dst []byte, ds int, above, left []byte, top byte, w int) bool { return false }

// TMPredictNative preserves the original generic pixel semantics on other builds.
func TMPredictNative(dst []byte, ds int, above, left []byte, top byte, w int) {
	TMPredict(dst, ds, above, left, top, w)
}
