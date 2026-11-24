package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestPeekVP8StreamInfoKeyFrame(t *testing.T) {
	packet := vp8test.KeyFramePacket(320, 240, 17, 0, true)

	info, err := govpx.PeekVP8StreamInfo(packet)
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
	packet := vp8test.InterFramePacket(31, 2, true)

	info, err := govpx.PeekVP8StreamInfo(packet)
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
	packet := vp8test.KeyFramePacket(16, 16, 0, 0, true)
	packet[3] = 0

	_, err := govpx.PeekVP8StreamInfo(packet)
	if !errors.Is(err, govpx.ErrInvalidData) {
		t.Fatalf("error = %v, want ErrInvalidData", err)
	}
}

func TestPeekVP8StreamInfoAllocatesZero(t *testing.T) {
	packet := vp8test.KeyFramePacket(64, 36, 3, 0, true)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = govpx.PeekVP8StreamInfo(packet)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}
