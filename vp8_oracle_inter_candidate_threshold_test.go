//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8OracleInterCandidateThresholdEvolution drives the same fixture
// through govpx and the libvpx oracle across a band of cpu_used values
// (negative for explicit-speed realtime, positive for the auto-select
// realtime path, and good-quality for the RD picker) and compares the
// per-MB-per-mode `threshold` value emitted by both pickers. libvpx's
// pickinter.c (fast picker) and rdopt.c (RD picker) raise/lower
// `rd_threshes[mode_index]` per MB from `rd_thresh_mult[]`, with
// frame-level resets routed through vp8_initialize_rd_consts and
// vp8_set_speed_features in onyx_if.c. govpx mirrors that mutation
// state machine in vp8_encoder_reconstruct.go (interModeRDThresholds*,
// raise/lowerInterRDThreshold*, beginInterRDModeDecisionFrame). This
// test is the parity sentinel for that state machine: any divergence
// in the per-frame `rd_threshes` evolution surfaces as a `threshold`
// field divergence in the projected JSONL stream.
//
// The test is gated by GOVPX_WITH_ORACLE so the standard test target
// stays green; it only runs in the oracle harness. When the streams
// stay in sync (no row-identity divergences) we hard-fail on threshold
// drift, which catches the R9-6 BPred-raise gap (govpx 97500 vs
// libvpx 136980 at frame=1 mb=(3,3) with good-quality-vbr-cpu3, fixed
// by mirroring libvpx's `this_rd == INT_MAX -> raise rd_thresh_mult`
// path on the intra/B_PRED RD branch). When upstream desync (q_index,
// mb ordering, mode picker drift) makes the streams shift relative to
// each other we log the histogram for diagnosis but do not fail, so
// this test does not regress on shifts already covered by the
// existing TestVP8OracleTrace* / TestVP8Oracle*Scoreboard suite.
func TestVP8OracleInterCandidateThresholdEvolution(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle threshold-evolution comparison")
	}
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

	cases := []struct {
		name      string
		deadline  Deadline
		cpuUsed   int
		extraArgs []string
	}{
		// Negative cpu_used pins libvpx Speed exactly per
		// vp8/encoder/encodeframe.c:686-687 (`cpi->Speed = -cpu_used`),
		// bypassing vp8_auto_select_speed. These bands isolate the
		// mutation-step parity question from the auto-select drift.
		{name: "realtime-cbr-cpu-neg7", deadline: DeadlineRealtime, cpuUsed: -7, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-neg8", deadline: DeadlineRealtime, cpuUsed: -8, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-neg10", deadline: DeadlineRealtime, cpuUsed: -10, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-neg12", deadline: DeadlineRealtime, cpuUsed: -12, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-neg14", deadline: DeadlineRealtime, cpuUsed: -14, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-neg16", deadline: DeadlineRealtime, cpuUsed: -16, extraArgs: []string{"--end-usage=cbr"}},
		// Positive cpu_used engages vp8_auto_select_speed which evolves
		// cpi->Speed frame-by-frame from a 4-seed; the threshold and
		// mode_check_freq tables are re-derived from the auto-selected
		// Speed at frame init (see libvpxAutoSelectSpeed +
		// libvpxCPUUsed / libvpxAutoSelectSpeedActive). Match here
		// confirms the per-frame remap also lands byte-identical.
		{name: "realtime-cbr-cpu-pos8", deadline: DeadlineRealtime, cpuUsed: 8, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-pos12", deadline: DeadlineRealtime, cpuUsed: 12, extraArgs: []string{"--end-usage=cbr"}},
		// Good-quality + cpu_used in [-3, 3] takes the RD picker
		// (selectRDInterFrameModeDecision -> rdopt.c). RD picker uses a
		// `>>2` best-mode lower vs `>>3` for the fast picker; sweep both
		// extremes so any RD-only mutation drift surfaces here.
		{name: "good-quality-vbr-cpu0", deadline: DeadlineGoodQuality, cpuUsed: 0, extraArgs: []string{"--end-usage=vbr"}},
		{name: "good-quality-vbr-cpu3", deadline: DeadlineGoodQuality, cpuUsed: 3, extraArgs: []string{"--end-usage=vbr"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rcMode := RateControlCBR
			for _, arg := range tc.extraArgs {
				if arg == "--end-usage=vbr" {
					rcMode = RateControlVBR
				}
			}
			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   rcMode,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
				KeyFrameInterval:  999,
			}

			govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-thresh-"+tc.name, opts, targetKbps, sources, tc.extraArgs)
			govpxProjected, err := coracle.ProjectVP8InterCandidateThresholdTrace(govpxTrace)
			if err != nil {
				t.Fatalf("ProjectVP8InterCandidateThresholdTrace(govpx): %v", err)
			}
			libvpxProjected, err := coracle.ProjectVP8InterCandidateThresholdTrace(libvpxTrace)
			if err != nil {
				t.Fatalf("ProjectVP8InterCandidateThresholdTrace(libvpx): %v", err)
			}

			div, err := coracle.CompareOracleTraces(bytes.NewReader(govpxProjected), bytes.NewReader(libvpxProjected), coracle.CompareOptions{
				MaxDivergences: 4096,
			})
			if err != nil {
				t.Fatalf("CompareOracleTraces returned error: %v", err)
			}

			govpxLines := splitNonEmptyLines(govpxProjected)
			libvpxLines := splitNonEmptyLines(libvpxProjected)
			totalRows := max(len(libvpxLines), len(govpxLines))

			fieldHist := map[string]int{}
			perFrameHist := map[int64]int{}
			firstByField := map[string]coracle.Divergence{}
			for _, d := range div {
				if d.Field != "" {
					fieldHist[d.Field]++
					if _, ok := firstByField[d.Field]; !ok {
						firstByField[d.Field] = d
					}
				}
				perFrameHist[d.FrameIndex]++
			}

			t.Logf("%s: total_inter_candidate_rows=%d divergences=%d",
				tc.name, totalRows, len(div))
			if len(div) == 0 {
				return
			}

			// Identifying fields whose divergence means the streams have
			// shifted relative to each other (different MB / mode order
			// already reported by other scoreboards). When any of these
			// fire, threshold mismatches downstream are byproducts of the
			// shift, not threshold-mutation drift, so we keep the failure
			// output descriptive without asserting the threshold-only path.
			// logging.
			rowSync := true
			for _, ident := range []string{"mb_row", "mb_col", "frame_index", "mode_index", "mode", "ref_slot", "picker"} {
				if fieldHist[ident] > 0 {
					rowSync = false
					break
				}
			}
			thresholdHits := fieldHist["threshold"]

			t.Logf("field histogram:")
			fields := make([]string, 0, len(fieldHist))
			for f := range fieldHist {
				fields = append(fields, f)
			}
			sort.Slice(fields, func(i, j int) bool {
				if fieldHist[fields[i]] != fieldHist[fields[j]] {
					return fieldHist[fields[i]] > fieldHist[fields[j]]
				}
				return fields[i] < fields[j]
			})
			for _, f := range fields {
				t.Logf("  %s = %d", f, fieldHist[f])
			}

			t.Logf("per-frame divergence histogram:")
			frames := make([]int64, 0, len(perFrameHist))
			for fi := range perFrameHist {
				frames = append(frames, fi)
			}
			slices.Sort(frames)
			for _, fi := range frames {
				t.Logf("  frame=%d count=%d", fi, perFrameHist[fi])
			}

			t.Logf("first divergence per field:")
			for _, f := range fields {
				d := firstByField[f]
				t.Logf("  field=%s row=%d frame=%d mb=(%d,%d) govpx=%v libvpx=%v",
					f, d.RowIndex, d.FrameIndex, d.MBRow, d.MBCol, d.Govpx, d.Libvpx)
			}
			// Dump 3 rows around the first threshold divergence on each
			// side so we can see the mode/ref/score context. Helps
			// localize which mode_index is involved.
			if d, ok := firstByField["threshold"]; ok {
				dumpThresholdContext(t, "govpx", govpxLines, d.RowIndex)
				dumpThresholdContext(t, "libvpx", libvpxLines, d.RowIndex)
				// Also dump every prior occurrence of the same mode_index
				// in the same frame so we can reconstruct the threshold-
				// mutation history that led to the divergence.
				dumpPriorModeIndex(t, "govpx", govpxLines, d.FrameIndex, int(d.RowIndex))
				dumpPriorModeIndex(t, "libvpx", libvpxLines, d.FrameIndex, int(d.RowIndex))
				// Dump the RATE row for the diverging frame so we can
				// rule in/out a Q-index drift as the cause: the
				// `(baseline >> 7) * rd_thresh_mult` formula has
				// `baseline = sf.thresh_mult * q [/100]` so a different
				// q_index makes the same untouched mode produce a
				// different baseline.
				dumpRateRow(t, "govpx", govpxTrace, d.FrameIndex)
				dumpRateRow(t, "libvpx", libvpxTrace, d.FrameIndex)
			}

			if rowSync && thresholdHits > 0 {
				t.Errorf("%s: %d threshold-field divergences with stream-identity in sync; %d total divergences. This is the regression mode for the rd_thresh_mult mutation state machine -- expected to be 0 with the byte-identical mutation steps that govpx mirrors from libvpx pickinter.c / rdopt.c.",
					tc.name, thresholdHits, len(div))
			} else {
				t.Logf("%s: stream desync (row-identity divergences present); skipping threshold-only assertion",
					tc.name)
			}
		})
	}
}

func dumpThresholdContext(t *testing.T, side string, lines [][]byte, rowIndex int) {
	t.Helper()
	from := max(rowIndex-1, 0)
	to := min(rowIndex+2, len(lines))
	for i := from; i < to; i++ {
		t.Logf("%s row=%d %s", side, i, lines[i])
	}
}

func dumpRateRow(t *testing.T, side string, trace []byte, wantFrame int64) {
	t.Helper()
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "rate" && typ != "frame" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		if int64(fi) == wantFrame {
			t.Logf("%s rate/frame frame=%d %s", side, wantFrame, scan.Bytes())
		}
	}
}

func dumpPriorModeIndex(t *testing.T, side string, lines [][]byte, wantFrame int64, divergenceRowIndex int) {
	t.Helper()
	if divergenceRowIndex >= len(lines) {
		return
	}
	var want struct {
		FrameIndex float64 `json:"frame_index"`
		ModeIndex  float64 `json:"mode_index"`
	}
	if err := json.Unmarshal(lines[divergenceRowIndex], &want); err != nil {
		return
	}
	for i, raw := range lines {
		if i > divergenceRowIndex {
			break
		}
		var row struct {
			FrameIndex float64 `json:"frame_index"`
			ModeIndex  float64 `json:"mode_index"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			continue
		}
		if int64(row.FrameIndex) == wantFrame && row.ModeIndex == want.ModeIndex {
			t.Logf("%s prior mode_index=%.0f at row=%d %s", side, row.ModeIndex, i, raw)
		}
	}
}
