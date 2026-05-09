package govpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

// scoreboardFixtureSnapshot captures one realtime CPU band's
// inter-candidate divergence summary. JSON shape is the on-disk baseline
// shape, so we can encode/decode it directly.
type scoreboardFixtureSnapshot struct {
	DivergentRows int            `json:"divergent_rows"`
	TotalRows     int            `json:"total_rows"`
	FieldHist     map[string]int `json:"field_hist"`
}

type scoreboardBaseline struct {
	Fixtures map[string]scoreboardFixtureSnapshot `json:"fixtures"`
}

// TestOracleEncoderTraceInterCandidateScoreboard runs the inter-candidate
// trace comparator across a band of realtime CPU presets and turns the
// resulting [][]Divergence into a scoreboard. Unlike
// TestOracleEncoderTraceInterCandidateCompare, this test does NOT fail on
// any divergence -- it logs a per-fixture markdown table and asserts only
// against a baseline so we catch regressions without blocking incremental
// progress on the realtime path.
func TestOracleEncoderTraceInterCandidateScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle trace scoreboard")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 4
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
	makeOpts := func(cpu int) EncoderOptions {
		return EncoderOptions{
			Width:             width,
			Height:            height,
			FPS:               fps,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: targetKbps,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			Deadline:          DeadlineRealtime,
			CpuUsed:           cpu,
			KeyFrameInterval:  999,
		}
	}
	fixtures := []fixture{
		{name: "realtime-cbr-cpu0", opts: makeOpts(0), extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu4", opts: makeOpts(4), extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu8", opts: makeOpts(8), extraArgs: []string{"--end-usage=cbr"}},
	}

	current := scoreboardBaseline{Fixtures: map[string]scoreboardFixtureSnapshot{}}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			govpxTrace := captureGovpxEncoderTrace(t, fx.opts, sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "scoreboard-"+fx.name, fx.opts, targetKbps, sources, fx.extraArgs)
			govpxProjected := projectOracleInterCandidateTrace(t, govpxTrace)
			libvpxProjected := projectOracleInterCandidateTrace(t, libvpxTrace)

			div, err := coracle.CompareOracleTraces(bytes.NewReader(govpxProjected), bytes.NewReader(libvpxProjected), coracle.CompareOptions{
				MaxDivergences: 4096,
			})
			if err != nil {
				t.Fatalf("CompareOracleTraces returned error: %v", err)
			}

			govpxLines := splitNonEmptyLines(govpxProjected)
			libvpxLines := splitNonEmptyLines(libvpxProjected)
			totalRows := max(len(libvpxLines), len(govpxLines))

			uniqueRows := map[int]struct{}{}
			fieldHist := map[string]int{}
			for _, d := range div {
				uniqueRows[d.RowIndex] = struct{}{}
				if d.Field != "" {
					fieldHist[d.Field]++
				}
			}
			divergentRows := len(uniqueRows)
			matchRate := 0.0
			if totalRows > 0 {
				matchRate = 1.0 - float64(divergentRows)/float64(totalRows)
			}

			snap := scoreboardFixtureSnapshot{
				DivergentRows: divergentRows,
				TotalRows:     totalRows,
				FieldHist:     fieldHist,
			}
			current.Fixtures[fx.name] = snap

			t.Logf("scoreboard %s: divergent_rows=%d total_inter_candidate_rows=%d match_rate=%.4f",
				fx.name, divergentRows, totalRows, matchRate)
			t.Logf("\n%s", formatScoreboardTable(fx.name, snap, matchRate))
		})
	}

	baselinePath := filepath.Join("testdata", "realtime_candidate_scoreboard.json")
	updateBaselines := os.Getenv("GOVPX_UPDATE_BASELINES") == "1"
	_, statErr := os.Stat(baselinePath)
	if updateBaselines || os.IsNotExist(statErr) {
		buf, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			t.Fatalf("Marshal scoreboard baseline: %v", err)
		}
		buf = append(buf, '\n')
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(baselinePath), err)
		}
		if err := os.WriteFile(baselinePath, buf, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", baselinePath, err)
		}
		t.Logf("wrote baseline %s", baselinePath)
		return
	}
	if statErr != nil {
		t.Fatalf("stat %s: %v", baselinePath, statErr)
	}

	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", baselinePath, err)
	}
	var base scoreboardBaseline
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatalf("Unmarshal baseline: %v", err)
	}

	for _, fx := range fixtures {
		got, ok := current.Fixtures[fx.name]
		if !ok {
			continue
		}
		want, ok := base.Fixtures[fx.name]
		if !ok {
			t.Errorf("baseline %s missing fixture %q (rerun with GOVPX_UPDATE_BASELINES=1)", baselinePath, fx.name)
			continue
		}
		slack := max(want.DivergentRows/10, 8)
		limit := want.DivergentRows + slack
		if got.DivergentRows > limit {
			t.Errorf("%s: divergent_rows=%d exceeds baseline %d + slack %d (=%d); rerun with GOVPX_UPDATE_BASELINES=1 if intended",
				fx.name, got.DivergentRows, want.DivergentRows, slack, limit)
		} else {
			t.Logf("%s: divergent_rows=%d (baseline %d, slack %d, limit %d, %+d)",
				fx.name, got.DivergentRows, want.DivergentRows, slack, limit, got.DivergentRows-want.DivergentRows)
		}
		for field, count := range got.FieldHist {
			prev := want.FieldHist[field]
			if prev == 0 && count > 8 {
				t.Errorf("%s: field %q newly diverges %d times (baseline absent); rerun with GOVPX_UPDATE_BASELINES=1 if intended",
					fx.name, field, count)
			}
		}
	}
}

func formatScoreboardTable(name string, snap scoreboardFixtureSnapshot, matchRate float64) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "### %s\n", name)
	fmt.Fprintf(&buf, "| metric | value |\n")
	fmt.Fprintf(&buf, "|---|---|\n")
	fmt.Fprintf(&buf, "| total_inter_candidate_rows | %d |\n", snap.TotalRows)
	fmt.Fprintf(&buf, "| divergent_rows | %d |\n", snap.DivergentRows)
	fmt.Fprintf(&buf, "| match_rate | %.4f |\n", matchRate)
	if len(snap.FieldHist) > 0 {
		fmt.Fprintf(&buf, "\n| field | count |\n|---|---|\n")
		fields := make([]string, 0, len(snap.FieldHist))
		for f := range snap.FieldHist {
			fields = append(fields, f)
		}
		sort.Slice(fields, func(i, j int) bool {
			if snap.FieldHist[fields[i]] != snap.FieldHist[fields[j]] {
				return snap.FieldHist[fields[i]] > snap.FieldHist[fields[j]]
			}
			return fields[i] < fields[j]
		})
		for _, f := range fields {
			fmt.Fprintf(&buf, "| %s | %d |\n", f, snap.FieldHist[f])
		}
	}
	return buf.String()
}
