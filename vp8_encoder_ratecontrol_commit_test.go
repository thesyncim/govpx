package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestEncodeIntoUpdatesRateControlAfterFrame(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	initialQuantizer := e.rc.currentQuantizer
	initialRollingActual := e.rc.rollingActualBits
	initialRollingTarget := e.rc.rollingTargetBits
	initialLongRollingActual := e.rc.longRollingActualBits
	initialLongRollingTarget := e.rc.longRollingTargetBits
	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	wantRollingActual := libvpxRollingBits(initialRollingActual, result.SizeBytes*8, 3, 2)
	wantRollingTarget := libvpxRollingBits(initialRollingTarget, result.FrameTargetBits, 3, 2)
	if e.rc.rollingActualBits != wantRollingActual || e.rc.rollingTargetBits != wantRollingTarget {
		t.Fatalf("rolling bits = actual:%d target:%d, want %d/%d", e.rc.rollingActualBits, e.rc.rollingTargetBits, wantRollingActual, wantRollingTarget)
	}
	wantLongRollingActual := libvpxRollingBits(initialLongRollingActual, result.SizeBytes*8, 31, 5)
	wantLongRollingTarget := libvpxRollingBits(initialLongRollingTarget, result.FrameTargetBits, 31, 5)
	if e.rc.longRollingActualBits != wantLongRollingActual || e.rc.longRollingTargetBits != wantLongRollingTarget {
		t.Fatalf("long rolling bits = actual:%d target:%d, want %d/%d", e.rc.longRollingActualBits, e.rc.longRollingTargetBits, wantLongRollingActual, wantLongRollingTarget)
	}
	if result.BufferLevelBits != e.rc.bufferLevelBits {
		t.Fatalf("result buffer = %d, want rc buffer %d", result.BufferLevelBits, e.rc.bufferLevelBits)
	}
	if e.rc.currentQuantizer <= initialQuantizer {
		t.Fatalf("currentQuantizer = %d, want above initial %d after overshoot", e.rc.currentQuantizer, initialQuantizer)
	}
	if e.rc.framesSinceKeyframe != 0 {
		t.Fatalf("framesSinceKeyframe = %d, want 0 after keyframe", e.rc.framesSinceKeyframe)
	}
}

func TestEncodeIntoRetriesQuantizerBeforeCommitOnOvershoot(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := rateControlTestFrame(32, 32, 0)
	packet := make([]byte, 16384)

	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	if result.Quantizer <= 4 {
		t.Fatalf("result quantizer = %d, want retry above initial 4", result.Quantizer)
	}
	if got := packetBaseQIndex(t, result.Data); got != vp8common.PublicQuantizerToQIndex(result.Quantizer) {
		t.Fatalf("packet base q = %d, want public result quantizer %d mapped to qindex %d", got, result.Quantizer, vp8common.PublicQuantizerToQIndex(result.Quantizer))
	}
	if e.rc.lastQuantizer != packetBaseQIndex(t, result.Data) {
		t.Fatalf("last quantizer = %d, want committed packet qindex %d", e.rc.lastQuantizer, packetBaseQIndex(t, result.Data))
	}
	decoded := decodeSingleFrame(t, result.Data)
	assertImagesEqual(t, "retried current", decoded, publicImageFromVP8(&e.current.Img))
}

func TestEncodeKeyFrameAttemptDefersEntropyCommit(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	e.coefProbs[0][0][0][0] = 77
	e.modeProbs.MV[0][0] = 99
	wantCoefProbs := e.coefProbs
	wantModeProbs := e.modeProbs

	rows := geometry.MacroblockRows(32)
	cols := geometry.MacroblockCols(32)
	attempt, err := e.encodeKeyFrameAttempt(make([]byte, 16384), sourceImageFromImage(rateControlTestFrame(32, 32, 0)), rows, cols, rows*cols, 0, false, false, e.rc.currentQuantizer)
	if err != nil {
		t.Fatalf("encodeKeyFrameAttempt returned error: %v", err)
	}
	if !attempt.RefreshEntropyProbs {
		t.Fatalf("key attempt RefreshEntropyProbs = false, want true")
	}
	if e.coefProbs != wantCoefProbs {
		t.Fatalf("encodeKeyFrameAttempt mutated coefficient probabilities before commit")
	}
	if e.modeProbs != wantModeProbs {
		t.Fatalf("encodeKeyFrameAttempt mutated mode probabilities before commit")
	}

	e.commitKeyFrameEntropy(attempt)
	if e.coefProbs != attempt.FrameCoefProbs {
		t.Fatalf("committed coefficient probabilities do not match accepted key attempt")
	}
	if e.coefProbsLast != attempt.FrameCoefProbs {
		t.Fatalf("keyframe LAST entropy snapshot does not match accepted key attempt")
	}
	if e.coefProbsGolden != vp8tables.DefaultCoefProbs {
		t.Fatalf("keyframe GOLDEN entropy snapshot changed, want default")
	}
	if e.coefProbsAltRef != attempt.FrameCoefProbs {
		t.Fatalf("keyframe ALTREF entropy snapshot does not match accepted key attempt")
	}
	if e.modeProbs == wantModeProbs {
		t.Fatalf("committed keyframe mode probabilities still match pre-commit sentinel")
	}
}

func TestEncodeInterFrameAttemptDefersSkipFalseCommit(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	if _, err := e.EncodeInto(make([]byte, 16384), first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	e.probSkipFalse = 91
	rows := geometry.MacroblockRows(32)
	cols := geometry.MacroblockCols(32)
	cyclicRefresh := newInterFrameCyclicRefreshRecodeState(e.rc.currentQuantizer)
	attempt, err := e.encodeInterFrameAttempt(make([]byte, 16384), sourceImageFromImage(second), rows, cols, rows*cols, 0, false, false, true, false, &cyclicRefresh, true, false)
	if err != nil {
		t.Fatalf("encodeInterFrameAttempt returned error: %v", err)
	}
	if e.probSkipFalse != 91 {
		t.Fatalf("inter attempt probSkipFalse = %d, want pre-attempt sentinel 91 before commit", e.probSkipFalse)
	}

	e.commitInterFrameAttempt(attempt, true)
	if e.probSkipFalse != attempt.Config.ProbSkipFalse {
		t.Fatalf("committed probSkipFalse = %d, want accepted attempt probability %d", e.probSkipFalse, attempt.Config.ProbSkipFalse)
	}
}

func TestRefFrameProbsFromUsageMirrorsLibvpxClamp(t *testing.T) {
	if _, _, _, ok := refFrameProbsFromUsage(0, 0, 0, 0); ok {
		t.Fatalf("empty ref usage returned ok=true, want false")
	}

	probIntra, probLast, probGolden, ok := refFrameProbsFromUsage(0, 0, 0, 4)
	if !ok {
		t.Fatalf("alt-only ref usage returned ok=false")
	}
	if probIntra != 1 || probLast != 1 || probGolden != 1 {
		t.Fatalf("alt-only probs = %d/%d/%d, want clamped 1/1/1", probIntra, probLast, probGolden)
	}

	probIntra, probLast, probGolden, ok = refFrameProbsFromUsage(1, 2, 1, 1)
	if !ok {
		t.Fatalf("mixed ref usage returned ok=false")
	}
	if probIntra != 51 || probLast != 127 || probGolden != 127 {
		t.Fatalf("mixed probs = %d/%d/%d, want 51/127/127", probIntra, probLast, probGolden)
	}
}

func TestCommitInterFrameEntropyRefreshesInterIntraModeProbs(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	vp8dec.ResetModeProbs(&e.modeProbs)
	original := e.modeProbs
	frameYModeProbs := vp8tables.DefaultYModeProbs
	frameYModeProbs[0] = 251
	frameUVModeProbs := vp8tables.DefaultUVModeProbs
	frameUVModeProbs[0] = 249
	frameMVProbs := vp8tables.DefaultMVContext
	frameMVProbs[0][0] = 99
	attempt := interFrameEncodeAttempt{
		Config:           vp8enc.InterFrameStateConfig{RefreshEntropyProbs: true},
		FrameCoefProbs:   e.coefProbs,
		FrameYModeProbs:  frameYModeProbs,
		FrameUVModeProbs: frameUVModeProbs,
		FrameMVProbs:     frameMVProbs,
	}

	e.commitInterFrameEntropy(attempt)

	if e.modeProbs.YMode != frameYModeProbs {
		t.Fatalf("committed Y mode probs = %v, want %v", e.modeProbs.YMode, frameYModeProbs)
	}
	if e.modeProbs.UVMode != frameUVModeProbs {
		t.Fatalf("committed UV mode probs = %v, want %v", e.modeProbs.UVMode, frameUVModeProbs)
	}
	if e.modeProbs.MV != frameMVProbs {
		t.Fatalf("committed MV probs = %v, want %v", e.modeProbs.MV, frameMVProbs)
	}

	e.modeProbs = original
	attempt.Config.RefreshEntropyProbs = false
	e.commitInterFrameEntropy(attempt)
	if e.modeProbs != original {
		t.Fatalf("mode probs changed on no-refresh commit: got %+v want %+v", e.modeProbs, original)
	}
}

func TestCommitInterFrameEntropyUpdatesMVCostTablesWithoutEntropyRefresh(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	vp8dec.ResetModeProbs(&e.modeProbs)
	base := e.modeProbs.MV
	staleCosts := base
	staleCosts[1][0] = 254
	e.mvCostTables.Build(&staleCosts)
	e.mvCostProbs = staleCosts
	e.mvCostTablesValid = true

	frameMVProbs := base
	frameMVProbs[0][0] = 1
	frameMVProbs[1][0] = 2
	attempt := interFrameEncodeAttempt{
		Config:           vp8enc.InterFrameStateConfig{RefreshEntropyProbs: false},
		FrameCoefProbs:   e.coefProbs,
		FrameYModeProbs:  e.modeProbs.YMode,
		FrameUVModeProbs: e.modeProbs.UVMode,
		FrameMVProbs:     frameMVProbs,
	}
	attempt.Config.MVUpdate[0][0] = true
	attempt.Config.MVUpdateCount = 1

	e.commitInterFrameEntropy(attempt)

	if e.modeProbs.MV != base {
		t.Fatalf("mode MV probs changed on no-refresh commit: got %v want %v", e.modeProbs.MV, base)
	}
	wantCosts := staleCosts
	wantCosts[0] = frameMVProbs[0]
	if e.mvCostProbs != wantCosts {
		t.Fatalf("MV cost probs = %v, want mixed stale/update table %v", e.mvCostProbs, wantCosts)
	}
	mv := vp8enc.MotionVector{Row: 32, Col: 32}
	got := e.currentMotionVectorCostTables().BitCost(mv, vp8enc.MotionVector{}, 128)
	want := vp8enc.MotionVectorBitCost(mv, vp8enc.MotionVector{}, &wantCosts, 128)
	if got != want {
		t.Fatalf("MV cost table cost = %d, want %d", got, want)
	}
}

func TestRDPickerCoefProbsSelectsLibvpxFrameContext(t *testing.T) {
	e := &VP8Encoder{}
	if got := e.rdPickerCoefProbs(false, false); got != nil {
		t.Fatalf("rdPickerCoefProbs before snapshot seed = %p, want nil", got)
	}

	e.coefProbsSnapshotsValid = true
	if got := e.rdPickerCoefProbs(false, false); got != &e.coefProbsLast {
		t.Fatalf("single-layer default context = %p, want coefProbsLast %p", got, &e.coefProbsLast)
	}
	if got := e.rdPickerCoefProbs(true, false); got != &e.coefProbsGolden {
		t.Fatalf("single-layer golden context = %p, want coefProbsGolden %p", got, &e.coefProbsGolden)
	}
	if got := e.rdPickerCoefProbs(false, true); got != &e.coefProbsAltRef {
		t.Fatalf("single-layer altref context = %p, want coefProbsAltRef %p", got, &e.coefProbsAltRef)
	}

	e.opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringOneLayer}
	if got := e.rdPickerCoefProbs(false, false); got != &e.coefProbsLast {
		t.Fatalf("one-layer temporal default context = %p, want coefProbsLast %p", got, &e.coefProbsLast)
	}
	if got := e.rdPickerCoefProbs(true, false); got != &e.coefProbsGolden {
		t.Fatalf("one-layer temporal golden context = %p, want coefProbsGolden %p", got, &e.coefProbsGolden)
	}

	e.opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}
	if got := e.rdPickerCoefProbs(false, false); got != &e.coefProbsLast {
		t.Fatalf("non-realtime temporal multilayer default context = %p, want coefProbsLast %p", got, &e.coefProbsLast)
	}
	if got := e.rdPickerCoefProbs(true, false); got != &e.coefProbsGolden {
		t.Fatalf("non-realtime temporal multilayer golden context = %p, want coefProbsGolden %p", got, &e.coefProbsGolden)
	}
	if got := e.rdPickerCoefProbs(false, true); got != &e.coefProbsAltRef {
		t.Fatalf("temporal multilayer altref context = %p, want coefProbsAltRef %p", got, &e.coefProbsAltRef)
	}

	e.opts.Deadline = DeadlineRealtime
	e.opts.CpuUsed = 0
	if got := e.rdPickerCoefProbs(false, false); got != &e.coefProbsLast {
		t.Fatalf("realtime temporal multilayer default context = %p, want coefProbsLast %p", got, &e.coefProbsLast)
	}
	if got := e.rdPickerCoefProbs(true, false); got != &e.coefProbsGolden {
		t.Fatalf("realtime temporal multilayer golden context = %p, want coefProbsGolden %p", got, &e.coefProbsGolden)
	}
	e.opts.CpuUsed = -3
	if got := e.rdPickerCoefProbs(true, false); got != &e.coefProbsGolden {
		t.Fatalf("cold pinned realtime temporal golden context = %p, want coefProbsGolden %p", got, &e.coefProbsGolden)
	}
	e.runtimePinnedCPUUsed = true
	if got := e.rdPickerCoefProbs(true, false); got != &e.coefProbsGolden {
		t.Fatalf("runtime pinned realtime temporal golden context = %p, want coefProbsGolden %p", got, &e.coefProbsGolden)
	}
}
