//go:build govpx_oracle_trace

package govpx

// Loop-filter header oracle parity report.
//
// TestVP8OracleLoopFilterHeaderMatchRate captures the per-frame loop-filter header
// from both govpx and the libvpx oracle on a panning corpus across a deadline x
// CPU matrix and reports per-field match rates against a baseline pinned in
// testdata/loop_filter_match_rate_baseline.json. Each fixture exercises a
// different speed-feature configuration so divergences in any one path
// (e.g. high-cpu simple-filter, RD/VBR, fast/CBR) surface independently.
//
// The harness skips unless GOVPX_WITH_ORACLE=1. To bootstrap or refresh the
// baseline, set GOVPX_UPDATE_BASELINES=1.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

const loopFilterMatchRateBaselinePath = "testdata/loop_filter_match_rate_baseline.json"

// loopFilterFrameRow is the slice of trace fields the loop-filter parity report
// inspects. Captured separately on each side so missing fields can be
// interpreted as zeros (per the doc).
type loopFilterFrameRow struct {
	FrameIndex     int64
	LoopFilter     int
	SharpnessLevel int
	RefLFDeltas    [4]int8
	ModeLFDeltas   [4]int8
	EnabledSet     bool
	Enabled        bool
	UpdateSet      bool
	Update         bool
}

// FixtureLFReport is the per-fixture parity summary. The percentages are
// expressed in [0, 100] and rounded to two decimals when emitted to the log
// table; the on-disk baseline keeps the raw float so 100% / 99.99% don't
// flap a regression.
type FixtureLFReport struct {
	Name               string
	FrameTotal         int
	LevelMatchPct      float64
	SharpnessMatchPct  float64
	RefDeltasMatchPct  float64
	ModeDeltasMatchPct float64
	EnabledMatchPct    float64
	UpdateMatchPct     float64
}

type loopFilterBaselineEntry struct {
	LevelMatchPct      float64 `json:"level_match_pct"`
	SharpnessMatchPct  float64 `json:"sharpness_match_pct"`
	RefDeltasMatchPct  float64 `json:"ref_deltas_match_pct"`
	ModeDeltasMatchPct float64 `json:"mode_deltas_match_pct"`
	EnabledMatchPct    float64 `json:"enabled_match_pct"`
	UpdateMatchPct     float64 `json:"update_match_pct"`
}

type loopFilterBaselineFile struct {
	Fixtures map[string]loopFilterBaselineEntry `json:"fixtures"`
}

func TestVP8OracleLoopFilterHeaderMatchRate(t *testing.T) {
	vp8test.RequireOracle(t, "loop-filter header oracle parity report")
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 6
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	type fixture struct {
		name      string
		opts      EncoderOptions
		extraArgs []string
	}
	baseOpts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
	}
	fixtures := []fixture{
		{
			name: "realtime-cpu0-cbr",
			opts: func() EncoderOptions {
				o := baseOpts
				o.RateControlMode = RateControlCBR
				o.Deadline = DeadlineRealtime
				o.CpuUsed = 0
				return o
			}(),
			extraArgs: []string{"--end-usage=cbr"},
		},
		{
			name: "realtime-cpu8-cbr",
			opts: func() EncoderOptions {
				o := baseOpts
				o.RateControlMode = RateControlCBR
				o.Deadline = DeadlineRealtime
				o.CpuUsed = 8
				return o
			}(),
			extraArgs: []string{"--end-usage=cbr"},
		},
		{
			name: "realtime-cpu15-cbr",
			opts: func() EncoderOptions {
				o := baseOpts
				o.RateControlMode = RateControlCBR
				o.Deadline = DeadlineRealtime
				o.CpuUsed = 15
				return o
			}(),
			extraArgs: []string{"--end-usage=cbr"},
		},
		{
			name: "good-quality-cpu3-vbr",
			opts: func() EncoderOptions {
				o := baseOpts
				o.RateControlMode = RateControlVBR
				o.Deadline = DeadlineGoodQuality
				o.CpuUsed = 3
				return o
			}(),
			extraArgs: []string{"--end-usage=vbr"},
		},
	}

	reports := make([]FixtureLFReport, 0, len(fixtures))
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			govpxTrace := captureGovpxEncoderTrace(t, f.opts, sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "lf-"+f.name, f.opts, targetKbps, sources, f.extraArgs)
			govpxRows := loopFilterFrameRowsFromTrace(t, govpxTrace)
			libvpxRows := loopFilterFrameRowsFromTrace(t, libvpxTrace)
			report := scoreLoopFilterFrames(f.name, govpxRows, libvpxRows)
			reports = append(reports, report)
			t.Logf("loop-filter parity report: %s frames=%d level=%.2f%% sharp=%.2f%% refdeltas=%.2f%% modedeltas=%.2f%% enabled=%.2f%% update=%.2f%%",
				report.Name, report.FrameTotal,
				report.LevelMatchPct, report.SharpnessMatchPct,
				report.RefDeltasMatchPct, report.ModeDeltasMatchPct,
				report.EnabledMatchPct, report.UpdateMatchPct)
		})
	}

	// Sort to keep on-disk JSON stable across runs.
	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })

	if vp8test.UpdateBaselines() {
		writeLoopFilterBaseline(t, reports)
		return
	}
	enforceLoopFilterBaseline(t, reports)
}

func loopFilterFrameRowsFromTrace(t *testing.T, trace []byte) []loopFilterFrameRow {
	t.Helper()
	var rows []loopFilterFrameRow
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scan.Scan() {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			t.Fatalf("trace row not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typeRaw, ok := raw["type"]
		if !ok {
			continue
		}
		var rowType string
		if err := json.Unmarshal(typeRaw, &rowType); err != nil {
			continue
		}
		if rowType != "frame" {
			continue
		}
		row := loopFilterFrameRow{}
		if v, ok := raw["frame_index"]; ok {
			_ = json.Unmarshal(v, &row.FrameIndex)
		}
		if v, ok := raw["loop_filter_level"]; ok {
			_ = json.Unmarshal(v, &row.LoopFilter)
		}
		if v, ok := raw["sharpness_level"]; ok {
			_ = json.Unmarshal(v, &row.SharpnessLevel)
		}
		if v, ok := raw["ref_lf_deltas"]; ok {
			_ = json.Unmarshal(v, &row.RefLFDeltas)
		}
		if v, ok := raw["mode_lf_deltas"]; ok {
			_ = json.Unmarshal(v, &row.ModeLFDeltas)
		}
		if v, ok := raw["mode_ref_lf_delta_enabled"]; ok {
			row.EnabledSet = true
			_ = json.Unmarshal(v, &row.Enabled)
		}
		if v, ok := raw["mode_ref_lf_delta_update"]; ok {
			row.UpdateSet = true
			_ = json.Unmarshal(v, &row.Update)
		}
		rows = append(rows, row)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return rows
}

func scoreLoopFilterFrames(name string, govpxRows []loopFilterFrameRow, libvpxRows []loopFilterFrameRow) FixtureLFReport {
	total := min(len(libvpxRows), len(govpxRows))
	report := FixtureLFReport{Name: name, FrameTotal: total}
	if total == 0 {
		return report
	}
	var (
		level    int
		sharp    int
		refDelta int
		modDelta int
		enabled  int
		update   int
	)
	for i := range total {
		g := govpxRows[i]
		l := libvpxRows[i]
		if g.LoopFilter == l.LoopFilter {
			level++
		}
		if g.SharpnessLevel == l.SharpnessLevel {
			sharp++
		}
		if g.RefLFDeltas == l.RefLFDeltas {
			refDelta++
		}
		if g.ModeLFDeltas == l.ModeLFDeltas {
			modDelta++
		}
		// Per the spec: missing field on either side -> treat as false.
		if g.Enabled == l.Enabled {
			enabled++
		}
		if g.Update == l.Update {
			update++
		}
	}
	pct := func(n int) float64 { return 100.0 * float64(n) / float64(total) }
	report.LevelMatchPct = pct(level)
	report.SharpnessMatchPct = pct(sharp)
	report.RefDeltasMatchPct = pct(refDelta)
	report.ModeDeltasMatchPct = pct(modDelta)
	report.EnabledMatchPct = pct(enabled)
	report.UpdateMatchPct = pct(update)
	return report
}

func writeLoopFilterBaseline(t *testing.T, reports []FixtureLFReport) {
	t.Helper()
	file := loopFilterBaselineFile{Fixtures: map[string]loopFilterBaselineEntry{}}
	for _, r := range reports {
		file.Fixtures[r.Name] = loopFilterBaselineEntry{
			LevelMatchPct:      r.LevelMatchPct,
			SharpnessMatchPct:  r.SharpnessMatchPct,
			RefDeltasMatchPct:  r.RefDeltasMatchPct,
			ModeDeltasMatchPct: r.ModeDeltasMatchPct,
			EnabledMatchPct:    r.EnabledMatchPct,
			UpdateMatchPct:     r.UpdateMatchPct,
		}
	}
	vp8test.WriteJSONBaseline(t, loopFilterMatchRateBaselinePath, file)
	t.Logf("wrote %s", loopFilterMatchRateBaselinePath)
}

func enforceLoopFilterBaseline(t *testing.T, reports []FixtureLFReport) {
	t.Helper()
	file, ok := vp8test.ReadOptionalJSONBaseline[loopFilterBaselineFile](t, loopFilterMatchRateBaselinePath)
	if !ok {
		t.Fatalf("baseline %s is missing; run with GOVPX_UPDATE_BASELINES=1 to bootstrap", loopFilterMatchRateBaselinePath)
	}
	const tol = 2.0
	for _, r := range reports {
		baseline, ok := file.Fixtures[r.Name]
		if !ok {
			t.Errorf("loop-filter baseline missing fixture %q (run with GOVPX_UPDATE_BASELINES=1)", r.Name)
			continue
		}
		checks := []struct {
			field    string
			got      float64
			baseline float64
		}{
			{"level_match_pct", r.LevelMatchPct, baseline.LevelMatchPct},
			{"sharpness_match_pct", r.SharpnessMatchPct, baseline.SharpnessMatchPct},
			{"ref_deltas_match_pct", r.RefDeltasMatchPct, baseline.RefDeltasMatchPct},
			{"mode_deltas_match_pct", r.ModeDeltasMatchPct, baseline.ModeDeltasMatchPct},
			{"enabled_match_pct", r.EnabledMatchPct, baseline.EnabledMatchPct},
			{"update_match_pct", r.UpdateMatchPct, baseline.UpdateMatchPct},
		}
		for _, c := range checks {
			if c.got < c.baseline-tol {
				t.Errorf("%s %s = %.4f, baseline %.4f, regression > %.2f",
					r.Name, c.field, c.got, c.baseline, tol)
			}
		}
	}
}
