package govpx

import (
	"math"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
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
			name:       "good clamps cpu-used before nstep iterative config",
			deadline:   DeadlineGoodQuality,
			cpuUsed:    8,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  2,
			further:    5,
			improved:   true,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "realtime positive cpu-used auto-selects speed four",
			deadline:   DeadlineRealtime,
			cpuUsed:    8,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  2,
			further:    5,
			improved:   true,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "realtime explicit speed three RD uses first step directly",
			deadline:   DeadlineRealtime,
			cpuUsed:    -3,
			fullPixel:  interAnalysisFullPixelSearchNstep,
			stepParam:  1,
			further:    6,
			improved:   true,
			fractional: interAnalysisFractionalSearchIterative,
		},
		{
			name:       "realtime explicit speed five switches to hex and step subpixel",
			deadline:   DeadlineRealtime,
			cpuUsed:    -5,
			fullPixel:  interAnalysisFullPixelSearchHex,
			stepParam:  2,
			further:    5,
			improved:   true,
			fractional: interAnalysisFractionalSearchStep,
		},
		{
			name:       "realtime explicit speed nine keeps hex and half-pixel only",
			deadline:   DeadlineRealtime,
			cpuUsed:    -9,
			fullPixel:  interAnalysisFullPixelSearchHex,
			stepParam:  4,
			further:    0,
			improved:   false,
			fractional: interAnalysisFractionalSearchHalf,
		},
		{
			name:       "realtime explicit speed fifteen skips fractional search",
			deadline:   DeadlineRealtime,
			cpuUsed:    -15,
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
			if cfg.fullPixelSearch != tt.fullPixel || int(cfg.fullPixelSearchParam) != tt.stepParam || int(cfg.fullPixelFurtherSteps) != tt.further || cfg.improvedMVPrediction != tt.improved || cfg.fractionalSearch != tt.fractional {
				t.Fatalf("config = {%d %d %d %t %d}, want {%d %d %d %t %d}", cfg.fullPixelSearch, cfg.fullPixelSearchParam, cfg.fullPixelFurtherSteps, cfg.improvedMVPrediction, cfg.fractionalSearch, tt.fullPixel, tt.stepParam, tt.further, tt.improved, tt.fractional)
			}
		})
	}
}

func TestInterAnalysisSearchConfigKeepsLibvpxSpeed4RealtimeSearch(t *testing.T) {
	serial := &VP8Encoder{
		opts: EncoderOptions{
			Width:    1280,
			Height:   720,
			Deadline: DeadlineRealtime,
			CpuUsed:  8,
		},
	}
	if got := serial.interAnalysisSearchConfig(); got.fullPixelSearch != interAnalysisFullPixelSearchNstep || got.fractionalSearch != interAnalysisFractionalSearchIterative {
		t.Fatalf("serial 720p speed=4 search = full %d fractional %d, want nstep+iterative", got.fullPixelSearch, got.fractionalSearch)
	}

	large := &VP8Encoder{
		opts: EncoderOptions{
			Width:    1920,
			Height:   1080,
			Deadline: DeadlineRealtime,
			CpuUsed:  8,
		},
	}
	if got := large.interAnalysisSearchConfig(); got.fullPixelSearch != interAnalysisFullPixelSearchNstep || got.fractionalSearch != interAnalysisFractionalSearchIterative {
		t.Fatalf("large 1080p speed=4 search = full %d fractional %d, want nstep+iterative", got.fullPixelSearch, got.fractionalSearch)
	}

	threaded := &VP8Encoder{
		opts: EncoderOptions{
			Width:    1920,
			Height:   1080,
			Deadline: DeadlineRealtime,
			CpuUsed:  8,
		},
		threadedRowsActive: true,
	}
	if got := threaded.interAnalysisSearchConfig(); got.fullPixelSearch != interAnalysisFullPixelSearchNstep || got.fractionalSearch != interAnalysisFractionalSearchIterative {
		t.Fatalf("threaded 1080p speed=4 search = full %d fractional %d, want nstep+iterative", got.fullPixelSearch, got.fractionalSearch)
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
		{name: "realtime positive cpu-used auto-speed keeps improved MV prediction", deadline: DeadlineRealtime, cpuUsed: 7, want: true},
		{name: "realtime explicit speed six keeps improved MV prediction", deadline: DeadlineRealtime, cpuUsed: -6, want: true},
		{name: "realtime explicit speed seven disables improved MV prediction", deadline: DeadlineRealtime, cpuUsed: -7, want: false},
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
		{name: "good speed two final encode uses regular quant", deadline: DeadlineGoodQuality, cpuUsed: 2, want: false},
		{name: "good speed three uses fast quant", deadline: DeadlineGoodQuality, cpuUsed: 3, want: true},
		{name: "realtime positive cpu-used auto-speed uses fast quant", deadline: DeadlineRealtime, cpuUsed: 0, want: true},
		{name: "realtime explicit speed one uses fast quant", deadline: DeadlineRealtime, cpuUsed: -1, want: true},
		{name: "realtime explicit speed eight uses fast quant", deadline: DeadlineRealtime, cpuUsed: -8, want: true},
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

func TestLibvpxUseFastQuantForPickGateMirrorsSpeedFeatures(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality keeps regular quant for pick", deadline: DeadlineBestQuality, cpuUsed: 15, want: false},
		{name: "good speed zero keeps regular quant for pick", deadline: DeadlineGoodQuality, cpuUsed: 0, want: false},
		{name: "good speed one uses fast quant for pick", deadline: DeadlineGoodQuality, cpuUsed: 1, want: true},
		{name: "good speed two uses fast quant for pick", deadline: DeadlineGoodQuality, cpuUsed: 2, want: true},
		{name: "realtime positive cpu-used auto-speed uses fast quant for pick", deadline: DeadlineRealtime, cpuUsed: 0, want: true},
		{name: "realtime explicit speed one uses fast quant for pick", deadline: DeadlineRealtime, cpuUsed: -1, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.libvpxUseFastQuantForPick(); got != tt.want {
				t.Fatalf("fast quant for pick = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestConsumeInterRDCoeffCacheKeepsWinnerValidForConsumer(t *testing.T) {
	var e VP8Encoder
	e.interRDCoeffCacheWinner = 0
	e.interRDCoeffCacheSlots[0] = interRDCoeffCacheState{
		valid:         true,
		is4x4:         false,
		intra:         false,
		fastQuant:     true,
		qIndex:        20,
		zbinOverQuant: 7,
		zbinModeBoost: lastFrameZeroMVZbinBoost,
		mbRow:         2,
		mbCol:         3,
	}

	cache := e.consumeInterRDCoeffCache()
	if cache == nil {
		t.Fatalf("consumeInterRDCoeffCache returned nil")
	}
	args := predictedMacroblockCoefficientArgs{
		mbRow:         2,
		mbCol:         3,
		is4x4:         false,
		intra:         false,
		fastQuant:     true,
		qIndex:        20,
		zbinOverQuant: 7,
		zbinModeBoost: lastFrameZeroMVZbinBoost,
	}
	if !interRDCacheReusable(cache, &args) {
		t.Fatalf("consumed cache is not reusable by matching accepted-path args")
	}
}

func TestInterRDCoeffCacheCountsDCTReuse(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	if err := vp8enc.InitSegmentMacroblockQuants(20, vp8common.QuantDeltas{}, vp8enc.SegmentationConfig{}, &quants); err != nil {
		t.Fatalf("InitSegmentMacroblockQuants returned error: %v", err)
	}
	src := testImage(48, 48)
	pred := testVP8Frame(t, 48, 48, 0, 90, 170)
	cache := interRDCoeffCacheState{
		valid:         true,
		is4x4:         false,
		intra:         false,
		fastQuant:     true,
		qIndex:        20,
		zbinOverQuant: 7,
		zbinModeBoost: lastFrameZeroMVZbinBoost,
		mbRow:         1,
		mbCol:         1,
	}
	var out vp8enc.MacroblockCoefficients
	var phaseStats EncoderPhaseStats

	buildPredictedMacroblockCoefficientsInternal(&predictedMacroblockCoefficientArgs{
		coefProbs:     &probs,
		src:           sourceImageFromPublic(src),
		mbRow:         1,
		mbCol:         1,
		pred:          &pred.Img,
		quant:         &quants[0],
		qIndex:        20,
		zbinOverQuant: 7,
		zbinModeBoost: lastFrameZeroMVZbinBoost,
		fastQuant:     true,
		coeffs:        &out,
		cacheIn:       &cache,
		phaseStats:    &phaseStats,
	})

	if phaseStats.InterRDCoeffCacheRequests != 1 || phaseStats.InterRDCoeffCacheDCTHits != 1 {
		t.Fatalf("phase cache stats = %+v, want one DCT-cache hit", phaseStats)
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
		{name: "realtime positive cpu-used auto-speed uses fast pick mode", deadline: DeadlineRealtime, cpuUsed: 3, want: false},
		{name: "realtime explicit speed three keeps RD mode decision", deadline: DeadlineRealtime, cpuUsed: -3, want: true},
		{name: "realtime explicit speed four uses fast pick mode", deadline: DeadlineRealtime, cpuUsed: -4, want: false},
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
	if realtime[libvpxThrVPred] != 2000 || realtime[libvpxThrHPred] != 2000 || realtime[libvpxThrBPred] != 5000 {
		t.Fatalf("realtime positive cpu-used auto-speed intra thresholds = V:%d H:%d B:%d, want 2000/2000/5000", realtime[libvpxThrVPred], realtime[libvpxThrHPred], realtime[libvpxThrBPred])
	}
	if realtime[libvpxThrZero2] != 2000 || realtime[libvpxThrNew2] != 2500 || realtime[libvpxThrSplit1] != libvpxInterModeThresholdDisabled {
		t.Fatalf("realtime positive cpu-used auto-speed thresholds = ZERO2:%d NEW2:%d SPLIT1:%d, want 2000/2500/disabled", realtime[libvpxThrZero2], realtime[libvpxThrNew2], realtime[libvpxThrSplit1])
	}

	explicitRealtime := libvpxInterModeThresholdMultipliers(DeadlineRealtime, -8)
	if explicitRealtime[libvpxThrVPred] != libvpxInterModeThresholdDisabled || explicitRealtime[libvpxThrHPred] != libvpxInterModeThresholdDisabled || explicitRealtime[libvpxThrBPred] != libvpxInterModeThresholdDisabled {
		t.Fatalf("realtime explicit speed 8 intra thresholds = V:%d H:%d B:%d, want disabled", explicitRealtime[libvpxThrVPred], explicitRealtime[libvpxThrHPred], explicitRealtime[libvpxThrBPred])
	}
	if explicitRealtime[libvpxThrZero2] != 2000 || explicitRealtime[libvpxThrNew2] != 4000 || explicitRealtime[libvpxThrSplit1] != libvpxInterModeThresholdDisabled {
		t.Fatalf("realtime explicit speed 8 thresholds = ZERO2:%d NEW2:%d SPLIT1:%d, want 2000/4000/disabled", explicitRealtime[libvpxThrZero2], explicitRealtime[libvpxThrNew2], explicitRealtime[libvpxThrSplit1])
	}
}

func TestLibvpxRealtimeAdaptiveInterModeThresholdMirrorsSpeedFeature(t *testing.T) {
	var empty [1024]uint32
	if got, want := libvpxRealtimeAdaptiveInterModeThreshold(&empty, 100, 8, 0), 1023<<7; got != want {
		t.Fatalf("empty error-bin threshold = %d, want %d", got, want)
	}

	var bins [1024]uint32
	bins[0] = 90
	bins[20] = 2
	if got, want := libvpxRealtimeAdaptiveInterModeThreshold(&bins, 100, 8, 0), 19<<7; got != want {
		t.Fatalf("populated error-bin threshold = %d, want %d", got, want)
	}

	var low [1024]uint32
	low[15] = 1
	if got := libvpxRealtimeAdaptiveInterModeThreshold(&low, 1, 8, 0); got != 2000 {
		t.Fatalf("low adaptive threshold = %d, want libvpx 2000 floor", got)
	}
}

func TestLibvpxInterModeThresholdMultipliersApplyRealtimeErrorBins(t *testing.T) {
	var bins [1024]uint32
	ctx := libvpxInterModeThresholdContext{
		refFrameCount: 3,
		totalMBs:      100,
		errorBins:     &bins,
	}
	got := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, -8, ctx)
	if got[libvpxThrNew1] != 1023<<7 ||
		got[libvpxThrNearest1] != (1023<<7)>>1 ||
		got[libvpxThrNear1] != (1023<<7)>>1 {
		t.Fatalf("slot-1 adaptive thresholds = NEW:%d NEAREST:%d NEAR:%d, want %d/%d/%d",
			got[libvpxThrNew1], got[libvpxThrNearest1], got[libvpxThrNear1],
			1023<<7, (1023<<7)>>1, (1023<<7)>>1)
	}
	if got[libvpxThrNew2] != (1023<<7)<<1 ||
		got[libvpxThrNearest2] != 1023<<7 ||
		got[libvpxThrNear2] != 1023<<7 {
		t.Fatalf("slot-2 adaptive thresholds = NEW:%d NEAREST:%d NEAR:%d, want %d/%d/%d",
			got[libvpxThrNew2], got[libvpxThrNearest2], got[libvpxThrNear2],
			(1023<<7)<<1, 1023<<7, 1023<<7)
	}
	if got[libvpxThrNew3] == (1023<<7)<<1 {
		t.Fatalf("slot-3 adaptive threshold changed with refFrameCount=3, want unchanged static map")
	}
}

func TestFastInterModeErrorBinsResetAndClampLikeLibvpx(t *testing.T) {
	e := &VP8Encoder{
		opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: -8, Width: 160, Height: 160},
		rc:   rateControlState{currentQuantizer: 40},
	}
	e.recordFastInterModeErrorBin(64 << 7)
	e.recordFastInterModeErrorBin(1 << 30)
	if e.interModeErrorBins[64] != 1 || e.interModeErrorBins[1023] != 1 {
		t.Fatalf("error bins[64]/[1023] = %d/%d, want 1/1", e.interModeErrorBins[64], e.interModeErrorBins[1023])
	}
	e.interModeErrorBins[0] = 90
	e.interModeErrorBins[20] = 2
	e.beginInterRDModeDecisionFrame()
	if e.interModeErrorBins[64] != 0 || e.interModeErrorBins[1023] != 0 {
		t.Fatalf("error bins after frame reset = %d/%d, want 0/0", e.interModeErrorBins[64], e.interModeErrorBins[1023])
	}
	if e.interModeSpeedErrorBins[64] != 1 || e.interModeSpeedErrorBins[1023] != 1 || e.interModeSpeedErrorBins[0] != 90 || e.interModeSpeedErrorBins[20] != 2 {
		t.Fatalf("speed-feature error bins[64]/[1023]/[0]/[20] = %d/%d/%d/%d, want previous-frame 1/1/90/2",
			e.interModeSpeedErrorBins[64], e.interModeSpeedErrorBins[1023], e.interModeSpeedErrorBins[0], e.interModeSpeedErrorBins[20])
	}
	var refImg vp8common.Image
	refs := []interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &refImg},
		{Frame: vp8common.GoldenFrame, Img: &refImg},
	}
	thresholds := e.interModeRDThresholdsForReferences(40, refs, len(refs))
	qValue := vp8common.DCQuant(40, 0)
	q := max(int(math.Pow(float64(qValue), 1.25)), 8)
	want := ((19 << 7) << 1) * q / 100
	if thresholds[libvpxThrNew2] != want {
		t.Fatalf("NEW2 threshold from previous-frame error bins = %d, want %d", thresholds[libvpxThrNew2], want)
	}
}

func TestInterModeThresholdsUseFrameBaseQWithSegmentation(t *testing.T) {
	var refImg vp8common.Image
	refs := []interAnalysisReference{{Frame: vp8common.LastFrame, Img: &refImg}}
	e := &VP8Encoder{
		opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: -8, Width: 160, Height: 160},
		rc:   rateControlState{currentQuantizer: 106},
	}
	e.beginInterRDModeDecisionFrame()
	got := e.interModeRDThresholdsForReferences(92, refs, len(refs))
	want := libvpxInterModeRDThresholdsForContext(106, 0, DeadlineRealtime, -8, libvpxInterModeThresholdContext{
		totalMBs:        e.interAnalysisMacroblockCount(),
		staticThreshold: e.opts.StaticThreshold,
		errorBins:       &e.interModeSpeedErrorBins,
		lastEnabled:     true,
		refFrameCount:   2,
		temporalLayers:  1,
	})
	if got[libvpxThrNearest1] != want[libvpxThrNearest1] {
		t.Fatalf("NEAREST1 threshold = %d, want frame-base-Q threshold %d", got[libvpxThrNearest1], want[libvpxThrNearest1])
	}
	if segmentQ := libvpxInterModeRDThresholdsForContext(92, 0, DeadlineRealtime, -8, libvpxInterModeThresholdContext{
		totalMBs:        e.interAnalysisMacroblockCount(),
		staticThreshold: e.opts.StaticThreshold,
		errorBins:       &e.interModeSpeedErrorBins,
		lastEnabled:     true,
		refFrameCount:   2,
		temporalLayers:  1,
	}); got[libvpxThrNearest1] == segmentQ[libvpxThrNearest1] {
		t.Fatalf("NEAREST1 threshold used segment Q: got %d segmentQ %d", got[libvpxThrNearest1], segmentQ[libvpxThrNearest1])
	}
}
