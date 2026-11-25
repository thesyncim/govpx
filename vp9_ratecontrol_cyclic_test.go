package govpx

import (
	"errors"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

func TestVP9EncoderMaxInterBitratePctStored(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		MaxInterBitratePct: 200,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.maxInterBitratePct != 200 {
		t.Fatalf("rc.maxInterBitratePct = %d, want 200", e.rc.maxInterBitratePct)
	}
}

func TestVP9SetMaxInterBitratePctAppliesValue(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxInterBitratePct(150); err != nil {
		t.Fatalf("SetMaxInterBitratePct(150): %v", err)
	}
	if e.opts.MaxInterBitratePct != 150 || e.rc.maxInterBitratePct != 150 {
		t.Fatalf("opts=%d rc=%d, want both 150",
			e.opts.MaxInterBitratePct, e.rc.maxInterBitratePct)
	}
}

func TestVP9SetMaxIntraBitratePctMutation(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxIntraBitratePct(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetMaxIntraBitratePct(-1) err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.MaxIntraBitratePct != 0 || e.rc.maxIntraBitratePct != 0 {
		t.Fatalf("invalid max-intra mutation opts=%d rc=%d, want both 0",
			e.opts.MaxIntraBitratePct, e.rc.maxIntraBitratePct)
	}
	if err := e.SetMaxIntraBitratePct(175); err != nil {
		t.Fatalf("SetMaxIntraBitratePct(175): %v", err)
	}
	if e.opts.MaxIntraBitratePct != 175 || e.rc.maxIntraBitratePct != 175 {
		t.Fatalf("opts=%d rc=%d, want both 175",
			e.opts.MaxIntraBitratePct, e.rc.maxIntraBitratePct)
	}
}

func TestVP9SetGFCBRBoostPctMutation(t *testing.T) {
	noRC, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder(no RC): %v", err)
	}
	if err := noRC.SetGFCBRBoostPct(25); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetGFCBRBoostPct without CBR err = %v, want ErrInvalidConfig", err)
	}
	if noRC.opts.GFCBRBoostPct != 0 || noRC.rc.gfCBRBoostPct != 0 {
		t.Fatalf("invalid gf-boost mutation opts=%d rc=%d, want both 0",
			noRC.opts.GFCBRBoostPct, noRC.rc.gfCBRBoostPct)
	}

	cbr, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(CBR): %v", err)
	}
	if err := cbr.SetGFCBRBoostPct(40); err != nil {
		t.Fatalf("SetGFCBRBoostPct(40): %v", err)
	}
	if cbr.opts.GFCBRBoostPct != 40 || cbr.rc.gfCBRBoostPct != 40 {
		t.Fatalf("opts=%d rc=%d, want both 40",
			cbr.opts.GFCBRBoostPct, cbr.rc.gfCBRBoostPct)
	}
	if err := cbr.SetGFCBRBoostPct(0); err != nil {
		t.Fatalf("SetGFCBRBoostPct(0): %v", err)
	}
	if cbr.opts.GFCBRBoostPct != 0 || cbr.rc.gfCBRBoostPct != 0 {
		t.Fatalf("cleared opts=%d rc=%d, want both 0",
			cbr.opts.GFCBRBoostPct, cbr.rc.gfCBRBoostPct)
	}
}

func TestVP9MaxInterBoundCapsInterTarget(t *testing.T) {
	rc := &vp9RateControlState{
		bitsPerFrame:       1000,
		maxInterBitratePct: 200,
	}
	if got := rc.applyVP9MaxInterBound(5000); got != 2000 {
		t.Fatalf("max-inter bound = %d, want 2000", got)
	}
	if got := rc.applyVP9MaxInterBound(1500); got != 1500 {
		t.Fatalf("under-cap target = %d, want 1500", got)
	}
	rc.maxInterBitratePct = 0
	if got := rc.applyVP9MaxInterBound(99999); got != 99999 {
		t.Fatalf("zero cap disabled, got %d", got)
	}
}

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
