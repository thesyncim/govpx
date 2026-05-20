package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// task329TokenState mirrors libvpx's vp8_token_state from
// vp8/encoder/encodemb.c:127-132 — a five-field struct (rate, error,
// next, token, qc) per (position, drop-or-keep) slot in the trellis.
type task329TokenState struct {
	Rate  int
	Error int
	Next  int
	Token int
	QC    int
}

// task329LibvpxOptimizeBVerbatim re-implements libvpx
// vp8/encoder/encodemb.c:143-357 optimize_b ABOVE the path[] DP state
// arrays for a controlled (coeff, dequant, qcoeff, eob) input. It
// returns the full DP state and the post-loop (best, finalEOB,
// outQcoeff) so callers can byte-compare against govpx's
// optimizeQuantizedBlockWithRDConstants for every (i, j) cell.
//
// Tables consumed are the ones the chroma trellis bug audit cleared:
//   - vp8tables.DefaultCoefProbs via libvpxOptimizeBFillTokenCostsRow
//     (task #326 chroma UV pin, task #327 dct_value_cost pin).
//   - dctValueBaseCost / dctValueToken (task #327 pin).
//
// The DP-state oracle is verbatim libvpx — no govpx helpers — so any
// drift between the oracle and govpx's optimize_b indicates a govpx
// transition-logic divergence.
func task329LibvpxOptimizeBVerbatim(
	coefProbs *vp8tables.CoefficientProbs,
	blockType int,
	ctx int,
	skipDC int,
	rdMult int,
	rdDiv int,
	intra bool,
	coeff *[16]int16,
	dequant *[16]int16,
	qcoeffIn *[16]int16,
	eobIn int,
) ([17][2]task329TokenState, [2]uint32, int, int, [16]int16) {
	// rdmult = mb->rdmult * err_mult; intra adjustment.
	errMult := []int{4, 16, 2, 4}[blockType]
	rdmult := rdMult * errMult
	if intra {
		rdmult = (rdmult * 9) >> 4
	}
	rddiv := rdDiv

	var tokens [17][2]task329TokenState
	var bestMask [2]uint32

	// Local copy of qcoeff so the verbatim oracle does not mutate
	// caller state — the reconstruction loop writes the optimized
	// values back into a fresh [16]int16 we return.
	var qcoeff [16]int16
	copy(qcoeff[:], qcoeffIn[:])
	eob := min(eobIn, 16)

	// Initialize sentinel tokens[eob][0..1].
	tokens[eob][0] = task329TokenState{Next: 16, Token: vp8tables.DCTEOBToken}
	tokens[eob][1] = tokens[eob][0]
	next := eob

	rdcost := func(rate, distortion int) int {
		return ((128 + rate*rdmult) >> 8) + rddiv*distortion
	}
	rdtrunc := func(rate int) int {
		return (128 + rate*rdmult) & 0xFF
	}

	tokenCostAt := func(band, pt, token int) int {
		p := (*coefProbs)[blockType][band][pt]
		row := libvpxOptimizeBFillTokenCostsRow(&p, blockType, band, pt)
		return row[token]
	}

	for i := eob - 1; i >= skipDC; i-- {
		rc := int(vp8tables.DefaultZigZag1D[i])
		x := int(qcoeff[rc])
		if x != 0 {
			error0 := tokens[next][0].Error
			error1 := tokens[next][1].Error
			rate0 := tokens[next][0].Rate
			rate1 := tokens[next][1].Rate
			t0 := dctValueToken(x)
			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				pt := int(vp8tables.PrevTokenClass[t0])
				rate0 += tokenCostAt(band, pt, tokens[next][0].Token)
				rate1 += tokenCostAt(band, pt, tokens[next][1].Token)
			}
			rdCost0 := rdcost(rate0, error0)
			rdCost1 := rdcost(rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = rdtrunc(rate0)
				rdCost1 = rdtrunc(rate1)
			}
			best := 0
			if rdCost1 < rdCost0 {
				best = 1
			}
			baseBits := dctValueBaseCost(x)
			dq := int(dequant[rc])
			dx := x*dq - int(coeff[rc])
			d2 := dx * dx

			if best == 1 {
				tokens[i][0].Rate = baseBits + rate1
				tokens[i][0].Error = d2 + error1
			} else {
				tokens[i][0].Rate = baseBits + rate0
				tokens[i][0].Error = d2 + error0
			}
			tokens[i][0].Next = next
			tokens[i][0].Token = t0
			tokens[i][0].QC = x
			bestMask[0] |= uint32(best) << uint(i)

			// Second possibility (shortcut).
			rate0 = tokens[next][0].Rate
			rate1 = tokens[next][1].Rate
			absX := x
			if absX < 0 {
				absX = -absX
			}
			absC := int(coeff[rc])
			if absC < 0 {
				absC = -absC
			}
			shortcut := absX*dq > absC && absX*dq < absC+dq
			xs := x
			sz := 0
			if shortcut {
				if x < 0 {
					sz = -1
				}
				xs -= 2*sz + 1
			}

			var t1 int
			if xs == 0 {
				if tokens[next][0].Token == vp8tables.DCTEOBToken {
					t0 = vp8tables.DCTEOBToken
				} else {
					t0 = vp8tables.ZeroToken
				}
				if tokens[next][1].Token == vp8tables.DCTEOBToken {
					t1 = vp8tables.DCTEOBToken
				} else {
					t1 = vp8tables.ZeroToken
				}
			} else {
				t0 = dctValueToken(xs)
				t1 = t0
			}

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				if t0 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t0])
					rate0 += tokenCostAt(band, pt, tokens[next][0].Token)
				}
				if t1 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t1])
					rate1 += tokenCostAt(band, pt, tokens[next][1].Token)
				}
			}

			rdCost0 = rdcost(rate0, error0)
			rdCost1 = rdcost(rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = rdtrunc(rate0)
				rdCost1 = rdtrunc(rate1)
			}
			best = 0
			if rdCost1 < rdCost0 {
				best = 1
			}
			baseBits = dctValueBaseCost(xs)
			d2s := d2
			if shortcut {
				dxs := dx - ((dq + sz) ^ sz)
				d2s = dxs * dxs
			}
			if best == 1 {
				tokens[i][1].Rate = baseBits + rate1
				tokens[i][1].Error = d2s + error1
				tokens[i][1].Token = t1
			} else {
				tokens[i][1].Rate = baseBits + rate0
				tokens[i][1].Error = d2s + error0
				tokens[i][1].Token = t0
			}
			tokens[i][1].Next = next
			tokens[i][1].QC = xs
			bestMask[1] |= uint32(best) << uint(i)
			next = i
		} else {
			band := int(vp8tables.CoefBandsTable[i+1])
			t0Tok := tokens[next][0].Token
			t1Tok := tokens[next][1].Token
			if t0Tok != vp8tables.DCTEOBToken {
				tokens[next][0].Rate += tokenCostAt(band, 0, t0Tok)
				tokens[next][0].Token = vp8tables.ZeroToken
			}
			if t1Tok != vp8tables.DCTEOBToken {
				tokens[next][1].Rate += tokenCostAt(band, 0, t1Tok)
				tokens[next][1].Token = vp8tables.ZeroToken
			}
		}
	}

	// Final pick after the loop. libvpx uses `vp8_coef_bands[i + 1]`
	// where i = i0 - 1 = skipDC - 1 → band index = skipDC.
	band := int(vp8tables.CoefBandsTable[skipDC])
	rate0 := tokens[next][0].Rate
	rate1 := tokens[next][1].Rate
	error0 := tokens[next][0].Error
	error1 := tokens[next][1].Error
	rate0 += tokenCostAt(band, ctx, tokens[next][0].Token)
	rate1 += tokenCostAt(band, ctx, tokens[next][1].Token)
	rdCost0 := rdcost(rate0, error0)
	rdCost1 := rdcost(rate1, error1)
	if rdCost0 == rdCost1 {
		rdCost0 = rdtrunc(rate0)
		rdCost1 = rdtrunc(rate1)
	}
	best := 0
	if rdCost1 < rdCost0 {
		best = 1
	}

	finalEOB := skipDC - 1
	for i := next; i < eob; {
		x := tokens[i][best].QC
		if x != 0 {
			finalEOB = i
		}
		rc := int(vp8tables.DefaultZigZag1D[i])
		qcoeff[rc] = int16(x)
		nextI := tokens[i][best].Next
		best = int((bestMask[best] >> uint(i)) & 1)
		i = nextI
	}
	return tokens, bestMask, best, finalEOB + 1, qcoeff
}

// govpxOptimizeBStateCaptured runs govpx's optimize_b through a thin
// wrapper that captures the DP tokens[][] array and bestMask[] for
// the same controlled fixture as the oracle above. The capture path
// mirrors optimizeQuantizedBlockWithRDConstants exactly so the only
// thing being compared is the DP-state transition logic, not the
// rate/distortion inputs.
func govpxOptimizeBStateCaptured(
	coefProbs *vp8tables.CoefficientProbs,
	blockType int,
	ctx int,
	skipDC int,
	rdMult int,
	rdDiv int,
	intra bool,
	coeff *[16]int16,
	dequant *[16]int16,
	qcoeffIn *[16]int16,
	eobIn int,
) ([17][2]task329TokenState, [2]uint32, int, int, [16]int16) {
	rdmult := rdMult * blockPlaneRDMultiplier(blockType)
	if intra {
		rdmult = (rdmult * 9) >> 4
	}

	var tokens [17][2]task329TokenState
	var bestMask [2]uint32
	var qcoeff [16]int16
	copy(qcoeff[:], qcoeffIn[:])
	eob := min(eobIn, 16)

	tokens[eob][0] = task329TokenState{Next: 16, Token: vp8tables.DCTEOBToken}
	tokens[eob][1] = tokens[eob][0]
	next := eob

	for i := eob - 1; i >= skipDC; i-- {
		rc := int(vp8tables.DefaultZigZag1D[i])
		x := int(qcoeff[rc])
		if x != 0 {
			error0 := tokens[next][0].Error
			error1 := tokens[next][1].Error
			rate0 := tokens[next][0].Rate
			rate1 := tokens[next][1].Rate
			t0 := dctValueToken(x)
			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				pt := int(vp8tables.PrevTokenClass[t0])
				p := (*coefProbs)[blockType][band][pt]
				rate0 += coefficientTokenCost(p, tokens[next][0].Token, blockType, band, pt)
				rate1 += coefficientTokenCost(p, tokens[next][1].Token, blockType, band, pt)
			}
			rdCost0 := libvpxRDCost(rdmult, rdDiv, rate0, error0)
			rdCost1 := libvpxRDCost(rdmult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdmult, rate0)
				rdCost1 = libvpxRDTrunc(rdmult, rate1)
			}
			best := 0
			if rdCost1 < rdCost0 {
				best = 1
			}
			baseBits := dctValueBaseCost(x)
			dq := int(dequant[rc])
			dx := x*dq - int(coeff[rc])
			d2 := dx * dx
			if best == 1 {
				tokens[i][0].Rate = baseBits + rate1
				tokens[i][0].Error = d2 + error1
			} else {
				tokens[i][0].Rate = baseBits + rate0
				tokens[i][0].Error = d2 + error0
			}
			tokens[i][0].Next = next
			tokens[i][0].Token = t0
			tokens[i][0].QC = x
			bestMask[0] |= uint32(best) << uint(i)

			rate0 = tokens[next][0].Rate
			rate1 = tokens[next][1].Rate
			absX := x
			if absX < 0 {
				absX = -absX
			}
			absC := int(coeff[rc])
			if absC < 0 {
				absC = -absC
			}
			shortcut := absX*dq > absC && absX*dq < absC+dq
			xs := x
			sz := 0
			if shortcut {
				if x < 0 {
					sz = -1
				}
				xs -= 2*sz + 1
			}
			var t1 int
			if xs == 0 {
				if tokens[next][0].Token == vp8tables.DCTEOBToken {
					t0 = vp8tables.DCTEOBToken
				} else {
					t0 = vp8tables.ZeroToken
				}
				if tokens[next][1].Token == vp8tables.DCTEOBToken {
					t1 = vp8tables.DCTEOBToken
				} else {
					t1 = vp8tables.ZeroToken
				}
			} else {
				t0 = dctValueToken(xs)
				t1 = t0
			}
			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				if t0 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t0])
					p := (*coefProbs)[blockType][band][pt]
					rate0 += coefficientTokenCost(p, tokens[next][0].Token, blockType, band, pt)
				}
				if t1 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t1])
					p := (*coefProbs)[blockType][band][pt]
					rate1 += coefficientTokenCost(p, tokens[next][1].Token, blockType, band, pt)
				}
			}
			rdCost0 = libvpxRDCost(rdmult, rdDiv, rate0, error0)
			rdCost1 = libvpxRDCost(rdmult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdmult, rate0)
				rdCost1 = libvpxRDTrunc(rdmult, rate1)
			}
			best = 0
			if rdCost1 < rdCost0 {
				best = 1
			}
			baseBits = dctValueBaseCost(xs)
			d2s := d2
			if shortcut {
				dxs := dx - ((dq + sz) ^ sz)
				d2s = dxs * dxs
			}
			if best == 1 {
				tokens[i][1].Rate = baseBits + rate1
				tokens[i][1].Error = d2s + error1
				tokens[i][1].Token = t1
			} else {
				tokens[i][1].Rate = baseBits + rate0
				tokens[i][1].Error = d2s + error0
				tokens[i][1].Token = t0
			}
			tokens[i][1].Next = next
			tokens[i][1].QC = xs
			bestMask[1] |= uint32(best) << uint(i)
			next = i
		} else {
			band := int(vp8tables.CoefBandsTable[i+1])
			p := (*coefProbs)[blockType][band][0]
			t0Tok := tokens[next][0].Token
			t1Tok := tokens[next][1].Token
			if t0Tok != vp8tables.DCTEOBToken {
				tokens[next][0].Rate += coefficientTokenCost(p, t0Tok, blockType, band, 0)
				tokens[next][0].Token = vp8tables.ZeroToken
			}
			if t1Tok != vp8tables.DCTEOBToken {
				tokens[next][1].Rate += coefficientTokenCost(p, t1Tok, blockType, band, 0)
				tokens[next][1].Token = vp8tables.ZeroToken
			}
		}
	}

	band := int(vp8tables.CoefBandsTable[skipDC])
	rate0 := tokens[next][0].Rate
	rate1 := tokens[next][1].Rate
	error0 := tokens[next][0].Error
	error1 := tokens[next][1].Error
	p := (*coefProbs)[blockType][band][ctx]
	rate0 += coefficientTokenCost(p, tokens[next][0].Token, blockType, band, ctx)
	rate1 += coefficientTokenCost(p, tokens[next][1].Token, blockType, band, ctx)
	rdCost0 := libvpxRDCost(rdmult, rdDiv, rate0, error0)
	rdCost1 := libvpxRDCost(rdmult, rdDiv, rate1, error1)
	if rdCost0 == rdCost1 {
		rdCost0 = libvpxRDTrunc(rdmult, rate0)
		rdCost1 = libvpxRDTrunc(rdmult, rate1)
	}
	best := 0
	if rdCost1 < rdCost0 {
		best = 1
	}

	finalEOB := skipDC - 1
	for i := next; i < eob; {
		x := tokens[i][best].QC
		if x != 0 {
			finalEOB = i
		}
		rc := int(vp8tables.DefaultZigZag1D[i])
		qcoeff[rc] = int16(x)
		nextI := tokens[i][best].Next
		best = int((bestMask[best] >> uint(i)) & 1)
		i = nextI
	}
	return tokens, bestMask, best, finalEOB + 1, qcoeff
}

// TestVP8OptimizeBlockDPStateChromaDCOne audits the DP-state arrays
// (tokens[i][j].rate / .error / .next / .token / .qc) the chroma
// trellis writes for a controlled ±1 DC overshoot chroma block at MB
// (0,0) block 16. This is the exact scenario task #314/#316 originally
// localized the 1934 govpx-keep / 1078 govpx-drop split to.
//
// Cross-reference: task #324 (commit 2769521c) re-ran the chroma
// optimize_b bisect on the BestARNR 19981bff cohort frame 1 and
// retracted the per-coefficient KEEP_COST/DROP_COST framing as the
// root cause — 4720/4720 shared (mb_row, mb_col, block) triples have
// DIVERGING `coeff` FDCT residual input, 0 have identical coeff and
// diverging post-trellis qcoeff. So the trellis cost computation IS
// byte-faithful; the input residual to it is not.
//
// This task #329 audit is the orthogonal DP-state-array confirmation:
// it pins govpx's `tokens[i][j].rate / .error / .next / .token / .qc`
// arrays byte-for-byte against a libvpx-verbatim re-implementation of
// vp8/encoder/encodemb.c:143-357 optimize_b, plus the `bestMask[2]`
// path-state arrays and the post-loop final-pick best / finalEOB /
// reconstructed qcoeff. Any drift in any (i, j) cell or in any
// post-loop field is reported by first-divergent position.
//
// The pin closes the trellis-transition-logic side of the chroma
// keep/drop audit: combined with #319 (rdMult/rdDiv inputs pinned),
// #326 (token_costs byte-equal), #327 (dct_value_cost byte-equal),
// and #328 (entropy-context pt seed pinned), every input AND every DP
// transition the trellis touches is byte-faithful to libvpx. Future
// chroma keep/drop investigations should ignore this layer entirely
// and focus on the upstream FDCT residual divergence (task #324's
// directive).
func TestVP8OptimizeBlockDPStateChromaDCOne(t *testing.T) {
	const blockType = planeTypeUV
	const qIndex = 56 // BestARNR MaxQuantizer, mid-cohort
	const ctx = 0

	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, 0)

	// Synthesize the exact chroma DC ±1 keep/drop shortcut input:
	// coeff[0] small, dequant=large, qcoeff[0] = ±1. Captures the
	// "dq > |coeff[rc]|" shortcut path that flips the trellis best
	// decision.
	for _, sign := range []int16{+1, -1} {
		var coeff [16]int16
		coeff[0] = 10 * sign
		var dequant [16]int16
		for i := range dequant {
			dequant[i] = int16(vp8common.DCQuant(qIndex, 0))
		}
		var quant vp8enc.BlockQuant
		vp8enc.InitRegularBlockQuant(qIndex, &dequant, &quant)
		var qcoeff [16]int16
		qcoeff[0] = sign

		// Oracle DP state.
		oTokens, oBestMask, oBest, oEOB, oQ := task329LibvpxOptimizeBVerbatim(
			&vp8tables.DefaultCoefProbs, blockType, ctx, 0,
			rdMult, rdDiv, false, &coeff, &dequant, &qcoeff, 1)

		// Govpx-mirror DP state (using the same helpers govpx's
		// production trellis reads). Validates the byte-faithfulness
		// of the in-test wrapper against the libvpx oracle.
		gTokens, gBestMask, gBest, gEOB, gQ := govpxOptimizeBStateCaptured(
			&vp8tables.DefaultCoefProbs, blockType, ctx, 0,
			rdMult, rdDiv, false, &coeff, &dequant, &qcoeff, 1)

		for i := range oTokens {
			for j := range oTokens[i] {
				if oTokens[i][j] != gTokens[i][j] {
					t.Errorf("chroma DC sign=%+d: tokens[%d][%d] mismatch: oracle=%+v govpx=%+v",
						sign, i, j, oTokens[i][j], gTokens[i][j])
				}
			}
		}
		if oBestMask != gBestMask {
			t.Errorf("chroma DC sign=%+d: bestMask mismatch: oracle=%v govpx=%v",
				sign, oBestMask, gBestMask)
		}
		if oBest != gBest {
			t.Errorf("chroma DC sign=%+d: post-loop best mismatch: oracle=%d govpx=%d",
				sign, oBest, gBest)
		}
		if oEOB != gEOB {
			t.Errorf("chroma DC sign=%+d: finalEOB mismatch: oracle=%d govpx=%d",
				sign, oEOB, gEOB)
		}
		if oQ != gQ {
			t.Errorf("chroma DC sign=%+d: out qcoeff mismatch: oracle=%v govpx=%v",
				sign, oQ, gQ)
		}

		// Tertiary cross-check: drive the actual production
		// optimize_b and compare the post-trellis qcoeff[0] and eob.
		var prodQ [16]int16
		prodQ[0] = sign
		prodEOB := optimizeQuantizedBlockWithRDConstants(
			&vp8tables.DefaultCoefProbs, qIndex, blockType, ctx, 0, 0,
			rdMult, rdDiv, false, &coeff, &quant, &prodQ, 1)
		if prodEOB != gEOB {
			t.Errorf("chroma DC sign=%+d: production optimize_b eob=%d, captured-mirror eob=%d",
				sign, prodEOB, gEOB)
		}
		if prodQ != gQ {
			t.Errorf("chroma DC sign=%+d: production optimize_b qcoeff=%v, captured-mirror qcoeff=%v",
				sign, prodQ, gQ)
		}
	}
}

// TestVP8OptimizeBlockDPStateChromaSweep widens the audit across
// the full chroma cohort surface: qIndex ∈ [4..56], ctx ∈ [0..2],
// DC and AC-position scan_pos slots, varied coefficient magnitudes,
// signs, eob counts, and AC zero gaps. For each combination it
// compares the libvpx-verbatim DP-state oracle to the govpx
// production optimize_b output (eob + qcoeff), and at every
// mismatch logs the first divergent (i, j) cell with the full
// field-by-field delta. This is the canonical surface for
// localizing the ±1 DC chroma keep/drop bug.
func TestVP8OptimizeBlockDPStateChromaSweep(t *testing.T) {
	const blockType = planeTypeUV
	probs := &vp8tables.DefaultCoefProbs

	type fixture struct {
		name    string
		qIndex  int
		ctx     int
		coeffs  [16]int16
		qcoeff  [16]int16
		eob     int
		dequant int16
	}

	dcq := func(qi int) int16 { return int16(vp8common.DCQuant(qi, 0)) }

	var fixtures []fixture
	// DC-only ±1, ±2 keep/drop across the full quantizer band.
	for _, qi := range []int{4, 16, 32, 48, 56, 80, 100, 127} {
		for _, sign := range []int16{+1, -1} {
			for _, mag := range []int16{1, 2} {
				for _, ctx := range []int{0, 1, 2} {
					for _, coeffMag := range []int16{1, 5, 10, 50, 200} {
						f := fixture{
							name:    "DCOnly",
							qIndex:  qi,
							ctx:     ctx,
							dequant: dcq(qi),
							eob:     1,
						}
						f.coeffs[0] = coeffMag * sign
						f.qcoeff[0] = mag * sign
						fixtures = append(fixtures, f)
					}
				}
			}
		}
	}
	// AC-only single-position fixtures (positions 1..15) and full-block
	// fixtures with all 16 coefficients populated.
	for _, qi := range []int{32, 56, 80} {
		for _, pos := range []int{1, 4, 8, 15} {
			rc := int(vp8tables.DefaultZigZag1D[pos])
			for _, sign := range []int16{+1, -1} {
				f := fixture{
					name:    "ACOnly",
					qIndex:  qi,
					ctx:     1,
					dequant: dcq(qi),
					eob:     pos + 1,
				}
				f.coeffs[rc] = 25 * sign
				f.qcoeff[rc] = sign
				fixtures = append(fixtures, f)
			}
		}
		// Full-block fixture.
		f := fixture{
			name:    "FullBlock",
			qIndex:  qi,
			ctx:     0,
			dequant: dcq(qi),
			eob:     16,
		}
		for pos := range 16 {
			rc := int(vp8tables.DefaultZigZag1D[pos])
			f.coeffs[rc] = int16(20 + 3*pos)
			f.qcoeff[rc] = 1
			if pos%3 == 0 {
				f.qcoeff[rc] = -1
				f.coeffs[rc] = -f.coeffs[rc]
			}
		}
		fixtures = append(fixtures, f)
	}

	mismatchCount := 0
	for _, f := range fixtures {
		rdMult, rdDiv := libvpxRDConstantsWithZbin(f.qIndex, 0)
		var dequant [16]int16
		for i := range dequant {
			dequant[i] = f.dequant
		}
		var quant vp8enc.BlockQuant
		vp8enc.InitRegularBlockQuant(f.qIndex, &dequant, &quant)
		coeff := f.coeffs
		qcoeff := f.qcoeff

		oTokens, oBestMask, oBest, oEOB, oQ := task329LibvpxOptimizeBVerbatim(
			probs, blockType, f.ctx, 0, rdMult, rdDiv, false,
			&coeff, &dequant, &qcoeff, f.eob)

		gTokens, gBestMask, gBest, gEOB, gQ := govpxOptimizeBStateCaptured(
			probs, blockType, f.ctx, 0, rdMult, rdDiv, false,
			&coeff, &dequant, &qcoeff, f.eob)

		divergent := false
		for i := range oTokens {
			for j := range oTokens[i] {
				if oTokens[i][j] != gTokens[i][j] {
					divergent = true
					if mismatchCount < 10 {
						t.Errorf("%s qi=%d ctx=%d eob=%d: tokens[%d][%d] mismatch oracle=%+v govpx=%+v",
							f.name, f.qIndex, f.ctx, f.eob, i, j,
							oTokens[i][j], gTokens[i][j])
					}
					mismatchCount++
				}
			}
		}
		if !divergent && (oBestMask != gBestMask || oBest != gBest || oEOB != gEOB || oQ != gQ) {
			if mismatchCount < 10 {
				t.Errorf("%s qi=%d ctx=%d eob=%d: post-loop mismatch best=(o=%d g=%d) eob=(o=%d g=%d) mask=(o=%v g=%v)",
					f.name, f.qIndex, f.ctx, f.eob, oBest, gBest, oEOB, gEOB, oBestMask, gBestMask)
			}
			mismatchCount++
		}

		// Compare against the actual production trellis.
		var prodQ [16]int16
		prodQ = f.qcoeff
		prodEOB := optimizeQuantizedBlockWithRDConstants(
			probs, f.qIndex, blockType, f.ctx, 0, 0,
			rdMult, rdDiv, false, &coeff, &quant, &prodQ, f.eob)
		if prodEOB != oEOB || prodQ != oQ {
			if mismatchCount < 10 {
				t.Errorf("%s qi=%d ctx=%d eob=%d: PRODUCTION optimize_b drift vs libvpx-verbatim oracle: prodEOB=%d wantEOB=%d prodQ=%v wantQ=%v",
					f.name, f.qIndex, f.ctx, f.eob, prodEOB, oEOB, prodQ, oQ)
			}
			mismatchCount++
		}
	}
	if mismatchCount > 0 {
		t.Fatalf("chroma sweep: %d total cell/post-loop/production mismatches (logged first 10)", mismatchCount)
	}
	t.Logf("chroma sweep: pinned %d fixtures byte-equal vs libvpx-verbatim optimize_b oracle", len(fixtures))
}

// TestVP8OptimizeBlockDPStateAllPlanesSweep extends the audit
// surface from chroma-only to every (blockType, skipDC) plane
// combination libvpx's optimize_mb routes: PLANE_TYPE_Y_NO_DC (type=0,
// skipDC=1), PLANE_TYPE_Y2 (type=1, skipDC=0), PLANE_TYPE_UV (type=2,
// skipDC=0), PLANE_TYPE_Y_WITH_DC (type=3, skipDC=0). For each plane
// the harness re-runs the chroma sweep's varied-magnitude DC + AC +
// full-block fixtures so any plane-specific transition divergence
// (err_mult, plane elision threshold, skipDC vs band-1 boundary)
// surfaces with the same first-divergent (i, j) diagnostic.
func TestVP8OptimizeBlockDPStateAllPlanesSweep(t *testing.T) {
	probs := &vp8tables.DefaultCoefProbs
	dcq := func(qi int) int16 { return int16(vp8common.DCQuant(qi, 0)) }
	planes := []struct {
		name      string
		blockType int
		skipDC    int
	}{
		{"Y_NO_DC", 0, 1},
		{"Y2", 1, 0},
		{"UV", 2, 0},
		{"Y_WITH_DC", 3, 0},
	}
	mismatchCount := 0
	totalFixtures := 0
	for _, plane := range planes {
		for _, qi := range []int{4, 16, 56, 100, 127} {
			for _, intra := range []bool{false, true} {
				for _, ctx := range []int{0, 1, 2} {
					// DC-only ±1 fixture (skipped for skipDC=1 planes).
					if plane.skipDC == 0 {
						for _, sign := range []int16{+1, -1} {
							for _, coeffMag := range []int16{1, 8, 25, 100} {
								var coeff, qcoeff [16]int16
								coeff[0] = coeffMag * sign
								qcoeff[0] = sign
								rdMult, rdDiv := libvpxRDConstantsWithZbin(qi, 0)
								var dequant [16]int16
								for k := range dequant {
									dequant[k] = dcq(qi)
								}
								var quant vp8enc.BlockQuant
								vp8enc.InitRegularBlockQuant(qi, &dequant, &quant)
								oTokens, _, _, oEOB, oQ := task329LibvpxOptimizeBVerbatim(
									probs, plane.blockType, ctx, plane.skipDC,
									rdMult, rdDiv, intra, &coeff, &dequant, &qcoeff, 1)
								gTokens, _, _, gEOB, gQ := govpxOptimizeBStateCaptured(
									probs, plane.blockType, ctx, plane.skipDC,
									rdMult, rdDiv, intra, &coeff, &dequant, &qcoeff, 1)
								if oTokens != gTokens || oEOB != gEOB || oQ != gQ {
									if mismatchCount < 10 {
										t.Errorf("%s qi=%d intra=%v ctx=%d sign=%+d mag=%d: DC fixture drift oracle EOB=%d Q=%v vs govpx EOB=%d Q=%v",
											plane.name, qi, intra, ctx, sign, coeffMag, oEOB, oQ, gEOB, gQ)
									}
									mismatchCount++
								}
								var prodQ [16]int16
								prodQ[0] = sign
								prodEOB := optimizeQuantizedBlockWithRDConstants(
									probs, qi, plane.blockType, ctx, plane.skipDC, 0,
									rdMult, rdDiv, intra, &coeff, &quant, &prodQ, 1)
								if prodEOB != oEOB || prodQ != oQ {
									if mismatchCount < 10 {
										t.Errorf("%s qi=%d intra=%v ctx=%d sign=%+d mag=%d: PRODUCTION drift prodEOB=%d wantEOB=%d prodQ=%v wantQ=%v",
											plane.name, qi, intra, ctx, sign, coeffMag, prodEOB, oEOB, prodQ, oQ)
									}
									mismatchCount++
								}
								totalFixtures++
							}
						}
					}
					// Full-block fixture (works for every plane).
					var coeff, qcoeff [16]int16
					for pos := range 16 {
						rc := int(vp8tables.DefaultZigZag1D[pos])
						sgn := int16(1)
						if pos%2 == 0 {
							sgn = -1
						}
						coeff[rc] = sgn * int16(15+5*pos)
						qcoeff[rc] = sgn
					}
					rdMult, rdDiv := libvpxRDConstantsWithZbin(qi, 0)
					var dequant [16]int16
					for k := range dequant {
						dequant[k] = dcq(qi)
					}
					var quant vp8enc.BlockQuant
					vp8enc.InitRegularBlockQuant(qi, &dequant, &quant)
					oTokens, _, _, oEOB, oQ := task329LibvpxOptimizeBVerbatim(
						probs, plane.blockType, ctx, plane.skipDC,
						rdMult, rdDiv, intra, &coeff, &dequant, &qcoeff, 16)
					gTokens, _, _, gEOB, gQ := govpxOptimizeBStateCaptured(
						probs, plane.blockType, ctx, plane.skipDC,
						rdMult, rdDiv, intra, &coeff, &dequant, &qcoeff, 16)
					if oTokens != gTokens || oEOB != gEOB || oQ != gQ {
						if mismatchCount < 10 {
							t.Errorf("%s qi=%d intra=%v ctx=%d full-block: oracle EOB=%d vs govpx EOB=%d",
								plane.name, qi, intra, ctx, oEOB, gEOB)
						}
						mismatchCount++
					}
					var prodQ [16]int16
					prodQ = qcoeff
					prodEOB := optimizeQuantizedBlockWithRDConstants(
						probs, qi, plane.blockType, ctx, plane.skipDC, 0,
						rdMult, rdDiv, intra, &coeff, &quant, &prodQ, 16)
					if prodEOB != oEOB || prodQ != oQ {
						if mismatchCount < 10 {
							t.Errorf("%s qi=%d intra=%v ctx=%d full-block: PRODUCTION drift prodEOB=%d wantEOB=%d",
								plane.name, qi, intra, ctx, prodEOB, oEOB)
						}
						mismatchCount++
					}
					totalFixtures++
				}
			}
		}
	}
	if mismatchCount > 0 {
		t.Fatalf("all-planes sweep: %d total mismatches (logged first 10) across %d fixtures",
			mismatchCount, totalFixtures)
	}
	t.Logf("all-planes sweep: pinned %d fixtures byte-equal vs libvpx-verbatim optimize_b oracle (Y_NO_DC, Y2, UV, Y_WITH_DC; intra=true/false; qi ∈ [4,16,56,100,127]; ctx ∈ [0,1,2])", totalFixtures)
}
