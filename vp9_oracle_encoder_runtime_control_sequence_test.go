//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9OracleRuntimeControlByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-control byte-parity trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 10
	type runtimeCase struct {
		name        string
		opts        govpx.VP9EncoderOptions
		flags       []govpx.EncodeFlags
		before      func(*testing.T, *govpx.VP9Encoder, int)
		extraArgs   []string
		exactPrefix int
		exactFrames []int
		strictBytes bool
	}
	baseOpts := func(targetKbps int) govpx.VP9EncoderOptions {
		return vp9oracle.CBROptions(width, height, targetKbps)
	}
	cases := []runtimeCase{
		{
			name: "bitrate-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget bitrate 300",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{BitrateKbps: 300}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget bitrate 900",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{BitrateKbps: 900}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--target-bitrate-schedule=3:300,7:900"),
			exactPrefix: 1,
		},
		{
			name: "q-band-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget q band 10-50",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{
							MinQuantizer: 10,
							MaxQuantizer: 50,
						}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--min-q-schedule=3:10,7:4",
				"--max-q-schedule=3:50,7:56"),
			exactPrefix: 1,
		},
		{
			name: "fixed-q-runtime-window",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget fixed q 20",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{
							MinQuantizer: 20,
							MaxQuantizer: 20,
						}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--min-q-schedule=3:20,7:4",
				"--max-q-schedule=3:20,7:56"),
			exactPrefix: 1,
		},
		{
			name: "fps-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget fps 15",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{FPS: 15}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget fps 30",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{FPS: 30}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--fps-schedule=3:15,7:30"),
			exactPrefix: 1,
		},
		{
			name: "buffer-model-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRateControlBuffer tight",
						enc.SetRateControlBuffer(400, 300, 350))
				case 7:
					vp9oracle.MustRuntime(t, "SetRateControlBuffer restore",
						enc.SetRateControlBuffer(600, 400, 500))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--buf-sz-schedule=3:400,7:600",
				"--buf-initial-sz-schedule=3:300,7:400",
				"--buf-optimal-sz-schedule=3:350,7:500"),
			exactPrefix: 1,
		},
		{
			name: "combined-bitrate-fps-q",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget combined low",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{
							BitrateKbps:  300,
							FPS:          15,
							MinQuantizer: 10,
							MaxQuantizer: 50,
						}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget combined restore",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{
							BitrateKbps:  700,
							FPS:          30,
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--target-bitrate-schedule=3:300,7:700",
				"--fps-schedule=3:15,7:30",
				"--min-q-schedule=3:10,7:4",
				"--max-q-schedule=3:50,7:56"),
			exactPrefix: 1,
		},
		{
			name:  "bitrate-with-force-key",
			opts:  baseOpts(700),
			flags: vp9oracle.FlagAt(frames, 4, govpx.EncodeForceKeyFrame),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget bitrate 300",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{BitrateKbps: 300}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget bitrate 700",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{BitrateKbps: 700}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--target-bitrate-schedule=3:300,7:700"),
			exactPrefix: 1,
			exactFrames: []int{4},
		},
		{
			name:  "fixed-q-with-no-update-all",
			opts:  baseOpts(700),
			flags: vp9oracle.RepeatInterFlag(frames, vp9oracle.NoUpdateRefFlags),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget fixed q 20",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{
							MinQuantizer: 20,
							MaxQuantizer: 20,
						}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(govpx.RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--min-q-schedule=3:20,7:4",
				"--max-q-schedule=3:20,7:56"),
			exactPrefix: 1,
		},
		{
			name: "buffer-with-no-reference-all",
			opts: baseOpts(700),
			flags: vp9oracle.RepeatInterFlag(frames,
				govpx.EncodeNoReferenceLast|govpx.EncodeNoReferenceGolden|govpx.EncodeNoReferenceAltRef),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRateControlBuffer tight",
						enc.SetRateControlBuffer(400, 300, 350))
				case 7:
					vp9oracle.MustRuntime(t, "SetRateControlBuffer restore",
						enc.SetRateControlBuffer(600, 400, 500))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--buf-sz-schedule=3:400,7:600",
				"--buf-initial-sz-schedule=3:300,7:400",
				"--buf-optimal-sz-schedule=3:350,7:500"),
			exactPrefix: 1,
		},
		{
			name: "active-map-checker-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					activeMap, rows, cols := vp9test.ActiveMap(width,
						height, "checker")
					vp9oracle.MustRuntime(t, "SetActiveMap checker",
						enc.SetActiveMap(activeMap, rows, cols))
				case 7:
					vp9oracle.MustRuntime(t, "SetActiveMap nil",
						enc.SetActiveMap(nil, 0, 0))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,active:checker,-,-,-,-,-,active:off,-,-"),
			exactPrefix: 1,
		},
		{
			name: "roi-border-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					vp9oracle.MustRuntime(t, "SetROIMap border1",
						enc.SetROIMap(vp9oracle.ROIMap(width, height, "border1")))
				case 7:
					vp9oracle.MustRuntime(t, "SetROIMap nil", enc.SetROIMap(nil))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,roi:border1,-,-,-,-,-,roi:off,-,-"),
			exactPrefix: 1,
		},
		{
			name: "active-roi-combined-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					activeMap, rows, cols := vp9test.ActiveMap(width,
						height, "checker")
					vp9oracle.MustRuntime(t, "SetActiveMap checker",
						enc.SetActiveMap(activeMap, rows, cols))
					vp9oracle.MustRuntime(t, "SetROIMap border1",
						enc.SetROIMap(vp9oracle.ROIMap(width, height, "border1")))
				case 7:
					vp9oracle.MustRuntime(t, "SetActiveMap nil",
						enc.SetActiveMap(nil, 0, 0))
					vp9oracle.MustRuntime(t, "SetROIMap nil", enc.SetROIMap(nil))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,active:checker+roi:border1,-,-,-,-,-,active:off+roi:off,-,-"),
			exactPrefix: 1,
		},
		{
			name: "noise-sensitivity-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					vp9oracle.MustRuntime(t, "SetNoiseSensitivity 3",
						enc.SetNoiseSensitivity(3))
				case 7:
					vp9oracle.MustRuntime(t, "SetNoiseSensitivity 0",
						enc.SetNoiseSensitivity(0))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,noise:3,-,-,-,-,-,noise:0,-,-"),
			exactPrefix: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9oracle.TransitionSources(width, height, frames)
			govpxPackets, libvpxPackets := vp9oracle.CaptureStreamParityPacketsWithHooks(t,
				tc.opts, sources, tc.flags, tc.extraArgs,
				func(enc *govpx.VP9Encoder, frame int) {
					tc.before(t, enc, frame)
				})
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 runtime-control byte-parity trace %s: matches=%d/%d first_mismatch=%d exact_prefix=%d exact_frames=%v",
				tc.name, matches, len(govpxPackets), firstMismatch,
				tc.exactPrefix, tc.exactFrames)
			t.Logf("VP9 runtime-control byte-parity rows %s:\n%s",
				tc.name, vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					vp9test.AssertPacketByteParity(t,
						fmt.Sprintf("%s frame %d", tc.name, frame),
						govpxPackets[frame], libvpxPackets[frame])
				}
			}
			for _, frame := range tc.exactFrames {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					vp9test.AssertPacketByteParity(t,
						fmt.Sprintf("%s frame %d", tc.name, frame),
						govpxPackets[frame], libvpxPackets[frame])
				}
			}
			if tc.strictBytes && matches != len(govpxPackets) {
				t.Fatalf("strict VP9 pinned runtime-control byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
			if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_BYTE_STRICT") &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 runtime-control byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}
