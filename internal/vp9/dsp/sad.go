package dsp

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
	var s uint32
	for y := range h {
		srcRow := srcOff + y*srcStride
		refRow := refOff + y*refStride
		for x := range w {
			a, b := src[srcRow+x], ref[refRow+x]
			if a >= b {
				s += uint32(a - b)
			} else {
				s += uint32(b - a)
			}
		}
	}
	return s
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
	out[0] = sad(src, srcOff, srcStride, ref, refOff0, refStride, w, h)
	out[1] = sad(src, srcOff, srcStride, ref, refOff1, refStride, w, h)
	out[2] = sad(src, srcOff, srcStride, ref, refOff2, refStride, w, h)
	out[3] = sad(src, srcOff, srcStride, ref, refOff3, refStride, w, h)
	return true
}
