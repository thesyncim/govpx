package encoder

// VP8 rate-distortion and motion-search cost helpers port libvpx v1.16.0
// vp8/encoder/rdopt.c and vp8/encoder/mcomp.c mechanics.

import (
	"math"
	"math/bits"

	common "github.com/thesyncim/govpx/internal/vp8/common"
)

const rdCostSignShift = bits.UintSize - 1

// RDModeScore returns the libvpx VP8 RDCOST for a rate/distortion pair.
func RDModeScore(qIndex int, rate int, distortion int) int {
	return RDModeScoreWithZbin(qIndex, 0, rate, distortion)
}

// RDModeScoreWithZbin returns the libvpx VP8 RDCOST with zbin-over-quant
// factored into the frame RD multiplier.
func RDModeScoreWithZbin(qIndex int, zbinOverQuant int, rate int, distortion int) int {
	rdMult, rdDiv := RDConstantsWithZbin(qIndex, zbinOverQuant)
	return RDCost(rdMult, rdDiv, rate, distortion)
}

// InterIntraRDPenalty ports the VP8 inter/intra mode RD bias.
func InterIntraRDPenalty(qIndex int) int {
	return 10 * common.DCQuant(qIndex, 0)
}

// ErrorPerBit ports the encodeframe.c errorperbit derivation used by
// libvpx fractional motion searches.
func ErrorPerBit(qIndex int) int {
	return ErrorPerBitWithZbin(qIndex, 0)
}

// ErrorPerBitWithZbin returns the motion-search error-per-bit scale for a
// zbin-adjusted RD multiplier.
func ErrorPerBitWithZbin(qIndex int, zbinOverQuant int) int {
	return ErrorPerBitWithZbinAndIIRatio(qIndex, zbinOverQuant, -1)
}

// ErrorPerBitWithZbinAndIIRatio ports vp8_initialize_rd_consts's
// `cpi->mb.errorperbit = cpi->RDMULT / 110` (rdopt.c:198), evaluated AFTER
// the pass-2 iiratio lift (rdopt.c:189-196) but BEFORE the >1000 /100 split
// (rdopt.c:211). Pass iiRatio < 0 to skip the lift (one-pass / KEY_FRAME).
func ErrorPerBitWithZbinAndIIRatio(qIndex int, zbinOverQuant int, iiRatio int) int {
	rdMult := RawRDMultiplierWithZbin(qIndex, zbinOverQuant)
	if iiRatio >= 0 {
		idx := min(iiRatio, 31)
		rdMult += (rdMult * rdIIFactor[idx]) >> 4
	}
	errorPerBit := rdMult / 110
	if errorPerBit == 0 {
		return 1
	}
	return errorPerBit
}

// SADPerBit16 ports sad_per_bit16lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts.
func SADPerBit16(qIndex int) int {
	return sadPerBit16LUT[common.ClampQIndex(qIndex)]
}

// SADPerBit4 ports sad_per_bit4lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts for SPLITMV block search.
func SADPerBit4(qIndex int) int {
	return sadPerBit4LUT[common.ClampQIndex(qIndex)]
}

// RDConstants ports vp8_initialize_rd_consts for the single-pass path.
func RDConstants(qIndex int) (int, int) {
	return RDConstantsWithZbin(qIndex, 0)
}

// RawRDMultiplierWithZbin returns vp8_initialize_rd_consts's unsplit RDMULT.
func RawRDMultiplierWithZbin(qIndex int, zbinOverQuant int) int {
	qValue := min(common.DCQuant(qIndex, 0), 160)
	// zbinOverQuant=0 collapses oqFactor to 1.0 and modq to qValue, so
	// the no-zbin path produces the same rdMult as the modq formula.
	// Single straight-line computation drops the function below the
	// inliner budget.
	modq := int(float64(qValue) * (1.0 + 0.0015625*float64(zbinOverQuant)))
	return int(2.80 * float64(modq*modq))
}

// RDConstantsWithZbin returns vp8_initialize_rd_consts's split RD constants
// after zbin-over-quant adjustment.
func RDConstantsWithZbin(qIndex int, zbinOverQuant int) (int, int) {
	return RDConstantsWithZbinAndIIRatio(qIndex, zbinOverQuant, -1)
}

// rdIIFactor mirrors the `rd_iifactor[32]` table at
// libvpx vp8/encoder/rdopt.c:134-136. vp8_initialize_rd_consts applies a
// per-frame lift to cpi->RDMULT on pass==2 && !KEY_FRAME using
// `(RDMULT * rd_iifactor[clamp(next_iiratio, 0, 31)]) >> 4` (rdopt.c:189-196).
// The lift fires BEFORE the >1000 /100 split, so a raw RDMULT near the
// 1000 cutoff (e.g. 907) can be lifted to ~1077 and cross into the /100
// branch — a path govpx never reached without the iiratio plumbing.
var rdIIFactor = [32]int{
	4, 4, 3, 2, 1, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

// RDConstantsWithZbinAndIIRatio ports vp8_initialize_rd_consts including
// the pass==2 && !KEY_FRAME iiratio lift at rdopt.c:189-196. Pass iiRatio < 0
// to skip the lift (single-pass or KEY_FRAME path). Otherwise iiRatio is the
// libvpx `cpi->twopass.next_iiratio` value (clamped to [0, 31] internally,
// matching the >31 branch at rdopt.c:190).
func RDConstantsWithZbinAndIIRatio(qIndex int, zbinOverQuant int, iiRatio int) (int, int) {
	rdMult := RawRDMultiplierWithZbin(qIndex, zbinOverQuant)
	if iiRatio >= 0 {
		idx := min(iiRatio, 31)
		rdMult += (rdMult * rdIIFactor[idx]) >> 4
	}
	rdDiv := 100
	if rdMult > 1000 {
		rdDiv = 1
		rdMult /= 100
	}
	return rdMult, rdDiv
}

// RDCost evaluates libvpx's VP8 RDCOST macro.
func RDCost(rdMult int, rdDiv int, rate int, distortion int) int {
	return ((128 + rate*rdMult) >> 8) + rdDiv*distortion
}

var sadPerBit16LUT = [common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 10, 10,
	10, 10, 10, 10, 10, 10, 11, 11,
	11, 11, 11, 11, 12, 12, 12, 12,
	12, 12, 13, 13, 13, 13, 14, 14,
}

var sadPerBit4LUT = [common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 5, 5,
	5, 5, 5, 5, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	10, 10, 10, 10, 10, 10, 10, 10,
	11, 11, 11, 11, 11, 11, 11, 11,
	12, 12, 12, 12, 12, 12, 12, 12,
	13, 13, 13, 13, 13, 13, 13, 14,
	14, 14, 14, 14, 15, 15, 15, 15,
	16, 16, 16, 16, 17, 17, 17, 18,
	18, 18, 19, 19, 19, 20, 20, 20,
}

// FullPelMVSADComponentCost16 stores initialized full-pel MV SAD costs for
// each q index.
var FullPelMVSADComponentCost16 [common.QIndexRange][256]int

// FullPelMVSADComponentCost4 stores initialized full-pel MV SAD costs for
// SPLITMV sub-block searches.
var FullPelMVSADComponentCost4 [common.QIndexRange][256]int

// FirstPassFullPelMVSADComponentCost16 is the zero-sad_per_bit table
// used during vp8_first_pass. libvpx never calls vp8cx_initialize_me_consts
// before the first-pass loop, so x->sadperbit16 stays at its zero-initialised
// value (calloc'd VP8_COMP) and mvsad_err_cost collapses to 0 inside
// diamond_search_sad. govpx must mirror that behaviour or its diamond search
// over-penalises off-center candidates and converges to a worse MV than
// libvpx, breaking first-pass MV stats (plan-§3 gap E Step 2).
var FirstPassFullPelMVSADComponentCost16 [256]int

func init() {
	initFullPelMVSADComponentCost16()
	initFullPelMVSADComponentCost4()
}

func initFullPelMVSADComponentCost16() {
	for q := range FullPelMVSADComponentCost16 {
		sadPerBit := sadPerBit16LUT[q]
		for i := range FullPelMVSADComponentCost16[q] {
			cost := 300
			if i > 0 {
				cost = int(256 * (2 * (math.Log2(float64(8*i)) + 0.6)))
			}
			FullPelMVSADComponentCost16[q][i] = cost * sadPerBit
		}
	}
	// First-pass: sadPerBit = 0 → all costs zero. The explicit zero init
	// keeps the table read-only and addressable like the per-q variant so
	// the search hot path can hold a single *[256]int pointer regardless
	// of which pass is running.
	for i := range FirstPassFullPelMVSADComponentCost16 {
		FirstPassFullPelMVSADComponentCost16[i] = 0
	}
}

func initFullPelMVSADComponentCost4() {
	for q := range FullPelMVSADComponentCost4 {
		sadPerBit := sadPerBit4LUT[q]
		for i := range FullPelMVSADComponentCost4[q] {
			cost := 300
			if i > 0 {
				cost = int(256 * (2 * (math.Log2(float64(8*i)) + 0.6)))
			}
			FullPelMVSADComponentCost4[q][i] = cost * sadPerBit
		}
	}
}

func FullPelMVSADCost16FromDeltas(mvRow8 int, mvCol8 int, refRow8 int, refCol8 int, qIndex int) int {
	// Clamp-to-[-255,255] then abs collapses to abs-then-clamp-to-255:
	// the original sign of the delta has no effect on the cost table
	// lookup (costs are symmetric in delta sign).
	rowDelta := mvRow8 - refRow8
	rowMask := rowDelta >> rdCostSignShift
	rowDelta = min((rowDelta^rowMask)-rowMask, 255)
	colDelta := mvCol8 - refCol8
	colMask := colDelta >> rdCostSignShift
	colDelta = min((colDelta^colMask)-colMask, 255)
	costs := &FullPelMVSADComponentCost16[common.ClampQIndex(qIndex)]
	return (costs[rowDelta] + costs[colDelta] + 128) >> 8
}

func FullPelMVSADCost4FromDeltas(mvRow8 int, mvCol8 int, refRow8 int, refCol8 int, qIndex int) int {
	rowDelta := mvRow8 - refRow8
	rowMask := rowDelta >> rdCostSignShift
	rowDelta = min((rowDelta^rowMask)-rowMask, 255)
	colDelta := mvCol8 - refCol8
	colMask := colDelta >> rdCostSignShift
	colDelta = min((colDelta^colMask)-colMask, 255)
	costs := &FullPelMVSADComponentCost4[common.ClampQIndex(qIndex)]
	return (costs[rowDelta] + costs[colDelta] + 128) >> 8
}
