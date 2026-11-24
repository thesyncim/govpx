package encoder

import "errors"

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c frame-tag, keyframe
// header, and partition-size packing.

const (
	FrameTagSize                = 3
	KeyFrameExtraHeaderSize     = 7
	KeyFrameUncompressedHdrSize = FrameTagSize + KeyFrameExtraHeaderSize
	MaxFirstPartitionSize       = 1<<19 - 1
	MaxPartitionSize            = 1<<24 - 1
)

var ErrInvalidPacketConfig = errors.New("govpx: invalid VP8 packet config")

func PutFrameTag(dst []byte, keyFrame bool, version int, showFrame bool, firstPartitionSize int) error {
	if len(dst) < FrameTagSize {
		return ErrBufferTooSmall
	}
	if version < 0 || version > 7 || firstPartitionSize < 0 || firstPartitionSize > MaxFirstPartitionSize {
		return ErrInvalidPacketConfig
	}

	frameType := uint32(1)
	if keyFrame {
		frameType = 0
	}
	tag := uint32(firstPartitionSize)<<5 | uint32(version)<<1 | frameType
	if showFrame {
		tag |= 1 << 4
	}
	dst[0] = byte(tag)
	dst[1] = byte(tag >> 8)
	dst[2] = byte(tag >> 16)
	return nil
}

func PutKeyFrameExtraHeader(dst []byte, width int, height int, horizontalScale int, verticalScale int) error {
	if len(dst) < KeyFrameExtraHeaderSize {
		return ErrBufferTooSmall
	}
	if width <= 0 || width > 0x3fff || height <= 0 || height > 0x3fff || horizontalScale < 0 || horizontalScale > 3 || verticalScale < 0 || verticalScale > 3 {
		return ErrInvalidPacketConfig
	}

	dst[0] = 0x9d
	dst[1] = 0x01
	dst[2] = 0x2a
	scaledWidth := uint16(width) | uint16(horizontalScale)<<14
	scaledHeight := uint16(height) | uint16(verticalScale)<<14
	dst[3] = byte(scaledWidth)
	dst[4] = byte(scaledWidth >> 8)
	dst[5] = byte(scaledHeight)
	dst[6] = byte(scaledHeight >> 8)
	return nil
}

func PutPartitionSize(dst []byte, size int) error {
	if len(dst) < 3 {
		return ErrBufferTooSmall
	}
	if size < 0 || size > MaxPartitionSize {
		return ErrInvalidPacketConfig
	}
	dst[0] = byte(size)
	dst[1] = byte(size >> 8)
	dst[2] = byte(size >> 16)
	return nil
}
