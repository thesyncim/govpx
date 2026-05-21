package encoder

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestMacroblockCoefficientsEmptyTreatsSkippedDCLumaAsEmpty(t *testing.T) {
	var coeffs MacroblockCoefficients
	for block := range 16 {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	if !MacroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("empty = false, want true for skipped-DC luma blocks")
	}

	coeffs.SetBlockEOB(0, 2)
	if MacroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("empty = true, want false for luma AC EOB")
	}

	coeffs.SetBlockEOB(0, 1)
	if !MacroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("whole-block empty = false, want true for luma DC carried by empty Y2")
	}
	if MacroblockCoefficientsEmpty(&coeffs, true) {
		t.Fatalf("4x4 empty = true, want false for luma DC coefficient")
	}
}

func TestStaticInterRDEncodeBreakoutUsesStrictLibvpxThreshold(t *testing.T) {
	src, pred := flatSourceAndReference(16, 16, 128, 90, 170)
	quant := breakoutTestQuant(16)

	src.Y[0] = 131
	if !StaticInterRDEncodeBreakout(src, &pred, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want skip below AC threshold")
	}

	src.Y[0] = 132
	if StaticInterRDEncodeBreakout(src, &pred, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want no skip at strict AC threshold")
	}
}

func TestStaticInterRDEncodeBreakoutUsesChromaGate(t *testing.T) {
	src, pred := flatSourceAndReference(16, 16, 128, 90, 170)
	quant := breakoutTestQuant(64)

	if !StaticInterRDEncodeBreakout(src, &pred, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want uniform low-residual block skipped")
	}

	src.U[0] = 110
	if StaticInterRDEncodeBreakout(src, &pred, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want chroma SSE to prevent skip")
	}
}

func TestStaticInterEncodeBreakoutUsesLibvpxChromaGates(t *testing.T) {
	src, ref := flatSourceAndReference(16, 16, 128, 90, 170)
	quant := breakoutTestQuant(128)
	mode := InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}

	src.U[0] = 100
	if StaticInterFastEncodeBreakout(src, &ref, 0, 0, &mode, &quant, 1, 0) {
		t.Fatalf("fast static breakout = true, want pickinter encode_breakout chroma gate to reject")
	}
	if !StaticInterRDEncodeBreakout(src, &ref, 0, 0, &quant, 1) {
		t.Fatalf("RD static breakout = false, want rdopt threshold chroma gate to accept low residual")
	}
}

func TestMacroblockErrorHelpersClampVisibleEdges(t *testing.T) {
	src, ref := edgeSourceAndReference()

	gotVar, gotSSE := MacroblockLumaVarianceSSE(src, &ref, 1, 1)
	wantVar, wantSSE := expectedLumaVarianceSSE(src, &ref, 1, 1)
	if gotVar != wantVar || gotSSE != wantSSE {
		t.Fatalf("edge luma variance/SSE = %d/%d, want %d/%d", gotVar, gotSSE, wantVar, wantSSE)
	}

	if got, want := MacroblockChromaSSE(src, &ref, 1, 1), expectedChromaSSE(src, &ref, 1, 1); got != want {
		t.Fatalf("edge chroma SSE = %d, want %d", got, want)
	}
}

func breakoutTestQuant(yAC int) MacroblockQuant {
	var quant MacroblockQuant
	quant.Y1.Dequant[1] = int16(yAC)
	quant.Y2.Dequant[0] = 16
	return quant
}

func flatSourceAndReference(width int, height int, y byte, u byte, v byte) (SourceImage, vp8common.Image) {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	src := SourceImage{
		Width:    width,
		Height:   height,
		UVWidth:  uvWidth,
		UVHeight: uvHeight,
		YStride:  width,
		UStride:  uvWidth,
		VStride:  uvWidth,
		Y:        make([]byte, width*height),
		U:        make([]byte, uvWidth*uvHeight),
		V:        make([]byte, uvWidth*uvHeight),
	}
	ref := vp8common.Image{
		Width:       width,
		Height:      height,
		CodedWidth:  width,
		CodedHeight: height,
		YStride:     width,
		UStride:     uvWidth,
		VStride:     uvWidth,
		Y:           make([]byte, width*height),
		U:           make([]byte, uvWidth*uvHeight),
		V:           make([]byte, uvWidth*uvHeight),
	}
	for i := range src.Y {
		src.Y[i] = y
		ref.Y[i] = y
	}
	for i := range src.U {
		src.U[i] = u
		src.V[i] = v
		ref.U[i] = u
		ref.V[i] = v
	}
	return src, ref
}

func edgeSourceAndReference() (SourceImage, vp8common.Image) {
	src := SourceImage{
		Width:    17,
		Height:   17,
		UVWidth:  9,
		UVHeight: 9,
		YStride:  17,
		UStride:  9,
		VStride:  9,
		Y:        make([]byte, 17*17),
		U:        make([]byte, 9*9),
		V:        make([]byte, 9*9),
	}
	ref := vp8common.Image{
		Width:       17,
		Height:      17,
		CodedWidth:  32,
		CodedHeight: 32,
		YStride:     32,
		UStride:     16,
		VStride:     16,
		Y:           make([]byte, 32*32),
		U:           make([]byte, 16*16),
		V:           make([]byte, 16*16),
	}
	for y := range src.Height {
		for x := range src.Width {
			src.Y[y*src.YStride+x] = byte(30 + y*3 + x)
		}
	}
	for y := range ref.CodedHeight {
		for x := range ref.CodedWidth {
			ref.Y[y*ref.YStride+x] = byte(20 + y*2 + x)
		}
	}
	for y := range src.UVHeight {
		for x := range src.UVWidth {
			src.U[y*src.UStride+x] = byte(80 + y + x*2)
			src.V[y*src.VStride+x] = byte(130 + y*2 + x)
		}
	}
	refUVHeight := (ref.CodedHeight + 1) >> 1
	refUVWidth := (ref.CodedWidth + 1) >> 1
	for y := range refUVHeight {
		for x := range refUVWidth {
			ref.U[y*ref.UStride+x] = byte(70 + y*3 + x)
			ref.V[y*ref.VStride+x] = byte(120 + y + x*3)
		}
	}
	return src, ref
}

func expectedLumaVarianceSSE(src SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := testClampCoord(baseY+row, src.Height)
		refY := testClampCoord(baseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := testClampCoord(baseX+col, src.Width)
			refX := testClampCoord(baseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

func expectedChromaSSE(src SourceImage, ref *vp8common.Image, mbRow int, mbCol int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	refUVWidth := (ref.CodedWidth + 1) >> 1
	refUVHeight := (ref.CodedHeight + 1) >> 1
	sse := 0
	for row := range 8 {
		srcY := testClampCoord(baseY+row, src.UVHeight)
		refY := testClampCoord(baseY+row, refUVHeight)
		for col := range 8 {
			srcX := testClampCoord(baseX+col, src.UVWidth)
			refX := testClampCoord(baseX+col, refUVWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(ref.U[refY*ref.UStride+refX])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(ref.V[refY*ref.VStride+refX])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

func testClampCoord(v int, limit int) int {
	if v < 0 {
		return 0
	}
	if v >= limit {
		return limit - 1
	}
	return v
}
