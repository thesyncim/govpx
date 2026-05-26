package govpx

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9CBRCyclicGoldenFrameSeedsIntervalOnKeyframeWithoutRefreshBit(t *testing.T) {
	rc := &vp9RateControlState{
		enabled: true,
		mode:    RateControlCBR,
	}
	cr := &vp9enc.CyclicRefreshState{
		Enabled:         true,
		PercentRefresh:  10,
		ContentMode:     true,
		MaxQDeltaPerc:   60,
		RateRatioQDelta: 2.0,
	}
	rc.prepareOnePassCBRCyclicGoldenFrame(true, false, VP9AQCyclicRefresh, cr, 0, false)
	if rc.refreshGoldenFrame {
		t.Fatal("refreshGoldenFrame set on key, want interval seed only")
	}
	if rc.framesTillGFUpdateDue != 40 {
		t.Fatalf("framesTillGFUpdateDue = %d, want 40 on key", rc.framesTillGFUpdateDue)
	}
}

func TestVP9CBRCyclicGoldenFrameSchedulesRefreshAtInterval(t *testing.T) {
	rc := &vp9RateControlState{
		enabled: true,
		mode:    RateControlCBR,
	}
	cr := &vp9enc.CyclicRefreshState{
		Enabled:         true,
		PercentRefresh:  10,
		ContentMode:     true,
		MaxQDeltaPerc:   60,
		RateRatioQDelta: 2.0,
	}
	rc.prepareOnePassCBRCyclicGoldenFrame(false, false, VP9AQCyclicRefresh, cr, 0, false)
	if !rc.refreshGoldenFrame {
		t.Fatal("expected golden refresh when frames_till_gf_update_due == 0")
	}
	if rc.baselineGFInterval != 40 {
		t.Fatalf("baselineGFInterval = %d, want 40 for percent_refresh=10", rc.baselineGFInterval)
	}
	if rc.framesTillGFUpdateDue != 40 {
		t.Fatalf("framesTillGFUpdateDue = %d, want 40", rc.framesTillGFUpdateDue)
	}
	rc.refreshGoldenFrame = false
	rc.framesTillGFUpdateDue = 5
	rc.prepareOnePassCBRCyclicGoldenFrame(false, false, VP9AQCyclicRefresh, cr, 0, false)
	if rc.refreshGoldenFrame {
		t.Fatal("expected no golden refresh while countdown active")
	}
}

func TestVP9CBRCyclicGoldenCadenceDecrementsOnKeyframeRefresh(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:               true,
		mode:                  RateControlCBR,
		framesTillGFUpdateDue: 40,
	}
	rc.postOnePassCBRGoldenCadence(0xff)
	if rc.framesTillGFUpdateDue != 39 {
		t.Fatalf("framesTillGFUpdateDue = %d, want 39 after key golden refresh", rc.framesTillGFUpdateDue)
	}
}

func TestVP9CBRCyclicGoldenCadenceDecrementsCountdown(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:               true,
		mode:                  RateControlCBR,
		framesTillGFUpdateDue: 3,
	}
	rc.postOnePassCBRGoldenCadence(1 << vp9LastRefSlot)
	if rc.framesTillGFUpdateDue != 2 {
		t.Fatalf("framesTillGFUpdateDue = %d, want 2", rc.framesTillGFUpdateDue)
	}
}

func TestVP9ComputeFrameLowMotionMatchesLibvpxEMA(t *testing.T) {
	rc := &vp9RateControlState{enabled: true, avgFrameLowMotion: 80}
	mi := func(miRow, miCol int) *vp9dec.NeighborMi {
		_ = miRow
		_ = miCol
		return &vp9dec.NeighborMi{
			RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
			Mv:       [2]vp9dec.MV{},
		}
	}
	rc.computeFrameLowMotion(2, 2, mi)
	if rc.avgFrameLowMotion != 85 {
		t.Fatalf("avgFrameLowMotion = %d, want 85 ((3*80+100)/4)", rc.avgFrameLowMotion)
	}
}

func TestVP9ApplyCyclicRefreshPostencodeResizeSchedulesGolden(t *testing.T) {
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
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
	hdr := &vp9dec.UncompressedHeader{}
	enc.applyCyclicRefreshPostencodeResult(hdr, vp9enc.CyclicRefreshPostencodeResult{
		SetGoldenUpdate:    true,
		ForceGoldenRefresh: true,
	})
	if hdr.RefreshFrameFlags&(1<<vp9GoldenRefSlot) == 0 {
		t.Fatal("expected golden refresh flag after forced resize postencode")
	}
	if enc.rc.framesTillGFUpdateDue <= 0 {
		t.Fatalf("framesTillGFUpdateDue = %d, want > 0 after SetGoldenUpdate", enc.rc.framesTillGFUpdateDue)
	}
}

func TestVP9CyclicRefreshPostencodeClearsGoldenOnLowContent(t *testing.T) {
	cr := &vp9enc.CyclicRefreshState{}
	cr.Configure(true, 64, 64)
	cr.Alloc(8, 8)
	cr.ContentMode = true
	cr.Apply = true
	n := 8 * 8
	isInter := make([]uint8, n)
	mvRow := make([]int16, n)
	mvCol := make([]int16, n)
	for i := range isInter {
		isInter[i] = 1
	}
	res := cr.Postencode(vp9enc.CyclicRefreshPostencodeArgs{
		RefreshGoldenFrame: true,
		FramesSinceKey:     10,
		FramesSinceGolden:  2,
		IsInterBlock:       isInter,
		MvRow:              mvRow,
		MvCol:              mvCol,
	})
	if !res.ClearRefreshGolden {
		t.Fatal("expected ClearRefreshGolden on all-zero-MV low-content frame")
	}
}
