package common

import "testing"

func TestNewDecoderFrameLayoutMatchesLibvpxBorderShape(t *testing.T) {
	layout := NewDecoderFrameLayout(65, 49, 0)
	if layout.YWidth != 72 || layout.YHeight != 56 {
		t.Fatalf("visible alignment = %dx%d, want 72x56",
			layout.YWidth, layout.YHeight)
	}
	if layout.YStride != 160 {
		t.Fatalf("YStride = %d, want 160", layout.YStride)
	}
	if layout.UVStride != 80 {
		t.Fatalf("UVStride = %d, want 80", layout.UVStride)
	}
	if layout.YOrigin != 32*160+32 {
		t.Fatalf("YOrigin = %d, want %d", layout.YOrigin, 32*160+32)
	}
	if layout.UVOrigin != 16*80+16 {
		t.Fatalf("UVOrigin = %d, want %d", layout.UVOrigin, 16*80+16)
	}
	if layout.YFullLen != 160*(56+64) {
		t.Fatalf("YFullLen = %d, want %d", layout.YFullLen, 160*(56+64))
	}
	if layout.UVFullLen != 80*(28+32) {
		t.Fatalf("UVFullLen = %d, want %d", layout.UVFullLen, 80*(28+32))
	}
}

func TestNewDecoderFrameLayoutForPlanesAlignsOrigins(t *testing.T) {
	const alignment = 128
	layout := NewDecoderFrameLayout(64, 64, alignment)
	y := make([]byte, layout.YFullLen+alignment)
	u := make([]byte, layout.UVFullLen+alignment)
	v := make([]byte, layout.UVFullLen+alignment)
	layout = NewDecoderFrameLayoutForPlanes(64, 64, alignment, y, u, v)
	if !ByteSliceAligned(y[layout.YOrigin:], alignment) {
		t.Fatalf("Y origin %d is not %d-byte aligned", layout.YOrigin, alignment)
	}
	if !ByteSliceAligned(u[layout.UOrigin:], alignment) {
		t.Fatalf("U origin %d is not %d-byte aligned", layout.UOrigin, alignment)
	}
	if !ByteSliceAligned(v[layout.VOrigin:], alignment) {
		t.Fatalf("V origin %d is not %d-byte aligned", layout.VOrigin, alignment)
	}
}

func TestAlignAndPadding(t *testing.T) {
	if got := Align(65, 8); got != 72 {
		t.Fatalf("Align(65, 8) = %d, want 72", got)
	}
	buf := make([]byte, 512)
	off := AlignmentPadding(buf, 64)
	if !ByteSliceAligned(buf[off:], 64) {
		t.Fatalf("AlignmentPadding returned unaligned offset %d", off)
	}
	next := AlignOffsetForSlice(buf, 7, 64)
	if !ByteSliceAligned(buf[next:], 64) {
		t.Fatalf("AlignOffsetForSlice returned unaligned offset %d", next)
	}
}
