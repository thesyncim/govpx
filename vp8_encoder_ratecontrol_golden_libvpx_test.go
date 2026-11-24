package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

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
