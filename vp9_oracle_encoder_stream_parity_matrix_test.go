//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9OracleEncoderStreamByteParityMatrix(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 stream byte-parity matrix")
	vp9test.RequireVpxencFrameFlags(t)

	type streamFixture struct {
		name   string
		width  int
		height int
		source func(width, height, frame int) *image.YCbCr
	}
	constant64 := streamFixture{
		name:   "constant-64x64",
		width:  64,
		height: 64,
		source: func(width, height, frame int) *image.YCbCr {
			return vp9test.NewYCbCr(width, height, 128, 128, 128)
		},
	}
	constant320 := streamFixture{
		name:   "constant-320x180",
		width:  320,
		height: 180,
		source: func(width, height, frame int) *image.YCbCr {
			return vp9test.NewYCbCr(width, height, 128, 128, 128)
		},
	}
	constant640 := streamFixture{
		name:   "constant-640x480",
		width:  640,
		height: 480,
		source: func(width, height, frame int) *image.YCbCr {
			return vp9test.NewYCbCr(width, height, 128, 128, 128)
		},
	}
	constant720 := streamFixture{
		name:   "constant-1280x720",
		width:  1280,
		height: 720,
		source: func(width, height, frame int) *image.YCbCr {
			return vp9test.NewYCbCr(width, height, 128, 128, 128)
		},
	}
	stepped64 := streamFixture{
		name:   "stepped-64x64",
		width:  64,
		height: 64,
		source: func(width, height, frame int) *image.YCbCr {
			return vp9test.NewYCbCr(width, height,
				uint8(96+frame*8), 128, 128)
		},
	}
	stepped320 := streamFixture{
		name:   "stepped-320x180",
		width:  320,
		height: 180,
		source: func(width, height, frame int) *image.YCbCr {
			return vp9test.NewYCbCr(width, height,
				uint8(96+frame*8), 128, 128)
		},
	}
	stepped720 := streamFixture{
		name:   "stepped-1280x720",
		width:  1280,
		height: 720,
		source: func(width, height, frame int) *image.YCbCr {
			return vp9test.NewYCbCr(width, height,
				uint8(96+frame*8), 128, 128)
		},
	}
	softNoise64 := streamFixture{
		name:   "soft-noise-64x64",
		width:  64,
		height: 64,
		source: func(width, height, frame int) *image.YCbCr {
			return vp9test.NewYCbCr(width, height,
				uint8(100+(frame&1)*2), 128, 128)
		},
	}
	panning64 := streamFixture{
		name:   "panning-64x64",
		width:  64,
		height: 64,
		source: vp9test.NewPanningYCbCr,
	}
	panning320 := streamFixture{
		name:   "panning-320x180",
		width:  320,
		height: 180,
		source: vp9test.NewPanningYCbCr,
	}
	panning720 := streamFixture{
		name:   "panning-1280x720",
		width:  1280,
		height: 720,
		source: vp9test.NewPanningYCbCr,
	}
	tiled1024 := streamFixture{
		name:   "panning-1024x64",
		width:  1024,
		height: 64,
		source: vp9test.NewPanningYCbCr,
	}
	tiledRows64 := streamFixture{
		name:   "panning-64x128",
		width:  64,
		height: 128,
		source: vp9test.NewPanningYCbCr,
	}

	type streamCase struct {
		name        string
		fixture     streamFixture
		frames      int
		opts        VP9EncoderOptions
		flags       []EncodeFlags
		extraArgs   []string
		exactPrefix int
		exactFrames []int
		strictBytes bool
		tileJobs    int
	}
	cases := []streamCase{
		{
			name:    "fixed-q-constant",
			fixture: constant64,
			frames:  6,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:    "fixed-q-constant-320",
			fixture: constant320,
			frames:  4,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "fixed-q-stepped-320",
			fixture: stepped320,
			frames:  4,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 1,
		},
		{
			name:    "fixed-q-force-key-stepped-320",
			fixture: stepped320,
			frames:  4,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags: vp9OracleRepeatAllFramesFlag(4, EncodeForceKeyFrame),
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "fixed-q-constant-640",
			fixture: constant640,
			frames:  2,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 2,
			strictBytes: true,
		},
		{
			name:    "fixed-q-constant-720p",
			fixture: constant720,
			frames:  2,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 2,
			strictBytes: true,
		},
		{
			name:    "fixed-q-threaded-stepped-720p",
			fixture: stepped720,
			frames:  2,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 1,
			tileJobs:    4,
		},
		{
			name:    "fixed-q-threaded-constant-720p",
			fixture: constant720,
			frames:  2,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 2,
			strictBytes: true,
			tileJobs:    4,
		},
		{
			name:    "fixed-q-threaded-panning-720p",
			fixture: panning720,
			frames:  2,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			// Visibility row for the non-constant 720p threaded tile path:
			// byte parity still diverges, but every frame must exercise the
			// per-tile writer pool so the remaining gap stays pinned.
			exactPrefix: 0,
			tileJobs:    4,
		},
		{
			name:    "fixed-q-rt-cpu4-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				Deadline:     DeadlineRealtime,
				CpuUsed:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--deadline=rt",
				"--cpu-used=4",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactFrames: []int{1, 2, 3},
		},
		{
			name:    "fixed-q-rt-cpu0-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				Deadline:     DeadlineRealtime,
				CpuUsed:      0,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--deadline=rt",
				"--cpu-used=0",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 0,
		},
		{
			name:    "fixed-q-rt-cpu-neg3-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				Deadline:     DeadlineRealtime,
				CpuUsed:      -3,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--deadline=rt",
				"--cpu-used=-3",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 0,
		},
		{
			name:    "fixed-q-rt-cpu5-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				Deadline:     DeadlineRealtime,
				CpuUsed:      5,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--deadline=rt",
				"--cpu-used=5",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "fixed-q-rt-cpu8-explicit-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				Deadline:     DeadlineRealtime,
				CpuUsed:      8,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--deadline=rt",
				"--cpu-used=8",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "fixed-q-good-cpu4-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				Deadline:     DeadlineGoodQuality,
				CpuUsed:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--deadline=good",
				"--cpu-used=4",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 0,
		},
		{
			name:    "fixed-q-best-cpu5-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				Deadline:     DeadlineBestQuality,
				CpuUsed:      5,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--deadline=best",
				"--cpu-used=5",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 0,
		},
		{
			name:    "error-resilient-constant-720p",
			fixture: constant720,
			frames:  2,
			opts: VP9EncoderOptions{
				ErrorResilient: true,
			},
			extraArgs:   []string{"--error-resilient=1"},
			exactPrefix: 2,
			strictBytes: true,
		},
		{
			name:    "error-resilient-constant",
			fixture: constant64,
			frames:  6,
			opts: VP9EncoderOptions{
				ErrorResilient: true,
			},
			extraArgs:   []string{"--error-resilient=1"},
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:    "max-keyframe-interval-2",
			fixture: constant64,
			frames:  6,
			opts: VP9EncoderOptions{
				MaxKeyframeInterval: 2,
			},
			extraArgs:   []string{"--kf-max-dist=2"},
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:    "fixed-keyframe-interval-2-minmax",
			fixture: constant64,
			frames:  6,
			opts: VP9EncoderOptions{
				MinKeyframeInterval: 2,
				MaxKeyframeInterval: 2,
			},
			extraArgs:   []string{"--kf-min-dist=2", "--kf-max-dist=2"},
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:    "force-key-frame1",
			fixture: stepped64,
			frames:  6,
			flags:   vp9OracleFlagAt(6, 1, EncodeForceKeyFrame),
			// The forced keyframe itself is exact; the following inter
			// frames currently expose the reference/rate-state gap.
			exactPrefix: 2,
			exactFrames: []int{4, 5},
		},
		{
			name:        "no-update-all",
			fixture:     stepped64,
			frames:      6,
			flags:       vp9OracleRepeatInterFlag(6, vp9NoUpdateRefFlags),
			exactPrefix: 5,
		},
		{
			name:        "no-reference-all",
			fixture:     stepped64,
			frames:      6,
			flags:       vp9OracleRepeatInterFlag(6, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:    "screen-content-fixed-q-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				ScreenContentMode: 1,
				MinQuantizer:      20,
				MaxQuantizer:      20,
			},
			extraArgs: []string{
				"--tune-content=screen",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "screen-content-no-reference-all",
			fixture: stepped64,
			frames:  6,
			opts: VP9EncoderOptions{
				ScreenContentMode: 1,
			},
			flags:       vp9OracleRepeatInterFlag(6, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			extraArgs:   []string{"--tune-content=screen"},
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:        "no-reference-all-stepped-320",
			fixture:     stepped320,
			frames:      4,
			flags:       vp9OracleRepeatInterFlag(4, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			exactPrefix: 0,
		},
		{
			name:        "no-reference-all-panning",
			fixture:     panning64,
			frames:      6,
			flags:       vp9OracleRepeatInterFlag(6, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			exactPrefix: 0,
		},
		{
			name:        "fixed-q-no-reference-all-panning-320",
			fixture:     panning320,
			frames:      4,
			flags:       vp9OracleRepeatInterFlag(4, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			extraArgs:   []string{"--min-q=20", "--max-q=20"},
			exactPrefix: 0,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
		},
		{
			name:        "cbr-rate-panning",
			fixture:     panning64,
			frames:      8,
			opts:        vp9OracleCBROptions(64, 64, 700),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			exactPrefix: 1,
		},
		{
			name:    "noise-sensitivity-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				NoiseSensitivity: 3,
			},
			extraArgs:   []string{"--noise-sensitivity=3"},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "noise-sensitivity-soft",
			fixture: softNoise64,
			frames:  4,
			opts: VP9EncoderOptions{
				NoiseSensitivity: 3,
			},
			extraArgs:   []string{"--noise-sensitivity=3"},
			exactPrefix: 1,
		},
		{
			name:    "vbr-rate-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "vbr-rate-constant-320",
			fixture: constant320,
			frames:  4,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "vbr-rate-constant-720p",
			fixture: constant720,
			frames:  2,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
			},
			exactPrefix: 2,
			strictBytes: true,
		},
		{
			name:    "cq-rate-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=cq",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "cq-rate-constant-320",
			fixture: constant320,
			frames:  4,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=cq",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "cq-rate-constant-720p",
			fixture: constant720,
			frames:  2,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=cq",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 2,
			strictBytes: true,
		},
		{
			name:    "q-rate-constant",
			fixture: constant64,
			frames:  4,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=q",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "q-rate-constant-320",
			fixture: constant320,
			frames:  4,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=q",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 4,
			strictBytes: true,
		},
		{
			name:    "q-rate-constant-720p",
			fixture: constant720,
			frames:  2,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=q",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 2,
			strictBytes: true,
		},
		{
			name:    "vbr-rate-panning",
			fixture: panning320,
			frames:  8,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
			},
			exactPrefix: 0,
		},
		{
			name:    "cq-rate-panning",
			fixture: panning320,
			frames:  8,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=cq",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 0,
		},
		{
			name:    "q-rate-panning",
			fixture: panning320,
			frames:  8,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=q",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 0,
		},
		{
			name:    "cbr-cyclic-aq-panning",
			fixture: panning320,
			frames:  8,
			opts: func() VP9EncoderOptions {
				opts := vp9OracleCBROptions(320, 180, 700)
				opts.AQMode = VP9AQCyclicRefresh
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--aq-mode=3"),
			exactPrefix: 0,
		},
		{
			name:    "cbr-cyclic-aq-constant",
			fixture: constant64,
			frames:  4,
			opts: func() VP9EncoderOptions {
				opts := vp9OracleCBROptions(64, 64, 700)
				opts.AQMode = VP9AQCyclicRefresh
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--aq-mode=3"),
			exactPrefix: 1,
		},
		{
			name:    "variance-aq-panning",
			fixture: panning320,
			frames:  8,
			opts: VP9EncoderOptions{
				MinQuantizer:        20,
				MaxQuantizer:        20,
				MaxKeyframeInterval: 128,
				AQMode:              VP9AQVariance,
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--aq-mode=1",
			},
			exactPrefix: 0,
		},
		{
			name:    "complexity-aq-panning",
			fixture: panning320,
			frames:  8,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
				AQMode:              VP9AQComplexity,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--aq-mode=2",
			},
			exactPrefix: 0,
		},
		{
			name:    "vbr-rate-panning-720p",
			fixture: panning720,
			frames:  3,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
			},
			exactPrefix: 0,
		},
		{
			name:    "tile-columns-from-threads",
			fixture: tiled1024,
			frames:  4,
			opts: func() VP9EncoderOptions {
				opts := VP9EncoderOptions{Threads: 4}
				return opts
			}(),
			extraArgs:   []string{"--tile-columns=2"},
			exactPrefix: 0,
		},
		{
			name:    "tile-rows-from-option",
			fixture: tiledRows64,
			frames:  4,
			opts: VP9EncoderOptions{
				Threads:      2,
				Log2TileRows: 1,
			},
			extraArgs:   []string{"--tile-rows=1"},
			exactPrefix: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = tc.fixture.source(tc.fixture.width,
					tc.fixture.height, i)
			}
			var beforeFrame func(*VP9Encoder, int)
			var afterFrame func(*VP9Encoder, int)
			if tc.tileJobs > 0 {
				beforeFrame = func(enc *VP9Encoder, frame int) {
					resetVP9OracleThreadedTileJobsForTest(enc)
				}
				afterFrame = func(enc *VP9Encoder, frame int) {
					assertVP9OracleThreadedTileWriterUsed(t, enc, frame, tc.tileJobs)
				}
			}
			govpxPackets, libvpxPackets := captureVP9StreamParityPacketsWithFrameHooks(t,
				tc.opts, sources, tc.flags, tc.extraArgs, beforeFrame, afterFrame)
			matches := 0
			firstMismatch := -1
			for i := range govpxPackets {
				if bytes.Equal(govpxPackets[i], libvpxPackets[i]) {
					matches++
					continue
				}
				if firstMismatch < 0 {
					firstMismatch = i
				}
			}
			t.Logf("VP9 stream byte-parity matrix %s/%s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, tc.fixture.name, matches, len(govpxPackets),
				firstMismatch, tc.exactPrefix)
			t.Logf("VP9 stream byte-parity rows %s:\n%s", tc.name,
				vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
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
				t.Fatalf("strict VP9 pinned byte parity %s/%s: matches=%d/%d",
					tc.name, tc.fixture.name, matches, len(govpxPackets))
			}
			panningByteCase := tc.name == "fixed-q-threaded-panning-720p" ||
				tc.name == "no-reference-all-panning" ||
				tc.name == "fixed-q-no-reference-all-panning-320"
			newModeByteCase := tc.name == "vbr-rate-panning" ||
				tc.name == "vbr-rate-constant" ||
				tc.name == "vbr-rate-constant-320" ||
				tc.name == "vbr-rate-constant-720p" ||
				tc.name == "cq-rate-panning" ||
				tc.name == "cq-rate-constant" ||
				tc.name == "cq-rate-constant-320" ||
				tc.name == "cq-rate-constant-720p" ||
				tc.name == "q-rate-panning" ||
				tc.name == "q-rate-constant" ||
				tc.name == "q-rate-constant-320" ||
				tc.name == "q-rate-constant-720p" ||
				tc.name == "cbr-cyclic-aq-panning" ||
				tc.name == "cbr-cyclic-aq-constant" ||
				tc.name == "vbr-rate-panning-720p"
			speedByteCase := tc.name == "fixed-q-rt-cpu0-constant" ||
				tc.name == "fixed-q-rt-cpu4-constant" ||
				tc.name == "fixed-q-rt-cpu5-constant" ||
				tc.name == "fixed-q-rt-cpu8-explicit-constant" ||
				tc.name == "fixed-q-rt-cpu-neg3-constant" ||
				tc.name == "fixed-q-good-cpu4-constant" ||
				tc.name == "fixed-q-best-cpu5-constant"
			denoiserByteCase := tc.name == "noise-sensitivity-constant" ||
				tc.name == "noise-sensitivity-soft"
			if vp9test.StrictEnv("GOVPX_VP9_STREAM_MATRIX_STRICT") &&
				!panningByteCase &&
				!newModeByteCase &&
				!speedByteCase &&
				!denoiserByteCase &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 stream byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
			if vp9test.StrictEnv("GOVPX_VP9_NEW_MODE_BYTE_STRICT") &&
				newModeByteCase &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 new-mode byte parity %s/%s: matches=%d/%d",
					tc.name, tc.fixture.name, matches, len(govpxPackets))
			}
			if vp9test.StrictEnv("GOVPX_VP9_SPEED_BYTE_STRICT") &&
				speedByteCase &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 speed byte parity %s/%s: matches=%d/%d",
					tc.name, tc.fixture.name, matches, len(govpxPackets))
			}
			if vp9test.StrictEnv("GOVPX_VP9_DENOISER_BYTE_STRICT") &&
				denoiserByteCase &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 denoiser byte parity %s/%s: matches=%d/%d",
					tc.name, tc.fixture.name, matches, len(govpxPackets))
			}
			if vp9test.StrictEnv("GOVPX_VP9_PANNING_BYTE_STRICT") &&
				panningByteCase &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 panning byte parity %s/%s: matches=%d/%d",
					tc.name, tc.fixture.name, matches, len(govpxPackets))
			}
		})
	}
}
