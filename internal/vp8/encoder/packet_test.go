package encoder

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
)

func TestPutKeyFrameHeaderMatchesDecoder(t *testing.T) {
	packet := make([]byte, KeyFrameUncompressedHdrSize)
	if err := PutFrameTag(packet, true, 2, true, 17); err != nil {
		t.Fatalf("PutFrameTag returned error: %v", err)
	}
	if err := PutKeyFrameExtraHeader(packet[FrameTagSize:], 320, 240, 1, 2); err != nil {
		t.Fatalf("PutKeyFrameExtraHeader returned error: %v", err)
	}

	header, err := vp8dec.ParseFrameHeader(packet)
	if err != nil {
		t.Fatalf("ParseFrameHeader returned error: %v", err)
	}
	if header.FrameType != vp8common.KeyFrame || header.Profile != 2 || !header.ShowFrame || header.FirstPartitionSize != 17 {
		t.Fatalf("frame header = %+v, want keyframe version 2 show partition 17", header)
	}
	if header.Width != 320 || header.Height != 240 || header.HorizScale != 1 || header.VertScale != 2 {
		t.Fatalf("dimensions/scales = %dx%d %d/%d, want 320x240 1/2", header.Width, header.Height, header.HorizScale, header.VertScale)
	}
}

func TestPutInterFrameTagMatchesDecoder(t *testing.T) {
	packet := make([]byte, FrameTagSize)
	if err := PutFrameTag(packet, false, 1, false, 31); err != nil {
		t.Fatalf("PutFrameTag returned error: %v", err)
	}

	header, err := vp8dec.ParseFrameHeader(packet)
	if err != nil {
		t.Fatalf("ParseFrameHeader returned error: %v", err)
	}
	if header.FrameType != vp8common.InterFrame || header.Profile != 1 || header.ShowFrame || header.FirstPartitionSize != 31 {
		t.Fatalf("frame header = %+v, want hidden inter version 1 partition 31", header)
	}
}

func TestPutPartitionSize(t *testing.T) {
	var dst [3]byte
	if err := PutPartitionSize(dst[:], 0x123456); err != nil {
		t.Fatalf("PutPartitionSize returned error: %v", err)
	}
	if dst != [3]byte{0x56, 0x34, 0x12} {
		t.Fatalf("partition size bytes = % x, want 56 34 12", dst)
	}
}

func TestPacketWritersRejectInvalidInput(t *testing.T) {
	if err := PutFrameTag(make([]byte, 2), true, 0, true, 0); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("small frame tag error = %v, want ErrBufferTooSmall", err)
	}
	if err := PutFrameTag(make([]byte, 3), true, 8, true, 0); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid version error = %v, want ErrInvalidPacketConfig", err)
	}
	if err := PutFrameTag(make([]byte, 3), true, 0, true, MaxFirstPartitionSize+1); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid partition error = %v, want ErrInvalidPacketConfig", err)
	}
	if err := PutKeyFrameExtraHeader(make([]byte, 6), 16, 16, 0, 0); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("small keyframe header error = %v, want ErrBufferTooSmall", err)
	}
	if err := PutKeyFrameExtraHeader(make([]byte, 7), 0, 16, 0, 0); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid dimension error = %v, want ErrInvalidPacketConfig", err)
	}
	if err := PutPartitionSize(make([]byte, 3), MaxPartitionSize+1); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid partition size error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestPacketWritersAllocateZero(t *testing.T) {
	packet := make([]byte, KeyFrameUncompressedHdrSize)
	var size [3]byte
	allocs := testing.AllocsPerRun(1000, func() {
		_ = PutFrameTag(packet, true, 0, true, 123)
		_ = PutKeyFrameExtraHeader(packet[FrameTagSize:], 16, 16, 0, 0)
		_ = PutPartitionSize(size[:], 456)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkPutFrameTag(b *testing.B) {
	var packet [FrameTagSize]byte
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = PutFrameTag(packet[:], i&1 == 0, i&7, true, i&MaxFirstPartitionSize)
	}
}
