//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Saturated byte differences express clip(above+left-top) exactly: at
// most one of positive and negative is nonzero. Capture above only after
// proving it cannot be changed by a destination store; left stays per-row.
func tmpredictBytesX64(dst []byte, ds int, above, left []byte, top byte, w int) bool {
	if !Enabled || (w != 8 && w != 16) || uint(ds)>>30 != 0 {
		return false
	}
	span := (w-1)*ds + w
	if span > len(dst) || len(above) < w || len(left) < w {
		return false
	}
	if slicesOverlap(dst, above, span, w) {
		return false
	}
	var v archsimd.Uint8x16
	if w == 8 {
		v = archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(above)).ReshapeToUint8s()
	} else {
		v = archsimd.LoadUint8x16(above)
	}
	t := archsimd.BroadcastUint8x16(top)
	for y := 0; y < w; y++ {
		l := archsimd.BroadcastUint8x16(left[y])
		row := v.AddSaturated(l.SubSaturated(t)).SubSaturated(t.SubSaturated(l))
		if w == 8 {
			binary.LittleEndian.PutUint64(dst[y*ds:], row.ReshapeToUint64s().GetElem(0))
		} else {
			row.Store(dst[y*ds:])
		}
	}
	return true
}
