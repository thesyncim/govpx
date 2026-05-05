package decoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
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
	frame, err := ParseFrameHeader(packet)
	if err != nil {
		return FrameHeader{}, StateHeader{}, err
	}
	if len(packet) < frame.HeaderSize {
		return FrameHeader{}, StateHeader{}, ErrInvalidFrameHeader
	}
	firstPartitionEnd := frame.HeaderSize + frame.FirstPartitionSize
	if frame.FirstPartitionSize <= 0 || firstPartitionEnd < frame.HeaderSize || firstPartitionEnd > len(packet) {
		return FrameHeader{}, StateHeader{}, ErrTruncatedStateHeader
	}

	var br boolcoder.Decoder
	if err := br.Init(packet[frame.HeaderSize:firstPartitionEnd]); err != nil {
		return FrameHeader{}, StateHeader{}, err
	}

	var state StateHeader
	if frame.KeyFrame() {
		state.ColorSpace = int(br.ReadBit())
		state.ClampType = common.ClampType(br.ReadBit())
	}

	state.Segmentation = parseSegmentationHeader(&br)
	state.LoopFilter = parseLoopFilterHeader(&br)
	state.TokenPartition = common.TokenPartition(br.ReadLiteral(2))
	state.Quant = parseQuantHeader(&br, previousQuant)
	state.Refresh = parseRefreshHeader(&br, frame)
	state.Probability = parseCoefficientProbabilityHeader(&br)
	state.Mode = parseModeHeaderInto(&br, frame.KeyFrame(), nil)

	if br.Err() != nil {
		return FrameHeader{}, StateHeader{}, ErrTruncatedStateHeader
	}
	return frame, state, nil
}
