//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

func SAD(src []byte, ss int, ref []byte, rs, w, h int) int {
	if !Enabled {
		return SADScalar(src, ss, ref, rs, w, h)
	}
	if w == 8 && (h == 8 || h == 16) && ss >= 0 && rs >= 0 {
		_ = src[(h-1)*ss+7]
		_ = ref[(h-1)*rs+7]
		value := sad8x8Slice(src, ss, ref, rs)
		if h == 16 {
			value += sad8x8Slice(src[8*ss:], ss, ref[8*rs:], rs)
		}
		return value
	}
	// Full windows and bounded geometry prove every exact32-byte load below.
	// Invalid/unusual shapes retain sadGeneric's existing slicing/panic order.
	if (w == 32 || w == 64) && h > 0 && h <= 64 && (uint(ss)|uint(rs))>>30 == 0 && archsimd.X86.AVX2() {
		lastSrc, lastRef := (h-1)*ss+w, (h-1)*rs+w
		if lastSrc <= len(src) && lastRef <= len(ref) {
			var left, right archsimd.Uint64x4
			for y := range h {
				a := src[y*ss:]
				b := ref[y*rs:]
				av := archsimd.LoadUint8x32(a)
				bv := archsimd.LoadUint8x32(b)
				left = left.Add(av.SumOf8AbsDiff(bv))
				if w == 64 {
					av = archsimd.LoadUint8x32(a[32:])
					bv = archsimd.LoadUint8x32(b[32:])
					right = right.Add(av.SumOf8AbsDiff(bv))
				}
			}
			// Each PSADBW lane and the complete total are below 2^31 here.
			// Reinterpreted high32 halves are zero; the existing packed-int32
			// reduction therefore returns the exact full uint64-lane sum.
			lanes := left.Add(right).ReshapeToUint32s().BitsToInt32()
			result := int(sum32(lanes.GetLo().Add(lanes.GetHi())))
			archsimd.ClearAVXUpperBits()
			return result
		}
	}

	return sadGeneric(src, ss, ref, rs, w, h)
}
