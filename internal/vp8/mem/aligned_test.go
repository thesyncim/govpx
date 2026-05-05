package mem

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
