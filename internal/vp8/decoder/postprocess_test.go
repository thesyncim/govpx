package decoder

import (
	"bytes"
	"errors"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestApplyPostProcessFiltersOutputWithoutChangingSource(t *testing.T) {
	src := newPostProcessFrame(t, 32, 32)
	fillPostProcessPattern(&src.Img)
	src.ExtendBorders()
	var dst common.FrameBuffer
	if err := dst.Resize(32, 32, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	modes := postProcessModes(2, 2)
	scratch := make([]byte, 2*24)
	beforeY := append([]byte(nil), src.Img.Y...)

	if err := ApplyPostProcess(&src.Img, &dst, 2, 2, modes, 63, scratch); err != nil {
		t.Fatalf("ApplyPostProcess returned error: %v", err)
	}
	if !bytes.Equal(src.Img.Y, beforeY) {
		t.Fatalf("ApplyPostProcess changed source Y plane")
	}
	if bytes.Equal(dst.Img.Y, beforeY) {
		t.Fatalf("postprocessed Y plane equals source, want filtered output")
	}
}

func TestApplyPostProcessRejectsSmallBuffers(t *testing.T) {
	src := newPostProcessFrame(t, 16, 16)
	var dst common.FrameBuffer
	if err := dst.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}

	err := ApplyPostProcess(&src.Img, &dst, 1, 1, []MacroblockMode{{}}, 20, nil)

	if !errors.Is(err, ErrPostProcessBufferTooSmall) {
		t.Fatalf("error = %v, want ErrPostProcessBufferTooSmall", err)
	}
}

func TestApplyPostProcessHandlesSmallVisibleFrame(t *testing.T) {
	src := newPostProcessFrame(t, 1, 1)
	fillPostProcessPattern(&src.Img)
	src.ExtendBorders()
	var dst common.FrameBuffer
	if err := dst.Resize(1, 1, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	modes := postProcessModes(1, 1)
	scratch := make([]byte, 24)

	if err := ApplyPostProcess(&src.Img, &dst, 1, 1, modes, 63, scratch); err != nil {
		t.Fatalf("ApplyPostProcess returned error: %v", err)
	}
	if dst.Img.Width != 1 || dst.Img.Height != 1 || len(dst.Img.Y) == 0 {
		t.Fatalf("postprocessed image = %dx%d len %d, want populated 1x1", dst.Img.Width, dst.Img.Height, len(dst.Img.Y))
	}
}

func TestApplyPostProcessAllocatesZero(t *testing.T) {
	src := newPostProcessFrame(t, 32, 32)
	fillPostProcessPattern(&src.Img)
	src.ExtendBorders()
	var dst common.FrameBuffer
	if err := dst.Resize(32, 32, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	modes := postProcessModes(2, 2)
	scratch := make([]byte, 2*24)

	allocs := testing.AllocsPerRun(1000, func() {
		_ = ApplyPostProcess(&src.Img, &dst, 2, 2, modes, 63, scratch)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestPostProcessRandMatchesLibcSequences(t *testing.T) {
	tests := []struct {
		name   string
		flavor postProcessRandFlavor
		want   []int
	}{
		{
			name:   "glibc",
			flavor: postProcessRandFlavorGlibc,
			want:   []int{1804289383, 846930886, 1681692777, 1714636915, 1957747793, 424238335},
		},
		{
			name:   "darwin",
			flavor: postProcessRandFlavorMinStd,
			want:   []int{16807, 282475249, 1622650073, 984943658, 1144108930, 470211272},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rand postProcessRand
			rand.seed(postProcessNoiseSeed, tc.flavor)
			for i, want := range tc.want {
				if got := rand.next(); got != want {
					t.Fatalf("next[%d] = %d, want %d", i, got, want)
				}
			}
		})
	}
}

func TestApplyPostProcessWithOptionsAddsDeterministicLumaNoise(t *testing.T) {
	src := newPostProcessFrame(t, 32, 16)
	fillPostProcessPattern(&src.Img)
	src.ExtendBorders()
	var dstA common.FrameBuffer
	if err := dstA.Resize(32, 16, 32, 32); err != nil {
		t.Fatalf("Resize A returned error: %v", err)
	}
	var dstB common.FrameBuffer
	if err := dstB.Resize(32, 16, 32, 32); err != nil {
		t.Fatalf("Resize B returned error: %v", err)
	}
	modes := postProcessModes(1, 2)
	scratch := make([]byte, 2*24)
	opts := PostProcessOptions{AddNoise: true, NoiseLevel: 4}
	var stateA PostProcessState
	var stateB PostProcessState
	stateA.EnsureNoise(src.Img.Width)
	stateB.EnsureNoise(src.Img.Width)
	beforeY := append([]byte(nil), src.Img.Y...)
	beforeU := append([]byte(nil), src.Img.U...)
	beforeV := append([]byte(nil), src.Img.V...)

	if err := ApplyPostProcessWithOptions(&src.Img, &dstA, 1, 2, modes, 63, scratch, opts, &stateA); err != nil {
		t.Fatalf("ApplyPostProcessWithOptions A returned error: %v", err)
	}
	if err := ApplyPostProcessWithOptions(&src.Img, &dstB, 1, 2, modes, 63, scratch, opts, &stateB); err != nil {
		t.Fatalf("ApplyPostProcessWithOptions B returned error: %v", err)
	}

	if !bytes.Equal(src.Img.Y, beforeY) {
		t.Fatalf("ApplyPostProcessWithOptions changed source Y plane")
	}
	if bytes.Equal(dstA.Img.Y, beforeY) {
		t.Fatalf("noise postprocess left Y unchanged")
	}
	if !bytes.Equal(dstA.Img.Y, dstB.Img.Y) {
		t.Fatalf("fresh postprocess noise states produced different output")
	}
	if !bytes.Equal(dstA.Img.U, beforeU) || !bytes.Equal(dstA.Img.V, beforeV) {
		t.Fatalf("noise postprocess changed chroma planes")
	}
}

func TestApplyPostProcessWithOptionsRejectsMissingNoiseState(t *testing.T) {
	src := newPostProcessFrame(t, 16, 16)
	fillPostProcessPattern(&src.Img)
	var dst common.FrameBuffer
	if err := dst.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	err := ApplyPostProcessWithOptions(&src.Img, &dst, 1, 1, []MacroblockMode{{}}, 63, make([]byte, 24), PostProcessOptions{AddNoise: true}, nil)

	if !errors.Is(err, ErrPostProcessBufferTooSmall) {
		t.Fatalf("error = %v, want ErrPostProcessBufferTooSmall", err)
	}
}

func TestPostProcessNoiseClampsAtLumaExtremes(t *testing.T) {
	src := newPostProcessFrame(t, 16, 16)
	for row := 0; row < src.Img.CodedHeight; row++ {
		for col := 0; col < src.Img.CodedWidth; col++ {
			src.Img.Y[row*src.Img.YStride+col] = 0
		}
	}
	src.ExtendBorders()
	var dst common.FrameBuffer
	if err := dst.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	var state PostProcessState
	state.EnsureNoise(src.Img.Width)

	err := ApplyPostProcessWithOptions(&src.Img, &dst, 1, 1, []MacroblockMode{{}}, 63, make([]byte, 24), PostProcessOptions{AddNoise: true, NoiseLevel: 16}, &state)
	if err != nil {
		t.Fatalf("ApplyPostProcessWithOptions returned error: %v", err)
	}
	maxAllowed := byte(state.clamp * 2)
	for row := 0; row < dst.Img.Height; row++ {
		for col := 0; col < dst.Img.Width; col++ {
			if got := dst.Img.Y[row*dst.Img.YStride+col]; got > maxAllowed {
				t.Fatalf("Y[%d,%d] = %d, want <= %d after black clamp", row, col, got, maxAllowed)
			}
		}
	}
}

func TestApplyPostProcessWithOptionsAddNoiseAllocatesZero(t *testing.T) {
	src := newPostProcessFrame(t, 32, 16)
	fillPostProcessPattern(&src.Img)
	src.ExtendBorders()
	var dst common.FrameBuffer
	if err := dst.Resize(32, 16, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	var state PostProcessState
	state.EnsureNoise(src.Img.Width)
	modes := postProcessModes(1, 2)
	scratch := make([]byte, 2*24)
	opts := PostProcessOptions{AddNoise: true, NoiseLevel: 4}

	allocs := testing.AllocsPerRun(1000, func() {
		_ = ApplyPostProcessWithOptions(&src.Img, &dst, 1, 2, modes, 63, scratch, opts, &state)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestApplyPostProcessWithOptionsMFQEBlendsPreviousFrameOnQJump(t *testing.T) {
	prev := newPostProcessFrame(t, 16, 16)
	curr := newPostProcessFrame(t, 16, 16)
	fillPostProcessConstant(&prev.Img, 100, 80, 90)
	fillPostProcessConstant(&curr.Img, 103, 80, 90)
	prev.ExtendBorders()
	curr.ExtendBorders()
	var dst common.FrameBuffer
	if err := dst.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	var state PostProcessState
	modes := postProcessModes(1, 1)
	scratch := make([]byte, 24)
	first := PostProcessOptions{MFQE: true, BaseQIndex: 20, CurrentFrame: 10, KeyFrame: true}
	second := PostProcessOptions{MFQE: true, BaseQIndex: 60, CurrentFrame: 11, KeyFrame: true}

	if err := ApplyPostProcessWithOptions(&prev.Img, &dst, 1, 1, modes, 0, scratch, first, &state); err != nil {
		t.Fatalf("first ApplyPostProcessWithOptions returned error: %v", err)
	}
	if got := dst.Img.Y[0]; got != 100 {
		t.Fatalf("first Y = %d, want copied previous frame", got)
	}
	if err := ApplyPostProcessWithOptions(&curr.Img, &dst, 1, 1, modes, 0, scratch, second, &state); err != nil {
		t.Fatalf("second ApplyPostProcessWithOptions returned error: %v", err)
	}
	got := dst.Img.Y[0]
	if got <= 100 || got >= 103 {
		t.Fatalf("MFQE Y = %d, want blend between previous 100 and current 103", got)
	}
	if state.lastBaseQIndex != 30 {
		t.Fatalf("lastBaseQIndex = %d, want partial move to 30", state.lastBaseQIndex)
	}
}

func TestApplyPostProcessWithOptionsMFQECopiesHighMotionInterBlock(t *testing.T) {
	prev := newPostProcessFrame(t, 16, 16)
	curr := newPostProcessFrame(t, 16, 16)
	fillPostProcessConstant(&prev.Img, 100, 80, 90)
	fillPostProcessConstant(&curr.Img, 112, 84, 94)
	prev.ExtendBorders()
	curr.ExtendBorders()
	var dst common.FrameBuffer
	if err := dst.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	var state PostProcessState
	modes := []MacroblockMode{{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 16}}}
	scratch := make([]byte, 24)

	if err := ApplyPostProcessWithOptions(&prev.Img, &dst, 1, 1, modes, 0, scratch, PostProcessOptions{MFQE: true, BaseQIndex: 20, CurrentFrame: 10}, &state); err != nil {
		t.Fatalf("first ApplyPostProcessWithOptions returned error: %v", err)
	}
	if err := ApplyPostProcessWithOptions(&curr.Img, &dst, 1, 1, modes, 0, scratch, PostProcessOptions{MFQE: true, BaseQIndex: 60, CurrentFrame: 11}, &state); err != nil {
		t.Fatalf("second ApplyPostProcessWithOptions returned error: %v", err)
	}
	if got := dst.Img.Y[0]; got != 112 {
		t.Fatalf("high-motion MFQE Y = %d, want current frame copy", got)
	}
	if got := dst.Img.U[0]; got != 84 {
		t.Fatalf("high-motion MFQE U = %d, want current frame copy", got)
	}
}

func TestPostProcessQUsesVP9FormulaWhenVP9Flagged(t *testing.T) {
	// libvpx vp9_post_proc_frame: q = min(105, filter_level * 2).
	cases := []struct {
		filterLevel int
		vp9         bool
		want        int
	}{
		{filterLevel: 0, vp9: true, want: 0},
		{filterLevel: 20, vp9: true, want: 40},
		{filterLevel: 50, vp9: true, want: 100},
		{filterLevel: 63, vp9: true, want: 105},
		{filterLevel: 80, vp9: true, want: 105},
		{filterLevel: 0, vp9: false, want: 0},
		{filterLevel: 20, vp9: false, want: 33},
		{filterLevel: 50, vp9: false, want: 63},
		{filterLevel: 63, vp9: false, want: 63},
	}
	for _, tc := range cases {
		got := postProcessQ(tc.filterLevel, tc.vp9)
		if got != tc.want {
			t.Errorf("postProcessQ(fl=%d vp9=%v) = %d, want %d",
				tc.filterLevel, tc.vp9, got, tc.want)
		}
	}
}

func TestShouldApplyMFQEHonorsVP9Threshold(t *testing.T) {
	// libvpx vp9_post_proc_frame triggers MFQE when current_video_frame >= 2,
	// last_base_qindex <= 170, and base_qindex - last_base_qindex >= 20.
	// VP8 keeps the stricter last_base_qindex < 60 and current > 10 gate.
	state := PostProcessState{lastFrameValid: true, lastBaseQIndex: 100}
	vp9 := PostProcessOptions{MFQE: true, VP9: true, BaseQIndex: 130, CurrentFrame: 3}
	if !shouldApplyMFQE(vp9, &state) {
		t.Fatalf("VP9 MFQE should trigger at last=100 cur=130 frame=3")
	}
	vp8 := PostProcessOptions{MFQE: true, BaseQIndex: 130, CurrentFrame: 3}
	if shouldApplyMFQE(vp8, &state) {
		t.Fatalf("VP8 MFQE must not trigger when last_base_qindex>=60")
	}
	stateLow := PostProcessState{lastFrameValid: true, lastBaseQIndex: 40}
	vp8 = PostProcessOptions{MFQE: true, BaseQIndex: 80, CurrentFrame: 11}
	if !shouldApplyMFQE(vp8, &stateLow) {
		t.Fatalf("VP8 MFQE should trigger at last=40 cur=80 frame=11")
	}
	vp9EarlyFrame := PostProcessOptions{MFQE: true, VP9: true, BaseQIndex: 80, CurrentFrame: 1}
	if shouldApplyMFQE(vp9EarlyFrame, &stateLow) {
		t.Fatalf("VP9 MFQE must not trigger before current_video_frame >= 2")
	}
	vp9TooClose := PostProcessOptions{MFQE: true, VP9: true, BaseQIndex: 55, CurrentFrame: 3}
	if shouldApplyMFQE(vp9TooClose, &stateLow) {
		t.Fatalf("VP9 MFQE must require qcurr-qprev >= 20")
	}
	vp9HighPrev := PostProcessOptions{MFQE: true, VP9: true, BaseQIndex: 220, CurrentFrame: 3}
	stateHigh := PostProcessState{lastFrameValid: true, lastBaseQIndex: 180}
	if shouldApplyMFQE(vp9HighPrev, &stateHigh) {
		t.Fatalf("VP9 MFQE must reject last_base_qindex > 170")
	}
}

func TestQualifyInterMFQESplitMVChecksEachSubblock(t *testing.T) {
	mode := MacroblockMode{Mode: common.SplitMV}
	mode.BlockMV[0] = MotionVector{Row: 3}
	mode.BlockMV[2] = MotionVector{Col: 3}
	mode.BlockMV[8] = MotionVector{Row: 4}
	mode.BlockMV[10] = MotionVector{Col: 4}

	var got [4]int
	if total := qualifyInterMFQEMacroblock(&mode, &got); total != 0 {
		t.Fatalf("split-MV MFQE map = %v total=%d, want all rejected", got, total)
	}
}

func TestMultiframeQualityEnhanceBlockLargeSizesBlendStationaryContent(t *testing.T) {
	// VP9 SB-aware MFQE walker calls MultiframeQualityEnhanceBlock
	// with blockSize 32 and 64. The kernel must blend the previous
	// frame into the dst when activity and SAD stay well below the
	// thresholds.
	cases := []int{32, 64}
	for _, blockSize := range cases {
		blockSize := blockSize
		t.Run("size_"+itoaTestSize(blockSize), func(t *testing.T) {
			prev := newPostProcessFrame(t, blockSize, blockSize)
			curr := newPostProcessFrame(t, blockSize, blockSize)
			fillPostProcessConstant(&prev.Img, 100, 80, 90)
			fillPostProcessConstant(&curr.Img, 103, 80, 90)
			prev.ExtendBorders()
			curr.ExtendBorders()
			MultiframeQualityEnhanceBlock(blockSize, 60, 20,
				prev.Img.Y, prev.Img.U, prev.Img.V,
				prev.Img.YStride, prev.Img.UStride,
				curr.Img.Y, curr.Img.U, curr.Img.V,
				curr.Img.YStride, curr.Img.UStride)
			// Stationary input → kernel should blend prev=100 into
			// curr=103 instead of holding curr unchanged. Without
			// the kernel doing anything we'd get 103; with a pure
			// copy we'd get 100. Either way must change something
			// in dst; the blended result lies strictly between.
			got := curr.Img.Y[0]
			if got == 103 {
				t.Fatalf("MFQE blockSize=%d did not touch luma (got %d, want blended)", blockSize, got)
			}
			if got < 100 || got > 103 {
				t.Fatalf("MFQE blockSize=%d luma = %d, want [100, 103]", blockSize, got)
			}
		})
	}
}

func TestMultiframeQualityEnhanceBlockLargeSizesCopyHighSAD(t *testing.T) {
	// High SAD between prev and curr should make the kernel fall
	// through the activity test and copy the previous frame onto
	// dst (preserving the lower-q reference).
	const blockSize = 32
	prev := newPostProcessFrame(t, blockSize, blockSize)
	curr := newPostProcessFrame(t, blockSize, blockSize)
	fillPostProcessConstant(&prev.Img, 60, 80, 90)
	fillPostProcessConstant(&curr.Img, 200, 80, 90) // 140 LSB delta → huge SAD
	prev.ExtendBorders()
	curr.ExtendBorders()
	MultiframeQualityEnhanceBlock(blockSize, 60, 20,
		prev.Img.Y, prev.Img.U, prev.Img.V,
		prev.Img.YStride, prev.Img.UStride,
		curr.Img.Y, curr.Img.U, curr.Img.V,
		curr.Img.YStride, curr.Img.UStride)
	got := curr.Img.Y[0]
	if got != 60 {
		t.Fatalf("MFQE high-SAD blockSize=%d luma = %d, want previous-frame copy (60)", blockSize, got)
	}
}

func TestCopyMFQEBlockCopiesSourcePlanes(t *testing.T) {
	const blockSize = 16
	prev := newPostProcessFrame(t, blockSize, blockSize)
	curr := newPostProcessFrame(t, blockSize, blockSize)
	fillPostProcessConstant(&prev.Img, 70, 60, 50)
	fillPostProcessConstant(&curr.Img, 200, 200, 200)
	CopyMFQEBlock(blockSize,
		prev.Img.Y, prev.Img.U, prev.Img.V,
		prev.Img.YStride, prev.Img.UStride,
		curr.Img.Y, curr.Img.U, curr.Img.V,
		curr.Img.YStride, curr.Img.UStride)
	if curr.Img.Y[0] != 70 {
		t.Fatalf("CopyMFQEBlock did not copy Y: got %d, want 70", curr.Img.Y[0])
	}
	if curr.Img.U[0] != 60 || curr.Img.V[0] != 50 {
		t.Fatalf("CopyMFQEBlock did not copy UV: U=%d V=%d, want 60/50", curr.Img.U[0], curr.Img.V[0])
	}
}

// MFQEOverride must be invoked when MFQE is engaged. The injected
// callback receives the same qcurr/qprev pair the default walker
// would. We use the callback to stamp a known sentinel value into
// dst.Y so the rest of the postprocess chain is bypassed (no deblock
// in this test) and the sentinel surfaces verbatim.
func TestApplyPostProcessWithOptionsMFQEOverrideRunsCallback(t *testing.T) {
	prev := newPostProcessFrame(t, 16, 16)
	curr := newPostProcessFrame(t, 16, 16)
	fillPostProcessConstant(&prev.Img, 100, 80, 90)
	fillPostProcessConstant(&curr.Img, 130, 80, 90)
	prev.ExtendBorders()
	curr.ExtendBorders()
	var dst common.FrameBuffer
	if err := dst.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	var state PostProcessState
	modes := postProcessModes(1, 1)
	scratch := make([]byte, 24)
	first := PostProcessOptions{MFQE: true, BaseQIndex: 20, CurrentFrame: 10, KeyFrame: true}
	if err := ApplyPostProcessWithOptions(&prev.Img, &dst, 1, 1, modes, 0, scratch, first, &state); err != nil {
		t.Fatalf("warmup ApplyPostProcessWithOptions returned error: %v", err)
	}

	called := 0
	var sawKey bool
	var sawQcurr, sawQprev int
	override := MFQEWalker(func(src *common.Image, dstImg *common.Image, keyFrame bool, qcurr int, qprev int) {
		called++
		sawKey = keyFrame
		sawQcurr = qcurr
		sawQprev = qprev
		// Stamp a sentinel into dst so the test can confirm the
		// override actually drives the output.
		dstImg.Y[0] = 211
	})
	second := PostProcessOptions{
		MFQE:         true,
		BaseQIndex:   60,
		CurrentFrame: 11,
		KeyFrame:     false,
		MFQEOverride: override,
	}
	if err := ApplyPostProcessWithOptions(&curr.Img, &dst, 1, 1, modes, 0, scratch, second, &state); err != nil {
		t.Fatalf("override ApplyPostProcessWithOptions returned error: %v", err)
	}
	if called != 1 {
		t.Fatalf("override callback called %d times, want 1", called)
	}
	if sawKey {
		t.Fatalf("override callback saw keyFrame=true, want false")
	}
	if sawQcurr != 60 || sawQprev != 20 {
		t.Fatalf("override callback saw qcurr=%d qprev=%d, want 60/20", sawQcurr, sawQprev)
	}
	if dst.Img.Y[0] != 211 {
		t.Fatalf("override sentinel did not reach dst.Y[0]: got %d", dst.Img.Y[0])
	}
}

func itoaTestSize(n int) string {
	return strconv.Itoa(n)
}

func newPostProcessFrame(t testing.TB, width int, height int) *common.FrameBuffer {
	t.Helper()
	fb, err := common.NewFrameBuffer(width, height, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	return fb
}

func fillPostProcessPattern(img *common.Image) {
	for row := 0; row < img.CodedHeight; row++ {
		for col := 0; col < img.CodedWidth; col++ {
			img.Y[row*img.YStride+col] = byte(80 + ((row*7 + col*11) & 31))
		}
	}
	uvWidth := (img.CodedWidth + 1) >> 1
	uvHeight := (img.CodedHeight + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*5 + col*3) & 15))
			img.V[row*img.VStride+col] = byte(144 + ((row*3 + col*5) & 15))
		}
	}
}

func fillPostProcessConstant(img *common.Image, y byte, u byte, v byte) {
	for row := 0; row < img.CodedHeight; row++ {
		for col := 0; col < img.CodedWidth; col++ {
			img.Y[row*img.YStride+col] = y
		}
	}
	uvWidth := (img.CodedWidth + 1) >> 1
	uvHeight := (img.CodedHeight + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = u
			img.V[row*img.VStride+col] = v
		}
	}
}

func postProcessModes(rows int, cols int) []MacroblockMode {
	modes := make([]MacroblockMode, rows*cols)
	for i := range modes {
		modes[i] = MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame}
	}
	return modes
}
