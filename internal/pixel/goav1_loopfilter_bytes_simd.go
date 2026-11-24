// SPDX-License-Identifier: BSD-2-Clause
// Byte-domain dataflow extracted from goav1 ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Saturating the threshold sum is exact for block limits below 255. The
// complete int16 path retains the full public threshold domain.
func loopFilterByteWide(p []byte, q0, step, outer, n, blimit, limit, thresh, kind int) bool {
	if n <= 0 || step <= 0 || outer <= 0 || n%8 != 0 || (kind != 2 && kind != 4 && kind != 6) || blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 || (step != 1 && outer != 1) {
		return false
	}
	// With positive strides these endpoints cover every exact 4/8/16-byte
	// row load and each strictly smaller modified store window below.
	left, right := 4, 3
	if kind == 2 {
		left, right = 2, 1
	}
	_ = p[q0-left*step]
	_ = p[q0+right*step+(n-1)*outer]
	chunk := 16
	// Preserve the original eight-position ordering for overlapping layouts.
	if (outer == 1 && step < 16) || (step == 1 && outer < left+right+1) {
		chunk = 8
	}
	for base := 0; base < n; base += chunk {
		lanes := min(chunk, n-base)
		if lanes != 8 && lanes != 16 {
			return false
		}
		loopFilterBytes(p, q0+base*outer, step, outer, lanes, blimit, limit, thresh, kind)
	}
	return true
}
func loadLFByte(p []byte, off, lanes int) archsimd.Uint8x16 {
	if lanes == 8 {
		return load8(p[off:])
	}
	return archsimd.LoadUint8x16Array((*[16]byte)(p[off : off+16]))
}

// The filter entry point has already proved the complete tile. Each shape
// reads only its exact four/eight bytes, with no per-row width dispatch.
func loadLFColumnTile[Size [4]byte | [8]byte](p []byte, off, pitch int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	base := p[off:]
	row := func(y int) archsimd.Uint8x16 {
		at := base[y*pitch:]
		var size Size
		if len(size) == 4 {
			return archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(at)).ReshapeToUint8s()
		}
		return archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(at)).ReshapeToUint8s()
	}
	var size Size
	if len(size) == 4 {
		a0, a1 := zipBytesLo(row(0), row(1)), zipBytesLo(row(2), row(3))
		a2, a3 := zipBytesLo(row(4), row(5)), zipBytesLo(row(6), row(7))
		b0, b2 := a0.InterleaveLo(a1).ReshapeToUint32s(), a2.InterleaveLo(a3).ReshapeToUint32s()
		c01, c23 := b0.InterleaveLo(b2).ReshapeToUint8s(), b0.InterleaveHi(b2).ReshapeToUint8s()
		z := archsimd.Uint8x16{}
		return c01, highBytes(c01), c23, highBytes(c23), z, z, z, z
	}
	return transposeBytes8(row(0), row(1), row(2), row(3), row(4), row(5), row(6), row(7))
}
func joinLFBytes(a, b archsimd.Uint8x16) archsimd.Uint8x16 {
	return a.ReshapeToUint64s().InterleaveLo(b.ReshapeToUint64s()).ReshapeToUint8s()
}
func loadLFColumns[Size [4]byte | [8]byte](p []byte, off, pitch, lanes int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	var size Size
	if transposeLFColumns16 && lanes == 16 && len(size) == 8 {
		return loadLFColumns16(p, off, pitch)
	}
	a0, a1, a2, a3, a4, a5, a6, a7 := loadLFColumnTile[Size](p, off, pitch)
	if lanes == 8 {
		return a0, a1, a2, a3, a4, a5, a6, a7
	}
	b0, b1, b2, b3, b4, b5, b6, b7 := loadLFColumnTile[Size](p, off+8*pitch, pitch)
	return joinLFBytes(a0, b0), joinLFBytes(a1, b1), joinLFBytes(a2, b2), joinLFBytes(a3, b3), joinLFBytes(a4, b4), joinLFBytes(a5, b5), joinLFBytes(a6, b6), joinLFBytes(a7, b7)
}
func lfAbsByte(a, b archsimd.Uint8x16) archsimd.Uint8x16 { return a.Max(b).Sub(a.Min(b)) }
func lfJoinSigned(a, b archsimd.Int8x16) archsimd.Int8x16 {
	return joinLFBytes(a.ToBits(), b.ToBits()).BitsToInt8()
}
func lfMacroCorrection(f archsimd.Int8x16, c int16) archsimd.Int8x16 {
	// The 9/18/27 corrections fit int8 after rounding (maximum +/-27).
	k, bias := archsimd.BroadcastInt16x8(c), archsimd.BroadcastInt16x8(63)
	lo := lfTruncSigned(filterShift7(f.ExtendLo8ToInt16().Mul(k).Add(bias)))
	hi := lfTruncSigned(filterShift7(highBytes(f.ToBits()).BitsToInt8().ExtendLo8ToInt16().Mul(k).Add(bias)))
	return lfJoinSigned(lo, hi)
}
func loopFilterBytes(p []byte, off, step, outer, lanes, blimit, limit, thresh, kind int) {
	var p3, p2, p1, p0, q0, q1, q2, q3 archsimd.Uint8x16
	if step == 1 && outer > 1 {
		if kind == 2 {
			a0, a1, a2, a3, _, _, _, _ := loadLFColumns[[4]byte](p, off-2, outer, lanes)
			p1, p0, q0, q1 = a0, a1, a2, a3
		} else {
			p3, p2, p1, p0, q0, q1, q2, q3 = loadLFColumns[[8]byte](p, off-4, outer, lanes)
		}
	} else {
		p1, p0, q0, q1 = loadLFByte(p, off-2*step, lanes), loadLFByte(p, off-step, lanes), loadLFByte(p, off, lanes), loadLFByte(p, off+step, lanes)
		if kind != 2 {
			p3, p2, q2, q3 = loadLFByte(p, off-4*step, lanes), loadLFByte(p, off-3*step, lanes), loadLFByte(p, off+2*step, lanes), loadLFByte(p, off+3*step, lanes)
		}
	}
	d1, d2, d0, d3 := lfAbsByte(p1, p0), lfAbsByte(q1, q0), lfAbsByte(p0, q0), lfAbsByte(p1, q1)
	threshold := d0.AddSaturated(d0).AddSaturated(lfByteHalf(d3))
	need := lfByteLE(threshold, archsimd.BroadcastUint8x16(byte(blimit)))
	if kind != 2 {
		delta := d1.Max(d2).Max(lfAbsByte(p3, p2)).Max(lfAbsByte(p2, p1)).Max(lfAbsByte(q2, q1)).Max(lfAbsByte(q3, q2))
		need = need.And(lfByteLE(delta, archsimd.BroadcastUint8x16(byte(limit))))
	}
	bits := need.ToInt8x16().ToBits().ReshapeToUint64s()
	if bits.GetElem(0)|bits.GetElem(1) == 0 {
		return
	}
	hev := lfByteGT(d1.Max(d2), archsimd.BroadcastUint8x16(byte(thresh)))
	bias := archsimd.BroadcastUint8x16(128)
	p1c, p0c, q0c, q1c := p1.Xor(bias).BitsToInt8(), p0.Xor(bias).BitsToInt8(), q0.Xor(bias).BitsToInt8(), q1.Xor(bias).BitsToInt8()
	outerF := p1c.SubSaturated(q1c)
	if kind == 4 {
		outerF = outerF.Masked(hev)
	}
	// Add the same signed delta monotonically from the already-clamped
	// outer correction. Intermediate saturation is then exact: a delta
	// outside int8 forces the final clamp even at the opposite outer extreme.
	f := lfFilterDelta(outerF, p0c, q0c).Masked(need)
	three, four := archsimd.BroadcastInt8x16(3), archsimd.BroadcastInt8x16(4)
	z := archsimd.Uint8x16{}
	if kind == 6 {
		fh := f.Masked(hev)
		ps0 := p0c.AddSaturated(lfByteShift3(fh.AddSaturated(three)))
		qs0 := q0c.SubSaturated(lfByteShift3(fh.AddSaturated(four)))
		ff := f.Masked(lfByteNot(hev))
		u := lfMacroCorrection(ff, 27)
		np0, nq0 := ps0.AddSaturated(u).ToBits().Xor(bias), qs0.SubSaturated(u).ToBits().Xor(bias)
		u = lfMacroCorrection(ff, 18)
		np1, nq1 := p1c.AddSaturated(u).ToBits().Xor(bias), q1c.SubSaturated(u).ToBits().Xor(bias)
		u = lfMacroCorrection(ff, 9)
		np2, nq2 := p2.Xor(bias).BitsToInt8().AddSaturated(u).ToBits().Xor(bias), q2.Xor(bias).BitsToInt8().SubSaturated(u).ToBits().Xor(bias)
		storeLFBytes(p, off-3*step, step, outer, lanes, 6, np2, np1, np0, nq0, nq1, nq2)
		return
	}
	f1, f2 := lfByteShift3(f.AddSaturated(four)), lfByteShift3(f.AddSaturated(three))
	np0, nq0 := p0c.AddSaturated(f2).ToBits().Xor(bias), q0c.SubSaturated(f1).ToBits().Xor(bias)
	if kind == 2 {
		storeLFBytes(p, off-step, step, outer, lanes, 2, np0, nq0, z, z, z, z)
		return
	}
	u := lfByteShift1(f1.Add(archsimd.BroadcastInt8x16(1))).Masked(lfByteNot(hev))
	np1, nq1 := p1c.AddSaturated(u).ToBits().Xor(bias), q1c.SubSaturated(u).ToBits().Xor(bias)
	storeLFBytes(p, off-2*step, step, outer, lanes, 4, np1, np0, nq0, nq1, z, z)
}
func storeLFBytes(p []byte, off, step, outer, lanes, count int, a, b, c, d, e, f archsimd.Uint8x16) {
	if outer == 1 {
		store := func(at int, v archsimd.Uint8x16) {
			if lanes == 16 {
				v.StoreArray((*[16]byte)(p[at : at+16]))
			} else {
				binary.LittleEndian.PutUint64(p[at:], v.ReshapeToUint64s().GetElem(0))
			}
		}
		store(off, a)
		store(off+step, b)
		if count >= 4 {
			store(off+2*step, c)
			store(off+3*step, d)
		}
		if count == 6 {
			store(off+4*step, e)
			store(off+5*step, f)
		}
		return
	}
	storeLFByteTile(p, off, outer, count, a, b, c, d, e, f)
	if lanes == 16 {
		storeLFByteTile(p, off+8*outer, outer, count, highBytes(a), highBytes(b), highBytes(c), highBytes(d), highBytes(e), highBytes(f))
	}
}
func storeLFByteTile(p []byte, off, pitch, count int, a, b, c, d, e, f archsimd.Uint8x16) {
	switch count {
	case 2:
		storeLFByteTileRows[[2]byte](p, off, pitch, a, b, c, d, e, f)
	case 4:
		storeLFByteTileRows[[4]byte](p, off, pitch, a, b, c, d, e, f)
	case 6:
		storeLFByteTileRows[[6]byte](p, off, pitch, a, b, c, d, e, f)
	}
}

func storeLFByteTileRows[Size [2]byte | [4]byte | [6]byte](p []byte, off, pitch int, a, b, c, d, e, f archsimd.Uint8x16) {
	base := p[off:]
	var width Size
	if len(width) == 2 {
		rows := zipBytesLo(a, b)
		binary.LittleEndian.PutUint16(base, rows.GetElem(0))
		binary.LittleEndian.PutUint16(base[pitch:], rows.GetElem(1))
		binary.LittleEndian.PutUint16(base[2*pitch:], rows.GetElem(2))
		binary.LittleEndian.PutUint16(base[3*pitch:], rows.GetElem(3))
		binary.LittleEndian.PutUint16(base[4*pitch:], rows.GetElem(4))
		binary.LittleEndian.PutUint16(base[5*pitch:], rows.GetElem(5))
		binary.LittleEndian.PutUint16(base[6*pitch:], rows.GetElem(6))
		binary.LittleEndian.PutUint16(base[7*pitch:], rows.GetElem(7))
		return
	}
	if len(width) == 4 {
		ab, cd := zipBytesLo(a, b), zipBytesLo(c, d)
		lo, hi := ab.InterleaveLo(cd).ReshapeToUint32s(), ab.InterleaveHi(cd).ReshapeToUint32s()
		binary.LittleEndian.PutUint32(base, lo.GetElem(0))
		binary.LittleEndian.PutUint32(base[pitch:], lo.GetElem(1))
		binary.LittleEndian.PutUint32(base[2*pitch:], lo.GetElem(2))
		binary.LittleEndian.PutUint32(base[3*pitch:], lo.GetElem(3))
		binary.LittleEndian.PutUint32(base[4*pitch:], hi.GetElem(0))
		binary.LittleEndian.PutUint32(base[5*pitch:], hi.GetElem(1))
		binary.LittleEndian.PutUint32(base[6*pitch:], hi.GetElem(2))
		binary.LittleEndian.PutUint32(base[7*pitch:], hi.GetElem(3))
		return
	}
	z := archsimd.Uint8x16{}
	r0, r1, r2, r3, r4, r5, r6, r7 := transposeBytes8(a, b, c, d, e, f, z, z)
	// The size is constant in each instantiation. Unrolling keeps the
	// transposed row values in registers until their exact-width store.
	store := func(y int, row archsimd.Uint8x16) {
		var size Size
		v := row.ReshapeToUint64s().GetElem(0)
		dest := base[y*pitch:]
		if len(size) == 2 {
			binary.LittleEndian.PutUint16(dest, uint16(v))
		} else {
			binary.LittleEndian.PutUint32(dest, uint32(v))
			if len(size) == 6 {
				binary.LittleEndian.PutUint16(dest[4:], uint16(v>>32))
			}
		}
	}
	store(0, r0)
	store(1, r1)
	store(2, r2)
	store(3, r3)
	store(4, r4)
	store(5, r5)
	store(6, r6)
	store(7, r7)
}

func lfFilterDelta(outer, p0, q0 archsimd.Int8x16) archsimd.Int8x16 {
	delta := q0.SubSaturated(p0)
	return outer.AddSaturated(delta).AddSaturated(delta).AddSaturated(delta)
}

// LoopFilterSimpleHorizontal16 applies the VP8 simple 16-pixel horizontal edge
// filter directly in byte-domain SIMD without multi-layer dispatch or normal-LF
// limit/hev work. s points at row p1 (2 rows above the edge).
func LoopFilterSimpleHorizontal16(s []byte, stride int, blimit byte) bool {
	if stride < 16 || blimit >= 255 || 3*stride+16 > len(s) {
		return false
	}
	p1 := archsimd.LoadUint8x16Array((*[16]byte)(s[0:16]))
	p0 := archsimd.LoadUint8x16Array((*[16]byte)(s[stride : stride+16]))
	q0 := archsimd.LoadUint8x16Array((*[16]byte)(s[2*stride : 2*stride+16]))
	q1 := archsimd.LoadUint8x16Array((*[16]byte)(s[3*stride : 3*stride+16]))
	d0, d3 := lfAbsByte(p0, q0), lfAbsByte(p1, q1)
	need := lfByteLE(d0.AddSaturated(d0).AddSaturated(lfByteHalf(d3)), archsimd.BroadcastUint8x16(blimit))
	bits := need.ToInt8x16().ToBits().ReshapeToUint64s()
	if bits.GetElem(0)|bits.GetElem(1) == 0 {
		return true
	}
	bias := archsimd.BroadcastUint8x16(128)
	p1c, p0c, q0c, q1c := p1.Xor(bias).BitsToInt8(), p0.Xor(bias).BitsToInt8(), q0.Xor(bias).BitsToInt8(), q1.Xor(bias).BitsToInt8()
	f := lfFilterDelta(p1c.SubSaturated(q1c), p0c, q0c).Masked(need)
	f1 := lfByteShift3(f.AddSaturated(archsimd.BroadcastInt8x16(4)))
	f2 := lfByteShift3(f.AddSaturated(archsimd.BroadcastInt8x16(3)))
	p0c.AddSaturated(f2).ToBits().Xor(bias).StoreArray((*[16]byte)(s[stride : stride+16]))
	q0c.SubSaturated(f1).ToBits().Xor(bias).StoreArray((*[16]byte)(s[2*stride : 2*stride+16]))
	return true
}

// LoopFilterSimpleVertical16 applies the VP8 simple 16-pixel vertical edge
// filter directly in byte-domain SIMD. s points at column p1 (2 cols left of edge).
// Uses direct 4-byte row loads + 4x uint64 transposed stores (4 UMOVs instead
// of 16) to avoid generic tile-gather/scatter overhead.
func LoopFilterSimpleVertical16(s []byte, stride int, blimit byte) bool {
	if stride < 4 || blimit >= 255 || 15*stride+4 > len(s) {
		return false
	}
	// Gather 16x4 bytes (rows 0..15, columns [p1, p0, q0, q1]) into 4 Uint32x4
	// vectors and transpose into column vectors p1, p0, q0, q1.
	s0 := s[0*stride:]
	s1 := s[1*stride:]
	s2 := s[2*stride:]
	s3 := s[3*stride:]
	s4 := s[4*stride:]
	s5 := s[5*stride:]
	s6 := s[6*stride:]
	s7 := s[7*stride:]
	r0 := archsimd.LoadUint32x4Array(&[4]uint32{
		binary.LittleEndian.Uint32(s0), binary.LittleEndian.Uint32(s4),
		binary.LittleEndian.Uint32(s2), binary.LittleEndian.Uint32(s6),
	}).ReshapeToUint8s()
	r1 := archsimd.LoadUint32x4Array(&[4]uint32{
		binary.LittleEndian.Uint32(s1), binary.LittleEndian.Uint32(s5),
		binary.LittleEndian.Uint32(s3), binary.LittleEndian.Uint32(s7),
	}).ReshapeToUint8s()
	s8 := s[8*stride:]
	s9 := s[9*stride:]
	s10 := s[10*stride:]
	s11 := s[11*stride:]
	s12 := s[12*stride:]
	s13 := s[13*stride:]
	s14 := s[14*stride:]
	s15 := s[15*stride:]
	r2 := archsimd.LoadUint32x4Array(&[4]uint32{
		binary.LittleEndian.Uint32(s8), binary.LittleEndian.Uint32(s12),
		binary.LittleEndian.Uint32(s10), binary.LittleEndian.Uint32(s14),
	}).ReshapeToUint8s()
	r3 := archsimd.LoadUint32x4Array(&[4]uint32{
		binary.LittleEndian.Uint32(s9), binary.LittleEndian.Uint32(s13),
		binary.LittleEndian.Uint32(s11), binary.LittleEndian.Uint32(s15),
	}).ReshapeToUint8s()
	// 4x16 byte transpose in exact row order 0..15 (cross-arch arm64+amd64).
	t0 := zipBytesLo(r0, r1)
	t1 := zipBytesLo(highBytes(r0), highBytes(r1))
	t2 := zipBytesLo(r2, r3)
	t3 := zipBytesLo(highBytes(r2), highBytes(r3))
	u0 := t0.InterleaveLo(t1).ReshapeToUint32s()
	u1 := t0.InterleaveHi(t1).ReshapeToUint32s()
	u2 := t2.InterleaveLo(t3).ReshapeToUint32s()
	u3 := t2.InterleaveHi(t3).ReshapeToUint32s()
	loP := u0.InterleaveLo(u1).ReshapeToUint64s() // p1[0..7], p0[0..7]
	loQ := u0.InterleaveHi(u1).ReshapeToUint64s() // q0[0..7], q1[0..7]
	hiP := u2.InterleaveLo(u3).ReshapeToUint64s() // p1[8..15], p0[8..15]
	hiQ := u2.InterleaveHi(u3).ReshapeToUint64s() // q0[8..15], q1[8..15]
	p1 := loP.InterleaveLo(hiP).ReshapeToUint8s()
	p0 := loP.InterleaveHi(hiP).ReshapeToUint8s()
	q0 := loQ.InterleaveLo(hiQ).ReshapeToUint8s()
	q1 := loQ.InterleaveHi(hiQ).ReshapeToUint8s()

	d0, d3 := lfAbsByte(p0, q0), lfAbsByte(p1, q1)
	need := lfByteLE(d0.AddSaturated(d0).AddSaturated(lfByteHalf(d3)), archsimd.BroadcastUint8x16(blimit))
	bits := need.ToInt8x16().ToBits().ReshapeToUint64s()
	if bits.GetElem(0)|bits.GetElem(1) == 0 {
		return true
	}
	bias := archsimd.BroadcastUint8x16(128)
	p1c, p0c, q0c, q1c := p1.Xor(bias).BitsToInt8(), p0.Xor(bias).BitsToInt8(), q0.Xor(bias).BitsToInt8(), q1.Xor(bias).BitsToInt8()
	f := lfFilterDelta(p1c.SubSaturated(q1c), p0c, q0c).Masked(need)
	f1 := lfByteShift3(f.AddSaturated(archsimd.BroadcastInt8x16(4)))
	f2 := lfByteShift3(f.AddSaturated(archsimd.BroadcastInt8x16(3)))
	np0 := p0c.AddSaturated(f2).ToBits().Xor(bias)
	nq0 := q0c.SubSaturated(f1).ToBits().Xor(bias)
	// Interleave np0 and nq0 into 16 pairs of [p0, q0] and extract via 4 uint64s.
	pairLo := zipBytesLo(np0, nq0).ReshapeToUint64s()
	pairHi := zipBytesLo(highBytes(np0), highBytes(nq0)).ReshapeToUint64s()
	v0, v1 := pairLo.GetElem(0), pairLo.GetElem(1)
	v2, v3 := pairHi.GetElem(0), pairHi.GetElem(1)
	binary.LittleEndian.PutUint16(s0[1:], uint16(v0))
	binary.LittleEndian.PutUint16(s1[1:], uint16(v0>>16))
	binary.LittleEndian.PutUint16(s2[1:], uint16(v0>>32))
	binary.LittleEndian.PutUint16(s3[1:], uint16(v0>>48))
	binary.LittleEndian.PutUint16(s4[1:], uint16(v1))
	binary.LittleEndian.PutUint16(s5[1:], uint16(v1>>16))
	binary.LittleEndian.PutUint16(s6[1:], uint16(v1>>32))
	binary.LittleEndian.PutUint16(s7[1:], uint16(v1>>48))
	binary.LittleEndian.PutUint16(s8[1:], uint16(v2))
	binary.LittleEndian.PutUint16(s9[1:], uint16(v2>>16))
	binary.LittleEndian.PutUint16(s10[1:], uint16(v2>>32))
	binary.LittleEndian.PutUint16(s11[1:], uint16(v2>>48))
	binary.LittleEndian.PutUint16(s12[1:], uint16(v3))
	binary.LittleEndian.PutUint16(s13[1:], uint16(v3>>16))
	binary.LittleEndian.PutUint16(s14[1:], uint16(v3>>32))
	binary.LittleEndian.PutUint16(s15[1:], uint16(v3>>48))
	return true
}
