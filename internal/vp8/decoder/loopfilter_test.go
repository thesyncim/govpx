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
		for x := range 4 {
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

func TestApplyLoopFilterPartialMatchesFullOnLumaWindow(t *testing.T) {
	const cols, rows = 4, 8
	full := newLoopFilterFrame(t, cols*16, rows*16)
	partial := newLoopFilterFrame(t, cols*16, rows*16)
	fillLoopFilterMacroblockColumns(&full.Img, 100, 110, 80, 90)
	fillLoopFilterMacroblockColumns(&partial.Img, 100, 110, 80, 90)
	// Tweak per-row content so each MB row has a distinct horizontal edge.
	for r := range rows {
		base := r * 16
		for y := base; y < base+16; y++ {
			row := y * full.Img.YStride
			for x := 0; x < full.Img.CodedWidth; x++ {
				v := byte(50 + (r*7+x)%80)
				full.Img.Y[row+x] = v
				partial.Img.Y[row+x] = v
			}
		}
	}

	modes := make([]MacroblockMode, rows*cols)
	for i := range modes {
		modes[i] = MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame}
	}
	header := LoopFilterHeader{Type: NormalLoopFilter, Level: 24}
	startRow, rowCount := rows/2, rows/8
	if rowCount == 0 {
		rowCount = 1
	}

	var fullLFI, partialLFI common.LoopFilterInfo
	if err := ApplyLoopFilter(&full.Img, rows, cols, modes, common.InterFrame, header, SegmentationHeader{}, &fullLFI); err != nil {
		t.Fatalf("ApplyLoopFilter returned error: %v", err)
	}
	if err := ApplyLoopFilterPartial(&partial.Img, rows, cols, modes, common.InterFrame, header, SegmentationHeader{}, &partialLFI, startRow, rowCount); err != nil {
		t.Fatalf("ApplyLoopFilterPartial returned error: %v", err)
	}

	// Compare only the inner rows of the partial window. The bottom 4 luma
	// lines of the last MB row in the window are touched by the next MB
	// row's mbh in the full-frame filter; the partial filter intentionally
	// stops at the window edge so they would not match.
	for r := startRow; r < startRow+rowCount; r++ {
		baseY := r * 16
		endY := baseY + 16
		if r == startRow+rowCount-1 {
			endY -= 4
		}
		for y := baseY; y < endY; y++ {
			fullRow := y * full.Img.YStride
			partRow := y * partial.Img.YStride
			for x := 0; x < full.Img.CodedWidth; x++ {
				if full.Img.Y[fullRow+x] != partial.Img.Y[partRow+x] {
					t.Fatalf("luma mismatch at row=%d col=%d: full=%d partial=%d", y, x, full.Img.Y[fullRow+x], partial.Img.Y[partRow+x])
				}
			}
		}
	}
}

func TestApplyLoopFilterPartialIgnoresChroma(t *testing.T) {
	const cols, rows = 2, 4
	fb := newLoopFilterFrame(t, cols*16, rows*16)
	fillLoopFilterMacroblockColumns(&fb.Img, 100, 110, 80, 90)
	uBefore := append([]byte(nil), fb.Img.U...)
	vBefore := append([]byte(nil), fb.Img.V...)
	modes := make([]MacroblockMode, rows*cols)
	for i := range modes {
		modes[i] = MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame}
	}
	header := LoopFilterHeader{Type: NormalLoopFilter, Level: 24}
	var lfi common.LoopFilterInfo

	if err := ApplyLoopFilterPartial(&fb.Img, rows, cols, modes, common.InterFrame, header, SegmentationHeader{}, &lfi, rows/2, 1); err != nil {
		t.Fatalf("ApplyLoopFilterPartial returned error: %v", err)
	}
	for i := range uBefore {
		if fb.Img.U[i] != uBefore[i] || fb.Img.V[i] != vBefore[i] {
			t.Fatalf("partial loop filter modified chroma at %d (u=%d/%d v=%d/%d)", i, uBefore[i], fb.Img.U[i], vBefore[i], fb.Img.V[i])
		}
	}
}

func TestApplyLoopFilterPartialZeroLevelNoop(t *testing.T) {
	const cols, rows = 2, 4
	fb := newLoopFilterFrame(t, cols*16, rows*16)
	fillLoopFilterMacroblockColumns(&fb.Img, 100, 110, 80, 90)
	modes := make([]MacroblockMode, rows*cols)
	var lfi common.LoopFilterInfo
	if err := ApplyLoopFilterPartial(&fb.Img, rows, cols, modes, common.InterFrame, LoopFilterHeader{Level: 0}, SegmentationHeader{}, &lfi, rows/2, 1); err != nil {
		t.Fatalf("ApplyLoopFilterPartial returned error: %v", err)
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
		for x := range 16 {
			img.Y[row+x] = leftY
		}
		for x := 16; x < img.CodedWidth; x++ {
			img.Y[row+x] = rightY
		}
	}
	uvWidth := (img.CodedWidth + 1) >> 1
	uvHeight := (img.CodedHeight + 1) >> 1
	for y := range uvHeight {
		uRow := y * img.UStride
		vRow := y * img.VStride
		for x := range 8 {
			img.U[uRow+x] = leftUV
			img.V[vRow+x] = leftUV
		}
		for x := 8; x < uvWidth; x++ {
			img.U[uRow+x] = rightUV
			img.V[vRow+x] = rightUV
		}
	}
}
