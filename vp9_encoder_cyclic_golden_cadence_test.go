package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9CyclicRefreshFirstInterRefreshesLastOnly pins libvpx
// vp9_ratectrl.c:2518-2529 + update_golden_frame_stats: the keyframe seeds
// frames_till_gf_update_due and the first inter frame must not schedule an
// extra golden refresh.
func TestVP9CyclicRefreshFirstInterRefreshesLastOnly(t *testing.T) {
	const width, height = 64, 64
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  700,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		Deadline:           DeadlineRealtime,
		CpuUsed:            -8,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = enc.Close() })
	dst := make([]byte, 1<<20)
	if _, err := enc.EncodeInto(vp9test.NewPanningYCbCr(width, height, 0), dst); err != nil {
		t.Fatalf("key: %v", err)
	}
	if enc.rc.framesTillGFUpdateDue <= 0 {
		t.Fatalf("framesTillGFUpdateDue = %d after key, want seeded countdown", enc.rc.framesTillGFUpdateDue)
	}
	if enc.rc.refreshGoldenFrame {
		t.Fatal("refreshGoldenFrame still set after key encode")
	}
	if _, err := enc.EncodeInto(vp9test.NewPanningYCbCr(width, height, 1), dst); err != nil {
		t.Fatalf("first inter: %v", err)
	}
	want := uint8(1 << vp9LastRefSlot)
	if got := enc.vp9HeaderScratch.RefreshFrameFlags; got != want {
		t.Fatalf("first inter RefreshFrameFlags = 0x%x, want 0x%x (LAST only)", got, want)
	}
	if enc.rc.refreshGoldenFrame {
		t.Fatal("refreshGoldenFrame set on first inter, want false")
	}
}
