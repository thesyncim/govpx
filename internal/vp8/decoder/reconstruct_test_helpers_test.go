package decoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

// TestReconstructWholeMVInterMacroblockMVBaseOffsetMB0MV816 pins the
// MV->reference-plane base-offset arithmetic used by the encoder picker and
// the decoder grid path at MB(0,0) for the integer-pel motion vector
// MV=(row=8, col=16).
//
//	libvpx v1.16.0 vp8/common/reconinter.c:297-356 computes
//	  ptr = ptr_base + (mv_row >> 3) * pre_stride + (mv_col >> 3);
//	with ptr_base = pre.y_buffer + recon_yoffset (the per-MB origin, which
//	is mb_row*16*ystride + mb_col*16 at MB(0,0) => 0). For MV=(8,16):
//	  mv_row >> 3 = 1; mv_col >> 3 = 2 => ptr - pre_base = 1*stride + 2.
//	(mv_row | mv_col) & 7 == 0 so libvpx takes the vp8_copy_mem16x16 path.
//
//	govpx reconstructWholeMVInterMacroblockFast
//	(reconstruct_inter_fast.go:171-202) computes
//	  yRow = mbRow*16 + (mvRow >> 3); yCol = mbCol*16 + (mvCol >> 3)
//	  yOff = state.yOrigin + yRow*yStride + yCol
//	at xoff|yoff == 0 (no -2 adjust). For MB(0,0) MV=(8,16) this yields
//	  yOff = yOrigin + 1*yStride + 2  ===  libvpx ptr offset.
//
// The two engines compute the same base-offset; the predicted MB Y bytes
// must therefore equal ref.Y[1*YStride + 2 .. 1*YStride + 18] row-by-row.
// This test pins that invariant at the decoder API surface so a future
// regression in either the MV >> 3 fold, the mbRow*16/mbCol*16 anchor, or
// the yOrigin add surfaces as an explicit failure rather than as a silent
// picker-side residual divergence.
func TestReconstructWholeMVInterMacroblockMVBaseOffsetMB0MV816(t *testing.T) {
	const width, height = 64, 48
	fb, err := common.NewFrameBuffer(width, height, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer: %v", err)
	}
	fillImage(&fb.Img, testImage(width, height))
	fb.ExtendBorders()

	dst := blankImage(width, height)
	mode := MacroblockMode{
		Mode:        common.NewMV,
		RefFrame:    common.LastFrame,
		MV:          MotionVector{Row: 8, Col: 16},
		MBSkipCoeff: true,
	}
	var tokens MacroblockTokens
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch
	ok := ReconstructWholeMVInterMacroblock(
		&mode, &tokens, &dequants[0], &fb.Img,
		dst.Y, dst.YStride,
		dst.U, dst.UStride,
		dst.V, dst.VStride,
		&scratch.Residual, 0, 0, InterPredictionConfig{},
	)
	if !ok {
		t.Fatalf("ReconstructWholeMVInterMacroblock returned false for MB(0,0) MV=(8,16)")
	}

	// The reference plane address that libvpx's `ptr = ptr_base +
	// (mv_row >> 3)*pre_stride + (mv_col >> 3)` resolves to at MB(0,0).
	// Using YFull (the border-padded buffer) anchored at YOrigin is the
	// govpx mirror of `ptr_base = pre.y_buffer + recon_yoffset` with
	// recon_yoffset = 0.
	const expectedRowShift = 8 >> 3  // mv_row >> 3 = 1
	const expectedColShift = 16 >> 3 // mv_col >> 3 = 2
	if expectedRowShift != 1 || expectedColShift != 2 {
		t.Fatalf("static arithmetic guard: row>>3=%d col>>3=%d (want 1, 2)", expectedRowShift, expectedColShift)
	}
	refBase := fb.Img.YOrigin + expectedRowShift*fb.Img.YStride + expectedColShift

	// libvpx copy_mem16x16 path triggers when (mv_row|mv_col)&7 == 0.
	// Verify the predicted MB Y bytes equal the reference plane bytes
	// at the computed base offset, row by row.
	for row := range 16 {
		for col := range 16 {
			got := dst.Y[row*dst.YStride+col]
			want := fb.Img.YFull[refBase+row*fb.Img.YStride+col]
			if got != want {
				t.Fatalf("Y predictor diverges at (row=%d, col=%d): got=%d want=%d (ref offset %d = yOrigin %d + row*%d + col)",
					row, col, got, want,
					refBase+row*fb.Img.YStride+col, fb.Img.YOrigin, fb.Img.YStride)
			}
		}
	}

	// Chroma derivation pin: libvpx adjusts the MV by +1 (or -1 if
	// negative) before halving. MV=(8,16) is positive in both
	// components, so uv_mv = ((8+1)/2, (16+1)/2) = (4, 8). row>>3=0
	// col>>3=1, xoff=4 yoff=4, NOT the copy path. We don't pin the
	// sub-pel UV output here (covered by other tests) but the U/V
	// destination must have been written (the predictor returned ok=true
	// indicates the bounds check passed and the sixtap8x8 / bilinear8x8
	// path executed).
	uvAllZero := true
	for i := range dst.U[:8*dst.UStride] {
		if dst.U[i] != 0 {
			uvAllZero = false
			break
		}
	}
	if uvAllZero {
		t.Fatalf("U plane untouched after MV=(8,16) predict; chroma derivation regression")
	}
}

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
