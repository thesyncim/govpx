package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
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
	LFDeltaEnabled   bool
	LFDeltaUpdate    bool
	RefLFDeltas      [common.MaxRefLFDeltas]int8
	ModeLFDeltas     [common.MaxModeLFDeltas]int8

	TokenPartition common.TokenPartition
	BaseQIndex     uint8
	QuantDeltas    common.QuantDeltas

	RefreshEntropyProbs bool

	// IndependentContexts mirrors libvpx's
	// VPX_ERROR_RESILIENT_PARTITIONS branch in
	// vp8/encoder/bitstream.c independent_coef_context_savings /
	// vp8_update_coef_probs. See InterFrameStateConfig.IndependentContexts.
	IndependentContexts bool

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
	writeLoopFilterDeltas(w, cfg.LFDeltaEnabled, cfg.LFDeltaUpdate, cfg.RefLFDeltas, cfg.ModeLFDeltas)
	w.WriteLiteral(uint32(cfg.TokenPartition), 2)
	w.WriteLiteral(uint32(cfg.BaseQIndex), 7)
	writeQuantDeltas(w, cfg.QuantDeltas)
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

func writeQuantDeltas(w *BoolWriter, deltas common.QuantDeltas) {
	writeQuantDelta(w, deltas.Y1DC)
	writeQuantDelta(w, deltas.Y2DC)
	writeQuantDelta(w, deltas.Y2AC)
	writeQuantDelta(w, deltas.UVDC)
	writeQuantDelta(w, deltas.UVAC)
}

func writeQuantDelta(w *BoolWriter, delta int) {
	if delta == 0 {
		w.WriteBit(0)
		return
	}
	w.WriteBit(1)
	if delta < 0 {
		w.WriteLiteral(uint32(-delta), 4)
		w.WriteBit(1)
		return
	}
	w.WriteLiteral(uint32(delta), 4)
	w.WriteBit(0)
}

func writeLoopFilterDeltas(w *BoolWriter, enabled bool, update bool, refDeltas [common.MaxRefLFDeltas]int8, modeDeltas [common.MaxModeLFDeltas]int8) {
	if !enabled {
		w.WriteBit(0)
		return
	}
	w.WriteBit(1)
	if !update {
		w.WriteBit(0)
		return
	}
	w.WriteBit(1)
	for _, delta := range refDeltas {
		writeLoopFilterDelta(w, delta)
	}
	for _, delta := range modeDeltas {
		writeLoopFilterDelta(w, delta)
	}
}

func writeLoopFilterDelta(w *BoolWriter, delta int8) {
	if delta == 0 {
		w.WriteBit(0)
		return
	}
	w.WriteBit(1)
	value := delta
	if value < 0 {
		value = -value
	}
	w.WriteLiteral(uint32(value)&0x3f, 6)
	if delta < 0 {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}
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
		validQuantDeltas(cfg.QuantDeltas) &&
		validSegmentationConfig(cfg.Segmentation)
}

func validQuantDeltas(deltas common.QuantDeltas) bool {
	return validQuantDelta(deltas.Y1DC) &&
		validQuantDelta(deltas.Y2DC) &&
		validQuantDelta(deltas.Y2AC) &&
		validQuantDelta(deltas.UVDC) &&
		validQuantDelta(deltas.UVAC)
}

func validQuantDelta(delta int) bool {
	return delta >= -15 && delta <= 15
}
