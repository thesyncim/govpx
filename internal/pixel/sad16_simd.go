//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import "simd/archsimd"

// The codec's 16-wide hot path avoids a dynamic horizontal loop. For at
// most 64 rows the paired byte differences fit exactly in uint16 lanes.
func SAD16(src []byte, ss int, ref []byte, rs, h int) int {
	if !Enabled {
		return SAD16Scalar(src, ss, ref, rs, h)
	}

	if h <= 0 {
		return 0
	}
	if h > 64 || ss < 0 || rs < 0 {
		return sadGeneric(src, ss, ref, rs, 16, h)
	}
	_ = src[(h-1)*ss+15]
	_ = ref[(h-1)*rs+15]
	if h%2 == 0 {
		return sad16ColsDualSlice(src, ss, ref, rs, h)
	}
	var sum archsimd.Uint16x8
	for y := range h {
		sum = sum.Add(absPairs(archsimd.LoadUint8x16(src[y*ss:]), archsimd.LoadUint8x16(ref[y*rs:])))
	}
	return sumPairs(sum)
}
func SAD16Limit(src []byte, ss int, ref []byte, rs, h, limit int) int {
	if !Enabled {
		return SAD16LimitScalar(src, ss, ref, rs, h, limit)
	}

	if h > 64 || ss < 0 || rs < 0 {
		return SAD16LimitScalar(src, ss, ref, rs, h, limit)
	}

	if h <= 0 {
		return 0
	}
	needSrc, needRef := (h-1)*ss+16, (h-1)*rs+16
	if cap(src) >= needSrc && cap(ref) >= needRef {
		return sad16LimitSlice(src[:needSrc], ss, ref[:needRef], rs, h, limit)
	}
	return SAD16LimitScalar(src, ss, ref, rs, h, limit)
}
func absPairs(a, b archsimd.Uint8x16) archsimd.Uint16x8 {
	d := a.Max(b).Sub(a.Min(b))
	return widen8(d).ToBits().Add(widen8(highBytes(d)).ToBits())
}
func sumPairs(v archsimd.Uint16x8) int {
	return int(sumU32(v.ExtendLo4ToUint32().Add(highU16(v).ExtendLo4ToUint32())))
}
func SAD16x4(src []byte, ss int, r0, r1, r2, r3 []byte, rs, h int, out *[4]uint32) {
	if !Enabled {
		SAD16x4Scalar(src, ss, r0, r1, r2, r3, rs, h, out)
		return
	}

	if h <= 0 {
		*out = [4]uint32{}
		return
	}
	if h > 64 || ss < 0 || rs < 0 {
		SAD16x4Scalar(src, ss, r0, r1, r2, r3, rs, h, out)
		return
	}

	_ = src[(h-1)*ss+15]
	_ = r0[(h-1)*rs+15]
	_ = r1[(h-1)*rs+15]
	_ = r2[(h-1)*rs+15]
	_ = r3[(h-1)*rs+15]
	a, b, c, d := sad16ColsX4Slice(src, r0, r1, r2, r3, ss, rs, h)
	out[0], out[1], out[2], out[3] = uint32(a), uint32(b), uint32(c), uint32(d)
}
