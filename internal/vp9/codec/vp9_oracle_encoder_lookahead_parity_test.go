//go:build govpx_oracle_trace

package codec_test

import (
	"bytes"
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9OracleInvisibleKeyFrameByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 invisible-frame byte-parity trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	sources := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 96, 128, 128),
	}
	flags := []govpx.EncodeFlags{govpx.EncodeInvisibleFrame}
	govpxRows, govpxPackets := vp9oracle.CaptureVariablePacketRows(t,
		govpx.VP9EncoderOptions{Width: width, Height: height, MinQuantizer: 32, MaxQuantizer: 32},
		sources, flags, nil)
	libvpxRows, libvpxPackets := vp9oracle.CaptureLibvpxVariablePacketRows(t,
		sources, flags, []bool{true},
		[]string{"--cq-level=32", "--min-q=32", "--max-q=32"})
	stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
	matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
		libvpxPackets)
	t.Logf("VP9 invisible keyframe byte-parity trace: matches=%d/%d first_mismatch=%d stats=%s",
		matches, len(govpxPackets), firstMismatch, stats)
	t.Logf("VP9 invisible keyframe rate rows:\n%s",
		vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
	t.Logf("VP9 invisible keyframe byte rows:\n%s",
		vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
	if vp9test.StrictEnv("GOVPX_VP9_INVISIBLE_KEY_STRICT") &&
		(stats.HasMismatch() || matches != len(govpxPackets)) {
		t.Fatalf("strict VP9 invisible keyframe parity: matches=%d/%d stats=%s",
			matches, len(govpxPackets), stats)
	}
}

func TestVP9OracleInvisibleKeyFrameStrictByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 invisible-frame byte-parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	sources := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 96, 128, 128),
	}
	flags := []govpx.EncodeFlags{govpx.EncodeInvisibleFrame}
	_, govpxPackets := vp9oracle.CaptureVariablePacketRows(t,
		govpx.VP9EncoderOptions{Width: width, Height: height, MinQuantizer: 32, MaxQuantizer: 32},
		sources, flags, nil)
	_, libvpxPackets := vp9oracle.CaptureLibvpxVariablePacketRows(t,
		sources, flags, []bool{true},
		[]string{"--cq-level=32", "--min-q=32", "--max-q=32"})
	if len(govpxPackets) != len(libvpxPackets) {
		t.Fatalf("VP9 invisible keyframe packet count: govpx=%d libvpx=%d",
			len(govpxPackets), len(libvpxPackets))
	}
	for frame := range govpxPackets {
		vp9test.AssertPacketByteParity(t,
			fmt.Sprintf("VP9 invisible keyframe frame %d", frame),
			govpxPackets[frame], libvpxPackets[frame])
	}
}

func TestVP9OracleEncoderStreamByteParityLookaheadFlushBursts(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 lookahead flush byte-parity trace")
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	type flushCase struct {
		name        string
		lag         int
		frames      int
		flushAfter  []int
		exactPrefix int
	}
	cases := []flushCase{
		{
			name:        "lag1-mid-flush",
			lag:         1,
			frames:      5,
			flushAfter:  []int{2},
			exactPrefix: 5,
		},
		{
			name:        "lag2-two-bursts",
			lag:         2,
			frames:      6,
			flushAfter:  []int{2, 4},
			exactPrefix: 6,
		},
		{
			name:        "lag4-early-drain",
			lag:         4,
			frames:      8,
			flushAfter:  []int{3},
			exactPrefix: 8,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9test.NewSteppedSources(width, height, tc.frames)
			govpxPackets := vp9oracle.CaptureLookaheadPacketsWithFlushes(t,
				govpx.VP9EncoderOptions{LookaheadFrames: tc.lag}, sources, tc.flushAfter)
			libvpxPackets := vp9test.VpxencPackets(t, sources,
				fmt.Sprintf("--lag-in-frames=%d", tc.lag), "--auto-alt-ref=0")
			if len(govpxPackets) != len(libvpxPackets) {
				t.Fatalf("VP9 lookahead flush packets: govpx=%d libvpx=%d",
					len(govpxPackets), len(libvpxPackets))
			}
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 lookahead flush byte-parity trace %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			t.Logf("VP9 lookahead flush byte-parity rows %s:\n%s", tc.name,
				vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					t.Fatalf("frame %d should be inside exact prefix for %s",
						frame, tc.name)
				}
			}
			if matches != len(govpxPackets) {
				t.Fatalf("strict VP9 lookahead flush byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleEncoderStreamByteParityAutoAltRefVisibility(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 auto-alt-ref visibility trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames, lag = 64, 64, 16, 4
	sources := vp9test.NewSteppedSources(width, height, frames)
	govpxRows, govpxPackets := vp9oracle.CaptureGovpxAutoAltRefPacketRows(t,
		govpx.VP9EncoderOptions{
			Deadline:           govpx.DeadlineRealtime,
			CpuUsed:            4,
			RateControlModeSet: true,
			RateControlMode:    govpx.RateControlVBR,
			TargetBitrateKbps:  300,
			LookaheadFrames:    lag,
			AutoAltRef:         true,
			ARNRMaxFrames:      7,
			ARNRStrength:       3,
			ARNRType:           3,
		}, sources)
	libvpxRows, libvpxPackets := vp9oracle.CaptureLibvpxAutoAltRefPacketRows(t,
		sources,
		"--deadline=rt",
		"--cpu-used=4",
		"--end-usage=vbr",
		"--target-bitrate=300",
		fmt.Sprintf("--lag-in-frames=%d", lag),
		"--auto-alt-ref=1",
		"--arnr-maxframes=7",
		"--arnr-strength=3",
		"--arnr-type=3")
	govpxHidden := vp9test.CountHiddenRows(govpxRows)
	libvpxHidden := vp9test.CountHiddenRows(libvpxRows)
	limit := len(govpxPackets)
	if len(libvpxPackets) < limit {
		limit = len(libvpxPackets)
	}
	matches := 0
	firstMismatch := -1
	for i := 0; i < limit; i++ {
		if bytes.Equal(govpxPackets[i], libvpxPackets[i]) {
			matches++
			continue
		}
		if firstMismatch < 0 {
			firstMismatch = i
		}
	}
	t.Logf("VP9 auto-alt-ref visibility trace: govpx_packets=%d libvpx_packets=%d compare=%d matches=%d first_mismatch=%d govpx_hidden=%d libvpx_hidden=%d govpx_altref_refresh=%d libvpx_altref_refresh=%d",
		len(govpxPackets), len(libvpxPackets), limit, matches, firstMismatch,
		govpxHidden, libvpxHidden,
		vp9test.CountAltRefRefreshRows(govpxRows, vp9oracle.AltRefRefreshMask),
		vp9test.CountAltRefRefreshRows(libvpxRows, vp9oracle.AltRefRefreshMask))
	t.Logf("VP9 auto-alt-ref visibility rows:\n%s",
		vp9test.FormatAutoAltRefVisibilityRows(govpxRows, libvpxRows))
	if govpxHidden == 0 {
		t.Fatal("govpx emitted no hidden auto-alt-ref packet")
	}
	if libvpxHidden == 0 {
		t.Log("libvpx emitted no hidden auto-alt-ref packet for this one-pass trace fixture")
	}
	if vp9test.StrictEnv("GOVPX_VP9_AUTO_ALT_REF_STRICT") &&
		(len(govpxPackets) != len(libvpxPackets) || matches != len(govpxPackets)) {
		t.Fatalf("strict VP9 auto-alt-ref byte parity: matches=%d/%d libvpx_packets=%d",
			matches, len(govpxPackets), len(libvpxPackets))
	}
}

func TestVP9OracleEncoderStreamByteParityAutoAltRefARNRMatrix(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 auto-alt-ref ARNR byte-parity matrix")
	vp9test.RequireVpxencFrameFlags(t)

	type autoAltRefCase struct {
		name      string
		width     int
		height    int
		frames    int
		lag       int
		targetKbs int
		source    func(width, height, frame int) *image.YCbCr
		arnrType  int
	}
	cases := []autoAltRefCase{
		{
			name:      "stepped-64x64-centered",
			width:     64,
			height:    64,
			frames:    16,
			lag:       4,
			targetKbs: 300,
			source: func(width, height, frame int) *image.YCbCr {
				return vp9test.NewYCbCr(width, height,
					uint8(96+frame*8), 128, 128)
			},
			arnrType: 3,
		},
		{
			name:      "panning-320x180-backward",
			width:     320,
			height:    180,
			frames:    12,
			lag:       4,
			targetKbs: 900,
			source:    vp9test.NewPanningYCbCr,
			arnrType:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = tc.source(tc.width, tc.height, i)
			}
			govpxRows, govpxPackets := vp9oracle.CaptureGovpxAutoAltRefPacketRows(t,
				govpx.VP9EncoderOptions{
					Deadline:           govpx.DeadlineRealtime,
					CpuUsed:            4,
					RateControlModeSet: true,
					RateControlMode:    govpx.RateControlVBR,
					TargetBitrateKbps:  tc.targetKbs,
					LookaheadFrames:    tc.lag,
					AutoAltRef:         true,
					ARNRMaxFrames:      7,
					ARNRStrength:       3,
					ARNRType:           tc.arnrType,
				}, sources)
			libvpxRows, libvpxPackets := vp9oracle.CaptureLibvpxAutoAltRefPacketRows(t,
				sources,
				"--deadline=rt",
				"--cpu-used=4",
				"--end-usage=vbr",
				"--target-bitrate="+fmt.Sprintf("%d", tc.targetKbs),
				fmt.Sprintf("--lag-in-frames=%d", tc.lag),
				"--auto-alt-ref=1",
				"--arnr-maxframes=7",
				"--arnr-strength=3",
				fmt.Sprintf("--arnr-type=%d", tc.arnrType))
			limit := len(govpxPackets)
			if len(libvpxPackets) < limit {
				limit = len(libvpxPackets)
			}
			matches := 0
			firstMismatch := -1
			for i := 0; i < limit; i++ {
				if bytes.Equal(govpxPackets[i], libvpxPackets[i]) {
					matches++
					continue
				}
				if firstMismatch < 0 {
					firstMismatch = i
				}
			}
			t.Logf("VP9 auto-alt-ref ARNR byte-parity matrix %s: govpx_packets=%d libvpx_packets=%d compare=%d matches=%d first_mismatch=%d govpx_hidden=%d libvpx_hidden=%d",
				tc.name, len(govpxPackets), len(libvpxPackets), limit, matches,
				firstMismatch, vp9test.CountHiddenRows(govpxRows),
				vp9test.CountHiddenRows(libvpxRows))
			t.Logf("VP9 auto-alt-ref ARNR rows %s:\n%s", tc.name,
				vp9test.FormatAutoAltRefVisibilityRows(govpxRows, libvpxRows))
			if vp9test.CountHiddenRows(govpxRows) == 0 {
				t.Fatalf("govpx emitted no hidden auto-alt-ref packet for %s",
					tc.name)
			}
			if vp9test.StrictEnv("GOVPX_VP9_AUTO_ALT_REF_ARNR_BYTE_STRICT") &&
				(len(govpxPackets) != len(libvpxPackets) ||
					matches != len(govpxPackets)) {
				t.Fatalf("strict VP9 auto-alt-ref ARNR byte parity %s: matches=%d/%d libvpx_packets=%d",
					tc.name, matches, len(govpxPackets), len(libvpxPackets))
			}
		})
	}
}

// TestVP9EncoderNonrdMlPartitionColorSensitivityByteParity pins the
// frames-0..9 byte-exact prefix of the {0,0,0,0,0} long-fixture gap seed
// (CBR 300kbps kf=999 realtime cpu8 on the panning source) that the
// ML_BASED_PARTITION color_sensitivity fix opened.
//
// libvpx selects ML_BASED_PARTITION at cpu_used=8 for w*h <= 352*288
// (vp9_speed_features.c:762-763,825-826). That dispatch
// (vp9_encodeframe.c:5313-5321) runs get_estimated_pred + nonrd_pick_partition
// and never calls choose_partitioning, so x->color_sensitivity stays at the
// per-SB reset [0,0] (vp9_encodeframe.c:5245-5246) and x->variance_low stays
// all-zero (only touched inside choose_partitioning @ vp9_encodeframe.c:1336).
// govpx previously ran its choose_partitioning prepass (chroma_check @
// vp9_encodeframe.c:1165-1199) for every non-VAR_BASED path, spuriously
// flagging color_sensitivity[V] and adding a UV model-RD term
// (vp9_pickmode.c:2388-2402) to nonrd inter candidates — flipping inter blocks
// to intra at frame 4. This guards that regression. The seed itself stays
// deferred on its frame-10 golden-frame-refresh (rate-control) gap.
func TestVP9EncoderNonrdMlPartitionColorSensitivityByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height, frames = 64, 64, 10
	sources := vp9test.NewPanningSources(width, height, frames)
	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   300,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 999,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
	}
	args := []string{
		"--end-usage=cbr",
		"--target-bitrate=300",
		"--cpu-used=8",
		"--kf-min-dist=0",
		"--kf-max-dist=999",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
	}

	govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, opts, sources, nil)
	libvpxFrames := vp9test.VpxencPackets(t, sources, args...)
	if len(govpxFrames) < frames || len(libvpxFrames) < frames {
		t.Fatalf("encoded frame counts govpx=%d libvpx=%d, want >= %d",
			len(govpxFrames), len(libvpxFrames), frames)
	}
	for i := 0; i < frames; i++ {
		g, l := govpxFrames[i], libvpxFrames[i]
		if len(g) != len(l) {
			t.Fatalf("frame %d length govpx=%d libvpx=%d", i, len(g), len(l))
		}
		for k := range g {
			if g[k] != l[k] {
				t.Fatalf("frame %d byte %d differs: govpx=%#x libvpx=%#x",
					i, k, g[k], l[k])
			}
		}
	}
}
