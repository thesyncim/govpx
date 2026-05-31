package govpx_test

import (
	"bytes"
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

const (
	vp9EncoderKeyframeAllocRunsForTest = 10
	vp9EncoderInterAllocRunsForTest    = 3
)

func TestVP9EncoderTileRowsSteadyStateAlloc(t *testing.T) {
	const width, height = 1024, 128
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frames := [4]*image.YCbCr{}
	for i := range frames {
		frames[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	dst := make([]byte, 1<<20)
	for i := range frames {
		if _, err := e.EncodeInto(frames[i], dst); err != nil {
			t.Fatalf("warm EncodeInto[%d]: %v", i, err)
		}
	}
	idx := 0
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRunsForTest, func() {
		frame := frames[idx&3]
		idx++
		if _, err := e.EncodeInto(frame, dst); err != nil {
			t.Fatalf("EncodeInto tile-row alloc run: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("tile-row EncodeInto steady-state allocs = %f, want 0", allocs)
	}
}

func TestVP9EncoderIVFRoundTrip(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               64,
		Height:              64,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          1,
	}
	stream := append(testutil.WriteIVFHeader(header), testutil.WriteIVFFrame(payload, 0)...)

	gotHdr, err := testutil.ParseIVFHeader(stream)
	if err != nil {
		t.Fatalf("ParseIVFHeader: %v", err)
	}
	if gotHdr.FourCC != header.FourCC {
		t.Errorf("FourCC = %v, want VP90", gotHdr.FourCC)
	}
	if gotHdr.Width != 64 || gotHdr.Height != 64 {
		t.Errorf("ivf size = (%d, %d), want (64, 64)", gotHdr.Width, gotHdr.Height)
	}

	offset, err := testutil.FirstIVFFrameOffset(stream)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	frame, _, err := testutil.NextIVFFrame(stream, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}
	if !bytes.Equal(frame.Data, payload) {
		t.Fatal("recovered IVF payload differs from encoded VP9 payload")
	}

	info, err := govpx.PeekVP9StreamInfo(frame.Data)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo on IVF payload: %v", err)
	}
	if !info.KeyFrame {
		t.Fatal("recovered IVF payload is not a VP9 keyframe")
	}
}
