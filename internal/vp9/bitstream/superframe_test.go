package bitstream

import (
	"bytes"
	"errors"
	"testing"

	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

func TestPackSuperframeIndexInto(t *testing.T) {
	frameSizes := []int{1, 256, 3}
	need, err := SuperframeIndexSize(frameSizes)
	if err != nil {
		t.Fatalf("SuperframeIndexSize: %v", err)
	}
	if need != 8 {
		t.Fatalf("SuperframeIndexSize = %d, want 8", need)
	}
	dst := make([]byte, need)
	n, err := PackSuperframeIndexInto(dst, frameSizes)
	if err != nil {
		t.Fatalf("PackSuperframeIndexInto: %v", err)
	}
	if n != need {
		t.Fatalf("n = %d, want %d", n, need)
	}
	want := []byte{0xca, 0x01, 0x00, 0x00, 0x01, 0x03, 0x00, 0xca}
	if !bytes.Equal(dst, want) {
		t.Fatalf("index = % x, want % x", dst, want)
	}
}

func TestPackSuperframeIndexIntoRejectsInvalid(t *testing.T) {
	if _, err := SuperframeIndexSize(nil); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("empty sizes error = %v, want ErrInvalidConfig", err)
	}
	if _, err := SuperframeIndexSize([]int{0}); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("zero size error = %v, want ErrInvalidConfig", err)
	}
	need, err := PackSuperframeIndexInto(nil, []int{1, 2})
	if !errors.Is(err, vpxerrors.ErrBufferTooSmall) {
		t.Fatalf("short dst error = %v, want ErrBufferTooSmall", err)
	}
	if need != 4 {
		t.Fatalf("short dst need = %d, want 4", need)
	}
}
