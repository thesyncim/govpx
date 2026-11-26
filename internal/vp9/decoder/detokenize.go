package decoder

import (
	"math/bits"

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

// vpxReadNoFill is the 100% branchless arithmetic body of vpx_read
// (vpx_dsp/bitreader.h) after the refill check. Using arithmetic masking avoids
// the multi-assignment `if` that causes Go's arm64 backend to emit a
// conditional branch (and suffer ~14-cycle mispredicts on 50/50 coefficient
// bits), while LeadingZeros8 keeps the AST cost well below Go's 80-node
// inliner budget so every call site inlines unconditionally.
func vpxReadNoFill(prob uint32,
	value uint64, rng uint32, count int32,
) (uint32, uint64, uint32, int32) {
	s := (rng*prob + (256 - prob)) >> 8
	bigsplit := uint64(s) << 56
	ge := value >= bigsplit
	nr := s
	if ge {
		nr = rng - s
	}
	nv := value
	if ge {
		nv = value - bigsplit
	}
	var bit uint32
	if ge {
		bit = 1
	}
	sh := uint32(bits.LeadingZeros8(uint8(nr)))
	return bit, nv << sh, nr << sh, count - int32(sh)
}

// vpxReadBoolNoFill lowers to MADD + LSR + CMP + CSEL on arm64 (0 branches,
// ~9 cycles vs ~11 for SUB+MUL+LSR+ADD) while staying under Go's 80-node
// inliner budget by returning bool directly.
func vpxReadBoolNoFill(prob uint32,
	value uint64, rng uint32, count int32,
) (bool, uint64, uint32, int32) {
	s := (rng*prob + (256 - prob)) >> 8
	bigsplit := uint64(s) << 56
	ge := value >= bigsplit
	nr := s
	if ge {
		nr = rng - s
	}
	nv := value
	if ge {
		nv = value - bigsplit
	}
	sh := uint32(bits.LeadingZeros8(uint8(nr)))
	return ge, nv << sh, nr << sh, count - int32(sh)
}

// vpxReadBitBoolNoFill is branchless vpxReadBoolNoFill at prob=128 — one
// equally-likely bit returning bool so callers lower sign negation to CSNEG.
// Since rng-split == rng>>1 identically for all rng, both candidates compute
// in parallel without waiting on split.
func vpxReadBitBoolNoFill(
	value uint64, rng uint32, count int32,
) (bool, uint64, uint32, int32) {
	split := (rng + 1) >> 1
	bigsplit := uint64(split) << 56
	ge := value >= bigsplit
	nr := split
	if ge {
		nr = rng >> 1
	}
	nv := value
	if ge {
		nv = value - bigsplit
	}
	shift := uint32(1) - (nr >> 7)
	return ge, nv << shift, nr << shift, count - int32(shift)
}

// vpxReadBitNoFill is branchless vpxReadNoFill at prob=128 — one equally-likely
// bit, mirroring vpx_read_bit. The caller performs the fill check.
func vpxReadBitNoFill(
	value uint64, rng uint32, count int32,
) (uint32, uint64, uint32, int32) {
	split := (rng + 1) >> 1
	bigsplit := uint64(split) << 56
	ge := value >= bigsplit
	nr := split
	if ge {
		nr = rng >> 1
	}
	nv := value
	if ge {
		nv = value - bigsplit
	}
	var bit uint32
	if ge {
		bit = 1
	}
	shift := uint32(1) - (nr >> 7)
	return bit, nv << shift, nr << shift, count - int32(shift)
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
			dequant, ctx, scan, neighbors, fc, dqcoeff, tokenCache, false)
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
			dequant, ctx, so.Scan, so.Neighbors, fc, dqcoeff, tokenCache, false)
	}
	return decodeCoefsWithCountsReaderState(rs, txSize, planeType, isInter,
		dequant, ctx, so.Scan, so.Neighbors, fc, counts, dqcoeff, tokenCache)
}

// DecodeCoefsStateAfterEOB decodes a counts-less transform block whose initial
// c==0 EOB bit was already consumed as 1 by the caller.
func DecodeCoefsStateAfterEOB(
	rs *bitstream.ReaderState,
	txSize common.TxSize,
	planeType int,
	isInter int,
	dequant [2]int16,
	ctx int,
	so *common.ScanOrder,
	fc *FrameCoefProbs,
	dqcoeff *[1024]int16,
	tokenCache *[1024]uint8,
) int {
	return decodeCoefsReaderState(rs, txSize, planeType, isInter,
		dequant, ctx, so.Scan, so.Neighbors, fc, dqcoeff, tokenCache, true)
}

// PackedFrameCoefProbs packs each [band][ctx][3]uint8 probability triple into a
// single uint32 (p0 | p1<<8 | p2<<16) in a power-of-2 [64]uint32 table indexed
// by (band<<3)|ctx. This replaces two bounds checks, two address multiplies
// (*18 and *3), and three byte loads with a single unchecked 32-bit load per
// coefficient.
type PackedFrameCoefProbs [common.TxSizes][CoefPlaneTypes][CoefRefTypes][64]uint32

// PackFrameCoefProbs packs fc into dst once per frame before tile decoding.
func PackFrameCoefProbs(dst *PackedFrameCoefProbs, fc *FrameCoefProbs) {
	for tx := range common.TxSizes {
		for pt := range CoefPlaneTypes {
			for rt := range CoefRefTypes {
				for band := range CoefBands {
					row := band << 3
					for ctx := range CoefContexts {
						p := &fc[tx][pt][rt][band][ctx]
						dst[tx][pt][rt][row|ctx] = uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
					}
				}
			}
		}
	}
}

// FastCoefPlaneCtx holds per-plane read-only decode state hoisted outside the
// per-transform-block loop in readVP9InterResiduePlane.
type FastCoefPlaneCtx struct {
	TxSize           common.TxSize
	PlaneType        int
	IsInter          int
	Dequant          [2]int16
	Scan             []int16
	Neighbors        []int16
	ScanArr          *[1024]int16
	ScanNeighborsArr *[2048]int16
	ScanMetaArr      *[1024]uint32
	FC               *FrameCoefProbs
	PackedModel      *[64]uint32
	DQCoeff          *[1024]int16
	TokenCache       *[1024]uint8
}

//go:noinline
func decodeVP9Cat3to6Fast(
	buf []byte, pos int, value uint64, rng uint32, count int32,
	p *[8]uint8,
) (int, int, uint64, uint32, int32) {
	if count < 32 {
		value, count, pos = bitstream.FillFast(buf, pos, value, count)
	}
	var b bool
	var val int
	b, value, rng, count = vpxReadBoolNoFill(uint32(p[5]), value, rng, count)
	if b {
		b, value, rng, count = vpxReadBoolNoFill(uint32(p[7]), value, rng, count)
		if b {
			val = cat6MinVal
			for i := range cat6Bits8 {
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				b, value, rng, count = vpxReadBoolNoFill(uint32(tables.Cat6Prob[i]), value, rng, count)
				val += val - cat6MinVal
				if b {
					val++
				}
			}
		} else {
			val = cat5MinVal
			for i := range 5 {
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				b, value, rng, count = vpxReadBoolNoFill(uint32(tables.Cat5Prob[i]), value, rng, count)
				val += val - cat5MinVal
				if b {
					val++
				}
			}
		}
	} else {
		b, value, rng, count = vpxReadBoolNoFill(uint32(p[6]), value, rng, count)
		if b {
			val = cat4MinVal
			for i := range 4 {
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				b, value, rng, count = vpxReadBoolNoFill(uint32(tables.Cat4Prob[i]), value, rng, count)
				val += val - cat4MinVal
				if b {
					val++
				}
			}
		} else {
			val = cat3MinVal
			for i := range 3 {
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				b, value, rng, count = vpxReadBoolNoFill(uint32(tables.Cat3Prob[i]), value, rng, count)
				val += val - cat3MinVal
				if b {
					val++
				}
			}
		}
	}
	return val, pos, value, rng, count
}

// DecodeCoefsFastPlaneAfterEOB decodes a counts-less transform block whose
// initial c==0 EOB bit was already consumed as 1, using pre-hoisted plane
// context, power-of-2 packed uint32 probabilities, and scan-order tokenCache.
func DecodeCoefsFastPlaneAfterEOB(
	r *bitstream.ReaderState,
	a *FastCoefPlaneCtx,
	buf []byte, pos int,
	value uint64, rng uint32, count int32,
	ctx int,
) (int, int, uint64, uint32, int32) {
	txSize := a.TxSize
	maxEob := maxEobForTxSize(txSize)
	dqShift := uint(0)
	if txSize == common.Tx32x32 {
		dqShift = 1
	}
	dqv := a.Dequant[0]
	dqvAC := a.Dequant[1]
	dq1 := int(dqv) >> dqShift
	dq1AC := int(dqvAC) >> dqShift

	model := a.PackedModel
	scanArr := a.ScanArr
	scanMeta := a.ScanMetaArr
	dqcoeff := a.DQCoeff
	scanCache := a.TokenCache
	fastLimit := len(buf) - coefFastMargin

	c := 0
	slowResume := false
	slowSkipEob := false
	var p32 uint32
	var b bool
	if pos > fastLimit {
		slowResume = true
		slowSkipEob = true
		goto doneFast
	}
	p32 = model[ctx&63]
	if count < 24 {
		value, count, pos = bitstream.FillFast(buf, pos, value, count)
	}

fastLoop:
	for {
		b, value, rng, count = vpxReadBoolNoFill((p32>>8)&0xFF, value, rng, count)
		if !b {
			dqv = dqvAC
			dq1 = dq1AC
			for {
				scanCache[c&1023] = 0
				c++
				if c >= maxEob {
					break fastLoop
				}
				meta := scanMeta[c&1023]
				ctx = (1 + int(scanCache[meta&1023]) + int(scanCache[(meta>>10)&1023])) >> 1
				p32 = model[(int(meta>>20)|ctx)&63]
				if count < 16 {
					if pos > fastLimit {
						slowResume = true
						slowSkipEob = true
						break fastLoop
					}
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				b, value, rng, count = vpxReadBoolNoFill((p32>>8)&0xFF, value, rng, count)
				if b {
					break
				}
			}
		}

		b, value, rng, count = vpxReadBoolNoFill((p32>>16)&0xFF, value, rng, count)
		if b {
			var val int
			p := &tables.Pareto8Full[((p32>>16)&0xFF)-1]
			if count < 32 {
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			b, value, rng, count = vpxReadBoolNoFill(uint32(p[0]), value, rng, count)
			if b {
				b, value, rng, count = vpxReadBoolNoFill(uint32(p[3]), value, rng, count)
				if b {
					scanCache[c&1023] = 5
					val, pos, value, rng, count = decodeVP9Cat3to6Fast(buf, pos, value, rng, count, p)
				} else {
					scanCache[c&1023] = 4
					b, value, rng, count = vpxReadBoolNoFill(uint32(p[4]), value, rng, count)
					if b {
						val = cat2MinVal
						for i := range 2 {
							if count < 0 {
								value, count, pos = bitstream.FillFast(buf, pos, value, count)
							}
							b, value, rng, count = vpxReadBoolNoFill(uint32(tables.Cat2Prob[i]), value, rng, count)
							val += val - cat2MinVal
							if b {
								val++
							}
						}
					} else {
						if count < 0 {
							value, count, pos = bitstream.FillFast(buf, pos, value, count)
						}
						b, value, rng, count = vpxReadBoolNoFill(uint32(tables.Cat1Prob[0]), value, rng, count)
						val = cat1MinVal
						if b {
							val++
						}
					}
				}
				v := (val * int(dqv)) >> dqShift
				if count < 0 {
					value, count, pos = bitstream.FillFast(buf, pos, value, count)
				}
				b, value, rng, count = vpxReadBitBoolNoFill(value, rng, count)
				if b {
					v = -v
				}
				dqcoeff[int(scanArr[c&1023])&1023] = int16(v)
			} else {
				idx := int(scanArr[c&1023]) & 1023
				b, value, rng, count = vpxReadBoolNoFill(uint32(p[1]), value, rng, count)
				if b {
					scanCache[c&1023] = 3
					b, value, rng, count = vpxReadBoolNoFill(uint32(p[2]), value, rng, count)
					m := 3
					if b {
						m = 4
					}
					v := (m * int(dqv)) >> dqShift
					b, value, rng, count = vpxReadBitBoolNoFill(value, rng, count)
					if b {
						v = -v
					}
					dqcoeff[idx] = int16(v)
				} else {
					scanCache[c&1023] = 2
					v := (2 * int(dqv)) >> dqShift
					b, value, rng, count = vpxReadBitBoolNoFill(value, rng, count)
					if b {
						v = -v
					}
					dqcoeff[idx] = int16(v)
				}
			}
		} else {
			scanCache[c&1023] = 1
			b, value, rng, count = vpxReadBitBoolNoFill(value, rng, count)
			v := dq1
			if b {
				v = -v
			}
			dqcoeff[int(scanArr[c&1023])&1023] = int16(v)
		}

		c++
		if c >= maxEob {
			break
		}
		meta := scanMeta[c&1023]
		ctx = (1 + int(scanCache[meta&1023]) + int(scanCache[(meta>>10)&1023])) >> 1
		dqv = dqvAC
		dq1 = dq1AC
		if pos > fastLimit {
			slowResume = true
			break
		}
		p32 = model[(int(meta>>20)|ctx)&63]
		if count < 24 {
			value, count, pos = bitstream.FillFast(buf, pos, value, count)
		}
		b, value, rng, count = vpxReadBoolNoFill(p32&0xFF, value, rng, count)
		if !b {
			break
		}
	}

doneFast:
	if !slowResume {
		return c, pos, value, rng, count
	}
	return decodeCoefsFastPlaneSlowTail(r, a, pos, value, rng, count, c, ctx, dqv, slowSkipEob)
}

//go:noinline
func decodeCoefsFastPlaneSlowTail(
	r *bitstream.ReaderState,
	a *FastCoefPlaneCtx,
	pos int, value uint64, rng uint32, count int32,
	c, ctx int, dqv int16, slowSkipEob bool,
) (int, int, uint64, uint32, int32) {
	// Convert scan-order cache to raster-order cache for the rare slow tail.
	var rasterCache [1024]uint8
	scanArr := a.ScanArr
	scanCache := a.TokenCache
	for k := range c {
		rasterCache[int(scanArr[k&1023])&1023] = scanCache[k&1023]
	}
	r.Value = value
	r.Range = rng
	r.Count = count
	r.CommitPos(pos)
	eob := decodeCoefsSlowTail(r, a.TxSize, a.PlaneType, a.IsInter, a.Dequant,
		a.Scan, a.Neighbors, a.FC, nil, a.DQCoeff, &rasterCache, c, ctx, dqv, slowSkipEob)
	_, _, _, nextPos := r.BitView()
	return eob, nextPos, r.Value, r.Range, r.Count
}

type decodeCoefsCoreArgs struct {
	r            *bitstream.ReaderState
	txSize       common.TxSize
	planeType    int
	isInter      int
	dequant      [2]int16
	scan         []int16
	neighbors    []int16
	scanArr      *[1024]int16
	neighborsArr *[2048]int16
	fc           *FrameCoefProbs
	dqcoeff      *[1024]int16
	tokenCache   *[1024]uint8
	skipFirstEOB bool
}

// DecodeCoefsFastAfterEOB decodes a counts-less transform block whose initial
// c==0 EOB bit was already consumed as 1 by the caller, keeping the boolean
// reader state in registers across the call boundary without spilling to rs
// unless the slow tail at the end of the tile is reached.
func DecodeCoefsFastAfterEOB(
	r *bitstream.ReaderState,
	buf []byte, pos int,
	value uint64, rng uint32, count int32,
	txSize common.TxSize,
	planeType, isInter int,
	dequant [2]int16,
	ctx int,
	so *common.ScanOrder,
	fc *FrameCoefProbs,
	dqcoeff *[1024]int16,
	tokenCache *[1024]uint8,
) (int, int, uint64, uint32, int32) {
	args := decodeCoefsCoreArgs{
		r:            r,
		txSize:       txSize,
		planeType:    planeType,
		isInter:      isInter,
		dequant:      dequant,
		scan:         so.Scan,
		neighbors:    so.Neighbors,
		scanArr:      so.ScanArr,
		neighborsArr: so.NeighborsArr,
		fc:           fc,
		dqcoeff:      dqcoeff,
		tokenCache:   tokenCache,
		skipFirstEOB: true,
	}
	return decodeCoefsReaderStateCore(&args, buf, pos, value, rng, count, ctx)
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
	skipFirstEOB bool,
) int {
	buf, _, _, pos := r.BitView()
	args := decodeCoefsCoreArgs{
		r:            r,
		txSize:       txSize,
		planeType:    planeType,
		isInter:      isInter,
		dequant:      dequant,
		scan:         scan,
		neighbors:    neighbors,
		fc:           fc,
		dqcoeff:      dqcoeff,
		tokenCache:   tokenCache,
		skipFirstEOB: skipFirstEOB,
	}
	eob, nextPos, value, rng, count := decodeCoefsReaderStateCore(
		&args, buf, pos, r.Value, r.Range, r.Count, ctx)
	r.Value = value
	r.Range = rng
	r.Count = count
	r.CommitPos(nextPos)
	return eob
}

func decodeCoefsReaderStateCore(
	a *decodeCoefsCoreArgs,
	buf []byte, pos int,
	value uint64, rng uint32, count int32,
	ctx int,
) (int, int, uint64, uint32, int32) {
	txSize := a.txSize
	maxEob := maxEobForTxSize(txSize)
	scan := a.scan
	neighbors := a.neighbors
	scanArr := a.scanArr
	neighborsArr := a.neighborsArr
	_ = scan[maxEob-1]
	_ = neighbors[(maxEob<<1)-1]
	bt := bandTranslatePadded(txSize)
	dqShift := uint(0)
	if txSize == common.Tx32x32 {
		dqShift = 1
	}
	dqv := a.dequant[0]
	dqvAC := a.dequant[1]
	dq1 := int(dqv) >> dqShift
	dq1AC := int(dqvAC) >> dqShift

	coefModel := &a.fc[txSize][a.planeType][a.isInter]
	dqcoeff := a.dqcoeff
	tokenCache := a.tokenCache
	fastLimit := len(buf) - coefFastMargin

	c := 0
	slowResume := false
	skipEob := a.skipFirstEOB

	scanAt := func(i int) int {
		if scanArr != nil {
			return int(scanArr[i&1023]) & 1023
		}
		return int(scan[i]) & 1023
	}
	coefCtxAt := func(i int) int {
		if neighborsArr != nil {
			i2 := (i << 1) & 2046
			a := tokenCache[int(neighborsArr[i2])&1023]
			b := tokenCache[int(neighborsArr[i2+1])&1023]
			return (1 + int(a) + int(b)) >> 1
		}
		return getCoefContextArr(neighbors, tokenCache, i)
	}

fastLoop:
	for c < maxEob {
		if pos > fastLimit {
			slowResume = true
			break
		}
		band := int(bt[c&1023])
		probs := &coefModel[band][ctx]

		// One batched refill guarantees count >= 24 (>= 32 valid bits),
		// enough for EOB (<=7) + zero (<=7) + one (<=7) + sign (<=1) = <=22
		// bits without another refill check on the hot ±1 path.
		if count < 24 {
			value, count, pos = bitstream.FillFast(buf, pos, value, count)
		}
		var bit uint32
		if skipEob {
			skipEob = false
		} else {
			bit, value, rng, count = vpxReadNoFill(uint32(probs[eobContextNode]),
				value, rng, count)
			if bit == 0 {
				break
			}
		}

		bit, value, rng, count = vpxReadNoFill(uint32(probs[zeroContextNode]),
			value, rng, count)
		for bit == 0 {
			dqv = dqvAC
			dq1 = dq1AC
			tokenCache[scanAt(c)] = 0
			c++
			if c >= maxEob {
				break fastLoop
			}
			ctx = coefCtxAt(c)
			band = int(bt[c&1023])
			probs = &coefModel[band][ctx]
			// Keep count >= 16 before zeroContextNode so exiting the zero-run
			// still leaves count >= 9 for oneContextNode (<=7) + sign (<=1).
			if count < 16 {
				if pos > fastLimit {
					slowResume = true
					skipEob = true
					break fastLoop
				}
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(probs[zeroContextNode]),
				value, rng, count)
		}

		var val int
		bit, value, rng, count = vpxReadNoFill(uint32(probs[oneContextNode]),
			value, rng, count)
		if bit != 0 {
			p := &tables.Pareto8Full[probs[pivotNode]-1]
			// count >= 32 covers p[0], p[1], p[2], and sign (<=22 bits) for ±2..±4,
			// and p[0], p[3], p[4], cat1/cat2, and sign for small categories.
			if count < 32 {
				value, count, pos = bitstream.FillFast(buf, pos, value, count)
			}
			bit, value, rng, count = vpxReadNoFill(uint32(p[0]), value, rng,
				count)
			if bit != 0 {
				bit, value, rng, count = vpxReadNoFill(uint32(p[3]), value,
					rng, count)
				if bit != 0 {
					tokenCache[int(scan[c])&1023] = 5
					if count < 32 {
						value, count, pos = bitstream.FillFast(buf, pos, value, count)
					}
					bit, value, rng, count = vpxReadNoFill(uint32(p[5]),
						value, rng, count)
					if bit != 0 {
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
				sign := -int(bit)
				dqcoeff[int(scan[c])&1023] = int16((v ^ sign) - sign)
			} else {
				idx := int(scan[c]) & 1023
				bit, value, rng, count = vpxReadNoFill(uint32(p[1]), value,
					rng, count)
				if bit != 0 {
					tokenCache[idx] = 3
					bit, value, rng, count = vpxReadNoFill(uint32(p[2]),
						value, rng, count)
					v := ((3 + int(bit)) * int(dqv)) >> dqShift
					bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
					sign := -int(bit)
					dqcoeff[idx] = int16((v ^ sign) - sign)
				} else {
					tokenCache[idx] = 2
					v := (2 * int(dqv)) >> dqShift
					bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
					sign := -int(bit)
					dqcoeff[idx] = int16((v ^ sign) - sign)
				}
			}
		} else {
			idx := scanAt(c)
			tokenCache[idx] = 1
			bit, value, rng, count = vpxReadBitNoFill(value, rng, count)
			sign := -int(bit)
			dqcoeff[idx] = int16((dq1 ^ sign) - sign)
		}

		c++
		if c >= maxEob {
			break
		}
		ctx = coefCtxAt(c)
		dqv = dqvAC
		dq1 = dq1AC
	}

	if !slowResume {
		return c, pos, value, rng, count
	}
	r := a.r
	r.Value = value
	r.Range = rng
	r.Count = count
	r.CommitPos(pos)
	eob := decodeCoefsSlowTail(r, txSize, a.planeType, a.isInter, a.dequant,
		scan, neighbors, a.fc, nil, dqcoeff, tokenCache, c, ctx, dqv, skipEob)
	_, _, _, nextPos := r.BitView()
	return eob, nextPos, r.Value, r.Range, r.Count
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
	_ = scan[maxEob-1]
	_ = neighbors[(maxEob<<1)-1]
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
	_ = scan[maxEob-1]
	_ = neighbors[(maxEob<<1)-1]
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
