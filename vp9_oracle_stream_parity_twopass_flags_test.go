//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9OracleTwoPassStreamByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 two-pass byte-parity trace")
	vp9test.RequireVpxenc(t)

	const width, height, frames = 64, 64, 6
	sources := make([]*image.YCbCr, frames)
	statsEnc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(firstpass): %v", err)
	}
	stats := make([]VP9FirstPassFrameStats, frames)
	for frame := range frames {
		src := vp9test.NewPanningYCbCr(width, height, frame)
		sources[frame] = src
		stats[frame], err = statsEnc.CollectFirstPassStats(src,
			uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats[%d]: %v", frame, err)
		}
	}

	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  700,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats:       FinalizeVP9FirstPassStats(stats),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(secondpass): %v", err)
	}
	dst := make([]byte, 1<<20)
	govpxPackets := make([][]byte, frames)
	for frame, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		if result.TwoPassFrameTargetBits <= 0 {
			t.Fatalf("frame %d two-pass target = %d, want positive",
				frame, result.TwoPassFrameTargetBits)
		}
		govpxPackets[frame] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencTwoPassPackets(t, sources,
		"--target-bitrate=700",
		"--min-q=4",
		"--max-q=56",
		"--disable-warning-prompt")
	matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
		libvpxPackets)
	t.Logf("VP9 two-pass byte-parity trace: matches=%d/%d first_mismatch=%d",
		matches, frames, firstMismatch)
	t.Logf("VP9 two-pass byte-parity rows:\n%s",
		vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
	if vp9test.StrictEnv("GOVPX_VP9_TWOPASS_BYTE_STRICT") &&
		matches != frames {
		t.Fatalf("strict VP9 two-pass byte parity: matches=%d/%d",
			matches, frames)
	}
}

func TestVP9OracleTwoPassConstantByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 two-pass constant byte-parity trace")
	vp9test.RequireVpxenc(t)

	const width, height, frames = 64, 64, 4
	sources := make([]*image.YCbCr, frames)
	statsEnc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(firstpass): %v", err)
	}
	stats := make([]VP9FirstPassFrameStats, frames)
	for frame := range frames {
		src := vp9test.NewYCbCr(width, height, 128, 128, 128)
		sources[frame] = src
		stats[frame], err = statsEnc.CollectFirstPassStats(src,
			uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats[%d]: %v", frame, err)
		}
	}

	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  700,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats:       FinalizeVP9FirstPassStats(stats),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(secondpass): %v", err)
	}
	dst := make([]byte, 1<<20)
	govpxPackets := make([][]byte, frames)
	for frame, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		if result.TwoPassFrameTargetBits <= 0 {
			t.Fatalf("frame %d two-pass target = %d, want positive",
				frame, result.TwoPassFrameTargetBits)
		}
		govpxPackets[frame] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencTwoPassPackets(t, sources,
		"--target-bitrate=700",
		"--min-q=4",
		"--max-q=56",
		"--disable-warning-prompt")
	matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
		libvpxPackets)
	t.Logf("VP9 two-pass constant byte-parity trace: matches=%d/%d first_mismatch=%d",
		matches, frames, firstMismatch)
	t.Logf("VP9 two-pass constant byte-parity rows:\n%s",
		vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
	if vp9test.StrictEnv("GOVPX_VP9_TWOPASS_CONSTANT_BYTE_STRICT") &&
		matches != frames {
		t.Fatalf("strict VP9 two-pass constant byte parity: matches=%d/%d",
			matches, frames)
	}
}

func TestVP9OracleEncoderStreamByteParityFrameFlagsMatrix(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 frame-flag byte-parity matrix")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 6
	type flagCase struct {
		name        string
		flags       []EncodeFlags
		exactPrefix int
		exactFrames []int
		strictBytes bool
	}
	cases := []flagCase{
		{
			name:        "force-key-frame1",
			flags:       vp9OracleFlagAt(frames, 1, EncodeForceKeyFrame),
			exactPrefix: 2,
			exactFrames: []int{5},
		},
		{
			name:        "force-key-frame3",
			flags:       vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame),
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:        "repeat-no-update-last",
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateLast),
			exactPrefix: 2,
		},
		{
			name:        "repeat-no-update-golden",
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateGolden),
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:        "repeat-no-update-altref",
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateAltRef),
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:        "repeat-no-update-all",
			flags:       vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
			exactPrefix: 5,
		},
		{
			name: "repeat-no-reference-golden-altref",
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name: "repeat-no-reference-all",
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:        "repeat-no-update-entropy",
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateEntropy),
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:        "force-ref-refresh-transitions",
			flags:       vp9OracleRefRefreshTransitions(frames),
			exactPrefix: 3,
		},
		{
			name:        "alternating-reference-controls",
			flags:       vp9OracleAlternatingReferenceControls(frames),
			exactPrefix: 6,
			strictBytes: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9test.NewSteppedSources(width, height, frames)
			govpxPackets, libvpxPackets := captureVP9StreamParityPackets(t,
				VP9EncoderOptions{}, sources, tc.flags, nil)
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 frame-flag byte-parity matrix %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			t.Logf("VP9 frame-flag byte-parity rows %s:\n%s", tc.name,
				vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					t.Fatalf("frame %d should be inside exact prefix for %s",
						frame, tc.name)
				}
			}
			for _, frame := range tc.exactFrames {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					t.Fatalf("frame %d should be exact for %s",
						frame, tc.name)
				}
			}
			if vp9test.StrictEnv("GOVPX_VP9_FLAG_BYTE_MATRIX_STRICT") &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 frame-flag byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
			if tc.strictBytes && matches != len(govpxPackets) {
				t.Fatalf("strict VP9 pinned frame-flag byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleEncoderStreamByteParityControlCrossMatrix(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 control-cross byte-parity matrix")
	vp9test.RequireVpxencFrameFlags(t)

	const frames = 6
	type crossCase struct {
		name        string
		width       int
		height      int
		opts        VP9EncoderOptions
		flags       []EncodeFlags
		extraArgs   []string
		exactPrefix int
		strictBytes bool
	}
	cases := []crossCase{
		{
			name:   "fixed-q-no-update-all",
			width:  64,
			height: 64,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags: vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
			extraArgs: []string{
				"--min-q=20",
				"--max-q=20",
			},
			exactPrefix: 1,
		},
		{
			name:        "cbr-force-key-frame3",
			width:       64,
			height:      64,
			opts:        vp9OracleCBROptions(64, 64, 700),
			flags:       vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			exactPrefix: 4,
		},
		{
			name:   "error-resilient-no-update-entropy",
			width:  64,
			height: 64,
			opts: VP9EncoderOptions{
				ErrorResilient: true,
			},
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateEntropy),
			extraArgs:   []string{"--error-resilient=1"},
			exactPrefix: 6,
			strictBytes: true,
		},
		{
			name:   "cbr-no-reference-all",
			width:  64,
			height: 64,
			opts:   vp9OracleCBROptions(64, 64, 700),
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			exactPrefix: 3,
		},
		{
			name:   "tile-columns-ref-refresh",
			width:  1024,
			height: 64,
			opts: VP9EncoderOptions{
				Threads: 4,
			},
			flags:       vp9OracleRefRefreshTransitions(frames),
			extraArgs:   []string{"--tile-columns=2"},
			exactPrefix: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9test.NewSteppedSources(tc.width, tc.height, frames)
			govpxPackets, libvpxPackets := captureVP9StreamParityPackets(t,
				tc.opts, sources, tc.flags, tc.extraArgs)
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 control-cross byte-parity matrix %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			t.Logf("VP9 control-cross byte-parity rows %s:\n%s", tc.name,
				vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					t.Fatalf("frame %d should be inside exact prefix for %s",
						frame, tc.name)
				}
			}
			if vp9test.StrictEnv("GOVPX_VP9_CONTROL_CROSS_BYTE_STRICT") &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 control-cross byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
			if tc.strictBytes && matches != len(govpxPackets) {
				t.Fatalf("strict VP9 pinned control-cross byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}
