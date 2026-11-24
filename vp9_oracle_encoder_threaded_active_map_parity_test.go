//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9OracleStreamSelectedThreadedAndActiveMapCasesMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 selected threaded and active-map byte parity")
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
	steppedSource := func(width, height, frame int) *image.YCbCr {
		return vp9test.NewYCbCr(width, height, uint8(96+frame*8), 128, 128)
	}

	cases := []selectedCase{
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
				activeMap, rows, cols := vp9test.ActiveMap(320, 180, "checker")
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			source := tc.source
			if source == nil {
				source = func(width, height, frame int) *image.YCbCr {
					return vp9test.NewYCbCr(width, height, 128, 128, 128)
				}
			}
			for i := range sources {
				sources[i] = source(tc.width, tc.height, i)
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
			t.Logf("VP9 selected stream threaded/active-map gate %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			for frame := 0; frame < tc.exactPrefix; frame++ {
				vp9test.AssertPacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			if tc.strictBytes && matches != len(govpxPackets) {
				t.Fatalf("strict VP9 selected stream threaded/active-map byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}
