package encoder

import "testing"

func TestDenoiserModeMappingMatchesLibvpx(t *testing.T) {
	cases := []struct {
		level    int
		wantMode int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 3},
		{4, 2},
		{5, 2},
		{6, 2},
	}
	for _, c := range cases {
		if got := DenoiserModeForSensitivity(c.level); got != c.wantMode {
			t.Fatalf("noise_sensitivity %d -> mode %d, want %d", c.level, got, c.wantMode)
		}
	}
}

func TestDenoiserSetParametersMatchesLibvpxModes(t *testing.T) {
	for _, mode := range []int{1, 2} {
		kind, params := DenoiserSetParameters(mode)
		if mode == 1 && kind != DenoiserOnYOnly {
			t.Fatalf("mode=1 kind = %d, want DenoiserOnYOnly", kind)
		}
		if mode == 2 && kind != DenoiserOnYUV {
			t.Fatalf("mode=2 kind = %d, want DenoiserOnYUV", kind)
		}
		if params.ScaleSSEThresh != 1 || params.ScaleMotionThresh != 8 || params.ScaleIncreaseFilter != 0 || params.DenoiseMVBias != 95 || params.PickmodeMVBias != 100 || params.QPThresh != 0 {
			t.Fatalf("non-aggressive params for mode=%d = %+v, want libvpx defaults", mode, params)
		}
	}
	kind, params := DenoiserSetParameters(3)
	if kind != DenoiserOnYUVAggressive {
		t.Fatalf("mode=3 kind = %d, want DenoiserOnYUVAggressive", kind)
	}
	if params.ScaleSSEThresh != 2 || params.ScaleMotionThresh != 16 || params.ScaleIncreaseFilter != 1 || params.DenoiseMVBias != 60 || params.PickmodeMVBias != 75 || params.QPThresh != 80 || params.ConsecZeroLast != 15 {
		t.Fatalf("aggressive params = %+v, want libvpx aggressive defaults", params)
	}
}

func TestDenoiserFilterYReturnsCopyForSharpDifference(t *testing.T) {
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		mc[i] = 250
	}
	for i := range sig {
		sig[i] = 0
	}
	if got := DenoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false); got != DenoiserCopyBlock {
		t.Fatalf("max-divergence filter decision = %d, want COPY_BLOCK", got)
	}
}

func TestDenoiserFilterYUsesMCWhenAbsdiffSmall(t *testing.T) {
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		mc[i] = 130
	}
	for i := range sig {
		sig[i] = 128
	}
	if got := DenoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false); got != DenoiserFilterBlock {
		t.Fatalf("small-diff filter decision = %d, want FILTER_BLOCK", got)
	}
	for i := range avg {
		if avg[i] != 130 {
			t.Fatalf("avg[%d] = %d, want 130 (mc value taken when |diff|<=3)", i, avg[i])
		}
	}
}

func TestDenoiserWindowOK(t *testing.T) {
	const (
		w = 16
		h = 16
	)
	packed := make([]byte, w*h)
	if !denoiserWindowOK(packed, w, w, h) {
		t.Fatal("packed window was rejected")
	}

	const stride = 20
	strided := make([]byte, (h-1)*stride+w)
	if !denoiserWindowOK(strided, stride, w, h) {
		t.Fatal("strided window was rejected")
	}
	if denoiserWindowOK(strided[:len(strided)-1], stride, w, h) {
		t.Fatal("short strided window was accepted")
	}
	if denoiserWindowOK(packed, w-1, w, h) {
		t.Fatal("too-small stride was accepted")
	}
	if denoiserWindowOK(packed, -w, w, h) {
		t.Fatal("negative stride was accepted")
	}
	if denoiserWindowOK(packed, w, 0, h) {
		t.Fatal("zero width was accepted")
	}
	if denoiserWindowOK(packed, w, w, 0) {
		t.Fatal("zero height was accepted")
	}

	maxInt := int(^uint(0) >> 1)
	if denoiserWindowOK(nil, (maxInt-w)/(h-1)+1, w, h) {
		t.Fatal("overflowing window was accepted")
	}
}

func TestDenoiserWindowsOKRejectsAnyShortPlane(t *testing.T) {
	const (
		w = 8
		h = 8
	)
	full := make([]byte, w*h)
	short := make([]byte, w*h-1)
	if !denoiserWindowsOK(full, w, full, w, full, w, w, h) {
		t.Fatal("valid triple window was rejected")
	}
	if denoiserWindowsOK(short, w, full, w, full, w, w, h) {
		t.Fatal("short mc window was accepted")
	}
	if denoiserWindowsOK(full, w, short, w, full, w, w, h) {
		t.Fatal("short avg window was accepted")
	}
	if denoiserWindowsOK(full, w, full, w, short, w, w, h) {
		t.Fatal("short sig window was accepted")
	}
}

func TestDenoiserFilterUVCopiesNearNeutralBlocks(t *testing.T) {
	mc := make([]byte, 8*8)
	avg := make([]byte, 8*8)
	sig := make([]byte, 8*8)
	for i := range sig {
		sig[i] = 128
	}
	if got := DenoiserFilterUV(mc, 8, avg, 8, sig, 8, 0, false); got != DenoiserCopyBlock {
		t.Fatalf("near-neutral UV filter = %d, want COPY_BLOCK", got)
	}
}

func TestDenoiserFilterUVNearNeutralDoesNotRequireMCAvg(t *testing.T) {
	sig := make([]byte, 8*8)
	for i := range sig {
		sig[i] = 128
	}
	if got := DenoiserFilterUV(nil, 8, nil, 8, sig, 8, 0, false); got != DenoiserCopyBlock {
		t.Fatalf("near-neutral UV filter with nil mc/avg = %d, want COPY_BLOCK", got)
	}
}
