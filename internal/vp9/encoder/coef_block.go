package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// WriteCoefBlockArgs bundles the inputs WriteCoefBlock consults.
//
// Layout mirrors libvpx's tokenize_b context: the coefficient
// stream is walked in scan order; band + ctx pick a probability
// row from `fc`; the per-position token cache feeds the next
// context.
type WriteCoefBlockArgs struct {
	TxSize    common.TxSize
	PlaneType int
	IsInter   int
	DequantDC int16
	DequantAC int16
	Scan      []int16
	Neighbors []int16

	// Coeffs are the dequantized residual coefficients in raster
	// order (indexed by scan[c]).
	Coeffs []int16

	// QCoeffs are the signed quantized coefficients in raster order.
	// When supplied, coefficient tokens are derived from qcoeff exactly
	// like libvpx's tokenize_b; Coeffs remain the reconstructed dqcoeff
	// plane consumed by inverse transforms and legacy callers.
	QCoeffs []int16

	// Fc carries the active per-frame coefficient probabilities.
	Fc *vp9dec.FrameCoefProbs

	// CoefBranchStats, when non-nil, receives the branch counts for
	// the coefficient token tree slots touched by this block. These
	// are the counts consumed by WriteCoefProbsFromCounts.
	CoefBranchStats *FrameCoefBranchStats

	// InitCtx is the band-0 coefficient context derived from the
	// above/left entropy-context cache via GetEntropyContext. Mirrors
	// libvpx's get_entropy_context result (0..2). Zero is correct
	// only when there's no neighbor residue (top-left of the SB or
	// directly after a skip block).
	InitCtx int

	// EOB, when non-nil, receives the computed end-of-block value so
	// callers that need residue presence can avoid rescanning coeffs.
	EOB *int

	// KnownEOB, when valid, is the quantizer-produced end-of-block value.
	// libvpx carries this through token staging, so callers that already have
	// qcoeff/eob can avoid rescanning the transform block before writing it.
	KnownEOB      int
	KnownEOBValid bool

	// TokenCache, when non-nil, is reusable (possibly dirty) scratch for the
	// per-position token energy-class cache. libvpx tokenize_b keeps this as
	// an uninitialized stack array: the scan-order walk writes every position
	// before any neighbor context read touches it, so no clearing between
	// blocks is required. When nil a zeroed local is used.
	TokenCache *[1024]uint8

	// TokenClasses, when it spans the transform block, carries the
	// per-raster-position token energy classes the quantizer scan already
	// produced for exactly these QCoeffs
	// (classes[rc] == vp9_pt_energy_class[token(|QCoeffs[rc]|)]). Token
	// staging then reads neighbor contexts straight from this span instead
	// of re-deriving and re-writing the token cache; the scan-order walk
	// only ever consults positions earlier in scan order, whose precomputed
	// classes match the values the incremental walk would have written.
	TokenClasses []uint8
}

// WriteCoefBlock emits the wire fragment for one transform block's
// coefficient stream. Mirrors the t >= TWO_TOKEN branch of
// libvpx's tokenize_b inverted into the encoder side: walks
// `Coeffs` in scan order, emitting non-EOB / non-ZERO / ONE-or-tree
// + sign for each non-zero entry, ZERO inside runs, and EOB once
// the trailing zeros begin. Returns the boolean-coded byte count
// written.
func WriteCoefBlock(bw *bitstream.Writer, a WriteCoefBlockArgs) error {
	maxEob := vp9dec.MaxEobForTxSize(a.TxSize)
	scan := a.Scan[:maxEob]
	bandTrans := vp9dec.BandTranslateForTxSize(a.TxSize)[:maxEob]
	_ = a.Neighbors[(maxEob<<1)-1]
	dq := [2]int16{a.DequantDC, a.DequantAC}
	qcoeffs := a.QCoeffs
	if len(qcoeffs) < maxEob {
		qcoeffs = nil
	}

	// Find EOB position: one past the last non-zero coefficient.
	eob := 0
	if a.KnownEOBValid && a.KnownEOB >= 0 && a.KnownEOB <= maxEob {
		eob = a.KnownEOB
	} else if qcoeffs != nil {
		eob = coeffBlockEOBCompleteQCoeffWindow(scan, maxEob, qcoeffs)
	} else {
		eob = coeffBlockEOBEncode(scan, maxEob, a.Coeffs, qcoeffs)
	}
	if a.EOB != nil {
		*a.EOB = eob
	}

	coefModel := &a.Fc[a.TxSize][a.PlaneType][a.IsInter]
	branchStatsRows := coefBranchStatsRowsFor(a.CoefBranchStats, a.TxSize,
		a.PlaneType, a.IsInter)
	tokenCache := a.TokenCache
	if tokenCache == nil {
		var local [1024]uint8
		tokenCache = &local
	}
	ctx := a.InitCtx

	c := 0
	for c < maxEob {
		band := int(bandTrans[c])
		probs := &coefModel[band][ctx]
		branchStats := coefBranchStatsSlot(branchStatsRows, band, ctx)
		if c == eob {
			recordCoefBranch00(branchStats)
			bw.Write(0, uint32(probs[0])) // EOB
			return nil
		}
		recordCoefBranch01(branchStats)
		bw.Write(1, uint32(probs[0])) // not EOB

		// ZERO inner loop: mirror the decoder, which reads only the
		// ZERO bit (no fresh EOB) for each zero in a run.
		raster := int(scan[c])
		for !coeffBlockHasCoeffAtRaster(raster, a.Coeffs, qcoeffs) {
			recordCoefBranch10(branchStats)
			bw.Write(0, uint32(probs[1])) // ZERO
			tokenCacheSet(tokenCache, raster, 0)
			c++
			if c >= maxEob {
				return nil
			}
			ctx = tokenCacheContext(a.Neighbors, tokenCache, c)
			band = int(bandTrans[c])
			probs = &coefModel[band][ctx]
			branchStats = coefBranchStatsSlot(branchStatsRows, band, ctx)
			raster = int(scan[c])
		}

		// Non-zero at c.
		recordCoefBranch11(branchStats)
		bw.Write(1, uint32(probs[1])) // not ZERO

		dqv := dq[1]
		if c == 0 {
			dqv = dq[0]
		}
		var absVal, sign int
		if qcoeffs != nil {
			absVal, sign = coeffMagnitudeAndSignQ(qcoeffAt(qcoeffs, raster))
		} else {
			absVal, sign = coeffMagnitudeAndSignDQ(a.Coeffs[raster],
				dqv, a.TxSize == common.Tx32x32)
		}
		writeTokenForCoeff(bw, probs, absVal, sign, branchStats)

		switch {
		case absVal == 1:
			tokenCacheSet(tokenCache, raster, 1)
		case absVal == 2:
			tokenCacheSet(tokenCache, raster, 2)
		case absVal == 3 || absVal == 4:
			tokenCacheSet(tokenCache, raster, 3)
		case absVal <= 10:
			tokenCacheSet(tokenCache, raster, 4)
		default:
			tokenCacheSet(tokenCache, raster, 5)
		}
		c++
		if c < maxEob {
			ctx = tokenCacheContext(a.Neighbors, tokenCache, c)
		}
	}
	return nil
}

func tokenCacheSet(tokenCache *[1024]uint8, raster int, v uint8) {
	tokenCache[raster&1023] = v
}

func tokenCacheContext(neighbors []int16, tokenCache *[1024]uint8, c int) int {
	off := c << 1
	a := tokenCache[int(neighbors[off])&1023]
	b := tokenCache[int(neighbors[off+1])&1023]
	return (1 + int(a) + int(b)) >> 1
}

// tokenClassAt reads one quantizer-produced token energy class.
func tokenClassAt(classes []uint8, raster int) uint8 {
	return classes[raster]
}

// tokenClassContext mirrors tokenCacheContext over the quantizer-produced
// class span: the VP9 scan-neighbor tables reference only raster positions of
// coefficients earlier in scan order, all inside the same transform block, so
// the precomputed span substitutes for the incrementally written cache.
func tokenClassContext(neighbors []int16, classes []uint8, c int) int {
	off := c << 1
	a := classes[neighbors[off]]
	b := classes[neighbors[off+1]]
	return (1 + int(a) + int(b)) >> 1
}

func qcoeffAt(qcoeffs []int16, raster int) int16 {
	return qcoeffs[raster]
}

func coeffBlockEOBCompleteQCoeffWindow(scan []int16, maxEob int, qcoeffs []int16) int {
	_ = scan[maxEob-1]
	_ = qcoeffs[maxEob-1]
	for i := maxEob - 1; i >= 0; i-- {
		if qcoeffAt(qcoeffs, int(scan[i])) != 0 {
			return i + 1
		}
	}
	return 0
}

type coefBranchStatsRows = [vp9dec.CoefBands][vp9dec.CoefContexts][EntropyNodes][2]uint32

func coefBranchStatsRowsFor(
	stats *FrameCoefBranchStats, tx common.TxSize, planeType, isInter int,
) *coefBranchStatsRows {
	if stats == nil {
		return nil
	}
	return &stats[tx][planeType][isInter]
}

func coefBranchStatsSlot(
	stats *coefBranchStatsRows, band, ctx int,
) *[EntropyNodes][2]uint32 {
	if stats == nil {
		return nil
	}
	return &stats[band][ctx]
}

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

	// Retain each independent slot decision for the emit pass.
	var proposed vp9dec.CoefProbsModel
	// Dry run.
	var totalSavings int64
	updateCount := 0
	for i := range vp9dec.CoefPlaneTypes {
		for j := range vp9dec.CoefRefTypes {
			for k := range vp9dec.CoefBands {
				bcc := vp9dec.BandCoefContexts(k)
				for l := range bcc {
					for t := range UnconstrainedNodes {
						s, newp := coefSlotSavings(probs, counts, i, j, k, l, t, stepsize)
						proposed[i][j][k][l][t] = newp
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
						newp := proposed[i][j][k][l][t]
						oldp := probs[i][j][k][l][t]
						if newp != oldp {
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
