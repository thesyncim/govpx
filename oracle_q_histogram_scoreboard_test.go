package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestOracleEncoderQHistogramScoreboard captures per-fixture Q histograms for
// govpx and libvpx and emits a side-by-side scoreboard. Regression-gated
// against testdata/q_histogram_baseline.json: each fixture's mean Q must stay
// within 1.5 of the recorded baseline, and the L1 distance between govpx's and
// libvpx's histograms must not grow by more than 8 frames of drift.
//
// Bootstrap with GOVPX_UPDATE_BASELINES=1 to seed the file.
func TestOracleEncoderQHistogramScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle Q histogram scoreboard")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
	)

	type fixtureSpec struct {
		Width    int
		Height   int
		Deadline Deadline
		CpuUsed  int
		Name     string
	}
	specs := []fixtureSpec{
		{64, 64, DeadlineRealtime, 0, "rt-cpu0-64x64"},
		{64, 64, DeadlineRealtime, 4, "rt-cpu4-64x64"},
		{64, 64, DeadlineRealtime, 8, "rt-cpu8-64x64"},
		{64, 64, DeadlineGoodQuality, 5, "good-cpu5-64x64"},
		{128, 128, DeadlineRealtime, 0, "rt-cpu0-128x128"},
		{128, 128, DeadlineRealtime, 4, "rt-cpu4-128x128"},
		{128, 128, DeadlineRealtime, 8, "rt-cpu8-128x128"},
		{128, 128, DeadlineGoodQuality, 5, "good-cpu5-128x128"},
	}

	type fixtureQReport struct {
		Width       int      `json:"width"`
		Height      int      `json:"height"`
		Deadline    string   `json:"deadline"`
		CpuUsed     int      `json:"cpu_used"`
		GovpxQMin   int      `json:"govpx_q_min"`
		GovpxQMax   int      `json:"govpx_q_max"`
		GovpxQMean  float64  `json:"govpx_q_mean"`
		GovpxQHist  [128]int `json:"govpx_q_hist"`
		LibvpxQMin  int      `json:"libvpx_q_min"`
		LibvpxQMax  int      `json:"libvpx_q_max"`
		LibvpxQMean float64  `json:"libvpx_q_mean"`
		LibvpxQHist [128]int `json:"libvpx_q_hist"`
		Name        string   `json:"name"`
	}

	type baselineEntry struct {
		QMean        float64 `json:"q_mean"`
		QMax         int     `json:"q_max"`
		HistL1Libvpx int     `json:"hist_l1_to_libvpx"`
	}
	type baselineFile struct {
		Fixtures map[string]baselineEntry `json:"fixtures"`
	}

	baselinePath := filepath.Join("testdata", "q_histogram_baseline.json")
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
	reports := make([]fixtureQReport, 0, len(specs))

	for _, spec := range specs {
		spec := spec
		t.Run(spec.Name, func(t *testing.T) {
			width, height := spec.Width, spec.Height
			rcMode := RateControlCBR
			extraArgs := []string{"--end-usage=cbr"}
			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   rcMode,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          spec.Deadline,
				CpuUsed:           spec.CpuUsed,
				KeyFrameInterval:  999,
			}
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(width, height, i)
			}
			govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "qhist-"+spec.Name, opts, targetKbps, sources, extraArgs)

			govpxHist, govpxMin, govpxMax, govpxMean := computeQHistogramFromTrace(t, govpxTrace)
			libvpxHist, libvpxMin, libvpxMax, libvpxMean := computeQHistogramFromTrace(t, libvpxTrace)

			report := fixtureQReport{
				Name:        spec.Name,
				Width:       width,
				Height:      height,
				Deadline:    deadlineString(spec.Deadline),
				CpuUsed:     spec.CpuUsed,
				GovpxQMin:   govpxMin,
				GovpxQMax:   govpxMax,
				GovpxQMean:  govpxMean,
				GovpxQHist:  govpxHist,
				LibvpxQMin:  libvpxMin,
				LibvpxQMax:  libvpxMax,
				LibvpxQMean: libvpxMean,
				LibvpxQHist: libvpxHist,
			}
			if data, err := json.MarshalIndent(report, "", "  "); err == nil {
				t.Logf("scoreboard %s:\n%s", spec.Name, data)
			}

			l1 := histL1(govpxHist, libvpxHist)
			currentBaseline.Fixtures[spec.Name] = baselineEntry{
				QMean:        govpxMean,
				QMax:         govpxMax,
				HistL1Libvpx: l1,
			}
			reports = append(reports, report)

			if !updateBaselines && baselineExists {
				prev, ok := baseline.Fixtures[spec.Name]
				if !ok {
					t.Errorf("baseline %s missing fixture %q (rerun with GOVPX_UPDATE_BASELINES=1)", baselinePath, spec.Name)
					return
				}
				if math.Abs(govpxMean-prev.QMean) > 1.5 {
					t.Errorf("Q mean regression %s: govpx=%.3f baseline=%.3f drift=%.3f > 1.5",
						spec.Name, govpxMean, prev.QMean, govpxMean-prev.QMean)
				}
				if l1 > prev.HistL1Libvpx+8 {
					t.Errorf("Q histogram L1 regression %s: govpx-vs-libvpx L1=%d baseline=%d drift=%d > 8",
						spec.Name, l1, prev.HistL1Libvpx, l1-prev.HistL1Libvpx)
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

	// Stable order summary for human readability.
	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })
	var summary bytes.Buffer
	fmt.Fprintln(&summary, "fixture,govpx_q_mean,libvpx_q_mean,govpx_q_max,libvpx_q_max,hist_l1")
	for _, r := range reports {
		l1 := histL1(r.GovpxQHist, r.LibvpxQHist)
		fmt.Fprintf(&summary, "%s,%.3f,%.3f,%d,%d,%d\n",
			r.Name, r.GovpxQMean, r.LibvpxQMean, r.GovpxQMax, r.LibvpxQMax, l1)
	}
	t.Logf("Q histogram scoreboard summary:\n%s", summary.String())
}

func computeQHistogramFromTrace(t *testing.T, trace []byte) (hist [128]int, qMin int, qMax int, qMean float64) {
	t.Helper()
	qMin = -1
	qMax = -1
	count := 0
	sum := 0
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("Q histogram: trace row not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typ, _ := row["type"].(string)
		if typ != "frame" {
			continue
		}
		qf, ok := row["q_index"].(float64)
		if !ok {
			continue
		}
		q := int(qf)
		if q < 0 {
			q = 0
		}
		if q > 127 {
			q = 127
		}
		hist[q]++
		sum += q
		count++
		if qMin < 0 || q < qMin {
			qMin = q
		}
		if q > qMax {
			qMax = q
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("Q histogram: scan trace: %v", err)
	}
	if count > 0 {
		qMean = float64(sum) / float64(count)
	}
	if qMin < 0 {
		qMin = 0
	}
	if qMax < 0 {
		qMax = 0
	}
	return hist, qMin, qMax, qMean
}

func histL1(a, b [128]int) int {
	sum := 0
	for i := 0; i < 128; i++ {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return sum
}

func deadlineString(d Deadline) string {
	switch d {
	case DeadlineBestQuality:
		return "best"
	case DeadlineGoodQuality:
		return "good"
	case DeadlineRealtime:
		return "realtime"
	default:
		return fmt.Sprintf("deadline_%d", int(d))
	}
}
