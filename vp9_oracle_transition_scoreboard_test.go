//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"testing"
)

func TestVP9OracleFrameFlagTransitionScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 frame-flag transition scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 8
	opts := vp9OracleCBROptions(width, height, 600)
	extraArgs := vp9OracleCBRArgs(600, 600, 400, 500, 0)
	cases := []struct {
		name  string
		flags []EncodeFlags
	}{
		{
			name:  "force-kf-frame3",
			flags: vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame),
		},
		{
			name:  "repeat-no-update-last",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateLast),
		},
		{
			name:  "repeat-no-update-golden-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateGolden|EncodeNoUpdateAltRef),
		},
		{
			name:  "repeat-no-reference-golden-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
		},
		{
			name:  "force-golden-altref-transitions",
			flags: vp9OracleRefRefreshTransitions(frames),
		},
		{
			name:  "repeat-no-update-entropy",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateEntropy),
		},
		{
			name:  "alternating-reference-controls",
			flags: vp9OracleAlternatingReferenceControls(frames),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateScoreboardRows(t, opts, sources, tc.flags)
			libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
				sources, tc.flags, extraArgs)
			stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
			t.Logf("VP9 frame-flag transition scoreboard %s: %s",
				tc.name, stats)
			t.Logf("VP9 frame-flag transition rows %s:\n%s",
				tc.name, formatVP9RateScoreboardRows(govpxRows, libvpxRows))
			if os.Getenv("GOVPX_VP9_TRANSITION_SCOREBOARD_STRICT") == "1" &&
				stats.hasMismatch() {
				t.Fatalf("strict VP9 frame-flag transition mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleRuntimeControlTransitionScoreboard(t *testing.T) {
	const width, height, frames = 64, 64, 10
	opts := vp9OracleCBROptions(width, height, 900)
	opts.DropFrameAllowed = true
	opts.DropFrameWaterMark = 60
	sources := newVP9OracleTransitionSources(width, height, frames)
	rows := captureVP9RateScoreboardRowsWithHooks(t, opts, sources, nil,
		func(enc *VP9Encoder, frame int) {
			switch frame {
			case 2:
				if err := enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 300}); err != nil {
					t.Fatalf("SetRealtimeTarget bitrate at frame %d: %v", frame, err)
				}
			case 4:
				if err := enc.SetRealtimeTarget(RealtimeTarget{
					MinQuantizer: 20,
					MaxQuantizer: 20,
				}); err != nil {
					t.Fatalf("SetRealtimeTarget quantizers at frame %d: %v", frame, err)
				}
			case 5:
				if err := enc.SetRealtimeTarget(RealtimeTarget{FPS: 15}); err != nil {
					t.Fatalf("SetRealtimeTarget fps at frame %d: %v", frame, err)
				}
			case 6:
				if err := enc.SetRealtimeTarget(RealtimeTarget{
					FrameDrop: RealtimeFrameDropDisabled,
				}); err != nil {
					t.Fatalf("SetRealtimeTarget disable drop at frame %d: %v", frame, err)
				}
			case 8:
				if err := enc.SetFrameDropAllowed(true); err != nil {
					t.Fatalf("SetFrameDropAllowed at frame %d: %v", frame, err)
				}
			}
		})

	if len(rows) != frames {
		t.Fatalf("runtime control rows = %d, want %d", len(rows), frames)
	}
	if rows[2].TargetBitrateKbps != 300 {
		t.Fatalf("frame 2 target bitrate = %d, want 300",
			rows[2].TargetBitrateKbps)
	}
	if rows[5].FrameTargetBits <= rows[4].FrameTargetBits {
		t.Fatalf("frame 5 target bits = %d, want above frame 4 target %d after fps drop",
			rows[5].FrameTargetBits, rows[4].FrameTargetBits)
	}
	wantQ := vp9PublicQuantizerToQIndex(20)
	for frame := 4; frame <= 9; frame++ {
		if rows[frame].Dropped {
			continue
		}
		if rows[frame].BaseQIndex != wantQ {
			t.Fatalf("frame %d base qindex = %d, want fixed-q %d",
				frame, rows[frame].BaseQIndex, wantQ)
		}
	}
	t.Logf("VP9 runtime control transition rows:\n%s",
		formatVP9SingleRateScoreboardRows(rows))
}

func TestVP9OracleTemporalControlTransitionScoreboard(t *testing.T) {
	const width, height, frames = 64, 64, 9
	opts := vp9OracleCBROptions(width, height, 600)
	sources := newVP9OracleTransitionSources(width, height, frames)
	rows := captureVP9RateScoreboardRowsWithHooks(t, opts, sources, nil,
		func(enc *VP9Encoder, frame int) {
			switch frame {
			case 2:
				if err := enc.SetTemporalScalability(TemporalScalabilityConfig{
					Enabled: true,
					Mode:    TemporalLayeringTwoLayers,
				}); err != nil {
					t.Fatalf("SetTemporalScalability at frame %d: %v", frame, err)
				}
			case 6:
				if err := enc.SetTemporalLayerID(1); err != nil {
					t.Fatalf("SetTemporalLayerID at frame %d: %v", frame, err)
				}
			case 7:
				if err := enc.SetTemporalScalability(TemporalScalabilityConfig{}); err != nil {
					t.Fatalf("disable temporal at frame %d: %v", frame, err)
				}
			}
		})

	if len(rows) != frames {
		t.Fatalf("temporal control rows = %d, want %d", len(rows), frames)
	}
	seenLayer1 := false
	for frame := 2; frame <= 6; frame++ {
		if rows[frame].TemporalLayerCount != 2 {
			t.Fatalf("frame %d temporal layer count = %d, want 2",
				frame, rows[frame].TemporalLayerCount)
		}
		if rows[frame].TemporalLayerID == 1 {
			seenLayer1 = true
		}
	}
	if !seenLayer1 {
		t.Fatal("temporal control transition did not emit a layer-1 row")
	}
	if rows[7].TemporalLayerCount != 1 || rows[8].TemporalLayerCount != 1 {
		t.Fatalf("temporal disable rows = %d/%d, want 1/1",
			rows[7].TemporalLayerCount, rows[8].TemporalLayerCount)
	}
	t.Logf("VP9 temporal control transition rows:\n%s",
		formatVP9SingleRateScoreboardRows(rows))
}

func TestVP9OracleInvisibleFrameVisibilityScoreboard(t *testing.T) {
	const width, height = 64, 64
	sources := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 64, 128, 128),
		newVP9YCbCrForTest(width, height, 188, 96, 224),
		newVP9YCbCrForTest(width, height, 188, 96, 224),
	}
	flags := []EncodeFlags{
		0,
		EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast |
			EncodeNoUpdateGolden | EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
		EncodeNoReferenceLast | EncodeNoReferenceGolden | EncodeNoUpdateLast,
	}
	rows := captureVP9RateScoreboardRows(t, VP9EncoderOptions{
		Width:  width,
		Height: height,
	}, sources, flags)
	if len(rows) != len(sources) {
		t.Fatalf("invisible frame rows = %d, want %d", len(rows), len(sources))
	}
	if !rows[0].KeyFrame || !rows[0].ShowFrame {
		t.Fatalf("frame 0 key/show = %t/%t, want visible keyframe",
			rows[0].KeyFrame, rows[0].ShowFrame)
	}
	if rows[1].ShowFrame || rows[1].Dropped ||
		rows[1].RefreshFrameFlags != 1<<vp9AltRefSlot {
		t.Fatalf("frame 1 hidden row = show:%t dropped:%t refresh:%#x, want hidden ALTREF refresh",
			rows[1].ShowFrame, rows[1].Dropped, rows[1].RefreshFrameFlags)
	}
	if !rows[2].ShowFrame || rows[2].Dropped {
		t.Fatalf("frame 2 visible row = show:%t dropped:%t, want visible packet",
			rows[2].ShowFrame, rows[2].Dropped)
	}
	t.Logf("VP9 invisible-frame visibility rows:\n%s",
		formatVP9SingleRateScoreboardRows(rows))
}

type vp9OracleTransitionStats struct {
	Rows               int
	FlagMismatches     int
	DropMismatches     int
	KeyMismatches      int
	ShowMismatches     int
	QMismatches        int
	SizeMismatches     int
	TargetMismatches   int
	BufferMismatches   int
	RefreshMismatches  int
	TemporalMismatches int
	MaxQDrift          int
	MaxSizeDeltaPct    float64
	MaxBufferDeltaPct  float64
}

func (s vp9OracleTransitionStats) hasMismatch() bool {
	return s.FlagMismatches != 0 || s.DropMismatches != 0 ||
		s.KeyMismatches != 0 || s.ShowMismatches != 0 ||
		s.QMismatches != 0 || s.SizeMismatches != 0 ||
		s.TargetMismatches != 0 || s.BufferMismatches != 0 ||
		s.RefreshMismatches != 0 || s.TemporalMismatches != 0
}

func (s vp9OracleTransitionStats) String() string {
	return fmt.Sprintf("rows=%d flag=%d drop=%d key=%d show=%d q=%d size=%d target=%d buffer=%d refresh=%d temporal=%d max_q_drift=%d max_size_delta_pct=%.2f max_buffer_delta_pct=%.2f",
		s.Rows, s.FlagMismatches, s.DropMismatches, s.KeyMismatches,
		s.ShowMismatches, s.QMismatches, s.SizeMismatches, s.TargetMismatches,
		s.BufferMismatches, s.RefreshMismatches, s.TemporalMismatches,
		s.MaxQDrift, s.MaxSizeDeltaPct, s.MaxBufferDeltaPct)
}

func compareVP9OracleTransitionRows(t *testing.T, govpxRows, libvpxRows []vp9RateScoreboardRow) vp9OracleTransitionStats {
	t.Helper()
	if len(govpxRows) == 0 || len(libvpxRows) == 0 {
		t.Fatalf("empty VP9 transition scoreboard rows: govpx=%d libvpx=%d",
			len(govpxRows), len(libvpxRows))
	}
	if len(govpxRows) != len(libvpxRows) {
		t.Fatalf("VP9 transition row count: govpx=%d libvpx=%d",
			len(govpxRows), len(libvpxRows))
	}
	stats := vp9OracleTransitionStats{Rows: len(govpxRows)}
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		if g.FrameIndex != l.FrameIndex {
			t.Fatalf("row %d frame_index: govpx=%d libvpx=%d",
				i, g.FrameIndex, l.FrameIndex)
		}
		if g.RecodeAllowed || l.RecodeAllowed ||
			g.RecodeLoopCount != 0 || l.RecodeLoopCount != 0 {
			t.Fatalf("row %d recode: govpx allowed=%t loops=%d libvpx allowed=%t loops=%d, want one-pass VP9 no-recode",
				i, g.RecodeAllowed, g.RecodeLoopCount, l.RecodeAllowed,
				l.RecodeLoopCount)
		}
		if vp9FrameFlagsForLibvpx(EncodeFlags(g.Flags)) != l.Flags {
			stats.FlagMismatches++
		}
		if g.Dropped != l.Dropped {
			stats.DropMismatches++
		}
		if g.KeyFrame != l.KeyFrame {
			stats.KeyMismatches++
		}
		if g.ShowFrame != l.ShowFrame {
			stats.ShowMismatches++
		}
		if g.BaseQIndex != l.BaseQIndex {
			stats.QMismatches++
			drift := g.BaseQIndex - l.BaseQIndex
			if drift < 0 {
				drift = -drift
			}
			if drift > stats.MaxQDrift {
				stats.MaxQDrift = drift
			}
		}
		if g.SizeBits != l.SizeBits {
			stats.SizeMismatches++
			if delta := pctDelta(g.SizeBits, l.SizeBits); delta > stats.MaxSizeDeltaPct {
				stats.MaxSizeDeltaPct = delta
			}
		}
		if g.TargetBitrateKbps != l.TargetBitrateKbps ||
			g.FrameTargetBits != l.FrameTargetBits {
			stats.TargetMismatches++
		}
		if g.BufferLevelBits != l.BufferLevelBits {
			stats.BufferMismatches++
			if delta := pctDelta(g.BufferLevelBits, l.BufferLevelBits); delta > stats.MaxBufferDeltaPct {
				stats.MaxBufferDeltaPct = delta
			}
		}
		if g.RefreshFrameFlags != l.RefreshFrameFlags {
			stats.RefreshMismatches++
		}
		if g.TemporalLayerID != l.TemporalLayerID ||
			g.TemporalLayerCount != l.TemporalLayerCount ||
			g.TemporalLayerSync != l.TemporalLayerSync {
			stats.TemporalMismatches++
		}
	}
	return stats
}

func vp9OracleCBROptions(width, height, targetKbps int) VP9EncoderOptions {
	return VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	}
}

func vp9OracleCBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return []string{
		"--end-usage=cbr",
		fmt.Sprintf("--target-bitrate=%d", targetKbps),
		fmt.Sprintf("--buf-sz=%d", bufSizeMs),
		fmt.Sprintf("--buf-initial-sz=%d", bufInitialMs),
		fmt.Sprintf("--buf-optimal-sz=%d", bufOptimalMs),
		fmt.Sprintf("--drop-frame=%d", dropFrame),
	}
}

func newVP9OracleTransitionSources(width, height, frames int) []*image.YCbCr {
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = newVP9PanningYCbCrForRateTest(width, height, i)
	}
	return sources
}

func vp9OracleFlagAt(frames, index int, flag EncodeFlags) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	if uint(index) < uint(frames) {
		flags[index] = flag
	}
	return flags
}

func vp9OracleRepeatInterFlag(frames int, flag EncodeFlags) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := 1; i < frames; i++ {
		flags[i] = flag
	}
	return flags
}

func vp9OracleRefRefreshTransitions(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	if frames > 2 {
		flags[2] = EncodeForceGoldenFrame | EncodeNoUpdateLast
	}
	if frames > 4 {
		flags[4] = EncodeForceAltRefFrame | EncodeNoUpdateGolden
	}
	if frames > 6 {
		flags[6] = EncodeForceGoldenFrame | EncodeNoUpdateLast
	}
	return flags
}

func vp9OracleAlternatingReferenceControls(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := 1; i < frames; i++ {
		if i&1 == 0 {
			flags[i] = EncodeNoUpdateGolden | EncodeNoReferenceAltRef
		} else {
			flags[i] = EncodeNoUpdateAltRef | EncodeNoReferenceGolden
		}
	}
	return flags
}

func formatVP9SingleRateScoreboardRows(rows []vp9RateScoreboardRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,flags,drop,reason,key,show,q,bits,target,frame_target,buffer,refresh,tid,tlayers,tsync")
	for _, row := range rows {
		fmt.Fprintf(&b, "%d,%#x,%t,%s,%t,%t,%d,%d,%d,%d,%d,%#x,%d,%d,%t\n",
			row.FrameIndex, row.Flags, row.Dropped, row.DropReason, row.KeyFrame,
			row.ShowFrame, row.BaseQIndex, row.SizeBits, row.TargetBitrateKbps,
			row.FrameTargetBits, row.BufferLevelBits, row.RefreshFrameFlags,
			row.TemporalLayerID, row.TemporalLayerCount, row.TemporalLayerSync)
	}
	return b.String()
}
