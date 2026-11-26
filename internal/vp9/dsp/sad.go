package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// VP9 Sum-of-Absolute-Differences kernels. Ported from libvpx v1.16.0
// vpx_dsp/sad.c. The parametric `sad` helper is wrapped per (width,
// height) by the explicit *_c entry points the encoder calls; the
// _avg variants subtract against a comp-pred buffer derived from a
// second reference; the _x4d variants take four refs and run SAD
// against each in turn; the _skip variants run SAD on half the rows
// (even-indexed) and double the result, used in the motion-search
// coarse pass.

// sad is the parametric SAD helper. It walks the (width * height)
// block at the supplied strides, accumulating |src - ref| over every
// pixel.
func sad(src []uint8, srcOff, srcStride int,
	ref []uint8, refOff, refStride, w, h int,
) uint32 {
	if w <= 0 || h <= 0 {
		return 0
	}
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, w, h))
}

// The size-specialized SAD wrappers. Names match libvpx's
// vpx_sad{W}x{H}_c. Each is a simple delegate to the parametric sad —
// the Go compiler specializes the inner loops per call site through
// inlining of the size constants.

func VpxSad64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad64x64(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad64x32(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad32x64(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad32x32(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad32x16(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad16x32(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad16x16(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad16x8(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad8x16(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad8x8(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad8x4(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad4x8(src, srcOff, srcStride, ref, refOff, refStride)
}
func VpxSad4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad4x4(src, srcOff, srcStride, ref, refOff, refStride)
}

// VpxSad4D mirrors libvpx's vpx_sad{W}x{H}x4d family: compare one source
// block against four reference blocks and write the four SADs in candidate
// order. It returns false only for nonsensical dimensions; malformed windows
// fall back to the scalar reference and keep the same panic behavior as the
// single-block wrappers.
func VpxSad4D(src []uint8, srcOff, srcStride int,
	ref []uint8, refOff0, refOff1, refOff2, refOff3, refStride int,
	w, h int, out *[4]uint32,
) bool {
	return sad4D(src, srcOff, srcStride, ref, refOff0, refOff1, refOff2,
		refOff3, refStride, w, h, out)
}

func sad4DScalar(src []uint8, srcOff, srcStride int,
	ref []uint8, refOff0, refOff1, refOff2, refOff3, refStride int,
	w, h int, out *[4]uint32,
) bool {
	if out == nil || w <= 0 || h <= 0 {
		return false
	}
	if w%16 == 0 {
		a, b0, b1, b2, b3 := src[srcOff:], ref[refOff0:], ref[refOff1:], ref[refOff2:], ref[refOff3:]
		*out = [4]uint32{}
		for x := 0; x < w; x += 16 {
			var part [4]uint32
			pixel.SAD16x4(a[x:], srcStride, b0[x:], b1[x:], b2[x:], b3[x:], refStride, h, &part)
			out[0] += part[0]
			out[1] += part[1]
			out[2] += part[2]
			out[3] += part[3]
		}
		return true
	}
	if w == 8 {
		var s0, s1, s2, s3 int
		si := srcOff
		r0, r1, r2, r3 := refOff0, refOff1, refOff2, refOff3
		for range h {
			s := src[si : si+8]
			p0 := ref[r0 : r0+8]
			p1 := ref[r1 : r1+8]
			p2 := ref[r2 : r2+8]
			p3 := ref[r3 : r3+8]
			_ = s[7]
			_ = p0[7]
			_ = p1[7]
			_ = p2[7]
			_ = p3[7]
			for x := range 8 {
				sv := int(s[x])
				d0 := sv - int(p0[x])
				d1 := sv - int(p1[x])
				d2 := sv - int(p2[x])
				d3 := sv - int(p3[x])
				s0 += max(d0, -d0)
				s1 += max(d1, -d1)
				s2 += max(d2, -d2)
				s3 += max(d3, -d3)
			}
			si += srcStride
			r0 += refStride
			r1 += refStride
			r2 += refStride
			r3 += refStride
		}
		out[0] = uint32(s0)
		out[1] = uint32(s1)
		out[2] = uint32(s2)
		out[3] = uint32(s3)
		return true
	}
	out[0] = sad(src, srcOff, srcStride, ref, refOff0, refStride, w, h)
	out[1] = sad(src, srcOff, srcStride, ref, refOff1, refStride, w, h)
	out[2] = sad(src, srcOff, srcStride, ref, refOff2, refStride, w, h)
	out[3] = sad(src, srcOff, srcStride, ref, refOff3, refStride, w, h)
	return true
}
