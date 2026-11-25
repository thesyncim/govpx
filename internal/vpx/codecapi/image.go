package codecapi

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// ChromaSubsampling identifies the chroma plane subsampling of an [Image].
// The zero value is [Subsampling420] (I420), preserving full backwards
// compatibility with existing callers that leave the field unset.
type ChromaSubsampling uint8

const (
	// Subsampling420 is 4:2:0 chroma subsampling (half width, half height).
	Subsampling420 ChromaSubsampling = iota
	// Subsampling422 is 4:2:2 chroma subsampling (half width, full height).
	Subsampling422
	// Subsampling440 is 4:4:0 chroma subsampling (full width, half height).
	Subsampling440
	// Subsampling444 is 4:4:4 / RGB (full width, full height).
	Subsampling444
)

// Shifts returns the (subsamplingX, subsamplingY) right-shift counts.
func (s ChromaSubsampling) Shifts() (int, int) {
	switch s {
	case Subsampling422:
		return 1, 0
	case Subsampling440:
		return 0, 1
	case Subsampling444:
		return 0, 0
	default:
		return 1, 1
	}
}

// ChromaDimensions returns the visible chroma plane dimensions for a luma
// plane of size width x height.
func (s ChromaSubsampling) ChromaDimensions(width, height int) (int, int) {
	ssX, ssY := s.Shifts()
	return (width + (1 << ssX) - 1) >> ssX, (height + (1 << ssY) - 1) >> ssY
}

// ChromaSubsamplingFromShifts maps VP9 (subsamplingX, subsamplingY) flags to
// a [ChromaSubsampling] enum value.
func ChromaSubsamplingFromShifts(ssX, ssY uint8) ChromaSubsampling {
	switch {
	case ssX == 1 && ssY == 0:
		return Subsampling422
	case ssX == 0 && ssY == 1:
		return Subsampling440
	case ssX == 0 && ssY == 0:
		return Subsampling444
	default:
		return Subsampling420
	}
}

// Image is an 8-bit planar YUV/RGB image (I420 by default, or I422/I440/I444
// when Subsampling is set for VP9 Profile 1).
//
// Plane slices may include stride padding. Width and Height define the
// visible image size, and callers must honor the per-plane strides. The
// zero value is not a usable image: Width, Height, the three plane slices,
// and the three strides must all be set before passing the value to
// EncodeInto or DecodeInto.
type Image struct {
	// Width and Height are the visible luma dimensions in pixels.
	Width  int
	Height int

	// Y, U, and V hold visible plane data plus any caller-owned stride padding.
	// Chroma plane dimensions follow Subsampling (4:2:0 by default).
	Y []byte
	U []byte
	V []byte

	// YStride, UStride, and VStride are bytes between adjacent rows in each
	// plane. They must be at least the visible width of their plane.
	YStride int
	UStride int
	VStride int

	// Subsampling selects the chroma plane subsampling. Zero is 4:2:0 (I420).
	Subsampling ChromaSubsampling
}

// ValidForEncode reports whether img has valid plane slices and strides
// for the given visible width and height.
func (img Image) ValidForEncode(width int, height int) bool {
	if img.Width != width || img.Height != height {
		return false
	}
	if width <= 0 || height <= 0 {
		return false
	}
	uvWidth, uvHeight := img.Subsampling.ChromaDimensions(width, height)
	if img.YStride < width || img.UStride < uvWidth || img.VStride < uvWidth {
		return false
	}
	if len(img.Y) < buffers.PlaneLen(img.YStride, height, width) {
		return false
	}
	if len(img.U) < buffers.PlaneLen(img.UStride, uvHeight, uvWidth) {
		return false
	}
	if len(img.V) < buffers.PlaneLen(img.VStride, uvHeight, uvWidth) {
		return false
	}
	return true
}

// PublicImageFromVP8 wraps an internal VP8 image view as a public Image.
func PublicImageFromVP8(src *vp8common.Image) Image {
	if src == nil {
		return Image{}
	}
	return Image{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
}
