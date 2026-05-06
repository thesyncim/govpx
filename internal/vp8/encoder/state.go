package encoder

import (
	"github.com/thesyncim/gopvx/internal/vp8/common"
	"github.com/thesyncim/gopvx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c keyframe state-header
// packing.

type KeyFrameStateConfig struct {
	ClampType common.ClampType

	InvisibleFrame   bool
	Segmentation     SegmentationConfig
	SimpleLoopFilter bool
	LoopFilterLevel  uint8
	SharpnessLevel   uint8

	TokenPartition common.TokenPartition
	BaseQIndex     uint8

	RefreshEntropyProbs bool

	CoefficientProbs CoefficientProbabilityUpdates

	MBNoCoeffSkip bool
	ProbSkipFalse uint8
}

func WriteKeyFrameStateHeader(w *BoolWriter, cfg KeyFrameStateConfig) error {
	if w == nil || !validKeyFrameStateConfig(cfg) {
		return ErrInvalidPacketConfig
	}

	w.WriteBit(0)
	w.WriteBit(uint8(cfg.ClampType))
	if err := writeSegmentationHeader(w, cfg.Segmentation); err != nil {
		return err
	}
	if cfg.SimpleLoopFilter {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}
	w.WriteLiteral(uint32(cfg.LoopFilterLevel), 6)
	w.WriteLiteral(uint32(cfg.SharpnessLevel), 3)
	w.WriteBit(0)
	w.WriteLiteral(uint32(cfg.TokenPartition), 2)
	w.WriteLiteral(uint32(cfg.BaseQIndex), 7)
	for i := 0; i < 5; i++ {
		w.WriteBit(0)
	}
	if cfg.RefreshEntropyProbs {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}

	if err := WriteCoefficientProbabilityUpdates(w, &cfg.CoefficientProbs); err != nil {
		return err
	}

	if cfg.MBNoCoeffSkip {
		w.WriteBit(1)
		w.WriteLiteral(uint32(cfg.ProbSkipFalse), 8)
	} else {
		w.WriteBit(0)
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteNoCoefficientProbabilityUpdates(w *BoolWriter) {
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
					w.WriteBool(0, tables.CoefUpdateProbs[block][band][ctx][node])
				}
			}
		}
	}
}

func validKeyFrameStateConfig(cfg KeyFrameStateConfig) bool {
	return cfg.ClampType >= common.ReconClampRequired &&
		cfg.ClampType <= common.ReconClampNotRequired &&
		cfg.LoopFilterLevel <= 63 &&
		cfg.SharpnessLevel <= 7 &&
		cfg.TokenPartition >= common.OnePartition &&
		cfg.TokenPartition <= common.EightPartition &&
		cfg.BaseQIndex <= 127 &&
		validSegmentationConfig(cfg.Segmentation)
}
