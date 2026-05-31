package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9NewmvDiffBiasNoiseInputs(t *testing.T) {
	cases := []struct {
		name        string
		ne          encoder.NoiseEstimateState
		wantEnabled bool
		wantMedium  bool
	}{
		{
			name: "disabled_high_value_stays_disabled",
			ne: encoder.NoiseEstimateState{
				Enabled: false,
				Thresh:  115,
				Value:   300,
			},
			wantEnabled: false,
			wantMedium:  false,
		},
		{
			name: "enabled_low_below_medium",
			ne: encoder.NoiseEstimateState{
				Enabled: true,
				Thresh:  115,
				Value:   90,
			},
			wantEnabled: true,
			wantMedium:  false,
		},
		{
			name: "enabled_medium_or_higher",
			ne: encoder.NoiseEstimateState{
				Enabled: true,
				Thresh:  115,
				Value:   116,
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

func TestVP9VarPartForceSkipLowTempVarOK(t *testing.T) {
	e := &VP9Encoder{}
	e.sf.ShortCircuitLowTempVar = 3
	if force, ok := e.vp9VarPartForceSkipLowTempVarOK(8, 0, 0,
		common.Block32x32); ok || force {
		t.Fatalf("missing cache force=%v ok=%v, want false/false", force, ok)
	}

	e.varPartSBVarLow = make([][25]uint8, 1)
	e.varPartSBComputed = make([]bool, 1)
	if force, ok := e.vp9VarPartForceSkipLowTempVarOK(8, 0, 0,
		common.Block32x32); ok || force {
		t.Fatalf("uncomputed cache force=%v ok=%v, want false/false", force, ok)
	}

	e.varPartSBComputed[0] = true
	if force, ok := e.vp9VarPartForceSkipLowTempVarOK(8, 0, 0,
		common.Block32x32); !ok || force {
		t.Fatalf("computed non-low cache force=%v ok=%v, want false/true", force, ok)
	}

	e.varPartSBVarLow[0][5] = 1
	if force, ok := e.vp9VarPartForceSkipLowTempVarOK(8, 0, 0,
		common.Block32x32); !ok || !force {
		t.Fatalf("computed low cache force=%v ok=%v, want true/true", force, ok)
	}
}

func TestVP9UseModelYrdLargeBlockContentStateGate(t *testing.T) {
	e := &VP9Encoder{
		opts: VP9EncoderOptions{
			RateControlMode:    RateControlCBR,
			RateControlModeSet: true,
			CpuUsed:            8,
		},
	}
	if !e.vp9UseModelYrdLargeBlock(common.Block32x32,
		encoder.ContentStateLowSadLowSumdiff) {
		t.Fatal("speed8 low-content Block32x32 = false, want true")
	}
	if e.vp9UseModelYrdLargeBlock(common.Block32x32,
		encoder.ContentStateVeryHighSad) {
		t.Fatal("speed8 very-high-SAD Block32x32 = true, want false")
	}
	if !e.vp9UseModelYrdLargeBlock(common.Block64x64,
		encoder.ContentStateVeryHighSad) {
		t.Fatal("speed8 very-high-SAD Block64x64 = false, want true")
	}

	e.opts.CpuUsed = 6
	if e.vp9UseModelYrdLargeBlock(common.Block32x32,
		encoder.ContentStateInvalid) {
		t.Fatal("speed6 Block32x32 = true, want false")
	}
	if !e.vp9UseModelYrdLargeBlock(common.Block64x64,
		encoder.ContentStateInvalid) {
		t.Fatal("speed6 Block64x64 = false, want true")
	}

	e.opts.RateControlModeSet = false
	if e.vp9UseModelYrdLargeBlock(common.Block64x64,
		encoder.ContentStateInvalid) {
		t.Fatal("rate-control-disabled Block64x64 = true, want false")
	}
}
