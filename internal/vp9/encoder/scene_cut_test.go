package encoder

import "testing"

func TestSceneCutFrameStatsPromotesKeyFrame(t *testing.T) {
	stats := SceneCutFrameStats{Macroblocks: 4}
	for range stats.Macroblocks {
		stats.AddMacroblock(2_000_000, 100_000)
	}

	if !stats.PromotesKeyFrame() {
		t.Fatalf("PromotesKeyFrame = false for strong scene cut stats: %+v", stats)
	}
	if stats.ReferenceError != 8_000_000 || stats.IntraError != 400_000 {
		t.Fatalf("totals = reference:%d intra:%d, want reference:8000000 intra:400000",
			stats.ReferenceError, stats.IntraError)
	}
	if stats.IntraBetterBlocks != 4 || stats.HighErrorBlocks != 4 {
		t.Fatalf("block counts = intraBetter:%d highError:%d, want 4/4",
			stats.IntraBetterBlocks, stats.HighErrorBlocks)
	}
}

func TestSceneCutFrameStatsRejectsWeakReferenceError(t *testing.T) {
	stats := SceneCutFrameStats{Macroblocks: 4}
	for range stats.Macroblocks {
		stats.AddMacroblock(100_000, 10_000)
	}

	if stats.PromotesKeyFrame() {
		t.Fatalf("PromotesKeyFrame = true for weak reference error stats: %+v", stats)
	}
}

func TestMacroblockLumaSSE(t *testing.T) {
	src := LumaPlane{
		Pixels: makeFilledPlane(16, 16, 10),
		Stride: 16,
		Width:  16,
		Height: 16,
	}
	ref := LumaPlane{
		Pixels: makeFilledPlane(16, 16, 20),
		Stride: 16,
		Width:  16,
		Height: 16,
	}

	if got := MacroblockLumaSSE(src, ref, 0, 0); got != 16*16*10*10 {
		t.Fatalf("MacroblockLumaSSE = %d, want %d", got, 16*16*10*10)
	}
}

func TestMacroblockLumaSSEReplicatesVisibleEdges(t *testing.T) {
	src := LumaPlane{
		Pixels: []byte{
			1, 2,
			3, 4,
		},
		Stride: 2,
		Width:  2,
		Height: 2,
	}
	ref := LumaPlane{
		Pixels: makeFilledPlane(2, 2, 0),
		Stride: 2,
		Width:  2,
		Height: 2,
	}

	got := MacroblockLumaSSE(src, ref, 0, 0)
	want := 1*1 + 15*2*2 + 15*3*3 + 15*15*4*4
	if got != want {
		t.Fatalf("MacroblockLumaSSE edge replicated = %d, want %d", got, want)
	}
}

func TestMacroblockMeanLumaSSE(t *testing.T) {
	src := LumaPlane{
		Pixels: makeCheckerPlane(16, 16, 0, 255),
		Stride: 16,
		Width:  16,
		Height: 16,
	}

	got := MacroblockMeanLumaSSE(src, 0, 0)
	const want = 4_161_600
	if got != want {
		t.Fatalf("MacroblockMeanLumaSSE = %d, want %d", got, want)
	}
}

func TestMacroblockMeanLumaSSEConstantBlock(t *testing.T) {
	src := LumaPlane{
		Pixels: makeFilledPlane(16, 16, 128),
		Stride: 16,
		Width:  16,
		Height: 16,
	}

	if got := MacroblockMeanLumaSSE(src, 0, 0); got != 0 {
		t.Fatalf("MacroblockMeanLumaSSE constant block = %d, want 0", got)
	}
}
