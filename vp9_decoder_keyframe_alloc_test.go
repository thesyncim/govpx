package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9DecoderDecodeSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("Decode steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderLoopFilteredKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode loop-filtered keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode loop-filtered keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("loop-filtered keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderThreadedLoopFilteredKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 3})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if d.vp9LoopFilterPool == nil {
		t.Fatal("threaded VP9 decoder did not initialize loop-filter pool")
	}
	if got, want := d.vp9LoopFilterPool.helperCount, int8(2); got != want {
		t.Fatalf("VP9 loop-filter helper count = %d, want %d", got, want)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode threaded loop-filtered keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode threaded loop-filtered keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("threaded loop-filtered keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderSegmentedAltQKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9SegmentedAltQKeyframeForTest(t)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode segmented alt-q keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode segmented alt-q keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("segmented alt-q keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestVP9DecoderDecodeIntoSteadyStateAlloc keeps caller-owned VP9 output
// allocation-free after the decoder and reference slots are warm.
func TestVP9DecoderDecodeIntoSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(96, 96)
	if _, err := d.DecodeInto(packet, &dst); err != nil {
		t.Fatalf("warm DecodeInto err = %v, want nil", err)
	}

	var info VP9FrameInfo
	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		info, err = d.DecodeInto(packet, &dst)
	})
	if err != nil {
		t.Fatalf("DecodeInto err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 || !info.ShowFrame {
		t.Fatalf("DecodeInto info = %+v, want visible 96x96 frame", info)
	}
	if allocs != 0 {
		t.Fatalf("DecodeInto steady state: got %v allocs/op, want 0", allocs)
	}
}
