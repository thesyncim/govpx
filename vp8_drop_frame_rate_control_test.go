package govpx

import "testing"

// These tests pin the libvpx v1.16.0 buffer-underrun drop path,
// tight-buffer P-target undershoot bookkeeping, and the surrounding
// vp8/encoder/ratectrl.c calc_pframe_target_size buffer-underrun drop
// (lines 861-899) and the surrounding tight-buffer percent_low p-frame
// target shrink (lines 695-727), then verify the post-drop pid-controller
// bookkeeping (rate_correction_factor untouched, total_actual_bits
// untouched, framesSinceKeyframe bumped) matches libvpx's
// vp8_pick_frame_size + encode_frame_to_data_rate return contract.

// TestVP8DropFrameRefundsAvailableBandwidth pins libvpx
// vp8/encoder/ratectrl.c lines 879-883 verbatim:
//
//	cpi->bits_off_target += cpi->av_per_frame_bandwidth;
//	if (cpi->bits_off_target > cpi->oxcf.maximum_buffer_size)
//	    cpi->bits_off_target = (int)cpi->oxcf.maximum_buffer_size;
//	cpi->buffer_level = cpi->bits_off_target;
//
// av_per_frame_bandwidth = target_bandwidth / framerate, which govpx
// maps to rc.bitsPerFrame. Refund must be exact (not boosted, not
// per-layer), and the clamp must hit maximum_buffer_size.
func TestVP8DropFrameRefundsAvailableBandwidth(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		bitsPerFrame:      8000,
		bufferLevelBits:   -2000,
		bufferOptimalBits: 50000,
		maximumBufferBits: 100000,
		dropFrameAllowed:  true,
	}
	if !rc.shouldDropInterFrame() {
		t.Fatalf("shouldDropInterFrame = false, want true on negative buffer")
	}
	prevBuffer := rc.bufferLevelBits
	rc.postDropFrame()
	wantBuffer := prevBuffer + rc.bitsPerFrame // 6000
	if rc.bufferLevelBits != wantBuffer {
		t.Fatalf("post-drop buffer = %d, want %d (refund of bitsPerFrame=%d)",
			rc.bufferLevelBits, wantBuffer, rc.bitsPerFrame)
	}
}

// TestVP8DropFrameClampsToMaximumBufferSize pins the post-refund
// clamp libvpx applies at ratectrl.c line 880-882. If the refund would
// push bits_off_target past maximum_buffer_size, libvpx caps it.
func TestVP8DropFrameClampsToMaximumBufferSize(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		bitsPerFrame:      8000,
		bufferLevelBits:   95000, // already near max
		bufferOptimalBits: 50000,
		maximumBufferBits: 100000,
		dropFrameAllowed:  true,
	}
	// Underrun guard: shouldDropInterFrame requires bufferLevelBits<0,
	// so to verify the clamp branch in postDropFrame we drive it via
	// directly invoking postDropFrame (the clamp must be invariant
	// whether the caller arrived via shouldDropInterFrame or not).
	rc.bufferLevelBits = -100
	rc.postDropFrame()
	// Refund: -100 + 8000 = 7900. No clamp expected here.
	if rc.bufferLevelBits != 7900 {
		t.Fatalf("post-drop buffer = %d, want 7900", rc.bufferLevelBits)
	}
	// Now drive the clamp path: stage bufferLevelBits=99000, then
	// postDropFrame's saturatingAdd+clampBuffer caps to maximumBufferBits.
	rc.bufferLevelBits = 99000
	rc.postDropFrame()
	if rc.bufferLevelBits != rc.maximumBufferBits {
		t.Fatalf("post-drop clamp = %d, want maximumBufferBits=%d",
			rc.bufferLevelBits, rc.maximumBufferBits)
	}
}

// TestVP8DropFrameLeavesRateCorrectionFactorUntouched pins libvpx's
// implicit contract: when vp8_pick_frame_size returns 0, the encoder's
// encode_frame_to_data_rate early-returns BEFORE calling
// vp8_update_rate_correction_factors (which fires only after pack at
// onyx_if.c:4461). The dropped frame therefore does NOT pump
// rate_correction_factor.
func TestVP8DropFrameLeavesRateCorrectionFactorUntouched(t *testing.T) {
	rc := rateControlState{
		mode:                 RateControlCBR,
		bitsPerFrame:         8000,
		bufferLevelBits:      -1000,
		bufferOptimalBits:    50000,
		maximumBufferBits:    100000,
		dropFrameAllowed:     true,
		rateCorrectionFactor: 0.625,
	}
	if !rc.shouldDropInterFrame() {
		t.Fatalf("shouldDropInterFrame = false, want true")
	}
	rc.postDropFrame()
	if rc.rateCorrectionFactor != 0.625 {
		t.Fatalf("rateCorrectionFactor after drop = %g, want untouched 0.625",
			rc.rateCorrectionFactor)
	}
}

// TestVP8DropFrameLeavesTotalActualBitsUntouched pins libvpx's
// total_byte_count contract: the cumulative byte counter only advances
// in encode_frame_to_data_rate post-pack (onyx_if.c:4451). A
// vp8_pick_frame_size drop returns BEFORE pack, so total_byte_count is
// untouched. govpx mirrors this via rc.totalActualBits being mutated
// only in postEncodeFrameWithPacketContext.
func TestVP8DropFrameLeavesTotalActualBitsUntouched(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		bitsPerFrame:      8000,
		bufferLevelBits:   -1000,
		bufferOptimalBits: 50000,
		maximumBufferBits: 100000,
		dropFrameAllowed:  true,
		totalActualBits:   123456,
	}
	rc.postDropFrame()
	if rc.totalActualBits != 123456 {
		t.Fatalf("totalActualBits after drop = %d, want untouched 123456",
			rc.totalActualBits)
	}
}

// TestVP8DropFrameAdvancesFramesSinceKey pins libvpx's
// onyx_if.c:3572 post-pick-drop bump:
//
//	cpi->frames_since_key++;
//
// which fires in encode_frame_to_data_rate when vp8_pick_frame_size
// returns 0. govpx folds this into rc.postDropFrame to keep the
// bookkeeping co-located with the buffer refund.
func TestVP8DropFrameAdvancesFramesSinceKey(t *testing.T) {
	rc := rateControlState{
		mode:                RateControlCBR,
		bitsPerFrame:        8000,
		bufferLevelBits:     -1000,
		bufferOptimalBits:   50000,
		maximumBufferBits:   100000,
		dropFrameAllowed:    true,
		framesSinceKeyframe: 7,
	}
	rc.postDropFrame()
	if rc.framesSinceKeyframe != 8 {
		t.Fatalf("framesSinceKeyframe after drop = %d, want 8 (libvpx onyx_if.c:3572)",
			rc.framesSinceKeyframe)
	}
}

// TestVP8DropFrameRefusesWhenNotCBR pins libvpx's end_usage gate:
// the calc_pframe_target_size buffer-underrun drop fires ONLY when
// cpi->oxcf.end_usage == USAGE_STREAM_FROM_SERVER. In other usage
// modes (one-pass GOOD/BEST VBR or local-file playback) the drop
// branch is skipped even if buffer_level < 0.
func TestVP8DropFrameRefusesWhenNotCBR(t *testing.T) {
	for _, mode := range []RateControlMode{RateControlVBR, RateControlCQ, RateControlQ} {
		rc := rateControlState{
			mode:              mode,
			bitsPerFrame:      8000,
			bufferLevelBits:   -1000,
			bufferOptimalBits: 50000,
			maximumBufferBits: 100000,
			dropFrameAllowed:  true,
		}
		if rc.shouldDropInterFrame() {
			t.Fatalf("mode=%v: shouldDropInterFrame = true, want false (CBR-only)", mode)
		}
	}
}

// TestVP8DropFrameRefusesWhenDisabled pins libvpx's
// cpi->drop_frames_allowed gate: when allow_df == 0 (or buffered_mode
// == 0) the drop branch is unreachable even under tight-buffer CBR.
func TestVP8DropFrameRefusesWhenDisabled(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		bitsPerFrame:      8000,
		bufferLevelBits:   -1000,
		bufferOptimalBits: 50000,
		maximumBufferBits: 100000,
		dropFrameAllowed:  false,
	}
	if rc.shouldDropInterFrame() {
		t.Fatalf("shouldDropInterFrame = true with dropFrameAllowed=false, want false")
	}
}

// TestVP8TightBufferPFrameTargetUndershoot pins libvpx
// calc_pframe_target_size lines 695-727 verbatim for CBR
// (USAGE_STREAM_FROM_SERVER):
//
//	one_percent_bits = 1 + optimal_buffer_level / 100
//	percent_low      = (optimal_buffer_level - buffer_level) / one_percent_bits
//	clamp percent_low to [0, under_shoot_pct]
//	target -= target * percent_low / 200
//
// Drive it with a tight buffer at half optimal and verify the target
// is shrunk by exactly the libvpx formula's percent.
func TestVP8TightBufferPFrameTargetUndershoot(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		bitsPerFrame:      8000,
		bufferLevelBits:   25000, // half of optimal 50000
		bufferOptimalBits: 50000,
		maximumBufferBits: 100000,
		undershootPct:     100,
	}
	// one_percent_bits = 1 + 50000/100 = 501
	// percent_low = (50000-25000)/501 = 49 (truncated)
	// target -= 8000*49/200 = 1960 -> 6040
	got := rc.bufferAdjustedFrameTargetBits(8000)
	const want = 6040
	if got != want {
		t.Fatalf("tight-buffer target = %d, want %d", got, want)
	}
}

// TestVP8TightBufferPFrameTargetUndershootClampedByUnderShootPct
// pins the percent_low clamp at libvpx ratectrl.c line 719-723. When
// the raw percent_low exceeds under_shoot_pct, libvpx caps it; a
// smaller under_shoot_pct must yield a smaller shrink.
func TestVP8TightBufferPFrameTargetUndershootClampedByUnderShootPct(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		bitsPerFrame:      8000,
		bufferLevelBits:   1000, // very tight; raw percent_low ~ 97
		bufferOptimalBits: 50000,
		maximumBufferBits: 100000,
		undershootPct:     10, // clamps to 10
	}
	// percent_low raw = (50000-1000)/501 = 97 -> clamped to 10
	// target -= 8000*10/200 = 400 -> 7600
	got := rc.bufferAdjustedFrameTargetBits(8000)
	const want = 7600
	if got != want {
		t.Fatalf("under_shoot_pct-clamped target = %d, want %d", got, want)
	}
}

// TestVP8DropFrameSequenceMatchesLibvpxCBR plays a 5-frame
// tight-buffer CBR sequence where libvpx's calc_pframe_target_size
// drops every frame after the first inter (buffer recovers by
// av_per_frame_bandwidth per drop). Pin the buffer trajectory:
//
//	start: -5000
//	drop1: -5000 + 8000 = 3000  (refund saturated below max)
//	drop2: not triggered (buffer >= 0)
func TestVP8DropFrameSequenceMatchesLibvpxCBR(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		bitsPerFrame:      8000,
		bufferLevelBits:   -5000,
		bufferOptimalBits: 50000,
		maximumBufferBits: 100000,
		dropFrameAllowed:  true,
	}
	if !rc.shouldDropInterFrame() {
		t.Fatalf("first frame: shouldDropInterFrame = false, want true")
	}
	rc.postDropFrame()
	if rc.bufferLevelBits != 3000 {
		t.Fatalf("after first drop: buffer = %d, want 3000", rc.bufferLevelBits)
	}
	if rc.shouldDropInterFrame() {
		t.Fatalf("second frame: shouldDropInterFrame = true, want false (buffer recovered)")
	}
}
