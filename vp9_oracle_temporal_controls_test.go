//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9OracleTemporalControlTransitionsMatchLibvpx(t *testing.T) {
	const width, height, frames = 64, 64, 9
	opts := vp9OracleCBROptions(width, height, 600)
	sources := newVP9OracleTransitionSources(width, height, frames)
	rows := captureVP9RateTraceRowsWithHooks(t, opts, sources, nil,
		func(enc *VP9Encoder, frame int) {
			switch frame {
			case 2:
				if err := enc.SetTemporalScalability(TemporalScalabilityConfig{
					Enabled: true,
					Mode:    TemporalLayeringTwoLayers,
				}); err != nil {
					t.Fatalf("SetTemporalScalability at frame %d: %v", frame, err)
				}
			case 6:
				if err := enc.SetTemporalLayerID(1); err != nil {
					t.Fatalf("SetTemporalLayerID at frame %d: %v", frame, err)
				}
			case 7:
				if err := enc.SetTemporalScalability(TemporalScalabilityConfig{}); err != nil {
					t.Fatalf("disable temporal at frame %d: %v", frame, err)
				}
			}
		})

	if len(rows) != frames {
		t.Fatalf("temporal control rows = %d, want %d", len(rows), frames)
	}
	seenLayer1 := false
	for frame := 2; frame <= 6; frame++ {
		if rows[frame].TemporalLayerCount != 2 {
			t.Fatalf("frame %d temporal layer count = %d, want 2",
				frame, rows[frame].TemporalLayerCount)
		}
		if rows[frame].TemporalLayerID == 1 {
			seenLayer1 = true
		}
	}
	if !seenLayer1 {
		t.Fatal("temporal control transition did not emit a layer-1 row")
	}
	if rows[7].TemporalLayerCount != 1 || rows[8].TemporalLayerCount != 1 {
		t.Fatalf("temporal disable rows = %d/%d, want 1/1",
			rows[7].TemporalLayerCount, rows[8].TemporalLayerCount)
	}
	t.Logf("VP9 temporal control transition rows:\n%s",
		vp9test.FormatSingleRateTraceRows(rows))
}

func TestVP9OracleTemporalFlagPatternsMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 temporal flag patterns")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 12
	cases := []struct {
		name string
		mode TemporalLayeringMode
	}{
		{name: "two-layer", mode: TemporalLayeringTwoLayers},
		{name: "three-layer", mode: TemporalLayeringThreeLayers},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pattern, ok := temporalLayeringPattern(tc.mode)
			if !ok {
				t.Fatalf("temporalLayeringPattern(%d) failed", tc.mode)
			}
			opts := vp9OracleCBROptions(width, height, 700)
			opts.TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    tc.mode,
			}
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateTraceRows(t, opts, sources, nil)
			flags := vp9OracleTemporalPatternFlags(pattern, frames)
			expected := buildExpectedTemporalPattern(pattern, frames)
			extraArgs := append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				vp9OracleTemporalArgs(t, tc.mode, 700)...)
			libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
				sources, flags, extraArgs)
			assertVP9TemporalMetadataRows(t, libvpxRows, expected,
				pattern.Layers)

			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
			t.Logf("VP9 temporal flag patterns %s: %s", tc.name, stats)
			t.Logf("VP9 temporal flag-pattern rows %s:\n%s",
				tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			if vp9test.StrictEnv("GOVPX_VP9_TEMPORAL_PATTERN_STRICT") &&
				stats.HasMismatch() {
				t.Fatalf("strict VP9 temporal flag-pattern mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleTemporalPatternMatrixMatchesLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 temporal pattern matrix")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames, targetKbps = 64, 64, 16, 700
	cases := []struct {
		name string
		mode TemporalLayeringMode
	}{
		{name: "one-layer", mode: TemporalLayeringOneLayer},
		{name: "two-layer", mode: TemporalLayeringTwoLayers},
		{name: "two-layer-three-frame", mode: TemporalLayeringTwoLayersThreeFrame},
		{name: "three-layer-six-frame", mode: TemporalLayeringThreeLayersSixFrame},
		{name: "three-layer-no-inter-layer-prediction", mode: TemporalLayeringThreeLayersNoInterLayerPrediction},
		{name: "three-layer-layer-one-prediction", mode: TemporalLayeringThreeLayersLayerOnePrediction},
		{name: "three-layer-default", mode: TemporalLayeringThreeLayers},
		{name: "five-layer", mode: TemporalLayeringFiveLayers},
		{name: "two-layer-sync", mode: TemporalLayeringTwoLayersWithSync},
		{name: "three-layer-sync", mode: TemporalLayeringThreeLayersWithSync},
		{name: "three-layer-altref-sync", mode: TemporalLayeringThreeLayersAltRefWithSync},
		{name: "three-layer-one-reference", mode: TemporalLayeringThreeLayersOneReference},
		{name: "three-layer-no-sync", mode: TemporalLayeringThreeLayersNoSync},
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
			govpxRows := captureVP9RateTraceRows(t, opts, sources, nil)
			flags := vp9OracleTemporalPatternFlags(pattern, frames)
			expected := buildExpectedTemporalPattern(pattern, frames)
			extraArgs := append(vp9OracleCBRArgs(targetKbps, 600, 400, 500, 0),
				vp9OracleTemporalArgs(t, tc.mode, targetKbps)...)
			libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
				sources, flags, extraArgs)
			assertVP9TemporalMetadataRows(t, libvpxRows, expected,
				pattern.Layers)

			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
			t.Logf("VP9 temporal pattern matrix %s: %s",
				tc.name, stats)
			t.Logf("VP9 temporal pattern matrix rows %s:\n%s",
				tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			if vp9test.StrictEnv("GOVPX_VP9_TEMPORAL_MATRIX_STRICT") &&
				stats.HasMismatch() {
				t.Fatalf("strict VP9 temporal pattern matrix mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}
