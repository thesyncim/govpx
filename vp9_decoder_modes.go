package govpx

import (
	"encoding/binary"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// parseVP9IntraModeTiles consumes the tile mode-info and residual-token
// stream for keyframes and intra-only inter frames. For the currently
// supported zero-residue path, it also reconstructs each transform
// block from its intra predictor while walking the tile.
func (d *VP9Decoder) parseVP9IntraModeTiles(tileData []byte,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
) error {
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	vp9dec.SetupBlockPlanes(&d.planes, hdr.BitDepthColor.SubsamplingX,
		hdr.BitDepthColor.SubsamplingY)
	d.ensureVP9DecoderModeBuffers(miRows, miCols)
	for i := range d.aboveSegCtx {
		d.aboveSegCtx[i] = 0
	}
	d.resetVP9AboveEntropyContexts()
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
		d.resetVP9LeftEntropyContexts()
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

func (d *VP9Decoder) parseVP9InterModeTiles(tileData []byte,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
) error {
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	vp9dec.SetupBlockPlanes(&d.planes, hdr.BitDepthColor.SubsamplingX,
		hdr.BitDepthColor.SubsamplingY)
	d.ensureVP9DecoderModeBuffers(miRows, miCols)
	for i := range d.aboveSegCtx {
		d.aboveSegCtx[i] = 0
	}
	d.resetVP9AboveEntropyContexts()
	for i := range d.miGrid {
		d.miGrid[i] = vp9dec.NeighborMi{}
	}
	for i := range d.segMap {
		d.segMap[i] = 0
	}

	maps := vp9dec.InterSegmentMaps{
		IntraSegmentMaps: vp9dec.IntraSegmentMaps{
			CurrentFrameSegMap: d.segMap,
			LastFrameSegMap:    d.lastSegMap,
			MiCols:             miCols,
		},
	}
	partitionProbs := &d.fc.PartitionProb
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
			if err := d.parseVP9InterModeTile(tileData[offset:offset+tileSize],
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

func (d *VP9Decoder) parseVP9InterModeTile(data []byte,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	maps *vp9dec.InterSegmentMaps, tile vp9dec.TileBounds,
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
		d.resetVP9LeftEntropyContexts()
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			if !d.readVP9InterModeSb(&r, hdr, comp, maps, tile, miRows, miCols,
				miRow, miCol, common.Block64x64, partitionProbs) {
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
		if !d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol, subsize, txMode) {
			return false
		}
	} else {
		switch partition {
		case common.PartitionNone:
			if !d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol, subsize, txMode) {
				return false
			}
		case common.PartitionHorz:
			if !d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol, subsize, txMode) {
				return false
			}
			if miRow+bs < miRows {
				if !d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow+bs, miCol, subsize, txMode) {
					return false
				}
			}
		case common.PartitionVert:
			if !d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol, subsize, txMode) {
				return false
			}
			if miCol+bs < miCols {
				if !d.readVP9IntraModeBlock(r, hdr, maps, tile, miRows, miCols, miRow, miCol+bs, subsize, txMode) {
					return false
				}
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

func (d *VP9Decoder) readVP9InterModeSb(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	maps *vp9dec.InterSegmentMaps, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
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
		if !d.readVP9InterModeBlock(r, hdr, comp, maps, tile, miRows, miCols, miRow, miCol, subsize) {
			return false
		}
	} else {
		switch partition {
		case common.PartitionNone:
			if !d.readVP9InterModeBlock(r, hdr, comp, maps, tile, miRows, miCols, miRow, miCol, subsize) {
				return false
			}
		case common.PartitionHorz:
			if !d.readVP9InterModeBlock(r, hdr, comp, maps, tile, miRows, miCols, miRow, miCol, subsize) {
				return false
			}
			if miRow+bs < miRows {
				if !d.readVP9InterModeBlock(r, hdr, comp, maps, tile, miRows, miCols, miRow+bs, miCol, subsize) {
					return false
				}
			}
		case common.PartitionVert:
			if !d.readVP9InterModeBlock(r, hdr, comp, maps, tile, miRows, miCols, miRow, miCol, subsize) {
				return false
			}
			if miCol+bs < miCols {
				if !d.readVP9InterModeBlock(r, hdr, comp, maps, tile, miRows, miCols, miRow, miCol+bs, subsize) {
					return false
				}
			}
		case common.PartitionSplit:
			if !d.readVP9InterModeSb(r, hdr, comp, maps, tile, miRows, miCols,
				miRow, miCol, subsize, partitionProbs) {
				return false
			}
			if !d.readVP9InterModeSb(r, hdr, comp, maps, tile, miRows, miCols,
				miRow, miCol+bs, subsize, partitionProbs) {
				return false
			}
			if !d.readVP9InterModeSb(r, hdr, comp, maps, tile, miRows, miCols,
				miRow+bs, miCol, subsize, partitionProbs) {
				return false
			}
			if !d.readVP9InterModeSb(r, hdr, comp, maps, tile, miRows, miCols,
				miRow+bs, miCol+bs, subsize, partitionProbs) {
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
) bool {
	xMis := min(int(common.Num8x8BlocksWideLookup[bsize]), miCols-miCol)
	yMis := min(int(common.Num8x8BlocksHighLookup[bsize]), miRows-miRow)

	mi := &d.miGrid[miRow*miCols+miCol]
	*mi = vp9dec.NeighborMi{SbType: bsize}
	above := d.vp9DecoderMiAt(miRows, miCols, miRow-1, miCol)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = d.vp9DecoderMiAt(miRows, miCols, miRow, miCol-1)
	}
	out := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
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
	if mi.Skip != 0 {
		aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
		vp9dec.ResetSkipContext(d.planes[:], bsize, aboveOffsets[:], leftOffsets[:])
		if !d.unsupportedReconstruct {
			d.reconstructVP9IntraPredictBlock(hdr, mi, out.UvMode, tile,
				miRow, miCol, bsize)
		}
		d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
		return true
	}
	if !d.readVP9IntraResidueBlock(r, hdr, mi, out.UvMode, tile, miRow, miCol, bsize) {
		return false
	}
	d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
	return true
}

func (d *VP9Decoder) readVP9InterModeBlock(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	maps *vp9dec.InterSegmentMaps, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) bool {
	xMis := min(int(common.Num8x8BlocksWideLookup[bsize]), miCols-miCol)
	yMis := min(int(common.Num8x8BlocksHighLookup[bsize]), miRows-miRow)

	mi := &d.miGrid[miRow*miCols+miCol]
	*mi = vp9dec.NeighborMi{SbType: bsize}
	above := d.vp9DecoderMiAt(miRows, miCols, miRow-1, miCol)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = d.vp9DecoderMiAt(miRows, miCols, miRow, miCol-1)
	}

	d.segIDPredictedScratch = 0
	maps.SegIDPredictedOut = &d.segIDPredictedScratch
	segID := vp9dec.ReadInterSegmentId(r, &hdr.Seg, maps,
		miRow*miCols+miCol, xMis, yMis, above, left)
	mi.SegIDPredicted = d.segIDPredictedScratch
	if !hdr.Seg.TemporalUpdate {
		mi.SegIDPredicted = uint8(segID)
	}
	mi.Skip = uint8(vp9dec.ReadSkipWithSeg(r, &hdr.Seg, segID, &d.fc, above, left))
	isInter := vp9dec.ReadIsInterBlock(r, &hdr.Seg, segID, &d.fc, above, left)

	if bsize >= common.Block8x8 && comp.TxMode == common.TxModeSelect &&
		!(isInter != 0 && mi.Skip != 0) {
		mi.TxSize = vp9dec.ReadTxSize(r, &d.fc, comp.TxMode, bsize, above, left, true)
	} else {
		mi.TxSize = vp9dec.ReadTxSize(r, &d.fc, comp.TxMode, bsize, above, left, false)
	}

	uvMode := common.DcPred
	if isInter == 0 {
		mi.RefFrame = [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}
		uvMode = vp9dec.ReadIntraBlockModeInfoInter(r, &d.fc, mi)
	} else if !d.readVP9InterBlockModeInfo(r, hdr, comp, mi, segID, above, left) {
		return false
	}

	if mi.Skip != 0 {
		aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
		vp9dec.ResetSkipContext(d.planes[:], bsize, aboveOffsets[:], leftOffsets[:])
		d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
		return true
	}
	if !d.readVP9ResidueBlock(r, hdr, mi, uvMode, tile, miRow, miCol, bsize, segID, isInter) {
		return false
	}
	d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
	return true
}

func (d *VP9Decoder) readVP9InterBlockModeInfo(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	mi *vp9dec.NeighborMi, segID int,
	above, left *vp9dec.NeighborMi,
) bool {
	signBias := vp9FrameRefSignBias(hdr)
	refs := vp9dec.SetupCompoundReferenceMode(signBias)
	vp9dec.ReadRefFrames(r, comp.ReferenceMode, signBias, refs,
		&hdr.Seg, segID, &d.fc, above, left, &mi.RefFrame)

	if !vp9dec.SegFeatureActive(&hdr.Seg, segID, vp9dec.SegLvlSkip) {
		if mi.SbType >= common.Block8x8 {
			mi.Mode = vp9dec.ReadInterMode(r, d.fc.InterModeProbs[0])
		}
	}

	if hdr.InterpFilter == vp9dec.InterpSwitchable {
		mi.InterpFilter = uint8(vp9dec.ReadSwitchableInterpFilter(r, &d.fc, above, left))
	} else {
		mi.InterpFilter = uint8(hdr.InterpFilter)
	}

	isCompound := 0
	if mi.RefFrame[1] > vp9dec.IntraFrame {
		isCompound = 1
	}
	var mv, refMv, nearNearest [2]vp9dec.MV
	if mi.SbType < common.Block8x8 {
		num4x4W := int(common.Num4x4BlocksWideLookup[mi.SbType])
		num4x4H := int(common.Num4x4BlocksHighLookup[mi.SbType])
		for idy := 0; idy < 2; idy += num4x4H {
			for idx := 0; idx < 2; idx += num4x4W {
				j := idy*2 + idx
				mi.Bmi[j].AsMode = vp9dec.ReadInterMode(r, d.fc.InterModeProbs[0])
				if vp9dec.AssignMv(mi.Bmi[j].AsMode, &mi.Bmi[j].AsMv,
					&refMv, &nearNearest, isCompound, hdr.AllowHighPrecisionMv,
					r, &d.fc) == 0 {
					return false
				}
			}
		}
		return true
	}
	return vp9dec.AssignMv(mi.Mode, &mv, &refMv, &nearNearest,
		isCompound, hdr.AllowHighPrecisionMv, r, &d.fc) != 0
}

func (d *VP9Decoder) readVP9IntraResidueBlock(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, tile vp9dec.TileBounds,
	miRow, miCol int, bsize common.BlockSize,
) bool {
	return d.readVP9ResidueBlock(r, hdr, mi, uvMode, tile, miRow, miCol, bsize,
		int(mi.SegIDPredicted), 0)
}

func (d *VP9Decoder) readVP9ResidueBlock(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, tile vp9dec.TileBounds,
	miRow, miCol int, bsize common.BlockSize, segID int, isInter int,
) bool {
	aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		planeType := 0
		dequant := d.dq.Y[segID]
		if plane > 0 {
			planeType = 1
			dequant = d.dq.Uv[segID]
		}

		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return false
		}
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
		step := 1 << uint(txSize)
		aboveBase := aboveOffsets[plane]
		leftBase := leftOffsets[plane]
		blockIdx := 0

		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				aboveCtx := pd.AboveContext[aboveBase+cc : aboveBase+cc+step]
				leftCtx := pd.LeftContext[leftBase+rr : leftBase+rr+step]
				initCtx := vp9dec.GetEntropyContext(txSize, aboveCtx, leftCtx)
				scanOrder := common.GetScan(txSize, planeType, isInter,
					hdr.Quant.Lossless, mode)
				maxEob := vp9dec.MaxEobForTxSize(txSize)
				coeffs := d.dqcoeff[:maxEob]
				for i := range coeffs {
					coeffs[i] = 0
				}

				eob := vp9dec.DecodeCoefs(r, txSize, planeType, 0, dequant,
					initCtx, scanOrder.Scan, scanOrder.Neighbors, &d.fc.CoefProbs, coeffs)
				if isInter == 0 && !d.unsupportedReconstruct {
					dst, stride, ok := d.reconstructVP9IntraPredictTx(hdr, pd, plane,
						mode, txSize, tile, miRow, miCol, rr, cc)
					if !ok {
						d.unsupportedReconstruct = true
					} else if eob > 0 && dst != nil {
						txType := common.DctDct
						if planeType == 0 && !hdr.Quant.Lossless {
							txType = common.IntraModeToTxType[mode]
						}
						vp9dec.InverseTransformBlock(coeffs, dst, stride, txSize,
							txType, eob, hdr.Quant.Lossless)
					}
				}
				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				for i := range step {
					aboveCtx[i] = hasResidue
					leftCtx[i] = hasResidue
				}
				blockIdx += step * step
			}
		}
	}
	return true
}

func (d *VP9Decoder) reconstructVP9IntraPredictBlock(
	hdr *vp9dec.UncompressedHeader,
	mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode,
	tile vp9dec.TileBounds,
	miRow, miCol int,
	bsize common.BlockSize,
) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			d.unsupportedReconstruct = true
			return
		}

		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
		step := 1 << uint(txSize)
		blockIdx := 0
		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				if _, _, ok := d.reconstructVP9IntraPredictTx(hdr, pd, plane, mode, txSize,
					tile, miRow, miCol, rr, cc); !ok {
					d.unsupportedReconstruct = true
					return
				}
				blockIdx += step * step
			}
		}
	}
}

func (d *VP9Decoder) reconstructVP9IntraPredictTx(
	hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane,
	plane int,
	mode common.PredictionMode,
	txSize common.TxSize,
	tile vp9dec.TileBounds,
	miRow, miCol int,
	blockRow4x4, blockCol4x4 int,
) (dst []byte, stride int, ok bool) {
	planeData, stride := d.vp9OutputPlane(plane)
	if stride <= 0 || len(planeData) == 0 || int(mode) >= common.IntraModes {
		return nil, 0, false
	}
	rows := len(planeData) / stride
	planeWidth, planeHeight := vp9dec.FramePlaneDims(int(hdr.Width), int(hdr.Height),
		pd.SubsamplingX, pd.SubsamplingY)
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 := baseX + blockCol4x4*4
	y0 := baseY + blockRow4x4*4
	if x0 >= planeWidth || y0 >= planeHeight {
		return nil, 0, true
	}

	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, false
	}

	leftAvailable := x0 > 0 &&
		x0 > (tile.MiColStart*common.MiSize)>>pd.SubsamplingX
	left := d.intraScratch.Left[:bs]
	if leftAvailable {
		for i := range bs {
			left[i] = planeData[(y0+i)*stride+x0-1]
		}
	}

	edges := vp9dec.IntraEdgeRefs{
		AboveLeft: 127,
		Left:      left,
	}
	upAvailable := y0 > 0
	if upAvailable {
		edges.Above = planeData[(y0-1)*stride+x0:]
		if leftAvailable {
			edges.AboveLeft = planeData[(y0-1)*stride+x0-1]
		}
	}
	rightAvailable := x0+2*bs <= planeWidth
	dst = planeData[y0*stride+x0:]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            dst,
		DstStride:      stride,
		Mode:           mode,
		TxSize:         txSize,
		Edges:          edges,
		UpAvailable:    upAvailable,
		LeftAvailable:  leftAvailable,
		RightAvailable: rightAvailable,
		FrameWidth:     planeWidth,
		FrameHeight:    planeHeight,
		X0:             x0,
		Y0:             y0,
		MbToRightEdge:  planeWidth - (x0 + bs),
		MbToBottomEdge: planeHeight - (y0 + bs),
	}, &d.intraScratch)
	return dst, stride, true
}

func (d *VP9Decoder) vp9OutputPlane(plane int) ([]byte, int) {
	switch plane {
	case 0:
		return d.frameY, d.lastFrame.YStride
	case 1:
		return d.frameU, d.lastFrame.UStride
	case 2:
		return d.frameV, d.lastFrame.VStride
	default:
		return nil, 0
	}
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

	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		aboveLen := vp9PlaneEntropyLen(miColsAligned, pd.SubsamplingX)
		leftLen := vp9PlaneEntropyLen(common.MiBlockSize, pd.SubsamplingY)
		if cap(pd.AboveContext) < aboveLen {
			pd.AboveContext = make([]uint8, aboveLen)
		} else {
			pd.AboveContext = pd.AboveContext[:aboveLen]
		}
		if cap(pd.LeftContext) < leftLen {
			pd.LeftContext = make([]uint8, leftLen)
		} else {
			pd.LeftContext = pd.LeftContext[:leftLen]
		}
	}
}

func vp9PlaneEntropyLen(miCount int, subsampling uint8) int {
	return (miCount * 2) >> subsampling
}

func (d *VP9Decoder) resetVP9AboveEntropyContexts() {
	for plane := range vp9dec.MaxMbPlane {
		ctx := d.planes[plane].AboveContext
		for i := range ctx {
			ctx[i] = 0
		}
	}
}

func (d *VP9Decoder) resetVP9LeftEntropyContexts() {
	for plane := range vp9dec.MaxMbPlane {
		ctx := d.planes[plane].LeftContext
		for i := range ctx {
			ctx[i] = 0
		}
	}
}

func (d *VP9Decoder) vp9PlaneContextOffsets(miRow, miCol int) (
	above [vp9dec.MaxMbPlane]int, left [vp9dec.MaxMbPlane]int,
) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		above[plane] = (miCol * 2) >> pd.SubsamplingX
		left[plane] = ((miRow * 2) >> pd.SubsamplingY) % len(pd.LeftContext)
	}
	return above, left
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
