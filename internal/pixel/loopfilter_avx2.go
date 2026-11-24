//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
)

func loopFilterWide(p []byte, q0, step, outer, n, blimit, limit, thresh, kind int) bool {
	if loopFilterByteWide(p, q0, step, outer, n, blimit, limit, thresh, kind) {
		return true
	}
	return loopFilterWide16(p, q0, step, outer, n, blimit, limit, thresh, kind)
}
func loopFilterWide16(p []byte, q0, step, outer, n, blimit, limit, thresh, kind int) bool {
	if (kind != 2 && kind != 4 && kind != 6) || n < 16 || (step != 1 && outer != 1) {
		return false
	}
	width := 8
	if kind == 2 {
		width = 4
	}
	if (outer == 1 && step < 16) || (step == 1 && outer < width) {
		return false
	}
	defer archsimd.ClearAVXUpperBits()
	base := 0
	for ; base+16 <= n; base += 16 {
		loopFilterNarrow16(p, q0+base*outer, step, outer, blimit, limit, thresh, kind)
	}
	if base < n {
		loopFilterNarrow8(p, q0+base*outer, step, outer, blimit, limit, thresh, kind)
	}
	return true
}
func wideBytes(a, b archsimd.Uint8x16) archsimd.Uint8x16 {
	return a.ReshapeToUint64s().InterleaveLo(b.ReshapeToUint64s()).ReshapeToUint8s()
}
func loadEdgeRows16(p []byte, off, pitch, width int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	archsimd.ClearAVXUpperBits()
	a0, a1, a2, a3, a4, a5, a6, a7 := loadEdgeRows(p, off, pitch, width)
	b0, b1, b2, b3, b4, b5, b6, b7 := loadEdgeRows(p, off+8*pitch, pitch, width)
	return wideBytes(a0, b0), wideBytes(a1, b1), wideBytes(a2, b2), wideBytes(a3, b3), wideBytes(a4, b4), wideBytes(a5, b5), wideBytes(a6, b6), wideBytes(a7, b7)
}
func edgeLoad16(p []byte, off, outer int) archsimd.Int16x16 {
	// The wide entry point uses this only for contiguous horizontal samples.
	return archsimd.LoadUint8x16(p[off:]).ExtendToUint16().BitsToInt16()
}
func widen16(v archsimd.Uint8x16) archsimd.Int16x16 { return v.ExtendToUint16().BitsToInt16() }
func anyWide16(v archsimd.Mask16x16) bool           { return !v.ToInt16x16().IsZero() }

func lfClamp16(v archsimd.Int16x16) archsimd.Int16x16 {
	return v.Max(archsimd.BroadcastInt16x16(-128)).Min(archsimd.BroadcastInt16x16(127))
}
func lfWideShift1(v archsimd.Int16x16) archsimd.Int16x16 { return v.ShiftAllRight(1) }
func lfWideShift3(v archsimd.Int16x16) archsimd.Int16x16 { return v.ShiftAllRight(3) }
func lfWideShift7(v archsimd.Int16x16) archsimd.Int16x16 { return v.ShiftAllRight(7) }
func storeNarrowEdge16(p []byte, off, step, outer, count int, v0, v1, v2, v3, v4, v5 archsimd.Int16x16) {
	// Materialize full-width vectors before the width boundary. Keeping wide
	// values live across ClearAVXUpperBits would force vector reloads and
	// immediately dirty the upper halves again. This handoff uses six VEX
	// stores, then only 128-bit loads and operations in the callee.
	var words [96]int16
	v0.Store(words[0:])
	v1.Store(words[16:])
	v2.Store(words[32:])
	v3.Store(words[48:])
	v4.Store(words[64:])
	v5.Store(words[80:])
	storeNarrowEdge16Memory(p, off, step, outer, count, &words)
}

//go:noinline
func storeNarrowEdge16Memory(p []byte, off, step, outer, count int, words *[96]int16) {
	archsimd.ClearAVXUpperBits()

	if outer != 1 {
		storeNarrowEdge(p, off, step, outer, count, archsimd.LoadInt16x8(words[0:]), archsimd.LoadInt16x8(words[16:]), archsimd.LoadInt16x8(words[32:]), archsimd.LoadInt16x8(words[48:]), archsimd.LoadInt16x8(words[64:]), archsimd.LoadInt16x8(words[80:]))
		storeNarrowEdge(p, off+8*outer, step, outer, count, archsimd.LoadInt16x8(words[8:]), archsimd.LoadInt16x8(words[24:]), archsimd.LoadInt16x8(words[40:]), archsimd.LoadInt16x8(words[56:]), archsimd.LoadInt16x8(words[72:]), archsimd.LoadInt16x8(words[88:]))
		return
	}
	wideBytes(packBytes(archsimd.LoadInt16x8(words[0:])), packBytes(archsimd.LoadInt16x8(words[8:]))).Store(p[off:])
	wideBytes(packBytes(archsimd.LoadInt16x8(words[16:])), packBytes(archsimd.LoadInt16x8(words[24:]))).Store(p[off+step:])
	if count >= 4 {
		wideBytes(packBytes(archsimd.LoadInt16x8(words[32:])), packBytes(archsimd.LoadInt16x8(words[40:]))).Store(p[off+2*step:])
		wideBytes(packBytes(archsimd.LoadInt16x8(words[48:])), packBytes(archsimd.LoadInt16x8(words[56:]))).Store(p[off+3*step:])
	}
	if count == 6 {
		wideBytes(packBytes(archsimd.LoadInt16x8(words[64:])), packBytes(archsimd.LoadInt16x8(words[72:]))).Store(p[off+4*step:])
		wideBytes(packBytes(archsimd.LoadInt16x8(words[80:])), packBytes(archsimd.LoadInt16x8(words[88:]))).Store(p[off+5*step:])
	}
}

// Masking a word shift removes bits arriving from the adjacent byte.
// Shifting biased unsigned bytes and subtracting the shifted bias gives
// exact signed rounding without widening either half of the packet.
func lfByteShift1(v archsimd.Int8x16) archsimd.Int8x16 {
	u := v.ToBits().Xor(archsimd.BroadcastUint8x16(128)).ReshapeToUint16s().ShiftAllRight(1).ReshapeToUint8s()
	return u.And(archsimd.BroadcastUint8x16(127)).Sub(archsimd.BroadcastUint8x16(64)).BitsToInt8()
}
func lfByteShift3(v archsimd.Int8x16) archsimd.Int8x16 {
	u := v.ToBits().Xor(archsimd.BroadcastUint8x16(128)).ReshapeToUint16s().ShiftAllRight(3).ReshapeToUint8s()
	return u.And(archsimd.BroadcastUint8x16(31)).Sub(archsimd.BroadcastUint8x16(16)).BitsToInt8()
}
func lfByteHalf(v archsimd.Uint8x16) archsimd.Uint8x16 {
	return v.ReshapeToUint16s().ShiftAllRight(1).ReshapeToUint8s().And(archsimd.BroadcastUint8x16(127))
}
func lfByteLE(a, b archsimd.Uint8x16) archsimd.Mask8x16 {
	return a.SubSaturated(b).Equal(archsimd.Uint8x16{})
}
func lfByteGT(a, b archsimd.Uint8x16) archsimd.Mask8x16 { return lfByteNot(lfByteLE(a, b)) }
func lfTruncSigned(v archsimd.Int16x8) archsimd.Int8x16 {
	return v.ToBits().ReshapeToUint8s().PermuteOrZero(archsimd.LoadInt8x16Array(&packByteIndices)).BitsToInt8()
}

func lfByteNot(v archsimd.Mask8x16) archsimd.Mask8x16 { return v.ToInt8x16().Equal(archsimd.Int8x16{}) }
