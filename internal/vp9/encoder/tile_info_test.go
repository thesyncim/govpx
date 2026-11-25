package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

// TestWriteTileInfoRoundTrip walks (mi_cols, log2_tile_cols,
// log2_tile_rows) combinations and verifies each (encoder, decoder)
// pair recovers the same TileInfo from the wire fragment. Both
// directions share vp9dec.TileNBits, so this is a tight check on
// the encoder's bit pattern matching libvpx's write_tile_info.
func TestWriteTileInfoRoundTrip(t *testing.T) {
	// Cover a handful of mi_cols values across the (min, max) regime
	// shifts. The 8x8 mi-unit projection: width / 8 rounded up.
	miColSet := []int{8, 16, 32, 64, 128, 256, 512, 1024}
	for _, miCols := range miColSet {
		minLog2, maxLog2 := vp9dec.TileNBits(miCols)
		for log2Cols := minLog2; log2Cols <= maxLog2 && log2Cols <= 6; log2Cols++ {
			for _, log2Rows := range []int{0, 1, 2} {
				tile := &vp9dec.TileInfo{
					Log2TileCols: log2Cols,
					Log2TileRows: log2Rows,
				}

				buf := make([]byte, 8)
				w := NewBitWriter(buf)
				WriteTileInfo(w, tile, miCols)
				size := w.BytesWritten()

				var r vp9dec.BitReader
				r.Init(buf[:size])
				var got vp9dec.TileInfo
				if err := vp9dec.ReadTileInfo(&r, miCols, &got); err != nil {
					t.Fatalf("miCols=%d log2(c,r)=(%d,%d): ReadTileInfo: %v",
						miCols, log2Cols, log2Rows, err)
				}
				if got.Log2TileCols != log2Cols {
					t.Errorf("miCols=%d log2(c,r)=(%d,%d): got Log2TileCols=%d",
						miCols, log2Cols, log2Rows, got.Log2TileCols)
				}
				if got.Log2TileRows != log2Rows {
					t.Errorf("miCols=%d log2(c,r)=(%d,%d): got Log2TileRows=%d",
						miCols, log2Cols, log2Rows, got.Log2TileRows)
				}
			}
		}
	}
}

// TestWriteTileInfoBitCount confirms the bit-count formula: the
// columns side emits (log2_tile_cols - min) ones + (1 if cols <
// max), and the rows side emits 1 bit (if rows == 0) or 2 bits
// (rows > 0). This is the libvpx invariant the parser depends on
// when bounding header reads.
func TestWriteTileInfoBitCount(t *testing.T) {
	miCols := 128 // sb64_cols = 16 → min=0, max=2
	minLog2, maxLog2 := vp9dec.TileNBits(miCols)
	if minLog2 != 0 || maxLog2 != 2 {
		t.Fatalf("miCols=128 unexpected (min,max)=(%d,%d)", minLog2, maxLog2)
	}

	cases := []struct {
		log2Cols, log2Rows int
		// Expected bit count: ones for cols + zero terminator (if cols
		// < max) + 1 or 2 row bits.
		wantBits int
	}{
		{0, 0, 0 + 1 + 1},
		{0, 1, 0 + 1 + 2},
		{1, 0, 1 + 1 + 1},
		{2, 0, 2 + 0 + 1}, // at max, no terminator
		{2, 2, 2 + 0 + 2},
	}
	for _, c := range cases {
		tile := &vp9dec.TileInfo{Log2TileCols: c.log2Cols, Log2TileRows: c.log2Rows}
		buf := make([]byte, 4)
		w := NewBitWriter(buf)
		startBits := w.BitsWritten()
		WriteTileInfo(w, tile, miCols)
		got := w.BitsWritten() - startBits
		if got != c.wantBits {
			t.Errorf("log2(c,r)=(%d,%d): bits=%d, want %d",
				c.log2Cols, c.log2Rows, got, c.wantBits)
		}
	}
}

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
