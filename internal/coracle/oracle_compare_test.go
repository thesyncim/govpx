package coracle

import (
	"fmt"
	"strings"
	"testing"
)

// frameRow returns a JSON Lines-encoded "frame" row matching the schema in
// encoder_oracle_trace.go. Helper kept inside the test file so the test does
// not depend on the encoder package and CompareOracleTraces stays
// reader-only.
func frameRow(frameIndex int, qIndex int, refreshLast bool, yAdler uint32) string {
	return strings.Join([]string{
		"{\"type\":\"frame\"",
		fmt.Sprintf("\"frame_index\":%d", frameIndex),
		"\"frame_type\":\"inter\"",
		fmt.Sprintf("\"q_index\":%d", qIndex),
		"\"base_q_index\":40",
		"\"loop_filter_level\":12",
		fmt.Sprintf("\"refresh_last\":%t", refreshLast),
		"\"refresh_golden\":false",
		"\"refresh_altref\":false",
		"\"sign_bias_golden\":false",
		"\"sign_bias_altref\":false",
		"\"segmentation_enabled\":false",
		fmt.Sprintf("\"y_adler32\":%d", yAdler),
		"\"u_adler32\":0",
		"\"v_adler32\":0",
		"\"size_bytes\":1234}",
	}, ",")
}

func mbRowJSON(frameIndex, mbRow, mbCol int, mode, ref string, mvRow, mvCol int, skip bool, eobSum int) string {
	return strings.Join([]string{
		"{\"type\":\"mb\"",
		fmt.Sprintf("\"frame_index\":%d", frameIndex),
		fmt.Sprintf("\"mb_row\":%d", mbRow),
		fmt.Sprintf("\"mb_col\":%d", mbCol),
		"\"segment_id\":0",
		fmt.Sprintf("\"mode\":%q", mode),
		fmt.Sprintf("\"ref_frame\":%q", ref),
		fmt.Sprintf("\"mv_row\":%d", mvRow),
		fmt.Sprintf("\"mv_col\":%d", mvCol),
		fmt.Sprintf("\"skip\":%t", skip),
		"\"eob\":[1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]",
		fmt.Sprintf("\"eob_sum\":%d}", eobSum),
	}, ",")
}

func TestCompareOracleTracesDetectsFieldDivergences(t *testing.T) {
	t.Parallel()

	govpx := strings.Join([]string{
		frameRow(0, 60, true, 0xdeadbeef),
		mbRowJSON(0, 0, 0, "ZEROMV", "LAST_FRAME", 0, 0, false, 1),
		mbRowJSON(0, 0, 1, "NEARESTMV", "LAST_FRAME", 4, -2, false, 3),
		frameRow(1, 62, false, 0xfeedface),
	}, "\n") + "\n"

	libvpx := strings.Join([]string{
		// Same frame 0, but q_index differs (60 vs 61) and y_adler32
		// differs (0xdeadbeef vs 0xdeadbeee).
		frameRow(0, 61, true, 0xdeadbeee),
		// MB (0,0) matches.
		mbRowJSON(0, 0, 0, "ZEROMV", "LAST_FRAME", 0, 0, false, 1),
		// MB (0,1) differs: mode picks NEWMV with non-zero MV vs
		// govpx's NEARESTMV. eob_sum also differs.
		mbRowJSON(0, 0, 1, "NEWMV", "LAST_FRAME", 8, -1, false, 5),
		// Frame 1: refresh_last differs (govpx=false, libvpx=true).
		frameRow(1, 62, true, 0xfeedface),
	}, "\n") + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) == 0 {
		t.Fatalf("expected divergences, got none")
	}

	// Build a (rowIndex,field) -> Divergence map for assertion ergonomics;
	// the comparator iterates over Go map keys for fields so order within
	// a row is non-deterministic but the per-(row,field) presence is.
	got := make(map[string]Divergence, len(div))
	for _, d := range div {
		got[divKey(d)] = d
	}

	wantKeys := []string{
		"row=0/field=q_index",
		"row=0/field=y_adler32",
		"row=2/field=mode",
		"row=2/field=mv_row",
		"row=2/field=mv_col",
		"row=2/field=eob_sum",
		"row=3/field=refresh_last",
	}
	for _, key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Errorf("missing divergence for %s; got divergences: %v", key, divKeys(div))
		}
	}

	// Spot-check that the row index 0 q_index reports the actual values
	// we fed in, and that frame index / coords are populated correctly.
	q := got["row=0/field=q_index"]
	if q.RowKind != "frame" {
		t.Errorf("row=0/field=q_index: RowKind=%q want frame", q.RowKind)
	}
	if q.FrameIndex != 0 {
		t.Errorf("row=0/field=q_index: FrameIndex=%d want 0", q.FrameIndex)
	}
	if gf, _ := q.Govpx.(float64); gf != 60 {
		t.Errorf("row=0/field=q_index: Govpx=%v want 60", q.Govpx)
	}
	if lf, _ := q.Libvpx.(float64); lf != 61 {
		t.Errorf("row=0/field=q_index: Libvpx=%v want 61", q.Libvpx)
	}

	mb := got["row=2/field=mode"]
	if mb.RowKind != "mb" {
		t.Errorf("row=2/field=mode: RowKind=%q want mb", mb.RowKind)
	}
	if mb.MBRow != 0 || mb.MBCol != 1 {
		t.Errorf("row=2/field=mode: coords=(%d,%d) want (0,1)", mb.MBRow, mb.MBCol)
	}
	if mb.Govpx != "NEARESTMV" || mb.Libvpx != "NEWMV" {
		t.Errorf("row=2/field=mode: values=(%v,%v) want (NEARESTMV,NEWMV)", mb.Govpx, mb.Libvpx)
	}
}

func TestCompareOracleTracesIdenticalStreams(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		frameRow(0, 60, true, 0xdeadbeef),
		mbRowJSON(0, 0, 0, "ZEROMV", "LAST_FRAME", 0, 0, false, 1),
		mbRowJSON(0, 0, 1, "NEARESTMV", "LAST_FRAME", 4, -2, false, 3),
	}, "\n") + "\n"

	div, err := CompareOracleTraces(strings.NewReader(stream), strings.NewReader(stream), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) != 0 {
		t.Fatalf("expected zero divergences, got %d: %+v", len(div), div)
	}
}

func TestCompareOracleTracesMissingRows(t *testing.T) {
	t.Parallel()

	govpx := strings.Join([]string{
		frameRow(0, 60, true, 0xdeadbeef),
		mbRowJSON(0, 0, 0, "ZEROMV", "LAST_FRAME", 0, 0, false, 1),
		mbRowJSON(0, 0, 1, "NEARESTMV", "LAST_FRAME", 4, -2, false, 3),
	}, "\n") + "\n"

	// libvpx truncated to one row: comparator should report two
	// "missing_libvpx" divergences for the trailing govpx rows.
	libvpx := frameRow(0, 60, true, 0xdeadbeef) + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	missing := 0
	for _, d := range div {
		if d.RowKind == "missing_libvpx" {
			missing++
		}
	}
	if missing != 2 {
		t.Fatalf("expected 2 missing_libvpx divergences, got %d: %+v", missing, div)
	}
}

func TestCompareOracleTracesIgnoreField(t *testing.T) {
	t.Parallel()

	govpx := frameRow(0, 60, true, 0xdeadbeef) + "\n"
	libvpx := frameRow(0, 60, true, 0x12345678) + "\n" // y_adler32 differs

	opts := CompareOptions{IgnoreFields: map[string]bool{"y_adler32": true}}
	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), opts)
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) != 0 {
		t.Fatalf("expected zero divergences with y_adler32 ignored, got: %+v", div)
	}
}

func TestCompareOracleTracesTypeMismatch(t *testing.T) {
	t.Parallel()

	govpx := mbRowJSON(0, 0, 0, "ZEROMV", "LAST_FRAME", 0, 0, false, 1) + "\n"
	libvpx := frameRow(0, 60, true, 0xdeadbeef) + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) != 1 {
		t.Fatalf("expected 1 divergence, got %d: %+v", len(div), div)
	}
	if div[0].RowKind != "type_mismatch" {
		t.Errorf("RowKind=%q want type_mismatch", div[0].RowKind)
	}
}

// divKey formats a divergence as "row=<idx>/field=<name>" for assertion
// keys. Stream-level divergences (no field) collapse to "row=<idx>/<kind>".
func divKey(d Divergence) string {
	if d.Field == "" {
		return fmt.Sprintf("row=%d/%s", d.RowIndex, d.RowKind)
	}
	return fmt.Sprintf("row=%d/field=%s", d.RowIndex, d.Field)
}

func divKeys(divs []Divergence) []string {
	out := make([]string, 0, len(divs))
	for _, d := range divs {
		out = append(out, divKey(d))
	}
	return out
}
