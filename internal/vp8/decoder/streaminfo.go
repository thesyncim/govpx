package decoder

import vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"

// Stream-info fields are derived from the libvpx v1.16.0 VP8 frame tag
// parsing path in vp8/decoder/decodeframe.c and vp8/vp8_dx_iface.c.

// StreamInfo describes the VP8 frame-header fields that can be read without
// fully decoding a packet.
type StreamInfo struct {
	// Width and Height are the visible coded dimensions carried by key frames.
	// Inter frames reuse the current stream dimensions.
	Width  int
	Height int
	// Profile is the VP8 version_number field from the frame tag. The VP8
	// spec defines values 0..3; govpx accepts 4..7 for compatibility with
	// libvpx but treats them as version 0.
	Profile int

	// KeyFrame reports whether the packet is a VP8 key frame.
	KeyFrame bool
	// ShowFrame reports whether decoding the packet produces visible output.
	ShowFrame bool

	// FirstPartitionSize is the VP8 first-partition byte count from the frame
	// tag.
	FirstPartitionSize int
}

// PeekStreamInfo parses VP8 frame-header metadata without decoding the frame.
func PeekStreamInfo(packet []byte) (StreamInfo, error) {
	header, err := ParseFrameHeader(packet)
	if err != nil {
		return StreamInfo{}, vpxerrors.ErrInvalidData
	}
	return StreamInfoFromFrameHeader(header), nil
}

// StreamInfoFromFrameHeader converts an already-parsed VP8 frame header into
// public stream metadata.
func StreamInfoFromFrameHeader(header FrameHeader) StreamInfo {
	return StreamInfo{
		Width:              header.Width,
		Height:             header.Height,
		Profile:            header.Profile,
		KeyFrame:           header.KeyFrame(),
		ShowFrame:          header.ShowFrame,
		FirstPartitionSize: header.FirstPartitionSize,
	}
}
