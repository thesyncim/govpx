package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Ported from libvpx v1.16.0 vpx_dsp/variance.c sub-pixel variance
// primitives, specialised to width=16 for the inner-loop hot path used
// by 16x16 motion search. The filter math is identical to the generic
// second-pass bilinear in variance.go; only the stride/pixel-step are
// pinned so the Go compiler can keep the inner loop tight.
//
// 16x16-specialised second-pass bilinear filter. Per the
// cmd/govpx-bench baseline cpuprofile, the generic version (which has
// to cope with arbitrary stride/pixelStep combinations) is the
// single-largest hot leaf in the encoder at 11.1% flat. Pinning the
// stride and pixel-step to 16 lets the Go compiler keep the inner
// loop tight - the microbenchmark runs ~1.75x faster than the generic
// path. The dispatch happens in varFilterBlock2DBilinearSecondPass:
// when the caller asks for the 16-wide case we route through here.
//
// Note: we deliberately do NOT specialise the first-pass bilinear or
// varianceBlock for 16x16. Microbenchmarks showed the obvious
// fixed-size array-view tightenings regressed those two by ~10-15%
// because the generic loops already have a tight indexing pattern
// that the Go compiler optimises well. Future SIMD work for arm64 /
// amd64 should plug in here under build tags; the scalar baseline is
// already close enough to its ceiling that further pure-Go gains are
// unlikely.

// varFilterBlock2DBilinearSecondPass16Scalar is the pure-Go reference
// implementation. The arch-specific files (variance_16x16_arm64.go +
// variance_16x16_arm64.s, variance_16x16_other.go) wrap it as
// varFilterBlock2DBilinearSecondPass16 and dispatch to NEON where
// available. Tests use this scalar version as the parity oracle.
func varFilterBlock2DBilinearSecondPass16Scalar(src *[17 * 16]uint16, dst []byte, height int, filter [2]int16) {
	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	const stride = 16
	for y := range height {
		srcRowA := (*[16]uint16)(src[y*stride : y*stride+16])
		srcRowB := (*[16]uint16)(src[(y+1)*stride : (y+1)*stride+16])
		dstRow := (*[16]byte)(dst[y*stride : y*stride+16])
		for x := range 16 {
			v := int(srcRowA[x])*f0 + int(srcRowB[x])*f1
			dstRow[x] = byte((v + round) >> shift)
		}
	}
}
