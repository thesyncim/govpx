package common

import (
	"testing"
)

// Pinning tests for the YV12 border substrate. All reference values are
// hand-derived from the libvpx v1.16.0 C bodies at
// vpx_scale/generic/yv12extend.c:22-60 (extend_plane) and lines 130-171
// (vpx_extend_frame_borders_c).

// TestVp9YV12BorderConstantsMatchLibvpx pins the border / extend
// constants against the libvpx vpx_scale/yv12config.h:23-27 macros so
// any future drift fails fast.
func TestVp9YV12BorderConstantsMatchLibvpx(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"VP8BORDERINPIXELS", VP8BorderInPixels, 32},
		{"VP9INNERBORDERINPIXELS", VP9InnerBorderInPixels, 96},
		{"VP9_INTERP_EXTEND", VP9InterpExtend, 4},
		{"VP9_ENC_BORDER_IN_PIXELS", VP9EncBorderInPixels, 160},
		{"VP9_DEC_BORDER_IN_PIXELS", VP9DecBorderInPixels, 32},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestVp9ExtendPlaneLeftRightBorder builds a 4x4 visible plane inside
// a stride=8 padded buffer with extendLeft=2 / extendRight=2 (no
// top/bottom yet) and asserts the left / right border rows replicate
// the row's leftmost / rightmost pixel.
//
// Layout (X = uninitialised, V = visible body, dot = expected after
// extend):
//
//	y=0:  X X V V V V X X   -> .L .L V V V V .R .R   where L=V[y*stride+2], R=V[y*stride+5]
//	y=1:  X X V V V V X X
//	y=2:  X X V V V V X X
//	y=3:  X X V V V V X X
//
// libvpx extend_plane reference (yv12extend.c:33-41) — the per-row
// loop memsets extend_left copies of src_ptr1[0] into dst_ptr1 and
// extend_right copies of src_ptr2[0] into dst_ptr2.
func TestVp9ExtendPlaneLeftRightBorder(t *testing.T) {
	stride := 8
	rows := 4
	pixels := make([]uint8, stride*rows)
	// Visible body — distinct values per row so the test catches a
	// cross-row memcpy bug.
	want := map[int][8]uint8{}
	for y := range rows {
		base := y*stride + 2
		pixels[base+0] = uint8(10 + y*10)
		pixels[base+1] = uint8(11 + y*10)
		pixels[base+2] = uint8(12 + y*10)
		pixels[base+3] = uint8(13 + y*10)
		want[y] = [8]uint8{
			uint8(10 + y*10), uint8(10 + y*10), // left border (== row's leftmost).
			uint8(10 + y*10), uint8(11 + y*10), uint8(12 + y*10), uint8(13 + y*10),
			uint8(13 + y*10), uint8(13 + y*10), // right border (== row's rightmost).
		}
	}

	// srcOff = 2 (visible plane's top-left at column 2).
	// width=4, height=4, extendTop=0, extendBottom=0, extendLeft=2, extendRight=2.
	extendPlane(pixels, 2, stride, 4, 4, 0, 2, 0, 2)

	for y := range rows {
		var got [8]uint8
		copy(got[:], pixels[y*stride:y*stride+stride])
		if got != want[y] {
			t.Errorf("row %d: got %v want %v", y, got, want[y])
		}
	}
}

// TestVp9ExtendPlaneTopBottomBorder verifies the top / bottom block
// memcpy bodies in extend_plane (libvpx yv12extend.c:51-59). Builds a
// stride=4 buffer with extendTop=2, extendBottom=2 (no left/right);
// the top two rows must equal the first visible row and the bottom
// two rows must equal the last visible row, byte-for-byte across the
// full linesize = extend_left + extend_right + width = 4.
func TestVp9ExtendPlaneTopBottomBorder(t *testing.T) {
	stride := 4
	rows := 6 // 2 top + 2 visible + 2 bottom.
	pixels := make([]uint8, stride*rows)
	// Visible plane at row 2..3 (extendTop = 2). Distinct values per
	// row so the top / bottom replication is unambiguous.
	for x := range 4 {
		pixels[2*stride+x] = uint8(20 + x)
		pixels[3*stride+x] = uint8(30 + x)
	}
	// srcOff = 2*stride + 0 (visible plane top-left = (row=2, col=0)).
	extendPlane(pixels, 2*stride, stride, 4, 2, 2, 0, 2, 0)

	wantTop := [4]uint8{20, 21, 22, 23}
	wantBot := [4]uint8{30, 31, 32, 33}
	for y := range 2 {
		var got [4]uint8
		copy(got[:], pixels[y*stride:y*stride+stride])
		if got != wantTop {
			t.Errorf("top row %d: got %v want %v", y, got, wantTop)
		}
	}
	for y := 4; y < 6; y++ {
		var got [4]uint8
		copy(got[:], pixels[y*stride:y*stride+stride])
		if got != wantBot {
			t.Errorf("bottom row %d: got %v want %v", y, got, wantBot)
		}
	}
}

// TestVp9YV12BuildBorderedPlaneFullExtension is the end-to-end test:
// build an 8x8 visible plane and ask for the libvpx encoder border
// (32 — the dec border value, smaller than the ENC's 160 but sufficient
// for int_pro_motion's 32-pixel reach). Verify all four corners
// replicate the corresponding visible corner and that the visible
// body is copied byte-for-byte.
func TestVp9YV12BuildBorderedPlaneFullExtension(t *testing.T) {
	w, h := 8, 8
	border := 32
	src := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			// Pattern: distinct value per (y, x) so any
			// shift-by-N bug is caught.
			src[y*w+x] = uint8(y*16 + x)
		}
	}

	var buf YV12BorderBuffer
	pixels, stride, originX, originY := YV12BuildBorderedPlane(&buf, src, w, w, h, border)

	if stride != w+2*border {
		t.Fatalf("stride: got %d want %d", stride, w+2*border)
	}
	if originX != border || originY != border {
		t.Fatalf("origin: got (%d,%d) want (%d,%d)", originX, originY, border, border)
	}
	if buf.Rows() != h+2*border {
		t.Fatalf("rows: got %d want %d", buf.Rows(), h+2*border)
	}

	// Visible body — must match src byte-for-byte.
	for y := range h {
		for x := range w {
			got := pixels[(originY+y)*stride+(originX+x)]
			want := src[y*w+x]
			if got != want {
				t.Fatalf("visible (%d,%d): got %d want %d", y, x, got, want)
			}
		}
	}

	// Left border — every row r in [0, rows) should have left
	// border == src[clamp(r-border, 0, h-1) * w + 0].
	for r := 0; r < buf.Rows(); r++ {
		srcRow := r - border
		if srcRow < 0 {
			srcRow = 0
		} else if srcRow >= h {
			srcRow = h - 1
		}
		want := src[srcRow*w+0]
		for x := range border {
			got := pixels[r*stride+x]
			if got != want {
				t.Fatalf("left border (r=%d,x=%d): got %d want %d", r, x, got, want)
			}
		}
	}

	// Right border — every row r should have right border ==
	// src[clamp(r-border, 0, h-1) * w + (w-1)].
	for r := 0; r < buf.Rows(); r++ {
		srcRow := r - border
		if srcRow < 0 {
			srcRow = 0
		} else if srcRow >= h {
			srcRow = h - 1
		}
		want := src[srcRow*w+(w-1)]
		for x := border + w; x < stride; x++ {
			got := pixels[r*stride+x]
			if got != want {
				t.Fatalf("right border (r=%d,x=%d): got %d want %d", r, x, got, want)
			}
		}
	}

	// Top border — every row r in [0, border) should equal the
	// first visible row across the full linesize.
	firstVisible := make([]uint8, stride)
	copy(firstVisible, pixels[border*stride:border*stride+stride])
	for r := range border {
		for x := range stride {
			if pixels[r*stride+x] != firstVisible[x] {
				t.Fatalf("top border row %d col %d: got %d want %d",
					r, x, pixels[r*stride+x], firstVisible[x])
			}
		}
	}

	// Bottom border — every row r in [border+h, rows) should equal
	// the last visible row.
	lastVisible := make([]uint8, stride)
	copy(lastVisible, pixels[(border+h-1)*stride:(border+h-1)*stride+stride])
	for r := border + h; r < buf.Rows(); r++ {
		for x := range stride {
			if pixels[r*stride+x] != lastVisible[x] {
				t.Fatalf("bottom border row %d col %d: got %d want %d",
					r, x, pixels[r*stride+x], lastVisible[x])
			}
		}
	}
}

// TestVp9YV12BuildBorderedPlaneReusesBuffer verifies the lazy-alloc /
// reuse pattern: calling YV12BuildBorderedPlane twice on the same
// buf with the same dimensions reuses the underlying slice (no
// reallocation).
func TestVp9YV12BuildBorderedPlaneReusesBuffer(t *testing.T) {
	w, h := 16, 16
	border := 32
	src := make([]uint8, w*h)
	for i := range src {
		src[i] = uint8(i & 0xFF)
	}
	var buf YV12BorderBuffer
	_, _, _, _ = YV12BuildBorderedPlane(&buf, src, w, w, h, border)
	first := buf.Pixels
	_, _, _, _ = YV12BuildBorderedPlane(&buf, src, w, w, h, border)
	second := buf.Pixels
	// Same backing array => unsafe.SliceData would match; compare by
	// length & cap & a sentinel byte to keep the test stdlib-only.
	if cap(first) != cap(second) || len(first) != len(second) {
		t.Fatalf("buffer reuse: cap/len mismatch (%d/%d vs %d/%d)",
			cap(first), len(first), cap(second), len(second))
	}
	// Mutate first[0] and expect second[0] to track (same backing array).
	first[0] = 99
	if second[0] != 99 {
		t.Fatalf("buffer reuse: backing array detached on reuse")
	}
}

// TestVp9YV12BuildBorderedPlaneSatisfiesIntProReach is the pinning
// case that justifies the existence of this substrate. After building
// a bordered copy with VP9_ENC_BORDER_IN_PIXELS (or the smaller
// VP9_DEC_BORDER_IN_PIXELS = 32 — the minimum int_pro_motion needs),
// the int-pro motion search's worst-case reach of
// refOff - (bw>>1) = -32 bytes for BLOCK_64X64 must stay within the
// allocation. We validate the formula directly: with originX = border
// and a 64x64 SB anchored at the visible plane's top-left, the
// negative-reach offset must be >= 0.
func TestVp9YV12BuildBorderedPlaneSatisfiesIntProReach(t *testing.T) {
	w, h := 64, 64
	border := VP9DecBorderInPixels // 32 — the int_pro minimum.
	src := make([]uint8, w*h)
	for i := range src {
		src[i] = 128
	}
	var buf YV12BorderBuffer
	pixels, stride, originX, originY := YV12BuildBorderedPlane(&buf, src, w, w, h, border)

	// SB at visible plane's top-left.
	bw := 64
	refOff := originY*stride + originX
	negReachOff := refOff - (bw >> 1) // libvpx vp9_mcomp.c:2317.
	if negReachOff < 0 {
		t.Fatalf("int_pro negative-reach offset underflows: refOff=%d "+
			"reach=-%d origin=(%d,%d) stride=%d",
			refOff, bw>>1, originX, originY, stride)
	}
	// Spot-check the row above the SB (row originY-1) — must equal
	// the first visible row (top-border replication).
	for x := range bw {
		got := pixels[(originY-1)*stride+(originX+x)]
		want := pixels[originY*stride+(originX+x)]
		if got != want {
			t.Fatalf("top-border (r=%d,x=%d): got %d want %d (= visible row 0)",
				originY-1, x, got, want)
		}
	}
	// Spot-check the column left of the SB (col originX-1) — must
	// equal column 0 of every visible row.
	for y := range bw {
		got := pixels[(originY+y)*stride+(originX-1)]
		want := pixels[(originY+y)*stride+originX]
		if got != want {
			t.Fatalf("left-border (r=%d,c=%d): got %d want %d (= col 0)",
				originY+y, originX-1, got, want)
		}
	}
}
