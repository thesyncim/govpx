package decoder

import "testing"

// TestAdaptModeProbsConstantsMatchLibvpx pins every adaptation saturation
// constant govpx uses to its libvpx value. Drift in any of these breaks the
// per-frame Bayesian probability update and accumulates bitstream error every
// frame.
func TestAdaptModeProbsConstantsMatchLibvpx(t *testing.T) {
	cases := []struct {
		name string
		got  uint32
		want uint32
	}{
		// libvpx: vpx_dsp/prob.h:37.
		{"MODE_MV_COUNT_SAT", modeMvCountSat, 20},
		// libvpx: vpx_dsp/prob.h:78-82 — last entry of count_to_update_factor.
		{"MODE_MV_MAX_UPDATE_FACTOR", modeMvMaxUpdateFactor, 128},
		// libvpx: vp9/common/vp9_entropy.c:1048.
		{"COEF_COUNT_SAT", coefCountSatInterAfterInter, 24},
		// libvpx: vp9/common/vp9_entropy.c:1049.
		{"COEF_MAX_UPDATE_FACTOR", coefMaxUpdateFactorInterAfterInter, 112},
		// libvpx: vp9/common/vp9_entropy.c:1050.
		{"COEF_COUNT_SAT_KEY", coefCountSatIntraOnly, 24},
		// libvpx: vp9/common/vp9_entropy.c:1051.
		{"COEF_MAX_UPDATE_FACTOR_KEY", coefMaxUpdateFactorIntraOnly, 112},
		// libvpx: vp9/common/vp9_entropy.c:1052.
		{"COEF_COUNT_SAT_AFTER_KEY", coefCountSatInterAfterKey, 24},
		// libvpx: vp9/common/vp9_entropy.c:1053.
		{"COEF_MAX_UPDATE_FACTOR_AFTER_KEY", coefMaxUpdateFactorInterAfterKey, 128},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d (libvpx)", tc.name, tc.got, tc.want)
		}
	}
}

func TestAdaptCoefProbsMatchesLibvpxFormula(t *testing.T) {
	cases := []struct {
		name     string
		prePrior uint8
		ct       [2]uint32
		countSat uint32
		maxUpd   uint32
		want     uint8
	}{
		{"zero_counts_inter", 100, [2]uint32{0, 0}, 24, 112, 100},
		{"all_zero_inter", 128, [2]uint32{24, 0}, 24, 112, ((128 * (256 - 112)) + (255 * 112) + 128) >> 8},
		{"all_one_inter", 128, [2]uint32{0, 24}, 24, 112, ((128 * (256 - 112)) + (1 * 112) + 128) >> 8},
		{"balanced_sat_inter", 200, [2]uint32{12, 12}, 24, 112, ((200 * (256 - 112)) + (128 * 112) + 128) >> 8},
		{"sub_sat_inter", 100, [2]uint32{6, 6}, 24, 112, ((100 * (256 - (112 * 12 / 24))) + (128 * (112 * 12 / 24)) + 128) >> 8},
		{"after_key_balanced", 64, [2]uint32{12, 12}, 24, 128, ((64 * (256 - 128)) + (128 * 128) + 128) >> 8},
	}
	for _, tc := range cases {
		got := mergeProbs(tc.prePrior, tc.ct, tc.countSat, tc.maxUpd)
		if got != tc.want {
			t.Errorf("%s: mergeProbs(pre=%d, ct=%v, sat=%d, upd=%d) = %d, want %d",
				tc.name, tc.prePrior, tc.ct, tc.countSat, tc.maxUpd, got, tc.want)
		}
	}
}

func TestModeMvMergeProbsMatchesLibvpxFormula(t *testing.T) {
	wantFactorTable := [21]uint32{
		0, 6, 12, 19, 25, 32, 38, 44, 51, 57, 64,
		70, 76, 83, 89, 96, 102, 108, 115, 121, 128,
	}
	for i, wantFactor := range wantFactorTable {
		const pre = uint8(64)
		got := modeMvMergeProbs(pre, [2]uint32{uint32(i), 0})
		if i == 0 {
			if got != pre {
				t.Errorf("ct=(0,0): got %d, want pre_prob=%d", got, pre)
			}
			continue
		}
		want := uint8(((uint32(pre) * (256 - wantFactor)) + 255*wantFactor + 128) >> 8)
		if got != want {
			t.Errorf("ct=(%d,0): got %d, want %d (factor=%d)",
				i, got, want, wantFactor)
		}
	}
}
