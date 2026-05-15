package govpx

import (
	"math"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestLibvpxInterModeRDThresholdsScaleLikeInitializeRDConsts(t *testing.T) {
	qValue := vp8common.DCQuant(40, 0)
	q := max(int(math.Pow(float64(qValue), 1.25)), 8)
	thresholds := libvpxInterModeRDThresholds(40, 0, DeadlineBestQuality, 0)
	if got, want := thresholds[libvpxThrNew1], 1000*q/100; got != want {
		t.Fatalf("high-rdmult NEW1 threshold = %d, want thresh_mult*q/100 = %d", got, want)
	}
	if got := thresholds[libvpxThrDC]; got != 0 {
		t.Fatalf("DC threshold = %d, want always-tested zero", got)
	}

	lowQ := vp8common.DCQuant(4, 0)
	lowQPow := max(int(math.Pow(float64(lowQ), 1.25)), 8)
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

	realtime := libvpxInterModeCheckFrequencies(DeadlineRealtime, -10)
	if realtime[libvpxThrZero2] != 2 || realtime[libvpxThrNew1] != 0 || realtime[libvpxThrNew2] != 8 {
		t.Fatalf("realtime explicit speed 10 frequencies = ZERO2:%d NEW1:%d NEW2:%d, want 2/0/8", realtime[libvpxThrZero2], realtime[libvpxThrNew1], realtime[libvpxThrNew2])
	}
}

func TestLibvpxInterModeThresholdMultipliersTemporalLayerTweaks(t *testing.T) {
	baseline := libvpxInterModeThresholdMultipliers(DeadlineRealtime, -6)
	unchanged := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, -6, libvpxInterModeThresholdContext{
		temporalLayers: 1,
		lastEnabled:    true,
		goldenEnabled:  true,
		closestRef:     vp8common.GoldenFrame,
	})
	if unchanged != baseline {
		t.Fatalf("one-layer temporal multipliers changed: %v want %v", unchanged, baseline)
	}

	tooFast := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, -7, libvpxInterModeThresholdContext{
		temporalLayers: 2,
		lastEnabled:    true,
		goldenEnabled:  true,
		closestRef:     vp8common.GoldenFrame,
	})
	if want := libvpxInterModeThresholdMultipliers(DeadlineRealtime, -7); tooFast != want {
		t.Fatalf("explicit speed 7 temporal multipliers changed: %v want %v", tooFast, want)
	}

	missingGolden := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, -6, libvpxInterModeThresholdContext{
		temporalLayers: 2,
		lastEnabled:    true,
		closestRef:     vp8common.LastFrame,
	})
	if missingGolden != baseline {
		t.Fatalf("missing-GOLDEN temporal multipliers changed: %v want %v", missingGolden, baseline)
	}

	closestGolden := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, -6, libvpxInterModeThresholdContext{
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

	closestLast := libvpxInterModeThresholdMultipliersForContext(DeadlineRealtime, -6, libvpxInterModeThresholdContext{
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
	e := &VP8Encoder{
		opts: EncoderOptions{Deadline: DeadlineBestQuality},
		rc:   rateControlState{currentQuantizer: 40},
	}
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

func TestInterFastBestThresholdUsesPickInterBestDecay(t *testing.T) {
	baseline := libvpxInterModeRDThresholds(40, 0, DeadlineRealtime, 8)

	best := &VP8Encoder{
		opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		rc:   rateControlState{currentQuantizer: 40},
	}
	best.resetInterRDThresholdMultipliers()
	best.beginInterRDModeDecisionFrame()
	defer best.endInterRDModeDecisionFrame()

	best.lowerBestInterFastThreshold(libvpxThrNew1)
	afterBest := best.interModeRDThresholds(40)
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

func TestSplitMVThresholdUsesNewMVReferenceThresholds(t *testing.T) {
	var thresholds [libvpxInterModeCount]int
	thresholds[libvpxThrNew1] = 11
	thresholds[libvpxThrNew2] = 22
	thresholds[libvpxThrNew3] = 33

	if got := splitMVThresholdForRefSlot(thresholds, 1); got != 11 {
		t.Fatalf("slot 1 SplitMV threshold = %d, want THR_NEW1", got)
	}
	if got := splitMVThresholdForRefSlot(thresholds, 2); got != 22 {
		t.Fatalf("slot 2 SplitMV threshold = %d, want THR_NEW2", got)
	}
	if got := splitMVThresholdForRefSlot(thresholds, 3); got != 33 {
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
		want int8
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

func TestInterReferenceSearchOrderCompactsEnabledReferences(t *testing.T) {
	refs := [...]interAnalysisReference{
		{Frame: vp8common.GoldenFrame, Img: &vp8common.Image{}},
		{Frame: vp8common.AltRefFrame, Img: &vp8common.Image{}},
	}
	order := interReferenceSearchOrder(refs[:], len(refs))
	if order != [4]int8{-1, 0, 1, -1} {
		t.Fatalf("reference search order = %v, want compacted GOLDEN/ALT in slots 1/2", order)
	}

	ref, refIndex, ok := interReferenceBySearchSlot(refs[:], order, 1)
	if !ok || refIndex != 0 || ref.Frame != vp8common.GoldenFrame {
		t.Fatalf("slot 1 = %+v index=%d ok=%t, want compacted GOLDEN", ref, refIndex, ok)
	}
	ref, refIndex, ok = interReferenceBySearchSlot(refs[:], order, 2)
	if !ok || refIndex != 1 || ref.Frame != vp8common.AltRefFrame {
		t.Fatalf("slot 2 = %+v index=%d ok=%t, want compacted ALTREF", ref, refIndex, ok)
	}
	if _, _, ok := interReferenceBySearchSlot(refs[:], order, 3); ok {
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
		{name: "realtime explicit speed one can skip 4x4 search", deadline: DeadlineRealtime, cpuUsed: -1, want: false},
		{name: "realtime positive auto-speed can skip 4x4 search", deadline: DeadlineRealtime, cpuUsed: 1, want: false},
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
