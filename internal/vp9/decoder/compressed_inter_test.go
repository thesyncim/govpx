package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

func TestReadFrameReferenceModeNotAllowed(t *testing.T) {
	// When compound is disallowed the function reads no bits.
	var r bitstream.Reader
	// Init needs at least one byte; a zero byte fails the marker check
	// but we never call Init when compoundAllowed=false. The function
	// signature lets us pass a zero-valued reader.
	if got := ReadFrameReferenceMode(&r, false); got != SingleReference {
		t.Errorf("got %d, want SingleReference", got)
	}
}

func TestReadFrameReferenceModeAllowed(t *testing.T) {
	cases := []struct {
		name string
		bits []uint32
		want ReferenceMode
	}{
		{"single", []uint32{0}, SingleReference},
		{"compound", []uint32{1, 0}, CompoundReference},
		{"select", []uint32{1, 1}, ReferenceModeSelect},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := make([]byte, 16)
			var w bitstream.Writer
			w.Start(buf)
			for _, b := range c.bits {
				w.Write(b, 128)
			}
			size, err := w.Stop()
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}
			var r bitstream.Reader
			if err := r.Init(buf[:size]); err != nil {
				t.Fatalf("Init: %v", err)
			}
			if got := ReadFrameReferenceMode(&r, true); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestUpdateMvProbsNoUpdates(t *testing.T) {
	buf := make([]byte, 32)
	var w bitstream.Writer
	w.Start(buf)
	// Five "no update" bits.
	for range 5 {
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
	probs := []uint8{10, 20, 30, 40, 50}
	want := append([]uint8(nil), probs...)
	UpdateMvProbs(&r, probs)
	for i := range probs {
		if probs[i] != want[i] {
			t.Errorf("probs[%d] = %d, want %d", i, probs[i], want[i])
		}
	}
}

func TestUpdateMvProbsWithUpdate(t *testing.T) {
	buf := make([]byte, 32)
	var w bitstream.Writer
	w.Start(buf)
	// Slot 0: no update. Slot 1: update with literal 0x5A (= 90).
	w.Write(0, MvUpdateProb)
	w.Write(1, MvUpdateProb)
	const literal = 0x5A
	for b := 6; b >= 0; b-- {
		w.Write(uint32((literal>>uint(b))&1), 128)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	probs := []uint8{77, 88}
	UpdateMvProbs(&r, probs)
	if probs[0] != 77 {
		t.Errorf("probs[0] = %d, want 77 (preserved)", probs[0])
	}
	wantSlot1 := uint8((literal << 1) | 1)
	if probs[1] != wantSlot1 {
		t.Errorf("probs[1] = %d, want %d", probs[1], wantSlot1)
	}
}
