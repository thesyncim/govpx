package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestBandCoefContexts(t *testing.T) {
	if got := BandCoefContexts(0); got != 3 {
		t.Errorf("band 0: got %d, want 3", got)
	}
	for b := 1; b < CoefBands; b++ {
		if got := BandCoefContexts(b); got != 6 {
			t.Errorf("band %d: got %d, want 6", b, got)
		}
	}
}

// TestReadCoefProbsCommonSkip writes a single "0" gate bit; the table
// must be untouched.
func TestReadCoefProbsCommonSkip(t *testing.T) {
	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, 128) // skip update
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var model CoefProbsModel
	for i := range model {
		for j := range model[i] {
			for k := range model[i][j] {
				for l := range model[i][j][k] {
					for m := range model[i][j][k][l] {
						model[i][j][k][l][m] = 77
					}
				}
			}
		}
	}
	readCoefProbsCommon(&r, &model)
	// Validate by sampling four corners of the 4D table.
	if model[0][0][0][0][0] != 77 || model[1][1][5][5][2] != 77 ||
		model[1][0][3][2][1] != 77 || model[0][1][2][4][0] != 77 {
		t.Error("skip path mutated table")
	}
}

// TestReadCoefProbsWalksUpToTxModeBudget writes one skip bit per tx
// level allowed by the supplied TxMode and confirms no extra bits get
// consumed past the cap.
func TestReadCoefProbsWalksUpToTxModeBudget(t *testing.T) {
	cases := []struct {
		mode       common.TxMode
		expectedTx int // how many leading skip bits we should emit
	}{
		{common.Only4x4, 1},  // tx 4x4 only
		{common.Allow8x8, 2}, // 4x4 + 8x8
		{common.Allow16x16, 3},
		{common.Allow32x32, 4},
		{common.TxModeSelect, 4}, // also caps at 32x32 in the table
	}
	for _, c := range cases {
		buf := make([]byte, 16)
		var w bitstream.Writer
		w.Start(buf)
		for range c.expectedTx {
			w.Write(0, 128) // skip update for this tx level
		}
		// Trailing sentinel bit so we can confirm the reader stopped at
		// the right spot.
		w.Write(1, 128)
		size, err := w.Stop()
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}

		var fc FrameCoefProbs
		ReadCoefProbs(&r, &fc, c.mode)
		// The next bit should be the sentinel = 1.
		if got := r.Read(128); got != 1 {
			t.Errorf("tx_mode=%d: reader consumed extra bits past cap (sentinel got %d)", c.mode, got)
		}
	}
}
