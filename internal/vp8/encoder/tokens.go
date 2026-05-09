package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/tokenize.c and
// vp8/encoder/bitstream.c zero-coefficient token packing behavior.

func WriteZeroMacroblockTokens(w *BoolWriter, probs *tables.CoefficientProbs, is4x4 bool) error {
	if w == nil || probs == nil {
		return ErrInvalidPacketConfig
	}

	if !is4x4 {
		writeImmediateEOB(w, probs, 1, 0, 0)
	}

	yBlockType := 3
	skipDC := 0
	if !is4x4 {
		yBlockType = 0
		skipDC = 1
	}
	for range 16 {
		writeImmediateEOB(w, probs, yBlockType, skipDC, 0)
	}
	for block := 16; block < 24; block++ {
		writeImmediateEOB(w, probs, 2, 0, 0)
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteZeroTokenGrid(w *BoolWriter, rows int, cols int, modes []KeyFrameMacroblockMode, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || probs == nil || len(modes) < required {
		return ErrModeBufferTooSmall
	}

	for row := range rows {
		for col := range cols {
			mode := &modes[row*cols+col]
			if !validKeyFrameMacroblockMode(mode) {
				return ErrInvalidPacketConfig
			}
			if err := WriteZeroMacroblockTokens(w, probs, mode.YMode == common.BPred); err != nil {
				return err
			}
		}
	}
	return nil
}

func WriteZeroTokenGridPartitioned(writers *[8]BoolWriter, partitions int, rows int, cols int, modes []KeyFrameMacroblockMode, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if writers == nil || probs == nil || len(modes) < required || partitions != 2 && partitions != 4 && partitions != 8 {
		return ErrModeBufferTooSmall
	}

	for row := range rows {
		w := &writers[row&(partitions-1)]
		for col := range cols {
			mode := &modes[row*cols+col]
			if !validKeyFrameMacroblockMode(mode) {
				return ErrInvalidPacketConfig
			}
			if err := WriteZeroMacroblockTokens(w, probs, mode.YMode == common.BPred); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeImmediateEOB(w *BoolWriter, probs *tables.CoefficientProbs, blockType int, coefBand int, ctx int) {
	w.WriteBool(0, (*probs)[blockType][coefBand][ctx][0])
}
