package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestUpdateRefFrameProbsFromZeroReferenceMirrorsLibvpxConvertRFCT(t *testing.T) {
	modes := make([]vp8enc.InterFrameMacroblockMode, 4)
	fillZeroInterFrameModes(modes, vp8common.GoldenFrame)
	e := &VP8Encoder{
		interFrameModes: modes,
		refProbIntra:    63,
		refProbLast:     128,
		refProbGolden:   128,
	}

	e.updateRefFrameProbsFromAttempt(interFrameEncodeAttempt{ZeroReference: true})

	if e.refProbIntra != 1 || e.refProbLast != 1 || e.refProbGolden != 255 {
		t.Fatalf("zero-reference ref probs = %d/%d/%d, want libvpx RFCT 1/1/255",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

func TestCarriedExternalReferenceMaskSurvivesDroppedFrameUntilPacket(t *testing.T) {
	e := &VP8Encoder{}
	e.armExternalReferenceMask(EncodeNoReferenceAltRef)

	last, golden, alt, ok := e.currentExternalReferenceMask()
	if !ok || !last || !golden || alt {
		t.Fatalf("carried no-ref-arf mask = ok=%v last=%v golden=%v alt=%v, want ok true last/golden only",
			ok, last, golden, alt)
	}

	gotLast, gotGolden, gotAlt := e.interReferenceAvailability(0)
	if !gotLast || !gotGolden || gotAlt {
		t.Fatalf("interReferenceAvailability without current flags = last=%v golden=%v alt=%v, want carried last/golden only",
			gotLast, gotGolden, gotAlt)
	}

	e.clearExternalReferenceMaskAfterPacket()
	_, _, _, ok = e.currentExternalReferenceMask()
	if ok {
		t.Fatalf("carried no-ref mask still active after packet clear")
	}
}

func TestUpdateRefFrameProbsFromAttemptSkipsSingleLayerGFAndARFRefresh(t *testing.T) {
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
	}
	e := &VP8Encoder{
		interFrameModes: modes,
		refProbIntra:    63,
		refProbLast:     99,
		refProbGolden:   77,
	}

	e.updateRefFrameProbsFromAttempt(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{RefreshGolden: true}})
	if e.refProbIntra != 63 || e.refProbLast != 99 || e.refProbGolden != 77 {
		t.Fatalf("single-layer GF refresh converted refs = %d/%d/%d, want unchanged 63/99/77",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}

	e.updateRefFrameProbsFromAttempt(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{RefreshAltRef: true}})
	if e.refProbIntra != 63 || e.refProbLast != 99 || e.refProbGolden != 77 {
		t.Fatalf("single-layer ARF refresh converted refs = %d/%d/%d, want unchanged 63/99/77",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

func TestUpdateRefFrameProbsFromPackedAttemptConvertsSingleLayerGFRefresh(t *testing.T) {
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
		{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
		{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
		{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
		{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
		{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
		{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}
	e := &VP8Encoder{
		interFrameModes: modes,
		refProbIntra:    15,
		refProbLast:     255,
		refProbGolden:   128,
	}

	e.updateRefFrameProbsFromPackedAttempt()

	if e.refProbIntra != 111 || e.refProbLast != 141 || e.refProbGolden != 255 {
		t.Fatalf("packed GF-refresh ref probs = %d/%d/%d, want libvpx RFCT 111/141/255",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

func TestUpdateRefFrameProbsFromAttemptConvertsTemporalLayerRefresh(t *testing.T) {
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
	}
	e := &VP8Encoder{
		opts:            EncoderOptions{TemporalScalability: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}},
		interFrameModes: modes,
		refProbIntra:    63,
		refProbLast:     99,
		refProbGolden:   77,
	}

	e.updateRefFrameProbsFromAttempt(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{RefreshGolden: true}})

	if e.refProbIntra != 1 || e.refProbLast != 255 || e.refProbGolden != 128 {
		t.Fatalf("temporal GF refresh ref probs = %d/%d/%d, want libvpx RFCT 1/255/128",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

// TestApplyLibvpxRdRefFrameProbRefreshAdjustmentsAltRefRefresh verifies the
// alt-ref-refresh branch of vp8/encoder/onyx_if.c update_rd_ref_frame_probs:
// prob_intra is bumped by 40 (clamped to 255), prob_last forced to 200, and
// prob_gf set to 1 only if source_alt_ref_active is true at RD time;
// otherwise the trailing `if (!source_alt_ref_active) prob_gf = 255` clamps
// prob_gf to 255 (since the alt-ref refresh transitions the flag *after* the
// frame's RD).

func TestApplyLibvpxRdRefFrameProbRefreshAdjustmentsAltRefRefresh(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.applyLibvpxRdRefFrameProbRefreshAdjustments(true)
	if e.refProbIntra != 103 {
		t.Fatalf("alt-ref refresh prob_intra = %d, want 63+40=103", e.refProbIntra)
	}
	if e.refProbLast != 200 {
		t.Fatalf("alt-ref refresh prob_last = %d, want 200", e.refProbLast)
	}
	// source_alt_ref_active was false before this frame, so the trailing
	// libvpx override clamps prob_gf to 255.
	if e.refProbGolden != 255 {
		t.Fatalf("alt-ref refresh prob_gf = %d, want 255 (trailing clamp)", e.refProbGolden)
	}

	// When alt-ref was already active, the trailing clamp does not fire and
	// prob_gf stays at the libvpx-set 1.
	e2 := &VP8Encoder{refProbIntra: 230, refProbLast: 128, refProbGolden: 128, sourceAltRefActive: true}
	e2.applyLibvpxRdRefFrameProbRefreshAdjustments(true)
	if e2.refProbIntra != 255 {
		t.Fatalf("alt-ref refresh prob_intra clamp = %d, want 255", e2.refProbIntra)
	}
	if e2.refProbGolden != 1 {
		t.Fatalf("alt-ref refresh prob_gf = %d, want 1", e2.refProbGolden)
	}
}

func TestInterAttemptRDRefFrameProbsRestoresAndReturnsAdjustedRefreshProbs(t *testing.T) {
	e := &VP8Encoder{
		refProbIntra:      1,
		refProbLast:       47,
		refProbGolden:     1,
		framesSinceGolden: 4,
	}

	probIntra, probLast, probGolden := e.interAttemptRDRefFrameProbs(true)
	if probIntra != 41 || probLast != 200 || probGolden != 255 {
		t.Fatalf("adjusted ARF-refresh ref probs = %d/%d/%d, want 41/200/255", probIntra, probLast, probGolden)
	}
	if e.refProbIntra != 1 || e.refProbLast != 47 || e.refProbGolden != 1 {
		t.Fatalf("live ref probs after helper = %d/%d/%d, want restored 1/47/1",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

func TestInterRecodeNextRDRefFrameProbsDoesNotRepeatRefreshBias(t *testing.T) {
	e := &VP8Encoder{
		refProbIntra:      119,
		refProbLast:       200,
		refProbGolden:     255,
		framesSinceGolden: 4,
	}

	probIntra, probLast, probGolden := e.interRecodeNextRDRefFrameProbs(true, true, 0, true)
	if probIntra != 119 || probLast != 200 || probGolden != 255 {
		t.Fatalf("preconfigured GF/ARF recode probs = %d/%d/%d, want unchanged 119/200/255", probIntra, probLast, probGolden)
	}
}

func TestInterRecodeNextRDRefFrameProbsCarriesConvertedCounts(t *testing.T) {
	e := &VP8Encoder{
		interFrameModes: []vp8enc.InterFrameMacroblockMode{
			{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
			{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
			{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
			{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
		},
		refProbIntra:  119,
		refProbLast:   200,
		refProbGolden: 255,
	}

	probIntra, probLast, probGolden := e.interRecodeNextRDRefFrameProbs(false, false, 4, true)
	if probIntra != 63 || probLast != 170 || probGolden != 255 {
		t.Fatalf("converted recode probs = %d/%d/%d, want 63/170/255", probIntra, probLast, probGolden)
	}
}

func TestTemporalEmptyLayerRefUsageDefaultsRDRefFrameProbs(t *testing.T) {
	e := &VP8Encoder{
		opts: EncoderOptions{
			TemporalScalability: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayers},
		},
		refProbIntra:  1,
		refProbLast:   255,
		refProbGolden: 1,
	}

	e.updateRDRefFrameProbsForDroppedFrame(false)

	if e.refProbIntra != 63 || e.refProbLast != 128 || e.refProbGolden != 128 {
		t.Fatalf("empty temporal-layer RD probs = %d/%d/%d, want defaults 63/128/128",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

func TestTemporalNonEmptyLayerRefUsageKeepsRDRefFrameProbs(t *testing.T) {
	e := &VP8Encoder{
		opts: EncoderOptions{
			TemporalScalability: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayers},
		},
		refProbIntra:          1,
		refProbLast:           255,
		refProbGolden:         128,
		temporalLayerRefUsage: [vp8common.MaxRefFrames]int{0, 4, 0, 0},
	}

	e.updateRDRefFrameProbsForDroppedFrame(false)

	if e.refProbIntra != 1 || e.refProbLast != 255 || e.refProbGolden != 128 {
		t.Fatalf("non-empty temporal-layer RD probs = %d/%d/%d, want unchanged 1/255/128",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

// TestApplyLibvpxRdRefFrameProbRefreshAdjustmentsFramesSinceGolden verifies
// the frames_since_golden==0 / ==1 branches of update_rd_ref_frame_probs.

func TestApplyLibvpxRdRefFrameProbRefreshAdjustmentsFramesSinceGolden(t *testing.T) {
	// frames_since_golden == 0: prob_last=214; trailing clamp forces
	// prob_gf=255 because source_alt_ref_active is false.
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, framesSinceGolden: 0}
	e.applyLibvpxRdRefFrameProbRefreshAdjustments(false)
	if e.refProbLast != 214 {
		t.Fatalf("frames_since_golden=0 prob_last = %d, want 214", e.refProbLast)
	}
	if e.refProbGolden != 255 {
		t.Fatalf("frames_since_golden=0 prob_gf = %d, want 255 (trailing clamp)", e.refProbGolden)
	}

	// frames_since_golden == 1: prob_last=192, prob_gf=220, but the trailing
	// clamp overrides prob_gf to 255 when source_alt_ref_active is false.
	e = &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, framesSinceGolden: 1}
	e.applyLibvpxRdRefFrameProbRefreshAdjustments(false)
	if e.refProbLast != 192 {
		t.Fatalf("frames_since_golden=1 prob_last = %d, want 192", e.refProbLast)
	}
	if e.refProbGolden != 255 {
		t.Fatalf("frames_since_golden=1 prob_gf = %d, want 255 (trailing clamp)", e.refProbGolden)
	}

	// frames_since_golden == 1, source_alt_ref_active=true: prob_gf stays at
	// the libvpx-set 220.
	e = &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, framesSinceGolden: 1, sourceAltRefActive: true}
	e.applyLibvpxRdRefFrameProbRefreshAdjustments(false)
	if e.refProbGolden != 220 {
		t.Fatalf("alt-ref active frames_since_golden=1 prob_gf = %d, want 220", e.refProbGolden)
	}
}

// TestApplyLibvpxRdRefFrameProbRefreshAdjustmentsAltRefActiveDecay verifies
// the source_alt_ref_active branch (frames_since_golden>=2): prob_gf decays
// by 20 per frame down to a floor of 10.

func TestApplyLibvpxRdRefFrameProbRefreshAdjustmentsAltRefActiveDecay(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 100, framesSinceGolden: 5, sourceAltRefActive: true}
	e.applyLibvpxRdRefFrameProbRefreshAdjustments(false)
	if e.refProbGolden != 80 {
		t.Fatalf("alt-ref active decay prob_gf = %d, want 100-20=80", e.refProbGolden)
	}

	// Floor clamp at 10.
	e = &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 15, framesSinceGolden: 5, sourceAltRefActive: true}
	e.applyLibvpxRdRefFrameProbRefreshAdjustments(false)
	if e.refProbGolden != 10 {
		t.Fatalf("alt-ref active decay floor prob_gf = %d, want 10", e.refProbGolden)
	}

	// frames_since_golden>=2 with source_alt_ref_active=false: no branch
	// matched, trailing clamp sets prob_gf=255.
	e = &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 100, framesSinceGolden: 5, sourceAltRefActive: false}
	e.applyLibvpxRdRefFrameProbRefreshAdjustments(false)
	if e.refProbGolden != 255 {
		t.Fatalf("inactive alt-ref far-from-golden prob_gf = %d, want 255", e.refProbGolden)
	}
}

func TestUpdateRefFrameProbsFromKeyFrameMirrorsLibvpx(t *testing.T) {
	e := &VP8Encoder{
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
	}
	e.updateRefFrameProbsFromKeyFrame()
	if e.refProbIntra != 255 || e.refProbLast != 214 || e.refProbGolden != 255 {
		t.Fatalf("keyframe ref probs = %d/%d/%d, want libvpx 255/214/255",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

// TestUpdateGoldenFrameStatsMirrorsLibvpxCounter verifies the lifecycle of
// frames_since_golden / source_alt_ref_active matches libvpx's
// update_alt_ref_frame_stats / update_golden_frame_stats: refreshing alt-ref
// resets frames_since_golden and sets source_alt_ref_active=true; refreshing
// golden resets frames_since_golden and clears source_alt_ref_active; plain
// inter frames increment the counter.
//
// The libvpx dispatcher at vp8/encoder/onyx_if.c:4724-4732 calls
// update_alt_ref_frame_stats only when (oxcf.play_alternate &&
// refresh_alt_ref_frame); govpx mirrors that with the AutoAltRef gate, so
// this test runs with AutoAltRef=true to exercise the auto-ARF lifecycle
// (the no-AutoAltRef ARF-refresh path is covered separately by the
// frame-flags byte-parity oracle).
