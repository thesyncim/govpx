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

	if header.Type == SimpleLoopFilter {
		return applySimpleLoopFilterPartialLuma(img, rows, cols, modes, lfi, 0, rows)
	}
	return applyNormalLoopFilterPartialLuma(img, rows, cols, modes, frameType, lfi, 0, rows)
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

func applyNormalLoopFilterPartialLuma(img *common.Image, rows int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo, startRow int, rowCount int) error {
	_ = rows
	for row := startRow; row < startRow+rowCount; row++ {
		yRow := row * 16 * img.YStride
		for col := 0; col < cols; col++ {
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

func applySimpleLoopFilterPartialLuma(img *common.Image, rows int, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo, startRow int, rowCount int) error {
	_ = rows
	for row := startRow; row < startRow+rowCount; row++ {
		yRow := row * 16 * img.YStride
		for col := 0; col < cols; col++ {
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
		dsp.LoopFilterVerticalEdge(img.Y[yOff:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterVerticalEdge(img.Y[yOff+4:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterVerticalEdge(img.Y[yOff+8:], img.YStride, blim, lim, hev, 2)
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
		dsp.LoopFilterHorizontalEdge(img.Y[yOff:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterHorizontalEdge(img.Y[yOff+4*img.YStride:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterHorizontalEdge(img.Y[yOff+8*img.YStride:], img.YStride, blim, lim, hev, 2)
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
	for row := 0; row < rows; row++ {
		if err := applyNormalLoopFilterRow(img, row, cols, modes, frameType, lfi); err != nil {
			return err
		}
	}
	return nil
}

func applyNormalLoopFilterRow(img *common.Image, row int, cols int, modes []MacroblockMode, frameType common.FrameType, lfi *common.LoopFilterInfo) error {
	yRow := row * 16 * img.YStride
	uRow := row * 8 * img.UStride
	vRow := row * 8 * img.VStride
	for col := 0; col < cols; col++ {
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

func applySimpleLoopFilterGrid(img *common.Image, rows int, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo) error {
	for row := 0; row < rows; row++ {
		if err := applySimpleLoopFilterRow(img, row, cols, modes, lfi); err != nil {
			return err
		}
	}
	return nil
}

func applySimpleLoopFilterRow(img *common.Image, row int, cols int, modes []MacroblockMode, lfi *common.LoopFilterInfo) error {
	yRow := row * 16 * img.YStride
	for col := 0; col < cols; col++ {
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

func loopFilterFrameConfig(header LoopFilterHeader, segmentation SegmentationHeader) common.LoopFilterFrameConfig {
	cfg := common.LoopFilterFrameConfig{
		SegmentationEnabled: segmentation.Enabled,
		SegmentAbsDelta:     segmentation.AbsDelta,
		ModeRefDeltaEnabled: header.DeltaEnabled,
		RefDeltas:           header.RefDeltas,
		ModeDeltas:          header.ModeDeltas,
	}
	for segment := 0; segment < common.MaxMBSegments; segment++ {
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
		dsp.MBLoopFilterVerticalEdge(img.U[uOff-4:], img.UStride, mblim, lim, hev, 1)
		dsp.MBLoopFilterVerticalEdge(img.V[vOff-4:], img.VStride, mblim, lim, hev, 1)
	}
	if !skipLF {
		dsp.LoopFilterVerticalEdge(img.Y[yOff:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterVerticalEdge(img.Y[yOff+4:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterVerticalEdge(img.Y[yOff+8:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterVerticalEdge(img.U[uOff:], img.UStride, blim, lim, hev, 1)
		dsp.LoopFilterVerticalEdge(img.V[vOff:], img.VStride, blim, lim, hev, 1)
	}

	if row > 0 {
		dsp.MBLoopFilterHorizontalEdge(img.Y[yOff-4*img.YStride:], img.YStride, mblim, lim, hev, 2)
		dsp.MBLoopFilterHorizontalEdge(img.U[uOff-4*img.UStride:], img.UStride, mblim, lim, hev, 1)
		dsp.MBLoopFilterHorizontalEdge(img.V[vOff-4*img.VStride:], img.VStride, mblim, lim, hev, 1)
	}
	if !skipLF {
		dsp.LoopFilterHorizontalEdge(img.Y[yOff:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterHorizontalEdge(img.Y[yOff+4*img.YStride:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterHorizontalEdge(img.Y[yOff+8*img.YStride:], img.YStride, blim, lim, hev, 2)
		dsp.LoopFilterHorizontalEdge(img.U[uOff:], img.UStride, blim, lim, hev, 1)
		dsp.LoopFilterHorizontalEdge(img.V[vOff:], img.VStride, blim, lim, hev, 1)
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
