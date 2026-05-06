package govpx

import (
	"errors"
	"testing"
)

func TestPeekVP8StreamInfoKeyFrame(t *testing.T) {
	packet := vp8KeyFramePacket(320, 240, 17, 0, true)

	info, err := PeekVP8StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
	}
	if !info.KeyFrame {
		t.Fatalf("KeyFrame = false, want true")
	}
	if !info.ShowFrame {
		t.Fatalf("ShowFrame = false, want true")
	}
	if info.Width != 320 || info.Height != 240 {
		t.Fatalf("dimensions = %dx%d, want 320x240", info.Width, info.Height)
	}
	if info.Profile != 0 {
		t.Fatalf("Profile = %d, want 0", info.Profile)
	}
	if info.FirstPartitionSize != 17 {
		t.Fatalf("FirstPartitionSize = %d, want 17", info.FirstPartitionSize)
	}
}

func TestPeekVP8StreamInfoInterFrame(t *testing.T) {
	packet := vp8InterFramePacket(31, 2, true)

	info, err := PeekVP8StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
	}
	if info.KeyFrame {
		t.Fatalf("KeyFrame = true, want false")
	}
	if !info.ShowFrame {
		t.Fatalf("ShowFrame = false, want true")
	}
	if info.Profile != 2 {
		t.Fatalf("Profile = %d, want 2", info.Profile)
	}
	if info.FirstPartitionSize != 31 {
		t.Fatalf("FirstPartitionSize = %d, want 31", info.FirstPartitionSize)
	}
}

func TestPeekVP8StreamInfoRejectsMalformedKeyFrame(t *testing.T) {
	packet := vp8KeyFramePacket(16, 16, 0, 0, true)
	packet[3] = 0

	_, err := PeekVP8StreamInfo(packet)
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("error = %v, want ErrInvalidData", err)
	}
}

func TestPeekVP8StreamInfoAllocatesZero(t *testing.T) {
	packet := vp8KeyFramePacket(64, 36, 3, 0, true)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = PeekVP8StreamInfo(packet)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func vp8KeyFramePacket(width int, height int, firstPartitionSize int, profile int, showFrame bool) []byte {
	packet := make([]byte, 10)
	tag := uint32(profile&7) << 1
	if showFrame {
		tag |= 1 << 4
	}
	tag |= uint32(firstPartitionSize) << 5
	packet[0] = byte(tag)
	packet[1] = byte(tag >> 8)
	packet[2] = byte(tag >> 16)
	packet[3] = 0x9d
	packet[4] = 0x01
	packet[5] = 0x2a
	packet[6] = byte(width)
	packet[7] = byte(width >> 8)
	packet[8] = byte(height)
	packet[9] = byte(height >> 8)
	return packet
}

func vp8KeyFramePacketWithPayload(width int, height int, firstPartitionSize int, profile int, showFrame bool) []byte {
	packet := vp8KeyFramePacket(width, height, firstPartitionSize, profile, showFrame)
	packet = append(packet, make([]byte, firstPartitionSize)...)
	return append(packet, make([]byte, 10000)...)
}

func vp8InterFramePacket(firstPartitionSize int, profile int, showFrame bool) []byte {
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
