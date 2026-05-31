package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestIntraCostPenaltyNoiseHighSuppressesSmallBlockReduction(t *testing.T) {
	const qindex = 37
	base := 20 * int(vp9dec.VpxDcQuant(qindex, 0, vp9dec.BitDepth8))
	cases := []struct {
		name                 string
		bsize                common.BlockSize
		noiseEstimateEnabled bool
		noiseLevel           NoiseLevel
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
			noiseLevel:           NoiseLevelHigh,
			want:                 base,
		},
		{
			name:                 "block16x16_enabled_high_no_reduction",
			bsize:                common.Block16x16,
			noiseEstimateEnabled: true,
			noiseLevel:           NoiseLevelHigh,
			want:                 base,
		},
		{
			name:                 "block8x8_disabled_high_still_reduces",
			bsize:                common.Block8x8,
			noiseEstimateEnabled: false,
			noiseLevel:           NoiseLevelHigh,
			want:                 base >> 4,
		},
		{
			name:                 "block8x8_enabled_medium_still_reduces",
			bsize:                common.Block8x8,
			noiseEstimateEnabled: true,
			noiseLevel:           NoiseLevelMedium,
			want:                 base >> 4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IntraCostPenalty(qindex, 0, tc.bsize,
				tc.noiseEstimateEnabled, tc.noiseLevel)
			if got != tc.want {
				t.Fatalf("IntraCostPenalty = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNewmvDiffBiasLowvarInput(t *testing.T) {
	cases := []struct {
		state ContentStateSB
		want  bool
	}{
		{state: ContentStateInvalid, want: false},
		{state: ContentStateLowSadLowSumdiff, want: false},
		{state: ContentStateLowVarHighSumdiff, want: true},
	}
	for _, tc := range cases {
		if got := NewmvDiffBiasLowvarInput(tc.state); got != tc.want {
			t.Fatalf("NewmvDiffBiasLowvarInput(%d) = %v, want %v",
				tc.state, got, tc.want)
		}
	}
}

func TestNonrdAllowEncodeBreakoutSceneAndMotionGates(t *testing.T) {
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
		got := NonrdAllowEncodeBreakout(tc.lossless,
			tc.sceneChangeDetected, tc.highNumBlocksWithMotion)
		if got != tc.want {
			t.Fatalf("%s: allowEncodeBreakout = %v, want %v",
				tc.name, got, tc.want)
		}
	}
}

func TestNonrdModeRDThresholdBiasGolden(t *testing.T) {
	const base = 100
	if got := NonrdModeRDThreshold(base, false, false,
		vp9dec.GoldenFrame, 5); got != base {
		t.Fatalf("plain threshold = %d, want %d", got, base)
	}
	if got := NonrdModeRDThreshold(base, true, false,
		vp9dec.GoldenFrame, 5); got != base<<1 {
		t.Fatalf("skip-txfm threshold = %d, want %d", got, base<<1)
	}
	if got := NonrdModeRDThreshold(base, false, true,
		vp9dec.GoldenFrame, 5); got != base<<3 {
		t.Fatalf("bias-golden threshold = %d, want %d", got, base<<3)
	}
	if got := NonrdModeRDThreshold(base, true, true,
		vp9dec.GoldenFrame, 5); got != base<<4 {
		t.Fatalf("combined threshold = %d, want %d", got, base<<4)
	}
	if got := NonrdModeRDThreshold(base, false, true,
		vp9dec.GoldenFrame, 4); got != base {
		t.Fatalf("early-golden threshold = %d, want %d", got, base)
	}
	if got := NonrdModeRDThreshold(base, false, true,
		vp9dec.LastFrame, 5); got != base {
		t.Fatalf("non-golden threshold = %d, want %d", got, base)
	}
}

func TestNonrdForceLastReference(t *testing.T) {
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
		if got := NonrdForceLastReference(tc.shortCircuitLowTempVar,
			tc.useNonrdPickMode, tc.forceSkipLowTempVar); got != tc.want {
			t.Fatalf("%s: forceLastReference = %v, want %v",
				tc.name, got, tc.want)
		}
	}
}

func TestNonrdIntraFallbackPrecheckSceneChangeBypassesInterGates(t *testing.T) {
	if got := NonrdIntraFallbackPrecheck(10, 20, true,
		common.Block64x64, ContentStateLowSadLowSumdiff,
		true, true, false, false, false); !got {
		t.Fatalf("scene-change precheck = false, want true")
	}
	if got := NonrdIntraFallbackPrecheck(10, 20, true,
		common.Block64x64, ContentStateLowSadLowSumdiff,
		true, false, false, false, false); got {
		t.Fatalf("non-scene precheck = true, want false")
	}
}

func TestNonrdIntraFallbackPrecheckVeryHighSadBypassesLowTempSkip(t *testing.T) {
	if got := NonrdIntraFallbackPrecheck(30, 20, true,
		common.Block64x64, ContentStateLowSadLowSumdiff,
		false, false, false, false, false); got {
		t.Fatalf("low-temp precheck = true, want false")
	}
	if got := NonrdIntraFallbackPrecheck(30, 20, true,
		common.Block64x64, ContentStateVeryHighSad,
		false, false, false, false, false); !got {
		t.Fatalf("very-high-SAD precheck = false, want true")
	}
}

func TestNonrdIntraFallbackPrecheckScreenFlatBypassesInterGates(t *testing.T) {
	if got := NonrdIntraFallbackPrecheck(10, 20, true,
		common.Block64x64, ContentStateLowSadLowSumdiff,
		true, false, true, false, false); !got {
		t.Fatalf("screen-flat precheck = false, want true")
	}
	if got := NonrdIntraFallbackPrecheck(10, 20, true,
		common.Block64x64, ContentStateLowSadLowSumdiff,
		true, false, false, false, false); got {
		t.Fatalf("non-screen-flat precheck = true, want false")
	}
}

func TestNonrdNormalizeSSEUsesPixelCount(t *testing.T) {
	const sse = 4096
	if got := NonrdNormalizeSSE(sse, common.Block16x16); got != 16 {
		t.Fatalf("16x16 normalized SSE = %d, want 16", got)
	}
	if got := NonrdNormalizeSSE(sse, common.Block8x8); got != 64 {
		t.Fatalf("8x8 normalized SSE = %d, want 64", got)
	}
	if got := NonrdNormalizeSSE(sse, common.BlockSizes); got != sse {
		t.Fatalf("invalid-block normalized SSE = %d, want %d", got, sse)
	}
}

func TestNonrdScreenZeroLastBias(t *testing.T) {
	zeroMV := vp9dec.MV{}
	if !NonrdScreenZeroLastBias(true, true, false,
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
		if got := NonrdScreenZeroLastBias(tc.screen, tc.sceneChangeDetected,
			tc.highNumBlocksWithMotion, tc.refFrame, tc.mv,
			tc.sourceVariance, tc.sseY); got {
			t.Fatalf("%s: screen zero-LAST bias = true, want false", tc.name)
		}
	}
}

func TestNonrdSkipScreenContentCandidate(t *testing.T) {
	zeroMV := vp9dec.MV{}
	nonZeroMV := vp9dec.MV{Col: 4}
	cases := []struct {
		name              string
		screen            bool
		sourceSADReady    bool
		refFrame          int8
		mv                vp9dec.MV
		mvValid           bool
		sourceVariance    uint
		zeroTempSADSource bool
		want              bool
	}{
		{
			name:              "stationary source skips nonzero motion",
			screen:            true,
			sourceSADReady:    true,
			refFrame:          vp9dec.LastFrame,
			mv:                nonZeroMV,
			mvValid:           true,
			zeroTempSADSource: true,
			want:              true,
		},
		{
			name:              "moving flat source skips zero last",
			screen:            true,
			sourceSADReady:    true,
			refFrame:          vp9dec.LastFrame,
			mv:                zeroMV,
			mvValid:           true,
			zeroTempSADSource: false,
			want:              true,
		},
		{
			name:              "moving flat source keeps zero golden",
			screen:            true,
			sourceSADReady:    true,
			refFrame:          vp9dec.GoldenFrame,
			mv:                zeroMV,
			mvValid:           true,
			zeroTempSADSource: false,
		},
		{
			name:           "no source sad skips nonzero flat",
			screen:         true,
			refFrame:       vp9dec.LastFrame,
			mv:             nonZeroMV,
			mvValid:        true,
			sourceVariance: 0,
			want:           true,
		},
		{
			name:              "nonflat source keeps zero last",
			screen:            true,
			sourceSADReady:    true,
			refFrame:          vp9dec.LastFrame,
			mv:                zeroMV,
			mvValid:           true,
			sourceVariance:    1,
			zeroTempSADSource: false,
		},
		{
			name:           "non-screen keeps candidate",
			refFrame:       vp9dec.LastFrame,
			mv:             nonZeroMV,
			mvValid:        true,
			sourceVariance: 0,
		},
		{
			name:           "invalid mv waits for later candidate gate",
			screen:         true,
			refFrame:       vp9dec.LastFrame,
			sourceVariance: 0,
		},
	}
	for _, tc := range cases {
		got := NonrdSkipScreenContentCandidate(tc.screen, tc.sourceSADReady,
			tc.refFrame, tc.mv, tc.mvValid, tc.sourceVariance,
			tc.zeroTempSADSource)
		if got != tc.want {
			t.Fatalf("%s: skip = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNeighborIsInter(t *testing.T) {
	if NeighborIsInter(nil) {
		t.Fatalf("nil neighbor is inter")
	}
	if NeighborIsInter(&vp9dec.NeighborMi{RefFrame: [2]int8{vp9dec.IntraFrame}}) {
		t.Fatalf("intra neighbor is inter")
	}
	if !NeighborIsInter(&vp9dec.NeighborMi{RefFrame: [2]int8{vp9dec.LastFrame}}) {
		t.Fatalf("LAST_FRAME neighbor is not inter")
	}
}
