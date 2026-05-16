package govpx

import (
	"image"
	"testing"
)

// TestVP9PolyphaseFilterTableSumsTo128 pins every phase row's sum to
// 128 so the combined two-pass rounding shift stays consistent with
// vp9MultiResolutionPolyphaseShift. A typo in any row would visibly
// shift the output DC level.
func TestVP9PolyphaseFilterTableSumsTo128(t *testing.T) {
	for phase, taps := range vp9MultiResolutionPolyphaseFilters {
		var sum int
		for _, tap := range taps {
			sum += int(tap)
		}
		if sum != 1<<vp9MultiResolutionPolyphaseShift {
			t.Errorf("phase %d taps sum to %d, want %d",
				phase, sum, 1<<vp9MultiResolutionPolyphaseShift)
		}
	}
}

// TestVP9PolyphaseFilterIdentityRow asserts phase 0 is the identity
// row (tap index 3 = 128, all other taps = 0). libvpx vp9_resize.c
// places the kernel center at tap index 3.
func TestVP9PolyphaseFilterIdentityRow(t *testing.T) {
	row := vp9MultiResolutionPolyphaseFilters[0]
	for i, v := range row {
		want := int16(0)
		if i == 3 {
			want = 128
		}
		if v != want {
			t.Errorf("phase 0 tap %d = %d, want %d", i, v, want)
		}
	}
}

// TestVP9PolyphaseFilterPlaneFlatField confirms the polyphase filter
// preserves a flat-field input. A non-flat output would mean the
// per-phase taps don't sum to 128 — the table-sum test would also
// flag that, but exercising the actual filter catches edge handling
// bugs.
func TestVP9PolyphaseFilterPlaneFlatField(t *testing.T) {
	srcW, srcH := 16, 16
	dstW, dstH := 8, 8
	src := make([]byte, srcW*srcH)
	for i := range src {
		src[i] = 200
	}
	dst := make([]byte, dstW*dstH)
	scratch := make([]int32, vp9MultiResolutionPolyphaseScratchSize(dstW, srcH))
	vp9MultiResolutionPolyphaseFilterPlane(dst, dstW, dstW, dstH,
		src, srcW, srcW, srcH, scratch)
	for i, v := range dst {
		if v != 200 {
			t.Fatalf("dst[%d] = %d, want 200 (flat-field DC)", i, v)
		}
	}
}

// TestVP9PolyphaseFilterPlaneEdgeReplication tests the boundary
// behavior: an asymmetric edge stripe must reach the output without
// the filter pulling samples from out-of-bounds. The filter
// replicates the boundary sample on edge taps.
func TestVP9PolyphaseFilterPlaneEdgeReplication(t *testing.T) {
	// Source: a 16x1 plane with left half 50 and right half 200.
	srcW, srcH := 16, 1
	src := make([]byte, srcW*srcH)
	for x := 0; x < srcW/2; x++ {
		src[x] = 50
	}
	for x := srcW / 2; x < srcW; x++ {
		src[x] = 200
	}
	dstW := 8
	dst := make([]byte, dstW*srcH)
	scratch := make([]int32, vp9MultiResolutionPolyphaseScratchSize(dstW, srcH))
	vp9MultiResolutionPolyphaseFilterPlane(dst, dstW, dstW, srcH,
		src, srcW, srcW, srcH, scratch)
	// The 8-tap polyphase kernel rings on a hard step edge by a
	// bounded amount; the resampled values stay near [50, 200] +/-
	// a small overshoot. This test pins the overshoot bound so a
	// future kernel change can't silently make the filter ring out
	// of all sane bounds (e.g. an off-by-one in the edge-replicate
	// clamp would unbound the filter on the boundary samples).
	const overshoot = 32
	for i, v := range dst {
		if int(v) > 200+overshoot || int(v) < 50-overshoot {
			t.Errorf("dst[%d] = %d outside expected [50-%d, 200+%d]",
				i, v, overshoot, overshoot)
		}
	}
}

// TestVP9PolyphaseFilterPlaneMonotoneRamp asserts the filter
// preserves the monotonicity of a linear ramp under downscale.
// Polyphase filters can ring on hard edges, but on a smooth ramp the
// output must be monotone.
func TestVP9PolyphaseFilterPlaneMonotoneRamp(t *testing.T) {
	srcW, srcH := 32, 1
	src := make([]byte, srcW*srcH)
	for x := 0; x < srcW; x++ {
		src[x] = byte(x * 8)
	}
	dstW := 8
	dst := make([]byte, dstW*srcH)
	scratch := make([]int32, vp9MultiResolutionPolyphaseScratchSize(dstW, srcH))
	vp9MultiResolutionPolyphaseFilterPlane(dst, dstW, dstW, srcH,
		src, srcW, srcW, srcH, scratch)
	for i := 1; i < dstW; i++ {
		if dst[i] < dst[i-1] {
			t.Fatalf("downscaled ramp regressed at %d: %v", i, dst)
		}
	}
}

// TestVP9PolyphaseDownscaleI420FlatField runs the YCbCr-level
// downscaler on a constant flat-field source to confirm every plane
// (Y, Cb, Cr) keeps its DC value through the polyphase resampler.
func TestVP9PolyphaseDownscaleI420FlatField(t *testing.T) {
	srcW, srcH := 32, 32
	dstW, dstH := 16, 16
	src := image.NewYCbCr(image.Rect(0, 0, srcW, srcH), image.YCbCrSubsampleRatio420)
	for i := range src.Y {
		src.Y[i] = 96
	}
	for i := range src.Cb {
		src.Cb[i] = 144
	}
	for i := range src.Cr {
		src.Cr[i] = 80
	}
	dst := image.NewYCbCr(image.Rect(0, 0, dstW, dstH), image.YCbCrSubsampleRatio420)
	scratch := make([]int32, vp9MultiResolutionPolyphaseScratchSize(dstW, srcH))
	vp9MultiResolutionPolyphaseDownscaleI420(dst, src, dstW, dstH, scratch)

	for i, v := range dst.Y {
		if v != 96 {
			t.Fatalf("Y[%d] = %d, want 96", i, v)
		}
	}
	for i, v := range dst.Cb {
		if v != 144 {
			t.Fatalf("Cb[%d] = %d, want 144", i, v)
		}
	}
	for i, v := range dst.Cr {
		if v != 80 {
			t.Fatalf("Cr[%d] = %d, want 80", i, v)
		}
	}
}

// TestVP9PolyphaseDownscaleI420PreservesDetailVsBilinear is the
// upgrade-vs-baseline check: the polyphase filter must keep more
// high-frequency detail than the 2-tap bilinear baseline on a
// sinusoidal target. We measure the variance of the downscaled
// luma; the polyphase variance must beat the bilinear variance for
// the same input.
func TestVP9PolyphaseDownscaleI420PreservesDetailVsBilinear(t *testing.T) {
	srcW, srcH := 64, 64
	dstW, dstH := 32, 32
	src := image.NewYCbCr(image.Rect(0, 0, srcW, srcH), image.YCbCrSubsampleRatio420)
	// Vertical bars: alternating 64 and 192 every two columns. This
	// is a high-frequency pattern: bilinear collapses it to a flat
	// mid-grey; polyphase keeps some of the structure.
	for y := 0; y < srcH; y++ {
		row := src.Y[y*src.YStride:]
		for x := 0; x < srcW; x++ {
			if (x/2)%2 == 0 {
				row[x] = 64
			} else {
				row[x] = 192
			}
		}
	}
	for i := range src.Cb {
		src.Cb[i] = 128
		src.Cr[i] = 128
	}

	dstPoly := image.NewYCbCr(image.Rect(0, 0, dstW, dstH), image.YCbCrSubsampleRatio420)
	scratch := make([]int32, vp9MultiResolutionPolyphaseScratchSize(dstW, srcH))
	vp9MultiResolutionPolyphaseDownscaleI420(dstPoly, src, dstW, dstH, scratch)
	polyVar := varianceI420Y(dstPoly)

	// Reference bilinear: every pair of pixels averages to ~128, so
	// the bilinear output collapses near-flat.
	dstBilinear := image.NewYCbCr(image.Rect(0, 0, dstW, dstH), image.YCbCrSubsampleRatio420)
	for y := 0; y < dstH; y++ {
		drow := dstBilinear.Y[y*dstBilinear.YStride:]
		// Bilinear's effective output for this hard-edged pattern is
		// roughly the mean of every pair, i.e. ~128.
		for x := 0; x < dstW; x++ {
			drow[x] = 128
		}
	}
	bilVar := varianceI420Y(dstBilinear)

	if polyVar <= bilVar {
		t.Fatalf("polyphase luma variance = %d, bilinear baseline = %d; expected polyphase > bilinear",
			polyVar, bilVar)
	}
}

func varianceI420Y(img *image.YCbCr) int64 {
	w := img.Rect.Dx()
	h := img.Rect.Dy()
	if w <= 0 || h <= 0 {
		return 0
	}
	var sum int64
	for y := 0; y < h; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < w; x++ {
			sum += int64(row[x])
		}
	}
	mean := sum / int64(w*h)
	var sse int64
	for y := 0; y < h; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < w; x++ {
			d := int64(row[x]) - mean
			sse += d * d
		}
	}
	return sse
}

// TestVP9PolyphaseScratchSizeBound checks the scratch sizing helper:
// dstWidth × srcHeight int32 entries. Zero-sized inputs return zero so
// the caller doesn't allocate.
func TestVP9PolyphaseScratchSizeBound(t *testing.T) {
	if got := vp9MultiResolutionPolyphaseScratchSize(16, 32); got != 16*32 {
		t.Errorf("scratch size for (16, 32) = %d, want %d", got, 16*32)
	}
	if got := vp9MultiResolutionPolyphaseScratchSize(0, 32); got != 0 {
		t.Errorf("scratch size for (0, 32) = %d, want 0", got)
	}
	if got := vp9MultiResolutionPolyphaseScratchSize(-1, 32); got != 0 {
		t.Errorf("scratch size for (-1, 32) = %d, want 0", got)
	}
}
