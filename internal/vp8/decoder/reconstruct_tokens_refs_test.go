package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestTransformMacroblockTokens4x4YAndUV(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[0][0] = 2
	tokens.EOB[0] = 1
	tokens.QCoeff[16][1] = -3
	tokens.EOB[16] = 2
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	TransformMacroblockTokens(&tokens, &dequant, true, &residual)

	if got := residual.Block(0)[0]; got != 10 {
		t.Fatalf("Y block DC = %d, want 10", got)
	}
	if got := residual.Block(16)[1]; got != -21 {
		t.Fatalf("UV block AC = %d, want -21", got)
	}
}

func TestBuildIntraPredictorRefsTopLeftDefaults(t *testing.T) {
	img := testImage(32, 32)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefs(&img, 0, 0, &scratch)

	if refs.UpAvailable || refs.LeftAvailable {
		t.Fatalf("availability = %v/%v, want false/false", refs.UpAvailable, refs.LeftAvailable)
	}
	assertSliceValue(t, "YAbove", refs.YAbove, 127)
	assertSliceValue(t, "YLeft", refs.YLeft, 129)
	assertSliceValue(t, "UAbove", refs.UAbove, 127)
	assertSliceValue(t, "ULeft", refs.ULeft, 129)
	if refs.YTopLeft != 127 || refs.UTopLeft != 127 || refs.VTopLeft != 127 {
		t.Fatalf("top-left defaults = %d/%d/%d, want 127", refs.YTopLeft, refs.UTopLeft, refs.VTopLeft)
	}
}

func TestBuildIntraPredictorRefsInteriorSamples(t *testing.T) {
	img := testImage(48, 48)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefs(&img, 1, 1, &scratch)

	if !refs.UpAvailable || !refs.LeftAvailable {
		t.Fatalf("availability = %v/%v, want true/true", refs.UpAvailable, refs.LeftAvailable)
	}
	for i := range 20 {
		want := img.Y[15*img.YStride+16+i]
		if got := refs.YAbove[i]; got != want {
			t.Fatalf("YAbove[%d] = %d, want %d", i, got, want)
		}
	}
	for i := range 16 {
		want := img.Y[(16+i)*img.YStride+15]
		if got := refs.YLeft[i]; got != want {
			t.Fatalf("YLeft[%d] = %d, want %d", i, got, want)
		}
	}
	if got, want := refs.YTopLeft, img.Y[15*img.YStride+15]; got != want {
		t.Fatalf("YTopLeft = %d, want %d", got, want)
	}
	for i := range 8 {
		if got, want := refs.UAbove[i], img.U[7*img.UStride+8+i]; got != want {
			t.Fatalf("UAbove[%d] = %d, want %d", i, got, want)
		}
		if got, want := refs.VLeft[i], img.V[(8+i)*img.VStride+7]; got != want {
			t.Fatalf("VLeft[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestBuildIntraPredictorRefsEdgesFillSyntheticSamples(t *testing.T) {
	img := testImage(18, 18)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefs(&img, 1, 1, &scratch)

	if got, want := refs.YAbove[0], img.Y[15*img.YStride+16]; got != want {
		t.Fatalf("YAbove[0] = %d, want %d", got, want)
	}
	for i := 2; i < len(refs.YAbove); i++ {
		if got := refs.YAbove[i]; got != 127 {
			t.Fatalf("YAbove[%d] = %d, want synthetic 127", i, got)
		}
	}
	if got, want := refs.YLeft[0], img.Y[16*img.YStride+15]; got != want {
		t.Fatalf("YLeft[0] = %d, want %d", got, want)
	}
	for i := 2; i < len(refs.YLeft); i++ {
		if got := refs.YLeft[i]; got != 129 {
			t.Fatalf("YLeft[%d] = %d, want synthetic 129", i, got)
		}
	}
}

func TestBuildIntraPredictorRefsUsesExtendedRightEdge(t *testing.T) {
	fb, err := common.NewFrameBuffer(32, 32, 8, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	img := &fb.Img
	img.Y[14*img.YStride+31] = 44
	img.Y[15*img.YStride+31] = 55

	extendIntraRightEdgeForRow(img, 0)

	for i := range 4 {
		if got := img.YFull[img.YOrigin+14*img.YStride+32+i]; got != 44 {
			t.Fatalf("extended row 14 right[%d] = %d, want 44", i, got)
		}
		if got := img.YFull[img.YOrigin+15*img.YStride+32+i]; got != 55 {
			t.Fatalf("extended row 15 right[%d] = %d, want 55", i, got)
		}
	}

	var scratch IntraPredictorScratch
	refs := BuildIntraPredictorRefs(img, 1, 1, &scratch)
	for i := 16; i < 20; i++ {
		if got := refs.YAbove[i]; got != 55 {
			t.Fatalf("YAbove[%d] = %d, want extended right edge 55", i, got)
		}
	}
}

func TestBuildIntraPredictorRefsLumaMatchesFullLumaRefs(t *testing.T) {
	interior := testImage(48, 48)
	topLeft := testImage(32, 32)
	syntheticEdge := testImage(18, 18)
	fb, err := common.NewFrameBuffer(32, 32, 8, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	for row := range fb.Img.CodedHeight {
		for col := range fb.Img.CodedWidth {
			fb.Img.Y[row*fb.Img.YStride+col] = byte((row*11 + col*17 + 5) & 0xff)
		}
	}
	extendIntraRightEdgeForRow(&fb.Img, 0)

	cases := []struct {
		name string
		img  *common.Image
		row  int
		col  int
	}{
		{name: "interior", img: &interior, row: 1, col: 1},
		{name: "top-left", img: &topLeft, row: 0, col: 0},
		{name: "synthetic-edge", img: &syntheticEdge, row: 1, col: 1},
		{name: "extended-right-edge", img: &fb.Img, row: 1, col: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fullScratch IntraPredictorScratch
			var lumaScratch IntraPredictorScratch
			fullRefs := BuildIntraPredictorRefs(tc.img, tc.row, tc.col, &fullScratch)
			lumaRefs := BuildIntraPredictorRefsLuma(tc.img, tc.row, tc.col, &lumaScratch)

			assertByteSlicesEqual(t, "YAbove", lumaRefs.YAbove, fullRefs.YAbove)
			assertByteSlicesEqual(t, "YLeft", lumaRefs.YLeft, fullRefs.YLeft)
			if lumaRefs.YTopLeft != fullRefs.YTopLeft {
				t.Fatalf("YTopLeft = %d, want %d", lumaRefs.YTopLeft, fullRefs.YTopLeft)
			}
			if lumaRefs.UpAvailable != fullRefs.UpAvailable || lumaRefs.LeftAvailable != fullRefs.LeftAvailable {
				t.Fatalf("availability = %v/%v, want %v/%v", lumaRefs.UpAvailable, lumaRefs.LeftAvailable, fullRefs.UpAvailable, fullRefs.LeftAvailable)
			}
		})
	}
}

func TestBuildIntraPredictorRefsLumaAliasesVisibleAboveRow(t *testing.T) {
	img := testImage(64, 64)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefsLuma(&img, 1, 1, &scratch)

	if len(refs.YAbove) != 20 {
		t.Fatalf("YAbove len = %d, want 20", len(refs.YAbove))
	}
	if got, want := &refs.YAbove[0], &img.Y[15*img.YStride+16]; got != want {
		t.Fatalf("YAbove does not alias visible above row")
	}
	if got, want := &refs.YLeft[0], &scratch.YLeft[0]; got != want {
		t.Fatalf("YLeft should still use contiguous scratch")
	}
}

func TestBuildIntraPredictorRefsLumaAliasesExtendedAboveRow(t *testing.T) {
	fb, err := common.NewFrameBuffer(32, 32, 8, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	img := &fb.Img
	img.Y[15*img.YStride+31] = 55
	extendIntraRightEdgeForRow(img, 0)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefsLuma(img, 1, 1, &scratch)

	if len(refs.YAbove) != 20 {
		t.Fatalf("YAbove len = %d, want 20", len(refs.YAbove))
	}
	if got, want := &refs.YAbove[0], &img.YFull[img.YOrigin+15*img.YStride+16]; got != want {
		t.Fatalf("YAbove does not alias extended above row")
	}
	for i := 16; i < 20; i++ {
		if got := refs.YAbove[i]; got != 55 {
			t.Fatalf("YAbove[%d] = %d, want extended right edge 55", i, got)
		}
	}
}

func TestBuildIntraPredictorRefsLumaUsesScratchForSyntheticEdges(t *testing.T) {
	img := testImage(18, 18)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefsLuma(&img, 1, 1, &scratch)

	if got, want := &refs.YAbove[0], &scratch.YAbove[0]; got != want {
		t.Fatalf("YAbove should use scratch when right-edge samples are synthetic")
	}
	if got, want := refs.YAbove[0], img.Y[15*img.YStride+16]; got != want {
		t.Fatalf("YAbove[0] = %d, want %d", got, want)
	}
	for i := 2; i < len(refs.YAbove); i++ {
		if got := refs.YAbove[i]; got != 127 {
			t.Fatalf("YAbove[%d] = %d, want synthetic 127", i, got)
		}
	}
}

func TestBuildIntraPredictorRefsAllocatesZero(t *testing.T) {
	img := testImage(32, 32)
	var scratch IntraPredictorScratch
	allocs := testing.AllocsPerRun(1000, func() {
		_ = BuildIntraPredictorRefs(&img, 1, 1, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestBuildIntraPredictorRefsLumaAllocatesZero(t *testing.T) {
	img := testImage(32, 32)
	var scratch IntraPredictorScratch
	allocs := testing.AllocsPerRun(1000, func() {
		_ = BuildIntraPredictorRefsLuma(&img, 1, 1, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestTransformMacroblockTokensY2DCOnly(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	TransformMacroblockTokens(&tokens, &dequant, false, &residual)

	for i := range 16 {
		if got := residual.Block(i)[0]; got != 8 {
			t.Fatalf("Y block %d DC = %d, want 8", i, got)
		}
	}
}

func assertByteSlicesEqual(t *testing.T, name string, got []byte, want []byte) {
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

func TestTransformMacroblockTokensAddsY1ACToY2DC(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	tokens.QCoeff[0][1] = 3
	tokens.EOB[0] = 2
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	TransformMacroblockTokens(&tokens, &dequant, false, &residual)

	if got := residual.Block(0)[0]; got != 8 {
		t.Fatalf("Y block 0 DC = %d, want 8 from Y2", got)
	}
	if got := residual.Block(0)[1]; got != 21 {
		t.Fatalf("Y block 0 AC = %d, want 21", got)
	}
}

func TestTransformMacroblockTokensIgnoresCoefficientsPastEOB(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[0][1] = 2
	tokens.QCoeff[0][4] = 99
	tokens.EOB[0] = 2
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	TransformMacroblockTokens(&tokens, &dequant, true, &residual)

	if got := residual.Block(0)[1]; got != 12 {
		t.Fatalf("Y block 0 AC = %d, want active coefficient", got)
	}
	if got := residual.Block(0)[4]; got != 0 {
		t.Fatalf("Y block 0 coefficient past EOB = %d, want ignored", got)
	}
}

func TestTransformMacroblockTokensHandlesSkipDCEOB(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[0][15] = 2
	tokens.EOB[0] = 17
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	TransformMacroblockTokens(&tokens, &dequant, false, &residual)

	if got := residual.Block(0)[15]; got != 42 {
		t.Fatalf("Y block 0 last AC = %d, want transformed skip-DC coefficient", got)
	}
}

func TestTransformMacroblockTokensClearsActiveResidualBlocks(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[0][1] = 3
	tokens.EOB[0] = 2
	tokens.QCoeff[16][0] = 4
	tokens.EOB[16] = 1
	dequant := testMacroblockDequant()
	var residual MacroblockResidual
	residual.Block(0)[1] = 99
	residual.Block(16)[0] = -99

	TransformMacroblockTokens(&tokens, &dequant, false, &residual)

	if got := residual.Block(0)[1]; got != 21 {
		t.Fatalf("Y block 0 AC = %d, want 21 without stale residual", got)
	}
	if got := residual.Block(16)[0]; got != 24 {
		t.Fatalf("UV block 16 DC = %d, want 24 without stale residual", got)
	}
}

func TestTransformMacroblockTokensY2ClearsStaleYACResidual(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	dequant := testMacroblockDequant()
	var residual MacroblockResidual
	residual.Block(0)[5] = 99
	residual.Block(15)[7] = -44

	TransformMacroblockTokens(&tokens, &dequant, false, &residual)

	if got := residual.Block(0)[0]; got != 8 {
		t.Fatalf("Y block 0 DC = %d, want 8 from Y2", got)
	}
	if got := residual.Block(0)[5]; got != 0 {
		t.Fatalf("Y block 0 AC = %d, want cleared stale residual", got)
	}
	if got := residual.Block(15)[7]; got != 0 {
		t.Fatalf("Y block 15 AC = %d, want cleared stale residual", got)
	}
}

func TestTransformMacroblockTokensAllocatesZero(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	dequant := testMacroblockDequant()
	var residual MacroblockResidual
	allocs := testing.AllocsPerRun(1000, func() {
		TransformMacroblockTokens(&tokens, &dequant, false, &residual)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkTransformMacroblockTokensSparse(b *testing.B) {
	var tokens MacroblockTokens
	tokens.QCoeff[3][1] = 2
	tokens.EOB[3] = 2
	tokens.QCoeff[20][0] = -4
	tokens.EOB[20] = 1
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		TransformMacroblockTokens(&tokens, &dequant, true, &residual)
	}
	if got := residual.Block(3)[1]; got != 12 {
		b.Fatalf("Y block 3 AC = %d, want 12", got)
	}
	if got := residual.Block(20)[0]; got != -24 {
		b.Fatalf("UV block 20 DC = %d, want -24", got)
	}
}

func BenchmarkTransformMacroblockTokensDCOnly(b *testing.B) {
	var tokens MacroblockTokens
	for i := range 16 {
		tokens.QCoeff[i][0] = 1
		tokens.EOB[i] = 1
	}
	for i := 16; i < 24; i++ {
		tokens.QCoeff[i][0] = -1
		tokens.EOB[i] = 1
	}
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		TransformMacroblockTokens(&tokens, &dequant, true, &residual)
	}
	if got := residual.Block(0)[0]; got != 5 {
		b.Fatalf("Y block 0 DC = %d, want 5", got)
	}
	if got := residual.Block(16)[0]; got != -6 {
		b.Fatalf("UV block 16 DC = %d, want -6", got)
	}
}

func BenchmarkTransformMacroblockTokensY2DCOnly(b *testing.B) {
	var tokens MacroblockTokens
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		TransformMacroblockTokens(&tokens, &dequant, false, &residual)
	}
	if got := residual.Block(0)[0]; got != 8 {
		b.Fatalf("Y block 0 DC = %d, want 8", got)
	}
}
