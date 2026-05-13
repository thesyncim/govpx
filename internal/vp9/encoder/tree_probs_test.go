package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestTreeProbsFromDistributionPartition exercises the partition
// tree (3 internal nodes / 4 leaves), feeding distinct event counts
// per leaf and verifying the resulting branch counts. The branch
// pairs are: (PartitionNone, PartitionHorz+Vert+Split) at root,
// (PartitionHorz, PartitionVert+Split) at the first sub-node, and
// (PartitionVert, PartitionSplit) at the second.
func TestTreeProbsFromDistributionPartition(t *testing.T) {
	// Leaf index = the negated tree entry. PartitionTree leaves:
	//   -PartitionNone=0, -PartitionHorz=1, -PartitionVert=2, -PartitionSplit=3.
	// Plant 4 distinct counts so each branch pair has a unique sum.
	counts := []uint32{
		3, // PartitionNone
		5, // PartitionHorz
		7, // PartitionVert
		2, // PartitionSplit
	}
	branchCt := make([][2]uint32, 3)
	TreeProbsFromDistribution(common.PartitionTree[:], branchCt, counts)

	// Root: (None, Horz+Vert+Split) = (3, 14).
	if branchCt[0] != [2]uint32{3, 14} {
		t.Errorf("root branchCt = %v, want (3, 14)", branchCt[0])
	}
	// Sub 1: (Horz, Vert+Split) = (5, 9).
	if branchCt[1] != [2]uint32{5, 9} {
		t.Errorf("sub1 branchCt = %v, want (5, 9)", branchCt[1])
	}
	// Sub 2: (Vert, Split) = (7, 2).
	if branchCt[2] != [2]uint32{7, 2} {
		t.Errorf("sub2 branchCt = %v, want (7, 2)", branchCt[2])
	}
}

// TestProbDiffUpdateForTreeRoundTrip encodes a multi-leaf update
// through the partition tree, then re-parses each branch's prob via
// the decoder's VpxDiffUpdateProb. Both sides see the same wire
// fragment and arrive at the same updated probability values.
func TestProbDiffUpdateForTreeRoundTrip(t *testing.T) {
	probs := []uint8{128, 128, 128}
	counts := []uint32{
		1000, // PartitionNone — heavily weighted
		50,   // Horz
		50,   // Vert
		50,   // Split
	}
	scratch := make([][2]uint32, len(probs))

	buf := make([]byte, 32)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := make([]uint8, len(probs))
	copy(writerProbs, probs)
	ProbDiffUpdateForTree(&bw, common.PartitionTree[:], writerProbs, counts, scratch)
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decodedProbs := make([]uint8, len(probs))
	copy(decodedProbs, probs)
	for i := range decodedProbs {
		vp9dec.VpxDiffUpdateProb(&r, &decodedProbs[i])
	}
	for i := range probs {
		if decodedProbs[i] != writerProbs[i] {
			t.Errorf("slot %d: decoded=%d, writer=%d (was=%d)",
				i, decodedProbs[i], writerProbs[i], probs[i])
		}
	}
}
