package decoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// Header parsing helpers for the VP9 uncompressed frame header.
// Ported from libvpx v1.16.0 vp9/decoder/vp9_decodeframe.c —
// specifically vp9_read_profile, vp9_read_sync_code, vp9_read_frame_size,
// and read_bitdepth_colorspace_sampling.

// ErrInvalidHeader is returned when the uncompressed header fails any
// of the validation steps libvpx surfaces as VPX_CODEC_UNSUP_BITSTREAM:
// bad frame marker, bad sync code, illegal subsampling combination, or
// a reserved bit set to 1.
var ErrInvalidHeader = errors.New("govpx: invalid VP9 uncompressed header")

// ReadProfile reads the 2- or 3-bit profile field. Profiles 0..2 use
// 2 bits; profile 3 uses 3 (the third bit is read only when the first
// two say "3"). Mirrors vp9_read_profile.
func ReadProfile(r *BitReader) common.BitstreamProfile {
	p := r.ReadBit()
	p |= r.ReadBit() << 1
	if p > 2 {
		p += r.ReadBit()
	}
	return common.BitstreamProfile(p)
}

// ReadSyncCode consumes the three VP9 sync code bytes and returns true
// iff they match. Mirrors vp9_read_sync_code — callers convert a false
// return into a VPX_CODEC_UNSUP_BITSTREAM error.
func ReadSyncCode(r *BitReader) bool {
	return r.ReadLiteral(8) == common.VP9SyncCode0 &&
		r.ReadLiteral(8) == common.VP9SyncCode1 &&
		r.ReadLiteral(8) == common.VP9SyncCode2
}

// ReadFrameSize reads the 32 bits encoding (width-1, height-1) and
// returns them as 1-based dimensions. Mirrors vp9_read_frame_size.
func ReadFrameSize(r *BitReader) (width, height uint32) {
	width = r.ReadLiteral(16) + 1
	height = r.ReadLiteral(16) + 1
	return
}

// BitDepth values returned by ReadBitdepthColorspaceSampling. The
// non-highbitdepth build is always 8.
const (
	Bits8  = 8
	Bits10 = 10
	Bits12 = 12
)

// BitdepthColorspaceSampling carries the values parsed by
// read_bitdepth_colorspace_sampling: bit depth, color space, color
// range, and (subsampling_x, subsampling_y).
type BitdepthColorspaceSampling struct {
	BitDepth      uint8
	ColorSpace    common.ColorSpace
	ColorRange    common.ColorRange
	SubsamplingX  uint8
	SubsamplingY  uint8
}

// ReadBitdepthColorspaceSampling parses the block after the sync code
// in a key frame (and in an intra-only frame on profile > 0). Mirrors
// read_bitdepth_colorspace_sampling.
func ReadBitdepthColorspaceSampling(r *BitReader, profile common.BitstreamProfile) (BitdepthColorspaceSampling, error) {
	var out BitdepthColorspaceSampling
	if profile >= common.Profile2 {
		if r.ReadBit() == 1 {
			out.BitDepth = Bits12
		} else {
			out.BitDepth = Bits10
		}
	} else {
		out.BitDepth = Bits8
	}
	out.ColorSpace = common.ColorSpace(r.ReadLiteral(3))
	if out.ColorSpace != common.CSSRGB {
		out.ColorRange = common.ColorRange(r.ReadBit())
		if profile == common.Profile1 || profile == common.Profile3 {
			out.SubsamplingX = uint8(r.ReadBit())
			out.SubsamplingY = uint8(r.ReadBit())
			if out.SubsamplingX == 1 && out.SubsamplingY == 1 {
				return out, ErrInvalidHeader
			}
			if r.ReadBit() != 0 {
				return out, ErrInvalidHeader
			}
		} else {
			out.SubsamplingX = 1
			out.SubsamplingY = 1
		}
	} else {
		out.ColorRange = common.CRFullRange
		if profile == common.Profile1 || profile == common.Profile3 {
			// 4:4:4 chroma is the only legal sampling for sRGB + profile
			// 1/3.
			out.SubsamplingX = 0
			out.SubsamplingY = 0
			if r.ReadBit() != 0 {
				return out, ErrInvalidHeader
			}
		} else {
			// 4:4:4 not allowed in profile 0 or 2.
			return out, ErrInvalidHeader
		}
	}
	return out, nil
}

// ReadFrameMarker validates the 2-bit VP9 frame marker. Returns nil if
// the bits are 0b10; otherwise ErrInvalidHeader. This is the very
// first call against a fresh BitReader on a new frame.
func ReadFrameMarker(r *BitReader) error {
	if r.ReadLiteral(2) != common.VP9FrameMarker {
		return ErrInvalidHeader
	}
	return nil
}
