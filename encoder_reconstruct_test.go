package govpx

import (
	"errors"
	"math"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

var benchmarkInterReference interAnalysisReference
var benchmarkInterMV vp8enc.MotionVector
var benchmarkBool bool

const testInterSearchQIndex = 20

func TestInterAnalysisSearchConfigMirrorsLibvpxRealtimeThresholds(t *testing.T) {
	tests := []struct {
		name       string
		deadline   Deadline
		cpuUsed    int
		fullPixel  interAnalysisFullPixelSearchMethod
		stepParam  int
		further    int
		improved   bool
		fractional interAnalysisFractionalSearchMethod
	}{
		{
			name:       "best RD uses first step directly",
			deadline:   DeadlineBestQuality,
			cpuUsed:    8,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  0,
			further:    7,
			improved:   true,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "good uses nstep iterative",
			deadline:   DeadlineGoodQuality,
			cpuUsed:    8,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  4,
			further:    0,
			improved:   true,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "realtime speed three RD uses first step directly",
			deadline:   DeadlineRealtime,
			cpuUsed:    3,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  1,
			further:    6,
			improved:   true,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "realtime speed four keeps nstep-equivalent baseline",
			deadline:   DeadlineRealtime,
			cpuUsed:    4,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  2,
			further:    5,
			improved:   true,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "realtime speed five switches to hex and step subpixel",
			deadline:   DeadlineRealtime,
			cpuUsed:    5,
			fullPixel:  interAnalysisFullPixelSearchHex,
			stepParam:  2,
			further:    5,
			improved:   true,
			fractional: interAnalysisFractionalSearchStep,
		},
		{
			name:       "realtime speed nine keeps hex and half-pixel only",
			deadline:   DeadlineRealtime,
			cpuUsed:    9,
			fullPixel:  interAnalysisFullPixelSearchHex,
			stepParam:  4,
			further:    0,
			improved:   false,
			fractional: interAnalysisFractionalSearchHalf,
		},
		{
			name:       "realtime speed fifteen skips fractional search",
			deadline:   DeadlineRealtime,
			cpuUsed:    15,
			fullPixel:  interAnalysisFullPixelSearchHex,
			stepParam:  4,
			further:    0,
			improved:   false,
			fractional: interAnalysisFractionalSearchSkip,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			cfg := e.interAnalysisSearchConfig()
			if cfg.fullPixelSearch != tt.fullPixel || cfg.fullPixelSearchParam != tt.stepParam || cfg.fullPixelFurtherSteps != tt.further || cfg.improvedMVPrediction != tt.improved || cfg.fractionalSearch != tt.fractional {
				t.Fatalf("config = {%d %d %d %t %d}, want {%d %d %d %t %d}", cfg.fullPixelSearch, cfg.fullPixelSearchParam, cfg.fullPixelFurtherSteps, cfg.improvedMVPrediction, cfg.fractionalSearch, tt.fullPixel, tt.stepParam, tt.further, tt.improved, tt.fractional)
			}
		})
	}
}

func TestInterFrameImprovedMVPredictionGateMirrorsLibvpxQualities(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality keeps improved MV prediction", deadline: DeadlineBestQuality, cpuUsed: 15, want: true},
		{name: "good quality keeps improved MV prediction", deadline: DeadlineGoodQuality, cpuUsed: 8, want: true},
		{name: "realtime speed six keeps improved MV prediction", deadline: DeadlineRealtime, cpuUsed: 6, want: true},
		{name: "realtime speed seven disables improved MV prediction", deadline: DeadlineRealtime, cpuUsed: 7, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := libvpxInterFrameImprovedMVPrediction(tt.deadline, tt.cpuUsed); got != tt.want {
				t.Fatalf("improved MV prediction = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLibvpxOptimizeCoefficientsGateMirrorsSpeedFeatures(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality optimizes", deadline: DeadlineBestQuality, cpuUsed: 15, want: true},
		{name: "good speed zero optimizes", deadline: DeadlineGoodQuality, cpuUsed: 0, want: true},
		{name: "good speed one disables", deadline: DeadlineGoodQuality, cpuUsed: 1, want: false},
		{name: "realtime speed zero disables", deadline: DeadlineRealtime, cpuUsed: 0, want: false},
		{name: "realtime speed eight disables", deadline: DeadlineRealtime, cpuUsed: 8, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.libvpxOptimizeCoefficients(); got != tt.want {
				t.Fatalf("optimize coefficients = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLibvpxUseFastQuantGateMirrorsSpeedFeatures(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality uses regular quant", deadline: DeadlineBestQuality, cpuUsed: 15, want: false},
		{name: "good speed two uses regular quant", deadline: DeadlineGoodQuality, cpuUsed: 2, want: false},
		{name: "good speed three uses fast quant", deadline: DeadlineGoodQuality, cpuUsed: 3, want: true},
		{name: "realtime speed zero uses regular quant", deadline: DeadlineRealtime, cpuUsed: 0, want: false},
		{name: "realtime speed one uses fast quant", deadline: DeadlineRealtime, cpuUsed: 1, want: true},
		{name: "realtime speed eight uses fast quant", deadline: DeadlineRealtime, cpuUsed: 8, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.libvpxUseFastQuant(); got != tt.want {
				t.Fatalf("fast quant = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLibvpxFrameQuantDeltas(t *testing.T) {
	tests := []struct {
		name              string
		qIndex            int
		screenContentMode int
		want              vp8common.QuantDeltas
	}{
		{name: "q zero y2 dc", qIndex: 0, want: vp8common.QuantDeltas{Y2DC: 4}},
		{name: "q three y2 dc", qIndex: 3, want: vp8common.QuantDeltas{Y2DC: 1}},
		{name: "q four neutral", qIndex: 4, want: vp8common.QuantDeltas{}},
		{name: "screen q eighty uv", qIndex: 80, screenContentMode: 1, want: vp8common.QuantDeltas{UVDC: -12, UVAC: -12}},
		{name: "screen q one twenty seven clamps uv", qIndex: 127, screenContentMode: 1, want: vp8common.QuantDeltas{UVDC: -15, UVAC: -15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := libvpxFrameQuantDeltas(tt.qIndex, tt.screenContentMode); got != tt.want {
				t.Fatalf("quant deltas = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestInterAnalysisSplitPartitionOrderMirrorsLibvpxCompressorSpeed(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		want     [vp8tables.NumMBSplits]int
	}{
		{
			name:     "best quality keeps original exhaustive order",
			deadline: DeadlineBestQuality,
			want:     [vp8tables.NumMBSplits]int{0, 1, 2, 3},
		},
		{
			name:     "good quality checks 8x8 before elongated splits",
			deadline: DeadlineGoodQuality,
			want:     [vp8tables.NumMBSplits]int{2, 1, 0, 3},
		},
		{
			name:     "realtime checks 8x8 before elongated splits",
			deadline: DeadlineRealtime,
			want:     [vp8tables.NumMBSplits]int{2, 1, 0, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline}}
			if got := e.interAnalysisSplitPartitionOrder(); got != tt.want {
				t.Fatalf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInterAnalysisRDModeDecisionMirrorsLibvpxSpeedFeature(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality keeps RD mode decision", deadline: DeadlineBestQuality, cpuUsed: 8, want: true},
		{name: "good speed three keeps RD mode decision", deadline: DeadlineGoodQuality, cpuUsed: 3, want: true},
		{name: "good speed four uses fast pick mode", deadline: DeadlineGoodQuality, cpuUsed: 4, want: false},
		{name: "realtime speed three keeps RD mode decision", deadline: DeadlineRealtime, cpuUsed: 3, want: true},
		{name: "realtime speed four uses fast pick mode", deadline: DeadlineRealtime, cpuUsed: 4, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.interAnalysisUsesRDModeDecision(); got != tt.want {
				t.Fatalf("RD mode decision = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLibvpxFastInterModeLoopTablesMirrorPickInter(t *testing.T) {
	wantModes := [...]vp8common.MBPredictionMode{
		vp8common.ZeroMV, vp8common.DCPred,
		vp8common.NearestMV, vp8common.NearMV,
		vp8common.ZeroMV, vp8common.NearestMV,
		vp8common.ZeroMV, vp8common.NearestMV,
		vp8common.NearMV, vp8common.NearMV,
		vp8common.VPred, vp8common.HPred, vp8common.TMPred,
		vp8common.NewMV, vp8common.NewMV, vp8common.NewMV,
		vp8common.SplitMV, vp8common.SplitMV, vp8common.SplitMV,
		vp8common.BPred,
	}
	wantRefs := [...]int{
		1, 0,
		1, 1,
		2, 2,
		3, 3,
		2, 3,
		0, 0, 0,
		1, 2, 3,
		1, 2, 3,
		0,
	}
	if libvpxFastInterModeOrder != wantModes {
		t.Fatalf("mode order = %v, want %v", libvpxFastInterModeOrder, wantModes)
	}
	if libvpxFastRefFrameOrder != wantRefs {
		t.Fatalf("ref order = %v, want %v", libvpxFastRefFrameOrder, wantRefs)
	}
}

func TestLibvpxInterModeThresholdMultipliersMirrorSpeedFeatures(t *testing.T) {
	best := libvpxInterModeThresholdMultipliers(DeadlineBestQuality, 8)
	if best[libvpxThrZero1] != 0 || best[libvpxThrNearest1] != 0 || best[libvpxThrNear1] != 0 || best[libvpxThrDC] != 0 {
		t.Fatalf("best-quality always-tested multipliers = zero:%d nearest:%d near:%d dc:%d, want all zero", best[libvpxThrZero1], best[libvpxThrNearest1], best[libvpxThrNear1], best[libvpxThrDC])
	}
	if best[libvpxThrVPred] != 1000 || best[libvpxThrHPred] != 1000 || best[libvpxThrBPred] != 2000 || best[libvpxThrNew1] != 1000 || best[libvpxThrSplit1] != 2500 {
		t.Fatalf("best-quality thresholds = V:%d H:%d B:%d NEW1:%d SPLIT1:%d, want 1000/1000/2000/1000/2500", best[libvpxThrVPred], best[libvpxThrHPred], best[libvpxThrBPred], best[libvpxThrNew1], best[libvpxThrSplit1])
	}

	good := libvpxInterModeThresholdMultipliers(DeadlineGoodQuality, 3)
	if good[libvpxThrZero2] != 2000 || good[libvpxThrBPred] != 7500 || good[libvpxThrNew2] != 2500 || good[libvpxThrSplit2] != 50000 {
		t.Fatalf("good speed 3 thresholds = ZERO2:%d B:%d NEW2:%d SPLIT2:%d, want 2000/7500/2500/50000", good[libvpxThrZero2], good[libvpxThrBPred], good[libvpxThrNew2], good[libvpxThrSplit2])
	}

	realtime := libvpxInterModeThresholdMultipliers(DeadlineRealtime, 8)
	if realtime[libvpxThrVPred] != libvpxInterModeThresholdDisabled || realtime[libvpxThrHPred] != libvpxInterModeThresholdDisabled || realtime[libvpxThrBPred] != libvpxInterModeThresholdDisabled {
		t.Fatalf("realtime speed 8 intra thresholds = V:%d H:%d B:%d, want disabled", realtime[libvpxThrVPred], realtime[libvpxThrHPred], realtime[libvpxThrBPred])
	}
	if realtime[libvpxThrZero2] != 2000 || realtime[libvpxThrNew2] != 4000 || realtime[libvpxThrSplit1] != libvpxInterModeThresholdDisabled {
		t.Fatalf("realtime speed 8 thresholds = ZERO2:%d NEW2:%d SPLIT1:%d, want 2000/4000/disabled", realtime[libvpxThrZero2], realtime[libvpxThrNew2], realtime[libvpxThrSplit1])
	}
}

func TestLibvpxInterModeRDThresholdsScaleLikeInitializeRDConsts(t *testing.T) {
	qValue := vp8common.DCQuant(40, 0)
	q := int(math.Pow(float64(qValue), 1.25))
	if q < 8 {
		q = 8
	}
	thresholds := libvpxInterModeRDThresholds(40, 0, DeadlineBestQuality, 0)
	if got, want := thresholds[libvpxThrNew1], 1000*q/100; got != want {
		t.Fatalf("high-rdmult NEW1 threshold = %d, want thresh_mult*q/100 = %d", got, want)
	}
	if got := thresholds[libvpxThrDC]; got != 0 {
		t.Fatalf("DC threshold = %d, want always-tested zero", got)
	}

	lowQ := vp8common.DCQuant(4, 0)
	lowQPow := int(math.Pow(float64(lowQ), 1.25))
	if lowQPow < 8 {
		lowQPow = 8
	}
	lowThresholds := libvpxInterModeRDThresholds(4, 0, DeadlineBestQuality, 0)
	if got, want := lowThresholds[libvpxThrNew1], 1000*lowQPow; got != want {
		t.Fatalf("low-rdmult NEW1 threshold = %d, want thresh_mult*q = %d", got, want)
	}
}

func TestLibvpxInterModeCheckFrequenciesMirrorSpeedFeatures(t *testing.T) {
	best := libvpxInterModeCheckFrequencies(DeadlineBestQuality, 8)
	if best[libvpxThrZero2] != 0 || best[libvpxThrNew2] != 0 || best[libvpxThrSplit2] != 0 {
		t.Fatalf("best-quality check frequencies = ZERO2:%d NEW2:%d SPLIT2:%d, want all zero", best[libvpxThrZero2], best[libvpxThrNew2], best[libvpxThrSplit2])
	}

	good := libvpxInterModeCheckFrequencies(DeadlineGoodQuality, 5)
	if good[libvpxThrVPred] != 2 || good[libvpxThrNew2] != 4 || good[libvpxThrSplit2] != 15 {
		t.Fatalf("good speed 5 frequencies = V:%d NEW2:%d SPLIT2:%d, want 2/4/15", good[libvpxThrVPred], good[libvpxThrNew2], good[libvpxThrSplit2])
	}

	realtime := libvpxInterModeCheckFrequencies(DeadlineRealtime, 10)
	if realtime[libvpxThrZero2] != 2 || realtime[libvpxThrNew1] != 0 || realtime[libvpxThrNew2] != 8 {
		t.Fatalf("realtime speed 10 frequencies = ZERO2:%d NEW1:%d NEW2:%d, want 2/0/8", realtime[libvpxThrZero2], realtime[libvpxThrNew1], realtime[libvpxThrNew2])
	}
}

func TestLibvpxInterModeThresholdMultipliersTemporalLayerTweaks(t *testing.T) {
	baseline := libvpxInterModeThresholdMultipliers(DeadlineRealtime, 6)
	unchanged := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, 6, libvpxInterModeThresholdContext{
		temporalLayers: 1,
		lastEnabled:    true,
		goldenEnabled:  true,
		closestRef:     vp8common.GoldenFrame,
	})
	if unchanged != baseline {
		t.Fatalf("one-layer temporal multipliers changed: %v want %v", unchanged, baseline)
	}

	tooFast := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, 7, libvpxInterModeThresholdContext{
		temporalLayers: 2,
		lastEnabled:    true,
		goldenEnabled:  true,
		closestRef:     vp8common.GoldenFrame,
	})
	if want := libvpxInterModeThresholdMultipliers(DeadlineRealtime, 7); tooFast != want {
		t.Fatalf("speed 7 temporal multipliers changed: %v want %v", tooFast, want)
	}

	missingGolden := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, 6, libvpxInterModeThresholdContext{
		temporalLayers: 2,
		lastEnabled:    true,
		closestRef:     vp8common.LastFrame,
	})
	if missingGolden != baseline {
		t.Fatalf("missing-GOLDEN temporal multipliers changed: %v want %v", missingGolden, baseline)
	}

	closestGolden := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, 6, libvpxInterModeThresholdContext{
		temporalLayers: 2,
		lastEnabled:    true,
		goldenEnabled:  true,
		closestRef:     vp8common.GoldenFrame,
	})
	if got, want := closestGolden[libvpxThrZero2], baseline[libvpxThrZero2]>>3; got != want {
		t.Fatalf("closest GOLDEN ZERO2 = %d, want %d", got, want)
	}
	if got, want := closestGolden[libvpxThrNearest2], baseline[libvpxThrNearest2]>>3; got != want {
		t.Fatalf("closest GOLDEN NEAREST2 = %d, want %d", got, want)
	}
	if got, want := closestGolden[libvpxThrNear2], baseline[libvpxThrNear2]>>3; got != want {
		t.Fatalf("closest GOLDEN NEAR2 = %d, want %d", got, want)
	}
	if closestGolden[libvpxThrZero3] != baseline[libvpxThrZero3] || closestGolden[libvpxThrNew2] != baseline[libvpxThrNew2] {
		t.Fatalf("temporal tweak touched unrelated thresholds ZERO3:%d/%d NEW2:%d/%d", closestGolden[libvpxThrZero3], baseline[libvpxThrZero3], closestGolden[libvpxThrNew2], baseline[libvpxThrNew2])
	}

	closestLast := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, 6, libvpxInterModeThresholdContext{
		temporalLayers: 2,
		lastEnabled:    true,
		goldenEnabled:  true,
		closestRef:     vp8common.LastFrame,
	})
	if got, want := closestLast[libvpxThrZero2], baseline[libvpxThrZero2]>>1; got != want {
		t.Fatalf("closest LAST ZERO2 = %d, want %d", got, want)
	}
}

func TestClosestInterAnalysisReferenceUsesLibvpxFrameNumbers(t *testing.T) {
	e := &VP8Encoder{}
	e.referenceFrameNumbers[vp8common.LastFrame] = 7
	e.referenceFrameNumbers[vp8common.GoldenFrame] = 9
	e.referenceFrameNumbers[vp8common.AltRefFrame] = 8
	refs := []interAnalysisReference{
		{Frame: vp8common.LastFrame},
		{Frame: vp8common.GoldenFrame},
		{Frame: vp8common.AltRefFrame},
	}
	if got := e.closestInterAnalysisReference(refs, len(refs)); got != vp8common.GoldenFrame {
		t.Fatalf("closest ref = %v, want GOLDEN", got)
	}

	e.referenceFrameNumbers[vp8common.LastFrame] = 9
	if got := e.closestInterAnalysisReference(refs, len(refs)); got != vp8common.LastFrame {
		t.Fatalf("closest tie ref = %v, want LAST", got)
	}

	refs = refs[1:]
	if got := e.closestInterAnalysisReference(refs, len(refs)); got != vp8common.GoldenFrame {
		t.Fatalf("closest tie without LAST = %v, want GOLDEN", got)
	}
}

func TestInterReferenceFrameNumbersMirrorLibvpxRefreshAndCopy(t *testing.T) {
	e := &VP8Encoder{}
	e.frameCount = 4
	e.referenceFrameNumbers[vp8common.LastFrame] = 1
	e.referenceFrameNumbers[vp8common.GoldenFrame] = 2
	e.referenceFrameNumbers[vp8common.AltRefFrame] = 3

	e.updateInterReferenceFrameNumbers(vp8enc.InterFrameStateConfig{
		RefreshLast:        true,
		RefreshGolden:      true,
		CopyBufferToAltRef: 2,
	})

	if got, want := e.referenceFrameNumbers[vp8common.AltRefFrame], uint64(2); got != want {
		t.Fatalf("ALT frame number after old-GF copy = %d, want %d", got, want)
	}
	if got, want := e.referenceFrameNumbers[vp8common.GoldenFrame], uint64(4); got != want {
		t.Fatalf("GOLDEN frame number after refresh = %d, want %d", got, want)
	}
	if got, want := e.referenceFrameNumbers[vp8common.LastFrame], uint64(4); got != want {
		t.Fatalf("LAST frame number after refresh = %d, want %d", got, want)
	}
}

func TestInterModeRDThresholdsUseTemporalClosestReference(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{
		Deadline: DeadlineRealtime,
		CpuUsed:  6,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringTwoLayers,
		},
	}}
	refs := []interAnalysisReference{
		{Frame: vp8common.LastFrame},
		{Frame: vp8common.GoldenFrame},
	}
	e.referenceFrameNumbers[vp8common.LastFrame] = 7
	e.referenceFrameNumbers[vp8common.GoldenFrame] = 9

	got := e.interModeRDThresholdsForReferences(40, refs, len(refs))
	want := libvpxInterModeRDThresholdsForContext(40, 0, DeadlineRealtime, 6, libvpxInterModeThresholdContext{
		temporalLayers: 2,
		lastEnabled:    true,
		goldenEnabled:  true,
		closestRef:     vp8common.GoldenFrame,
	})
	if got[libvpxThrZero2] != want[libvpxThrZero2] || got[libvpxThrNearest2] != want[libvpxThrNearest2] || got[libvpxThrNear2] != want[libvpxThrNear2] {
		t.Fatalf("closest GOLDEN temporal thresholds ZERO2/NEAREST2/NEAR2 = %d/%d/%d, want %d/%d/%d", got[libvpxThrZero2], got[libvpxThrNearest2], got[libvpxThrNear2], want[libvpxThrZero2], want[libvpxThrNearest2], want[libvpxThrNear2])
	}

	e.referenceFrameNumbers[vp8common.LastFrame] = 10
	got = e.interModeRDThresholdsForReferences(40, refs, len(refs))
	want = libvpxInterModeRDThresholdsForContext(40, 0, DeadlineRealtime, 6, libvpxInterModeThresholdContext{
		temporalLayers: 2,
		lastEnabled:    true,
		goldenEnabled:  true,
		closestRef:     vp8common.LastFrame,
	})
	if got[libvpxThrZero2] != want[libvpxThrZero2] {
		t.Fatalf("closest LAST temporal ZERO2 threshold = %d, want %d", got[libvpxThrZero2], want[libvpxThrZero2])
	}
}

func TestInterRDThresholdStateMutatesLikeLibvpxRDLoop(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineBestQuality}}
	e.resetInterRDThresholdMultipliers()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()

	baseline := libvpxInterModeRDThresholds(40, 0, DeadlineBestQuality, 0)
	if got := e.interModeRDThresholds(40); got != baseline {
		t.Fatalf("initial thresholds = %v, want baseline %v", got, baseline)
	}

	e.lowerInterRDThresholdForImprovement(libvpxThrNew1)
	afterImprovement := e.interModeRDThresholds(40)
	if got, want := afterImprovement[libvpxThrNew1], (baseline[libvpxThrNew1]>>7)*126; got != want {
		t.Fatalf("improved NEW1 threshold = %d, want %d", got, want)
	}

	e.raiseInterRDThreshold(libvpxThrNew2)
	afterRaise := e.interModeRDThresholds(40)
	if got, want := afterRaise[libvpxThrNew2], (baseline[libvpxThrNew2]>>7)*132; got != want {
		t.Fatalf("raised NEW2 threshold = %d, want %d", got, want)
	}

	e.lowerBestInterRDThreshold(libvpxThrNew1)
	afterBest := e.interModeRDThresholds(40)
	if got, want := afterBest[libvpxThrNew1], (baseline[libvpxThrNew1]>>7)*95; got != want {
		t.Fatalf("best NEW1 threshold = %d, want %d", got, want)
	}
}

func TestInterFastBestThresholdUsesPickInterDecay(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8}}
	e.resetInterRDThresholdMultipliers()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()

	baseline := libvpxInterModeRDThresholds(40, 0, DeadlineRealtime, 8)
	e.lowerBestInterFastThreshold(libvpxThrNew1)
	afterBest := e.interModeRDThresholds(40)
	if got, want := afterBest[libvpxThrNew1], (baseline[libvpxThrNew1]>>7)*112; got != want {
		t.Fatalf("fast best NEW1 threshold = %d, want %d", got, want)
	}
}

func TestInterRDModeHitCountGateRaisesThreshold(t *testing.T) {
	e := &VP8Encoder{}
	e.resetInterRDThresholdMultipliers()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.interModeCheckFreq[libvpxThrNew2] = 4
	e.interModeTestHitCounts[libvpxThrNew2] = 1
	e.interMBsTestedSoFar = 4

	if e.interRDModeTestAllowed(libvpxThrNew2) {
		t.Fatalf("hit-count gate allowed mode at mbs_tested_so_far <= freq*hits")
	}
	if got := e.interRDThreshMult[libvpxThrNew2]; got != 132 {
		t.Fatalf("NEW2 threshold mult after hit gate = %d, want 132", got)
	}

	e.interMBsTestedSoFar = 5
	if !e.interRDModeTestAllowed(libvpxThrNew2) {
		t.Fatalf("hit-count gate blocked mode after frequency window")
	}
}

func TestLibvpxSplitMVSubsearchThresholdUsesNewMVReferenceThresholds(t *testing.T) {
	var thresholds [libvpxInterModeCount]int
	thresholds[libvpxThrNew1] = 11
	thresholds[libvpxThrNew2] = 22
	thresholds[libvpxThrNew3] = 33

	if got := libvpxSplitMVSubsearchThreshold(thresholds, 1); got != 11 {
		t.Fatalf("slot 1 SplitMV threshold = %d, want THR_NEW1", got)
	}
	if got := libvpxSplitMVSubsearchThreshold(thresholds, 2); got != 22 {
		t.Fatalf("slot 2 SplitMV threshold = %d, want THR_NEW2", got)
	}
	if got := libvpxSplitMVSubsearchThreshold(thresholds, 3); got != 33 {
		t.Fatalf("slot 3 SplitMV threshold = %d, want THR_NEW3", got)
	}
}

func TestSplitMVSubsearchThresholdUsesReferenceSearchSlot(t *testing.T) {
	e := &VP8Encoder{}
	e.resetInterRDThresholdMultipliers()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()

	refs := []interAnalysisReference{{Frame: vp8common.GoldenFrame, Img: &vp8common.Image{}}}
	const qIndex = 40
	e.raiseInterRDThreshold(libvpxThrNew2)
	e.raiseInterRDThreshold(libvpxThrNew2)
	thresholds := e.interModeRDThresholdsForReferences(qIndex, refs, 1)
	if thresholds[libvpxThrNew1] == thresholds[libvpxThrNew2] {
		t.Fatalf("test setup NEW1 threshold equals NEW2 (%d)", thresholds[libvpxThrNew1])
	}
	if got := e.splitMVSubsearchThresholdForSlot(qIndex, refs, 1, 1); got != thresholds[libvpxThrNew1] {
		t.Fatalf("golden-only slot-1 SplitMV threshold = %d, want current THR_NEW1 %d", got, thresholds[libvpxThrNew1])
	}

	e.raiseInterRDThreshold(libvpxThrNew1)
	fresh := e.interModeRDThresholdsForReferences(qIndex, refs, 1)
	got := e.splitMVSubsearchThresholdForSlot(qIndex, refs, 1, 1)
	if got != fresh[libvpxThrNew1] {
		t.Fatalf("fresh slot-1 SplitMV threshold = %d, want raised THR_NEW1 %d", got, fresh[libvpxThrNew1])
	}
	if got == thresholds[libvpxThrNew1] {
		t.Fatalf("slot-1 SplitMV threshold reused stale THR_NEW1 %d after raise", got)
	}
}

func TestLibvpxSplitMVStepParamFromSeedDistance(t *testing.T) {
	tests := []struct {
		sr   int
		want int
	}{
		{sr: 0, want: 7},
		{sr: 1, want: 7},
		{sr: 2, want: 6},
		{sr: 3, want: 6},
		{sr: 4, want: 5},
		{sr: 127, want: 1},
		{sr: 128, want: 0},
		{sr: 512, want: 0},
	}
	for _, tt := range tests {
		if got := libvpxSplitMVStepParamFromSeedDistance(tt.sr); got != tt.want {
			t.Fatalf("step_param(%d) = %d, want %d", tt.sr, got, tt.want)
		}
	}
}

func TestLibvpxFastInterReferenceAtUsesEnabledReferenceSlots(t *testing.T) {
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &vp8common.Image{}},
		{Frame: vp8common.GoldenFrame, Img: &vp8common.Image{}},
	}
	ref, refIndex, ok := libvpxFastInterReferenceAt(refs[:], 2, 2)
	if !ok || refIndex != 1 || ref.Frame != vp8common.GoldenFrame {
		t.Fatalf("slot 2 = %+v index=%d ok=%t, want GOLDEN index 1", ref, refIndex, ok)
	}
	if _, _, ok := libvpxFastInterReferenceAt(refs[:], 2, 3); ok {
		t.Fatalf("slot 3 ok=true, want disabled ALTREF slot to be skipped")
	}
}

func TestLibvpxInterReferenceSearchOrderCompactsEnabledReferences(t *testing.T) {
	refs := [...]interAnalysisReference{
		{Frame: vp8common.GoldenFrame, Img: &vp8common.Image{}},
		{Frame: vp8common.AltRefFrame, Img: &vp8common.Image{}},
	}
	order := libvpxInterReferenceSearchOrder(refs[:], len(refs))
	if order != [4]int{-1, 0, 1, -1} {
		t.Fatalf("reference search order = %v, want compacted GOLDEN/ALT in slots 1/2", order)
	}

	ref, refIndex, ok := libvpxInterReferenceSearchAt(refs[:], order, 1)
	if !ok || refIndex != 0 || ref.Frame != vp8common.GoldenFrame {
		t.Fatalf("slot 1 = %+v index=%d ok=%t, want compacted GOLDEN", ref, refIndex, ok)
	}
	ref, refIndex, ok = libvpxInterReferenceSearchAt(refs[:], order, 2)
	if !ok || refIndex != 1 || ref.Frame != vp8common.AltRefFrame {
		t.Fatalf("slot 2 = %+v index=%d ok=%t, want compacted ALTREF", ref, refIndex, ok)
	}
	if _, _, ok := libvpxInterReferenceSearchAt(refs[:], order, 3); ok {
		t.Fatalf("slot 3 ok=true, want no third enabled reference")
	}
}

func TestInterAnalysisNoSkipBlock4x4SearchMirrorsLibvpxSpeedFeature(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality always keeps 4x4 search", deadline: DeadlineBestQuality, cpuUsed: 8, want: true},
		{name: "good speed zero keeps 4x4 search", deadline: DeadlineGoodQuality, cpuUsed: 0, want: true},
		{name: "good positive speed can skip 4x4 search", deadline: DeadlineGoodQuality, cpuUsed: 1, want: false},
		{name: "realtime negative speed keeps 4x4 search", deadline: DeadlineRealtime, cpuUsed: -1, want: true},
		{name: "realtime positive speed can skip 4x4 search", deadline: DeadlineRealtime, cpuUsed: 1, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.interAnalysisNoSkipBlock4x4Search(); got != tt.want {
				t.Fatalf("no-skip 4x4 = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestInterFrameNstepSearchSitesMirrorLibvpx3StepTable(t *testing.T) {
	sites := interFrameNstepSearchSites()
	if len(sites) != 65 {
		t.Fatalf("nstep search sites = %d, want 65", len(sites))
	}
	wantFirst := [...]vp8enc.MotionVector{
		{},
		{Row: -128},
		{Row: 128},
		{Col: -128},
		{Col: 128},
		{Row: -128, Col: -128},
		{Row: -128, Col: 128},
		{Row: 128, Col: -128},
		{Row: 128, Col: 128},
	}
	for i, want := range wantFirst {
		if sites[i] != want {
			t.Fatalf("site[%d] = %+v, want %+v", i, sites[i], want)
		}
	}
	if sites[57] != (vp8enc.MotionVector{Row: -1}) || sites[64] != (vp8enc.MotionVector{Row: 1, Col: 1}) {
		t.Fatalf("final step sites = %+v/%+v, want -1 row and +1,+1", sites[57], sites[64])
	}
}

func TestInterFrameDiamondSearchSitesMirrorLibvpxDSMotionTable(t *testing.T) {
	sites := interFrameDiamondSearchSites()
	if len(sites) != 33 {
		t.Fatalf("diamond search sites = %d, want 33", len(sites))
	}
	wantFirst := [...]vp8enc.MotionVector{
		{},
		{Row: -128},
		{Row: 128},
		{Col: -128},
		{Col: 128},
	}
	for i, want := range wantFirst {
		if sites[i] != want {
			t.Fatalf("site[%d] = %+v, want %+v", i, sites[i], want)
		}
	}
	if sites[29] != (vp8enc.MotionVector{Row: -1}) || sites[32] != (vp8enc.MotionVector{Col: 1}) {
		t.Fatalf("final step sites = %+v/%+v, want -1 row and +1 col", sites[29], sites[32])
	}
}

func TestSelectInterFrameReferenceMotionVectorChoosesLowestCostReference(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 16, 16, 220, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	alt := testVP8Frame(t, 16, 16, 80, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			v := byte(32 + ((row*17 + col*11) & 127))
			src.Y[row*src.YStride+col] = v
			golden.Img.Y[row*golden.Img.YStride+col] = v
			last.Img.Y[row*last.Img.YStride+col] = byte(200 - ((row*7 + col*19) & 63))
			alt.Img.Y[row*alt.Img.YStride+col] = byte(96 + ((row*5 + col*3) & 63))
		}
	}
	last.ExtendBorders()
	golden.ExtendBorders()
	alt.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)

	ref, mv := selectInterFrameReferenceMotionVector(source, refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.GoldenFrame || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("selection = %v %+v, want golden zero MV", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorUsesLibvpxHexCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(17 + ((row*19 + col*11) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+2)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 16}) {
		t.Fatalf("selection = %v %+v, want last row +16 from libvpx hex ring", ref.Frame, mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorRealtimeHexWalksNextCheckpoints(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((19 + row*73 + col*151 + row*col*37) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 127, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			v := src.Y[(row+16)*src.YStride+col+16]
			last.Img.Y[(row+18)*last.Img.YStride+col+16] = v ^ 1
		}
	}
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{fullPixelSearch: interAnalysisFullPixelSearchHex, fractionalSearch: interAnalysisFractionalSearchStep}
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("hex full-pixel MV = %+v, want row +32 from libvpx next_chkpts walk", mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorNstepUsesLibvpxSearchSites(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((23 + row*71 + col*139 + row*col*41) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  0,
		fullPixelFurtherSteps: 7,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("nstep full-pixel MV = %+v, want row +32 from libvpx search-site contraction", mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorDiamondUsesLibvpxSearchSites(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((23 + row*71 + col*139 + row*col*41) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchDiamond,
		fullPixelSearchParam:  0,
		fullPixelFurtherSteps: 7,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("diamond full-pixel MV = %+v, want row +32 from libvpx four-site contraction", mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorDiamondKeepsFourSitePath(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((31 + row*67 + col*149 + row*col*43) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+20)*last.Img.YStride+col+20] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	nstepCfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  0,
		fullPixelFurtherSteps: 7,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	diamondCfg := nstepCfg
	diamondCfg.fullPixelSearch = interAnalysisFullPixelSearchDiamond

	source := sourceImageFromPublic(src)
	nstepMV, _ := selectInterFrameFullPixelMotionVectorWithSearch(source, &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, nstepCfg)
	diamondMV, _ := selectInterFrameFullPixelMotionVectorWithSearch(source, &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, diamondCfg)

	if nstepMV != (vp8enc.MotionVector{Row: 32, Col: 32}) {
		t.Fatalf("nstep full-pixel MV = %+v, want diagonal +32,+32", nstepMV)
	}
	if diamondMV == nstepMV {
		t.Fatalf("diamond full-pixel MV = %+v, want four-site path distinct from NSTEP diagonal", diamondMV)
	}
}

func TestSelectInterFrameFullPixelMotionVectorRDRefinesNstepResult(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 0, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = 200
		}
	}

	last := testVP8Frame(t, 64, 64, 0, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+18)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}
	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  interFrameMaxMVSearchSteps - 1,
		fullPixelFurtherSteps: 0,
	}
	unrefined, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	cfg.fullPixelFinalRefine = true
	refined, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)
	if refined != (vp8enc.MotionVector{Row: 16}) {
		t.Fatalf("refined nstep MV = %+v, want libvpx final 1-away refine to row +16", refined)
	}
	if refined == unrefined {
		t.Fatalf("refined nstep MV = unrefined %+v, want final refine to move the candidate", refined)
	}
}

func TestSelectInterFrameFullPixelMotionVectorUsesImprovedStartAndBestRefMVCost(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((41 + row*19 + col*31 + row*col*7) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	bestRefMV := vp8enc.MotionVector{}
	start := interFrameSearchStart{mv: vp8enc.MotionVector{Row: 32}, sr: 3, ok: true}
	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  interFrameMaxMVSearchSteps - 1,
		fullPixelFurtherSteps: 0,
		fullPixelSpeed:        8,
		fullPixelSpeedAdjust:  3,
		improvedMVPrediction:  true,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	mv, cost := selectInterFrameFullPixelMotionVectorWithSearchStart(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, bestRefMV, testInterSearchQIndex, cfg, start)

	if mv != start.mv {
		t.Fatalf("full-pixel MV = %+v, want improved search start %+v", mv, start.mv)
	}
	variance, _ := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &last.Img, 1, 1, mv)
	wantCost := variance + interMotionSearchErrorVectorCost(mv, bestRefMV, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	if cost != wantCost {
		t.Fatalf("full-pixel cost = %d, want variance plus best_ref_mv anchored error cost %d", cost, wantCost)
	}
	if legacyCost := interMotionSearchCost(sourceImageFromPublic(src), &last.Img, 1, 1, mv, bestRefMV, testInterSearchQIndex); cost == legacyCost {
		t.Fatalf("full-pixel cost = legacy SAD plus vector cost %d, want variance plus error vector cost", legacyCost)
	}
}

func TestImprovedInterFrameSearchStartUsesLibvpxSADOrderAndStepRange(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 8, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((73 + row*43 + col*17 + row*col*11) & 255)
		}
	}

	analysis := testVP8Frame(t, 64, 64, 211, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			srcPixel := src.Y[(row+16)*src.YStride+col+16]
			analysis.Img.Y[(row+16)*analysis.Img.YStride+col] = srcPixel
			analysis.Img.Y[row*analysis.Img.YStride+col+16] = srcPixel ^ 0xff
		}
	}
	e := &VP8Encoder{analysis: analysis}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 8}}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 40}}
	aboveLeft := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: -24}}
	search := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchHex,
		fullPixelSearchParam:  2,
		fullPixelFurtherSteps: 5,
		fullPixelSpeed:        5,
		fullPixelSpeedAdjust:  2,
		improvedMVPrediction:  true,
	}

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, &above, &left, &aboveLeft, search)
	if !start.ok || start.mv != left.MV || start.sr != 3 {
		t.Fatalf("improved search start = %+v, want left MV %+v with sr 3", start, left.MV)
	}
	if start.nearSADIndex != 1 {
		t.Fatalf("near_sadidx = %d, want current-frame left slot 1", start.nearSADIndex)
	}
	adjusted := search.adjustedForImprovedMVStart(start)
	if adjusted.fullPixelSearchParam != 5 || adjusted.fullPixelFurtherSteps != 2 {
		t.Fatalf("adjusted search = step %d further %d, want step 5 further 2", adjusted.fullPixelSearchParam, adjusted.fullPixelFurtherSteps)
	}

	search.improvedMVPrediction = false
	if disabled := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, &above, &left, &aboveLeft, search); disabled.ok {
		t.Fatalf("disabled improved search start = %+v, want not set", disabled)
	}
}

func TestImprovedInterFrameSearchStartReadsPreviousInterFrameModes(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 19, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[(row+16)*src.YStride+col+16] = byte((31 + row*29 + col*13 + row*col*5) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 151, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+16)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}
	e := &VP8Encoder{
		lastRef:                  last,
		lastFrameInterModes:      make([]vp8enc.InterFrameMacroblockMode, 16),
		lastFrameInterModesValid: true,
	}
	e.lastFrameInterModes[1*4+1] = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 56, Col: -8}}
	search := interAnalysisSearchConfig{improvedMVPrediction: true}

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, nil, nil, nil, search)
	if !start.ok || start.mv != e.lastFrameInterModes[1*4+1].MV || start.sr != 3 {
		t.Fatalf("previous-frame search start = %+v, want %+v with sr 3", start, e.lastFrameInterModes[1*4+1].MV)
	}
	if start.nearSADIndex != 3 {
		t.Fatalf("near_sadidx = %d, want previous-frame current-MB slot 3", start.nearSADIndex)
	}
}

func TestImprovedInterFrameSearchStartBiasesCurrentSlots(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 80, 90, 170)
	analysis := testVP8Frame(t, 32, 32, 80, 90, 170)
	e := &VP8Encoder{analysis: analysis, sourceAltRefActive: true}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	left := above
	aboveLeft := above

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, 2, 2, &above, &left, &aboveLeft, interAnalysisSearchConfig{improvedMVPrediction: true})
	if !start.ok || start.sr != 0 || start.mv != (vp8enc.MotionVector{Col: -16}) {
		t.Fatalf("sign-biased current-frame start = %+v, want median col -16 with sr 0", start)
	}
}

func TestImprovedInterFrameSearchStartBiasesPreviousFrameSlots(t *testing.T) {
	const mbRows, mbCols = 3, 3
	src := testImage(mbCols*16, mbRows*16)
	fillImage(src, 72, 90, 170)
	last := testVP8Frame(t, mbCols*16, mbRows*16, 72, 90, 170)
	modes := make([]vp8enc.InterFrameMacroblockMode, mbRows*mbCols)
	for _, index := range []int{
		1*mbCols + 1,
		0*mbCols + 1,
		1*mbCols + 0,
		1*mbCols + 2,
		2*mbCols + 1,
	} {
		modes[index] = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	}
	e := &VP8Encoder{
		lastRef:                  last,
		lastFrameInterModes:      modes,
		lastFrameInterModeBias:   make([]bool, len(modes)),
		lastFrameInterModesValid: true,
		sourceAltRefActive:       true,
	}

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, mbRows, mbCols, nil, nil, nil, interAnalysisSearchConfig{improvedMVPrediction: true})
	if !start.ok || start.sr != 0 || start.mv != (vp8enc.MotionVector{Col: -16}) {
		t.Fatalf("sign-biased previous-frame start = %+v, want median col -16 with sr 0", start)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsFullPixelCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(21 + ((row*23 + col*7) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+3)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 24}) {
		t.Fatalf("selection = %v %+v, want last row +24 after exhaustive search", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsExhaustiveCornerCandidate(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(31 + ((row*29 + col*5) & 127))
		}
	}

	last := testVP8Frame(t, 64, 64, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+4)*last.Img.YStride+col+4] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 32, Col: 32}) {
		t.Fatalf("selection = %v %+v, want last +32,+32 exhaustive candidate", ref.Frame, mv)
	}
}

func TestSelectInterFrameSplitMotionModeFindsQuadrantMotion(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < 32; row++ {
		for col := 0; col < 32; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*37 + col*13) & 255)
		}
	}
	copyShifted8x8FromReference(src, &ref.Img, 0, 0, 0, 1)
	copyShifted8x8FromReference(src, &ref.Img, 0, 8, 1, 0)
	copyShifted8x8FromReference(src, &ref.Img, 8, 0, 0, 2)
	copyShifted8x8FromReference(src, &ref.Img, 8, 8, 2, 0)
	ref.ExtendBorders()

	mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 2)

	if !ok {
		t.Fatalf("split mode selection returned false")
	}
	if mode.Mode != vp8common.SplitMV || mode.RefFrame != vp8common.LastFrame || mode.Partition != 2 {
		t.Fatalf("mode = %+v, want LAST/SPLITMV partition 2", mode)
	}
	want := [4]vp8enc.MotionVector{
		{Col: 8},
		{Row: 8},
		{Col: 16},
		{Row: 16},
	}
	for subset, mv := range want {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		if mode.BlockMV[block] != mv {
			t.Fatalf("subset %d block %d MV = %+v, want %+v", subset, block, mode.BlockMV[block], mv)
		}
	}
	if mode.MV != mode.BlockMV[15] {
		t.Fatalf("mode MV = %+v, want last block %+v", mode.MV, mode.BlockMV[15])
	}
}

func TestSelectInterFrameSplitSubsetMotionModeTrialsReusableLabels(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < 32; row++ {
		copy(src.Y[row*src.YStride:row*src.YStride+32], ref.Img.Y[row*ref.Img.YStride:row*ref.Img.YStride+32])
	}
	ref.ExtendBorders()

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 2,
	}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	width, height := splitMotionPartitionBlockSize(int(mode.Partition))

	mv, bMode := selectInterFrameSplitSubsetMotionMode(sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, 0, width, height, vp8enc.MotionVector{}, testInterSearchQIndex, &left, &above)

	if mv != (vp8enc.MotionVector{}) || bMode != vp8common.Above4x4 {
		t.Fatalf("subset candidate = %+v/%v, want ABOVE4X4 zero-MV reuse", mv, bMode)
	}
}

func TestSelectInterFrameSplitSubsetMotionModeRefinesNew4x4Subpixel(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((19 + row*17 + col*13 + row*col*3) & 0xff)
		}
	}
	ref.ExtendBorders()
	targetMV := vp8enc.MotionVector{Row: 18, Col: 18}
	refBaseY := int(targetMV.Row >> 3)
	refBaseX := int(targetMV.Col >> 3)
	refStart := ref.Img.YFull[ref.Img.YOrigin+(refBaseY-2)*ref.Img.YStride+refBaseX-2:]
	dsp.SixTapPredict4x4(refStart, ref.Img.YStride, int(targetMV.Col)&7, int(targetMV.Row)&7, src.Y, src.YStride)

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 3,
	}
	width, height := splitMotionPartitionBlockSize(int(mode.Partition))

	mv, bMode := selectInterFrameSplitSubsetMotionMode(sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, 0, width, height, vp8enc.MotionVector{}, testInterSearchQIndex, nil, nil)

	if bMode != vp8common.New4x4 || (int(mv.Row)&7 == 0 && int(mv.Col)&7 == 0) {
		t.Fatalf("subset candidate = %+v/%v, want NEW4X4 subpixel MV", mv, bMode)
	}
	if sad := splitBlockSAD(sourceImageFromPublic(src), &ref.Img, 0, 0, 0, 4, 4, targetMV); sad != 0 {
		t.Fatalf("subpixel split SAD = %d, want exact predictor match", sad)
	}
}

func TestSplitBlockSADUsesSubpixelPredictorForAllShapes(t *testing.T) {
	cases := []struct {
		name    string
		block   int
		width   int
		height  int
		predict func(src []byte, srcStride int, xOffset int, yOffset int, dst []byte, dstStride int)
	}{
		{name: "16x8", block: 0, width: 16, height: 8, predict: dsp.SixTapPredict16x8},
		{name: "8x16", block: 0, width: 8, height: 16, predict: dsp.SixTapPredict8x16},
		{name: "8x8", block: 0, width: 8, height: 8, predict: dsp.SixTapPredict8x8},
		{name: "4x4", block: 5, width: 4, height: 4, predict: dsp.SixTapPredict4x4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := testImage(32, 32)
			fillImage(src, 0, 90, 170)
			ref := testVP8Frame(t, 32, 32, 0, 90, 170)
			for row := 0; row < ref.Img.CodedHeight; row++ {
				for col := 0; col < ref.Img.CodedWidth; col++ {
					ref.Img.Y[row*ref.Img.YStride+col] = byte((17 + row*19 + col*23 + row*col*11) & 0xff)
				}
			}
			ref.ExtendBorders()

			baseY := (tc.block >> 2) * 4
			baseX := (tc.block & 3) * 4
			refStart := ref.Img.YOrigin + (baseY-2)*ref.Img.YStride + baseX - 2
			tc.predict(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[baseY*src.YStride+baseX:], src.YStride)

			if sad := splitBlockSAD(sourceImageFromPublic(src), &ref.Img, 0, 0, tc.block, tc.width, tc.height, vp8enc.MotionVector{Row: 2, Col: 2}); sad != 0 {
				t.Fatalf("splitBlockSAD = %d, want exact subpixel predictor match", sad)
			}
		})
	}
}

func TestRefineInterFrameSplitBlockSubpixelMotionVectorUsesBilinearVariance(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*11 + col*7 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin
	dsp.BilinearPredict4x4(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y, src.YStride)

	mv, cost, ok := refineInterFrameSplitBlockSubpixelMotionVector(sourceImageFromPublic(src), &ref.Img, 0, 0, 0, 4, 4, vp8enc.MotionVector{}, vp8enc.MotionVector{}, testInterSearchQIndex, defaultInterAnalysisSearchConfig(), &vp8tables.DefaultMVContext)

	if !ok {
		t.Fatalf("refineInterFrameSplitBlockSubpixelMotionVector returned ok=false")
	}
	if mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("mv = %+v, want +2,+2 quarter-pel candidate", mv)
	}
	if want := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost != want {
		t.Fatalf("cost = %d, want zero distortion plus mv cost %d", cost, want)
	}
}

func TestSelectInterFrameSplitBlockFullPixelMotionVectorUsesSearchCenter(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*53 + col*97 + row*col*29 + col*col*7) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 4, 4, 4, 0, 12)
	ref.ExtendBorders()

	bestRefMV := vp8enc.MotionVector{}
	reusedCenter := vp8enc.MotionVector{Col: 64}
	mv, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 4, 4, reusedCenter, bestRefMV, 0, 5, false)
	noReuseMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 4, 4, bestRefMV, bestRefMV, 0, 5, false)
	bestQualityMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenter(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 4, 4, bestRefMV, bestRefMV, 0)

	if mv != (vp8enc.MotionVector{Col: 96}) {
		t.Fatalf("search-centered split MV = %+v, want col +96", mv)
	}
	if noReuseMV == mv {
		t.Fatalf("zero-centered search unexpectedly reached %+v; test no longer proves predictor reuse", mv)
	}
	if bestQualityMV != (vp8enc.MotionVector{Col: 96}) {
		t.Fatalf("best-quality full-search fallback MV = %+v, want col +96", bestQualityMV)
	}
}

func TestSelectInterFrameSplitBlockFullPixelMotionVectorUsesStepParam(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*67 + col*43 + row*col*19 + col*col*5) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 4, 4, 0, 2)
	ref.ExtendBorders()

	source := sourceImageFromPublic(src)
	stepTwoMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(source, &ref.Img, 0, 0, 0, 4, 4, vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, 6, false)
	stepOneMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(source, &ref.Img, 0, 0, 0, 4, 4, vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, 7, false)

	if stepTwoMV != (vp8enc.MotionVector{Col: 16}) {
		t.Fatalf("step_param 6 MV = %+v, want col +16", stepTwoMV)
	}
	if stepOneMV == stepTwoMV {
		t.Fatalf("step_param 7 reached %+v; want smaller diamond window than step_param 6", stepOneMV)
	}
}

func TestSelectInterFrameSplitMotionModeWithSearchUses8x8SeedFor8x16(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*71 + col*37 + row*col*17 + col*col*11) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 8, 16, 0, 9)
	copyShiftedBlockFromReference(src, &ref.Img, 0, 8, 8, 16, 0, 0)
	ref.ExtendBorders()
	seeds := splitMotionSearchSeeds{
		valid: true,
		mv: [4]vp8enc.MotionVector{
			{Col: 64},
			{},
			{Col: 64},
			{},
		},
	}

	mode, ok := selectInterFrameSplitMotionModeWithSearch(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, 0, 1, nil, nil, defaultInterAnalysisSearchConfig(), 1, &seeds, &vp8tables.DefaultMVContext)

	if !ok || mode.Partition != 1 {
		t.Fatalf("mode = %+v ok=%t, want 8x16 SplitMV", mode, ok)
	}
	if mode.BlockMV[0] != (vp8enc.MotionVector{Col: 72}) {
		t.Fatalf("seeded 8x16 left MV = %+v, want col +72", mode.BlockMV[0])
	}
	if mode.BlockMV[2] != (vp8enc.MotionVector{}) {
		t.Fatalf("8x16 right MV = %+v, want zero", mode.BlockMV[2])
	}
}

func TestSplitMotionSubsetSearchCenterMatchesLibvpxSeedReuse(t *testing.T) {
	bestRefMV := vp8enc.MotionVector{Row: 8, Col: -16}
	mode := vp8enc.InterFrameMacroblockMode{Partition: 3}
	mode.BlockMV[0] = vp8enc.MotionVector{Col: 64}
	mode.BlockMV[4] = vp8enc.MotionVector{Row: 32}
	seeds := splitMotionSearchSeeds{
		valid: true,
		mv: [4]vp8enc.MotionVector{
			{Col: 16},
			{Col: 24},
			{Row: 32},
			{Row: 40},
		},
	}

	if got := splitMotionSubsetSearchCenter(1, 0, &mode, bestRefMV, 1, &seeds); got != seeds.mv[0] {
		t.Fatalf("8x16 subset 0 search center = %+v, want 8x8 seed %+v", got, seeds.mv[0])
	}
	if got := splitMotionSubsetSearchCenter(1, 1, &mode, bestRefMV, 1, &seeds); got != seeds.mv[1] {
		t.Fatalf("8x16 subset 1 search center = %+v, want 8x8 seed %+v", got, seeds.mv[1])
	}
	if got := splitMotionSubsetSearchCenter(0, 0, &mode, bestRefMV, 1, &seeds); got != seeds.mv[0] {
		t.Fatalf("16x8 subset 0 search center = %+v, want 8x8 seed %+v", got, seeds.mv[0])
	}
	if got := splitMotionSubsetSearchCenter(0, 1, &mode, bestRefMV, 1, &seeds); got != seeds.mv[2] {
		t.Fatalf("16x8 subset 1 search center = %+v, want 8x8 seed %+v", got, seeds.mv[2])
	}
	if got := splitMotionSubsetSearchCenter(3, 0, &mode, bestRefMV, 1, &seeds); got != seeds.mv[0] {
		t.Fatalf("4x4 subset 0 search center = %+v, want 8x8 seed %+v", got, seeds.mv[0])
	}
	if got := splitMotionSubsetSearchCenter(3, 1, &mode, bestRefMV, 1, &seeds); got != mode.BlockMV[0] {
		t.Fatalf("subset 1 search center = %+v, want left block %+v", got, mode.BlockMV[0])
	}
	if got := splitMotionSubsetSearchCenter(3, 8, &mode, bestRefMV, 1, &seeds); got != mode.BlockMV[4] {
		t.Fatalf("subset 8 search center = %+v, want above block %+v", got, mode.BlockMV[4])
	}
	if got := splitMotionSubsetSearchCenter(1, 1, &mode, bestRefMV, 0, &seeds); got != bestRefMV {
		t.Fatalf("best-quality search center = %+v, want bestRefMV %+v", got, bestRefMV)
	}
}

func TestSplitMotionSearchSeedsFrom8x8UsesLibvpxBlocks(t *testing.T) {
	mode := vp8enc.InterFrameMacroblockMode{
		Mode:      vp8common.SplitMV,
		Partition: 2,
	}
	mode.BlockMV[0] = vp8enc.MotionVector{Col: 16}
	mode.BlockMV[2] = vp8enc.MotionVector{Col: 24}
	mode.BlockMV[8] = vp8enc.MotionVector{Row: 32}
	mode.BlockMV[10] = vp8enc.MotionVector{Row: 40}

	seeds := splitMotionSearchSeedsFrom8x8(&mode)

	if !seeds.valid {
		t.Fatalf("8x8 seeds are not valid")
	}
	want := [4]vp8enc.MotionVector{mode.BlockMV[0], mode.BlockMV[2], mode.BlockMV[8], mode.BlockMV[10]}
	if seeds.mv != want {
		t.Fatalf("seeds = %+v, want %+v", seeds.mv, want)
	}
	if seeds.step8x16 != [2]int{5, 5} || seeds.step16x8 != [2]int{7, 7} {
		t.Fatalf("seed steps 8x16=%v 16x8=%v, want [5 5] and [7 7]", seeds.step8x16, seeds.step16x8)
	}
}

func TestSelectInterFrameSplitMotionModeFindsAllPartitionShapes(t *testing.T) {
	t.Run("horizontal", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 1)
		copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 2, 0)

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 0)

		if !ok || mode.Partition != 0 {
			t.Fatalf("mode = %+v ok=%t, want partition 0", mode, ok)
		}
		if mode.BlockMV[0] != (vp8enc.MotionVector{Col: 8}) || mode.BlockMV[8] != (vp8enc.MotionVector{Row: 16}) {
			t.Fatalf("partition 0 MVs = %+v/%+v, want col +8 and row +16", mode.BlockMV[0], mode.BlockMV[8])
		}
	})
	t.Run("vertical", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 8, 16, 1, 0)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 8, 8, 16, 0, 2)

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 1)

		if !ok || mode.Partition != 1 {
			t.Fatalf("mode = %+v ok=%t, want partition 1", mode, ok)
		}
		if mode.BlockMV[0] != (vp8enc.MotionVector{Row: 8}) || mode.BlockMV[2] != (vp8enc.MotionVector{Col: 16}) {
			t.Fatalf("partition 1 MVs = %+v/%+v, want row +8 and col +16", mode.BlockMV[0], mode.BlockMV[2])
		}
	})
	t.Run("four-by-four", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		var want [16]vp8enc.MotionVector
		for block := 0; block < 16; block++ {
			y := (block >> 2) * 4
			x := (block & 3) * 4
			dy := block >> 2
			dx := block & 3
			copyShiftedBlockFromReference(src, &ref.Img, y, x, 4, 4, dy, dx)
			want[block] = vp8enc.MotionVector{Row: int16(dy * 8), Col: int16(dx * 8)}
		}

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 3)

		if !ok || mode.Partition != 3 {
			t.Fatalf("mode = %+v ok=%t, want partition 3", mode, ok)
		}
		for block := range want {
			if mode.BlockMV[block] != want[block] {
				t.Fatalf("partition 3 block %d MV = %+v, want %+v", block, mode.BlockMV[block], want[block])
			}
		}
	})
}

func TestSelectInterFrameReferenceMotionVectorRefinesSubpixelCandidate(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 13, 90, 170)
	last := testVP8Frame(t, 48, 48, 40, 90, 170)
	for row := 0; row < last.Img.CodedHeight; row++ {
		for col := 0; col < last.Img.CodedWidth; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte((19 + row*17 + col*13 + row*col*3) & 0xff)
		}
	}
	last.ExtendBorders()
	refStart := last.Img.YFull[last.Img.YOrigin+(16-2)*last.Img.YStride+16-2:]
	dsp.SixTapPredict16x16(refStart, last.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 1, 1, 2, 2, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("selection = %v %+v, want last subpixel +2,+2", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorPrefersCheaperMotionOnTie(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 32, 32, 40, 90, 170)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	_, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if mv != (vp8enc.MotionVector{}) {
		t.Fatalf("mv = %+v, want zero MV for equal-SAD candidates", mv)
	}
}

func TestMacroblockSubpixelSADHonorsLimit(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(t, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	full, ok := macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, maxInt())
	if !ok {
		t.Fatalf("macroblockSubpixelSAD returned ok=false")
	}
	limited, ok := macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, 1024)
	if !ok {
		t.Fatalf("limited macroblockSubpixelSAD returned ok=false")
	}
	if limited <= 1024 || limited >= full {
		t.Fatalf("limited SAD = %d, full = %d, want early result above limit and below full", limited, full)
	}
}

func TestMacroblockSubpixelVarianceMatchesBilinearPredictor(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((17 + row*13 + col*19 + row*col*3) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 6, src.Y[16*src.YStride+16:], src.YStride)

	variance, sse, ok := macroblockSubpixelVariance(sourceImageFromPublic(src), &ref.Img, 16, 16, 16, 16, 2, 6)

	if !ok {
		t.Fatalf("macroblockSubpixelVariance returned ok=false")
	}
	if variance != 0 || sse != 0 {
		t.Fatalf("subpixel variance = %d/%d, want exact bilinear match", variance, sse)
	}
}

func TestIterativeInterFrameSubpixelMotionVectorUsesBilinearVariance(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*11 + col*7 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)

	mv, cost, ok := iterativeInterFrameSubpixelMotionVector(sourceImageFromPublic(src), &ref.Img, 1, 1, vp8enc.MotionVector{}, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if !ok {
		t.Fatalf("iterativeInterFrameSubpixelMotionVector returned ok=false")
	}
	if mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("mv = %+v, want +2,+2 quarter-pel candidate", mv)
	}
	if want := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost != want {
		t.Fatalf("cost = %d, want zero distortion plus mv cost %d", cost, want)
	}
}

func TestCollectInterFrameMotionCandidatesIncludesSubpixelCandidate(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((31 + row*5 + col*17 + row*col*11) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	refs := []interAnalysisReference{{Frame: vp8common.LastFrame, Img: &ref.Img}}
	var candidates [interFrameMotionCandidateMax]interAnalysisMotionCandidate

	count := collectInterFrameMotionCandidates(sourceImageFromPublic(src), refs, len(refs), 1, 1, 3, 3, testInterSearchQIndex, nil, nil, nil, &vp8tables.DefaultMVContext, &candidates)

	if count != 2 {
		t.Fatalf("candidate count = %d, want full-pixel plus subpixel", count)
	}
	if candidates[0].MV != (vp8enc.MotionVector{}) {
		t.Fatalf("full-pixel candidate = %+v, want zero MV", candidates[0].MV)
	}
	if candidates[1].MV != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("subpixel candidate = %+v, want +2,+2", candidates[1].MV)
	}
}

func TestCollectInterFrameMotionCandidatesIncludesNearestAndNear(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 80, 90, 170)
	ref := testVP8Frame(t, 16, 16, 80, 90, 170)
	refs := []interAnalysisReference{{Frame: vp8common.LastFrame, Img: &ref.Img}}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 8}}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 8}}
	aboveLeft := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	var candidates [interFrameMotionCandidateMax]interAnalysisMotionCandidate

	count := collectInterFrameMotionCandidates(sourceImageFromPublic(src), refs, len(refs), 0, 0, 1, 1, testInterSearchQIndex, &above, &left, &aboveLeft, &vp8tables.DefaultMVContext, &candidates)

	if count != 3 {
		t.Fatalf("candidate count = %d, want zero, nearest, near", count)
	}
	want := [...]vp8enc.MotionVector{{}, {Col: 8}, {Row: 8}}
	for i := range want {
		if candidates[i].MV != want[i] {
			t.Fatalf("candidate[%d] MV = %+v, want %+v", i, candidates[i].MV, want[i])
		}
	}
}

func TestInterFrameSubpixelSearchCandidateCount(t *testing.T) {
	if got := interFrameSubpixelSearchCandidateCount(); got != 31 {
		t.Fatalf("subpixel candidate count = %d, want libvpx iterative max 31", got)
	}
}

func TestPredictBestKeyFrameIntraModeChoosesBPred(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	pred := testVP8Frame(t, 32, 32, 128, 128, 128)
	for i := 0; i < 16; i++ {
		pred.Img.Y[15*pred.Img.YStride+16+i] = byte(40 + i*7)
		pred.Img.Y[(16+i)*pred.Img.YStride+15] = byte(210 - i*5)
	}
	pred.ExtendBorders()

	var genScratch vp8dec.IntraReconstructionScratch
	refs := vp8dec.BuildIntraPredictorRefs(&pred.Img, 1, 1, &genScratch.Refs)
	yOff := 16*pred.Img.YStride + 16
	y := pred.Img.Y[yOff:]
	for block := 0; block < 16; block++ {
		var blockPred [16]byte
		if !predictAnalysisBPredBlock(vp8common.BHEPred, blockPred[:], 4, y, pred.Img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			t.Fatalf("predictAnalysisBPredBlock returned false")
		}
		copyBPredBlock(blockPred[:], 4, y, pred.Img.YStride, block)
		copyBPredBlockToSource(blockPred[:], 4, src, 1, 1, block)
	}
	for row := 16; row < 32; row++ {
		for col := 16; col < 32; col++ {
			pred.Img.Y[row*pred.Img.YStride+col] = 128
		}
	}

	var scratch vp8dec.IntraReconstructionScratch
	quant := testMacroblockQuant(20)
	mode, ok := predictBestKeyFrameIntraMode(sourceImageFromPublic(src), 20, 1, 1, nil, nil, nil, nil, &quant, &pred.Img, &scratch, false)
	if !ok {
		t.Fatalf("predictBestKeyFrameIntraMode returned ok=false")
	}
	if mode.YMode != vp8common.BPred || mode.UVMode != vp8common.DCPred {
		t.Fatalf("mode = %+v, want B_PRED/DC chroma", mode)
	}
	if mode.BModes[0] != vp8common.BHEPred {
		t.Fatalf("B mode[0] = %v, want B_HE_PRED", mode.BModes[0])
	}
}

func TestEstimateFastBPredIntraModeRestrictsCandidatesLikeLibvpx(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	e.analysis = testVP8Frame(t, 32, 32, 128, 128, 128)
	for i := 0; i < 16; i++ {
		e.analysis.Img.Y[15*e.analysis.Img.YStride+16+i] = byte(30 + i*11)
		e.analysis.Img.Y[(16+i)*e.analysis.Img.YStride+15] = byte(220 - i*9)
	}
	e.analysis.ExtendBorders()

	var genScratch vp8dec.IntraReconstructionScratch
	refs := vp8dec.BuildIntraPredictorRefs(&e.analysis.Img, 1, 1, &genScratch.Refs)
	yOff := 16*e.analysis.Img.YStride + 16
	y := e.analysis.Img.Y[yOff:]
	for block := 0; block < 16; block++ {
		var blockPred [16]byte
		if !predictAnalysisBPredBlock(vp8common.BLDPred, blockPred[:], 4, y, e.analysis.Img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			t.Fatalf("predictAnalysisBPredBlock returned false")
		}
		copyBPredBlock(blockPred[:], 4, y, e.analysis.Img.YStride, block)
		copyBPredBlockToSource(blockPred[:], 4, src, 1, 1, block)
	}
	for row := 16; row < 32; row++ {
		for col := 16; col < 32; col++ {
			e.analysis.Img.Y[row*e.analysis.Img.YStride+col] = 128
		}
	}

	mode, _, _, ok := e.estimateFastBPredIntraModeScore(sourceImageFromPublic(src), 1, 1, 20, maxInt())
	if !ok {
		t.Fatalf("estimateFastBPredIntraModeScore returned ok=false")
	}
	for block, bMode := range mode.BModes {
		if bMode > vp8common.BHEPred {
			t.Fatalf("fast B mode[%d] = %v, want libvpx non-RD candidate <= B_HE_PRED", block, bMode)
		}
	}
}

func TestPredictBestBPredLumaModeRDReconstructsChosenBlocks(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			src.Y[row*src.YStride+col] = 200
		}
	}
	pred := testVP8Frame(t, 16, 16, 128, 128, 128)
	quant := testMacroblockQuant(4)
	var scratch vp8dec.IntraReconstructionScratch
	probs := vp8tables.DefaultCoefProbs

	_, rate, dist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), 4, 0, true, 0, 0, nil, nil, nil, nil, &quant, &pred.Img, &scratch, 0, &probs, false)

	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD returned ok=false")
	}
	if rate <= 0 || dist < 0 {
		t.Fatalf("rate=%d dist=%d, want positive rate and non-negative distortion", rate, dist)
	}
	if pred.Img.Y[0] <= 128 {
		t.Fatalf("reconstructed block sample = %d, want above raw predictor 128", pred.Img.Y[0])
	}
}

func TestPredictBestIntraChromaModeRDUsesTransformTokenCost(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			src.U[row*src.UStride+col] = byte(24 + ((row*37 + col*19) & 0xff))
			src.V[row*src.VStride+col] = byte(224 - ((row*11 + col*43) & 0x7f))
		}
	}
	pred := testVP8Frame(t, 16, 16, 128, 128, 128)
	quant := testRegularMacroblockQuant(t, 20)
	probs := vp8tables.DefaultCoefProbs
	var scratch vp8dec.IntraReconstructionScratch

	mode, rate, dist, ok := predictBestIntraChromaModeRD(sourceImageFromPublic(src), 20, 0, true, 0, 0, nil, nil, &quant, &pred.Img, &scratch, &probs, false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRD returned ok=false")
	}
	if mode < vp8common.DCPred || mode > vp8common.TMPred {
		t.Fatalf("UV mode = %v, want valid intra chroma mode", mode)
	}
	if modeRate := intraUVModeRate(true, mode); rate <= modeRate {
		t.Fatalf("UV rate = %d, want mode rate %d plus transform token cost", rate, modeRate)
	}

	chosenPred := testVP8Frame(t, 16, 16, 128, 128, 128)
	var chosenScratch vp8dec.IntraReconstructionScratch
	if !predictAnalysisChroma(&chosenPred.Img, 0, 0, mode, &chosenScratch) {
		t.Fatalf("predictAnalysisChroma returned false")
	}
	tokenRate, wantDist := wholeBlockChromaTransformRD(sourceImageFromPublic(src), &chosenPred.Img, 0, 0, 20, 0, nil, nil, &quant, &probs, false)
	wantRate := intraUVModeRate(true, mode) + tokenRate
	if rate != wantRate || dist != wantDist {
		t.Fatalf("UV RD = rate:%d dist:%d, want transform/token rate:%d dist:%d", rate, dist, wantRate, wantDist)
	}
	if sse := macroblockChromaSSE(sourceImageFromPublic(src), &chosenPred.Img, 0, 0); dist == sse {
		t.Fatalf("UV distortion = %d, want transform-domain error rather than chroma SSE", dist)
	}
}

func TestCoefficientBlockTokenRateUsesEntropyCosts(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero [16]int16

	zeroRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &zero, 0)
	wantZero := treeTokenCost(vp8tables.CoefTree[:], probs[3][0][0][:], vp8tables.DCTEOBToken)
	if zeroRate != wantZero {
		t.Fatalf("zero token rate = %d, want %d", zeroRate, wantZero)
	}

	positive := [16]int16{0: 1}
	positiveRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &positive, 1)
	negative := [16]int16{0: -1}
	negativeRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &negative, 1)
	if positiveRate <= zeroRate {
		t.Fatalf("positive token rate = %d, zero = %d, want nonzero token to cost more", positiveRate, zeroRate)
	}
	if negativeRate <= zeroRate {
		t.Fatalf("negative token rate = %d, zero = %d, want nonzero token to cost more", negativeRate, zeroRate)
	}

	zeroThenOne := [16]int16{1: 1}
	zeroThenOneRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &zeroThenOne, 2)
	p0 := probs[3][0][0]
	p1 := probs[3][vp8tables.CoefBandsTable[1]][0]
	p2 := probs[3][vp8tables.CoefBandsTable[2]][vp8tables.PrevTokenClass[vp8tables.OneToken]]
	wantZeroThenOne := boolBitCost(p0[0], 1) +
		boolBitCost(p0[1], 0) +
		nonZeroCoeffTokenRate(p1, vp8tables.OneToken) +
		boolBitCost(128, 0) +
		treeTokenCost(vp8tables.CoefTree[:], p2[:], vp8tables.DCTEOBToken)
	if zeroThenOneRate != wantZeroThenOne {
		t.Fatalf("zero-then-one rate = %d, want %d", zeroThenOneRate, wantZeroThenOne)
	}
}

func TestBPredAnalysisKeyFrameUsesNeighborContexts(t *testing.T) {
	var modes [16]vp8common.BPredictionMode
	modes[1] = vp8common.BTMPred
	modes[4] = vp8common.BHDPred

	aboveB := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred}
	aboveB.BModes[12] = vp8common.BHUPred
	aboveB.BModes[13] = vp8common.BRDPred
	leftB := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred}
	leftB.BModes[3] = vp8common.BVLPred
	leftB.BModes[7] = vp8common.BLDPred

	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 0); got != vp8common.BHUPred {
		t.Fatalf("above edge B_PRED context = %v, want B_HU_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 1); got != vp8common.BRDPred {
		t.Fatalf("above edge block 1 context = %v, want B_RD_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 0); got != vp8common.BVLPred {
		t.Fatalf("left edge B_PRED context = %v, want B_VL_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 4); got != vp8common.BLDPred {
		t.Fatalf("left edge block 4 context = %v, want B_LD_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 5); got != vp8common.BTMPred {
		t.Fatalf("internal above context = %v, want B_TM_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 5); got != vp8common.BHDPred {
		t.Fatalf("internal left context = %v, want B_HD_PRED", got)
	}
}

func TestBPredAnalysisKeyFrameMapsWholeBlockNeighborContexts(t *testing.T) {
	var modes [16]vp8common.BPredictionMode
	aboveV := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.VPred}
	aboveH := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.HPred}
	leftTM := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.TMPred}

	if got := bPredAnalysisAboveMode(true, &aboveV, modes, 0); got != vp8common.BVEPred {
		t.Fatalf("above V_PRED context = %v, want B_VE_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveH, modes, 0); got != vp8common.BHEPred {
		t.Fatalf("above H_PRED context = %v, want B_HE_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftTM, modes, 0); got != vp8common.BTMPred {
		t.Fatalf("left TM_PRED context = %v, want B_TM_PRED", got)
	}
	if got := bPredAnalysisAboveMode(false, &aboveV, modes, 0); got != vp8common.BDCPred {
		t.Fatalf("inter above context = %v, want B_DC_PRED", got)
	}
	if got := bPredAnalysisLeftMode(false, &leftTM, modes, 0); got != vp8common.BDCPred {
		t.Fatalf("inter left context = %v, want B_DC_PRED", got)
	}
}

func TestMacroblockCoefficientsEmptyTreatsSkippedDCLumaAsEmpty(t *testing.T) {
	var coeffs vp8enc.MacroblockCoefficients
	for block := 0; block < 16; block++ {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	if !macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("empty = false, want true for skipped-DC luma blocks")
	}

	coeffs.SetBlockEOB(0, 2)
	if macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("empty = true, want false for luma AC EOB")
	}

	coeffs.SetBlockEOB(0, 1)
	if !macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("whole-block empty = false, want true for luma DC carried by empty Y2")
	}
	if macroblockCoefficientsEmpty(&coeffs, true) {
		t.Fatalf("4x4 empty = true, want false for luma DC coefficient")
	}
}

func TestLibvpxRDConstantsMatchSinglePassInitializeRDConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		rdMult int
		rdDiv  int
		errBit int
	}{
		{qIndex: 0, rdMult: 44, rdDiv: 100, errBit: 1},
		{qIndex: 4, rdMult: 179, rdDiv: 100, errBit: 1},
		{qIndex: 40, rdMult: 38, rdDiv: 1, errBit: 34},
		{qIndex: 127, rdMult: 690, rdDiv: 1, errBit: 627},
	}
	for _, tt := range tests {
		rdMult, rdDiv := libvpxRDConstants(tt.qIndex)
		if rdMult != tt.rdMult || rdDiv != tt.rdDiv {
			t.Fatalf("q=%d rd = %d/%d, want %d/%d", tt.qIndex, rdMult, rdDiv, tt.rdMult, tt.rdDiv)
		}
		if got := libvpxErrorPerBit(tt.qIndex); got != tt.errBit {
			t.Fatalf("q=%d errorperbit = %d, want %d", tt.qIndex, got, tt.errBit)
		}
	}

	if got := rdModeScore(4, 512, 10); got != 1358 {
		t.Fatalf("rdModeScore low q = %d, want libvpx RDCOST 1358", got)
	}
	if got := rdModeScore(40, 512, 100); got != 176 {
		t.Fatalf("rdModeScore mid q = %d, want libvpx RDCOST 176", got)
	}
}

func TestLibvpxRDConstantsUseZbinOverQuant(t *testing.T) {
	baseMult, baseDiv := libvpxRDConstants(127)
	overMult, overDiv := libvpxRDConstantsWithZbin(127, 128)
	if overMult != 989 || overDiv != 1 {
		t.Fatalf("q127 zbin-over-quant rd = %d/%d, want 989/1", overMult, overDiv)
	}
	if overMult <= baseMult || overDiv != baseDiv {
		t.Fatalf("zbin-over-quant rd = %d/%d, base %d/%d, want larger multiplier with same divider", overMult, overDiv, baseMult, baseDiv)
	}
	if got := rdModeScoreWithZbin(127, 128, 512, 100); got != 2078 {
		t.Fatalf("zbin-over-quant rdModeScore = %d, want libvpx RDCOST 2078", got)
	}
	if got := libvpxErrorPerBitWithZbin(127, 128); got != 899 {
		t.Fatalf("zbin-over-quant errorperbit = %d, want 899", got)
	}
}

func TestLibvpxSADPerBitLUTsMatchInitializeMEConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		want16 int
		want4  int
	}{
		{qIndex: 0, want16: 2, want4: 2},
		{qIndex: 6, want16: 2, want4: 3},
		{qIndex: 20, want16: 3, want4: 4},
		{qIndex: 30, want16: 4, want4: 5},
		{qIndex: 42, want16: 5, want4: 6},
		{qIndex: 54, want16: 6, want4: 7},
		{qIndex: 62, want16: 6, want4: 8},
		{qIndex: 78, want16: 8, want4: 10},
		{qIndex: 90, want16: 9, want4: 12},
		{qIndex: 102, want16: 10, want4: 13},
		{qIndex: 114, want16: 11, want4: 16},
		{qIndex: 126, want16: 14, want4: 20},
	}
	for _, tt := range tests {
		if got := libvpxSADPerBit16(tt.qIndex); got != tt.want16 {
			t.Fatalf("q=%d sad_per_bit16 = %d, want %d", tt.qIndex, got, tt.want16)
		}
		if got := libvpxSADPerBit4(tt.qIndex); got != tt.want4 {
			t.Fatalf("q=%d sad_per_bit4 = %d, want %d", tt.qIndex, got, tt.want4)
		}
	}
}

func TestInterMotionModeVectorCostOnlyChargesNewMVDelta(t *testing.T) {
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	newMode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}

	if got, want := interMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext), vp8enc.MotionVectorBitCost(newMode.MV, above.MV, &vp8tables.DefaultMVContext, libvpxRDNewMVBitCostWeight); got != want {
		t.Fatalf("NEWMV vector cost = %d, want delta cost %d", got, want)
	}

	nearest := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NearestMV, MV: above.MV}
	if got := interMotionModeVectorCost(&nearest, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext); got != 0 {
		t.Fatalf("NEARESTMV vector cost = %d, want 0", got)
	}

	liveProbs := vp8tables.DefaultMVContext
	liveProbs[1][0] = 1
	liveCost := interMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1, &liveProbs)
	wantLive := vp8enc.MotionVectorBitCost(newMode.MV, above.MV, &liveProbs, libvpxRDNewMVBitCostWeight)
	if liveCost != wantLive {
		t.Fatalf("live NEWMV vector cost = %d, want live-prob delta cost %d", liveCost, wantLive)
	}
	if liveCost == vp8enc.MotionVectorBitCost(newMode.MV, above.MV, &vp8tables.DefaultMVContext, libvpxRDNewMVBitCostWeight) {
		t.Fatalf("live NEWMV vector cost = default cost %d, want MV probs to affect RD cost", liveCost)
	}
}

func TestInterMotionModeVectorCostChargesRDNewMVWithLibvpxWeight(t *testing.T) {
	mvProbs := vp8tables.DefaultMVContext
	bestRefMV := vp8enc.MotionVector{Row: 8, Col: -16}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 24, Col: 8}}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: bestRefMV}

	got := interMotionModeVectorCost(&mode, &above, nil, nil, 0, 0, 1, 1, &mvProbs)
	want := vp8enc.MotionVectorBitCost(mode.MV, bestRefMV, &mvProbs, libvpxRDNewMVBitCostWeight)
	if got != want {
		t.Fatalf("RD NEWMV vector cost = %d, want MotionVectorBitCost weight-96 cost %d", got, want)
	}
	if fastWeight := vp8enc.MotionVectorBitCost(mode.MV, bestRefMV, &mvProbs, libvpxFastNewMVBitCostWeight); got == fastWeight {
		t.Fatalf("RD NEWMV vector cost = fast weight-128 cost %d, want weight 96", fastWeight)
	}
}

func TestFastInterMotionModeRateKeepsPickInterNewMVWeight(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	refRate := 17
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, defaultInterFrameSignBias())

	got := e.fastInterMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 1, refRate)
	want := boolBitCost(63, 1) +
		refRate +
		interPredictionModeRate(vp8common.NewMV, counts) +
		vp8enc.MotionVectorBitCost(mode.MV, above.MV, &vp8tables.DefaultMVContext, libvpxFastNewMVBitCostWeight)
	if got != want {
		t.Fatalf("fast NEWMV mode rate = %d, want pickinter weight-128 rate %d", got, want)
	}
	if rdRate := e.interMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 1, refRate); got == rdRate {
		t.Fatalf("fast NEWMV mode rate = RD rate %d, want separate pickinter weight", rdRate)
	}
}

func TestEncoderInterMotionModeRateUsesAltRefSignBias(t *testing.T) {
	e := &VP8Encoder{
		refProbIntra:       63,
		refProbLast:        128,
		refProbGolden:      128,
		sourceAltRefActive: true,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.AltRefFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: -24}}
	refRate := 23

	signBias := e.interFrameSignBias()
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, signBias)
	vectorCost := interMotionModeVectorCostWithNewMVWeightAndSignBias(&mode, &above, nil, nil, 0, 0, 1, 2, &vp8tables.DefaultMVContext, libvpxRDNewMVBitCostWeight, signBias)
	wantVectorCost := vp8enc.MotionVectorBitCost(mode.MV, vp8enc.MotionVector{Col: -16}, &vp8tables.DefaultMVContext, libvpxRDNewMVBitCostWeight)
	if vectorCost != wantVectorCost {
		t.Fatalf("sign-biased NEWMV vector cost = %d, want cost against inverted best ref MV %d", vectorCost, wantVectorCost)
	}

	want := boolBitCost(63, 1) +
		refRate +
		interPredictionModeRate(vp8common.NewMV, counts) +
		vectorCost
	if got := e.interMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 2, refRate); got != want {
		t.Fatalf("sign-biased inter mode rate = %d, want %d", got, want)
	}
}

func TestEncoderInterReferenceMotionPredictorsUseAltRefSignBias(t *testing.T) {
	e := &VP8Encoder{sourceAltRefActive: true}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}

	nearest, near := e.interAnalysisReferenceMotionPredictors(vp8common.AltRefFrame, &above, nil, nil, 0, 0, 1, 2)
	if nearest != (vp8enc.MotionVector{Col: -16}) || !near.IsZero() {
		t.Fatalf("ALTREF predictors = %+v/%+v, want inverted nearest col -16 and zero near", nearest, near)
	}
}

func TestInterPredictionModeRateMirrorsWriterBranches(t *testing.T) {
	counts := vp8enc.InterModeCounts{Intra: 3, Nearest: 4, Near: 2, Split: 1}
	probs := vp8tables.InterModeContexts
	tests := []struct {
		name string
		mode vp8common.MBPredictionMode
		want int
	}{
		{name: "zero", mode: vp8common.ZeroMV, want: boolBitCost(probs[3][0], 0)},
		{name: "nearest", mode: vp8common.NearestMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 0)},
		{name: "near", mode: vp8common.NearMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 0)},
		{name: "new", mode: vp8common.NewMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 1) + boolBitCost(probs[1][3], 0)},
		{name: "split", mode: vp8common.SplitMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 1) + boolBitCost(probs[1][3], 1)},
	}
	for _, tt := range tests {
		if got := interPredictionModeRate(tt.mode, counts); got != tt.want {
			t.Fatalf("%s mode rate = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestInterMotionModeRateChargesReferenceModeAndVector(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, probSkipFalse: 200}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, defaultInterFrameSignBias())
	want := boolBitCost(63, 1) +
		e.interReferenceFrameRate(vp8common.GoldenFrame) +
		interPredictionModeRate(vp8common.NewMV, counts) +
		interMotionModeVectorCost(&mode, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext)

	if got := e.interMotionModeRate(&mode, &above, nil, nil, 0, 0, 1, 1); got != want {
		t.Fatalf("inter mode rate = %d, want %d", got, want)
	}
	if got := interMacroblockSkipRate(false); got != boolBitCost(128, 0) {
		t.Fatalf("coded skip rate = %d, want prob-128 false cost", got)
	}
	if got := interMacroblockSkipRate(true); got != boolBitCost(128, 1) {
		t.Fatalf("skipped rate = %d, want prob-128 true cost", got)
	}
	if got := e.interMacroblockSkipRate(false); got != boolBitCost(200, 0) {
		t.Fatalf("live coded skip rate = %d, want prob-200 false cost", got)
	}
	if got := e.interMacroblockSkipRate(true); got != boolBitCost(200, 1) {
		t.Fatalf("live skipped rate = %d, want prob-200 true cost", got)
	}
	if got, want := e.interIntraMacroblockModeRate(), boolBitCost(200, 0)+boolBitCost(63, 0); got != want {
		t.Fatalf("inter-intra mode rate = %d, want skip plus intra-reference rate %d", got, want)
	}
}

func TestEstimateFastInterModeScoreUsesLibvpxPickInterDistortion(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	ref := testVP8Frame(t, 16, 16, 50, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	qIndex := testInterSearchQIndex

	got, ok := e.estimateFastInterModeScore(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, qIndex)
	if !ok {
		t.Fatalf("estimateFastInterModeScore returned ok=false")
	}
	variance, sse := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &ref.Img, 0, 0, mode.MV)
	if variance != 0 || sse == 0 {
		t.Fatalf("variance/sse = %d/%d, want flat luma offset with zero variance and nonzero SSE", variance, sse)
	}
	rate := e.interMotionModeRate(&mode, nil, nil, nil, 0, 0, 1, 1)
	want := rdModeScore(qIndex, rate, variance)
	if got != want {
		t.Fatalf("fast inter score = %d, want rate plus luma variance %d", got, want)
	}
	if sseScore := rdModeScore(qIndex, rate, sse); got == sseScore {
		t.Fatalf("fast inter score used SSE %d, want libvpx variance distortion", sse)
	}
}

func TestInterModeForRDLoopEntryAllowsZeroNewMVOnFlatMatch(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	fillBenchmarkVP8Image(&e.analysis.Img, 72, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 72, 90, 170)
	last := testVP8Frame(t, 16, 16, 72, 90, 170)
	ref := interAnalysisReference{Frame: vp8common.LastFrame, Img: &last.Img}
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	}

	mode, ok := e.interModeForRDLoopEntry(sourceImageFromPublic(src), ref, 0, vp8common.NewMV, 0, 0, 1, 1, testInterSearchQIndex, nil, nil, nil, &newMVCandidates)
	if !ok {
		t.Fatalf("RD NEWMV loop entry rejected zero MV on flat matching frame")
	}
	if mode.Mode != vp8common.NewMV || mode.RefFrame != vp8common.LastFrame || !mode.MV.IsZero() {
		t.Fatalf("RD NEWMV loop entry mode = %+v, want LAST/NEWMV with zero MV", mode)
	}
}

func TestFastInterModeForLoopEntryRejectsZeroNewMVOnFlatMatch(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.modeProbs.MV = vp8tables.DefaultMVContext
	fillBenchmarkVP8Image(&e.analysis.Img, 72, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 72, 90, 170)
	last := testVP8Frame(t, 16, 16, 72, 90, 170)
	ref := interAnalysisReference{Frame: vp8common.LastFrame, Img: &last.Img}
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	}

	mode, ok := e.fastInterModeForLoopEntry(sourceImageFromPublic(src), ref, 0, 1, vp8common.NewMV, 0, 0, 1, 1, testInterSearchQIndex, nil, nil, nil, &newMVCandidates)
	if ok {
		t.Fatalf("fast NEWMV loop entry accepted mode %+v, want zero MV rejected", mode)
	}
}

func TestSelectRDInterFrameMotionVectorAllowsSubpixelRefinementWithBestRefMVCost(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*11 + col*7 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	bestRefMV := vp8enc.MotionVector{Row: 2, Col: 2}

	mv, cost := selectRDInterFrameMotionVectorWithSearchStart(sourceImageFromPublic(src), &ref.Img, 1, 1, 3, 3, bestRefMV, testInterSearchQIndex, defaultInterAnalysisSearchConfig(), interFrameSearchStart{}, &vp8tables.DefaultMVContext)

	if mv != bestRefMV {
		t.Fatalf("RD NEWMV search MV = %+v, want accepted subpel refinement %+v", mv, bestRefMV)
	}
	want := interMotionSearchErrorVectorCost(mv, bestRefMV, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	if cost != want {
		t.Fatalf("RD NEWMV search cost = %d, want best_ref_mv anchored subpel cost %d", cost, want)
	}
	if zeroAnchor := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost == zeroAnchor {
		t.Fatalf("RD NEWMV search cost = zero-anchor cost %d, want best_ref_mv anchor", zeroAnchor)
	}
}

func TestSelectFastInterFrameModeDecisionCanChooseInterleavedIntra(t *testing.T) {
	e := &VP8Encoder{
		opts:          EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
		probSkipFalse: 128,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	if err := e.analysis.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("analysis resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte((row*29 + col*53) & 255)
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, testInterSearchQIndex, 0, nil, nil, nil, nil)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if !decision.useIntra || decision.intraMode.Mode != vp8common.DCPred || decision.intraMode.RefFrame != vp8common.IntraFrame {
		t.Fatalf("decision = %+v, want intra DC from libvpx interleaved mode loop", decision)
	}
}

func TestSelectFastInterFrameModeDecisionUsesLibvpxReferenceSlots(t *testing.T) {
	e := &VP8Encoder{
		opts:          EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
		probSkipFalse: 128,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	if err := e.analysis.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("analysis resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.analysis.Img, 127, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 127, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte((17 + row*43 + col*71 + row*col*11) & 255)
		}
	}
	last := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte((231 - row*17 - col*31) & 255)
		}
	}
	last.ExtendBorders()
	golden := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := 0; row < 16; row++ {
		copy(golden.Img.Y[row*golden.Img.YStride:], src.Y[row*src.YStride:row*src.YStride+16])
	}
	golden.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
	}

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, testInterSearchQIndex, 0, nil, nil, nil, nil)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if decision.useIntra || decision.ref.Frame != vp8common.GoldenFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want GOLDEN/ZEROMV from libvpx slot-2 loop entry", decision)
	}
}

func TestSelectFastInterFrameModeDecisionUsesThresholdState(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.Deadline = DeadlineRealtime
	e.opts.CpuUsed = 8
	fillBenchmarkVP8Image(&e.analysis.Img, 96, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 96, 90, 170)
	last := testVP8Frame(t, 16, 16, 96, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			y := byte((11 + row*37 + col*19 + row*col*5) & 255)
			src.Y[row*src.YStride+col] = y
			last.Img.Y[row*last.Img.YStride+col] = y
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	e.resetInterRDThresholdMultipliers()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, 40, 0, nil, nil, nil, nil)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if decision.useIntra || decision.ref.Frame != vp8common.LastFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want LAST/ZEROMV on matching reference", decision)
	}
	if got := e.interModeTestHitCounts[libvpxThrZero1]; got != 1 {
		t.Fatalf("ZERO1 hit count = %d, want 1", got)
	}
	if !e.interRDThreshTouched[libvpxThrZero1] {
		t.Fatalf("ZERO1 threshold was not touched")
	}
	if got := e.interRDThreshMult[libvpxThrZero1]; got >= libvpxRDThreshMultStart {
		t.Fatalf("ZERO1 threshold multiplier = %d, want below start after improvement", got)
	}
}

func TestSelectRDInterFrameModeDecisionStopsOnStaticEncodeBreakout(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.opts.StaticThreshold = 1

	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, 16, 16, 128, 90, 170)
	refs := [...]interAnalysisReference{{
		Frame:      vp8common.LastFrame,
		Img:        &last.Img,
		RefRateSet: true,
		RefRate:    1 << 20,
	}}
	quant := testRegularMacroblockQuant(t, 20)

	decision, ok := e.selectRDInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, 20, staticSegmentID, nil, nil, nil, nil, nil, &quant)

	if !ok {
		t.Fatalf("RD mode decision returned ok=false")
	}
	if !decision.cyclicRefreshEligible() || decision.interMode.SegmentID != staticSegmentID {
		t.Fatalf("decision = %+v, want static breakout to stop on LAST/ZEROMV with cyclic segment", decision)
	}
}

func TestEstimateInterResidualRDScoreUsesLibvpxStaticBreakoutRate(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.opts.StaticThreshold = 1
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	ref := testVP8Frame(t, 16, 16, 128, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(t, 20)

	score, rdLoopSkip, ok := e.estimateInterResidualRDScoreWithReferenceRateAndSkip(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, 0)

	if !ok || !rdLoopSkip {
		t.Fatalf("static breakout score ok=%t rdLoopSkip=%t, want true/true", ok, rdLoopSkip)
	}
	if want := rdModeScoreWithZbin(20, 0, 500, 0); score != want {
		t.Fatalf("static breakout RD score = %d, want libvpx rate-500 score %d", score, want)
	}

	acct, ok := e.estimateInterResidualRDAccounting(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, 0)
	if !ok || !acct.rdLoopSkip || !acct.mbSkipCoeff {
		t.Fatalf("static breakout accounting ok=%t rdLoopSkip=%t mbSkipCoeff=%t, want true/true/true", ok, acct.rdLoopSkip, acct.mbSkipCoeff)
	}
	if acct.rate2 != 500 || acct.distortion2 != 0 || acct.rd != score {
		t.Fatalf("static breakout accounting rate/dist/rd = %d/%d/%d, want 500/0/%d", acct.rate2, acct.distortion2, acct.rd, score)
	}
}

func TestEstimateInterResidualRDAccountingUsesTransformDomainDistortion(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 96, 90, 170)
	for i := range src.Y {
		src.Y[i] = byte(64 + (i*17)%96)
	}
	for i := range src.U {
		src.U[i] = byte(80 + (i*11)%48)
		src.V[i] = byte(144 + (i*7)%48)
	}
	ref := testVP8Frame(t, 16, 16, 96, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(t, 20)

	acct, ok := e.estimateInterResidualRDAccounting(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, 0)
	if !ok {
		t.Fatalf("estimateInterResidualRDAccounting returned ok=false")
	}
	var coeffs vp8enc.MacroblockCoefficients
	stats := buildPredictedMacroblockCoefficientsRD(&e.coefProbs, sourceImageFromPublic(src), 0, 0, &ref.Img, nil, nil, &quant, 20, e.rc.currentZbinOverQuant, interZbinModeBoost(&mode), false, false, e.libvpxUseFastQuant(), false, &coeffs)
	wantDistortion := stats.distortionY + stats.distortionUV
	if acct.distortion2 != wantDistortion || acct.distortionUV != stats.distortionUV {
		t.Fatalf("accounting distortion = %d uv=%d, want transform-domain %d uv=%d", acct.distortion2, acct.distortionUV, wantDistortion, stats.distortionUV)
	}
	if pixelSSE := macroblockImageSSE(sourceImageFromPublic(src), &ref.Img, 0, 0); acct.distortion2 == pixelSSE {
		t.Fatalf("accounting distortion = pixel SSE %d, want transform-domain distortion", pixelSSE)
	}
}

func TestEstimateInterResidualRDAccountingReturnsLibvpxRate2AndYRD(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.refProbIntra = 63
	e.refProbLast = 128
	e.refProbGolden = 128
	e.probSkipFalse = 200
	e.modeProbs.MV = vp8tables.DefaultMVContext
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	for i := range src.Y {
		src.Y[i] = byte(32 + (i*13)%128)
	}
	ref := testVP8Frame(t, 16, 16, 96, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(t, 20)
	refRate := 17

	acct, ok := e.estimateInterResidualRDAccounting(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, refRate)
	if !ok {
		t.Fatalf("estimateInterResidualRDAccounting returned ok=false")
	}
	if acct.mbSkipCoeff {
		t.Fatalf("mbSkipCoeff = true, want coded residual for rate2 accounting test")
	}
	modeRate := e.interMotionModeRateWithReferenceRate(&mode, nil, nil, nil, 0, 0, 1, 1, refRate)
	wantOtherCost := e.interMacroblockSkipRate(false)
	wantRate2 := modeRate + wantOtherCost + acct.rateY + acct.rateUV
	wantRefCost := boolBitCost(e.refProbIntra, 1) + refRate
	if acct.rate2 != wantRate2 || acct.otherCost != wantOtherCost || acct.refCost != wantRefCost {
		t.Fatalf("rate2/other/ref = %d/%d/%d, want %d/%d/%d", acct.rate2, acct.otherCost, acct.refCost, wantRate2, wantOtherCost, wantRefCost)
	}
	if wantRD := rdModeScoreWithZbin(20, e.rc.currentZbinOverQuant, acct.rate2, acct.distortion2); acct.rd != wantRD {
		t.Fatalf("rd = %d, want %d", acct.rd, wantRD)
	}
	wantYRD := rdModeScoreWithZbin(20, e.rc.currentZbinOverQuant, acct.rate2-acct.rateUV-acct.otherCost-acct.refCost, acct.distortion2-acct.distortionUV)
	if acct.yrd != wantYRD {
		t.Fatalf("yrd = %d, want %d", acct.yrd, wantYRD)
	}
}

func TestEstimateInterResidualRDAccountingEmptyCoeffSkipBacksOutTokenRates(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.refProbIntra = 63
	e.probSkipFalse = 200
	e.opts.StaticThreshold = 0
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	ref := testVP8Frame(t, 16, 16, 128, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(t, 20)
	refRate := 23

	acct, ok := e.estimateInterResidualRDAccounting(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, refRate)
	if !ok {
		t.Fatalf("estimateInterResidualRDAccounting returned ok=false")
	}
	if !acct.mbSkipCoeff || acct.rdLoopSkip {
		t.Fatalf("mbSkipCoeff/rdLoopSkip = %t/%t, want true/false", acct.mbSkipCoeff, acct.rdLoopSkip)
	}
	modeRate := e.interMotionModeRateWithReferenceRate(&mode, nil, nil, nil, 0, 0, 1, 1, refRate)
	wantRate2 := modeRate + e.interMacroblockSkipRate(true)
	if acct.rate2 != wantRate2 || acct.rateUV != 0 || acct.otherCost != e.interMacroblockSkipRate(true) {
		t.Fatalf("skip accounting rate2/rateUV/other = %d/%d/%d, want %d/0/%d", acct.rate2, acct.rateUV, acct.otherCost, wantRate2, e.interMacroblockSkipRate(true))
	}
	if wantRD := rdModeScoreWithZbin(20, e.rc.currentZbinOverQuant, wantRate2, acct.distortion2); acct.rd != wantRD {
		t.Fatalf("skip accounting rd = %d, want %d", acct.rd, wantRD)
	}
}

func TestEstimateInterIntraModeRDScoreAddsLibvpxPenalty(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	quant := testRegularMacroblockQuant(t, 20)

	_, got, gotYRD, ok := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, vp8common.DCPred, maxInt(), nil, nil, &quant)
	if !ok {
		t.Fatalf("estimateInterIntraModeRDScore returned ok=false")
	}

	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	decMode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, 0, 0, &decMode, &e.reconstructScratch) {
		t.Fatalf("predictAnalysisMacroblock returned false")
	}
	yRate, yDist := wholeBlockYTransformRD(sourceImageFromPublic(src), &e.analysis.Img, 0, 0, 20, 0, nil, nil, &quant, &e.coefProbs, false)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRD mode=%v ok=false", uvMode)
	}
	rate := yRate + uvRate + intraYModeRate(false, vp8common.DCPred) + e.interIntraMacroblockModeRate()
	want := rdModeScoreWithZbin(20, 0, rate, yDist+uvDist) + libvpxInterIntraRDPenalty(20)
	if got != want {
		t.Fatalf("inter-intra RD score = %d, want %d with libvpx penalty", got, want)
	}
	wantYRD := rdModeScoreWithZbin(20, 0, yRate+intraYModeRate(false, vp8common.DCPred), yDist)
	if gotYRD != wantYRD {
		t.Fatalf("inter-intra YRD = %d, want libvpx Y-only RD %d", gotYRD, wantYRD)
	}
}

func TestEstimateInterIntraBPredYRDExcludesUVAndRefCosts(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	quant := testRegularMacroblockQuant(t, 20)

	_, got, gotYRD, ok := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, vp8common.BPred, maxInt(), nil, nil, &quant)
	if !ok {
		t.Fatalf("estimateInterIntraModeRDScore BPred returned ok=false")
	}

	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, maxInt(), &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD returned ok=false")
	}
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRD mode=%v bModes=%v ok=false", uvMode, bModes)
	}
	yRate := bRate + intraYModeRate(false, vp8common.BPred)
	wantYRD := rdModeScoreWithZbin(20, 0, yRate, bDist)
	if gotYRD != wantYRD {
		t.Fatalf("BPred YRD = %d, want libvpx Y-only RD %d", gotYRD, wantYRD)
	}
	rate := yRate + uvRate + e.interIntraMacroblockModeRate()
	want := rdModeScoreWithZbin(20, 0, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(20)
	if got != want {
		t.Fatalf("BPred RD score = %d, want %d with UV/ref costs and penalty", got, want)
	}
}

func TestFastZeroMVLastRDAdjustmentMirrorsLibvpxLocalMotionBias(t *testing.T) {
	zero := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	moving := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	intra := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}

	if got := fastZeroMVLastRDAdjustment(0, 2, nil, &zero, nil); got != 80 {
		t.Fatalf("edge adjustment = %d, want 80", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, &zero, &moving, &intra); got != 90 {
		t.Fatalf("single local zero adjustment = %d, want 90", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, &zero, &zero, &zero); got != 80 {
		t.Fatalf("three local zero adjustment = %d, want 80", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, nil, &moving, &intra); got != 100 {
		t.Fatalf("moving adjustment = %d, want 100", got)
	}
}

func TestMBSplitPartitionRateMirrorsWriterBranches(t *testing.T) {
	tests := []struct {
		partition uint8
		want      int
	}{
		{partition: 3, want: boolBitCost(vp8tables.MBSplitProbs[0], 0)},
		{partition: 2, want: boolBitCost(vp8tables.MBSplitProbs[0], 1) + boolBitCost(vp8tables.MBSplitProbs[1], 0)},
		{partition: 0, want: boolBitCost(vp8tables.MBSplitProbs[0], 1) + boolBitCost(vp8tables.MBSplitProbs[1], 1) + boolBitCost(vp8tables.MBSplitProbs[2], 0)},
		{partition: 1, want: boolBitCost(vp8tables.MBSplitProbs[0], 1) + boolBitCost(vp8tables.MBSplitProbs[1], 1) + boolBitCost(vp8tables.MBSplitProbs[2], 1)},
	}
	for _, tt := range tests {
		if got := mbSplitPartitionRate(tt.partition); got != tt.want {
			t.Fatalf("partition %d rate = %d, want %d", tt.partition, got, tt.want)
		}
	}
}

func TestSplitMotionModeVectorCostChargesPartitionAndNew4x4Weight(t *testing.T) {
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 2,
	}
	fillInterFrameSplitSubset(&mode, 0, vp8enc.MotionVector{Col: 16})
	fillInterFrameSplitSubset(&mode, 1, vp8enc.MotionVector{Row: 16})
	fillInterFrameSplitSubset(&mode, 2, vp8enc.MotionVector{Col: -16})
	fillInterFrameSplitSubset(&mode, 3, vp8enc.MotionVector{Row: -16})

	mvProbs := vp8tables.DefaultMVContext
	best := vp8enc.MotionVector{Col: 8}
	want := mbSplitPartitionRate(mode.Partition)
	partitions := int(vp8tables.MBSplitCount[mode.Partition])
	for subset := 0; subset < partitions; subset++ {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		leftMV := analysisSplitLeftMV(&mode, nil, block)
		aboveMV := analysisSplitAboveMV(&mode, nil, block)
		target := mode.BlockMV[block]
		probs := analysisSubMVRefProbs(leftMV, aboveMV)
		want += boolBitCost(probs[0], 1)
		want += boolBitCost(probs[1], 1)
		want += boolBitCost(probs[2], 1)
		delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
		want += vp8enc.MotionVectorBitCost(delta, vp8enc.MotionVector{}, &mvProbs, 102)
	}

	defaultCost := splitMotionModeVectorCost(&mode, nil, nil, best, &mvProbs)
	if defaultCost != want {
		t.Fatalf("split vector cost = %d, want partition + NEW4X4 weight-102 cost %d", defaultCost, want)
	}

	liveProbs := mvProbs
	liveProbs[1][0] = 1
	if liveCost := splitMotionModeVectorCost(&mode, nil, nil, best, &liveProbs); liveCost == defaultCost {
		t.Fatalf("live split vector cost = default cost %d, want MV probs to affect SPLITMV sub-vector cost", liveCost)
	}
}

func TestSplitMotionModeVectorCostUsesExplicitSubMVLabel(t *testing.T) {
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 0,
	}
	fillInterFrameSplitSubsetWithMode(&mode, 0, left.MV, vp8common.New4x4)
	fillInterFrameSplitSubsetWithMode(&mode, 1, left.MV, vp8common.Left4x4)
	mode.MV = mode.BlockMV[15]
	mvProbs := vp8tables.DefaultMVContext

	newCost := splitMotionModeVectorCost(&mode, &left, nil, vp8enc.MotionVector{}, &mvProbs)
	mode.BModes[0] = vp8common.Left4x4
	leftCost := splitMotionModeVectorCost(&mode, &left, nil, vp8enc.MotionVector{}, &mvProbs)

	if newCost <= leftCost {
		t.Fatalf("explicit NEW4X4 cost = %d, want greater than LEFT4X4 cost %d for same MV", newCost, leftCost)
	}
}

func TestSplitSubMotionLabelSearchCostUsesAnalysisContext(t *testing.T) {
	left := vp8enc.MotionVector{Col: 8}
	above := vp8enc.MotionVector{}
	const qIndex = 127

	got := splitSubMotionLabelSearchCostWithContext(vp8common.Above4x4, left, above, qIndex)
	wantRate := splitSubMotionLabelRate(vp8common.Above4x4, left, above)
	want := (wantRate*libvpxSADPerBit4(qIndex) + 128) >> 8
	if got != want {
		t.Fatalf("contextual ABOVE4X4 search cost = %d, want %d", got, want)
	}
	if got == splitSubMotionLabelSearchCost(vp8common.Above4x4, qIndex) {
		t.Fatalf("contextual ABOVE4X4 search cost matched default cost %d; want left/above context to affect SplitMV search", got)
	}
}

func TestSelectInterFrameSplitSubsetMotionModeRanksLabelsByRD(t *testing.T) {
	leftMV := vp8enc.MotionVector{Col: 32}
	aboveMV := leftMV
	leftRate := splitSubMotionLabelRate(vp8common.Left4x4, leftMV, aboveMV)
	zeroRate := splitSubMotionLabelRate(vp8common.Zero4x4, leftMV, aboveMV)
	qIndex, leftDiff, zeroDiff := -1, -1, -1
	for q := 0; q < vp8common.QIndexRange && leftDiff < 0; q++ {
		sadPerBit := libvpxSADPerBit4(q)
		for ld := 0; ld <= 127 && leftDiff < 0; ld++ {
			for zd := 0; zd <= 127; zd++ {
				leftSAD := ld * 16
				zeroSAD := zd * 16
				oldLeft := leftSAD + ((leftRate*sadPerBit + 128) >> 8)
				oldZero := zeroSAD + ((zeroRate*sadPerBit + 128) >> 8)
				rdLeft := splitMotionLabelRDScore(q, leftRate, leftSAD)
				rdZero := splitMotionLabelRDScore(q, zeroRate, zeroSAD)
				if oldLeft < oldZero && rdZero < rdLeft {
					qIndex, leftDiff, zeroDiff = q, ld, zd
					break
				}
			}
		}
	}
	if leftDiff < 0 {
		t.Fatalf("failed to find split-label fixture with divergent SAD+rate and RDCOST ordering (leftRate=%d zeroRate=%d)", leftRate, zeroRate)
	}

	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	ref := testVP8Frame(t, 32, 32, 255, 128, 128)
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte(128 + zeroDiff)
			ref.Img.Y[row*ref.Img.YStride+col+4] = byte(128 + leftDiff)
		}
	}
	ref.ExtendBorders()

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 3,
	}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV, Partition: 3}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV, Partition: 3}
	for block := 0; block < 16; block++ {
		left.BlockMV[block] = leftMV
		above.BlockMV[block] = aboveMV
	}
	width, height := splitMotionPartitionBlockSize(int(mode.Partition))
	mv, bMode := selectInterFrameSplitSubsetMotionModeWithSearchAndThreshold(
		sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, 0, width, height,
		vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, false, qIndex,
		&left, &above, defaultInterAnalysisSearchConfig(), &vp8tables.DefaultMVContext, maxInt(),
	)
	if bMode != vp8common.Zero4x4 || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("split-label choice = %v/%+v, want ZERO4X4 by RDCOST ordering (leftDiff=%d zeroDiff=%d)", bMode, mv, leftDiff, zeroDiff)
	}
}

// TestInterReferenceFrameRateUsesLivePrevFrameProbs locks in libvpx parity for
// vp8_calc_ref_frame_costs: ref-frame selection bits are charged against the
// previous frame's prob_last_coded / prob_gf_coded, not a static 128 prior.
func TestInterReferenceFrameRateUsesLivePrevFrameProbs(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 50, refProbLast: 200, refProbGolden: 90}
	if got, want := e.interReferenceFrameRate(vp8common.LastFrame), boolBitCost(200, 0); got != want {
		t.Fatalf("LAST rate = %d, want %d", got, want)
	}
	if got, want := e.interReferenceFrameRate(vp8common.GoldenFrame), boolBitCost(200, 1)+boolBitCost(90, 0); got != want {
		t.Fatalf("GOLDEN rate = %d, want %d", got, want)
	}
	if got, want := e.interReferenceFrameRate(vp8common.AltRefFrame), boolBitCost(200, 1)+boolBitCost(90, 1); got != want {
		t.Fatalf("ALTREF rate = %d, want %d", got, want)
	}
}

func TestInterReferenceFrameRatesForFlagsMirrorLibvpxSingleReferenceSpecialCases(t *testing.T) {
	e := &VP8Encoder{refProbLast: 200, refProbGolden: 90}
	last, golden, alt := e.interReferenceFrameRatesForFlags(0)
	if want := boolBitCost(200, 0); last != want {
		t.Fatalf("all-ref LAST rate = %d, want %d", last, want)
	}
	if want := boolBitCost(200, 1) + boolBitCost(90, 0); golden != want {
		t.Fatalf("all-ref GOLDEN rate = %d, want %d", golden, want)
	}
	if want := boolBitCost(200, 1) + boolBitCost(90, 1); alt != want {
		t.Fatalf("all-ref ALTREF rate = %d, want %d", alt, want)
	}

	last, _, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceGolden | EncodeNoReferenceAltRef)
	if want := boolBitCost(255, 0); last != want {
		t.Fatalf("single-LAST rate = %d, want libvpx special-case %d", last, want)
	}

	e.opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringOneLayer}
	_, golden, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceAltRef)
	if want := boolBitCost(200, 1) + boolBitCost(90, 0); golden != want {
		t.Fatalf("one-layer single-GOLDEN rate = %d, want non-temporal live cost %d", golden, want)
	}

	e.opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}
	_, golden, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceAltRef)
	if want := boolBitCost(1, 1) + boolBitCost(255, 0); golden != want {
		t.Fatalf("temporal single-GOLDEN rate = %d, want libvpx special-case %d", golden, want)
	}
	_, _, alt = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceGolden)
	if want := boolBitCost(1, 1) + boolBitCost(1, 1); alt != want {
		t.Fatalf("temporal single-ALTREF rate = %d, want libvpx special-case %d", alt, want)
	}
}

func TestInterAnalysisReferencesCarryLibvpxFlagSpecificReferenceRates(t *testing.T) {
	e := &VP8Encoder{refProbLast: 200, refProbGolden: 90}
	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(EncodeNoReferenceGolden|EncodeNoReferenceAltRef, &refs)
	if count != 1 || refs[0].Frame != vp8common.LastFrame || !refs[0].RefRateSet {
		t.Fatalf("single-LAST refs = count:%d ref:%+v, want one LAST with explicit rate", count, refs[0])
	}
	if want := boolBitCost(255, 0); refs[0].RefRate != want {
		t.Fatalf("single-LAST carried rate = %d, want %d", refs[0].RefRate, want)
	}
}

func TestInterAnalysisReferencesPruneLibvpxAliasFlagsAfterKeyFrame(t *testing.T) {
	e := newTestEncoder(t)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	e.refreshKeyFrameReferencesFromAnalysis()
	e.frameCount = 1

	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(0, &refs)
	if count != 1 || refs[0].Frame != vp8common.LastFrame {
		t.Fatalf("post-key refs = count:%d first:%+v, want only LAST after libvpx alias pruning", count, refs[0])
	}
	if want := boolBitCost(255, 0); refs[0].RefRate != want {
		t.Fatalf("post-key LAST rate = %d, want single-reference libvpx cost %d", refs[0].RefRate, want)
	}
	if !e.shouldEncodeKeyFrame(EncodeNoReferenceLast) {
		t.Fatalf("shouldEncodeKeyFrame with LAST disabled = false, want keyframe when aliased GOLDEN/ALTREF are unavailable")
	}
}

func TestInterAnalysisReferencesKeepAltAfterInternalGoldenRefreshCopiesOldGF(t *testing.T) {
	e := newTestEncoder(t)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	e.refreshKeyFrameReferencesFromAnalysis()
	e.updateInterReferenceAliases(vp8enc.InterFrameStateConfig{
		RefreshLast:        true,
		RefreshGolden:      true,
		CopyBufferToAltRef: 2,
	})

	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(0, &refs)
	if count != 2 || refs[0].Frame != vp8common.LastFrame || refs[1].Frame != vp8common.AltRefFrame {
		t.Fatalf("post-GF-refresh refs = count:%d refs:%+v/%+v, want LAST and old-GF ALTREF", count, refs[0], refs[1])
	}
}

func TestRdBlockScoreAppliesLibvpxPlaneAndIntraMultipliers(t *testing.T) {
	if got := rdBlockScore(40, 4, false, 100, 20); got != 79 {
		t.Fatalf("inter block rd = %d, want 79", got)
	}
	if got := rdBlockScore(40, 4, true, 100, 20); got != 53 {
		t.Fatalf("intra block rd = %d, want 53", got)
	}
}

func TestStaticInterRDEncodeBreakoutUsesStrictLibvpxThreshold(t *testing.T) {
	pred := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	quant := testMacroblockQuant(20)

	src.Y[0] = 133
	if !staticInterRDEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want skip below AC threshold")
	}

	src.Y[0] = 134
	if staticInterRDEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want no skip at strict AC threshold")
	}
}

func TestStaticInterRDEncodeBreakoutUsesChromaGate(t *testing.T) {
	pred := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 129, 90, 170)
	quant := testMacroblockQuant(80)

	if !staticInterRDEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want uniform low-luma residual skipped")
	}

	src.U[0] = 110
	if staticInterRDEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want chroma SSE to prevent skip")
	}
}

func TestStaticInterFastEncodeBreakoutUsesPickinterChromaGate(t *testing.T) {
	ref := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	quant := testMacroblockQuant(80)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	_, lumaSSE := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &ref.Img, 0, 0, mode.MV)

	src.U[0] = 92
	if !staticInterRDEncodeBreakout(sourceImageFromPublic(src), &ref.Img, 0, 0, &quant, 1) {
		t.Fatalf("RD static breakout = false, want threshold-based chroma gate to skip")
	}
	if staticInterFastEncodeBreakout(sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, &quant, 1, lumaSSE) {
		t.Fatalf("fast static breakout = true, want pickinter encode_breakout chroma gate to reject")
	}
}

func TestSelectFastInterFrameModeDecisionStopsOnStaticEncodeBreakout(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineRealtime); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.opts.StaticThreshold = 1
	e.refProbIntra = 1
	e.refProbLast = 1
	e.refProbGolden = 1
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, 16, 16, 128, 90, 170)
	golden := testVP8Frame(t, 16, 16, 30, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img, RefRateSet: true, RefRate: 1 << 20},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img, RefRateSet: true, RefRate: 0},
	}
	quant := testRegularMacroblockQuant(t, 20)

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, 20, 0, nil, nil, nil, &quant)
	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if decision.useIntra || decision.interMode.RefFrame != vp8common.LastFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want LAST/ZEROMV static breakout candidate", decision)
	}
	if !decision.interMode.MBSkipCoeff {
		t.Fatalf("fast decision MBSkipCoeff = false, want candidate-level static breakout skip")
	}
	if e.interModeTestHitCounts[libvpxThrDC] != 0 {
		t.Fatalf("DC mode hit count = %d, want fast loop to break after static breakout", e.interModeTestHitCounts[libvpxThrDC])
	}
}

func TestBuildReconstructingInterFrameCoefficientsUsesStaticEncodeBreakout(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	src.Y[0] = 160

	noBreakout := newSizedTestEncoder(t, 16, 16)
	if err := noBreakout.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	fillBenchmarkVP8Image(&noBreakout.lastRef.Img, 128, 90, 170)
	noBreakout.lastRef.ExtendBorders()
	noBreakoutModes := make([]vp8enc.InterFrameMacroblockMode, 1)
	noBreakoutCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := noBreakout.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), 20, noBreakoutModes, noBreakoutCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("no-breakout inter reconstruction returned error: %v", err)
	}
	if noBreakoutModes[0].MBSkipCoeff || macroblockCoeffAbsSum(&noBreakoutCoeffs[0]) == 0 {
		t.Fatalf("no-breakout mode skip=%t coeff sum=%d, want coded residual", noBreakoutModes[0].MBSkipCoeff, macroblockCoeffAbsSum(&noBreakoutCoeffs[0]))
	}

	breakout := newSizedTestEncoder(t, 16, 16)
	if err := breakout.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	breakout.opts.StaticThreshold = 7000
	fillBenchmarkVP8Image(&breakout.lastRef.Img, 128, 90, 170)
	breakout.lastRef.ExtendBorders()
	breakoutModes := make([]vp8enc.InterFrameMacroblockMode, 1)
	breakoutCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := breakout.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), 20, breakoutModes, breakoutCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("breakout inter reconstruction returned error: %v", err)
	}
	if !breakoutModes[0].MBSkipCoeff || macroblockCoeffAbsSum(&breakoutCoeffs[0]) != 0 {
		t.Fatalf("breakout mode skip=%t coeff sum=%d, want forced skip", breakoutModes[0].MBSkipCoeff, macroblockCoeffAbsSum(&breakoutCoeffs[0]))
	}
}

func TestMacroblockCoefficientTokenRateChargesNonZeroResiduals(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero vp8enc.MacroblockCoefficients
	zeroRate := macroblockCoefficientTokenRate(&probs, false, &zero)

	nonzero := zero
	nonzero.QCoeff[24][0] = 2
	nonzero.SetBlockEOB(24, 1)
	nonzero.QCoeff[0][1] = -1
	nonzero.SetBlockEOB(0, 2)
	nonzero.QCoeff[16][0] = 1
	nonzero.SetBlockEOB(16, 1)
	nonzeroRate := macroblockCoefficientTokenRate(&probs, false, &nonzero)

	if zeroRate <= 0 {
		t.Fatalf("zero residual token rate = %d, want positive EOB signalling cost", zeroRate)
	}
	if nonzeroRate <= zeroRate {
		t.Fatalf("nonzero residual token rate = %d, zero = %d, want higher rate", nonzeroRate, zeroRate)
	}

	clearMacroblockCoefficients(&nonzero)
	if clearedRate := macroblockCoefficientTokenRate(&probs, false, &nonzero); clearedRate != zeroRate {
		t.Fatalf("cleared residual rate = %d, want zero residual rate %d", clearedRate, zeroRate)
	}
}

func TestOptimizeQuantizedBlockDropsTrailingCoefficientWhenRateWins(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 9
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 2)

	if eob != 1 || qcoeff[1] != 0 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want trailing coefficient dropped", eob, qcoeff[1])
	}
}

func TestOptimizeQuantizedBlockUsesProvidedCoefficientProbs(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 9
	qcoeff[1] = 1

	defaultQ := qcoeff
	defaultEOB := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &defaultQ, 2)
	if defaultEOB != 1 || defaultQ[1] != 0 {
		t.Fatalf("default optimized eob/qcoeff = %d/%d, want trailing coefficient dropped", defaultEOB, defaultQ[1])
	}

	liveProbs := vp8tables.DefaultCoefProbs
	liveProbs[0][1][0][0] = 1
	liveProbs[0][1][0][1] = 1
	liveProbs[0][1][0][2] = 255
	nextBand := vp8tables.CoefBandsTable[2]
	nextCtx := vp8tables.PrevTokenClass[vp8tables.OneToken]
	liveProbs[0][nextBand][nextCtx][0] = 255

	liveQ := qcoeff
	liveEOB := optimizeQuantizedBlock(&liveProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &liveQ, 2)
	if liveEOB != 2 || liveQ[1] != 1 {
		t.Fatalf("live-prob optimized eob/qcoeff = %d/%d, want coefficient preserved", liveEOB, liveQ[1])
	}
}

func TestOptimizeQuantizedBlockKeepsCoefficientWhenDistortionDominates(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 100
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 100
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 4, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 2)

	if eob != 2 || qcoeff[1] != 1 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want coefficient preserved", eob, qcoeff[1])
	}
}

func TestOptimizeQuantizedBlockKeepsUndershootCoefficient(t *testing.T) {
	// Undershoot |x|*dq=10 < |c|=11: the libvpx Viterbi only considers the
	// shift-toward-zero shortcut for overshoots inside one quant step, so the
	// trailing coefficient must stay even though the greedy optimizer would
	// drop it.
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 11
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 2)

	if eob != 2 || qcoeff[1] != 1 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want libvpx Viterbi to keep undershoot coefficient", eob, qcoeff[1])
	}
}

func TestOptimizeQuantizedBlockShortensTrailingZerosWithInteriorRetained(t *testing.T) {
	// First non-zero overshoots inside one quant step (Viterbi keeps it via the
	// shortcut), trailing non-zero overshoots and is rate-dominated, so the
	// trellis drops only the trailing coefficient and pulls EOB back.
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	rc1 := int(vp8tables.DefaultZigZag1D[1])
	rc2 := int(vp8tables.DefaultZigZag1D[2])
	coeff[rc1] = 60
	qcoeff[rc1] = 6
	coeff[rc2] = 9
	qcoeff[rc2] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 3)

	if eob != 2 || qcoeff[rc1] != 6 || qcoeff[rc2] != 0 {
		t.Fatalf("trellis output eob/q1/q2 = %d/%d/%d, want trailing dropped while interior retained", eob, qcoeff[rc1], qcoeff[rc2])
	}
}

func TestQuantizeBlockWithZbinUsesZeroRunBoost(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	boostedRC := int(vp8tables.DefaultZigZag1D[7])
	coeff[boostedRC] = 75

	eob := quantizeBlockWithZbin(&coeff, &quant, 80, 0, 0, &qcoeff, &dqcoeff)

	if eob != 0 || qcoeff[boostedRC] != 0 || dqcoeff[boostedRC] != 0 {
		t.Fatalf("boosted coefficient eob/q/dq = %d/%d/%d, want suppressed", eob, qcoeff[boostedRC], dqcoeff[boostedRC])
	}

	coeff = [16]int16{}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	earlyRC := int(vp8tables.DefaultZigZag1D[1])
	coeff[earlyRC] = 80
	eob = quantizeBlockWithZbin(&coeff, &quant, 80, 0, 0, &qcoeff, &dqcoeff)

	if eob != 2 || qcoeff[earlyRC] == 0 || dqcoeff[earlyRC] == 0 {
		t.Fatalf("early coefficient eob/q/dq = %d/%d/%d, want quantized", eob, qcoeff[earlyRC], dqcoeff[earlyRC])
	}
}

func TestQuantizeBlockWithZbinUsesModeBoost(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 66

	if eob := quantizeBlockWithZbin(&coeff, &quant, 80, 0, 0, &qcoeff, &dqcoeff); eob != 2 || qcoeff[rc] == 0 {
		t.Fatalf("unboosted eob/q = %d/%d, want coefficient quantized", eob, qcoeff[rc])
	}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	if eob := quantizeBlockWithZbin(&coeff, &quant, 80, 0, lastFrameZeroMVZbinBoost, &qcoeff, &dqcoeff); eob != 0 || qcoeff[rc] != 0 {
		t.Fatalf("boosted eob/q = %d/%d, want coefficient suppressed", eob, qcoeff[rc])
	}
}

func TestQuantizeBlockWithZbinUsesOverQuant(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	zbinOverQuant := 128
	extra := (int(quant.Dequant[1]) * zbinOverQuant) >> 7
	coeff[rc] = int16(int(quant.Zbin[rc]) + int(quant.ZbinBoost[0]) + extra/2)

	if eob := quantizeBlockWithZbin(&coeff, &quant, 80, 0, 0, &qcoeff, &dqcoeff); eob != 2 || qcoeff[rc] == 0 {
		t.Fatalf("unboosted eob/q = %d/%d, want coefficient quantized", eob, qcoeff[rc])
	}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	if eob := quantizeBlockWithZbin(&coeff, &quant, 80, zbinOverQuant, 0, &qcoeff, &dqcoeff); eob != 0 || qcoeff[rc] != 0 || dqcoeff[rc] != 0 {
		t.Fatalf("over-quant eob/q/dq = %d/%d/%d, want coefficient suppressed", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func TestQuantizeOptimizedBlockUpdatesDequantizedCoefficients(t *testing.T) {
	quant := testRegularBlockQuant(127, 10)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	// Overshoot inside one quant step: |x|*dq=10 > |c|=9 < |c|+dq=19, so the
	// libvpx Viterbi trellis explores the shift-toward-zero shortcut.
	coeff[rc] = 9

	eob := quantizeOptimizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, &coeff, &quant, &qcoeff, &dqcoeff)

	if eob != 1 || qcoeff[rc] != 0 || dqcoeff[rc] != 0 {
		t.Fatalf("optimized eob/q/dq = %d/%d/%d, want trailing coefficient dropped and dequantized", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func TestQuantizeOptimizedBlockKeepsDequantizedCoefficient(t *testing.T) {
	quant := testRegularBlockQuant(4, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 100

	eob := quantizeOptimizedBlock(&vp8tables.DefaultCoefProbs, 4, 0, 0, 1, 0, 0, false, &coeff, &quant, &qcoeff, &dqcoeff)

	if eob != 2 || qcoeff[rc] != 1 || dqcoeff[rc] != 100 {
		t.Fatalf("optimized eob/q/dq = %d/%d/%d, want coefficient kept and dequantized", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func TestQuantizeEncodedBlockHonorsOptimizeGate(t *testing.T) {
	quant := testRegularBlockQuant(127, 10)
	var coeff [16]int16
	var optimizedQ [16]int16
	var optimizedDQ [16]int16
	var plainQ [16]int16
	var plainDQ [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	// Overshoot inside one quant step (|x|*dq=10 > |c|=9 < |c|+dq=19): the
	// optimize path's libvpx Viterbi shortcut picks x=0; the plain path keeps
	// the unoptimized x=1.
	coeff[rc] = 9

	optimizedEOB := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, false, true, &coeff, &quant, &optimizedQ, &optimizedDQ)
	plainEOB := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, false, false, &coeff, &quant, &plainQ, &plainDQ)

	if optimizedEOB != 1 || optimizedQ[rc] != 0 || optimizedDQ[rc] != 0 {
		t.Fatalf("optimized encoding eob/q/dq = %d/%d/%d, want dropped coefficient", optimizedEOB, optimizedQ[rc], optimizedDQ[rc])
	}
	if plainEOB != 2 || plainQ[rc] != 1 || plainDQ[rc] != 10 {
		t.Fatalf("plain encoding eob/q/dq = %d/%d/%d, want unoptimized quantized coefficient", plainEOB, plainQ[rc], plainDQ[rc])
	}
}

func TestQuantizeEncodedBlockUsesFastQuantWhenSpeedFeatureRequestsIt(t *testing.T) {
	quant := testRegularBlockQuant(4, 100)
	var coeff [16]int16
	var regularQ [16]int16
	var regularDQ [16]int16
	var fastQ [16]int16
	var fastDQ [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 64

	regularEOB := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, false, false, &coeff, &quant, &regularQ, &regularDQ)
	fastEOB := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, true, false, &coeff, &quant, &fastQ, &fastDQ)

	if regularEOB != 0 || regularQ[rc] != 0 || regularDQ[rc] != 0 {
		t.Fatalf("regular encoding eob/q/dq = %d/%d/%d, want zbin-suppressed coefficient", regularEOB, regularQ[rc], regularDQ[rc])
	}
	if fastEOB != 2 || fastQ[rc] != 1 || fastDQ[rc] != 100 {
		t.Fatalf("fast encoding eob/q/dq = %d/%d/%d, want fast-quantized coefficient", fastEOB, fastQ[rc], fastDQ[rc])
	}
}

func TestResetLibvpxSmallSecondOrderCoefficientsClearsTinyY2(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var qcoeff [16]int16
	var dqcoeff [16]int16
	qcoeff[0] = 3
	dqcoeff[0] = 30

	eob := resetLibvpxSmallSecondOrderCoefficients(&quant, &qcoeff, &dqcoeff, 1)

	if eob != 0 || qcoeff[0] != 0 || dqcoeff[0] != 0 {
		t.Fatalf("small Y2 reset = eob:%d q:%d dq:%d, want cleared", eob, qcoeff[0], dqcoeff[0])
	}
}

func TestResetLibvpxSmallSecondOrderCoefficientsKeepsVisibleY2(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var qcoeff [16]int16
	var dqcoeff [16]int16
	qcoeff[0] = 4
	dqcoeff[0] = 40

	eob := resetLibvpxSmallSecondOrderCoefficients(&quant, &qcoeff, &dqcoeff, 1)

	if eob != 1 || qcoeff[0] != 4 || dqcoeff[0] != 40 {
		t.Fatalf("visible Y2 reset = eob:%d q:%d dq:%d, want preserved", eob, qcoeff[0], dqcoeff[0])
	}
}

func TestResetLibvpxSmallSecondOrderCoefficientsHonorsDequantGuard(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 35
	}
	var qcoeff [16]int16
	qcoeff[0] = 1

	eob := resetLibvpxSmallSecondOrderCoefficients(&quant, &qcoeff, nil, 1)

	if eob != 1 || qcoeff[0] != 1 {
		t.Fatalf("guarded Y2 reset = eob:%d q:%d, want preserved when dequant >= 35", eob, qcoeff[0])
	}
}

func testRegularBlockQuant(qIndex int, dequantValue int16) vp8enc.BlockQuant {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = dequantValue
	}
	var quant vp8enc.BlockQuant
	vp8enc.InitRegularBlockQuant(qIndex, &dequant, &quant)
	return quant
}

func TestInterZbinModeBoostMatchesLibvpxClasses(t *testing.T) {
	tests := []struct {
		name string
		mode vp8enc.InterFrameMacroblockMode
		want int
	}{
		{name: "last zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}, want: lastFrameZeroMVZbinBoost},
		{name: "golden zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV}, want: goldenAltZeroMVZbinBoost},
		{name: "alt zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.AltRefFrame, Mode: vp8common.ZeroMV}, want: goldenAltZeroMVZbinBoost},
		{name: "newmv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV}, want: nonZeroInterModeZbinBoost},
		{name: "splitmv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV}, want: splitInterModeZbinBoost},
		{name: "intra", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}, want: intraInterFrameZbinBoost},
	}
	for _, tt := range tests {
		if got := interZbinModeBoost(&tt.mode); got != tt.want {
			t.Fatalf("%s boost = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestEncoderSegmentQIndex(t *testing.T) {
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateData: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][1] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = -10
	if got := encoderSegmentQIndex(20, segmentation, 1); got != 10 {
		t.Fatalf("delta segment q = %d, want 10", got)
	}
	if got := encoderSegmentQIndex(4, segmentation, 1); got != vp8common.MinQ {
		t.Fatalf("clamped delta segment q = %d, want MinQ", got)
	}
	segmentation.AbsDelta = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = 63
	if got := encoderSegmentQIndex(20, segmentation, 1); got != 63 {
		t.Fatalf("absolute segment q = %d, want 63", got)
	}
	if got := encoderSegmentQIndex(20, segmentation, 2); got != 20 {
		t.Fatalf("disabled segment q = %d, want base q", got)
	}
}

func TestBuildReconstructingKeyFrameCoefficientsWithSegmentationQuantizesPerSegment(t *testing.T) {
	lowEncoder := newSizedTestEncoder(t, 32, 16)
	highEncoder := newSizedTestEncoder(t, 32, 16)
	src := segmentedQuantizationTestImage()
	lowModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	highModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	lowCoeffs := make([]vp8enc.MacroblockCoefficients, 2)
	highCoeffs := make([]vp8enc.MacroblockCoefficients, 2)

	lowSegmentation := testAltQSegmentation(1, 0)
	highSegmentation := testAltQSegmentation(1, 100)
	if err := lowEncoder.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, lowSegmentation, true, lowModes, lowCoeffs, 1, 2); err != nil {
		t.Fatalf("low-q keyframe reconstruction returned error: %v", err)
	}
	if err := highEncoder.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, highSegmentation, true, highModes, highCoeffs, 1, 2); err != nil {
		t.Fatalf("high-q keyframe reconstruction returned error: %v", err)
	}

	if lowModes[0].SegmentID != 0 || lowModes[1].SegmentID != 1 || highModes[0].SegmentID != 0 || highModes[1].SegmentID != 1 {
		t.Fatalf("segment IDs low=%d/%d high=%d/%d, want preserved 0/1", lowModes[0].SegmentID, lowModes[1].SegmentID, highModes[0].SegmentID, highModes[1].SegmentID)
	}
	if highEncoder.reconstructModes[1].SegmentID != 1 {
		t.Fatalf("decoder reconstruct segment ID = %d, want 1", highEncoder.reconstructModes[1].SegmentID)
	}
	if highEncoder.dequants[0].Y1[0] == highEncoder.dequants[1].Y1[0] {
		t.Fatalf("segment dequant Y1 DC = %d/%d, want segment-specific dequant", highEncoder.dequants[0].Y1[0], highEncoder.dequants[1].Y1[0])
	}

	lowSum := macroblockCoeffAbsSum(&lowCoeffs[1])
	highSum := macroblockCoeffAbsSum(&highCoeffs[1])
	if lowSum <= highSum {
		t.Fatalf("segment 1 coefficient abs sum low/high = %d/%d, want high segment q to quantize harder", lowSum, highSum)
	}
}

func TestBuildReconstructingInterFrameCoefficientsWithSegmentationPreservesSegmentDequants(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	fillBenchmarkVP8Image(&e.lastRef.Img, 128, 128, 128)
	for row := 0; row < 16; row++ {
		for col := 16; col < 32; col++ {
			if (row+col)&1 == 0 {
				e.lastRef.Img.Y[row*e.lastRef.Img.YStride+col] = 32
			} else {
				e.lastRef.Img.Y[row*e.lastRef.Img.YStride+col] = 224
			}
		}
	}
	e.lastRef.ExtendBorders()
	src := segmentedQuantizationTestImage()
	modes := []vp8enc.InterFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	coeffs := make([]vp8enc.MacroblockCoefficients, 2)
	segmentation := testAltQSegmentation(1, 100)

	if err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, segmentation, true, modes, coeffs, 1, 2, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("inter reconstruction returned error: %v", err)
	}

	if modes[0].SegmentID != 0 || modes[1].SegmentID != 1 {
		t.Fatalf("segment IDs = %d/%d, want preserved 0/1", modes[0].SegmentID, modes[1].SegmentID)
	}
	if e.reconstructModes[1].SegmentID != 1 {
		t.Fatalf("decoder reconstruct segment ID = %d, want 1", e.reconstructModes[1].SegmentID)
	}
	if e.dequants[0].Y1[0] == e.dequants[1].Y1[0] {
		t.Fatalf("segment dequant Y1 DC = %d/%d, want segment-specific dequant", e.dequants[0].Y1[0], e.dequants[1].Y1[0])
	}
	if got := macroblockCoeffAbsSum(&coeffs[1]); got == 0 {
		t.Fatalf("segment 1 coefficient abs sum = 0, want residual coefficients")
	}
}

func TestBuildReconstructingInterFrameCoefficientsWithSegmentationClearsCyclicSegmentForNonLastZero(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	copyFrameImage(&e.goldenRef.Img, &golden.Img)
	e.goldenRef.ExtendBorders()
	fillBenchmarkVP8Image(&e.lastRef.Img, 220, 90, 170)
	e.lastRef.ExtendBorders()

	modes := []vp8enc.InterFrameMacroblockMode{{SegmentID: staticSegmentID}}
	coeffs := make([]vp8enc.MacroblockCoefficients, 1)
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID] = -10

	err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(
		sourceImageFromPublic(src), 20, segmentation, true, modes, coeffs, 1, 1,
		EncodeNoReferenceLast|EncodeNoReferenceAltRef,
	)
	if err != nil {
		t.Fatalf("inter reconstruction returned error: %v", err)
	}
	if modes[0].RefFrame != vp8common.GoldenFrame || modes[0].Mode != vp8common.ZeroMV {
		t.Fatalf("mode = %+v, want GOLDEN/ZEROMV setup", modes[0])
	}
	if modes[0].SegmentID != 0 || e.reconstructModes[0].SegmentID != 0 {
		t.Fatalf("segment IDs = mode:%d reconstruct:%d, want cleared to 0 for non-LAST/ZEROMV", modes[0].SegmentID, e.reconstructModes[0].SegmentID)
	}
}

func TestBuildReconstructingCoefficientsWithSegmentationRejectsInvalidSegmentID(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	src := segmentedQuantizationTestImage()
	segmentation := testAltQSegmentation(1, 63)
	keyModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: vp8common.MaxMBSegments}}
	keyCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := e.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 20, segmentation, true, keyModes, keyCoeffs, 1, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("keyframe invalid segment error = %v, want ErrInvalidConfig", err)
	}

	interModes := []vp8enc.InterFrameMacroblockMode{{SegmentID: vp8common.MaxMBSegments}}
	interCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 20, segmentation, true, interModes, interCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("inter invalid segment error = %v, want ErrInvalidConfig", err)
	}
}

func copyBPredBlockToSource(block []byte, blockStride int, dst Image, mbRow int, mbCol int, blockIndex int) {
	baseY := mbRow*16 + (blockIndex>>2)*4
	baseX := mbCol*16 + (blockIndex&3)*4
	for row := 0; row < 4; row++ {
		copy(dst.Y[(baseY+row)*dst.YStride+baseX:], block[row*blockStride:row*blockStride+4])
	}
}

func testAltQSegmentation(segmentID uint8, qIndex int8) vp8enc.SegmentationConfig {
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true, AbsDelta: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][segmentID] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][segmentID] = qIndex
	return segmentation
}

func segmentedQuantizationTestImage() Image {
	img := testImage(32, 16)
	fillImage(img, 128, 128, 128)
	for row := 0; row < img.Height; row++ {
		for col := 16; col < img.Width; col++ {
			if (row+col)&1 == 0 {
				img.Y[row*img.YStride+col] = 16
			} else {
				img.Y[row*img.YStride+col] = 240
			}
		}
	}
	return img
}

func macroblockCoeffAbsSum(coeffs *vp8enc.MacroblockCoefficients) int {
	sum := 0
	for block := range coeffs.QCoeff {
		for _, coeff := range coeffs.QCoeff[block] {
			if coeff < 0 {
				sum -= int(coeff)
			} else {
				sum += int(coeff)
			}
		}
	}
	return sum
}

func BenchmarkMacroblockCoefficientsEmpty(b *testing.B) {
	var coeffs vp8enc.MacroblockCoefficients
	for block := 0; block < 16; block++ {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkBool = macroblockCoefficientsEmpty(&coeffs, false)
	}
}

func BenchmarkSelectInterFrameReferenceMotionVector(b *testing.B) {
	src := testImage(64, 64)
	for row := 0; row < src.Height; row++ {
		for col := 0; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = byte(32 + ((row + col) & 127))
		}
	}
	for i := range src.U {
		src.U[i] = 90
		src.V[i] = 170
	}
	last := testVP8Frame(b, 64, 64, 32, 90, 170)
	golden := testVP8Frame(b, 64, 64, 40, 90, 170)
	alt := testVP8Frame(b, 64, 64, 48, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)
	b.ReportAllocs()
	b.SetBytes(16 * 16 * int64(len(refs)) * int64(interFrameSubpixelSearchCandidateCount()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col, 4, 4, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	}
}

func BenchmarkSelectInterFrameReferenceMotionVectorZeroCost(b *testing.B) {
	src := testImage(64, 64)
	for row := 0; row < src.Height; row++ {
		for col := 0; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = byte(32 + ((row + col) & 127))
		}
	}
	for i := range src.U {
		src.U[i] = 90
		src.V[i] = 170
	}
	last := testVP8Frame(b, 64, 64, 0, 0, 0)
	copyPlane(last.Img.Y, last.Img.YStride, src.Y, src.YStride, src.Width, src.Height)
	copyPlane(last.Img.U, last.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1)
	copyPlane(last.Img.V, last.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1)
	last.ExtendBorders()
	golden := testVP8Frame(b, 64, 64, 40, 90, 170)
	alt := testVP8Frame(b, 64, 64, 48, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)
	b.ReportAllocs()
	b.SetBytes(16 * 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col, 4, 4, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	}
}

func BenchmarkMacroblockSubpixelSADLimit(b *testing.B) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(b, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_, _ = macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, 1024)
	}
}

func BenchmarkMacroblockSubpixelSADFull(b *testing.B) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(b, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_, _ = macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, maxInt())
	}
}

func sourceImageFromPublic(img Image) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Width:   img.Width,
		Height:  img.Height,
		Y:       img.Y,
		U:       img.U,
		V:       img.V,
		YStride: img.YStride,
		UStride: img.UStride,
		VStride: img.VStride,
	}
}

func testMacroblockQuant(qIndex int) vp8enc.MacroblockQuant {
	var tables vp8common.FrameDequantTables
	var dequant vp8common.MacroblockDequant
	var quant vp8enc.MacroblockQuant
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &tables)
	vp8common.InitMacroblockDequant(&tables, qIndex, &dequant)
	vp8enc.InitFastMacroblockQuant(&dequant, &quant)
	return quant
}

func testRegularMacroblockQuant(tb testing.TB, qIndex int) vp8enc.MacroblockQuant {
	tb.Helper()
	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, vp8common.QuantDeltas{}, vp8enc.SegmentationConfig{}, &quants); err != nil {
		tb.Fatalf("InitSegmentMacroblockQuants returned error: %v", err)
	}
	return quants[0]
}

func testVP8Frame(tb testing.TB, width int, height int, y byte, u byte, v byte) vp8common.FrameBuffer {
	tb.Helper()
	var frame vp8common.FrameBuffer
	if err := frame.Resize(width, height, 32, 32); err != nil {
		tb.Fatalf("Resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&frame.Img, y, u, v)
	frame.ExtendBorders()
	return frame
}

func fillBenchmarkVP8Image(img *vp8common.Image, y byte, u byte, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.U {
		img.U[i] = u
	}
	for i := range img.V {
		img.V[i] = v
	}
}

func copyShifted8x8FromReference(dst Image, ref *vp8common.Image, y int, x int, dy int, dx int) {
	copyShiftedBlockFromReference(dst, ref, y, x, 8, 8, dy, dx)
}

func splitMotionSourceAndReference(tb testing.TB) (Image, vp8common.FrameBuffer) {
	tb.Helper()
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	ref := testVP8Frame(tb, 32, 32, 0, 90, 170)
	for row := 0; row < 32; row++ {
		for col := 0; col < 32; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*17 + col*col*31 + row*col*7 + row*13 + col*29) & 255)
		}
	}
	return src, ref
}

func copyShiftedBlockFromReference(dst Image, ref *vp8common.Image, y int, x int, width int, height int, dy int, dx int) {
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			dst.Y[(y+row)*dst.YStride+x+col] = ref.Y[(y+row+dy)*ref.YStride+x+col+dx]
		}
	}
}

func TestCoefficientBlockTokenTraceMatchesAggregateRate(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	// Use a representative block: a couple of non-zero coefficients
	// at varying scan positions, plus an interior zero, with eob<16.
	var qcoeff [16]int16
	qcoeff[vp8tables.DefaultZigZag1D[0]] = 3
	qcoeff[vp8tables.DefaultZigZag1D[1]] = -1
	qcoeff[vp8tables.DefaultZigZag1D[3]] = 5
	const eob = 4

	wantTotal := coefficientBlockTokenRate(&probs, 3, 0, 0, &qcoeff, eob)
	trace, gotTotal := coefficientBlockTokenTrace(&probs, 3, 0, 0, &qcoeff, eob)
	if gotTotal != wantTotal {
		t.Fatalf("trace total = %d, want %d", gotTotal, wantTotal)
	}
	if len(trace) == 0 {
		t.Fatalf("trace empty, want entries for positions 0..%d", eob)
	}

	sum := 0
	for _, e := range trace {
		sum += e.TokenRate + e.SignRate + e.ExtraBits
	}
	if sum != wantTotal {
		t.Fatalf("sum of per-position rates = %d, want %d", sum, wantTotal)
	}
	// EOB transition recorded as the trailing entry since eob<16.
	last := trace[len(trace)-1]
	if last.Token != vp8tables.DCTEOBToken {
		t.Fatalf("trailing trace token = %d, want EOB %d", last.Token, vp8tables.DCTEOBToken)
	}
	if last.Position != eob {
		t.Fatalf("trailing trace position = %d, want %d", last.Position, eob)
	}
}

func TestCoefficientBlockTokenTraceAllZerosRecordsSingleEOB(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var qcoeff [16]int16

	wantTotal := coefficientBlockTokenRate(&probs, 3, 0, 0, &qcoeff, 0)
	trace, gotTotal := coefficientBlockTokenTrace(&probs, 3, 0, 0, &qcoeff, 0)
	if gotTotal != wantTotal {
		t.Fatalf("trace total = %d, want %d", gotTotal, wantTotal)
	}
	if len(trace) != 1 {
		t.Fatalf("trace length = %d, want 1 EOB entry", len(trace))
	}
	entry := trace[0]
	if entry.Position != 0 {
		t.Fatalf("eob entry position = %d, want 0", entry.Position)
	}
	if entry.Token != vp8tables.DCTEOBToken {
		t.Fatalf("eob entry token = %d, want EOB %d", entry.Token, vp8tables.DCTEOBToken)
	}
	if entry.Coefficient != 0 {
		t.Fatalf("eob entry coefficient = %d, want 0", entry.Coefficient)
	}
	if entry.SignRate != 0 || entry.ExtraBits != 0 {
		t.Fatalf("eob entry sign/extra = (%d,%d), want (0,0)", entry.SignRate, entry.ExtraBits)
	}
	if entry.TokenRate != wantTotal {
		t.Fatalf("eob entry rate = %d, want total %d", entry.TokenRate, wantTotal)
	}
}

func TestCoefficientBlockTokenTraceSingleNonZeroAtSkipDC(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	// skipDC=1 with a single non-zero at scan position 1 (eob=2): the trace
	// should contain the non-zero entry at position 1 followed by the EOB
	// entry at position 2.
	var qcoeff [16]int16
	qcoeff[vp8tables.DefaultZigZag1D[1]] = 1
	const skipDC = 1
	const eob = 2

	wantTotal := coefficientBlockTokenRate(&probs, 0, 0, skipDC, &qcoeff, eob)
	trace, gotTotal := coefficientBlockTokenTrace(&probs, 0, 0, skipDC, &qcoeff, eob)
	if gotTotal != wantTotal {
		t.Fatalf("trace total = %d, want %d", gotTotal, wantTotal)
	}
	if len(trace) != 2 {
		t.Fatalf("trace length = %d, want 2 (non-zero + EOB)", len(trace))
	}

	first := trace[0]
	if first.Position != skipDC {
		t.Fatalf("first entry position = %d, want %d", first.Position, skipDC)
	}
	if first.Coefficient != 1 {
		t.Fatalf("first entry coefficient = %d, want 1", first.Coefficient)
	}
	if first.Token != vp8tables.OneToken {
		t.Fatalf("first entry token = %d, want OneToken %d", first.Token, vp8tables.OneToken)
	}
	if first.SignRate != boolBitCost(128, 0) {
		t.Fatalf("first entry sign rate = %d, want %d", first.SignRate, boolBitCost(128, 0))
	}

	second := trace[1]
	if second.Position != skipDC+1 {
		t.Fatalf("second entry position = %d, want %d", second.Position, skipDC+1)
	}
	if second.Token != vp8tables.DCTEOBToken {
		t.Fatalf("second entry token = %d, want EOB %d", second.Token, vp8tables.DCTEOBToken)
	}
	if second.SignRate != 0 || second.ExtraBits != 0 {
		t.Fatalf("second entry sign/extra = (%d,%d), want (0,0)", second.SignRate, second.ExtraBits)
	}

	sum := 0
	for _, e := range trace {
		sum += e.TokenRate + e.SignRate + e.ExtraBits
	}
	if sum != wantTotal {
		t.Fatalf("sum of per-position rates = %d, want %d", sum, wantTotal)
	}
}

// TestSelectInterFrameSplitMotionDecisionRDAccountsForChromaResidual exercises
// the SPLITMV RD picker after the Y partition is committed. libvpx's
// vp8_rd_pick_inter_mode SPLITMV branch invokes rd_inter4x4_uv to add the
// chroma rate/distortion on top of vp8_rd_pick_best_mbsegmentation's Y RD;
// this test asserts our port does the same and stores per-4x4-block luma
// EOBs so packet writers can reuse the chosen partition's coefficients.
func TestSelectInterFrameSplitMotionDecisionRDAccountsForChromaResidual(t *testing.T) {
	const w, h = 32, 32
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
	// Vary luma so the partitioned MV search has a unique optimum, and vary
	// chroma so the derived 8x8 chroma MVs leave non-trivial UV residual
	// (rd_inter4x4_uv only contributes when vp8_mbuverror is non-zero).
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*11 + col*col*23 + row*col*5 + 7) & 255)
		}
	}
	uvWidth := (w + 1) >> 1
	uvHeight := (h + 1) >> 1
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			ref.Img.U[row*ref.Img.UStride+col] = byte((row*19 ^ col*13) & 255)
			ref.Img.V[row*ref.Img.VStride+col] = byte((row*7 + col*29 + 41) & 255)
		}
	}
	// Top 16x8 luma half: shift dx=1 (MV col=+8 in 1/8-pel units, MV(0,1)).
	// Bottom 16x8 luma half: identity (MV(0,0)). Apply a strong DC offset
	// to the source so the forward DCT lands above the inter zbin and the
	// per-block EOBs are populated — this is what we are asserting.
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 1)
	copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 0, 0)
	// Drop the source by a per-4x4-block DC offset so the forward DCT
	// concentrates energy at the DC coefficient that survives the inter
	// zbin and leaves a populated EOB on each block.
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			block := (row>>2)*4 + (col >> 2)
			delta := 60
			if block&1 == 0 {
				delta = -60
			}
			pixel := int(src.Y[row*src.YStride+col]) + delta
			if pixel < 0 {
				pixel = 0
			} else if pixel > 255 {
				pixel = 255
			}
			src.Y[row*src.YStride+col] = byte(pixel)
		}
	}
	// Match chroma so the test only depends on encoder-derived UV MVs and
	// the sixtap/bilinear residual from chroma sub-pel filtering.
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			src.U[row*src.UStride+col] = ref.Img.U[row*ref.Img.UStride+col]
			src.V[row*src.VStride+col] = ref.Img.V[row*ref.Img.VStride+col]
		}
	}
	ref.ExtendBorders()

	var pred vp8common.FrameBuffer
	if err := pred.Resize(w, h, 32, 32); err != nil {
		t.Fatalf("pred.Resize: %v", err)
	}

	const splitRDQIndex = testInterSearchQIndex
	var (
		dequantTables vp8common.FrameDequantTables
		dequant       vp8common.MacroblockDequant
		quant         vp8enc.MacroblockQuant
	)
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &dequantTables)
	vp8common.InitMacroblockDequant(&dequantTables, splitRDQIndex, &dequant)
	vp8enc.InitRegularMacroblockQuant(splitRDQIndex, &dequant, &quant)
	decision, ok := selectInterFrameSplitMotionDecisionRD(
		sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame,
		0, 0, vp8enc.MotionVector{}, splitRDQIndex, 0,
		&quant, nil, nil, &vp8tables.DefaultCoefProbs, &pred.Img,
		0, false, true,
	)
	if !ok {
		t.Fatalf("selectInterFrameSplitMotionDecisionRD returned false")
	}
	if decision.Mode.Mode != vp8common.SplitMV || decision.Mode.Partition != 0 {
		t.Fatalf("decision.Mode = %+v, want SPLITMV partition 0", decision.Mode)
	}
	if decision.Mode.BlockMV[0] == decision.Mode.BlockMV[8] {
		t.Fatalf("expected distinct top/bottom MVs, got %+v / %+v", decision.Mode.BlockMV[0], decision.Mode.BlockMV[8])
	}

	// Per-4x4 luma EOB storage: at least one block in the moving top half
	// quantises to non-zero coefficients. Without this storage the SPLITMV
	// packet writer would have to re-quantise to recover the EOBs.
	nonZeroLumaEOBs := 0
	for block := 0; block < 16; block++ {
		if decision.LumaEOB(block) > 0 {
			nonZeroLumaEOBs++
		}
	}
	if nonZeroLumaEOBs == 0 {
		var snap [16]int
		for i := 0; i < 16; i++ {
			snap[i] = decision.LumaEOB(i)
		}
		t.Fatalf("expected at least one populated luma EOB, got %v", snap)
	}

	// UV rate must be non-zero: the chroma 8x8 MVs derived from the luma
	// partition (MV col=+4 half-pel for the top half, zero for the bottom
	// half) leave residual through the chroma sub-pixel filter taps. Prior
	// to this change selectInterFrameSplitMotionMode returned only the Y
	// mode and the SPLITMV RD score never charged any UV rate.
	if decision.UVRate <= 0 {
		t.Fatalf("expected non-zero UV rate, got %d (uv dist=%d)", decision.UVRate, decision.UVDist)
	}
	if decision.YRate <= 0 {
		t.Fatalf("expected non-zero Y rate, got %d", decision.YRate)
	}
}

// TestSelectInterFrameSplitMotionLabelLevelTrials mirrors
// rd_check_segment's per-label NEAREST/NEAR/ZERO/NEW exploration.
// The macroblock is split as BLOCK_16X8 (partition 0): the top 16x8 half
// is identical to the reference (best mode is ZERO4X4 with MV=0) and the
// bottom 16x8 half is shifted by a non-trivial MV that the per-label
// NEW4X4 motion search must locate. The two labels therefore commit to
// different sub-MV modes — exactly the ZERO+NEW combo libvpx's
// rd_check_segment supports — and the picker must explore both so that
// neither label is forced into a single shared mode.
func TestSelectInterFrameSplitMotionLabelLevelTrials(t *testing.T) {
	const w, h = 32, 32
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*13 + col*col*29 + row*col*5 + 11) & 255)
		}
	}
	// Top 16x8 half: identity copy from ref (subset 0 wants MV=0, which
	// the ZERO4X4 label trial covers without running a motion search).
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 0)
	// Bottom 16x8 half: shift by (dy=2, dx=3) — this MV is not zero, not
	// the LEFT4X4 predictor (MV from subset 0 = 0), and not the ABOVE4X4
	// predictor (no above-MB MV in this test). The NEW4X4 motion search
	// must locate it.
	copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 2, 3)
	ref.ExtendBorders()

	mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 0)
	if !ok {
		t.Fatalf("selectInterFrameSplitMotionMode returned ok=false")
	}
	if mode.Partition != 0 {
		t.Fatalf("partition = %d, want 0 (16x8)", mode.Partition)
	}

	// Subset 0: blocks 0..7 — wants MV=0. Either LEFT4X4 (which inherits
	// from bestRefMV=0 in the absence of an in-MB left predictor) or
	// ZERO4X4 satisfies the label-trial result; libvpx prefers LEFT4X4
	// when its MV equals ZERO4X4's per labels2mode's tie-breaking. We
	// only assert the chosen MV is zero — the per-label loop must have
	// considered both LEFT/ABOVE/ZERO trials on top of NEW.
	topMV := mode.BlockMV[0]
	topBMode := mode.BModes[0]
	if topMV != (vp8enc.MotionVector{}) {
		t.Fatalf("subset 0 (top) MV = %+v, want zero (LEFT4X4/ZERO4X4 label trial)", topMV)
	}
	if topBMode != vp8common.Zero4x4 && topBMode != vp8common.Left4x4 {
		t.Fatalf("subset 0 (top) BMode = %v, want Zero4x4 or Left4x4", topBMode)
	}

	// Subset 1: blocks 8..15 — wants NEW4X4 with MV=(16,24) in 1/8-pel
	// units, which is the (dy=2,dx=3) full-pel shift the NEW search
	// finds.
	bottomMV := mode.BlockMV[8]
	bottomBMode := mode.BModes[8]
	if bottomMV == (vp8enc.MotionVector{}) {
		t.Fatalf("subset 1 (bottom) MV = zero, want NEW4X4 search to find shift")
	}
	if bottomBMode != vp8common.New4x4 {
		t.Fatalf("subset 1 (bottom) BMode = %v, want New4x4 (label trial chose NEW)", bottomBMode)
	}
	if bottomMV != (vp8enc.MotionVector{Row: 16, Col: 24}) {
		t.Fatalf("subset 1 (bottom) MV = %+v, want {Row:16, Col:24} for (dy=2, dx=3) full-pel shift", bottomMV)
	}
}

// TestSelectInterFrameSplitMotionTHRNEWGatingSkipsSearch covers libvpx
// rd_check_segment's NEW4X4 gate:
//
//	if (best_label_rd < label_mv_thresh) break;
//
// where label_mv_thresh = bsi->mvthresh / label_count and bsi->mvthresh
// is x->rd_threshes[THR_NEW{1,2,3}]. With the gate disabled (mvthresh
// == 0) the picker locates the (dy=2, dx=3) motion via NEW4X4. With
// mvthresh set high enough that label_mv_thresh exceeds the running
// best label cost on every label, the NEW4X4 search is skipped and the
// per-label picker falls back to LEFT4X4/ABOVE4X4/ZERO4X4 only — so
// neither label commits to a non-zero NEW vector.
func TestSelectInterFrameSplitMotionTHRNEWGatingSkipsSearch(t *testing.T) {
	const w, h = 32, 32
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*13 + col*col*29 + row*col*5 + 11) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 0)
	copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 2, 3)
	ref.ExtendBorders()

	source := sourceImageFromPublic(src)
	// Sanity check: with the gate disabled, the picker still finds the
	// NEW vector. (Same setup as TestSelectInterFrameSplitMotionLabelLevelTrials.)
	open, ok := selectInterFrameSplitMotionModeWithSearchAndThreshold(
		source, &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{},
		testInterSearchQIndex, 0,
		nil, nil, defaultInterAnalysisSearchConfig(), 0, nil,
		&vp8tables.DefaultMVContext, 0,
	)
	if !ok {
		t.Fatalf("ungated picker returned ok=false")
	}
	if open.BModes[8] != vp8common.New4x4 {
		t.Fatalf("ungated bottom BMode = %v, want New4x4", open.BModes[8])
	}

	// Now set mvthresh so high that label_mv_thresh = mvthresh/label_count
	// exceeds the running best label cost (SAD + sub-MV-rate) on every
	// label trial — this fires the gate and the NEW4X4 motion search is
	// skipped. The picker still has to commit non-NEW labels for both
	// subsets, so we just verify (a) the gated picker returned ok and
	// (b) no subset committed NEW4X4. To keep the partition non-trivial
	// (the picker rejects all-equal-MV returns), we supply a SplitMV
	// left-MB whose right-edge per-4x4 MVs differ between block 3
	// (subset 0's left predictor) and block 11 (subset 1's left
	// predictor). With NEW gated, subset 0 then commits LEFT4X4 with
	// the upper-half left-edge MV, and subset 1 commits LEFT4X4 with
	// the lower-half left-edge MV — both non-NEW, both distinct.
	left := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 0,
	}
	for block := 0; block < 16; block++ {
		left.BModes[block] = vp8common.Left4x4
		if block < 8 {
			left.BlockMV[block] = vp8enc.MotionVector{Row: 8, Col: 0}
		} else {
			left.BlockMV[block] = vp8enc.MotionVector{Row: 0, Col: 8}
		}
	}
	left.MV = left.BlockMV[15]
	const huge = 1 << 30
	gated, ok := selectInterFrameSplitMotionModeWithSearchAndThreshold(
		source, &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{},
		testInterSearchQIndex, 0,
		&left, nil, defaultInterAnalysisSearchConfig(), 0, nil,
		&vp8tables.DefaultMVContext, huge,
	)
	if !ok {
		// The gated picker rejects returns where every subset has the
		// same MV. With a SplitMV left-MB that has distinct per-half
		// right-edge MVs, the LEFT4X4 trials for subset 0 and subset
		// 1 differ, so the gated picker should return a valid SPLITMV.
		t.Fatalf("gated picker returned ok=false (synthetic left-MB SplitMV did not break label symmetry)")
	}
	for block := 0; block < 16; block++ {
		if gated.BModes[block] == vp8common.New4x4 {
			t.Fatalf("block %d BMode = New4x4 with gate fired (mvthresh=%d), want non-NEW (LEFT/ABOVE/ZERO)", block, huge)
		}
	}
}

// TestSelectInterFrameSplitMotionOtherCostBreakdown asserts the
// rate-decomposition invariant from update_best_mode in
// vp8_rd_pick_inter_mode:
//
//	rd.rate2 = rd.rate_y (label tree + sub-MV-mode + MV cost) +
//	           rd.rate_uv (rd_inter4x4_uv) +
//	           other_cost (default no-skip / skip backout) +
//	           x->ref_frame_cost[ref_frame]
//
// The govpx port plumbs all four contributors through
// interSplitMVRDDecision so callers can verify the breakdown without
// rerunning the picker. This test sets explicit otherCost / refCost
// values and asserts decision.TotalRate sums to YRate + UVRate +
// OtherCost + RefCost, mirroring update_best_mode's accounting.
func TestSelectInterFrameSplitMotionOtherCostBreakdown(t *testing.T) {
	const w, h = 32, 32
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*11 + col*col*23 + row*col*5 + 7) & 255)
		}
	}
	uvWidth := (w + 1) >> 1
	uvHeight := (h + 1) >> 1
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			ref.Img.U[row*ref.Img.UStride+col] = byte((row*19 ^ col*13) & 255)
			ref.Img.V[row*ref.Img.VStride+col] = byte((row*7 + col*29 + 41) & 255)
		}
	}
	// Use the same shape as the existing chroma residual test so the
	// picker commits a non-trivial SPLITMV decision with both Y and UV
	// rate populated.
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 1)
	copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 0, 0)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			block := (row>>2)*4 + (col >> 2)
			delta := 60
			if block&1 == 0 {
				delta = -60
			}
			pixel := int(src.Y[row*src.YStride+col]) + delta
			if pixel < 0 {
				pixel = 0
			} else if pixel > 255 {
				pixel = 255
			}
			src.Y[row*src.YStride+col] = byte(pixel)
		}
	}
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			src.U[row*src.UStride+col] = ref.Img.U[row*ref.Img.UStride+col]
			src.V[row*src.VStride+col] = ref.Img.V[row*ref.Img.VStride+col]
		}
	}
	ref.ExtendBorders()

	var pred vp8common.FrameBuffer
	if err := pred.Resize(w, h, 32, 32); err != nil {
		t.Fatalf("pred.Resize: %v", err)
	}
	const splitRDQIndex = testInterSearchQIndex
	var (
		dequantTables vp8common.FrameDequantTables
		dequant       vp8common.MacroblockDequant
		quant         vp8enc.MacroblockQuant
	)
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &dequantTables)
	vp8common.InitMacroblockDequant(&dequantTables, splitRDQIndex, &dequant)
	vp8enc.InitRegularMacroblockQuant(splitRDQIndex, &dequant, &quant)

	const otherCost = 40
	const refCost = 90
	decision, ok := selectInterFrameSplitMotionDecisionRDWithThreshold(
		sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame,
		0, 0, vp8enc.MotionVector{}, splitRDQIndex, 0,
		&quant, nil, nil, &vp8tables.DefaultCoefProbs, &pred.Img,
		0, false, true,
		0, otherCost, refCost,
	)
	if !ok {
		t.Fatalf("selectInterFrameSplitMotionDecisionRDWithThreshold returned false")
	}
	if decision.OtherCost != otherCost {
		t.Fatalf("OtherCost = %d, want %d", decision.OtherCost, otherCost)
	}
	if decision.RefCost != refCost {
		t.Fatalf("RefCost = %d, want %d", decision.RefCost, refCost)
	}
	if decision.YRate <= 0 {
		t.Fatalf("YRate = %d, want > 0", decision.YRate)
	}
	if decision.UVRate < 0 {
		t.Fatalf("UVRate = %d, want >= 0", decision.UVRate)
	}
	want := decision.YRate + decision.UVRate + decision.OtherCost + decision.RefCost
	if decision.TotalRate != want {
		t.Fatalf("TotalRate = %d, want YRate+UVRate+OtherCost+RefCost = %d", decision.TotalRate, want)
	}
	if decision.Rate2 != decision.TotalRate {
		t.Fatalf("Rate2 = %d, want TotalRate %d", decision.Rate2, decision.TotalRate)
	}
	// Y-only RD must be strictly less than full RD because UV rate /
	// distortion both contribute non-negatively (and UV rate > 0 in
	// this synthetic case via TestSelectInterFrameSplitMotionDecisionRDAccountsForChromaResidual's
	// shape).
	if decision.UVRate > 0 && decision.YRD >= decision.RD {
		t.Fatalf("YRD = %d, RD = %d, want YRD < RD when UV rate is non-zero", decision.YRD, decision.RD)
	}
}

// TestSelectInterFrameModeDecisionShortCircuitsInactiveMacroblock asserts that
// the inter mode dispatcher mirrors libvpx's evaluate_inter_mode /
// evaluate_inter_mode_rd active_ptr early exits: when active_map[r][c]==0 the
// picker must return a ZEROMV/LAST decision with skip=1 and segment=0 without
// running any of the per-mode RD evaluations (i.e. no mode-test counter is
// incremented and no threshold is touched).
func TestSelectInterFrameModeDecisionShortCircuitsInactiveMacroblock(t *testing.T) {
	const w, h = 32, 16
	e := newSizedTestEncoder(t, w, h)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	const mbRows, mbCols = 1, 2
	mask := []uint8{0, 1}
	if err := e.SetActiveMap(mask, mbRows, mbCols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	if !e.activeMapEnabled {
		t.Fatalf("activeMapEnabled = false after SetActiveMap, want true")
	}
	src := testImage(w, h)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, w, h, 64, 96, 160)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte((row*53 + col*97 + 7) & 255)
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{
		Frame:      vp8common.LastFrame,
		Img:        &last.Img,
		RefRateSet: true,
		RefRate:    1 << 20,
	}}
	quant := testRegularMacroblockQuant(t, testInterSearchQIndex)

	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()
	beforeMBs := e.interMBsTestedSoFar
	beforeHits := e.interModeTestHitCounts
	beforeTouched := e.interRDThreshTouched

	decision, ok := e.selectInterFrameModeDecision(
		sourceImageFromPublic(src), refs[:], len(refs),
		0, 0, mbRows, mbCols,
		testInterSearchQIndex, vp8enc.SegmentationConfig{}, 0,
		nil, nil, nil, nil, nil, &quant,
	)

	if !ok {
		t.Fatalf("dispatcher returned ok=false for inactive MB")
	}
	if decision.useIntra {
		t.Fatalf("decision.useIntra = true, want false (inactive MB must defer to LAST/ZEROMV)")
	}
	if decision.interMode.RefFrame != vp8common.LastFrame {
		t.Fatalf("interMode.RefFrame = %v, want LAST", decision.interMode.RefFrame)
	}
	if decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("interMode.Mode = %v, want ZEROMV", decision.interMode.Mode)
	}
	if decision.interMode.MV != (vp8enc.MotionVector{}) {
		t.Fatalf("interMode.MV = %+v, want zero", decision.interMode.MV)
	}
	if !decision.interMode.MBSkipCoeff {
		t.Fatalf("interMode.MBSkipCoeff = false, want true")
	}
	if decision.interMode.SegmentID != 0 {
		t.Fatalf("interMode.SegmentID = %d, want 0", decision.interMode.SegmentID)
	}
	if !decision.cyclicRefreshEligible() {
		t.Fatalf("cyclicRefreshEligible = false, want true for ZEROMV/LAST")
	}
	if e.interMBsTestedSoFar != beforeMBs {
		t.Fatalf("interMBsTestedSoFar = %d, want %d (no per-MB picker increment on short-circuit)", e.interMBsTestedSoFar, beforeMBs)
	}
	if e.interModeTestHitCounts != beforeHits {
		t.Fatalf("interModeTestHitCounts changed; short-circuit must skip the mode loop entirely")
	}
	if e.interRDThreshTouched != beforeTouched {
		t.Fatalf("interRDThreshTouched changed; short-circuit must not touch RD thresholds")
	}
}

// TestImprovedInterFrameSearchStartReferencePolicyAppliesAltRefSignBias verifies
// the high-level reference-switching sign-bias policy: when libvpx walks LAST,
// GOLDEN, and ALTREF as candidate references in vp8_pick_inter_mode /
// vp8_rd_pick_inter_mode, vp8_mv_pred biases each near-MV with mv_bias() based
// on the neighbour's stored ref frame versus the currently tested ref frame.
// In libvpx only ALTREF ever flips its sign bias (driven by source_alt_ref_active
// in onyx_if.c update_alt_ref_frame_stats); LAST and GOLDEN remain at 0. The
// expected behaviour is that re-running the predictor for the same neighbour
// table with target=LAST vs target=ALTREF produces opposite-signed predicted
// MVs when the neighbour ref disagrees with the target on the sign bias map.
func TestImprovedInterFrameSearchStartReferencePolicyAppliesAltRefSignBias(t *testing.T) {
	const mbRows, mbCols = 3, 3
	src := testImage(mbCols*16, mbRows*16)
	fillImage(src, 96, 90, 170)
	analysis := testVP8Frame(t, mbCols*16, mbRows*16, 96, 90, 170)
	last := testVP8Frame(t, mbCols*16, mbRows*16, 96, 90, 170)
	// Populate the previous-frame mode grid with the same LAST-ref MV in every
	// MB so all five lf-frame slots land on LAST sign-bias=false. Two cells
	// (mbRow,mbCol-1 = 1,0 and mbRow-1,mbCol-1 = 0,0) are intra to mirror an
	// arbitrary mix; the remaining cells stamp a positive col MV.
	modes := make([]vp8enc.InterFrameMacroblockMode, mbRows*mbCols)
	bias := make([]bool, len(modes))
	for r := 0; r < mbRows; r++ {
		for c := 0; c < mbCols; c++ {
			modes[r*mbCols+c] = vp8enc.InterFrameMacroblockMode{
				RefFrame: vp8common.LastFrame,
				Mode:     vp8common.NewMV,
				MV:       vp8enc.MotionVector{Col: 24},
			}
		}
	}
	e := &VP8Encoder{
		analysis:                 analysis,
		lastRef:                  last,
		lastFrameInterModes:      modes,
		lastFrameInterModeBias:   bias,
		lastFrameInterModesValid: true,
		sourceAltRefActive:       true, // sign_bias[ALTREF] = 1, sign_bias[LAST] = 0
	}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	left := above
	aboveLeft := above
	search := interAnalysisSearchConfig{improvedMVPrediction: true}

	// Target=LAST: target sign-bias matches the LAST-ref neighbours, so the
	// predicted MV is taken verbatim from the first neighbour-ranked LAST slot.
	startLast := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, mbRows, mbCols, &above, &left, &aboveLeft, search)
	if !startLast.ok {
		t.Fatalf("LAST predictor returned ok=false")
	}
	if startLast.mv != (vp8enc.MotionVector{Col: 24}) {
		t.Fatalf("LAST predictor MV = %+v, want {Col: 24} (no sign flip when sign_bias[LAST] == sign_bias[LAST])", startLast.mv)
	}

	// Target=ALTREF: every neighbour holds a LAST ref with sign-bias=0, but
	// target ALTREF has sign-bias=1, so libvpx's mv_bias flips every near-MV.
	// No neighbour has refFrame==ALTREF, so improvedInterFrameSearchStart falls
	// through to the median-of-flipped-MVs fallback with sr=0 — the libvpx
	// "sr=0 lets caller pick search range" branch.
	startAltRef := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, mbRows, mbCols, &above, &left, &aboveLeft, search)
	if !startAltRef.ok {
		t.Fatalf("ALTREF predictor returned ok=false")
	}
	if startAltRef.mv != (vp8enc.MotionVector{Col: -24}) {
		t.Fatalf("ALTREF predictor MV = %+v, want {Col: -24} (sign flipped because sign_bias[ALTREF] != sign_bias[LAST])", startAltRef.mv)
	}
	if startAltRef.sr != 0 {
		t.Fatalf("ALTREF predictor sr = %d, want 0 (median fallback when no neighbour matches target ref)", startAltRef.sr)
	}

	// Symmetry check: a neighbour table populated with ALTREF refs collapses
	// the bias decision the other way — predicting ALTREF returns the raw MV,
	// predicting LAST flips it.
	above.RefFrame = vp8common.AltRefFrame
	left.RefFrame = vp8common.AltRefFrame
	aboveLeft.RefFrame = vp8common.AltRefFrame
	for i := range modes {
		modes[i].RefFrame = vp8common.AltRefFrame
		bias[i] = true
	}
	startAltRef2 := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, mbRows, mbCols, &above, &left, &aboveLeft, search)
	if !startAltRef2.ok || startAltRef2.mv != (vp8enc.MotionVector{Col: 24}) {
		t.Fatalf("ALTREF predictor with ALTREF neighbours = %+v, want {Col: 24} (matching sign_bias must not flip)", startAltRef2.mv)
	}
	startLast2 := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, mbRows, mbCols, &above, &left, &aboveLeft, search)
	if !startLast2.ok || startLast2.mv != (vp8enc.MotionVector{Col: -24}) {
		t.Fatalf("LAST predictor with ALTREF neighbours = %+v, want {Col: -24} (sign flipped because sign_bias[LAST] != sign_bias[ALTREF])", startLast2.mv)
	}
}

// TestSelectRDInterFrameModeDecisionUsesTempTokenContext anchors libvpx
// rdopt.c vp8_rd_pick_inter_mode's tempa/templ contract: candidate-mode
// trials operate on stack-local copies of ENTROPY_CONTEXT and only the
// chosen mode's context is committed to the row state. Pre-populating the
// caller's aboveTok/leftTok with distinctive sentinels and then driving the
// RD picker must leave those structs untouched on return — the deferred
// updateInterAnalysisTokenContext in
// buildReconstructingInterFrameCoefficientsWithSegmentation owns the commit.
func TestSelectRDInterFrameModeDecisionUsesTempTokenContext(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	if !e.interAnalysisUsesRDModeDecision() {
		t.Fatalf("interAnalysisUsesRDModeDecision = false, want true under best-quality deadline")
	}

	src := testImage(16, 16)
	fillImage(src, 96, 96, 96)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte((19 + row*41 + col*23 + row*col*7) & 255)
		}
	}
	last := testVP8Frame(t, 16, 16, 96, 96, 96)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte((211 - row*13 - col*29) & 255)
		}
	}
	last.ExtendBorders()
	golden := testVP8Frame(t, 16, 16, 96, 96, 96)
	for row := 0; row < 16; row++ {
		copy(golden.Img.Y[row*golden.Img.YStride:], src.Y[row*src.YStride:row*src.YStride+16])
	}
	golden.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img, RefRateSet: true, RefRate: 1 << 20},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img, RefRateSet: true, RefRate: 1 << 20},
	}
	quant := testRegularMacroblockQuant(t, testInterSearchQIndex)

	// Distinctive sentinels: every Y/UV/Y2 plane gets a non-zero pattern that
	// no legitimate post-trial token state could match (libvpx hasCoeffs
	// values are 0 or 1, never ones with high bits set).
	above := vp8enc.TokenContextPlanes{
		Y1: [4]uint8{0xA1, 0xA2, 0xA3, 0xA4},
		U:  [2]uint8{0xA5, 0xA6},
		V:  [2]uint8{0xA7, 0xA8},
		Y2: 0xA9,
	}
	left := vp8enc.TokenContextPlanes{
		Y1: [4]uint8{0xB1, 0xB2, 0xB3, 0xB4},
		U:  [2]uint8{0xB5, 0xB6},
		V:  [2]uint8{0xB7, 0xB8},
		Y2: 0xB9,
	}
	aboveSnapshot := above
	leftSnapshot := left

	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()
	decision, ok := e.selectRDInterFrameModeDecision(
		sourceImageFromPublic(src), refs[:], len(refs),
		0, 0, 1, 1, testInterSearchQIndex, 0,
		nil, nil, nil,
		&above, &left,
		&quant,
	)
	if !ok {
		t.Fatalf("selectRDInterFrameModeDecision returned ok=false")
	}
	// The picker must explore at least one inter or intra candidate, so this
	// is a meaningful exercise of the per-mode token-context paths.
	if !decision.useIntra && decision.interMode.Mode == vp8common.SplitMV {
		// SplitMV exercises a different RD subroutine; either is fine for the
		// invariant we're testing.
		_ = decision
	}

	if above != aboveSnapshot {
		t.Fatalf("aboveTok mutated by RD picker: got %+v, want %+v (caller-owned ENTROPY_CONTEXT must not be touched during candidate trials)", above, aboveSnapshot)
	}
	if left != leftSnapshot {
		t.Fatalf("leftTok mutated by RD picker: got %+v, want %+v (caller-owned ENTROPY_CONTEXT must not be touched during candidate trials)", left, leftSnapshot)
	}
}

// TestRecodeLoopResetsTokenContext anchors libvpx onyx_if.c
// restore_coding_context's effect on the per-row ENTROPY_CONTEXT during the
// inter-frame recode loop: each call to
// buildReconstructingInterFrameCoefficientsWithSegmentation begins with a
// freshly zeroed above/left token-context working set, so a rejected
// attempt's commits never leak into the next attempt. We simulate two
// recode attempts on the same input by corrupting e.tokenAbove between
// calls; the second pass must produce identical coefficients to the first
// because the per-MB working contexts are local to the function.
func TestRecodeLoopResetsTokenContext(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte((33 + row*51 + col*61 + row*col*9) & 255)
		}
	}
	fillBenchmarkVP8Image(&e.lastRef.Img, 200, 90, 170)
	e.lastRef.ExtendBorders()

	modesA := make([]vp8enc.InterFrameMacroblockMode, 1)
	coeffsA := make([]vp8enc.MacroblockCoefficients, 1)
	if err := e.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), testInterSearchQIndex, modesA, coeffsA, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("first recode attempt returned error: %v", err)
	}

	// Simulate a rejected first attempt that left junk in the encoder's
	// per-frame e.tokenAbove buffer (which the packet writer also expects
	// to overwrite at the start of every call). Set every plane to 0xFF so
	// any leak into the second attempt's RD picker would produce different
	// quantized residuals than the first attempt.
	for i := range e.tokenAbove {
		e.tokenAbove[i] = vp8enc.TokenContextPlanes{
			Y1: [4]uint8{0xFF, 0xFF, 0xFF, 0xFF},
			U:  [2]uint8{0xFF, 0xFF},
			V:  [2]uint8{0xFF, 0xFF},
			Y2: 0xFF,
		}
	}

	modesB := make([]vp8enc.InterFrameMacroblockMode, 1)
	coeffsB := make([]vp8enc.MacroblockCoefficients, 1)
	if err := e.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), testInterSearchQIndex, modesB, coeffsB, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("second recode attempt returned error: %v", err)
	}

	if modesA[0].Mode != modesB[0].Mode || modesA[0].RefFrame != modesB[0].RefFrame || modesA[0].MV != modesB[0].MV || modesA[0].MBSkipCoeff != modesB[0].MBSkipCoeff {
		t.Fatalf("recode mode drift: first=%+v second=%+v (per-MB token contexts must reset at start of each attempt)", modesA[0], modesB[0])
	}
	if coeffsA[0] != coeffsB[0] {
		t.Fatalf("recode coefficient drift: corrupted e.tokenAbove leaked across attempts")
	}
}
