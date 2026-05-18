package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9GetIntraCostPenaltyNoiseHighSuppressesSmallBlockReduction(t *testing.T) {
	const qindex = 37
	base := 20 * int(vp9dec.VpxDcQuant(qindex, 0, vp9dec.BitDepth8))
	cases := []struct {
		name                 string
		bsize                common.BlockSize
		noiseEstimateEnabled bool
		noiseLevel           vp9NoiseLevel
		want                 int
	}{
		{
			name:  "block8x8_disabled_reduces_by_4",
			bsize: common.Block8x8,
			want:  base >> 4,
		},
		{
			name:  "block16x16_disabled_reduces_by_2",
			bsize: common.Block16x16,
			want:  base >> 2,
		},
		{
			name:  "block32x32_disabled_no_reduction",
			bsize: common.Block32x32,
			want:  base,
		},
		{
			name:                 "block8x8_enabled_high_no_reduction",
			bsize:                common.Block8x8,
			noiseEstimateEnabled: true,
			noiseLevel:           vp9NoiseLevelHigh,
			want:                 base,
		},
		{
			name:                 "block16x16_enabled_high_no_reduction",
			bsize:                common.Block16x16,
			noiseEstimateEnabled: true,
			noiseLevel:           vp9NoiseLevelHigh,
			want:                 base,
		},
		{
			name:                 "block8x8_disabled_high_still_reduces",
			bsize:                common.Block8x8,
			noiseEstimateEnabled: false,
			noiseLevel:           vp9NoiseLevelHigh,
			want:                 base >> 4,
		},
		{
			name:                 "block8x8_enabled_medium_still_reduces",
			bsize:                common.Block8x8,
			noiseEstimateEnabled: true,
			noiseLevel:           vp9NoiseLevelMedium,
			want:                 base >> 4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := vp9GetIntraCostPenalty(qindex, 0, tc.bsize,
				tc.noiseEstimateEnabled, tc.noiseLevel)
			if got != tc.want {
				t.Fatalf("vp9GetIntraCostPenalty = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestVP9NewmvDiffBiasNoiseInputs(t *testing.T) {
	cases := []struct {
		name        string
		ne          vp9NoiseEstimateState
		wantEnabled bool
		wantMedium  bool
	}{
		{
			name: "disabled_high_value_stays_disabled",
			ne: vp9NoiseEstimateState{
				enabled: false,
				thresh:  115,
				value:   300,
			},
			wantEnabled: false,
			wantMedium:  false,
		},
		{
			name: "enabled_low_below_medium",
			ne: vp9NoiseEstimateState{
				enabled: true,
				thresh:  115,
				value:   90,
			},
			wantEnabled: true,
			wantMedium:  false,
		},
		{
			name: "enabled_medium_or_higher",
			ne: vp9NoiseEstimateState{
				enabled: true,
				thresh:  115,
				value:   116,
			},
			wantEnabled: true,
			wantMedium:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &VP9Encoder{noiseEstimate: tc.ne}
			gotEnabled, gotMedium := e.vp9NewmvDiffBiasNoiseInputs()
			if gotEnabled != tc.wantEnabled || gotMedium != tc.wantMedium {
				t.Fatalf("noise inputs = (%v,%v), want (%v,%v)",
					gotEnabled, gotMedium, tc.wantEnabled, tc.wantMedium)
			}
		})
	}
}
