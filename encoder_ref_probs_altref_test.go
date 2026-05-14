package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
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
func TestUpdateGoldenFrameStatsMirrorsLibvpxCounter(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{AutoAltRef: true}}

	// Plain inter frame increments frames_since_golden.
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 1 || e.sourceAltRefActive {
		t.Fatalf("plain inter -> {framesSinceGolden:%d sourceAltRefActive:%v}, want {1 false}",
			e.framesSinceGolden, e.sourceAltRefActive)
	}
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 2 {
		t.Fatalf("two plain inters frames_since_golden = %d, want 2", e.framesSinceGolden)
	}

	// Refresh alt-ref: counter resets, alt-ref becomes active.
	e.updateGoldenFrameStats(false, true)
	if e.framesSinceGolden != 0 || !e.sourceAltRefActive {
		t.Fatalf("alt-ref refresh -> {%d %v}, want {0 true}", e.framesSinceGolden, e.sourceAltRefActive)
	}

	// Plain inter after alt-ref keeps alt-ref active and increments counter.
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 1 || !e.sourceAltRefActive {
		t.Fatalf("post-altref inter -> {%d %v}, want {1 true}", e.framesSinceGolden, e.sourceAltRefActive)
	}

	// Refresh golden: counter resets, alt-ref active clears (no auto-arf
	// pending in govpx).
	e.updateGoldenFrameStats(true, false)
	if e.framesSinceGolden != 0 || e.sourceAltRefActive {
		t.Fatalf("golden refresh -> {%d %v}, want {0 false}", e.framesSinceGolden, e.sourceAltRefActive)
	}
}

// TestResetGoldenFrameStatsMirrorsLibvpxKeyFrameBranch pins
// `resetGoldenFrameStats` to the libvpx
// `update_golden_frame_stats(refresh_golden_frame=1)` keyframe branch in
// vp8/encoder/onyx_if.c. Two regimes are exercised:
//
//  1. No ARF schedule armed: source_alt_ref_active is zeroed (libvpx
//     `if (!cpi->source_alt_ref_pending) cpi->source_alt_ref_active = 0`),
//     frames_since_golden is reset, and frames_till_gf_update_due is
//     decremented (libvpx `if (frames_till_gf_update_due > 0)
//     frames_till_gf_update_due--`).
//
//  2. ARF schedule armed during the keyframe's vp8_second_pass call:
//     source_alt_ref_pending and alt_ref_source are preserved so that the
//     next vp8_get_compressed_data ARF block can fire; only
//     frames_till_alt_ref_frame decrements per the libvpx update.
func TestResetGoldenFrameStatsMirrorsLibvpxKeyFrameBranch(t *testing.T) {
	t.Run("no-arf-schedule", func(t *testing.T) {
		e := &VP8Encoder{
			framesSinceGolden:     7,
			sourceAltRefActive:    true,
			sourceAltRefPending:   false,
			altRefSourceValid:     false,
			framesTillAltRefFrame: 5,
		}
		e.resetGoldenFrameStats()
		if e.framesSinceGolden != 0 || e.sourceAltRefActive ||
			e.framesTillAltRefFrame != 4 {
			t.Fatalf("post-keyframe state = {fsg:%d active:%v till:%d}, want {0 false 4}",
				e.framesSinceGolden, e.sourceAltRefActive, e.framesTillAltRefFrame)
		}
	})
	t.Run("preserves-arf-schedule", func(t *testing.T) {
		e := &VP8Encoder{
			framesSinceGolden:     3,
			sourceAltRefActive:    true,
			sourceAltRefPending:   true,
			altRefSourceValid:     true,
			altRefSourcePTS:       1234,
			framesTillAltRefFrame: 7,
		}
		e.resetGoldenFrameStats()
		if e.framesSinceGolden != 0 {
			t.Fatalf("framesSinceGolden = %d, want 0", e.framesSinceGolden)
		}
		if !e.sourceAltRefActive {
			t.Fatalf("sourceAltRefActive cleared while pending=true; libvpx only zeroes active when !pending")
		}
		if !e.sourceAltRefPending || !e.altRefSourceValid || e.altRefSourcePTS != 1234 {
			t.Fatalf("ARF schedule mutated: pending=%v valid=%v pts=%d, want true/true/1234",
				e.sourceAltRefPending, e.altRefSourceValid, e.altRefSourcePTS)
		}
		if e.framesTillAltRefFrame != 6 {
			t.Fatalf("framesTillAltRefFrame = %d, want 6 (decremented from 7)", e.framesTillAltRefFrame)
		}
	})
}

// TestClearAltRefScheduleDropsPendingState pins the lifecycle reset path used
// from Reset()/encoder init: dropping any in-flight ARF schedule entirely so
// that no leftover pending state survives into a fresh stream.
func TestClearAltRefScheduleDropsPendingState(t *testing.T) {
	e := &VP8Encoder{
		sourceAltRefPending:   true,
		altRefSourceValid:     true,
		altRefSourcePTS:       42,
		framesTillAltRefFrame: 5,
	}
	e.clearAltRefSchedule()
	if e.sourceAltRefPending || e.altRefSourceValid || e.framesTillAltRefFrame != 0 {
		t.Fatalf("post-clear state = {pending:%v valid:%v till:%d}, want {false false 0}",
			e.sourceAltRefPending, e.altRefSourceValid, e.framesTillAltRefFrame)
	}
}

// TestScheduleAltRefSourceArmsPendingFlagAndPTS pins the libvpx
// `cpi->source_alt_ref_pending = 1; cpi->alt_ref_source = source` set
// inside vp8_get_compressed_data: scheduling the ARF arms the pending
// flag, records the PTS, and primes frames_till_alt_ref_frame.
func TestScheduleAltRefSourceArmsPendingFlagAndPTS(t *testing.T) {
	var e VP8Encoder
	e.scheduleAltRefSource(1234, 7)
	if !e.sourceAltRefPending {
		t.Fatalf("sourceAltRefPending after schedule = false, want true")
	}
	if !e.altRefSourceValid || e.altRefSourcePTS != 1234 {
		t.Fatalf("altRefSourcePTS = %d valid=%v, want 1234 valid=true",
			e.altRefSourcePTS, e.altRefSourceValid)
	}
	if e.framesTillAltRefFrame != 7 {
		t.Fatalf("framesTillAltRefFrame = %d, want 7", e.framesTillAltRefFrame)
	}
}

// TestIsSrcFrameAltRefMatchesScheduledPTS pins the libvpx
// is_src_frame_alt_ref = (alt_ref_source != NULL && source ==
// alt_ref_source) check.
func TestIsSrcFrameAltRefMatchesScheduledPTS(t *testing.T) {
	var e VP8Encoder
	if e.isSrcFrameAltRef(1234) {
		t.Fatalf("unscheduled frame should not match")
	}
	e.scheduleAltRefSource(1234, 7)
	if !e.isSrcFrameAltRef(1234) {
		t.Fatalf("scheduled PTS should match")
	}
	if e.isSrcFrameAltRef(9999) {
		t.Fatalf("non-matching PTS should not be ARF source")
	}
}

// TestUpdateGoldenFrameStatsCountsDownAltRefFrame pins the libvpx
// `if (cpi->frames_till_alt_ref_frame) cpi->frames_till_alt_ref_frame--`
// counter.
func TestUpdateGoldenFrameStatsCountsDownAltRefFrame(t *testing.T) {
	var e VP8Encoder
	e.scheduleAltRefSource(1234, 3)
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 2 {
		t.Fatalf("frames_till_alt_ref_frame after first inter = %d, want 2", e.framesTillAltRefFrame)
	}
	e.updateGoldenFrameStats(false, false)
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 0 {
		t.Fatalf("frames_till_alt_ref_frame after 3 inters = %d, want 0", e.framesTillAltRefFrame)
	}
	// Counter should not go negative.
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 0 {
		t.Fatalf("frames_till_alt_ref_frame after underflow = %d, want 0 floor", e.framesTillAltRefFrame)
	}
}

// TestUpdateGoldenFrameStatsAltRefRefreshClearsPending pins the libvpx
// update_alt_ref_frame_stats branch: a successful ARF refresh consumes
// the pending flag. The branch is gated on AutoAltRef
// (libvpx oxcf.play_alternate) — see TestUpdateGoldenFrameStatsMirrorsLibvpxCounter
// for the dispatcher reference.
func TestUpdateGoldenFrameStatsAltRefRefreshClearsPending(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{AutoAltRef: true}, sourceAltRefPending: true, framesTillAltRefFrame: 2}
	e.updateGoldenFrameStats(false, true)
	if e.sourceAltRefPending {
		t.Fatalf("sourceAltRefPending after ARF refresh = true, want false (consumed)")
	}
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive after ARF refresh = false, want true")
	}
}

// TestUpdateGoldenFrameStatsDirectAltRefRefreshSkipsPlainInterCounters pins the
// libvpx update_golden_frame_stats path when refresh_alt_ref_frame=1 but
// play_alternate is disabled: the function does not enter the plain inter
// `else if (!refresh_alt_ref_frame)` branch, so frames_since_golden and
// frames_till_alt_ref_frame are left untouched.
func TestUpdateGoldenFrameStatsDirectAltRefRefreshSkipsPlainInterCounters(t *testing.T) {
	e := &VP8Encoder{framesSinceGolden: 7, framesTillAltRefFrame: 3}
	e.updateGoldenFrameStats(false, true)
	if e.framesSinceGolden != 7 || e.framesTillAltRefFrame != 3 || e.sourceAltRefActive {
		t.Fatalf("direct ALTREF refresh state = {since:%d till:%d active:%v}, want {7 3 false}",
			e.framesSinceGolden, e.framesTillAltRefFrame, e.sourceAltRefActive)
	}
}

func TestSnapshotDroppedFrameCoefProbsMirrorsRefreshMask(t *testing.T) {
	var e VP8Encoder
	e.coefProbsSnapshotsValid = true
	e.coefProbs[0][0][0][0] = 77
	e.coefProbsLast[0][0][0][0] = 11
	e.coefProbsGolden[0][0][0][0] = 22
	e.coefProbsAltRef[0][0][0][0] = 33

	e.snapshotDroppedFrameCoefProbs(true, false, false)
	if got := e.coefProbsLast[0][0][0][0]; got != 77 {
		t.Fatalf("LAST snapshot = %d, want 77", got)
	}
	if got := e.coefProbsGolden[0][0][0][0]; got != 22 {
		t.Fatalf("GOLDEN snapshot changed to %d, want 22", got)
	}
	if got := e.coefProbsAltRef[0][0][0][0]; got != 33 {
		t.Fatalf("ALTREF snapshot changed to %d, want 33", got)
	}
}

// TestUpdateGoldenFrameStatsGoldenRefreshKeepsActiveOnPending pins the
// libvpx `if (!source_alt_ref_pending) source_alt_ref_active = 0`
// branch: when an ARF is still pending, refreshing GOLDEN does not
// clear the active flag.
func TestUpdateGoldenFrameStatsGoldenRefreshKeepsActiveOnPending(t *testing.T) {
	e := &VP8Encoder{sourceAltRefActive: true, sourceAltRefPending: true}
	e.updateGoldenFrameStats(true, false)
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive after GF refresh with ARF pending = false, want true (gated)")
	}
}

func TestEncodeIntoAltRefSignBiasFollowsLibvpxSourceAltRefActive(t *testing.T) {
	// AutoAltRef mirrors libvpx oxcf.play_alternate; without it the
	// vp8/encoder/onyx_if.c:4724-4732 dispatcher routes refresh_alt_ref_frame=1
	// through update_golden_frame_stats (which leaves source_alt_ref_active=0),
	// so the ALTREF sign-bias activation seen here only fires when AutoAltRef
	// is set. The frame-flags byte-parity oracle covers the no-AutoAltRef path.
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		AutoAltRef:          true,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	keySrc := testImage(16, 16)
	altSrc := testImage(16, 16)
	interSrc := testImage(16, 16)
	fillImage(keySrc, 220, 90, 170)
	fillImage(altSrc, 40, 91, 171)
	fillImage(interSrc, 60, 92, 172)
	dst := make([]byte, 4096)

	if _, err := e.EncodeInto(dst, keySrc, 0, 1, EncodeForceKeyFrame); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	altRefresh, err := e.EncodeInto(dst, altSrc, 1, 1, EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("alt refresh EncodeInto returned error: %v", err)
	}
	altState := packetState(t, altRefresh.Data)
	if altState.Refresh.AltRefSignBias {
		t.Fatalf("alt-refresh frame AltRefSignBias = true, want false before update_alt_ref_frame_stats activates ALTREF")
	}
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive = false after ALTREF refresh, want true")
	}
	if len(altRefresh.Data) == 0 {
		t.Fatalf("alt refresh wrote no packet data")
	}

	inter, err := e.EncodeInto(dst, interSrc, 2, 1, 0)
	if err != nil {
		t.Fatalf("post-altref inter EncodeInto returned error: %v", err)
	}
	interState := packetState(t, inter.Data)
	if !interState.Refresh.AltRefSignBias || interState.Refresh.GoldenSignBias {
		t.Fatalf("post-altref sign bias = golden:%v alt:%v, want golden:false alt:true", interState.Refresh.GoldenSignBias, interState.Refresh.AltRefSignBias)
	}

	// FORCE_GF in isolation maps (via libvpx vp8e_set_frame_flags
	// upd-mask) to refresh_last=refresh_golden=refresh_alt_ref=1, which
	// re-routes the post-encode dispatcher to update_alt_ref_frame_stats
	// (play_alternate=AutoAltRef=true on this encoder) and keeps
	// sourceAltRefActive=true. To exercise the libvpx "GOLDEN refresh
	// while ALTREF active" branch (update_golden_frame_stats with
	// refresh_golden_frame=1 and refresh_alt_ref_frame=0), the user has
	// to opt out of the ALTREF half of the FORCE_GF mask with
	// EncodeNoUpdateAltRef.
	golden, err := e.EncodeInto(dst, interSrc, 3, 1, EncodeForceGoldenFrame|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("golden refresh EncodeInto returned error: %v", err)
	}
	goldenState := packetState(t, golden.Data)
	if !goldenState.Refresh.AltRefSignBias {
		t.Fatalf("golden-refresh frame AltRefSignBias = false, want true while ALTREF was active for this frame")
	}
	if e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive = true after GOLDEN refresh, want false")
	}
}

// TestSignBiasEvolutionMatchesLibvpxAcrossGFAndARF drives a 12-frame sequence
// with AutoAltRef enabled so the encoder produces a key frame, several inter
// frames, a hidden ARF refresh, the matching deferred show frame, more inter
// frames, and a forced GOLDEN refresh. For each emitted packet it parses the
// (golden_sign_bias, altref_sign_bias) header bits and asserts they match the
// libvpx evolution rule out of vp8/encoder/onyx_if.c:
//
//   - GOLDEN sign bias is always 0: update_golden_frame_stats never flips
//     ref_frame_sign_bias[GOLDEN_FRAME].
//   - ALTREF sign bias at frame N equals cpi->source_alt_ref_active as seen
//     ENTERING frame N. update_alt_ref_frame_stats sets source_alt_ref_active
//     AFTER the hidden ARF refresh, so the refresh frame itself encodes the
//     prior bias (false). The first show frame after the hidden ARF then
//     encodes (false, true). update_golden_frame_stats clears the active
//     flag on a GOLDEN refresh ONLY if no ARF is pending; the GOLDEN refresh
//     frame still encodes the prior bias because the clear runs AFTER pack.
//
// The expected per-packet tuple is derived by replaying libvpx's two stat
// updates against each packet's RefreshAltRef / RefreshGolden bits, so any
// drift between govpx's interFrameSignBias() / updateGoldenFrameStats() and
// libvpx's update_alt_ref_frame_stats / update_golden_frame_stats surfaces
// here as a per-frame tuple mismatch with the failing frame index pinned.
func TestSignBiasEvolutionMatchesLibvpxAcrossGFAndARF(t *testing.T) {
	e := newAutoAltRefTestEncoder(t)
	const frameCount = 12
	const width = 32
	const height = 32
	dst := make([]byte, 1<<16)
	type emittedFrame struct {
		index      int
		pts        uint64
		key        bool
		show       bool
		refresh    vp8dec.RefreshHeader
		forcedGold bool
	}
	emitted := make([]emittedFrame, 0, frameCount+8)
	pushPacket := func(idx int, pts uint64, data []byte, forcedGold bool) {
		t.Helper()
		hdr, err := vp8dec.ParseFrameHeader(data)
		if err != nil {
			t.Fatalf("ParseFrameHeader frame %d (pts=%d): %v", idx, pts, err)
		}
		state := parseEncoderStateHeader(t, data)
		emitted = append(emitted, emittedFrame{
			index:      idx,
			pts:        pts,
			key:        hdr.KeyFrame(),
			show:       hdr.ShowFrame,
			refresh:    state.Refresh,
			forcedGold: forcedGold,
		})
	}
	// Drive frameCount source frames; force a GOLDEN refresh on frame 10 so
	// the evolution covers a forced GF refresh AFTER the auto-ARF has
	// activated (libvpx's "GOLDEN refresh while ALTREF active" branch). The
	// auto-ARF driver's hidden ARF and matching deferred show frame are
	// scheduled naturally by the lookahead during the early frames.
	for i := range frameCount {
		img := movingBarTestImage(width, height, i)
		var flags EncodeFlags
		forced := false
		if i == 10 {
			// FORCE_GF alone runs through libvpx's upd-mask and sets
			// refresh_alt_ref=1 too, which would re-route the post-encode
			// dispatcher away from update_golden_frame_stats. Opt out of
			// the ALTREF half of the mask so the libvpx "GOLDEN refresh
			// while ALTREF active" branch (update_golden_frame_stats
			// with refresh_golden=1, refresh_alt_ref=0) actually fires.
			flags = EncodeForceGoldenFrame | EncodeNoUpdateAltRef
			forced = true
		}
		result, err := e.EncodeInto(dst, img, uint64(i)*1000, 1000, flags)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		pushPacket(i, result.PTS, append([]byte(nil), result.Data...), forced)
	}
	for {
		result, err := e.FlushInto(dst)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		pushPacket(-1, result.PTS, append([]byte(nil), result.Data...), false)
	}
	if len(emitted) == 0 {
		t.Fatalf("no packets emitted")
	}
	// The test only buys parity coverage if at least one hidden ARF, one
	// deferred show frame, and one forced GOLDEN refresh actually fire in
	// the captured stream.
	hiddenSeen := false
	deferredShowSeen := false
	goldenRefreshSeen := false
	for i, p := range emitted {
		if !p.key && !p.show && p.refresh.RefreshAltRef {
			hiddenSeen = true
			// The deferred show frame is the next visible non-key packet.
			for j := i + 1; j < len(emitted); j++ {
				if !emitted[j].key && emitted[j].show {
					deferredShowSeen = true
					break
				}
			}
		}
		if !p.key && p.refresh.RefreshGolden && !p.refresh.RefreshAltRef {
			goldenRefreshSeen = true
		}
	}
	if !hiddenSeen {
		t.Fatalf("expected at least one hidden ARF in the captured stream; got %d packets", len(emitted))
	}
	if !deferredShowSeen {
		t.Fatalf("expected at least one deferred show frame after the hidden ARF")
	}
	if !goldenRefreshSeen {
		t.Fatalf("expected at least one GOLDEN refresh in the captured stream")
	}
	// Replay libvpx's per-frame sign-bias derivation against each packet.
	// State entering frame N is (active, pending). For each packet:
	//   1. Expected bias = (false, active) — the libvpx onyx_if.c
	//      pre-pack write at line 3397-3401 reads source_alt_ref_active
	//      and never flips GOLDEN.
	//   2. Update active/pending using update_alt_ref_frame_stats /
	//      update_golden_frame_stats semantics for the refresh bits in the
	//      packet (and reset to (false,false) on a key frame).
	active := false
	pending := false
	for i, p := range emitted {
		var wantGolden, wantAltRef bool
		if p.key {
			// Key frame's RefreshHeader has no sign-bias bits, so the
			// decoder leaves them as the zero value. After the key
			// frame libvpx clears source_alt_ref_active /
			// source_alt_ref_pending in resetGoldenFrameStats.
			wantGolden = false
			wantAltRef = false
		} else {
			wantGolden = false
			wantAltRef = active
		}
		gotGolden := p.refresh.GoldenSignBias
		gotAltRef := p.refresh.AltRefSignBias
		if gotGolden != wantGolden || gotAltRef != wantAltRef {
			t.Fatalf("packet %d (src=%d pts=%d key=%v show=%v refLast=%v refGold=%v refARF=%v forcedGold=%v) sign-bias = (golden=%v, altref=%v), want (golden=%v, altref=%v); state entering frame: active=%v pending=%v",
				i, p.index, p.pts, p.key, p.show,
				p.refresh.RefreshLast, p.refresh.RefreshGolden, p.refresh.RefreshAltRef,
				p.forcedGold,
				gotGolden, gotAltRef, wantGolden, wantAltRef,
				active, pending)
		}
		// Advance (active, pending) using the libvpx update rules.
		if p.key {
			active = false
			pending = false
			continue
		}
		if p.refresh.RefreshAltRef {
			// update_alt_ref_frame_stats: clears pending, sets active.
			active = true
			pending = false
			continue
		}
		if p.refresh.RefreshGolden {
			// update_golden_frame_stats: when no ARF is pending the
			// active flag clears; when one is pending it stays.
			if !pending {
				active = false
			}
		}
		// Non-refresh inter frames leave (active, pending) unchanged for
		// the purposes of the sign-bias derivation. govpx's auto-ARF
		// driver may set pending later via scheduleAltRefSource, but
		// pending alone never affects ref_frame_sign_bias[ALTREF_FRAME];
		// only update_alt_ref_frame_stats does, and that runs on a
		// hidden ARF commit.
		_ = pending
	}
}
