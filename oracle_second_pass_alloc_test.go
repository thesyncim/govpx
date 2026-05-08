package govpx

// Second-pass allocation oracle compare.
//
// TestOracleSecondPassAllocationCompare drives a libvpx pass-1 run against a
// deterministic ramp source to produce a libvpx FIRSTPASS_STATS file
// (.fpf), then runs both encoders on a matched pass-2 configuration:
//   - libvpx: vpxenc-oracle --pass=2 --fpf=<file> with the trace env var,
//     emitting a per-frame "rate" row carrying q_index / this_frame_target.
//   - govpx: a fresh VP8Encoder configured with the same first-pass stats
//     parsed from the fpf and fed through EncoderOptions.TwoPassStats.
//
// We then compare per-frame q_index (within +/- 2 qindex) and
// this_frame_target (within 5% relative). Tallies are written / compared
// against testdata/second_pass_alloc_baseline.json under the standard
// GOVPX_UPDATE_BASELINES=1 bootstrap pattern.
//
// All work is gated behind GOVPX_WITH_ORACLE=1.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

const secondPassAllocBaselinePath = "testdata/second_pass_alloc_baseline.json"

type secondPassRateRow struct {
	FrameIndex      int64 `json:"frame_index"`
	QIndex          int   `json:"q_index"`
	ThisFrameTarget int   `json:"this_frame_target"`
}

type secondPassFrameDiff struct {
	FrameIndex     int64
	QIndexGovpx    int
	QIndexLibvpx   int
	QIndexDelta    int
	TargetGovpx    int
	TargetLibvpx   int
	TargetRelDelta float64
}

type FixtureSecondPassReport struct {
	Name              string
	FrameTotal        int
	QWithinTol        int
	TargetWithinTol   int
	QMatchPct         float64
	TargetMatchPct    float64
	MaxQIndexDelta    int
	MaxTargetRelDelta float64
}

type secondPassBaselineEntry struct {
	QMatchPct         float64 `json:"q_match_pct"`
	TargetMatchPct    float64 `json:"target_match_pct"`
	MaxQIndexDelta    int     `json:"max_qindex_delta"`
	MaxTargetRelDelta float64 `json:"max_target_rel_delta"`
}

type secondPassBaselineFile struct {
	Fixtures map[string]secondPassBaselineEntry `json:"fixtures"`
}

func TestOracleSecondPassAllocationCompare(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run second-pass allocation oracle compare")
	}
	vpxenc := findVpxenc(t)
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 32
		height     = 32
		fps        = 30
		targetKbps = 400
		frames     = 8
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = firstPassOracleRampFrame(width, height, i)
	}

	type fixture struct {
		name string
		opts EncoderOptions
	}
	fixtures := []fixture{
		{
			name: "good-quality-cpu0-vbr",
			opts: EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  60,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           0,
			},
		},
	}

	reports := make([]FixtureSecondPassReport, 0, len(fixtures))
	for _, f := range fixtures {
		f := f
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()
			yuvPath := filepath.Join(dir, f.name+".yuv")
			fpfPath := filepath.Join(dir, f.name+".fpf")
			ivf1Path := filepath.Join(dir, f.name+"-pass1.ivf")
			ivf2Path := filepath.Join(dir, f.name+"-pass2.ivf")
			tracePath := filepath.Join(dir, f.name+"-pass2.jsonl")

			writeEncoderValidationI420(t, yuvPath, sources)
			runLibvpxPass1(t, vpxenc, yuvPath, ivf1Path, fpfPath, f.opts, targetKbps, len(sources))
			libvpxTrace := runLibvpxPass2WithTrace(t, vpxencOracle, yuvPath, ivf2Path, fpfPath, tracePath, f.opts, targetKbps, len(sources))

			fpfData, err := os.ReadFile(fpfPath)
			if err != nil {
				t.Fatalf("ReadFile %s: %v", fpfPath, err)
			}
			parsed := parseLibvpxFirstPassStats(t, fpfData)

			// Drive govpx pass 2 with the libvpx-emitted stats; capture trace.
			govpxOpts := f.opts
			govpxOpts.TwoPassStats = parsed
			govpxTrace := captureGovpxEncoderTrace(t, govpxOpts, sources)

			govpxRows := secondPassRateRowsFromTrace(t, govpxTrace)
			libvpxRows := secondPassRateRowsFromTrace(t, libvpxTrace)
			report, diffs := scoreSecondPassAlloc(f.name, govpxRows, libvpxRows)
			reports = append(reports, report)
			t.Logf("second-pass scoreboard: %s frames=%d q_match=%.2f%% target_match=%.2f%% maxQΔ=%d maxTargetRelΔ=%.4f",
				report.Name, report.FrameTotal,
				report.QMatchPct, report.TargetMatchPct,
				report.MaxQIndexDelta, report.MaxTargetRelDelta)
			for _, d := range diffs {
				if absInt(d.QIndexDelta) > 2 || math.Abs(d.TargetRelDelta) > 0.05 {
					t.Logf("  frame %d qΔ=%d (govpx=%d libvpx=%d) targetΔrel=%.4f (govpx=%d libvpx=%d)",
						d.FrameIndex, d.QIndexDelta, d.QIndexGovpx, d.QIndexLibvpx,
						d.TargetRelDelta, d.TargetGovpx, d.TargetLibvpx)
				}
			}
		})
	}

	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })

	if os.Getenv("GOVPX_UPDATE_BASELINES") == "1" {
		writeSecondPassBaseline(t, reports)
		return
	}
	enforceSecondPassBaseline(t, reports)
}

func runLibvpxPass1(t *testing.T, vpxenc string, yuvPath string, ivfPath string, fpfPath string, opts EncoderOptions, targetKbps int, count int) {
	t.Helper()
	deadlineArg := libvpxDeadlineArg(opts.Deadline)
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--passes=2",
		"--pass=1",
		"--fpf=" + fpfPath,
		"--end-usage=vbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--kf-min-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--kf-max-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(count),
		"--output=" + ivfPath,
		yuvPath,
	}
	cmd := exec.Command(vpxenc, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxenc pass 1 failed: %v\n%s", err, out)
	}
}

func runLibvpxPass2WithTrace(t *testing.T, vpxencOracle string, yuvPath string, ivfPath string, fpfPath string, tracePath string, opts EncoderOptions, targetKbps int, count int) []byte {
	t.Helper()
	deadlineArg := libvpxDeadlineArg(opts.Deadline)
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--passes=2",
		"--pass=2",
		"--fpf=" + fpfPath,
		"--end-usage=vbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--kf-min-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--kf-max-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(count),
		"--output=" + ivfPath,
		yuvPath,
	}
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = append(os.Environ(), "GOVPX_ORACLE_TRACE_OUT="+tracePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxenc-oracle pass 2 failed: %v\n%s", err, out)
	}
	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", tracePath, err)
	}
	return trace
}

func libvpxDeadlineArg(deadline Deadline) string {
	switch deadline {
	case DeadlineBestQuality:
		return "--best"
	case DeadlineRealtime:
		return "--rt"
	default:
		return "--good"
	}
}

func secondPassRateRowsFromTrace(t *testing.T, trace []byte) []secondPassRateRow {
	t.Helper()
	var rows []secondPassRateRow
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scan.Scan() {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			t.Fatalf("trace row not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typeRaw, ok := raw["type"]
		if !ok {
			continue
		}
		var rowType string
		if err := json.Unmarshal(typeRaw, &rowType); err != nil {
			continue
		}
		if rowType != "rate" {
			continue
		}
		var row secondPassRateRow
		if v, ok := raw["frame_index"]; ok {
			_ = json.Unmarshal(v, &row.FrameIndex)
		}
		if v, ok := raw["q_index"]; ok {
			_ = json.Unmarshal(v, &row.QIndex)
		}
		if v, ok := raw["this_frame_target"]; ok {
			_ = json.Unmarshal(v, &row.ThisFrameTarget)
		}
		rows = append(rows, row)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return rows
}

func scoreSecondPassAlloc(name string, govpxRows []secondPassRateRow, libvpxRows []secondPassRateRow) (FixtureSecondPassReport, []secondPassFrameDiff) {
	total := len(govpxRows)
	if len(libvpxRows) < total {
		total = len(libvpxRows)
	}
	report := FixtureSecondPassReport{Name: name, FrameTotal: total}
	if total == 0 {
		return report, nil
	}
	diffs := make([]secondPassFrameDiff, total)
	for i := 0; i < total; i++ {
		g := govpxRows[i]
		l := libvpxRows[i]
		d := secondPassFrameDiff{
			FrameIndex:   g.FrameIndex,
			QIndexGovpx:  g.QIndex,
			QIndexLibvpx: l.QIndex,
			QIndexDelta:  g.QIndex - l.QIndex,
			TargetGovpx:  g.ThisFrameTarget,
			TargetLibvpx: l.ThisFrameTarget,
		}
		// Relative delta is referenced to libvpx's target; sentinel zero
		// means we report absolute delta as ratio over max(1, target).
		denom := math.Abs(float64(l.ThisFrameTarget))
		if denom < 1.0 {
			denom = 1.0
		}
		d.TargetRelDelta = float64(g.ThisFrameTarget-l.ThisFrameTarget) / denom
		diffs[i] = d

		if absInt(d.QIndexDelta) <= 2 {
			report.QWithinTol++
		}
		if math.Abs(d.TargetRelDelta) <= 0.05 {
			report.TargetWithinTol++
		}
		if absInt(d.QIndexDelta) > absInt(report.MaxQIndexDelta) {
			report.MaxQIndexDelta = d.QIndexDelta
		}
		if math.Abs(d.TargetRelDelta) > math.Abs(report.MaxTargetRelDelta) {
			report.MaxTargetRelDelta = d.TargetRelDelta
		}
	}
	pct := func(n int) float64 { return 100.0 * float64(n) / float64(total) }
	report.QMatchPct = pct(report.QWithinTol)
	report.TargetMatchPct = pct(report.TargetWithinTol)
	return report, diffs
}

func writeSecondPassBaseline(t *testing.T, reports []FixtureSecondPassReport) {
	t.Helper()
	file := secondPassBaselineFile{Fixtures: map[string]secondPassBaselineEntry{}}
	for _, r := range reports {
		file.Fixtures[r.Name] = secondPassBaselineEntry{
			QMatchPct:         r.QMatchPct,
			TargetMatchPct:    r.TargetMatchPct,
			MaxQIndexDelta:    r.MaxQIndexDelta,
			MaxTargetRelDelta: r.MaxTargetRelDelta,
		}
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(secondPassAllocBaselinePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(secondPassAllocBaselinePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Logf("wrote %s", secondPassAllocBaselinePath)
}

func enforceSecondPassBaseline(t *testing.T, reports []FixtureSecondPassReport) {
	t.Helper()
	data, err := os.ReadFile(secondPassAllocBaselinePath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v (run with GOVPX_UPDATE_BASELINES=1 to bootstrap)", secondPassAllocBaselinePath, err)
	}
	var file secondPassBaselineFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshal baseline: %v", err)
	}
	const tol = 2.0
	for _, r := range reports {
		baseline, ok := file.Fixtures[r.Name]
		if !ok {
			t.Errorf("second-pass baseline missing fixture %q (run with GOVPX_UPDATE_BASELINES=1)", r.Name)
			continue
		}
		if r.QMatchPct < baseline.QMatchPct-tol {
			t.Errorf("%s q_match_pct = %.4f, baseline %.4f, regression > %.2f",
				r.Name, r.QMatchPct, baseline.QMatchPct, tol)
		}
		if r.TargetMatchPct < baseline.TargetMatchPct-tol {
			t.Errorf("%s target_match_pct = %.4f, baseline %.4f, regression > %.2f",
				r.Name, r.TargetMatchPct, baseline.TargetMatchPct, tol)
		}
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
