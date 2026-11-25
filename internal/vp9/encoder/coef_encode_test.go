package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
)

// TestWriteTokenForCoeffOneRoundTrip emits a single ONE coefficient
// at scan position 0 (with positive sign) followed by EOB at scan
// position 1, then decodes the block via DecodeCoefs and confirms
// the coefficient comes back equal to dq[0].
func TestWriteTokenForCoeffOneRoundTrip(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 16}

	probs := fc[common.Tx4x4][0][0][0][0]
	band1 := tables.CoefbandTrans4x4[1]
	probs1 := fc[common.Tx4x4][0][0][band1][1]

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	// Coefficient at scan position 0: not EOB, not ZERO, ONE.
	bw.Write(1, uint32(probs[0])) // not EOB
	bw.Write(1, uint32(probs[1])) // not ZERO
	WriteTokenForCoeff(&bw, probs[:], 1, 0)
	// Coefficient at scan position 1: EOB.
	bw.Write(0, uint32(probs1[0]))

	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("bw.Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob got %d, want 1", got)
	}
	if dqcoeff[scan[0]] != dq[0] {
		t.Errorf("dqcoeff[scan[0]] = %d, want %d", dqcoeff[scan[0]], dq[0])
	}
}

// TestWriteTokenForCoeffCat1Negative writes a magnitude-5 (CAT1
// boundary) coefficient with negative sign and decodes it back.
func TestWriteTokenForCoeffCat1Negative(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 16}

	probs := fc[common.Tx4x4][0][0][0][0]
	band1 := tables.CoefbandTrans4x4[1]
	// After this coefficient ctx becomes (1+5+0)>>1 = 3? No — token cache
	// stamps 5 at scan[0], so ctx for next = (1+5+0)>>1 = 3.
	probs1 := fc[common.Tx4x4][0][0][band1][3]

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	bw.Write(1, uint32(probs[0])) // not EOB
	bw.Write(1, uint32(probs[1])) // not ZERO
	WriteTokenForCoeff(&bw, probs[:], 5, 1)
	bw.Write(0, uint32(probs1[0]))

	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob got %d, want 1", got)
	}
	want := -int16(5 * int(dq[0]))
	if dqcoeff[scan[0]] != want {
		t.Errorf("dqcoeff[scan[0]] = %d, want %d", dqcoeff[scan[0]], want)
	}
}

// seedDefaultCoefProbsForEnc seeds FrameCoefProbs with libvpx's
// default coefficient probabilities. Mirrors the decoder-side test
// helper.
func seedDefaultCoefProbsForEnc() vp9dec.FrameCoefProbs {
	var out vp9dec.FrameCoefProbs
	out[common.Tx4x4] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs4x4)
	out[common.Tx8x8] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs8x8)
	out[common.Tx16x16] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs16x16)
	out[common.Tx32x32] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs32x32)
	return out
}

// TestWriteCoefProbsFromCountsZeroCountsNoUpdate: all-zero counts
// give zero savings, so the dry-run tally hits the "no update"
// path and the writer emits a single 0 bit per tx_size.
func TestWriteCoefProbsFromCountsZeroCountsNoUpdate(t *testing.T) {
	var probs vp9dec.FrameCoefProbs
	for i := range probs {
		for p := range probs[i] {
			for r := range probs[i][p] {
				for b := range probs[i][p][r] {
					for c := range probs[i][p][r][b] {
						for n := range probs[i][p][r][b][c] {
							probs[i][p][r][b][c][n] = 128
						}
					}
				}
			}
		}
	}
	var counts FrameCoefBranchStats // all zero

	buf := make([]byte, 4096)
	var bw bitstream.Writer
	bw.Start(buf)
	var txTotals [common.TxSizes]uint32
	txTotals[common.Tx4x4] = 21
	WriteCoefProbsFromCounts(&bw, &probs, &counts, &txTotals,
		false, common.Only4x4, 4, CoefUpdateTwoLoop, false)
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Decoder side: ReadCoefProbs walks one tx_size (Only4x4 caps at
	// Tx4x4). The "no update" path consumes exactly one bit.
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var decProbs vp9dec.FrameCoefProbs
	for i := range decProbs {
		for p := range decProbs[i] {
			for ri := range decProbs[i][p] {
				for b := range decProbs[i][p][ri] {
					for c := range decProbs[i][p][ri][b] {
						for n := range decProbs[i][p][ri][b][c] {
							decProbs[i][p][ri][b][c][n] = 128
						}
					}
				}
			}
		}
	}
	vp9dec.ReadCoefProbs(&r, &decProbs, common.Only4x4)
	if decProbs != probs {
		t.Errorf("decoder side probs diverged from encoder side")
	}
}

// TestWriteCoefProbsFromCountsAtLeastOneUpdate: bias counts at a
// single (plane, ref, band, ctx, node) slot heavily; the dry-run
// tally should pick that update, the gate emits 1, and the decoder
// recovers the same probability for the updated slot.
func TestWriteCoefProbsFromCountsAtLeastOneUpdate(t *testing.T) {
	var probs vp9dec.FrameCoefProbs
	for i := range probs {
		for p := range probs[i] {
			for r := range probs[i][p] {
				for b := range probs[i][p][r] {
					for c := range probs[i][p][r][b] {
						for n := range probs[i][p][r][b][c] {
							probs[i][p][r][b][c][n] = 128
						}
					}
				}
			}
		}
	}

	var counts FrameCoefBranchStats
	// Plant heavy bias on (tx=0, plane=0, ref=0, band=1, ctx=0, node=0).
	// Use a non-pivot node so the binary search variant runs; band 1
	// dodges the band-0 special case.
	counts[0][0][0][1][0][0] = [2]uint32{2000, 50}
	// Plant noise across other slots to give a realistic dry-run.
	counts[0][0][0][1][0][1] = [2]uint32{500, 200}
	counts[0][0][0][1][0][2] = [2]uint32{300, 200}
	var txTotals [common.TxSizes]uint32
	txTotals[common.Tx4x4] = 21

	buf := make([]byte, 8192)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := probs
	WriteCoefProbsFromCounts(&bw, &writerProbs, &counts, &txTotals,
		false, common.Only4x4, 4, CoefUpdateTwoLoop, false)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	vp9dec.ReadCoefProbs(&r, &decProbs, common.Only4x4)

	if decProbs != writerProbs {
		t.Errorf("decoder side probs diverged from encoder side")
	}
	// Confirm at least one slot moved: the writer should have updated
	// (tx=0, plane=0, ref=0, band=1, ctx=0, node=0).
	if writerProbs[0][0][0][1][0][0] == 128 {
		t.Errorf("expected node 0 prob to have moved from 128; counts didn't drive an update")
	}
}

func TestWriteCoefProbsFromCountsOneLoopReducedRoundTrip(t *testing.T) {
	var probs vp9dec.FrameCoefProbs
	for i := range probs {
		for p := range probs[i] {
			for r := range probs[i][p] {
				for b := range probs[i][p][r] {
					for c := range probs[i][p][r][b] {
						for n := range probs[i][p][r][b][c] {
							probs[i][p][r][b][c][n] = 128
						}
					}
				}
			}
		}
	}

	var counts FrameCoefBranchStats
	// Leave the first slots neutral so ONE_LOOP_REDUCED has to emit
	// deferred no-update bits before the first real update.
	counts[0][0][0][1][0][0] = [2]uint32{2000, 50}
	var txTotals [common.TxSizes]uint32
	txTotals[common.Tx4x4] = 21

	buf := make([]byte, 8192)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := probs
	WriteCoefProbsFromCounts(&bw, &writerProbs, &counts, &txTotals,
		false, common.Only4x4, 4, CoefUpdateOneLoopReduced, false)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	vp9dec.ReadCoefProbs(&r, &decProbs, common.Only4x4)

	if decProbs != writerProbs {
		t.Errorf("decoder side probs diverged from encoder side")
	}
	if writerProbs[0][0][0][1][0][0] == 128 {
		t.Errorf("expected node 0 prob to have moved from 128")
	}
}

func TestWriteCoefProbsFromCountsLowTxTotalNoUpdate(t *testing.T) {
	var probs vp9dec.FrameCoefProbs
	for i := range probs {
		for p := range probs[i] {
			for r := range probs[i][p] {
				for b := range probs[i][p][r] {
					for c := range probs[i][p][r][b] {
						for n := range probs[i][p][r][b][c] {
							probs[i][p][r][b][c][n] = 128
						}
					}
				}
			}
		}
	}

	var counts FrameCoefBranchStats
	counts[0][0][0][1][0][0] = [2]uint32{2000, 50}
	var txTotals [common.TxSizes]uint32
	txTotals[common.Tx4x4] = 20

	buf := make([]byte, 8192)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := probs
	WriteCoefProbsFromCounts(&bw, &writerProbs, &counts, &txTotals,
		false, common.Only4x4, 4, CoefUpdateTwoLoop, false)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	vp9dec.ReadCoefProbs(&r, &decProbs, common.Only4x4)

	if writerProbs != probs {
		t.Fatalf("writer probs changed despite low tx total")
	}
	if decProbs != probs {
		t.Fatalf("decoder probs changed despite low tx total")
	}
}

func TestWriteCoefProbsFromCountsSkipsTx16Plus(t *testing.T) {
	var probs vp9dec.FrameCoefProbs
	for i := range probs {
		for p := range probs[i] {
			for r := range probs[i][p] {
				for b := range probs[i][p][r] {
					for c := range probs[i][p][r][b] {
						for n := range probs[i][p][r][b][c] {
							probs[i][p][r][b][c][n] = 128
						}
					}
				}
			}
		}
	}

	var counts FrameCoefBranchStats
	counts[common.Tx16x16][0][0][1][0][0] = [2]uint32{2000, 50}
	var txTotals [common.TxSizes]uint32
	txTotals[common.Tx16x16] = 21

	buf := make([]byte, 8192)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := probs
	WriteCoefProbsFromCounts(&bw, &writerProbs, &counts, &txTotals,
		false, common.TxModeSelect, 4, CoefUpdateOneLoopReduced, true)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	vp9dec.ReadCoefProbs(&r, &decProbs, common.TxModeSelect)

	if writerProbs != probs {
		t.Fatalf("writer probs changed despite TX_16X16 skip")
	}
	if decProbs != probs {
		t.Fatalf("decoder probs changed despite TX_16X16 skip")
	}
}
