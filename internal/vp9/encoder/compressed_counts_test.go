package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteSkipProbsFromCountsRoundTrip: each of the 3 skip contexts
// runs the counts-driven cond-update; the decoder's
// ReadSkipProbs (per-slot VpxDiffUpdateProb) recovers the exact
// probability the savings_search settled on.
func TestWriteSkipProbsFromCountsRoundTrip(t *testing.T) {
	startProbs := [3]uint8{128, 128, 128}
	counts := [3][2]uint32{
		{900, 100}, // skewed: skip rare
		{100, 900}, // skewed: skip common
		{500, 500}, // balanced
	}

	buf := make([]byte, 32)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := startProbs
	WriteSkipProbsFromCounts(&bw, &writerProbs, &counts)
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decodedProbs := startProbs
	for i := range decodedProbs {
		vp9dec.VpxDiffUpdateProb(&r, &decodedProbs[i])
	}
	if decodedProbs != writerProbs {
		t.Errorf("decoded = %v, writer = %v", decodedProbs, writerProbs)
	}
}

// TestWriteInterModeProbsFromCountsRoundTrip: 7 contexts × the
// 3-node InterModeTree. Different per-context distributions exercise
// every branch of the savings_search loop. Decoder side walks each
// branch slot via VpxDiffUpdateProb and arrives at the same probs.
func TestWriteInterModeProbsFromCountsRoundTrip(t *testing.T) {
	var probs [common.InterModeContexts][common.InterModes - 1]uint8
	var counts [common.InterModeContexts][common.InterModes]uint32
	for i := range probs {
		for j := range probs[i] {
			probs[i][j] = 128
		}
		// Plant a peak at a different mode per context to drive distinct
		// branch counts.
		counts[i][i%common.InterModes] = 500
		// Salt the rest so each context has a non-degenerate distribution.
		for k := range counts[i] {
			if k != i%common.InterModes {
				counts[i][k] = 20
			}
		}
	}
	scratch := make([][2]uint32, common.InterModes-1)

	writerProbs := probs
	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteInterModeProbsFromCounts(&bw, &writerProbs, &counts, scratch)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decodedProbs := probs
	for i := range decodedProbs {
		for j := range decodedProbs[i] {
			vp9dec.VpxDiffUpdateProb(&r, &decodedProbs[i][j])
		}
	}
	for i := range probs {
		if decodedProbs[i] != writerProbs[i] {
			t.Errorf("ctx %d: decoded=%v, writer=%v",
				i, decodedProbs[i], writerProbs[i])
		}
	}
}

// TestWritePartitionProbsFromCountsRoundTrip: 16 partition contexts
// × the 3-node PartitionTree. Each context gets a distinct event
// distribution so the savings_search picks different update
// patterns across slots; decoder reads them back via ReadPartitionProbs
// per-slot calls.
func TestWritePartitionProbsFromCountsRoundTrip(t *testing.T) {
	var probs [common.PartitionContexts][common.PartitionTypes - 1]uint8
	var counts [common.PartitionContexts][common.PartitionTypes]uint32
	for i := range probs {
		for j := range probs[i] {
			probs[i][j] = 128
		}
		// Different leaf gets the peak in each context.
		counts[i] = [4]uint32{10, 10, 10, 10}
		counts[i][i%4] = 500
	}
	scratch := make([][2]uint32, common.PartitionTypes-1)

	writerProbs := probs
	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	WritePartitionProbsFromCounts(&bw, &writerProbs, &counts, scratch)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	decodedProbs := probs
	for i := range decodedProbs {
		for j := range decodedProbs[i] {
			vp9dec.VpxDiffUpdateProb(&r, &decodedProbs[i][j])
		}
	}
	for i := range probs {
		if decodedProbs[i] != writerProbs[i] {
			t.Errorf("ctx %d: decoded=%v, writer=%v",
				i, decodedProbs[i], writerProbs[i])
		}
	}
}
