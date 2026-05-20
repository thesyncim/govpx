package govpx

import (
	"math"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func rdModeScore(qIndex int, rate int, distortion int) int {
	return rdModeScoreWithZbin(qIndex, 0, rate, distortion)
}

func rdModeScoreWithZbin(qIndex int, zbinOverQuant int, rate int, distortion int) int {
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func libvpxInterIntraRDPenalty(qIndex int) int {
	return 10 * vp8common.DCQuant(qIndex, 0)
}

// libvpxErrorPerBit ports the encodeframe.c errorperbit derivation used by
// libvpx fractional motion searches.
func libvpxErrorPerBit(qIndex int) int {
	return libvpxErrorPerBitWithZbin(qIndex, 0)
}

func libvpxErrorPerBitWithZbin(qIndex int, zbinOverQuant int) int {
	return libvpxErrorPerBitWithZbinAndIIRatio(qIndex, zbinOverQuant, -1)
}

// libvpxErrorPerBitWithZbinAndIIRatio ports vp8_initialize_rd_consts's
// `cpi->mb.errorperbit = cpi->RDMULT / 110` (rdopt.c:198), evaluated AFTER
// the pass-2 iiratio lift (rdopt.c:189-196) but BEFORE the >1000 /100 split
// (rdopt.c:211). Pass iiRatio < 0 to skip the lift (one-pass / KEY_FRAME).
func libvpxErrorPerBitWithZbinAndIIRatio(qIndex int, zbinOverQuant int, iiRatio int) int {
	rdMult := libvpxRawRDMultiplierWithZbin(qIndex, zbinOverQuant)
	if iiRatio >= 0 {
		idx := min(iiRatio, 31)
		rdMult += (rdMult * libvpxRDIIFactor[idx]) >> 4
	}
	errorPerBit := rdMult / 110
	if errorPerBit == 0 {
		return 1
	}
	return errorPerBit
}

// libvpxSADPerBit16 ports sad_per_bit16lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts.
func libvpxSADPerBit16(qIndex int) int {
	return libvpxSADPerBit16LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxSADPerBit4 ports sad_per_bit4lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts for SPLITMV block search.
func libvpxSADPerBit4(qIndex int) int {
	return libvpxSADPerBit4LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxRDConstants ports vp8_initialize_rd_consts for the single-pass path.
func libvpxRDConstants(qIndex int) (int, int) {
	return libvpxRDConstantsWithZbin(qIndex, 0)
}

func libvpxRawRDMultiplierWithZbin(qIndex int, zbinOverQuant int) int {
	qValue := min(vp8common.DCQuant(qIndex, 0), 160)
	// zbinOverQuant=0 collapses oqFactor to 1.0 and modq to qValue, so
	// the no-zbin path produces the same rdMult as the modq formula.
	// Single straight-line computation drops the function below the
	// inliner budget.
	modq := int(float64(qValue) * (1.0 + 0.0015625*float64(zbinOverQuant)))
	return int(2.80 * float64(modq*modq))
}

func libvpxRDConstantsWithZbin(qIndex int, zbinOverQuant int) (int, int) {
	return libvpxRDConstantsWithZbinAndIIRatio(qIndex, zbinOverQuant, -1)
}

// libvpxRDIIFactor mirrors the `rd_iifactor[32]` table at
// libvpx vp8/encoder/rdopt.c:134-136. vp8_initialize_rd_consts applies a
// per-frame lift to cpi->RDMULT on pass==2 && !KEY_FRAME using
// `(RDMULT * rd_iifactor[clamp(next_iiratio, 0, 31)]) >> 4` (rdopt.c:189-196).
// The lift fires BEFORE the >1000 /100 split, so a raw RDMULT near the
// 1000 cutoff (e.g. 907) can be lifted to ~1077 and cross into the /100
// branch — a path govpx never reached without the iiratio plumbing.
var libvpxRDIIFactor = [32]int{
	4, 4, 3, 2, 1, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

// libvpxRDConstantsWithZbinAndIIRatio ports vp8_initialize_rd_consts including
// the pass==2 && !KEY_FRAME iiratio lift at rdopt.c:189-196. Pass iiRatio < 0
// to skip the lift (single-pass or KEY_FRAME path). Otherwise iiRatio is the
// libvpx `cpi->twopass.next_iiratio` value (clamped to [0, 31] internally,
// matching the >31 branch at rdopt.c:190).
func libvpxRDConstantsWithZbinAndIIRatio(qIndex int, zbinOverQuant int, iiRatio int) (int, int) {
	rdMult := libvpxRawRDMultiplierWithZbin(qIndex, zbinOverQuant)
	if iiRatio >= 0 {
		idx := min(iiRatio, 31)
		rdMult += (rdMult * libvpxRDIIFactor[idx]) >> 4
	}
	rdDiv := 100
	if rdMult > 1000 {
		rdDiv = 1
		rdMult /= 100
	}
	return rdMult, rdDiv
}

func libvpxRDCost(rdMult int, rdDiv int, rate int, distortion int) int {
	return ((128 + rate*rdMult) >> 8) + rdDiv*distortion
}

var libvpxSADPerBit16LUT = [vp8common.QIndexRange]int{
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

var libvpxSADPerBit4LUT = [vp8common.QIndexRange]int{
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

var libvpxFullPelMVSADComponentCost16 [vp8common.QIndexRange][256]int

// libvpxFirstPassFullPelMVSADComponentCost16 is the zero-sad_per_bit table
// used during vp8_first_pass. libvpx never calls vp8cx_initialize_me_consts
// before the first-pass loop, so x->sadperbit16 stays at its zero-initialised
// value (calloc'd VP8_COMP) and mvsad_err_cost collapses to 0 inside
// diamond_search_sad. govpx must mirror that behaviour or its diamond search
// over-penalises off-center candidates and converges to a worse MV than
// libvpx, breaking first-pass MV stats (plan-§3 gap E Step 2).
var libvpxFirstPassFullPelMVSADComponentCost16 [256]int

func init() {
	initLibvpxFullPelMVSADComponentCost16()
}

func initLibvpxFullPelMVSADComponentCost16() {
	for q := range libvpxFullPelMVSADComponentCost16 {
		sadPerBit := libvpxSADPerBit16LUT[q]
		for i := range libvpxFullPelMVSADComponentCost16[q] {
			cost := 300
			if i > 0 {
				cost = int(256 * (2 * (math.Log2(float64(8*i)) + 0.6)))
			}
			libvpxFullPelMVSADComponentCost16[q][i] = cost * sadPerBit
		}
	}
	// First-pass: sadPerBit = 0 → all costs zero. The explicit zero init
	// keeps the table read-only and addressable like the per-q variant so
	// the search hot path can hold a single *[256]int pointer regardless
	// of which pass is running.
	for i := range libvpxFirstPassFullPelMVSADComponentCost16 {
		libvpxFirstPassFullPelMVSADComponentCost16[i] = 0
	}
}

func libvpxFullPelMVSADCost16FromDeltas(mvRow8 int, mvCol8 int, refRow8 int, refCol8 int, qIndex int) int {
	// Clamp-to-[-255,255] then abs collapses to abs-then-clamp-to-255:
	// the original sign of the delta has no effect on the cost table
	// lookup (costs are symmetric in delta sign).
	rowDelta := mvRow8 - refRow8
	rowMask := rowDelta >> mvKernelSignShift
	rowDelta = min((rowDelta^rowMask)-rowMask, 255)
	colDelta := mvCol8 - refCol8
	colMask := colDelta >> mvKernelSignShift
	colDelta = min((colDelta^colMask)-colMask, 255)
	costs := &libvpxFullPelMVSADComponentCost16[vp8common.ClampQIndex(qIndex)]
	return (costs[rowDelta] + costs[colDelta] + 128) >> 8
}
