package decoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodeframe.c.

var ErrTruncatedStateHeader = errors.New("govpx: truncated VP8 state header")

type StateHeader struct {
	ColorSpace int
	ClampType  common.ClampType

	Segmentation   SegmentationHeader
	LoopFilter     LoopFilterHeader
	TokenPartition common.TokenPartition
	Quant          QuantHeader
	Refresh        RefreshHeader
	Probability    CoefficientProbabilityHeader
	Mode           ModeHeader
}

func ParseStateHeader(packet []byte, previousQuant QuantHeader) (FrameHeader, StateHeader, error) {
	frame, state, _, err := ParseStateHeaderWithReader(packet, previousQuant)
	return frame, state, err
}

func ParseStateHeaderWithReader(packet []byte, previousQuant QuantHeader) (FrameHeader, StateHeader, boolcoder.Decoder, error) {
	return ParseStateHeaderWithReaderAndProbs(packet, previousQuant, nil)
}

func ParseStateHeaderWithReaderAndProbs(packet []byte, previousQuant QuantHeader, probs *tables.CoefficientProbs) (FrameHeader, StateHeader, boolcoder.Decoder, error) {
	return ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, previousQuant, LoopFilterHeader{}, probs, nil)
}

func ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet []byte, previousQuant QuantHeader, previousLoopFilter LoopFilterHeader, probs *tables.CoefficientProbs, modeProbs *ModeProbs) (FrameHeader, StateHeader, boolcoder.Decoder, error) {
	frame, err := ParseFrameHeader(packet)
	if err != nil {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, err
	}
	return parseStateHeaderFromFrameWithReaderAndProbsAndLoopFilter(packet, frame, previousQuant, previousLoopFilter, probs, modeProbs, false)
}

func ParseStateHeaderFromFrameWithReaderAndProbsAndLoopFilter(packet []byte, frame FrameHeader, previousQuant QuantHeader, previousLoopFilter LoopFilterHeader, probs *tables.CoefficientProbs, modeProbs *ModeProbs) (FrameHeader, StateHeader, boolcoder.Decoder, error) {
	return parseStateHeaderFromFrameWithReaderAndProbsAndLoopFilter(packet, frame, previousQuant, previousLoopFilter, probs, modeProbs, false)
}

func ParseStateHeaderWithErrorConcealment(packet []byte, previousQuant QuantHeader, previousLoopFilter LoopFilterHeader, probs *tables.CoefficientProbs, modeProbs *ModeProbs) (FrameHeader, StateHeader, boolcoder.Decoder, bool, error) {
	frame, err := ParseFrameHeader(packet)
	if err != nil {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, false, err
	}
	return ParseStateHeaderFromFrameWithErrorConcealment(packet, frame, previousQuant, previousLoopFilter, probs, modeProbs)
}

func ParseStateHeaderFromFrameWithErrorConcealment(packet []byte, frame FrameHeader, previousQuant QuantHeader, previousLoopFilter LoopFilterHeader, probs *tables.CoefficientProbs, modeProbs *ModeProbs) (FrameHeader, StateHeader, boolcoder.Decoder, bool, error) {
	frame, state, br, err := parseStateHeaderFromFrameWithReaderAndProbsAndLoopFilter(packet, frame, previousQuant, previousLoopFilter, probs, modeProbs, true)
	if err != nil {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, false, err
	}
	corrupted := br.Err() != nil
	if corrupted && !frame.KeyFrame() {
		state.Refresh.RefreshGolden = false
		state.Refresh.RefreshAltRef = false
		state.Refresh.CopyBufferToGolden = 0
		state.Refresh.CopyBufferToAltRef = 0
		state.Refresh.RefreshEntropyProbs = false
		state.Refresh.RefreshLast = true
	}
	return frame, state, br, corrupted, nil
}

func parseStateHeaderFromFrameWithReaderAndProbsAndLoopFilter(packet []byte, frame FrameHeader, previousQuant QuantHeader, previousLoopFilter LoopFilterHeader, probs *tables.CoefficientProbs, modeProbs *ModeProbs, errorConcealment bool) (FrameHeader, StateHeader, boolcoder.Decoder, error) {
	if len(packet) < frame.HeaderSize {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, ErrInvalidFrameHeader
	}
	firstPartitionEnd := frame.HeaderSize + frame.FirstPartitionSize
	if frame.FirstPartitionSize <= 0 || firstPartitionEnd < frame.HeaderSize || firstPartitionEnd > len(packet) {
		if !errorConcealment || frame.FirstPartitionSize < 0 || firstPartitionEnd < frame.HeaderSize {
			return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, ErrTruncatedStateHeader
		}
		firstPartitionEnd = len(packet)
	}

	var br boolcoder.Decoder
	if err := br.Init(packet[frame.HeaderSize:firstPartitionEnd]); err != nil {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, err
	}

	var state StateHeader
	loopFilter := previousLoopFilter
	if frame.KeyFrame() {
		if probs != nil {
			*probs = tables.DefaultCoefProbs
		}
		if modeProbs != nil {
			ResetModeProbs(modeProbs)
		}
		loopFilter = LoopFilterHeader{}
		state.ColorSpace = int(br.ReadBit())
		state.ClampType = common.ClampType(br.ReadBit())
	}

	state.Segmentation = parseSegmentationHeader(&br)
	state.LoopFilter = parseLoopFilterHeaderWithPrevious(&br, loopFilter)
	state.TokenPartition = common.TokenPartition(br.ReadLiteral(2))
	state.Quant = parseQuantHeader(&br, previousQuant)
	state.Refresh = parseRefreshHeader(&br, frame)
	state.Probability = parseCoefficientProbabilityHeaderInto(&br, probs)
	state.Mode = parseModeHeaderInto(&br, frame.KeyFrame(), modeProbs)

	if br.Err() != nil && !errorConcealment {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, ErrTruncatedStateHeader
	}
	return frame, state, br, nil
}
