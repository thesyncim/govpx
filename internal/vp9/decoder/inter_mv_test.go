package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
)

func TestInterModeContextNearestNeighbors(t *testing.T) {
	const miRows = 16
	const miCols = 16
	tile := TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	grid := make([]NeighborMi, miRows*miCols)
	for i := range grid {
		grid[i].Mode = common.ZeroMv
	}

	cases := []struct {
		name         string
		miRow, miCol int
		want         int
	}{
		{"no-neighbors", 0, 0, tables.BothPredicted},
		{"left-zero", 0, 8, tables.ZeroPlusPredicted},
		{"above-zero", 8, 0, tables.ZeroPlusPredicted},
		{"above-left-zero", 8, 8, tables.BothZero},
	}
	for _, tc := range cases {
		got := InterModeContext(grid, miCols, tile, miRows,
			tc.miRow, tc.miCol, common.Block64x64)
		if got != tc.want {
			t.Fatalf("%s: InterModeContext = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestInterModeContextHonorsTileLeftEdge(t *testing.T) {
	const miRows = 16
	const miCols = 16
	tile := TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 8, MiColEnd: miCols}
	grid := make([]NeighborMi, miRows*miCols)
	for i := range grid {
		grid[i].Mode = common.ZeroMv
	}

	got := InterModeContext(grid, miCols, tile, miRows, 0, 8, common.Block64x64)
	if got != tables.BothPredicted {
		t.Fatalf("InterModeContext at tile left edge = %d, want %d", got, tables.BothPredicted)
	}
}

func TestFindInterMvRefs(t *testing.T) {
	const miRows = 8
	const miCols = 8
	miGrid := make([]NeighborMi, miRows*miCols)
	tile := TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	miGrid[3*miCols+5] = NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{LastFrame, NoRefFrame},
		Mv:       [2]MV{{Col: 64}},
	}
	miGrid[5*miCols+3] = NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{LastFrame, NoRefFrame},
		Mv:       [2]MV{{Col: -128}},
	}

	refs, count := FindInterMvRefsFields(miGrid, false, nil, 0, 0,
		tile, miRows, miCols, 4, 4, common.Block32x32,
		common.NearMv, LastFrame, [MaxRefFrames]uint8{}, -1)
	if count != 2 {
		t.Fatalf("mv ref count = %d, want 2", count)
	}
	if got := InterModeMvCandidate(refs, count, common.NearestMv); got != (MV{Col: 64}) {
		t.Fatalf("nearest candidate = %+v, want col 64", got)
	}
	if got := InterModeMvCandidate(refs, count, common.NearMv); got != (MV{Col: -128}) {
		t.Fatalf("near candidate = %+v, want col -128", got)
	}
}

func TestFindInterMvRefsUsesDiffRefSignBias(t *testing.T) {
	const miRows = 8
	const miCols = 8
	miGrid := make([]NeighborMi, miRows*miCols)
	tile := TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	miGrid[3*miCols+5] = NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{GoldenFrame, NoRefFrame},
		Mv:       [2]MV{{Row: 16, Col: -32}},
	}
	var signBias [MaxRefFrames]uint8
	signBias[GoldenFrame] = 1

	refs, count := FindInterMvRefsFields(miGrid, false, nil, 0, 0,
		tile, miRows, miCols, 4, 4, common.Block32x32,
		common.NearestMv, LastFrame, signBias, -1)
	if count != 1 {
		t.Fatalf("diff-ref mv ref count = %d, want 1", count)
	}
	if got := refs[0]; got != (MV{Row: -16, Col: 32}) {
		t.Fatalf("diff-ref candidate = %+v, want sign-bias inverted", got)
	}
}

func TestFindInterMvRefsUsesCompoundRefs(t *testing.T) {
	const miRows = 8
	const miCols = 8
	miGrid := make([]NeighborMi, miRows*miCols)
	tile := TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	miGrid[3*miCols+3] = NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{LastFrame, AltrefFrame},
		Mv:       [2]MV{{Col: 64}, {Col: 96}},
	}
	miGrid[3*miCols+5] = NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{LastFrame, AltrefFrame},
		Mv:       [2]MV{{Col: 128}, {Col: 160}},
	}

	refs, count := FindInterMvRefsFields(miGrid, false, nil, 0, 0,
		tile, miRows, miCols, 4, 4, common.Block32x32,
		common.NearMv, AltrefFrame, [MaxRefFrames]uint8{}, -1)
	if count != 2 {
		t.Fatalf("compound mv ref count = %d, want 2", count)
	}
	if got := InterModeMvCandidate(refs, count, common.NearestMv); got != (MV{Col: 128}) {
		t.Fatalf("compound nearest candidate = %+v, want ALTREF col 128", got)
	}
	if got := InterModeMvCandidate(refs, count, common.NearMv); got != (MV{Col: 96}) {
		t.Fatalf("compound near candidate = %+v, want ALTREF col 96", got)
	}
}

func TestFindInterMvRefsUsesPreviousFrameMvs(t *testing.T) {
	const miRows = 8
	const miCols = 8
	miGrid := make([]NeighborMi, miRows*miCols)
	prev := make([]MvRef, miRows*miCols)
	prev[4*miCols+4] = MvRef{
		RefFrame: [2]int8{LastFrame, NoRefFrame},
		Mv:       [2]MV{{Row: 24, Col: -40}},
	}
	tile := TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}

	refs, count := FindInterMvRefsFields(miGrid, true, prev, miRows, miCols,
		tile, miRows, miCols, 4, 4, common.Block16x16,
		common.NearestMv, LastFrame, [MaxRefFrames]uint8{}, -1)
	if count != 1 {
		t.Fatalf("previous-frame mv ref count = %d, want 1", count)
	}
	if refs[0] != (MV{Row: 24, Col: -40}) {
		t.Fatalf("previous-frame candidate = %+v, want row 24 col -40", refs[0])
	}
}

var benchFindInterMvRefsRefs [2]MV
var benchFindInterMvRefsCount int

func BenchmarkFindInterMvRefsFields(b *testing.B) {
	const miRows = 16
	const miCols = 16
	miGrid := make([]NeighborMi, miRows*miCols)
	tile := TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	signBias := [MaxRefFrames]uint8{}
	signBias[GoldenFrame] = 1
	for r := range miRows {
		for c := range miCols {
			idx := r*miCols + c
			miGrid[idx] = NeighborMi{
				Mode: common.NewMv,
				RefFrame: [2]int8{
					LastFrame,
					GoldenFrame,
				},
				Mv: [2]MV{
					{Row: int16((r - 8) * 8), Col: int16((c - 8) * 8)},
					{Row: int16((8 - r) * 4), Col: int16((8 - c) * 4)},
				},
			}
		}
	}
	miGrid[7*miCols+8].RefFrame[0] = AltrefFrame
	miGrid[8*miCols+7].RefFrame[0] = LastFrame
	miGrid[7*miCols+7].RefFrame[0] = GoldenFrame

	b.Run("near-full-walk", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			benchFindInterMvRefsRefs, benchFindInterMvRefsCount =
				FindInterMvRefsFields(miGrid, false, nil, 0, 0,
					tile, miRows, miCols, 8, 8, common.Block32x32,
					common.NearMv, LastFrame, signBias, -1)
		}
	})
	b.Run("nearest-early-break", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			benchFindInterMvRefsRefs, benchFindInterMvRefsCount =
				FindInterMvRefsFields(miGrid, false, nil, 0, 0,
					tile, miRows, miCols, 8, 8, common.Block32x32,
					common.NearestMv, LastFrame, signBias, -1)
		}
	})
}

func TestInterPredictSourceInBounds(t *testing.T) {
	if !InterPredictSourceInBounds(32, 32, 32, 32, 96, 96, 8, 8) {
		t.Fatal("interior two-axis subpel window rejected")
	}
	if InterPredictSourceInBounds(32, 32, 32, 32, 64, 64, 8, 0) {
		t.Fatal("right-edge horizontal subpel window accepted without border")
	}
	if InterPredictSourceInBounds(0, 32, 32, 32, 96, 96, 8, 0) {
		t.Fatal("left-edge horizontal subpel window accepted without border")
	}
	if InterPredictSourceInBounds(32, 0, 32, 32, 96, 96, 0, 8) {
		t.Fatal("top-edge vertical subpel window accepted without border")
	}
	if !InterPredictSourceInBounds(0, 0, 32, 32, 32, 32, 0, 0) {
		t.Fatal("integer-pel exact window rejected")
	}
}

func TestModeInfoDecodeBSize(t *testing.T) {
	if got := ModeInfoDecodeBSize(common.Block4x4); got != common.Block8x8 {
		t.Fatalf("sub-8x8 decode bsize = %v, want BLOCK_8X8", got)
	}
	if got := ModeInfoDecodeBSize(common.Block32x32); got != common.Block32x32 {
		t.Fatalf("32x32 decode bsize = %v, want BLOCK_32X32", got)
	}
}

func TestPlaneMaxBlocks4x4ClipsFrameEdge(t *testing.T) {
	pd := MacroblockdPlane{}
	gotW, gotH := PlaneMaxBlocks4x4(5, 5, 4, 4,
		common.Block16x16, &pd, common.Block16x16)
	if gotW != 2 || gotH != 2 {
		t.Fatalf("edge luma blocks = %dx%d, want 2x2", gotW, gotH)
	}
	pd.SubsamplingX = 1
	pd.SubsamplingY = 1
	gotW, gotH = PlaneMaxBlocks4x4(5, 5, 4, 4,
		common.Block16x16, &pd, common.Block8x8)
	if gotW != 1 || gotH != 1 {
		t.Fatalf("edge chroma blocks = %dx%d, want 1x1", gotW, gotH)
	}
}

// TestRoundMvComp anchors the half-away-from-zero rounding helpers
// against a hand-built truth table.
func TestRoundMvComp(t *testing.T) {
	cases := []struct {
		v  int
		q2 int
		q4 int
	}{
		{0, 0, 0},
		{1, 1, 0},
		{2, 1, 1},
		{4, 2, 1},
		{-1, -1, 0},
		{-2, -1, -1},
		{8, 4, 2},
		{-8, -4, -2},
	}
	for _, c := range cases {
		if got := roundMvCompQ2(c.v); got != c.q2 {
			t.Errorf("roundMvCompQ2(%d)=%d want %d", c.v, got, c.q2)
		}
		if got := roundMvCompQ4(c.v); got != c.q4 {
			t.Errorf("roundMvCompQ4(%d)=%d want %d", c.v, got, c.q4)
		}
	}
}

// TestAverageSplitMvsByChroma covers the four subsampling shapes:
// 4:4:4 (identity), 4:4:2 (vertical pair), 4:2:2 (horizontal pair),
// and 4:2:0 (4-way average).
func TestAverageSplitMvsByChroma(t *testing.T) {
	var bmi [4]Bmi
	bmi[0].AsMv[0] = MV{Row: 8, Col: 4}
	bmi[1].AsMv[0] = MV{Row: 4, Col: 4}
	bmi[2].AsMv[0] = MV{Row: 4, Col: -2}
	bmi[3].AsMv[0] = MV{Row: -4, Col: 6}
	// 4:4:4 → identity, returns bmi[block].
	if got := AverageSplitMvs(&bmi, 0, 1, 0, 0); got != bmi[1].AsMv[0] {
		t.Errorf("ss=0,0: got %+v want %+v", got, bmi[1].AsMv[0])
	}
	// 4:4:2 → vertical pair (block, block+2). block=0 → avg(0, 2):
	// row=(8+4)/2=6, col=(4+-2)/2 = round_q2(2)=1.
	got := AverageSplitMvs(&bmi, 0, 0, 0, 1)
	if got != (MV{Row: 6, Col: 1}) {
		t.Errorf("ss=0,1 block=0: got %+v want (6,1)", got)
	}
	// 4:2:2 → horizontal pair (block, block+1). block=0 → avg(0, 1):
	// row=(8+4)/2=6, col=(4+4)/2=4.
	got = AverageSplitMvs(&bmi, 0, 0, 1, 0)
	if got != (MV{Row: 6, Col: 4}) {
		t.Errorf("ss=1,0 block=0: got %+v want (6,4)", got)
	}
	// 4:2:0 → 4-way average. row=(8+4+4-4)/4=q4(12)=3, col=(4+4-2+6)/4=q4(12)=3.
	got = AverageSplitMvs(&bmi, 0, 0, 1, 1)
	if got != (MV{Row: 3, Col: 3}) {
		t.Errorf("ss=1,1: got %+v want (3,3)", got)
	}
}

// TestClampMvToUmvBorderSb on a small block confirms the saturation
// math: a far-out-of-frame MV gets clamped to the per-edge limits.
func TestClampMvToUmvBorderSb(t *testing.T) {
	// 16x16 block; ssX=ssY=0 → shift=2 → MV*2 is clamped against
	// edges*2 ± (spelLeft / spelRight / spelTop / spelBottom).
	// Pick edges so we can predict the saturation cleanly.
	edges := BlockBoundsEdges{
		MbToLeftEdge:   -100,
		MbToRightEdge:  100,
		MbToTopEdge:    -200,
		MbToBottomEdge: 200,
	}
	// Inside the box → MV scaled by 2.
	in := MV{Row: 10, Col: -5}
	got := ClampMvToUmvBorderSb(edges, in, 16, 16, 0, 0)
	if got.Row != 20 || got.Col != -10 {
		t.Errorf("inside: got %+v want (20,-10)", got)
	}
	// Force a Col under-clamp with a value small enough to stay
	// inside int16 after the *shiftX scale-up: -500 * 2 = -1000,
	// well below the lower edge -520.
	in = MV{Row: 0, Col: -500}
	got = ClampMvToUmvBorderSb(edges, in, 16, 16, 0, 0)
	wantMinCol := edges.MbToLeftEdge*2 - ((VP9InterpExtend + 16) << SubpelBitsConst)
	if int(got.Col) != wantMinCol {
		t.Errorf("under col: got %d want %d", got.Col, wantMinCol)
	}
}
