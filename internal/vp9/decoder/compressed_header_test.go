package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestReadCompressedHeaderLosslessKeyframe writes the minimal
// compressed-header bitstream for a lossless keyframe: TxMode forced
// to Only4x4 (no tx-mode read), one coef-probs skip bit, then three
// skip-probs no-update bits. Confirms the parser consumes exactly
// that and leaves every FrameContext slot untouched.
func TestReadCompressedHeaderLosslessKeyframe(t *testing.T) {
	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	// Coef-probs: a single 0 bit per tx level. Lossless → Only4x4
	// → only one tx level walked.
	w.Write(0, 128)
	// Skip-probs: three "no update" bits.
	for range SkipContexts {
		w.Write(0, DiffUpdateProb)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var fc FrameContext
	// Seed two arbitrary slots so we can detect any unintended write.
	fc.SkipProbs = [SkipContexts]uint8{11, 22, 33}
	fc.IntraInterProb[0] = 55

	out := ReadCompressedHeader(&r, &fc, ReadCompressedHeaderArgs{
		Lossless: true,
		KeyFrame: true,
	})
	if out.TxMode != common.Only4x4 {
		t.Errorf("TxMode = %d, want Only4x4", out.TxMode)
	}
	if fc.SkipProbs != [SkipContexts]uint8{11, 22, 33} {
		t.Errorf("SkipProbs changed: %v", fc.SkipProbs)
	}
	if fc.IntraInterProb[0] != 55 {
		t.Error("IntraInterProb[0] changed despite KeyFrame branch")
	}
}

// TestReadCompressedHeaderInterFrameSelectMode writes a compressed
// header for an inter frame with TxMode = TxModeSelect, no probability
// updates anywhere along the long inter path. Confirms the parser
// walks every fragment without panicking.
func TestReadCompressedHeaderInterFrameSelectMode(t *testing.T) {
	buf := make([]byte, 512)
	var w bitstream.Writer
	w.Start(buf)
	// TxMode select: bits 11 then 1.
	w.Write(1, 128) // bit1
	w.Write(1, 128) // bit0  → Allow32x32 = 3
	w.Write(1, 128) // promote to TxModeSelect
	// TxModeProbs: 12 "no update" bits.
	for range 12 {
		w.Write(0, DiffUpdateProb)
	}
	// CoefProbs: 4 skip bits (Allow32x32 caps at 32x32 = all 4 levels).
	for range 4 {
		w.Write(0, 128)
	}
	// SkipProbs: 3 no-update bits.
	for range SkipContexts {
		w.Write(0, DiffUpdateProb)
	}
	// InterModeProbs: 7 contexts × 3 mode-prob slots = 21 no-updates.
	for range common.InterModeContexts * (common.InterModes - 1) {
		w.Write(0, DiffUpdateProb)
	}
	// SwitchableInterpProbs skipped (InterpFilter != Switchable).
	// IntraInterProbs: 4 no-update bits.
	for range common.IntraInterContexts {
		w.Write(0, DiffUpdateProb)
	}
	// ReferenceMode: compound disallowed → 0 bits read. We pass
	// CompoundRefAllowed=false to skip the 1-or-2-bit prefix and the
	// gating subtables.
	// SingleReference path runs: COMP_INTER skipped, single_ref runs
	// (RefContexts × 2 slots), comp_ref skipped.
	for range common.RefContexts * 2 {
		w.Write(0, DiffUpdateProb)
	}
	// YModeProbs: 4 × (10-1) = 36 no-update bits.
	for range BlockSizeGroups * (common.IntraModes - 1) {
		w.Write(0, DiffUpdateProb)
	}
	// PartitionProbs: 16 × 3 = 48 no-update bits.
	for range common.PartitionContexts * (common.PartitionTypes - 1) {
		w.Write(0, DiffUpdateProb)
	}
	// MvProbs (no_hp): 65 no-update bits.
	for range 65 {
		w.Write(0, MvUpdateProb)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var fc FrameContext
	out := ReadCompressedHeader(&r, &fc, ReadCompressedHeaderArgs{
		KeyFrame:             false,
		IntraOnly:            false,
		InterpFilter:         InterpEighttap,
		AllowHighPrecisionMv: false,
		CompoundRefAllowed:   false,
	})
	if out.TxMode != common.TxModeSelect {
		t.Errorf("TxMode = %d, want TxModeSelect", out.TxMode)
	}
	if out.ReferenceMode != SingleReference {
		t.Errorf("ReferenceMode = %d, want SingleReference", out.ReferenceMode)
	}
}
