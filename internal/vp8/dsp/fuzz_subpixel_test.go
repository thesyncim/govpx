package dsp

import (
	"testing"
)

// FuzzVP8DSPSubpixel is a differential SIMD-vs-scalar fuzz harness for
// the VP8 six-tap and bilinear sub-pel predictor families. Mirrors the
// libvpx test/vp8_predict_test.cc cross-check pattern.
//
// Op selector covers (per-size kernel variants — both SIMD and scalar):
//
//	0  SixTapPredict16x16
//	1  SixTapPredict8x8
//	2  SixTapPredict8x4
//	3  SixTapPredict4x4
//	4  BilinearPredict16x16
//	5  BilinearPredict8x8
//	6  BilinearPredict8x4
//	7  BilinearPredict4x4
//
// The SIMD dispatch lives in subpixel_{amd64,arm64}.go via the
// six*Maybe gates; the scalar oracle goes through the bypass functions
// sixTapPredict / bilinearPredict in subpixel.go which are the
// canonical libvpx vp8/common/filter.c ports.

func FuzzVP8DSPSubpixel(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 1024),
		bytes255(1024),
		bytesAlt(1024),
		bytesRamp(1024, 0),
		bytesRamp(1024, 17),
		bytesPattern(1024, 0x4D, 0x9B),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Need 1 op + 1 xoff + 1 yoff + a generous 32x32 source plane.
		const srcStride = 32
		const srcRows = 32
		const srcBytes = srcStride * srcRows
		if len(data) < 3+srcBytes {
			return
		}
		op := int(data[0]) % 8
		// Six-tap filter range is [0,7] (libvpx vp8/common/filter.c
		// SubPelFilters has 8 phases). Bilinear is also [0,7].
		xoff := int(data[1]) & 7
		yoff := int(data[2]) & 7

		src := make([]byte, srcBytes)
		copy(src, data[3:3+srcBytes])

		// SixTap needs 2 rows + 2 cols of source context above/left of
		// the prediction origin. Pick a safe start offset that gives
		// the largest (16x16) block 5 rows of bottom margin too.
		// origin row=8, col=8 -> rows [8-2 .. 8+15+3]=[6..26], cols similar
		const originY = 8
		const originX = 8
		srcView := src[originY*srcStride+originX:]

		var dstSim, dstScl []byte

		switch op {
		case 0: // SixTapPredict16x16
			dstSim = make([]byte, 16*16)
			dstScl = make([]byte, 16*16)
			SixTapPredict16x16(srcView, srcStride, xoff, yoff, dstSim, 16)
			sixTapPredict(srcView, srcStride, xoff, yoff, dstScl, 16, 16, 16)
			compareBlock(t, "SixTapPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 1: // SixTapPredict8x8
			dstSim = make([]byte, 8*8)
			dstScl = make([]byte, 8*8)
			SixTapPredict8x8(srcView, srcStride, xoff, yoff, dstSim, 8)
			sixTapPredict(srcView, srcStride, xoff, yoff, dstScl, 8, 8, 8)
			compareBlock(t, "SixTapPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 2: // SixTapPredict8x4
			dstSim = make([]byte, 8*4)
			dstScl = make([]byte, 8*4)
			SixTapPredict8x4(srcView, srcStride, xoff, yoff, dstSim, 8)
			sixTapPredict(srcView, srcStride, xoff, yoff, dstScl, 8, 8, 4)
			compareBlock(t, "SixTapPredict8x4", dstSim, dstScl, 8, 4, 8)
		case 3: // SixTapPredict4x4
			dstSim = make([]byte, 4*4)
			dstScl = make([]byte, 4*4)
			SixTapPredict4x4(srcView, srcStride, xoff, yoff, dstSim, 4)
			sixTapPredict(srcView, srcStride, xoff, yoff, dstScl, 4, 4, 4)
			compareBlock(t, "SixTapPredict4x4", dstSim, dstScl, 4, 4, 4)
		case 4: // BilinearPredict16x16
			dstSim = make([]byte, 16*16)
			dstScl = make([]byte, 16*16)
			BilinearPredict16x16(srcView, srcStride, xoff, yoff, dstSim, 16)
			bilinearPredict(srcView, srcStride, xoff, yoff, dstScl, 16, 16, 16)
			compareBlock(t, "BilinearPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 5: // BilinearPredict8x8
			dstSim = make([]byte, 8*8)
			dstScl = make([]byte, 8*8)
			BilinearPredict8x8(srcView, srcStride, xoff, yoff, dstSim, 8)
			bilinearPredict(srcView, srcStride, xoff, yoff, dstScl, 8, 8, 8)
			compareBlock(t, "BilinearPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 6: // BilinearPredict8x4
			dstSim = make([]byte, 8*4)
			dstScl = make([]byte, 8*4)
			BilinearPredict8x4(srcView, srcStride, xoff, yoff, dstSim, 8)
			bilinearPredict(srcView, srcStride, xoff, yoff, dstScl, 8, 8, 4)
			compareBlock(t, "BilinearPredict8x4", dstSim, dstScl, 8, 4, 8)
		case 7: // BilinearPredict4x4
			dstSim = make([]byte, 4*4)
			dstScl = make([]byte, 4*4)
			BilinearPredict4x4(srcView, srcStride, xoff, yoff, dstSim, 4)
			bilinearPredict(srcView, srcStride, xoff, yoff, dstScl, 4, 4, 4)
			compareBlock(t, "BilinearPredict4x4", dstSim, dstScl, 4, 4, 4)
		}
	})
}
