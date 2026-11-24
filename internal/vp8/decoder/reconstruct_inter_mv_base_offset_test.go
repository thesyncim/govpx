package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
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
