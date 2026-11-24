package scale

import "testing"

// TestMode_Valid asserts the libvpx-defined modes are valid and out-of-range
// values are rejected, mirroring vp8_set_internal_size's range check
// (vp8/encoder/onyx_if.c:5416-5430).
func TestMode_Valid(t *testing.T) {
	if !ModeNormal.Valid() || !ModeFourFive.Valid() || !ModeThreeFive.Valid() || !ModeOneTwo.Valid() {
		t.Fatalf("ModeNormal/FourFive/ThreeFive/OneTwo should all be valid")
	}
	if Mode(-1).Valid() {
		t.Fatalf("Mode(-1).Valid() = true, want false")
	}
	if Mode(4).Valid() {
		t.Fatalf("Mode(4).Valid() = true, want false")
	}
}

// TestScale2Ratio cross-checks the four published mode→ratio pairs against
// libvpx vp8/common/onyx.h:52-74.
func TestScale2Ratio(t *testing.T) {
	cases := []struct {
		mode   Mode
		hr, hs int
	}{
		{ModeNormal, 1, 1},
		{ModeFourFive, 4, 5},
		{ModeThreeFive, 3, 5},
		{ModeOneTwo, 1, 2},
	}
	for _, c := range cases {
		hr, hs := Scale2Ratio(c.mode)
		if hr != c.hr || hs != c.hs {
			t.Fatalf("Scale2Ratio(%v) = (%d, %d), want (%d, %d)", c.mode, hr, hs, c.hr, c.hs)
		}
	}
}

// TestScaledDimension cross-checks the (hs - 1 + src * hr) / hs rounding
// from libvpx vp8/encoder/onyx_if.c:1681-1685 across known dimensions.
func TestScaledDimension(t *testing.T) {
	cases := []struct {
		src  int
		mode Mode
		want int
	}{
		// Normal (1:1) is identity.
		{320, ModeNormal, 320},
		{1, ModeNormal, 1},
		// 4:5 — 320 * 4 / 5 = 256
		{320, ModeFourFive, 256},
		{640, ModeFourFive, 512},
		// 3:5 — 320 * 3 / 5 = 192
		{320, ModeThreeFive, 192},
		{640, ModeThreeFive, 384},
		// 1:2 — 320 / 2 = 160
		{320, ModeOneTwo, 160},
		{640, ModeOneTwo, 320},
		// Odd-pixel rounding: ((hs - 1 + src*hr) / hs) rounds up.
		// src=10, mode=4:5 → (4 + 40) / 5 = 8.
		{10, ModeFourFive, 8},
		// src=7, mode=1:2 → (1 + 7) / 2 = 4.
		{7, ModeOneTwo, 4},
	}
	for _, c := range cases {
		got := ScaledDimension(c.src, c.mode)
		if got != c.want {
			t.Fatalf("ScaledDimension(%d, %v) = %d, want %d", c.src, c.mode, got, c.want)
		}
	}
}

// TestHorizontalLine54 asserts the 5→4 kernel matches libvpx's published
// weights on a ramp input. The libvpx C reference (gen_scalers.c:36-62)
// computes:
//
//	d[0] = a
//	d[1] = (b*192 + c*64 + 128) >> 8
//	d[2] = (c*128 + d*128 + 128) >> 8
//	d[3] = (d*64 + e*192 + 128) >> 8
//
// for every contiguous 5-pixel input chunk a,b,c,d,e.
func TestHorizontalLine54(t *testing.T) {
	src := []byte{0, 64, 128, 192, 255, 10, 50, 100, 150, 200}
	dst := make([]byte, 8)
	horizontalLine54(src, len(src), dst)
	// First 5-pixel chunk: a=0, b=64, c=128, d=192, e=255
	want := []byte{
		0,
		byte((64*192 + 128*64 + 128) >> 8),
		byte((128*128 + 192*128 + 128) >> 8),
		byte((192*64 + 255*192 + 128) >> 8),
		// Second 5-pixel chunk: a=10, b=50, c=100, d=150, e=200
		10,
		byte((50*192 + 100*64 + 128) >> 8),
		byte((100*128 + 150*128 + 128) >> 8),
		byte((150*64 + 200*192 + 128) >> 8),
	}
	for i, v := range want {
		if dst[i] != v {
			t.Fatalf("horizontalLine54[%d] = %d, want %d", i, dst[i], v)
		}
	}
}

// TestHorizontalLine53 cross-checks the 5→3 kernel
// (gen_scalers.c:110-135).
func TestHorizontalLine53(t *testing.T) {
	src := []byte{0, 64, 128, 192, 255}
	dst := make([]byte, 3)
	horizontalLine53(src, len(src), dst)
	want := []byte{
		0,
		byte((64*85 + 128*171 + 128) >> 8),
		byte((192*171 + 255*85 + 128) >> 8),
	}
	for i, v := range want {
		if dst[i] != v {
			t.Fatalf("horizontalLine53[%d] = %d, want %d", i, dst[i], v)
		}
	}
}

// TestHorizontalLine21 asserts the 2→1 point-subsample kernel
// (gen_scalers.c:181-198).
func TestHorizontalLine21(t *testing.T) {
	src := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	dst := make([]byte, 4)
	horizontalLine21(src, len(src), dst)
	want := []byte{0, 2, 4, 6}
	for i, v := range want {
		if dst[i] != v {
			t.Fatalf("horizontalLine21[%d] = %d, want %d", i, dst[i], v)
		}
	}
}

// TestVerticalBand54 asserts the 5→4 vertical kernel
// (gen_scalers.c:64-88) on a single-column input.
func TestVerticalBand54(t *testing.T) {
	srcPitch := 1
	dstPitch := 1
	src := []byte{0, 64, 128, 192, 255}
	dst := make([]byte, 4)
	verticalBand54(src, srcPitch, dst, dstPitch, 1)
	want := []byte{
		0,
		byte((64*192 + 128*64 + 128) >> 8),
		byte((128*128 + 192*128 + 128) >> 8),
		byte((192*64 + 255*192 + 128) >> 8),
	}
	for i, v := range want {
		if dst[i*dstPitch] != v {
			t.Fatalf("verticalBand54[%d] = %d, want %d", i, dst[i*dstPitch], v)
		}
	}
}

// TestVerticalBand53 cross-checks the 5→3 vertical kernel
// (gen_scalers.c:137-160).
func TestVerticalBand53(t *testing.T) {
	srcPitch := 1
	dstPitch := 1
	src := []byte{0, 64, 128, 192, 255}
	dst := make([]byte, 3)
	verticalBand53(src, srcPitch, dst, dstPitch, 1)
	want := []byte{
		0,
		byte((64*85 + 128*171 + 128) >> 8),
		byte((192*171 + 255*85 + 128) >> 8),
	}
	for i, v := range want {
		if dst[i*dstPitch] != v {
			t.Fatalf("verticalBand53[%d] = %d, want %d", i, dst[i*dstPitch], v)
		}
	}
}

// TestVerticalBand21 asserts the 2→1 point-sample kernel
// (gen_scalers.c:200-207). Plain copy.
func TestVerticalBand21(t *testing.T) {
	src := []byte{10, 20, 30, 40}
	dst := make([]byte, 4)
	verticalBand21(src, dst, len(dst))
	for i := range src {
		if dst[i] != src[i] {
			t.Fatalf("verticalBand21[%d] = %d, want %d", i, dst[i], src[i])
		}
	}
}

// TestVerticalBand21Interpolated asserts the 3/10/3 interpolated kernel
// (gen_scalers.c:209-228). For each output pixel:
//
//	dst[i] = (3*a + 10*b + 3*c + 8) >> 4
//
// where a,b,c are above/current/below rows. The Go port takes (buf, mid,
// srcPitch) so the kernel can index the row above without negative slice
// indexing.
func TestVerticalBand21Interpolated(t *testing.T) {
	above := []byte{10, 20, 30, 40}
	middle := []byte{50, 60, 70, 80}
	below := []byte{90, 100, 110, 120}
	buf := make([]byte, 12)
	copy(buf[0:4], above)
	copy(buf[4:8], middle)
	copy(buf[8:12], below)
	dst := make([]byte, 4)
	verticalBand21Interpolated(buf, 4, 4, dst, 4)
	for i := range 4 {
		want := byte((3*int(above[i]) + 10*int(middle[i]) + 3*int(below[i]) + 8) >> 4)
		if dst[i] != want {
			t.Fatalf("verticalBand21Interpolated[%d] = %d, want %d", i, dst[i], want)
		}
	}
}

// TestScale2D_4to5_FullFrame end-to-end checks Scale2D's 4:5 dispatch
// against directly invoked horizontal+vertical kernels. The frame is
// 5x5 input → 4x4 output.
func TestScale2D_4to5_FullFrame(t *testing.T) {
	const srcW, srcH = 5, 5
	const dstW, dstH = 4, 4
	const dstPitch = 4
	const tempH = 6 // sourceBandHeight (5) + 1
	src := []byte{
		0, 64, 128, 192, 255,
		10, 74, 138, 202, 255,
		20, 84, 148, 212, 255,
		30, 94, 158, 222, 255,
		40, 104, 168, 232, 255,
	}
	dst := make([]byte, dstPitch*dstH)
	temp := make([]byte, tempH*dstPitch)
	Scale2D(src, srcW, srcW, srcH, dst, dstPitch, dstW, dstH, temp, tempH, 5, 4, 5, 4, false)

	// Compute reference: first horizontally scale every source row into a
	// per-row buffer, then vertically scale 5 rows into 4 columns.
	hScaled := make([]byte, dstPitch*srcH)
	for r := range srcH {
		horizontalLine54(src[r*srcW:(r+1)*srcW], srcW, hScaled[r*dstPitch:(r+1)*dstPitch])
	}
	expected := make([]byte, dstPitch*dstH)
	for col := range dstW {
		colSrc := make([]byte, srcH)
		for r := range srcH {
			colSrc[r] = hScaled[r*dstPitch+col]
		}
		colDst := make([]byte, dstH)
		verticalBand54(colSrc, 1, colDst, 1, 1)
		for r := range dstH {
			expected[r*dstPitch+col] = colDst[r]
		}
	}
	for i := range expected {
		if dst[i] != expected[i] {
			t.Fatalf("Scale2D 5x5→4x4 dst[%d] = %d, want %d (full frame: dst=%v expected=%v)",
				i, dst[i], expected[i], dst, expected)
		}
	}
}

func TestScale2DDoesNotAllocate(t *testing.T) {
	const (
		srcW     = 10
		srcH     = 10
		dstW     = 8
		dstH     = 8
		dstPitch = 8
		tempH    = 6
	)
	src := make([]byte, srcW*srcH)
	dst := make([]byte, dstPitch*dstH)
	temp := make([]byte, tempH*dstPitch)
	for i := range src {
		src[i] = byte((i*3 + 17) & 0xff)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		Scale2D(src, srcW, srcW, srcH, dst, dstPitch, dstW, dstH, temp, tempH, 5, 4, 5, 4, false)
	})
	if allocs != 0 {
		t.Fatalf("Scale2D allocations = %.1f, want 0", allocs)
	}
}
