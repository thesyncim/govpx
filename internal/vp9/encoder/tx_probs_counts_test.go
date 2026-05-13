package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteTxModeProbsFromCountsRoundTrip exercises each per-context
// sub-table with a distinct count distribution per max-tx level.
// The decoder's per-slot VpxDiffUpdateProb walk recovers the same
// updated probabilities the savings_search settled on.
func TestWriteTxModeProbsFromCountsRoundTrip(t *testing.T) {
	probs := vp9dec.TxProbs{}
	for i := 0; i < vp9dec.TxSizeContexts; i++ {
		probs.P8x8[i][0] = 128
		for j := range probs.P16x16[i] {
			probs.P16x16[i][j] = 128
		}
		for j := range probs.P32x32[i] {
			probs.P32x32[i][j] = 128
		}
	}

	counts := TxModeCounts{
		// Strong bias toward Tx4x4 at ctx=0, Tx8x8 at ctx=1.
		P8x8: [vp9dec.TxSizeContexts][2]uint32{
			{1000, 50},
			{50, 1000},
		},
		// Distinct distributions per context.
		P16x16: [vp9dec.TxSizeContexts][3]uint32{
			{800, 100, 50},  // mostly Tx4x4
			{100, 100, 800}, // mostly Tx16x16
		},
		// Distinct distributions per context.
		P32x32: [vp9dec.TxSizeContexts][4]uint32{
			{700, 100, 100, 50}, // mostly Tx4x4
			{50, 100, 100, 700}, // mostly Tx32x32
		},
	}

	buf := make([]byte, 64)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := probs
	WriteTxModeProbsFromCounts(&bw, &writerProbs, &counts)
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Decoder side mirrors ReadTxModeProbs: walks the same slot order.
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	for i := 0; i < vp9dec.TxSizeContexts; i++ {
		vp9dec.VpxDiffUpdateProb(&r, &decProbs.P8x8[i][0])
	}
	for i := 0; i < vp9dec.TxSizeContexts; i++ {
		for j := range decProbs.P16x16[i] {
			vp9dec.VpxDiffUpdateProb(&r, &decProbs.P16x16[i][j])
		}
	}
	for i := 0; i < vp9dec.TxSizeContexts; i++ {
		for j := range decProbs.P32x32[i] {
			vp9dec.VpxDiffUpdateProb(&r, &decProbs.P32x32[i][j])
		}
	}
	if decProbs != writerProbs {
		t.Errorf("decoder side probs diverged from encoder side")
	}
	// Confirm at least one prob moved away from 128.
	moved := false
	for i := 0; i < vp9dec.TxSizeContexts; i++ {
		if writerProbs.P8x8[i][0] != 128 {
			moved = true
		}
	}
	if !moved {
		t.Errorf("no p8x8 prob moved from 128 despite heavy bias counts")
	}
}
