package govpx

// VP9SuperframeSize returns the number of bytes needed to pack frames into a
// VP9 superframe packet, including the trailing superframe index.
func VP9SuperframeSize(frames ...[]byte) (int, error) {
	if len(frames) == 0 || len(frames) > 8 {
		return 0, ErrInvalidConfig
	}
	maxSize := 0
	total := 0
	maxInt := int(^uint(0) >> 1)
	for _, frame := range frames {
		if len(frame) == 0 {
			return 0, ErrInvalidConfig
		}
		if uint64(len(frame)) > uint64(^uint32(0)) || total > maxInt-len(frame) {
			return 0, ErrInvalidConfig
		}
		if len(frame) > maxSize {
			maxSize = len(frame)
		}
		total += len(frame)
	}
	sizeBytes := vp9SuperframeSizeBytes(maxSize)
	indexSize := 2 + len(frames)*sizeBytes
	if total > maxInt-indexSize {
		return 0, ErrInvalidConfig
	}
	return total + indexSize, nil
}

// PackVP9SuperframeInto packs 1..8 raw VP9 Profile 0 frames into dst as a
// VP9 superframe. The frame payloads are copied in order, followed by the
// VP9 little-endian superframe index.
func PackVP9SuperframeInto(dst []byte, frames ...[]byte) (int, error) {
	need, err := VP9SuperframeSize(frames...)
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, ErrBufferTooSmall
	}
	maxSize := 0
	offset := 0
	for _, frame := range frames {
		if len(frame) > maxSize {
			maxSize = len(frame)
		}
		copy(dst[offset:], frame)
		offset += len(frame)
	}

	sizeBytes := vp9SuperframeSizeBytes(maxSize)
	marker := vp9SuperframeMarker(len(frames), sizeBytes)
	dst[offset] = marker
	offset++
	for _, frame := range frames {
		size := len(frame)
		for i := 0; i < sizeBytes; i++ {
			dst[offset+i] = byte(size >> (8 * i))
		}
		offset += sizeBytes
	}
	dst[offset] = marker
	return need, nil
}

// PackVP9Superframe is the allocating wrapper around PackVP9SuperframeInto.
func PackVP9Superframe(frames ...[]byte) ([]byte, error) {
	need, err := VP9SuperframeSize(frames...)
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = PackVP9SuperframeInto(out, frames...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func vp9SuperframeSizeBytes(maxSize int) int {
	sizeBytes := 1
	for sizeBytes < 4 && maxSize >= 1<<(8*uint(sizeBytes)) {
		sizeBytes++
	}
	return sizeBytes
}

func vp9SuperframeMarker(frameCount, sizeBytes int) byte {
	return byte(0xc0 | ((sizeBytes - 1) << 3) | (frameCount - 1))
}
