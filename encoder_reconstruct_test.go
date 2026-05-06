package libgopx

import (
	"testing"

	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
)

var benchmarkInterReference interAnalysisReference
var benchmarkInterMV vp8enc.MotionVector
var benchmarkBool bool

func TestSelectInterFrameReferenceMotionVectorChoosesLowestCostReference(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 16, 16, 220, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	alt := testVP8Frame(t, 16, 16, 80, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)

	ref, mv := selectInterFrameReferenceMotionVector(source, refs[:], len(refs), 0, 0)

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

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 16}) {
		t.Fatalf("selection = %v %+v, want last row +16 from libvpx hex ring", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorRefinesDiamondCandidate(t *testing.T) {
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

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 24}) {
		t.Fatalf("selection = %v %+v, want last row +24 after diamond refinement", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorRefinesSubpixelCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	last := testVP8Frame(t, 32, 32, 40, 90, 170)
	for row := 0; row < last.Img.CodedHeight; row++ {
		for col := 0; col < last.Img.CodedWidth; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte(19 + ((row*17 + col*13) & 127))
		}
	}
	last.ExtendBorders()
	dsp.SixTapPredict16x16(last.Img.YFull[last.Img.YOrigin-2*last.Img.YStride-2:], last.Img.YStride, 2, 2, src.Y, src.YStride)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("selection = %v %+v, want last subpixel +2,+2", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorPrefersCheaperMotionOnTie(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 32, 32, 40, 90, 170)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	_, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0)

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

func TestMacroblockCoefficientsEmptyTreatsSkippedDCLumaAsEmpty(t *testing.T) {
	var coeffs vp8enc.MacroblockCoefficients
	for block := 0; block < 16; block++ {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	if !macroblockCoefficientsEmpty(&coeffs) {
		t.Fatalf("empty = false, want true for skipped-DC luma blocks")
	}

	coeffs.SetBlockEOB(0, 2)
	if macroblockCoefficientsEmpty(&coeffs) {
		t.Fatalf("empty = true, want false for luma AC EOB")
	}
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
		benchmarkBool = macroblockCoefficientsEmpty(&coeffs)
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
	b.SetBytes(16 * 16 * int64(len(refs)) * int64(len(interFrameMVCandidates)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col)
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
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col)
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
