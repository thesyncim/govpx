package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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
		false, common.Only4x4, 4)
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
		false, common.Only4x4, 4)
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
		false, common.Only4x4, 4)
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
