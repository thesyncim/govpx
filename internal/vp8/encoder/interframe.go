package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe state-header
// and simple LAST/ZEROMV mode packing.

type InterFrameStateConfig struct {
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

		MVProbs: tables.DefaultMVContext,
		MVBase:  tables.DefaultMVContext,
	}
}

func WriteInterFrameStateHeader(w *BoolWriter, cfg InterFrameStateConfig) error {
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
	writeLoopFilterDeltas(w, cfg.LFDeltaEnabled, cfg.LFDeltaUpdate, cfg.RefLFDeltas, cfg.ModeLFDeltas)
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
	if err := WriteInterFrameStateHeader(&first, cfg); err != nil {
		return 0, err
	}
	if err := WriteReferenceFrameZeroMVModeGrid(&first, rows, cols, cfg, refFrame); err != nil {
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

type InterFrameMacroblockMode struct {
	SegmentID   uint8
	MBSkipCoeff bool
	RefFrame    common.MVReferenceFrame
	Mode        common.MBPredictionMode
	UVMode      common.MBPredictionMode
	BModes      [16]common.BPredictionMode
	MV          MotionVector
	Partition   uint8
	BlockMV     [16]MotionVector

	ImprovedMVStart        bool
	ImprovedMVNearSADIndex int8
	ImprovedMVSR           int8
	ImprovedMVPredictor    MotionVector
}

func WriteCoefficientInterFrame(dst []byte, width int, height int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes) (int, error) {
	n, _, _, _, _, err := WriteCoefficientInterFrameWithProbabilityBase(dst, width, height, cfg, modes, coeffs, above, &tables.DefaultCoefProbs, tables.DefaultYModeProbs, tables.DefaultUVModeProbs, tables.DefaultMVContext)
	return n, err
}

func WriteCoefficientInterFrameWithProbabilityBase(dst []byte, width int, height int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, coefBase *tables.CoefficientProbs, yModeBase [tables.YModeProbCount]uint8, uvModeBase [tables.UVModeProbCount]uint8, mvBase [2][tables.MVPCount]uint8) (int, tables.CoefficientProbs, [tables.YModeProbCount]uint8, [tables.UVModeProbCount]uint8, [2][tables.MVPCount]uint8, error) {
	return WriteCoefficientInterFrameWithProbabilityBaseScratch(dst, width, height, cfg, modes, coeffs, above, coefBase, yModeBase, uvModeBase, mvBase, nil)
}

// WriteCoefficientInterFrameWithProbabilityBaseScratch is the
// allocation-pooled variant. Callers in the encoder hot path pass a
// long-lived *PartitionScratch so the multi-token-partition path reuses
// its per-partition byte buffers across encodes; passing nil falls back to
// allocating per call (preserves the legacy public API behaviour).
func WriteCoefficientInterFrameWithProbabilityBaseScratch(dst []byte, width int, height int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, coefBase *tables.CoefficientProbs, yModeBase [tables.YModeProbCount]uint8, uvModeBase [tables.UVModeProbCount]uint8, mvBase [2][tables.MVPCount]uint8, partScratch *PartitionScratch) (int, tables.CoefficientProbs, [tables.YModeProbCount]uint8, [tables.UVModeProbCount]uint8, [2][tables.MVPCount]uint8, error) {
	n, frameCoefProbs, frameYModeProbs, frameUVModeProbs, frameMVProbs, _, err := WriteCoefficientInterFrameWithProbabilityBaseScratchAndSavings(dst, width, height, cfg, modes, coeffs, above, coefBase, yModeBase, uvModeBase, mvBase, partScratch)
	return n, frameCoefProbs, frameYModeProbs, frameUVModeProbs, frameMVProbs, err
}

func WriteCoefficientInterFrameWithProbabilityBaseScratchAndSavings(dst []byte, width int, height int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, coefBase *tables.CoefficientProbs, yModeBase [tables.YModeProbCount]uint8, uvModeBase [tables.UVModeProbCount]uint8, mvBase [2][tables.MVPCount]uint8, partScratch *PartitionScratch) (int, tables.CoefficientProbs, [tables.YModeProbCount]uint8, [tables.UVModeProbCount]uint8, [2][tables.MVPCount]uint8, int, error) {
	if len(dst) < FrameTagSize {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, ErrInvalidPacketConfig
	}
	partitionCount, ok := tokenPartitionCount(cfg.TokenPartition)
	if !ok || !cfg.MBNoCoeffSkip {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, ErrInvalidPacketConfig
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	required := rows * cols
	if coefBase == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, ErrModeBufferTooSmall
	}
	var (
		frameCoefProbs tables.CoefficientProbs
		coefUpdates    CoefficientProbabilityUpdates
		err            error
	)
	if cfg.IndependentContexts {
		frameCoefProbs, coefUpdates, err = BuildInterCoefficientProbabilityUpdatesIndependent(rows, cols, modes, coeffs, above, coefBase, false)
	} else {
		frameCoefProbs, coefUpdates, err = BuildInterCoefficientProbabilityUpdates(rows, cols, modes, coeffs, above, coefBase)
	}
	if err != nil {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
	}
	cfg.CoefficientProbs = coefUpdates
	frameYModeProbs, frameUVModeProbs, frameMVProbs, err := adaptInterFrameModeProbabilitiesWithBases(rows, cols, modes, yModeBase, uvModeBase, mvBase, &cfg)
	if err != nil {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
	}

	firstStart := FrameTagSize
	first := BoolWriter{}
	first.Init(dst[firstStart:])
	if err := WriteInterFrameStateHeader(&first, cfg); err != nil {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
	}
	if err := WriteLastFrameZeroMVModeGridWithSkip(&first, rows, cols, cfg, modes); err != nil {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
	}
	first.Finish()
	if err := first.Err(); err != nil {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
	}
	firstSize := first.BytesWritten()
	if firstSize > MaxFirstPartitionSize {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, ErrInvalidPacketConfig
	}

	tokenStart := firstStart + firstSize
	n := 0
	if partitionCount == 1 {
		tokens := BoolWriter{}
		tokens.Init(dst[tokenStart:])
		if err := WriteInterCoefficientTokenGrid(&tokens, rows, cols, modes, coeffs, above, &frameCoefProbs); err != nil {
			return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
		}
		tokens.Finish()
		if err := tokens.Err(); err != nil {
			return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
		}
		n = tokenStart + tokens.BytesWritten()
	} else {
		var (
			writers    [8]BoolWriter
			partitions int
			resolved   *PartitionScratch
		)
		resolved, partitions, err = preparePartitionWriters(partScratch, &writers, dst, tokenStart, cfg.TokenPartition)
		if err != nil {
			return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
		}
		if err := WriteInterCoefficientTokenGridPartitioned(&writers, partitions, rows, cols, modes, coeffs, above, &frameCoefProbs); err != nil {
			return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
		}
		n, err = finalizePartitionedTokenPayload(resolved, &writers, dst, tokenStart, partitions)
		if err != nil {
			return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
		}
	}

	if err := PutFrameTag(dst, false, 0, !cfg.InvisibleFrame, firstSize); err != nil {
		return 0, tables.CoefficientProbs{}, [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, 0, err
	}
	return n, frameCoefProbs, frameYModeProbs, frameUVModeProbs, frameMVProbs, coefUpdates.SavingsBits, nil
}

func WriteLastFrameZeroMVModeGrid(w *BoolWriter, rows int, cols int, cfg InterFrameStateConfig) error {
	return WriteReferenceFrameZeroMVModeGrid(w, rows, cols, cfg, common.LastFrame)
}

func WriteReferenceFrameZeroMVModeGrid(w *BoolWriter, rows int, cols int, cfg InterFrameStateConfig, refFrame common.MVReferenceFrame) error {
	if w == nil || rows <= 0 || cols <= 0 || !cfg.MBNoCoeffSkip || !validSegmentationConfig(cfg.Segmentation) {
		return ErrInvalidPacketConfig
	}
	if refFrame != common.LastFrame && refFrame != common.GoldenFrame && refFrame != common.AltRefFrame {
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
			if !WriteInterReferenceFrame(w, cfg, refFrame) {
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

func WriteLastFrameZeroMVModeGridWithSkip(w *BoolWriter, rows int, cols int, cfg InterFrameStateConfig, modes []InterFrameMacroblockMode) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || len(modes) < required || !cfg.MBNoCoeffSkip {
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
			if !WriteInterReferenceFrame(w, cfg, refFrame) {
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
	partitions := int(tables.MBSplitCount[mode.Partition])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition][subset])
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

func WriteInterReferenceFrame(w *BoolWriter, cfg InterFrameStateConfig, refFrame common.MVReferenceFrame) bool {
	switch refFrame {
	case common.LastFrame:
		w.WriteBool(0, cfg.ProbLast)
	case common.GoldenFrame:
		w.WriteBool(1, cfg.ProbLast)
		w.WriteBool(0, cfg.ProbGolden)
	case common.AltRefFrame:
		w.WriteBool(1, cfg.ProbLast)
		w.WriteBool(1, cfg.ProbGolden)
	default:
		return false
	}
	return w.Err() == nil
}

func adaptZeroReferenceInterFrameModeProbabilities(rows int, cols int, refFrame common.MVReferenceFrame, cfg *InterFrameStateConfig) {
	blocks := rows * cols
	if blocks <= 0 || cfg == nil {
		return
	}
	var skipCounts [2]int
	var intraCounts [2]int
	var lastCounts [2]int
	var goldenCounts [2]int
	skipCounts[1] = blocks
	intraCounts[1] = blocks
	switch refFrame {
	case common.LastFrame:
		lastCounts[0] = blocks
	case common.GoldenFrame:
		lastCounts[1] = blocks
		goldenCounts[0] = blocks
	case common.AltRefFrame:
		lastCounts[1] = blocks
		goldenCounts[1] = blocks
	default:
		return
	}
	cfg.ProbSkipFalse = interFrameSkipFalseProbability(skipCounts, cfg.ProbSkipFalse)
	cfg.ProbIntra = interFrameRefProbability(intraCounts, cfg.ProbIntra)
	cfg.ProbLast = interFrameRefProbability(lastCounts, cfg.ProbLast)
	cfg.ProbGolden = interFrameRefProbability(goldenCounts, cfg.ProbGolden)
}

func adaptInterFrameModeProbabilities(rows int, cols int, modes []InterFrameMacroblockMode, cfg *InterFrameStateConfig) error {
	_, err := adaptInterFrameModeProbabilitiesWithMVBase(rows, cols, modes, tables.DefaultMVContext, cfg)
	return err
}

func adaptInterFrameModeProbabilitiesWithMVBase(rows int, cols int, modes []InterFrameMacroblockMode, mvBase [2][tables.MVPCount]uint8, cfg *InterFrameStateConfig) ([2][tables.MVPCount]uint8, error) {
	_, _, frameMVProbs, err := adaptInterFrameModeProbabilitiesWithBases(rows, cols, modes, tables.DefaultYModeProbs, tables.DefaultUVModeProbs, mvBase, cfg)
	return frameMVProbs, err
}

func adaptInterFrameModeProbabilitiesWithBases(rows int, cols int, modes []InterFrameMacroblockMode, yModeBase [tables.YModeProbCount]uint8, uvModeBase [tables.UVModeProbCount]uint8, mvBase [2][tables.MVPCount]uint8, cfg *InterFrameStateConfig) ([tables.YModeProbCount]uint8, [tables.UVModeProbCount]uint8, [2][tables.MVPCount]uint8, error) {
	if rows < 0 || cols < 0 {
		return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrModeBufferTooSmall
	}
	required := rows * cols
	if cfg == nil || len(modes) < required {
		return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrModeBufferTooSmall
	}
	var skipCounts [2]int
	var intraCounts [2]int
	var lastCounts [2]int
	var goldenCounts [2]int
	var yModeCounts [tables.YModeProbCount][2]int
	var uvModeCounts [tables.UVModeProbCount][2]int
	var mvEvents motionVectorEventCounts
	signBias := interFrameSignBias(*cfg)
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if mode.MBSkipCoeff {
				skipCounts[1]++
			} else {
				skipCounts[0]++
			}
			refFrame := interFrameReference(mode)
			if refFrame == common.IntraFrame {
				if !validInterIntraMacroblockMode(mode) {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
				}
				intraCounts[0]++
				if !countTreeTokenBranches(yModeCounts[:], tables.YModeTree[:], interFrameYModeTokens[int(mode.Mode)]) {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
				}
				if !countTreeTokenBranches(uvModeCounts[:], tables.UVModeTree[:], keyFrameUVModeTokens[int(mode.UVMode)]) {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
				}
				continue
			}
			intraCounts[1]++
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
				return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
			}
			switch refFrame {
			case common.LastFrame:
				lastCounts[0]++
			case common.GoldenFrame:
				lastCounts[1]++
				goldenCounts[0]++
			case common.AltRefFrame:
				lastCounts[1]++
				goldenCounts[1]++
			default:
				return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
			}
			switch mode.Mode {
			case common.NewMV:
				best := interBestMotionVectorAt(above, left, aboveLeft, refFrame, row, col, rows, cols, signBias)
				delta := MotionVector{Row: mode.MV.Row - best.Row, Col: mode.MV.Col - best.Col}
				if err := countMotionVectorEvents(&mvEvents, delta); err != nil {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, err
				}
			case common.SplitMV:
				best := interBestMotionVectorAt(above, left, aboveLeft, refFrame, row, col, rows, cols, signBias)
				if err := countSplitMotionVectorEvents(&mvEvents, mode, left, above, best); err != nil {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, err
				}
			}
		}
	}
	cfg.ProbSkipFalse = interFrameSkipFalseProbability(skipCounts, cfg.ProbSkipFalse)
	cfg.ProbIntra = interFrameRefProbability(intraCounts, cfg.ProbIntra)
	cfg.ProbLast = interFrameRefProbability(lastCounts, cfg.ProbLast)
	cfg.ProbGolden = interFrameRefProbability(goldenCounts, cfg.ProbGolden)
	frameYModeProbs := adaptInterFrameYModeProbabilitiesWithBase(&yModeCounts, yModeBase, cfg)
	frameUVModeProbs := adaptInterFrameUVModeProbabilitiesWithBase(&uvModeCounts, uvModeBase, cfg)
	mvCounts := motionVectorBranchCountsFromEvents(&mvEvents)
	frameMVProbs := adaptInterFrameMVProbabilitiesWithBase(&mvCounts, mvBase, cfg)
	return frameYModeProbs, frameUVModeProbs, frameMVProbs, nil
}

func interFrameSkipFalseProbability(counts [2]int, fallback uint8) uint8 {
	total := counts[0] + counts[1]
	if total == 0 {
		if fallback == 0 {
			return 128
		}
		return fallback
	}
	prob := counts[0] * 256 / total
	if prob <= 1 {
		return 1
	}
	if prob > 255 {
		return 255
	}
	return uint8(prob)
}

func interFrameRefProbability(counts [2]int, fallback uint8) uint8 {
	total := counts[0] + counts[1]
	if total == 0 {
		if fallback == 0 {
			return 128
		}
		return fallback
	}
	prob := counts[0] * 255 / total
	if prob <= 0 {
		return 1
	}
	if prob > 255 {
		return 255
	}
	return uint8(prob)
}

func adaptInterFrameMVProbabilities(counts *[2][tables.MVPCount][2]int, cfg *InterFrameStateConfig) {
	adaptInterFrameMVProbabilitiesWithBase(counts, tables.DefaultMVContext, cfg)
}

func adaptInterFrameMVProbabilitiesWithBase(counts *[2][tables.MVPCount][2]int, base [2][tables.MVPCount]uint8, cfg *InterFrameStateConfig) [2][tables.MVPCount]uint8 {
	if counts == nil || cfg == nil {
		return base
	}
	if base == ([2][tables.MVPCount]uint8{}) {
		base = tables.DefaultMVContext
	}
	cfg.MVBase = base
	cfg.MVProbs = base
	cfg.MVUpdate = [2][tables.MVPCount]bool{}
	cfg.MVUpdateCount = 0
	frameProbs := base
	for component := range 2 {
		for i := range tables.MVPCount {
			ct := (*counts)[component][i]
			if ct[0]+ct[1] == 0 {
				continue
			}
			oldProb := base[component][i]
			newProb := motionVectorProbabilityFromBranchCount(ct)
			if newProb == oldProb {
				continue
			}
			if motionVectorProbabilityUpdateSavings(ct, oldProb, newProb, tables.MVUpdateProbs[component][i]) <= 0 {
				continue
			}
			cfg.MVProbs[component][i] = newProb
			frameProbs[component][i] = newProb
			cfg.MVUpdate[component][i] = true
			cfg.MVUpdateCount++
		}
	}
	return frameProbs
}

func motionVectorProbabilityFromBranchCount(counts [2]int) uint8 {
	total := counts[0] + counts[1]
	if total <= 0 {
		return 128
	}
	prob := (counts[0] * 255 / total) &^ 1
	if prob == 0 {
		return 1
	}
	return uint8(prob)
}

func motionVectorProbabilityUpdateSavings(counts [2]int, oldProb uint8, newProb uint8, updateProb uint8) int {
	oldBits := coefficientBranchCost(counts, oldProb)
	newBits := coefficientBranchCost(counts, newProb)
	updateBits := 7 - 1 + ((coefficientBitCost(updateProb, 1) - coefficientBitCost(updateProb, 0) + 128) >> 8)
	return oldBits - newBits - updateBits
}

func adaptInterFrameYModeProbabilitiesWithBase(counts *[tables.YModeProbCount][2]int, base [tables.YModeProbCount]uint8, cfg *InterFrameStateConfig) [tables.YModeProbCount]uint8 {
	base = normalizeYModeProbabilityBase(base)
	cfg.YModeBase = base
	cfg.YModeProbs = base
	cfg.YModeUpdate = false
	var frameProbs [tables.YModeProbCount]uint8
	if !modeProbabilityUpdateFromBranchCounts(base[:], counts[:], frameProbs[:]) {
		return base
	}
	cfg.YModeProbs = frameProbs
	cfg.YModeUpdate = true
	return cfg.YModeProbs
}

func adaptInterFrameUVModeProbabilitiesWithBase(counts *[tables.UVModeProbCount][2]int, base [tables.UVModeProbCount]uint8, cfg *InterFrameStateConfig) [tables.UVModeProbCount]uint8 {
	base = normalizeUVModeProbabilityBase(base)
	cfg.UVModeBase = base
	cfg.UVModeProbs = base
	cfg.UVModeUpdate = false
	var frameProbs [tables.UVModeProbCount]uint8
	if !modeProbabilityUpdateFromBranchCounts(base[:], counts[:], frameProbs[:]) {
		return base
	}
	cfg.UVModeProbs = frameProbs
	cfg.UVModeUpdate = true
	return cfg.UVModeProbs
}

func modeProbabilityUpdateFromBranchCounts(base []uint8, counts []([2]int), frameProbs []uint8) bool {
	copy(frameProbs, base)
	oldBits := 0
	newBits := 0
	for i := range counts {
		newProb := coefficientProbabilityFromBranchCount(counts[i])
		oldBits += coefficientBranchCost(counts[i], base[i])
		newBits += coefficientBranchCost(counts[i], newProb)
		if newProb == 0 {
			newProb = 1
		}
		frameProbs[i] = newProb
	}
	return newBits+(len(counts)<<8) < oldBits
}

func normalizeYModeProbabilityBase(base [tables.YModeProbCount]uint8) [tables.YModeProbCount]uint8 {
	if base == ([tables.YModeProbCount]uint8{}) {
		return tables.DefaultYModeProbs
	}
	return base
}

func normalizeUVModeProbabilityBase(base [tables.UVModeProbCount]uint8) [tables.UVModeProbCount]uint8 {
	if base == ([tables.UVModeProbCount]uint8{}) {
		return tables.DefaultUVModeProbs
	}
	return base
}

func interFrameYModeProbs(cfg InterFrameStateConfig) [tables.YModeProbCount]uint8 {
	probs := normalizeYModeProbabilityBase(cfg.YModeBase)
	if cfg.YModeUpdate {
		probs = normalizeYModeProbabilityBase(cfg.YModeProbs)
	}
	return probs
}

func interFrameUVModeProbs(cfg InterFrameStateConfig) [tables.UVModeProbCount]uint8 {
	probs := normalizeUVModeProbabilityBase(cfg.UVModeBase)
	if cfg.UVModeUpdate {
		probs = normalizeUVModeProbabilityBase(cfg.UVModeProbs)
	}
	return probs
}

func interFrameMVProbs(cfg InterFrameStateConfig) [2][tables.MVPCount]uint8 {
	probs := cfg.MVBase
	if probs == ([2][tables.MVPCount]uint8{}) {
		probs = tables.DefaultMVContext
	}
	for component := range 2 {
		for i := range tables.MVPCount {
			if cfg.MVUpdate[component][i] {
				probs[component][i] = cfg.MVProbs[component][i]
			}
		}
	}
	return probs
}

func countMotionVectorBranches(counts *[2][tables.MVPCount][2]int, mv MotionVector) error {
	if counts == nil || mv.Row&1 != 0 || mv.Col&1 != 0 {
		return ErrInvalidPacketConfig
	}
	if !countMVComponentBranches(&(*counts)[0], int(mv.Row/2)) {
		return ErrInvalidPacketConfig
	}
	if !countMVComponentBranches(&(*counts)[1], int(mv.Col/2)) {
		return ErrInvalidPacketConfig
	}
	return nil
}

func countMotionVectorEvents(events *motionVectorEventCounts, mv MotionVector) error {
	if events == nil || mv.Row&1 != 0 || mv.Col&1 != 0 {
		return ErrInvalidPacketConfig
	}
	row := int(mv.Row / 2)
	col := int(mv.Col / 2)
	if !validMotionVectorEventComponent(row) || !validMotionVectorEventComponent(col) {
		return nil
	}
	(*events)[0][mvComponentMax+row]++
	(*events)[1][mvComponentMax+col]++
	return nil
}

func validMotionVectorEventComponent(component int) bool {
	return component >= -mvComponentMax && component <= mvComponentMax
}

func motionVectorBranchCountsFromEvents(events *motionVectorEventCounts) [2][tables.MVPCount][2]int {
	var counts [2][tables.MVPCount][2]int
	if events == nil {
		return counts
	}
	for component := range counts {
		counts[component] = motionVectorComponentBranchCountsFromEvents(&(*events)[component])
	}
	return counts
}

func motionVectorComponentBranchCountsFromEvents(events *motionVectorComponentEvents) [tables.MVPCount][2]int {
	var counts [tables.MVPCount][2]int
	if events == nil {
		return counts
	}
	var shortDistribution [mvNumShort]int
	for magnitude := 0; magnitude <= mvComponentMax; magnitude++ {
		positive := (*events)[mvComponentMax+magnitude]
		negative := 0
		if magnitude != 0 {
			negative = (*events)[mvComponentMax-magnitude]
		}
		total := positive + negative
		if total == 0 {
			continue
		}
		if magnitude == 0 {
			counts[mvProbIsShort][0] += total
			shortDistribution[0] += total
			continue
		}
		counts[mvProbSign][0] += positive
		counts[mvProbSign][1] += negative
		if magnitude < mvNumShort {
			counts[mvProbIsShort][0] += total
			shortDistribution[magnitude] += total
			continue
		}
		counts[mvProbIsShort][1] += total
		for bit := mvLongWidth - 1; bit >= 0; bit-- {
			counts[mvProbBits+bit][(magnitude>>bit)&1] += total
		}
	}
	for token, total := range shortDistribution {
		if total == 0 {
			continue
		}
		if !countTreeTokenBranchesWeighted(counts[mvProbShort:], tables.SmallMVTree[:], smallMVTokens[token], total) {
			return [tables.MVPCount][2]int{}
		}
	}
	return counts
}

func countMVComponentBranches(counts *[tables.MVPCount][2]int, component int) bool {
	negative := component < 0
	if negative {
		component = -component
	}
	if component >= 8 {
		return countLargeMVComponentBranches(counts, component, negative)
	}
	counts[mvProbIsShort][0]++
	if !countTreeTokenBranches(counts[mvProbShort:], tables.SmallMVTree[:], smallMVTokens[component]) {
		return false
	}
	if component != 0 {
		countBoolBranch(&counts[mvProbSign], negative)
	}
	return true
}

func countLargeMVComponentBranches(counts *[tables.MVPCount][2]int, component int, negative bool) bool {
	if component < 8 || component > 0x7ff {
		return false
	}
	counts[mvProbIsShort][1]++
	coded := component
	if component < 16 {
		coded = component - 8
	}
	for i := range 3 {
		counts[mvProbBits+i][(coded>>i)&1]++
	}
	for i := mvLongWidth - 1; i > 3; i-- {
		counts[mvProbBits+i][(coded>>i)&1]++
	}
	if coded&0xfff0 != 0 {
		counts[mvProbBits+3][(component>>3)&1]++
	}
	if component != 0 {
		countBoolBranch(&counts[mvProbSign], negative)
	}
	return true
}

func countBoolBranch(counts *[2]int, value bool) {
	if value {
		counts[1]++
		return
	}
	counts[0]++
}

func countTreeTokenBranches(counts []([2]int), tree []int16, token TreeToken) bool {
	node := int16(0)
	for bitIndex := int(token.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= len(counts) || int(node)+1 >= len(tree) {
			return false
		}
		bit := int((token.Value >> uint(bitIndex)) & 1)
		counts[probIndex][bit]++
		next := tree[int(node)+bit]
		if next <= 0 {
			return bitIndex == 0
		}
		node = next
	}
	return false
}

func countTreeTokenBranchesWeighted(counts []([2]int), tree []int16, token TreeToken, weight int) bool {
	if weight < 0 {
		return false
	}
	if weight == 0 {
		return true
	}
	node := int16(0)
	for bitIndex := int(token.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= len(counts) || int(node)+1 >= len(tree) {
			return false
		}
		bit := int((token.Value >> uint(bitIndex)) & 1)
		counts[probIndex][bit] += weight
		next := tree[int(node)+bit]
		if next <= 0 {
			return bitIndex == 0
		}
		node = next
	}
	return false
}

type InterModeCounts struct {
	Intra   uint8
	Nearest uint8
	Near    uint8
	Split   uint8
}

func interFrameSignBias(cfg InterFrameStateConfig) [common.MaxRefFrames]bool {
	return [common.MaxRefFrames]bool{
		common.GoldenFrame: cfg.GoldenSignBias,
		common.AltRefFrame: cfg.AltRefSignBias,
	}
}

func interModeCounts(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) InterModeCounts {
	_, _, _, counts := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	return counts
}

func InterFrameModeCounts(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) InterModeCounts {
	return interModeCounts(above, left, aboveLeft, refFrame, signBias)
}

func interBestMotionVector(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) MotionVector {
	_, _, best, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	return best
}

func interBestMotionVectorAt(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) MotionVector {
	return clampInterMotionVectorToModeEdges(interBestMotionVector(above, left, aboveLeft, refFrame, signBias), mbRow, mbCol, mbRows, mbCols)
}

func InterFrameBestMotionVectorAt(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) MotionVector {
	return interBestMotionVectorAt(above, left, aboveLeft, refFrame, mbRow, mbCol, mbRows, mbCols, signBias)
}

func InterFrameNearMotionVectorsAt(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) (MotionVector, MotionVector) {
	nearest, near, _, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	nearest = clampInterMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	near = clampInterMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	return nearest, near
}

func InterFrameMotionModeForVector(refFrame common.MVReferenceFrame, mv MotionVector, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, signBias [common.MaxRefFrames]bool) InterFrameMacroblockMode {
	return InterFrameMotionModeForVectorAt(refFrame, mv, above, left, aboveLeft, 0, 0, 1, 1, signBias)
}

func InterFrameMotionModeForVectorAt(refFrame common.MVReferenceFrame, mv MotionVector, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) InterFrameMacroblockMode {
	if mv.IsZero() {
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.ZeroMV}
	}
	nearest, near, _, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	nearest = clampInterMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	near = clampInterMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	switch mv {
	case nearest:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NearestMV, MV: mv}
	case near:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NearMV, MV: mv}
	default:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NewMV, MV: mv}
	}
}

func findNearInterMotionVectors(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) (MotionVector, MotionVector, MotionVector, InterModeCounts) {
	var nearMVs [4]MotionVector
	var counts [4]uint8
	mvIndex := 0
	countIndex := 0

	if aboveRef := interFrameReference(above); aboveRef != common.IntraFrame {
		mv := signBiasMotionVector(above.MV, aboveRef, refFrame, signBias)
		if !mv.IsZero() {
			mvIndex++
			nearMVs[mvIndex] = mv
			countIndex++
		}
		counts[countIndex] += 2
	}
	if leftRef := interFrameReference(left); leftRef != common.IntraFrame {
		mv := signBiasMotionVector(left.MV, leftRef, refFrame, signBias)
		if !mv.IsZero() {
			if mv != nearMVs[mvIndex] {
				mvIndex++
				nearMVs[mvIndex] = mv
				countIndex++
			}
			counts[countIndex] += 2
		} else {
			counts[0] += 2
		}
	}
	if aboveLeftRef := interFrameReference(aboveLeft); aboveLeftRef != common.IntraFrame {
		mv := signBiasMotionVector(aboveLeft.MV, aboveLeftRef, refFrame, signBias)
		if !mv.IsZero() {
			if mv != nearMVs[mvIndex] {
				mvIndex++
				nearMVs[mvIndex] = mv
				countIndex++
			}
			counts[countIndex]++
		} else {
			counts[0]++
		}
	}
	if counts[3] != 0 && nearMVs[mvIndex] == nearMVs[1] {
		counts[1]++
	}
	counts[3] = splitModeCount(above)*2 + splitModeCount(left)*2 + splitModeCount(aboveLeft)
	if counts[2] > counts[1] {
		counts[1], counts[2] = counts[2], counts[1]
		nearMVs[1], nearMVs[2] = nearMVs[2], nearMVs[1]
	}
	if counts[1] >= counts[0] {
		nearMVs[0] = nearMVs[1]
	}
	return nearMVs[1], nearMVs[2], nearMVs[0], InterModeCounts{
		Intra:   counts[0],
		Nearest: counts[1],
		Near:    counts[2],
		Split:   counts[3],
	}
}

func signBiasMotionVector(mv MotionVector, srcRefFrame common.MVReferenceFrame, targetRefFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) MotionVector {
	if srcRefFrame >= 0 && int(srcRefFrame) < len(signBias) &&
		targetRefFrame >= 0 && int(targetRefFrame) < len(signBias) &&
		signBias[srcRefFrame] != signBias[targetRefFrame] {
		return MotionVector{Row: -mv.Row, Col: -mv.Col}
	}
	return mv
}

func (mv MotionVector) IsZero() bool {
	return mv.Row == 0 && mv.Col == 0
}

func validInterFrameMacroblockModeAt(mode *InterFrameMacroblockMode, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) bool {
	if mode == nil {
		return false
	}
	refFrame := interFrameReference(mode)
	if refFrame == common.IntraFrame {
		return validInterIntraMacroblockMode(mode)
	}
	if refFrame != common.LastFrame && refFrame != common.GoldenFrame && refFrame != common.AltRefFrame {
		return false
	}
	nearest, near, _, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	nearest = clampInterMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	near = clampInterMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	switch mode.Mode {
	case common.ZeroMV:
		return mode.MV.IsZero()
	case common.NearestMV:
		return mode.MV == nearest
	case common.NearMV:
		return mode.MV == near
	case common.NewMV:
		return true
	case common.SplitMV:
		return validSplitMVModeWithContext(mode, left, above)
	default:
		return false
	}
}

func validSplitMVMode(mode *InterFrameMacroblockMode) bool {
	if mode == nil || mode.Mode != common.SplitMV || mode.Partition >= tables.NumMBSplits {
		return false
	}
	partitions := int(tables.MBSplitCount[mode.Partition])
	fillCount := int(tables.MBSplitFillCount[mode.Partition])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition][subset])
		mv := mode.BlockMV[block]
		if mv.Row&1 != 0 || mv.Col&1 != 0 {
			return false
		}
		if mode.BModes[block] < common.Left4x4 || mode.BModes[block] > common.New4x4 {
			return false
		}
		fillStart := subset * fillCount
		for i := range fillCount {
			if mode.BlockMV[tables.MBSplitFillOffset[mode.Partition][fillStart+i]] != mv {
				return false
			}
		}
	}
	return mode.MV == mode.BlockMV[15]
}

func validSplitMVModeWithContext(mode *InterFrameMacroblockMode, left *InterFrameMacroblockMode, above *InterFrameMacroblockMode) bool {
	if !validSplitMVMode(mode) {
		return false
	}
	partitions := int(tables.MBSplitCount[mode.Partition])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition][subset])
		leftMV := splitLeftMV(mode, left, block)
		aboveMV := splitAboveMV(mode, above, block)
		if !splitSubMotionLabelMatchesMV(mode.BModes[block], mode.BlockMV[block], leftMV, aboveMV) {
			return false
		}
	}
	return true
}

func clampInterMotionVectorToModeEdges(mv MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) MotionVector {
	if mbRows <= 0 || mbCols <= 0 {
		return mv
	}
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return MotionVector{
		Row: int16(clampInterModeMVComponent(int(mv.Row), top, bottom)),
		Col: int16(clampInterModeMVComponent(int(mv.Col), left, right)),
	}
}

func clampInterModeMVComponent(v int, lowEdge int, highEdge int) int {
	if v < lowEdge-(16<<3) {
		return lowEdge - (16 << 3)
	}
	if v > highEdge+(16<<3) {
		return highEdge + (16 << 3)
	}
	return v
}

func interFrameReference(mode *InterFrameMacroblockMode) common.MVReferenceFrame {
	if mode == nil {
		return common.IntraFrame
	}
	if isInterIntraMacroblockMode(mode.Mode) {
		return common.IntraFrame
	}
	if mode.RefFrame == common.IntraFrame {
		return common.LastFrame
	}
	return mode.RefFrame
}

func splitModeCount(mode *InterFrameMacroblockMode) uint8 {
	if mode != nil && mode.Mode == common.SplitMV {
		return 1
	}
	return 0
}

func splitLeftMV(cur *InterFrameMacroblockMode, left *InterFrameMacroblockMode, block int) MotionVector {
	if block&3 == 0 {
		if left == nil {
			return MotionVector{}
		}
		if left.Mode == common.SplitMV {
			return left.BlockMV[block+3]
		}
		return left.MV
	}
	return cur.BlockMV[block-1]
}

func splitAboveMV(cur *InterFrameMacroblockMode, above *InterFrameMacroblockMode, block int) MotionVector {
	if block>>2 == 0 {
		if above == nil {
			return MotionVector{}
		}
		if above.Mode == common.SplitMV {
			return above.BlockMV[block+12]
		}
		return above.MV
	}
	return cur.BlockMV[block-4]
}

func subMVRefProbs(left MotionVector, above MotionVector) [3]uint8 {
	lez := 0
	if left.IsZero() {
		lez = 1
	}
	aez := 0
	if above.IsZero() {
		aez = 1
	}
	lea := 0
	if left == above {
		lea = 1
	}
	return tables.SubMVRefProb3[(aez<<2)|(lez<<1)|lea]
}

func countSplitMotionVectorBranches(counts *[2][tables.MVPCount][2]int, mode *InterFrameMacroblockMode, left *InterFrameMacroblockMode, above *InterFrameMacroblockMode, best MotionVector) error {
	if counts == nil || !validSplitMVModeWithContext(mode, left, above) {
		return ErrInvalidPacketConfig
	}
	partitions := int(tables.MBSplitCount[mode.Partition])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition][subset])
		leftMV := splitLeftMV(mode, left, block)
		aboveMV := splitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		bMode := mode.BModes[block]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return ErrInvalidPacketConfig
		}
		if bMode != common.New4x4 {
			continue
		}
		delta := MotionVector{Row: target.Row - best.Row, Col: target.Col - best.Col}
		if err := countMotionVectorBranches(counts, delta); err != nil {
			return err
		}
	}
	return nil
}

func countSplitMotionVectorEvents(events *motionVectorEventCounts, mode *InterFrameMacroblockMode, left *InterFrameMacroblockMode, above *InterFrameMacroblockMode, best MotionVector) error {
	if events == nil || !validSplitMVModeWithContext(mode, left, above) {
		return ErrInvalidPacketConfig
	}
	partitions := int(tables.MBSplitCount[mode.Partition])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition][subset])
		leftMV := splitLeftMV(mode, left, block)
		aboveMV := splitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		bMode := mode.BModes[block]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return ErrInvalidPacketConfig
		}
		if bMode != common.New4x4 {
			continue
		}
		delta := MotionVector{Row: target.Row - best.Row, Col: target.Col - best.Col}
		if err := countMotionVectorEvents(events, delta); err != nil {
			return err
		}
	}
	return nil
}

func splitSubMotionLabelMatchesMV(mode common.BPredictionMode, target MotionVector, left MotionVector, above MotionVector) bool {
	switch mode {
	case common.Left4x4:
		return target == left
	case common.Above4x4:
		return above != left && target == above
	case common.Zero4x4:
		return target.IsZero()
	case common.New4x4:
		return true
	default:
		return false
	}
}

func validInterIntraMacroblockMode(mode *InterFrameMacroblockMode) bool {
	if mode.RefFrame != common.IntraFrame || !isInterIntraMacroblockMode(mode.Mode) || mode.UVMode < common.DCPred || mode.UVMode > common.TMPred {
		return false
	}
	if mode.Mode != common.BPred {
		return true
	}
	for _, bMode := range mode.BModes {
		if bMode < common.BDCPred || bMode > common.BHUPred {
			return false
		}
	}
	return true
}

func isInterIntraMacroblockMode(mode common.MBPredictionMode) bool {
	return mode >= common.DCPred && mode <= common.BPred
}

func initInterFrameYModeTokens() [common.VP8YModes]TreeToken {
	var out [common.VP8YModes]TreeToken
	for i := range out {
		BuildTreeToken(tables.YModeTree[:], i, &out[i])
	}
	return out
}

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
			mbCoeffs := &coeffs[index]
			if mbCoeffs.eobCacheComplete(is4x4) {
				if err := writeCoefficientMacroblockTokensCached(w, probs, is4x4, &above[col], &left, mbCoeffs); err != nil {
					return err
				}
			} else if err := WriteCoefficientMacroblockTokens(w, probs, is4x4, &above[col], &left, mbCoeffs); err != nil {
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
			mbCoeffs := &coeffs[index]
			if mbCoeffs.eobCacheComplete(is4x4) {
				if err := writeCoefficientMacroblockTokensCached(w, probs, is4x4, &above[col], &left, mbCoeffs); err != nil {
					return err
				}
			} else if err := WriteCoefficientMacroblockTokens(w, probs, is4x4, &above[col], &left, mbCoeffs); err != nil {
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

func writeInterRefreshHeader(w *BoolWriter, cfg InterFrameStateConfig) {
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

func writeInterModeHeader(w *BoolWriter, cfg InterFrameStateConfig) error {
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

func validInterFrameStateConfig(cfg InterFrameStateConfig) bool {
	return cfg.LoopFilterLevel <= 63 &&
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
