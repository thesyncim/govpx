package decoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodeframe.c.

var ErrTruncatedStateHeader = errors.New("libgopx: truncated VP8 state header")

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
	frame, err := ParseFrameHeader(packet)
	if err != nil {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, err
	}
	if len(packet) < frame.HeaderSize {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, ErrInvalidFrameHeader
	}
	firstPartitionEnd := frame.HeaderSize + frame.FirstPartitionSize
	if frame.FirstPartitionSize <= 0 || firstPartitionEnd < frame.HeaderSize || firstPartitionEnd > len(packet) {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, ErrTruncatedStateHeader
	}

	var br boolcoder.Decoder
	if err := br.Init(packet[frame.HeaderSize:firstPartitionEnd]); err != nil {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, err
	}

	var state StateHeader
	if frame.KeyFrame() {
		if probs != nil {
			*probs = tables.DefaultCoefProbs
		}
		state.ColorSpace = int(br.ReadBit())
		state.ClampType = common.ClampType(br.ReadBit())
	}

	state.Segmentation = parseSegmentationHeader(&br)
	state.LoopFilter = parseLoopFilterHeader(&br)
	state.TokenPartition = common.TokenPartition(br.ReadLiteral(2))
	state.Quant = parseQuantHeader(&br, previousQuant)
	state.Refresh = parseRefreshHeader(&br, frame)
	state.Probability = parseCoefficientProbabilityHeaderInto(&br, probs)
	state.Mode = parseModeHeaderInto(&br, frame.KeyFrame(), nil)

	if br.Err() != nil {
		return FrameHeader{}, StateHeader{}, boolcoder.Decoder{}, ErrTruncatedStateHeader
	}
	return frame, state, br, nil
}
