//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// IntraDCFill follows libvpx v1.16.0 vpx_dsp/intrapred.c DC rounding.
// All references are consumed before stores, including destination aliases.
const IntraDCFillNative = true

func IntraDCFill(dst []byte, ds int, above, left []byte, n int, up, lf bool) bool {
	if !Enabled || (n != 8 && n != 16) || uint(ds)>>30 != 0 || (n-1)*ds+n > len(dst) || (up && len(above) < n) || (lf && len(left) < n) {
		return false
	}
	value := byte(128)
	if up || lf {
		var acc archsimd.Uint16x8
		if up {
			if n == 8 {
				v := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(above)).ReshapeToUint8s()
				acc = acc.Add(v.ExtendLo8ToUint16())
			} else {
				v := archsimd.LoadUint8x16(above)
				acc = acc.Add(v.ExtendLo8ToUint16()).Add(v.HiToLo().ExtendLo8ToUint16())
			}
		}
		if lf {
			if n == 8 {
				v := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(left)).ReshapeToUint8s()
				acc = acc.Add(v.ExtendLo8ToUint16())
			} else {
				v := archsimd.LoadUint8x16(left)
				acc = acc.Add(v.ExtendLo8ToUint16()).Add(v.HiToLo().ExtendLo8ToUint16())
			}
		}
		sum := int(acc.ReduceSum())
		if n == 8 {
			if up && lf {
				value = byte((sum + 8) >> 4)
			} else {
				value = byte((sum + 4) >> 3)
			}
		} else {
			if up && lf {
				value = byte((sum + 16) >> 5)
			} else {
				value = byte((sum + 8) >> 4)
			}
		}
	}
	v := archsimd.BroadcastUint8x16(value)
	d2, d3 := 2*ds, 3*ds
	if n == 8 {
		word := v.ReshapeToUint64s().GetElem(0)
		for row := 0; row < 8; row += 4 {
			at := dst[row*ds:]
			binary.LittleEndian.PutUint64(at, word)
			binary.LittleEndian.PutUint64(at[ds:], word)
			binary.LittleEndian.PutUint64(at[d2:], word)
			binary.LittleEndian.PutUint64(at[d3:], word)
		}
	} else {
		for row := 0; row < 16; row += 4 {
			at := dst[row*ds:]
			v.Store(at)
			v.Store(at[ds:])
			v.Store(at[d2:])
			v.Store(at[d3:])
		}
	}
	return true
}
