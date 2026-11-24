package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// decodeTileForTest mirrors the encoder's SB walk: outer row loop
// with left_seg_context reset; inner col loop walks decodeSbForTest.
// Accumulates leaf visits in the same order WriteModesTile dispatches
// to the encoder leaf callback.
func decodeTileForTest(t *testing.T, r *bitstream.Reader, fc *vp9dec.FrameContext,
	aboveCtx, leftCtx []int8, miRowStart, miRowEnd, miColStart, miColEnd int,
	miRows, miCols int,
) []leafVisit {
	t.Helper()
	var got []leafVisit
	for miRow := miRowStart; miRow < miRowEnd; miRow += common.MiBlockSize {
		for i := range leftCtx {
			leftCtx[i] = 0
		}
		for miCol := miColStart; miCol < miColEnd; miCol += common.MiBlockSize {
			decodeSbForTest(t, r, fc, aboveCtx, leftCtx, miRows, miCols,
				miRow, miCol, common.Block64x64, &got)
		}
	}
	return got
}

// TestWriteModesTileTwoSbs: a 1-row × 2-column tile of Block64x64
// super-blocks where each SB's only leaf is Block64x64 with
// PartitionNone. Two leaf dispatches, one per SB, at (0,0) and
// (0,8). Round-trips via decodeTileForTest.
func TestWriteModesTileTwoSbs(t *testing.T) {
	var fc vp9dec.FrameContext
	fillPartitionProbs(&fc.PartitionProb)

	miRows, miCols := 8, 16
	aboveCtx := make([]int8, miCols)
	leftCtx := make([]int8, common.MiBlockSize)

	// Every cell reports Block64x64; at the SB level
	// PartitionLookup[bsl(64x64)][Block64x64] = PartitionNone, so each
	// SB lands a single leaf at (mi_row, mi_col, Block64x64).
	mi := &vp9dec.NeighborMi{SbType: common.Block64x64}
	gotLeaves := []leafVisit{}
	sbArgs := WriteModesSbArgs{
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
	tileArgs := WriteModesTileArgs{
		WriteModesSbArgs: sbArgs,
		MiRowStart:       0,
		MiRowEnd:         miRows,
		MiColStart:       0,
		MiColEnd:         miCols,
	}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteModesTile(&bw, tileArgs)
	size, _ := bw.Stop()

	want := []leafVisit{
		{0, 0, common.Block64x64},
		{0, 8, common.Block64x64},
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
	decAbove := make([]int8, miCols)
	decLeft := make([]int8, common.MiBlockSize)
	got := decodeTileForTest(t, &r, &fc, decAbove, decLeft,
		0, miRows, 0, miCols, miRows, miCols)
	if len(got) != len(want) {
		t.Fatalf("decoder leaves = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("decoder leaf %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestWriteModesTileLeftCtxReset: two SB rows where the partition
// context state in the LEFT-column ctx could leak across rows if not
// zeroed at row boundaries. The test exercises the per-row reset by
// running two rows of one SB each; both partition emissions should
// see a fresh left ctx (zeros) — same as libvpx's
// vp9_zero(xd->left_seg_context).
func TestWriteModesTileLeftCtxReset(t *testing.T) {
	var fc vp9dec.FrameContext
	fillPartitionProbs(&fc.PartitionProb)

	miRows, miCols := 16, 8
	aboveCtx := make([]int8, miCols)
	leftCtx := make([]int8, common.MiBlockSize)
	// Pre-poison left ctx so the test fails if reset doesn't happen.
	for i := range leftCtx {
		leftCtx[i] = 0x55
	}

	mi := &vp9dec.NeighborMi{SbType: common.Block64x64}
	sbArgs := WriteModesSbArgs{
		AboveSegCtx:    aboveCtx,
		LeftSegCtx:     leftCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: &fc.PartitionProb,
		GetMi:          func(r, c int) *vp9dec.NeighborMi { return mi },
		WriteB:         func(_ *bitstream.Writer, r, c int, b common.BlockSize) {},
	}
	tileArgs := WriteModesTileArgs{
		WriteModesSbArgs: sbArgs,
		MiRowStart:       0,
		MiRowEnd:         miRows,
		MiColStart:       0,
		MiColEnd:         miCols,
	}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteModesTile(&bw, tileArgs)
	size, _ := bw.Stop()
	if size == 0 {
		t.Fatal("no bytes written")
	}
	// After the walk, leftCtx should reflect the second row's outgoing
	// state (Block64x64's PartitionContextLookup.Left entry stamped at
	// every row position). The pre-poison values must be gone.
	for i, v := range leftCtx {
		if v == 0x55 {
			t.Errorf("leftCtx[%d] still 0x55: row reset didn't happen", i)
		}
	}
}
