//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// loadLFColumnsWide20 requires sixteen disjoint rows of twenty readable bytes.
func loadLFColumnsWide20(p []byte, pitch int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	var p3, p2, p1, p0, q0, q1, q2, q3 archsimd.Uint8x16
	ptr := unsafe.Pointer(unsafe.SliceData(p))
	r0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 0*pitch))).ReshapeToUint64s().InterleaveLo(archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 8*pitch))).ReshapeToUint64s()).ReshapeToUint32s()
	r1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 1*pitch))).ReshapeToUint64s().InterleaveLo(archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 9*pitch))).ReshapeToUint64s()).ReshapeToUint32s()
	r2 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 2*pitch))).ReshapeToUint64s().InterleaveLo(archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 10*pitch))).ReshapeToUint64s()).ReshapeToUint32s()
	r3 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 3*pitch))).ReshapeToUint64s().InterleaveLo(archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 11*pitch))).ReshapeToUint64s()).ReshapeToUint32s()
	r4 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 4*pitch))).ReshapeToUint64s().InterleaveLo(archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 12*pitch))).ReshapeToUint64s()).ReshapeToUint32s()
	r5 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 5*pitch))).ReshapeToUint64s().InterleaveLo(archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 13*pitch))).ReshapeToUint64s()).ReshapeToUint32s()
	r6 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 6*pitch))).ReshapeToUint64s().InterleaveLo(archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 14*pitch))).ReshapeToUint64s()).ReshapeToUint32s()
	r7 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 7*pitch))).ReshapeToUint64s().InterleaveLo(archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr, 15*pitch))).ReshapeToUint64s()).ReshapeToUint32s()
	a0 := r0.InterleaveEven(r4).ReshapeToUint16s()
	a1 := r0.InterleaveOdd(r4).ReshapeToUint16s()
	a2 := r1.InterleaveEven(r5).ReshapeToUint16s()
	a3 := r1.InterleaveOdd(r5).ReshapeToUint16s()
	a4 := r2.InterleaveEven(r6).ReshapeToUint16s()
	a5 := r2.InterleaveOdd(r6).ReshapeToUint16s()
	a6 := r3.InterleaveEven(r7).ReshapeToUint16s()
	a7 := r3.InterleaveOdd(r7).ReshapeToUint16s()
	b0 := a0.InterleaveEven(a4).ReshapeToUint8s()
	b1 := a0.InterleaveOdd(a4).ReshapeToUint8s()
	b2 := a2.InterleaveEven(a6).ReshapeToUint8s()
	b3 := a2.InterleaveOdd(a6).ReshapeToUint8s()
	b4 := a1.InterleaveEven(a5).ReshapeToUint8s()
	b5 := a1.InterleaveOdd(a5).ReshapeToUint8s()
	b6 := a3.InterleaveEven(a7).ReshapeToUint8s()
	b7 := a3.InterleaveOdd(a7).ReshapeToUint8s()
	p3 = b0.InterleaveEven(b2)
	p2 = b0.InterleaveOdd(b2)
	p1 = b1.InterleaveEven(b3)
	p0 = b1.InterleaveOdd(b3)
	q0 = b4.InterleaveEven(b6)
	q1 = b4.InterleaveOdd(b6)
	q2 = b5.InterleaveEven(b7)
	q3 = b5.InterleaveOdd(b7)
	return p3, p2, p1, p0, q0, q1, q2, q3
}
