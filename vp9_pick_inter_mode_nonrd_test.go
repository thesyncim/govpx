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

func TestVP9NewmvDiffBiasLowvarInput(t *testing.T) {
	cases := []struct {
		state vp9ContentStateSB
		want  bool
	}{
		{state: vp9ContentStateInvalid, want: false},
		{state: vp9ContentStateLowSadLowSumdiff, want: false},
		{state: vp9ContentStateLowVarHighSumdiff, want: true},
	}
	for _, tc := range cases {
		if got := vp9NewmvDiffBiasLowvarInput(tc.state); got != tc.want {
			t.Fatalf("vp9NewmvDiffBiasLowvarInput(%d) = %v, want %v",
				tc.state, got, tc.want)
		}
	}
}

func TestVP9NonrdAllowEncodeBreakoutSceneAndMotionGates(t *testing.T) {
	cases := []struct {
		name                    string
		lossless                bool
		sceneChangeDetected     bool
		highNumBlocksWithMotion bool
		want                    bool
	}{
		{name: "plain", want: true},
		{name: "lossless", lossless: true},
		{name: "scene change", sceneChangeDetected: true},
		{name: "high motion", highNumBlocksWithMotion: true},
	}
	for _, tc := range cases {
		got := vp9NonrdAllowEncodeBreakout(tc.lossless,
			tc.sceneChangeDetected, tc.highNumBlocksWithMotion)
		if got != tc.want {
			t.Fatalf("%s: allowEncodeBreakout = %v, want %v",
				tc.name, got, tc.want)
		}
	}
}

func TestVP9NonrdModeRdThresholdBiasGolden(t *testing.T) {
	const base = 100
	if got := vp9NonrdModeRdThreshold(base, false, false,
		vp9dec.GoldenFrame, 5); got != base {
		t.Fatalf("plain threshold = %d, want %d", got, base)
	}
	if got := vp9NonrdModeRdThreshold(base, true, false,
		vp9dec.GoldenFrame, 5); got != base<<1 {
		t.Fatalf("skip-txfm threshold = %d, want %d", got, base<<1)
	}
	if got := vp9NonrdModeRdThreshold(base, false, true,
		vp9dec.GoldenFrame, 5); got != base<<3 {
		t.Fatalf("bias-golden threshold = %d, want %d", got, base<<3)
	}
	if got := vp9NonrdModeRdThreshold(base, true, true,
		vp9dec.GoldenFrame, 5); got != base<<4 {
		t.Fatalf("combined threshold = %d, want %d", got, base<<4)
	}
	if got := vp9NonrdModeRdThreshold(base, false, true,
		vp9dec.GoldenFrame, 4); got != base {
		t.Fatalf("early-golden threshold = %d, want %d", got, base)
	}
	if got := vp9NonrdModeRdThreshold(base, false, true,
		vp9dec.LastFrame, 5); got != base {
		t.Fatalf("non-golden threshold = %d, want %d", got, base)
	}
}

func TestVP9NonrdForceLastReference(t *testing.T) {
	cases := []struct {
		name                   string
		shortCircuitLowTempVar int
		useNonrdPickMode       bool
		forceSkipLowTempVar    bool
		want                   bool
	}{
		{name: "level1 force", shortCircuitLowTempVar: 1, useNonrdPickMode: true, forceSkipLowTempVar: true, want: true},
		{name: "level3 force", shortCircuitLowTempVar: 3, useNonrdPickMode: true, forceSkipLowTempVar: true, want: true},
		{name: "level2 does not force ref fan", shortCircuitLowTempVar: 2, useNonrdPickMode: true, forceSkipLowTempVar: true},
		{name: "no low temp block", shortCircuitLowTempVar: 3, useNonrdPickMode: true},
		{name: "not nonrd", shortCircuitLowTempVar: 3, forceSkipLowTempVar: true},
	}
	for _, tc := range cases {
		if got := vp9NonrdForceLastReference(tc.shortCircuitLowTempVar,
			tc.useNonrdPickMode, tc.forceSkipLowTempVar); got != tc.want {
			t.Fatalf("%s: forceLastReference = %v, want %v",
				tc.name, got, tc.want)
		}
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

func TestVP9NonrdIntraFallbackPrecheckSceneChangeBypassesInterGates(t *testing.T) {
	if got := vp9NonrdIntraFallbackPrecheck(10, 20, true,
		common.Block64x64, vp9ContentStateLowSadLowSumdiff,
		true, true, false); !got {
		t.Fatalf("scene-change precheck = false, want true")
	}
	if got := vp9NonrdIntraFallbackPrecheck(10, 20, true,
		common.Block64x64, vp9ContentStateLowSadLowSumdiff,
		true, false, false); got {
		t.Fatalf("non-scene precheck = true, want false")
	}
}

func TestVP9NonrdIntraFallbackPrecheckVeryHighSadBypassesLowTempSkip(t *testing.T) {
	if got := vp9NonrdIntraFallbackPrecheck(30, 20, true,
		common.Block64x64, vp9ContentStateLowSadLowSumdiff,
		false, false, false); got {
		t.Fatalf("low-temp precheck = true, want false")
	}
	if got := vp9NonrdIntraFallbackPrecheck(30, 20, true,
		common.Block64x64, vp9ContentStateVeryHighSad,
		false, false, false); !got {
		t.Fatalf("very-high-SAD precheck = false, want true")
	}
}

func TestVP9NonrdIntraFallbackPrecheckScreenFlatBypassesInterGates(t *testing.T) {
	if got := vp9NonrdIntraFallbackPrecheck(10, 20, true,
		common.Block64x64, vp9ContentStateLowSadLowSumdiff,
		true, false, true); !got {
		t.Fatalf("screen-flat precheck = false, want true")
	}
	if got := vp9NonrdIntraFallbackPrecheck(10, 20, true,
		common.Block64x64, vp9ContentStateLowSadLowSumdiff,
		true, false, false); got {
		t.Fatalf("non-screen-flat precheck = true, want false")
	}
}

func TestVP9NonrdNormalizeSSEUsesPixelCount(t *testing.T) {
	const sse = 4096
	if got := vp9NonrdNormalizeSSE(sse, common.Block16x16); got != 16 {
		t.Fatalf("16x16 normalized SSE = %d, want 16", got)
	}
	if got := vp9NonrdNormalizeSSE(sse, common.Block8x8); got != 64 {
		t.Fatalf("8x8 normalized SSE = %d, want 64", got)
	}
	if got := vp9NonrdNormalizeSSE(sse, common.BlockSizes); got != sse {
		t.Fatalf("invalid-block normalized SSE = %d, want %d", got, sse)
	}
}

func TestVP9SourceVariancePerPixel(t *testing.T) {
	const side = 16
	buf := make([]byte, side*side)
	for i := range buf {
		buf[i] = 128
	}
	if got := vp9SourceVariancePerPixel(buf, side, 0, 0, side, side,
		common.Block16x16); got != 0 {
		t.Fatalf("flat source variance = %d, want 0", got)
	}

	for i := range buf {
		if i%2 == 0 {
			buf[i] = 0
		} else {
			buf[i] = 255
		}
	}
	if got := vp9SourceVariancePerPixel(buf, side, 0, 0, side, side,
		common.Block16x16); got != 16256 {
		t.Fatalf("checker source variance = %d, want 16256", got)
	}
}

func TestVP9NonrdScreenZeroLastBias(t *testing.T) {
	zeroMV := vp9dec.MV{}
	if !vp9NonrdScreenZeroLastBias(true, true, false,
		vp9dec.LastFrame, zeroMV, 0, 1) {
		t.Fatalf("screen zero-LAST bias = false, want true")
	}
	cases := []struct {
		name                    string
		screen                  bool
		sceneChangeDetected     bool
		highNumBlocksWithMotion bool
		refFrame                int8
		mv                      vp9dec.MV
		sourceVariance          uint
		sseY                    uint64
	}{
		{name: "not screen", sceneChangeDetected: true, refFrame: vp9dec.LastFrame, sseY: 1},
		{name: "no scene or high motion", screen: true, refFrame: vp9dec.LastFrame, sseY: 1},
		{name: "non last", screen: true, sceneChangeDetected: true, refFrame: vp9dec.GoldenFrame, sseY: 1},
		{name: "nonzero mv", screen: true, sceneChangeDetected: true, refFrame: vp9dec.LastFrame, mv: vp9dec.MV{Row: 1}, sseY: 1},
		{name: "nonflat source", screen: true, sceneChangeDetected: true, refFrame: vp9dec.LastFrame, sourceVariance: 1, sseY: 1},
		{name: "zero sse", screen: true, sceneChangeDetected: true, refFrame: vp9dec.LastFrame},
	}
	for _, tc := range cases {
		if got := vp9NonrdScreenZeroLastBias(tc.screen, tc.sceneChangeDetected,
			tc.highNumBlocksWithMotion, tc.refFrame, tc.mv,
			tc.sourceVariance, tc.sseY); got {
			t.Fatalf("%s: screen zero-LAST bias = true, want false", tc.name)
		}
	}
}
