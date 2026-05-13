package common

// Wire-format constants that aren't strictly enums but live in the VP9
// frame header. Ported from libvpx v1.16.0 vp9/common/vp9_common.h and
// vp9/common/vp9_onyxc_int.h byte-for-byte.

const (
	// VP9FrameMarker is the 2-bit prefix that identifies a VP9 frame.
	// Every valid VP9 frame starts with the bits 0b10.
	VP9FrameMarker = 0x2

	// VP9 sync code — three bytes after the profile + frame parameters
	// in a key frame or an intra-only frame. Constants from
	// vp9_common.h.
	VP9SyncCode0 = 0x49
	VP9SyncCode1 = 0x83
	VP9SyncCode2 = 0x42

	// Reference frame ring slots. VP9 keeps 8 slots ({LAST, GOLDEN,
	// ALTREF} are 3 of them at any time).
	RefFramesLog2 = 3
	RefFrames     = 1 << RefFramesLog2
	RefsPerFrame  = 3

	// 4 frame-context slots (frame-context index is 2 bits in the
	// uncompressed header).
	FrameContextsLog2 = 2
	FrameContexts     = 1 << FrameContextsLog2
)

// FrameType is the high-level frame classification carried in the
// uncompressed header. Matches FRAME_TYPE in libvpx vp9_blockd.h.
type FrameType uint8

const (
	KeyFrame   FrameType = 0
	InterFrame FrameType = 1
	FrameTypes FrameType = 2
)

// ColorSpace mirrors vpx_color_space_t in vpx/vpx_image.h — the 3-bit
// color-space field that appears in the bitdepth/colorspace/sampling
// block.
type ColorSpace uint8

const (
	CSUnknown ColorSpace = 0
	CSBT601   ColorSpace = 1
	CSBT709   ColorSpace = 2
	CSSMPTE170 ColorSpace = 3
	CSSMPTE240 ColorSpace = 4
	CSBT2020  ColorSpace = 5
	CSReserved ColorSpace = 6
	CSSRGB    ColorSpace = 7
)

// ColorRange mirrors vpx_color_range_t — the single bit that selects
// studio vs. full range.
type ColorRange uint8

const (
	CRStudioRange ColorRange = 0
	CRFullRange   ColorRange = 1
)
