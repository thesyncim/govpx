package buffers

import (
	"errors"
	"testing"
	"unsafe"

	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
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

func TestI420EncodeBufferSize(t *testing.T) {
	got, err := I420EncodeBufferSize(64, 32, 4096, 65536)
	if err != nil {
		t.Fatalf("I420EncodeBufferSize returned error: %v", err)
	}
	if got != 65536 {
		t.Fatalf("small frame size = %d, want min size 65536", got)
	}

	got, err = I420EncodeBufferSize(640, 360, 4096, 65536)
	if err != nil {
		t.Fatalf("I420EncodeBufferSize returned error: %v", err)
	}
	const raw420 = 640*360 + 2*320*180
	want := 4096 + raw420*4
	if got != want {
		t.Fatalf("640x360 size = %d, want %d", got, want)
	}
}

func TestI420EncodeBufferSizeRejectsInvalidConfig(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name                      string
		width, height             int
		headerSlack, minimumBytes int
	}{
		{name: "zero width", width: 0, height: 16, headerSlack: 4096, minimumBytes: 65536},
		{name: "negative slack", width: 16, height: 16, headerSlack: -1, minimumBytes: 65536},
		{name: "negative minimum", width: 16, height: 16, headerSlack: 4096, minimumBytes: -1},
		{name: "luma overflow", width: maxInt, height: 2, headerSlack: 4096, minimumBytes: 65536},
		{name: "output overflow", width: maxInt / 8, height: 4, headerSlack: 4096, minimumBytes: 65536},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := I420EncodeBufferSize(tt.width, tt.height,
				tt.headerSlack, tt.minimumBytes)
			if !errors.Is(err, vpxerrors.ErrInvalidConfig) {
				t.Fatalf("I420EncodeBufferSize error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}
