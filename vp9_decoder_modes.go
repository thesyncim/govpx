package govpx

import (
	"encoding/binary"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// parseVP9IntraModeTiles consumes the tile mode-info stream for
// keyframes and intra-only inter frames. Residual decode and
// reconstruction still live behind ErrVP9NotImplemented, but this
// validates the partition + intra-mode layer the current encoder emits.
func (d *VP9Decoder) parseVP9IntraModeTiles(tileData []byte,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
) error {
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	d.ensureVP9DecoderModeBuffers(miRows, miCols)
	for i := range d.aboveSegCtx {
		d.aboveSegCtx[i] = 0
	}
	for i := range d.miGrid {
		d.miGrid[i] = vp9dec.NeighborMi{}
	}
	for i := range d.segMap {
		d.segMap[i] = 0
	}

	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: d.segMap,
		LastFrameSegMap:    d.lastSegMap,
		MiCols:             miCols,
	}
	partitionProbs := &tables.KfPartitionProbs
	tileRows := 1 << uint(hdr.Tile.Log2TileRows)
	tileCols := 1 << uint(hdr.Tile.Log2TileCols)
	nTiles := tileRows * tileCols
	offset := 0

	for tileRow := range tileRows {
		for tileCol := range tileCols {
			idx := tileRow*tileCols + tileCol
			isLast := idx == nTiles-1
			tileSize := len(tileData) - offset
			if !isLast {
				if len(tileData)-offset < 4 {
					return ErrInvalidVP9Data
				}
				size := binary.BigEndian.Uint32(tileData[offset : offset+4])
				offset += 4
				if size > uint32(len(tileData)-offset) {
					return ErrInvalidVP9Data
				}
				tileSize = int(size)
			}
			if tileSize < 0 || offset+tileSize > len(tileData) {
				return ErrInvalidVP9Data
			}

			tile := vp9dec.TileBounds{
				MiRowStart: vp9DecoderTileOffset(tileRow, miRows, hdr.Tile.Log2TileRows),
				MiRowEnd:   vp9DecoderTileOffset(tileRow+1, miRows, hdr.Tile.Log2TileRows),
				MiColStart: vp9DecoderTileOffset(tileCol, miCols, hdr.Tile.Log2TileCols),
				MiColEnd:   vp9DecoderTileOffset(tileCol+1, miCols, hdr.Tile.Log2TileCols),
			}
			if err := d.parseVP9IntraModeTile(tileData[offset:offset+tileSize],
				hdr, comp, &maps, tile, miRows, miCols, partitionProbs); err != nil {
				return err
			}
			offset += tileSize
		}
	}
	if offset != len(tileData) {
		return ErrInvalidVP9Data
	}
	d.lastSegMap, d.segMap = d.segMap, d.lastSegMap
	return nil
}

func (d *VP9Decoder) parseVP9IntraModeTile(data []byte,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	maps *vp9dec.IntraSegmentMaps, tile vp9dec.TileBounds,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) error {
	var r bitstream.Reader
	if err := r.Init(data); err != nil {
		return ErrInvalidVP9Data
	}
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range d.leftSegCtx {
			d.leftSegCtx[i] = 0
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			if !d.readVP9IntraModeSb(&r, hdr, maps, tile, miRows, miCols,
				miRow, miCol, common.Block64x64, comp.TxMode, partitionProbs) {
				return ErrInvalidVP9Data
			}
		}
	}
	if r.HasError() {
		return ErrInvalidVP9Data
	}
	return nil
}

func (d *VP9Decoder) readVP9IntraModeSb(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, maps *vp9dec.IntraSegmentMaps,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txMode common.TxMode,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) bool {
	if miRow >= miRows || miCol >= miCols {
		return true
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	ctx := vp9dec.PartitionPlaneContext(d.aboveSegCtx, d.leftSegCtx, miRow, miCol, bsize)
	probs := partitionProbs[ctx][:]
	hasRows := miRow+bs < miRows
	hasCols := miCol+bs < miCols
	partition := vp9dec.ReadPartition(r, probs, hasRows, hasCols)
	subsize := common.SubsizeLookup[partition][bsize]
	if subsize >= common.BlockSizes {
		return false
	}

	if subsize < common.Block8x8 {
		d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol, subsize, txMode)
	} else {
		switch partition {
		case common.PartitionNone:
			d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol, subsize, txMode)
		case common.PartitionHorz:
			d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol, subsize, txMode)
			if miRow+bs < miRows {
				d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow+bs, miCol, subsize, txMode)
			}
		case common.PartitionVert:
			d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol, subsize, txMode)
			if miCol+bs < miCols {
				d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol+bs, subsize, txMode)
			}
		case common.PartitionSplit:
			if !d.readVP9IntraModeSb(r, hdr, maps, tile, miRows, miCols,
				miRow, miCol, subsize, txMode, partitionProbs) {
				return false
			}
			if !d.readVP9IntraModeSb(r, hdr, maps, tile, miRows, miCols,
				miRow, miCol+bs, subsize, txMode, partitionProbs) {
				return false
			}
			if !d.readVP9IntraModeSb(r, hdr, maps, tile, miRows, miCols,
				miRow+bs, miCol, subsize, txMode, partitionProbs) {
				return false
			}
			if !d.readVP9IntraModeSb(r, hdr, maps, tile, miRows, miCols,
				miRow+bs, miCol+bs, subsize, txMode, partitionProbs) {
				return false
			}
		default:
			return false
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(d.aboveSegCtx, d.leftSegCtx,
			miRow, miCol, subsize, 2*bs)
	}
	return true
}

func (d *VP9Decoder) readVP9IntraModeBlock(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, maps *vp9dec.IntraSegmentMaps,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txMode common.TxMode,
) {
	xMis := min(int(common.Num8x8BlocksWideLookup[bsize]), miCols-miCol)
	yMis := min(int(common.Num8x8BlocksHighLookup[bsize]), miRows-miRow)

	mi := &d.miGrid[miRow*miCols+miCol]
	*mi = vp9dec.NeighborMi{SbType: bsize}
	above := d.vp9DecoderMiAt(miRows, miCols, miRow-1, miCol)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = d.vp9DecoderMiAt(miRows, miCols, miRow, miCol-1)
	}
	vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
		Reader:   r,
		Fc:       &d.fc,
		Seg:      &hdr.Seg,
		Maps:     maps,
		TxMode:   txMode,
		MiOffset: miRow*miCols + miCol,
		XMis:     xMis,
		YMis:     yMis,
		Above:    above,
		Left:     left,
	}, mi)
	d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
}

func (d *VP9Decoder) ensureVP9DecoderModeBuffers(miRows, miCols int) {
	miColsAligned := alignToSb(miCols)
	if cap(d.aboveSegCtx) < miColsAligned {
		d.aboveSegCtx = make([]int8, miColsAligned)
	} else {
		d.aboveSegCtx = d.aboveSegCtx[:miColsAligned]
	}
	if cap(d.leftSegCtx) < common.MiBlockSize {
		d.leftSegCtx = make([]int8, common.MiBlockSize)
	} else {
		d.leftSegCtx = d.leftSegCtx[:common.MiBlockSize]
	}

	miGridLen := miRows * miCols
	if cap(d.miGrid) < miGridLen {
		d.miGrid = make([]vp9dec.NeighborMi, miGridLen)
	} else {
		d.miGrid = d.miGrid[:miGridLen]
	}
	if cap(d.segMap) < miGridLen {
		d.segMap = make([]uint8, miGridLen)
	} else {
		d.segMap = d.segMap[:miGridLen]
	}
	if cap(d.lastSegMap) < miGridLen {
		d.lastSegMap = make([]uint8, miGridLen)
	} else {
		d.lastSegMap = d.lastSegMap[:miGridLen]
	}
}

func (d *VP9Decoder) vp9DecoderMiAt(miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	return &d.miGrid[r*miCols+c]
}

func (d *VP9Decoder) fillVP9DecoderMiGrid(miRows, miCols, r, c int,
	bsize common.BlockSize, mi vp9dec.NeighborMi,
) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := d.miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

func vp9DecoderTileOffset(idx, mis, log2 int) int {
	sbCols := alignToSb(mis) >> common.MiBlockSizeLog2
	offset := ((idx * sbCols) >> uint(log2)) << common.MiBlockSizeLog2
	return min(offset, mis)
}
