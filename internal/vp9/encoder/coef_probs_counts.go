package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 coefficient probability update writer.
// Ported from libvpx v1.16.0 vp9/encoder/vp9_bitstream.c —
// update_coef_probs + update_coef_probs_common. Keyframes in the
// realtime cpu8 path use TWO_LOOP, while inter frames use
// ONE_LOOP_REDUCED.

// CoefUpdateMode mirrors libvpx's FAST_COEFF_UPDATE selection.
type CoefUpdateMode int

const (
	CoefUpdateTwoLoop CoefUpdateMode = iota
	CoefUpdateOneLoopReduced
)

// CoefBranchStatsPerTx mirrors libvpx's vp9_coeff_stats prefixed
// with PLANE_TYPES — the per-tx-size branch-count payload for one
// tx_size. Layout: [PlaneTypes][RefTypes][CoefBands][CoefContexts][EntropyNodes][2].
type CoefBranchStatsPerTx = [vp9dec.CoefPlaneTypes][vp9dec.CoefRefTypes][vp9dec.CoefBands][vp9dec.CoefContexts][EntropyNodes][2]uint32

// FrameCoefBranchStats aggregates the per-tx-size branch stats for
// every TxSize. Mirrors the encoder's per-frame counts.coef array
// (one CoefBranchStatsPerTx slot per TxSize).
type FrameCoefBranchStats [common.TxSizes]CoefBranchStatsPerTx

// WriteCoefProbsFromCounts mirrors libvpx's update_coef_probs +
// update_coef_probs_common TWO_LOOP path. Walks every active
// TxSize (gated by lossless + txMode) and invokes the per-tx update
// driver when tx_totals has enough samples. The probs slice is mutated
// in place to reflect any updates emitted; the same probs feed the
// per-block coefficient writers that follow this header.
func WriteCoefProbsFromCounts(bw *bitstream.Writer,
	probs *vp9dec.FrameCoefProbs, counts *FrameCoefBranchStats,
	txTotals *[common.TxSizes]uint32, lossless bool, txMode common.TxMode,
	stepsize int, mode CoefUpdateMode, skipTx16Plus bool,
) {
	var max common.TxSize
	switch {
	case lossless:
		max = common.Tx4x4
	case txMode == common.TxModeSelect:
		max = common.Tx32x32
	default:
		max = common.TxModeToBiggestTxSize[txMode]
	}
	for tx := common.Tx4x4; tx <= max; tx++ {
		if (txTotals != nil && txTotals[tx] <= 20) ||
			(skipTx16Plus && tx >= common.Tx16x16) {
			bw.WriteBit(0)
			continue
		}
		updateCoefProbsTxSize(bw, &probs[tx], &counts[tx], stepsize, mode)
	}
}

// updateCoefProbsTxSize mirrors libvpx's update_coef_probs_common
// TWO_LOOP path for a single tx_size. Pass 1: dry run accumulates
// total savings + update count. Pass 2: emits the wire fragment if
// the tally is positive.
func updateCoefProbsTxSize(bw *bitstream.Writer,
	probs *vp9dec.CoefProbsModel, counts *CoefBranchStatsPerTx,
	stepsize int, mode CoefUpdateMode,
) {
	if mode == CoefUpdateOneLoopReduced {
		updateCoefProbsTxSizeOneLoopReduced(bw, probs, counts, stepsize)
		return
	}

	// Dry run.
	var totalSavings int64
	updateCount := 0
	for i := range vp9dec.CoefPlaneTypes {
		for j := range vp9dec.CoefRefTypes {
			for k := range vp9dec.CoefBands {
				bcc := vp9dec.BandCoefContexts(k)
				for l := range bcc {
					for t := range UnconstrainedNodes {
						s, _ := coefSlotSavings(probs, counts, i, j, k, l, t, stepsize)
						if s > 0 {
							totalSavings += s - int64(VP9CostZero(DiffUpdateProb))
							updateCount++
						} else {
							totalSavings -= int64(VP9CostZero(DiffUpdateProb))
						}
					}
				}
			}
		}
	}

	if updateCount == 0 || totalSavings < 0 {
		bw.WriteBit(0)
		return
	}
	bw.WriteBit(1)

	// Emit pass.
	for i := range vp9dec.CoefPlaneTypes {
		for j := range vp9dec.CoefRefTypes {
			for k := range vp9dec.CoefBands {
				bcc := vp9dec.BandCoefContexts(k)
				for l := range bcc {
					for t := range UnconstrainedNodes {
						s, newp := coefSlotSavings(probs, counts, i, j, k, l, t, stepsize)
						oldp := probs[i][j][k][l][t]
						if s > 0 && newp != oldp {
							bw.Write(1, DiffUpdateProb)
							WriteProbDiffUpdate(bw, newp, oldp)
							probs[i][j][k][l][t] = newp
						} else {
							bw.Write(0, DiffUpdateProb)
						}
					}
				}
			}
		}
	}
}

// updateCoefProbsTxSizeOneLoopReduced mirrors libvpx's realtime
// ONE_LOOP_REDUCED branch. It delays the tx-size update gate until
// the first positive slot so leading no-update bits can be elided
// when the entire tx size has no updates.
func updateCoefProbsTxSizeOneLoopReduced(bw *bitstream.Writer,
	probs *vp9dec.CoefProbsModel, counts *CoefBranchStatsPerTx,
	stepsize int,
) {
	updates := 0
	noUpdatesBeforeFirst := 0

	for i := range vp9dec.CoefPlaneTypes {
		for j := range vp9dec.CoefRefTypes {
			for k := range vp9dec.CoefBands {
				bcc := vp9dec.BandCoefContexts(k)
				for l := range bcc {
					for t := range UnconstrainedNodes {
						s, newp := coefSlotSavings(probs, counts, i, j, k, l, t, stepsize)
						oldp := probs[i][j][k][l][t]
						update := s > 0 && newp != oldp
						if !update && updates == 0 {
							noUpdatesBeforeFirst++
							continue
						}
						if update {
							updates++
							if updates == 1 {
								bw.WriteBit(1)
								for range noUpdatesBeforeFirst {
									bw.Write(0, DiffUpdateProb)
								}
							}
						}
						if update {
							bw.Write(1, DiffUpdateProb)
							WriteProbDiffUpdate(bw, newp, oldp)
							probs[i][j][k][l][t] = newp
						} else {
							bw.Write(0, DiffUpdateProb)
						}
					}
				}
			}
		}
	}
	if updates == 0 {
		bw.WriteBit(0)
	}
}

// coefSlotSavings runs the savings search for one (plane, ref, band,
// ctx, node) slot. Returns (savings, newp). The PivotNode branch
// calls the pareto8-aware model variant; other nodes use the binary
// search.
func coefSlotSavings(probs *vp9dec.CoefProbsModel,
	counts *CoefBranchStatsPerTx, i, j, k, l, t int, stepsize int,
) (int64, uint8) {
	oldp := probs[i][j][k][l][t]
	newp := max(GetBinaryProb(counts[i][j][k][l][t][0], counts[i][j][k][l][t][1]), 1)
	var s int64
	if t == PivotNode {
		s = ProbDiffUpdateSavingsSearchModel(&counts[i][j][k][l],
			oldp, &newp, DiffUpdateProb, stepsize)
	} else {
		s = ProbDiffUpdateSavingsSearch(counts[i][j][k][l][t],
			oldp, &newp, DiffUpdateProb)
	}
	if s <= 0 || newp == oldp {
		return 0, oldp
	}
	return s, newp
}
