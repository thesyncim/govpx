//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9OracleTemporalPatternByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 temporal byte-parity trace")
	vp9test.RequireVpxencFrameFlags(t)

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
			matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 temporal byte-parity trace %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			t.Logf("VP9 temporal byte-parity rows %s:\n%s", tc.name,
				vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				vp9test.AssertPacketByteParity(t,
					fmt.Sprintf("%s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
			if vp9test.StrictEnv("GOVPX_VP9_TEMPORAL_BYTE_STRICT") &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 temporal byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}
