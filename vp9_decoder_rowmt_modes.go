package govpx

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

type vp9DecoderRowMTPartitionCursor struct {
	parts []common.PartitionType
	pos   int
}

type vp9DecoderRowMTResidueCursor struct {
	eobPos   [vp9dec.MaxMbPlane]int
	coeffPos [vp9dec.MaxMbPlane]int
}

func (c *vp9DecoderRowMTPartitionCursor) write(partition common.PartitionType) bool {
	if c == nil || c.pos < 0 || c.pos >= len(c.parts) {
		return false
	}
	c.parts[c.pos] = partition
	c.pos++
	return true
}

func (c *vp9DecoderRowMTPartitionCursor) read() (common.PartitionType, bool) {
	if c == nil || c.pos < 0 || c.pos >= len(c.parts) {
		return common.PartitionNone, false
	}
	partition := c.parts[c.pos]
	c.pos++
	return partition, true
}

func (s *vp9DecoderRowMTFrameStorage) partitionCursor(sb int) vp9DecoderRowMTPartitionCursor {
	if s == nil || sb < 0 || sb >= s.numSBs {
		return vp9DecoderRowMTPartitionCursor{}
	}
	return vp9DecoderRowMTPartitionCursor{parts: s.partitionsForSB(sb)}
}

func (s *vp9DecoderRowMTFrameStorage) setUVMode(miCols, miRow, miCol int,
	mode common.PredictionMode,
) bool {
	idx := miRow*miCols + miCol
	if s == nil || miCols <= 0 || idx < 0 || idx >= len(s.uvMode) {
		return false
	}
	s.uvMode[idx] = mode
	return true
}

func (s *vp9DecoderRowMTFrameStorage) getUVMode(miCols, miRow, miCol int) (
	common.PredictionMode, bool,
) {
	idx := miRow*miCols + miCol
	if s == nil || miCols <= 0 || idx < 0 || idx >= len(s.uvMode) {
		return common.DcPred, false
	}
	return s.uvMode[idx], true
}

func (s *vp9DecoderRowMTFrameStorage) setResidueParsed(miCols, miRow, miCol int,
	parsed bool,
) bool {
	idx := miRow*miCols + miCol
	if s == nil || miCols <= 0 || idx < 0 || idx >= len(s.residueParsed) {
		return false
	}
	s.residueParsed[idx] = parsed
	return true
}

func (s *vp9DecoderRowMTFrameStorage) residueParsedAt(miCols, miRow, miCol int) (
	bool, bool,
) {
	idx := miRow*miCols + miCol
	if s == nil || miCols <= 0 || idx < 0 || idx >= len(s.residueParsed) {
		return false, false
	}
	return s.residueParsed[idx], true
}

func (d *VP9Decoder) vp9DecoderRowMTOneTileStorage() *vp9DecoderRowMTFrameStorage {
	if d == nil || d.vp9TilePool == nil || !d.opts.DecoderRowMT ||
		len(d.vp9TilePool.rowMTSyncs) != 1 {
		return nil
	}
	return &d.vp9TilePool.rowMTFrame
}

func vp9DecoderRowMTSBIndex(miCols, miRow, miCol int) int {
	sbCols := common.AlignToSB(miCols) >> common.MiBlockSizeLog2
	return (miRow>>common.MiBlockSizeLog2)*sbCols +
		(miCol >> common.MiBlockSizeLog2)
}

func vp9DecoderRowMTDQCoeffScratch(coeffs []int16, off, maxEob int) (
	*[1024]int16, bool,
) {
	if off < 0 || maxEob <= 0 || off+maxEob > len(coeffs) {
		return nil, false
	}
	// libvpx points xd.plane[].dqcoeff at the transform's first coefficient
	// inside the per-SB slab. DecodeCoefsState only touches maxEob entries.
	return (*[1024]int16)(unsafe.Pointer(&coeffs[off])), true
}

func (d *VP9Decoder) parseVP9IntraModeTileRowMTSplit(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	maps *vp9dec.IntraSegmentMaps, tile vp9dec.TileBounds,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) error {
	storage := d.vp9DecoderRowMTOneTileStorage()
	rowMT := d.rowMTSync
	if storage == nil || rowMT == nil {
		return ErrInvalidVP9Data
	}
	tileSbCols := (tile.MiColEnd - tile.MiColStart + common.MiBlockSize - 1) >>
		common.MiBlockSizeLog2
	globalSbCols := common.AlignToSB(miCols) >> common.MiBlockSizeLog2
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range d.leftSegCtx {
			d.leftSegCtx[i] = 0
		}
		d.resetVP9LeftEntropyContexts()
		sbRow := (miRow - tile.MiRowStart) >> common.MiBlockSizeLog2
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			sbCol := (miCol - tile.MiColStart) >> common.MiBlockSizeLog2
			rowMT.read(sbRow, sbCol)
			sb := vp9DecoderRowMTSBIndex(miCols, miRow, miCol)
			cursor := storage.partitionCursor(sb)
			residue := vp9DecoderRowMTResidueCursor{}
			if !d.parseVP9IntraModeSbRowMT(r, hdr, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, common.Block64x64, comp.TxMode,
				partitionProbs, &cursor, &residue) {
				return ErrInvalidVP9Data
			}
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			sbCol := (miCol - tile.MiColStart) >> common.MiBlockSizeLog2
			sb := vp9DecoderRowMTSBIndex(miCols, miRow, miCol)
			if sbRow > 0 && !storage.reconMapRead(sb-globalSbCols, sbRow-1) {
				return ErrInvalidVP9Data
			}
			cursor := storage.partitionCursor(sb)
			residue := vp9DecoderRowMTResidueCursor{}
			if !d.reconstructVP9IntraModeSbRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow, miCol, common.Block64x64, &cursor,
				&residue) {
				return ErrInvalidVP9Data
			}
			if !storage.reconMapWrite(sb, sbRow) {
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

func (d *VP9Decoder) parseVP9InterModeTileRowMTSplit(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	maps *vp9dec.InterSegmentMaps, tile vp9dec.TileBounds,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) error {
	storage := d.vp9DecoderRowMTOneTileStorage()
	rowMT := d.rowMTSync
	if storage == nil || rowMT == nil {
		return ErrInvalidVP9Data
	}
	tileSbCols := (tile.MiColEnd - tile.MiColStart + common.MiBlockSize - 1) >>
		common.MiBlockSizeLog2
	globalSbCols := common.AlignToSB(miCols) >> common.MiBlockSizeLog2
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range d.leftSegCtx {
			d.leftSegCtx[i] = 0
		}
		d.resetVP9LeftEntropyContexts()
		sbRow := (miRow - tile.MiRowStart) >> common.MiBlockSizeLog2
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			sbCol := (miCol - tile.MiColStart) >> common.MiBlockSizeLog2
			rowMT.read(sbRow, sbCol)
			sb := vp9DecoderRowMTSBIndex(miCols, miRow, miCol)
			cursor := storage.partitionCursor(sb)
			residue := vp9DecoderRowMTResidueCursor{}
			if !d.parseVP9InterModeSbRowMT(r, hdr, comp, maps, storage, sb,
				tile, miRows, miCols, miRow, miCol, common.Block64x64,
				partitionProbs, &cursor, &residue) {
				return ErrInvalidVP9Data
			}
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			sbCol := (miCol - tile.MiColStart) >> common.MiBlockSizeLog2
			sb := vp9DecoderRowMTSBIndex(miCols, miRow, miCol)
			if sbRow > 0 && !storage.reconMapRead(sb-globalSbCols, sbRow-1) {
				return ErrInvalidVP9Data
			}
			cursor := storage.partitionCursor(sb)
			residue := vp9DecoderRowMTResidueCursor{}
			if !d.reconstructVP9InterModeSbRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow, miCol, common.Block64x64, &cursor,
				&residue) {
				return ErrInvalidVP9Data
			}
			if !storage.reconMapWrite(sb, sbRow) {
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

func (d *VP9Decoder) parseVP9IntraModeSbRowMT(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, maps *vp9dec.IntraSegmentMaps,
	storage *vp9DecoderRowMTFrameStorage, sb int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txMode common.TxMode,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	cursor *vp9DecoderRowMTPartitionCursor,
	residue *vp9DecoderRowMTResidueCursor,
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
	if !cursor.write(partition) {
		return false
	}
	if !hdr.FrameParallelDecoding {
		d.counts.Partition[ctx][partition]++
	}
	subsize := common.SubsizeLookup[partition][bsize]
	if subsize >= common.BlockSizes {
		return false
	}
	if subsize < common.Block8x8 {
		if !d.readVP9IntraModeBlockRowMT(r, hdr, maps, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, txMode, residue) {
			return false
		}
	} else {
		switch partition {
		case common.PartitionNone:
			if !d.readVP9IntraModeBlockRowMT(r, hdr, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, subsize, txMode, residue) {
				return false
			}
		case common.PartitionHorz:
			if !d.readVP9IntraModeBlockRowMT(r, hdr, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, subsize, txMode, residue) {
				return false
			}
			if miRow+bs < miRows {
				if !d.readVP9IntraModeBlockRowMT(r, hdr, maps, storage, sb, tile,
					miRows, miCols, miRow+bs, miCol, subsize, txMode,
					residue) {
					return false
				}
			}
		case common.PartitionVert:
			if !d.readVP9IntraModeBlockRowMT(r, hdr, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, subsize, txMode, residue) {
				return false
			}
			if miCol+bs < miCols {
				if !d.readVP9IntraModeBlockRowMT(r, hdr, maps, storage, sb, tile,
					miRows, miCols, miRow, miCol+bs, subsize, txMode,
					residue) {
					return false
				}
			}
		case common.PartitionSplit:
			if !d.parseVP9IntraModeSbRowMT(r, hdr, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, subsize, txMode,
				partitionProbs, cursor, residue) {
				return false
			}
			if !d.parseVP9IntraModeSbRowMT(r, hdr, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol+bs, subsize, txMode,
				partitionProbs, cursor, residue) {
				return false
			}
			if !d.parseVP9IntraModeSbRowMT(r, hdr, maps, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol, subsize, txMode,
				partitionProbs, cursor, residue) {
				return false
			}
			if !d.parseVP9IntraModeSbRowMT(r, hdr, maps, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol+bs, subsize, txMode,
				partitionProbs, cursor, residue) {
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

func (d *VP9Decoder) parseVP9InterModeSbRowMT(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	maps *vp9dec.InterSegmentMaps, storage *vp9DecoderRowMTFrameStorage,
	sb int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	cursor *vp9DecoderRowMTPartitionCursor,
	residue *vp9DecoderRowMTResidueCursor,
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
	if !cursor.write(partition) {
		return false
	}
	if !hdr.FrameParallelDecoding {
		d.counts.Partition[ctx][partition]++
	}
	subsize := common.SubsizeLookup[partition][bsize]
	if subsize >= common.BlockSizes {
		return false
	}
	if subsize < common.Block8x8 {
		if !d.readVP9InterModeBlockRowMT(r, hdr, comp, maps, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue) {
			return false
		}
	} else {
		switch partition {
		case common.PartitionNone:
			if !d.readVP9InterModeBlockRowMT(r, hdr, comp, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, subsize, residue) {
				return false
			}
		case common.PartitionHorz:
			if !d.readVP9InterModeBlockRowMT(r, hdr, comp, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, subsize, residue) {
				return false
			}
			if miRow+bs < miRows {
				if !d.readVP9InterModeBlockRowMT(r, hdr, comp, maps, storage, sb,
					tile, miRows, miCols, miRow+bs, miCol, subsize, residue) {
					return false
				}
			}
		case common.PartitionVert:
			if !d.readVP9InterModeBlockRowMT(r, hdr, comp, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, subsize, residue) {
				return false
			}
			if miCol+bs < miCols {
				if !d.readVP9InterModeBlockRowMT(r, hdr, comp, maps, storage, sb,
					tile, miRows, miCols, miRow, miCol+bs, subsize, residue) {
					return false
				}
			}
		case common.PartitionSplit:
			if !d.parseVP9InterModeSbRowMT(r, hdr, comp, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol, subsize, partitionProbs, cursor,
				residue) {
				return false
			}
			if !d.parseVP9InterModeSbRowMT(r, hdr, comp, maps, storage, sb, tile,
				miRows, miCols, miRow, miCol+bs, subsize, partitionProbs,
				cursor, residue) {
				return false
			}
			if !d.parseVP9InterModeSbRowMT(r, hdr, comp, maps, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol, subsize, partitionProbs,
				cursor, residue) {
				return false
			}
			if !d.parseVP9InterModeSbRowMT(r, hdr, comp, maps, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol+bs, subsize, partitionProbs,
				cursor, residue) {
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

func (d *VP9Decoder) reconstructVP9IntraModeSbRowMT(
	hdr *vp9dec.UncompressedHeader, storage *vp9DecoderRowMTFrameStorage,
	sb int, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, cursor *vp9DecoderRowMTPartitionCursor,
	residue *vp9DecoderRowMTResidueCursor,
) bool {
	if miRow >= miRows || miCol >= miCols {
		return true
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	partition, ok := cursor.read()
	if !ok {
		return false
	}
	subsize := common.SubsizeLookup[partition][bsize]
	if subsize >= common.BlockSizes {
		return false
	}
	if subsize < common.Block8x8 {
		return d.reconstructVP9IntraModeBlockRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue)
	}
	switch partition {
	case common.PartitionNone:
		return d.reconstructVP9IntraModeBlockRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue)
	case common.PartitionHorz:
		if !d.reconstructVP9IntraModeBlockRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue) {
			return false
		}
		if miRow+bs < miRows {
			return d.reconstructVP9IntraModeBlockRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol, subsize, residue)
		}
		return true
	case common.PartitionVert:
		if !d.reconstructVP9IntraModeBlockRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue) {
			return false
		}
		if miCol+bs < miCols {
			return d.reconstructVP9IntraModeBlockRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow, miCol+bs, subsize, residue)
		}
		return true
	case common.PartitionSplit:
		return d.reconstructVP9IntraModeSbRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, cursor, residue) &&
			d.reconstructVP9IntraModeSbRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow, miCol+bs, subsize, cursor, residue) &&
			d.reconstructVP9IntraModeSbRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol, subsize, cursor, residue) &&
			d.reconstructVP9IntraModeSbRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol+bs, subsize, cursor, residue)
	default:
		return false
	}
}

func (d *VP9Decoder) reconstructVP9InterModeSbRowMT(
	hdr *vp9dec.UncompressedHeader, storage *vp9DecoderRowMTFrameStorage,
	sb int, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, cursor *vp9DecoderRowMTPartitionCursor,
	residue *vp9DecoderRowMTResidueCursor,
) bool {
	if miRow >= miRows || miCol >= miCols {
		return true
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	partition, ok := cursor.read()
	if !ok {
		return false
	}
	subsize := common.SubsizeLookup[partition][bsize]
	if subsize >= common.BlockSizes {
		return false
	}
	if subsize < common.Block8x8 {
		return d.reconstructVP9InterModeBlockRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue)
	}
	switch partition {
	case common.PartitionNone:
		return d.reconstructVP9InterModeBlockRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue)
	case common.PartitionHorz:
		if !d.reconstructVP9InterModeBlockRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue) {
			return false
		}
		if miRow+bs < miRows {
			return d.reconstructVP9InterModeBlockRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol, subsize, residue)
		}
		return true
	case common.PartitionVert:
		if !d.reconstructVP9InterModeBlockRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, residue) {
			return false
		}
		if miCol+bs < miCols {
			return d.reconstructVP9InterModeBlockRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow, miCol+bs, subsize, residue)
		}
		return true
	case common.PartitionSplit:
		return d.reconstructVP9InterModeSbRowMT(hdr, storage, sb, tile,
			miRows, miCols, miRow, miCol, subsize, cursor, residue) &&
			d.reconstructVP9InterModeSbRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow, miCol+bs, subsize, cursor, residue) &&
			d.reconstructVP9InterModeSbRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol, subsize, cursor, residue) &&
			d.reconstructVP9InterModeSbRowMT(hdr, storage, sb, tile,
				miRows, miCols, miRow+bs, miCol+bs, subsize, cursor, residue)
	default:
		return false
	}
}

func (d *VP9Decoder) readVP9IntraModeBlockRowMT(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, maps *vp9dec.IntraSegmentMaps,
	storage *vp9DecoderRowMTFrameStorage, sb int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txMode common.TxMode,
	residue *vp9DecoderRowMTResidueCursor,
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
	if !storage.setUVMode(miCols, miRow, miCol, out.UvMode) {
		return false
	}
	reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
	if mi.Skip != 0 {
		if !storage.setResidueParsed(miCols, miRow, miCol, false) {
			return false
		}
		aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
		vp9dec.ResetSkipContext(d.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
		d.storeVP9CurrentFrameMvs(miRows, miCols, miRow, miCol, xMis, yMis, mi)
		d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
		return true
	}
	if !storage.setResidueParsed(miCols, miRow, miCol, true) {
		return false
	}
	if _, ok := d.parseVP9ResidueBlockRowMT(r, hdr, mi, out.UvMode, tile,
		miRow, miCol, reconBsize, int(mi.SegmentID), 0, storage, sb,
		residue); !ok {
		return false
	}
	d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
	return true
}

func (d *VP9Decoder) readVP9InterModeBlockRowMT(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	maps *vp9dec.InterSegmentMaps, storage *vp9DecoderRowMTFrameStorage,
	sb int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	residue *vp9DecoderRowMTResidueCursor,
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
	segID := 0
	d.segIDPredictedScratch = 0
	if hdr.Seg.Enabled {
		maps.SegIDPredictedOut = &d.segIDPredictedScratch
		segID = vp9dec.ReadInterSegmentId(r, &hdr.Seg, maps,
			miRow*miCols+miCol, xMis, yMis, above, left)
	}
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
	} else if !d.readVP9InterBlockModeInfo(r, hdr, comp, mi, segID, above,
		left, tile, miRows, miCols, miRow, miCol) {
		return false
	}
	if !storage.setUVMode(miCols, miRow, miCol, uvMode) {
		return false
	}
	reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
	if mi.Skip != 0 {
		if !storage.setResidueParsed(miCols, miRow, miCol, false) {
			return false
		}
		aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
		vp9dec.ResetSkipContext(d.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
		d.storeVP9CurrentFrameMvs(miRows, miCols, miRow, miCol, xMis, yMis, mi)
		d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
		return true
	}
	if isInter != 0 && !vp9dec.CanReconstructInterBlock(mi) {
		d.unsupportedReconstruct = true
	}
	if !storage.setResidueParsed(miCols, miRow, miCol, true) {
		return false
	}
	eobTotal, ok := d.parseVP9ResidueBlockRowMT(r, hdr, mi, uvMode, tile,
		miRow, miCol, reconBsize, segID, isInter, storage, sb, residue)
	if !ok {
		return false
	}
	if isInter != 0 && mi.SbType >= common.Block8x8 && eobTotal == 0 {
		mi.Skip = 1
	}
	d.storeVP9CurrentFrameMvs(miRows, miCols, miRow, miCol, xMis, yMis, mi)
	d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
	return true
}

func (d *VP9Decoder) reconstructVP9IntraModeBlockRowMT(
	hdr *vp9dec.UncompressedHeader, storage *vp9DecoderRowMTFrameStorage,
	sb int, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	residue *vp9DecoderRowMTResidueCursor,
) bool {
	mi := d.vp9DecoderMiAt(miRows, miCols, miRow, miCol)
	if mi == nil {
		return false
	}
	uvMode, ok := storage.getUVMode(miCols, miRow, miCol)
	if !ok {
		return false
	}
	reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
	parsed, ok := storage.residueParsedAt(miCols, miRow, miCol)
	if !ok {
		return false
	}
	if !parsed {
		if !d.unsupportedReconstruct {
			d.reconstructVP9IntraPredictBlock(hdr, mi, uvMode, tile,
				miRow, miCol, reconBsize)
		}
		return true
	}
	return d.reconstructVP9ResidueBlockRowMT(hdr, mi, uvMode, tile, miRow,
		miCol, reconBsize, 0, storage, sb, residue)
}

func (d *VP9Decoder) reconstructVP9InterModeBlockRowMT(
	hdr *vp9dec.UncompressedHeader, storage *vp9DecoderRowMTFrameStorage,
	sb int, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	residue *vp9DecoderRowMTResidueCursor,
) bool {
	mi := d.vp9DecoderMiAt(miRows, miCols, miRow, miCol)
	if mi == nil {
		return false
	}
	uvMode, ok := storage.getUVMode(miCols, miRow, miCol)
	if !ok {
		return false
	}
	reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
	isInter := 0
	if mi.RefFrame[0] > vp9dec.IntraFrame {
		isInter = 1
	}
	parsed, ok := storage.residueParsedAt(miCols, miRow, miCol)
	if !ok {
		return false
	}
	if !parsed {
		if !d.unsupportedReconstruct {
			if isInter != 0 {
				if !d.reconstructVP9InterPredictBlock(hdr, mi, miRow, miCol,
					reconBsize) {
					return false
				}
			} else {
				d.reconstructVP9IntraPredictBlock(hdr, mi, uvMode, tile,
					miRow, miCol, reconBsize)
			}
		}
		return true
	}
	return d.reconstructVP9ResidueBlockRowMT(hdr, mi, uvMode, tile, miRow,
		miCol, reconBsize, isInter, storage, sb, residue)
}

func (d *VP9Decoder) parseVP9ResidueBlockRowMT(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, tile vp9dec.TileBounds,
	miRow, miCol int, bsize common.BlockSize, segID int, isInter int,
	storage *vp9DecoderRowMTFrameStorage, sb int,
	residue *vp9DecoderRowMTResidueCursor,
) (int, bool) {
	aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	rs := r.LocalState()
	var coefCounts *vp9dec.CoefCounts
	if !hdr.FrameParallelDecoding {
		coefCounts = &d.counts.Coef
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
			rs.Commit()
			return 0, false
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
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		sbCoeffs := storage.dqcoeffForSB(plane, sb)
		sbEobs := storage.eobForSB(plane, sb)
		var planeScan *common.ScanOrder
		if isInter != 0 || planeType != 0 || hdr.Quant.Lossless {
			planeScan = common.GetScanPtr(txSize, planeType, isInter,
				hdr.Quant.Lossless, uvMode)
		}
		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				aboveCtx := pd.AboveContext[aboveBase+cc : aboveBase+cc+step]
				leftCtx := pd.LeftContext[leftBase+rr : leftBase+rr+step]
				initCtx := vp9CoeffInitContext(aboveCtx, leftCtx, txSize)
				so := planeScan
				if so == nil {
					so = common.GetScanPtr(txSize, planeType, isInter,
						hdr.Quant.Lossless, mode)
				}
				eobPos := residue.eobPos[plane]
				coeffPos := residue.coeffPos[plane]
				coeffs, ok := vp9DecoderRowMTDQCoeffScratch(sbCoeffs, coeffPos,
					maxEob)
				if !ok || eobPos < 0 || eobPos >= len(sbEobs) {
					rs.Commit()
					return 0, false
				}
				eob := vp9dec.DecodeCoefsState(&rs, txSize, planeType, isInter,
					dequant, initCtx, so, &d.fc.CoefProbs, coefCounts,
					coeffs, &d.coefTokenCache)
				sbEobs[eobPos] = eob
				residue.eobPos[plane]++
				residue.coeffPos[plane] += maxEob
				eobTotal += eob
				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				vp9SetHasResidueContexts(aboveCtx, leftCtx, txSize, hasResidue)
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	rs.Commit()
	return eobTotal, true
}

func (d *VP9Decoder) reconstructVP9ResidueBlockRowMT(
	hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, tile vp9dec.TileBounds,
	miRow, miCol int, bsize common.BlockSize, isInter int,
	storage *vp9DecoderRowMTFrameStorage, sb int,
	residue *vp9DecoderRowMTResidueCursor,
) bool {
	if isInter != 0 && !d.unsupportedReconstruct {
		if !d.reconstructVP9InterPredictBlock(hdr, mi, miRow, miCol, bsize) {
			return false
		}
	}
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		planeType := 0
		if plane > 0 {
			planeType = 1
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
		num4x4W, num4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols, miRow,
			miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((full4x4W - num4x4W) >> txSize) * blockStep
		blockIdx := 0
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		sbCoeffs := storage.dqcoeffForSB(plane, sb)
		sbEobs := storage.eobForSB(plane, sb)
		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				eobPos := residue.eobPos[plane]
				coeffPos := residue.coeffPos[plane]
				if eobPos < 0 || eobPos >= len(sbEobs) ||
					coeffPos < 0 || coeffPos+maxEob > len(sbCoeffs) {
					return false
				}
				eob := sbEobs[eobPos]
				coeffs := sbCoeffs[coeffPos : coeffPos+maxEob]
				residue.eobPos[plane]++
				residue.coeffPos[plane] += maxEob
				if isInter == 0 && !d.unsupportedReconstruct {
					mode := uvMode
					if plane == 0 {
						mode = vp9dec.GetYMode(mi, blockIdx)
					}
					txType := common.DctDct
					if planeType == 0 && !hdr.Quant.Lossless {
						txType = common.IntraModeToTxType[mode]
					}
					dst, stride, ok := d.reconstructVP9IntraPredictTx(hdr, pd,
						plane, mode, txSize, tile, miRow, miCol, bsize, rr, cc)
					if !ok {
						d.markVP9Unsupported()
					} else if eob > 0 && dst != nil {
						vp9dec.InverseTransformBlock(coeffs, dst, stride, txSize,
							txType, eob, hdr.Quant.Lossless)
					}
				} else if isInter != 0 && !d.unsupportedReconstruct {
					dst, stride, ok := d.vp9InterTxDst(hdr, pd, plane, txSize,
						miRow, miCol, rr, cc)
					if !ok {
						d.markVP9Unsupported()
					} else if eob > 0 {
						vp9dec.InverseTransformBlock(coeffs, dst, stride, txSize,
							common.DctDct, eob, hdr.Quant.Lossless)
					}
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	return true
}

func vp9CoeffInitContext(aboveCtx, leftCtx []uint8, txSize common.TxSize) int {
	initCtx := 0
	switch txSize {
	case common.Tx4x4:
		if aboveCtx[0] != 0 {
			initCtx++
		}
		if leftCtx[0] != 0 {
			initCtx++
		}
	case common.Tx8x8:
		if aboveCtx[0]|aboveCtx[1] != 0 {
			initCtx++
		}
		if leftCtx[0]|leftCtx[1] != 0 {
			initCtx++
		}
	case common.Tx16x16:
		if aboveCtx[0]|aboveCtx[1]|aboveCtx[2]|aboveCtx[3] != 0 {
			initCtx++
		}
		if leftCtx[0]|leftCtx[1]|leftCtx[2]|leftCtx[3] != 0 {
			initCtx++
		}
	default:
		if aboveCtx[0]|aboveCtx[1]|aboveCtx[2]|aboveCtx[3]|
			aboveCtx[4]|aboveCtx[5]|aboveCtx[6]|aboveCtx[7] != 0 {
			initCtx++
		}
		if leftCtx[0]|leftCtx[1]|leftCtx[2]|leftCtx[3]|
			leftCtx[4]|leftCtx[5]|leftCtx[6]|leftCtx[7] != 0 {
			initCtx++
		}
	}
	return initCtx
}

func vp9SetHasResidueContexts(aboveCtx, leftCtx []uint8, txSize common.TxSize,
	hasResidue uint8,
) {
	switch txSize {
	case common.Tx4x4:
		aboveCtx[0] = hasResidue
		leftCtx[0] = hasResidue
	case common.Tx8x8:
		aboveCtx[0] = hasResidue
		aboveCtx[1] = hasResidue
		leftCtx[0] = hasResidue
		leftCtx[1] = hasResidue
	case common.Tx16x16:
		aboveCtx[0] = hasResidue
		aboveCtx[1] = hasResidue
		aboveCtx[2] = hasResidue
		aboveCtx[3] = hasResidue
		leftCtx[0] = hasResidue
		leftCtx[1] = hasResidue
		leftCtx[2] = hasResidue
		leftCtx[3] = hasResidue
	default:
		for i := range 8 {
			aboveCtx[i] = hasResidue
			leftCtx[i] = hasResidue
		}
	}
}
