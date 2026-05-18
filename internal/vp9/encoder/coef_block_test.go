package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestWriteCoefBlockEarlyEob: an all-zero block emits only the
// EOB-at-position-0 bit; the decoder must read back eob=0 and leave
// dqcoeff untouched.
func TestWriteCoefBlockEarlyEob(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]

	coeffs := make([]int16, 16)
	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:    common.Tx4x4,
		DequantDC: 16,
		DequantAC: 16,
		Scan:      scan,
		Neighbors: neigh,
		Coeffs:    coeffs,
		Fc:        &fc,
	})
	if err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	for i := range dqcoeff {
		dqcoeff[i] = 0x7777
	}
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, [2]int16{16, 16}, 0, scan, neigh, &fc, dqcoeff)
	if got != 0 {
		t.Errorf("eob got %d, want 0", got)
	}
	for i, v := range dqcoeff {
		if v != 0x7777 {
			t.Errorf("dqcoeff[%d] disturbed: %d", i, v)
		}
	}
}

// TestWriteCoefBlockSingleOne: a block whose first coefficient is
// the DC dequant (i.e. absVal=1) round-trips with eob=1 and the
// matching dqcoeff value at scan[0].
func TestWriteCoefBlockSingleOne(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 24}

	coeffs := make([]int16, 16)
	coeffs[scan[0]] = dq[0] // sign +1, absVal = 1

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:    common.Tx4x4,
		DequantDC: dq[0],
		DequantAC: dq[1],
		Scan:      scan,
		Neighbors: neigh,
		Coeffs:    coeffs,
		Fc:        &fc,
	}); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, _ := bw.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob got %d want 1", got)
	}
	if dqcoeff[scan[0]] != dq[0] {
		t.Errorf("dqcoeff[scan[0]] = %d want %d", dqcoeff[scan[0]], dq[0])
	}
}

// TestWriteCoefBlockZeroRunThenOne: scan position 0 is zero, scan
// position 1 has the AC dequant (absVal=1), then EOB.
func TestWriteCoefBlockZeroRunThenOne(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 32}

	coeffs := make([]int16, 16)
	coeffs[scan[0]] = 0
	coeffs[scan[1]] = dq[1]

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:    common.Tx4x4,
		DequantDC: dq[0],
		DequantAC: dq[1],
		Scan:      scan,
		Neighbors: neigh,
		Coeffs:    coeffs,
		Fc:        &fc,
	}); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, _ := bw.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 2 {
		t.Errorf("eob got %d want 2", got)
	}
	if dqcoeff[scan[0]] != 0 {
		t.Errorf("dqcoeff[scan[0]] = %d want 0", dqcoeff[scan[0]])
	}
	if dqcoeff[scan[1]] != dq[1] {
		t.Errorf("dqcoeff[scan[1]] = %d want %d", dqcoeff[scan[1]], dq[1])
	}
}

func TestWriteCoefBlockBranchStatsMatchDecoderPrefixCounts(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 24}

	coeffs := make([]int16, 16)
	coeffs[scan[0]] = 5 * dq[0] // CAT1 at DC.
	coeffs[scan[3]] = -dq[1]    // ZERO run, then ONE at AC.

	var stats FrameCoefBranchStats
	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:          common.Tx4x4,
		DequantDC:       dq[0],
		DequantAC:       dq[1],
		Scan:            scan,
		Neighbors:       neigh,
		Coeffs:          coeffs,
		Fc:              &fc,
		CoefBranchStats: &stats,
	}); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var counts vp9dec.CoefCounts
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefsWithCounts(&r, common.Tx4x4, 0, 0, dq, 0,
		scan, neigh, &fc, &counts, dqcoeff)
	if got != 4 {
		t.Fatalf("eob got %d want 4", got)
	}

	assertCoefPrefixStatsMatchDecoderCounts(t, &stats, &counts)
}

func TestWriteCoefBlockCat2UsesLibvpxEnergyClass(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 24}

	coeffs := make([]int16, 16)
	coeffs[scan[0]] = 7 * dq[0] // CAT2 at DC; libvpx energy class is 4.
	coeffs[scan[1]] = dq[1]

	var stats FrameCoefBranchStats
	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:          common.Tx4x4,
		DequantDC:       dq[0],
		DequantAC:       dq[1],
		Scan:            scan,
		Neighbors:       neigh,
		Coeffs:          coeffs,
		Fc:              &fc,
		CoefBranchStats: &stats,
	}); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}

	var tokenCache [1024]uint8
	tokenCache[scan[0]] = 4
	wantCtx := vp9dec.GetCoefContext(neigh, &tokenCache, 1)
	tokenCache[scan[0]] = 5
	wrongCtx := vp9dec.GetCoefContext(neigh, &tokenCache, 1)
	if wantCtx == wrongCtx {
		t.Fatalf("test setup did not distinguish CAT2 context: got %d", wantCtx)
	}

	band := tables.CoefbandTrans4x4[1]
	if got := stats[common.Tx4x4][0][0][band][wantCtx][0]; got[1] == 0 {
		t.Fatalf("CAT2-following EOB branch at ctx %d = %v, want not-EOB usage", wantCtx, got)
	}
	if got := stats[common.Tx4x4][0][0][band][wrongCtx][0]; got != [2]uint32{} {
		t.Fatalf("CAT2-following wrong ctx %d was used: %v", wrongCtx, got)
	}
}

func TestWriteCoefBlockTx32CeilsHalfDequantToken(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan32x32[:]
	neigh := tables.DefaultScan32x32Neighbors[:]
	dq := [2]int16{7, 9}

	coeffs := make([]int16, 1024)
	coeffs[scan[0]] = dq[0] >> 1

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:    common.Tx32x32,
		DequantDC: dq[0],
		DequantAC: dq[1],
		Scan:      scan,
		Neighbors: neigh,
		Coeffs:    coeffs,
		Fc:        &fc,
	}); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, _ := bw.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	dqcoeff := make([]int16, 1024)
	got := vp9dec.DecodeCoefs(&r, common.Tx32x32, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob got %d want 1", got)
	}
	if dqcoeff[scan[0]] != coeffs[scan[0]] {
		t.Errorf("dqcoeff[scan[0]] = %d want %d", dqcoeff[scan[0]], coeffs[scan[0]])
	}
}

func TestWriteCoefBlockBranchStatsIncludeParetoTail(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 16}

	coeffs := make([]int16, 16)
	coeffs[scan[0]] = 5 * dq[0] // CAT1 tail path: 1, 0, 0.

	var stats FrameCoefBranchStats
	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:          common.Tx4x4,
		DequantDC:       dq[0],
		DequantAC:       dq[1],
		Scan:            scan,
		Neighbors:       neigh,
		Coeffs:          coeffs,
		Fc:              &fc,
		CoefBranchStats: &stats,
	}); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}

	slot := stats[common.Tx4x4][0][0][0][0]
	if got := slot[PivotNode]; got != [2]uint32{0, 1} {
		t.Fatalf("pivot branch = %v, want [0 1]", got)
	}
	if got := slot[UnconstrainedNodes+0]; got != [2]uint32{0, 1} {
		t.Fatalf("tail root branch = %v, want [0 1]", got)
	}
	if got := slot[UnconstrainedNodes+3]; got != [2]uint32{1, 0} {
		t.Fatalf("tail CAT1/CAT2 split branch = %v, want [1 0]", got)
	}
	if got := slot[UnconstrainedNodes+4]; got != [2]uint32{1, 0} {
		t.Fatalf("tail CAT1 branch = %v, want [1 0]", got)
	}
}

// TestWriteCoefBlockBranchStatsAccumulateAcrossMultipleBlocks pins the
// task #154 negative finding: govpx's per-block branch-count
// accumulation matches libvpx's build_tree_distribution output
// (vp9/encoder/vp9_bitstream.c:519-543 — vp9_tree_probs_from_distribution
// + eob_branch overwrite) when summed across multiple tx-blocks. The
// per-block contract is already covered by
// TestWriteCoefBlockBranchStatsMatchDecoderPrefixCounts; this extends it
// to a 3-block sequence with mixed token classes so the
// SUM(per-block contribution) = build_tree_distribution(SUM(per-block
// token counts)) identity is regression-locked. Without this guard a
// future refactor of WriteCoefBlock's record sites could drop the
// summing semantics that the live encoder relies on at
// internal/vp9/encoder/coef_sb.go:194-198 (the same stats pointer is
// passed across every tx-block in the SB).
func TestWriteCoefBlockBranchStatsAccumulateAcrossMultipleBlocks(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 24}

	// Three blocks with distinct token-class shapes so the summed
	// contributions exercise EOB, ZERO, ONE, and pareto-tail node
	// slots:
	//   Block 0: single ONE at DC, rest zero -> 1 ONE + EOB
	//   Block 1: CAT1 at DC, ONE at AC, ZERO run, ONE at scan[4]
	//   Block 2: all zero (EOB at scan[0])
	type blockShape struct {
		coeffs [16]int16
	}
	var blocks [3]blockShape
	blocks[0].coeffs[scan[0]] = dq[0] // ONE
	blocks[1].coeffs[scan[0]] = 5 * dq[0]
	blocks[1].coeffs[scan[1]] = dq[1]
	blocks[1].coeffs[scan[4]] = -dq[1]
	// blocks[2] left zero.

	var stats FrameCoefBranchStats
	var counts vp9dec.CoefCounts
	buf := make([]byte, 1024)

	for bi := range blocks {
		var bw bitstream.Writer
		bw.Start(buf)
		coeffs := blocks[bi].coeffs[:]
		if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
			TxSize:          common.Tx4x4,
			DequantDC:       dq[0],
			DequantAC:       dq[1],
			Scan:            scan,
			Neighbors:       neigh,
			Coeffs:          coeffs,
			Fc:              &fc,
			CoefBranchStats: &stats,
		}); err != nil {
			t.Fatalf("block %d WriteCoefBlock: %v", bi, err)
		}
		size, _ := bw.Stop()

		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("block %d Init: %v", bi, err)
		}
		dqcoeff := make([]int16, 16)
		vp9dec.DecodeCoefsWithCounts(&r, common.Tx4x4, 0, 0, dq, 0,
			scan, neigh, &fc, &counts, dqcoeff)
	}

	assertCoefPrefixStatsMatchDecoderCounts(t, &stats, &counts)
}

func assertCoefPrefixStatsMatchDecoderCounts(
	t *testing.T, stats *FrameCoefBranchStats, counts *vp9dec.CoefCounts,
) {
	t.Helper()
	for tx := common.Tx4x4; tx <= common.Tx32x32; tx++ {
		for plane := range vp9dec.CoefPlaneTypes {
			for ref := range vp9dec.CoefRefTypes {
				for band := range vp9dec.CoefBands {
					for ctx := range vp9dec.BandCoefContexts(band) {
						n0 := counts.Coef[tx][plane][ref][band][ctx][ZeroToken]
						n1 := counts.Coef[tx][plane][ref][band][ctx][OneToken]
						n2 := counts.Coef[tx][plane][ref][band][ctx][TwoToken]
						neob := counts.Coef[tx][plane][ref][band][ctx][EobModelToken]
						eob := counts.EobBranch[tx][plane][ref][band][ctx]
						want := [UnconstrainedNodes][2]uint32{
							{neob, eob - neob},
							{n0, n1 + n2},
							{n1, n2},
						}
						for node := range UnconstrainedNodes {
							if got := stats[tx][plane][ref][band][ctx][node]; got != want[node] {
								t.Fatalf("stats[%d][%d][%d][%d][%d][%d] = %v, want %v",
									tx, plane, ref, band, ctx, node, got, want[node])
							}
						}
					}
				}
			}
		}
	}
}
