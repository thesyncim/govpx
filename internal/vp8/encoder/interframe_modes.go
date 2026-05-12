package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe mode
// packing.

func WriteReferenceFrameZeroMVModeGrid(w *BoolWriter, rows int, cols int, cfg *InterFrameStateConfig, refFrame common.MVReferenceFrame) error {
	if w == nil || cfg == nil || rows <= 0 || cols <= 0 || !cfg.MBNoCoeffSkip || !validSegmentationConfig(cfg.Segmentation) {
		return ErrInvalidPacketConfig
	}
	// LastFrame=1, AltRefFrame=3: refFrame must be in [Last, AltRef].
	// uint(refFrame - LastFrame) > uint(AltRefFrame - LastFrame) covers
	// IntraFrame (0) wrapping huge and any value above AltRefFrame.
	if uint(refFrame-common.LastFrame) > uint(common.AltRefFrame-common.LastFrame) {
		return ErrInvalidPacketConfig
	}
	writeSegmentID := cfg.Segmentation.Enabled && cfg.Segmentation.UpdateMap
	segmentProbs := segmentationTreeProbs(cfg.Segmentation)
	for row := range rows {
		for col := range cols {
			if writeSegmentID && !writeMacroblockSegmentID(w, &segmentProbs, 0) {
				if w.Err() != nil {
					return w.Err()
				}
				return ErrInvalidPacketConfig
			}
			w.WriteBool(1, cfg.ProbSkipFalse)
			w.WriteBool(1, cfg.ProbIntra)
			if !writeInterReferenceFrameWithProbs(w, cfg.ProbLast, cfg.ProbGolden, refFrame) {
				return ErrInvalidPacketConfig
			}
			counts := zeroMVInterModeCounts(row, col)
			w.WriteBool(0, tables.InterModeContexts[counts][0])
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteLastFrameZeroMVModeGridWithSkip(w *BoolWriter, rows int, cols int, cfg *InterFrameStateConfig, modes []InterFrameMacroblockMode) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || cfg == nil || len(modes) < required || !cfg.MBNoCoeffSkip {
		return ErrModeBufferTooSmall
	}
	if !validSegmentationConfig(cfg.Segmentation) {
		return ErrInvalidPacketConfig
	}
	writeSegmentID := cfg.Segmentation.Enabled && cfg.Segmentation.UpdateMap
	segmentProbs := segmentationTreeProbs(cfg.Segmentation)
	yModeProbs := interFrameYModeProbs(cfg)
	uvModeProbs := interFrameUVModeProbs(cfg)
	mvProbs := interFrameMVProbs(cfg)
	signBias := interFrameSignBias(cfg)
	probLast := cfg.ProbLast
	probGolden := cfg.ProbGolden
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if writeSegmentID && !writeMacroblockSegmentID(w, &segmentProbs, mode.SegmentID) {
				if w.Err() != nil {
					return w.Err()
				}
				return ErrInvalidPacketConfig
			}
			if mode.MBSkipCoeff {
				w.WriteBool(1, cfg.ProbSkipFalse)
			} else {
				w.WriteBool(0, cfg.ProbSkipFalse)
			}
			refFrame := interFrameReference(mode)
			if refFrame == common.IntraFrame {
				w.WriteBool(0, cfg.ProbIntra)
				if !WriteInterIntraMacroblockMode(w, mode, yModeProbs, uvModeProbs) {
					return ErrInvalidPacketConfig
				}
				continue
			}
			w.WriteBool(1, cfg.ProbIntra)
			if !writeInterReferenceFrameWithProbs(w, probLast, probGolden, refFrame) {
				return ErrInvalidPacketConfig
			}
			var above *InterFrameMacroblockMode
			var left *InterFrameMacroblockMode
			var aboveLeft *InterFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if row > 0 && col > 0 {
				aboveLeft = &modes[index-cols-1]
			}
			if !validInterFrameMacroblockModeAt(mode, above, left, aboveLeft, row, col, rows, cols, signBias) {
				return ErrInvalidPacketConfig
			}
			if !WriteInterPredictionMode(w, interModeCounts(above, left, aboveLeft, refFrame, signBias), mode.Mode) {
				return ErrInvalidPacketConfig
			}
			switch mode.Mode {
			case common.NewMV:
				best := interBestMotionVectorAt(above, left, aboveLeft, refFrame, row, col, rows, cols, signBias)
				delta := MotionVector{Row: mode.MV.Row - best.Row, Col: mode.MV.Col - best.Col}
				if err := WriteMotionVector(w, &mvProbs, delta); err != nil {
					return err
				}
			case common.SplitMV:
				best := interBestMotionVectorAt(above, left, aboveLeft, refFrame, row, col, rows, cols, signBias)
				if err := WriteSplitMotionVectors(w, &mvProbs, mode, left, above, best); err != nil {
					return err
				}
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

var interFrameYModeTokens = initInterFrameYModeTokens()

func WriteInterIntraMacroblockMode(w *BoolWriter, mode *InterFrameMacroblockMode, yModeProbs [tables.YModeProbCount]uint8, uvModeProbs [tables.UVModeProbCount]uint8) bool {
	if w == nil || mode == nil || !validInterIntraMacroblockMode(mode) {
		return false
	}
	yModeProbs = normalizeYModeProbabilityBase(yModeProbs)
	uvModeProbs = normalizeUVModeProbabilityBase(uvModeProbs)
	if !WriteTreeToken(w, tables.YModeTree[:], yModeProbs[:], interFrameYModeTokens[int(mode.Mode)]) {
		return false
	}
	if mode.Mode == common.BPred {
		for block := range 16 {
			if !WriteTreeToken(w, tables.BModeTree[:], tables.DefaultBModeProbs[:], bModeTokens[int(mode.BModes[block])]) {
				return false
			}
		}
	}
	return WriteTreeToken(w, tables.UVModeTree[:], uvModeProbs[:], keyFrameUVModeTokens[int(mode.UVMode)])
}

func WriteInterPredictionMode(w *BoolWriter, counts InterModeCounts, mode common.MBPredictionMode) bool {
	switch mode {
	case common.ZeroMV:
		w.WriteBool(0, tables.InterModeContexts[counts.Intra][0])
	case common.NearestMV:
		w.WriteBool(1, tables.InterModeContexts[counts.Intra][0])
		w.WriteBool(0, tables.InterModeContexts[counts.Nearest][1])
	case common.NearMV:
		w.WriteBool(1, tables.InterModeContexts[counts.Intra][0])
		w.WriteBool(1, tables.InterModeContexts[counts.Nearest][1])
		w.WriteBool(0, tables.InterModeContexts[counts.Near][2])
	case common.NewMV:
		w.WriteBool(1, tables.InterModeContexts[counts.Intra][0])
		w.WriteBool(1, tables.InterModeContexts[counts.Nearest][1])
		w.WriteBool(1, tables.InterModeContexts[counts.Near][2])
		w.WriteBool(0, tables.InterModeContexts[counts.Split][3])
	case common.SplitMV:
		w.WriteBool(1, tables.InterModeContexts[counts.Intra][0])
		w.WriteBool(1, tables.InterModeContexts[counts.Nearest][1])
		w.WriteBool(1, tables.InterModeContexts[counts.Near][2])
		w.WriteBool(1, tables.InterModeContexts[counts.Split][3])
	default:
		return false
	}
	return w.Err() == nil
}

func WriteSplitMotionVectors(w *BoolWriter, probs *[2][tables.MVPCount]uint8, mode *InterFrameMacroblockMode, left *InterFrameMacroblockMode, above *InterFrameMacroblockMode, best MotionVector) error {
	if w == nil || probs == nil || !validSplitMVModeWithContext(mode, left, above) {
		return ErrInvalidPacketConfig
	}
	if !writeMBSplit(w, int(mode.Partition)) {
		return ErrInvalidPacketConfig
	}
	partitions := int(tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := splitLeftMV(mode, left, block)
		aboveMV := splitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		bMode := mode.BModes[block]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return ErrInvalidPacketConfig
		}
		if err := writeSubMotionVector(w, probs, bMode, target, leftMV, aboveMV, best); err != nil {
			return err
		}
	}
	return w.Err()
}

func writeMBSplit(w *BoolWriter, partition int) bool {
	switch partition {
	case 3:
		w.WriteBool(0, tables.MBSplitProbs[0])
	case 2:
		w.WriteBool(1, tables.MBSplitProbs[0])
		w.WriteBool(0, tables.MBSplitProbs[1])
	case 0:
		w.WriteBool(1, tables.MBSplitProbs[0])
		w.WriteBool(1, tables.MBSplitProbs[1])
		w.WriteBool(0, tables.MBSplitProbs[2])
	case 1:
		w.WriteBool(1, tables.MBSplitProbs[0])
		w.WriteBool(1, tables.MBSplitProbs[1])
		w.WriteBool(1, tables.MBSplitProbs[2])
	default:
		return false
	}
	return w.Err() == nil
}

func writeSubMotionVector(w *BoolWriter, probs *[2][tables.MVPCount]uint8, mode common.BPredictionMode, target MotionVector, left MotionVector, above MotionVector, best MotionVector) error {
	subProbs := subMVRefProbs(left, above)
	if !writeSubMotionVectorReference(w, mode, subProbs) {
		return ErrInvalidPacketConfig
	}
	if mode != common.New4x4 {
		return w.Err()
	}
	delta := MotionVector{Row: target.Row - best.Row, Col: target.Col - best.Col}
	return WriteMotionVector(w, probs, delta)
}

func writeSubMotionVectorReference(w *BoolWriter, mode common.BPredictionMode, probs [3]uint8) bool {
	switch mode {
	case common.Left4x4:
		w.WriteBool(0, probs[0])
	case common.Above4x4:
		w.WriteBool(1, probs[0])
		w.WriteBool(0, probs[1])
	case common.Zero4x4:
		w.WriteBool(1, probs[0])
		w.WriteBool(1, probs[1])
		w.WriteBool(0, probs[2])
	case common.New4x4:
		w.WriteBool(1, probs[0])
		w.WriteBool(1, probs[1])
		w.WriteBool(1, probs[2])
	default:
		return false
	}
	return w.Err() == nil
}

func writeInterReferenceFrameWithProbs(w *BoolWriter, probLast uint8, probGolden uint8, refFrame common.MVReferenceFrame) bool {
	switch refFrame {
	case common.LastFrame:
		w.WriteBool(0, probLast)
	case common.GoldenFrame:
		w.WriteBool(1, probLast)
		w.WriteBool(0, probGolden)
	case common.AltRefFrame:
		w.WriteBool(1, probLast)
		w.WriteBool(1, probGolden)
	default:
		return false
	}
	return w.Err() == nil
}
