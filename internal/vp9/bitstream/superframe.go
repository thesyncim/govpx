package bitstream

import vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"

// SuperframeIndex describes the frame slices carried by a VP9 superframe
// index. Count is zero when a packet is not a superframe.
type SuperframeIndex struct {
	Frames [8][]byte
	Count  int
}

// SuperframeSize returns the number of bytes needed to pack frames into a
// VP9 superframe packet, including the trailing superframe index.
//
// Ported from libvpx v1.16.0 vp9/vp9_cx_iface.c write_superframe_index.
func SuperframeSize(frames ...[]byte) (int, error) {
	if len(frames) == 0 || len(frames) > 8 {
		return 0, vpxerrors.ErrInvalidConfig
	}
	maxSize := 0
	total := 0
	maxInt := int(^uint(0) >> 1)
	for _, frame := range frames {
		if len(frame) == 0 {
			return 0, vpxerrors.ErrInvalidConfig
		}
		if uint64(len(frame)) > uint64(^uint32(0)) || total > maxInt-len(frame) {
			return 0, vpxerrors.ErrInvalidConfig
		}
		if len(frame) > maxSize {
			maxSize = len(frame)
		}
		total += len(frame)
	}
	sizeBytes := SuperframeSizeBytes(maxSize)
	indexSize := 2 + len(frames)*sizeBytes
	if total > maxInt-indexSize {
		return 0, vpxerrors.ErrInvalidConfig
	}
	return total + indexSize, nil
}

// PackSuperframeInto packs 1..8 raw VP9 Profile 0 frames into dst as a
// VP9 superframe. The frame payloads are copied in order, followed by the
// VP9 little-endian superframe index.
func PackSuperframeInto(dst []byte, frames ...[]byte) (int, error) {
	need, err := SuperframeSize(frames...)
	if err != nil {
		return 0, err
	}
	if len(dst) < need {
		return need, vpxerrors.ErrBufferTooSmall
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

	sizeBytes := SuperframeSizeBytes(maxSize)
	marker := SuperframeMarker(len(frames), sizeBytes)
	dst[offset] = marker
	offset++
	for _, frame := range frames {
		size := len(frame)
		for i := range sizeBytes {
			dst[offset+i] = byte(size >> (8 * i))
		}
		offset += sizeBytes
	}
	dst[offset] = marker
	return need, nil
}

// PackSuperframe is the allocating wrapper around PackSuperframeInto.
func PackSuperframe(frames ...[]byte) ([]byte, error) {
	need, err := SuperframeSize(frames...)
	if err != nil {
		return nil, err
	}
	out := make([]byte, need)
	_, err = PackSuperframeInto(out, frames...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ParseSuperframe parses a trailing VP9 superframe index. It returns Count
// zero when packet is a non-superframe packet.
//
// Ported from libvpx v1.16.0 vp9/decoder/vp9_decoder.c
// vp9_parse_superframe_index.
func ParseSuperframe(packet []byte) (SuperframeIndex, error) {
	var sf SuperframeIndex
	if len(packet) == 0 {
		return sf, vpxerrors.ErrInvalidVP9Data
	}
	marker := packet[len(packet)-1]
	if marker&0xe0 != 0xc0 {
		return sf, nil
	}

	frames := int(marker&0x7) + 1
	sizeBytes := int((marker>>3)&0x3) + 1
	indexSize := 2 + frames*sizeBytes
	if len(packet) < indexSize {
		return sf, vpxerrors.ErrInvalidVP9Data
	}
	indexStart := len(packet) - indexSize
	if packet[indexStart] != marker {
		return sf, vpxerrors.ErrInvalidVP9Data
	}

	offset := 0
	sizeOffset := indexStart + 1
	for i := range frames {
		frameSize := 0
		for j := range sizeBytes {
			frameSize |= int(packet[sizeOffset+i*sizeBytes+j]) << (8 * j)
		}
		if frameSize <= 0 || frameSize > indexStart-offset {
			return sf, vpxerrors.ErrInvalidVP9Data
		}
		sf.Frames[i] = packet[offset : offset+frameSize]
		offset += frameSize
	}
	if offset != indexStart {
		return sf, vpxerrors.ErrInvalidVP9Data
	}
	sf.Count = frames
	return sf, nil
}

// SuperframeSizeBytes returns the number of little-endian bytes needed for the
// largest frame size in a VP9 superframe index.
func SuperframeSizeBytes(maxSize int) int {
	sizeBytes := 1
	for sizeBytes < 4 && maxSize >= 1<<(8*uint(sizeBytes)) {
		sizeBytes++
	}
	return sizeBytes
}

// SuperframeMarker returns the VP9 superframe index marker byte.
func SuperframeMarker(frameCount, sizeBytes int) byte {
	return byte(0xc0 | ((sizeBytes - 1) << 3) | (frameCount - 1))
}
