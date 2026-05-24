//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// FixtureImprovedMVReport is the per-fixture row of the improved-MV
// predictor scoreboard. It captures the per-MB match rate of the
// improved-MV start fields on NEWMV inter MBs that took the improved-MV
// predictor path on either side.
type FixtureImprovedMVReport struct {
	Name               string  `json:"-"`
	MBTotalNEWMV       int     `json:"mb_total_newmv"`
	NearSadIdxMatchPct float64 `json:"near_sadidx_match_pct"`
	MVMatchPct         float64 `json:"mv_match_pct"`
	SRMatchPct         float64 `json:"sr_match_pct"`
	CombinedMatchPct   float64 `json:"combined_match_pct"`
}

// improvedMVBaseline matches the on-disk schema used by every other
// scoreboard baseline JSON: a top-level `"fixtures"` map keyed by
// fixture name, so cmd/scoreboard-report can render it uniformly.
type improvedMVBaseline struct {
	Fixtures map[string]FixtureImprovedMVReport `json:"fixtures"`
}

const improvedMVBaselinePath = "testdata/improved_mv_match_rate_baseline.json"

// improvedMVMatchTolerance is the absolute slack (percentage points)
// the scoreboard allows below the recorded baseline before failing.
const improvedMVMatchTolerance = 2.0

// TestVP8OracleImprovedMVMatchScoreboard captures govpx + libvpx oracle traces for
// a panning corpus across two fixture configurations (Good/VBR cpu=3 and
// Realtime/CBR cpu=0) and reports the per-MB match rate of the improved-MV
// predictor fields (improved_mv_near_sadidx, improved_mv_row,
// improved_mv_col, improved_mv_sr) on NEWMV inter MBs that took the
// improved-MV start path on either side.
//
// This is a tripwire scoreboard: each percentage must stay within
// improvedMVMatchTolerance of the recorded baseline. To bootstrap or
// refresh the baseline run with GOVPX_UPDATE_BASELINES=1.
func TestVP8OracleImprovedMVMatchScoreboard(t *testing.T) {
	vp8test.RequireOracle(t, "improved-MV scoreboard")
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 8
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	cases := []struct {
		name      string
		opts      EncoderOptions
		extraArgs []string
	}{
		{
			name: "good-cpu3-vbr",
			opts: EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           3,
				KeyFrameInterval:  999,
			},
			extraArgs: []string{"--end-usage=vbr"},
		},
		{
			name: "rt-cpu0-cbr",
			opts: EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          DeadlineRealtime,
				CpuUsed:           0,
				KeyFrameInterval:  999,
			},
			extraArgs: []string{"--end-usage=cbr"},
		},
	}

	reports := make([]FixtureImprovedMVReport, 0, len(cases))
	for _, tc := range cases {
		govpxTrace := captureGovpxEncoderTrace(t, tc.opts, sources)
		libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "improved-mv-"+tc.name, tc.opts, targetKbps, sources, tc.extraArgs)
		report := computeImprovedMVReport(t, tc.name, govpxTrace, libvpxTrace)
		reports = append(reports, report)
	}

	// Markdown table for human readers.
	t.Logf("\n| fixture | mb_newmv | near_sadidx%% | mv%% | sr%% | combined%% |\n" +
		"|---------|----------|---------------|------|------|-----------|")
	for _, r := range reports {
		t.Logf("| %s | %d | %.2f | %.2f | %.2f | %.2f |",
			r.Name, r.MBTotalNEWMV,
			r.NearSadIdxMatchPct, r.MVMatchPct, r.SRMatchPct, r.CombinedMatchPct)
	}

	current := improvedMVBaseline{Fixtures: make(map[string]FixtureImprovedMVReport, len(reports))}
	for _, r := range reports {
		current.Fixtures[r.Name] = r
	}

	if vp8test.UpdateBaselines() {
		vp8test.WriteJSONBaseline(t, improvedMVBaselinePath, current)
		t.Logf("wrote baseline %s with %d fixtures", improvedMVBaselinePath, len(reports))
		return
	}

	baseline, baselineExists := vp8test.ReadOptionalJSONBaseline[improvedMVBaseline](t, improvedMVBaselinePath)
	if !baselineExists || len(baseline.Fixtures) == 0 {
		t.Fatalf("baseline %s is empty; run with GOVPX_UPDATE_BASELINES=1 to bootstrap", improvedMVBaselinePath)
	}
	for _, r := range reports {
		want, ok := baseline.Fixtures[r.Name]
		if !ok {
			t.Errorf("fixture %q missing from baseline %s", r.Name, improvedMVBaselinePath)
			continue
		}
		checkImprovedMVPctNoRegression(t, r.Name, "near_sadidx", r.NearSadIdxMatchPct, want.NearSadIdxMatchPct)
		checkImprovedMVPctNoRegression(t, r.Name, "mv", r.MVMatchPct, want.MVMatchPct)
		checkImprovedMVPctNoRegression(t, r.Name, "sr", r.SRMatchPct, want.SRMatchPct)
		checkImprovedMVPctNoRegression(t, r.Name, "combined", r.CombinedMatchPct, want.CombinedMatchPct)
	}
}

func checkImprovedMVPctNoRegression(t *testing.T, fixture string, field string, got float64, want float64) {
	t.Helper()
	if got+1e-9 < want-improvedMVMatchTolerance {
		t.Errorf("fixture %q %s match = %.4f%%, baseline = %.4f%% (tolerance %.2f%%)",
			fixture, field, got, want, improvedMVMatchTolerance)
	}
}

// improvedMVRow is the projected NEWMV inter MB used by the scoreboard.
type improvedMVRow struct {
	frameIndex int64
	mbRow      int
	mbCol      int
	mode       string
	improved   bool
	sadIdx     int64
	mvRow      int64
	mvCol      int64
	sr         int64
}

func computeImprovedMVReport(t *testing.T, name string, govpxTrace []byte, libvpxTrace []byte) FixtureImprovedMVReport {
	t.Helper()
	govpxMBs := parseImprovedMVTraceRows(t, govpxTrace)
	libvpxMBs := parseImprovedMVTraceRows(t, libvpxTrace)

	type key struct {
		frame int64
		row   int
		col   int
	}
	libvpxByKey := make(map[key]improvedMVRow, len(libvpxMBs))
	for _, r := range libvpxMBs {
		libvpxByKey[key{r.frameIndex, r.mbRow, r.mbCol}] = r
	}

	var (
		total    int
		nearOK   int
		mvOK     int
		srOK     int
		combined int
	)
	for _, g := range govpxMBs {
		// Walk per-MB rows that have improved_mv_near_sadidx populated:
		// NEWMV inter MBs that took improved-MV start.
		if g.mode != "NEWMV" || !g.improved {
			continue
		}
		l, ok := libvpxByKey[key{g.frameIndex, g.mbRow, g.mbCol}]
		if !ok {
			// libvpx side doesn't emit a matching MB row for this slot -
			// counts as a failure on every field so divergences surface.
			total++
			continue
		}
		// Match against libvpx only when libvpx also took improved-MV start
		// AND chose the same final mode (NEWMV). Otherwise the per-MB
		// improved-MV slot on the libvpx side is stale or never recorded;
		// count this as a non-matching row.
		total++
		if !(l.mode == "NEWMV" && l.improved) {
			continue
		}
		near := g.sadIdx == l.sadIdx
		mv := g.mvRow == l.mvRow && g.mvCol == l.mvCol
		sr := g.sr == l.sr
		if near {
			nearOK++
		}
		if mv {
			mvOK++
		}
		if sr {
			srOK++
		}
		if near && mv && sr {
			combined++
		}
	}
	if total == 0 {
		t.Fatalf("fixture %q produced 0 NEWMV improved-MV rows", name)
	}
	pct := func(n int) float64 { return 100.0 * float64(n) / float64(total) }
	return FixtureImprovedMVReport{
		Name:               name,
		MBTotalNEWMV:       total,
		NearSadIdxMatchPct: pct(nearOK),
		MVMatchPct:         pct(mvOK),
		SRMatchPct:         pct(srOK),
		CombinedMatchPct:   pct(combined),
	}
}

func parseImprovedMVTraceRows(t *testing.T, trace []byte) []improvedMVRow {
	t.Helper()
	var rows []improvedMVRow
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) == 0 {
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			t.Fatalf("trace row not valid JSON: %v\n%s", err, line)
		}
		if head.Type != "mb" {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			t.Fatalf("trace row not valid JSON: %v\n%s", err, line)
		}
		var row improvedMVRow
		row.frameIndex = mustNumber(t, raw, "frame_index")
		row.mbRow = int(mustNumber(t, raw, "mb_row"))
		row.mbCol = int(mustNumber(t, raw, "mb_col"))
		row.mode = mustString(t, raw, "mode")
		row.improved = optBool(raw, "improved_mv_start")
		row.sadIdx = optNumber(raw, "improved_mv_near_sadidx")
		row.mvRow = optNumber(raw, "improved_mv_row")
		row.mvCol = optNumber(raw, "improved_mv_col")
		row.sr = optNumber(raw, "improved_mv_sr")
		rows = append(rows, row)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return rows
}

func mustNumber(t *testing.T, raw map[string]json.RawMessage, field string) int64 {
	t.Helper()
	v, ok := raw[field]
	if !ok {
		t.Fatalf("trace row missing %q", field)
	}
	var n json.Number
	if err := json.Unmarshal(v, &n); err != nil {
		t.Fatalf("trace row %q not numeric: %v", field, err)
	}
	i, err := n.Int64()
	if err != nil {
		t.Fatalf("trace row %q not integer: %v", field, err)
	}
	return i
}

func mustString(t *testing.T, raw map[string]json.RawMessage, field string) string {
	t.Helper()
	v, ok := raw[field]
	if !ok {
		t.Fatalf("trace row missing %q", field)
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		t.Fatalf("trace row %q not a string: %v", field, err)
	}
	return s
}

func optNumber(raw map[string]json.RawMessage, field string) int64 {
	v, ok := raw[field]
	if !ok {
		return 0
	}
	var n json.Number
	if err := json.Unmarshal(v, &n); err != nil {
		return 0
	}
	i, err := n.Int64()
	if err != nil {
		return 0
	}
	return i
}

func optBool(raw map[string]json.RawMessage, field string) bool {
	v, ok := raw[field]
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return false
	}
	return b
}
