package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

// TestInterReferenceFrameRateUsesLivePrevFrameProbs locks in libvpx parity for
// vp8_calc_ref_frame_costs: ref-frame selection bits are charged against the
// previous frame's prob_last_coded / prob_gf_coded, not a static 128 prior.
func TestInterReferenceFrameRateUsesLivePrevFrameProbs(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 50, refProbLast: 200, refProbGolden: 90}
	if got, want := e.interReferenceFrameRate(vp8common.LastFrame), vp8enc.BoolBitCost(200, 0); got != want {
		t.Fatalf("LAST rate = %d, want %d", got, want)
	}
	if got, want := e.interReferenceFrameRate(vp8common.GoldenFrame), vp8enc.BoolBitCost(200, 1)+vp8enc.BoolBitCost(90, 0); got != want {
		t.Fatalf("GOLDEN rate = %d, want %d", got, want)
	}
	if got, want := e.interReferenceFrameRate(vp8common.AltRefFrame), vp8enc.BoolBitCost(200, 1)+vp8enc.BoolBitCost(90, 1); got != want {
		t.Fatalf("ALTREF rate = %d, want %d", got, want)
	}
}

func TestThreadedHelperRowsUseZeroReferenceFrameRate(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 200, refProbGolden: 90, probSkipFalse: 200}
	normalGolden := vp8enc.BoolBitCost(200, 1) + vp8enc.BoolBitCost(90, 0)
	if got := e.interReferenceFrameRate(vp8common.GoldenFrame); got != normalGolden {
		t.Fatalf("normal helper-disabled GOLDEN rate = %d, want %d", got, normalGolden)
	}
	if got, want := e.interIntraMacroblockModeRate(), vp8enc.BoolBitCost(200, 0)+vp8enc.BoolBitCost(63, 0); got != want {
		t.Fatalf("normal helper-disabled intra MB rate = %d, want %d", got, want)
	}

	e.threadedHelperRowsActive = true
	if got := e.interIntraReferenceRate(); got != 0 {
		t.Fatalf("helper intra-reference rate = %d, want 0", got)
	}
	if got := e.interInterReferenceRate(12345); got != 0 {
		t.Fatalf("helper inter-reference rate = %d, want 0", got)
	}
	if got := e.interReferenceFrameRate(vp8common.GoldenFrame); got != 0 {
		t.Fatalf("helper GOLDEN rate = %d, want 0", got)
	}
	ref := interAnalysisReference{Frame: vp8common.GoldenFrame, RefRateSet: true, RefRate: 12345}
	if got := e.interReferenceFrameRateForReference(ref); got != 0 {
		t.Fatalf("helper explicit reference rate = %d, want 0", got)
	}
	if got, want := e.interIntraMacroblockModeRate(), e.interMacroblockSkipRate(false); got != want {
		t.Fatalf("helper intra MB rate = %d, want skip-only %d", got, want)
	}
}

func TestInterReferenceFrameRatesForFlagsMirrorLibvpxSingleReferenceSpecialCases(t *testing.T) {
	e := &VP8Encoder{refProbLast: 200, refProbGolden: 90}
	last, golden, alt := e.interReferenceFrameRatesForFlags(0)
	if want := vp8enc.BoolBitCost(200, 0); last != want {
		t.Fatalf("all-ref LAST rate = %d, want %d", last, want)
	}
	if want := vp8enc.BoolBitCost(200, 1) + vp8enc.BoolBitCost(90, 0); golden != want {
		t.Fatalf("all-ref GOLDEN rate = %d, want %d", golden, want)
	}
	if want := vp8enc.BoolBitCost(200, 1) + vp8enc.BoolBitCost(90, 1); alt != want {
		t.Fatalf("all-ref ALTREF rate = %d, want %d", alt, want)
	}

	last, _, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceGolden | EncodeNoReferenceAltRef)
	if want := vp8enc.BoolBitCost(255, 0); last != want {
		t.Fatalf("single-LAST rate = %d, want libvpx special-case %d", last, want)
	}

	e.opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringOneLayer}
	_, golden, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceAltRef)
	if want := vp8enc.BoolBitCost(200, 1) + vp8enc.BoolBitCost(90, 0); golden != want {
		t.Fatalf("one-layer single-GOLDEN rate = %d, want non-temporal live cost %d", golden, want)
	}

	e.opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}
	_, golden, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceAltRef)
	if want := vp8enc.BoolBitCost(1, 1) + vp8enc.BoolBitCost(255, 0); golden != want {
		t.Fatalf("temporal single-GOLDEN rate = %d, want libvpx special-case %d", golden, want)
	}
	_, _, alt = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceGolden)
	if want := vp8enc.BoolBitCost(1, 1) + vp8enc.BoolBitCost(1, 1); alt != want {
		t.Fatalf("temporal single-ALTREF rate = %d, want libvpx special-case %d", alt, want)
	}
}

func TestInterAnalysisReferencesCarryLibvpxFlagSpecificReferenceRates(t *testing.T) {
	e := &VP8Encoder{refProbLast: 200, refProbGolden: 90}
	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(EncodeNoReferenceGolden|EncodeNoReferenceAltRef, &refs)
	if count != 1 || refs[0].Frame != vp8common.LastFrame || !refs[0].RefRateSet {
		t.Fatalf("single-LAST refs = count:%d ref:%+v, want one LAST with explicit rate", count, refs[0])
	}
	if want := vp8enc.BoolBitCost(255, 0); refs[0].RefRate != want {
		t.Fatalf("single-LAST carried rate = %d, want %d", refs[0].RefRate, want)
	}
}

func TestInterAnalysisReferencesPruneLibvpxAliasFlagsAfterKeyFrame(t *testing.T) {
	e := newTestEncoder(t)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	e.refreshKeyFrameReferencesFromAnalysis()
	e.frameCount = 1

	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(0, &refs)
	if count != 1 || refs[0].Frame != vp8common.LastFrame {
		t.Fatalf("post-key refs = count:%d first:%+v, want only LAST after libvpx alias pruning", count, refs[0])
	}
	if want := vp8enc.BoolBitCost(255, 0); refs[0].RefRate != want {
		t.Fatalf("post-key LAST rate = %d, want single-reference libvpx cost %d", refs[0].RefRate, want)
	}
	// Explicit NO_REF_* user masks route through libvpx's
	// vp8_use_as_reference path, which replaces ref_frame_flags with the
	// user-derived mask and bypasses the post-keyframe alias filter. When
	// LAST is explicitly masked, both GOLDEN and ALTREF remain available
	// even though the keyframe refresh seeded them with the LAST
	// reconstruction.
	if e.shouldEncodeKeyFrame(EncodeNoReferenceLast) {
		t.Fatalf("shouldEncodeKeyFrame with LAST disabled = true, want inter frame using user-selected aliased refs")
	}
	count = e.interAnalysisReferences(EncodeNoReferenceLast, &refs)
	if count != 2 || refs[0].Frame != vp8common.GoldenFrame || refs[1].Frame != vp8common.AltRefFrame {
		t.Fatalf("NoReferenceLast picker refs = count:%d refs:%+v, want GOLDEN and ALTREF user-selected aliases", count, refs[:count])
	}
	count = e.interAnalysisReferences(EncodeNoReferenceAltRef, &refs)
	if count != 2 || refs[0].Frame != vp8common.LastFrame || refs[1].Frame != vp8common.GoldenFrame {
		t.Fatalf("NoReferenceAltRef picker refs = count:%d refs:%+v, want LAST and GOLDEN user-selected aliases", count, refs[:count])
	}
}

func TestInterAnalysisReferencesKeepAltAfterInternalGoldenRefreshCopiesOldGF(t *testing.T) {
	e := newTestEncoder(t)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	e.refreshKeyFrameReferencesFromAnalysis()
	e.updateInterReferenceAliases(vp8enc.InterFrameStateConfig{
		RefreshLast:        true,
		RefreshGolden:      true,
		CopyBufferToAltRef: 2,
	})

	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(0, &refs)
	if count != 2 || refs[0].Frame != vp8common.LastFrame || refs[1].Frame != vp8common.AltRefFrame {
		t.Fatalf("post-GF-refresh refs = count:%d refs:%+v/%+v, want LAST and old-GF ALTREF", count, refs[0], refs[1])
	}
}
