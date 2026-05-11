package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe coefficient
// token packing.

func WriteInterCoefficientTokenGrid(w *BoolWriter, rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || probs == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return ErrModeBufferTooSmall
	}

	for col := range cols {
		above[col] = TokenContextPlanes{}
	}
	for row := range rows {
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			is4x4 := interModeUses4x4Tokens(modes[index].Mode)
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left, is4x4)
				continue
			}
			if !validInterCoefficientTokenMode(&modes[index]) {
				return ErrInvalidPacketConfig
			}
			if err := writeCoefficientMacroblockTokensWithEOBs(w, probs, is4x4, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteInterCoefficientTokenGridPartitioned(writers *[8]BoolWriter, partitions int, rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if writers == nil || probs == nil || len(modes) < required || len(coeffs) < required || len(above) < cols || partitions != 2 && partitions != 4 && partitions != 8 {
		return ErrModeBufferTooSmall
	}

	for col := range cols {
		above[col] = TokenContextPlanes{}
	}
	for row := range rows {
		w := &writers[row&(partitions-1)]
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			is4x4 := interModeUses4x4Tokens(modes[index].Mode)
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left, is4x4)
				continue
			}
			if !validInterCoefficientTokenMode(&modes[index]) {
				return ErrInvalidPacketConfig
			}
			if err := writeCoefficientMacroblockTokensWithEOBs(w, probs, is4x4, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func resetTokenContext(above *TokenContextPlanes, left *TokenContextPlanes, is4x4 bool) {
	ResetTokenContextPlanes(above, left, is4x4)
}

func validInterCoefficientTokenMode(mode *InterFrameMacroblockMode) bool {
	if mode == nil {
		return false
	}
	refFrame := interFrameReference(mode)
	if refFrame == common.IntraFrame {
		return validInterIntraMacroblockMode(mode)
	}
	switch refFrame {
	case common.LastFrame, common.GoldenFrame, common.AltRefFrame:
	default:
		return false
	}
	return isWholeInterMacroblockMode(mode.Mode) || validSplitMVMode(mode)
}

func isWholeInterMacroblockMode(mode common.MBPredictionMode) bool {
	switch mode {
	case common.ZeroMV, common.NearestMV, common.NearMV, common.NewMV:
		return true
	default:
		return false
	}
}

func interModeUses4x4Tokens(mode common.MBPredictionMode) bool {
	return mode == common.BPred || mode == common.SplitMV
}
