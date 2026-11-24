package govpx

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9PrepareCyclicRefreshFrameUsesRefreshFlagsForRDMult(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  600,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	miRows, miCols := 8, 8
	mbs := vp9enc.MacroblockCount(miRows, miCols)
	header := &vp9dec.UncompressedHeader{}
	header.Quant.BaseQindex = 80

	refreshFlags := uint8(1 << vp9GoldenRefSlot)
	e.vp9UpdateCyclicRefreshParameters(false, false, true, miRows, miCols, mbs, refreshFlags, false)
	e.vp9PrepareCyclicRefreshFrame(false, false, true, miRows, miCols, mbs, header, false, refreshFlags)

	if !e.cyclicAQ.Apply {
		t.Fatal("cyclic refresh did not apply on inter frame")
	}
	q2 := int(header.Quant.BaseQindex) + e.cyclicAQ.QIndexDelta[vp9enc.CyclicRefreshSegmentBoost2]
	want := vp9enc.ComputeRDMult(q2, vp9enc.RDFrameTypeFor(false, false, true, false))
	if e.cyclicAQ.RDMult != want {
		t.Fatalf("RDMult = %d, want %d for golden-refresh frame type", e.cyclicAQ.RDMult, want)
	}
}
