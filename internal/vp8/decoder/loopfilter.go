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

// ApplyLoopFilterFullLumaPreparedUnchecked is the encoder picker hot path.
// The caller must have already validated img, dimensions, frameType, mode
// count, and sharpness setup. This mirrors libvpx's picker loop over trusted
// MODE_INFO state.
func ApplyLoopFilterFullLumaPreparedUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) {
	ApplyLoopFilterFullLumaConfiguredUnchecked(img, rows, cols, modes, frameType, header.Type, int(header.Level), LoopFilterFrameConfig(header, segmentation), lfi)
}

// ApplyLoopFilterFullLumaConfiguredUnchecked is the lowest-level encoder
// picker path: sharpness tables are already initialized and the caller passes
// the immutable per-frame delta config used for this trial set.
func ApplyLoopFilterFullLumaConfiguredUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, filterType LoopFilterType, level int, cfg common.LoopFilterFrameConfig, lfi *common.LoopFilterInfo) {
	common.InitLoopFilterFrame(lfi, level, cfg)
	if filterType == SimpleLoopFilter {
		applySimpleLoopFilterPartialLumaUnchecked(img, cols, modes, lfi, 0, rows, false)
		return
	}
	applyNormalLoopFilterPartialLumaUnchecked(img, cols, modes, frameType, lfi, 0, rows, false)
}

// ApplyLoopFilterChromaOnlyPrepared skips sharpness-table rebuilds when the
// caller already prepared lfi for header.SharpnessLevel.
func ApplyLoopFilterChromaOnlyPrepared(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) error {
	if err := ApplyLoopFilterChromaOnlyPreparedInit(img, rows, cols, modes, frameType, header, segmentation, lfi); err != nil {
		return err
	}
	if header.Level == 0 || header.Type == SimpleLoopFilter {
		return nil
	}
	return applyNormalLoopFilterChromaOnlyGrid(img, rows, cols, modes, frameType, lfi)
}

// ApplyLoopFilterChromaOnlyPreparedInit validates the chroma-only LF inputs and
// initializes lfi for the requested frame. Returns nil with no side effects
// when the filter is disabled or the simple type is selected. Used by the
// row-parallel encoder dispatch which performs upfront validation on the main
// goroutine and then submits per-row work to the worker pool.
func ApplyLoopFilterChromaOnlyPreparedInit(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo) error {
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

	common.InitLoopFilterFrame(lfi, int(header.Level), LoopFilterFrameConfig(header, segmentation))
	return nil
}

// ApplyLoopFilterChromaOnlyPreparedRow filters a single MB row's chroma planes
// using a pre-initialized lfi. Each row's writes are disjoint from every other
// row's writes (chroma horizontal edges at the MB boundary only touch 4 rows
// above and 4 rows below the boundary, but the inner-edge writes for row R
// stop at chroma row R*8+5, and row R+1's MB top edge starts reading at chroma
// row (R+1)*8-2 = R*8+6, so per-row dispatch is safe without a wave-front).
func ApplyLoopFilterChromaOnlyPreparedRow(img *common.Image, row int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo) error {
	return applyNormalLoopFilterChromaOnlyRow(img, row, cols, modes, frameType, lfi)
}

// ApplyLoopFilterPartialPreparedUnchecked is the encoder fast-picker hot path.
// The caller must have already validated img, dimensions, frameType, mode
// count, partial window, and sharpness setup.
func ApplyLoopFilterPartialPreparedUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, header LoopFilterHeader, segmentation SegmentationHeader, lfi *common.LoopFilterInfo, startRow int, rowCount int) {
	if rowCount <= 0 {
		return
	}
	if startRow+rowCount > rows {
		rowCount = rows - startRow
	}
	if rowCount <= 0 {
		return
	}

	ApplyLoopFilterPartialConfiguredUnchecked(img, rows, cols, modes, frameType, header.Type, int(header.Level), LoopFilterFrameConfig(header, segmentation), lfi, startRow, rowCount)
}

// ApplyLoopFilterPartialConfiguredUnchecked mirrors libvpx's
// vp8_loop_filter_partial_frame over trusted encoder state.
func ApplyLoopFilterPartialConfiguredUnchecked(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, filterType LoopFilterType, level int, cfg common.LoopFilterFrameConfig, lfi *common.LoopFilterInfo, startRow int, rowCount int) {
	if rowCount <= 0 {
		return
	}
	if startRow+rowCount > rows {
		rowCount = rows - startRow
	}
	if rowCount <= 0 {
		return
	}

	common.InitLoopFilterFrame(lfi, level, cfg)

	if filterType == SimpleLoopFilter {
		applySimpleLoopFilterPartialLumaUnchecked(img, cols, modes, lfi, startRow, rowCount, true)
		return
	}
	applyNormalLoopFilterPartialLumaUnchecked(img, cols, modes, frameType, lfi, startRow, rowCount, true)
}

func applyNormalLoopFilterPartialLumaUnchecked(img *common.Image, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo, startRow int, rowCount int, topContext bool) {
	for row := startRow; row < startRow+rowCount; row++ {
		yRow := row * 16 * img.YStride
		for col := range cols {
			mode := &modes[row*cols+col]
			level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
			if level == 0 {
				continue
			}
			yOff := yRow + col*16
			applyNormalLoopFilterPartialLumaMB(img, row, col, yOff, mode, frameType, level, lfi, topContext)
		}
	}
}

func applySimpleLoopFilterPartialLumaUnchecked(img *common.Image, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo, startRow int, rowCount int, topContext bool) {
	for row := startRow; row < startRow+rowCount; row++ {
		yRow := row * 16 * img.YStride
		for col := range cols {
			mode := &modes[row*cols+col]
			level := lfi.Level[mode.SegmentID][mode.RefFrame][lfi.ModeLFLUT[mode.Mode]]
			if level == 0 {
				continue
			}
			yOff := yRow + col*16
			applySimpleLoopFilterPartialLumaMB(img, row, col, yOff, mode, level, lfi, topContext)
		}
	}
}

func applyNormalLoopFilterPartialLumaMB(img *common.Image, row int, col int, yOff int, mode *MacroblockMode, frameType common.FrameType, level byte, lfi *common.LoopFilterInfo, topContext bool) {
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

	if row > 0 || topContext {
		dsp.MBLoopFilterHorizontalEdge(loopFilterYAt(img, yOff-4*img.YStride), img.YStride, mblim, lim, hev, 2)
	}
	if !skipLF {
		dsp.LoopFilterHorizontalEdgesY(img.Y[yOff:], img.YStride, blim, lim, hev)
	}
}

func applySimpleLoopFilterPartialLumaMB(img *common.Image, row int, col int, yOff int, mode *MacroblockMode, level byte, lfi *common.LoopFilterInfo, topContext bool) {
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

	if row > 0 || topContext {
		dsp.LoopFilterSimpleHorizontalEdge(loopFilterYAt(img, yOff-2*img.YStride), img.YStride, mblim)
	}
	if !skipLF {
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+2*img.YStride:], img.YStride, blim)
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+6*img.YStride:], img.YStride, blim)
		dsp.LoopFilterSimpleHorizontalEdge(img.Y[yOff+10*img.YStride:], img.YStride, blim)
	}
}

func loopFilterYAt(img *common.Image, off int) []byte {
	if off >= 0 {
		return img.Y[off:]
	}
	return img.YFull[img.YOrigin+off:]
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
	return LoopFilterFrameConfig(header, segmentation)
}

// LoopFilterFrameConfig translates parsed VP8 loop-filter header state into
// the immutable frame-level tables used by common.InitLoopFilterFrame.
func LoopFilterFrameConfig(header LoopFilterHeader, segmentation SegmentationHeader) common.LoopFilterFrameConfig {
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
