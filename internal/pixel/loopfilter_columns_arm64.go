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

const NativeLFByAxis = true

// CanLoopFilterMacroHorizontal16 proves the complete sequential fused window.
func CanLoopFilterMacroHorizontal16(p []byte, pitch int, mblimit, blimit byte) bool {
	return Enabled && uint(pitch-16)>>30 == 0 && 19*pitch+16 <= len(p) && mblimit != 255 && blimit != 255
}

// LoopFilterMacroHorizontal16Unchecked requires a successful corresponding
// CanLoopFilterMacroHorizontal16 call on this unchanged slice, pitch and limits.
func LoopFilterMacroHorizontal16Unchecked(p []byte, pitch int, mblimit, blimit, limit, thresh byte) {
	loopFilterMacroInternalFusedHorizontal(p, pitch, int(mblimit), int(blimit), int(limit), int(thresh))
}

// CanLoopFilterMacroVertical16 proves the complete sequential fused window.
func CanLoopFilterMacroVertical16(p []byte, pitch int, mblimit, blimit byte) bool {
	return Enabled && uint(pitch-20)>>30 == 0 && 15*pitch+20 <= len(p) && mblimit != 255 && blimit != 255
}

// LoopFilterMacroVertical16Unchecked requires a successful corresponding
// CanLoopFilterMacroVertical16 call on this unchanged slice, pitch and limits.
func LoopFilterMacroVertical16Unchecked(p []byte, pitch int, mblimit, blimit, limit, thresh byte) {
	loopFilterMacroInternalFusedVertical(p, pitch, int(mblimit), int(blimit), int(limit), int(thresh))
}

// LoopFilterMacroInternal16 applies the VP8 macroblock edge and three internal
// edges, in that order, to sixteen lanes. p starts four pixels before the
// macroblock; step crosses edges and outer walks along them. It returns false
// without writing when the complete, nonoverlapping 20x16 window is unavailable.
func LoopFilterMacroInternal16(p []byte, step, outer, mblimit, blimit, limit, thresh int) bool {
	if !Enabled || step <= 0 || outer <= 0 || mblimit < 0 || mblimit >= 255 || blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 {
		return false
	}
	if !((outer == 1 && step >= 16) || (step == 1 && outer >= 20)) || len(p) == 0 {
		return false
	}
	// Prove 19*step+15*outer < len(p) before either multiplication. Incoming
	// four-byte rows and six-byte stores stay within this exact window, even
	// with unaligned addresses and the smallest accepted pitch.
	avail := len(p) - 1
	if step > avail/19 {
		return false
	}
	avail -= 19 * step
	if outer > avail/15 {
		return false
	}
	if outer == 1 {
		loopFilterMacroInternalFusedHorizontal(p, step, mblimit, blimit, limit, thresh)
	} else {
		loopFilterMacroInternalFusedVertical(p, outer, mblimit, blimit, limit, thresh)
	}
	return true
}

// Only identity corrections can skip stores. Unsigned ABS preserves -128.
func uvRoundedZero(f archsimd.Int8x16, kind int) bool {
	bound := uint8(4)
	if kind == 6 {
		bound = 3
	}
	return f.Abs().ToBits().ReduceMax() < bound
}
