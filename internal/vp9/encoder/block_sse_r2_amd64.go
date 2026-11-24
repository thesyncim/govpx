//go:build goexperiment.simd && amd64 && !purego

package encoder

import "github.com/thesyncim/govpx/internal/pixel"

// The consumer needs only SSE, so avoid the variance kernel's signed sum.
func blockSSEOnlyR2(src []byte, so, ss int, ref []byte, ro, rs, w, h int) (uint64, bool) {
	if !pixel.Enabled || (w != 8 && w != 32 && w != 64) || h <= 0 || h > 64 || so < 0 || ro < 0 || so > len(src) || ro > len(ref) || ss < w || rs < w {
		return 0, false
	}
	src, ref = src[so:], ref[ro:]
	if len(src) < w || len(ref) < w || h-1 > (len(src)-w)/ss || h-1 > (len(ref)-w)/rs {
		return 0, false
	}
	switch w {
	case 8:
		return uint64(pixel.SSE8(src, ss, ref, rs, h)), true
	case 16:
		return uint64(pixel.SSE16(src, ss, ref, rs, h)), true
	}
	return pixel.SSEPlane16(src, ss, ref, rs, w, h)
}
