package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe state-header
// and simple LAST/ZEROMV mode packing.

type InterFrameStateConfig struct {
	InvisibleFrame        bool
	Segmentation          SegmentationConfig
	SimpleLoopFilter      bool
	LoopFilterLevel       uint8
	SharpnessLevel        uint8
	LFDeltaEnabled        bool
	LFDeltaUpdate         bool
	LFDeltaForceUpdateAll bool
	RefLFDeltas           [common.MaxRefLFDeltas]int8
	ModeLFDeltas          [common.MaxModeLFDeltas]int8
	RefLFDeltasBase       [common.MaxRefLFDeltas]int8
	ModeLFDeltasBase      [common.MaxModeLFDeltas]int8

	TokenPartition common.TokenPartition
	BaseQIndex     uint8
	QuantDeltas    common.QuantDeltas

	RefreshLast   bool
	RefreshGolden bool
	RefreshAltRef bool

	CopyBufferToGolden int
	CopyBufferToAltRef int

	GoldenSignBias bool
	AltRefSignBias bool

	RefreshEntropyProbs bool

	// IndependentContexts mirrors libvpx's
	// VPX_ERROR_RESILIENT_PARTITIONS branch in
	// vp8/encoder/bitstream.c independent_coef_context_savings /
	// vp8_update_coef_probs. When true, coefficient probability updates
	// are computed from PREV_COEF_CONTEXTS-summed counts and applied
	// uniformly across all k contexts so a lost partition cannot
	// corrupt the per-context prob tables.
	IndependentContexts bool

	CoefficientProbs CoefficientProbabilityUpdates

	MBNoCoeffSkip bool
	ProbSkipFalse uint8

	ProbIntra  uint8
	ProbLast   uint8
	ProbGolden uint8

	YModeProbs   [tables.YModeProbCount]uint8
	YModeBase    [tables.YModeProbCount]uint8
	YModeUpdate  bool
	UVModeProbs  [tables.UVModeProbCount]uint8
	UVModeBase   [tables.UVModeProbCount]uint8
	UVModeUpdate bool
	BModeBase    [tables.BModeProbCount]uint8

	MVProbs       [2][tables.MVPCount]uint8
	MVBase        [2][tables.MVPCount]uint8
	MVUpdate      [2][tables.MVPCount]bool
	MVUpdateCount int
}

const mvEventCount = mvComponentMax*2 + 1

type motionVectorComponentEvents [mvEventCount]int
type motionVectorEventCounts [2]motionVectorComponentEvents

func DefaultInterFrameStateConfig(baseQIndex uint8) InterFrameStateConfig {
	return InterFrameStateConfig{
		TokenPartition: common.OnePartition,
		BaseQIndex:     baseQIndex,

		RefreshLast: true,

		MBNoCoeffSkip: true,
		ProbSkipFalse: 128,

		ProbIntra:  128,
		ProbLast:   128,
		ProbGolden: 128,

		YModeProbs:  tables.DefaultYModeProbs,
		YModeBase:   tables.DefaultYModeProbs,
		UVModeProbs: tables.DefaultUVModeProbs,
		UVModeBase:  tables.DefaultUVModeProbs,
		BModeBase:   tables.DefaultBModeProbs,

		MVProbs: tables.DefaultMVContext,
		MVBase:  tables.DefaultMVContext,
	}
}

func WriteInterFrameStateHeader(w *BoolWriter, cfg *InterFrameStateConfig) error {
	if w == nil || !validInterFrameStateConfig(cfg) {
		return ErrInvalidPacketConfig
	}

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
	writeLoopFilterDeltas(w, cfg.LFDeltaEnabled, cfg.LFDeltaUpdate, cfg.LFDeltaForceUpdateAll, cfg.RefLFDeltas, cfg.ModeLFDeltas, cfg.RefLFDeltasBase, cfg.ModeLFDeltasBase)
	w.WriteLiteral(uint32(cfg.TokenPartition), 2)
	w.WriteLiteral(uint32(cfg.BaseQIndex), 7)
	writeQuantDeltas(w, cfg.QuantDeltas)

	writeInterRefreshHeader(w, cfg)
	if err := WriteCoefficientProbabilityUpdates(w, &cfg.CoefficientProbs); err != nil {
		return err
	}
	if err := writeInterModeHeader(w, cfg); err != nil {
		return err
	}

	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteZeroInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig) (int, error) {
	return WriteZeroReferenceInterFrame(dst, width, height, cfg, common.LastFrame)
}

func WriteZeroReferenceInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig, refFrame common.MVReferenceFrame) (int, error) {
	if len(dst) < FrameTagSize {
		return 0, ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff {
		return 0, ErrInvalidPacketConfig
	}
	partitionCount, ok := tokenPartitionCount(cfg.TokenPartition)
	if !ok || !cfg.MBNoCoeffSkip {
		return 0, ErrInvalidPacketConfig
	}
	if refFrame != common.LastFrame && refFrame != common.GoldenFrame && refFrame != common.AltRefFrame {
		return 0, ErrInvalidPacketConfig
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	adaptZeroReferenceInterFrameModeProbabilities(rows, cols, refFrame, &cfg)

	firstStart := FrameTagSize
	first := BoolWriter{}
	first.Init(dst[firstStart:])
	if err := WriteInterFrameStateHeader(&first, &cfg); err != nil {
		return 0, err
	}
	if err := WriteReferenceFrameZeroMVModeGrid(&first, rows, cols, &cfg, refFrame); err != nil {
		return 0, err
	}
	first.Finish()
	if err := first.Err(); err != nil {
		return 0, err
	}
	firstSize := first.BytesWritten()
	if firstSize > MaxFirstPartitionSize {
		return 0, ErrInvalidPacketConfig
	}

	tokenStart := firstStart + firstSize
	n := 0
	if partitionCount == 1 {
		tokens := BoolWriter{}
		tokens.Init(dst[tokenStart:])
		tokens.Finish()
		if err := tokens.Err(); err != nil {
			return 0, err
		}
		n = tokenStart + tokens.BytesWritten()
	} else {
		var (
			writers    [8]BoolWriter
			partitions int
			scratch    *PartitionScratch
			err        error
		)
		scratch, partitions, err = preparePartitionWriters(nil, &writers, dst, tokenStart, cfg.TokenPartition)
		if err != nil {
			return 0, err
		}
		n, err = finalizePartitionedTokenPayload(scratch, &writers, dst, tokenStart, partitions)
		if err != nil {
			return 0, err
		}
	}

	if err := PutFrameTag(dst, false, 0, !cfg.InvisibleFrame, firstSize); err != nil {
		return 0, err
	}
	return n, nil
}
