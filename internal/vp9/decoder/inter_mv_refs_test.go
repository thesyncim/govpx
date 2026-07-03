package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

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
