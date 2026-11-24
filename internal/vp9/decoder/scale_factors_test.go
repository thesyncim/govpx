package decoder

import "testing"

// TestSetupScaleFactorsIdentity: same-size ref → x/y_scale_fp =
// RefNoScale, step_q4 = 16, IsScaled()==false.
func TestSetupScaleFactorsIdentity(t *testing.T) {
	var sf ScaleFactors
	SetupScaleFactorsForFrame(&sf, 640, 480, 640, 480)
	if sf.XScaleFp != RefNoScale || sf.YScaleFp != RefNoScale {
		t.Errorf("identity scale_fp got (%d,%d) want (%d,%d)",
			sf.XScaleFp, sf.YScaleFp, RefNoScale, RefNoScale)
	}
	if sf.XStepQ4 != 16 || sf.YStepQ4 != 16 {
		t.Errorf("identity step got (%d,%d) want (16,16)", sf.XStepQ4, sf.YStepQ4)
	}
	if !sf.IsValidScale() {
		t.Error("identity is not valid?")
	}
	if sf.IsScaled() {
		t.Error("identity should not be flagged as scaled")
	}
}

// TestSetupScaleFactorsHalfSize: ref is half the size in both axes —
// x_step_q4 must be 8 (each output pel maps to 0.5 ref pels).
func TestSetupScaleFactorsHalfSize(t *testing.T) {
	var sf ScaleFactors
	SetupScaleFactorsForFrame(&sf, 320, 240, 640, 480)
	wantScale := int32((320 << RefScaleShift) / 640)
	if sf.XScaleFp != wantScale || sf.YScaleFp != wantScale {
		t.Errorf("scale_fp got (%d,%d) want %d", sf.XScaleFp, sf.YScaleFp, wantScale)
	}
	if sf.XStepQ4 != 8 || sf.YStepQ4 != 8 {
		t.Errorf("step got (%d,%d) want (8,8)", sf.XStepQ4, sf.YStepQ4)
	}
	if !sf.IsScaled() {
		t.Error("half-size should be flagged as scaled")
	}
}

// TestSetupScaleFactorsRejectsOutOfRange: ref > 2x current frame in
// either axis → invalid scale.
func TestSetupScaleFactorsRejectsOutOfRange(t *testing.T) {
	var sf ScaleFactors
	SetupScaleFactorsForFrame(&sf, 4000, 4000, 100, 100)
	if sf.IsValidScale() {
		t.Error("out-of-range ref should be invalid")
	}
}

// TestScaleMvIdentityPassthrough: at identity scale the projected
// MV equals the input scaled by 1 (with sub-pel offset 0).
func TestScaleMvIdentityPassthrough(t *testing.T) {
	var sf ScaleFactors
	SetupScaleFactorsForFrame(&sf, 64, 64, 64, 64)
	in := MV{Row: 10, Col: -20}
	got := ScaleMv(in, 0, 0, &sf)
	if got.Row != 10 || got.Col != -20 {
		t.Errorf("identity ScaleMv got (%d,%d) want (10,-20)", got.Row, got.Col)
	}
}

// TestScaleMvHalfReference: when the ref frame is half the size of
// the current frame, an MV of (16, 32) projects to roughly (8, 16)
// before adding the (x,y)-based subpel offset.
func TestScaleMvHalfReference(t *testing.T) {
	var sf ScaleFactors
	SetupScaleFactorsForFrame(&sf, 32, 32, 64, 64)
	in := MV{Row: 32, Col: 16}
	got := ScaleMv(in, 0, 0, &sf)
	if got.Row != 16 || got.Col != 8 {
		t.Errorf("half-ref ScaleMv got (%d,%d) want (16,8)", got.Row, got.Col)
	}
}

// TestScaleValueXY at identity returns val unchanged via fast path.
func TestScaleValueXYAtIdentity(t *testing.T) {
	var sf ScaleFactors
	SetupScaleFactorsForFrame(&sf, 64, 64, 64, 64)
	if got := sf.ScaleValueX(123); got != 123 {
		t.Errorf("ScaleValueX got %d want 123", got)
	}
	if got := sf.ScaleValueY(456); got != 456 {
		t.Errorf("ScaleValueY got %d want 456", got)
	}
}
