package benchcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestQualityFixtureGovpxOnly runs the canonical quality-gate fixtures
// through runVP9Benchmark (govpx-only, libvpx skipped) so we can verify
// each fixture stays above the default gate floors on the current
// vp9-port encoder. Skipped in -short mode; set
// GOVPX_BENCH_QUALITY_SMOKE=1 to force the run.
func TestQualityFixtureGovpxOnly(t *testing.T) {
	if testing.Short() && os.Getenv("GOVPX_BENCH_QUALITY_SMOKE") != "1" {
		t.Skip("skipping fixture smoke test in -short mode; set GOVPX_BENCH_QUALITY_SMOKE=1 to force run")
	}
	gate := defaultQualityGate()
	for _, fx := range qualityGateFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			cfg := benchConfig{
				Codec:       codecVP9,
				Width:       fx.Width,
				Height:      fx.Height,
				Frames:      10, // shorter than the 120 used by the gate so the test stays fast
				FPS:         fx.FPS,
				BitrateKbps: fx.BitrateKbps,
				Mode:        fx.Mode,
			}
			report, err := runVP9BenchmarkWithSource(cfg, fx.Source)
			if err != nil {
				t.Fatalf("runVP9BenchmarkWithSource returned error: %v", err)
			}
			if report.QualityFrames == 0 || report.PSNR <= 0 || report.SSIM <= 0 {
				t.Fatalf("quality metrics = frames:%d psnr:%f ssim:%f, want populated",
					report.QualityFrames, report.PSNR, report.SSIM)
			}
			t.Logf("%s govpx-only: psnr=%.2f dB ssim=%.5f output=%.2f kbps frames=%d/%d",
				fx.Name, report.PSNR, report.SSIM,
				report.OutputBitrateKbps, report.EncodedFrames, report.Frames)

			// Each fixture must stay above the documented gate floor.
			// A regression that drops these numbers below the floor
			// will land here first, before the production verify-
			// quality run.
			if report.PSNR < gate.MinPSNR {
				t.Fatalf("%s govpx PSNR %.2f dB < default floor %.2f dB; investigate before relaxing MinPSNR",
					fx.Name, report.PSNR, gate.MinPSNR)
			}
			if report.SSIM < gate.MinSSIM {
				t.Fatalf("%s govpx SSIM %.5f < default floor %.2f; investigate before relaxing MinSSIM",
					fx.Name, report.SSIM, gate.MinSSIM)
			}
		})
	}
}

func TestQualityGateAllowsValuesAboveFloor(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	report := benchReport{
		PSNR: 32.5,
		SSIM: 0.91,
		Reference: &referenceReport{
			PSNR: 32.7,
			SSIM: 0.92,
		},
	}
	if v := gate.Evaluate(report); len(v) != 0 {
		t.Fatalf("Evaluate = %+v, want no violations", v)
	}
}

func TestQualityGateFiresOnAbsolutePSNRFloor(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	report := benchReport{
		PSNR: 15.0,
		SSIM: 0.90,
	}
	v := gate.Evaluate(report)
	if len(v) != 1 || !strings.Contains(v[0].Metric, "PSNR") || v[0].Limit != "min" {
		t.Fatalf("Evaluate = %+v, want 1 PSNR min violation", v)
	}
	if v[0].Observed != 15.0 || v[0].Threshold != gate.MinPSNR {
		t.Fatalf("violation = %+v, want observed=15 threshold=%f", v[0], gate.MinPSNR)
	}
}

func TestQualityGateFiresOnAbsoluteSSIMFloor(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	report := benchReport{
		PSNR: 30.0,
		SSIM: 0.60,
	}
	v := gate.Evaluate(report)
	if len(v) != 1 || !strings.Contains(v[0].Metric, "SSIM") || v[0].Limit != "min" {
		t.Fatalf("Evaluate = %+v, want 1 SSIM min violation", v)
	}
}

func TestQualityGateFiresOnPSNRGapVsLibvpx(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	report := benchReport{
		PSNR: 27.0,
		SSIM: 0.91,
		Reference: &referenceReport{
			PSNR: 30.0, // 3 dB gap, exceeds default 2 dB
			SSIM: 0.92,
		},
	}
	v := gate.Evaluate(report)
	if len(v) != 1 || !strings.Contains(v[0].Metric, "PSNR") || v[0].Limit != "max-gap" {
		t.Fatalf("Evaluate = %+v, want 1 PSNR gap violation", v)
	}
	if v[0].Observed != 3.0 || v[0].Threshold != gate.MaxPSNRBehindLibvpx {
		t.Fatalf("violation = %+v, want gap=3 threshold=%f", v[0], gate.MaxPSNRBehindLibvpx)
	}
}

func TestQualityGateFiresOnSSIMGapVsLibvpx(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	report := benchReport{
		PSNR: 30.0,
		SSIM: 0.85,
		Reference: &referenceReport{
			PSNR: 30.2,
			SSIM: 0.92, // 0.07 gap, exceeds 0.03 default
		},
	}
	v := gate.Evaluate(report)
	if len(v) != 1 || !strings.Contains(v[0].Metric, "SSIM") || v[0].Limit != "max-gap" {
		t.Fatalf("Evaluate = %+v, want 1 SSIM gap violation", v)
	}
}

func TestQualityGateSkipsWhenDisabled(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = false
	report := benchReport{
		PSNR: 1.0,
		SSIM: 0.0,
		Reference: &referenceReport{
			PSNR: 99.0,
			SSIM: 1.0,
		},
	}
	if v := gate.Evaluate(report); len(v) != 0 {
		t.Fatalf("Evaluate disabled = %+v, want zero violations", v)
	}
}

func TestQualityGateSkipsWhenQualitySkipped(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	report := benchReport{
		QualitySkipped: true,
		PSNR:           0,
		SSIM:           0,
	}
	if v := gate.Evaluate(report); len(v) != 0 {
		t.Fatalf("Evaluate skipped quality = %+v, want zero violations", v)
	}
}

func TestQualityGateReportsMultipleViolations(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	report := benchReport{
		PSNR: 15.0, // below MinPSNR=20
		SSIM: 0.50, // below MinSSIM=0.70
		Reference: &referenceReport{
			PSNR: 30.0, // 15 dB gap
			SSIM: 0.95, // 0.45 gap
		},
	}
	v := gate.Evaluate(report)
	if len(v) != 4 {
		t.Fatalf("Evaluate = %+v (%d violations), want 4", v, len(v))
	}
}

func TestFormatQualityGateViolations(t *testing.T) {
	violations := []QualityGateViolation{
		{Metric: "PSNR (dB)", Observed: 22.5, Threshold: 25.0, Limit: "min"},
		{Metric: "PSNR gap", Observed: 3.0, Threshold: 1.5, Limit: "max-gap"},
	}
	got := formatQualityGateViolations("panning-720p", violations)
	if !strings.Contains(got, "FAILED for panning-720p") {
		t.Fatalf("output missing label header:\n%s", got)
	}
	if !strings.Contains(got, "PSNR (dB) 22.5000 below floor 25.0000") {
		t.Fatalf("output missing min violation line:\n%s", got)
	}
	if !strings.Contains(got, "PSNR gap 3.0000 exceeds max 1.5000") {
		t.Fatalf("output missing max-gap violation line:\n%s", got)
	}
}

func TestQualityGateFixturesAreDeterministic(t *testing.T) {
	a := qualityGateFixtures()
	b := qualityGateFixtures()
	if len(a) != len(b) || len(a) < 2 {
		t.Fatalf("fixtures len = %d/%d, want >=2 equal", len(a), len(b))
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Width != b[i].Width || a[i].Frames != b[i].Frames {
			t.Fatalf("fixture[%d] differs between calls: %+v vs %+v", i, a[i], b[i])
		}
	}
	wantNames := map[string]bool{
		"panning-360p-2m-60f":    true,
		"checker-360p-600k-120f": true,
	}
	for _, fx := range a {
		if !wantNames[fx.Name] {
			t.Fatalf("unexpected fixture name %q (want %v)", fx.Name, wantNames)
		}
	}
}

func TestMakePanningFrameDeterministic(t *testing.T) {
	a := makePanningFrame(64, 32, 5)
	b := makePanningFrame(64, 32, 5)
	if len(a.Y) != len(b.Y) || a.Y[0] != b.Y[0] || a.Y[len(a.Y)-1] != b.Y[len(b.Y)-1] {
		t.Fatalf("panning frame not deterministic")
	}
	// Index advance must change the pixel values: this catches mistakes
	// where the source function ignores the frame index.
	c := makePanningFrame(64, 32, 6)
	diff := 0
	for i := range a.Y {
		if a.Y[i] != c.Y[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatalf("panning frame index advance had no effect")
	}
}

func TestMakeCheckerFrameDeterministic(t *testing.T) {
	a := makeCheckerFrame(48, 32, 2)
	b := makeCheckerFrame(48, 32, 2)
	if len(a.Y) != len(b.Y) || a.Y[0] != b.Y[0] {
		t.Fatalf("checker frame not deterministic")
	}
	c := makeCheckerFrame(48, 32, 16) // phase shift = exactly one cell
	diff := 0
	for i := range a.Y {
		if a.Y[i] != c.Y[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatalf("checker frame index advance had no effect")
	}
}

func TestEvaluateQualityGateBenchReport(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	// In-range -> zero
	if code := evaluateQualityGate(gate, benchReport{PSNR: 35, SSIM: 0.95}); code != 0 {
		t.Fatalf("clean run exit code = %d, want 0", code)
	}
	if code := evaluateQualityGate(gate, benchReport{PSNR: 10, SSIM: 0.1}); code != 3 {
		t.Fatalf("bad run exit code = %d, want 3", code)
	}
}

func TestEvaluateQualityGateSuiteReport(t *testing.T) {
	gate := defaultQualityGate()
	gate.Enabled = true
	suite := suiteReport{
		Cases: []suiteCaseReport{
			{Name: "ok", Report: benchReport{PSNR: 35, SSIM: 0.95}},
			{Name: "bad", Report: benchReport{PSNR: 10, SSIM: 0.20}},
		},
	}
	if code := evaluateQualityGate(gate, suite); code != 3 {
		t.Fatalf("suite with failing case exit code = %d, want 3", code)
	}
	suite.Cases = suite.Cases[:1] // keep only "ok"
	if code := evaluateQualityGate(gate, suite); code != 0 {
		t.Fatalf("suite with all passing cases exit code = %d, want 0", code)
	}
}

func TestParseVpxencEncodeTimeUnits(t *testing.T) {
	tests := []struct {
		name      string
		stderr    string
		ok        bool
		frames    int
		totalNS   int64
		bytesWant int
	}{
		{
			name:      "microseconds",
			stderr:    "\rPass 1/1 frame    1/0      0B       0 us 0.00 fps   \rPass 1/1 frame    3/3   1234B   45000 us  66.67 fps   \n",
			ok:        true,
			frames:    3,
			totalNS:   45_000 * int64(time.Microsecond),
			bytesWant: 1234,
		},
		{
			name:      "milliseconds-when-long",
			stderr:    "Pass 1/1 frame   30/30 567890B   12345 ms   2.43 fps   \n",
			ok:        true,
			frames:    30,
			totalNS:   12_345 * int64(time.Millisecond),
			bytesWant: 567890,
		},
		{
			name:   "no-progress-output",
			stderr: "some unrelated logging\nthat does not match\n",
			ok:     false,
		},
		{
			name:   "frames-zero",
			stderr: "Pass 1/1 frame    0/0      0B       0 us 0.00 fps   \n",
			ok:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseVpxencEncodeTime([]byte(tt.stderr))
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v (got=%+v)", ok, tt.ok, got)
			}
			if !ok {
				return
			}
			if got.frames != tt.frames {
				t.Fatalf("frames = %d, want %d", got.frames, tt.frames)
			}
			if got.totalNS != tt.totalNS {
				t.Fatalf("totalNS = %d, want %d", got.totalNS, tt.totalNS)
			}
			if got.bytes != tt.bytesWant {
				t.Fatalf("bytes = %d, want %d", got.bytes, tt.bytesWant)
			}
		})
	}
}

func TestParseLibvpxVP9CallStats(t *testing.T) {
	stderr := []byte("\rPass 1/1 frame    3/3   1234B   45000 us  66.67 fps\n" +
		"LIBVPX_VP9_CALL_STATS inter_mode_picks=231648 sad_calls=123 " +
		"sad_candidates=2819567 mode_block_32x32=51956 " +
		"mode_block_16x16=161783 mode_block_8x8=30988 " +
		"varpart_choose_calls=240 varpart_copy_hits=3 " +
		"varpart_content_state_very_high_sad=42 " +
		"varpart_force_split_16=43 varpart_setvt_calls=44 " +
		"varpart_force_split_16_variance=45 " +
		"varpart_force_split_16_minmax=46 " +
		"varpart_threshold2_count=2 varpart_threshold2_sum=100 " +
		"varpart_var16_samples=4 varpart_var16_sum=1000 " +
		"varpart_force16_variance_sum=900 " +
		"varpart_force16_threshold_sum=80 " +
		"varpart_setvt_force_split_16x16=47 " +
		"varpart_setvt_select=48 varpart_setvt_split=49 unknown_key=99\n")
	stats, ok := parseLibvpxVP9CallStats(stderr)
	if !ok || stats == nil {
		t.Fatalf("parseLibvpxVP9CallStats failed")
	}
	if stats.InterModePicks != 231648 || stats.SADCalls != 123 || stats.SADCandidates != 2819567 {
		t.Fatalf("hot counters = %+v", *stats)
	}
	if stats.ModeBlock32x32 != 51956 || stats.ModeBlock16x16 != 161783 || stats.ModeBlock8x8 != 30988 {
		t.Fatalf("mode-block counters = %+v", *stats)
	}
	if stats.VarpartChooseCalls != 240 || stats.VarpartCopyHits != 3 || stats.VarpartContentStateVeryHighSad != 42 {
		t.Fatalf("varpart counters = %+v", *stats)
	}
	if stats.VarpartForceSplit16 != 43 || stats.VarpartSetVTCalls != 44 ||
		stats.VarpartForceSplit16Variance != 45 ||
		stats.VarpartForceSplit16Minmax != 46 ||
		stats.VarpartThreshold2Count != 2 ||
		stats.VarpartThreshold2Sum != 100 ||
		stats.VarpartVar16Samples != 4 ||
		stats.VarpartVar16Sum != 1000 ||
		stats.VarpartForce16VarianceSum != 900 ||
		stats.VarpartForce16ThresholdSum != 80 ||
		stats.VarpartSetVTForceSplit16x16 != 47 ||
		stats.VarpartSetVTSelect != 48 || stats.VarpartSetVTSplit != 49 {
		t.Fatalf("varpart split counters = %+v", *stats)
	}
	if stats.ModeBlocks() != 51956+161783+30988 {
		t.Fatalf("ModeBlocks = %d", stats.ModeBlocks())
	}
}

func TestImageSSIM(t *testing.T) {
	src := makeBenchmarkFrame(16, 16, 0)
	same := makeBenchmarkFrame(16, 16, 0)
	if got := imageSSIM(src, same); got != 1 {
		t.Fatalf("identical SSIM = %f, want 1", got)
	}
	changed := makeBenchmarkFrame(16, 16, 1)
	if got := imageSSIM(src, changed); got <= 0 || got >= 1 {
		t.Fatalf("changed SSIM = %f, want between 0 and 1", got)
	}
}

func TestParseFFmpegVMAFStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmaf.json")
	data := []byte(`{
  "frames": [
    {"frameNum": 0, "metrics": {"integer_vmaf": 91.25, "integer_motion2": 0.1}},
    {"frameNum": 1, "metrics": {"vmaf": 92.50}}
  ],
  "pooled_metrics": {"integer_vmaf": {"mean": 91.875}}
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	values, err := parseFFmpegVMAFStats(path)
	if err != nil {
		t.Fatalf("parseFFmpegVMAFStats returned error: %v", err)
	}
	if len(values) != 2 || values[0] != 91.25 || values[1] != 92.50 {
		t.Fatalf("values = %v, want [91.25 92.5]", values)
	}
}

func TestPlotArtifactsIncludeVMAF(t *testing.T) {
	report := plotComparisonReport{
		Width:       16,
		Height:      16,
		Frames:      2,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
		Govpx: plotEncoderSummary{
			EncodeFPS:         60,
			OutputBitrateKbps: 1100,
			AverageVMAF:       91,
			AveragePSNR:       40,
			AverageSSIM:       0.98,
		},
		Libvpx: plotEncoderSummary{
			EncodeFPS:         120,
			OutputBitrateKbps: 1200,
			AverageVMAF:       93,
			AveragePSNR:       41,
			AverageSSIM:       0.99,
		},
		FramesData: []plotFrameComparison{
			{Frame: 0, GovpxVMAF: 90, LibvpxVMAF: 92, GovpxPSNR: 39, LibvpxPSNR: 40, GovpxSSIM: 0.97, LibvpxSSIM: 0.98, GovpxBytes: 100, LibvpxBytes: 110},
			{Frame: 1, GovpxVMAF: 92, LibvpxVMAF: 94, GovpxPSNR: 41, LibvpxPSNR: 42, GovpxSSIM: 0.99, LibvpxSSIM: 0.995, GovpxBytes: 90, LibvpxBytes: 100},
		},
	}
	csv := renderPlotCSV(report)
	if !strings.Contains(csv, "govpx_vmaf,libvpx_vmaf") || !strings.Contains(csv, "0,90.000000,92.000000") {
		t.Fatalf("CSV missing VMAF columns/data:\n%s", csv)
	}
	svg := renderPlotSVG(report)
	if !strings.Contains(svg, "VMAF") || !strings.Contains(svg, "govpx 60.00 fps") {
		t.Fatalf("SVG missing VMAF summary:\n%s", svg)
	}
	text := formatPlotReport(report)
	if !strings.Contains(text, "vmaf=91.000") || !strings.Contains(text, "vmaf_delta=-2.000") {
		t.Fatalf("text report missing VMAF data:\n%s", text)
	}
}
