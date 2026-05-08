package main

import (
	"strings"
	"testing"
)

func TestCleanBodyStripsFrameworkLinesAndPrefix(t *testing.T) {
	in := strings.Join([]string{
		"=== RUN   TestX",
		"=== PAUSE TestX",
		"=== CONT  TestX",
		"    oracle_x_test.go:120: scoreboard summary:",
		"        | fixture | metric |",
		"        |---|---|",
		"        | good-cpu3 | 99% |",
		"    oracle_x_test.go:140: wrote baseline testdata/x.json with 3 fixtures",
		"--- PASS: TestX (1.23s)",
		"PASS",
		"ok  	github.com/thesyncim/govpx	1.234s",
		"",
	}, "\n")

	got := cleanBody(in)

	if strings.Contains(got, "=== RUN") || strings.Contains(got, "=== PAUSE") || strings.Contains(got, "=== CONT") {
		t.Errorf("framework lines leaked through:\n%s", got)
	}
	if strings.Contains(got, "--- PASS") || strings.Contains(got, "ok  ") {
		t.Errorf("terminal status lines leaked through:\n%s", got)
	}
	if strings.Contains(got, "wrote baseline ") {
		t.Errorf("'wrote baseline' noise not filtered:\n%s", got)
	}
	if strings.Contains(got, "oracle_x_test.go:") {
		t.Errorf("file:line prefix not stripped:\n%s", got)
	}
	for _, want := range []string{
		"scoreboard summary:",
		"| fixture | metric |",
		"|---|---|",
		"| good-cpu3 | 99% |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in cleaned body, got:\n%s", want, got)
		}
	}
}

func TestFormatGapReportFixtureTable(t *testing.T) {
	raw := []byte(`{
	  "fixtures": {
	    "good-cpu3": {"mode_match_pct": 97.32142857142857, "mv_match_pct": 100},
	    "rt-cpu0":   {"mode_match_pct": 99.10714285714286, "mv_match_pct": 99.10714285714286}
	  }
	}`)
	headline, table := formatGapReport(raw)
	if !strings.Contains(headline, "max 2.6786pp deficit on mode") {
		t.Errorf("headline should call out worst gap, got: %q", headline)
	}
	if !strings.Contains(headline, "good-cpu3") {
		t.Errorf("headline should name the worst fixture, got: %q", headline)
	}
	for _, want := range []string{
		"fixture",
		// _match_pct columns are renamed to their stem.
		"mode",
		"mv",
		"good-cpu3",
		"2.6786pp",
		"0pp",
		"rt-cpu0",
		"0.8929pp",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("expected %q in gap table, got:\n%s", want, table)
		}
	}
	if strings.Contains(table, "_match_pct") {
		t.Errorf("gap table should hide the verbose _match_pct suffix, got:\n%s", table)
	}
}

func TestFormatGapReportPerfect(t *testing.T) {
	raw := []byte(`{
	  "fixtures": {
	    "good-cpu3": {"mode_match_pct": 100, "mv_match_pct": 100}
	  }
	}`)
	headline, _ := formatGapReport(raw)
	if !strings.Contains(headline, "PERFECT") {
		t.Errorf("expected PERFECT headline when all match-rates are 100, got: %q", headline)
	}
}

func TestFormatGapReportFlat(t *testing.T) {
	raw := []byte(`{
	  "max_abs_q_delta": 0,
	  "mean_abs_q_delta": 0,
	  "max_size_delta_pct": 1.5120967741935485,
	  "keyframe_q_match": true
	}`)
	headline, table := formatGapReport(raw)
	if !strings.Contains(headline, "max_size_delta_pct=1.5121") {
		t.Errorf("headline should highlight the largest raw gap, got: %q", headline)
	}
	for _, want := range []string{
		"max_abs_q_delta",
		"= 0",
		"max_size_delta_pct",
		"1.5121%",
		"keyframe_q_match",
		"= yes",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("expected %q in flat gap render, got:\n%s", want, table)
		}
	}
}

func TestFormatGapReportRawDeltaCount(t *testing.T) {
	// realtime_candidate_scoreboard.json shape: divergent_rows is the gap.
	raw := []byte(`{
	  "fixtures": {
	    "rt-cbr-cpu0": {"divergent_rows": 2, "total_rows": 285, "field_hist": {"mv_row":2}}
	  }
	}`)
	headline, table := formatGapReport(raw)
	if !strings.Contains(headline, "divergent_rows=2") {
		t.Errorf("headline should call out divergent_rows, got: %q", headline)
	}
	if !strings.Contains(table, "divergent_rows") || !strings.Contains(table, "total_rows") {
		t.Errorf("expected both gap and reference columns in table, got:\n%s", table)
	}
}

