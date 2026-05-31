package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestVP9DecoderFrameContextSlotsTrackInterHeaderUpdates(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}

	packet, wantSkipProb := vp9InterFrameContextUpdatePacketForTest(t, 64, 64, 1, true)
	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if got := d.frameContexts[1].SkipProbs[0]; got != wantSkipProb {
		t.Fatalf("context 1 skip prob = %d, want %d", got, wantSkipProb)
	}
	if got := d.frameContexts[0].SkipProbs[0]; got != tables.DefaultSkipProbs[0] {
		t.Fatalf("context 0 skip prob = %d, want default %d",
			got, tables.DefaultSkipProbs[0])
	}
}

// TestVP9DecoderFrameContextNoRefreshDoesNotPersistUpdates covers the
// refresh_frame_context gate: compressed-header updates are still used
// for the current frame parse, but they must not become the stored slot
// state when the header clears the refresh bit.
func TestVP9DecoderFrameContextNoRefreshDoesNotPersistUpdates(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}

	packet, wantSkipProb := vp9InterFrameContextUpdatePacketForTest(t, 64, 64, 2, false)
	if wantSkipProb == tables.DefaultSkipProbs[0] {
		t.Fatalf("test packet did not update skip prob away from default %d", wantSkipProb)
	}
	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if got := d.frameContexts[2].SkipProbs[0]; got != tables.DefaultSkipProbs[0] {
		t.Fatalf("context 2 skip prob = %d, want default %d",
			got, tables.DefaultSkipProbs[0])
	}
}

func TestVP9DecoderResetClearsFrameContextSlots(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	packet, wantSkipProb := vp9InterFrameContextUpdatePacketForTest(t, 64, 64, 3, true)
	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if got := d.frameContexts[3].SkipProbs[0]; got != wantSkipProb {
		t.Fatalf("context 3 skip prob = %d, want %d", got, wantSkipProb)
	}

	d.Reset()
	for i := range d.frameContexts {
		if got := d.frameContexts[i].SkipProbs[0]; got != tables.DefaultSkipProbs[0] {
			t.Fatalf("context %d skip prob after Reset = %d, want default %d",
				i, got, tables.DefaultSkipProbs[0])
		}
	}
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatal("LastFrameInfo after Reset returned ok")
	}
}
