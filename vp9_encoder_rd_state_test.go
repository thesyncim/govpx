package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9EncoderInitializeRDConstsPopulatesPerFrameState validates that
// vp9_initialize_rd_consts populates rc.rdmult / rc.rddiv and clears
// cbRdmult.  Asserting the populated values lets the inter mode picker
// rely on the per-frame state instead of synthesising rdmult on every
// candidate score, which is the load-bearing wiring step.
func TestVP9EncoderInitializeRDConstsPopulatesPerFrameState(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	e.cbRdmult = 12345 // simulate stale per-SB cache from prior frame.
	e.vp9EncoderInitializeRDConsts(64, encoder.RDFrameInter, 4)
	if e.rc.rddiv != encoder.RDDivBits {
		t.Fatalf("rc.rddiv = %d, want %d", e.rc.rddiv, encoder.RDDivBits)
	}
	want := encoder.ComputeRDMult(64, encoder.RDFrameInter)
	if e.rc.rdmult != want {
		t.Fatalf("rc.rdmult = %d, want %d", e.rc.rdmult, want)
	}
	if e.cbRdmult != 0 {
		t.Fatalf("cbRdmult = %d, want 0 after frame init", e.cbRdmult)
	}
}

// TestVP9EncoderRDMultLookupPrecedence validates that the per-block
// scorer reads cb_rdmult > rc.rdmult > qindex-derived fallback, in that
// order — mirroring libvpx's MACROBLOCK::rdmult precedence.
func TestVP9EncoderRDMultLookupPrecedence(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	// Fallback path.
	want := encoder.ComputeRDMultBasedOnQindex(80, encoder.RDFrameInter)
	if got := e.activeRDMult(80); got != want {
		t.Fatalf("fallback activeRDMult = %d, want %d", got, want)
	}
	// rc.rdmult overrides the fallback.
	e.rc.rdmult = 9876
	if got := e.activeRDMult(80); got != 9876 {
		t.Fatalf("rc.rdmult override = %d, want 9876", got)
	}
	// cbRdmult overrides rc.rdmult.
	e.cbRdmult = 4321
	if got := e.activeRDMult(80); got != 4321 {
		t.Fatalf("cbRdmult override = %d, want 4321", got)
	}
	// Zero cbRdmult falls back to rc.rdmult.
	e.cbRdmult = 0
	if got := e.activeRDMult(80); got != 9876 {
		t.Fatalf("zero cbRdmult fallback = %d, want 9876", got)
	}
}
