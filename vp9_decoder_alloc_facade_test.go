package govpx_test

import (
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP9DecoderDecodeSteadyStateAlloc(t *testing.T) {
	packet := vp9EncodedKeyframeForTest(t, 96, 96, 128)

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRunsForTest, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("Decode steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderDecodeIntoSteadyStateAlloc(t *testing.T) {
	packet := vp9EncodedKeyframeForTest(t, 96, 96, 128)

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newVP9TestImageForTest(96, 96)
	if _, err := d.DecodeInto(packet, &dst); err != nil {
		t.Fatalf("warm DecodeInto err = %v, want nil", err)
	}

	var info govpx.VP9FrameInfo
	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRunsForTest, func() {
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
