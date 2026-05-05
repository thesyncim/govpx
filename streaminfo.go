package libgopx

type StreamInfo struct {
	Width   int
	Height  int
	Profile int

	KeyFrame  bool
	ShowFrame bool

	FirstPartitionSize int
}

type FrameInfo struct {
	Width  int
	Height int

	KeyFrame  bool
	ShowFrame bool
	Corrupted bool

	PTS uint64
}

// PeekVP8StreamInfo parses the VP8 frame tag and, for keyframes, the
// uncompressed keyframe header. The VP8 frame tag layout comes from libvpx
// v1.16.0 vp8/vp8_dx_iface.c and vp8/common/onyxc_int.h.
func PeekVP8StreamInfo(packet []byte) (StreamInfo, error) {
	if len(packet) < 3 {
		return StreamInfo{}, ErrInvalidData
	}

	tag := uint32(packet[0]) | uint32(packet[1])<<8 | uint32(packet[2])<<16
	keyFrame := tag&1 == 0
	profile := int((tag >> 1) & 7)
	showFrame := ((tag >> 4) & 1) != 0
	firstPartitionSize := int(tag >> 5)

	info := StreamInfo{
		Profile:            profile,
		KeyFrame:           keyFrame,
		ShowFrame:          showFrame,
		FirstPartitionSize: firstPartitionSize,
	}

	if !keyFrame {
		return info, nil
	}
	if len(packet) < 10 {
		return StreamInfo{}, ErrInvalidData
	}
	if packet[3] != 0x9d || packet[4] != 0x01 || packet[5] != 0x2a {
		return StreamInfo{}, ErrInvalidData
	}

	widthRaw := uint16(packet[6]) | uint16(packet[7])<<8
	heightRaw := uint16(packet[8]) | uint16(packet[9])<<8
	info.Width = int(widthRaw & 0x3fff)
	info.Height = int(heightRaw & 0x3fff)
	if info.Width <= 0 || info.Height <= 0 {
		return StreamInfo{}, ErrInvalidData
	}
	return info, nil
}
