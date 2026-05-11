package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func testMacroblockDequant() common.MacroblockDequant {
	var dequant common.MacroblockDequant
	for i := range 16 {
		dequant.Y1[i] = int16(5 + i)
		dequant.Y1DC[i] = int16(6 + i)
		dequant.Y2[i] = int16(4 + i)
		dequant.UV[i] = int16(6 + i)
	}
	dequant.Y1DC[0] = 1
	return dequant
}

func testMacroblockDequants() [common.MaxMBSegments]common.MacroblockDequant {
	var dequants [common.MaxMBSegments]common.MacroblockDequant
	for i := range dequants {
		dequants[i] = testMacroblockDequant()
	}
	return dequants
}

func wholeBlockResidualTokens() MacroblockTokens {
	var tokens MacroblockTokens
	for i := range 16 {
		tokens.EOB[i] = 1
	}
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	tokens.QCoeff[16][0] = 16
	tokens.EOB[16] = 1
	return tokens
}

func bpredResidualTokens() MacroblockTokens {
	var tokens MacroblockTokens
	tokens.QCoeff[0][0] = 16
	tokens.EOB[0] = 1
	return tokens
}

func bpredMacroblockMode(skip bool) MacroblockMode {
	mode := MacroblockMode{Mode: common.BPred, UVMode: common.DCPred, Is4x4: true, MBSkipCoeff: skip}
	for i := range mode.BModes {
		mode.BModes[i] = common.BTMPred
	}
	return mode
}

func fillSplitQuadrant(mode *MacroblockMode, quadrant int, mv MotionVector) {
	yBlock := (quadrant>>1)*8 + (quadrant&1)*2
	mode.BlockMV[yBlock] = mv
	mode.BlockMV[yBlock+1] = mv
	mode.BlockMV[yBlock+4] = mv
	mode.BlockMV[yBlock+5] = mv
}

func tmIntraPredictorRefs() IntraPredictorRefs {
	refs := testIntraPredictorRefs(0, 90, 70)
	refs.YAbove = make([]byte, 20)
	refs.YLeft = make([]byte, 16)
	for i := range refs.YAbove {
		refs.YAbove[i] = byte(50 + i)
	}
	for i := range refs.YLeft {
		refs.YLeft[i] = byte(70 + i)
	}
	refs.YTopLeft = 40
	return refs
}

func testIntraPredictorRefs(y byte, u byte, v byte) IntraPredictorRefs {
	return IntraPredictorRefs{
		YAbove:        filledPlane(20, 1, y),
		YLeft:         filledPlane(16, 1, y),
		UAbove:        filledPlane(8, 1, u),
		ULeft:         filledPlane(8, 1, u),
		VAbove:        filledPlane(8, 1, v),
		VLeft:         filledPlane(8, 1, v),
		YTopLeft:      y,
		UTopLeft:      u,
		VTopLeft:      v,
		UpAvailable:   true,
		LeftAvailable: true,
	}
}

func testImage(width int, height int) common.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := common.Image{
		Width:   width,
		Height:  height,
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
	}
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte((row*7 + col*3 + 1) & 0xff)
		}
	}
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte((row*5 + col*9 + 2) & 0xff)
			img.V[row*img.VStride+col] = byte((row*11 + col*13 + 3) & 0xff)
		}
	}
	return img
}

func fillImage(dst *common.Image, src common.Image) {
	for row := 0; row < dst.Height; row++ {
		copy(dst.Y[row*dst.YStride:row*dst.YStride+dst.Width], src.Y[row*src.YStride:row*src.YStride+dst.Width])
	}
	uvWidth := (dst.Width + 1) >> 1
	uvHeight := (dst.Height + 1) >> 1
	for row := range uvHeight {
		copy(dst.U[row*dst.UStride:row*dst.UStride+uvWidth], src.U[row*src.UStride:row*src.UStride+uvWidth])
		copy(dst.V[row*dst.VStride:row*dst.VStride+uvWidth], src.V[row*src.VStride:row*src.VStride+uvWidth])
	}
}

func blankImage(width int, height int) common.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return common.Image{
		Width:   width,
		Height:  height,
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
	}
}

func filledPlane(stride int, height int, value byte) []byte {
	plane := make([]byte, stride*height)
	for i := range plane {
		plane[i] = value
	}
	return plane
}

func assertPlaneValue(t *testing.T, name string, plane []byte, want byte) {
	t.Helper()
	for i, got := range plane {
		if got != want {
			t.Fatalf("%s[%d] = %d, want %d", name, i, got, want)
		}
	}
}

func assertSliceValue(t *testing.T, name string, got []byte, want byte) {
	t.Helper()
	for i, v := range got {
		if v != want {
			t.Fatalf("%s[%d] = %d, want %d", name, i, v, want)
		}
	}
}

func assertPlaneEqual(t *testing.T, name string, got []byte, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s len = %d, want %d", name, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %d, want %d", name, i, got[i], want[i])
		}
	}
}

func assertCopiedBlock(t *testing.T, name string, got []byte, gotStride int, want []byte, wantStride int, wantRow int, wantCol int, width int, height int) {
	t.Helper()
	for row := range height {
		for col := range width {
			gotValue := got[row*gotStride+col]
			wantValue := want[(wantRow+row)*wantStride+wantCol+col]
			if gotValue != wantValue {
				t.Fatalf("%s[%d,%d] = %d, want reference[%d,%d] %d", name, row, col, gotValue, wantRow+row, wantCol+col, wantValue)
			}
		}
	}
}
