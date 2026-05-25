package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestEncodeIntoGFCBRBoostRefreshesGoldenOnInterval(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	refreshFrame := e.rc.framesTillGFUpdateDue + 1
	cbrInterval := e.goldenFrameCBRInterval(rows, cols)
	const lastBoostSentinel = 149
	e.rc.lastBoost = lastBoostSentinel
	for frame := 1; frame <= refreshFrame; frame++ {
		wantRC := e.rc
		if frame == refreshFrame {
			wantRC.framesTillGFUpdateDue = cbrInterval
			wantRC.currentGFInterval = cbrInterval
		}
		wantRC.beginFrame(false)
		wantTarget := wantRC.frameTargetBits
		if frame == refreshFrame {
			wantTarget = boostedFrameTargetBits(wantTarget, e.rc.gfCBRBoostPct)
		}
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if frame < refreshFrame {
			if state.Refresh.RefreshGolden {
				t.Fatalf("inter %d refresh golden = true, want false before interval", frame)
			}
			if inter.FrameTargetBits != wantTarget {
				t.Fatalf("inter %d target = %d, want libvpx CBR buffer target %d", frame, inter.FrameTargetBits, wantTarget)
			}
			continue
		}
		if !state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = false, want true at GF CBR interval", frame)
		}
		if inter.FrameTargetBits != wantTarget {
			t.Fatalf("inter %d target = %d, want boosted libvpx CBR target %d", frame, inter.FrameTargetBits, wantTarget)
		}
		if e.rc.lastBoost != lastBoostSentinel {
			t.Fatalf("inter %d lastBoost = %d, want fixed-CBR GF refresh to preserve %d", frame, e.rc.lastBoost, lastBoostSentinel)
		}
	}
}

func TestEncodeIntoDefaultCBRRefreshesGoldenOnLibvpxInterval(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	refreshFrame := e.rc.framesTillGFUpdateDue + 1
	cbrInterval := e.goldenFrameCBRInterval(rows, cols)
	for frame := 1; frame <= refreshFrame; frame++ {
		wantRC := e.rc
		if frame == refreshFrame {
			wantRC.framesTillGFUpdateDue = cbrInterval
			wantRC.currentGFInterval = cbrInterval
		}
		wantRC.beginFrame(false)
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if frame < refreshFrame && state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = true, want false before interval", frame)
		}
		if frame < refreshFrame && state.Refresh.CopyBufferToAltRef != 0 {
			t.Fatalf("inter %d copy-to-alt = %d, want none before GF refresh", frame, state.Refresh.CopyBufferToAltRef)
		}
		if frame == refreshFrame && !state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = false, want default libvpx CBR GF refresh", frame)
		}
		if frame == refreshFrame && state.Refresh.CopyBufferToAltRef != 2 {
			t.Fatalf("inter %d copy-to-alt = %d, want libvpx old-GF-to-ARF copy", frame, state.Refresh.CopyBufferToAltRef)
		}
		if inter.FrameTargetBits != wantRC.frameTargetBits {
			t.Fatalf("inter %d target = %d, want unboosted libvpx CBR target %d", frame, inter.FrameTargetBits, wantRC.frameTargetBits)
		}
	}
}

func TestEncodeIntoOnePassVBRDoesNotRefreshGoldenImmediatelyAfterKey(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 64*64*3)
	if _, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 0), 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	state := packetState(t, inter.Data)
	if state.Refresh.RefreshGolden {
		t.Fatalf("frame-1 VBR refresh_golden = true, want false until libvpx DEFAULT_GF_INTERVAL countdown expires")
	}
	if e.rc.framesTillGFUpdateDue != libvpxDefaultGFInterval-2 {
		t.Fatalf("framesTillGFUpdateDue = %d, want %d after key decrement plus one inter frame",
			e.rc.framesTillGFUpdateDue, libvpxDefaultGFInterval-2)
	}
}

func TestLibvpxMaxGFIntervalMatchesFramerateAndKeyDistanceCaps(t *testing.T) {
	cases := []struct {
		name string
		opts EncoderOptions
		want int
	}{
		{
			name: "30fps",
			opts: EncoderOptions{Width: 64, Height: 64, FPS: 30, RateControlMode: RateControlVBR, TargetBitrateKbps: 700, KeyFrameInterval: 999},
			want: 17,
		},
		{
			name: "15fps-floor",
			opts: EncoderOptions{Width: 64, Height: 64, FPS: 15, RateControlMode: RateControlVBR, TargetBitrateKbps: 700, KeyFrameInterval: 999},
			want: 12,
		},
		{
			name: "key-distance-cap",
			opts: EncoderOptions{Width: 64, Height: 64, FPS: 30, RateControlMode: RateControlVBR, TargetBitrateKbps: 700, KeyFrameInterval: 8},
			want: 4,
		},
		{
			// One-pass auto-ARF: libvpx forces oxcf->lag_in_frames=0 in
			// vp8/vp8_cx_iface.c set_vp8e_config when g_pass == VPX_RC_ONE_PASS,
			// so vp8_new_framerate's `play_alternate && lag_in_frames` cap is
			// never entered. max_gf_interval stays at the framerate cap (17).
			name: "altref-onepass-no-lag-cap",
			opts: EncoderOptions{Width: 64, Height: 64, FPS: 30, RateControlMode: RateControlVBR, TargetBitrateKbps: 700, KeyFrameInterval: 999, LookaheadFrames: 4, AutoAltRef: true},
			want: 17,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP8Encoder(tc.opts)
			if err != nil {
				t.Fatalf("NewVP8Encoder returned error: %v", err)
			}
			if got := e.libvpxMaxGFInterval(); got != tc.want {
				t.Fatalf("libvpxMaxGFInterval = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCalcGFParamsHalvesRealtimeBoostWhenRecodeLoopIsDisabled(t *testing.T) {
	base := gfParamsInput{
		Q:                     127,
		RecentRefIntra:        1,
		RecentRefLast:         1,
		RecentRefGolden:       98,
		RecentRefAltRef:       0,
		GFActiveCount:         0,
		Macroblocks:           100,
		ThisFramePercentIntra: 0,
		BaselineGFInterval:    libvpxDefaultGFInterval,
		MaxGFInterval:         17,
	}
	normal := calcGFParams(base)
	realtime := calcGFParams(gfParamsInput{
		Q:                     base.Q,
		RecentRefIntra:        base.RecentRefIntra,
		RecentRefLast:         base.RecentRefLast,
		RecentRefGolden:       base.RecentRefGolden,
		RecentRefAltRef:       base.RecentRefAltRef,
		GFActiveCount:         base.GFActiveCount,
		Macroblocks:           base.Macroblocks,
		ThisFramePercentIntra: base.ThisFramePercentIntra,
		BaselineGFInterval:    base.BaselineGFInterval,
		MaxGFInterval:         base.MaxGFInterval,
		RealtimeNoRecode:      true,
	})
	if normal.Boost <= realtime.Boost {
		t.Fatalf("normal boost = %d, realtime no-recode boost = %d, want realtime lower", normal.Boost, realtime.Boost)
	}
	if normal.FramesTillUpdate != 11 {
		t.Fatalf("normal interval = %d, want high-GF-usage table interval 11", normal.FramesTillUpdate)
	}
	if realtime.FramesTillUpdate != 11 {
		t.Fatalf("realtime no-recode interval = %d, want high-GF-usage table interval 11", realtime.FramesTillUpdate)
	}
}

func TestGFActiveMapTracksLibvpxUsageRules(t *testing.T) {
	e := &VP8Encoder{gfActiveMap: make([]bool, 4)}
	e.resetGFActiveMap(4)
	if e.rc.gfActiveCount != 4 {
		t.Fatalf("initial gfActiveCount = %d, want 4", e.rc.gfActiveCount)
	}

	e.updateGFActiveMap(false, []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred},
	})
	want := []bool{true, false, true, false}
	for i, wantActive := range want {
		if e.gfActiveMap[i] != wantActive {
			t.Fatalf("gfActiveMap[%d] = %t, want %t", i, e.gfActiveMap[i], wantActive)
		}
	}
	if e.rc.gfActiveCount != 2 {
		t.Fatalf("gfActiveCount after mixed usage = %d, want 2", e.rc.gfActiveCount)
	}

	e.updateGFActiveMap(false, []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.AltRefFrame, Mode: vp8common.NewMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
	})
	want = []bool{true, true, false, false}
	for i, wantActive := range want {
		if e.gfActiveMap[i] != wantActive {
			t.Fatalf("second gfActiveMap[%d] = %t, want %t", i, e.gfActiveMap[i], wantActive)
		}
	}
	if e.rc.gfActiveCount != 2 {
		t.Fatalf("gfActiveCount after reactivation = %d, want 2", e.rc.gfActiveCount)
	}

	e.updateGFActiveMap(true, make([]vp8enc.InterFrameMacroblockMode, 3))
	if e.rc.gfActiveCount != 3 {
		t.Fatalf("gfActiveCount after refresh reset = %d, want 3", e.rc.gfActiveCount)
	}
	for i := range 3 {
		if !e.gfActiveMap[i] {
			t.Fatalf("gfActiveMap[%d] after refresh = false, want true", i)
		}
	}
	if e.gfActiveMap[3] {
		t.Fatalf("gfActiveMap[3] after 3-MB refresh = true, want false")
	}
}

func TestEncodeIntoOnePassVBRRefreshesGoldenOnLibvpxIntervals(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 64*64*3)
	for frame := range 15 {
		result, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, frame), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", frame, err)
		}
		if frame == 0 {
			continue
		}
		state := packetState(t, result.Data)
		switch frame {
		case libvpxDefaultGFInterval, 2 * libvpxDefaultGFInterval:
			if !state.Refresh.RefreshGolden {
				t.Fatalf("frame %d VBR refresh_golden = false, want true on libvpx GF interval", frame)
			}
			if state.Refresh.CopyBufferToAltRef != 2 {
				t.Fatalf("frame %d copy-to-alt = %d, want old-GF-to-ARF copy", frame, state.Refresh.CopyBufferToAltRef)
			}
		default:
			if state.Refresh.RefreshGolden {
				t.Fatalf("frame %d VBR refresh_golden = true, want false between default GF intervals", frame)
			}
		}
	}
}

func TestOnePassAutoGoldenPreservesStartupModeAcrossRuntimeReconfigure(t *testing.T) {
	newEncoder := func(t *testing.T, mode RateControlMode) *VP8Encoder {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:             32,
			Height:            32,
			FPS:               30,
			RateControlMode:   mode,
			TargetBitrateKbps: 700,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			CQLevel:           20,
			Deadline:          DeadlineRealtime,
			CpuUsed:           0,
			KeyFrameInterval:  999,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
		}
		return e
	}
	forceOpportunity := func(e *VP8Encoder) (int, int) {
		rows := geometry.MacroblockRows(e.opts.Height)
		cols := geometry.MacroblockCols(e.opts.Width)
		e.rc.framesTillGFUpdateDue = 0
		e.rc.thisFramePercentIntra = 10
		e.rc.recentRefFrameUsageIntra = rows * cols / 4
		e.rc.recentRefFrameUsageLast = rows * cols
		e.rc.gfActiveCount = rows * cols
		return rows, cols
	}

	cbrStart := newEncoder(t, RateControlCBR)
	if err := cbrStart.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(Q) from CBR returned error: %v", err)
	}
	rows, cols := forceOpportunity(cbrStart)
	if cbrStart.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("CBR-start runtime Q auto-golden refresh = true, want false")
	}

	cqStart := newEncoder(t, RateControlCQ)
	if err := cqStart.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(Q) from CQ returned error: %v", err)
	}
	rows, cols = forceOpportunity(cqStart)
	if !cqStart.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("CQ-start runtime Q auto-golden refresh = false, want true")
	}
	if err := cqStart.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(CBR) from CQ returned error: %v", err)
	}
	rows, cols = forceOpportunity(cqStart)
	if !cqStart.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("CQ-start runtime CBR auto-golden refresh = false, want true")
	}
	if got := cqStart.libvpxKeyFrameSetupGFInterval(rows, cols); got != 1 {
		t.Fatalf("CQ-start runtime CBR post-key GF interval = %d, want next-inter refresh", got)
	}

	qStart := newEncoder(t, RateControlQ)
	rows, cols = forceOpportunity(qStart)
	if !qStart.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("Q-start auto-golden refresh = false, want true")
	}
	if err := qStart.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(CBR) from Q returned error: %v", err)
	}
	if got := qStart.libvpxKeyFrameSetupGFInterval(rows, cols); got != 1 {
		t.Fatalf("Q-start runtime CBR post-key GF interval = %d, want next-inter refresh", got)
	}
}

func TestOnePassAutoGoldenDisabledForTwoPassStartup(t *testing.T) {
	sources := []Image{
		encoderValidationPanningFrame(32, 32, 0),
		encoderValidationPanningFrame(32, 32, 1),
		encoderValidationPanningFrame(32, 32, 2),
	}
	stats := collectRuntimeControlFirstPassStats(t, EncoderOptions{
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
	}, sources)
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		TwoPassStats:      stats,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	e.rc.framesTillGFUpdateDue = 0
	e.rc.thisFramePercentIntra = 10
	e.rc.recentRefFrameUsageIntra = rows * cols / 4
	e.rc.recentRefFrameUsageLast = rows * cols
	e.rc.gfActiveCount = rows * cols

	if e.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("two-pass startup auto-golden refresh = true, want false")
	}
}

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

func TestGFCBRBoostRequiresPriorLastZeroMVMajority(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	e.rc.framesTillGFUpdateDue = 0

	e.lastInterZeroMVCount = rows * cols / 2
	if e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = true, want false without LAST/ZEROMV majority")
	}
	e.lastInterZeroMVCount = rows*cols/2 + 1
	if !e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = false, want true with LAST/ZEROMV majority")
	}
}

func TestGFCBROpportunityUsesLibvpxCountdown(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	interval := e.goldenFrameCBRInterval(rows, cols)
	e.rc.framesSinceKeyframe = interval - 1
	e.rc.framesTillGFUpdateDue = 0
	e.lastInterZeroMVCount = rows*cols/2 + 1
	if !e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = false, want countdown-driven GF opportunity")
	}
}

func TestGoldenFrameCBRIntervalMirrorsLibvpxCyclicRefreshCadence(t *testing.T) {
	e := newSizedTestEncoder(t, 64, 80)

	if got := e.goldenFrameCBRInterval(geometry.MacroblockRows(e.opts.Height), geometry.MacroblockCols(e.opts.Width)); got != 40 {
		t.Fatalf("GF CBR interval = %d, want libvpx cyclic-refresh cadence clamp 40", got)
	}
}

func TestEncodeIntoGFCBRBoostDisabledForErrorResilient(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	for frame := 1; frame <= 11; frame++ {
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = true, want disabled for error resilient", frame)
		}
	}
}
