//go:build govpx_oracle_trace

package govpx_test

import (
	"fmt"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9OracleStreamSelectedCasesMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 selected stream byte parity")
	vp9test.RequireVpxencFrameFlags(t)

	type selectedCase struct {
		name        string
		width       int
		height      int
		frames      int
		opts        govpx.VP9EncoderOptions
		flags       []govpx.EncodeFlags
		extraArgs   []string
		source      func(width, height, frame int) *image.YCbCr
		exactPrefix int
		exactFrames []int
		strictBytes bool
	}
	fixedQOpts := govpx.VP9EncoderOptions{
		MinQuantizer: 20,
		MaxQuantizer: 20,
	}
	fixedQArgs := []string{
		"--cq-level=20",
		"--min-q=20",
		"--max-q=20",
		"--disable-warning-prompt",
	}
	cbrAQOpts := vp9oracle.CBROptions(64, 64, 700)
	cbrAQOpts.AQMode = govpx.VP9AQCyclicRefresh
	steppedSource := func(width, height, frame int) *image.YCbCr {
		return vp9test.NewYCbCr(width, height, uint8(96+frame*8), 128, 128)
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
			flags:       vp9oracle.RepeatAllFramesFlag(4, govpx.EncodeForceKeyFrame),
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
			opts:        vp9oracle.CBROptions(64, 64, 700),
			extraArgs:   vp9oracle.CBRArgs(700, 600, 400, 500, 0),
			source:      vp9test.NewPanningYCbCr,
			exactPrefix: 1,
		},
		{
			name:        "cbr-rate-panning-keyframe-64",
			width:       64,
			height:      64,
			frames:      1,
			opts:        vp9oracle.CBROptions(64, 64, 700),
			extraArgs:   vp9oracle.CBRArgs(700, 600, 400, 500, 0),
			source:      vp9test.NewPanningYCbCr,
			exactPrefix: 1,
			strictBytes: true,
		},
		{
			name:        "cbr-cyclic-rt-speed8-panning-keyframe-64",
			width:       64,
			height:      64,
			frames:      1,
			opts:        vp9oracle.CyclicRefreshCBROptions(64, 64, 700),
			extraArgs:   vp9oracle.CyclicRefreshCBRArgs(700, 600, 400, 500, 0),
			source:      vp9test.NewPanningYCbCr,
			exactPrefix: 1,
			strictBytes: true,
		},
		{
			name:        "frameflags-force-key-frame1",
			width:       64,
			height:      64,
			frames:      6,
			flags:       vp9oracle.FlagAt(6, 1, govpx.EncodeForceKeyFrame),
			source:      steppedSource,
			exactPrefix: 2,
			exactFrames: []int{5},
		},
		{
			name:        "frameflags-no-update-all",
			width:       64,
			height:      64,
			frames:      6,
			flags:       vp9oracle.RepeatInterFlag(6, vp9oracle.NoUpdateRefFlags),
			source:      steppedSource,
			exactPrefix: 5,
		},
		{
			name:   "control-cross-fixed-q-no-update-all",
			width:  64,
			height: 64,
			frames: 6,
			opts: govpx.VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags:       vp9oracle.RepeatInterFlag(6, vp9oracle.NoUpdateRefFlags),
			extraArgs:   []string{"--min-q=20", "--max-q=20"},
			source:      steppedSource,
			exactPrefix: 1,
		},
		{
			name:        "control-cross-cbr-force-key-frame3",
			width:       64,
			height:      64,
			frames:      6,
			opts:        vp9oracle.CBROptions(64, 64, 700),
			flags:       vp9oracle.FlagAt(6, 3, govpx.EncodeForceKeyFrame),
			extraArgs:   vp9oracle.CBRArgs(700, 600, 400, 500, 0),
			source:      steppedSource,
			exactPrefix: 4,
		},
		{
			name:   "noise-sensitivity-soft",
			width:  64,
			height: 64,
			frames: 2,
			opts: govpx.VP9EncoderOptions{
				NoiseSensitivity: 3,
			},
			extraArgs:   []string{"--noise-sensitivity=3"},
			exactPrefix: 1,
			source: func(width, height, frame int) *image.YCbCr {
				return vp9test.NewYCbCr(width, height, uint8(100+(frame&1)*2),
					128, 128)
			},
		},
		{
			name:   "noise-sensitivity-low-constant",
			width:  64,
			height: 64,
			frames: 2,
			opts: govpx.VP9EncoderOptions{
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
			opts: govpx.VP9EncoderOptions{
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
			opts: govpx.VP9EncoderOptions{
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
			extraArgs:   append(vp9oracle.CBRArgs(700, 600, 400, 500, 0), "--aq-mode=3"),
			exactPrefix: 1,
			source: func(width, height, frame int) *image.YCbCr {
				return vp9test.NewYCbCr(width, height, 128, 128, 128)
			},
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
			govpxPackets := vp9oracle.EncodeFramesWithGovpx(t, tc.opts,
				sources, tc.flags)
			libvpxPackets := vp9test.VpxencFrameFlagPackets(t, sources,
				vp9oracle.LibvpxFrameFlags(tc.flags), tc.extraArgs...)
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
