package govpx

import "testing"

func TestVP9RtSpeedFeaturesDisablePartitionCopyAfterDrop(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              640,
		Height:             360,
		Deadline:           DeadlineRealtime,
		CpuUsed:            7,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  300,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	ctx := func() vp9SpeedFrameContext {
		return e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
			ShowFrame:  true,
			BaseQIndex: 100,
		})
	}

	e.lastFrameDropped = false
	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx())
	if sf.CopyPartitionFlag != 1 {
		t.Fatalf("CopyPartitionFlag without prior drop = %d, want 1 (libvpx vp9_speed_features.c:722-728)",
			sf.CopyPartitionFlag)
	}
	if e.maxCopiedFrame != 2 {
		t.Fatalf("maxCopiedFrame without prior drop = %d, want 2 (libvpx vp9_speed_features.c:728)",
			e.maxCopiedFrame)
	}

	e.lastFrameDropped = true
	sf = SpeedFeatures{}
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx())
	if sf.CopyPartitionFlag != 0 {
		t.Fatalf("CopyPartitionFlag after prior drop = %d, want 0 (libvpx vp9_speed_features.c:722)",
			sf.CopyPartitionFlag)
	}
	if e.maxCopiedFrame != 0 {
		t.Fatalf("maxCopiedFrame after prior drop = %d, want 0", e.maxCopiedFrame)
	}
}
