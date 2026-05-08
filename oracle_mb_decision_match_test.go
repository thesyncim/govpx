package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestOracleInterDecisionMatchRate captures per-MB picker decisions for govpx
// vs libvpx on three fixtures and reports the per-field match-rate. A
// regression baseline lives in testdata/mb_match_rate_baseline.json: each
// match-rate must remain within 2 percentage points of the recorded baseline.
//
// Bootstrap with GOVPX_UPDATE_BASELINES=1 to seed the file.
func TestOracleInterDecisionMatchRate(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle MB decision match scoreboard")
	}
	vpxencOracle := findVpxencOracle(t)

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

	type fixtureSpec struct {
		Name      string
		Deadline  Deadline
		CpuUsed   int
		RC        RateControlMode
		ExtraArgs []string
	}
	specs := []fixtureSpec{
		{Name: "good-cpu3-vbr", Deadline: DeadlineGoodQuality, CpuUsed: 3, RC: RateControlVBR, ExtraArgs: []string{"--end-usage=vbr"}},
		{Name: "rt-cpu8-cbr", Deadline: DeadlineRealtime, CpuUsed: 8, RC: RateControlCBR, ExtraArgs: []string{"--end-usage=cbr"}},
		{Name: "rt-cpu0-cbr", Deadline: DeadlineRealtime, CpuUsed: 0, RC: RateControlCBR, ExtraArgs: []string{"--end-usage=cbr"}},
	}

	type fixtureMBReport struct {
		Name              string  `json:"name"`
		MBTotal           int     `json:"mb_total"`
		ModeMatchPct      float64 `json:"mode_match_pct"`
		RefFrameMatchPct  float64 `json:"ref_frame_match_pct"`
		MVMatchPct        float64 `json:"mv_match_pct"`
		SkipMatchPct      float64 `json:"skip_match_pct"`
		SegmentIDMatchPct float64 `json:"segment_id_match_pct"`
		EOBSumMatchPct    float64 `json:"eob_sum_match_pct"`
	}

	type baselineEntry struct {
		ModeMatchPct      float64 `json:"mode_match_pct"`
		RefFrameMatchPct  float64 `json:"ref_frame_match_pct"`
		MVMatchPct        float64 `json:"mv_match_pct"`
		SkipMatchPct      float64 `json:"skip_match_pct"`
		SegmentIDMatchPct float64 `json:"segment_id_match_pct"`
		EOBSumMatchPct    float64 `json:"eob_sum_match_pct"`
	}
	type baselineFile struct {
		Fixtures map[string]baselineEntry `json:"fixtures"`
	}

	baselinePath := filepath.Join("testdata", "mb_match_rate_baseline.json")
	updateBaselines := os.Getenv("GOVPX_UPDATE_BASELINES") == "1"

	var baseline baselineFile
	baselineExists := false
	if !updateBaselines {
		raw, err := os.ReadFile(baselinePath)
		if err == nil {
			if err := json.Unmarshal(raw, &baseline); err != nil {
				t.Fatalf("baseline %s: %v", baselinePath, err)
			}
			baselineExists = true
		} else if !os.IsNotExist(err) {
			t.Fatalf("read baseline %s: %v", baselinePath, err)
		}
	}

	currentBaseline := baselineFile{Fixtures: make(map[string]baselineEntry, len(specs))}
	reports := make([]fixtureMBReport, 0, len(specs))

	for _, spec := range specs {
		spec := spec
		t.Run(spec.Name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   spec.RC,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          spec.Deadline,
				CpuUsed:           spec.CpuUsed,
				KeyFrameInterval:  999,
			}
			govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "mbmatch-"+spec.Name, opts, targetKbps, sources, spec.ExtraArgs)

			govpxFrames := indexInterFrameMBs(t, govpxTrace)
			libvpxFrames := indexInterFrameMBs(t, libvpxTrace)

			var modeMatch, refMatch, mvMatch, skipMatch, segMatch, eobMatch, total int
			for frameIdx, gMBs := range govpxFrames {
				lMBs, ok := libvpxFrames[frameIdx]
				if !ok {
					continue
				}
				for key, gRow := range gMBs {
					lRow, ok := lMBs[key]
					if !ok {
						continue
					}
					total++
					if gRow.Mode == lRow.Mode {
						modeMatch++
					}
					if gRow.RefFrame == lRow.RefFrame {
						refMatch++
					}
					if gRow.MVRow == lRow.MVRow && gRow.MVCol == lRow.MVCol {
						mvMatch++
					}
					if gRow.Skip == lRow.Skip {
						skipMatch++
					}
					if gRow.SegmentID == lRow.SegmentID {
						segMatch++
					}
					if gRow.EOBSum == lRow.EOBSum {
						eobMatch++
					}
				}
			}

			report := fixtureMBReport{
				Name:    spec.Name,
				MBTotal: total,
			}
			if total > 0 {
				report.ModeMatchPct = pct(modeMatch, total)
				report.RefFrameMatchPct = pct(refMatch, total)
				report.MVMatchPct = pct(mvMatch, total)
				report.SkipMatchPct = pct(skipMatch, total)
				report.SegmentIDMatchPct = pct(segMatch, total)
				report.EOBSumMatchPct = pct(eobMatch, total)
			} else {
				t.Errorf("%s: zero inter MBs compared (govpx_frames=%d libvpx_frames=%d)",
					spec.Name, len(govpxFrames), len(libvpxFrames))
			}

			t.Logf("inter MB match-rate %s (mb_total=%d):\n%s", spec.Name, total, formatMBMatchTable(report))

			currentBaseline.Fixtures[spec.Name] = baselineEntry{
				ModeMatchPct:      report.ModeMatchPct,
				RefFrameMatchPct:  report.RefFrameMatchPct,
				MVMatchPct:        report.MVMatchPct,
				SkipMatchPct:      report.SkipMatchPct,
				SegmentIDMatchPct: report.SegmentIDMatchPct,
				EOBSumMatchPct:    report.EOBSumMatchPct,
			}
			reports = append(reports, report)

			if !updateBaselines && baselineExists {
				prev, ok := baseline.Fixtures[spec.Name]
				if !ok {
					t.Errorf("baseline %s missing fixture %q (rerun with GOVPX_UPDATE_BASELINES=1)", baselinePath, spec.Name)
					return
				}
				checks := []struct {
					name string
					cur  float64
					base float64
				}{
					{"mode_match_pct", report.ModeMatchPct, prev.ModeMatchPct},
					{"ref_frame_match_pct", report.RefFrameMatchPct, prev.RefFrameMatchPct},
					{"mv_match_pct", report.MVMatchPct, prev.MVMatchPct},
					{"skip_match_pct", report.SkipMatchPct, prev.SkipMatchPct},
					{"segment_id_match_pct", report.SegmentIDMatchPct, prev.SegmentIDMatchPct},
					{"eob_sum_match_pct", report.EOBSumMatchPct, prev.EOBSumMatchPct},
				}
				for _, c := range checks {
					if c.cur < c.base-2.0 {
						t.Errorf("MB match-rate regression %s/%s: current=%.2f%% baseline=%.2f%% drop=%.2fpp > 2.0pp",
							spec.Name, c.name, c.cur, c.base, c.base-c.cur)
					}
				}
			}
		})
	}

	if updateBaselines || !baselineExists {
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(baselinePath), err)
		}
		data, err := json.MarshalIndent(currentBaseline, "", "  ")
		if err != nil {
			t.Fatalf("Marshal baseline: %v", err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(baselinePath, data, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", baselinePath, err)
		}
		t.Logf("wrote baseline %s with %d fixtures", baselinePath, len(currentBaseline.Fixtures))
	}

	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })
	var summary bytes.Buffer
	fmt.Fprintln(&summary, "| fixture | mb_total | mode | ref_frame | mv | skip | segment_id | eob_sum |")
	fmt.Fprintln(&summary, "|---|---|---|---|---|---|---|---|")
	for _, r := range reports {
		fmt.Fprintf(&summary, "| %s | %d | %.2f%% | %.2f%% | %.2f%% | %.2f%% | %.2f%% | %.2f%% |\n",
			r.Name, r.MBTotal,
			r.ModeMatchPct, r.RefFrameMatchPct, r.MVMatchPct,
			r.SkipMatchPct, r.SegmentIDMatchPct, r.EOBSumMatchPct)
	}
	t.Logf("inter MB decision match-rate scoreboard:\n%s", summary.String())
}

// mbDecision is the subset of an oracle "mb" trace row used for match-rate
// computation. Field names mirror encoder_oracle_trace.go::oracleTraceMBRow.
type mbDecision struct {
	Mode      string
	RefFrame  string
	MVRow     int
	MVCol     int
	Skip      bool
	SegmentID int
	EOBSum    int
}

// indexInterFrameMBs returns frame_index -> (mb_row<<16|mb_col) -> mbDecision
// for trace rows belonging to inter frames only. Keyframes are excluded by
// looking up frame_type from the matching "frame" row.
func indexInterFrameMBs(t *testing.T, trace []byte) map[int64]map[int64]mbDecision {
	t.Helper()
	frameType := map[int64]string{}
	type rawMB struct {
		FrameIndex int64
		MBRow      int
		MBCol      int
		Mode       string
		RefFrame   string
		MVRow      int
		MVCol      int
		Skip       bool
		SegmentID  int
		EOBSum     int
	}
	var mbRows []rawMB

	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 128*1024), 32*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("MB index: trace row not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typ, _ := row["type"].(string)
		switch typ {
		case "frame":
			fi, _ := row["frame_index"].(float64)
			ft, _ := row["frame_type"].(string)
			frameType[int64(fi)] = ft
		case "mb":
			fi, _ := row["frame_index"].(float64)
			mr, _ := row["mb_row"].(float64)
			mc, _ := row["mb_col"].(float64)
			mode, _ := row["mode"].(string)
			ref, _ := row["ref_frame"].(string)
			mvR, _ := row["mv_row"].(float64)
			mvC, _ := row["mv_col"].(float64)
			skip, _ := row["skip"].(bool)
			seg, _ := row["segment_id"].(float64)
			eob, _ := row["eob_sum"].(float64)
			mbRows = append(mbRows, rawMB{
				FrameIndex: int64(fi),
				MBRow:      int(mr),
				MBCol:      int(mc),
				Mode:       mode,
				RefFrame:   ref,
				MVRow:      int(mvR),
				MVCol:      int(mvC),
				Skip:       skip,
				SegmentID:  int(seg),
				EOBSum:     int(eob),
			})
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("MB index: scan trace: %v", err)
	}

	out := make(map[int64]map[int64]mbDecision)
	for _, r := range mbRows {
		if frameType[r.FrameIndex] != "inter" {
			continue
		}
		bucket := out[r.FrameIndex]
		if bucket == nil {
			bucket = make(map[int64]mbDecision)
			out[r.FrameIndex] = bucket
		}
		key := (int64(r.MBRow) << 32) | int64(uint32(r.MBCol))
		bucket[key] = mbDecision{
			Mode:      r.Mode,
			RefFrame:  r.RefFrame,
			MVRow:     r.MVRow,
			MVCol:     r.MVCol,
			Skip:      r.Skip,
			SegmentID: r.SegmentID,
			EOBSum:    r.EOBSum,
		}
	}
	return out
}

func pct(num, denom int) float64 {
	if denom <= 0 {
		return 0
	}
	return 100.0 * float64(num) / float64(denom)
}

func formatMBMatchTable(r struct {
	Name              string  `json:"name"`
	MBTotal           int     `json:"mb_total"`
	ModeMatchPct      float64 `json:"mode_match_pct"`
	RefFrameMatchPct  float64 `json:"ref_frame_match_pct"`
	MVMatchPct        float64 `json:"mv_match_pct"`
	SkipMatchPct      float64 `json:"skip_match_pct"`
	SegmentIDMatchPct float64 `json:"segment_id_match_pct"`
	EOBSumMatchPct    float64 `json:"eob_sum_match_pct"`
}) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "| field | match_pct |")
	fmt.Fprintln(&b, "|---|---|")
	fmt.Fprintf(&b, "| mode | %.2f%% |\n", r.ModeMatchPct)
	fmt.Fprintf(&b, "| ref_frame | %.2f%% |\n", r.RefFrameMatchPct)
	fmt.Fprintf(&b, "| mv | %.2f%% |\n", r.MVMatchPct)
	fmt.Fprintf(&b, "| skip | %.2f%% |\n", r.SkipMatchPct)
	fmt.Fprintf(&b, "| segment_id | %.2f%% |\n", r.SegmentIDMatchPct)
	fmt.Fprintf(&b, "| eob_sum | %.2f%% |\n", r.EOBSumMatchPct)
	return b.String()
}
