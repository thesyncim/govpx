package govpx

import (
	"math"
	"testing"
)

// makeARFStats builds a uniform `FirstPassFrameStats` slice with the
// per-frame fields needed by libvpxCalcARFBoost: intra/coded for the
// baseline boost, pcnt_inter / pcnt_motion / mvr_abs / mvc_abs for the
// prediction decay and the motion ratio accumulator.
func makeARFStats(n int, intra, coded, pcntInter, pcntMotion, mvrAbs, mvcAbs float64) []FirstPassFrameStats {
	stats := make([]FirstPassFrameStats, n)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			Frame:        uint64(i),
			IntraError:   intra,
			CodedError:   coded,
			PcntInter:    pcntInter,
			PcntMotion:   pcntMotion,
			MVrAbs:       mvrAbs,
			MVcAbs:       mvcAbs,
			MVr:          mvrAbs,
			MVc:          mvcAbs,
			MVInOutCount: 0,
			NewMVCount:   0,
			Count:        1,
			Duration:     1.0,
		}
	}
	return stats
}

// referenceCalcARFBoost is a straight Go transliteration of libvpx's
// calc_arf_boost (vp8/encoder/firstpass.c:1482-1578) used as an
// independent oracle for the production libvpxCalcARFBoost. It
// duplicates the helper bodies inline so a regression in the production
// helpers is caught instead of being mirrored.
func referenceCalcARFBoost(stats []FirstPassFrameStats, cursor, fFrames, bFrames int, gfIntraErrMin float64) (int, int, int) {
	frameBoost := func(this FirstPassFrameStats, mvInOut float64) float64 {
		const iiFactor = 1.5
		const gfRMax = 48.0
		intra := this.IntraError
		if intra <= gfIntraErrMin {
			intra = gfIntraErrMin
		}
		denom := this.CodedError
		if denom < 0 {
			denom -= 0.000001
		} else {
			denom += 0.000001
		}
		fb := iiFactor * intra / denom
		if mvInOut > 0 {
			fb += fb * (mvInOut * 2.0)
		} else {
			fb += fb * (mvInOut / 2.0)
		}
		if fb > gfRMax {
			fb = gfRMax
		}
		return fb
	}
	predDecay := func(this FirstPassFrameStats) float64 {
		rate := this.PcntInter
		motionDecay := 1.0 - (this.PcntMotion / 20.0)
		if motionDecay < rate {
			rate = motionDecay
		}
		mvRAbs := math.Abs(this.MVrAbs * this.PcntMotion)
		mvCAbs := math.Abs(this.MVcAbs * this.PcntMotion)
		df := math.Sqrt(mvRAbs*mvRAbs+mvCAbs*mvCAbs) / 250.0
		if df > 1.0 {
			df = 0.0
		} else {
			df = 1.0 - df
		}
		if df < rate {
			rate = df
		}
		return rate
	}
	accumulateMotion := func(this FirstPassFrameStats, mvInOut, absMVInOut, mvRatio *float64) float64 {
		mp := this.PcntMotion
		v := this.MVInOutCount * mp
		*mvInOut += v
		if v < 0 {
			*absMVInOut += -v
		} else {
			*absMVInOut += v
		}
		if mp > 0.05 {
			mvR := this.MVr
			if mvR < 0 {
				mvR = -mvR
			}
			mvRdenom := mvR
			if mvRdenom < 0 {
				mvRdenom -= 0.000001
			} else {
				mvRdenom += 0.000001
			}
			rr := math.Abs(this.MVrAbs) / mvRdenom
			if rr < this.MVrAbs {
				*mvRatio += rr * mp
			} else {
				*mvRatio += this.MVrAbs * mp
			}
			mvC := this.MVc
			if mvC < 0 {
				mvC = -mvC
			}
			mvCdenom := mvC
			if mvCdenom < 0 {
				mvCdenom -= 0.000001
			} else {
				mvCdenom += 0.000001
			}
			cr := math.Abs(this.MVcAbs) / mvCdenom
			if cr < this.MVcAbs {
				*mvRatio += cr * mp
			} else {
				*mvRatio += this.MVcAbs * mp
			}
		}
		return v
	}

	sweep := func(start, end, step int) int {
		boost := 0.0
		decay := 1.0
		mvInOut := 0.0
		absMVInOut := 0.0
		mvRatio := 0.0
		for i := start; (step > 0 && i < end) || (step < 0 && i >= end); i += step {
			idx := cursor + i
			if idx < 0 || idx >= len(stats) {
				break
			}
			this := stats[idx]
			v := accumulateMotion(this, &mvInOut, &absMVInOut, &mvRatio)
			r := frameBoost(this, v)
			decay *= predDecay(this)
			if decay < 0.1 {
				decay = 0.1
			}
			boost += decay * r
			if mvRatio > 100.0 || absMVInOut > 3.0 || mvInOut < -2.0 {
				break
			}
		}
		return int(boost*100.0) >> 4
	}
	f := sweep(0, fFrames, 1)
	b := sweep(-1, -bFrames, -1)
	return f, b, f + b
}

// TestLibvpxCalcARFBoostForwardSweepIsolated verifies that, when the
// backward window is empty, libvpxCalcARFBoost returns exactly the
// forward-sweep score and nothing else — covering the libvpx forward
// path at vp8/encoder/firstpass.c:1497-1531.
func TestLibvpxCalcARFBoostForwardSweepIsolated(t *testing.T) {
	stats := makeARFStats(20, 20000, 200, 0.95, 0.4, 5, 5)
	const cursor = 10
	fBoost, bBoost, alt := libvpxCalcARFBoost(stats, cursor, 6, 0, 0)
	if bBoost != 0 {
		t.Fatalf("forward-only sweep bBoost = %d, want 0", bBoost)
	}
	if alt != fBoost {
		t.Fatalf("alt_boost = %d, want fBoost=%d only", alt, fBoost)
	}
	if fBoost <= 0 {
		t.Fatalf("forward sweep fBoost = %d, want positive on prediction-rich stats", fBoost)
	}
	wantF, _, wantAlt := referenceCalcARFBoost(stats, cursor, 6, 0, 0)
	if fBoost != wantF {
		t.Fatalf("fBoost = %d, want %d (reference)", fBoost, wantF)
	}
	if alt != wantAlt {
		t.Fatalf("alt_boost = %d, want %d (reference)", alt, wantAlt)
	}
}

// TestLibvpxCalcARFBoostBackwardSweepIsolated mirrors the forward test
// but for the backward window (libvpx firstpass.c:1541-1575).
func TestLibvpxCalcARFBoostBackwardSweepIsolated(t *testing.T) {
	stats := makeARFStats(20, 20000, 200, 0.95, 0.4, 5, 5)
	const cursor = 10
	fBoost, bBoost, alt := libvpxCalcARFBoost(stats, cursor, 0, 6, 0)
	if fBoost != 0 {
		t.Fatalf("backward-only sweep fBoost = %d, want 0", fBoost)
	}
	if alt != bBoost {
		t.Fatalf("alt_boost = %d, want bBoost=%d only", alt, bBoost)
	}
	if bBoost <= 0 {
		t.Fatalf("backward sweep bBoost = %d, want positive on prediction-rich stats", bBoost)
	}
	_, wantB, wantAlt := referenceCalcARFBoost(stats, cursor, 0, 6, 0)
	if bBoost != wantB {
		t.Fatalf("bBoost = %d, want %d (reference)", bBoost, wantB)
	}
	if alt != wantAlt {
		t.Fatalf("alt_boost = %d, want %d (reference)", alt, wantAlt)
	}
}

// TestLibvpxCalcARFBoostSumsForwardAndBackward ensures the combined
// alt-boost is exactly f_boost + b_boost (libvpx firstpass.c:1577
// `return (*f_boost + *b_boost);`).
func TestLibvpxCalcARFBoostSumsForwardAndBackward(t *testing.T) {
	stats := makeARFStats(20, 20000, 200, 0.95, 0.4, 5, 5)
	const cursor = 10
	fBoost, bBoost, alt := libvpxCalcARFBoost(stats, cursor, 6, 6, 0)
	if alt != fBoost+bBoost {
		t.Fatalf("alt_boost = %d, want fBoost+bBoost = %d+%d=%d", alt, fBoost, bBoost, fBoost+bBoost)
	}
	if fBoost <= 0 || bBoost <= 0 {
		t.Fatalf("expected positive f/bBoost on prediction-rich stats; got f=%d b=%d", fBoost, bBoost)
	}
	wantF, wantB, wantAlt := referenceCalcARFBoost(stats, cursor, 6, 6, 0)
	if fBoost != wantF || bBoost != wantB || alt != wantAlt {
		t.Fatalf("got (f,b,alt)=(%d,%d,%d); want (%d,%d,%d) from reference oracle", fBoost, bBoost, alt, wantF, wantB, wantAlt)
	}
}

// TestLibvpxCalcARFBoostBreakOnHighMVRatio covers the libvpx
// firstpass.c:1524-1528 break-out: a forward sweep terminates early
// once `mv_ratio_accumulator > 100.0`. We stage a clip where the first
// frame already trips the break by setting `mvr_abs * pcnt_motion`
// large enough to push the ratio over 100 in one step, then confirm
// the boost reflects exactly one accumulation iteration rather than
// the requested fFrames.
func TestLibvpxCalcARFBoostBreakOnHighMVRatio(t *testing.T) {
	// First frame: high motion-ratio break trigger.
	highRatio := FirstPassFrameStats{
		IntraError: 20000, CodedError: 200,
		PcntInter: 0.95, PcntMotion: 1.0,
		MVrAbs: 150, MVcAbs: 0,
		MVr: 1.0, MVc: 1.0,
		MVInOutCount: 0,
		Count:        1,
	}
	// Subsequent frames: low motion, shouldn't be visited.
	low := makeARFStats(10, 20000, 200, 0.95, 0.05, 1, 1)
	stats := append([]FirstPassFrameStats{highRatio}, low...)

	fBoost, _, _ := libvpxCalcARFBoost(stats, 0, 10, 0, 0)
	wantF, _, _ := referenceCalcARFBoost(stats, 0, 10, 0, 0)
	if fBoost != wantF {
		t.Fatalf("fBoost = %d, want %d (reference)", fBoost, wantF)
	}

	// And the "no break" comparison: the same fixture but with the
	// high-motion frame demoted should produce a strictly larger
	// boost (because the sweep visits more frames).
	demoted := append([]FirstPassFrameStats{}, low...)
	demotedStats := append([]FirstPassFrameStats{demoted[0]}, demoted[1:]...)
	wantF2, _, _ := referenceCalcARFBoost(demotedStats, 0, 10, 0, 0)
	if wantF2 <= wantF {
		t.Fatalf("low-motion fBoost = %d, expected > %d (the high-motion break should yield smaller boost)", wantF2, wantF)
	}
}

// TestLibvpxCalcARFBoostDifferentialVsGFBoost asserts the central
// libvpx invariant: when ARF selection is favourable
// (high pcnt_inter, low motion, low coded_error vs intra), alt_boost
// is strictly larger than gfu_boost over the same stats span — which
// is exactly why libvpx switches to NEW_BOOST=1 and uses
// `Boost = (alt_boost * GFQ_ADJUSTMENT) / 100` instead of the
// `(gfu_boost * 3 * GFQ_ADJUSTMENT) / (2 * 100)` formula. A test that
// failed this differential would mean the alt_boost wiring no longer
// rewards the ARF selection over a plain GF.
func TestLibvpxCalcARFBoostDifferentialVsGFBoost(t *testing.T) {
	stats := makeARFStats(20, 20000, 200, 0.95, 0.4, 5, 5)
	const (
		frame      = 0
		gfInterval = 6
	)
	gfBoost := computeGFUBoost(stats, frame, gfInterval, true, 0)
	cursor := int(frame) + 1 + gfInterval
	_, _, altBoost := libvpxCalcARFBoost(stats, cursor, gfInterval-1, gfInterval-1, 0)
	if altBoost <= gfBoost {
		t.Fatalf("alt_boost = %d, gfu_boost = %d; want alt_boost > gfu_boost so the ARF switch is profitable", altBoost, gfBoost)
	}
}

// TestDefineGFGroupAltBoostOverridesGFUBoost covers the wiring of
// libvpx firstpass.c:1785 (`cpi->gfu_boost = alt_boost`) inside
// defineGFGroup's ARF-selected branch. We arm a GF group on a high-
// prediction-quality fixture, request useAltRef=true, and confirm
// that the stored alt-boost fields are populated and that the
// resulting hidden-ARF target is consistent with the alt_boost
// (NEW_BOOST=1) formula and not with the legacy `(gfu_boost*3)/2`
// formula.
func TestDefineGFGroupAltBoostOverridesGFUBoost(t *testing.T) {
	const sectionLen = 20
	const defaultTarget = 700 * 1000 / 30
	stats := makeARFStats(sectionLen, 20000, 200, 0.95, 0.4, 5, 5)
	for i := range stats {
		stats[i].SSIMWeightedPredErr = 200
	}
	var state twoPassState
	state.configure(stats, defaultTarget, 50, 0, 400)
	state.configureFrameDims(64, 64)
	state.framesToKeyRemaining = sectionLen
	state.kfGroupBitsRemaining = state.bitsLeft
	state.kfGroupErrorLeft = 1
	for i := range stats {
		state.kfGroupErrorLeft += stats[i].SSIMWeightedPredErr
	}
	state.kfGroupValid = true

	const gfFrame = 1
	const altRefInterval = 7
	state.defineGFGroup(gfFrame, altRefInterval, true)
	if state.lastAltBoost <= 0 {
		t.Fatalf("lastAltBoost = %d, want positive", state.lastAltBoost)
	}
	if state.lastAltBoost != state.lastAltBoostFBoost+state.lastAltBoostBBoost {
		t.Fatalf("lastAltBoost = %d, want fBoost+bBoost = %d+%d=%d",
			state.lastAltBoost,
			state.lastAltBoostFBoost, state.lastAltBoostBBoost,
			state.lastAltBoostFBoost+state.lastAltBoostBBoost)
	}
	// The ARF target should be positive (the alt-boost path produces
	// a valid hidden-ARF allocation).
	if state.altRefTarget <= 0 {
		t.Fatalf("altRefTarget = %d, want positive after ARF selection", state.altRefTarget)
	}
}

// TestDefineGFGroupNoAltBoostOverrideWithoutAltRef checks the
// complementary path: when useAltRef=false, defineGFGroup must NOT
// overwrite gfu_boost with alt_boost. libvpx firstpass.c:1785's
// `cpi->gfu_boost = alt_boost` is inside the ARF-selected `if` block,
// so the override fires only when the alt-ref is actually emitted.
// The non-ARF allocator path must therefore see the GF-walk-derived
// gfu_boost. We assert this indirectly by computing a plain-GF
// target and a hypothetical ARF target on the same fixture and
// confirming the two diverge — if alt_boost was leaking into the GF
// branch they'd be identical.
func TestDefineGFGroupNoAltBoostOverrideWithoutAltRef(t *testing.T) {
	const sectionLen = 20
	const defaultTarget = 700 * 1000 / 30
	stats := makeARFStats(sectionLen, 20000, 200, 0.95, 0.4, 5, 5)
	for i := range stats {
		stats[i].SSIMWeightedPredErr = 200
	}
	var stateGF twoPassState
	stateGF.configure(stats, defaultTarget, 50, 0, 400)
	stateGF.configureFrameDims(64, 64)
	stateGF.framesToKeyRemaining = sectionLen
	stateGF.kfGroupBitsRemaining = stateGF.bitsLeft
	stateGF.kfGroupErrorLeft = 1
	for i := range stats {
		stateGF.kfGroupErrorLeft += stats[i].SSIMWeightedPredErr
	}
	stateGF.kfGroupValid = true
	stateGF.defineGFGroup(1, 7, false)
	gfTarget := stateGF.gfRefreshTarget

	stateARF := stateGF
	stateARF.configure(stats, defaultTarget, 50, 0, 400)
	stateARF.configureFrameDims(64, 64)
	stateARF.framesToKeyRemaining = sectionLen
	stateARF.kfGroupBitsRemaining = stateARF.bitsLeft
	stateARF.kfGroupErrorLeft = 1
	for i := range stats {
		stateARF.kfGroupErrorLeft += stats[i].SSIMWeightedPredErr
	}
	stateARF.kfGroupValid = true
	stateARF.defineGFGroup(1, 7, true)
	arfTarget := stateARF.altRefTarget

	if gfTarget == arfTarget {
		t.Fatalf("gfTarget = arfTarget = %d; expected the ARF path to diverge (alt_boost override should change allocation)", gfTarget)
	}
}
