package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9CyclicRefreshResizePendingLatch pins that SetRealtimeTarget
// resolution changes survive the forced keyframe and apply ResetResize +
// forced golden refresh on the first inter frame at the new size.
func TestVP9CyclicRefreshResizePendingLatch(t *testing.T) {
	const (
		w0 = 64
		h0 = 64
		w1 = 128
		h1 = 64
	)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              w0,
		Height:             h0,
		FPS:                30,
		TargetBitrateKbps:  300,
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
	src := vp9test.NewYCbCr(w0, h0, 96, 128, 128)
	if _, err := enc.EncodeInto(src, dst); err != nil {
		t.Fatalf("initial key: %v", err)
	}
	srcInter := vp9test.NewYCbCr(w0, h0, 110, 128, 128)
	if _, err := enc.EncodeInto(srcInter, dst); err != nil {
		t.Fatalf("initial inter: %v", err)
	}
	if err := enc.SetRealtimeTarget(RealtimeTarget{Width: w1, Height: h1}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if !enc.cyclicResizePending {
		t.Fatal("cyclicResizePending false after resize, want true until first inter")
	}
	srcKey := vp9test.NewYCbCr(w1, h1, 96, 128, 128)
	if _, err := enc.EncodeInto(srcKey, dst); err != nil {
		t.Fatalf("post-resize key: %v", err)
	}
	if !enc.cyclicResizePending {
		t.Fatal("cyclicResizePending cleared on key, want latched through to first inter")
	}
	if enc.cyclicResizeFramePending {
		t.Fatal("cyclicResizeFramePending set on key, want only on first inter")
	}
	const sentinel uint8 = 200
	for i := range enc.cyclicAQ.ConsecZeroMV {
		enc.cyclicAQ.ConsecZeroMV[i] = sentinel
	}
	srcInterNew := vp9test.NewYCbCr(w1, h1, 112, 128, 128)
	if _, err := enc.EncodeInto(srcInterNew, dst); err != nil {
		t.Fatalf("post-resize inter: %v", err)
	}
	if enc.cyclicResizePending || enc.cyclicResizeFramePending {
		t.Fatal("resize pending flags still set after first inter at new size")
	}
	sentinelLeft := 0
	for _, v := range enc.cyclicAQ.ConsecZeroMV {
		if v == sentinel {
			sentinelLeft++
		}
	}
	if sentinelLeft != 0 {
		t.Fatalf("consec_zero_mv still has %d sentinel values after resize setup",
			sentinelLeft)
	}
	if enc.rc.framesTillGFUpdateDue <= 0 {
		t.Fatalf("framesTillGFUpdateDue = %d after resize inter, want golden cadence from postencode", enc.rc.framesTillGFUpdateDue)
	}
	if !enc.refValid[vp9GoldenRefSlot] {
		t.Fatal("golden reference invalid after resize inter with forced refresh")
	}
}
