package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestEncodeIntoUsesLibvpxInitialKeyFrameTargetBits(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.FrameTargetBits != e.rc.bufferInitialBits/2 {
		t.Fatalf("key target = key:%t bits:%d, want initial buffer half %d", key.KeyFrame, key.FrameTargetBits, e.rc.bufferInitialBits/2)
	}
	wantRC := e.rc
	wantRC.beginFrame(false)

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame || inter.FrameTargetBits != wantRC.frameTargetBits {
		t.Fatalf("inter target = key:%t bits:%d, want libvpx CBR buffer target %d", inter.KeyFrame, inter.FrameTargetBits, wantRC.frameTargetBits)
	}
}

func TestEncodeIntoCapsKeyFrameTargetBitsWithMaxIntraBitrate(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxIntraBitratePct:  200,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if result.FrameTargetBits != e.rc.bitsPerFrame*2 {
		t.Fatalf("key target bits = %d, want max intra cap %d", result.FrameTargetBits, e.rc.bitsPerFrame*2)
	}
}

func TestEncodeIntoUsesLibvpxLaterForcedKeyFrameTargetBits(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)
	for i := range 20 {
		if _, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, i), uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
	}
	wantRC := e.rc
	wantRC.beginFrameWithTargetAndContext(true, wantRC.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame:     true,
		temporalLayerCount: 1,
		timing:             e.timing,
	})

	e.ForceKeyFrame()
	result, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 20), 20, 1, 0)
	if err != nil {
		t.Fatalf("forced key EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || result.FrameTargetBits != wantRC.frameTargetBits {
		t.Fatalf("forced key target = key:%t bits:%d, want %d", result.KeyFrame, result.FrameTargetBits, wantRC.frameTargetBits)
	}
}

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
	for frame := 1; frame <= 11; frame++ {
		wantRC := e.rc
		wantRC.beginFrame(false)
		wantTarget := wantRC.frameTargetBits
		if frame == 11 {
			wantTarget = boostedFrameTargetBits(wantTarget, e.rc.gfCBRBoostPct)
		}
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if frame < 11 {
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
	for frame := 1; frame <= 11; frame++ {
		wantRC := e.rc
		wantRC.beginFrame(false)
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if frame < 11 && state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = true, want false before interval", frame)
		}
		if frame < 11 && state.Refresh.CopyBufferToAltRef != 0 {
			t.Fatalf("inter %d copy-to-alt = %d, want none before GF refresh", frame, state.Refresh.CopyBufferToAltRef)
		}
		if frame == 11 && !state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = false, want default libvpx CBR GF refresh", frame)
		}
		if frame == 11 && state.Refresh.CopyBufferToAltRef != 2 {
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
	for i := 0; i < 3; i++ {
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
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	e.rc.framesSinceKeyframe = e.goldenFrameCBRInterval(rows, cols)

	e.lastInterZeroMVCount = rows * cols / 2
	if e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = true, want false without LAST/ZEROMV majority")
	}
	e.lastInterZeroMVCount = rows*cols/2 + 1
	if !e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = false, want true with LAST/ZEROMV majority")
	}
}

func TestGoldenFrameCBRIntervalMirrorsLibvpxCyclicRefreshCadence(t *testing.T) {
	e := newSizedTestEncoder(t, 64, 80)

	if got := e.goldenFrameCBRInterval(encoderMacroblockRows(e.opts.Height), encoderMacroblockCols(e.opts.Width)); got != 40 {
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

func TestEncodeIntoCQLevelSelectsQuantizer(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 4096)
	// libvpx vp8/encoder/onyx_if.c lines 3727-3739: in CQ mode the
	// cq_target_quality floor only applies to inter non-refresh frames;
	// keyframes/golden/altref stay at best_quality. So the keyframe Q is
	// the regulator-picked value (which for a 16x16 fixture with a high
	// target bitrate sits at minQuantizer), not cqLevel.
	key, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if key.Quantizer >= 32 {
		t.Fatalf("key quantizer = %d, want below CQ level 32 (libvpx allows KF below cq_target_quality)", key.Quantizer)
	}
	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.Quantizer != 32 || packetBaseQIndex(t, inter.Data) != libvpxPublicQuantizerToQIndex(32) {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 32 / qindex %d", inter.Quantizer, packetBaseQIndex(t, inter.Data), libvpxPublicQuantizerToQIndex(32))
	}
}

func TestEncodeIntoWritesLibvpxFrameQuantDeltas(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        0,
		MaxQuantizer:        1,
		CQLevel:             0,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 4096)
	key, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyQuant := packetState(t, key.Data).Quant
	if keyQuant.BaseQIndex != 0 || keyQuant.Y2DCDelta != 4 {
		t.Fatalf("key quant = %+v, want base Q 0 with Y2 DC delta 4", keyQuant)
	}

	screen, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             56,
		ScreenContentMode:   1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("screen NewVP8Encoder returned error: %v", err)
	}
	if _, err := screen.EncodeInto(dst, rateControlTestFrame(16, 16, 0), 0, 1, 0); err != nil {
		t.Fatalf("screen key EncodeInto returned error: %v", err)
	}
	inter, err := screen.EncodeInto(dst, rateControlTestFrame(16, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	interQuant := packetState(t, inter.Data).Quant
	wantUVDelta := int8(-15)
	if interQuant.BaseQIndex != uint8(libvpxPublicQuantizerToQIndex(56)) || interQuant.UVDCDelta != wantUVDelta || interQuant.UVACDelta != wantUVDelta {
		t.Fatalf("inter quant = %+v, want screen-content UV deltas %d at qindex %d", interQuant, wantUVDelta, libvpxPublicQuantizerToQIndex(56))
	}
}

func TestEncodeIntoCQDefaultLevelMirrorsLibvpx(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
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
	if e.opts.CQLevel != defaultCQLevel || e.rc.currentQuantizer != libvpxPublicQuantizerToQIndex(defaultCQLevel) {
		t.Fatalf("default CQ = opts:%d q:%d, want public %d / qindex %d", e.opts.CQLevel, e.rc.currentQuantizer, defaultCQLevel, libvpxPublicQuantizerToQIndex(defaultCQLevel))
	}
}

func TestEncodeIntoCQOutputBitrateAdaptsToContent(t *testing.T) {
	newCQEncoder := func(t *testing.T) *VP8Encoder {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               32,
			Height:              32,
			FPS:                 30,
			RateControlMode:     RateControlCQ,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			CQLevel:             24,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
		}
		return e
	}
	flat := testImage(32, 32)
	fillImage(flat, 90, 90, 170)
	detailed := rateControlTestFrame(32, 32, 3)
	dst := make([]byte, 16384)

	flatResult, err := newCQEncoder(t).EncodeInto(dst, flat, 0, 1, 0)
	if err != nil {
		t.Fatalf("flat EncodeInto returned error: %v", err)
	}
	detailedResult, err := newCQEncoder(t).EncodeInto(dst, detailed, 0, 1, 0)
	if err != nil {
		t.Fatalf("detailed EncodeInto returned error: %v", err)
	}
	if detailedResult.SizeBytes <= flatResult.SizeBytes {
		t.Fatalf("CQ sizes = detailed:%d flat:%d, want detailed content to use more bits", detailedResult.SizeBytes, flatResult.SizeBytes)
	}
}

func TestEncodeIntoWritesConfiguredTokenPartitions(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		TokenPartitions:     int(vp8common.EightPartition),
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if got := packetTokenPartition(t, key.Data); got != vp8common.EightPartition {
		t.Fatalf("key token partition = %d, want eight", got)
	}

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want inter frame")
	}
	if got := packetTokenPartition(t, inter.Data); got != vp8common.EightPartition {
		t.Fatalf("inter token partition = %d, want eight", got)
	}
}

func TestEncodeIntoUpdatesRateControlAfterFrame(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	initialQuantizer := e.rc.currentQuantizer
	initialRollingActual := e.rc.rollingActualBits
	initialRollingTarget := e.rc.rollingTargetBits
	initialLongRollingActual := e.rc.longRollingActualBits
	initialLongRollingTarget := e.rc.longRollingTargetBits
	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	wantRollingActual := libvpxRollingBits(initialRollingActual, result.SizeBytes*8, 3, 2)
	wantRollingTarget := libvpxRollingBits(initialRollingTarget, result.FrameTargetBits, 3, 2)
	if e.rc.rollingActualBits != wantRollingActual || e.rc.rollingTargetBits != wantRollingTarget {
		t.Fatalf("rolling bits = actual:%d target:%d, want %d/%d", e.rc.rollingActualBits, e.rc.rollingTargetBits, wantRollingActual, wantRollingTarget)
	}
	wantLongRollingActual := libvpxRollingBits(initialLongRollingActual, result.SizeBytes*8, 31, 5)
	wantLongRollingTarget := libvpxRollingBits(initialLongRollingTarget, result.FrameTargetBits, 31, 5)
	if e.rc.longRollingActualBits != wantLongRollingActual || e.rc.longRollingTargetBits != wantLongRollingTarget {
		t.Fatalf("long rolling bits = actual:%d target:%d, want %d/%d", e.rc.longRollingActualBits, e.rc.longRollingTargetBits, wantLongRollingActual, wantLongRollingTarget)
	}
	if result.BufferLevelBits != e.rc.bufferLevelBits {
		t.Fatalf("result buffer = %d, want rc buffer %d", result.BufferLevelBits, e.rc.bufferLevelBits)
	}
	if e.rc.currentQuantizer <= initialQuantizer {
		t.Fatalf("currentQuantizer = %d, want above initial %d after overshoot", e.rc.currentQuantizer, initialQuantizer)
	}
	if e.rc.framesSinceKeyframe != 0 {
		t.Fatalf("framesSinceKeyframe = %d, want 0 after keyframe", e.rc.framesSinceKeyframe)
	}
}

func TestEncodeIntoRetriesQuantizerBeforeCommitOnOvershoot(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := rateControlTestFrame(32, 32, 0)
	packet := make([]byte, 16384)

	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	if result.Quantizer <= 4 {
		t.Fatalf("result quantizer = %d, want retry above initial 4", result.Quantizer)
	}
	if got := packetBaseQIndex(t, result.Data); got != libvpxPublicQuantizerToQIndex(result.Quantizer) {
		t.Fatalf("packet base q = %d, want public result quantizer %d mapped to qindex %d", got, result.Quantizer, libvpxPublicQuantizerToQIndex(result.Quantizer))
	}
	if e.rc.lastQuantizer != packetBaseQIndex(t, result.Data) {
		t.Fatalf("last quantizer = %d, want committed packet qindex %d", e.rc.lastQuantizer, packetBaseQIndex(t, result.Data))
	}
	decoded := decodeSingleFrame(t, result.Data)
	assertImagesEqual(t, "retried current", decoded, publicImageFromVP8(&e.current.Img))
}

func TestEncodeKeyFrameAttemptDefersEntropyCommit(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
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
	e.coefProbs[0][0][0][0] = 77
	e.modeProbs.MV[0][0] = 99
	wantCoefProbs := e.coefProbs
	wantModeProbs := e.modeProbs

	rows := encoderMacroblockRows(32)
	cols := encoderMacroblockCols(32)
	attempt, err := e.encodeKeyFrameAttempt(make([]byte, 16384), sourceImageFromImage(rateControlTestFrame(32, 32, 0)), rows, cols, rows*cols, false, false, e.rc.currentQuantizer)
	if err != nil {
		t.Fatalf("encodeKeyFrameAttempt returned error: %v", err)
	}
	if !attempt.RefreshEntropyProbs {
		t.Fatalf("key attempt RefreshEntropyProbs = false, want true")
	}
	if e.coefProbs != wantCoefProbs {
		t.Fatalf("encodeKeyFrameAttempt mutated coefficient probabilities before commit")
	}
	if e.modeProbs != wantModeProbs {
		t.Fatalf("encodeKeyFrameAttempt mutated mode probabilities before commit")
	}

	e.commitKeyFrameEntropy(attempt)
	if e.coefProbs != attempt.FrameCoefProbs {
		t.Fatalf("committed coefficient probabilities do not match accepted key attempt")
	}
	if e.modeProbs == wantModeProbs {
		t.Fatalf("committed keyframe mode probabilities still match pre-commit sentinel")
	}
}

func TestEncodeInterFrameAttemptDefersSkipFalseCommit(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	if _, err := e.EncodeInto(make([]byte, 16384), first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	e.probSkipFalse = 91
	rows := encoderMacroblockRows(32)
	cols := encoderMacroblockCols(32)
	attempt, err := e.encodeInterFrameAttempt(make([]byte, 16384), sourceImageFromImage(second), rows, cols, rows*cols, 0, false, false, true, false, e.rc.currentQuantizer, true, false)
	if err != nil {
		t.Fatalf("encodeInterFrameAttempt returned error: %v", err)
	}
	if e.probSkipFalse != 91 {
		t.Fatalf("inter attempt probSkipFalse = %d, want pre-attempt sentinel 91 before commit", e.probSkipFalse)
	}

	e.commitInterFrameAttempt(attempt)
	if e.probSkipFalse != attempt.Config.ProbSkipFalse {
		t.Fatalf("committed probSkipFalse = %d, want accepted attempt probability %d", e.probSkipFalse, attempt.Config.ProbSkipFalse)
	}
}

func TestRefFrameProbsFromUsageMirrorsLibvpxClamp(t *testing.T) {
	if _, _, _, ok := refFrameProbsFromUsage(0, 0, 0, 0); ok {
		t.Fatalf("empty ref usage returned ok=true, want false")
	}

	probIntra, probLast, probGolden, ok := refFrameProbsFromUsage(0, 0, 0, 4)
	if !ok {
		t.Fatalf("alt-only ref usage returned ok=false")
	}
	if probIntra != 1 || probLast != 1 || probGolden != 1 {
		t.Fatalf("alt-only probs = %d/%d/%d, want clamped 1/1/1", probIntra, probLast, probGolden)
	}

	probIntra, probLast, probGolden, ok = refFrameProbsFromUsage(1, 2, 1, 1)
	if !ok {
		t.Fatalf("mixed ref usage returned ok=false")
	}
	if probIntra != 51 || probLast != 127 || probGolden != 127 {
		t.Fatalf("mixed probs = %d/%d/%d, want 51/127/127", probIntra, probLast, probGolden)
	}
}

func TestCommitInterFrameEntropyRefreshesInterIntraModeProbs(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	vp8dec.ResetModeProbs(&e.modeProbs)
	original := e.modeProbs
	frameYModeProbs := vp8tables.DefaultYModeProbs
	frameYModeProbs[0] = 251
	frameUVModeProbs := vp8tables.DefaultUVModeProbs
	frameUVModeProbs[0] = 249
	frameMVProbs := vp8tables.DefaultMVContext
	frameMVProbs[0][0] = 99
	attempt := interFrameEncodeAttempt{
		Config:           vp8enc.InterFrameStateConfig{RefreshEntropyProbs: true},
		FrameCoefProbs:   e.coefProbs,
		FrameYModeProbs:  frameYModeProbs,
		FrameUVModeProbs: frameUVModeProbs,
		FrameMVProbs:     frameMVProbs,
	}

	e.commitInterFrameEntropy(attempt)

	if e.modeProbs.YMode != frameYModeProbs {
		t.Fatalf("committed Y mode probs = %v, want %v", e.modeProbs.YMode, frameYModeProbs)
	}
	if e.modeProbs.UVMode != frameUVModeProbs {
		t.Fatalf("committed UV mode probs = %v, want %v", e.modeProbs.UVMode, frameUVModeProbs)
	}
	if e.modeProbs.MV != frameMVProbs {
		t.Fatalf("committed MV probs = %v, want %v", e.modeProbs.MV, frameMVProbs)
	}

	e.modeProbs = original
	attempt.Config.RefreshEntropyProbs = false
	e.commitInterFrameEntropy(attempt)
	if e.modeProbs != original {
		t.Fatalf("mode probs changed on no-refresh commit: got %+v want %+v", e.modeProbs, original)
	}
}
