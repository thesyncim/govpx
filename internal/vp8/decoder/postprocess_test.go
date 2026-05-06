package decoder

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/common"
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
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			img.U[row*img.UStride+col] = byte(96 + ((row*5 + col*3) & 15))
			img.V[row*img.VStride+col] = byte(144 + ((row*3 + col*5) & 15))
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
