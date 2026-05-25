//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9OracleStreamSelectedCasesMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 selected stream byte parity")
	vp9test.RequireVpxencFrameFlags(t)

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
		return vp9test.NewYCbCr(width, height, uint8(96+frame*8),
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
			source:      vp9test.NewBlockCheckerYCbCr,
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
			source:      vp9test.NewBlockCheckerYCbCr,
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
			source:      vp9test.NewBlockCheckerYCbCr,
		},
		{
			name:        "cbr-rate-panning",
			width:       64,
			height:      64,
			frames:      4,
			opts:        vp9OracleCBROptions(64, 64, 700),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			source:      vp9test.NewPanningYCbCr,
			exactPrefix: 1,
		},
		{
			name:        "cbr-rate-panning-keyframe-64",
			width:       64,
			height:      64,
			frames:      1,
			opts:        vp9OracleCBROptions(64, 64, 700),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			source:      vp9test.NewPanningYCbCr,
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
				return vp9test.NewYCbCr(width, height, 128, 128, 128)
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
				return vp9test.NewYCbCr(width, height,
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
				return vp9test.NewYCbCr(width, height, 128, 128, 128)
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
				return vp9test.NewYCbCr(width, height, 128, 128, 128)
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
				return vp9test.NewYCbCr(width, height, 128, 128, 128)
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
				return vp9test.NewYCbCr(width, height, 128, 128, 128)
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
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 selected stream byte-parity gate %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
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
				t.Fatalf("strict VP9 selected stream byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}
