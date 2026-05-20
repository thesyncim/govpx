package buffers

import (
	"testing"
	"unsafe"
)

func TestNewAligned(t *testing.T) {
	for _, align := range []int{1, 2, 8, 16, 32, 64, 63} {
		buf := NewAligned(257, align)
		if len(buf) != 257 {
			t.Fatalf("len(NewAligned(257, %d)) = %d, want 257", align, len(buf))
		}
		effectiveAlign := align
		if effectiveAlign&(effectiveAlign-1) != 0 {
			effectiveAlign = nextPowerOfTwo(effectiveAlign)
		}
		ptr := uintptr(unsafe.Pointer(&buf[0]))
		if ptr%uintptr(effectiveAlign) != 0 {
			t.Fatalf("ptr %% align = %d for align %d", ptr%uintptr(effectiveAlign), align)
		}
	}
}

func TestNewAlignedZeroSize(t *testing.T) {
	if NewAligned(0, 32) != nil {
		t.Fatalf("NewAligned(0, 32) returned non-nil slice")
	}
}

func TestAlign(t *testing.T) {
	if got := Align(65, 8); got != 72 {
		t.Fatalf("Align(65, 8) = %d, want 72", got)
	}
	if got := Align(5, 1); got != 5 {
		t.Fatalf("Align(5, 1) = %d, want 5", got)
	}
	if got := RoundUp(65, 10); got != 70 {
		t.Fatalf("RoundUp(65, 10) = %d, want 70", got)
	}
	if got := RoundUp(70, 10); got != 70 {
		t.Fatalf("RoundUp(70, 10) = %d, want 70", got)
	}
}

func TestSliceAlignmentHelpers(t *testing.T) {
	buf := make([]byte, 256)
	off := AlignmentPadding(buf, 64)
	if !ByteSliceAligned(buf[off:], 64) {
		t.Fatalf("AlignmentPadding returned unaligned offset %d", off)
	}
	next := AlignOffsetForSlice(buf, 7, 64)
	if !ByteSliceAligned(buf[next:], 64) {
		t.Fatalf("AlignOffsetForSlice returned unaligned offset %d", next)
	}
	if !ByteSliceAligned(nil, 64) {
		t.Fatalf("nil slice should be treated as aligned")
	}
}
