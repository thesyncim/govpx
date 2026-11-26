//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// TMPredictBytes retains the checked direct-call contract of the libvpx
// v1.16.0 vpx_dsp/intrapred.c true-motion byte path. Declines read no input and
// write no output. TMPredictNative owns the single arithmetic implementation,
// including sequential per-row left capture when left aliases destination.
func TMPredictBytes(dst []byte, ds int, above, left []byte, top byte, w int) bool {
	if !Enabled || (w != 8 && w != 16) || uint(ds)>>30 != 0 {
		return false
	}
	span := (w-1)*ds + w
	if span > len(dst) || len(above) < w || len(left) < w {
		return false
	}
	if slicesOverlapExact(dst, above, span, w) {
		return false
	}
	TMPredictNative(dst, ds, above, left, top, w)
	return true
}

const TMNative = true

// TMPredictNative retains libvpx v1.16.0 vpx_dsp/intrapred.c semantics.
// It owns both the checked byte fast path and the original SIMD fallback,
// allowing the public wrapper to forward without preserving fallback arguments.
// Enabled is currently constant true in this ARM build selection. The explicit
// scalar branch also preserves the direct entry contract if that changes.
func TMPredictNative(dst []byte, ds int, above, left []byte, top byte, w int) {
	if !Enabled {
		TMPredictScalar(dst, ds, above, left, top, w)
		return
	}
fast:
	for {

		if !Enabled || (w != 8 && w != 16) || uint(ds)>>30 != 0 {
			break fast
		}
		span := (w-1)*ds + w
		if span > len(dst) || len(above) < w || len(left) < w {
			break fast
		}
		if slicesOverlapExact(dst, above, span, w) {
			break fast
		}
		if !slicesOverlapExact(dst, left, span, w) {
			// Only disjoint left/output windows may capture all left rows up front.
			// The original per-row left read remains below for every alias.
			t := archsimd.BroadcastUint8x16(top)
			if w == 8 {
				a8 := binary.LittleEndian.Uint64(above)
				v := archsimd.Uint64x2{}.SetElem(0, a8).SetElem(1, a8).ReshapeToUint8s()
				lv := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(left)).ReshapeToUint8s()
				pos, neg := lv.SubSaturated(t), t.SubSaturated(lv)
				idx := archsimd.Uint64x2{}.SetElem(1, 0x0101010101010101).ReshapeToUint8s()
				step := archsimd.BroadcastUint8x16(2)
				off := 0
				for range 4 {
					rows := v.AddSaturated(pos.LookupOrZero(idx)).SubSaturated(neg.LookupOrZero(idx)).ReshapeToUint64s()
					binary.LittleEndian.PutUint64(dst[off:], rows.GetElem(0))
					binary.LittleEndian.PutUint64(dst[off+ds:], rows.GetElem(1))
					off += 2 * ds
					idx = idx.Add(step)
				}
			} else {
				v := archsimd.LoadUint8x16(above)
				lv := archsimd.LoadUint8x16(left)
				pos, neg := lv.SubSaturated(t), t.SubSaturated(lv)
				idx := archsimd.Uint8x16{}
				step := archsimd.BroadcastUint8x16(1)
				off := 0
				for range 16 {
					row := v.AddSaturated(pos.LookupOrZero(idx)).SubSaturated(neg.LookupOrZero(idx))
					row.Store(dst[off:])
					off += ds
					idx = idx.Add(step)
				}
			}
			return
		}
		// Read left per row after the previous row stores, including aliases.
		// delta is in [-255,255]. Only one of plus/minus is nonzero, so these
		// two unsigned saturations equal clamp(above+left-top,0,255) exactly.
		if w == 8 {
			v := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(above)).ReshapeToUint8s()
			for y := range 8 {
				delta := int(left[y]) - int(top)
				row := v.AddSaturated(archsimd.BroadcastUint8x16(byte(max(delta, 0)))).SubSaturated(archsimd.BroadcastUint8x16(byte(max(-delta, 0))))
				binary.LittleEndian.PutUint64(dst[y*ds:], row.ReshapeToUint64s().GetElem(0))
			}
		} else {
			v := archsimd.LoadUint8x16(above)
			for y := range 16 {
				delta := int(left[y]) - int(top)
				row := v.AddSaturated(archsimd.BroadcastUint8x16(byte(max(delta, 0)))).SubSaturated(archsimd.BroadcastUint8x16(byte(max(-delta, 0))))
				row.Store(dst[y*ds:])
			}
		}
		return

	}
	t := archsimd.BroadcastInt32x4(int32(top))
	for y := range w {
		v := archsimd.BroadcastInt32x4(int32(left[y])).Sub(t)
		for x := 0; x < w; x += 4 {
			StorePixels4(dst[y*ds+x:], LoadPixels4(above[x:]).Add(v))
		}
	}
}
