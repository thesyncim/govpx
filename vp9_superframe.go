package govpx

import vp9bits "github.com/thesyncim/govpx/internal/vp9/bitstream"

// VP9SuperframeSize returns the number of bytes needed to pack frames into a
// VP9 superframe packet, including the trailing superframe index.
func VP9SuperframeSize(frames ...[]byte) (int, error) {
	return vp9bits.SuperframeSize(frames...)
}

// PackVP9SuperframeInto packs 1..8 raw VP9 Profile 0 frames into dst as a
// VP9 superframe. The frame payloads are copied in order, followed by the
// VP9 little-endian superframe index.
func PackVP9SuperframeInto(dst []byte, frames ...[]byte) (int, error) {
	return vp9bits.PackSuperframeInto(dst, frames...)
}

// PackVP9Superframe is the allocating wrapper around PackVP9SuperframeInto.
func PackVP9Superframe(frames ...[]byte) ([]byte, error) {
	return vp9bits.PackSuperframe(frames...)
}

type vp9SuperframeIndex struct {
	frames [8][]byte
	count  int
}

func vp9ParseSuperframe(packet []byte) (vp9SuperframeIndex, error) {
	index, err := vp9bits.ParseSuperframe(packet)
	return vp9SuperframeIndex{
		frames: index.Frames,
		count:  index.Count,
	}, err
}

func vp9SuperframeSizeBytes(maxSize int) int {
	return vp9bits.SuperframeSizeBytes(maxSize)
}

func vp9SuperframeMarker(frameCount, sizeBytes int) byte {
	return vp9bits.SuperframeMarker(frameCount, sizeBytes)
}
