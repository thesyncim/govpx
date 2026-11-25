package govpx

import (
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/codecapi"
)

type StreamInfo = codecapi.StreamInfo
type ReferenceFlags = codecapi.ReferenceFlags
type ReferenceFrame = codecapi.ReferenceFrame
type FrameInfo = codecapi.FrameInfo
type VP9StreamInfo = codecapi.VP9StreamInfo

const (
	ReferenceFlagLast   = codecapi.ReferenceFlagLast
	ReferenceFlagGolden = codecapi.ReferenceFlagGolden
	ReferenceFlagAltRef = codecapi.ReferenceFlagAltRef
	ReferenceLast       = codecapi.ReferenceLast
	ReferenceGolden     = codecapi.ReferenceGolden
	ReferenceAltRef     = codecapi.ReferenceAltRef
)

func PeekVP8StreamInfo(packet []byte) (StreamInfo, error) {
	return codecapi.PeekVP8StreamInfo(packet)
}

func peekVP8FrameHeader(packet []byte) (vp8dec.FrameHeader, StreamInfo, error) {
	return codecapi.PeekVP8FrameHeader(packet)
}

func PeekVP9StreamInfo(packet []byte) (VP9StreamInfo, error) {
	return codecapi.PeekVP9StreamInfo(packet)
}

func vp9StreamInfoFromInternal(info vp9dec.StreamInfo) VP9StreamInfo {
	return codecapi.VP9StreamInfoFromInternal(info)
}
