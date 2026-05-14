package encoder

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestEncodeLoopfilterWithPrevPartialUpdate plants the new
// RefDeltas / ModeDeltas differing from prev in just a few slots;
// only those slots should emit a "changed=1" + value pair. The
// decoder side's ReadLoopfilter then leaves the unchanged slots at
// their incoming values.
func TestEncodeLoopfilterWithPrevPartialUpdate(t *testing.T) {
	prevRef := [vp9dec.MaxRefLfDeltas]int8{1, 0, -1, -1}
	prevMode := [vp9dec.MaxModeLfDeltas]int8{0, 0}
	lf := &vp9dec.LoopfilterParams{
		FilterLevel:         32,
		SharpnessLevel:      4,
		ModeRefDeltaEnabled: true,
		ModeRefDeltaUpdate:  true,
		RefDeltas:           [vp9dec.MaxRefLfDeltas]int8{1, 5, -1, -1}, // only slot 1 changed
		ModeDeltas:          [vp9dec.MaxModeLfDeltas]int8{0, 3},        // only slot 1 changed
	}

	buf := make([]byte, 16)
	w := NewBitWriter(buf)
	EncodeLoopfilterWithPrev(w, lf, &prevRef, &prevMode)
	size := w.BytesWritten()

	// Re-parse via ReadLoopfilter — the decoder side leaves unchanged
	// slots at their incoming value. Seed `decoded` with prev so the
	// round-trip behavior is testable.
	var r vp9dec.BitReader
	r.Init(buf[:size])
	decoded := &vp9dec.LoopfilterParams{
		RefDeltas:  prevRef,
		ModeDeltas: prevMode,
	}
	vp9dec.ReadLoopfilter(&r, decoded)

	if decoded.FilterLevel != lf.FilterLevel {
		t.Errorf("FilterLevel = %d, want %d", decoded.FilterLevel, lf.FilterLevel)
	}
	if decoded.SharpnessLevel != lf.SharpnessLevel {
		t.Errorf("SharpnessLevel = %d, want %d", decoded.SharpnessLevel, lf.SharpnessLevel)
	}
	if decoded.RefDeltas != lf.RefDeltas {
		t.Errorf("RefDeltas = %v, want %v", decoded.RefDeltas, lf.RefDeltas)
	}
	if decoded.ModeDeltas != lf.ModeDeltas {
		t.Errorf("ModeDeltas = %v, want %v", decoded.ModeDeltas, lf.ModeDeltas)
	}
}

// TestEncodeLoopfilterWithPrevNoChanges: all slots match prev → the
// wire fragment carries 0 bits in every per-slot position. Decoder
// side leaves everything at prev.
func TestEncodeLoopfilterWithPrevNoChanges(t *testing.T) {
	prevRef := [vp9dec.MaxRefLfDeltas]int8{1, 0, -1, -1}
	prevMode := [vp9dec.MaxModeLfDeltas]int8{0, 0}
	lf := &vp9dec.LoopfilterParams{
		FilterLevel:         16,
		SharpnessLevel:      0,
		ModeRefDeltaEnabled: true,
		ModeRefDeltaUpdate:  true,
		RefDeltas:           prevRef,
		ModeDeltas:          prevMode,
	}

	buf := make([]byte, 16)
	w := NewBitWriter(buf)
	startBits := w.BitsWritten()
	EncodeLoopfilterWithPrev(w, lf, &prevRef, &prevMode)
	wireBits := w.BitsWritten() - startBits

	// 6 filter_level + 3 sharpness + 1 enabled + 1 update +
	// 4 ref + 2 mode change bits = 17 bits. No per-slot signed value
	// emit since every slot is unchanged.
	if wireBits != 17 {
		t.Errorf("wire bits = %d, want 17 (all-unchanged path)", wireBits)
	}

	size := w.BytesWritten()
	var r vp9dec.BitReader
	r.Init(buf[:size])
	decoded := &vp9dec.LoopfilterParams{
		RefDeltas:  prevRef,
		ModeDeltas: prevMode,
	}
	vp9dec.ReadLoopfilter(&r, decoded)
	if decoded.RefDeltas != prevRef {
		t.Errorf("RefDeltas changed: %v != %v", decoded.RefDeltas, prevRef)
	}
	if decoded.ModeDeltas != prevMode {
		t.Errorf("ModeDeltas changed: %v != %v", decoded.ModeDeltas, prevMode)
	}
}

// TestEncodeLoopfilterDelegateNilPrev: the nil-prev path keeps the
// explicit "always emit changed=1 + value" behavior for callers that
// intentionally do not have a previous delta snapshot.
func TestEncodeLoopfilterDelegateNilPrev(t *testing.T) {
	lf := &vp9dec.LoopfilterParams{
		FilterLevel:         24,
		SharpnessLevel:      2,
		ModeRefDeltaEnabled: true,
		ModeRefDeltaUpdate:  true,
		RefDeltas:           [vp9dec.MaxRefLfDeltas]int8{1, 0, -1, -1},
		ModeDeltas:          [vp9dec.MaxModeLfDeltas]int8{0, 0},
	}
	buf := make([]byte, 16)
	w := NewBitWriter(buf)
	EncodeLoopfilterWithPrev(w, lf, nil, nil)
	size := w.BytesWritten()

	var r vp9dec.BitReader
	r.Init(buf[:size])
	decoded := &vp9dec.LoopfilterParams{}
	vp9dec.ReadLoopfilter(&r, decoded)
	if decoded.RefDeltas != lf.RefDeltas {
		t.Errorf("RefDeltas = %v, want %v", decoded.RefDeltas, lf.RefDeltas)
	}
}

func TestEncodeLoopfilterResetUsesZeroLastDeltas(t *testing.T) {
	lf := &vp9dec.LoopfilterParams{
		FilterLevel:         24,
		SharpnessLevel:      0,
		ModeRefDeltaEnabled: true,
		ModeRefDeltaUpdate:  true,
		RefDeltas:           [vp9dec.MaxRefLfDeltas]int8{1, 0, -1, -1},
		ModeDeltas:          [vp9dec.MaxModeLfDeltas]int8{0, 0},
	}

	buf := make([]byte, 16)
	w := NewBitWriter(buf)
	startBits := w.BitsWritten()
	encodeLoopfilter(w, lf)
	wireBits := w.BitsWritten() - startBits

	// 6 filter_level + 3 sharpness + 1 enabled + 1 update +
	// 4 ref-change bits + 2 mode-change bits + 3 non-zero signed ref deltas.
	if wireBits != 38 {
		t.Errorf("wire bits = %d, want 38 (libvpx reset-delta path)", wireBits)
	}

	var r vp9dec.BitReader
	r.Init(buf[:w.BytesWritten()])
	decoded := &vp9dec.LoopfilterParams{}
	vp9dec.ReadLoopfilter(&r, decoded)
	if decoded.RefDeltas != lf.RefDeltas {
		t.Errorf("RefDeltas = %v, want %v", decoded.RefDeltas, lf.RefDeltas)
	}
	if decoded.ModeDeltas != lf.ModeDeltas {
		t.Errorf("ModeDeltas = %v, want %v", decoded.ModeDeltas, lf.ModeDeltas)
	}
}
