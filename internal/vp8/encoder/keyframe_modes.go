package encoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c keyframe mode writers.
// Block-mode context derivation mirrors vp8/common/findnearmv.h.

var ErrModeBufferTooSmall = errors.New("libgopx: VP8 encoder mode buffer too small")

type KeyFrameMacroblockMode struct {
	YMode  common.MBPredictionMode
	UVMode common.MBPredictionMode
	BModes [16]common.BPredictionMode
}

var keyFrameYModeTokens = initKeyFrameYModeTokens()
var keyFrameUVModeTokens = initKeyFrameUVModeTokens()
var bModeTokens = initBModeTokens()

func WriteKeyFrameMacroblockMode(w *BoolWriter, above *KeyFrameMacroblockMode, left *KeyFrameMacroblockMode, mode *KeyFrameMacroblockMode) bool {
	if w == nil || mode == nil || !validKeyFrameMacroblockMode(mode) {
		return false
	}
	yMode := int(mode.YMode)
	if !WriteTreeToken(w, tables.KeyFrameYModeTree[:], tables.KeyFrameYModeProbs[:], keyFrameYModeTokens[yMode]) {
		return false
	}
	if mode.YMode == common.BPred {
		for block := 0; block < 16; block++ {
			a := keyFrameAboveBlockMode(mode, above, block)
			l := keyFrameLeftBlockMode(mode, left, block)
			probs := tables.KeyFrameBModeProbs[int(a)][int(l)][:]
			if !WriteTreeToken(w, tables.BModeTree[:], probs, bModeTokens[int(mode.BModes[block])]) {
				return false
			}
		}
	}
	return WriteTreeToken(w, tables.UVModeTree[:], tables.KeyFrameUVModeProbs[:], keyFrameUVModeTokens[int(mode.UVMode)])
}

func WriteKeyFrameModeGrid(w *BoolWriter, rows int, cols int, modes []KeyFrameMacroblockMode) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || len(modes) < required {
		return ErrModeBufferTooSmall
	}

	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			var above *KeyFrameMacroblockMode
			var left *KeyFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if !WriteKeyFrameMacroblockMode(w, above, left, &modes[index]) {
				if w.Err() != nil {
					return w.Err()
				}
				return ErrInvalidPacketConfig
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func validKeyFrameMacroblockMode(mode *KeyFrameMacroblockMode) bool {
	if mode.YMode < common.DCPred || mode.YMode > common.BPred || mode.UVMode < common.DCPred || mode.UVMode > common.TMPred {
		return false
	}
	if mode.YMode != common.BPred {
		return true
	}
	for _, bMode := range mode.BModes {
		if bMode < common.BDCPred || bMode > common.BHUPred {
			return false
		}
	}
	return true
}

func keyFrameLeftBlockMode(cur *KeyFrameMacroblockMode, left *KeyFrameMacroblockMode, block int) common.BPredictionMode {
	if block&3 == 0 {
		if left == nil {
			return common.BDCPred
		}
		if left.YMode == common.BPred {
			return left.BModes[block+3]
		}
		return blockModeFromMacroblockMode(left.YMode)
	}
	return cur.BModes[block-1]
}

func keyFrameAboveBlockMode(cur *KeyFrameMacroblockMode, above *KeyFrameMacroblockMode, block int) common.BPredictionMode {
	if block>>2 == 0 {
		if above == nil {
			return common.BDCPred
		}
		if above.YMode == common.BPred {
			return above.BModes[block+12]
		}
		return blockModeFromMacroblockMode(above.YMode)
	}
	return cur.BModes[block-4]
}

func blockModeFromMacroblockMode(mode common.MBPredictionMode) common.BPredictionMode {
	switch mode {
	case common.VPred:
		return common.BVEPred
	case common.HPred:
		return common.BHEPred
	case common.TMPred:
		return common.BTMPred
	default:
		return common.BDCPred
	}
}

func initKeyFrameYModeTokens() [5]TreeToken {
	var out [5]TreeToken
	for i := range out {
		BuildTreeToken(tables.KeyFrameYModeTree[:], i, &out[i])
	}
	return out
}

func initKeyFrameUVModeTokens() [4]TreeToken {
	var out [4]TreeToken
	for i := range out {
		BuildTreeToken(tables.UVModeTree[:], i, &out[i])
	}
	return out
}

func initBModeTokens() [10]TreeToken {
	var out [10]TreeToken
	for i := range out {
		BuildTreeToken(tables.BModeTree[:], i, &out[i])
	}
	return out
}
