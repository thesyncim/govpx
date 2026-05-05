package libgopx

import (
	"testing"

	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
)

var benchmarkInterReference interAnalysisReference
var benchmarkInterMV vp8enc.MotionVector

func TestSelectInterFrameReferenceMotionVectorChoosesLowestSADReference(t *testing.T) {
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
	source := vp8enc.SourceImage{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}

	ref, mv := selectInterFrameReferenceMotionVector(source, refs[:], len(refs), 0, 0)

	if ref.Frame != vp8common.GoldenFrame || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("selection = %v %+v, want golden zero MV", ref.Frame, mv)
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
	source := vp8enc.SourceImage{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
	b.ReportAllocs()
	b.SetBytes(16 * 16 * int64(len(refs)) * int64(len(interFrameMVCandidates)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col)
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
