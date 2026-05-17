package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
)

// VP9 loop-filter frame traversal. Ported from libvpx v1.16.0
// vp9/common/vp9_loopfilter.c: vp9_setup_mask, vp9_adjust_mask,
// filter_selectively_{vert,horiz}, vp9_filter_block_plane_ss00/ss11, and
// loop_filter_rows for the 8-bit 4:2:0 decoder path.

type vp9LoopFilterMask struct {
	leftY    [common.TxSizes]uint64
	aboveY   [common.TxSizes]uint64
	int4x4Y  uint64
	leftUV   [common.TxSizes]uint16
	aboveUV  [common.TxSizes]uint16
	int4x4UV uint16
	lflY     [64]uint8
}

var vp9LFLeft64x64TxformMask = [common.TxSizes]uint64{
	0xffffffffffffffff,
	0xffffffffffffffff,
	0x5555555555555555,
	0x1111111111111111,
}

var vp9LFAbove64x64TxformMask = [common.TxSizes]uint64{
	0xffffffffffffffff,
	0xffffffffffffffff,
	0x00ff00ff00ff00ff,
	0x000000ff000000ff,
}

var vp9LFLeftPredictionMask = [common.BlockSizes]uint64{
	0x0000000000000001, 0x0000000000000001, 0x0000000000000001,
	0x0000000000000001, 0x0000000000000101, 0x0000000000000001,
	0x0000000000000101, 0x0000000001010101, 0x0000000000000101,
	0x0000000001010101, 0x0101010101010101, 0x0000000001010101,
	0x0101010101010101,
}

var vp9LFAbovePredictionMask = [common.BlockSizes]uint64{
	0x0000000000000001, 0x0000000000000001, 0x0000000000000001,
	0x0000000000000001, 0x0000000000000001, 0x0000000000000003,
	0x0000000000000003, 0x0000000000000003, 0x000000000000000f,
	0x000000000000000f, 0x000000000000000f, 0x00000000000000ff,
	0x00000000000000ff,
}

var vp9LFSizeMask = [common.BlockSizes]uint64{
	0x0000000000000001, 0x0000000000000001, 0x0000000000000001,
	0x0000000000000001, 0x0000000000000101, 0x0000000000000003,
	0x0000000000000303, 0x0000000003030303, 0x0000000000000f0f,
	0x000000000f0f0f0f, 0x0f0f0f0f0f0f0f0f, 0x00000000ffffffff,
	0xffffffffffffffff,
}

var vp9LFLeft64x64TxformMaskUV = [common.TxSizes]uint16{
	0xffff, 0xffff, 0x5555, 0x1111,
}

var vp9LFAbove64x64TxformMaskUV = [common.TxSizes]uint16{
	0xffff, 0xffff, 0x0f0f, 0x000f,
}

var vp9LFLeftPredictionMaskUV = [common.BlockSizes]uint16{
	0x0001, 0x0001, 0x0001, 0x0001, 0x0001, 0x0001, 0x0001,
	0x0011, 0x0001, 0x0011, 0x1111, 0x0011, 0x1111,
}

var vp9LFAbovePredictionMaskUV = [common.BlockSizes]uint16{
	0x0001, 0x0001, 0x0001, 0x0001, 0x0001, 0x0001, 0x0001,
	0x0001, 0x0003, 0x0003, 0x0003, 0x000f, 0x000f,
}

var vp9LFSizeMaskUV = [common.BlockSizes]uint16{
	0x0001, 0x0001, 0x0001, 0x0001, 0x0001, 0x0001, 0x0001,
	0x0011, 0x0003, 0x0033, 0x3333, 0x00ff, 0xffff,
}

func (d *VP9Decoder) applyVP9LoopFilter(hdr *vp9dec.UncompressedHeader) bool {
	if hdr.Loopfilter.FilterLevel == 0 {
		return true
	}
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	// Gate the loop-filter worker pool on VP9D_SET_LOOP_FILTER_OPT. When
	// the option is off the deblock pass runs serially even on a threaded
	// decoder, matching libvpx's lpf_mt_opt = 0 path.
	if d.vp9LoopFilterPool != nil && d.opts.DecoderLoopFilterOpt {
		return d.applyVP9LoopFilterThreaded(miRows, miCols)
	}
	return d.applyVP9LoopFilterSerial(miRows, miCols)
}

func (d *VP9Decoder) applyVP9LoopFilterSerial(miRows, miCols int) bool {
	for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
		for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
			var lfm vp9LoopFilterMask
			if !d.vp9SetupLoopFilterMask(miRows, miCols, miRow, miCol, &lfm) {
				return false
			}
			vp9AdjustLoopFilterMask(miRows, miCols, miRow, miCol, &lfm)
			if !d.vp9FilterLoopBlock(miRows, miRow, miCol, &lfm) {
				return false
			}
		}
	}
	return true
}

func (d *VP9Decoder) applyVP9LoopFilterPlane(miRows, miCols int,
	plane vp9LoopFilterPlane,
) bool {
	return d.applyVP9LoopFilterPlaneRows(miRows, miCols, 0, miRows, plane)
}

// applyVP9LoopFilterPlaneRows runs the loop filter on a single plane
// restricted to mi rows in [startMiRow, endMiRow). Mirrors libvpx
// vp9_loop_filter_frame's row-range walk used by the partial-frame /
// LPF_PICK_FROM_SUBIMAGE path (vp9_loopfilter.c:1469-1483
// vp9_loop_filter_frame). When startMiRow == 0 and endMiRow == miRows
// the range covers the whole frame, matching the unrestricted
// applyVP9LoopFilterPlane path.
//
// libvpx: vp9_loopfilter.c:1469
//
//	void vp9_loop_filter_frame(YV12_BUFFER_CONFIG *frame, VP9_COMMON *cm,
//	                           MACROBLOCKD *xd, int frame_filter_level, int y_only,
//	                           int partial_frame) {
//	  int start_mi_row, end_mi_row, mi_rows_to_filter;
//	  if (!frame_filter_level) return;
//	  start_mi_row = 0;
//	  mi_rows_to_filter = cm->mi_rows;
//	  if (partial_frame && cm->mi_rows > 8) {
//	    start_mi_row = cm->mi_rows >> 1;
//	    start_mi_row &= 0xfffffff8;
//	    mi_rows_to_filter = VPXMAX(cm->mi_rows / 8, 8);
//	  }
//	  end_mi_row = start_mi_row + mi_rows_to_filter;
//	  loop_filter_rows(frame, cm, xd->plane, start_mi_row, end_mi_row, y_only);
//	}
func (d *VP9Decoder) applyVP9LoopFilterPlaneRows(miRows, miCols int,
	startMiRow, endMiRow int, plane vp9LoopFilterPlane,
) bool {
	if startMiRow < 0 {
		startMiRow = 0
	}
	if endMiRow > miRows {
		endMiRow = miRows
	}
	for miRow := startMiRow; miRow < endMiRow; miRow += common.MiBlockSize {
		for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
			var lfm vp9LoopFilterMask
			if !d.vp9SetupLoopFilterMask(miRows, miCols, miRow, miCol, &lfm) {
				return false
			}
			vp9AdjustLoopFilterMask(miRows, miCols, miRow, miCol, &lfm)
			if !d.vp9FilterLoopBlockPlane(miRows, miRow, miCol, plane, &lfm) {
				return false
			}
		}
	}
	return true
}

// vp9PickLpfPartialFrameRows returns the (startMiRow, endMiRow) range
// for LPF_PICK_FROM_SUBIMAGE partial-frame filtering. Verbatim port of
// libvpx vp9_loopfilter.c:1474-1481 (the partial_frame branch of
// vp9_loop_filter_frame) — start at mi_rows/2 aligned down to an
// 8-mi boundary, filter max(mi_rows/8, 8) rows. The guard
// `mi_rows > 8` keeps very small frames unrestricted (libvpx
// vp9_loopfilter.c:1476).
//
// libvpx: vp9_loopfilter.c:1476
//
//	if (partial_frame && cm->mi_rows > 8) {
//	  start_mi_row = cm->mi_rows >> 1;
//	  start_mi_row &= 0xfffffff8;
//	  mi_rows_to_filter = VPXMAX(cm->mi_rows / 8, 8);
//	}
//	end_mi_row = start_mi_row + mi_rows_to_filter;
func vp9PickLpfPartialFrameRows(miRows int) (startMiRow, endMiRow int) {
	if miRows <= 8 {
		return 0, miRows
	}
	startMiRow = (miRows >> 1) & ^7
	miRowsToFilter := max(miRows/8, 8)
	endMiRow = min(startMiRow+miRowsToFilter, miRows)
	return startMiRow, endMiRow
}

func (d *VP9Decoder) vp9SetupLoopFilterMask(miRows, miCols, miRow, miCol int,
	lfm *vp9LoopFilterMask,
) bool {
	top := d.vp9DecoderMiAt(miRows, miCols, miRow, miCol)
	if top == nil {
		return false
	}
	maxRows := common.MiBlockSize
	if miRow+common.MiBlockSize > miRows {
		maxRows = miRows - miRow
	}
	maxCols := common.MiBlockSize
	if miCol+common.MiBlockSize > miCols {
		maxCols = miCols - miCol
	}

	switch top.SbType {
	case common.Block64x64:
		return d.vp9BuildLoopFilterMasks(top, 0, 0, lfm)
	case common.Block64x32:
		if !d.vp9BuildLoopFilterMasks(top, 0, 0, lfm) {
			return false
		}
		if 4 >= maxRows {
			return true
		}
		return d.vp9BuildLoopFilterMasks(d.vp9DecoderMiAt(miRows, miCols, miRow+4, miCol),
			32, 8, lfm)
	case common.Block32x64:
		if !d.vp9BuildLoopFilterMasks(top, 0, 0, lfm) {
			return false
		}
		if 4 >= maxCols {
			return true
		}
		return d.vp9BuildLoopFilterMasks(d.vp9DecoderMiAt(miRows, miCols, miRow, miCol+4),
			4, 2, lfm)
	default:
		return d.vp9SetupLoopFilterMaskSub64(miRows, miCols, miRow, miCol,
			maxRows, maxCols, lfm)
	}
}

func (d *VP9Decoder) vp9SetupLoopFilterMaskSub64(miRows, miCols, miRow, miCol int,
	maxRows, maxCols int, lfm *vp9LoopFilterMask,
) bool {
	shift32Y := [4]int{0, 4, 32, 36}
	shift16Y := [4]int{0, 2, 16, 18}
	shift8Y := [4]int{0, 1, 8, 9}
	shift32UV := [4]int{0, 2, 8, 10}
	shift16UV := [4]int{0, 1, 4, 5}

	for idx32 := range 4 {
		row32 := miRow + ((idx32 >> 1) << 2)
		col32 := miCol + ((idx32 & 1) << 2)
		row32Off := (idx32 >> 1) << 2
		col32Off := (idx32 & 1) << 2
		if col32Off >= maxCols || row32Off >= maxRows {
			continue
		}
		mi32 := d.vp9DecoderMiAt(miRows, miCols, row32, col32)
		if mi32 == nil {
			return false
		}
		switch mi32.SbType {
		case common.Block32x32:
			if !d.vp9BuildLoopFilterMasks(mi32, shift32Y[idx32], shift32UV[idx32], lfm) {
				return false
			}
		case common.Block32x16:
			if !d.vp9BuildLoopFilterMasks(mi32, shift32Y[idx32], shift32UV[idx32], lfm) {
				return false
			}
			if row32Off+2 >= maxRows {
				continue
			}
			if !d.vp9BuildLoopFilterMasks(d.vp9DecoderMiAt(miRows, miCols, row32+2, col32),
				shift32Y[idx32]+16, shift32UV[idx32]+4, lfm) {
				return false
			}
		case common.Block16x32:
			if !d.vp9BuildLoopFilterMasks(mi32, shift32Y[idx32], shift32UV[idx32], lfm) {
				return false
			}
			if col32Off+2 >= maxCols {
				continue
			}
			if !d.vp9BuildLoopFilterMasks(d.vp9DecoderMiAt(miRows, miCols, row32, col32+2),
				shift32Y[idx32]+2, shift32UV[idx32]+1, lfm) {
				return false
			}
		default:
			if !d.vp9SetupLoopFilterMaskSub32(miRows, miCols, row32, col32,
				row32Off, col32Off, maxRows, maxCols,
				shift32Y[idx32], shift32UV[idx32], shift16Y, shift16UV, shift8Y, lfm) {
				return false
			}
		}
	}
	return true
}

func (d *VP9Decoder) vp9SetupLoopFilterMaskSub32(miRows, miCols, baseRow, baseCol int,
	row32Off, col32Off, maxRows, maxCols, shiftY32, shiftUV32 int,
	shift16Y, shift16UV, shift8Y [4]int,
	lfm *vp9LoopFilterMask,
) bool {
	for idx16 := range 4 {
		row16 := baseRow + ((idx16 >> 1) << 1)
		col16 := baseCol + ((idx16 & 1) << 1)
		row16Off := row32Off + ((idx16 >> 1) << 1)
		col16Off := col32Off + ((idx16 & 1) << 1)
		if col16Off >= maxCols || row16Off >= maxRows {
			continue
		}
		shiftY16 := shiftY32 + shift16Y[idx16]
		shiftUV16 := shiftUV32 + shift16UV[idx16]
		mi16 := d.vp9DecoderMiAt(miRows, miCols, row16, col16)
		if mi16 == nil {
			return false
		}
		switch mi16.SbType {
		case common.Block16x16:
			if !d.vp9BuildLoopFilterMasks(mi16, shiftY16, shiftUV16, lfm) {
				return false
			}
		case common.Block16x8:
			if !d.vp9BuildLoopFilterMasks(mi16, shiftY16, shiftUV16, lfm) {
				return false
			}
			if row16Off+1 >= maxRows {
				continue
			}
			if !d.vp9BuildLoopFilterYMask(d.vp9DecoderMiAt(miRows, miCols, row16+1, col16),
				shiftY16+8, lfm) {
				return false
			}
		case common.Block8x16:
			if !d.vp9BuildLoopFilterMasks(mi16, shiftY16, shiftUV16, lfm) {
				return false
			}
			if col16Off+1 >= maxCols {
				continue
			}
			if !d.vp9BuildLoopFilterYMask(d.vp9DecoderMiAt(miRows, miCols, row16, col16+1),
				shiftY16+1, lfm) {
				return false
			}
		default:
			if !d.vp9BuildLoopFilterMasks(mi16, shiftY16+shift8Y[0], shiftUV16, lfm) {
				return false
			}
			for idx8 := 1; idx8 < 4; idx8++ {
				row8 := row16 + (idx8 >> 1)
				col8 := col16 + (idx8 & 1)
				row8Off := row16Off + (idx8 >> 1)
				col8Off := col16Off + (idx8 & 1)
				if col8Off >= maxCols || row8Off >= maxRows {
					continue
				}
				if !d.vp9BuildLoopFilterYMask(d.vp9DecoderMiAt(miRows, miCols, row8, col8),
					shiftY16+shift8Y[idx8], lfm) {
					return false
				}
			}
		}
	}
	return true
}

func (d *VP9Decoder) vp9BuildLoopFilterMasks(mi *vp9dec.NeighborMi,
	shiftY, shiftUV int, lfm *vp9LoopFilterMask,
) bool {
	if mi == nil || mi.SbType >= common.BlockSizes || mi.TxSize >= common.TxSizes {
		return false
	}
	blockSize := mi.SbType
	txSizeY := mi.TxSize
	txSizeUV := common.UvTxsizeLookup[blockSize][txSizeY][1][1]
	if txSizeUV >= common.TxSizes {
		return false
	}
	filterLevel := d.vp9LoopFilterLevel(mi)
	if filterLevel == 0 {
		return true
	}
	w := int(common.Num8x8BlocksWideLookup[blockSize])
	h := int(common.Num8x8BlocksHighLookup[blockSize])
	index := shiftY
	for range h {
		for j := range w {
			lfm.lflY[index+j] = filterLevel
		}
		index += 8
	}

	lfm.aboveY[txSizeY] |= vp9LFAbovePredictionMask[blockSize] << shiftY
	lfm.aboveUV[txSizeUV] |= vp9LFAbovePredictionMaskUV[blockSize] << shiftUV
	lfm.leftY[txSizeY] |= vp9LFLeftPredictionMask[blockSize] << shiftY
	lfm.leftUV[txSizeUV] |= vp9LFLeftPredictionMaskUV[blockSize] << shiftUV

	if mi.Skip != 0 && mi.RefFrame[0] > vp9dec.IntraFrame {
		return true
	}

	lfm.aboveY[txSizeY] |= (vp9LFSizeMask[blockSize] & vp9LFAbove64x64TxformMask[txSizeY]) << shiftY
	lfm.aboveUV[txSizeUV] |= (vp9LFSizeMaskUV[blockSize] & vp9LFAbove64x64TxformMaskUV[txSizeUV]) << shiftUV
	lfm.leftY[txSizeY] |= (vp9LFSizeMask[blockSize] & vp9LFLeft64x64TxformMask[txSizeY]) << shiftY
	lfm.leftUV[txSizeUV] |= (vp9LFSizeMaskUV[blockSize] & vp9LFLeft64x64TxformMaskUV[txSizeUV]) << shiftUV
	if txSizeY == common.Tx4x4 {
		lfm.int4x4Y |= vp9LFSizeMask[blockSize] << shiftY
	}
	if txSizeUV == common.Tx4x4 {
		lfm.int4x4UV |= (vp9LFSizeMaskUV[blockSize] & 0xffff) << shiftUV
	}
	return true
}

func (d *VP9Decoder) vp9BuildLoopFilterYMask(mi *vp9dec.NeighborMi,
	shiftY int, lfm *vp9LoopFilterMask,
) bool {
	if mi == nil || mi.SbType >= common.BlockSizes || mi.TxSize >= common.TxSizes {
		return false
	}
	blockSize := mi.SbType
	txSizeY := mi.TxSize
	filterLevel := d.vp9LoopFilterLevel(mi)
	if filterLevel == 0 {
		return true
	}
	w := int(common.Num8x8BlocksWideLookup[blockSize])
	h := int(common.Num8x8BlocksHighLookup[blockSize])
	index := shiftY
	for range h {
		for j := range w {
			lfm.lflY[index+j] = filterLevel
		}
		index += 8
	}

	lfm.aboveY[txSizeY] |= vp9LFAbovePredictionMask[blockSize] << shiftY
	lfm.leftY[txSizeY] |= vp9LFLeftPredictionMask[blockSize] << shiftY
	if mi.Skip != 0 && mi.RefFrame[0] > vp9dec.IntraFrame {
		return true
	}
	lfm.aboveY[txSizeY] |= (vp9LFSizeMask[blockSize] & vp9LFAbove64x64TxformMask[txSizeY]) << shiftY
	lfm.leftY[txSizeY] |= (vp9LFSizeMask[blockSize] & vp9LFLeft64x64TxformMask[txSizeY]) << shiftY
	if txSizeY == common.Tx4x4 {
		lfm.int4x4Y |= vp9LFSizeMask[blockSize] << shiftY
	}
	return true
}

func (d *VP9Decoder) vp9LoopFilterLevel(mi *vp9dec.NeighborMi) uint8 {
	segID := int(mi.SegmentID)
	if segID < 0 || segID >= vp9dec.MaxSegments {
		return 0
	}
	ref := mi.RefFrame[0]
	if ref < vp9dec.IntraFrame || ref >= vp9dec.MaxRefFrames {
		return 0
	}
	return vp9dec.GetFilterLevel(&d.lfi, segID, ref, mi.Mode)
}

func vp9AdjustLoopFilterMask(miRows, miCols, miRow, miCol int, lfm *vp9LoopFilterMask) {
	lfm.leftY[common.Tx16x16] |= lfm.leftY[common.Tx32x32]
	lfm.aboveY[common.Tx16x16] |= lfm.aboveY[common.Tx32x32]
	lfm.leftUV[common.Tx16x16] |= lfm.leftUV[common.Tx32x32]
	lfm.aboveUV[common.Tx16x16] |= lfm.aboveUV[common.Tx32x32]

	lfm.leftY[common.Tx8x8] |= lfm.leftY[common.Tx4x4] & 0x1111111111111111
	lfm.leftY[common.Tx4x4] &^= 0x1111111111111111
	lfm.aboveY[common.Tx8x8] |= lfm.aboveY[common.Tx4x4] & 0x000000ff000000ff
	lfm.aboveY[common.Tx4x4] &^= 0x000000ff000000ff
	lfm.leftUV[common.Tx8x8] |= lfm.leftUV[common.Tx4x4] & 0x1111
	lfm.leftUV[common.Tx4x4] &^= 0x1111
	lfm.aboveUV[common.Tx8x8] |= lfm.aboveUV[common.Tx4x4] & 0x000f
	lfm.aboveUV[common.Tx4x4] &^= 0x000f

	if miRow+common.MiBlockSize > miRows {
		rows := miRows - miRow
		maskY := (uint64(1) << uint(rows<<3)) - 1
		maskUV := uint16((uint32(1) << uint(((rows+1)>>1)<<2)) - 1)
		for i := range int(common.Tx32x32) {
			lfm.leftY[i] &= maskY
			lfm.aboveY[i] &= maskY
			lfm.leftUV[i] &= maskUV
			lfm.aboveUV[i] &= maskUV
		}
		lfm.int4x4Y &= maskY
		lfm.int4x4UV &= maskUV
		if rows == 1 {
			lfm.aboveUV[common.Tx8x8] |= lfm.aboveUV[common.Tx16x16]
			lfm.aboveUV[common.Tx16x16] = 0
		}
		if rows == 5 {
			wide := lfm.aboveUV[common.Tx16x16] & 0xff00
			lfm.aboveUV[common.Tx8x8] |= wide
			lfm.aboveUV[common.Tx16x16] &^= wide
		}
	}

	if miCol+common.MiBlockSize > miCols {
		columns := miCols - miCol
		maskY := uint64((uint64(1)<<uint(columns))-1) * 0x0101010101010101
		maskUV := uint16(((uint32(1) << uint((columns+1)>>1)) - 1) * 0x1111)
		maskUVInt := uint16(((uint32(1) << uint(columns>>1)) - 1) * 0x1111)
		for i := range int(common.Tx32x32) {
			lfm.leftY[i] &= maskY
			lfm.aboveY[i] &= maskY
			lfm.leftUV[i] &= maskUV
			lfm.aboveUV[i] &= maskUV
		}
		lfm.int4x4Y &= maskY
		lfm.int4x4UV &= maskUVInt
		if columns == 1 {
			lfm.leftUV[common.Tx8x8] |= lfm.leftUV[common.Tx16x16]
			lfm.leftUV[common.Tx16x16] = 0
		}
		if columns == 5 {
			wide := lfm.leftUV[common.Tx16x16] & 0xcccc
			lfm.leftUV[common.Tx8x8] |= wide
			lfm.leftUV[common.Tx16x16] &^= wide
		}
	}

	if miCol == 0 {
		for i := range int(common.Tx32x32) {
			lfm.leftY[i] &= 0xfefefefefefefefe
			lfm.leftUV[i] &= 0xeeee
		}
	}
}

func (d *VP9Decoder) vp9FilterLoopBlock(miRows, miRow, miCol int,
	lfm *vp9LoopFilterMask,
) bool {
	if !d.vp9FilterLoopBlockPlaneSS00(d.frameYFull, d.frameYOrigin, d.lastFrame.YStride,
		miRows, miRow, miCol, lfm) {
		return false
	}
	if !d.vp9FilterLoopBlockPlaneSS11(d.frameUFull, d.frameUOrigin, d.lastFrame.UStride,
		miRows, miRow, miCol, lfm) {
		return false
	}
	return d.vp9FilterLoopBlockPlaneSS11(d.frameVFull, d.frameVOrigin, d.lastFrame.VStride,
		miRows, miRow, miCol, lfm)
}

func (d *VP9Decoder) vp9FilterLoopBlockPlane(miRows, miRow, miCol int,
	plane vp9LoopFilterPlane, lfm *vp9LoopFilterMask,
) bool {
	switch plane {
	case vp9LoopFilterPlaneY:
		return d.vp9FilterLoopBlockPlaneSS00(d.frameYFull, d.frameYOrigin,
			d.lastFrame.YStride, miRows, miRow, miCol, lfm)
	case vp9LoopFilterPlaneU:
		return d.vp9FilterLoopBlockPlaneSS11(d.frameUFull, d.frameUOrigin,
			d.lastFrame.UStride, miRows, miRow, miCol, lfm)
	case vp9LoopFilterPlaneV:
		return d.vp9FilterLoopBlockPlaneSS11(d.frameVFull, d.frameVOrigin,
			d.lastFrame.VStride, miRows, miRow, miCol, lfm)
	default:
		return false
	}
}

func (d *VP9Decoder) vp9FilterLoopBlockPlaneSS00(plane []byte, origin, stride int,
	miRows, miRow, miCol int, lfm *vp9LoopFilterMask,
) bool {
	if stride <= 0 || len(plane) == 0 {
		return false
	}
	base := origin + miRow*common.MiSize*stride + miCol*common.MiSize
	if base < 0 || base >= len(plane) {
		return false
	}

	mask16 := lfm.leftY[common.Tx16x16]
	mask8 := lfm.leftY[common.Tx8x8]
	mask4 := lfm.leftY[common.Tx4x4]
	mask4Int := lfm.int4x4Y
	offset := base
	for r := 0; r < common.MiBlockSize && miRow+r < miRows; r += 2 {
		vp9FilterSelectivelyVertRow2(0, plane, offset, stride,
			uint32(mask16), uint32(mask8), uint32(mask4), uint32(mask4Int),
			&d.lfi, lfm.lflY[r<<3:])
		offset += 16 * stride
		mask16 >>= 16
		mask8 >>= 16
		mask4 >>= 16
		mask4Int >>= 16
	}

	mask16 = lfm.aboveY[common.Tx16x16]
	mask8 = lfm.aboveY[common.Tx8x8]
	mask4 = lfm.aboveY[common.Tx4x4]
	mask4Int = lfm.int4x4Y
	offset = base
	for r := 0; r < common.MiBlockSize && miRow+r < miRows; r++ {
		var mask16R, mask8R, mask4R uint32
		if miRow+r != 0 {
			mask16R = uint32(mask16 & 0xff)
			mask8R = uint32(mask8 & 0xff)
			mask4R = uint32(mask4 & 0xff)
		}
		vp9FilterSelectivelyHoriz(plane, offset, stride,
			mask16R, mask8R, mask4R, uint32(mask4Int&0xff),
			&d.lfi, lfm.lflY[r<<3:])
		offset += 8 * stride
		mask16 >>= 8
		mask8 >>= 8
		mask4 >>= 8
		mask4Int >>= 8
	}
	return true
}

func (d *VP9Decoder) vp9FilterLoopBlockPlaneSS11(plane []byte, origin, stride int,
	miRows, miRow, miCol int, lfm *vp9LoopFilterMask,
) bool {
	if stride <= 0 || len(plane) == 0 {
		return false
	}
	base := origin + (miRow*(common.MiSize>>1))*stride + miCol*(common.MiSize>>1)
	if base < 0 || base >= len(plane) {
		return false
	}
	var lflUV [16]uint8
	mask16 := lfm.leftUV[common.Tx16x16]
	mask8 := lfm.leftUV[common.Tx8x8]
	mask4 := lfm.leftUV[common.Tx4x4]
	mask4Int := lfm.int4x4UV
	offset := base
	for r := 0; r < common.MiBlockSize && miRow+r < miRows; r += 4 {
		for c := range common.MiBlockSize >> 1 {
			lflUV[(r<<1)+c] = lfm.lflY[(r<<3)+(c<<1)]
			lflUV[((r+2)<<1)+c] = lfm.lflY[((r+2)<<3)+(c<<1)]
		}
		vp9FilterSelectivelyVertRow2(1, plane, offset, stride,
			uint32(mask16), uint32(mask8), uint32(mask4), uint32(mask4Int),
			&d.lfi, lflUV[r<<1:])
		offset += 16 * stride
		mask16 >>= 8
		mask8 >>= 8
		mask4 >>= 8
		mask4Int >>= 8
	}

	mask16 = lfm.aboveUV[common.Tx16x16]
	mask8 = lfm.aboveUV[common.Tx8x8]
	mask4 = lfm.aboveUV[common.Tx4x4]
	mask4Int = lfm.int4x4UV
	offset = base
	for r := 0; r < common.MiBlockSize && miRow+r < miRows; r += 2 {
		mask4IntR := uint32(mask4Int & 0xf)
		if miRow+r == miRows-1 {
			mask4IntR = 0
		}
		var mask16R, mask8R, mask4R uint32
		if miRow+r != 0 {
			mask16R = uint32(mask16 & 0xf)
			mask8R = uint32(mask8 & 0xf)
			mask4R = uint32(mask4 & 0xf)
		}
		vp9FilterSelectivelyHoriz(plane, offset, stride,
			mask16R, mask8R, mask4R, mask4IntR,
			&d.lfi, lflUV[r<<1:])
		offset += 8 * stride
		mask16 >>= 4
		mask8 >>= 4
		mask4 >>= 4
		mask4Int >>= 4
	}
	return true
}

func vp9FilterSelectivelyVertRow2(subsamplingFactor int,
	plane []byte, offset, pitch int,
	mask16, mask8, mask4, mask4Int uint32,
	lfi *vp9dec.LoopFilterInfoN, lfl []uint8,
) {
	dualMaskCutoff := uint32(0xffff)
	lflForward := 8
	if subsamplingFactor != 0 {
		dualMaskCutoff = 0xff
		lflForward = 4
	}
	dualOne := uint32(1 | (1 << uint(lflForward)))
	ss0 := offset
	for mask := (mask16 | mask8 | mask4 | mask4Int) & dualMaskCutoff; mask != 0; mask = (mask &^ dualOne) >> 1 {
		if mask&dualOne != 0 {
			level0 := lfi.Lfthr[lfl[0]]
			level1 := lfi.Lfthr[lfl[lflForward]]
			ss1 := ss0 + 8*pitch

			if mask16&dualOne != 0 {
				if mask16&dualOne == dualOne {
					dsp.VpxLpfVertical16Dual(plane, ss0, pitch,
						level0.Mblim, level0.Lim, level0.HevThr)
				} else {
					idx := 0
					if mask16&1 == 0 {
						idx = 1
					}
					level := level0
					ss := ss0
					if idx == 1 {
						level = level1
						ss = ss1
					}
					dsp.VpxLpfVertical16(plane, ss, pitch, level.Mblim, level.Lim, level.HevThr)
				}
			}
			if mask8&dualOne != 0 {
				if mask8&dualOne == dualOne {
					dsp.VpxLpfVertical8Dual(plane, ss0, pitch,
						level0.Mblim, level0.Lim, level0.HevThr,
						level1.Mblim, level1.Lim, level1.HevThr)
				} else {
					idx := 0
					if mask8&1 == 0 {
						idx = 1
					}
					level := level0
					ss := ss0
					if idx == 1 {
						level = level1
						ss = ss1
					}
					dsp.VpxLpfVertical8(plane, ss, pitch, level.Mblim, level.Lim, level.HevThr)
				}
			}
			if mask4&dualOne != 0 {
				if mask4&dualOne == dualOne {
					dsp.VpxLpfVertical4Dual(plane, ss0, pitch,
						level0.Mblim, level0.Lim, level0.HevThr,
						level1.Mblim, level1.Lim, level1.HevThr)
				} else {
					idx := 0
					if mask4&1 == 0 {
						idx = 1
					}
					level := level0
					ss := ss0
					if idx == 1 {
						level = level1
						ss = ss1
					}
					dsp.VpxLpfVertical4(plane, ss, pitch, level.Mblim, level.Lim, level.HevThr)
				}
			}
			if mask4Int&dualOne != 0 {
				if mask4Int&dualOne == dualOne {
					dsp.VpxLpfVertical4Dual(plane, ss0+4, pitch,
						level0.Mblim, level0.Lim, level0.HevThr,
						level1.Mblim, level1.Lim, level1.HevThr)
				} else {
					idx := 0
					if mask4Int&1 == 0 {
						idx = 1
					}
					level := level0
					ss := ss0 + 4
					if idx == 1 {
						level = level1
						ss = ss1 + 4
					}
					dsp.VpxLpfVertical4(plane, ss, pitch, level.Mblim, level.Lim, level.HevThr)
				}
			}
		}
		ss0 += 8
		lfl = lfl[1:]
		mask16 >>= 1
		mask8 >>= 1
		mask4 >>= 1
		mask4Int >>= 1
	}
}

func vp9FilterSelectivelyHoriz(plane []byte, offset, pitch int,
	mask16, mask8, mask4, mask4Int uint32,
	lfi *vp9dec.LoopFilterInfoN, lfl []uint8,
) {
	for mask := mask16 | mask8 | mask4 | mask4Int; mask != 0; {
		count := 1
		if mask&1 != 0 {
			level := lfi.Lfthr[lfl[0]]
			switch {
			case mask16&1 != 0:
				if mask16&3 == 3 {
					dsp.VpxLpfHorizontal16Dual(plane, offset, pitch,
						level.Mblim, level.Lim, level.HevThr)
					count = 2
				} else {
					dsp.VpxLpfHorizontal16(plane, offset, pitch,
						level.Mblim, level.Lim, level.HevThr)
				}
			case mask8&1 != 0:
				if mask8&3 == 3 {
					next := lfi.Lfthr[lfl[1]]
					dsp.VpxLpfHorizontal8Dual(plane, offset, pitch,
						level.Mblim, level.Lim, level.HevThr,
						next.Mblim, next.Lim, next.HevThr)
					if mask4Int&3 == 3 {
						dsp.VpxLpfHorizontal4Dual(plane, offset+4*pitch, pitch,
							level.Mblim, level.Lim, level.HevThr,
							next.Mblim, next.Lim, next.HevThr)
					} else if mask4Int&1 != 0 {
						dsp.VpxLpfHorizontal4(plane, offset+4*pitch, pitch,
							level.Mblim, level.Lim, level.HevThr)
					} else if mask4Int&2 != 0 {
						dsp.VpxLpfHorizontal4(plane, offset+8+4*pitch, pitch,
							next.Mblim, next.Lim, next.HevThr)
					}
					count = 2
				} else {
					dsp.VpxLpfHorizontal8(plane, offset, pitch,
						level.Mblim, level.Lim, level.HevThr)
					if mask4Int&1 != 0 {
						dsp.VpxLpfHorizontal4(plane, offset+4*pitch, pitch,
							level.Mblim, level.Lim, level.HevThr)
					}
				}
			case mask4&1 != 0:
				if mask4&3 == 3 {
					next := lfi.Lfthr[lfl[1]]
					dsp.VpxLpfHorizontal4Dual(plane, offset, pitch,
						level.Mblim, level.Lim, level.HevThr,
						next.Mblim, next.Lim, next.HevThr)
					if mask4Int&3 == 3 {
						dsp.VpxLpfHorizontal4Dual(plane, offset+4*pitch, pitch,
							level.Mblim, level.Lim, level.HevThr,
							next.Mblim, next.Lim, next.HevThr)
					} else if mask4Int&1 != 0 {
						dsp.VpxLpfHorizontal4(plane, offset+4*pitch, pitch,
							level.Mblim, level.Lim, level.HevThr)
					} else if mask4Int&2 != 0 {
						dsp.VpxLpfHorizontal4(plane, offset+8+4*pitch, pitch,
							next.Mblim, next.Lim, next.HevThr)
					}
					count = 2
				} else {
					dsp.VpxLpfHorizontal4(plane, offset, pitch,
						level.Mblim, level.Lim, level.HevThr)
					if mask4Int&1 != 0 {
						dsp.VpxLpfHorizontal4(plane, offset+4*pitch, pitch,
							level.Mblim, level.Lim, level.HevThr)
					}
				}
			default:
				dsp.VpxLpfHorizontal4(plane, offset+4*pitch, pitch,
					level.Mblim, level.Lim, level.HevThr)
			}
		}
		offset += 8 * count
		lfl = lfl[count:]
		mask >>= uint(count)
		mask16 >>= uint(count)
		mask8 >>= uint(count)
		mask4 >>= uint(count)
		mask4Int >>= uint(count)
	}
}
