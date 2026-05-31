package vp9test

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TileStart returns the byte offset where tile data begins in a VP9 packet.
func TileStart(packet []byte) (int, error) {
	var br vp9dec.BitReader
	br.Init(packet)
	hdr, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		return 0, err
	}
	return br.BytesRead() + int(hdr.FirstPartitionSize), nil
}

// MultiTileStubPacket builds a neutral keyframe fixture with the requested
// tile-column layout.
func MultiTileStubPacket(t testing.TB, width, height, log2TileCols int) []byte {
	t.Helper()
	return StubPacket(t, width, height, log2TileCols, common.DcPred)
}

func MultiTileStubPacketWithFrameParallel(t testing.TB, width, height,
	log2TileCols int, frameParallel bool,
) []byte {
	t.Helper()
	return StubPacketWithFrameParallel(t, width, height, log2TileCols,
		common.DcPred, frameParallel)
}

func StubPacket(t testing.TB, width, height, log2TileCols int,
	yMode common.PredictionMode,
) []byte {
	t.Helper()
	return StubPacketWithFrameParallel(t, width, height, log2TileCols,
		yMode, true)
}

func StubPacketWithFrameParallel(t testing.TB, width, height, log2TileCols int,
	yMode common.PredictionMode, frameParallel bool,
) []byte {
	t.Helper()
	return packSyntheticKeyframe(t, width, height, log2TileCols, frameParallel,
		func(int) common.PredictionMode { return yMode })
}

func MultiTileModePacket(t testing.TB, width, height, log2TileCols int,
	modes []common.PredictionMode,
) []byte {
	t.Helper()
	if len(modes) == 0 {
		t.Fatal("MultiTileModePacket requires at least one mode")
	}
	return packSyntheticKeyframe(t, width, height, log2TileCols, true,
		func(tileCol int) common.PredictionMode {
			return modes[tileCol%len(modes)]
		})
}

func packSyntheticKeyframe(t testing.TB, width, height, log2TileCols int,
	frameParallel bool, modeForTile func(tileCol int) common.PredictionMode,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)
	aboveSegCtx := make([]int8, common.AlignToSB(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: frameParallel,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	header.Tile.Log2TileCols = log2TileCols

	var seg vp9dec.SegmentationParams
	partitionProbs := tables.KfPartitionProbs
	tileCols := 1 << uint(log2TileCols)
	dest := make([]byte, 262144)
	scratch := make([]byte, 262144)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: tileCols,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: vp9dec.TileOffset(tileRow, miRows,
					header.Tile.Log2TileRows),
				MiRowEnd: vp9dec.TileOffset(tileRow+1, miRows,
					header.Tile.Log2TileRows),
				MiColStart: vp9dec.TileOffset(tileCol, miCols,
					header.Tile.Log2TileCols),
				MiColEnd: vp9dec.TileOffset(tileCol+1, miCols,
					header.Tile.Log2TileCols),
			}
			mode := modeForTile(tileCol)
			initSyntheticMiGrid(miGrid, miRows, miCols, mode)
			vp9enc.WriteModesTile(bw, vp9enc.WriteModesTileArgs{
				WriteModesSbArgs: vp9enc.WriteModesSbArgs{
					AboveSegCtx:    aboveSegCtx,
					LeftSegCtx:     leftSegCtx,
					MiRows:         miRows,
					MiCols:         miCols,
					PartitionProbs: &partitionProbs,
					GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
						return syntheticMiAt(miGrid, miRows, miCols, miRow, miCol)
					},
					WriteB: func(bw *bitstream.Writer, miRow, miCol int,
						bsize common.BlockSize,
					) {
						writeSyntheticKeyframeBlock(bw, syntheticBlockArgs{
							MiGrid: miGrid,
							MIRows: miRows,
							MICols: miCols,
							Tile:   tile,
							FC:     &fc,
							Seg:    &seg,
							MICol:  miCol,
							MIRow:  miRow,
							Block:  bsize,
							YMode:  mode,
							TxMode: common.Only4x4,
							UVMode: common.DcPred,
						})
					},
				},
				MiRowStart: tile.MiRowStart,
				MiRowEnd:   tile.MiRowEnd,
				MiColStart: tile.MiColStart,
				MiColEnd:   tile.MiColEnd,
			})
			return nil
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

type syntheticBlockArgs struct {
	MiGrid []vp9dec.NeighborMi
	MIRows int
	MICols int
	Tile   vp9dec.TileBounds
	FC     *vp9dec.FrameContext
	Seg    *vp9dec.SegmentationParams
	MICol  int
	MIRow  int
	Block  common.BlockSize
	YMode  common.PredictionMode
	TxMode common.TxMode
	UVMode common.PredictionMode
}

func writeSyntheticKeyframeBlock(bw *bitstream.Writer, a syntheticBlockArgs) {
	cur := syntheticMi(a.Block, a.YMode)
	left := (*vp9dec.NeighborMi)(nil)
	if a.MICol > a.Tile.MiColStart {
		left = syntheticMiAt(a.MiGrid, a.MIRows, a.MICols, a.MIRow, a.MICol-1)
	}
	above := syntheticMiAt(a.MiGrid, a.MIRows, a.MICols, a.MIRow-1, a.MICol)
	maxTxSize := common.MaxTxsizeLookup[a.Block]
	if cur.TxSize > maxTxSize {
		cur.TxSize = maxTxSize
	}
	vp9enc.WriteKeyframeBlock(bw, vp9enc.WriteKeyframeBlockArgs{
		Seg:       a.Seg,
		Mi:        &cur,
		AboveMi:   above,
		LeftMi:    left,
		TxMode:    a.TxMode,
		MaxTxSize: maxTxSize,
		SkipProbs: a.FC.SkipProbs,
	})
	vp9enc.WriteKeyframeUvMode(bw, a.UVMode, cur.Mode)
	fillSyntheticMiGrid(a.MiGrid, a.MIRows, a.MICols, a.MIRow, a.MICol,
		a.Block, cur)
}

func initSyntheticMiGrid(miGrid []vp9dec.NeighborMi, miRows, miCols int,
	mode common.PredictionMode,
) {
	for r := range miRows {
		for c := range miCols {
			bsize := syntheticBlockSizeForRegion(miRows, miCols, r, c,
				common.Block64x64)
			miGrid[r*miCols+c] = syntheticMi(bsize, mode)
		}
	}
}

func syntheticMi(bsize common.BlockSize, mode common.PredictionMode) vp9dec.NeighborMi {
	txSize := common.Tx4x4
	maxTxSize := common.MaxTxsizeLookup[bsize]
	if txSize > maxTxSize {
		txSize = maxTxSize
	}
	return vp9dec.NeighborMi{
		SbType: bsize,
		Mode:   mode,
		TxSize: txSize,
		Skip:   1,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
}

func syntheticMiAt(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	return &miGrid[r*miCols+c]
}

func fillSyntheticMiGrid(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int,
	bsize common.BlockSize, mi vp9dec.NeighborMi,
) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

var syntheticBlockSizeOrder = [...]common.BlockSize{
	common.Block64x64,
	common.Block64x32,
	common.Block32x64,
	common.Block32x32,
	common.Block32x16,
	common.Block16x32,
	common.Block16x16,
	common.Block16x8,
	common.Block8x16,
	common.Block8x8,
	common.Block8x4,
	common.Block4x8,
	common.Block4x4,
}

func syntheticBlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize,
) common.BlockSize {
	maxW := int(common.Num8x8BlocksWideLookup[root])
	maxH := int(common.Num8x8BlocksHighLookup[root])
	availW := min(miCols-miCol, maxW)
	availH := min(miRows-miRow, maxH)
	for _, bsize := range syntheticBlockSizeOrder {
		if int(common.Num8x8BlocksWideLookup[bsize]) <= availW &&
			int(common.Num8x8BlocksHighLookup[bsize]) <= availH {
			return bsize
		}
	}
	return common.Block4x4
}
