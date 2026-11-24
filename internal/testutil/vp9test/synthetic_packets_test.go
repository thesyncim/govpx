package vp9test

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestSyntheticStubPacketHeaderAndTileStart(t *testing.T) {
	packet := MultiTileStubPacket(t, 1024, 64, 1)
	header, parseTileStart := ParseHeader(t, packet)
	gotTileStart, err := TileStart(packet)
	if err != nil {
		t.Fatalf("TileStart: %v", err)
	}
	if gotTileStart != parseTileStart {
		t.Fatalf("tile start = %d, want %d", gotTileStart, parseTileStart)
	}
	if header.Width != 1024 || header.Height != 64 {
		t.Fatalf("coded size = %dx%d, want 1024x64", header.Width, header.Height)
	}
	if header.Tile.Log2TileCols != 1 || header.Tile.Log2TileRows != 0 {
		t.Fatalf("tile layout = log2 cols %d rows %d, want 1/0",
			header.Tile.Log2TileCols, header.Tile.Log2TileRows)
	}
	if !header.FrameParallelDecoding {
		t.Fatal("FrameParallelDecoding = false, want true")
	}
	if parseTileStart <= 0 || parseTileStart >= len(packet) {
		t.Fatalf("tile start = %d outside packet len %d", parseTileStart, len(packet))
	}
}

func TestSyntheticStubPacketFrameParallelFlag(t *testing.T) {
	packet := MultiTileStubPacketWithFrameParallel(t, 64, 64, 0, false)
	header, _ := ParseHeader(t, packet)
	if header.FrameParallelDecoding {
		t.Fatal("FrameParallelDecoding = true, want false")
	}
}

func TestSyntheticMultiTileModePacketHeader(t *testing.T) {
	packet := MultiTileModePacket(t, 1024, 64, 1,
		[]common.PredictionMode{common.DcPred, common.VPred})
	header, _ := ParseHeader(t, packet)
	if header.Tile.Log2TileCols != 1 {
		t.Fatalf("Log2TileCols = %d, want 1", header.Tile.Log2TileCols)
	}
	if header.FrameType != common.KeyFrame {
		t.Fatalf("FrameType = %d, want keyframe", header.FrameType)
	}
}

func TestColumnResidueKeyframeHeader(t *testing.T) {
	packet := ColumnResidueKeyframe(t, 64, 64, 32, 8)
	header, _ := ParseHeader(t, packet)
	if header.Loopfilter.FilterLevel != 32 {
		t.Fatalf("loop filter level = %d, want 32", header.Loopfilter.FilterLevel)
	}
	if header.Quant.BaseQindex != 1 {
		t.Fatalf("base qindex = %d, want 1", header.Quant.BaseQindex)
	}
}
