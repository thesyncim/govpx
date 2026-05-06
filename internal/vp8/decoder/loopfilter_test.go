package decoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestApplyLoopFilterNormalFiltersMacroblockEdges(t *testing.T) {
	fb := newLoopFilterFrame(t, 32, 16)
	fillLoopFilterMacroblockColumns(&fb.Img, 100, 110, 80, 90)
	modes := []MacroblockMode{
		{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame},
		{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame},
	}
	var lfi common.LoopFilterInfo

	err := ApplyLoopFilter(&fb.Img, 1, 2, modes, common.KeyFrame, LoopFilterHeader{Type: NormalLoopFilter, Level: 20}, SegmentationHeader{}, &lfi)

	if err != nil {
		t.Fatalf("ApplyLoopFilter returned error: %v", err)
	}
	left := fb.Img.Y[15]
	right := fb.Img.Y[16]
	if left == 100 && right == 110 {
		t.Fatalf("Y macroblock edge was not filtered")
	}
	if !(left > 100 && right < 110) {
		t.Fatalf("Y edge = %d/%d, want values pulled toward each other", left, right)
	}
	uLeft := fb.Img.U[7]
	uRight := fb.Img.U[8]
	if !(uLeft > 80 && uRight < 90) {
		t.Fatalf("U edge = %d/%d, want chroma values pulled toward each other", uLeft, uRight)
	}
}

func TestApplyLoopFilterSimpleFiltersOnlyY(t *testing.T) {
	fb := newLoopFilterFrame(t, 32, 16)
	fillLoopFilterMacroblockColumns(&fb.Img, 100, 110, 80, 90)
	modes := []MacroblockMode{
		{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame},
		{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame},
	}
	var lfi common.LoopFilterInfo

	err := ApplyLoopFilter(&fb.Img, 1, 2, modes, common.KeyFrame, LoopFilterHeader{Type: SimpleLoopFilter, Level: 20}, SegmentationHeader{}, &lfi)

	if err != nil {
		t.Fatalf("ApplyLoopFilter returned error: %v", err)
	}
	left := fb.Img.Y[15]
	right := fb.Img.Y[16]
	if !(left > 100 && right < 110) {
		t.Fatalf("Y edge = %d/%d, want values pulled toward each other", left, right)
	}
	if fb.Img.U[7] != 80 || fb.Img.U[8] != 90 {
		t.Fatalf("simple filter changed U edge to %d/%d, want 80/90", fb.Img.U[7], fb.Img.U[8])
	}
}

func TestApplyLoopFilterSkipsInnerEdgesForSkippedMacroblock(t *testing.T) {
	fb := newLoopFilterFrame(t, 16, 16)
	for y := 0; y < fb.Img.CodedHeight; y++ {
		row := y * fb.Img.YStride
		for x := 0; x < 4; x++ {
			fb.Img.Y[row+x] = 100
		}
		for x := 4; x < fb.Img.CodedWidth; x++ {
			fb.Img.Y[row+x] = 110
		}
	}
	modes := []MacroblockMode{{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame, MBSkipCoeff: true}}
	var lfi common.LoopFilterInfo

	err := ApplyLoopFilter(&fb.Img, 1, 1, modes, common.KeyFrame, LoopFilterHeader{Type: NormalLoopFilter, Level: 20}, SegmentationHeader{}, &lfi)

	if err != nil {
		t.Fatalf("ApplyLoopFilter returned error: %v", err)
	}
	if fb.Img.Y[3] != 100 || fb.Img.Y[4] != 110 {
		t.Fatalf("skipped inner edge changed to %d/%d, want 100/110", fb.Img.Y[3], fb.Img.Y[4])
	}
}

func TestApplyLoopFilterRejectsSmallBuffers(t *testing.T) {
	var lfi common.LoopFilterInfo

	err := ApplyLoopFilter(&common.Image{Width: 16, Height: 16}, 1, 1, []MacroblockMode{{}}, common.KeyFrame, LoopFilterHeader{Level: 20}, SegmentationHeader{}, &lfi)

	if !errors.Is(err, ErrLoopFilterBufferTooSmall) {
		t.Fatalf("error = %v, want ErrLoopFilterBufferTooSmall", err)
	}
}

func TestApplyLoopFilterAllocatesZero(t *testing.T) {
	fb := newLoopFilterFrame(t, 32, 16)
	fillLoopFilterMacroblockColumns(&fb.Img, 100, 110, 80, 90)
	modes := []MacroblockMode{
		{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame},
		{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame},
	}
	header := LoopFilterHeader{Type: NormalLoopFilter, Level: 20}
	var lfi common.LoopFilterInfo

	allocs := testing.AllocsPerRun(1000, func() {
		_ = ApplyLoopFilter(&fb.Img, 1, 2, modes, common.KeyFrame, header, SegmentationHeader{}, &lfi)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkApplyLoopFilterNormal(b *testing.B) {
	fb := newLoopFilterFrame(b, 64, 64)
	fillLoopFilterMacroblockColumns(&fb.Img, 100, 110, 80, 90)
	modes := loopFilterBenchmarkModes(4, 4)
	header := LoopFilterHeader{Type: NormalLoopFilter, Level: 20}
	var lfi common.LoopFilterInfo

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ApplyLoopFilter(&fb.Img, 4, 4, modes, common.KeyFrame, header, SegmentationHeader{}, &lfi)
	}
}

func BenchmarkApplyLoopFilterSimple(b *testing.B) {
	fb := newLoopFilterFrame(b, 64, 64)
	fillLoopFilterMacroblockColumns(&fb.Img, 100, 110, 80, 90)
	modes := loopFilterBenchmarkModes(4, 4)
	header := LoopFilterHeader{Type: SimpleLoopFilter, Level: 20}
	var lfi common.LoopFilterInfo

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ApplyLoopFilter(&fb.Img, 4, 4, modes, common.KeyFrame, header, SegmentationHeader{}, &lfi)
	}
}

func loopFilterBenchmarkModes(rows int, cols int) []MacroblockMode {
	modes := make([]MacroblockMode, rows*cols)
	for i := range modes {
		modes[i] = MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame}
	}
	return modes
}

func newLoopFilterFrame(t testing.TB, width int, height int) *common.FrameBuffer {
	t.Helper()
	fb, err := common.NewFrameBuffer(width, height, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	return fb
}

func fillLoopFilterMacroblockColumns(img *common.Image, leftY byte, rightY byte, leftUV byte, rightUV byte) {
	for y := 0; y < img.CodedHeight; y++ {
		row := y * img.YStride
		for x := 0; x < 16; x++ {
			img.Y[row+x] = leftY
		}
		for x := 16; x < img.CodedWidth; x++ {
			img.Y[row+x] = rightY
		}
	}
	uvWidth := (img.CodedWidth + 1) >> 1
	uvHeight := (img.CodedHeight + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		uRow := y * img.UStride
		vRow := y * img.VStride
		for x := 0; x < 8; x++ {
			img.U[uRow+x] = leftUV
			img.V[vRow+x] = leftUV
		}
		for x := 8; x < uvWidth; x++ {
			img.U[uRow+x] = rightUV
			img.V[vRow+x] = rightUV
		}
	}
}
