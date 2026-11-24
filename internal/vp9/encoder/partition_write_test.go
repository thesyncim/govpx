package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// fillPartitionProbs initializes the partition prob table to 128 so
// every partition bit lands mid-range. Writer and reader stay in sync
// at any value.
func fillPartitionProbs(p *[common.PartitionContexts][common.PartitionTypes - 1]uint8) {
	for i := range p {
		for j := range p[i] {
			p[i][j] = 128
		}
	}
}

// decodeSb walks the partition tree against the wire fragment
// WriteModesSb just produced. Returns a flat slice of
// (miRow, miCol, bsize) tuples in the order WriteModesSb dispatched
// to the leaf callback, so tests can compare against an expected
// trace.
type leafVisit struct {
	MiRow, MiCol int
	BSize        common.BlockSize
}

func decodeSbForTest(t *testing.T, r *bitstream.Reader, fc *vp9dec.FrameContext,
	aboveCtx, leftCtx []int8, miRows, miCols int,
	miRow, miCol int, bsize common.BlockSize, out *[]leafVisit,
) {
	t.Helper()
	if miRow >= miRows || miCol >= miCols {
		return
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	ctx := vp9dec.PartitionPlaneContext(aboveCtx, leftCtx, miRow, miCol, bsize)
	probs := fc.PartitionProb[ctx][:]
	hasRows := (miRow + bs) < miRows
	hasCols := (miCol + bs) < miCols
	partition := vp9dec.ReadPartition(r, probs, hasRows, hasCols)
	subsize := common.SubsizeLookup[partition][bsize]

	if subsize < common.Block8x8 {
		*out = append(*out, leafVisit{miRow, miCol, subsize})
	} else {
		switch partition {
		case common.PartitionNone:
			*out = append(*out, leafVisit{miRow, miCol, subsize})
		case common.PartitionHorz:
			*out = append(*out, leafVisit{miRow, miCol, subsize})
			if miRow+bs < miRows {
				*out = append(*out, leafVisit{miRow + bs, miCol, subsize})
			}
		case common.PartitionVert:
			*out = append(*out, leafVisit{miRow, miCol, subsize})
			if miCol+bs < miCols {
				*out = append(*out, leafVisit{miRow, miCol + bs, subsize})
			}
		default:
			decodeSbForTest(t, r, fc, aboveCtx, leftCtx, miRows, miCols,
				miRow, miCol, subsize, out)
			decodeSbForTest(t, r, fc, aboveCtx, leftCtx, miRows, miCols,
				miRow, miCol+bs, subsize, out)
			decodeSbForTest(t, r, fc, aboveCtx, leftCtx, miRows, miCols,
				miRow+bs, miCol, subsize, out)
			decodeSbForTest(t, r, fc, aboveCtx, leftCtx, miRows, miCols,
				miRow+bs, miCol+bs, subsize, out)
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(aboveCtx, leftCtx,
			miRow, miCol, subsize, bs)
	}
}

// TestWriteModesSbPartitionNone: 16x16 root with PartitionNone. One
// partition bit pattern (no split) and one leaf visit at the root.
func TestWriteModesSbPartitionNone(t *testing.T) {
	var fc vp9dec.FrameContext
	fillPartitionProbs(&fc.PartitionProb)

	aboveCtx := make([]int8, 16)
	leftCtx := make([]int8, common.MiBlockSize)
	miRows, miCols := 4, 4

	mi := &vp9dec.NeighborMi{SbType: common.Block16x16}
	gotLeaves := []leafVisit{}
	args := WriteModesSbArgs{
		AboveSegCtx:    aboveCtx,
		LeftSegCtx:     leftCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: &fc.PartitionProb,
		GetMi:          func(r, c int) *vp9dec.NeighborMi { return mi },
		WriteB: func(_ *bitstream.Writer, r, c int, b common.BlockSize) {
			gotLeaves = append(gotLeaves, leafVisit{r, c, b})
		},
	}

	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteModesSb(&bw, args, 0, 0, common.Block16x16)
	size, _ := bw.Stop()

	want := []leafVisit{{0, 0, common.Block16x16}}
	if len(gotLeaves) != 1 || gotLeaves[0] != want[0] {
		t.Errorf("encoder leaves = %v, want %v", gotLeaves, want)
	}

	// Decoder side mirrors the same recursion.
	var r bitstream.Reader
	r.Init(buf[:size])
	decAbove := make([]int8, 16)
	decLeft := make([]int8, common.MiBlockSize)
	var got []leafVisit
	decodeSbForTest(t, &r, &fc, decAbove, decLeft, miRows, miCols,
		0, 0, common.Block16x16, &got)
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("decoder leaves = %v, want %v", got, want)
	}
}

// TestWriteModesSbPartitionSplit: 32x32 root split into 4 Block16x16
// children, each PartitionNone. Verifies both the SPLIT bit at the
// root and the per-child PartitionNone trace.
func TestWriteModesSbPartitionSplit(t *testing.T) {
	var fc vp9dec.FrameContext
	fillPartitionProbs(&fc.PartitionProb)

	miRows, miCols := 8, 8
	aboveCtx := make([]int8, 16)
	leftCtx := make([]int8, common.MiBlockSize)

	// All leaf cells say Block16x16 — at the 32x32 level
	// PartitionLookup[bsl(32x32)][Block16x16] = PartitionSplit.
	mi := &vp9dec.NeighborMi{SbType: common.Block16x16}
	gotLeaves := []leafVisit{}
	args := WriteModesSbArgs{
		AboveSegCtx:    aboveCtx,
		LeftSegCtx:     leftCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: &fc.PartitionProb,
		GetMi:          func(r, c int) *vp9dec.NeighborMi { return mi },
		WriteB: func(_ *bitstream.Writer, r, c int, b common.BlockSize) {
			gotLeaves = append(gotLeaves, leafVisit{r, c, b})
		},
	}

	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteModesSb(&bw, args, 0, 0, common.Block32x32)
	size, _ := bw.Stop()

	// Block32x32 spans 4 mi units; the half-step bs = (1<<bsl)/4 = 2.
	want := []leafVisit{
		{0, 0, common.Block16x16},
		{0, 2, common.Block16x16},
		{2, 0, common.Block16x16},
		{2, 2, common.Block16x16},
	}
	if len(gotLeaves) != len(want) {
		t.Fatalf("encoder leaves = %v, want %v", gotLeaves, want)
	}
	for i := range want {
		if gotLeaves[i] != want[i] {
			t.Errorf("encoder leaf %d = %v, want %v", i, gotLeaves[i], want[i])
		}
	}

	var r bitstream.Reader
	r.Init(buf[:size])
	decAbove := make([]int8, 16)
	decLeft := make([]int8, common.MiBlockSize)
	var got []leafVisit
	decodeSbForTest(t, &r, &fc, decAbove, decLeft, miRows, miCols,
		0, 0, common.Block32x32, &got)
	if len(got) != len(want) {
		t.Fatalf("decoder leaves = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("decoder leaf %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestWriteModesSbEdgeForcedSplit: frame extents force a split at an
// edge cell. With (miRow + bs >= MiRows), libvpx asserts that only
// the SPLIT-vs-HORZ bit is emitted and the decoder is in the
// "no_rows" branch.
func TestWriteModesSbEdgeForcedSplit(t *testing.T) {
	var fc vp9dec.FrameContext
	fillPartitionProbs(&fc.PartitionProb)

	// Frame: 8 mi cols, 4 mi rows — 16x16 sub-block at (4, 0) has no
	// rows below it inside the frame.
	miRows, miCols := 4, 8
	aboveCtx := make([]int8, 16)
	leftCtx := make([]int8, common.MiBlockSize)

	mi := &vp9dec.NeighborMi{SbType: common.Block16x16}
	args := WriteModesSbArgs{
		AboveSegCtx:    aboveCtx,
		LeftSegCtx:     leftCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: &fc.PartitionProb,
		GetMi:          func(r, c int) *vp9dec.NeighborMi { return mi },
		WriteB:         func(_ *bitstream.Writer, r, c int, b common.BlockSize) {},
	}
	buf := make([]byte, 128)
	var bw bitstream.Writer
	bw.Start(buf)
	// Block32x32 at (0,0) where rows fit but bottom row doesn't:
	// has_rows=false, has_cols=true; partition must be HORZ here.
	// With every 16x16 sub-block, the natural partition is SPLIT —
	// at the edge the writer reduces it to the 1-bit form.
	WriteModesSb(&bw, args, 0, 0, common.Block32x32)
	size, _ := bw.Stop()
	if size == 0 {
		t.Fatal("no bytes written")
	}
}
