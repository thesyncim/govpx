//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

func TestVP9OracleTemporalPatternByteParityScoreboard(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 temporal byte-parity scoreboard")
	coracletest.VpxencVP9FrameFlags(t)

	const width, height, frames, targetKbps = 64, 64, 16, 700
	cases := []struct {
		name        string
		mode        TemporalLayeringMode
		exactPrefix int
	}{
		{name: "two-layer", mode: TemporalLayeringTwoLayers, exactPrefix: 1},
		{name: "three-layer-default", mode: TemporalLayeringThreeLayers, exactPrefix: 1},
		{name: "three-layer-no-inter-layer-prediction", mode: TemporalLayeringThreeLayersNoInterLayerPrediction, exactPrefix: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pattern, ok := temporalLayeringPattern(tc.mode)
			if !ok {
				t.Fatalf("temporalLayeringPattern(%d) failed", tc.mode)
			}
			opts := vp9OracleCBROptions(width, height, targetKbps)
			opts.TemporalScalability = vp9OracleTemporalConfig(tc.mode,
				targetKbps)
			sources := newVP9OracleTransitionSources(width, height, frames)
			flags := vp9OracleTemporalPatternFlags(pattern, frames)
			extraArgs := append(vp9OracleCBRArgs(targetKbps, 600, 400, 500, 0),
				vp9OracleTemporalArgs(t, tc.mode, targetKbps)...)
			govpxPackets, libvpxPackets := captureVP9StreamParityPackets(t,
				opts, sources, flags, extraArgs)
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 temporal byte-parity scoreboard %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			t.Logf("VP9 temporal byte-parity rows %s:\n%s", tc.name,
				vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				assertVP9PacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			if os.Getenv("GOVPX_VP9_TEMPORAL_BYTE_STRICT") == "1" &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 temporal byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleEncoderStreamByteParityLookaheadFlushBursts(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 lookahead flush byte-parity scoreboard")
	coracletest.VpxencVP9(t)

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
			sources := makeVP9SteppedOracleSources(width, height, tc.frames)
			govpxPackets := captureVP9LookaheadPacketsWithFlushesForOracleTest(t,
				VP9EncoderOptions{LookaheadFrames: tc.lag}, sources, tc.flushAfter)
			libvpxPackets := captureVP9VpxencPacketsForOracleTest(t, sources,
				fmt.Sprintf("--lag-in-frames=%d", tc.lag), "--auto-alt-ref=0")
			if len(govpxPackets) != len(libvpxPackets) {
				t.Fatalf("VP9 lookahead flush packets: govpx=%d libvpx=%d",
					len(govpxPackets), len(libvpxPackets))
			}
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 lookahead flush byte-parity scoreboard %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
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

func TestVP9OracleEncoderStreamByteParityAutoAltRefVisibilityScoreboard(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 auto-alt-ref visibility scoreboard")
	coracletest.VpxencVP9FrameFlags(t)

	const width, height, frames, lag = 64, 64, 16, 4
	sources := makeVP9SteppedOracleSources(width, height, frames)
	govpxRows, govpxPackets := captureGovpxVP9AutoAltRefPacketRowsForOracleTest(t,
		VP9EncoderOptions{
			Deadline:           DeadlineRealtime,
			CpuUsed:            4,
			RateControlModeSet: true,
			RateControlMode:    RateControlVBR,
			TargetBitrateKbps:  300,
			LookaheadFrames:    lag,
			AutoAltRef:         true,
			ARNRMaxFrames:      7,
			ARNRStrength:       3,
			ARNRType:           3,
		}, sources)
	libvpxRows, libvpxPackets := captureLibvpxVP9AutoAltRefPacketRowsForOracleTest(t,
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
	govpxHidden := countVP9HiddenRows(govpxRows)
	libvpxHidden := countVP9HiddenRows(libvpxRows)
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
	t.Logf("VP9 auto-alt-ref visibility scoreboard: govpx_packets=%d libvpx_packets=%d compare=%d matches=%d first_mismatch=%d govpx_hidden=%d libvpx_hidden=%d govpx_altref_refresh=%d libvpx_altref_refresh=%d",
		len(govpxPackets), len(libvpxPackets), limit, matches, firstMismatch,
		govpxHidden, libvpxHidden, countVP9AltRefRefreshRows(govpxRows),
		countVP9AltRefRefreshRows(libvpxRows))
	t.Logf("VP9 auto-alt-ref visibility rows:\n%s",
		formatVP9AutoAltRefVisibilityRows(govpxRows, libvpxRows))
	if govpxHidden == 0 {
		t.Fatal("govpx emitted no hidden auto-alt-ref packet")
	}
	if libvpxHidden == 0 {
		t.Log("libvpx emitted no hidden auto-alt-ref packet for this one-pass scoreboard fixture")
	}
	if os.Getenv("GOVPX_VP9_AUTO_ALT_REF_STRICT") == "1" &&
		(len(govpxPackets) != len(libvpxPackets) || matches != len(govpxPackets)) {
		t.Fatalf("strict VP9 auto-alt-ref byte parity: matches=%d/%d libvpx_packets=%d",
			matches, len(govpxPackets), len(libvpxPackets))
	}
}

func TestVP9OracleEncoderStreamByteParityAutoAltRefARNRMatrix(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 auto-alt-ref ARNR byte-parity matrix")
	coracletest.VpxencVP9FrameFlags(t)

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
			govpxRows, govpxPackets := captureGovpxVP9AutoAltRefPacketRowsForOracleTest(t,
				VP9EncoderOptions{
					Deadline:           DeadlineRealtime,
					CpuUsed:            4,
					RateControlModeSet: true,
					RateControlMode:    RateControlVBR,
					TargetBitrateKbps:  tc.targetKbs,
					LookaheadFrames:    tc.lag,
					AutoAltRef:         true,
					ARNRMaxFrames:      7,
					ARNRStrength:       3,
					ARNRType:           tc.arnrType,
				}, sources)
			libvpxRows, libvpxPackets := captureLibvpxVP9AutoAltRefPacketRowsForOracleTest(t,
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
				firstMismatch, countVP9HiddenRows(govpxRows),
				countVP9HiddenRows(libvpxRows))
			t.Logf("VP9 auto-alt-ref ARNR rows %s:\n%s", tc.name,
				formatVP9AutoAltRefVisibilityRows(govpxRows, libvpxRows))
			if countVP9HiddenRows(govpxRows) == 0 {
				t.Fatalf("govpx emitted no hidden auto-alt-ref packet for %s",
					tc.name)
			}
			if os.Getenv("GOVPX_VP9_AUTO_ALT_REF_ARNR_BYTE_STRICT") == "1" &&
				(len(govpxPackets) != len(libvpxPackets) ||
					matches != len(govpxPackets)) {
				t.Fatalf("strict VP9 auto-alt-ref ARNR byte parity %s: matches=%d/%d libvpx_packets=%d",
					tc.name, matches, len(govpxPackets), len(libvpxPackets))
			}
		})
	}
}
