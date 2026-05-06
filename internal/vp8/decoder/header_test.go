package decoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/gopvx/internal/vp8/common"
)

func TestParseFrameHeaderKeyFrame(t *testing.T) {
	packet := keyFramePacket(320, 240, 1, 2, 17, 0, true)

	header, err := ParseFrameHeader(packet)
	if err != nil {
		t.Fatalf("ParseFrameHeader returned error: %v", err)
	}
	if header.FrameType != common.KeyFrame || !header.KeyFrame() {
		t.Fatalf("FrameType = %d, want keyframe", header.FrameType)
	}
	if !header.ShowFrame {
		t.Fatalf("ShowFrame = false, want true")
	}
	if header.Width != 320 || header.Height != 240 {
		t.Fatalf("dimensions = %dx%d, want 320x240", header.Width, header.Height)
	}
	if header.HorizScale != 1 || header.VertScale != 2 {
		t.Fatalf("scale = %d/%d, want 1/2", header.HorizScale, header.VertScale)
	}
	if header.FirstPartitionSize != 17 {
		t.Fatalf("FirstPartitionSize = %d, want 17", header.FirstPartitionSize)
	}
	if header.HeaderSize != 10 {
		t.Fatalf("HeaderSize = %d, want 10", header.HeaderSize)
	}
}

func TestParseFrameHeaderInterFrame(t *testing.T) {
	packet := interFramePacket(31, 2, true)

	header, err := ParseFrameHeader(packet)
	if err != nil {
		t.Fatalf("ParseFrameHeader returned error: %v", err)
	}
	if header.FrameType != common.InterFrame || header.KeyFrame() {
		t.Fatalf("FrameType = %d, want interframe", header.FrameType)
	}
	if header.Profile != 2 || !header.ShowFrame || header.FirstPartitionSize != 31 {
		t.Fatalf("header = %+v, want profile/show/partition parsed", header)
	}
	if header.Width != 0 || header.Height != 0 || header.HeaderSize != 3 {
		t.Fatalf("interframe dimensions/header size = %dx%d/%d, want 0x0/3", header.Width, header.Height, header.HeaderSize)
	}
}

func TestParseFrameHeaderRejectsTruncated(t *testing.T) {
	_, err := ParseFrameHeader([]byte{0, 1})
	if !errors.Is(err, ErrInvalidFrameHeader) {
		t.Fatalf("short tag error = %v, want ErrInvalidFrameHeader", err)
	}

	_, err = ParseFrameHeader(keyFramePacket(16, 16, 0, 0, 0, 0, true)[:9])
	if !errors.Is(err, ErrInvalidFrameHeader) {
		t.Fatalf("short keyframe error = %v, want ErrInvalidFrameHeader", err)
	}
}

func TestParseFrameHeaderRejectsBadStartCode(t *testing.T) {
	packet := keyFramePacket(16, 16, 0, 0, 0, 0, true)
	packet[3] = 0

	_, err := ParseFrameHeader(packet)
	if !errors.Is(err, ErrInvalidFrameHeader) {
		t.Fatalf("error = %v, want ErrInvalidFrameHeader", err)
	}
}

func TestParseFrameHeaderAllocatesZero(t *testing.T) {
	packet := keyFramePacket(64, 36, 0, 0, 3, 0, true)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = ParseFrameHeader(packet)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func keyFramePacket(width int, height int, horizScale int, vertScale int, firstPartitionSize int, profile int, showFrame bool) []byte {
	packet := make([]byte, 10)
	tag := uint32(profile&7) << 1
	if showFrame {
		tag |= 1 << 4
	}
	tag |= uint32(firstPartitionSize) << 5
	packet[0] = byte(tag)
	packet[1] = byte(tag >> 8)
	packet[2] = byte(tag >> 16)
	packet[3] = KeyFrameStartCode[0]
	packet[4] = KeyFrameStartCode[1]
	packet[5] = KeyFrameStartCode[2]
	widthRaw := uint16(width&0x3fff) | uint16(horizScale&3)<<14
	heightRaw := uint16(height&0x3fff) | uint16(vertScale&3)<<14
	packet[6] = byte(widthRaw)
	packet[7] = byte(widthRaw >> 8)
	packet[8] = byte(heightRaw)
	packet[9] = byte(heightRaw >> 8)
	return packet
}

func interFramePacket(firstPartitionSize int, profile int, showFrame bool) []byte {
	packet := make([]byte, 3)
	tag := uint32(1) | uint32(profile&7)<<1
	if showFrame {
		tag |= 1 << 4
	}
	tag |= uint32(firstPartitionSize) << 5
	packet[0] = byte(tag)
	packet[1] = byte(tag >> 8)
	packet[2] = byte(tag >> 16)
	return packet
}
