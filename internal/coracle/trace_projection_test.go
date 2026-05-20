package coracle

import (
	"bytes"
	"strings"
	"testing"
)

func TestProjectVP8EncoderDecisionTraceDropsInterCandidateRows(t *testing.T) {
	trace := []byte(
		`{"type":"rate","frame_index":0,"frame_type":"key","q_index":4}` + "\n" +
			`{"type":"inter_candidate","frame_index":1,"mb_row":0,"mb_col":0,"picker":"rd","mode_index":0}` + "\n" +
			`{"type":"frame","frame_index":0,"frame_type":"key","q_index":4}` + "\n",
	)
	projected, err := ProjectVP8EncoderDecisionTrace(trace)
	if err != nil {
		t.Fatalf("ProjectVP8EncoderDecisionTrace: %v", err)
	}
	if bytes.Contains(projected, []byte("inter_candidate")) {
		t.Fatalf("projected decision trace retained inter_candidate row:\n%s", projected)
	}
	lines := TraceLines(projected)
	if len(lines) != 2 {
		t.Fatalf("projected decision trace lines = %d, want 2\n%s", len(lines), projected)
	}
}

func TestProjectVP8InterCandidateTraceKeepsStagedFields(t *testing.T) {
	trace := []byte(
		`{"type":"rate","frame_index":0,"frame_type":"key","q_index":4}` + "\n" +
			`{"type":"inter_candidate","frame_index":1,"mb_row":0,"mb_col":0,"picker":"rd","mode_index":7,"mode":"NEWMV","ref_slot":1,"ref_frame":"LAST_FRAME","outcome":"tested","became_best":true,"loop_break":false,"mv_row":8,"mv_col":16,"score":99,"rate":12}` + "\n" +
			`{"type":"inter_candidate","frame_index":1,"mb_row":0,"mb_col":0,"picker":"rd","mode_index":8,"outcome":"skipped_threshold","threshold":800}` + "\n" +
			`{"type":"mb","frame_index":1,"mb_row":0,"mb_col":0,"mode":"NEWMV"}` + "\n",
	)
	projected, err := ProjectVP8InterCandidateTrace(trace)
	if err != nil {
		t.Fatalf("ProjectVP8InterCandidateTrace: %v", err)
	}
	lines := TraceLines(projected)
	if len(lines) != 1 {
		t.Fatalf("projected candidate trace lines = %d, want 1\n%s", len(lines), projected)
	}
	if !bytes.Contains(projected, []byte(`"type":"inter_candidate"`)) {
		t.Fatalf("projected candidate trace omitted candidate row:\n%s", projected)
	}
	for _, dropped := range []string{"score", "rate", "q_index", `"type":"mb"`, "skipped_threshold"} {
		if bytes.Contains(projected, []byte(dropped)) {
			t.Fatalf("projected candidate trace retained %q:\n%s", dropped, projected)
		}
	}
}

func TestProjectVP8InterCandidateThresholdTraceKeepsThresholdRows(t *testing.T) {
	trace := []byte(
		`{"type":"inter_candidate","frame_index":1,"mb_row":0,"mb_col":0,"picker":"fast","mode_index":7,"mode":"NEWMV","ref_slot":1,"threshold":800,"score":801}` + "\n" +
			`{"type":"rate","frame_index":1,"q_index":20}` + "\n",
	)
	projected, err := ProjectVP8InterCandidateThresholdTrace(trace)
	if err != nil {
		t.Fatalf("ProjectVP8InterCandidateThresholdTrace: %v", err)
	}
	if !bytes.Contains(projected, []byte(`"threshold":800`)) {
		t.Fatalf("projected threshold trace omitted threshold:\n%s", projected)
	}
	if bytes.Contains(projected, []byte("score")) || bytes.Contains(projected, []byte(`"type":"rate"`)) {
		t.Fatalf("projected threshold trace retained unrelated data:\n%s", projected)
	}
}

func TestFirstTraceRowsFormatsPrefix(t *testing.T) {
	got := FirstTraceRows([]byte("a\n\nb\nc\n"), 2)
	want := "0: a\n1: b\n"
	if got != want {
		t.Fatalf("FirstTraceRows = %q, want %q", got, want)
	}
	if strings.Contains(FirstTraceRows(nil, 3), "0:") {
		t.Fatal("FirstTraceRows reported rows for empty trace")
	}
}
