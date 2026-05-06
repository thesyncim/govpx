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
