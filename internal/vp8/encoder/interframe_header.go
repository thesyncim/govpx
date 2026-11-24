package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe frame-header
// helpers.

func writeInterRefreshHeader(w *BoolWriter, cfg *InterFrameStateConfig) {
	writeBoolBit(w, cfg.RefreshGolden)
	writeBoolBit(w, cfg.RefreshAltRef)
	if !cfg.RefreshGolden {
		w.WriteLiteral(uint32(cfg.CopyBufferToGolden), 2)
	}
	if !cfg.RefreshAltRef {
		w.WriteLiteral(uint32(cfg.CopyBufferToAltRef), 2)
	}
	writeBoolBit(w, cfg.GoldenSignBias)
	writeBoolBit(w, cfg.AltRefSignBias)
	writeBoolBit(w, cfg.RefreshEntropyProbs)
	writeBoolBit(w, cfg.RefreshLast)
}

func writeInterModeHeader(w *BoolWriter, cfg *InterFrameStateConfig) error {
	writeBoolBit(w, cfg.MBNoCoeffSkip)
	if cfg.MBNoCoeffSkip {
		w.WriteLiteral(uint32(cfg.ProbSkipFalse), 8)
	}
	w.WriteLiteral(uint32(cfg.ProbIntra), 8)
	w.WriteLiteral(uint32(cfg.ProbLast), 8)
	w.WriteLiteral(uint32(cfg.ProbGolden), 8)
	if cfg.YModeUpdate {
		w.WriteBit(1)
		for _, prob := range cfg.YModeProbs {
			if prob == 0 {
				return ErrInvalidPacketConfig
			}
			w.WriteLiteral(uint32(prob), 8)
		}
	} else {
		w.WriteBit(0)
	}
	if cfg.UVModeUpdate {
		w.WriteBit(1)
		for _, prob := range cfg.UVModeProbs {
			if prob == 0 {
				return ErrInvalidPacketConfig
			}
			w.WriteLiteral(uint32(prob), 8)
		}
	} else {
		w.WriteBit(0)
	}
	for component := range 2 {
		for i := range tables.MVPCount {
			if cfg.MVUpdate[component][i] {
				encoded, ok := encodeMotionVectorProbabilityUpdate(cfg.MVProbs[component][i])
				if !ok {
					return ErrInvalidPacketConfig
				}
				w.WriteBool(1, tables.MVUpdateProbs[component][i])
				w.WriteLiteral(uint32(encoded), 7)
			} else {
				w.WriteBool(0, tables.MVUpdateProbs[component][i])
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func encodeMotionVectorProbabilityUpdate(prob uint8) (uint8, bool) {
	if prob == 1 {
		return 0, true
	}
	if prob >= 2 && prob <= 254 && prob&1 == 0 {
		return prob >> 1, true
	}
	return 0, false
}

func writeBoolBit(w *BoolWriter, value bool) {
	if value {
		w.WriteBit(1)
		return
	}
	w.WriteBit(0)
}

func zeroMVInterModeCounts(row int, col int) uint8 {
	var counts uint8
	if row > 0 {
		counts += 2
	}
	if col > 0 {
		counts += 2
	}
	if row > 0 && col > 0 {
		counts++
	}
	return counts
}

func validInterFrameStateConfig(cfg *InterFrameStateConfig) bool {
	return cfg != nil &&
		cfg.LoopFilterLevel <= 63 &&
		cfg.SharpnessLevel <= 7 &&
		cfg.TokenPartition >= common.OnePartition &&
		cfg.TokenPartition <= common.EightPartition &&
		cfg.BaseQIndex <= 127 &&
		validQuantDeltas(cfg.QuantDeltas) &&
		cfg.CopyBufferToGolden >= 0 &&
		cfg.CopyBufferToGolden <= 2 &&
		cfg.CopyBufferToAltRef >= 0 &&
		cfg.CopyBufferToAltRef <= 2 &&
		(!cfg.RefreshGolden || cfg.CopyBufferToGolden == 0) &&
		(!cfg.RefreshAltRef || cfg.CopyBufferToAltRef == 0) &&
		validSegmentationConfig(cfg.Segmentation)
}
