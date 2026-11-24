//go:build govpx_oracle_trace

package govpx

import (
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func vp9OracleTemporalConfig(mode TemporalLayeringMode, targetKbps int) TemporalScalabilityConfig {
	cfg := TemporalScalabilityConfig{Enabled: true, Mode: mode}
	if mode == TemporalLayeringFiveLayers {
		cfg.LayerTargetBitrateKbps = [MaxTemporalLayers]int{
			targetKbps / 7,
			(2 * targetKbps) / 7,
			(4 * targetKbps) / 7,
			(5 * targetKbps) / 7,
			targetKbps,
		}
	}
	return cfg
}

func vp9OracleTemporalArgs(t *testing.T, mode TemporalLayeringMode, targetKbps int) []string {
	t.Helper()
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		t.Fatalf("temporalLayeringPattern(%d) failed", mode)
	}
	cfg := vp9OracleTemporalConfig(mode, targetKbps)
	cfg, _, err := normalizeTemporalBitrates(cfg, pattern.Layers, targetKbps)
	if err != nil {
		t.Fatalf("normalizeTemporalBitrates(%d): %v", mode, err)
	}
	bitrates := make([]int, pattern.Layers)
	decimators := make([]int, pattern.Layers)
	for i := 0; i < pattern.Layers; i++ {
		bitrates[i] = cfg.LayerTargetBitrateKbps[i]
		decimators[i] = pattern.RateDecimator[i]
	}
	layerIDs := make([]int, pattern.Periodicity)
	for i := 0; i < pattern.Periodicity; i++ {
		layerIDs[i] = pattern.LayerID[i]
	}
	return []string{
		"--temporal-layers=" + strconv.Itoa(pattern.Layers),
		"--temporal-bitrates=" + vp9OracleIntCSV(bitrates),
		"--temporal-decimators=" + vp9OracleIntCSV(decimators),
		"--temporal-periodicity=" + strconv.Itoa(pattern.Periodicity),
		"--temporal-layer-ids=" + vp9OracleIntCSV(layerIDs),
	}
}

func vp9OracleIntCSV(values []int) string {
	var b strings.Builder
	for i, v := range values {
		if i != 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(v))
	}
	return b.String()
}

func vp9OracleTemporalPatternFlags(pattern temporalPattern, frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := range flags {
		flagIndex := i % pattern.FlagPeriodicity
		f := pattern.Flags[flagIndex]
		if i > 0 && flagIndex == 0 {
			f &^= EncodeForceKeyFrame
		}
		if i == 0 {
			f &^= vp9NoUpdateRefFlags
		}
		flags[i] = f
	}
	return flags
}

func assertVP9TemporalMetadataRows(t *testing.T, rows []vp9test.RateTraceRow, expected []expectedTemporalRow, layers int) {
	t.Helper()
	if len(rows) != len(expected) {
		t.Fatalf("temporal metadata rows = %d, want %d", len(rows), len(expected))
	}
	for i := range rows {
		if rows[i].TemporalLayerID != expected[i].layerID ||
			rows[i].TemporalLayerCount != layers ||
			rows[i].TL0PICIDX != uint8(expected[i].tl0picidx) ||
			rows[i].TemporalLayerSync != expected[i].layerSync {
			t.Fatalf("temporal metadata row %d = tid:%d layers:%d tl0:%d sync:%t, want tid:%d layers:%d tl0:%d sync:%t",
				i, rows[i].TemporalLayerID, rows[i].TemporalLayerCount,
				rows[i].TL0PICIDX, rows[i].TemporalLayerSync,
				expected[i].layerID, layers, expected[i].tl0picidx,
				expected[i].layerSync)
		}
	}
}
