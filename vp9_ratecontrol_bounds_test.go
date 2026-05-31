package govpx

import (
	"testing"
)

func TestVP9EncoderRateControlBoundsAreStored(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
		MinBitrateKbps:     500,
		MaxBitrateKbps:     1500,
		UndershootPct:      80,
		OvershootPct:       60,
		MaxIntraBitratePct: 200,
		GFCBRBoostPct:      30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.minBitrateKbps != 500 || e.rc.maxBitrateKbps != 1500 {
		t.Fatalf("bitrate bounds = (%d,%d), want (500,1500)",
			e.rc.minBitrateKbps, e.rc.maxBitrateKbps)
	}
	if e.rc.undershootPct != 80 || e.rc.overshootPct != 60 {
		t.Fatalf("under/overshoot = (%d,%d), want (80,60)",
			e.rc.undershootPct, e.rc.overshootPct)
	}
	if e.rc.maxIntraBitratePct != 200 || e.rc.gfCBRBoostPct != 30 {
		t.Fatalf("max-intra/gfboost = (%d,%d), want (200,30)",
			e.rc.maxIntraBitratePct, e.rc.gfCBRBoostPct)
	}
}

func TestVP9EncoderDefaultUndershootOvershootMatchLibvpx(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.undershootPct != defaultRateControlUndershootPct {
		t.Fatalf("default undershoot = %d, want %d",
			e.rc.undershootPct, defaultRateControlUndershootPct)
	}
	if e.rc.overshootPct != defaultRateControlOvershootPct {
		t.Fatalf("default overshoot = %d, want %d",
			e.rc.overshootPct, defaultRateControlOvershootPct)
	}
}

func TestVP9RateControlClampBitrateKbps(t *testing.T) {
	rc := &vp9RateControlState{
		minBitrateKbps: 500,
		maxBitrateKbps: 1500,
	}
	cases := []struct{ in, want int }{
		{100, 500},
		{500, 500},
		{1000, 1000},
		{1500, 1500},
		{2000, 1500},
	}
	for _, tc := range cases {
		if got := rc.clampBitrateKbps(tc.in); got != tc.want {
			t.Fatalf("clampBitrateKbps(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
	rc.minBitrateKbps = 0
	if got := rc.clampBitrateKbps(100); got != 100 {
		t.Fatalf("clampBitrateKbps disables min when zero, got %d", got)
	}
	rc.maxBitrateKbps = 0
	if got := rc.clampBitrateKbps(99999); got != 99999 {
		t.Fatalf("clampBitrateKbps disables max when zero, got %d", got)
	}
}

func TestVP9SetBitrateKbpsClampsToBounds(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
		MinBitrateKbps:     800,
		MaxBitrateKbps:     1200,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetBitrateKbps(200); err != nil {
		t.Fatalf("SetBitrateKbps 200: %v", err)
	}
	if e.rc.targetBitrateKbps != 800 {
		t.Fatalf("clamped target = %d, want 800 (min)", e.rc.targetBitrateKbps)
	}
	if err := e.SetBitrateKbps(5000); err != nil {
		t.Fatalf("SetBitrateKbps 5000: %v", err)
	}
	if e.rc.targetBitrateKbps != 1200 {
		t.Fatalf("clamped target = %d, want 1200 (max)", e.rc.targetBitrateKbps)
	}
	if err := e.SetBitrateKbps(1000); err != nil {
		t.Fatalf("SetBitrateKbps 1000: %v", err)
	}
	if e.rc.targetBitrateKbps != 1000 {
		t.Fatalf("unclamped target = %d, want 1000", e.rc.targetBitrateKbps)
	}
}

func TestVP9RateControlAppliesRawTargetRateClamp(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              32,
		Height:             32,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  10_000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.targetBitrateKbps != 10_000 || e.opts.TargetBitrateKbps != 10_000 {
		t.Fatalf("requested bitrate = opts:%d rc:%d, want 10000/10000",
			e.opts.TargetBitrateKbps, e.rc.targetBitrateKbps)
	}
	if e.rc.effectiveBitrateKbps != 737 ||
		e.rc.targetBandwidthBits != 737000 ||
		e.rc.bitsPerFrame != 24567 {
		t.Fatalf("effective rate = kbps:%d bandwidth:%d bpf:%d, want 737/737000/24567",
			e.rc.effectiveBitrateKbps, e.rc.targetBandwidthBits,
			e.rc.bitsPerFrame)
	}
	if e.rc.bufferSizeBits != 4_422_000 ||
		e.rc.bufferInitialBits != 2_948_000 ||
		e.rc.bufferOptimalBits != 3_685_000 ||
		e.rc.bufferLevelBits != 2_948_000 {
		t.Fatalf("buffer model = size:%d initial:%d optimal:%d level:%d, want effective-bitrate defaults",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits,
			e.rc.bufferOptimalBits, e.rc.bufferLevelBits)
	}

	if err := e.SetBitrateKbps(300); err != nil {
		t.Fatalf("SetBitrateKbps below raw cap: %v", err)
	}
	if e.rc.targetBitrateKbps != 300 || e.rc.effectiveBitrateKbps != 300 ||
		e.rc.bitsPerFrame != 10000 {
		t.Fatalf("below-cap rate = target:%d effective:%d bpf:%d, want 300/300/10000",
			e.rc.targetBitrateKbps, e.rc.effectiveBitrateKbps,
			e.rc.bitsPerFrame)
	}
}

func TestVP9BitsPerFrameRoundsLikeLibvpx(t *testing.T) {
	timing := timingState{timebaseNum: 1, timebaseDen: 60, frameDuration: 1}
	if got := computeVP9BitsPerFrame(100_000, timing); got != 1667 {
		t.Fatalf("computeVP9BitsPerFrame(100k@60) = %d, want 1667", got)
	}
	if got := computeVP9BitsPerFrame(900_000,
		timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1}); got != 30000 {
		t.Fatalf("computeVP9BitsPerFrame(900k@30) = %d, want 30000", got)
	}
	if got := computeVP9BitsPerFrame(900_000,
		timingState{timebaseNum: 1, timebaseDen: 300, frameDuration: 1}); got != 30000 {
		t.Fatalf("computeVP9BitsPerFrame(900k@300 fallback) = %d, want 30000", got)
	}
}

func TestVP9RateControlUsesLibvpxFrameRateFallbackAbove180Hz(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              32,
		Height:             32,
		FPS:                300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  10_000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.effectiveBitrateKbps != 737 || e.rc.bitsPerFrame != 24567 ||
		e.rc.frameRateNum != 30 || e.rc.frameRateDen != 1 {
		t.Fatalf("fallback rate state = effective:%d bpf:%d fps:%d/%d, want 737/24567/30/1",
			e.rc.effectiveBitrateKbps, e.rc.bitsPerFrame,
			e.rc.frameRateNum, e.rc.frameRateDen)
	}
}

func TestVP9OvershootCeilGrowsWithPct(t *testing.T) {
	cases := []struct {
		bpf, pct, want int
	}{
		{1000, 0, 0},
		{1000, 50, 1500},
		{1000, 100, 2000},
		{0, 50, 0},
	}
	for _, tc := range cases {
		if got := vp9OvershootCeil(tc.bpf, tc.pct); got != tc.want {
			t.Fatalf("vp9OvershootCeil(%d,%d) = %d, want %d",
				tc.bpf, tc.pct, got, tc.want)
		}
	}
}

func TestVP9ApplyVP9MaxIntraBoundCapsTarget(t *testing.T) {
	rc := &vp9RateControlState{
		bitsPerFrame:       1000,
		maxIntraBitratePct: 200,
	}
	if got := rc.applyVP9MaxIntraBound(5000); got != 2000 {
		t.Fatalf("max-intra bound = %d, want 2000", got)
	}
	if got := rc.applyVP9MaxIntraBound(1500); got != 1500 {
		t.Fatalf("under-cap target = %d, want 1500", got)
	}
	rc.maxIntraBitratePct = 0
	if got := rc.applyVP9MaxIntraBound(99999); got != 99999 {
		t.Fatalf("zero cap disabled, got %d", got)
	}
}

// TestVP9ClampIFrameTargetBitsAppliesMaxIntraBound pins the libvpx VP9
// vp9_rc_clamp_iframe_target_size invariant: MaxIntraBitratePct must cap
// the iframe target BEFORE the max_frame_bandwidth ceiling. Before the
// fix the one-pass VBR keyframe path (which only routes through
// clampIFrameTargetBits, not the keyFrameTargetBits post-clamp
// applyVP9MaxIntraBound) silently ignored MaxIntraBitratePct.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:245-255.
func TestVP9ClampIFrameTargetBitsAppliesMaxIntraBound(t *testing.T) {
	rc := &vp9RateControlState{
		bitsPerFrame:       1000,
		maxIntraBitratePct: 200,
		maxFrameBandwidth:  10000,
	}
	if got := rc.clampIFrameTargetBits(5000); got != 2000 {
		t.Fatalf("clampIFrameTargetBits with max-intra=200%% = %d, want 2000",
			got)
	}
	// max_frame_bandwidth still wins over an unbounded max-intra cap.
	rc.maxIntraBitratePct = 0
	if got := rc.clampIFrameTargetBits(50000); got != 10000 {
		t.Fatalf("clampIFrameTargetBits without max-intra = %d, want 10000",
			got)
	}
}

// TestVP9OnePassCBRKeyFrameTargetBitsMatchesLibvpx pins the libvpx VP9
// vp9_calc_iframe_target_size_one_pass_cbr formula for the kf_boost ramp.
// Prior to this fix govpx's CBR keyframe target was hard-coded to the
// per-frame bandwidth, producing a slightly higher base qindex than libvpx
// on small frames, matching the FuzzVP9OracleEncoderOptions parity seed.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:2205-2232.
func TestVP9OnePassCBRKeyFrameTargetBitsMatchesLibvpx(t *testing.T) {
	rc := &vp9RateControlState{
		mode:              RateControlCBR,
		bitsPerFrame:      20000,
		bufferInitialBits: 280000,
		frameRateNum:      30,
		frameRateDen:      1,
		maxFrameBandwidth: 10_000_000,
		framesSinceKey:    8,
	}
	// First video frame: target = starting_buffer_level / 2.
	if got := rc.onePassCBRKeyFrameTargetBits(0); got != 140000 {
		t.Fatalf("frame 0 target = %d, want 140000 (buffer_initial/2)", got)
	}
	// At fps=30: kf_boost = max(32, round(2*30-16)) = max(32, 44) = 44.
	// Since framesSinceKey(8) >= framerate/2(15)? Actually 8 < 15 so the
	// ramp applies: kf_boost' = round(44 * 8 / 15) = round(23.46) = 23.
	// target = ((16 + 23) * 20000) >> 4 = 780000 >> 4 = 48750.
	if got := rc.onePassCBRKeyFrameTargetBits(1); got != 48750 {
		t.Fatalf("frame 1 target = %d, want 48750 (kf_boost ramp)", got)
	}
	// After enough frames-since-key, the ramp saturates at kf_boost = 44.
	// target = ((16 + 44) * 20000) >> 4 = 1200000 >> 4 = 75000.
	rc.framesSinceKey = 30
	if got := rc.onePassCBRKeyFrameTargetBits(1); got != 75000 {
		t.Fatalf("frame 1 target (saturated kf_boost) = %d, want 75000", got)
	}
}

func TestVP9OnePassCBRInterFrameTargetBitsMatchesLibvpx(t *testing.T) {
	rc := &vp9RateControlState{
		mode:               RateControlCBR,
		bitsPerFrame:       1000,
		bufferOptimalBits:  10_000,
		bufferLevelBits:    8_700,
		undershootPct:      100,
		overshootPct:       100,
		baselineGFInterval: 10,
	}
	// diff=1300, one_pct_bits=101, pct_low=12:
	// target = 1000 - 1000*12/200 = 940.
	if got := rc.onePassCBRInterFrameTargetBits(0); got != 940 {
		t.Fatalf("buffer-adjusted target = %d, want 940", got)
	}
	rc.bufferLevelBits = 12_500
	// diff=-2500, pct_high=24: target = 1000 + 1000*24/200 = 1120.
	if got := rc.onePassCBRInterFrameTargetBits(0); got != 1120 {
		t.Fatalf("overshoot-adjusted target = %d, want 1120", got)
	}
	rc.bufferLevelBits = 10_000
	rc.gfCBRBoostPct = 40
	// gf_cbr_boost_pct redistributes a 10-frame group:
	// non-refresh: 1000*10*100 / (10*100+140-100) = 961
	if got := rc.onePassCBRInterFrameTargetBits(0); got != 961 {
		t.Fatalf("non-golden GF CBR target = %d, want 961", got)
	}
	// golden refresh: 1000*10*140 / 1040 = 1346.
	if got := rc.onePassCBRInterFrameTargetBits(1 << vp9GoldenRefSlot); got != 1346 {
		t.Fatalf("golden GF CBR target = %d, want 1346", got)
	}
}
