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
