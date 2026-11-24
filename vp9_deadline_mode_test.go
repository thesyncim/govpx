package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9ResolveDeadlineModes(t *testing.T) {
	cases := []struct {
		name     string
		deadline Deadline
		want     vp9DeadlineMode
	}{
		{"best", DeadlineBestQuality, vp9ModeBest},
		{"good", DeadlineGoodQuality, vp9ModeGood},
		{"realtime", DeadlineRealtime, vp9ModeRealtime},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := vp9ResolveDeadlineMode(tc.deadline); got != tc.want {
				t.Fatalf("vp9ResolveDeadlineMode(%d) = %d, want %d",
					tc.deadline, got, tc.want)
			}
		})
	}
}

func TestVP9BestDeadlineKeepsBestSpeedDefaults(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        64,
		Height:       64,
		Deadline:     DeadlineBestQuality,
		CpuUsed:      5,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.KeyFrame
	ctx.intraOnly = false
	ctx.showFrame = true

	var sf SpeedFeatures
	speed := e.vp9SpeedFeatureCPUUsed()
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, speed, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, speed, ctx)

	if sf.UseFastCoefUpdates != TwoLoop {
		t.Fatalf("UseFastCoefUpdates = %d, want TwoLoop", sf.UseFastCoefUpdates)
	}
	if sf.TxSizeSearchMethod != UseFullRD {
		t.Fatalf("TxSizeSearchMethod = %d, want UseFullRD", sf.TxSizeSearchMethod)
	}
	if sf.DisableSplitMask != 0 {
		t.Fatalf("DisableSplitMask = %#x, want 0", sf.DisableSplitMask)
	}
	if sf.PartitionSearchType != SearchPartition {
		t.Fatalf("PartitionSearchType = %d, want SearchPartition",
			sf.PartitionSearchType)
	}
	if !e.vp9KeyframeRDRefinementEnabled() {
		t.Fatalf("keyframe RD refinement disabled, want enabled for BEST")
	}
	if got := e.vp9EncoderFrameTxMode(true, false, false); got != common.TxModeSelect {
		t.Fatalf("keyframe tx mode = %d, want TxModeSelect", got)
	}
}

func TestVP9DeadlineModeTransitionForcesRealtimeKeyframe(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		Deadline:            DeadlineGoodQuality,
		CpuUsed:             8,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	first, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(64, 64, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("first EncodeIntoWithResult: %v", err)
	}
	if !first.KeyFrame {
		t.Fatalf("first frame KeyFrame = false, want true")
	}
	if e.vp9DeadlineModeChanged() {
		t.Fatalf("deadline mode changed after first-frame latch")
	}

	if err := e.SetDeadline(DeadlineRealtime); err != nil {
		t.Fatalf("SetDeadline(realtime): %v", err)
	}
	if !e.vp9DeadlineModeChanged() {
		t.Fatalf("deadline mode changed = false, want true after GOOD->RT")
	}
	if !e.vp9ShouldEncodeKeyFrame(0) {
		t.Fatalf("vp9ShouldEncodeKeyFrame = false, want true for deadline switch")
	}
	if e.sf.NonrdKeyframe != 1 {
		t.Fatalf("NonrdKeyframe = %d, want 1 (libvpx vp9_speed_features.c:862-866)",
			e.sf.NonrdKeyframe)
	}

	second, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(64, 64, 112, 128, 128), dst)
	if err != nil {
		t.Fatalf("second EncodeIntoWithResult: %v", err)
	}
	if second.Dropped {
		t.Fatalf("second frame was dropped, want coded keyframe")
	}
	if !second.KeyFrame {
		t.Fatalf("second KeyFrame = false, want true after deadline switch")
	}
	if e.vp9DeadlineModeChanged() {
		t.Fatalf("deadline mode changed after switch-frame latch")
	}
	if got, want := e.deadlineModePreviousFrame,
		vp9ResolveDeadlineMode(DeadlineRealtime); got != want {
		t.Fatalf("deadlineModePreviousFrame = %d, want %d", got, want)
	}
}
