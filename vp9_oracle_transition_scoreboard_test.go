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
			name:  "force-kf-frame1",
			flags: vp9OracleFlagAt(frames, 1, EncodeForceKeyFrame),
		},
		{
			name:  "force-kf-every-frame",
			flags: vp9OracleRepeatAllFramesFlag(frames, EncodeForceKeyFrame),
		},
		{
			name:  "repeat-no-update-last",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateLast),
		},
		{
			name:  "repeat-no-update-golden",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateGolden),
		},
		{
			name:  "repeat-no-update-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateAltRef),
		},
		{
			name:  "repeat-no-update-golden-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateGolden|EncodeNoUpdateAltRef),
		},
		{
			name:  "repeat-no-update-all",
			flags: vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
		},
		{
			name:  "repeat-no-reference-golden",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoReferenceGolden),
		},
		{
			name:  "repeat-no-reference-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoReferenceAltRef),
		},
		{
			name:  "repeat-no-reference-golden-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
		},
		{
			name: "repeat-no-reference-all",
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
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

func TestVP9OracleFrameFlagReferenceUpdateMatrixScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 reference/update matrix scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 6
	opts := vp9OracleCBROptions(width, height, 650)
	extraArgs := vp9OracleCBRArgs(650, 600, 400, 500, 0)
	cases := []struct {
		name string
		flag EncodeFlags
	}{
		{name: "no-update-last", flag: EncodeNoUpdateLast},
		{name: "no-update-golden", flag: EncodeNoUpdateGolden},
		{name: "no-update-altref", flag: EncodeNoUpdateAltRef},
		{name: "no-update-last-golden", flag: EncodeNoUpdateLast | EncodeNoUpdateGolden},
		{name: "no-update-all", flag: vp9NoUpdateRefFlags},
		{name: "no-reference-last", flag: EncodeNoReferenceLast},
		{name: "no-reference-golden", flag: EncodeNoReferenceGolden},
		{name: "no-reference-altref", flag: EncodeNoReferenceAltRef},
		{name: "no-reference-golden-altref", flag: EncodeNoReferenceGolden | EncodeNoReferenceAltRef},
		{name: "no-reference-all", flag: EncodeNoReferenceLast | EncodeNoReferenceGolden | EncodeNoReferenceAltRef},
		{name: "force-golden-no-update-last", flag: EncodeForceGoldenFrame | EncodeNoUpdateLast},
		{name: "force-altref-no-update-golden", flag: EncodeForceAltRefFrame | EncodeNoUpdateGolden},
		{name: "force-golden-altref-no-update-last", flag: EncodeForceGoldenFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			flags := vp9OracleRepeatInterFlag(frames, tc.flag)
			govpxRows := captureVP9RateScoreboardRows(t, opts, sources, flags)
			libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
				sources, flags, extraArgs)
			stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
			t.Logf("VP9 reference/update matrix scoreboard %s: %s",
				tc.name, stats)
			t.Logf("VP9 reference/update matrix rows %s:\n%s",
				tc.name, formatVP9RateScoreboardRows(govpxRows, libvpxRows))
			if os.Getenv("GOVPX_VP9_FLAG_MATRIX_STRICT") == "1" &&
				stats.hasMismatch() {
				t.Fatalf("strict VP9 reference/update matrix mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleOddSizeFrameFlagTransitionScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 odd-size transition scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 65, 63, 7
	opts := vp9OracleCBROptions(width, height, 650)
	extraArgs := vp9OracleCBRArgs(650, 600, 400, 500, 0)
	cases := []struct {
		name  string
		flags []EncodeFlags
	}{
		{
			name:  "force-kf-frame3",
			flags: vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame),
		},
		{
			name:  "repeat-no-update-all",
			flags: vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
		},
		{
			name: "repeat-no-reference-all",
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
		},
		{
			name:  "force-golden-altref-transitions",
			flags: vp9OracleRefRefreshTransitions(frames),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateScoreboardRows(t, opts, sources, tc.flags)
			libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
				sources, tc.flags, extraArgs)
			stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
			t.Logf("VP9 odd-size transition scoreboard %s: %s", tc.name, stats)
			t.Logf("VP9 odd-size transition rows %s:\n%s",
				tc.name, formatVP9RateScoreboardRows(govpxRows, libvpxRows))
			if os.Getenv("GOVPX_VP9_ODD_TRANSITION_STRICT") == "1" &&
				stats.hasMismatch() {
				t.Fatalf("strict VP9 odd-size transition mismatch %s: %s",
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
			vp9ApplyRuntimeControlTransition(t, enc, frame)
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

func TestVP9OracleRuntimeControlBitrateQuantizerScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 runtime bitrate/Q scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 8
	opts := vp9OracleCBROptions(width, height, 800)
	sources := newVP9OracleTransitionSources(width, height, frames)
	govpxRows := captureVP9RateScoreboardRowsWithHooks(t, opts, sources, nil,
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
			}
		})
	extraArgs := append(vp9OracleCBRArgs(800, 600, 400, 500, 0),
		"--target-bitrate-schedule=2:300",
		"--min-q-schedule=4:20",
		"--max-q-schedule=4:20")
	libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
		sources, nil, extraArgs)

	stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
	t.Logf("VP9 runtime bitrate/Q scoreboard: %s", stats)
	t.Logf("VP9 runtime bitrate/Q rows:\n%s",
		formatVP9RateScoreboardRows(govpxRows, libvpxRows))
	if govpxRows[2].TargetBitrateKbps != 300 ||
		libvpxRows[2].TargetBitrateKbps != 300 {
		t.Fatalf("frame 2 target bitrate: govpx=%d libvpx=%d, want 300/300",
			govpxRows[2].TargetBitrateKbps, libvpxRows[2].TargetBitrateKbps)
	}
	wantQ := vp9PublicQuantizerToQIndex(20)
	for frame := 4; frame < frames; frame++ {
		if govpxRows[frame].Dropped || libvpxRows[frame].Dropped {
			continue
		}
		if govpxRows[frame].BaseQIndex != wantQ ||
			libvpxRows[frame].BaseQIndex != wantQ {
			t.Fatalf("frame %d base qindex: govpx=%d libvpx=%d, want %d",
				frame, govpxRows[frame].BaseQIndex,
				libvpxRows[frame].BaseQIndex, wantQ)
		}
	}
	if os.Getenv("GOVPX_VP9_RUNTIME_CONTROL_STRICT") == "1" &&
		stats.hasMismatch() {
		t.Fatalf("strict VP9 runtime bitrate/Q mismatch: %s", stats)
	}
}

func TestVP9OracleRuntimeControlTransitionParityScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 runtime-control transition parity scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 10
	opts := vp9OracleCBROptions(width, height, 900)
	opts.DropFrameAllowed = true
	opts.DropFrameWaterMark = 60
	sources := newVP9OracleTransitionSources(width, height, frames)
	govpxRows := captureVP9RateScoreboardRowsWithHooks(t, opts, sources, nil,
		func(enc *VP9Encoder, frame int) {
			vp9ApplyRuntimeControlTransition(t, enc, frame)
		})
	extraArgs := append(vp9OracleCBRArgs(900, 600, 400, 500, 60),
		"--target-bitrate-schedule=2:300",
		"--min-q-schedule=4:20",
		"--max-q-schedule=4:20",
		"--fps-schedule=5:15",
		"--drop-frame-schedule=6:0,8:60")
	libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
		sources, nil, extraArgs)

	stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
	t.Logf("VP9 runtime-control transition parity scoreboard: %s", stats)
	t.Logf("VP9 runtime-control transition parity rows:\n%s",
		formatVP9RateScoreboardRows(govpxRows, libvpxRows))
	if os.Getenv("GOVPX_VP9_RUNTIME_TRANSITION_STRICT") == "1" &&
		stats.hasMismatch() {
		t.Fatalf("strict VP9 runtime-control transition mismatch: %s", stats)
	}
}

func TestVP9OracleRuntimeControlMatrixScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 runtime-control matrix scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 12
	type runtimeCase struct {
		name      string
		opts      VP9EncoderOptions
		apply     func(*testing.T, *VP9Encoder, int)
		extraArgs []string
	}
	baseOpts := func(targetKbps int) VP9EncoderOptions {
		return vp9OracleCBROptions(width, height, targetKbps)
	}
	cases := []runtimeCase{
		{
			name: "bitrate-only-two-step",
			opts: baseOpts(700),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 300",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 300}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 900",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 900}))
				}
			},
			extraArgs: []string{
				"--target-bitrate-schedule=3:300,8:900",
			},
		},
		{
			name: "q-band-only-two-step",
			opts: baseOpts(700),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget q band 10-50",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 10,
							MaxQuantizer: 50,
						}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: []string{
				"--min-q-schedule=3:10,8:4",
				"--max-q-schedule=3:50,8:56",
			},
		},
		{
			name: "fixed-q-runtime-window",
			opts: baseOpts(700),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fixed q 20",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 20,
							MaxQuantizer: 20,
						}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: []string{
				"--min-q-schedule=3:20,8:4",
				"--max-q-schedule=3:20,8:56",
			},
		},
		{
			name: "fps-only-two-step",
			opts: baseOpts(700),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fps 15",
						enc.SetRealtimeTarget(RealtimeTarget{FPS: 15}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget fps 30",
						enc.SetRealtimeTarget(RealtimeTarget{FPS: 30}))
				}
			},
			extraArgs: []string{
				"--fps-schedule=3:15,8:30",
			},
		},
		{
			name: "bitrate-fps-no-temporal",
			opts: baseOpts(700),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate/fps low",
						enc.SetRealtimeTarget(RealtimeTarget{
							BitrateKbps: 400,
							FPS:         15,
						}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate/fps restore",
						enc.SetRealtimeTarget(RealtimeTarget{
							BitrateKbps: 700,
							FPS:         30,
						}))
				}
			},
			extraArgs: []string{
				"--target-bitrate-schedule=3:400,8:700",
				"--fps-schedule=3:15,8:30",
			},
		},
		{
			name: "drop-frame-toggle",
			opts: func() VP9EncoderOptions {
				opts := baseOpts(120)
				opts.BufferSizeMs = 400
				opts.BufferInitialSizeMs = 300
				opts.BufferOptimalSizeMs = 350
				opts.DropFrameWaterMark = 60
				return opts
			}(),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget drop enabled",
						enc.SetRealtimeTarget(RealtimeTarget{
							FrameDrop: RealtimeFrameDropEnabled,
						}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget drop disabled",
						enc.SetRealtimeTarget(RealtimeTarget{
							FrameDrop: RealtimeFrameDropDisabled,
						}))
				}
			},
			extraArgs: []string{
				"--buf-sz=400",
				"--buf-initial-sz=300",
				"--buf-optimal-sz=350",
				"--drop-frame=0",
				"--drop-frame-schedule=3:60,8:0",
			},
		},
		{
			name: "fixed-q-drop-frame-toggle",
			opts: func() VP9EncoderOptions {
				opts := baseOpts(120)
				opts.BufferSizeMs = 400
				opts.BufferInitialSizeMs = 300
				opts.BufferOptimalSizeMs = 350
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				opts.DropFrameWaterMark = 60
				return opts
			}(),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fixed-q drop enabled",
						enc.SetRealtimeTarget(RealtimeTarget{
							FrameDrop: RealtimeFrameDropEnabled,
						}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget fixed-q drop disabled",
						enc.SetRealtimeTarget(RealtimeTarget{
							FrameDrop: RealtimeFrameDropDisabled,
						}))
				}
			},
			extraArgs: []string{
				"--min-q=20",
				"--max-q=20",
				"--buf-sz=400",
				"--buf-initial-sz=300",
				"--buf-optimal-sz=350",
				"--drop-frame=0",
				"--drop-frame-schedule=3:60,8:0",
			},
		},
		{
			name: "q-band-restores-after-drop-pressure",
			opts: func() VP9EncoderOptions {
				opts := baseOpts(140)
				opts.BufferSizeMs = 400
				opts.BufferInitialSizeMs = 300
				opts.BufferOptimalSizeMs = 350
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 60
				return opts
			}(),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fixed q under drop",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 20,
							MaxQuantizer: 20,
						}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget q band restore after drop",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: []string{
				"--buf-sz=400",
				"--buf-initial-sz=300",
				"--buf-optimal-sz=350",
				"--drop-frame=60",
				"--min-q-schedule=3:20,8:4",
				"--max-q-schedule=3:20,8:56",
			},
		},
		{
			name: "combined-bitrate-fps-q-drop",
			opts: func() VP9EncoderOptions {
				opts := baseOpts(700)
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 60
				return opts
			}(),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget combined low",
						enc.SetRealtimeTarget(RealtimeTarget{
							BitrateKbps:  300,
							FPS:          15,
							MinQuantizer: 10,
							MaxQuantizer: 50,
							FrameDrop:    RealtimeFrameDropEnabled,
						}))
				case 8:
					mustVP9Runtime(t, "SetRealtimeTarget combined restored",
						enc.SetRealtimeTarget(RealtimeTarget{
							BitrateKbps:  700,
							FPS:          30,
							MinQuantizer: 4,
							MaxQuantizer: 56,
							FrameDrop:    RealtimeFrameDropDisabled,
						}))
				}
			},
			extraArgs: []string{
				"--drop-frame=60",
				"--target-bitrate-schedule=3:300,8:700",
				"--fps-schedule=3:15,8:30",
				"--min-q-schedule=3:10,8:4",
				"--max-q-schedule=3:50,8:56",
				"--drop-frame-schedule=3:60,8:0",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateScoreboardRowsWithHooks(t, tc.opts,
				sources, nil,
				func(enc *VP9Encoder, frame int) {
					tc.apply(t, enc, frame)
				})
			extraArgs := append(vp9OracleCBRArgs(tc.opts.TargetBitrateKbps,
				tc.opts.BufferSizeMs, tc.opts.BufferInitialSizeMs,
				tc.opts.BufferOptimalSizeMs, vp9OracleDropFrameArg(tc.opts)),
				tc.extraArgs...)
			libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width,
				height, sources, nil, extraArgs)

			stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
			t.Logf("VP9 runtime-control matrix scoreboard %s: %s",
				tc.name, stats)
			t.Logf("VP9 runtime-control matrix rows %s:\n%s",
				tc.name, formatVP9RateScoreboardRows(govpxRows, libvpxRows))
			if os.Getenv("GOVPX_VP9_RUNTIME_MATRIX_STRICT") == "1" &&
				stats.hasMismatch() {
				t.Fatalf("strict VP9 runtime-control matrix mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleTileThreadControlScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 tile/thread control scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 1024, 64, 6
	opts := vp9OracleCBROptions(width, height, 900)
	opts.Threads = 4
	sources := newVP9OracleTransitionSources(width, height, frames)
	govpxRows := captureVP9RateScoreboardRows(t, opts, sources, nil)
	extraArgs := append(vp9OracleCBRArgs(900, 600, 400, 500, 0),
		"--tile-columns=2")
	libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
		sources, nil, extraArgs)

	stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
	t.Logf("VP9 tile/thread control scoreboard: %s", stats)
	t.Logf("VP9 tile/thread control rows:\n%s",
		formatVP9RateScoreboardRows(govpxRows, libvpxRows))
	tile2Rows := 0
	for i := range govpxRows {
		if govpxRows[i].Dropped || libvpxRows[i].Dropped {
			continue
		}
		if govpxRows[i].TileLog2Cols == 2 && libvpxRows[i].TileLog2Cols == 2 {
			tile2Rows++
		}
	}
	if tile2Rows == 0 {
		t.Fatal("VP9 tile/thread fixture did not expose any shared log2_tile_cols=2 row")
	}
	if os.Getenv("GOVPX_VP9_TILE_THREAD_STRICT") == "1" &&
		stats.hasMismatch() {
		t.Fatalf("strict VP9 tile/thread mismatch: %s", stats)
	}
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

func TestVP9OracleTemporalFlagPatternScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 temporal flag-pattern scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 12
	cases := []struct {
		name string
		mode TemporalLayeringMode
	}{
		{name: "two-layer", mode: TemporalLayeringTwoLayers},
		{name: "three-layer", mode: TemporalLayeringThreeLayers},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pattern, ok := temporalLayeringPattern(tc.mode)
			if !ok {
				t.Fatalf("temporalLayeringPattern(%d) failed", tc.mode)
			}
			opts := vp9OracleCBROptions(width, height, 700)
			opts.TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    tc.mode,
			}
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateScoreboardRows(t, opts, sources, nil)
			flags := vp9OracleTemporalPatternFlags(pattern, frames)
			libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
				sources, flags, vp9OracleCBRArgs(700, 600, 400, 500, 0))
			vp9ApplyExpectedTemporalMetadata(libvpxRows,
				buildExpectedTemporalPattern(pattern, frames), pattern.Layers)

			stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
			t.Logf("VP9 temporal flag-pattern scoreboard %s: %s", tc.name, stats)
			t.Logf("VP9 temporal flag-pattern rows %s:\n%s",
				tc.name, formatVP9RateScoreboardRows(govpxRows, libvpxRows))
			if os.Getenv("GOVPX_VP9_TEMPORAL_PATTERN_STRICT") == "1" &&
				stats.hasMismatch() {
				t.Fatalf("strict VP9 temporal flag-pattern mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
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
	Rows                     int
	FlagMismatches           int
	DropMismatches           int
	KeyMismatches            int
	ShowMismatches           int
	QMismatches              int
	PublicQMismatches        int
	SizeMismatches           int
	FirstPartitionMismatches int
	TargetMismatches         int
	BufferMismatches         int
	RefreshMismatches        int
	HeaderMismatches         int
	ModeHeaderMismatches     int
	LoopFilterMismatches     int
	TileMismatches           int
	TemporalMismatches       int
	TL0Mismatches            int
	MaxQDrift                int
	MaxSizeDeltaPct          float64
	MaxBufferDeltaPct        float64
}

func (s vp9OracleTransitionStats) hasMismatch() bool {
	return s.FlagMismatches != 0 || s.DropMismatches != 0 ||
		s.KeyMismatches != 0 || s.ShowMismatches != 0 ||
		s.QMismatches != 0 || s.PublicQMismatches != 0 ||
		s.SizeMismatches != 0 || s.FirstPartitionMismatches != 0 ||
		s.TargetMismatches != 0 || s.BufferMismatches != 0 ||
		s.RefreshMismatches != 0 || s.HeaderMismatches != 0 ||
		s.ModeHeaderMismatches != 0 || s.LoopFilterMismatches != 0 ||
		s.TileMismatches != 0 || s.TemporalMismatches != 0 ||
		s.TL0Mismatches != 0
}

func (s vp9OracleTransitionStats) String() string {
	return fmt.Sprintf("rows=%d flag=%d drop=%d key=%d show=%d q=%d public_q=%d size=%d first_part=%d target=%d buffer=%d refresh=%d header=%d mode_header=%d lf=%d tile=%d temporal=%d tl0=%d max_q_drift=%d max_size_delta_pct=%.2f max_buffer_delta_pct=%.2f",
		s.Rows, s.FlagMismatches, s.DropMismatches, s.KeyMismatches,
		s.ShowMismatches, s.QMismatches, s.PublicQMismatches,
		s.SizeMismatches, s.FirstPartitionMismatches, s.TargetMismatches,
		s.BufferMismatches, s.RefreshMismatches, s.HeaderMismatches,
		s.ModeHeaderMismatches, s.LoopFilterMismatches, s.TileMismatches,
		s.TemporalMismatches, s.TL0Mismatches, s.MaxQDrift,
		s.MaxSizeDeltaPct, s.MaxBufferDeltaPct)
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
		if !g.Dropped && !l.Dropped && g.PublicQuantizer != l.PublicQuantizer {
			stats.PublicQMismatches++
		}
		if g.SizeBits != l.SizeBits {
			stats.SizeMismatches++
			if delta := pctDelta(g.SizeBits, l.SizeBits); delta > stats.MaxSizeDeltaPct {
				stats.MaxSizeDeltaPct = delta
			}
		}
		if !g.Dropped && !l.Dropped &&
			g.FirstPartitionSize != l.FirstPartitionSize {
			stats.FirstPartitionMismatches++
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
		if !g.Dropped && !l.Dropped &&
			(g.RefreshFrameContext != l.RefreshFrameContext ||
				g.ErrorResilient != l.ErrorResilient ||
				g.FrameParallel != l.FrameParallel ||
				g.FrameContextIdx != l.FrameContextIdx) {
			stats.HeaderMismatches++
		}
		if !g.Dropped && !l.Dropped &&
			(g.TxMode != l.TxMode ||
				g.InterpFilter != l.InterpFilter ||
				g.ReferenceMode != l.ReferenceMode ||
				g.CompoundAllowed != l.CompoundAllowed ||
				g.ReferenceMask != l.ReferenceMask) {
			stats.ModeHeaderMismatches++
		}
		if !g.Dropped && !l.Dropped &&
			g.LoopFilterLevel != l.LoopFilterLevel {
			stats.LoopFilterMismatches++
		}
		if !g.Dropped && !l.Dropped &&
			(g.TileLog2Cols != l.TileLog2Cols ||
				g.TileLog2Rows != l.TileLog2Rows) {
			stats.TileMismatches++
		}
		if g.TemporalLayerID != l.TemporalLayerID ||
			g.TemporalLayerCount != l.TemporalLayerCount ||
			g.TemporalLayerSync != l.TemporalLayerSync {
			stats.TemporalMismatches++
		}
		if g.TL0PICIDX != l.TL0PICIDX {
			stats.TL0Mismatches++
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

func vp9OracleDropFrameArg(opts VP9EncoderOptions) int {
	if !opts.DropFrameAllowed {
		return 0
	}
	return opts.DropFrameWaterMark
}

func mustVP9Runtime(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
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

func vp9OracleRepeatAllFramesFlag(frames int, flag EncodeFlags) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := range flags {
		flags[i] = flag
	}
	return flags
}

func vp9ApplyRuntimeControlTransition(t *testing.T, enc *VP9Encoder, frame int) {
	t.Helper()
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
}

func vp9OracleTemporalPatternFlags(pattern temporalPattern, frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := range flags {
		flagIndex := i % pattern.FlagPeriodicity
		f := pattern.Flags[flagIndex]
		if i > 0 && flagIndex == 0 {
			f &^= EncodeForceKeyFrame
		}
		if i == 0 {
			f &^= vp9NoUpdateRefFlags
		}
		flags[i] = f
	}
	return flags
}

func vp9ApplyExpectedTemporalMetadata(rows []vp9RateScoreboardRow, expected []expectedTemporalRow, layers int) {
	for i := range rows {
		if i >= len(expected) {
			return
		}
		rows[i].TemporalLayerID = expected[i].layerID
		rows[i].TemporalLayerCount = layers
		rows[i].TemporalLayerSync = expected[i].layerSync
		rows[i].TL0PICIDX = uint8(expected[i].tl0picidx)
	}
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
	fmt.Fprintln(&b, "frame,flags,drop,reason,key,show,q,public_q,bytes,bits,first_part,target,frame_target,buffer,refresh,refresh_ctx,tx,filter,refmode,refmask,lf,tile_cols,tid,tlayers,tl0,tsync")
	for _, row := range rows {
		fmt.Fprintf(&b, "%d,%#x,%t,%s,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%#x,%t,%d,%d,%d,%#x,%d,%d,%d,%d,%d,%t\n",
			row.FrameIndex, row.Flags, row.Dropped, row.DropReason, row.KeyFrame,
			row.ShowFrame, row.BaseQIndex, row.PublicQuantizer, row.SizeBytes,
			row.SizeBits, row.FirstPartitionSize, row.TargetBitrateKbps,
			row.FrameTargetBits, row.BufferLevelBits, row.RefreshFrameFlags,
			row.RefreshFrameContext, row.TxMode, row.InterpFilter,
			row.ReferenceMode, row.ReferenceMask, row.LoopFilterLevel,
			row.TileLog2Cols, row.TemporalLayerID, row.TemporalLayerCount,
			row.TL0PICIDX, row.TemporalLayerSync)
	}
	return b.String()
}
