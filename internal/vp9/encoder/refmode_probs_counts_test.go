package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestCollapseReferenceModeFromCounts(t *testing.T) {
	t.Run("hybrid", func(t *testing.T) {
		counts := ReferenceModeCounts{}
		counts.CompInter[0] = [2]uint32{3, 4}
		got := CollapseReferenceModeFromCounts(vp9dec.ReferenceModeSelect, &counts)
		if got != vp9dec.ReferenceModeSelect {
			t.Fatalf("mode = %d, want ReferenceModeSelect", got)
		}
		if counts.CompInter[0] != [2]uint32{3, 4} {
			t.Fatalf("comp_inter was cleared for hybrid counts")
		}
	})
	t.Run("single only", func(t *testing.T) {
		counts := ReferenceModeCounts{}
		counts.CompInter[1] = [2]uint32{7, 0}
		got := CollapseReferenceModeFromCounts(vp9dec.ReferenceModeSelect, &counts)
		if got != vp9dec.SingleReference {
			t.Fatalf("mode = %d, want SingleReference", got)
		}
		if counts.CompInter != ([common.CompInterContexts][2]uint32{}) {
			t.Fatalf("comp_inter was not cleared after single-only demotion")
		}
	})
	t.Run("compound only", func(t *testing.T) {
		counts := ReferenceModeCounts{}
		counts.CompInter[2] = [2]uint32{0, 9}
		got := CollapseReferenceModeFromCounts(vp9dec.ReferenceModeSelect, &counts)
		if got != vp9dec.CompoundReference {
			t.Fatalf("mode = %d, want CompoundReference", got)
		}
		if counts.CompInter != ([common.CompInterContexts][2]uint32{}) {
			t.Fatalf("comp_inter was not cleared after compound-only demotion")
		}
	})
	t.Run("fixed mode", func(t *testing.T) {
		counts := ReferenceModeCounts{}
		counts.CompInter[0] = [2]uint32{0, 5}
		got := CollapseReferenceModeFromCounts(vp9dec.SingleReference, &counts)
		if got != vp9dec.SingleReference {
			t.Fatalf("mode = %d, want SingleReference", got)
		}
		if counts.CompInter[0] != [2]uint32{0, 5} {
			t.Fatalf("fixed reference mode mutated comp_inter counts")
		}
	})
}

// TestWriteReferenceModeProbsFromCountsHybridRoundTrip: hybrid
// (REFERENCE_MODE_SELECT) emits all three sub-tables. Decoder side
// walks the same slot order via the existing decoder helper.
func TestWriteReferenceModeProbsFromCountsHybridRoundTrip(t *testing.T) {
	var probs vp9dec.FrameReferenceModeProbs
	for i := range probs.CompInterProb {
		probs.CompInterProb[i] = 128
	}
	for i := range probs.SingleRefProb {
		probs.SingleRefProb[i] = [2]uint8{128, 128}
	}
	for i := range probs.CompRefProb {
		probs.CompRefProb[i] = 128
	}

	counts := ReferenceModeCounts{}
	// Bias each context distinctly.
	for i := range counts.CompInter {
		counts.CompInter[i] = [2]uint32{uint32(500 - 50*i), uint32(50 + 50*i)}
	}
	for i := range counts.SingleRef {
		counts.SingleRef[i][0] = [2]uint32{800, 50}
		counts.SingleRef[i][1] = [2]uint32{50, 800}
	}
	for i := range counts.CompRef {
		counts.CompRef[i] = [2]uint32{600, 100}
	}

	buf := make([]byte, 64)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := probs
	WriteReferenceModeProbsFromCounts(&bw, &writerProbs, &counts,
		vp9dec.ReferenceModeSelect, true)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	vp9dec.ReadFrameReferenceModeProbs(&r, vp9dec.ReferenceModeSelect, &decProbs)
	if decProbs != writerProbs {
		t.Errorf("decoded != writer: dec=%+v writer=%+v", decProbs, writerProbs)
	}
}

// TestWriteReferenceModeProbsFromCountsSingleOnly: SINGLE_REFERENCE
// emits only the single_ref sub-table; comp_inter and comp_ref are
// gated off. Decoder side reads the same shape.
func TestWriteReferenceModeProbsFromCountsSingleOnly(t *testing.T) {
	var probs vp9dec.FrameReferenceModeProbs
	for i := range probs.SingleRefProb {
		probs.SingleRefProb[i] = [2]uint8{128, 128}
	}
	counts := ReferenceModeCounts{}
	for i := range counts.SingleRef {
		counts.SingleRef[i][0] = [2]uint32{900, 50}
		counts.SingleRef[i][1] = [2]uint32{50, 900}
	}

	buf := make([]byte, 64)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := probs
	WriteReferenceModeProbsFromCounts(&bw, &writerProbs, &counts,
		vp9dec.SingleReference, true)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	vp9dec.ReadFrameReferenceModeProbs(&r, vp9dec.SingleReference, &decProbs)
	if decProbs != writerProbs {
		t.Errorf("decoded != writer: dec=%+v writer=%+v", decProbs, writerProbs)
	}
	// Both 0-th single_ref probs should have moved away from 128
	// given the bias.
	moved := false
	for i := range common.RefContexts {
		if writerProbs.SingleRefProb[i][0] != 128 {
			moved = true
		}
	}
	if !moved {
		t.Errorf("no single_ref prob moved despite heavy bias counts")
	}
}
