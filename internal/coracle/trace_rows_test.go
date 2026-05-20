package coracle

import "testing"

func TestTraceRowsOfTypeFiltersRows(t *testing.T) {
	trace := []byte(
		`{"type":"rate","frame_index":0,"q_index":20}` + "\n" +
			`{"type":"frame","frame_index":0,"size_bytes":100}` + "\n" +
			`{"type":"frame","frame_index":1,"size_bytes":110}` + "\n",
	)
	rows, err := TraceRowsOfType(trace, "frame")
	if err != nil {
		t.Fatalf("TraceRowsOfType: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("frame rows = %d, want 2", len(rows))
	}
	if got := TraceFloat(rows[1]["size_bytes"]); got != 110 {
		t.Fatalf("second size_bytes = %v, want 110", got)
	}
}

func TestTraceRowsByFrameKeepsLastRow(t *testing.T) {
	trace := []byte(
		`{"type":"recode","frame_index":2,"final_q":30}` + "\n" +
			`{"type":"frame","frame_index":2}` + "\n" +
			`{"type":"recode","frame_index":2,"final_q":32}` + "\n",
	)
	rows, err := TraceRowsByFrame(trace, "recode")
	if err != nil {
		t.Fatalf("TraceRowsByFrame: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("indexed rows = %d, want 1", len(rows))
	}
	if got := TraceFloat(rows[2]["final_q"]); got != 32 {
		t.Fatalf("final_q = %v, want 32", got)
	}
}

func TestTraceRowsRejectsInvalidJSON(t *testing.T) {
	if _, err := TraceRows([]byte("{not-json}\n")); err == nil {
		t.Fatal("TraceRows accepted invalid JSON")
	}
}

func TestFormatDivergencesIncludesContext(t *testing.T) {
	got := FormatDivergences([]Divergence{{
		RowIndex:   3,
		RowKind:    "mb",
		FrameIndex: 2,
		MBRow:      1,
		MBCol:      4,
		Field:      "mode",
		Govpx:      "NEWMV",
		Libvpx:     "ZEROMV",
	}})
	want := "row=3 kind=mb frame=2 mb=1,4 field=mode govpx=\"\\\"NEWMV\\\"\" libvpx=\"\\\"ZEROMV\\\"\"\n"
	if got != want {
		t.Fatalf("FormatDivergences = %q, want %q", got, want)
	}
}
