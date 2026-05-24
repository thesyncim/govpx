package buffers

import (
	"bytes"
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

func TestEnsureCapacityReusesAndGrows(t *testing.T) {
	buf := make([]byte, 3, 8)
	reused := EnsureCapacity(buf, 6)
	if len(reused) != 6 || cap(reused) != cap(buf) {
		t.Fatalf("EnsureCapacity reused len/cap = %d/%d, want 6/%d",
			len(reused), cap(reused), cap(buf))
	}
	grown := EnsureCapacity(buf, 16)
	if len(grown) != 16 || cap(grown) < 16 {
		t.Fatalf("EnsureCapacity grown len/cap = %d/%d, want >=16",
			len(grown), cap(grown))
	}
}

func TestEnsureAlignedCapacity(t *testing.T) {
	buf := NewAligned(64, 32)
	reused := EnsureAlignedCapacity(buf[:32], 48, 32)
	if len(reused) != 48 || !ByteSliceAligned(reused, 32) {
		t.Fatalf("EnsureAlignedCapacity reused len=%d aligned=%v",
			len(reused), ByteSliceAligned(reused, 32))
	}

	unalignedBase := make([]byte, 96)
	unaligned := unalignedBase[1:33]
	if ByteSliceAligned(unaligned, 32) {
		t.Skip("test allocation happened to make the offset aligned")
	}
	realigned := EnsureAlignedCapacity(unaligned, 32, 32)
	if len(realigned) != 32 || !ByteSliceAligned(realigned, 32) {
		t.Fatalf("EnsureAlignedCapacity realigned len=%d aligned=%v",
			len(realigned), ByteSliceAligned(realigned, 32))
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

func TestFill(t *testing.T) {
	buf := []byte{1, 2, 3, 4}
	Fill(buf, 9)
	if !bytes.Equal(buf, []byte{9, 9, 9, 9}) {
		t.Fatalf("Fill = %v, want all 9", buf)
	}
}

func TestPlaneLen(t *testing.T) {
	tests := []struct {
		name                    string
		stride, rows, rowPixels int
		want                    int
	}{
		{name: "last row has no padding", stride: 16, rows: 4, rowPixels: 13, want: 61},
		{name: "single row", stride: 16, rows: 1, rowPixels: 13, want: 13},
		{name: "empty", stride: 16, rows: 0, rowPixels: 13, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlaneLen(tt.stride, tt.rows, tt.rowPixels)
			if got != tt.want {
				t.Fatalf("PlaneLen(%d, %d, %d) = %d, want %d",
					tt.stride, tt.rows, tt.rowPixels, got, tt.want)
			}
		})
	}
}

func TestChroma420Dimensions(t *testing.T) {
	tests := []struct {
		width, height int
		wantWidth     int
		wantHeight    int
	}{
		{width: 640, height: 360, wantWidth: 320, wantHeight: 180},
		{width: 641, height: 361, wantWidth: 321, wantHeight: 181},
	}
	for _, tt := range tests {
		gotWidth, gotHeight := Chroma420Dimensions(tt.width, tt.height)
		if gotWidth != tt.wantWidth || gotHeight != tt.wantHeight {
			t.Fatalf("Chroma420Dimensions(%d, %d) = %dx%d, want %dx%d",
				tt.width, tt.height, gotWidth, gotHeight, tt.wantWidth, tt.wantHeight)
		}
	}
}

func TestI420PlaneSerialization(t *testing.T) {
	y := []byte{
		1, 2, 3, 99,
		4, 5, 6, 99,
		7, 8, 9, 99,
	}
	u := []byte{
		10, 11, 99,
		12, 13, 99,
	}
	v := []byte{
		20, 21, 99,
		22, 23, 99,
	}
	want := []byte{
		1, 2, 3,
		4, 5, 6,
		7, 8, 9,
		10, 11,
		12, 13,
		20, 21,
		22, 23,
	}

	got := AppendI420Planes(nil, 3, 3, y, 4, u, 3, v, 3)
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendI420Planes = %v, want %v", got, want)
	}

	var buf bytes.Buffer
	if err := WriteI420Planes(&buf, 3, 3, y, 4, u, 3, v, 3); err != nil {
		t.Fatalf("WriteI420Planes returned error: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("WriteI420Planes = %v, want %v", buf.Bytes(), want)
	}
}

func TestI420FrameSize(t *testing.T) {
	got, ok := I420FrameSize(641, 361)
	if !ok {
		t.Fatalf("I420FrameSize returned false")
	}
	const want = 641*361 + 2*321*181
	if got != want {
		t.Fatalf("I420FrameSize(641, 361) = %d, want %d", got, want)
	}
}

func TestI420FrameSizeRejectsInvalidDimensions(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name          string
		width, height int
	}{
		{name: "zero width", width: 0, height: 16},
		{name: "negative height", width: 16, height: -1},
		{name: "luma overflow", width: maxInt, height: 2},
		{name: "chroma overflow", width: maxInt, height: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := I420FrameSize(tt.width, tt.height)
			if ok {
				t.Fatalf("I420FrameSize(%d, %d) returned ok", tt.width, tt.height)
			}
		})
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
