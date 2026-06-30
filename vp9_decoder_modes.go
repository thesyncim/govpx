package govpx

import (
	"encoding/binary"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
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
	if vp9dec.HeaderResetsPastIndependence(hdr) {
		d.resetVP9SegmentationMapsForPastIndependence()
	}

	partitionProbs := &tables.KfPartitionProbs
	tileRows := 1 << uint(hdr.Tile.Log2TileRows)
	tileCols := 1 << uint(hdr.Tile.Log2TileCols)
	if d.vp9DecoderTileThreadingEnabled(hdr, tileRows, tileCols) {
		if err := d.parseVP9IntraModeTilesThreaded(tileData, *hdr, comp,
			miRows, miCols, partitionProbs); err != nil {
			return err
		}
		if hdr.Seg.Enabled {
			d.lastSegMap, d.segMap = d.segMap, d.lastSegMap
		}
		return nil
	}
	descs, err := prepareVP9DecoderTileDescs(d.vp9TileDescs, tileData, *hdr,
		miRows, miCols)
	if err != nil {
		return err
	}
	d.vp9TileDescs = descs
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: d.segMap,
		LastFrameSegMap:    d.lastSegMap,
		MiCols:             miCols,
	}

	for tileRow := range tileRows {
		for tileColIter := range tileCols {
			tileCol := d.vp9DecodeTileCol(tileColIter, tileCols)
			if d.vp9TileFilterMasksTile(tileRow, tileCol, tileRows, tileCols) {
				continue
			}
			desc := descs[tileRow*tileCols+tileCol]
			if err := d.parseVP9IntraModeTile(desc.data, hdr, comp, &maps,
				desc.tile, miRows, miCols, partitionProbs); err != nil {
				return err
			}
		}
	}
	if hdr.Seg.Enabled {
		d.lastSegMap, d.segMap = d.segMap, d.lastSegMap
	}
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
	rowMT := d.rowMTSync
	if rowMT == nil {
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
	tileSbCols := (tile.MiColEnd - tile.MiColStart + common.MiBlockSize - 1) >>
		common.MiBlockSizeLog2
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range d.leftSegCtx {
			d.leftSegCtx[i] = 0
		}
		d.resetVP9LeftEntropyContexts()
		sbRow := (miRow - tile.MiRowStart) >> common.MiBlockSizeLog2
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			sbCol := (miCol - tile.MiColStart) >> common.MiBlockSizeLog2
			// Wavefront: wait for the row above to decode the above and
			// above-right SB before consuming their entropy / above-context
			// state when DecoderRowMT is armed.
			rowMT.read(sbRow, sbCol)
			if !d.readVP9IntraModeSb(&r, hdr, maps, tile, miRows, miCols,
				miRow, miCol, common.Block64x64, comp.TxMode, partitionProbs) {
				return ErrInvalidVP9Data
			}
			rowMT.write(sbRow, sbCol, tileSbCols)
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
	if vp9dec.HeaderResetsPastIndependence(hdr) {
		d.resetVP9SegmentationMapsForPastIndependence()
	}

	partitionProbs := &d.fc.PartitionProb
	tileRows := 1 << uint(hdr.Tile.Log2TileRows)
	tileCols := 1 << uint(hdr.Tile.Log2TileCols)
	if d.vp9DecoderTileThreadingEnabled(hdr, tileRows, tileCols) {
		if err := d.parseVP9InterModeTilesThreaded(tileData, *hdr, comp,
			miRows, miCols, partitionProbs); err != nil {
			return err
		}
		if hdr.Seg.Enabled {
			d.lastSegMap, d.segMap = d.segMap, d.lastSegMap
		}
		return nil
	}
	descs, err := prepareVP9DecoderTileDescs(d.vp9TileDescs, tileData, *hdr,
		miRows, miCols)
	if err != nil {
		return err
	}
	d.vp9TileDescs = descs
	maps := vp9dec.InterSegmentMaps{
		IntraSegmentMaps: vp9dec.IntraSegmentMaps{
			CurrentFrameSegMap: d.segMap,
			LastFrameSegMap:    d.lastSegMap,
			MiCols:             miCols,
		},
	}

	for tileRow := range tileRows {
		for tileColIter := range tileCols {
			tileCol := d.vp9DecodeTileCol(tileColIter, tileCols)
			if d.vp9TileFilterMasksTile(tileRow, tileCol, tileRows, tileCols) {
				continue
			}
			desc := descs[tileRow*tileCols+tileCol]
			if err := d.parseVP9InterModeTile(desc.data, hdr, comp, &maps,
				desc.tile, miRows, miCols, partitionProbs); err != nil {
				return err
			}
		}
	}
	if hdr.Seg.Enabled {
		d.lastSegMap, d.segMap = d.segMap, d.lastSegMap
	}
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
	rowMT := d.rowMTSync
	if rowMT == nil {
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
	tileSbCols := (tile.MiColEnd - tile.MiColStart + common.MiBlockSize - 1) >>
		common.MiBlockSizeLog2
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range d.leftSegCtx {
			d.leftSegCtx[i] = 0
		}
		d.resetVP9LeftEntropyContexts()
		sbRow := (miRow - tile.MiRowStart) >> common.MiBlockSizeLog2
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			sbCol := (miCol - tile.MiColStart) >> common.MiBlockSizeLog2
			// Wavefront: wait for the row above to decode the above and
			// above-right SB before consuming their entropy / above-context
			// state when DecoderRowMT is armed.
			rowMT.read(sbRow, sbCol)
			if !d.readVP9InterModeSb(&r, hdr, comp, maps, tile, miRows, miCols,
				miRow, miCol, common.Block64x64, partitionProbs) {
				return ErrInvalidVP9Data
			}
			rowMT.write(sbRow, sbCol, tileSbCols)
		}
	}
	if r.HasError() {
		return ErrInvalidVP9Data
	}
	return nil
}

func (d *VP9Decoder) resetVP9SegmentationMapsForPastIndependence() {
	for i := range d.segMap {
		d.segMap[i] = 0
	}
	for i := range d.lastSegMap {
		d.lastSegMap[i] = 0
	}
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
	if !hdr.FrameParallelDecoding {
		d.counts.Partition[ctx][partition]++
	}
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
			miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
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
	if !hdr.FrameParallelDecoding {
		d.counts.Partition[ctx][partition]++
	}
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
			miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
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
	reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
	if mi.Skip != 0 {
		aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
		vp9dec.ResetSkipContext(d.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
		if !d.unsupportedReconstruct {
			d.reconstructVP9IntraPredictBlock(hdr, mi, out.UvMode, tile,
				miRow, miCol, reconBsize)
		}
		d.storeVP9CurrentFrameMvs(miRows, miCols, miRow, miCol, xMis, yMis, mi)
		d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
		return true
	}
	if !d.readVP9IntraResidueBlock(r, hdr, mi, out.UvMode, tile, miRow, miCol, reconBsize) {
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
	mi.SegmentID = uint8(segID)
	mi.SegIDPredicted = d.segIDPredictedScratch
	if !hdr.Seg.TemporalUpdate {
		mi.SegIDPredicted = uint8(segID)
	}
	mi.Skip = uint8(d.readVP9SkipWithCounts(r, hdr, segID, above, left))
	isInter := d.readVP9IsInterBlockWithCounts(r, hdr, segID, above, left)

	if bsize >= common.Block8x8 && comp.TxMode == common.TxModeSelect &&
		!(isInter != 0 && mi.Skip != 0) {
		mi.TxSize = d.readVP9TxSizeWithCounts(r, hdr, comp.TxMode, bsize, above, left, true)
	} else {
		mi.TxSize = d.readVP9TxSizeWithCounts(r, hdr, comp.TxMode, bsize, above, left, false)
	}

	uvMode := common.DcPred
	if isInter == 0 {
		uvMode = d.readVP9IntraBlockModeInfoInterWithCounts(r, hdr, mi)
	} else if !d.readVP9InterBlockModeInfo(r, hdr, comp, mi, segID, above, left,
		tile, miRows, miCols, miRow, miCol) {
		return false
	}

	reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
	if mi.Skip != 0 {
		aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
		vp9dec.ResetSkipContext(d.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
		if !d.unsupportedReconstruct {
			if isInter != 0 {
				if !d.reconstructVP9InterPredictBlock(hdr, mi, miRow, miCol, reconBsize) {
					return false
				}
			} else {
				d.reconstructVP9IntraPredictBlock(hdr, mi, uvMode, tile,
					miRow, miCol, reconBsize)
			}
		}
		d.storeVP9CurrentFrameMvs(miRows, miCols, miRow, miCol, xMis, yMis, mi)
		d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
		return true
	}
	if isInter != 0 && !vp9dec.CanReconstructInterBlock(mi) {
		d.unsupportedReconstruct = true
	}
	if !d.readVP9ResidueBlock(r, hdr, mi, uvMode, tile, miRow, miCol, reconBsize, segID, isInter) {
		return false
	}
	d.storeVP9CurrentFrameMvs(miRows, miCols, miRow, miCol, xMis, yMis, mi)
	d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
	return true
}

func (d *VP9Decoder) readVP9InterBlockModeInfo(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	mi *vp9dec.NeighborMi, segID int,
	above, left *vp9dec.NeighborMi,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
) bool {
	signBias := vp9dec.FrameRefSignBias(hdr)
	refs := vp9dec.SetupCompoundReferenceMode(signBias)
	d.readVP9RefFramesWithCounts(r, hdr, comp.ReferenceMode, signBias, refs,
		segID, above, left, &mi.RefFrame)
	interModeCtx := vp9dec.InterModeContext(d.miGrid, miCols, tile,
		miRows, miRow, miCol, mi.SbType)

	if vp9dec.SegFeatureActive(&hdr.Seg, segID, vp9dec.SegLvlSkip) {
		mi.Mode = common.ZeroMv
		if mi.SbType < common.Block8x8 {
			return false
		}
	} else {
		if mi.SbType >= common.Block8x8 {
			mi.Mode = d.readVP9InterModeWithCounts(r, hdr, interModeCtx)
		}
	}

	if hdr.InterpFilter == vp9dec.InterpSwitchable {
		mi.InterpFilter = uint8(d.readVP9SwitchableInterpFilterWithCounts(r, hdr, above, left))
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
		gotMvRefsForNew := false
		for idy := 0; idy < 2; idy += num4x4H {
			for idx := 0; idx < 2; idx += num4x4W {
				j := idy*2 + idx
				bMode := d.readVP9InterModeWithCounts(r, hdr, interModeCtx)
				mi.Bmi[j].AsMode = bMode
				if bMode == common.NearestMv || bMode == common.NearMv {
					halves := 1 + isCompound
					for ref := range halves {
						nearNearest[ref] = d.vp9AppendSub8x8MvsForIdx(tile,
							miRows, miCols, miRow, miCol, mi.SbType,
							bMode, j, ref, mi.RefFrame[ref], signBias)
					}
				} else if bMode == common.NewMv && !gotMvRefsForNew {
					halves := 1 + isCompound
					for ref := range halves {
						refList, _ := d.vp9FindInterMvRefsForBlock(tile, miRows, miCols,
							miRow, miCol, mi.SbType, common.NewMv,
							mi.RefFrame[ref], signBias, -1)
						best := refList[0]
						vp9dec.LowerMvPrecision(&best, hdr.AllowHighPrecisionMv)
						refMv[ref] = best
					}
					gotMvRefsForNew = true
				}
				if vp9dec.AssignMv(bMode, &mi.Bmi[j].AsMv,
					&refMv, &nearNearest, isCompound, hdr.AllowHighPrecisionMv,
					r, &d.fc) == 0 {
					return false
				}
				if !hdr.FrameParallelDecoding && bMode == common.NewMv {
					halves := 1 + isCompound
					for ref := range halves {
						d.countVP9NewMv(mi.Bmi[j].AsMv[ref], refMv[ref])
					}
				}
				if num4x4H == 2 {
					mi.Bmi[j+2] = mi.Bmi[j]
				}
				if num4x4W == 2 {
					mi.Bmi[j+1] = mi.Bmi[j]
				}
			}
		}
		mi.Mode = mi.Bmi[3].AsMode
		mi.Mv = mi.Bmi[3].AsMv
		return true
	}
	if mi.Mode != common.ZeroMv {
		halves := 1 + isCompound
		for ref := range halves {
			refList, refCount := d.vp9FindInterMvRefs(tile, miRows, miCols,
				miRow, miCol, mi.SbType, mi.Mode, mi.RefFrame[ref], signBias)
			best := vp9dec.InterModeMvCandidate(refList, refCount, mi.Mode)
			vp9dec.LowerMvPrecision(&best, hdr.AllowHighPrecisionMv)
			refMv[ref] = best
			nearNearest[ref] = best
		}
	}
	if vp9dec.AssignMv(mi.Mode, &mv, &refMv, &nearNearest,
		isCompound, hdr.AllowHighPrecisionMv, r, &d.fc) == 0 {
		return false
	}
	if !hdr.FrameParallelDecoding && mi.Mode == common.NewMv {
		halves := 1 + isCompound
		for ref := range halves {
			d.countVP9NewMv(mv[ref], refMv[ref])
		}
	}
	mi.Mv = mv
	return true
}

func (d *VP9Decoder) vp9FindInterMvRefs(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	mode common.PredictionMode,
	refFrame int8,
	signBias [vp9dec.MaxRefFrames]uint8,
) ([2]vp9dec.MV, int) {
	return vp9dec.FindInterMvRefsFields(d.miGrid, d.usePrevFrameMvs,
		d.prevFrameMvs, d.prevFrameMvRows, d.prevFrameMvCols,
		tile, miRows, miCols, miRow, miCol, bsize, mode, refFrame,
		signBias, -1)
}

func (d *VP9Decoder) vp9FindInterMvRefsForBlock(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	mode common.PredictionMode,
	refFrame int8,
	signBias [vp9dec.MaxRefFrames]uint8,
	block int,
) ([2]vp9dec.MV, int) {
	return vp9dec.FindInterMvRefsFields(d.miGrid, d.usePrevFrameMvs,
		d.prevFrameMvs, d.prevFrameMvRows, d.prevFrameMvCols,
		tile, miRows, miCols, miRow, miCol, bsize, mode, refFrame,
		signBias, block)
}

func (d *VP9Decoder) vp9AppendSub8x8MvsForIdx(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	mode common.PredictionMode,
	block, ref int,
	refFrame int8,
	signBias [vp9dec.MaxRefFrames]uint8,
) vp9dec.MV {
	mi := &d.miGrid[miRow*miCols+miCol]
	refList, refCount := d.vp9FindInterMvRefsForBlock(tile, miRows, miCols,
		miRow, miCol, bsize, mode, refFrame, signBias, block)
	switch block {
	case 0:
		return refList[refCount-1]
	case 1, 2:
		if mode == common.NearestMv {
			return mi.Bmi[0].AsMv[ref]
		}
		for i := range refList {
			if refList[i] != mi.Bmi[0].AsMv[ref] {
				return refList[i]
			}
		}
	case 3:
		if mode == common.NearestMv {
			return mi.Bmi[2].AsMv[ref]
		}
		if mi.Bmi[2].AsMv[ref] != mi.Bmi[1].AsMv[ref] {
			return mi.Bmi[1].AsMv[ref]
		}
		if mi.Bmi[2].AsMv[ref] != mi.Bmi[0].AsMv[ref] {
			return mi.Bmi[0].AsMv[ref]
		}
		for i := range refList {
			if refList[i] != mi.Bmi[2].AsMv[ref] {
				return refList[i]
			}
		}
	}
	return vp9dec.MV{}
}

func (d *VP9Decoder) readVP9IntraResidueBlock(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, tile vp9dec.TileBounds,
	miRow, miCol int, bsize common.BlockSize,
) bool {
	return d.readVP9ResidueBlock(r, hdr, mi, uvMode, tile, miRow, miCol, bsize,
		int(mi.SegmentID), 0)
}

func (d *VP9Decoder) reconstructVP9InterPredictBlock(
	hdr *vp9dec.UncompressedHeader,
	mi *vp9dec.NeighborMi,
	miRow, miCol int,
	bsize common.BlockSize,
) bool {
	if d.unsupportedReconstruct {
		return true
	}
	if !vp9dec.CanReconstructInterBlock(mi) {
		d.markVP9Unsupported()
		return true
	}
	nrefs := 1
	if mi.RefFrame[1] > vp9dec.IntraFrame {
		nrefs = 2
	}
	for plane := range vp9dec.MaxMbPlane {
		// Encoder motion-search/distortion measurements only consult the
		// luma plane; skip U/V to avoid two extra convolutions per
		// candidate. libvpx mirrors this with
		// vp9_build_inter_predictors_sby (luma) called from
		// nonrd_pickmode for mode scoring; chroma reconstruction
		// (vp9_build_inter_predictors_sbuv) is deferred until the
		// committed mode is encoded.
		// libvpx: vp9/encoder/vp9_pickmode.c:2336.
		if d.predictLumaOnly && plane > 0 {
			continue
		}
		if d.predictChromaOnly && plane == 0 {
			continue
		}
		pd := &d.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			d.markVP9Unsupported()
			return true
		}
		dst, dstStride := d.vp9OutputPlane(plane)
		if dstStride <= 0 || len(dst) == 0 {
			return false
		}
		dstRows := len(dst) / dstStride
		x0 := (miCol * common.MiSize) >> pd.SubsamplingX
		y0 := (miRow * common.MiSize) >> pd.SubsamplingY
		if x0 >= dstStride || y0 >= dstRows {
			continue
		}
		bw := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		bh := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		if x0+bw > dstStride || y0+bh > dstRows {
			d.markVP9Unsupported()
			return true
		}
		for refIdx := range nrefs {
			refSlot, ok := vp9dec.InterReferenceSlot(hdr, mi.RefFrame[refIdx])
			if !ok {
				return false
			}
			ref := &d.refFrames[refSlot]
			if !ref.valid {
				return false
			}
			var sf vp9dec.ScaleFactors
			vp9dec.SetupScaleFactorsForFrame(&sf, ref.img.Width, ref.img.Height,
				int(hdr.Width), int(hdr.Height))
			if !sf.IsValidScale() {
				d.markVP9Unsupported()
				return true
			}
			src, srcStride := vp9ReferencePlane(ref, plane)
			if srcStride <= 0 || len(src) == 0 {
				return false
			}
			srcRows := len(src) / srcStride
			if !sf.IsScaled() && (x0 >= srcStride || y0 >= srcRows) {
				continue
			}
			srcWidth := (ref.img.Width + (1 << pd.SubsamplingX) - 1) >> pd.SubsamplingX
			srcHeight := (ref.img.Height + (1 << pd.SubsamplingY) - 1) >> pd.SubsamplingY
			if srcWidth <= 0 || srcHeight <= 0 {
				return false
			}
			avg := refIdx == 1
			if mi.SbType < common.Block8x8 {
				block := 0
				num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
				num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
				for y := range num4x4H {
					for x := range num4x4W {
						mv := vp9dec.AverageSplitMvs(&mi.Bmi, refIdx, block,
							int(pd.SubsamplingX), int(pd.SubsamplingY))
						if !d.reconstructVP9InterPredictPlane(hdr, pd, mi, bsize,
							miRow, miCol, x0, y0, 4*x, 4*y, bw, bh, 4, 4,
							dst, dstStride, dstRows, src, srcStride, srcRows,
							srcWidth, srcHeight, &sf, mv, vp9dec.BoolInt(avg)) {
							return false
						}
						block++
					}
				}
				continue
			}
			if !sf.IsScaled() && mi.Mv[refIdx] == (vp9dec.MV{}) &&
				(srcWidth&0x7) == 0 && (srcHeight&0x7) == 0 {
				w := min(bw, min(dstStride-x0, srcStride-x0))
				h := min(bh, min(dstRows-y0, srcRows-y0))
				if w <= 0 || h <= 0 {
					continue
				}
				if avg {
					buffers.AveragePlaneInto(dst[y0*dstStride+x0:], dstStride,
						src[y0*srcStride+x0:], srcStride, w, h)
				} else {
					buffers.CopyPlane(dst[y0*dstStride+x0:], dstStride,
						src[y0*srcStride+x0:], srcStride, w, h)
				}
				continue
			}
			if !d.reconstructVP9InterPredictPlane(hdr, pd, mi, bsize,
				miRow, miCol, x0, y0, 0, 0, bw, bh, bw, bh,
				dst, dstStride, dstRows, src, srcStride, srcRows,
				srcWidth, srcHeight, &sf, mi.Mv[refIdx], vp9dec.BoolInt(avg)) {
				return false
			}
		}
	}
	return true
}

func (d *VP9Decoder) reconstructVP9InterPredictPlane(
	hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane,
	mi *vp9dec.NeighborMi,
	bsize common.BlockSize,
	miRow, miCol int,
	baseX, baseY, blockX, blockY, bw, bh, predW, predH int,
	dst []byte,
	dstStride, dstRows int,
	src []byte,
	srcStride, srcRows int,
	srcWidth, srcHeight int,
	sf *vp9dec.ScaleFactors,
	mv vp9dec.MV,
	avg int,
) bool {
	filterIdx := int(mi.InterpFilter)
	if filterIdx < 0 || filterIdx >= int(vp9dec.InterpSwitchable) {
		d.markVP9Unsupported()
		return true
	}
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	edges := vp9dec.BlockBoundsEdges{
		MbToLeftEdge:   -((miCol * common.MiSize) * 8),
		MbToRightEdge:  ((miCols - int(common.Num8x8BlocksWideLookup[bsize]) - miCol) * common.MiSize) * 8,
		MbToTopEdge:    -((miRow * common.MiSize) * 8),
		MbToBottomEdge: ((miRows - int(common.Num8x8BlocksHighLookup[bsize]) - miRow) * common.MiSize) * 8,
	}
	mvQ4 := vp9dec.ClampMvToUmvBorderSb(edges, mv, bw, bh,
		int(pd.SubsamplingX), int(pd.SubsamplingY))
	dstX := baseX + blockX
	dstY := baseY + blockY
	srcX := dstX
	srcY := dstY
	srcX16 := srcX << vp9dec.SubpelBitsConst
	srcY16 := srcY << vp9dec.SubpelBitsConst
	scaledMV := vp9dec.MV32{Row: int32(mvQ4.Row), Col: int32(mvQ4.Col)}
	xStepQ4 := vp9dec.SubpelShifts
	yStepQ4 := vp9dec.SubpelShifts
	scaled := sf != nil && sf.IsScaled()
	if scaled {
		srcX16 = sf.ScaleValueX(srcX16)
		srcY16 = sf.ScaleValueY(srcY16)
		srcX = sf.ScaleValueX(srcX)
		srcY = sf.ScaleValueY(srcY)
		scaledMV = vp9dec.ScaleMv(mvQ4,
			miCol*common.MiSize+blockX, miRow*common.MiSize+blockY, sf)
		xStepQ4 = sf.XStepQ4
		yStepQ4 = sf.YStepQ4
	}
	subpelX := int(scaledMV.Col) & (vp9dec.SubpelShifts - 1)
	subpelY := int(scaledMV.Row) & (vp9dec.SubpelShifts - 1)
	srcX += int(scaledMV.Col) >> vp9dec.SubpelBitsConst
	srcY += int(scaledMV.Row) >> vp9dec.SubpelBitsConst
	srcX16 += int(scaledMV.Col)
	srcY16 += int(scaledMV.Row)

	srcOffset := srcY*srcStride + srcX
	if scaled || scaledMV.Col != 0 || scaledMV.Row != 0 ||
		(srcWidth&0x7) != 0 || (srcHeight&0x7) != 0 {
		x1 := ((srcX16 + (predW-1)*xStepQ4) >> vp9dec.SubpelBitsConst) + 1
		y1 := ((srcY16 + (predH-1)*yStepQ4) >> vp9dec.SubpelBitsConst) + 1
		extX0, extY0 := srcX, srcY
		xPad, yPad := 0, 0
		if subpelX != 0 || xStepQ4 != vp9dec.SubpelShifts {
			extX0 -= vp9dec.VP9InterpExtend - 1
			x1 += vp9dec.VP9InterpExtend
			xPad = 1
		}
		if subpelY != 0 || yStepQ4 != vp9dec.SubpelShifts {
			extY0 -= vp9dec.VP9InterpExtend - 1
			y1 += vp9dec.VP9InterpExtend
			yPad = 1
		}
		if extX0 < 0 || extX0 > srcWidth-1 || x1 < 0 || x1 > srcWidth-1 ||
			extY0 < 0 || extY0 > srcHeight-1 || y1 < 0 || y1 > srcHeight-1 {
			extW := x1 - extX0 + 1
			extH := y1 - extY0 + 1
			if extW <= 0 || extH <= 0 {
				return false
			}
			borderOffset := yPad*(vp9dec.VP9InterpExtend-1)*extW +
				xPad*(vp9dec.VP9InterpExtend-1)
			src, srcStride, srcOffset = d.vp9ExtendInterPredictSource(
				src, srcStride, srcWidth, srcHeight, extX0, extY0, extW, extH,
				borderOffset)
			srcRows = extH
		}
	}
	if srcOffset < 0 || srcOffset >= len(src) ||
		dstX+predW > dstStride || dstY+predH > dstRows || srcRows <= 0 {
		return false
	}
	vp9dec.InterPredictor(src, srcStride, dst[dstY*dstStride+dstX:], dstStride,
		subpelX, subpelY, tables.FilterKernels[filterIdx],
		xStepQ4, yStepQ4, predW, predH, avg, srcOffset)
	return true
}

func (d *VP9Decoder) vp9ExtendInterPredictSource(src []byte, srcStride int,
	srcWidth, srcHeight int,
	startX, startY, extStride, extRows int,
	srcOffset int,
) ([]byte, int, int) {
	need := extStride * extRows
	d.interPredictScratch = buffers.EnsureLen(d.interPredictScratch, need)
	// libvpx: vp9/decoder/vp9_decodeframe.c:458 build_mc_border splits
	// each row into a left memset (clamped to src[0]), a center memcpy
	// of in-bounds pixels, and a right memset (clamped to src[w-1]).
	// Replaces the per-pixel clamp loop, which was the inner hot loop
	// when motion vectors push the kernel window outside the frame
	// boundary.
	w := srcWidth
	h := srcHeight
	for y := range extRows {
		sy := startY + y
		if sy < 0 {
			sy = 0
		} else if sy > h-1 {
			sy = h - 1
		}
		srcRow := src[sy*srcStride:]
		dstRow := d.interPredictScratch[y*extStride : y*extStride+extStride]
		x := startX
		left := 0
		if x < 0 {
			left = -x
		}
		if left > extStride {
			left = extStride
		}
		right := 0
		if x+extStride > w {
			right = x + extStride - w
		}
		if right > extStride {
			right = extStride
		}
		copyN := extStride - left - right
		if left > 0 {
			leftFill := srcRow[0]
			for i := 0; i < left; i++ {
				dstRow[i] = leftFill
			}
		}
		if copyN > 0 {
			copy(dstRow[left:left+copyN], srcRow[x+left:x+left+copyN])
		}
		if right > 0 {
			rightFill := srcRow[w-1]
			for i := 0; i < right; i++ {
				dstRow[left+copyN+i] = rightFill
			}
		}
	}
	return d.interPredictScratch, extStride, srcOffset
}

func vp9ReferencePlane(ref *vp9ReferenceFrame, plane int) ([]byte, int) {
	switch plane {
	case 0:
		return ref.img.Y, ref.img.YStride
	case 1:
		return ref.img.U, ref.img.UStride
	case 2:
		return ref.img.V, ref.img.VStride
	default:
		return nil, 0
	}
}

func (d *VP9Decoder) storeVP9CurrentFrameMvs(miRows, miCols, miRow, miCol,
	xMis, yMis int, mi *vp9dec.NeighborMi,
) {
	if len(d.curFrameMvs) < miRows*miCols || mi == nil {
		return
	}
	refFrame := mi.RefFrame
	mv := mi.Mv
	for y := 0; y < yMis && miRow+y < miRows; y++ {
		row := d.curFrameMvs[(miRow+y)*miCols:]
		for x := 0; x < xMis && miCol+x < miCols; x++ {
			row[miCol+x] = vp9dec.MvRef{RefFrame: refFrame, Mv: mv}
		}
	}
}

func (d *VP9Decoder) readVP9ResidueBlock(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, tile vp9dec.TileBounds,
	miRow, miCol int, bsize common.BlockSize, segID int, isInter int,
) bool {
	aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	if isInter != 0 && !d.unsupportedReconstruct {
		if !d.reconstructVP9InterPredictBlock(hdr, mi, miRow, miCol, bsize) {
			return false
		}
	}
	eobTotal := 0
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
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4W, num4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
			bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((full4x4W - num4x4W) >> txSize) * blockStep
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

				var coefCounts *vp9dec.CoefCounts
				if !hdr.FrameParallelDecoding {
					coefCounts = &d.counts.Coef
				}
				eob := vp9dec.DecodeCoefsWithCounts(r, txSize, planeType, isInter, dequant,
					initCtx, scanOrder.Scan, scanOrder.Neighbors, &d.fc.CoefProbs,
					coefCounts, coeffs)
				eobTotal += eob
				if isInter == 0 && !d.unsupportedReconstruct {
					dst, stride, ok := d.reconstructVP9IntraPredictTx(hdr, pd, plane,
						mode, txSize, tile, miRow, miCol, bsize, rr, cc)
					if !ok {
						d.markVP9Unsupported()
					} else if eob > 0 && dst != nil {
						txType := common.DctDct
						if planeType == 0 && !hdr.Quant.Lossless {
							txType = common.IntraModeToTxType[mode]
						}
						vp9dec.InverseTransformBlock(coeffs, dst, stride, txSize,
							txType, eob, hdr.Quant.Lossless)
					}
				} else if isInter != 0 && !d.unsupportedReconstruct {
					dst, stride, ok := d.vp9InterTxDst(hdr, pd, plane, txSize,
						miRow, miCol, rr, cc)
					if !ok {
						d.markVP9Unsupported()
					} else if eob > 0 && dst != nil {
						vp9dec.InverseTransformBlock(coeffs, dst, stride, txSize,
							common.DctDct, eob, hdr.Quant.Lossless)
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
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	if isInter != 0 && mi.SbType >= common.Block8x8 && eobTotal == 0 {
		mi.Skip = 1
	}
	return true
}

func (d *VP9Decoder) vp9InterTxDst(
	hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane,
	plane int,
	txSize common.TxSize,
	miRow, miCol int,
	blockRow4x4, blockCol4x4 int,
) (dst []byte, stride int, ok bool) {
	planeData, stride := d.vp9OutputPlane(plane)
	if stride <= 0 || len(planeData) == 0 {
		return nil, 0, false
	}
	rows := len(planeData) / stride
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 := baseX + blockCol4x4*4
	y0 := baseY + blockRow4x4*4

	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, false
	}
	return planeData[y0*stride+x0:], stride, true
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
			d.markVP9Unsupported()
			return
		}

		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		miRows := int((hdr.Height + 7) >> 3)
		miCols := int((hdr.Width + 7) >> 3)
		max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((num4x4W - max4x4W) >> txSize) * blockStep
		blockIdx := 0
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				if _, _, ok := d.reconstructVP9IntraPredictTx(hdr, pd, plane, mode, txSize,
					tile, miRow, miCol, bsize, rr, cc); !ok {
					d.markVP9Unsupported()
					return
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
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
	bsize common.BlockSize,
	blockRow4x4, blockCol4x4 int,
) (dst []byte, stride int, ok bool) {
	planeData, stride := d.vp9OutputPlane(plane)
	if stride <= 0 || len(planeData) == 0 || int(mode) >= common.IntraModes {
		return nil, 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return nil, 0, false
	}
	rows := len(planeData) / stride
	alignedWidth := buffers.Align(int(hdr.Width), 8)
	alignedHeight := buffers.Align(int(hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 := baseX + blockCol4x4*4
	y0 := baseY + blockRow4x4*4

	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, false
	}

	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	bounds := vp9dec.BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	left := d.intraScratch.Left[:bs]
	if leftAvailable {
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			left[i] = planeData[sy*stride+x0-1]
		}
	}

	edges := vp9dec.IntraEdgeRefs{
		AboveLeft: 127,
		Left:      left,
	}
	upAvailable := blockRow4x4 != 0 || miRow > 0
	if upAvailable {
		edges.Above = planeData[(y0-1)*stride+x0:]
		if leftAvailable {
			edges.AboveLeft = planeData[(y0-1)*stride+x0-1]
		}
	}
	planeBlock4x4W := vp9IntraPredictWidth4x4(bsize, planeBsize, pd)
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W
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
		MbToRightEdge:  bounds.MbToRightEdge,
		MbToBottomEdge: bounds.MbToBottomEdge,
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
	miColsAligned := common.AlignToSB(miCols)
	d.aboveSegCtx = buffers.EnsureLen(d.aboveSegCtx, miColsAligned)
	d.leftSegCtx = buffers.EnsureLen(d.leftSegCtx, common.MiBlockSize)

	miGridLen := miRows * miCols
	resetLastSegMap := d.segMapMiRows != miRows || d.segMapMiCols != miCols
	d.miGrid = buffers.EnsureLen(d.miGrid, miGridLen)
	d.segMap = buffers.EnsureLen(d.segMap, miGridLen)
	d.lastSegMap = buffers.EnsureLen(d.lastSegMap, miGridLen)
	if resetLastSegMap {
		for i := range d.lastSegMap {
			d.lastSegMap[i] = 0
		}
	}
	d.segMapMiRows = miRows
	d.segMapMiCols = miCols

	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		aboveLen := vp9dec.PlaneEntropyLen(miColsAligned, pd.SubsamplingX)
		leftLen := vp9dec.PlaneEntropyLen(common.MiBlockSize, pd.SubsamplingY)
		pd.AboveContext = buffers.EnsureLen(pd.AboveContext, aboveLen)
		pd.LeftContext = buffers.EnsureLen(pd.LeftContext, leftLen)
	}
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

func prepareVP9DecoderTileDescs(dst []vp9DecoderTileDesc, tileData []byte,
	hdr vp9dec.UncompressedHeader, miRows, miCols int,
) ([]vp9DecoderTileDesc, error) {
	tileRows := 1 << uint(hdr.Tile.Log2TileRows)
	tileCols := 1 << uint(hdr.Tile.Log2TileCols)
	nTiles := tileRows * tileCols
	dst = buffers.EnsureLen(dst, nTiles)

	offset := 0
	for tileRow := range tileRows {
		for tileCol := range tileCols {
			idx := tileRow*tileCols + tileCol
			isLast := idx == nTiles-1
			tileSize := len(tileData) - offset
			if !isLast {
				if len(tileData)-offset < 4 {
					return nil, ErrInvalidVP9Data
				}
				size := binary.BigEndian.Uint32(tileData[offset : offset+4])
				offset += 4
				if size > uint32(len(tileData)-offset) {
					return nil, ErrInvalidVP9Data
				}
				tileSize = int(size)
			}
			if tileSize < 0 || offset+tileSize > len(tileData) {
				return nil, ErrInvalidVP9Data
			}
			dst[idx] = vp9DecoderTileDesc{
				data: tileData[offset : offset+tileSize],
				tile: vp9dec.TileBounds{
					MiRowStart: vp9dec.TileOffset(tileRow, miRows,
						hdr.Tile.Log2TileRows),
					MiRowEnd: vp9dec.TileOffset(tileRow+1, miRows,
						hdr.Tile.Log2TileRows),
					MiColStart: vp9dec.TileOffset(tileCol, miCols,
						hdr.Tile.Log2TileCols),
					MiColEnd: vp9dec.TileOffset(tileCol+1, miCols,
						hdr.Tile.Log2TileCols),
				},
			}
			offset += tileSize
		}
	}
	if offset != len(tileData) {
		return nil, ErrInvalidVP9Data
	}
	return dst, nil
}

func (d *VP9Decoder) vp9DecodeTileCol(tileCol, tileCols int) int {
	if d != nil && d.opts.InvertTileDecodeOrder {
		return tileCols - tileCol - 1
	}
	return tileCol
}
