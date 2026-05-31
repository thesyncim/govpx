//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"image"
	"testing"
)

func TestVP9OracleRuntimeControlTransitionsMatchLibvpx(t *testing.T) {
	const width, height, frames = 64, 64, 10
	opts := vp9OracleCBROptions(width, height, 900)
	opts.DropFrameAllowed = true
	opts.DropFrameWaterMark = 60
	sources := newVP9OracleTransitionSources(width, height, frames)
	rows := captureVP9RateTraceRowsWithHooks(t, opts, sources, nil,
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
	wantQ := encoder.PublicQuantizerToQIndex(20)
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
		vp9test.FormatSingleRateTraceRows(rows))
}

func TestVP9OracleRuntimeBitrateAndQuantizerControlsMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime bitrate/Q controls")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 8
	opts := vp9OracleCBROptions(width, height, 800)
	sources := newVP9OracleTransitionSources(width, height, frames)
	govpxRows := captureVP9RateTraceRowsWithHooks(t, opts, sources, nil,
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
	libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
		sources, nil, extraArgs)

	stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
	t.Logf("VP9 runtime bitrate/Q controls: %s", stats)
	t.Logf("VP9 runtime bitrate/Q rows:\n%s",
		vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
	if govpxRows[2].TargetBitrateKbps != 300 ||
		libvpxRows[2].TargetBitrateKbps != 300 {
		t.Fatalf("frame 2 target bitrate: govpx=%d libvpx=%d, want 300/300",
			govpxRows[2].TargetBitrateKbps, libvpxRows[2].TargetBitrateKbps)
	}
	wantQ := encoder.PublicQuantizerToQIndex(20)
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
	if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_CONTROL_STRICT") &&
		stats.HasMismatch() {
		t.Fatalf("strict VP9 runtime bitrate/Q mismatch: %s", stats)
	}
}

func TestVP9OracleRuntimeControlTransitionSeedsMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-control transition parity")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 10
	opts := vp9OracleCBROptions(width, height, 900)
	opts.DropFrameAllowed = true
	opts.DropFrameWaterMark = 60
	sources := newVP9OracleTransitionSources(width, height, frames)
	govpxRows := captureVP9RateTraceRowsWithHooks(t, opts, sources, nil,
		func(enc *VP9Encoder, frame int) {
			vp9ApplyRuntimeControlTransition(t, enc, frame)
		})
	extraArgs := append(vp9OracleCBRArgs(900, 600, 400, 500, 60),
		"--target-bitrate-schedule=2:300",
		"--min-q-schedule=4:20",
		"--max-q-schedule=4:20",
		"--fps-schedule=5:15",
		"--drop-frame-schedule=6:0,8:60")
	libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
		sources, nil, extraArgs)

	stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
	t.Logf("VP9 runtime-control transition parity: %s", stats)
	t.Logf("VP9 runtime-control transition parity rows:\n%s",
		vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
	if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_TRANSITION_STRICT") &&
		stats.HasMismatch() {
		t.Fatalf("strict VP9 runtime-control transition mismatch: %s", stats)
	}
}

func TestVP9OracleRuntimeControlMatrixMatchesLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-control matrix")
	vp9test.RequireVpxencFrameFlags(t)

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
			name: "buffer-model-two-step",
			opts: baseOpts(700),
			apply: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRateControlBuffer tight",
						enc.SetRateControlBuffer(400, 300, 350))
				case 8:
					mustVP9Runtime(t, "SetRateControlBuffer restore",
						enc.SetRateControlBuffer(600, 400, 500))
				}
			},
			extraArgs: []string{
				"--buf-sz-schedule=3:400,8:600",
				"--buf-initial-sz-schedule=3:300,8:400",
				"--buf-optimal-sz-schedule=3:350,8:500",
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
			govpxRows := captureVP9RateTraceRowsWithHooks(t, tc.opts,
				sources, nil,
				func(enc *VP9Encoder, frame int) {
					tc.apply(t, enc, frame)
				})
			extraArgs := append(vp9OracleCBRArgs(tc.opts.TargetBitrateKbps,
				tc.opts.BufferSizeMs, tc.opts.BufferInitialSizeMs,
				tc.opts.BufferOptimalSizeMs, vp9OracleDropFrameArg(tc.opts)),
				tc.extraArgs...)
			libvpxRows := captureLibvpxVP9RateTraceRows(t, width,
				height, sources, nil, extraArgs)

			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
			t.Logf("VP9 runtime-control matrix %s: %s",
				tc.name, stats)
			t.Logf("VP9 runtime-control matrix rows %s:\n%s",
				tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_MATRIX_STRICT") &&
				stats.HasMismatch() {
				t.Fatalf("strict VP9 runtime-control matrix mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleConstructionControlMatrixMatchesLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 construction-control matrix")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 6
	cases := []struct {
		name      string
		opts      VP9EncoderOptions
		extraArgs []string
	}{
		{
			name:      "public-q-default",
			opts:      VP9EncoderOptions{Width: width, Height: height},
			extraArgs: nil,
		},
		{
			name: "public-q-band-cq30",
			opts: VP9EncoderOptions{
				Width:        width,
				Height:       height,
				MinQuantizer: 10,
				MaxQuantizer: 50,
				CQLevel:      30,
			},
			extraArgs: []string{"--min-q=10", "--max-q=50", "--cq-level=30"},
		},
		{
			name: "public-lossless",
			opts: VP9EncoderOptions{
				Width:    width,
				Height:   height,
				Lossless: true,
			},
			extraArgs: []string{"--lossless=1"},
		},
		{
			name: "error-resilient-kf2",
			opts: VP9EncoderOptions{
				Width:               width,
				Height:              height,
				ErrorResilient:      true,
				MaxKeyframeInterval: 2,
			},
			extraArgs: []string{"--error-resilient=1", "--kf-max-dist=2"},
		},
		{
			name:      "cbr-buffer-default",
			opts:      vp9OracleCBROptions(width, height, 700),
			extraArgs: vp9OracleCBRArgs(700, 600, 400, 500, 0),
		},
		{
			name: "cbr-tight-q-band",
			opts: func() VP9EncoderOptions {
				opts := vp9OracleCBROptions(width, height, 700)
				opts.MinQuantizer = 10
				opts.MaxQuantizer = 50
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--min-q=10", "--max-q=50"),
		},
		{
			name: "cbr-fixed-q20",
			opts: func() VP9EncoderOptions {
				opts := vp9OracleCBROptions(width, height, 700)
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--min-q=20", "--max-q=20"),
		},
		{
			name: "cbr-tight-buffer-drop",
			opts: func() VP9EncoderOptions {
				opts := vp9OracleCBROptions(width, height, 140)
				opts.BufferSizeMs = 400
				opts.BufferInitialSizeMs = 300
				opts.BufferOptimalSizeMs = 350
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 60
				return opts
			}(),
			extraArgs: vp9OracleCBRArgs(140, 400, 300, 350, 60),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateTraceRows(t, tc.opts, sources, nil)
			libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
				sources, nil, tc.extraArgs)

			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
			t.Logf("VP9 construction-control matrix %s: %s",
				tc.name, stats)
			t.Logf("VP9 construction-control matrix rows %s:\n%s",
				tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			if vp9test.StrictEnv("GOVPX_VP9_CONSTRUCTION_MATRIX_STRICT") &&
				stats.HasMismatch() {
				t.Fatalf("strict VP9 construction-control matrix mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleTileThreadControlsMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 tile/thread controls")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 1024, 64, 6
	opts := vp9OracleCBROptions(width, height, 900)
	opts.Threads = 4
	sources := newVP9OracleTransitionSources(width, height, frames)
	govpxRows := captureVP9RateTraceRows(t, opts, sources, nil)
	extraArgs := append(vp9OracleCBRArgs(900, 600, 400, 500, 0),
		"--tile-columns=2")
	libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
		sources, nil, extraArgs)

	stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
	t.Logf("VP9 tile/thread controls: %s", stats)
	t.Logf("VP9 tile/thread control rows:\n%s",
		vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
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
	if vp9test.StrictEnv("GOVPX_VP9_TILE_THREAD_STRICT") &&
		stats.HasMismatch() {
		t.Fatalf("strict VP9 tile/thread mismatch: %s", stats)
	}
}

func TestVP9OracleInvisibleFrameVisibilityMatchesLibvpx(t *testing.T) {
	const width, height = 64, 64
	sources := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 64, 128, 128),
		vp9test.NewYCbCr(width, height, 188, 96, 224),
		vp9test.NewYCbCr(width, height, 188, 96, 224),
	}
	flags := []EncodeFlags{
		0,
		EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast |
			EncodeNoUpdateGolden | EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
		EncodeNoReferenceLast | EncodeNoReferenceGolden | EncodeNoUpdateLast,
	}
	rows := captureVP9RateTraceRows(t, VP9EncoderOptions{
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
		vp9test.FormatSingleRateTraceRows(rows))
}
