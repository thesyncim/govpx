package dsp

import "github.com/thesyncim/govpx/internal/pixel"

func sad64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 64, 64))
}

func sad64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 64, 32))
}

func sad32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 32, 64))
}

func sad32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 32, 32))
}

func sad32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 32, 16))
}

func sad16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD16(src[srcOff:], srcStride, ref[refOff:], refStride, 32))
}

func sad16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD16(src[srcOff:], srcStride, ref[refOff:], refStride, 16))
}

func sad16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD16(src[srcOff:], srcStride, ref[refOff:], refStride, 8))
}

func sad8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 8, 16))
}

func sad8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 8, 8))
}

func sad8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 8, 4))
}

func sad4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 4, 8))
}

func sad4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return uint32(pixel.SAD(src[srcOff:], srcStride, ref[refOff:], refStride, 4, 4))
}

func sad4D(src []uint8, srcOff, srcStride int,
	ref []uint8, refOff0, refOff1, refOff2, refOff3, refStride int,
	w, h int, out *[4]uint32,
) bool {
	if pixel.Enabled && out != nil && w > 0 && h > 0 {
		a, b0, b1, b2, b3 := src[srcOff:], ref[refOff0:], ref[refOff1:], ref[refOff2:], ref[refOff3:]
		if w%16 == 0 && h <= 64 {
			*out = [4]uint32{}
			for x := 0; x < w; x += 16 {
				var part [4]uint32
				pixel.SAD16x4(a[x:], srcStride, b0[x:], b1[x:], b2[x:], b3[x:], refStride, h, &part)
				for i := range 4 {
					out[i] += part[i]
				}
			}
		} else {
			out[0] = uint32(pixel.SAD(a, srcStride, b0, refStride, w, h))
			out[1] = uint32(pixel.SAD(a, srcStride, b1, refStride, w, h))
			out[2] = uint32(pixel.SAD(a, srcStride, b2, refStride, w, h))
			out[3] = uint32(pixel.SAD(a, srcStride, b3, refStride, w, h))
		}
		return true
	}

	return sad4DScalar(src, srcOff, srcStride, ref, refOff0, refOff1, refOff2,
		refOff3, refStride, w, h, out)
}
