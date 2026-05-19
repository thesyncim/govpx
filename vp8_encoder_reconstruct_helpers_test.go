package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func copyBPredBlockToSource(block []byte, blockStride int, dst Image, mbRow int, mbCol int, blockIndex int) {
	baseY := mbRow*16 + (blockIndex>>2)*4
	baseX := mbCol*16 + (blockIndex&3)*4
	for row := range 4 {
		copy(dst.Y[(baseY+row)*dst.YStride+baseX:], block[row*blockStride:row*blockStride+4])
	}
}

func testAltQSegmentation(segmentID uint8, qIndex int8) vp8enc.SegmentationConfig {
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true, AbsDelta: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][segmentID] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][segmentID] = qIndex
	return segmentation
}

func segmentedQuantizationTestImage() Image {
	img := testImage(32, 16)
	fillImage(img, 128, 128, 128)
	for row := 0; row < img.Height; row++ {
		for col := 16; col < img.Width; col++ {
			if (row+col)&1 == 0 {
				img.Y[row*img.YStride+col] = 16
			} else {
				img.Y[row*img.YStride+col] = 240
			}
		}
	}
	return img
}

func macroblockCoeffAbsSum(coeffs *vp8enc.MacroblockCoefficients) int {
	sum := 0
	for block := range coeffs.QCoeff {
		for _, coeff := range coeffs.QCoeff[block] {
			if coeff < 0 {
				sum -= int(coeff)
			} else {
				sum += int(coeff)
			}
		}
	}
	return sum
}

func BenchmarkMacroblockCoefficientsEmpty(b *testing.B) {
	var coeffs vp8enc.MacroblockCoefficients
	for block := range 16 {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkBool = macroblockCoefficientsEmpty(&coeffs, false)
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
	b.SetBytes(16 * 16 * int64(len(refs)) * int64(interFrameSubpixelSearchCandidateCount()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col, 4, 4, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)
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
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col, 4, 4, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)
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

func testMacroblockQuant(qIndex int) vp8enc.MacroblockQuant {
	var tables vp8common.FrameDequantTables
	var dequant vp8common.MacroblockDequant
	var quant vp8enc.MacroblockQuant
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &tables)
	vp8common.InitMacroblockDequant(&tables, qIndex, &dequant)
	vp8enc.InitFastMacroblockQuant(&dequant, &quant)
	return quant
}

func testRegularMacroblockQuant(tb testing.TB, qIndex int) vp8enc.MacroblockQuant {
	tb.Helper()
	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, vp8common.QuantDeltas{}, vp8enc.SegmentationConfig{}, &quants); err != nil {
		tb.Fatalf("InitSegmentMacroblockQuants returned error: %v", err)
	}
	return quants[0]
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

func copyShifted8x8FromReference(dst Image, ref *vp8common.Image, y int, x int, dy int, dx int) {
	copyShiftedBlockFromReference(dst, ref, y, x, 8, 8, dy, dx)
}

func splitMotionSourceAndReference(tb testing.TB) (Image, vp8common.FrameBuffer) {
	tb.Helper()
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	ref := testVP8Frame(tb, 32, 32, 0, 90, 170)
	for row := range 32 {
		for col := range 32 {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*17 + col*col*31 + row*col*7 + row*13 + col*29) & 255)
		}
	}
	return src, ref
}

func copyShiftedBlockFromReference(dst Image, ref *vp8common.Image, y int, x int, width int, height int, dy int, dx int) {
	for row := range height {
		for col := range width {
			dst.Y[(y+row)*dst.YStride+x+col] = ref.Y[(y+row+dy)*ref.YStride+x+col+dx]
		}
	}
}
