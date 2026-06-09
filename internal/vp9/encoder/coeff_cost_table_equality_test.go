package encoder

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// coeffBlockRateCostRecursiveReference is a verbatim copy of the ORIGINAL
// recursive cost_coeffs implementation (pre table-lookup), kept here so the
// table-driven CoeffBlockRateCost can be proven byte-identical to the old
// per-coefficient CoeffTreeTokenCost recursion. It must NOT be changed to
// track the production code — it is the frozen oracle.
func coeffBlockRateCostRecursiveReference(in CoeffBlockRateCostInput) int {
	maxEob := vp9dec.MaxEobForTxSize(in.TxSize)
	if in.TxSize >= common.TxSizes || in.CoefModel == nil ||
		in.Dequant[0] == 0 || in.Dequant[1] == 0 ||
		len(in.Coeffs) < maxEob || in.InitCtx < 0 || in.InitCtx > 2 ||
		in.TokenCache == nil {
		return 0
	}
	if in.QCoeffs != nil && len(in.QCoeffs) < maxEob {
		in.QCoeffs = nil
	}
	scan := in.ScanOrder.Scan
	neighbors := in.ScanOrder.Neighbors
	if len(scan) < maxEob || len(neighbors) < common.MaxNeighbors*maxEob {
		return 0
	}
	for i := range in.TokenCache[:maxEob] {
		in.TokenCache[i] = 0
	}
	if in.Fast {
		return coeffBlockRateCostFastQReference(in, scan, maxEob)
	}
	eob := CoeffBlockEOB(scan, maxEob, in.Coeffs, in.QCoeffs)
	return coeffBlockRateCostSlowQReference(in, scan, neighbors, maxEob, eob)
}

func coeffBlockRateCostSlowQReference(in CoeffBlockRateCostInput,
	scan, neighbors []int16, maxEob int, eob int,
) int {
	if eob <= 0 {
		return CoeffTreeTokenCost((*in.CoefModel)[0][in.InitCtx][:], false,
			EobToken)
	}
	if eob > maxEob {
		eob = maxEob
	}

	dcAbs, dcSign := CoeffMagnitudeAndSign(in.QCoeffs, 0, in.Coeffs[0],
		in.Dequant[0], in.TxSize == common.Tx32x32)
	prevToken, extraCost := CoeffTokenExtraCost(dcAbs, dcSign)
	rate := extraCost + CoeffTreeTokenCost(
		(*in.CoefModel)[0][in.InitCtx][:], false, prevToken)
	in.TokenCache[0] = PtEnergyClass[prevToken]

	band := 1
	bandLeft := coeffCostBandCounts[in.TxSize][band]
	for c := 1; c < eob; c++ {
		if band >= vp9dec.CoefBands {
			return rate
		}
		raster := int(scan[c])
		absVal, sign := CoeffMagnitudeAndSign(in.QCoeffs, raster,
			in.Coeffs[raster], in.Dequant[1], in.TxSize == common.Tx32x32)
		token, extra := CoeffTokenExtraCost(absVal, sign)
		pt := vp9dec.GetCoefContext(neighbors, in.TokenCache, c)
		rate += extra + CoeffTreeTokenCost(
			(*in.CoefModel)[band][pt][:], prevToken == ZeroToken, token)
		in.TokenCache[raster] = PtEnergyClass[token]
		if bandLeft > 0 {
			bandLeft--
			if bandLeft == 0 {
				band++
				if band < len(coeffCostBandCounts[in.TxSize]) {
					bandLeft = coeffCostBandCounts[in.TxSize][band]
				}
			}
		}
		prevToken = token
	}
	if bandLeft != 0 && band < vp9dec.CoefBands {
		pt := vp9dec.GetCoefContext(neighbors, in.TokenCache, eob)
		rate += CoeffTreeTokenCost((*in.CoefModel)[band][pt][:], false,
			EobToken)
	}
	return rate
}

func coeffBlockRateCostFastQReference(in CoeffBlockRateCostInput, scan []int16,
	maxEob int,
) int {
	eob := CoeffBlockEOB(scan, maxEob, in.Coeffs, in.QCoeffs)
	if eob == 0 {
		return CoeffTreeTokenCost((*in.CoefModel)[0][in.InitCtx][:], false,
			EobToken)
	}

	rate := 0
	dcAbs, dcSign := CoeffMagnitudeAndSign(in.QCoeffs, 0, in.Coeffs[0],
		in.Dequant[0], in.TxSize == common.Tx32x32)
	prevToken, extraCost := CoeffTokenExtraCost(dcAbs, dcSign)
	rate += extraCost
	rate += CoeffTreeTokenCost((*in.CoefModel)[0][in.InitCtx][:], false,
		prevToken)

	bandIdx := 1
	bandLeft := coeffCostBandCounts[in.TxSize][bandIdx]
	for c := 1; c < eob; c++ {
		raster := int(scan[c])
		absVal, sign := CoeffMagnitudeAndSign(in.QCoeffs, raster,
			in.Coeffs[raster], in.Dequant[1], in.TxSize == common.Tx32x32)
		token, extra := CoeffTokenExtraCost(absVal, sign)
		ctx := 0
		skipEOB := false
		if prevToken == ZeroToken {
			ctx = 1
			skipEOB = true
		}
		rate += extra
		rate += CoeffTreeTokenCost((*in.CoefModel)[bandIdx][ctx][:],
			skipEOB, token)
		prevToken = token
		bandLeft--
		if bandLeft == 0 {
			bandIdx++
			if bandIdx >= len(coeffCostBandCounts[in.TxSize]) {
				break
			}
			bandLeft = coeffCostBandCounts[in.TxSize][bandIdx]
		}
	}
	if bandLeft != 0 {
		ctx := 0
		if prevToken == ZeroToken {
			ctx = 1
		}
		rate += CoeffTreeTokenCost((*in.CoefModel)[bandIdx][ctx][:], false,
			EobToken)
	}
	return rate
}

// randomCoefModel fills a coefficient-probs model with random pivot probs in
// [1, 255] so every band/ctx slot expands to a valid full-prob row (model[2]
// must be non-zero for both the recursive and table paths to charge a tree
// cost — a zero pivot is the early-out both share).
func randomCoefModel(rng *rand.Rand) *CoeffModel {
	m := &CoeffModel{}
	for band := 0; band < vp9dec.CoefBands; band++ {
		for ctx := 0; ctx < vp9dec.CoefContexts; ctx++ {
			m[band][ctx][0] = uint8(1 + rng.Intn(255))
			m[band][ctx][1] = uint8(1 + rng.Intn(255))
			m[band][ctx][2] = uint8(1 + rng.Intn(255))
		}
	}
	return m
}

// TestCoeffBlockRateCostTableMatchesRecursive asserts the table-driven
// CoeffBlockRateCost produces costs byte-identical to the frozen recursive
// reference over a broad sweep of randomized inputs (model, tx size, eob,
// init ctx, fast/slow, coeff magnitudes & signs, with and without qcoeffs).
func TestCoeffBlockRateCostTableMatchesRecursive(t *testing.T) {
	rng := rand.New(rand.NewSource(0xC0FFEE))
	txSizes := []common.TxSize{
		common.Tx4x4, common.Tx8x8, common.Tx16x16, common.Tx32x32,
	}

	const iters = 20000
	for it := 0; it < iters; it++ {
		txSize := txSizes[rng.Intn(len(txSizes))]
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		scanOrder := common.DefaultScanOrders[txSize]

		model := randomCoefModel(rng)
		// Build the per-frame table the production path would build once.
		table := BuildCoeffTokenCostTable(model)

		coeffs := make([]int16, maxEob)
		var qcoeffs []int16
		useQ := rng.Intn(2) == 0
		if useQ {
			qcoeffs = make([]int16, maxEob)
		}

		// Decide how many coefficients are non-zero and where (in scan order),
		// then write magnitudes/signs spanning all token classes including the
		// CAT6 range so extra-bit handling is exercised.
		nnz := rng.Intn(maxEob + 1)
		for c := 0; c < nnz; c++ {
			raster := int(scanOrder.Scan[c])
			var mag int
			switch rng.Intn(8) {
			case 0:
				mag = 0 // leave a hole inside the active range
			case 1:
				mag = 1
			case 2:
				mag = 2
			case 3:
				mag = 3 + rng.Intn(2)
			case 4:
				mag = 5 + rng.Intn(60)
			default:
				mag = 67 + rng.Intn(400) // CAT6 territory
			}
			if mag == 0 {
				continue
			}
			if rng.Intn(2) == 0 {
				mag = -mag
			}
			v := int16(mag)
			if useQ {
				qcoeffs[raster] = v
				// dqcoeff feeds the magnitude recovery when qcoeffs is nil; for
				// the qcoeff path coeffs/dqcoeff is unused for magnitude, but
				// keep it populated to mirror real callers.
				coeffs[raster] = v
			} else {
				// When reading from coeffs (dqcoeff), CoeffMagnitudeAndSign
				// divides by the dequant; multiply up so the recovered token
				// magnitude matches mag.
				coeffs[raster] = v
			}
		}

		dcQ := int16(1 + rng.Intn(255))
		acQ := int16(1 + rng.Intn(255))
		initCtx := rng.Intn(3)
		fast := rng.Intn(2) == 0

		base := CoeffBlockRateCostInput{
			TxSize:    txSize,
			CoefModel: model,
			ScanOrder: scanOrder,
			Dequant:   [2]int16{dcQ, acQ},
			Coeffs:    coeffs,
			QCoeffs:   qcoeffs,
			InitCtx:   initCtx,
			Fast:      fast,
		}

		var refScratch [1024]byte
		refIn := base
		refIn.TokenCache = &refScratch
		refIn.CostTable = nil
		want := coeffBlockRateCostRecursiveReference(refIn)

		// New path with the explicitly-supplied per-frame table.
		var gotScratch [1024]byte
		gotIn := base
		gotIn.TokenCache = &gotScratch
		gotIn.CostTable = table
		got := CoeffBlockRateCost(gotIn)

		// New path with a nil table (lazy one-shot build) — must also match.
		var lazyScratch [1024]byte
		lazyIn := base
		lazyIn.TokenCache = &lazyScratch
		lazyIn.CostTable = nil
		gotLazy := CoeffBlockRateCost(lazyIn)

		if got != want || gotLazy != want {
			t.Fatalf("iter %d tx=%d fast=%v useQ=%v nnz=%d ctx=%d dq=(%d,%d): "+
				"table=%d lazy=%d want(recursive)=%d",
				it, txSize, fast, useQ, nnz, initCtx, dcQ, acQ,
				got, gotLazy, want)
		}
	}
}

// TestCoeffTokenCostTableMatchesCoeffTreeTokenCost asserts the per-slot table
// entries equal CoeffTreeTokenCost for every (band, ctx, token, skipEOB),
// including the zero-pivot early-out slots.
func TestCoeffTokenCostTableMatchesCoeffTreeTokenCost(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5EED))
	for it := 0; it < 200; it++ {
		model := &CoeffModel{}
		for band := 0; band < vp9dec.CoefBands; band++ {
			for ctx := 0; ctx < vp9dec.CoefContexts; ctx++ {
				// Node probs are always in [1, 255] in real coef-probs models
				// (a 0 prob would index vp9_prob_cost[256] and is never emitted
				// by the bitstream); keep them valid for both paths.
				model[band][ctx][0] = uint8(1 + rng.Intn(255))
				model[band][ctx][1] = uint8(1 + rng.Intn(255))
				// Allow pivot==0 occasionally to exercise the early-out.
				if rng.Intn(8) == 0 {
					model[band][ctx][2] = 0
				} else {
					model[band][ctx][2] = uint8(1 + rng.Intn(255))
				}
			}
		}
		table := BuildCoeffTokenCostTable(model)
		for band := 0; band < vp9dec.CoefBands; band++ {
			for ctx := 0; ctx < vp9dec.BandCoefContexts(band); ctx++ {
				for token := 0; token < EntropyTokens; token++ {
					for _, skip := range []bool{false, true} {
						want := CoeffTreeTokenCost(model[band][ctx][:], skip, token)
						got := table.Lookup(band, ctx, token, skip)
						if got != want {
							t.Fatalf("iter %d band=%d ctx=%d token=%d skip=%v: "+
								"table=%d want=%d", it, band, ctx, token, skip,
								got, want)
						}
					}
				}
			}
		}
	}
}
