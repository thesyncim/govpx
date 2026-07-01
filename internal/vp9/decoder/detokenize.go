package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 coefficient detokenizer. Ported from libvpx v1.16.0
// vp9/decoder/vp9_detokenize.c — decode_coefs.
//
// The hot loop pulls coefficient tokens out of the boolean range coder
// in scan order, expanding the per-band/per-context PMF (3 unconstrained
// nodes + Pareto8Full tail) into magnitude/category/sign and writing
// dequantized values into the supplied dqcoeff buffer.
//
// 8-bit profile only (cat6 has 14 bits, no highbd cat-prob switch).

// Token-tree node positions inside the unconstrained-prefix PMF.
const (
	eobContextNode  = 0
	zeroContextNode = 1
	oneContextNode  = 2
	pivotNode       = 2
)

// Magnitude category minima, mirroring CAT{n}_MIN_VAL in
// vp9_entropy.h.
const (
	cat1MinVal = 5
	cat2MinVal = 7
	cat3MinVal = 11
	cat4MinVal = 19
	cat5MinVal = 35
	cat6MinVal = 67
)

const cat6Bits8 = 14 // 8-bit profile cat6 magnitude width

const (
	zeroToken     = 0
	oneToken      = 1
	twoToken      = 2
	eobModelToken = 3
)

// FrameCoefCounts mirrors libvpx's FRAME_COUNTS.coef slab:
// [TX_SIZES][PLANE_TYPES][REF_TYPES][COEF_BANDS][COEFF_CONTEXTS]
// [UNCONSTRAINED_NODES + EOB_MODEL_TOKEN].
type FrameCoefCounts [common.TxSizes][CoefPlaneTypes][CoefRefTypes][CoefBands][CoefContexts][UnconstrainedNodes + 1]uint32

// FrameEobBranchCounts mirrors libvpx's FRAME_COUNTS.eob_branch slab.
type FrameEobBranchCounts [common.TxSizes][CoefPlaneTypes][CoefRefTypes][CoefBands][CoefContexts]uint32

// CoefCounts carries the coefficient-count state needed by libvpx's
// counts-driven non-frame-parallel probability adaptation.
type CoefCounts struct {
	Coef      FrameCoefCounts
	EobBranch FrameEobBranchCounts
}

// maxEobForTxSize returns the maximum number of coefficients a given
// transform contains. Mirrors `max_eob = 16 << (tx_size << 1)`.
func maxEobForTxSize(tx common.TxSize) int {
	return MaxEobForTxSize(tx)
}

// MaxEobForTxSize is the public mirror of maxEobForTxSize — the
// coefficient count for the given transform shape.
func MaxEobForTxSize(tx common.TxSize) int {
	return 16 << (tx << 1)
}

// bandTranslateForTxSize selects the scan-position → coef-band map.
// Mirrors get_band_translate in vp9_entropy.h.
func bandTranslateForTxSize(tx common.TxSize) []uint8 {
	return BandTranslateForTxSize(tx)
}

// BandTranslateForTxSize is the public mirror of
// bandTranslateForTxSize — returns the scan-position → coef-band
// table for the given transform shape.
func BandTranslateForTxSize(tx common.TxSize) []uint8 {
	if tx == common.Tx4x4 {
		return tables.CoefbandTrans4x4[:]
	}
	return tables.CoefbandTrans8x8Plus[:]
}

// GetCoefContext is the public mirror of getCoefContext — neighbor
// average of the per-coefficient token cache, +1, >> 1.
func GetCoefContext(neighbors []int16, tokenCache *[1024]uint8, c int) int {
	return getCoefContext(neighbors, tokenCache[:], c)
}

// readCoeffBits pulls `n` raw bits from the boolean coder against the
// supplied per-bit probabilities, MSB first. Mirrors read_coeff.
func readCoeffBits(r *bitstream.Reader, probs []uint8, n int) int {
	val := 0
	for i := range n {
		val = (val << 1) | int(r.Read(uint32(probs[i])))
	}
	return val
}

// getCoefContext mirrors get_coef_context from vp9_scan.h. Looks up
// the two neighbor scan positions and averages their cached token
// magnitudes, +1, >> 1.
func getCoefContext(neighbors []int16, tokenCache []uint8, c int) int {
	a := tokenCache[neighbors[2*c+0]]
	b := tokenCache[neighbors[2*c+1]]
	return (1 + int(a) + int(b)) >> 1
}

// DecodeCoefs mirrors libvpx's decode_coefs. Returns the EOB
// (end-of-block) position — i.e. one past the last non-zero
// coefficient in scan order. `dqcoeff` is written for scan positions
// 0..eob-1; positions beyond eob are left as the caller set them
// (libvpx zeroes the block first).
//
// Inputs:
//   - r       : boolean range coder positioned at the start of the
//     coefficient stream for this block.
//   - txSize  : transform-size code (Tx4x4..Tx32x32).
//   - planeType: 0 for Y, 1 for U/V.
//   - isInter : 1 if the block's prediction mode is an inter mode,
//     0 otherwise — selects ref==1 vs ref==0 in CoefProbs.
//   - dequant : 2-element [DC, AC] dequant pair for this plane.
//   - ctx     : initial coefficient-band context (0..MAX-1) from the
//     above/left predictor cache.
//   - scan, neighbors: ScanOrder for the (txSize, txType) pair.
//   - fc      : per-frame entropy context (only CoefProbs is read).
//   - dqcoeff : output coefficient buffer in raster (NOT scan) order;
//     positions are indexed by scan[c].
func DecodeCoefs(
	r *bitstream.Reader,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	ctx int,
	scan, neighbors []int16,
	fc *FrameCoefProbs,
	dqcoeff []int16,
) int {
	return DecodeCoefsWithCounts(r, txSize, planeType, isInter, dequant, ctx,
		scan, neighbors, fc, nil, dqcoeff)
}

// DecodeCoefsWithCounts is DecodeCoefs plus libvpx FRAME_COUNTS updates.
// Passing nil counts preserves the historical DecodeCoefs behavior.
func DecodeCoefsWithCounts(
	r *bitstream.Reader,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	ctx int,
	scan, neighbors []int16,
	fc *FrameCoefProbs,
	counts *CoefCounts,
	dqcoeff []int16,
) int {
	switch txSize {
	case common.Tx4x4:
		var tokenCache [16]uint8
		return decodeCoefsWithCountsScratch(r, txSize, planeType, isInter,
			dequant, ctx, scan, neighbors, fc, counts, dqcoeff, tokenCache[:])
	case common.Tx8x8:
		var tokenCache [64]uint8
		return decodeCoefsWithCountsScratch(r, txSize, planeType, isInter,
			dequant, ctx, scan, neighbors, fc, counts, dqcoeff, tokenCache[:])
	case common.Tx16x16:
		var tokenCache [256]uint8
		return decodeCoefsWithCountsScratch(r, txSize, planeType, isInter,
			dequant, ctx, scan, neighbors, fc, counts, dqcoeff, tokenCache[:])
	default:
		var tokenCache [1024]uint8
		return decodeCoefsWithCountsScratch(r, txSize, planeType, isInter,
			dequant, ctx, scan, neighbors, fc, counts, dqcoeff, tokenCache[:])
	}
}

// DecodeCoefsWithCountsScratch is DecodeCoefsWithCounts using caller-owned
// token-cache storage. The cache does not need clearing; libvpx's decode_coefs
// overwrites every scan position before get_coef_context can read it.
func DecodeCoefsWithCountsScratch(
	r *bitstream.Reader,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	ctx int,
	scan, neighbors []int16,
	fc *FrameCoefProbs,
	counts *CoefCounts,
	dqcoeff []int16,
	tokenCache *[1024]uint8,
) int {
	if tokenCache == nil {
		return DecodeCoefsWithCounts(r, txSize, planeType, isInter, dequant,
			ctx, scan, neighbors, fc, counts, dqcoeff)
	}
	maxEob := maxEobForTxSize(txSize)
	return decodeCoefsWithCountsScratch(r, txSize, planeType, isInter,
		dequant, ctx, scan, neighbors, fc, counts, dqcoeff,
		tokenCache[:maxEob])
}

func decodeCoefsWithCountsScratch(
	r *bitstream.Reader,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	ctx int,
	scan, neighbors []int16,
	fc *FrameCoefProbs,
	counts *CoefCounts,
	dqcoeff []int16,
	tokenCache []uint8,
) int {
	maxEob := maxEobForTxSize(txSize)
	bandTrans := bandTranslateForTxSize(txSize)
	dqShift := uint(0)
	if txSize == common.Tx32x32 {
		dqShift = 1
	}
	dqv := dequant[0]

	coefModel := &fc[txSize][planeType][isInter]

	c := 0
	bandIdx := 0

	for c < maxEob {
		band := int(bandTrans[bandIdx])
		bandIdx++
		probs := &coefModel[band][ctx]

		// EOB node — bail out of the loop entirely if the bit is 0.
		if counts != nil {
			counts.EobBranch[txSize][planeType][isInter][band][ctx]++
		}
		if r.Read(uint32(probs[eobContextNode])) == 0 {
			if counts != nil {
				counts.Coef[txSize][planeType][isInter][band][ctx][eobModelToken]++
			}
			break
		}

		// ZERO node — runs of zero tokens.
		for r.Read(uint32(probs[zeroContextNode])) == 0 {
			if counts != nil {
				counts.Coef[txSize][planeType][isInter][band][ctx][zeroToken]++
			}
			dqv = dequant[1]
			tokenCache[scan[c]] = 0
			c++
			if c >= maxEob {
				return c
			}
			ctx = getCoefContext(neighbors, tokenCache, c)
			band = int(bandTrans[bandIdx])
			bandIdx++
			probs = &coefModel[band][ctx]
		}

		var val int
		if r.Read(uint32(probs[oneContextNode])) != 0 {
			// Token >= 2 — read the Pareto8 tail.
			if counts != nil {
				counts.Coef[txSize][planeType][isInter][band][ctx][twoToken]++
			}
			p := &tables.Pareto8Full[probs[pivotNode]-1]
			if r.Read(uint32(p[0])) != 0 {
				if r.Read(uint32(p[3])) != 0 {
					tokenCache[scan[c]] = 5
					if r.Read(uint32(p[5])) != 0 {
						if r.Read(uint32(p[7])) != 0 {
							val = cat6MinVal + readCoeffBits(r, tables.Cat6Prob[:], cat6Bits8)
						} else {
							val = cat5MinVal + readCoeffBits(r, tables.Cat5Prob[:], 5)
						}
					} else if r.Read(uint32(p[6])) != 0 {
						val = cat4MinVal + readCoeffBits(r, tables.Cat4Prob[:], 4)
					} else {
						val = cat3MinVal + readCoeffBits(r, tables.Cat3Prob[:], 3)
					}
				} else {
					tokenCache[scan[c]] = 4
					if r.Read(uint32(p[4])) != 0 {
						val = cat2MinVal + readCoeffBits(r, tables.Cat2Prob[:], 2)
					} else {
						val = cat1MinVal + readCoeffBits(r, tables.Cat1Prob[:], 1)
					}
				}
				v := (val * int(dqv)) >> dqShift
				if r.ReadBit() != 0 {
					v = -v
				}
				dqcoeff[scan[c]] = int16(v)
			} else {
				if r.Read(uint32(p[1])) != 0 {
					tokenCache[scan[c]] = 3
					v := ((3 + int(r.Read(uint32(p[2])))) * int(dqv)) >> dqShift
					if r.ReadBit() != 0 {
						v = -v
					}
					dqcoeff[scan[c]] = int16(v)
				} else {
					tokenCache[scan[c]] = 2
					v := (2 * int(dqv)) >> dqShift
					if r.ReadBit() != 0 {
						v = -v
					}
					dqcoeff[scan[c]] = int16(v)
				}
			}
		} else {
			// Token == 1 — magnitude is exactly the AC/DC dequant value.
			if counts != nil {
				counts.Coef[txSize][planeType][isInter][band][ctx][oneToken]++
			}
			tokenCache[scan[c]] = 1
			v := int(dqv) >> dqShift
			if r.ReadBit() != 0 {
				v = -v
			}
			dqcoeff[scan[c]] = int16(v)
		}

		c++
		ctx = getCoefContext(neighbors, tokenCache, c)
		dqv = dequant[1]
	}

	return c
}
