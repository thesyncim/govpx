package benchcmd

import (
	"strings"
	"testing"
)

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
