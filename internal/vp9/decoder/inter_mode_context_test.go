package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
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
