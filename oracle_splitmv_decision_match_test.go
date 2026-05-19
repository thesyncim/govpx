//go:build govpx_oracle_trace

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

// TestOracleSplitMVDecisionMatchRate is the SPLITMV-focused companion to
// TestOracleInterDecisionMatchRate. It runs a per-8x8-quadrant motion fixture
// (so each macroblock has sub-MB-level motion that drives SPLITMV in libvpx)
// and reports per-fixture match-rates against libvpx for the SPLITMV-specific
// fields the libvpx and govpx oracle traces both emit on the {"type":"mb"}
// row: mode (SPLITMV vs whole-MB inter), partition index, per-block
// MVs, and segment_id. The match-rate is computed across all inter
// macroblocks (not just SPLITMV ones), so any drift in the SPLITMV gate -- a
// SPLITMV pick on one side that the other left as NEWMV/NEAREST/NEAR/ZEROMV
// -- shows up as a mode_match deficit. partition_match_pct and
// block_mv_match_pct are computed only over the macroblocks where both sides
// picked SPLITMV.
//
// Baseline lives in testdata/splitmv_match_rate_baseline.json. Each
// match-rate must remain within 2 percentage points of the recorded value.
//
// Bootstrap with GOVPX_UPDATE_BASELINES=1 to seed the file.
func TestOracleSplitMVDecisionMatchRate(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle SPLITMV match scoreboard")
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
		sources[i] = encoderValidationSplitMVQuadrantFrame(width, height, i)
	}

	type fixtureSpec struct {
		Name      string
		Deadline  Deadline
		CpuUsed   int
		RC        RateControlMode
		ExtraArgs []string
	}
	specs := []fixtureSpec{
		{Name: "best-cpu0-vbr", Deadline: DeadlineBestQuality, CpuUsed: 0, RC: RateControlVBR, ExtraArgs: []string{"--end-usage=vbr"}},
		{Name: "good-cpu0-vbr", Deadline: DeadlineGoodQuality, CpuUsed: 0, RC: RateControlVBR, ExtraArgs: []string{"--end-usage=vbr"}},
		{Name: "good-cpu3-vbr", Deadline: DeadlineGoodQuality, CpuUsed: 3, RC: RateControlVBR, ExtraArgs: []string{"--end-usage=vbr"}},
	}

	type fixtureReport struct {
		Name                string  `json:"name"`
		MBTotal             int     `json:"mb_total"`
		SplitMVTotal        int     `json:"splitmv_total"`
		SplitMVAgreed       int     `json:"splitmv_agreed"`
		ModeMatchPct        float64 `json:"mode_match_pct"`
		SplitMVPickMatchPct float64 `json:"splitmv_pick_match_pct"`
		PartitionMatchPct   float64 `json:"partition_match_pct"`
		BlockMVMatchPct     float64 `json:"block_mv_match_pct"`
		SegmentIDMatchPct   float64 `json:"segment_id_match_pct"`
	}
	type baselineEntry struct {
		ModeMatchPct        float64 `json:"mode_match_pct"`
		SplitMVPickMatchPct float64 `json:"splitmv_pick_match_pct"`
		PartitionMatchPct   float64 `json:"partition_match_pct"`
		BlockMVMatchPct     float64 `json:"block_mv_match_pct"`
		SegmentIDMatchPct   float64 `json:"segment_id_match_pct"`
	}
	type baselineFile struct {
		Fixtures map[string]baselineEntry `json:"fixtures"`
	}

	baselinePath := filepath.Join("testdata", "splitmv_match_rate_baseline.json")
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
	reports := make([]fixtureReport, 0, len(specs))

	for _, spec := range specs {
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
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "splitmvmatch-"+spec.Name, opts, targetKbps, sources, spec.ExtraArgs)

			govpxFrames := indexInterFrameSplitMVMBs(t, govpxTrace)
			libvpxFrames := indexInterFrameSplitMVMBs(t, libvpxTrace)

			var modeMatch, splitmvBoth, partitionMatch, blockMVMatch, segMatch, total int
			var splitmvOnGovpx, splitmvOnLibvpx int
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
					if gRow.Mode == "SPLITMV" {
						splitmvOnGovpx++
					}
					if lRow.Mode == "SPLITMV" {
						splitmvOnLibvpx++
					}
					if gRow.SegmentID == lRow.SegmentID {
						segMatch++
					}
					if gRow.Mode == "SPLITMV" && lRow.Mode == "SPLITMV" {
						splitmvBoth++
						if gRow.Partition == lRow.Partition {
							partitionMatch++
						}
						if blockMVsEqual(gRow.BlockMVRow, lRow.BlockMVRow) &&
							blockMVsEqual(gRow.BlockMVCol, lRow.BlockMVCol) {
							blockMVMatch++
						}
					}
				}
			}

			report := fixtureReport{
				Name:          spec.Name,
				MBTotal:       total,
				SplitMVTotal:  maxIntPair(splitmvOnGovpx, splitmvOnLibvpx),
				SplitMVAgreed: splitmvBoth,
			}
			if total > 0 {
				report.ModeMatchPct = pct(modeMatch, total)
				report.SegmentIDMatchPct = pct(segMatch, total)
				// SPLITMV pick agreement: how often both sides agree on
				// "is this MB SPLITMV or not", regardless of partition.
				splitmvPickMatch := total - (splitmvOnGovpx + splitmvOnLibvpx - 2*splitmvBoth)
				report.SplitMVPickMatchPct = pct(splitmvPickMatch, total)
			} else {
				t.Errorf("%s: zero inter MBs compared (govpx_frames=%d libvpx_frames=%d)",
					spec.Name, len(govpxFrames), len(libvpxFrames))
			}
			if splitmvBoth > 0 {
				report.PartitionMatchPct = pct(partitionMatch, splitmvBoth)
				report.BlockMVMatchPct = pct(blockMVMatch, splitmvBoth)
			}

			t.Logf("SPLITMV match-rate %s (mb_total=%d splitmv_govpx=%d splitmv_libvpx=%d splitmv_agreed=%d):\n%s",
				spec.Name, total, splitmvOnGovpx, splitmvOnLibvpx, splitmvBoth,
				formatSplitMVMatchTable(report))

			currentBaseline.Fixtures[spec.Name] = baselineEntry{
				ModeMatchPct:        report.ModeMatchPct,
				SplitMVPickMatchPct: report.SplitMVPickMatchPct,
				PartitionMatchPct:   report.PartitionMatchPct,
				BlockMVMatchPct:     report.BlockMVMatchPct,
				SegmentIDMatchPct:   report.SegmentIDMatchPct,
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
					{"splitmv_pick_match_pct", report.SplitMVPickMatchPct, prev.SplitMVPickMatchPct},
					{"partition_match_pct", report.PartitionMatchPct, prev.PartitionMatchPct},
					{"block_mv_match_pct", report.BlockMVMatchPct, prev.BlockMVMatchPct},
					{"segment_id_match_pct", report.SegmentIDMatchPct, prev.SegmentIDMatchPct},
				}
				for _, c := range checks {
					if c.cur < c.base-2.0 {
						t.Errorf("SPLITMV match-rate regression %s/%s: current=%.2f%% baseline=%.2f%% drop=%.2fpp > 2.0pp",
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
	fmt.Fprintln(&summary, "| fixture | mb_total | splitmv_agreed | mode | splitmv_pick | partition | block_mv | segment_id |")
	fmt.Fprintln(&summary, "|---|---|---|---|---|---|---|---|")
	for _, r := range reports {
		fmt.Fprintf(&summary, "| %s | %d | %d | %.2f%% | %.2f%% | %.2f%% | %.2f%% | %.2f%% |\n",
			r.Name, r.MBTotal, r.SplitMVAgreed,
			r.ModeMatchPct, r.SplitMVPickMatchPct,
			r.PartitionMatchPct, r.BlockMVMatchPct, r.SegmentIDMatchPct)
	}
	t.Logf("SPLITMV decision match-rate scoreboard:\n%s", summary.String())
}

// splitMVDecision is the subset of an oracle "mb" trace row used for SPLITMV
// match-rate computation. Field names mirror vp8_encoder_oracle_trace.go's
// oracleTraceMBRow SPLITMV fields.
type splitMVDecision struct {
	Mode       string
	SegmentID  int
	Partition  int
	BlockMVRow []int16
	BlockMVCol []int16
}

// indexInterFrameSplitMVMBs returns frame_index -> (mb_row<<32|mb_col) ->
// splitMVDecision for trace rows belonging to inter frames only.
// Keyframes are excluded by looking up frame_type from the matching "frame"
// row. Macroblocks without a partition (i.e. not SPLITMV) carry Partition=-1
// and empty BlockMV slices.
func indexInterFrameSplitMVMBs(t *testing.T, trace []byte) map[int64]map[int64]splitMVDecision {
	t.Helper()
	frameType := map[int64]string{}
	type rawMB struct {
		FrameIndex int64
		MBRow      int
		MBCol      int
		Mode       string
		SegmentID  int
		Partition  int
		BlockMVRow []int16
		BlockMVCol []int16
	}
	var mbRows []rawMB

	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 128*1024), 32*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("SPLITMV index: trace row not valid JSON: %v\n%s", err, scan.Bytes())
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
			seg, _ := row["segment_id"].(float64)
			rec := rawMB{
				FrameIndex: int64(fi),
				MBRow:      int(mr),
				MBCol:      int(mc),
				Mode:       mode,
				SegmentID:  int(seg),
				Partition:  -1,
			}
			if part, ok := row["partition"].(float64); ok {
				rec.Partition = int(part)
			}
			if rows, ok := row["block_mv_rows"].([]any); ok {
				rec.BlockMVRow = make([]int16, len(rows))
				for i, v := range rows {
					if x, ok := v.(float64); ok {
						rec.BlockMVRow[i] = int16(x)
					}
				}
			}
			if cols, ok := row["block_mv_cols"].([]any); ok {
				rec.BlockMVCol = make([]int16, len(cols))
				for i, v := range cols {
					if x, ok := v.(float64); ok {
						rec.BlockMVCol[i] = int16(x)
					}
				}
			}
			mbRows = append(mbRows, rec)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("SPLITMV index: scan trace: %v", err)
	}

	out := make(map[int64]map[int64]splitMVDecision)
	for _, r := range mbRows {
		if frameType[r.FrameIndex] != "inter" {
			continue
		}
		bucket := out[r.FrameIndex]
		if bucket == nil {
			bucket = make(map[int64]splitMVDecision)
			out[r.FrameIndex] = bucket
		}
		key := (int64(r.MBRow) << 32) | int64(uint32(r.MBCol))
		bucket[key] = splitMVDecision{
			Mode:       r.Mode,
			SegmentID:  r.SegmentID,
			Partition:  r.Partition,
			BlockMVRow: r.BlockMVRow,
			BlockMVCol: r.BlockMVCol,
		}
	}
	return out
}

func blockMVsEqual(a, b []int16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func maxIntPair(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func formatSplitMVMatchTable(r struct {
	Name                string  `json:"name"`
	MBTotal             int     `json:"mb_total"`
	SplitMVTotal        int     `json:"splitmv_total"`
	SplitMVAgreed       int     `json:"splitmv_agreed"`
	ModeMatchPct        float64 `json:"mode_match_pct"`
	SplitMVPickMatchPct float64 `json:"splitmv_pick_match_pct"`
	PartitionMatchPct   float64 `json:"partition_match_pct"`
	BlockMVMatchPct     float64 `json:"block_mv_match_pct"`
	SegmentIDMatchPct   float64 `json:"segment_id_match_pct"`
}) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "| field | match_pct |")
	fmt.Fprintln(&b, "|---|---|")
	fmt.Fprintf(&b, "| mode | %.2f%% |\n", r.ModeMatchPct)
	fmt.Fprintf(&b, "| splitmv_pick | %.2f%% |\n", r.SplitMVPickMatchPct)
	fmt.Fprintf(&b, "| partition | %.2f%% |\n", r.PartitionMatchPct)
	fmt.Fprintf(&b, "| block_mv | %.2f%% |\n", r.BlockMVMatchPct)
	fmt.Fprintf(&b, "| segment_id | %.2f%% |\n", r.SegmentIDMatchPct)
	return b.String()
}

// encoderValidationSplitMVQuadrantFrame builds a SPLITMV-prone synthetic
// fixture: a high-frequency Y/UV pattern where each 8x8 block of every
// macroblock pans in a different direction across frames. The four 8x8
// quadrants of a single MB move in diagonally opposite directions, so the
// best whole-MB inter MV is a poor fit and the SPLITMV partition (typically
// VS_BISPLIT or 4x4) wins the RD competition. Because the source YUV is
// generated deterministically from (index, x, y) without depending on any
// previous reconstruction, govpx and libvpx see the exact same pixels and
// any SPLITMV-pick disagreement is purely an encoder-side decision.
//
// Layout (per MB):
//   - top-left  8x8 quadrant pans by (+2*idx, +1*idx)
//   - top-right 8x8 quadrant pans by (-2*idx, +1*idx)
//   - bot-left  8x8 quadrant pans by (+2*idx, -1*idx)
//   - bot-right 8x8 quadrant pans by (-2*idx, -1*idx)
func encoderValidationSplitMVQuadrantFrame(width int, height int, index int) Image {
	img := testImage(width, height)
	for y := range height {
		for x := range width {
			// Within each MB (16x16) figure out which 8x8 quadrant we are in.
			mbX := x & 0xF
			mbY := y & 0xF
			quad := 0
			if mbY >= 8 {
				quad |= 2
			}
			if mbX >= 8 {
				quad |= 1
			}
			var dx, dy int
			switch quad {
			case 0:
				dx, dy = 2*index, index
			case 1:
				dx, dy = -2*index, index
			case 2:
				dx, dy = 2*index, -index
			case 3:
				dx, dy = -2*index, -index
			}
			srcX := x + dx
			srcY := y + dy
			img.Y[y*img.YStride+x] = byte(48 + ((srcY*7 + srcX*11 + (srcX/4)*(srcY/4)*23) & 159))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		for x := range uvWidth {
			// Chroma lives at half resolution: each 8x8 chroma block covers
			// the same 8x8 luma quadrant region used above. Pan with the
			// matching quadrant offset (halved because chroma is 4:2:0).
			mbX := x & 0x7
			mbY := y & 0x7
			quad := 0
			if mbY >= 4 {
				quad |= 2
			}
			if mbX >= 4 {
				quad |= 1
			}
			var dx, dy int
			switch quad {
			case 0:
				dx, dy = index, index/2
			case 1:
				dx, dy = -index, index/2
			case 2:
				dx, dy = index, -index/2
			case 3:
				dx, dy = -index, -index/2
			}
			srcX := x + dx
			srcY := y + dy
			img.U[y*img.UStride+x] = byte(96 + ((srcX*5 + srcY*3) & 63))
			img.V[y*img.VStride+x] = byte(144 + ((srcX*2 + srcY*7) & 63))
		}
	}
	return img
}
