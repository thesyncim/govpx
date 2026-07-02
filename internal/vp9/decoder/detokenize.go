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
// Structure: the token loop exists twice. The fast variants
// (decodeCoefsReaderState / decodeCoefsWithCountsReaderState) run while
// the read position stays at least coefFastMargin bytes away from the
// end of the tile buffer; inside that window every boolean-coder refill
// is the inlined bitstream.FillFast (one unconditional 8-byte load), so
// the loop body contains no function calls at all and Go keeps the whole
// decoder state — value/range/count/pos plus scan context — in
// registers. This is the register discipline libvpx gets for free from C
// callee-saved registers around its cold vpx_reader_fill call; Go's ABI
// has no callee-saved registers, so any call site inside the loop would
// force every loop-carried value into a stack slot. The last few bytes
// of each tile fall through to decodeCoefsSlowTail, which resumes
// mid-block (even mid zero-run) and uses the general, sentinel-aware
// refill.
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

// coefFastMargin is how many bytes of headroom the call-free token loop
// keeps between the read position and the end of the tile buffer. One
// token consumes at most ~22 boolean reads (EOB + zero + one + 4 Pareto
// nodes + 14 cat6 bits + sign) of at most 8 bits each, and each refill
// advances the position by at most 8 bytes while topping the register up
// to at least 48 bits, so between two guard checks the position moves by
// well under 48 bytes. 64 leaves slack; the residue of the buffer is
// decoded by the resumable slow tail.
const coefFastMargin = 64

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

// coefbandTrans4x4Padded is CoefbandTrans4x4 widened to 1024 entries so
// the token loops can index one *[1024]uint8 band table with `scan
// position & 1023` for every transform size without a bounds check.
// Positions 16..1023 are unreachable for 4x4 blocks (max_eob is 16).
var coefbandTrans4x4Padded = func() [1024]uint8 {
	var t [1024]uint8
	copy(t[:], tables.CoefbandTrans4x4[:])
	return t
}()

// bandTranslatePadded selects the scan-position → coef-band map as a
// full-width array pointer. Mirrors get_band_translate in vp9_entropy.h.
func bandTranslatePadded(tx common.TxSize) *[1024]uint8 {
	if tx == common.Tx4x4 {
		return &coefbandTrans4x4Padded
	}
	return &tables.CoefbandTrans8x8Plus
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
func readCoeffBits(r *bitstream.ReaderState, probs []uint8, n int,
	value uint64, rng uint32, count int32,
) (int, uint64, uint32, int32) {
	val := 0
	for i := range n {
		if count < 0 {
			r.Fill(&value, &count)
		}
		var bit uint32
		bit, value, rng, count = vpxReadNoFill(uint32(probs[i]), value, rng, count)
		val = (val << 1) | int(bit)
	}
	return val, value, rng, count
}

// vpxReadNoFill is the arithmetic body of vpx_read (vpx_dsp/bitreader.h)
// after the vpx_reader_fill step. Callers perform libvpx's
// `if (r->count < 0) vpx_reader_fill(r)` check first; splitting the rare
// refill out keeps this body within the Go inliner budget so per-token
// reads stay call-free, matching libvpx's static INLINE vpx_read.
func vpxReadNoFill(prob uint32,
	value uint64, rng uint32, count int32,
) (uint32, uint64, uint32, int32) {
	split := (rng*prob + (256 - prob)) >> 8
	bigsplit := uint64(split) << (64 - 8)
	nextRange := split
	var bit uint32
	if value >= bigsplit {
		nextRange = rng - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(tables.VpxNorm[byte(nextRange)])
	return bit, value << shift, nextRange << shift, count - int32(shift)
}

// vpxReadBitNoFill is vpxReadNoFill at prob=128 — one equally-likely
// bit, mirroring vpx_read_bit. The caller performs the fill check.
func vpxReadBitNoFill(
	value uint64, rng uint32, count int32,
) (uint32, uint64, uint32, int32) {
	split := (rng + 1) >> 1
	bigsplit := uint64(split) << (64 - 8)
	nextRange := split
	var bit uint32
	if value >= bigsplit {
		nextRange = rng - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(tables.VpxNorm[byte(nextRange)])
	return bit, value << shift, nextRange << shift, count - int32(shift)
}

// getCoefContext mirrors get_coef_context from vp9_scan.h. Looks up
// the two neighbor scan positions and averages their cached token
// magnitudes, +1, >> 1.
func getCoefContext(neighbors []int16, tokenCache []uint8, c int) int {
	a := tokenCache[neighbors[2*c+0]]
	b := tokenCache[neighbors[2*c+1]]
	return (1 + int(a) + int(b)) >> 1
}

// getCoefContextArr is getCoefContext against the full token-cache
// array. Neighbor entries are trusted scan positions (< 1024, mirroring
// libvpx's unchecked table indexing); the mask makes that explicit to
// the compiler so the dependent load carries no bounds check.
func getCoefContextArr(neighbors []int16, tokenCache *[1024]uint8, c int) int {
	a := tokenCache[int(neighbors[2*c+0])&1023]
	b := tokenCache[int(neighbors[2*c+1])&1023]
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
	// Slice-facing compatibility shim over the array-pointer hot path:
	// stage the caller's coefficients in a full-size array, decode, and
	// copy the touched prefix back.
	var tokenCache [1024]uint8
	var staged [1024]int16
	maxEob := maxEobForTxSize(txSize)
	n := min(len(dqcoeff), maxEob)
	copy(staged[:n], dqcoeff[:n])
	eob := DecodeCoefsWithCountsScratch(r, txSize, planeType, isInter,
		dequant, ctx, scan, neighbors, fc, counts, &staged, &tokenCache)
	copy(dqcoeff[:n], staged[:n])
	return eob
}

// DecodeCoefsWithCountsScratch is DecodeCoefsWithCounts using caller-owned
// token-cache and coefficient storage. The cache does not need clearing;
// libvpx's decode_coefs overwrites every scan position before
// get_coef_context can read it. dqcoeff is written for scan positions
// 0..eob-1 only.
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
	dqcoeff *[1024]int16,
	tokenCache *[1024]uint8,
) int {
	rs := r.LocalState()
	var eob int
	if counts == nil {
		eob = decodeCoefsReaderState(&rs, txSize, planeType, isInter,
			dequant, ctx, scan, neighbors, fc, dqcoeff, tokenCache)
	} else {
		eob = decodeCoefsWithCountsReaderState(&rs, txSize, planeType, isInter,
			dequant, ctx, scan, neighbors, fc, counts, dqcoeff, tokenCache)
	}
	rs.Commit()
	return eob
}

// DecodeCoefsState is DecodeCoefsWithCountsScratch against an already
// materialized ReaderState, letting callers that decode many transform
// blocks in a row (the per-superblock residue loop) snapshot the reader
// once instead of copying reader state in and out per block. The caller
// owns the Commit.
func DecodeCoefsState(
	rs *bitstream.ReaderState,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	ctx int,
	so *common.ScanOrder,
	fc *FrameCoefProbs,
	counts *CoefCounts,
	dqcoeff *[1024]int16,
	tokenCache *[1024]uint8,
) int {
	if counts == nil {
		return decodeCoefsReaderState(rs, txSize, planeType, isInter,
			dequant, ctx, so.Scan, so.Neighbors, fc, dqcoeff, tokenCache)
	}
	return decodeCoefsWithCountsReaderState(rs, txSize, planeType, isInter,
		dequant, ctx, so.Scan, so.Neighbors, fc, counts, dqcoeff, tokenCache)
}

// decodeCoefsReaderState is the call-free fast token loop (counts-less,
// frame-parallel streams). See the package comment block above for the
// fast/slow structure.
func decodeCoefsReaderState(
	r *bitstream.ReaderState,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	ctx int,
	scan, neighbors []int16,
	fc *FrameCoefProbs,
	dqcoeff *[1024]int16,
	tokenCache *[1024]uint8,
) int {
	maxEob := maxEobForTxSize(txSize)
	bt := bandTranslatePadded(txSize)
	dqShift := uint(0)
	if txSize == common.Tx32x32 {
		dqShift = 1
	}
	dqv := dequant[0]
	dqvAC := dequant[1]

	coefModel := &fc[txSize][planeType][isInter]

	buf, _, _, pos := r.BitView()
	fastLimit := len(buf) - coefFastMargin
	value := r.Value
	rng := r.Range
	count := r.Count

	c := 0
	slowResume := false
	skipEob := false

fastLoop:
	for c < maxEob {
		if pos > fastLimit {
			slowResume = true
			break
		}
		band := int(bt[c&1023])
		probs := &coefModel[band][ctx]

		if count < 0 {
			value, count, pos = bitstream.FillFast(buf, pos, value, count)
		}
		var bit uint32
		bit, value, rng, count = vpxReadNoFill(uint32(probs[eobContextNode]),
			value, rng, count)
		if bit == 0 {
			break
		}

		if count < 0 {
			value, count, pos = bitstream.FillFast(buf, pos, value, count)
		}
		bit, value, rng, count = vpxReadNoFill(uint32(probs[zeroContextNode]),
			value, rng, count)
		for bit == 0 {
			dqv = dqvAC
			tokenCache[int(scan[c])&1023] = 0
			c++
			if c >= maxEob {
				break fastLoop
			}
			ctx = getCoefContextArr(neighbors, tokenCache, c)
			band = int(bt[c&1023])
			probs = &coefModel[band][ctx]
			if pos > fastLimit {
				slowResume = true
				skipEob = true
				break fastLoop
			}
			if count < 0 {
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(probs[zeroContextNode]),
				value, rng, count)
		}

		var val int
		if count < 0 {
			value, count, pos = bitstream.FillFast(buf, pos, value, count)
		}
		bit, value, rng, count = vpxReadNoFill(uint32(probs[oneContextNode]),
			value, rng, count)
		if bit != 0 {
			p := &tables.Pareto8Full[probs[pivotNode]-1]
			if count < 0 {
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(p[0]), value, rng,
				count)
			if bit != 0 {
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				bit, value, rng, count = vpxReadNoFill(uint32(p[3]), value,
					rng, count)
				if bit != 0 {
					tokenCache[int(scan[c])&1023] = 5
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[5]),
						value, rng, count)
					if bit != 0 {
						if count < 0 {
							value, count, pos = bitstream.FillFast(buf, pos, value, count)
						}
						bit, value, rng, count = vpxReadNoFill(uint32(p[7]),
							value, rng, count)
						if bit != 0 {
							val = cat6MinVal
							for i := range cat6Bits8 {
								if count < 0 {
									value, count, pos = bitstream.FillFast(buf, pos, value, count)
								}
								bit, value, rng, count = vpxReadNoFill(
									uint32(tables.Cat6Prob[i]), value, rng, count)
								val += val - cat6MinVal + int(bit)
							}
						} else {
							val = cat5MinVal
							for i := range 5 {
								if count < 0 {
									value, count, pos = bitstream.FillFast(buf, pos, value, count)
								}
								bit, value, rng, count = vpxReadNoFill(
									uint32(tables.Cat5Prob[i]), value, rng, count)
								val += val - cat5MinVal + int(bit)
							}
						}
					} else {
						if count < 0 {
							value, count, pos = bitstream.FillFast(buf, pos, value, count)
						}
						bit, value, rng, count = vpxReadNoFill(uint32(p[6]),
							value, rng, count)
						if bit != 0 {
							val = cat4MinVal
							for i := range 4 {
								if count < 0 {
									value, count, pos = bitstream.FillFast(buf, pos, value, count)
								}
								bit, value, rng, count = vpxReadNoFill(
									uint32(tables.Cat4Prob[i]), value, rng, count)
								val += val - cat4MinVal + int(bit)
							}
						} else {
							val = cat3MinVal
							for i := range 3 {
								if count < 0 {
									value, count, pos = bitstream.FillFast(buf, pos, value, count)
								}
								bit, value, rng, count = vpxReadNoFill(
									uint32(tables.Cat3Prob[i]), value, rng, count)
								val += val - cat3MinVal + int(bit)
							}
						}
					}
				} else {
					tokenCache[int(scan[c])&1023] = 4
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[4]),
						value, rng, count)
					if bit != 0 {
						val = cat2MinVal
						for i := range 2 {
							if count < 0 {
								value, count, pos = bitstream.FillFast(buf, pos, value, count)
							}
							bit, value, rng, count = vpxReadNoFill(
								uint32(tables.Cat2Prob[i]), value, rng, count)
							val += val - cat2MinVal + int(bit)
						}
					} else {
						if count < 0 {
							value, count, pos = bitstream.FillFast(buf, pos, value, count)
						}
						bit, value, rng, count = vpxReadNoFill(
							uint32(tables.Cat1Prob[0]), value, rng, count)
						val = cat1MinVal + int(bit)
					}
				}
				v := (val * int(dqv)) >> dqShift
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
				if bit != 0 {
					v = -v
				}
				dqcoeff[int(scan[c])&1023] = int16(v)
			} else {
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				bit, value, rng, count = vpxReadNoFill(uint32(p[1]), value,
					rng, count)
				if bit != 0 {
					tokenCache[int(scan[c])&1023] = 3
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[2]),
						value, rng, count)
					v := ((3 + int(bit)) * int(dqv)) >> dqShift
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
					if bit != 0 {
						v = -v
					}
					dqcoeff[int(scan[c])&1023] = int16(v)
				} else {
					tokenCache[int(scan[c])&1023] = 2
					v := (2 * int(dqv)) >> dqShift
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
					if bit != 0 {
						v = -v
					}
					dqcoeff[int(scan[c])&1023] = int16(v)
				}
			}
		} else {
			tokenCache[int(scan[c])&1023] = 1
			v := int(dqv) >> dqShift
			if count < 0 {
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
			if bit != 0 {
				v = -v
			}
			dqcoeff[int(scan[c])&1023] = int16(v)
		}

		c++
		ctx = getCoefContextArr(neighbors, tokenCache, c)
		dqv = dqvAC
	}

	r.Value = value
	r.Range = rng
	r.Count = count
	r.CommitPos(pos)
	if !slowResume {
		return c
	}
	return decodeCoefsSlowTail(r, txSize, planeType, isInter, dequant,
		scan, neighbors, fc, nil, dqcoeff, tokenCache, c, ctx, dqv, skipEob)
}

// decodeCoefsWithCountsReaderState is decodeCoefsReaderState plus the
// libvpx FRAME_COUNTS updates used by counts-driven (non-frame-parallel)
// probability adaptation. Kept as a separate copy so the counts-less
// loop pays nothing for the increments.
func decodeCoefsWithCountsReaderState(
	r *bitstream.ReaderState,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	ctx int,
	scan, neighbors []int16,
	fc *FrameCoefProbs,
	counts *CoefCounts,
	dqcoeff *[1024]int16,
	tokenCache *[1024]uint8,
) int {
	maxEob := maxEobForTxSize(txSize)
	bt := bandTranslatePadded(txSize)
	dqShift := uint(0)
	if txSize == common.Tx32x32 {
		dqShift = 1
	}
	dqv := dequant[0]
	dqvAC := dequant[1]

	coefModel := &fc[txSize][planeType][isInter]
	eobBranch := &counts.EobBranch[txSize][planeType][isInter]
	coefCount := &counts.Coef[txSize][planeType][isInter]

	buf, _, _, pos := r.BitView()
	fastLimit := len(buf) - coefFastMargin
	value := r.Value
	rng := r.Range
	count := r.Count

	c := 0
	slowResume := false
	skipEob := false

fastLoop:
	for c < maxEob {
		if pos > fastLimit {
			slowResume = true
			break
		}
		band := int(bt[c&1023])
		probs := &coefModel[band][ctx]

		eobBranch[band][ctx]++
		if count < 0 {
			value, count, pos = bitstream.FillFast(buf, pos, value, count)
		}
		var bit uint32
		bit, value, rng, count = vpxReadNoFill(uint32(probs[eobContextNode]),
			value, rng, count)
		if bit == 0 {
			coefCount[band][ctx][eobModelToken]++
			break
		}

		if count < 0 {
			value, count, pos = bitstream.FillFast(buf, pos, value, count)
		}
		bit, value, rng, count = vpxReadNoFill(uint32(probs[zeroContextNode]),
			value, rng, count)
		for bit == 0 {
			coefCount[band][ctx][zeroToken]++
			dqv = dqvAC
			tokenCache[int(scan[c])&1023] = 0
			c++
			if c >= maxEob {
				break fastLoop
			}
			ctx = getCoefContextArr(neighbors, tokenCache, c)
			band = int(bt[c&1023])
			probs = &coefModel[band][ctx]
			if pos > fastLimit {
				slowResume = true
				skipEob = true
				break fastLoop
			}
			if count < 0 {
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(probs[zeroContextNode]),
				value, rng, count)
		}

		var val int
		if count < 0 {
			value, count, pos = bitstream.FillFast(buf, pos, value, count)
		}
		bit, value, rng, count = vpxReadNoFill(uint32(probs[oneContextNode]),
			value, rng, count)
		if bit != 0 {
			coefCount[band][ctx][twoToken]++
			p := &tables.Pareto8Full[probs[pivotNode]-1]
			if count < 0 {
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(p[0]), value, rng,
				count)
			if bit != 0 {
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				bit, value, rng, count = vpxReadNoFill(uint32(p[3]), value,
					rng, count)
				if bit != 0 {
					tokenCache[int(scan[c])&1023] = 5
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[5]),
						value, rng, count)
					if bit != 0 {
						if count < 0 {
							value, count, pos = bitstream.FillFast(buf, pos, value, count)
						}
						bit, value, rng, count = vpxReadNoFill(uint32(p[7]),
							value, rng, count)
						if bit != 0 {
							val = cat6MinVal
							for i := range cat6Bits8 {
								if count < 0 {
									value, count, pos = bitstream.FillFast(buf, pos, value, count)
								}
								bit, value, rng, count = vpxReadNoFill(
									uint32(tables.Cat6Prob[i]), value, rng, count)
								val += val - cat6MinVal + int(bit)
							}
						} else {
							val = cat5MinVal
							for i := range 5 {
								if count < 0 {
									value, count, pos = bitstream.FillFast(buf, pos, value, count)
								}
								bit, value, rng, count = vpxReadNoFill(
									uint32(tables.Cat5Prob[i]), value, rng, count)
								val += val - cat5MinVal + int(bit)
							}
						}
					} else {
						if count < 0 {
							value, count, pos = bitstream.FillFast(buf, pos, value, count)
						}
						bit, value, rng, count = vpxReadNoFill(uint32(p[6]),
							value, rng, count)
						if bit != 0 {
							val = cat4MinVal
							for i := range 4 {
								if count < 0 {
									value, count, pos = bitstream.FillFast(buf, pos, value, count)
								}
								bit, value, rng, count = vpxReadNoFill(
									uint32(tables.Cat4Prob[i]), value, rng, count)
								val += val - cat4MinVal + int(bit)
							}
						} else {
							val = cat3MinVal
							for i := range 3 {
								if count < 0 {
									value, count, pos = bitstream.FillFast(buf, pos, value, count)
								}
								bit, value, rng, count = vpxReadNoFill(
									uint32(tables.Cat3Prob[i]), value, rng, count)
								val += val - cat3MinVal + int(bit)
							}
						}
					}
				} else {
					tokenCache[int(scan[c])&1023] = 4
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[4]),
						value, rng, count)
					if bit != 0 {
						val = cat2MinVal
						for i := range 2 {
							if count < 0 {
								value, count, pos = bitstream.FillFast(buf, pos, value, count)
							}
							bit, value, rng, count = vpxReadNoFill(
								uint32(tables.Cat2Prob[i]), value, rng, count)
							val += val - cat2MinVal + int(bit)
						}
					} else {
						if count < 0 {
							value, count, pos = bitstream.FillFast(buf, pos, value, count)
						}
						bit, value, rng, count = vpxReadNoFill(
							uint32(tables.Cat1Prob[0]), value, rng, count)
						val = cat1MinVal + int(bit)
					}
				}
				v := (val * int(dqv)) >> dqShift
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
				if bit != 0 {
					v = -v
				}
				dqcoeff[int(scan[c])&1023] = int16(v)
			} else {
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				bit, value, rng, count = vpxReadNoFill(uint32(p[1]), value,
					rng, count)
				if bit != 0 {
					tokenCache[int(scan[c])&1023] = 3
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[2]),
						value, rng, count)
					v := ((3 + int(bit)) * int(dqv)) >> dqShift
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
					if bit != 0 {
						v = -v
					}
					dqcoeff[int(scan[c])&1023] = int16(v)
				} else {
					tokenCache[int(scan[c])&1023] = 2
					v := (2 * int(dqv)) >> dqShift
					if count < 0 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
					if bit != 0 {
						v = -v
					}
					dqcoeff[int(scan[c])&1023] = int16(v)
				}
			}
		} else {
			coefCount[band][ctx][oneToken]++
			tokenCache[int(scan[c])&1023] = 1
			v := int(dqv) >> dqShift
			if count < 0 {
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
			if bit != 0 {
				v = -v
			}
			dqcoeff[int(scan[c])&1023] = int16(v)
		}

		c++
		ctx = getCoefContextArr(neighbors, tokenCache, c)
		dqv = dqvAC
	}

	r.Value = value
	r.Range = rng
	r.Count = count
	r.CommitPos(pos)
	if !slowResume {
		return c
	}
	return decodeCoefsSlowTail(r, txSize, planeType, isInter, dequant,
		scan, neighbors, fc, counts, dqcoeff, tokenCache, c, ctx, dqv, skipEob)
}

// decodeCoefsSlowTail finishes a coefficient block once the fast loops
// run out of buffer headroom. It resumes at scan position c with the
// given band context and dequant value; skipEob resumes mid zero-run
// (the EOB branch for the current token was already decoded — and, for
// the counts variant, already counted — by the fast loop). The refills
// here go through the general sentinel-aware path, so it also handles
// end-of-stream exactly like libvpx's byte-loop vpx_reader_fill.
func decodeCoefsSlowTail(
	r *bitstream.ReaderState,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	scan, neighbors []int16,
	fc *FrameCoefProbs,
	counts *CoefCounts,
	dqcoeff *[1024]int16,
	tokenCache *[1024]uint8,
	c, ctx int,
	dqv int16,
	skipEob bool,
) int {
	maxEob := maxEobForTxSize(txSize)
	bt := bandTranslatePadded(txSize)
	dqShift := uint(0)
	if txSize == common.Tx32x32 {
		dqShift = 1
	}

	coefModel := &fc[txSize][planeType][isInter]

	value := r.Value
	rng := r.Range
	count := r.Count

	for c < maxEob {
		band := int(bt[c&1023])
		probs := &coefModel[band][ctx]

		var bit uint32
		if !skipEob {
			// EOB node — bail out of the loop entirely if the bit is 0.
			if counts != nil {
				counts.EobBranch[txSize][planeType][isInter][band][ctx]++
			}
			if count < 0 {
				r.Fill(&value, &count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(probs[eobContextNode]), value, rng, count)
			if bit == 0 {
				if counts != nil {
					counts.Coef[txSize][planeType][isInter][band][ctx][eobModelToken]++
				}
				break
			}
		}
		skipEob = false

		// ZERO node — runs of zero tokens.
		if count < 0 {
			r.Fill(&value, &count)
		}
		bit, value, rng, count = vpxReadNoFill(uint32(probs[zeroContextNode]), value, rng, count)
		for bit == 0 {
			if counts != nil {
				counts.Coef[txSize][planeType][isInter][band][ctx][zeroToken]++
			}
			dqv = dequant[1]
			tokenCache[int(scan[c])&1023] = 0
			c++
			if c >= maxEob {
				r.Value = value
				r.Range = rng
				r.Count = count
				return c
			}
			ctx = getCoefContextArr(neighbors, tokenCache, c)
			band = int(bt[c&1023])
			probs = &coefModel[band][ctx]
			if count < 0 {
				r.Fill(&value, &count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(probs[zeroContextNode]), value, rng, count)
		}

		var val int
		if count < 0 {
			r.Fill(&value, &count)
		}
		bit, value, rng, count = vpxReadNoFill(uint32(probs[oneContextNode]), value, rng, count)
		if bit != 0 {
			// Token >= 2 — read the Pareto8 tail.
			if counts != nil {
				counts.Coef[txSize][planeType][isInter][band][ctx][twoToken]++
			}
			p := &tables.Pareto8Full[probs[pivotNode]-1]
			if count < 0 {
				r.Fill(&value, &count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(p[0]), value, rng, count)
			if bit != 0 {
				if count < 0 {
					r.Fill(&value, &count)
				}
				bit, value, rng, count = vpxReadNoFill(uint32(p[3]), value, rng, count)
				if bit != 0 {
					tokenCache[int(scan[c])&1023] = 5
					if count < 0 {
						r.Fill(&value, &count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[5]), value, rng, count)
					if bit != 0 {
						if count < 0 {
							r.Fill(&value, &count)
						}
						bit, value, rng, count = vpxReadNoFill(uint32(p[7]), value, rng, count)
						if bit != 0 {
							var extra int
							extra, value, rng, count = readCoeffBits(r, tables.Cat6Prob[:], cat6Bits8, value, rng, count)
							val = cat6MinVal + extra
						} else {
							var extra int
							extra, value, rng, count = readCoeffBits(r, tables.Cat5Prob[:], 5, value, rng, count)
							val = cat5MinVal + extra
						}
					} else {
						if count < 0 {
							r.Fill(&value, &count)
						}
						bit, value, rng, count = vpxReadNoFill(uint32(p[6]), value, rng, count)
						if bit != 0 {
							var extra int
							extra, value, rng, count = readCoeffBits(r, tables.Cat4Prob[:], 4, value, rng, count)
							val = cat4MinVal + extra
						} else {
							var extra int
							extra, value, rng, count = readCoeffBits(r, tables.Cat3Prob[:], 3, value, rng, count)
							val = cat3MinVal + extra
						}
					}
				} else {
					tokenCache[int(scan[c])&1023] = 4
					if count < 0 {
						r.Fill(&value, &count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[4]), value, rng, count)
					if bit != 0 {
						var extra int
						extra, value, rng, count = readCoeffBits(r, tables.Cat2Prob[:], 2, value, rng, count)
						val = cat2MinVal + extra
					} else {
						var extra int
						extra, value, rng, count = readCoeffBits(r, tables.Cat1Prob[:], 1, value, rng, count)
						val = cat1MinVal + extra
					}
				}
				v := (val * int(dqv)) >> dqShift
				if count < 0 {
					r.Fill(&value, &count)
				}
				bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
				if bit != 0 {
					v = -v
				}
				dqcoeff[int(scan[c])&1023] = int16(v)
			} else {
				if count < 0 {
					r.Fill(&value, &count)
				}
				bit, value, rng, count = vpxReadNoFill(uint32(p[1]), value, rng, count)
				if bit != 0 {
					tokenCache[int(scan[c])&1023] = 3
					if count < 0 {
						r.Fill(&value, &count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[2]), value, rng, count)
					v := ((3 + int(bit)) * int(dqv)) >> dqShift
					if count < 0 {
						r.Fill(&value, &count)
					}
					bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
					if bit != 0 {
						v = -v
					}
					dqcoeff[int(scan[c])&1023] = int16(v)
				} else {
					tokenCache[int(scan[c])&1023] = 2
					v := (2 * int(dqv)) >> dqShift
					if count < 0 {
						r.Fill(&value, &count)
					}
					bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
					if bit != 0 {
						v = -v
					}
					dqcoeff[int(scan[c])&1023] = int16(v)
				}
			}
		} else {
			// Token == 1 — magnitude is exactly the AC/DC dequant value.
			if counts != nil {
				counts.Coef[txSize][planeType][isInter][band][ctx][oneToken]++
			}
			tokenCache[int(scan[c])&1023] = 1
			v := int(dqv) >> dqShift
			if count < 0 {
				r.Fill(&value, &count)
			}
			bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
			if bit != 0 {
				v = -v
			}
			dqcoeff[int(scan[c])&1023] = int16(v)
		}

		c++
		ctx = getCoefContextArr(neighbors, tokenCache, c)
		dqv = dequant[1]
	}

	r.Value = value
	r.Range = rng
	r.Count = count
	return c
}
