package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestReadTxModeRoundTrip writes each of the five TxMode values via
// the boolean-coder encoding libvpx's read_tx_mode expects and
// confirms the decoder recovers the original.
func TestReadTxModeRoundTrip(t *testing.T) {
	cases := []common.TxMode{
		common.Only4x4,
		common.Allow8x8,
		common.Allow16x16,
		common.Allow32x32,
		common.TxModeSelect,
	}
	for _, want := range cases {
		buf := make([]byte, 32)
		var w bitstream.Writer
		w.Start(buf)
		// 2 bits: bit0 then bit1 (writeLiteral-style MSB-first is what
		// ReadLiteral expects).
		raw := uint32(want)
		if want > common.Allow32x32 {
			raw = uint32(common.Allow32x32)
		}
		w.Write((raw>>1)&1, 128)
		w.Write(raw&1, 128)
		if want >= common.Allow32x32 {
			if want == common.TxModeSelect {
				w.Write(1, 128)
			} else {
				w.Write(0, 128)
			}
		}
		size, err := w.Stop()
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if got := ReadTxMode(&r); got != want {
			t.Errorf("tx_mode %d: got %d", want, got)
		}
	}
}

// TestReadTxModeProbsNoUpdates confirms that when every update bit is
// 0 the probability table is preserved unchanged.
func TestReadTxModeProbsNoUpdates(t *testing.T) {
	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	// 18 update slots total (2*1 + 2*2 + 2*3 = 12... wait: 2*(1) + 2*(2)
	// + 2*(3) = 12). Each emits a "0" against DIFF_UPDATE_PROB.
	for range 12 {
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
	var tp TxProbs
	// Seed with sentinel values so we can detect any unintended write.
	for i := range tp.P8x8 {
		for j := range tp.P8x8[i] {
			tp.P8x8[i][j] = 33
		}
	}
	for i := range tp.P16x16 {
		for j := range tp.P16x16[i] {
			tp.P16x16[i][j] = 44
		}
	}
	for i := range tp.P32x32 {
		for j := range tp.P32x32[i] {
			tp.P32x32[i][j] = 55
		}
	}

	ReadTxModeProbs(&r, &tp)
	for i := range tp.P8x8 {
		for j := range tp.P8x8[i] {
			if tp.P8x8[i][j] != 33 {
				t.Errorf("P8x8[%d][%d] = %d, want 33 preserved", i, j, tp.P8x8[i][j])
			}
		}
	}
	for i := range tp.P16x16 {
		for j := range tp.P16x16[i] {
			if tp.P16x16[i][j] != 44 {
				t.Errorf("P16x16[%d][%d] = %d, want 44 preserved", i, j, tp.P16x16[i][j])
			}
		}
	}
	for i := range tp.P32x32 {
		for j := range tp.P32x32[i] {
			if tp.P32x32[i][j] != 55 {
				t.Errorf("P32x32[%d][%d] = %d, want 55 preserved", i, j, tp.P32x32[i][j])
			}
		}
	}
}

func TestReadSkipProbsNoUpdates(t *testing.T) {
	buf := make([]byte, 16)
	var w bitstream.Writer
	w.Start(buf)
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
	probs := [SkipContexts]uint8{42, 84, 168}
	ReadSkipProbs(&r, &probs)
	if probs != [SkipContexts]uint8{42, 84, 168} {
		t.Errorf("skip probs changed: %v", probs)
	}
}
