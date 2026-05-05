package decoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
)

// Ported from libvpx v1.16.0 vp8/common/vp8_loopfilter.c frame traversal and
// vp8/common/loopfilter_filters.c filter wrappers.

var ErrLoopFilterBufferTooSmall = errors.New("libgopx: VP8 loop filter buffer too small")

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

	for row := 0; row < rows; row++ {
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
			if header.Type == SimpleLoopFilter {
				applySimpleLoopFilterMB(img, row, col, yOff, mode, level, lfi)
			} else {
				applyNormalLoopFilterMB(img, row, col, yOff, uOff, vOff, mode, frameType, level, lfi)
			}
		}
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
