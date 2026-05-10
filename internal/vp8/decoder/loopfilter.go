package decoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

// Ported from libvpx v1.16.0 vp8/common/vp8_loopfilter.c frame traversal and
// vp8/common/loopfilter_filters.c filter wrappers.

var ErrLoopFilterBufferTooSmall = errors.New("govpx: VP8 loop filter buffer too small")

func ApplyLoopFilter(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) error {
	if header.Level == 0 {
		return nil
	}
	if rows < 0 || cols < 0 {
		return ErrLoopFilterBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrLoopFilterBufferTooSmall
	}
	required := rows * cols
	if img == nil || lfi == nil || len(modes) < required || frameType < common.KeyFrame || frameType > common.InterFrame {
		return ErrLoopFilterBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrLoopFilterBufferTooSmall
	}

	common.InitLoopFilterInfo(lfi, int(header.SharpnessLevel))
	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))

	if header.Type == SimpleLoopFilter {
		return applySimpleLoopFilterGrid(img, rows, cols, modes, lfi)
	}
	return applyNormalLoopFilterGrid(img, rows, cols, modes, frameType, lfi)
}

// ApplyLoopFilterUnchecked is the decoder hot-path variant. The caller must
// already have validated the image grid and produced trusted macroblock modes.
func ApplyLoopFilterUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) {
	if header.Level == 0 {
		return
	}

	common.InitLoopFilterInfo(lfi, int(header.SharpnessLevel))
	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))

	if header.Type == SimpleLoopFilter {
		applySimpleLoopFilterGridUnchecked(img, rows, cols, modes, lfi)
		return
	}
	applyNormalLoopFilterGridUnchecked(img, rows, cols, modes, frameType, lfi)
}

// ApplyLoopFilterFullLuma walks every MB in the frame but applies the
// loop filter to the luma plane only. It exists so the encoder's full
// pick-loop-filter-level search can score levels using luma SSE
// without paying for chroma filtering work that the picker never reads.
// Reconstruction-time loop filtering still goes through ApplyLoopFilter
// so the committed reference matches libvpx exactly.
func ApplyLoopFilterFullLuma(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) error {
	if header.Level == 0 {
		return nil
	}
	if rows < 0 || cols < 0 {
		return ErrLoopFilterBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrLoopFilterBufferTooSmall
	}
	required := rows * cols
	if img == nil || lfi == nil || len(modes) < required || frameType < common.KeyFrame || frameType > common.InterFrame {
		return ErrLoopFilterBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrLoopFilterBufferTooSmall
	}

	common.InitLoopFilterInfo(lfi, int(header.SharpnessLevel))
	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))

	return applyLoopFilterFullLumaWithInfo(img, rows, cols, modes, frameType, header, lfi)
}

// ApplyLoopFilterFullLumaPrepared is the encoder hot-path variant used by the
// loop-filter level picker. The caller must have already initialized lfi for
// header.SharpnessLevel; this mirrors libvpx's picker, which reuses sharpness
// tables while varying only the candidate level.
func ApplyLoopFilterFullLumaPrepared(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) error {
	if header.Level == 0 {
		return nil
	}
	if rows < 0 || cols < 0 {
		return ErrLoopFilterBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrLoopFilterBufferTooSmall
	}
	required := rows * cols
	if img == nil || lfi == nil || len(modes) < required || frameType < common.KeyFrame || frameType > common.InterFrame {
		return ErrLoopFilterBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrLoopFilterBufferTooSmall
	}

	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))
	return applyLoopFilterFullLumaWithInfo(img, rows, cols, modes, frameType, header, lfi)
}

// ApplyLoopFilterFullLumaPreparedUnchecked is the encoder picker hot path.
// The caller must have already validated img, dimensions, frameType, mode
// count, and sharpness setup. This mirrors libvpx's picker loop over trusted
// MODE_INFO state.
func ApplyLoopFilterFullLumaPreparedUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) {
	if header.Level == 0 {
		return
	}
	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))
	applyLoopFilterFullLumaWithInfoUnchecked(img, rows, cols, modes, frameType, header, lfi)
}

func applyLoopFilterFullLumaWithInfo(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, lfi *common.LoopFilterInfo) error {
	if header.Type == SimpleLoopFilter {
		return applySimpleLoopFilterPartialLuma(img, rows, cols, modes, lfi, 0, rows)
	}
	return applyNormalLoopFilterPartialLuma(img, rows, cols, modes, frameType, lfi, 0, rows)
}

func applyLoopFilterFullLumaWithInfoUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, lfi *common.LoopFilterInfo) {
	if header.Type == SimpleLoopFilter {
		applySimpleLoopFilterPartialLumaUnchecked(img, cols, modes, lfi, 0, rows)
		return
	}
	applyNormalLoopFilterPartialLumaUnchecked(img, cols, modes, frameType, lfi, 0, rows)
}

func ApplyLoopFilterChromaOnly(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) error {
	if header.Level == 0 || header.Type == SimpleLoopFilter {
		return nil
	}
	if rows < 0 || cols < 0 {
		return ErrLoopFilterBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrLoopFilterBufferTooSmall
	}
	required := rows * cols
	if img == nil || lfi == nil || len(modes) < required || frameType < common.KeyFrame || frameType > common.InterFrame {
		return ErrLoopFilterBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrLoopFilterBufferTooSmall
	}

	common.InitLoopFilterInfo(lfi, int(header.SharpnessLevel))
	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))
	return applyNormalLoopFilterChromaOnlyGrid(img, rows, cols, modes, frameType, lfi)
}

// ApplyLoopFilterChromaOnlyPrepared skips sharpness-table rebuilds when the
// caller already prepared lfi for header.SharpnessLevel.
func ApplyLoopFilterChromaOnlyPrepared(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) error {
	if header.Level == 0 || header.Type == SimpleLoopFilter {
		return nil
	}
	if rows < 0 || cols < 0 {
		return ErrLoopFilterBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrLoopFilterBufferTooSmall
	}
	required := rows * cols
	if img == nil || lfi == nil || len(modes) < required || frameType < common.KeyFrame || frameType > common.InterFrame {
		return ErrLoopFilterBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrLoopFilterBufferTooSmall
	}

	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))
	return applyNormalLoopFilterChromaOnlyGrid(img, rows, cols, modes, frameType, lfi)
}

// ApplyLoopFilterPartial mirrors libvpx's vp8_loop_filter_partial_frame: it
// filters only the luma plane for MB rows in [startRow, startRow+rowCount) and
// is intended for use by the encoder's fast loop-filter level picker. Unlike
// ApplyLoopFilter it skips the chroma planes entirely and unconditionally
// applies the macroblock horizontal edge filter on the first row of the
// window (libvpx parity, since the partial window never starts at MB row 0
// for realistic frame sizes).
func ApplyLoopFilterPartial(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo, startRow int, rowCount int) error {
	if header.Level == 0 {
		return nil
	}
	if rows < 0 || cols < 0 {
		return ErrLoopFilterBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrLoopFilterBufferTooSmall
	}
	required := rows * cols
	if img == nil || lfi == nil || len(modes) < required || frameType < common.KeyFrame || frameType > common.InterFrame {
		return ErrLoopFilterBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrLoopFilterBufferTooSmall
	}
	if startRow < 0 || rowCount < 0 || startRow > rows {
		return ErrLoopFilterBufferTooSmall
	}
	if startRow+rowCount > rows {
		rowCount = rows - startRow
	}
	if rowCount == 0 {
		return nil
	}

	common.InitLoopFilterInfo(lfi, int(header.SharpnessLevel))
	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))

	if header.Type == SimpleLoopFilter {
		return applySimpleLoopFilterPartialLuma(img, rows, cols, modes, lfi, startRow, rowCount)
	}
	return applyNormalLoopFilterPartialLuma(img, rows, cols, modes, frameType, lfi, startRow, rowCount)
}

// ApplyLoopFilterPartialPrepared is the fast LF-picker variant. The caller
// must have already initialized lfi for header.SharpnessLevel.
func ApplyLoopFilterPartialPrepared(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo, startRow int, rowCount int) error {
	if header.Level == 0 {
		return nil
	}
	if rows < 0 || cols < 0 {
		return ErrLoopFilterBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrLoopFilterBufferTooSmall
	}
	required := rows * cols
	if img == nil || lfi == nil || len(modes) < required || frameType < common.KeyFrame || frameType > common.InterFrame {
		return ErrLoopFilterBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrLoopFilterBufferTooSmall
	}
	if startRow < 0 || rowCount < 0 || startRow > rows {
		return ErrLoopFilterBufferTooSmall
	}
	if startRow+rowCount > rows {
		rowCount = rows - startRow
	}
	if rowCount == 0 {
		return nil
	}

	common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))

	if header.Type == SimpleLoopFilter {
		return applySimpleLoopFilterPartialLuma(img, rows, cols, modes, lfi, startRow, rowCount)
	}
	return applyNormalLoopFilterPartialLuma(img, rows, cols, modes, frameType, lfi, startRow, rowCount)
}

func applyNormalLoopFilterPartialLuma(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo, startRow int, rowCount int) error {
	_ = rows
	for row := startRow; row < startRow+rowCount; row++ {
		yRow := row * 16 * img.YStride
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if !validLoopFilterMode(mode) {
				return ErrLoopFilterBufferTooSmall
			}
			level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
			if level == 0 {
				continue
			}
			yOff := yRow + col*16
			applyNormalLoopFilterPartialLumaMB(img, row, col, yOff, mode, frameType, level, lfi)
		}
	}
	return nil
}

func applyNormalLoopFilterPartialLumaUnchecked(img *common.Image, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo, startRow int, rowCount int) {
	for row := startRow; row < startRow+rowCount; row++ {
		yRow := row * 16 * img.YStride
		for col := range cols {
			mode := &modes[row*cols+col]
			level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
			if level == 0 {
				continue
			}
			yOff := yRow + col*16
			applyNormalLoopFilterPartialLumaMB(img, row, col, yOff, mode, frameType, level, lfi)
		}
	}
}

func applySimpleLoopFilterPartialLuma(img *common.Image, rows int, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo, startRow int, rowCount int) error {
	_ = rows
	for row := startRow; row < startRow+rowCount; row++ {
		yRow := row * 16 * img.YStride
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if !validLoopFilterMode(mode) {
				return ErrLoopFilterBufferTooSmall
			}
			level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
			if level == 0 {
				continue
			}
			yOff := yRow + col*16
			applySimpleLoopFilterPartialLumaMB(img, row, col, yOff, mode, level, lfi)
		}
	}
	return nil
}

func applySimpleLoopFilterPartialLumaUnchecked(img *common.Image, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo, startRow int, rowCount int) {
	for row := startRow; row < startRow+rowCount; row++ {
		yRow := row * 16 * img.YStride
		for col := range cols {
			mode := &modes[row*cols+col]
			level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
			if level == 0 {
				continue
			}
			yOff := yRow + col*16
			applySimpleLoopFilterPartialLumaMB(img, row, col, yOff, mode, level, lfi)
		}
	}
}

func applyNormalLoopFilterPartialLumaMB(img *common.Image, row int, col int, yOff int, mode *MacroblockMode, frameType common.FrameType, level byte, lfi *common.LoopFilterInfo) {
	skipLF := loopFilterSkipsInnerEdges(mode)
	hev := lfi.HEVThresh[lfi.HEVThreshLUT[frameType][level]]
	mblim := lfi.MBLimit[level]
	blim := lfi.BLimit[level]
	lim := lfi.Limit[level]

	if col > 0 {
		dsp.MBLoopFilterVerticalEdge(img.Y[yOff-4:], img.YStride, mblim, lim, hev, 2)
	}
	if !skipLF {
		dsp.LoopFilterVerticalEdgesY(img.Y[yOff:], img.YStride, blim, lim, hev)
	}

	// libvpx's vp8_loop_filter_partial_frame applies mbh unconditionally for
	// every MB in the window. Practically the partial window starts at MB row
	// rows/2 so row > 0 always holds for realistic frame sizes; we keep the
	// guard so tiny frames where the window happens to begin at row 0 still
	// behave like the full-frame variant.
	if row > 0 {
		dsp.MBLoopFilterHorizontalEdge(img.Y[yOff-4*img.YStride:], img.YStride, mblim, lim, hev, 2)
	}
	if !skipLF {
		dsp.LoopFilterHorizontalEdgesY(img.Y[yOff:], img.YStride, blim, lim, hev)
	}
}

func applySimpleLoopFilterPartialLumaMB(img *common.Image, row int, col int, yOff int, mode *MacroblockMode, level byte, lfi *common.LoopFilterInfo) {
	skipLF := loopFilterSkipsInnerEdges(mode)
	mblim := lfi.MBLimit[level]
	blim := lfi.BLimit[level]

	if col > 0 {
		dsp.LoopFilterSimpleVerticalEdge(img.Y[yOff-2:], img.YStride, mblim)
	}
	if !skipLF {
		dsp.LoopFilterSimpleVerticalEdge(img.Y[yOff+2:], img.YStride, blim)
		dsp.LoopFilterSimpleVerticalEdge(img.Y[yOff+6:], img.YStride, blim)
		dsp.LoopFilterSimpleVerticalEdge(img.Y[yOff+10:], img.YStride, blim)
	}

	if row > 0 {
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff-2*img.YStride:], img.YStride, mblim)
	}
	if !skipLF {
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+2*img.YStride:], img.YStride, blim)
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+6*img.YStride:], img.YStride, blim)
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+10*img.YStride:], img.YStride, blim)
	}
}

func applyNormalLoopFilterGrid(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo) error {
	for row := range rows {
		if err := applyNormalLoopFilterRow(img, row, cols, modes, frameType, lfi); err != nil {
			return err
		}
	}
	return nil
}

func applyNormalLoopFilterGridUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo) {
	for row := range rows {
		applyNormalLoopFilterRowUnchecked(img, row, cols, modes, frameType, lfi)
	}
}

func applyNormalLoopFilterChromaOnlyGrid(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo) error {
	for row := range rows {
		if err := applyNormalLoopFilterChromaOnlyRow(img, row, cols, modes, frameType, lfi); err != nil {
			return err
		}
	}
	return nil
}

func applySimpleLoopFilterGridUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo) {
	for row := range rows {
		applySimpleLoopFilterRowUnchecked(img, row, cols, modes, lfi)
	}
}

func applyNormalLoopFilterRow(img *common.Image, row int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo) error {
	yRow := row * 16 * img.YStride
	uRow := row * 8 * img.UStride
	vRow := row * 8 * img.VStride
	for col := range cols {
		index := row*cols + col
		mode := &modes[index]
		if !validLoopFilterMode(mode) {
			return ErrLoopFilterBufferTooSmall
		}
		level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
		if level == 0 {
			continue
		}

		yOff := yRow + col*16
		uOff := uRow + col*8
		vOff := vRow + col*8
		applyNormalLoopFilterMB(img, row, col, yOff, uOff, vOff, mode, frameType, level, lfi)
	}
	return nil
}

func applyNormalLoopFilterRowUnchecked(img *common.Image, row int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo) {
	yRow := row * 16 * img.YStride
	uRow := row * 8 * img.UStride
	vRow := row * 8 * img.VStride
	for col := range cols {
		mode := &modes[row*cols+col]
		level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
		if level == 0 {
			continue
		}

		yOff := yRow + col*16
		uOff := uRow + col*8
		vOff := vRow + col*8
		applyNormalLoopFilterMB(img, row, col, yOff, uOff, vOff, mode, frameType, level, lfi)
	}
}

func applyNormalLoopFilterChromaOnlyRow(img *common.Image, row int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo) error {
	uRow := row * 8 * img.UStride
	vRow := row * 8 * img.VStride
	for col := range cols {
		index := row*cols + col
		mode := &modes[index]
		if !validLoopFilterMode(mode) {
			return ErrLoopFilterBufferTooSmall
		}
		level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
		if level == 0 {
			continue
		}

		uOff := uRow + col*8
		vOff := vRow + col*8
		applyNormalLoopFilterChromaOnlyMB(img, row, col, uOff, vOff, mode, frameType, level, lfi)
	}
	return nil
}

func applySimpleLoopFilterGrid(img *common.Image, rows int, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo) error {
	for row := range rows {
		if err := applySimpleLoopFilterRow(img, row, cols, modes, lfi); err != nil {
			return err
		}
	}
	return nil
}

func applySimpleLoopFilterRow(img *common.Image, row int, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo) error {
	yRow := row * 16 * img.YStride
	for col := range cols {
		index := row*cols + col
		mode := &modes[index]
		if !validLoopFilterMode(mode) {
			return ErrLoopFilterBufferTooSmall
		}
		level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
		if level == 0 {
			continue
		}

		yOff := yRow + col*16
		applySimpleLoopFilterMB(img, row, col, yOff, mode, level, lfi)
	}
	return nil
}

func applySimpleLoopFilterRowUnchecked(img *common.Image, row int, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo) {
	yRow := row * 16 * img.YStride
	for col := range cols {
		mode := &modes[row*cols+col]
		level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
		if level == 0 {
			continue
		}

		yOff := yRow + col*16
		applySimpleLoopFilterMB(img, row, col, yOff, mode, level, lfi)
	}
}

func loopFilterFrameConfig(header LoopFilterHeader, segmentation SegmentationHeader) common.LoopFilterFrameConfig {
	cfg := common.LoopFilterFrameConfig{
		SegmentationEnabled: segmentation.Enabled,
		SegmentAbsDelta:     segmentation.AbsDelta,
		ModeRefDeltaEnabled: header.DeltaEnabled,
		RefDeltas:           header.RefDeltas,
		ModeDeltas:          header.ModeDeltas,
	}
	for segment := range common.MaxMBSegments {
		cfg.SegmentLF[segment] = segmentation.FeatureData[common.MBLvlAltLF][segment]
	}
	return cfg
}

func validLoopFilterMode(mode *MacroblockMode) bool {
	return int(mode.SegmentID) < common.MaxMBSegments &&
		mode.RefFrame >= 0 &&
		mode.RefFrame < common.MaxRefFrames &&
		mode.Mode >= 0 &&
		mode.Mode < common.MBModeCount
}

func applyNormalLoopFilterMB(img *common.Image, row int, col int, yOff int, uOff int, vOff int, mode *MacroblockMode, frameType common.FrameType, level byte, lfi *common.LoopFilterInfo) {
	skipLF := loopFilterSkipsInnerEdges(mode)
	hev := lfi.HEVThresh[lfi.HEVThreshLUT[frameType][level]]
	mblim := lfi.MBLimit[level]
	blim := lfi.BLimit[level]
	lim := lfi.Limit[level]

	if col > 0 {
		dsp.MBLoopFilterVerticalEdge(img.Y[yOff-4:], img.YStride, mblim, lim, hev, 2)
		dsp.MBLoopFilterVerticalEdgeUV(img.U[uOff-4:], img.V[vOff-4:], img.UStride, mblim, lim, hev)
	}
	if !skipLF {
		dsp.LoopFilterVerticalEdgesY(img.Y[yOff:], img.YStride, blim, lim, hev)
		dsp.LoopFilterVerticalEdgeUV(img.U[uOff:], img.V[vOff:], img.UStride, blim, lim, hev)
	}

	if row > 0 {
		dsp.MBLoopFilterHorizontalEdge(img.Y[yOff-4*img.YStride:], img.YStride, mblim, lim, hev, 2)
		dsp.MBLoopFilterHorizontalEdgeUV(img.U[uOff-4*img.UStride:], img.V[vOff-4*img.VStride:], img.UStride, mblim, lim, hev)
	}
	if !skipLF {
		dsp.LoopFilterHorizontalEdgesY(img.Y[yOff:], img.YStride, blim, lim, hev)
		dsp.LoopFilterHorizontalEdgeUV(img.U[uOff:], img.V[vOff:], img.UStride, blim, lim, hev)
	}
}

func applyNormalLoopFilterChromaOnlyMB(img *common.Image, row int, col int, uOff int, vOff int, mode *MacroblockMode, frameType common.FrameType, level byte, lfi *common.LoopFilterInfo) {
	skipLF := loopFilterSkipsInnerEdges(mode)
	hev := lfi.HEVThresh[lfi.HEVThreshLUT[frameType][level]]
	mblim := lfi.MBLimit[level]
	blim := lfi.BLimit[level]
	lim := lfi.Limit[level]

	if col > 0 {
		dsp.MBLoopFilterVerticalEdgeUV(img.U[uOff-4:], img.V[vOff-4:], img.UStride, mblim, lim, hev)
	}
	if !skipLF {
		dsp.LoopFilterVerticalEdgeUV(img.U[uOff:], img.V[vOff:], img.UStride, blim, lim, hev)
	}

	if row > 0 {
		dsp.MBLoopFilterHorizontalEdgeUV(img.U[uOff-4*img.UStride:], img.V[vOff-4*img.VStride:], img.UStride, mblim, lim, hev)
	}
	if !skipLF {
		dsp.LoopFilterHorizontalEdgeUV(img.U[uOff:], img.V[vOff:], img.UStride, blim, lim, hev)
	}
}

func applySimpleLoopFilterMB(img *common.Image, row int, col int, yOff int, mode *MacroblockMode, level byte, lfi *common.LoopFilterInfo) {
	skipLF := loopFilterSkipsInnerEdges(mode)
	mblim := lfi.MBLimit[level]
	blim := lfi.BLimit[level]

	if col > 0 {
		dsp.LoopFilterSimpleVerticalEdge(img.Y[yOff-2:], img.YStride, mblim)
	}
	if !skipLF {
		dsp.LoopFilterSimpleVerticalEdge(img.Y[yOff+2:], img.YStride, blim)
		dsp.LoopFilterSimpleVerticalEdge(img.Y[yOff+6:], img.YStride, blim)
		dsp.LoopFilterSimpleVerticalEdge(img.Y[yOff+10:], img.YStride, blim)
	}

	if row > 0 {
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff-2*img.YStride:], img.YStride, mblim)
	}
	if !skipLF {
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+2*img.YStride:], img.YStride, blim)
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+6*img.YStride:], img.YStride, blim)
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+10*img.YStride:], img.YStride, blim)
	}
}

func loopFilterSkipsInnerEdges(mode *MacroblockMode) bool {
	return mode.Mode != common.BPred && mode.Mode != common.SplitMV && mode.MBSkipCoeff
}
