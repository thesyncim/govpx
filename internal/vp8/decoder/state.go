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

	Segmentation SegmentationHeader
	LoopFilter   LoopFilterHeader
	Quant        QuantHeader
	Refresh      RefreshHeader
	Probability  CoefficientProbabilityHeader
}

func ParseStateHeader(packet []byte, previousQuant QuantHeader) (FrameHeader, StateHeader, error) {
	frame, err := ParseFrameHeader(packet)
	if err != nil {
		return FrameHeader{}, StateHeader{}, err
	}
	if len(packet) < frame.HeaderSize {
		return FrameHeader{}, StateHeader{}, ErrInvalidFrameHeader
	}

	var br boolcoder.Decoder
	if err := br.Init(packet[frame.HeaderSize:]); err != nil {
		return FrameHeader{}, StateHeader{}, err
	}

	var state StateHeader
	if frame.KeyFrame() {
		state.ColorSpace = int(br.ReadBit())
		state.ClampType = common.ClampType(br.ReadBit())
	}

	state.Segmentation = parseSegmentationHeader(&br)
	state.LoopFilter = parseLoopFilterHeader(&br)
	state.Quant = parseQuantHeader(&br, previousQuant)
	state.Refresh = parseRefreshHeader(&br, frame)
	state.Probability = parseCoefficientProbabilityHeader(&br)

	if br.Err() != nil {
		return FrameHeader{}, StateHeader{}, ErrTruncatedStateHeader
	}
	return frame, state, nil
}
