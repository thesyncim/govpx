package govpx

import (
	"bytes"
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestMacroblockCornerGradientMatchesLibvpxFormula(t *testing.T) {
	// 16x16 plane with stride 16: every value 50 except the top-left corner pixel = 90.
	// Top-left corner (offRow=0, offCol=0, sgnRow=1, sgnCol=1) should yield max(|90-50|, ...) = 40.
	plane := make([]byte, 16*16)
	for i := range plane {
		plane[i] = 50
	}
	plane[0] = 90
	if got := macroblockCornerGradient(plane, 16, 0, 0, 1, 1); got != 40 {
		t.Fatalf("top-left gradient = %d, want 40", got)
	}
	// Flat plane: all corners should yield 0.
	for i := range plane {
		plane[i] = 50
	}
	if got := macroblockCornerGradient(plane, 16, 0, 15, 1, -1); got != 0 {
		t.Fatalf("flat top-right gradient = %d, want 0", got)
	}
}

func TestDotArtifactCornerCandidateYDetectsSharpRefAndFlatSrc(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	last := vp8common.FrameBuffer{}
	if err := last.Resize(16, 16, 16, 16); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// Flat last reference: not a candidate.
	for i := range last.Img.Y {
		last.Img.Y[i] = 128
	}
	if dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("flat last_ref should not be a dot-artifact candidate")
	}
	// Sharp gradient at top-left corner of last_ref: should be a candidate.
	last.Img.Y[0] = 200
	if !dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("sharp last_ref corner over flat src should be a candidate")
	}
	// If source also has sharp gradient, no longer a candidate.
	src.Y[0] = 200
	if dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("matching sharp source should suppress candidate")
	}
}

func TestCheckDotArtifactCandidateGatesOnLayerScreenContentAndConsecZeroLast(t *testing.T) {
	// Use a 64x64 encoder (16 MBs => cap = 16/10 = 1) so the cap is non-zero.
	e := newSizedTestEncoder(t, 64, 64)
	src := testImage(64, 64)
	fillImage(src, 128, 128, 128)
	for i := range e.lastRef.Img.Y {
		e.lastRef.Img.Y[i] = 128
	}
	// Sharp top-left corner of MB(0,0) on last_ref Y plane.
	e.lastRef.Img.Y[0] = 230

	// mvbias counter below threshold => not a candidate.
	e.consecZeroLastMVBias[0] = 5
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("low consec_zero_last_mvbias should not trigger dot-artifact bias")
	}
	if e.dotArtifactChecked[0] {
		t.Fatalf("ineligible MB should not set dotArtifactChecked")
	}
	// Above threshold => candidate; sets the per-MB checked flag.
	e.consecZeroLastMVBias[0] = 50
	e.mbsZeroLastDotSuppress = 0
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("high mvbias counter with sharp last_ref should be a candidate")
	}
	if !e.dotArtifactChecked[0] {
		t.Fatalf("eligible MB should set dotArtifactChecked")
	}
	if e.mbsZeroLastDotSuppress != 1 {
		t.Fatalf("mbsZeroLastDotSuppress = %d, want 1 after candidate", e.mbsZeroLastDotSuppress)
	}
	// Screen content disables it.
	e.mbsZeroLastDotSuppress = 0
	e.opts.ScreenContentMode = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("screen content should disable dot-artifact bias")
	}
	e.opts.ScreenContentMode = 0
	// Non-base layer disables it.
	e.currentTemporalLayer = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("non-base temporal layer should disable dot-artifact bias")
	}
	e.currentTemporalLayer = 0
	// Cap reached.
	e.mbsZeroLastDotSuppress = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("cap-reached suppression should disable further bias")
	}
}

func TestCheckDotArtifactCandidateChecksUVChannelsWhenYIsFlat(t *testing.T) {
	e := newSizedTestEncoder(t, 64, 64)
	src := testImage(64, 64)
	fillImage(src, 128, 128, 128)
	// Flat Y on last_ref so Y check returns false.
	for i := range e.lastRef.Img.Y {
		e.lastRef.Img.Y[i] = 128
	}
	// Sharp top-left corner on U plane only.
	for i := range e.lastRef.Img.U {
		e.lastRef.Img.U[i] = 128
	}
	for i := range e.lastRef.Img.V {
		e.lastRef.Img.V[i] = 128
	}
	e.lastRef.Img.U[0] = 230
	e.consecZeroLastMVBias[0] = 50
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("sharp U-plane corner should trigger dot-artifact bias when Y is flat")
	}
	// Reset and probe V plane only.
	e.lastRef.Img.U[0] = 128
	e.lastRef.Img.V[0] = 230
	e.mbsZeroLastDotSuppress = 0
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("sharp V-plane corner should trigger dot-artifact bias when Y/U are flat")
	}
}

func TestComputeSkin8x8BlockNeedsTwoSubBlocksToTrigger(t *testing.T) {
	// (Y=120, U=117, V=150) is a known skin tuple per
	// TestCyclicRefreshStaticClassificationMasksSkinBlocks. Build a 16x16
	// MB where exactly one 8x8 sub-block has the skin tuple and the other
	// three are neutral grey. SKIN_8X8 requires two skin sub-blocks =>
	// this MB is not skin.
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := range 8 {
		for col := range 8 {
			src.Y[row*src.YStride+col] = 120
		}
	}
	uvW := (src.Width + 1) >> 1
	uvH := (src.Height + 1) >> 1
	for row := range 4 {
		for col := range 4 {
			src.U[row*src.UStride+col] = 117
			src.V[row*src.VStride+col] = 150
		}
	}
	if computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 0) {
		t.Fatalf("single skin sub-block should not flag MB as skin under SKIN_8X8")
	}
	// Promote a second sub-block to skin colour: now MB qualifies.
	for row := range 8 {
		for col := 8; col < 16; col++ {
			src.Y[row*src.YStride+col] = 120
		}
	}
	for row := range 4 {
		for col := 4; col < 8; col++ {
			src.U[row*src.UStride+col] = 117
			src.V[row*src.VStride+col] = 150
		}
	}
	if !computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 0) {
		t.Fatalf("two skin sub-blocks should flag MB as skin under SKIN_8X8")
	}
	// Long zero-MV streak forces motion=0 and short-circuits past 60 frames.
	if computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 70) {
		t.Fatalf("consec_zero_last > 60 should suppress skin classification")
	}
}

func TestComputeSkinMapUsesSkin8x8ForSmallFramesAndSkin16x16ForLarge(t *testing.T) {
	makeSkinSrc := func(width int, height int) Image {
		src := testImage(width, height)
		// Y=120, U=117, V=150 is a known skin tuple.
		fillImage(src, 120, 117, 150)
		// Flip the top-left 8x8 Y sub-block of MB(0,0) to non-skin.
		for row := range 8 {
			for col := range 8 {
				src.Y[row*src.YStride+col] = 30
			}
		}
		return src
	}
	// Small frame: SKIN_8X8 with 3 of 4 sub-blocks skin classifies as skin.
	smallSrc := makeSkinSrc(16, 16)
	smallMap := make([]uint8, 1)
	computeSkinMap(sourceImageFromPublic(smallSrc), 1, 1, []uint8{0}, smallMap)
	if smallMap[0] != 1 {
		t.Fatalf("small-frame skin map = %d, want 1 (SKIN_8X8 path with majority skin sub-blocks)", smallMap[0])
	}
	// Width*Height > 352*288 selects SKIN_16X16. Use 384x288 (110592 > 101376).
	largeSrc := makeSkinSrc(384, 288)
	rows, cols := encoderMacroblockRows(288), encoderMacroblockCols(384)
	largeMap := make([]uint8, rows*cols)
	consec := make([]uint8, rows*cols)
	computeSkinMap(sourceImageFromPublic(largeSrc), rows, cols, consec, largeMap)
	if largeMap[0] != 1 {
		t.Fatalf("large-frame MB(0,0) skin map = %d, want 1 (SKIN_16X16 centre sample inside skin region)", largeMap[0])
	}
}

func TestUpdateConsecutiveZeroLastWithDotSuppressResetsCheckedMBs(t *testing.T) {
	counters := []uint8{40, 25}
	dotChecked := []bool{true, false}
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
	}
	updateConsecutiveZeroLastWithDotSuppress(modes, counters, dotChecked)
	if counters[0] != 0 {
		t.Fatalf("dot-checked counter[0] = %d, want reset to 0", counters[0])
	}
	if counters[1] != 26 {
		t.Fatalf("non-checked counter[1] = %d, want incremented to 26", counters[1])
	}
}

func TestSetActiveMapValidation(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	mapBytes := make([]byte, 4)
	for i := range mapBytes {
		mapBytes[i] = 1
	}
	if err := e.SetActiveMap(mapBytes, 1, 4); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-row SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes, 2, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-col SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes[:1], 2, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short-buffer SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes, 2, 2); err != nil {
		t.Fatalf("matching-size SetActiveMap error = %v", err)
	}
	if !e.activeMapEnabled {
		t.Fatalf("activeMapEnabled = false after SetActiveMap, want true")
	}
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("nil SetActiveMap error = %v", err)
	}
	if e.activeMapEnabled {
		t.Fatalf("activeMapEnabled = true after disabling, want false")
	}
}

func TestSetActiveMapInactiveInterMacroblocksAreSkippedZeroMVLast(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	// Distinct content per frame so inactive MBs would normally code residual.
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	keyPacket := make([]byte, 8192)
	keyResult, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	rows := encoderMacroblockRows(32)
	cols := encoderMacroblockCols(32)
	activeMap := make([]byte, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	// Mark a single MB inactive.
	inactiveRow, inactiveCol := 1, 0
	inactiveIndex := inactiveRow*cols + inactiveCol
	activeMap[inactiveIndex] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	interPacket := make([]byte, 8192)
	interResult, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	mode := e.interFrameModes[inactiveIndex]
	if mode.RefFrame != vp8common.LastFrame || mode.Mode != vp8common.ZeroMV || !mode.MBSkipCoeff {
		t.Fatalf("inactive MB mode = %+v, want skipped LAST/ZEROMV", mode)
	}
	if mode.MV != (vp8enc.MotionVector{}) {
		t.Fatalf("inactive MB MV = %+v, want zero", mode.MV)
	}
	if mode.SegmentID != 0 {
		t.Fatalf("inactive MB SegmentID = %d, want 0", mode.SegmentID)
	}
	if !e.interFrameModes[inactiveIndex].MBSkipCoeff {
		t.Fatalf("inactive MB MBSkipCoeff = false, want true")
	}
	decoded := decodeFrameSequence(t, keyResult.Data, interResult.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertMacroblockEqual(t, "inactive active-map MB", decoded[0], decoded[1], inactiveRow, inactiveCol)
	assertMacroblockDifferent(t, "neighboring active-map MB", decoded[0], decoded[1], 0, 1)
}

func TestSetActiveMapDisabledLeavesModeDecisionFree(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	rows := encoderMacroblockRows(32)
	cols := encoderMacroblockCols(32)
	activeMap := make([]byte, rows*cols)
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	// Disable: subsequent inter encode should not force any MB skip.
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("nil SetActiveMap returned error: %v", err)
	}
	keyPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(keyPacket, first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(interPacket, second, 1, 1, 0); err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	allSkipped := true
	for i := range e.interFrameModes {
		if !e.interFrameModes[i].MBSkipCoeff {
			allSkipped = false
			break
		}
	}
	if allSkipped {
		t.Fatalf("disabled active map still forced every MB to skip; want normal mode decision")
	}
}

func TestCyclicRefreshSegmentationConfigUsesAltLFUnderAggressiveDenoise(t *testing.T) {
	e := VP8Encoder{}
	e.cyclicRefreshConfigured = true
	e.rc.mode = RateControlCBR
	// Aggressive denoise (mode 3) brings consec_zerolast=15 and qp_thresh=80.
	// Pick Q below qp_thresh and frames_since_key past 2*consec_zerolast=30.
	e.opts.NoiseSensitivity = 3
	e.rc.currentQuantizer = 40
	e.rc.framesSinceKeyframe = 100
	cfg := e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("aggressive-denoise cyclic segmentation disabled, want enabled with alt-LF")
	}
	if cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] {
		t.Fatalf("aggressive-denoise alt-Q feature still set, want suppressed in favour of alt-LF")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltLF][staticSegmentID] {
		t.Fatalf("aggressive-denoise alt-LF feature = false, want enabled")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltLF][staticSegmentID]; got != -40 {
		t.Fatalf("aggressive-denoise alt-LF delta = %d, want libvpx -40", got)
	}

	// Q at or above qp_thresh: alt-Q path resumes.
	e.rc.currentQuantizer = 80
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if cfg.FeatureEnabled[vp8common.MBLvlAltLF][staticSegmentID] {
		t.Fatalf("Q>=qp_thresh alt-LF still set, want libvpx fallback to alt-Q delta")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] {
		t.Fatalf("Q>=qp_thresh alt-Q feature = false, want enabled")
	}

	// Too soon after keyframe: alt-Q path resumes too.
	e.rc.currentQuantizer = 40
	e.rc.framesSinceKeyframe = 10
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if cfg.FeatureEnabled[vp8common.MBLvlAltLF][staticSegmentID] {
		t.Fatalf("frames_since_key<=2*consec_zerolast alt-LF still set, want fallback to alt-Q")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] {
		t.Fatalf("frames_since_key<=2*consec_zerolast alt-Q feature = false, want enabled")
	}
}

func TestCyclicRefreshSegmentationConfigDisabledUnderForceMaxQuantizer(t *testing.T) {
	e := VP8Encoder{}
	e.cyclicRefreshConfigured = true
	e.rc.mode = RateControlCBR
	e.rc.currentQuantizer = 30
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("baseline CBR cyclic segmentation disabled, want enabled")
	}
	e.forceMaxQuantizer = true
	if cfg := e.cyclicRefreshSegmentationConfig(false); cfg.Enabled {
		t.Fatalf("force_maxqp cyclic segmentation = %+v, want disabled per libvpx force_maxqp gate", cfg)
	}
	e.forceMaxQuantizer = false
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("after clearing force_maxqp cyclic segmentation disabled, want enabled")
	}
}

func TestDropEncodedFrameOvershootReadsCurrentPredictionError(t *testing.T) {
	e := VP8Encoder{}
	e.opts.ScreenContentMode = 2
	e.rc.mode = RateControlCBR
	e.rc.dropFrameAllowed = true
	e.rc.currentQuantizer = 40
	e.rc.maxQuantizer = vp8common.MaxQ
	e.rc.bitsPerFrame = 8000
	e.rc.bufferOptimalBits = 16000
	e.rc.bufferLevelBits = 2000
	e.framePredictionError = int64((200<<4)+1) * 10
	e.lastPredErrorMB = 100

	if !e.vp8DropEncodedframeOvershoot(e.rc.currentQuantizer, 4000, 10, false) {
		t.Fatalf("overshoot drop = false, want true when current pred_err_mb crosses libvpx gates")
	}
	if !e.forceMaxQuantizer {
		t.Fatalf("forceMaxQuantizer = false, want true after overshoot drop")
	}
	if e.rc.bufferLevelBits != e.rc.bufferOptimalBits {
		t.Fatalf("buffer level = %d, want reset to optimal %d", e.rc.bufferLevelBits, e.rc.bufferOptimalBits)
	}
	if e.lastPredErrorMB != 100 {
		t.Fatalf("lastPredErrorMB changed inside drop helper to %d, want caller-owned value retained", e.lastPredErrorMB)
	}

	e = VP8Encoder{}
	e.opts.ScreenContentMode = 2
	e.opts.RTCExternalRateControl = true
	e.rc.mode = RateControlCBR
	e.rc.dropFrameAllowed = true
	e.rc.currentQuantizer = 40
	e.rc.maxQuantizer = vp8common.MaxQ
	e.rc.bitsPerFrame = 8000
	e.rc.bufferOptimalBits = 16000
	e.rc.bufferLevelBits = 2000
	e.framePredictionError = int64((200<<4)+1) * 10
	e.lastPredErrorMB = 100
	if e.vp8DropEncodedframeOvershoot(e.rc.currentQuantizer, 4000, 10, false) {
		t.Fatalf("RTC external rate-control overshoot drop = true, want disabled")
	}
}

func TestCyclicRefreshSegmentTransitionsClearOnNonZeroLast(t *testing.T) {
	// updateCyclicRefreshMapFromInterFrame is the per-MB segment-transition
	// recorder. After a frame:
	//   - Refreshed segment-1 MBs become -1 (cooldown).
	//   - Cooldown counters increment; ZEROMV-LAST flips a 1 to 0 (eligible).
	//   - Anything else sets the entry to 1 (dirty).
	refreshMap := []int8{-1, 1, 0, -1}
	modes := []vp8enc.InterFrameMacroblockMode{
		// MB0 was in segment 1 → final state -1
		{SegmentID: staticSegmentID, RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		// MB1 ZEROMV-LAST flips dirty→eligible
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		// MB2 NewMV last → dirty (1)
		{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV},
		// MB3 GOLDEN ZEROMV → dirty (1)
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}
	updateCyclicRefreshMapFromInterFrame(modes, refreshMap)
	want := []int8{-1, 0, 1, 1}
	for i := range want {
		if refreshMap[i] != want[i] {
			t.Fatalf("MB%d post-frame map = %d, want libvpx state %d", i, refreshMap[i], want[i])
		}
	}
}

// TestSetActiveMapOracleVectorPreservesEveryInactiveMB exercises a
// checkerboard active-map pattern and confirms libvpx's per-MB invariants
// across the whole frame: every inactive MB codes as ZEROMV-LAST with
// MBSkipCoeff=1 and segment 0, every inactive MB decodes back to the prior
// LAST reconstruction byte-for-byte, every active MB updates, and a second
// encode of the same source under the same active map is deterministic
// (decoder-stable). This is the active-map oracle vector for the
// single-threaded encodeframe path; govpx does not implement libvpx's
// row-threaded encodeframe loop so the threaded variant is N/A.
func TestSetActiveMapOracleVectorPreservesEveryInactiveMB(t *testing.T) {
	const width, height = 64, 64
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	first := testImage(width, height)
	second := testImage(width, height)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 80, 180)

	// Checkerboard active map: ~half MBs inactive across the frame, including
	// boundary positions, so token-context resets at MB edges are exercised.
	activeMap := make([]byte, rows*cols)
	for row := range rows {
		for col := range cols {
			if (row+col)%2 == 0 {
				activeMap[row*cols+col] = 0
			} else {
				activeMap[row*cols+col] = 1
			}
		}
	}

	encodeRun := func() ([]Image, []vp8enc.InterFrameMacroblockMode) {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
		}
		dst := make([]byte, 32*1024)
		key, err := e.EncodeInto(dst, first, 0, 1, 0)
		if err != nil {
			t.Fatalf("key EncodeInto returned error: %v", err)
		}
		keyData := append([]byte(nil), key.Data...)
		if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
			t.Fatalf("SetActiveMap returned error: %v", err)
		}
		inter, err := e.EncodeInto(dst, second, 1, 1, 0)
		if err != nil {
			t.Fatalf("inter EncodeInto returned error: %v", err)
		}
		interData := append([]byte(nil), inter.Data...)
		modes := append([]vp8enc.InterFrameMacroblockMode(nil), e.interFrameModes[:rows*cols]...)
		return decodeFrameSequence(t, keyData, interData), modes
	}

	decoded, modes := encodeRun()
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			if activeMap[index] == 0 {
				m := modes[index]
				if m.RefFrame != vp8common.LastFrame || m.Mode != vp8common.ZeroMV || !m.MBSkipCoeff || m.SegmentID != 0 {
					t.Fatalf("inactive MB(%d,%d) mode = %+v, want skipped LAST/ZEROMV in segment 0", row, col, m)
				}
				if m.MV != (vp8enc.MotionVector{}) {
					t.Fatalf("inactive MB(%d,%d) MV = %+v, want zero", row, col, m.MV)
				}
				assertMacroblockEqual(t, "active-map oracle inactive", decoded[0], decoded[1], row, col)
			} else {
				assertMacroblockDifferent(t, "active-map oracle active", decoded[0], decoded[1], row, col)
			}
		}
	}

	// Determinism: a second encode of the same source under the same active
	// map yields decoder-equivalent output (per-MB pixels match exactly).
	decoded2, modes2 := encodeRun()
	if len(decoded2) != 2 {
		t.Fatalf("second decoded frame count = %d, want 2", len(decoded2))
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			if modes2[index].RefFrame != modes[index].RefFrame || modes2[index].Mode != modes[index].Mode || modes2[index].MBSkipCoeff != modes[index].MBSkipCoeff || modes2[index].SegmentID != modes[index].SegmentID {
				t.Fatalf("MB(%d,%d) modes diverged across runs: first=%+v second=%+v", row, col, modes[index], modes2[index])
			}
			assertMacroblockEqual(t, "active-map oracle determinism", decoded[1], decoded2[1], row, col)
		}
	}
}

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
		if got := denoiserModeForSensitivity(c.level); got != c.wantMode {
			t.Fatalf("noise_sensitivity %d -> mode %d, want %d", c.level, got, c.wantMode)
		}
	}
}

func TestDenoiserSetParametersMatchesLibvpxModes(t *testing.T) {
	for _, mode := range []int{1, 2} {
		kind, params := denoiserSetParameters(mode)
		if mode == 1 && kind != denoiserOnYOnly {
			t.Fatalf("mode=1 kind = %d, want denoiserOnYOnly", kind)
		}
		if mode == 2 && kind != denoiserOnYUV {
			t.Fatalf("mode=2 kind = %d, want denoiserOnYUV", kind)
		}
		if params.scaleSSEThresh != 1 || params.scaleMotionThresh != 8 || params.scaleIncreaseFilter != 0 || params.denoiseMVBias != 95 || params.pickmodeMVBias != 100 || params.qpThresh != 0 {
			t.Fatalf("non-aggressive params for mode=%d = %+v, want libvpx defaults", mode, params)
		}
	}
	kind, params := denoiserSetParameters(3)
	if kind != denoiserOnYUVAggressive {
		t.Fatalf("mode=3 kind = %d, want denoiserOnYUVAggressive", kind)
	}
	if params.scaleSSEThresh != 2 || params.scaleMotionThresh != 16 || params.scaleIncreaseFilter != 1 || params.denoiseMVBias != 95 && params.denoiseMVBias != 60 || params.pickmodeMVBias != 75 || params.qpThresh != 80 || params.consecZeroLast != 15 {
		t.Fatalf("aggressive params = %+v, want libvpx aggressive defaults", params)
	}
}

func TestDenoiserFilterYReturnsCopyForSharpDifference(t *testing.T) {
	// Pixels where source and mc_running_avg differ by huge amounts: filter
	// should return COPY_BLOCK (sum_diff above threshold and delta>=4).
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		mc[i] = 250
	}
	for i := range sig {
		sig[i] = 0
	}
	if got := denoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false); got != denoiserCopyBlock {
		t.Fatalf("max-divergence filter decision = %d, want COPY_BLOCK", got)
	}
}

func TestDenoiserFilterYUsesMCWhenAbsdiffSmall(t *testing.T) {
	// |diff| <= 3 path: running_avg should be set to mc_running_avg, and the
	// filter should accept (FILTER_BLOCK).
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		mc[i] = 130
	}
	for i := range sig {
		sig[i] = 128
	}
	if got := denoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false); got != denoiserFilterBlock {
		t.Fatalf("small-diff filter decision = %d, want FILTER_BLOCK", got)
	}
	for i := range avg {
		if avg[i] != 130 {
			t.Fatalf("avg[%d] = %d, want 130 (mc value taken when |diff|<=3)", i, avg[i])
		}
	}
}

func TestDenoiserFilterUVCopiesNearNeutralBlocks(t *testing.T) {
	// 8x8 block where chroma is near 128 across the board: libvpx returns
	// COPY without filtering because |sum_block - 128*64| < threshold.
	mc := make([]byte, 8*8)
	avg := make([]byte, 8*8)
	sig := make([]byte, 8*8)
	for i := range sig {
		sig[i] = 128
	}
	if got := denoiserFilterUV(mc, 8, avg, 8, sig, 8, 0, false); got != denoiserCopyBlock {
		t.Fatalf("near-neutral UV filter = %d, want COPY_BLOCK", got)
	}
}

func TestDenoiserPickmodeMVBiasReturns75ForAggressiveMode(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.NoiseSensitivity = 0
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("denoiser-off bias = %d, want 100", got)
	}
	e.opts.NoiseSensitivity = 2
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("YUV mode bias = %d, want 100", got)
	}
	e.opts.NoiseSensitivity = 3
	if got := e.denoiserPickmodeMVBias(); got != 75 {
		t.Fatalf("aggressive bias = %d, want 75", got)
	}
}

func TestDenoiserReferenceTooOldMirrorsLibvpxRange(t *testing.T) {
	e := &VP8Encoder{}
	e.referenceFrameNumbers[vp8common.GoldenFrame] = 0
	e.referenceFrameNumbers[vp8common.AltRefFrame] = 1
	e.referenceFrameNumbers[vp8common.LastFrame] = 0

	e.frameCount = denoiserMaxGFARFRange
	if e.denoiserReferenceTooOld(vp8common.GoldenFrame) {
		t.Fatalf("GOLDEN ref at distance %d marked too old", denoiserMaxGFARFRange)
	}

	e.frameCount = denoiserMaxGFARFRange + 1
	if !e.denoiserReferenceTooOld(vp8common.GoldenFrame) {
		t.Fatalf("GOLDEN ref at distance %d not marked too old", denoiserMaxGFARFRange+1)
	}

	if e.denoiserReferenceTooOld(vp8common.LastFrame) {
		t.Fatalf("LAST ref should never be rejected by the GF/ARF denoiser age gate")
	}

	e.frameCount = denoiserMaxGFARFRange + 1
	if e.denoiserReferenceTooOld(vp8common.AltRefFrame) {
		t.Fatalf("ALTREF ref at distance %d marked too old", denoiserMaxGFARFRange)
	}
}

func TestDenoiserSkinGateUsesMVBiasCounter(t *testing.T) {
	e := &VP8Encoder{
		skinMap:              []uint8{1},
		consecZeroLast:       []uint8{0},
		consecZeroLastMVBias: []uint8{2},
	}
	if e.denoiserSkinGateBlocksFilter(0, 0, 1, 0, 0) {
		t.Fatalf("skin denoiser gate used regular zero-LAST counter, want mv-bias counter")
	}

	e.consecZeroLastMVBias[0] = 1
	if !e.denoiserSkinGateBlocksFilter(0, 0, 1, 0, 0) {
		t.Fatalf("skin denoiser gate did not block when mv-bias counter < 2")
	}

	e.consecZeroLastMVBias[0] = 2
	if !e.denoiserSkinGateBlocksFilter(0, 0, 1, 0, 1) {
		t.Fatalf("skin denoiser gate did not block non-zero motion")
	}
}

func TestDenoiserAvgForRefreshHonorsCopyBufferControls(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	e.opts.NoiseSensitivity = 2
	if err := e.denoiser.ensureAllocated(32, 32); err != nil {
		t.Fatalf("ensureAllocated: %v", err)
	}

	fillVP8Image(&e.denoiser.runningAvg[denoiserAvgIntra].Img, 33)
	fillVP8Image(&e.denoiser.runningAvg[denoiserAvgLast].Img, 11)
	fillVP8Image(&e.denoiser.runningAvg[denoiserAvgGolden].Img, 22)
	fillVP8Image(&e.denoiser.runningAvg[denoiserAvgAltRef].Img, 44)

	e.copyDenoiserAvgForRefresh(vp8enc.InterFrameStateConfig{
		CopyBufferToGolden: 1,
		CopyBufferToAltRef: 2,
	})

	intra := &e.denoiser.runningAvg[denoiserAvgIntra].Img
	last := &e.denoiser.runningAvg[denoiserAvgLast].Img
	golden := &e.denoiser.runningAvg[denoiserAvgGolden].Img
	alt := &e.denoiser.runningAvg[denoiserAvgAltRef].Img
	if !sameVP8Planes(golden, intra) {
		t.Fatalf("GOLDEN denoiser average did not follow CopyBufferToGolden")
	}
	if !sameVP8Planes(alt, intra) {
		t.Fatalf("ALTREF denoiser average did not follow CopyBufferToAltRef")
	}
	if last.Y[0] != 11 || last.U[0] != 11 || last.V[0] != 11 {
		t.Fatalf("LAST denoiser average changed without RefreshLast")
	}
	assertCodedBordersExtended(t, golden)
	assertCodedBordersExtended(t, alt)
}

func sameVP8Planes(a *vp8common.Image, b *vp8common.Image) bool {
	return bytes.Equal(a.Y, b.Y) && bytes.Equal(a.U, b.U) && bytes.Equal(a.V, b.V)
}
