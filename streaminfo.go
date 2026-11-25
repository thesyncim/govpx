package govpx

import "github.com/thesyncim/govpx/internal/vpx/codecapi"

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

// PeekVP8StreamInfo parses VP8 frame-header metadata without decoding the frame.
func PeekVP8StreamInfo(packet []byte) (StreamInfo, error) {
	return codecapi.PeekVP8StreamInfo(packet)
}

// PeekVP9StreamInfo parses VP9 uncompressed-header metadata without decoding the frame.
func PeekVP9StreamInfo(packet []byte) (VP9StreamInfo, error) {
	return codecapi.PeekVP9StreamInfo(packet)
}
