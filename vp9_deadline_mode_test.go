package govpx

import "testing"

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
		newVP9YCbCrForTest(64, 64, 96, 128, 128), dst)
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
		newVP9YCbCrForTest(64, 64, 112, 128, 128), dst)
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
