package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderEncodeShowExistingFrameInto(t *testing.T) {
	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 91, 143, 37)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}

	dst := make([]byte, 1)
	n, err := e.EncodeShowExistingFrameInto(dst, 5)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrameInto: %v", err)
	}
	if n != 1 {
		t.Fatalf("EncodeShowExistingFrameInto wrote %d bytes, want 1", n)
	}
	packet := dst[:n]

	info, err := govpx.PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("govpx.PeekVP9StreamInfo: %v", err)
	}
	if !info.ShowExistingFrame || info.ExistingFrameSlot != 5 ||
		!info.ShowFrame || info.KeyFrame || info.FirstPartitionSize != 0 {
		t.Fatalf("show-existing stream info = %+v, want visible slot 5 packet", info)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("govpx.NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if err := d.DecodeWithPTS(packet, 77); err != nil {
		t.Fatalf("Decode show-existing: %v", err)
	}
	last, ok := d.LastFrameInfo()
	if !ok || !last.ShowExistingFrame || last.ExistingFrameSlot != 5 ||
		!last.ShowFrame || last.PTS != 77 {
		t.Fatalf("LastFrameInfo after show-existing = %+v ok=%t, want slot 5 PTS 77",
			last, ok)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after show-existing")
	}
	assertVP9FilledFrameWithinForTest(t, frame, width, height, 91, 143, 37, 1)
}

func TestVP9EncoderEncodeShowExistingFrameRejectsInvalidState(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	dst := make([]byte, 1)
	if _, err := e.EncodeShowExistingFrameInto(dst, 0); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("EncodeShowExistingFrameInto before refs error = %v, want govpx.ErrInvalidConfig", err)
	}
	if _, err := e.Encode(vp9test.NewYCbCr(64, 64, 128, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if _, err := e.EncodeShowExistingFrameInto(nil, 0); !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("EncodeShowExistingFrameInto nil dst error = %v, want govpx.ErrBufferTooSmall", err)
	}
	if _, err := e.EncodeShowExistingFrameInto(dst, uint8(8)); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("EncodeShowExistingFrameInto bad slot error = %v, want govpx.ErrInvalidConfig", err)
	}
}

func TestVP9EncoderEncodeShowExistingFrameIntoSteadyStateAlloc(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	if _, err := e.Encode(vp9test.NewYCbCr(64, 64, 128, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	dst := make([]byte, 1)

	var n int
	var err error
	allocs := testing.AllocsPerRun(vp9EncoderKeyframeAllocRunsForTest, func() {
		n, err = e.EncodeShowExistingFrameInto(dst, 5)
	})
	if err != nil {
		t.Fatalf("EncodeShowExistingFrameInto: %v", err)
	}
	if n != 1 {
		t.Fatalf("EncodeShowExistingFrameInto wrote %d bytes, want 1", n)
	}
	if allocs != 0 {
		t.Fatalf("EncodeShowExistingFrameInto steady state: got %v allocs/op, want 0", allocs)
	}
}
