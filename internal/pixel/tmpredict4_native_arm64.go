//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

var tm4LeftRows = [16]byte{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3}

// TMPredict4Native retains libvpx v1.16.0 vpx_dsp/intrapred.c clipping.
// publicChecked preserves the DSP public entry's original upfront checks.
// Above and left may be hoisted only when both are disjoint from all output.
// Invalid windows and aliases retain the original SIMD row-capture fallback.
func TMPredict4Native(dst []byte, ds int, above, left []byte, top byte, publicChecked bool) {
	if publicChecked {
		_ = above[3]
		_ = left[3]
		_ = dst[3*ds+3]
	}
	if !Enabled {
		TMPredictScalar(dst, ds, above, left, top, 4)
		return
	}
fast:
	for {
		if uint(ds)>>30 != 0 {
			break fast
		}
		span := 3*ds + 4
		if span > len(dst) || len(above) < 4 || len(left) < 4 {
			break fast
		}
		if slicesOverlapExact(dst, above, span, 4) || slicesOverlapExact(dst, left, span, 4) {
			break fast
		}
		av := archsimd.BroadcastUint32x4(binary.LittleEndian.Uint32(above)).ReshapeToUint8s()
		lv := archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(left)).ReshapeToUint8s().LookupOrZero(archsimd.LoadUint8x16Array(&tm4LeftRows))
		t := archsimd.BroadcastUint8x16(top)
		pos, neg := lv.SubSaturated(t), t.SubSaturated(lv)
		tile := av.AddSaturated(pos).SubSaturated(neg)
		if ds == 4 {
			tile.Store(dst)
		} else {
			rows := tile.ReshapeToUint32s()
			binary.LittleEndian.PutUint32(dst, rows.GetElem(0))
			binary.LittleEndian.PutUint32(dst[ds:], rows.GetElem(1))
			binary.LittleEndian.PutUint32(dst[2*ds:], rows.GetElem(2))
			binary.LittleEndian.PutUint32(dst[3*ds:], rows.GetElem(3))
		}
		return
	}

	// Exact original SIMD evaluation order for four-pixel rows: capture left
	// once per row, then form the output slice before loading the above packet.
	t := archsimd.BroadcastInt32x4(int32(top))
	for y := 0; y < 4; y++ {
		v := archsimd.BroadcastInt32x4(int32(left[y])).Sub(t)
		StorePixels4(dst[y*ds:], LoadPixels4(above).Add(v))
	}
}
