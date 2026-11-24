//go:build govpx_oracle_trace

package govpx_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9OracleRuntimeResizeByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-resize byte-parity trace")
	vp9test.RequireVpxencFrameFlags(t)

	type resizeCase struct {
		name          string
		initialWidth  int
		initialHeight int
		nextWidth     int
		nextHeight    int
		resizeFrame   int
	}
	cases := []resizeCase{
		{name: "up-64x64-to-96x80", initialWidth: 64, initialHeight: 64, nextWidth: 96, nextHeight: 80, resizeFrame: 2},
		{name: "down-96x80-to-64x64", initialWidth: 96, initialHeight: 80, nextWidth: 64, nextHeight: 64, resizeFrame: 2},
		{name: "odd-65x63-to-81x79", initialWidth: 65, initialHeight: 63, nextWidth: 81, nextHeight: 79, resizeFrame: 2},
	}
	extraArgs := []string{"--cq-level=32", "--min-q=32", "--max-q=32"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const frames = 5
			sources := vp9test.NewRuntimeResizeSources(tc.initialWidth,
				tc.initialHeight, tc.nextWidth, tc.nextHeight,
				tc.resizeFrame, frames)
			opts := govpx.VP9EncoderOptions{
				Width:        tc.initialWidth,
				Height:       tc.initialHeight,
				MinQuantizer: 32,
				MaxQuantizer: 32,
			}
			before := func(enc *govpx.VP9Encoder, frame int) {
				if frame != tc.resizeFrame {
					return
				}
				if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
					Width:  tc.nextWidth,
					Height: tc.nextHeight,
				}); err != nil {
					t.Fatalf("SetRealtimeTarget resize: %v", err)
				}
			}
			govpxRows, govpxPackets := vp9oracle.CaptureVariablePacketRows(t,
				opts, sources, nil, before)
			libvpxRows, libvpxPackets := vp9oracle.CaptureLibvpxVariablePacketRows(t,
				sources, nil, nil, extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 runtime resize byte-parity trace %s: matches=%d/%d first_mismatch=%d stats=%s",
				tc.name, matches, len(govpxPackets), firstMismatch, stats)
			t.Logf("VP9 runtime resize rate rows %s:\n%s", tc.name,
				vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			t.Logf("VP9 runtime resize byte rows %s:\n%s", tc.name,
				vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_RESIZE_STRICT") &&
				(stats.HasMismatch() || matches != len(govpxPackets)) {
				t.Fatalf("strict VP9 runtime resize parity %s: matches=%d/%d stats=%s",
					tc.name, matches, len(govpxPackets), stats)
			}
		})
	}
}
