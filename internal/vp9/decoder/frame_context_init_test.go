package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestResetFrameContextSeedsAllFields covers the cumulative seed:
// every prob slot lines up with the matching libvpx default table.
func TestResetFrameContextSeedsAllFields(t *testing.T) {
	var fc FrameContext
	ResetFrameContext(&fc)

	if fc.YModeProb != tables.DefaultIfYProbs {
		t.Errorf("YModeProb mismatch")
	}
	if fc.UvModeProb != tables.DefaultIfUvProbs {
		t.Errorf("UvModeProb mismatch")
	}
	if fc.SwitchableInterpProb != tables.DefaultSwitchableInterpProb {
		t.Errorf("SwitchableInterpProb mismatch")
	}
	if fc.PartitionProb != tables.DefaultPartitionProbs {
		t.Errorf("PartitionProb mismatch")
	}
	if fc.IntraInterProb != tables.DefaultIntraInter {
		t.Errorf("IntraInterProb mismatch")
	}
	if fc.ReferenceModeProbs.CompInterProb != tables.DefaultCompInter {
		t.Errorf("CompInterProb mismatch")
	}
	if fc.ReferenceModeProbs.CompRefProb != tables.DefaultCompRef {
		t.Errorf("CompRefProb mismatch")
	}
	if fc.ReferenceModeProbs.SingleRefProb != tables.DefaultSingleRef {
		t.Errorf("SingleRefProb mismatch")
	}
	if fc.TxProbs.P32x32 != tables.DefaultTxProbsP32x32 {
		t.Errorf("TxProbs.P32x32 mismatch")
	}
	if fc.SkipProbs != tables.DefaultSkipProbs {
		t.Errorf("SkipProbs mismatch")
	}
	if fc.InterModeProbs != tables.DefaultInterModeProbs {
		t.Errorf("InterModeProbs mismatch")
	}
	if fc.CoefProbs[0] != tables.DefaultCoefProbs4x4 {
		t.Errorf("CoefProbs[Tx4x4] mismatch")
	}
	if fc.CoefProbs[3] != tables.DefaultCoefProbs32x32 {
		t.Errorf("CoefProbs[Tx32x32] mismatch")
	}
	if fc.Nmvc.Joints != tables.DefaultNmvJoints {
		t.Errorf("Nmvc.Joints mismatch")
	}
	for i := 0; i < 2; i++ {
		src := &tables.DefaultNmvComps[i]
		got := &fc.Nmvc.Comps[i]
		if got.Sign != src.Sign || got.Classes != src.Classes ||
			got.Class0 != src.Class0 || got.Bits != src.Bits ||
			got.Class0Fp != src.Class0Fp || got.Fp != src.Fp ||
			got.Class0Hp != src.Class0Hp || got.Hp != src.Hp {
			t.Errorf("Nmvc.Comps[%d] diverged from DefaultNmvComps[%d]", i, i)
		}
	}
}

// TestResetFrameContextIsIdempotent: calling ResetFrameContext on a
// FrameContext that the prob-update path has mutated puts it back
// at the libvpx defaults byte-for-byte. This is the post-mid-frame
// guarantee: the encoder can run a counts-driven update, then the
// decoder's next keyframe reset puts both back in sync.
func TestResetFrameContextIsIdempotent(t *testing.T) {
	var fc FrameContext
	ResetFrameContext(&fc)
	seed := fc

	// Mutate the prob slots.
	fc.SkipProbs[0] = 1
	fc.InterModeProbs[0][0] = 1
	fc.PartitionProb[0][0] = 1
	fc.Nmvc.Joints[0] = 1
	fc.CoefProbs[0][0][0][0][0][0] = 1

	if fc == seed {
		t.Fatal("mutation didn't take")
	}

	ResetFrameContext(&fc)
	if fc != seed {
		t.Errorf("ResetFrameContext didn't restore the libvpx defaults")
	}
}
