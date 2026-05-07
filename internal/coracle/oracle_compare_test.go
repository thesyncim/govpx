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
	return frameRowFull(frameIndex, qIndex, "inter", refreshLast, yAdler, false, false)
}

// frameRowFull is the full-control variant of frameRow used by tests that
// need to vary the frame type, refresh-entropy-probs flag, or default-coef
// reset gate. Existing call sites use frameRow which keeps the new fields
// at their pre-extension defaults (false) so older tests keep passing.
func frameRowFull(frameIndex int, qIndex int, frameType string, refreshLast bool, yAdler uint32, refreshEntropyProbs bool, defaultCoefReset bool) string {
	return frameRowLF(frameIndex, qIndex, frameType, refreshLast, yAdler, refreshEntropyProbs, defaultCoefReset, 0, [4]int{0, 0, 0, 0}, [4]int{0, 0, 0, 0}, false, false)
}

// frameRowLF is the full-control variant that also threads the LF-delta
// fields (sharpness, ref/mode deltas, enabled/update flags) so tests can
// exercise the libvpx-side oracle's per-frame loop-filter delta rows. The
// schema mirrors oracleTraceFrameRow in encoder_oracle_trace.go.
func frameRowLF(frameIndex int, qIndex int, frameType string, refreshLast bool, yAdler uint32, refreshEntropyProbs bool, defaultCoefReset bool, sharpness int, refLFDeltas [4]int, modeLFDeltas [4]int, modeRefDeltaEnabled bool, modeRefDeltaUpdate bool) string {
	return strings.Join([]string{
		"{\"type\":\"frame\"",
		fmt.Sprintf("\"frame_index\":%d", frameIndex),
		fmt.Sprintf("\"frame_type\":%q", frameType),
		fmt.Sprintf("\"q_index\":%d", qIndex),
		"\"base_q_index\":40",
		"\"loop_filter_level\":12",
		fmt.Sprintf("\"sharpness_level\":%d", sharpness),
		fmt.Sprintf("\"ref_lf_deltas\":[%d,%d,%d,%d]", refLFDeltas[0], refLFDeltas[1], refLFDeltas[2], refLFDeltas[3]),
		fmt.Sprintf("\"mode_lf_deltas\":[%d,%d,%d,%d]", modeLFDeltas[0], modeLFDeltas[1], modeLFDeltas[2], modeLFDeltas[3]),
		fmt.Sprintf("\"mode_ref_lf_delta_enabled\":%t", modeRefDeltaEnabled),
		fmt.Sprintf("\"mode_ref_lf_delta_update\":%t", modeRefDeltaUpdate),
		fmt.Sprintf("\"refresh_last\":%t", refreshLast),
		"\"refresh_golden\":false",
		"\"refresh_altref\":false",
		"\"sign_bias_golden\":false",
		"\"sign_bias_altref\":false",
		"\"segmentation_enabled\":false",
		fmt.Sprintf("\"refresh_entropy_probs\":%t", refreshEntropyProbs),
		fmt.Sprintf("\"default_coef_reset\":%t", defaultCoefReset),
		fmt.Sprintf("\"y_adler32\":%d", yAdler),
		"\"u_adler32\":0",
		"\"v_adler32\":0",
		"\"size_bytes\":1234}",
	}, ",")
}

// rateRow returns a JSON Lines-encoded "rate" row matching the schema in
// encoder_oracle_trace.go (oracleTraceRateRow). The helper takes the rare
// fields likely to vary in tests; the rest stay at deterministic defaults
// so the comparator's union-of-keys diff stays focused on the field under
// test.
func rateRow(frameIndex, qIndex, activeWorst, bufferLevel, projected, frameTarget, kfOverspend int) string {
	return rateRowFull(frameIndex, qIndex, activeWorst, bufferLevel, projected, frameTarget, kfOverspend, 0)
}

// rateRowFull is the full-control variant of rateRow used by tests that need
// to vary the zbin_over_quant field. Existing call sites use rateRow which
// keeps zbin_over_quant at the pre-extension default (0).
func rateRowFull(frameIndex, qIndex, activeWorst, bufferLevel, projected, frameTarget, kfOverspend, zbinOverQuant int) string {
	return strings.Join([]string{
		"{\"type\":\"rate\"",
		fmt.Sprintf("\"frame_index\":%d", frameIndex),
		"\"frame_type\":\"inter\"",
		fmt.Sprintf("\"q_index\":%d", qIndex),
		fmt.Sprintf("\"active_worst_quality\":%d", activeWorst),
		"\"active_best_quality\":4",
		fmt.Sprintf("\"buffer_level\":%d", bufferLevel),
		"\"total_byte_count\":0",
		fmt.Sprintf("\"projected_frame_size\":%d", projected),
		fmt.Sprintf("\"this_frame_target\":%d", frameTarget),
		fmt.Sprintf("\"kf_overspend_bits\":%d", kfOverspend),
		"\"gf_overspend_bits\":0",
		fmt.Sprintf("\"zbin_over_quant\":%d}", zbinOverQuant),
	}, ",")
}

// recodeRow returns a JSON Lines-encoded "recode" row matching the schema
// in encoder_oracle_trace.go (oracleTraceRecodeRow).
func recodeRow(frameIndex, loopCount, finalQ int, reason string) string {
	return strings.Join([]string{
		"{\"type\":\"recode\"",
		fmt.Sprintf("\"frame_index\":%d", frameIndex),
		fmt.Sprintf("\"loop_count\":%d", loopCount),
		fmt.Sprintf("\"final_q\":%d", finalQ),
		fmt.Sprintf("\"reason\":%q}", reason),
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

func TestCompareOracleTracesDetectsRateRowDivergence(t *testing.T) {
	t.Parallel()

	// govpx and libvpx agree on the frame and MB rows but diverge on the
	// rate row's q_index, active_worst_quality, buffer_level, and
	// projected_frame_size. The comparator should surface each field-level
	// mismatch with RowKind == "rate" and the right frame index.
	govpx := strings.Join([]string{
		rateRow(0, 60, 80, 50000, 9872, 12000, 0),
		frameRow(0, 60, true, 0xdeadbeef),
		mbRowJSON(0, 0, 0, "ZEROMV", "LAST_FRAME", 0, 0, false, 1),
	}, "\n") + "\n"

	libvpx := strings.Join([]string{
		rateRow(0, 61, 79, 49000, 10000, 12000, 0),
		frameRow(0, 60, true, 0xdeadbeef),
		mbRowJSON(0, 0, 0, "ZEROMV", "LAST_FRAME", 0, 0, false, 1),
	}, "\n") + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) == 0 {
		t.Fatalf("expected divergences, got none")
	}
	got := make(map[string]Divergence, len(div))
	for _, d := range div {
		got[divKey(d)] = d
	}
	wantKeys := []string{
		"row=0/field=q_index",
		"row=0/field=active_worst_quality",
		"row=0/field=buffer_level",
		"row=0/field=projected_frame_size",
	}
	for _, key := range wantKeys {
		d, ok := got[key]
		if !ok {
			t.Errorf("missing divergence for %s; got divergences: %v", key, divKeys(div))
			continue
		}
		if d.RowKind != "rate" {
			t.Errorf("%s: RowKind=%q want rate", key, d.RowKind)
		}
		if d.FrameIndex != 0 {
			t.Errorf("%s: FrameIndex=%d want 0", key, d.FrameIndex)
		}
		if d.MBRow != -1 || d.MBCol != -1 {
			t.Errorf("%s: coords=(%d,%d) want (-1,-1)", key, d.MBRow, d.MBCol)
		}
	}
	// Spot-check the q_index payload reflects the actual feed values.
	q := got["row=0/field=q_index"]
	if gf, _ := q.Govpx.(float64); gf != 60 {
		t.Errorf("row=0/field=q_index: Govpx=%v want 60", q.Govpx)
	}
	if lf, _ := q.Libvpx.(float64); lf != 61 {
		t.Errorf("row=0/field=q_index: Libvpx=%v want 61", q.Libvpx)
	}
}

func TestCompareOracleTracesDetectsRecodeRowDivergence(t *testing.T) {
	t.Parallel()

	// govpx records a 3-iteration size_recode terminating at Q=58 while
	// libvpx records a 4-iteration kf_forced_quality terminating at Q=60.
	// All three fields (loop_count, final_q, reason) must surface as
	// divergences with RowKind == "recode".
	govpx := strings.Join([]string{
		rateRow(0, 58, 80, 50000, 9872, 12000, 0),
		recodeRow(0, 3, 58, "size_recode"),
		frameRow(0, 58, true, 0xdeadbeef),
	}, "\n") + "\n"

	libvpx := strings.Join([]string{
		rateRow(0, 58, 80, 50000, 9872, 12000, 0),
		recodeRow(0, 4, 60, "kf_forced_quality"),
		frameRow(0, 58, true, 0xdeadbeef),
	}, "\n") + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	got := make(map[string]Divergence, len(div))
	for _, d := range div {
		got[divKey(d)] = d
	}
	wantKeys := []string{
		"row=1/field=loop_count",
		"row=1/field=final_q",
		"row=1/field=reason",
	}
	for _, key := range wantKeys {
		d, ok := got[key]
		if !ok {
			t.Errorf("missing divergence for %s; got divergences: %v", key, divKeys(div))
			continue
		}
		if d.RowKind != "recode" {
			t.Errorf("%s: RowKind=%q want recode", key, d.RowKind)
		}
		if d.FrameIndex != 0 {
			t.Errorf("%s: FrameIndex=%d want 0", key, d.FrameIndex)
		}
	}
	// Spot-check the reason payload is reported as the literal string.
	reason := got["row=1/field=reason"]
	if reason.Govpx != "size_recode" || reason.Libvpx != "kf_forced_quality" {
		t.Errorf("row=1/field=reason: values=(%v,%v) want (size_recode, kf_forced_quality)",
			reason.Govpx, reason.Libvpx)
	}
	// loop_count and final_q decode as float64 from JSON.
	loop := got["row=1/field=loop_count"]
	if gf, _ := loop.Govpx.(float64); gf != 3 {
		t.Errorf("row=1/field=loop_count: Govpx=%v want 3", loop.Govpx)
	}
	if lf, _ := loop.Libvpx.(float64); lf != 4 {
		t.Errorf("row=1/field=loop_count: Libvpx=%v want 4", loop.Libvpx)
	}
}

// TestCompareOracleTracesDetectsZbinOverQuantDivergence feeds two streams
// where the rate row's zbin_over_quant differs (govpx says 0, libvpx says
// 12 — modelling a GF/ARF cycle where libvpx engages the zbin overshoot
// while govpx does not). The comparator must surface the field-level
// mismatch with RowKind == "rate".
func TestCompareOracleTracesDetectsZbinOverQuantDivergence(t *testing.T) {
	t.Parallel()

	govpx := strings.Join([]string{
		rateRowFull(0, 60, 80, 50000, 9872, 12000, 0, 0),
		frameRow(0, 60, true, 0xdeadbeef),
	}, "\n") + "\n"

	libvpx := strings.Join([]string{
		rateRowFull(0, 60, 80, 50000, 9872, 12000, 0, 12),
		frameRow(0, 60, true, 0xdeadbeef),
	}, "\n") + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	got := make(map[string]Divergence, len(div))
	for _, d := range div {
		got[divKey(d)] = d
	}
	d, ok := got["row=0/field=zbin_over_quant"]
	if !ok {
		t.Fatalf("missing zbin_over_quant divergence; got: %v", divKeys(div))
	}
	if d.RowKind != "rate" {
		t.Errorf("RowKind=%q want rate", d.RowKind)
	}
	if d.FrameIndex != 0 {
		t.Errorf("FrameIndex=%d want 0", d.FrameIndex)
	}
	if d.MBRow != -1 || d.MBCol != -1 {
		t.Errorf("coords=(%d,%d) want (-1,-1)", d.MBRow, d.MBCol)
	}
	if gv, _ := d.Govpx.(float64); gv != 0 {
		t.Errorf("Govpx=%v want 0", d.Govpx)
	}
	if lv, _ := d.Libvpx.(float64); lv != 12 {
		t.Errorf("Libvpx=%v want 12", d.Libvpx)
	}
}

// TestCompareOracleTracesDetectsDefaultCoefResetDivergence feeds two streams
// where the key-frame "frame" row's default_coef_reset bool differs (govpx
// says false, libvpx says true — modelling a parity break where govpx is
// not yet in error-resilient mode while libvpx is). The comparator must
// surface the field-level mismatch with RowKind == "frame".
func TestCompareOracleTracesDetectsDefaultCoefResetDivergence(t *testing.T) {
	t.Parallel()

	govpx := frameRowFull(0, 60, "key", true, 0xdeadbeef, true, false) + "\n"
	libvpx := frameRowFull(0, 60, "key", true, 0xdeadbeef, true, true) + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	got := make(map[string]Divergence, len(div))
	for _, d := range div {
		got[divKey(d)] = d
	}
	d, ok := got["row=0/field=default_coef_reset"]
	if !ok {
		t.Fatalf("missing default_coef_reset divergence; got: %v", divKeys(div))
	}
	if d.RowKind != "frame" {
		t.Errorf("RowKind=%q want frame", d.RowKind)
	}
	if d.FrameIndex != 0 {
		t.Errorf("FrameIndex=%d want 0", d.FrameIndex)
	}
	if gv, _ := d.Govpx.(bool); gv {
		t.Errorf("Govpx=%v want false", d.Govpx)
	}
	if lv, _ := d.Libvpx.(bool); !lv {
		t.Errorf("Libvpx=%v want true", d.Libvpx)
	}
	// Sanity: the matching refresh_entropy_probs field must NOT show up
	// as a divergence since both sides emit true.
	if _, hasRefresh := got["row=0/field=refresh_entropy_probs"]; hasRefresh {
		t.Errorf("unexpected refresh_entropy_probs divergence: %+v", got["row=0/field=refresh_entropy_probs"])
	}
}

// TestCompareOracleTracesDetectsLoopFilterDeltaDivergence feeds two streams
// where the per-frame loop-filter delta fields diverge: govpx emits the
// libvpx default ref/mode deltas with mode_ref_lf_delta_enabled=true and
// mode_ref_lf_delta_update=true, while libvpx emits zeroed deltas with
// the enable/update flags off. The comparator must surface the
// sharpness_level, ref_lf_deltas, mode_lf_deltas, mode_ref_lf_delta_enabled,
// and mode_ref_lf_delta_update mismatches with RowKind == "frame".
func TestCompareOracleTracesDetectsLoopFilterDeltaDivergence(t *testing.T) {
	t.Parallel()

	govpx := frameRowLF(0, 60, "inter", true, 0xdeadbeef, true, false,
		3, [4]int{2, 0, -2, -2}, [4]int{4, -2, 2, 4}, true, true) + "\n"
	libvpx := frameRowLF(0, 60, "inter", true, 0xdeadbeef, true, false,
		0, [4]int{0, 0, 0, 0}, [4]int{0, 0, 0, 0}, false, false) + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	got := make(map[string]Divergence, len(div))
	for _, d := range div {
		got[divKey(d)] = d
	}
	wantKeys := []string{
		"row=0/field=sharpness_level",
		"row=0/field=ref_lf_deltas",
		"row=0/field=mode_lf_deltas",
		"row=0/field=mode_ref_lf_delta_enabled",
		"row=0/field=mode_ref_lf_delta_update",
	}
	for _, key := range wantKeys {
		d, ok := got[key]
		if !ok {
			t.Errorf("missing divergence for %s; got divergences: %v", key, divKeys(div))
			continue
		}
		if d.RowKind != "frame" {
			t.Errorf("%s: RowKind=%q want frame", key, d.RowKind)
		}
		if d.FrameIndex != 0 {
			t.Errorf("%s: FrameIndex=%d want 0", key, d.FrameIndex)
		}
	}
	// Spot-check the sharpness payload reflects the actual feed values.
	sharp := got["row=0/field=sharpness_level"]
	if gv, _ := sharp.Govpx.(float64); gv != 3 {
		t.Errorf("row=0/field=sharpness_level: Govpx=%v want 3", sharp.Govpx)
	}
	if lv, _ := sharp.Libvpx.(float64); lv != 0 {
		t.Errorf("row=0/field=sharpness_level: Libvpx=%v want 0", sharp.Libvpx)
	}
	// mode_ref_lf_delta_enabled must report a typed bool divergence.
	enabled := got["row=0/field=mode_ref_lf_delta_enabled"]
	if gv, _ := enabled.Govpx.(bool); !gv {
		t.Errorf("row=0/field=mode_ref_lf_delta_enabled: Govpx=%v want true", enabled.Govpx)
	}
	if lv, _ := enabled.Libvpx.(bool); lv {
		t.Errorf("row=0/field=mode_ref_lf_delta_enabled: Libvpx=%v want false", enabled.Libvpx)
	}
}

// mbRowImprovedMVJSON emits an "mb" row with the improved-MV predictor
// fields populated. Mirrors the schema in oracleTraceMBRow
// (encoder_oracle_trace.go) and the libvpx-side emit in
// build_vpxenc_oracle.sh.
func mbRowImprovedMVJSON(frameIndex, mbRow, mbCol int, mode, ref string, mvRow, mvCol int, skip bool, eobSum int, improvedStart bool, improvedNearSAD, improvedRow, improvedCol, improvedSR int) string {
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
		fmt.Sprintf("\"eob_sum\":%d", eobSum),
		fmt.Sprintf("\"improved_mv_start\":%t", improvedStart),
		fmt.Sprintf("\"improved_mv_near_sadidx\":%d", improvedNearSAD),
		fmt.Sprintf("\"improved_mv_row\":%d", improvedRow),
		fmt.Sprintf("\"improved_mv_col\":%d", improvedCol),
		fmt.Sprintf("\"improved_mv_sr\":%d}", improvedSR),
	}, ",")
}

// TestCompareOracleTracesDetectsImprovedMVPredictorDivergence feeds two
// streams whose only difference on a NEWMV macroblock is the improved-MV
// predictor companions: govpx records improved_mv_near_sadidx=3 with a
// (16, -16) predictor and sr=3, while libvpx (modelling the patched
// vpxenc-oracle binary) reports near_sadidx=4 / row=20 / col=-12 / sr=2.
// The comparator must surface every per-field divergence with
// RowKind == "mb" so a future libvpx-side regression in vp8_mv_pred or in
// the matched-rank capture is caught even when the chosen MV / mode /
// eob payload still match.
func TestCompareOracleTracesDetectsImprovedMVPredictorDivergence(t *testing.T) {
	t.Parallel()

	govpx := strings.Join([]string{
		frameRow(0, 60, true, 0xdeadbeef),
		mbRowImprovedMVJSON(0, 0, 0, "NEWMV", "LAST_FRAME",
			16, -16, false, 4,
			true, 3, 16, -16, 3),
	}, "\n") + "\n"

	libvpx := strings.Join([]string{
		frameRow(0, 60, true, 0xdeadbeef),
		mbRowImprovedMVJSON(0, 0, 0, "NEWMV", "LAST_FRAME",
			16, -16, false, 4,
			true, 4, 20, -12, 2),
	}, "\n") + "\n"

	div, err := CompareOracleTraces(strings.NewReader(govpx), strings.NewReader(libvpx), CompareOptions{})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) == 0 {
		t.Fatalf("expected divergences, got none")
	}
	got := make(map[string]Divergence, len(div))
	for _, d := range div {
		got[divKey(d)] = d
	}
	wantKeys := []string{
		"row=1/field=improved_mv_near_sadidx",
		"row=1/field=improved_mv_row",
		"row=1/field=improved_mv_col",
		"row=1/field=improved_mv_sr",
	}
	for _, key := range wantKeys {
		d, ok := got[key]
		if !ok {
			t.Errorf("missing divergence for %s; got divergences: %v", key, divKeys(div))
			continue
		}
		if d.RowKind != "mb" {
			t.Errorf("%s: RowKind=%q want mb", key, d.RowKind)
		}
		if d.FrameIndex != 0 {
			t.Errorf("%s: FrameIndex=%d want 0", key, d.FrameIndex)
		}
		if d.MBRow != 0 || d.MBCol != 0 {
			t.Errorf("%s: coords=(%d,%d) want (0,0)", key, d.MBRow, d.MBCol)
		}
	}
	// Spot-check the near_sadidx payload reflects the actual feed values.
	near := got["row=1/field=improved_mv_near_sadidx"]
	if gv, _ := near.Govpx.(float64); gv != 3 {
		t.Errorf("row=1/field=improved_mv_near_sadidx: Govpx=%v want 3", near.Govpx)
	}
	if lv, _ := near.Libvpx.(float64); lv != 4 {
		t.Errorf("row=1/field=improved_mv_near_sadidx: Libvpx=%v want 4", near.Libvpx)
	}
	// improved_mv_start matches on both sides, so it must NOT show up as
	// a divergence even though the four numeric companions diverge.
	if _, hasStart := got["row=1/field=improved_mv_start"]; hasStart {
		t.Errorf("unexpected improved_mv_start divergence: %+v", got["row=1/field=improved_mv_start"])
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
