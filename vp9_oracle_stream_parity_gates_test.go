//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

func TestVP9OracleSelectedStreamByteParityGate(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 selected stream byte-parity gate")
	coracletest.VpxencVP9FrameFlags(t)

	type selectedCase struct {
		name        string
		width       int
		height      int
		frames      int
		opts        VP9EncoderOptions
		flags       []EncodeFlags
		extraArgs   []string
		source      func(width, height, frame int) *image.YCbCr
		before      func(*testing.T, *VP9Encoder, int)
		exactPrefix int
		exactFrames []int
		strictBytes bool
		tileJobs    int
	}
	fixedQOpts := VP9EncoderOptions{
		MinQuantizer: 20,
		MaxQuantizer: 20,
	}
	fixedQArgs := []string{
		"--cq-level=20",
		"--min-q=20",
		"--max-q=20",
		"--disable-warning-prompt",
	}
	threadedFixedQOpts := fixedQOpts
	threadedFixedQOpts.Threads = 4
	threadedFixedQArgs := append([]string{"--tile-columns=2"}, fixedQArgs...)
	cbrAQOpts := vp9OracleCBROptions(64, 64, 700)
	cbrAQOpts.AQMode = VP9AQCyclicRefresh
	steppedSource := func(width, height, frame int) *image.YCbCr {
		return newVP9YCbCrForTest(width, height, uint8(96+frame*8),
			128, 128)
	}

	cases := []selectedCase{
		{
			name:        "fixed-q-stepped-320",
			width:       320,
			height:      180,
			frames:      2,
			opts:        fixedQOpts,
			extraArgs:   fixedQArgs,
			exactPrefix: 1,
			source:      steppedSource,
		},
		{
			name:        "fixed-q-threaded-stepped-720p",
			width:       1280,
			height:      720,
			frames:      2,
			opts:        threadedFixedQOpts,
			extraArgs:   threadedFixedQArgs,
			exactPrefix: 1,
			tileJobs:    4,
			source:      steppedSource,
		},
		{
			name:        "fixed-q-threaded-force-key-stepped-720p",
			width:       1280,
			height:      720,
			frames:      2,
			opts:        threadedFixedQOpts,
			flags:       vp9OracleRepeatAllFramesFlag(2, EncodeForceKeyFrame),
			extraArgs:   threadedFixedQArgs,
			exactPrefix: 2,
			strictBytes: true,
			tileJobs:    4,
			source:      steppedSource,
		},
		{
			name:        "fixed-q-threaded-block-checker-keyframe-720p",
			width:       1280,
			height:      720,
			frames:      1,
			opts:        threadedFixedQOpts,
			extraArgs:   threadedFixedQArgs,
			exactPrefix: 1,
			strictBytes: true,
			tileJobs:    4,
			source:      newVP9BlockCheckerYCbCrForOracleTest,
		},
		{
			name:        "fixed-q-block-checker-keyframe-320",
			width:       320,
			height:      180,
			frames:      1,
			opts:        fixedQOpts,
			extraArgs:   fixedQArgs,
			exactPrefix: 1,
			strictBytes: true,
			source:      newVP9BlockCheckerYCbCrForOracleTest,
		},
		{
			name:        "fixed-q-force-key-block-checker-320",
			width:       320,
			height:      180,
			frames:      4,
			opts:        fixedQOpts,
			flags:       vp9OracleRepeatAllFramesFlag(4, EncodeForceKeyFrame),
			extraArgs:   fixedQArgs,
			exactPrefix: 4,
			strictBytes: true,
			source:      newVP9BlockCheckerYCbCrForOracleTest,
		},
		{
			name:        "cbr-rate-panning",
			width:       64,
			height:      64,
			frames:      4,
			opts:        vp9OracleCBROptions(64, 64, 700),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			source:      newVP9PanningYCbCrForRateTest,
			exactPrefix: 1,
		},
		{
			name:        "cbr-rate-panning-keyframe-64",
			width:       64,
			height:      64,
			frames:      1,
			opts:        vp9OracleCBROptions(64, 64, 700),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			source:      newVP9PanningYCbCrForRateTest,
			exactPrefix: 1,
			strictBytes: true,
		},
		{
			name:      "active-map-fixed-q-constant-320",
			width:     320,
			height:    180,
			frames:    2,
			opts:      fixedQOpts,
			extraArgs: append(fixedQArgs, "--control-script=-,active:checker"),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				if frame != 1 {
					return
				}
				activeMap, rows, cols := vp9OracleActiveMap(320, 180, "checker")
				mustVP9Runtime(t, "SetActiveMap checker",
					enc.SetActiveMap(activeMap, rows, cols))
			},
			exactPrefix: 2,
			strictBytes: true,
			source: func(width, height, frame int) *image.YCbCr {
				return newVP9YCbCrForTest(width, height, 128, 128, 128)
			},
		},
		{
			name:        "frameflags-force-key-frame1",
			width:       64,
			height:      64,
			frames:      6,
			flags:       vp9OracleFlagAt(6, 1, EncodeForceKeyFrame),
			source:      steppedSource,
			exactPrefix: 2,
			exactFrames: []int{5},
		},
		{
			name:        "frameflags-no-update-all",
			width:       64,
			height:      64,
			frames:      6,
			flags:       vp9OracleRepeatInterFlag(6, vp9NoUpdateRefFlags),
			source:      steppedSource,
			exactPrefix: 5,
		},
		{
			name:   "control-cross-fixed-q-no-update-all",
			width:  64,
			height: 64,
			frames: 6,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags:       vp9OracleRepeatInterFlag(6, vp9NoUpdateRefFlags),
			extraArgs:   []string{"--min-q=20", "--max-q=20"},
			source:      steppedSource,
			exactPrefix: 1,
		},
		{
			name:        "control-cross-cbr-force-key-frame3",
			width:       64,
			height:      64,
			frames:      6,
			opts:        vp9OracleCBROptions(64, 64, 700),
			flags:       vp9OracleFlagAt(6, 3, EncodeForceKeyFrame),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			source:      steppedSource,
			exactPrefix: 4,
		},
		{
			name:   "control-cross-threaded-ref-refresh",
			width:  1024,
			height: 64,
			frames: 6,
			opts: VP9EncoderOptions{
				Threads: 4,
			},
			flags:       vp9OracleRefRefreshTransitions(6),
			extraArgs:   []string{"--tile-columns=2"},
			source:      steppedSource,
			exactPrefix: 3,
			tileJobs:    4,
		},
		{
			name:   "noise-sensitivity-soft",
			width:  64,
			height: 64,
			frames: 2,
			opts: VP9EncoderOptions{
				NoiseSensitivity: 3,
			},
			extraArgs:   []string{"--noise-sensitivity=3"},
			exactPrefix: 1,
			source: func(width, height, frame int) *image.YCbCr {
				return newVP9YCbCrForTest(width, height,
					uint8(100+(frame&1)*2), 128, 128)
			},
		},
		{
			name:   "noise-sensitivity-low-constant",
			width:  64,
			height: 64,
			frames: 2,
			opts: VP9EncoderOptions{
				NoiseSensitivity: 1,
			},
			extraArgs:   []string{"--noise-sensitivity=1"},
			exactPrefix: 2,
			strictBytes: true,
			source: func(width, height, frame int) *image.YCbCr {
				return newVP9YCbCrForTest(width, height, 128, 128, 128)
			},
		},
		{
			name:   "noise-sensitivity-medium-constant",
			width:  64,
			height: 64,
			frames: 2,
			opts: VP9EncoderOptions{
				NoiseSensitivity: 2,
			},
			extraArgs:   []string{"--noise-sensitivity=2"},
			exactPrefix: 2,
			strictBytes: true,
			source: func(width, height, frame int) *image.YCbCr {
				return newVP9YCbCrForTest(width, height, 128, 128, 128)
			},
		},
		{
			name:   "noise-sensitivity-high-constant-320",
			width:  320,
			height: 180,
			frames: 2,
			opts: VP9EncoderOptions{
				NoiseSensitivity: 6,
			},
			extraArgs:   []string{"--noise-sensitivity=6"},
			exactPrefix: 2,
			strictBytes: true,
			source: func(width, height, frame int) *image.YCbCr {
				return newVP9YCbCrForTest(width, height, 128, 128, 128)
			},
		},
		{
			name:        "cbr-cyclic-aq-constant",
			width:       64,
			height:      64,
			frames:      2,
			opts:        cbrAQOpts,
			extraArgs:   append(vp9OracleCBRArgs(700, 600, 400, 500, 0), "--aq-mode=3"),
			exactPrefix: 1,
			source: func(width, height, frame int) *image.YCbCr {
				return newVP9YCbCrForTest(width, height, 128, 128, 128)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = tc.source(tc.width, tc.height, i)
			}
			var beforeFrame func(*VP9Encoder, int)
			var afterFrame func(*VP9Encoder, int)
			if tc.tileJobs > 0 || tc.before != nil {
				beforeFrame = func(enc *VP9Encoder, frame int) {
					if tc.tileJobs > 0 {
						resetVP9OracleThreadedTileJobsForTest(enc)
					}
					if tc.before != nil {
						tc.before(t, enc, frame)
					}
				}
			}
			if tc.tileJobs > 0 {
				afterFrame = func(enc *VP9Encoder, frame int) {
					assertVP9OracleThreadedTileWriterUsed(t, enc, frame, tc.tileJobs)
				}
			}
			govpxPackets, libvpxPackets := captureVP9StreamParityPacketsWithFrameHooks(t,
				tc.opts, sources, tc.flags, tc.extraArgs, beforeFrame, afterFrame)
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 selected stream byte-parity gate %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			for frame := 0; frame < tc.exactPrefix; frame++ {
				assertVP9PacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			for _, frame := range tc.exactFrames {
				assertVP9PacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			if tc.strictBytes && matches != len(govpxPackets) {
				t.Fatalf("strict VP9 selected stream byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OraclePinnedRuntimeControlByteParity(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 pinned runtime-control byte-parity gate")
	coracletest.VpxencVP9FrameFlags(t)

	const width, height, frames = 64, 64, 10
	type runtimeGateCase struct {
		name        string
		opts        VP9EncoderOptions
		flags       []EncodeFlags
		constant    bool
		before      func(*testing.T, *VP9Encoder, int)
		extraArgs   []string
		exactPrefix int
		exactFrames []int
		strictBytes bool
	}
	baseOpts := func(targetKbps int) VP9EncoderOptions {
		return vp9OracleCBROptions(width, height, targetKbps)
	}
	cases := []runtimeGateCase{
		{
			name:     "constant-buffer-model-two-step",
			opts:     baseOpts(700),
			constant: true,
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
			name: "constant-set-cq-level-cq-mode-window",
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			constant: true,
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
			name: "constant-cpu-used-two-step-fixed-q",
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			constant: true,
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
			name:     "constant-set-bitrate-kbps-two-step",
			opts:     baseOpts(700),
			constant: true,
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
			name:     "constant-set-rate-control-cbr-full-two-step",
			opts:     baseOpts(700),
			constant: true,
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
			name:     "constant-set-rate-control-vbr-cbr-roundtrip",
			opts:     baseOpts(700),
			constant: true,
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
			name: "constant-set-rate-control-q-cq-roundtrip",
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			constant: true,
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
			name: "constant-tuning-ssim-roundtrip-fixed-q",
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			constant: true,
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
			name: "constant-screen-content-mode-roundtrip-fixed-q",
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			constant: true,
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetScreenContentMode screen",
						enc.SetScreenContentMode(1))
				case 7:
					mustVP9Runtime(t, "SetScreenContentMode default",
						enc.SetScreenContentMode(0))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,screen:1,-,-,-,screen:0,-,-",
			},
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "constant-static-threshold-roundtrip-fixed-q",
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			constant: true,
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetStaticThreshold 500",
						enc.SetStaticThreshold(500))
				case 7:
					mustVP9Runtime(t, "SetStaticThreshold 0",
						enc.SetStaticThreshold(0))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,static:500,-,-,-,static:0,-,-",
			},
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "constant-aq-mode-variance-before-start-fixed-q",
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			constant: true,
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 0:
					mustVP9Runtime(t, "SetAQMode variance",
						enc.SetAQMode(VP9AQVariance))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=aq:1,-,-,-,-,-,-,-,-,-",
			},
			exactFrames: []int{1, 2, 3, 4, 5, 6, 7, 8, 9},
		},
		{
			name: "constant-lossless-roundtrip-fixed-q",
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			constant: true,
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetLossless true",
						enc.SetLossless(true))
				case 7:
					mustVP9Runtime(t, "SetLossless false",
						enc.SetLossless(false))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,-,-,lossless:1,-,-,-,lossless:0,-,-",
			},
			exactPrefix: frames,
			strictBytes: true,
		},
		{
			name: "constant-set-keyframe-interval-2-fixed-q",
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			constant: true,
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				if frame == 1 {
					mustVP9Runtime(t, "SetKeyFrameInterval 2",
						enc.SetKeyFrameInterval(2))
				}
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,kfmax:2,-,-,-,-,-,-,-,-",
			},
			exactPrefix: 2,
		},
		{
			name:  "bitrate-with-force-key",
			opts:  baseOpts(700),
			flags: vp9OracleFlagAt(frames, 4, EncodeForceKeyFrame),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 300",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 300}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 700",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 700}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--target-bitrate-schedule=3:300,7:700"),
			exactPrefix: 1,
			exactFrames: []int{4},
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
			exactPrefix: 1,
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
		},
		{
			name:     "constant-active-map-checker-toggle",
			opts:     baseOpts(700),
			constant: true,
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
			name:     "constant-roi-border-toggle",
			opts:     baseOpts(700),
			constant: true,
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
			name:     "constant-active-roi-combined-toggle",
			opts:     baseOpts(700),
			constant: true,
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
			name:     "constant-noise-sensitivity-toggle",
			opts:     baseOpts(700),
			constant: true,
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sources []*image.YCbCr
			if tc.constant {
				sources = make([]*image.YCbCr, frames)
				for i := range sources {
					sources[i] = newVP9YCbCrForTest(width, height, 128,
						128, 128)
				}
			} else {
				sources = newVP9OracleTransitionSources(width, height, frames)
			}
			govpxPackets, libvpxPackets := captureVP9StreamParityPacketsWithHooks(t,
				tc.opts, sources, tc.flags, tc.extraArgs,
				func(enc *VP9Encoder, frame int) {
					tc.before(t, enc, frame)
				})
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 pinned runtime-control byte-parity %s: matches=%d/%d first_mismatch=%d exact_prefix=%d exact_frames=%v",
				tc.name, matches, len(govpxPackets), firstMismatch,
				tc.exactPrefix, tc.exactFrames)
			for frame := 0; frame < tc.exactPrefix; frame++ {
				assertVP9PacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			for _, frame := range tc.exactFrames {
				assertVP9PacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			if tc.strictBytes && matches != len(govpxPackets) {
				t.Fatalf("strict VP9 pinned runtime-control byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleThreaded720pStrictByteParityUsesTileWriter(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 threaded 720p byte-parity gate")
	coracletest.VpxencVP9FrameFlags(t)

	const width, height, defaultFrames = 1280, 720, 2
	type threadedCase struct {
		name   string
		frames int
		opts   VP9EncoderOptions
		flags  []EncodeFlags
		args   []string
		source func(frame int) *image.YCbCr
		before func(*testing.T, *VP9Encoder, int)
	}
	steppedKeyframe := func(frame int) *image.YCbCr {
		return newVP9YCbCrForTest(width, height,
			uint8(96+frame*8), 128, 128)
	}
	activeMapBefore := func(t *testing.T, enc *VP9Encoder, frame int) {
		t.Helper()
		if frame != 1 {
			return
		}
		activeMap, rows, cols := vp9OracleActiveMap(width, height, "checker")
		mustVP9Runtime(t, "SetActiveMap checker",
			enc.SetActiveMap(activeMap, rows, cols))
	}
	cases := []threadedCase{
		{
			name:   "fixed-q",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "fixed-q-non-neutral-keyframe",
			frames: 1,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			source: steppedKeyframe,
		},
		{
			name:   "fixed-q-block-checker-keyframe",
			frames: 1,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			source: func(frame int) *image.YCbCr {
				return newVP9BlockCheckerYCbCrForOracleTest(width, height,
					frame)
			},
		},
		{
			name:   "fixed-q-force-key-stepped",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags: vp9OracleRepeatAllFramesFlag(4, EncodeForceKeyFrame),
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			source: steppedKeyframe,
		},
		{
			name:   "fixed-q-force-key-block-checker",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags: vp9OracleRepeatAllFramesFlag(2, EncodeForceKeyFrame),
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			source: func(frame int) *image.YCbCr {
				return newVP9BlockCheckerYCbCrForOracleTest(width, height,
					frame)
			},
		},
		{
			name:   "fixed-q-active-map",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,active:checker",
				"--disable-warning-prompt",
			},
			before: activeMapBefore,
		},
		{
			name: "vbr",
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=vbr",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "vbr-active-map",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=vbr",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--control-script=-,active:checker",
				"--disable-warning-prompt",
			},
			before: activeMapBefore,
		},
		{
			name: "cq",
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=cq",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "cq-active-map",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=cq",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--control-script=-,active:checker",
				"--disable-warning-prompt",
			},
			before: activeMapBefore,
		},
		{
			name:   "q",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=q",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "q-active-map",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=q",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--control-script=-,active:checker",
				"--disable-warning-prompt",
			},
			before: activeMapBefore,
		},
		{
			name:   "error-resilient",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads:        4,
				ErrorResilient: true,
			},
			args: []string{
				"--tile-columns=2",
				"--error-resilient=1",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "force-key-frame3",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads: 4,
			},
			flags: vp9OracleFlagAt(4, 3, EncodeForceKeyFrame),
			args: []string{
				"--tile-columns=2",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "no-reference-all",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads: 4,
			},
			flags: vp9OracleRepeatInterFlag(4,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			args: []string{
				"--tile-columns=2",
				"--disable-warning-prompt",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := tc.frames
			if frames == 0 {
				frames = defaultFrames
			}
			sources := make([]*image.YCbCr, frames)
			source := tc.source
			if source == nil {
				source = func(frame int) *image.YCbCr {
					return newVP9YCbCrForTest(width, height, 128, 128, 128)
				}
			}
			for i := range sources {
				sources[i] = source(i)
			}
			govpxPackets, libvpxPackets := captureVP9StreamParityPacketsWithFrameHooks(t,
				tc.opts, sources, tc.flags, tc.args,
				func(enc *VP9Encoder, frame int) {
					resetVP9OracleThreadedTileJobsForTest(enc)
					if tc.before != nil {
						tc.before(t, enc, frame)
					}
				},
				func(enc *VP9Encoder, frame int) {
					assertVP9OracleThreadedTileWriterUsed(t, enc, frame, 4)
				})
			if len(govpxPackets) != len(libvpxPackets) {
				t.Fatalf("threaded 720p %s packet count: govpx=%d libvpx=%d",
					tc.name, len(govpxPackets), len(libvpxPackets))
			}
			for frame := range govpxPackets {
				assertVP9PacketByteParity(t,
					fmt.Sprintf("threaded 720p %s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
		})
	}
}

func TestVP9OraclePinnedNewModeStrictByteParity(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 new-mode byte-parity gate")
	coracletest.VpxencVP9FrameFlags(t)

	type pinnedCase struct {
		name   string
		width  int
		height int
		frames int
		opts   VP9EncoderOptions
		args   []string
	}
	rateOptions := func(mode RateControlMode, targetKbps int) VP9EncoderOptions {
		opts := VP9EncoderOptions{
			RateControlModeSet:  true,
			RateControlMode:     mode,
			TargetBitrateKbps:   targetKbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
		}
		if mode == RateControlCQ || mode == RateControlQ {
			opts.CQLevel = 20
		}
		return opts
	}
	rateArgs := func(mode RateControlMode, targetKbps int) []string {
		endUsage := "vbr"
		if mode == RateControlCQ {
			endUsage = "cq"
		} else if mode == RateControlQ {
			endUsage = "q"
		}
		args := []string{
			"--end-usage=" + endUsage,
			fmt.Sprintf("--target-bitrate=%d", targetKbps),
			"--min-q=4",
			"--max-q=56",
		}
		if mode == RateControlCQ || mode == RateControlQ {
			args = append(args, "--cq-level=20")
		}
		return args
	}
	cases := []pinnedCase{
		{name: "vbr-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(RateControlVBR, 700),
			args: rateArgs(RateControlVBR, 700)},
		{name: "vbr-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(RateControlVBR, 700),
			args: rateArgs(RateControlVBR, 700)},
		{name: "vbr-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(RateControlVBR, 2200),
			args: rateArgs(RateControlVBR, 2200)},
		{name: "cq-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(RateControlCQ, 700),
			args: rateArgs(RateControlCQ, 700)},
		{name: "cq-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(RateControlCQ, 700),
			args: rateArgs(RateControlCQ, 700)},
		{name: "cq-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(RateControlCQ, 2200),
			args: rateArgs(RateControlCQ, 2200)},
		{name: "q-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(RateControlQ, 700),
			args: rateArgs(RateControlQ, 700)},
		{name: "q-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(RateControlQ, 700),
			args: rateArgs(RateControlQ, 700)},
		{name: "q-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(RateControlQ, 2200),
			args: rateArgs(RateControlQ, 2200)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = newVP9YCbCrForTest(tc.width, tc.height, 128, 128, 128)
			}
			govpxPackets, libvpxPackets := captureVP9StreamParityPackets(t,
				tc.opts, sources, nil, tc.args)
			if len(govpxPackets) != len(libvpxPackets) {
				t.Fatalf("VP9 new-mode %s packet count: govpx=%d libvpx=%d",
					tc.name, len(govpxPackets), len(libvpxPackets))
			}
			for frame := range govpxPackets {
				assertVP9PacketByteParity(t,
					fmt.Sprintf("VP9 new-mode %s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
		})
	}
}
