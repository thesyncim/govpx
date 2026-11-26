//go:build goexperiment.simd && (arm64 || amd64) && !purego

package dsp

import (
	"encoding/binary"
	"simd/archsimd"

	"github.com/thesyncim/govpx/internal/pixel"
)

// Fixed-row kernels implement libvpx v1.16.0 vpx_dsp/intrapred.c arithmetic.
// Entry guards prove every complete row and preserve the scalar panic path.
func intraTM32Fixed(dst []byte, stride int, above, left []byte, top byte) bool {
	if !pixel.Enabled {
		return false
	}
	a := archsimd.LoadUint8x16Array((*[16]byte)(above[:16]))
	b := archsimd.LoadUint8x16Array((*[16]byte)(above[16:32]))
	for y := range 32 {
		delta := int(left[y]) - int(top)
		pos, neg := archsimd.BroadcastUint8x16(byte(max(delta, 0))), archsimd.BroadcastUint8x16(byte(max(-delta, 0)))
		row := dst[y*stride : y*stride+32]
		a.AddSaturated(pos).SubSaturated(neg).StoreArray((*[16]byte)(row[:16]))
		b.AddSaturated(pos).SubSaturated(neg).StoreArray((*[16]byte)(row[16:32]))
	}
	return true
}
func intraVFixed(dst []byte, stride, n int, above []byte) bool {
	if !intraBridgeDisjoint(dst, stride, n, above, n) {
		return false
	}
	switch n {
	case 4:
		v := binary.LittleEndian.Uint32(above[:4])
		for y := range 4 {
			binary.LittleEndian.PutUint32(dst[y*stride:y*stride+4], v)
		}
	case 8:
		v := binary.LittleEndian.Uint64(above[:8])
		for y := range 8 {
			binary.LittleEndian.PutUint64(dst[y*stride:y*stride+8], v)
		}
	case 16:
		v := archsimd.LoadUint8x16Array((*[16]byte)(above[:16]))
		for y := range 16 {
			v.StoreArray((*[16]byte)(dst[y*stride : y*stride+16]))
		}
	case 32:
		a := archsimd.LoadUint8x16Array((*[16]byte)(above[:16]))
		b := archsimd.LoadUint8x16Array((*[16]byte)(above[16:32]))
		for y := range 32 {
			row := dst[y*stride : y*stride+32]
			a.StoreArray((*[16]byte)(row[:16]))
			b.StoreArray((*[16]byte)(row[16:32]))
		}
	default:
		return false
	}
	return true
}
func intraHFixed(dst []byte, stride, n int, left []byte) bool {
	if !intraBridgeWindow(dst, stride, n) || len(left) < n {
		return false
	}
	switch n {
	case 4:
		for y := range 4 {
			v := uint32(left[y]) * 0x01010101
			binary.LittleEndian.PutUint32(dst[y*stride:y*stride+4], v)
		}
	case 8:
		for y := range 8 {
			v := uint64(left[y]) * 0x0101010101010101
			binary.LittleEndian.PutUint64(dst[y*stride:y*stride+8], v)
		}
	case 16:
		for y := range 16 {
			v := archsimd.BroadcastUint8x16(left[y])
			v.StoreArray((*[16]byte)(dst[y*stride : y*stride+16]))
		}
	case 32:
		for y := range 32 {
			v := archsimd.BroadcastUint8x16(left[y])
			row := dst[y*stride : y*stride+32]
			v.StoreArray((*[16]byte)(row[:16]))
			v.StoreArray((*[16]byte)(row[16:32]))
		}
	default:
		return false
	}
	return true
}
func intraEdgeSum(edge []byte, n int) int {
	switch n {
	case 4:
		return intraSumBytes(archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(edge[:4])).ReshapeToUint8s())
	case 8:
		return intraSumBytes(archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(edge[:8])).ReshapeToUint8s())
	case 16:
		return intraSumBytes(archsimd.LoadUint8x16Array((*[16]byte)(edge[:16])))
	default:
		return intraSumBytes(archsimd.LoadUint8x16Array((*[16]byte)(edge[:16]))) + intraSumBytes(archsimd.LoadUint8x16Array((*[16]byte)(edge[16:32])))
	}
}
func intraDCFixed(dst []byte, stride, n int, above, left []byte, up, lf bool) bool {
	if !intraBridgeWindow(dst, stride, n) || (up && len(above) < n) || (lf && len(left) < n) {
		return false
	}
	sum, count := 0, 0
	if up {
		sum += intraEdgeSum(above, n)
		count += n
	}
	if lf {
		sum += intraEdgeSum(left, n)
		count += n
	}
	value := byte(128)
	if count > 0 {
		value = byte((sum + count/2) / count)
	}
	v := archsimd.BroadcastUint8x16(value)
	switch n {
	case 4:
		word := v.ReshapeToUint32s().GetElem(0)
		for y := range 4 {
			binary.LittleEndian.PutUint32(dst[y*stride:y*stride+4], word)
		}
	case 8:
		word := v.ReshapeToUint64s().GetElem(0)
		for y := range 8 {
			binary.LittleEndian.PutUint64(dst[y*stride:y*stride+8], word)
		}
	case 16:
		for y := range 16 {
			v.StoreArray((*[16]byte)(dst[y*stride : y*stride+16]))
		}
	case 32:
		for y := range 32 {
			row := dst[y*stride : y*stride+32]
			v.StoreArray((*[16]byte)(row[:16]))
			v.StoreArray((*[16]byte)(row[16:32]))
		}
	default:
		return false
	}
	return true
}
