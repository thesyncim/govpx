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

// Image is a planar YUV/RGB image (8-bit I420 by default; 8-bit
// I422/I440/I444 for VP9 Profile 1; 10-bit or 12-bit via Y16/U16/V16 when
// BitDepth is 10 or 12 for VP9 Profile 2/3).
//
// Plane slices may include stride padding. Width and Height define the
// visible image size, and callers must honor the per-plane strides. For 8-bit
// images (BitDepth == 0 or 8), Y, U, and V carry pixel bytes and strides are
// in bytes. For 10/12-bit images (BitDepth == 10 or 12), Y16, U16, and V16
// carry uint16 samples in [0, (1<<BitDepth)-1] and strides are in uint16
// samples.
type Image struct {
	// Width and Height are the visible luma dimensions in pixels.
	Width  int
	Height int

	// Y, U, and V hold visible 8-bit plane data plus any caller-owned stride
	// padding. Chroma plane dimensions follow Subsampling (4:2:0 by default).
	Y []byte
	U []byte
	V []byte

	// Y16, U16, and V16 hold visible 10-bit or 12-bit plane data (uint16
	// samples in [0, (1<<BitDepth)-1]) when BitDepth is 10 or 12.
	Y16 []uint16
	U16 []uint16
	V16 []uint16

	// YStride, UStride, and VStride are elements between adjacent rows in each
	// plane (bytes for 8-bit Y/U/V; uint16 samples for 10/12-bit Y16/U16/V16).
	// They must be at least the visible width of their plane.
	YStride int
	UStride int
	VStride int

	// Subsampling selects the chroma plane subsampling. Zero is 4:2:0 (I420).
	Subsampling ChromaSubsampling

	// BitDepth is the sample bit depth: 0 or 8 for 8-bit, 10 or 12 for
	// high-bit-depth VP9 Profile 2/3.
	BitDepth uint8
}

// EffectiveBitDepth returns the sample bit depth of img, treating 0 as 8.
func (img Image) EffectiveBitDepth() uint8 {
	if img.BitDepth == 0 {
		return 8
	}
	return img.BitDepth
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
	bd := img.EffectiveBitDepth()
	if bd != 8 && bd != 10 && bd != 12 {
		return false
	}
	uvWidth, uvHeight := img.Subsampling.ChromaDimensions(width, height)
	if img.YStride < width || img.UStride < uvWidth || img.VStride < uvWidth {
		return false
	}
	if bd > 8 {
		if len(img.Y16) < buffers.PlaneLen(img.YStride, height, width) {
			return false
		}
		if len(img.U16) < buffers.PlaneLen(img.UStride, uvHeight, uvWidth) {
			return false
		}
		if len(img.V16) < buffers.PlaneLen(img.VStride, uvHeight, uvWidth) {
			return false
		}
		return true
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
