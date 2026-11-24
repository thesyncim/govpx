package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

const defaultVP9StreamInfoQuantizer = 37

func TestPeekVP9StreamInfoKeyFrame(t *testing.T) {
	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, 80, 128, 128))
	if err != nil {
		t.Fatalf("Encode VP9 keyframe: %v", err)
	}

	info, err := govpx.PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo returned error: %v", err)
	}
	if !info.KeyFrame || !info.ShowFrame {
		t.Fatalf("key/show = %v/%v, want true/true", info.KeyFrame, info.ShowFrame)
	}
	if info.Profile != 0 {
		t.Fatalf("Profile = %d, want 0", info.Profile)
	}
	if info.Width != width || info.Height != height {
		t.Fatalf("dimensions = %dx%d, want %dx%d", info.Width, info.Height, width, height)
	}
	if info.RefreshFrameFlags != 0xff {
		t.Fatalf("RefreshFrameFlags = %#x, want 0xff", info.RefreshFrameFlags)
	}
	if info.Quantizer != defaultVP9StreamInfoQuantizer {
		t.Fatalf("Quantizer = %d, want default qindex %d",
			info.Quantizer, defaultVP9StreamInfoQuantizer)
	}
	if info.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0, want non-zero")
	}
	if !info.TileInfoAvailable || info.TileLog2Cols != 0 || info.TileLog2Rows != 0 {
		t.Fatalf("tile info = available:%v log2:%dx%d, want available 0x0",
			info.TileInfoAvailable, info.TileLog2Cols, info.TileLog2Rows)
	}
	if info.Superframe || info.SuperframeFrames != 1 {
		t.Fatalf("superframe = %v frames=%d, want false/1",
			info.Superframe, info.SuperframeFrames)
	}
}

func TestPeekVP9StreamInfoReportsTileLayout(t *testing.T) {
	const width, height = 1280, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, 80, 128, 128))
	if err != nil {
		t.Fatalf("Encode VP9 tiled keyframe: %v", err)
	}

	info, err := govpx.PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo tiled keyframe: %v", err)
	}
	if !info.TileInfoAvailable || info.TileLog2Cols != 2 || info.TileLog2Rows != 0 {
		t.Fatalf("tile info = available:%v log2:%dx%d, want available 2x0",
			info.TileInfoAvailable, info.TileLog2Cols, info.TileLog2Rows)
	}
}

func TestPeekVP9StreamInfoInterFrameSizeFromReference(t *testing.T) {
	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode VP9 keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode VP9 inter: %v", err)
	}

	info, err := govpx.PeekVP9StreamInfo(inter)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo inter returned error: %v", err)
	}
	if info.KeyFrame {
		t.Fatal("KeyFrame = true, want false")
	}
	if !info.ShowFrame {
		t.Fatal("ShowFrame = false, want true")
	}
	if !info.FrameSizeFromReference || info.FrameSizeReference != 0 {
		t.Fatalf("FrameSizeFromReference/ref = %v/%d, want true/0",
			info.FrameSizeFromReference, info.FrameSizeReference)
	}
	if info.Width != 0 || info.Height != 0 {
		t.Fatalf("inherited dimensions = %dx%d, want unavailable 0x0",
			info.Width, info.Height)
	}
	if info.RefreshFrameFlags != 1 {
		t.Fatalf("RefreshFrameFlags = %#x, want 0x1", info.RefreshFrameFlags)
	}
	if info.FirstPartitionSize != 0 {
		t.Fatalf("FirstPartitionSize = %d, want 0 when dimensions are inherited",
			info.FirstPartitionSize)
	}
}

func TestPeekVP9StreamInfoSuperframeReportsFirstFrame(t *testing.T) {
	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 112, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode VP9 keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode VP9 inter: %v", err)
	}

	info, err := govpx.PeekVP9StreamInfo(vp9test.SuperframePacket(t, key, inter))
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo superframe returned error: %v", err)
	}
	if !info.Superframe || info.SuperframeFrames != 2 {
		t.Fatalf("superframe = %v frames=%d, want true/2",
			info.Superframe, info.SuperframeFrames)
	}
	if !info.KeyFrame || info.Width != width || info.Height != height {
		t.Fatalf("first frame key/dims = %v %dx%d, want key %dx%d",
			info.KeyFrame, info.Width, info.Height, width, height)
	}
}

func TestPeekVP9StreamInfoRejectsMalformed(t *testing.T) {
	_, err := govpx.PeekVP9StreamInfo([]byte{0x00})
	if !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Fatalf("error = %v, want ErrInvalidVP9Data", err)
	}
}

func TestPeekVP9StreamInfoAllocatesZero(t *testing.T) {
	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, 120, 128, 128))
	if err != nil {
		t.Fatalf("Encode VP9 keyframe: %v", err)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = govpx.PeekVP9StreamInfo(packet)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}
