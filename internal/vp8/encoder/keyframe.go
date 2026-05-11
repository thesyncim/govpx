package encoder

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c keyframe packet assembly
// shape for a one-token-partition, zero-coefficient keyframe.

func WriteNeutralKeyFrame(dst []byte, width int, height int, cfg KeyFrameStateConfig) (int, error) {
	if len(dst) < KeyFrameUncompressedHdrSize {
		return 0, ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff {
		return 0, ErrInvalidPacketConfig
	}
	partitionCount, ok := tokenPartitionCount(cfg.TokenPartition)
	if !ok || cfg.MBNoCoeffSkip {
		return 0, ErrInvalidPacketConfig
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	mode := KeyFrameMacroblockMode{YMode: 0, UVMode: 0}

	firstStart := KeyFrameUncompressedHdrSize
	first := BoolWriter{}
	first.Init(dst[firstStart:])
	if err := WriteKeyFrameStateHeader(&first, cfg); err != nil {
		return 0, err
	}
	writeSegmentID := cfg.Segmentation.Enabled && cfg.Segmentation.UpdateMap
	segmentProbs := segmentationTreeProbs(cfg.Segmentation)
	for range rows {
		for range cols {
			if writeSegmentID && !writeMacroblockSegmentID(&first, &segmentProbs, 0) {
				if first.Err() != nil {
					return 0, first.Err()
				}
				return 0, ErrInvalidPacketConfig
			}
			if !WriteKeyFrameMacroblockMode(&first, nil, nil, &mode) {
				if first.Err() != nil {
					return 0, first.Err()
				}
				return 0, ErrInvalidPacketConfig
			}
		}
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
		for range rows {
			for range cols {
				if err := WriteZeroMacroblockTokens(&tokens, &tables.DefaultCoefProbs, false); err != nil {
					return 0, err
				}
			}
		}
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
		modeGrid := make([]KeyFrameMacroblockMode, rows*cols)
		for i := range modeGrid {
			modeGrid[i] = mode
		}
		if err := WriteZeroTokenGridPartitioned(&writers, partitions, rows, cols, modeGrid, &tables.DefaultCoefProbs); err != nil {
			return 0, err
		}
		n, err = finalizePartitionedTokenPayload(scratch, &writers, dst, tokenStart, partitions)
		if err != nil {
			return 0, err
		}
	}

	if err := PutFrameTag(dst, true, 0, !cfg.InvisibleFrame, firstSize); err != nil {
		return 0, err
	}
	if err := PutKeyFrameExtraHeader(dst[FrameTagSize:], width, height, 0, 0); err != nil {
		return 0, err
	}
	return n, nil
}

func WriteZeroKeyFrame(dst []byte, width int, height int, cfg KeyFrameStateConfig, modes []KeyFrameMacroblockMode) (int, error) {
	if len(dst) < KeyFrameUncompressedHdrSize {
		return 0, ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff {
		return 0, ErrInvalidPacketConfig
	}
	partitionCount, ok := tokenPartitionCount(cfg.TokenPartition)
	if !ok || cfg.MBNoCoeffSkip {
		return 0, ErrInvalidPacketConfig
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	required := rows * cols
	if len(modes) < required {
		return 0, ErrModeBufferTooSmall
	}

	firstStart := KeyFrameUncompressedHdrSize
	first := BoolWriter{}
	first.Init(dst[firstStart:])
	if err := WriteKeyFrameStateHeader(&first, cfg); err != nil {
		return 0, err
	}
	if err := WriteKeyFrameModeGridWithSegmentation(&first, rows, cols, modes, cfg.Segmentation); err != nil {
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
		if err := WriteZeroTokenGrid(&tokens, rows, cols, modes, &tables.DefaultCoefProbs); err != nil {
			return 0, err
		}
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
		if err := WriteZeroTokenGridPartitioned(&writers, partitions, rows, cols, modes, &tables.DefaultCoefProbs); err != nil {
			return 0, err
		}
		n, err = finalizePartitionedTokenPayload(scratch, &writers, dst, tokenStart, partitions)
		if err != nil {
			return 0, err
		}
	}

	if err := PutFrameTag(dst, true, 0, !cfg.InvisibleFrame, firstSize); err != nil {
		return 0, err
	}
	if err := PutKeyFrameExtraHeader(dst[FrameTagSize:], width, height, 0, 0); err != nil {
		return 0, err
	}
	return n, nil
}

func WriteCoefficientKeyFrame(dst []byte, width int, height int, cfg KeyFrameStateConfig, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes) (int, error) {
	n, _, err := WriteCoefficientKeyFrameWithProbabilityBase(dst, width, height, cfg, modes, coeffs, above, &tables.DefaultCoefProbs)
	return n, err
}

func WriteCoefficientKeyFrameWithProbabilityBase(dst []byte, width int, height int, cfg KeyFrameStateConfig, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (int, tables.CoefficientProbs, error) {
	return WriteCoefficientKeyFrameWithProbabilityBaseScratch(dst, width, height, cfg, modes, coeffs, above, base, nil)
}

// WriteCoefficientKeyFrameWithProbabilityBaseScratch is the
// allocation-pooled variant. Callers that drive the encoder hot path pass
// a long-lived *PartitionScratch so the multi-token-partition path reuses
// the same per-partition byte buffers across encodes; passing nil falls
// back to allocating per call (the historical behaviour).
func WriteCoefficientKeyFrameWithProbabilityBaseScratch(dst []byte, width int, height int, cfg KeyFrameStateConfig, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs, scratch *PartitionScratch) (int, tables.CoefficientProbs, error) {
	if len(dst) < KeyFrameUncompressedHdrSize {
		return 0, tables.CoefficientProbs{}, ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff {
		return 0, tables.CoefficientProbs{}, ErrInvalidPacketConfig
	}
	partitionCount, ok := tokenPartitionCount(cfg.TokenPartition)
	if !ok {
		return 0, tables.CoefficientProbs{}, ErrInvalidPacketConfig
	}
	// Mirror libvpx alloccommon.c init: pc->mb_no_coeff_skip defaults to 1
	// for every frame, so the keyframe header always emits the
	// mb_no_coeff_skip bit + 8-bit prob_skip_false literal. ProbSkipFalse
	// defaults to 255 (no MB actually skipped) which matches libvpx when
	// every MB carries at least one non-zero coefficient.
	if cfg.MBNoCoeffSkip && cfg.ProbSkipFalse == 0 {
		cfg.ProbSkipFalse = 255
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	required := rows * cols
	if base == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return 0, tables.CoefficientProbs{}, ErrModeBufferTooSmall
	}
	var (
		frameCoefProbs tables.CoefficientProbs
		coefUpdates    CoefficientProbabilityUpdates
		err            error
	)
	if cfg.IndependentContexts {
		frameCoefProbs, coefUpdates, err = BuildKeyFrameCoefficientProbabilityUpdatesIndependent(rows, cols, modes, coeffs, above, base)
	} else {
		frameCoefProbs, coefUpdates, err = BuildKeyFrameCoefficientProbabilityUpdates(rows, cols, modes, coeffs, above, base)
	}
	if err != nil {
		return 0, tables.CoefficientProbs{}, err
	}
	cfg.CoefficientProbs = coefUpdates

	firstStart := KeyFrameUncompressedHdrSize
	first := BoolWriter{}
	first.Init(dst[firstStart:])
	if err := WriteKeyFrameStateHeader(&first, cfg); err != nil {
		return 0, tables.CoefficientProbs{}, err
	}
	if err := WriteKeyFrameModeGridWithSegmentationAndSkip(&first, rows, cols, modes, cfg.Segmentation, cfg.MBNoCoeffSkip, cfg.ProbSkipFalse); err != nil {
		return 0, tables.CoefficientProbs{}, err
	}
	first.Finish()
	if err := first.Err(); err != nil {
		return 0, tables.CoefficientProbs{}, err
	}
	firstSize := first.BytesWritten()
	if firstSize > MaxFirstPartitionSize {
		return 0, tables.CoefficientProbs{}, ErrInvalidPacketConfig
	}

	tokenStart := firstStart + firstSize
	n := 0
	if partitionCount == 1 {
		tokens := BoolWriter{}
		tokens.Init(dst[tokenStart:])
		if err := WriteCoefficientTokenGrid(&tokens, rows, cols, modes, coeffs, above, &frameCoefProbs); err != nil {
			return 0, tables.CoefficientProbs{}, err
		}
		tokens.Finish()
		if err := tokens.Err(); err != nil {
			return 0, tables.CoefficientProbs{}, err
		}
		n = tokenStart + tokens.BytesWritten()
	} else {
		var (
			writers    [8]BoolWriter
			partitions int
			resolved   *PartitionScratch
		)
		resolved, partitions, err = preparePartitionWriters(scratch, &writers, dst, tokenStart, cfg.TokenPartition)
		if err != nil {
			return 0, tables.CoefficientProbs{}, err
		}
		if err := WriteCoefficientTokenGridPartitioned(&writers, partitions, rows, cols, modes, coeffs, above, &frameCoefProbs); err != nil {
			return 0, tables.CoefficientProbs{}, err
		}
		n, err = finalizePartitionedTokenPayload(resolved, &writers, dst, tokenStart, partitions)
		if err != nil {
			return 0, tables.CoefficientProbs{}, err
		}
	}

	if err := PutFrameTag(dst, true, 0, !cfg.InvisibleFrame, firstSize); err != nil {
		return 0, tables.CoefficientProbs{}, err
	}
	if err := PutKeyFrameExtraHeader(dst[FrameTagSize:], width, height, 0, 0); err != nil {
		return 0, tables.CoefficientProbs{}, err
	}
	return n, frameCoefProbs, nil
}
