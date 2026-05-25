package govpx

import "testing"

func TestSetTwoPassStatsBeforeFrameZeroSwitchesStartupMode(t *testing.T) {
	sources := []Image{
		encoderValidationPanningFrame(32, 32, 0),
		encoderValidationPanningFrame(32, 32, 1),
		encoderValidationPanningFrame(32, 32, 2),
	}
	opts := EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		KeyFrameInterval:  999,
	}
	stats := collectRuntimeControlFirstPassStats(t, opts, sources)

	onePass, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder one-pass returned error: %v", err)
	}
	defer onePass.Close()
	if !onePass.rc.onePassAutoGold || onePass.rc.framesTillGFUpdateDue != libvpxDefaultGFInterval {
		t.Fatalf("one-pass seed = auto_gold:%t due:%d, want true/%d",
			onePass.rc.onePassAutoGold, onePass.rc.framesTillGFUpdateDue, libvpxDefaultGFInterval)
	}
	if err := onePass.SetTwoPassStats(stats); err != nil {
		t.Fatalf("SetTwoPassStats(enable) returned error: %v", err)
	}
	if onePass.rc.onePassAutoGold || onePass.rc.framesTillGFUpdateDue != 0 {
		t.Fatalf("two-pass seed = auto_gold:%t due:%d, want false/0",
			onePass.rc.onePassAutoGold, onePass.rc.framesTillGFUpdateDue)
	}

	twoPassOpts := opts
	twoPassOpts.TwoPassStats = stats
	twoPass, err := NewVP8Encoder(twoPassOpts)
	if err != nil {
		t.Fatalf("NewVP8Encoder two-pass returned error: %v", err)
	}
	defer twoPass.Close()
	if err := twoPass.SetTwoPassStats(nil); err != nil {
		t.Fatalf("SetTwoPassStats(disable) returned error: %v", err)
	}
	if !twoPass.rc.onePassAutoGold || twoPass.rc.framesTillGFUpdateDue != libvpxDefaultGFInterval {
		t.Fatalf("disabled two-pass seed = auto_gold:%t due:%d, want true/%d",
			twoPass.rc.onePassAutoGold, twoPass.rc.framesTillGFUpdateDue, libvpxDefaultGFInterval)
	}
}
