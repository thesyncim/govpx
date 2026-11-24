//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// loadLFColumns16 uses the 16x8 TRN schedule from libvpx v1.16.0's
// VP8 vertical filter (also in govpx assembly at 691cd051). Pairing rows
// eight apart avoids two separate 8x8 transposes and eight final joins.
// The caller proves sixteen rows of eight readable bytes; every scalar
// load reads exactly eight bytes, even at the last row and unaligned offsets.
func loadLFColumns16(p []byte, off, pitch int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	var p3, p2, p1, p0, q0, q1, q2, q3 archsimd.Uint8x16
	base := p[off:]
	r0 := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(base[0*pitch:])).SetElem(1, binary.LittleEndian.Uint64(base[8*pitch:])).ReshapeToUint32s()
	r1 := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(base[1*pitch:])).SetElem(1, binary.LittleEndian.Uint64(base[9*pitch:])).ReshapeToUint32s()
	r2 := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(base[2*pitch:])).SetElem(1, binary.LittleEndian.Uint64(base[10*pitch:])).ReshapeToUint32s()
	r3 := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(base[3*pitch:])).SetElem(1, binary.LittleEndian.Uint64(base[11*pitch:])).ReshapeToUint32s()
	r4 := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(base[4*pitch:])).SetElem(1, binary.LittleEndian.Uint64(base[12*pitch:])).ReshapeToUint32s()
	r5 := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(base[5*pitch:])).SetElem(1, binary.LittleEndian.Uint64(base[13*pitch:])).ReshapeToUint32s()
	r6 := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(base[6*pitch:])).SetElem(1, binary.LittleEndian.Uint64(base[14*pitch:])).ReshapeToUint32s()
	r7 := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(base[7*pitch:])).SetElem(1, binary.LittleEndian.Uint64(base[15*pitch:])).ReshapeToUint32s()
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
