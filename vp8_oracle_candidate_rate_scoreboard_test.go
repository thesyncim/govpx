//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// candidateRateScoreboardSnapshot is the per-fixture summary persisted to
// testdata. Each scalar is computed in lockstep across govpx and libvpx
// inter_candidate rows (matched on frame_index, mb_row, mb_col, picker,
// mode_index, ref_slot). Aggregate-frame match counts use the per-frame
// sum of |rate_govpx - rate_libvpx| across all matched candidates.
type candidateRateScoreboardSnapshot struct {
	TotalCandidates             int     `json:"total_candidates"`
	MatchedCandidates           int     `json:"matched_candidates"`
	UnmatchedGovpxCandidates    int     `json:"unmatched_govpx_candidates"`
	UnmatchedLibvpxCandidates   int     `json:"unmatched_libvpx_candidates"`
	MeanAbsRateDeltaBits        float64 `json:"mean_abs_rate_delta_bits"`
	MaxAbsRateDeltaBits         int     `json:"max_abs_rate_delta_bits"`
	FramesCompared              int     `json:"frames_compared"`
	FramesWithAggregateMatch    int     `json:"frames_with_aggregate_match"`
	FramesWithPerCandidateMatch int     `json:"frames_with_per_candidate_match"`
	PerCandidateMatchRate       float64 `json:"per_candidate_match_rate"`
}

type candidateRateScoreboardBaseline struct {
	Fixtures map[string]candidateRateScoreboardSnapshot `json:"fixtures"`
}

// TestVP8OracleCandidateRateScoreboard captures per-candidate rate scalars
// from both encoders and reports a per-frame match rate. The fixture
// matches TestVP8OracleTraceInterCandidateCompare so anyone looking at
// candidate-row divergences can pivot directly to a rate-only summary.
//
// govpx records `rate` as `rd.rate2` (full mode rate including ref-frame
// cost, mb_skip cost, MV cost, intra mode cost, rate_y, rate_uv) at the
// candidate evaluation point. libvpx records the same `rd.rate2` from the
// rdopt picker (vp8/encoder/rdopt.c, calculate_final_rd_costs tail) and
// the equivalent `rate2` from the fast picker (vp8/encoder/pickinter.c,
// before update_best_mode). Both encoders flush these rows at frame
// commit time; this test walks them in lockstep and tabulates the deltas.
func TestVP8OracleCandidateRateScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle trace scoreboard")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 4
	)
	opts := EncoderOptions{
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
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	type fixture struct {
		name      string
		opts      EncoderOptions
		extraArgs []string
	}
	fixtures := []fixture{
		{name: "good-quality-rd", opts: opts, extraArgs: []string{"--end-usage=vbr"}},
	}

	current := candidateRateScoreboardBaseline{Fixtures: map[string]candidateRateScoreboardSnapshot{}}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			govpxTrace := captureGovpxEncoderTrace(t, fx.opts, sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "candidate-rate-"+fx.name, fx.opts, targetKbps, sources, fx.extraArgs)
			snap := summarizeCandidateRateScoreboard(t, govpxTrace, libvpxTrace)
			current.Fixtures[fx.name] = snap

			t.Logf("scoreboard %s: total=%d matched=%d (%.4f match) mean_abs=%.2f max_abs=%d frames(agg=%d per_cand=%d / %d)",
				fx.name,
				snap.TotalCandidates, snap.MatchedCandidates, snap.PerCandidateMatchRate,
				snap.MeanAbsRateDeltaBits, snap.MaxAbsRateDeltaBits,
				snap.FramesWithAggregateMatch, snap.FramesWithPerCandidateMatch, snap.FramesCompared)
			t.Logf("\n%s", formatCandidateRateScoreboardTable(fx.name, snap))
		})
	}

	baselinePath := "testdata/candidate_rate_scoreboard_baseline.json"
	base, wrote := coracletest.ReadOrWriteJSONBaseline(t, baselinePath, current)
	if wrote {
		return
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
		// Per-candidate match rate must not regress more than 0.02
		// (absolute) below baseline. Mean abs delta must not balloon
		// more than 25% above baseline.
		if got.PerCandidateMatchRate+0.02 < want.PerCandidateMatchRate {
			t.Errorf("%s: per_candidate_match_rate=%.4f below baseline %.4f (allowed slack 0.02); rerun with GOVPX_UPDATE_BASELINES=1 if intended",
				fx.name, got.PerCandidateMatchRate, want.PerCandidateMatchRate)
		}
		meanLimit := want.MeanAbsRateDeltaBits * 1.25
		if want.MeanAbsRateDeltaBits < 1 {
			meanLimit = want.MeanAbsRateDeltaBits + 4
		}
		if got.MeanAbsRateDeltaBits > meanLimit {
			t.Errorf("%s: mean_abs_rate_delta_bits=%.2f exceeds baseline %.2f * 1.25 = %.2f; rerun with GOVPX_UPDATE_BASELINES=1 if intended",
				fx.name, got.MeanAbsRateDeltaBits, want.MeanAbsRateDeltaBits, meanLimit)
		}
		if got.FramesWithAggregateMatch < want.FramesWithAggregateMatch {
			t.Errorf("%s: frames_with_aggregate_match=%d below baseline %d; rerun with GOVPX_UPDATE_BASELINES=1 if intended",
				fx.name, got.FramesWithAggregateMatch, want.FramesWithAggregateMatch)
		}
		if got.FramesWithPerCandidateMatch < want.FramesWithPerCandidateMatch {
			t.Errorf("%s: frames_with_per_candidate_match=%d below baseline %d; rerun with GOVPX_UPDATE_BASELINES=1 if intended",
				fx.name, got.FramesWithPerCandidateMatch, want.FramesWithPerCandidateMatch)
		}
	}
}

// candidateRateRow is a minimal projection of inter_candidate trace rows
// for the rate scoreboard. The match key is the tuple
// (frame_index, mb_row, mb_col, picker, mode_index, ref_slot).
type candidateRateRow struct {
	FrameIndex int64
	MBRow      int
	MBCol      int
	Picker     string
	ModeIndex  int
	RefSlot    int
	Rate       int
}

func parseCandidateRateRows(t *testing.T, trace []byte) []candidateRateRow {
	t.Helper()
	var rows []candidateRateRow
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typ, _ := row["type"].(string)
		if typ != "inter_candidate" {
			continue
		}
		// The libvpx oracle (internal/coracle/build_vpxenc_oracle.sh
		// rd_emit_anchor / pickinter emit_anchor) emits an inter_candidate
		// row only when the candidate's RD score evaluates to a finite
		// value (this_rd != INT_MAX). govpx records extra pre-RD outcomes
		// (`skipped_no_ref`, `skipped_threshold`, etc.) for diagnostics.
		// Restrict the rate scoreboard to libvpx's emission contract so
		// the per-frame match metric is computed on the same candidate
		// set on both sides.
		if outcome, _ := row["outcome"].(string); outcome != "tested" {
			continue
		}
		out := candidateRateRow{
			FrameIndex: traceInt64Field(row, "frame_index"),
			MBRow:      int(traceInt64Field(row, "mb_row")),
			MBCol:      int(traceInt64Field(row, "mb_col")),
			Picker:     traceStringField(row, "picker"),
			ModeIndex:  int(traceInt64Field(row, "mode_index")),
			RefSlot:    int(traceInt64Field(row, "ref_slot")),
			Rate:       int(traceInt64Field(row, "rate")),
		}
		rows = append(rows, out)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan candidate rate rows: %v", err)
	}
	return rows
}

func traceInt64Field(row map[string]any, key string) int64 {
	switch v := row[key].(type) {
	case float64:
		return int64(v)
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return n
		}
		f, err := v.Float64()
		if err == nil {
			return int64(f)
		}
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

func traceStringField(row map[string]any, key string) string {
	if s, ok := row[key].(string); ok {
		return s
	}
	return ""
}

type candidateRateKey struct {
	FrameIndex int64
	MBRow      int
	MBCol      int
	Picker     string
	ModeIndex  int
	RefSlot    int
}

func summarizeCandidateRateScoreboard(t *testing.T, govpxTrace, libvpxTrace []byte) candidateRateScoreboardSnapshot {
	t.Helper()
	govpxRows := parseCandidateRateRows(t, govpxTrace)
	libvpxRows := parseCandidateRateRows(t, libvpxTrace)
	libIndex := make(map[candidateRateKey]int, len(libvpxRows))
	for i, r := range libvpxRows {
		key := candidateRateKey{r.FrameIndex, r.MBRow, r.MBCol, r.Picker, r.ModeIndex, r.RefSlot}
		// Last-write-wins: identical keys can occur if the picker
		// re-evaluates a mode. Existing scoreboard data shows this is
		// rare; the comparator absorbs it by overwriting.
		libIndex[key] = i
	}
	govpxIndex := make(map[candidateRateKey]struct{}, len(govpxRows))
	for _, r := range govpxRows {
		key := candidateRateKey{r.FrameIndex, r.MBRow, r.MBCol, r.Picker, r.ModeIndex, r.RefSlot}
		govpxIndex[key] = struct{}{}
	}

	type frameAccum struct {
		matched         int
		perCandidateOK  bool
		aggregateDelta  int64
		anyDeltaCounted bool
	}
	frames := map[int64]*frameAccum{}

	matched := 0
	totalAbs := int64(0)
	maxAbs := int64(0)
	for _, gr := range govpxRows {
		acc, ok := frames[gr.FrameIndex]
		if !ok {
			acc = &frameAccum{perCandidateOK: true}
			frames[gr.FrameIndex] = acc
		}
		key := candidateRateKey{gr.FrameIndex, gr.MBRow, gr.MBCol, gr.Picker, gr.ModeIndex, gr.RefSlot}
		li, has := libIndex[key]
		if !has {
			acc.perCandidateOK = false
			continue
		}
		matched++
		delta := int64(gr.Rate - libvpxRows[li].Rate)
		abs := delta
		if abs < 0 {
			abs = -abs
		}
		totalAbs += abs
		if abs > maxAbs {
			maxAbs = abs
		}
		if abs > 64 {
			acc.perCandidateOK = false
		}
		acc.matched++
		acc.aggregateDelta += delta
		acc.anyDeltaCounted = true
	}

	unmatchedGovpx := 0
	for _, gr := range govpxRows {
		key := candidateRateKey{gr.FrameIndex, gr.MBRow, gr.MBCol, gr.Picker, gr.ModeIndex, gr.RefSlot}
		if _, ok := libIndex[key]; !ok {
			unmatchedGovpx++
		}
	}
	unmatchedLibvpx := 0
	for _, lr := range libvpxRows {
		key := candidateRateKey{lr.FrameIndex, lr.MBRow, lr.MBCol, lr.Picker, lr.ModeIndex, lr.RefSlot}
		if _, ok := govpxIndex[key]; !ok {
			unmatchedLibvpx++
		}
	}

	totalCandidates := max(len(libvpxRows), len(govpxRows))

	mean := 0.0
	if matched > 0 {
		mean = float64(totalAbs) / float64(matched)
	}
	matchRate := 0.0
	if totalCandidates > 0 {
		matchRate = float64(matched) / float64(totalCandidates)
	}

	framesCompared := 0
	framesAgg := 0
	framesPer := 0
	for _, acc := range frames {
		framesCompared++
		agg := acc.aggregateDelta
		if agg < 0 {
			agg = -agg
		}
		if acc.anyDeltaCounted && agg <= 256 {
			framesAgg++
		}
		if acc.perCandidateOK && acc.matched > 0 {
			framesPer++
		}
	}

	// Round mean to two decimal places for stable JSON.
	mean = math.Round(mean*100) / 100
	matchRate = math.Round(matchRate*10000) / 10000

	return candidateRateScoreboardSnapshot{
		TotalCandidates:             totalCandidates,
		MatchedCandidates:           matched,
		UnmatchedGovpxCandidates:    unmatchedGovpx,
		UnmatchedLibvpxCandidates:   unmatchedLibvpx,
		MeanAbsRateDeltaBits:        mean,
		MaxAbsRateDeltaBits:         int(maxAbs),
		FramesCompared:              framesCompared,
		FramesWithAggregateMatch:    framesAgg,
		FramesWithPerCandidateMatch: framesPer,
		PerCandidateMatchRate:       matchRate,
	}
}

func formatCandidateRateScoreboardTable(name string, snap candidateRateScoreboardSnapshot) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "### %s\n", name)
	fmt.Fprintf(&buf, "| metric | value |\n|---|---|\n")
	type row struct {
		key   string
		value string
	}
	rows := []row{
		{"total_candidates", fmt.Sprintf("%d", snap.TotalCandidates)},
		{"matched_candidates", fmt.Sprintf("%d", snap.MatchedCandidates)},
		{"unmatched_govpx_candidates", fmt.Sprintf("%d", snap.UnmatchedGovpxCandidates)},
		{"unmatched_libvpx_candidates", fmt.Sprintf("%d", snap.UnmatchedLibvpxCandidates)},
		{"mean_abs_rate_delta_bits", fmt.Sprintf("%.2f", snap.MeanAbsRateDeltaBits)},
		{"max_abs_rate_delta_bits", fmt.Sprintf("%d", snap.MaxAbsRateDeltaBits)},
		{"frames_compared", fmt.Sprintf("%d", snap.FramesCompared)},
		{"frames_with_aggregate_match", fmt.Sprintf("%d", snap.FramesWithAggregateMatch)},
		{"frames_with_per_candidate_match", fmt.Sprintf("%d", snap.FramesWithPerCandidateMatch)},
		{"per_candidate_match_rate", fmt.Sprintf("%.4f", snap.PerCandidateMatchRate)},
	}
	sort.SliceStable(rows, func(i, j int) bool { return false })
	for _, r := range rows {
		fmt.Fprintf(&buf, "| %s | %s |\n", r.key, r.value)
	}
	return buf.String()
}
