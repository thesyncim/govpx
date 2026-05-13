package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestCompressedHeaderRoundTripKeyframeAllow32x32: emit the
// no-update compressed header for a TxMode=Allow32x32 keyframe and
// re-parse it through ReadCompressedHeader. The fc tables should
// stay byte-identical to the seed (DefaultIntraInter and friends
// are kept intact because every "update?" bit is 0).
func TestCompressedHeaderRoundTripKeyframeAllow32x32(t *testing.T) {
	dst := make([]byte, 4096)
	in := CompressedHeaderInputs{
		Lossless:      false,
		TxMode:        common.Allow32x32,
		IntraOnly:     true, // keyframe path → intra-only treated true.
		InterpFilter:  vp9dec.InterpEighttap,
		ReferenceMode: vp9dec.SingleReference,
	}
	n, err := WriteCompressedHeaderNoUpdate(dst, in)
	if err != nil {
		t.Fatalf("WriteCompressedHeaderNoUpdate: %v", err)
	}
	if n <= 0 {
		t.Fatalf("returned %d bytes", n)
	}

	var fc vp9dec.FrameContext
	// Seed FrameContext fields the parser walks.
	seedFC(&fc)
	pre := fc

	var br bitstream.Reader
	if err := br.Init(dst[:n]); err != nil {
		t.Fatalf("Reader.Init: %v", err)
	}
	out := vp9dec.ReadCompressedHeader(&br, &fc, vp9dec.ReadCompressedHeaderArgs{
		KeyFrame:     true,
		IntraOnly:    true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Allow32x32 {
		t.Errorf("TxMode = %d, want Allow32x32", out.TxMode)
	}
	if br.HasError() {
		t.Fatal("compressed header reader reported over-read")
	}
	// Every probability table should round-trip identical because
	// no "update?" bit was set.
	for ctx := range skipContexts {
		if fc.SkipProbs[ctx] != pre.SkipProbs[ctx] {
			t.Errorf("SkipProbs[%d] = %d, want %d (no-update)", ctx,
				fc.SkipProbs[ctx], pre.SkipProbs[ctx])
		}
	}
}

// TestCompressedHeaderRoundTripInterFrame exercises the inter-frame
// gates: comp_inter / single_ref / comp_ref blocks land based on the
// frame ReferenceMode, plus MV probs at the end.
func TestCompressedHeaderRoundTripInterFrame(t *testing.T) {
	dst := make([]byte, 4096)
	in := CompressedHeaderInputs{
		Lossless:             false,
		TxMode:               common.Allow16x16,
		IntraOnly:            false,
		InterpFilter:         vp9dec.InterpSwitchable,
		ReferenceMode:        vp9dec.ReferenceModeSelect,
		CompoundRefAllowed:   true,
		AllowHighPrecisionMv: true,
	}
	n, err := WriteCompressedHeaderNoUpdate(dst, in)
	if err != nil {
		t.Fatalf("WriteCompressedHeaderNoUpdate: %v", err)
	}

	var fc vp9dec.FrameContext
	seedFC(&fc)

	var br bitstream.Reader
	if err := br.Init(dst[:n]); err != nil {
		t.Fatalf("Reader.Init: %v", err)
	}
	out := vp9dec.ReadCompressedHeader(&br, &fc, vp9dec.ReadCompressedHeaderArgs{
		IntraOnly:            false,
		InterpFilter:         vp9dec.InterpSwitchable,
		AllowHighPrecisionMv: true,
		CompoundRefAllowed:   true,
	})
	if out.TxMode != common.Allow16x16 {
		t.Errorf("TxMode = %d, want Allow16x16", out.TxMode)
	}
	if out.ReferenceMode != vp9dec.ReferenceModeSelect {
		t.Errorf("ReferenceMode = %d, want ReferenceModeSelect", out.ReferenceMode)
	}
}

// seedFC seeds the FrameContext fields the parser consults with
// libvpx's default-tables seed, mirroring the past-independent
// state setup_past_independent_state would emit.
func seedFC(fc *vp9dec.FrameContext) {
	fc.SkipProbs = [3]uint8{192, 128, 64}
	for i := range fc.IntraInterProb {
		fc.IntraInterProb[i] = tables.DefaultIntraInter[i]
	}
	for i := range fc.InterModeProbs {
		for j := range fc.InterModeProbs[i] {
			fc.InterModeProbs[i][j] = tables.DefaultInterModeProbs[i][j]
		}
	}
	for i := range fc.SwitchableInterpProb {
		for j := range fc.SwitchableInterpProb[i] {
			fc.SwitchableInterpProb[i][j] = tables.DefaultSwitchableInterpProb[i][j]
		}
	}
	for i := range fc.PartitionProb {
		for j := range fc.PartitionProb[i] {
			fc.PartitionProb[i][j] = tables.DefaultPartitionProbs[i][j]
		}
	}
	for i := range fc.YModeProb {
		for j := range fc.YModeProb[i] {
			fc.YModeProb[i][j] = tables.DefaultIfYProbs[i][j]
		}
	}
	// Seed Nmvc joints + components from canonical defaults so the
	// MV-prob update walk has something to read against.
	copy(fc.Nmvc.Joints[:], tables.DefaultNmvJoints[:])
	for i := range 2 {
		src := &tables.DefaultNmvComps[i]
		dst := &fc.Nmvc.Comps[i]
		dst.Sign = src.Sign
		copy(dst.Classes[:], src.Classes[:])
		copy(dst.Class0[:], src.Class0[:])
		copy(dst.Bits[:], src.Bits[:])
		for j := range vp9dec.Class0Size {
			copy(dst.Class0Fp[j][:], src.Class0Fp[j][:])
		}
		copy(dst.Fp[:], src.Fp[:])
		dst.Class0Hp = src.Class0Hp
		dst.Hp = src.Hp
	}
}
