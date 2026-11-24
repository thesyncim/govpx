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

func TestParseSuperframeSplitsFrames(t *testing.T) {
	wantFrames := [][]byte{
		{0x82, 0x49, 0x83},
		{0x04, 0x05, 0x06, 0x07},
		{0x08},
	}
	need, err := SuperframeSize(wantFrames...)
	if err != nil {
		t.Fatalf("SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := PackSuperframeInto(packet, wantFrames...)
	if err != nil {
		t.Fatalf("PackSuperframeInto: %v", err)
	}
	sf, err := ParseSuperframe(packet[:n])
	if err != nil {
		t.Fatalf("ParseSuperframe returned error: %v", err)
	}
	if sf.Count != len(wantFrames) {
		t.Fatalf("superframe count = %d, want %d", sf.Count, len(wantFrames))
	}
	for i := range wantFrames {
		if !bytes.Equal(sf.Frames[i], wantFrames[i]) {
			t.Fatalf("frame %d = %v, want %v", i, sf.Frames[i], wantFrames[i])
		}
	}
}

func TestParseSuperframeRejectsInvalidMarker(t *testing.T) {
	if _, err := ParseSuperframe([]byte{0x01, 0xc0}); !errors.Is(err, vpxerrors.ErrInvalidVP9Data) {
		t.Fatalf("ParseSuperframe err = %v, want ErrInvalidVP9Data", err)
	}
}

func TestParseSuperframeRejectsSizeMismatch(t *testing.T) {
	need, err := SuperframeSize([]byte{0x01}, []byte{0x02})
	if err != nil {
		t.Fatalf("SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := PackSuperframeInto(packet, []byte{0x01}, []byte{0x02})
	if err != nil {
		t.Fatalf("PackSuperframeInto: %v", err)
	}
	packet = packet[:n]
	marker := packet[len(packet)-1]
	indexSize := 2 + (int(marker&0x7)+1)*(int((marker>>3)&0x3)+1)
	indexStart := len(packet) - indexSize
	bad := append([]byte{}, packet[:indexStart]...)
	bad = append(bad, 0xff)
	bad = append(bad, packet[indexStart:]...)

	if _, err := ParseSuperframe(bad); !errors.Is(err, vpxerrors.ErrInvalidVP9Data) {
		t.Fatalf("ParseSuperframe err = %v, want ErrInvalidVP9Data", err)
	}
}
