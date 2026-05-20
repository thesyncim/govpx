package geometry

import "testing"

func TestMacroblockGridRoundsVisibleDimensions(t *testing.T) {
	tests := []struct {
		width    int
		height   int
		wantCols int
		wantRows int
	}{
		{width: 1, height: 1, wantCols: 1, wantRows: 1},
		{width: 16, height: 16, wantCols: 1, wantRows: 1},
		{width: 17, height: 16, wantCols: 2, wantRows: 1},
		{width: 32, height: 33, wantCols: 2, wantRows: 3},
	}
	for _, tt := range tests {
		if got := MacroblockCols(tt.width); got != tt.wantCols {
			t.Fatalf("MacroblockCols(%d) = %d, want %d", tt.width, got, tt.wantCols)
		}
		if got := MacroblockRows(tt.height); got != tt.wantRows {
			t.Fatalf("MacroblockRows(%d) = %d, want %d", tt.height, got, tt.wantRows)
		}
		if got := MacroblockCount(tt.width, tt.height); got != tt.wantCols*tt.wantRows {
			t.Fatalf("MacroblockCount(%d, %d) = %d, want %d", tt.width, tt.height, got, tt.wantCols*tt.wantRows)
		}
	}
}
