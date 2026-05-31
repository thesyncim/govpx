//go:build govpx_oracle_trace

package govpx_test

import (
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9OracleRuntimeControlConstantByteParityMatrix(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-control constant byte-parity matrix")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 10
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewYCbCr(width, height, 128, 128, 128)
	}

	type runtimeConstantCase struct {
		name        string
		opts        govpx.VP9EncoderOptions
		before      func(*testing.T, *govpx.VP9Encoder, int)
		extraArgs   []string
		exactPrefix int
		exactFrames []int
		strictBytes bool
	}
	baseOpts := func(targetKbps int) govpx.VP9EncoderOptions {
		return vp9oracle.CBROptions(width, height, targetKbps)
	}
	cases := []runtimeConstantCase{
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
			exactPrefix: 3,
		},
		{
			name: "set-bitrate-kbps-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetBitrateKbps 300",
						enc.SetBitrateKbps(300))
				case 7:
					vp9oracle.MustRuntime(t, "SetBitrateKbps 900",
						enc.SetBitrateKbps(900))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,-,-,bitrate:300,-,-,-,bitrate:900,-,-"),
			exactPrefix: 3,
		},
		{
			name: "set-rate-control-cbr-full-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRateControl CBR tight",
						enc.SetRateControl(govpx.RateControlConfig{
							Mode:                govpx.RateControlCBR,
							TargetBitrateKbps:   300,
							MinQuantizer:        10,
							MaxQuantizer:        50,
							BufferSizeMs:        400,
							BufferInitialSizeMs: 300,
							BufferOptimalSizeMs: 350,
							DropFrameAllowed:    true,
							DropFrameWaterMark:  60,
						}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRateControl CBR restore",
						enc.SetRateControl(govpx.RateControlConfig{
							Mode:                govpx.RateControlCBR,
							TargetBitrateKbps:   900,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							BufferSizeMs:        600,
							BufferInitialSizeMs: 400,
							BufferOptimalSizeMs: 500,
						}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,-,-,endusage:cbr+bitrate:300+minq:10+maxq:50+bufsz:400+bufinit:300+bufopt:350+drop:60,-,-,-,endusage:cbr+bitrate:900+minq:4+maxq:56+bufsz:600+bufinit:400+bufopt:500+drop:0,-,-"),
			exactPrefix: 3,
		},
		{
			name: "set-rate-control-vbr-cbr-roundtrip",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRateControl VBR",
						enc.SetRateControl(govpx.RateControlConfig{
							Mode:                govpx.RateControlVBR,
							TargetBitrateKbps:   700,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							BufferSizeMs:        6000,
							BufferInitialSizeMs: 4000,
							BufferOptimalSizeMs: 5000,
						}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRateControl CBR",
						enc.SetRateControl(govpx.RateControlConfig{
							Mode:                govpx.RateControlCBR,
							TargetBitrateKbps:   700,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							BufferSizeMs:        6000,
							BufferInitialSizeMs: 4000,
							BufferOptimalSizeMs: 5000,
						}))
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,-,-,endusage:vbr+bitrate:700+minq:4+maxq:56+bufsz:6000+bufinit:4000+bufopt:5000,-,-,-,endusage:cbr+bitrate:700+minq:4+maxq:56+bufsz:6000+bufinit:4000+bufopt:5000,-,-"),
			exactPrefix: frames,
			strictBytes: true,
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
			exactPrefix: 3,
			exactFrames: []int{6},
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
			exactPrefix: 7,
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
			exactPrefix: 3,
			exactFrames: []int{4, 5, 6, 7, 8, 9},
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
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "active-map-checker-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					activeMap, rows, cols := vp9oracle.ActiveMap(width,
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
			exactPrefix: 4,
			exactFrames: []int{8, 9},
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
			exactFrames: []int{7, 8, 9},
		},
		{
			name: "active-roi-combined-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					activeMap, rows, cols := vp9oracle.ActiveMap(width,
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
			exactFrames: []int{7, 8, 9},
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
			exactPrefix: 4,
			exactFrames: []int{7, 8, 9},
		},
		{
			name: "set-cq-level-cq-mode-window",
			opts: govpx.VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     govpx.RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetCQLevel 35", enc.SetCQLevel(35))
				case 7:
					vp9oracle.MustRuntime(t, "SetCQLevel 20", enc.SetCQLevel(20))
				}
			},
			extraArgs: []string{
				"--end-usage=cq",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--control-script=-,-,-,cq:35,-,-,-,cq:20,-,-",
			},
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "set-rate-control-q-cq-roundtrip",
			opts: govpx.VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     govpx.RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetRateControl Q",
						enc.SetRateControl(govpx.RateControlConfig{
							Mode:                govpx.RateControlQ,
							TargetBitrateKbps:   700,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							CQLevel:             20,
							BufferSizeMs:        6000,
							BufferInitialSizeMs: 4000,
							BufferOptimalSizeMs: 5000,
						}))
				case 7:
					vp9oracle.MustRuntime(t, "SetRateControl CQ",
						enc.SetRateControl(govpx.RateControlConfig{
							Mode:                govpx.RateControlCQ,
							TargetBitrateKbps:   700,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							CQLevel:             20,
							BufferSizeMs:        6000,
							BufferInitialSizeMs: 4000,
							BufferOptimalSizeMs: 5000,
						}))
				}
			},
			extraArgs: []string{
				"--end-usage=cq",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--control-script=-,-,-,endusage:q+bitrate:700+minq:4+maxq:56+cq:20+bufsz:6000+bufinit:4000+bufopt:5000,-,-,-,endusage:cq+bitrate:700+minq:4+maxq:56+cq:20+bufsz:6000+bufinit:4000+bufopt:5000,-,-",
			},
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "cpu-used-two-step-fixed-q",
			opts: govpx.VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetCPUUsed 4", enc.SetCPUUsed(4))
				case 7:
					vp9oracle.MustRuntime(t, "SetCPUUsed 5", enc.SetCPUUsed(5))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,cpu:4,-,-,-,cpu:5,-,-",
			},
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "tuning-ssim-roundtrip-fixed-q",
			opts: govpx.VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetTuning SSIM",
						enc.SetTuning(govpx.TuneSSIM))
				case 7:
					vp9oracle.MustRuntime(t, "SetTuning PSNR",
						enc.SetTuning(govpx.TunePSNR))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,tune:ssim,-,-,-,tune:psnr,-,-",
			},
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "cpu-used-minus3-roundtrip-fixed-q",
			opts: govpx.VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetCPUUsed -3", enc.SetCPUUsed(-3))
				case 7:
					vp9oracle.MustRuntime(t, "SetCPUUsed 8", enc.SetCPUUsed(8))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,cpu:-3,-,-,-,cpu:8,-,-",
			},
			exactPrefix: 3,
			exactFrames: []int{7, 8, 9},
		},
		{
			name: "cpu-used-minus8-roundtrip-fixed-q",
			opts: govpx.VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetCPUUsed -8", enc.SetCPUUsed(-8))
				case 7:
					vp9oracle.MustRuntime(t, "SetCPUUsed 8", enc.SetCPUUsed(8))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,cpu:-8,-,-,-,cpu:8,-,-",
			},
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "deadline-good-roundtrip-fixed-q",
			opts: govpx.VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetDeadline good",
						enc.SetDeadline(govpx.DeadlineGoodQuality))
				case 7:
					vp9oracle.MustRuntime(t, "SetDeadline rt",
						enc.SetDeadline(govpx.DeadlineRealtime))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,deadline:good,-,-,-,deadline:rt,-,-",
			},
			exactPrefix: 3,
			exactFrames: []int{8, 9},
		},
		{
			name: "deadline-best-roundtrip-fixed-q",
			opts: govpx.VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					vp9oracle.MustRuntime(t, "SetDeadline best",
						enc.SetDeadline(govpx.DeadlineBestQuality))
				case 7:
					vp9oracle.MustRuntime(t, "SetDeadline rt",
						enc.SetDeadline(govpx.DeadlineRealtime))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,deadline:best,-,-,-,deadline:rt,-,-",
			},
			exactPrefix: 3,
			exactFrames: []int{8, 9},
		},
		{
			name: "deadline-cpu-combined-fixed-q",
			opts: govpx.VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 0:
					vp9oracle.MustRuntime(t, "SetDeadline best",
						enc.SetDeadline(govpx.DeadlineBestQuality))
					vp9oracle.MustRuntime(t, "SetCPUUsed 0", enc.SetCPUUsed(0))
				case 3:
					vp9oracle.MustRuntime(t, "SetDeadline good",
						enc.SetDeadline(govpx.DeadlineGoodQuality))
					vp9oracle.MustRuntime(t, "SetCPUUsed 4", enc.SetCPUUsed(4))
				case 7:
					vp9oracle.MustRuntime(t, "SetDeadline rt",
						enc.SetDeadline(govpx.DeadlineRealtime))
					vp9oracle.MustRuntime(t, "SetCPUUsed 8", enc.SetCPUUsed(8))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=deadline:best+cpu:0,-,-,deadline:good+cpu:4,-,-,-,deadline:rt+cpu:8,-,-",
			},
			exactFrames: []int{8, 9},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			govpxPackets, libvpxPackets := vp9oracle.CaptureStreamParityPacketsWithHooks(t,
				tc.opts, sources, nil, tc.extraArgs,
				func(enc *govpx.VP9Encoder, frame int) {
					tc.before(t, enc, frame)
				})
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 runtime-control constant byte-parity matrix %s: matches=%d/%d first_mismatch=%d exact_prefix=%d exact_frames=%v",
				tc.name, matches, len(govpxPackets), firstMismatch,
				tc.exactPrefix, tc.exactFrames)
			t.Logf("VP9 runtime-control constant byte-parity rows %s:\n%s",
				tc.name, vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				vp9test.AssertPacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			for _, frame := range tc.exactFrames {
				vp9test.AssertPacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			if tc.strictBytes && matches != len(govpxPackets) {
				t.Fatalf("strict VP9 pinned runtime-control constant byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
			if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_CONSTANT_BYTE_STRICT") &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 runtime-control constant byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}
