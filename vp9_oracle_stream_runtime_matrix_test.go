//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
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
		opts        VP9EncoderOptions
		before      func(*testing.T, *VP9Encoder, int)
		extraArgs   []string
		exactPrefix int
		exactFrames []int
		strictBytes bool
	}
	baseOpts := func(targetKbps int) VP9EncoderOptions {
		return vp9OracleCBROptions(width, height, targetKbps)
	}
	cases := []runtimeConstantCase{
		{
			name: "bitrate-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 300",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 300}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 900",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 900}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--target-bitrate-schedule=3:300,7:900"),
			exactPrefix: 3,
		},
		{
			name: "set-bitrate-kbps-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetBitrateKbps 300",
						enc.SetBitrateKbps(300))
				case 7:
					mustVP9Runtime(t, "SetBitrateKbps 900",
						enc.SetBitrateKbps(900))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,-,-,bitrate:300,-,-,-,bitrate:900,-,-"),
			exactPrefix: 3,
		},
		{
			name: "set-rate-control-cbr-full-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRateControl CBR tight",
						enc.SetRateControl(RateControlConfig{
							Mode:                RateControlCBR,
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
					mustVP9Runtime(t, "SetRateControl CBR restore",
						enc.SetRateControl(RateControlConfig{
							Mode:                RateControlCBR,
							TargetBitrateKbps:   900,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							BufferSizeMs:        600,
							BufferInitialSizeMs: 400,
							BufferOptimalSizeMs: 500,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,-,-,endusage:cbr+bitrate:300+minq:10+maxq:50+bufsz:400+bufinit:300+bufopt:350+drop:60,-,-,-,endusage:cbr+bitrate:900+minq:4+maxq:56+bufsz:600+bufinit:400+bufopt:500+drop:0,-,-"),
			exactPrefix: 3,
		},
		{
			name: "set-rate-control-vbr-cbr-roundtrip",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRateControl VBR",
						enc.SetRateControl(RateControlConfig{
							Mode:                RateControlVBR,
							TargetBitrateKbps:   700,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							BufferSizeMs:        6000,
							BufferInitialSizeMs: 4000,
							BufferOptimalSizeMs: 5000,
						}))
				case 7:
					mustVP9Runtime(t, "SetRateControl CBR",
						enc.SetRateControl(RateControlConfig{
							Mode:                RateControlCBR,
							TargetBitrateKbps:   700,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							BufferSizeMs:        6000,
							BufferInitialSizeMs: 4000,
							BufferOptimalSizeMs: 5000,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,-,-,endusage:vbr+bitrate:700+minq:4+maxq:56+bufsz:6000+bufinit:4000+bufopt:5000,-,-,-,endusage:cbr+bitrate:700+minq:4+maxq:56+bufsz:6000+bufinit:4000+bufopt:5000,-,-"),
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "q-band-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget q band 10-50",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 10,
							MaxQuantizer: 50,
						}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--min-q-schedule=3:10,7:4",
				"--max-q-schedule=3:50,7:56"),
			exactPrefix: 3,
			exactFrames: []int{6},
		},
		{
			name: "fixed-q-runtime-window",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fixed q 20",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 20,
							MaxQuantizer: 20,
						}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--min-q-schedule=3:20,7:4",
				"--max-q-schedule=3:20,7:56"),
			exactPrefix: 7,
		},
		{
			name: "fps-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fps 15",
						enc.SetRealtimeTarget(RealtimeTarget{FPS: 15}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget fps 30",
						enc.SetRealtimeTarget(RealtimeTarget{FPS: 30}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--fps-schedule=3:15,7:30"),
			exactPrefix: 3,
			exactFrames: []int{4, 5, 6, 7, 8, 9},
		},
		{
			name: "buffer-model-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRateControlBuffer tight",
						enc.SetRateControlBuffer(400, 300, 350))
				case 7:
					mustVP9Runtime(t, "SetRateControlBuffer restore",
						enc.SetRateControlBuffer(600, 400, 500))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--buf-sz-schedule=3:400,7:600",
				"--buf-initial-sz-schedule=3:300,7:400",
				"--buf-optimal-sz-schedule=3:350,7:500"),
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "active-map-checker-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					activeMap, rows, cols := vp9OracleActiveMap(width,
						height, "checker")
					mustVP9Runtime(t, "SetActiveMap checker",
						enc.SetActiveMap(activeMap, rows, cols))
				case 7:
					mustVP9Runtime(t, "SetActiveMap nil",
						enc.SetActiveMap(nil, 0, 0))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,active:checker,-,-,-,-,-,active:off,-,-"),
			exactPrefix: 4,
			exactFrames: []int{8, 9},
		},
		{
			name: "roi-border-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					mustVP9Runtime(t, "SetROIMap border1",
						enc.SetROIMap(vp9OracleROIMap(width, height, "border1")))
				case 7:
					mustVP9Runtime(t, "SetROIMap nil", enc.SetROIMap(nil))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,roi:border1,-,-,-,-,-,roi:off,-,-"),
			exactPrefix: 1,
			exactFrames: []int{7, 8, 9},
		},
		{
			name: "active-roi-combined-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					activeMap, rows, cols := vp9OracleActiveMap(width,
						height, "checker")
					mustVP9Runtime(t, "SetActiveMap checker",
						enc.SetActiveMap(activeMap, rows, cols))
					mustVP9Runtime(t, "SetROIMap border1",
						enc.SetROIMap(vp9OracleROIMap(width, height, "border1")))
				case 7:
					mustVP9Runtime(t, "SetActiveMap nil",
						enc.SetActiveMap(nil, 0, 0))
					mustVP9Runtime(t, "SetROIMap nil", enc.SetROIMap(nil))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,active:checker+roi:border1,-,-,-,-,-,active:off+roi:off,-,-"),
			exactPrefix: 1,
			exactFrames: []int{7, 8, 9},
		},
		{
			name: "noise-sensitivity-toggle",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 1:
					mustVP9Runtime(t, "SetNoiseSensitivity 3",
						enc.SetNoiseSensitivity(3))
				case 7:
					mustVP9Runtime(t, "SetNoiseSensitivity 0",
						enc.SetNoiseSensitivity(0))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--control-script=-,noise:3,-,-,-,-,-,noise:0,-,-"),
			exactPrefix: 4,
			exactFrames: []int{7, 8, 9},
		},
		{
			name: "set-cq-level-cq-mode-window",
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetCQLevel 35", enc.SetCQLevel(35))
				case 7:
					mustVP9Runtime(t, "SetCQLevel 20", enc.SetCQLevel(20))
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
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRateControl Q",
						enc.SetRateControl(RateControlConfig{
							Mode:                RateControlQ,
							TargetBitrateKbps:   700,
							MinQuantizer:        4,
							MaxQuantizer:        56,
							CQLevel:             20,
							BufferSizeMs:        6000,
							BufferInitialSizeMs: 4000,
							BufferOptimalSizeMs: 5000,
						}))
				case 7:
					mustVP9Runtime(t, "SetRateControl CQ",
						enc.SetRateControl(RateControlConfig{
							Mode:                RateControlCQ,
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
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetCPUUsed 4", enc.SetCPUUsed(4))
				case 7:
					mustVP9Runtime(t, "SetCPUUsed 5", enc.SetCPUUsed(5))
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
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetTuning SSIM",
						enc.SetTuning(TuneSSIM))
				case 7:
					mustVP9Runtime(t, "SetTuning PSNR",
						enc.SetTuning(TunePSNR))
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
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetCPUUsed -3", enc.SetCPUUsed(-3))
				case 7:
					mustVP9Runtime(t, "SetCPUUsed 8", enc.SetCPUUsed(8))
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
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetCPUUsed -8", enc.SetCPUUsed(-8))
				case 7:
					mustVP9Runtime(t, "SetCPUUsed 8", enc.SetCPUUsed(8))
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
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetDeadline good",
						enc.SetDeadline(DeadlineGoodQuality))
				case 7:
					mustVP9Runtime(t, "SetDeadline rt",
						enc.SetDeadline(DeadlineRealtime))
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
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetDeadline best",
						enc.SetDeadline(DeadlineBestQuality))
				case 7:
					mustVP9Runtime(t, "SetDeadline rt",
						enc.SetDeadline(DeadlineRealtime))
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
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 0:
					mustVP9Runtime(t, "SetDeadline best",
						enc.SetDeadline(DeadlineBestQuality))
					mustVP9Runtime(t, "SetCPUUsed 0", enc.SetCPUUsed(0))
				case 3:
					mustVP9Runtime(t, "SetDeadline good",
						enc.SetDeadline(DeadlineGoodQuality))
					mustVP9Runtime(t, "SetCPUUsed 4", enc.SetCPUUsed(4))
				case 7:
					mustVP9Runtime(t, "SetDeadline rt",
						enc.SetDeadline(DeadlineRealtime))
					mustVP9Runtime(t, "SetCPUUsed 8", enc.SetCPUUsed(8))
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
			govpxPackets, libvpxPackets := captureVP9StreamParityPacketsWithHooks(t,
				tc.opts, sources, nil, tc.extraArgs,
				func(enc *VP9Encoder, frame int) {
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
