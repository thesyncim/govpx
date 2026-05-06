package encoder

import "github.com/thesyncim/libgopx/internal/vp8/tables"

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
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
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
		for row := 0; row < rows; row++ {
			for col := 0; col < cols; col++ {
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
		var err error
		n, err = writePartitionedTokenPayload(dst, tokenStart, cfg.TokenPartition, func(partitions int, writers *[8]BoolWriter) error {
			modeGrid := make([]KeyFrameMacroblockMode, rows*cols)
			for i := range modeGrid {
				modeGrid[i] = mode
			}
			return WriteZeroTokenGridPartitioned(writers, partitions, rows, cols, modeGrid, &tables.DefaultCoefProbs)
		})
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
		var err error
		n, err = writePartitionedTokenPayload(dst, tokenStart, cfg.TokenPartition, func(partitions int, writers *[8]BoolWriter) error {
			return WriteZeroTokenGridPartitioned(writers, partitions, rows, cols, modes, &tables.DefaultCoefProbs)
		})
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
	if len(modes) < required || len(coeffs) < required || len(above) < cols {
		return 0, ErrModeBufferTooSmall
	}
	frameCoefProbs, coefUpdates, err := BuildKeyFrameCoefficientProbabilityUpdates(rows, cols, modes, coeffs, above, &tables.DefaultCoefProbs)
	if err != nil {
		return 0, err
	}
	cfg.CoefficientProbs = coefUpdates

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
		if err := WriteCoefficientTokenGrid(&tokens, rows, cols, modes, coeffs, above, &frameCoefProbs); err != nil {
			return 0, err
		}
		tokens.Finish()
		if err := tokens.Err(); err != nil {
			return 0, err
		}
		n = tokenStart + tokens.BytesWritten()
	} else {
		var err error
		n, err = writePartitionedTokenPayload(dst, tokenStart, cfg.TokenPartition, func(partitions int, writers *[8]BoolWriter) error {
			return WriteCoefficientTokenGridPartitioned(writers, partitions, rows, cols, modes, coeffs, above, &frameCoefProbs)
		})
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
