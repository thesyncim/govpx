//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// LoopFilterInternal16 filters the three four-pixel-spaced internal edges
// of a 16x16 tile. step crosses the edges; outer walks along them. Edges
// retain their original order because each can read the preceding result.
func LoopFilterInternal16(p []byte, step, outer, blimit, limit, thresh int) bool {
	if !Enabled || step <= 0 || outer <= 0 || blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 {
		return false
	}
	if !((outer == 1 && step >= 16) || (step == 1 && outer >= 8)) {
		return false
	}
	// Three complete eight-row/column windows occupy the exact 16x16 tile.
	// Divide before multiplying so oversized strides cannot wrap the tile.
	if len(p) == 0 {
		return false
	}
	span := (len(p) - 1) / 15
	if step > span || outer > span-step {
		return false
	}
	if internalFused {
		if outer == 1 {
			loopFilterInternalFusedHorizontal(p, step, blimit, limit, thresh)
		} else {
			loopFilterInternalFusedVertical(p, outer, blimit, limit, thresh)
		}
		return true
	}
	if internalVerticalGeneric && step == 1 {
		loopFilterBytes(p, 4, 1, outer, 16, blimit, limit, thresh, 4)
		loopFilterBytes(p, 8, 1, outer, 16, blimit, limit, thresh, 4)
		loopFilterBytes(p, 12, 1, outer, 16, blimit, limit, thresh, 4)
		return true
	}
	loopFilterInternalEdge(p, 4*step, step, outer, blimit, limit, thresh)
	loopFilterInternalEdge(p, 8*step, step, outer, blimit, limit, thresh)
	loopFilterInternalEdge(p, 12*step, step, outer, blimit, limit, thresh)
	return true
}

// loopFilterNonFlat uses the byte-domain narrow filter only when no lane
// requires flat filtering. It never writes a rejected packet.
func loopFilterNonFlat(p []byte, off, step, outer, blimit, limit, thresh int) bool {
	if blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 || !((outer == 1 && step >= 8) || (step == 1 && outer >= 8)) {
		return false
	}
	if off < 0 || off >= len(p) || step > off/4 {
		return false
	}
	room := len(p) - 1 - off
	if step > room/3 || outer > (room-3*step)/7 {
		return false
	}
	return loopFilterNonFlatBytes(p, off, step, outer, blimit, limit, thresh)
}

// LoopFilter is the shared 8-bit libvpx deblocker. kind is 2 (VP8 simple),
// 4 (VP8/VP9 narrow), 6 (VP8 macroblock), 8 or 16 (VP9 flat filters).
// Lanes span eight independent edge positions; step crosses the edge and
// outer walks along it. All centering and signed saturation follow libvpx.
func LoopFilter(p []byte, q0, step, outer, n, blimit, limit, thresh, kind int) bool {
	if !Enabled {
		return false
	}
	if loopFilterWide(p, q0, step, outer, n, blimit, limit, thresh, kind) {
		return true
	}
	if kind == 2 || kind == 4 || kind == 6 {
		for base := 0; base < n; base += 8 {
			loopFilterNarrow8(p, q0+base*outer, step, outer, blimit, limit, thresh, kind)
		}
		return true
	}
	for base := 0; base < n; base += 8 {
		if !loopFilterNonFlat(p, q0+base*outer, step, outer, blimit, limit, thresh) {
			loopFilter8(p, q0+base*outer, step, outer, blimit, limit, thresh, kind)
		}
	}
	return true
}
func edgeLoad(p []byte, off, outer int) archsimd.Int16x8 {
	if outer == 1 {
		return widen8(load8(p[off:]))
	}
	var b [16]byte
	for i := range 8 {
		b[i] = p[off+i*outer]
	}
	return widen8(archsimd.LoadUint8x16(b[:]))
}
func edgeStore(p []byte, off, outer int, v archsimd.Int16x8) {
	if outer == 1 {
		storeBytes8(p[off:], packBytes(v))
		return
	}
	var b [16]byte
	packBytes(v).Store(b[:])
	for i := range 8 {
		p[off+i*outer] = b[i]
	}
}

// transposeBytes8 keeps eight strided edge positions in registers. Each
// input/output uses its low eight bytes; transposing twice is the identity.
func transposeBytes8(v0, v1, v2, v3, v4, v5, v6, v7 archsimd.Uint8x16) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	a0 := zipBytesLo(v0, v1)
	a1 := zipBytesLo(v2, v3)
	a2 := zipBytesLo(v4, v5)
	a3 := zipBytesLo(v6, v7)
	b0 := a0.InterleaveLo(a1).ReshapeToUint32s()
	b1 := a0.InterleaveHi(a1).ReshapeToUint32s()
	b2 := a2.InterleaveLo(a3).ReshapeToUint32s()
	b3 := a2.InterleaveHi(a3).ReshapeToUint32s()
	c01 := b0.InterleaveLo(b2).ReshapeToUint8s()
	c23 := b0.InterleaveHi(b2).ReshapeToUint8s()
	c45 := b1.InterleaveLo(b3).ReshapeToUint8s()
	c67 := b1.InterleaveHi(b3).ReshapeToUint8s()
	return c01, highBytes(c01), c23, highBytes(c23), c45, highBytes(c45), c67, highBytes(c67)
}
func loadEdgeRows(p []byte, off, pitch, width int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	// loadN reads exactly width bytes, including on the final row. The slice
	// checks prove the complete tile before any vector read.
	row := func(y int) archsimd.Uint8x16 {
		start := off + y*pitch
		return loadN(p[start:start+width], width)
	}
	return transposeBytes8(row(0), row(1), row(2), row(3), row(4), row(5), row(6), row(7))
}
func loadEdgeTile(p []byte, off, pitch, width int, a *[16]archsimd.Int16x8, first int) {
	c0, c1, c2, c3, c4, c5, c6, c7 := loadEdgeRows(p, off, pitch, width)
	a[first], a[first+1], a[first+2], a[first+3] = widen8(c0), widen8(c1), widen8(c2), widen8(c3)
	if width == 8 {
		a[first+4], a[first+5], a[first+6], a[first+7] = widen8(c4), widen8(c5), widen8(c6), widen8(c7)
	}
}
func storeEdgeTile(p []byte, off, pitch, tileFirst, width, first, last int, b *[16]archsimd.Int16x8) {
	c0, c1, c2, c3 := packBytes(b[tileFirst]), packBytes(b[tileFirst+1]), packBytes(b[tileFirst+2]), packBytes(b[tileFirst+3])
	c4, c5, c6, c7 := archsimd.Uint8x16{}, archsimd.Uint8x16{}, archsimd.Uint8x16{}, archsimd.Uint8x16{}
	if width == 8 {
		c4, c5, c6, c7 = packBytes(b[tileFirst+4]), packBytes(b[tileFirst+5]), packBytes(b[tileFirst+6]), packBytes(b[tileFirst+7])
	}
	r0, r1, r2, r3, r4, r5, r6, r7 := transposeBytes8(c0, c1, c2, c3, c4, c5, c6, c7)
	rows := [8]archsimd.Uint8x16{r0, r1, r2, r3, r4, r5, r6, r7}
	// Only write columns that the original filter writes. Rewriting unchanged
	// neighbors would introduce conflicting accesses in parallel row filtering.
	for y := range 8 {
		start := off + y*pitch + first - tileFirst
		out := p[start : start+last-first]
		bits := rows[y].ReshapeToUint64s().GetElem(0) >> uint(8*(first-tileFirst))
		switch last - first {
		case 2:
			binary.LittleEndian.PutUint16(out, uint16(bits))
		case 4:
			binary.LittleEndian.PutUint32(out, uint32(bits))
		case 6:
			binary.LittleEndian.PutUint32(out, uint32(bits))
			binary.LittleEndian.PutUint16(out[4:], uint16(bits>>32))
		case 7:
			binary.LittleEndian.PutUint32(out, uint32(bits))
			binary.LittleEndian.PutUint16(out[4:], uint16(bits>>32))
			out[6] = byte(bits >> 48)
		}
	}
}
func loadFarEdgeTiles(p []byte, off, pitch int, a *[16]archsimd.Int16x8) {
	// Pack both 4-column far strips (p7..p4 at off-8, q4..q7 at off+4) into
	// one 8-byte row per scanline so a single 8x8 transpose produces all eight
	// far columns in registers instead of two separate transposes.
	row := func(y int) archsimd.Uint8x16 {
		base := off + y*pitch
		lo := uint64(binary.LittleEndian.Uint32(p[base-8 : base-4]))
		hi := uint64(binary.LittleEndian.Uint32(p[base+4 : base+8]))
		return archsimd.Uint64x2{}.SetElem(0, lo|(hi<<32)).ReshapeToUint8s()
	}
	c0, c1, c2, c3, c4, c5, c6, c7 := transposeBytes8(row(0), row(1), row(2), row(3), row(4), row(5), row(6), row(7))
	a[0], a[1], a[2], a[3] = widen8(c0), widen8(c1), widen8(c2), widen8(c3)
	a[12], a[13], a[14], a[15] = widen8(c4), widen8(c5), widen8(c6), widen8(c7)
}
func storeEdgeTile14(p []byte, off, pitch int, b *[16]archsimd.Int16x8) {
	// The 15-tap vertical filter writes 14 contiguous columns p6..q6 (indices
	// 1..14) per row without touching p7 or q7. Two overlapping 8-byte stores
	// cover [off-7, off+7) per row in 2 stores instead of 6.
	z := archsimd.Uint8x16{}
	l0, l1, l2, l3, l4, l5, l6, l7 := transposeBytes8(
		z, packBytes(b[1]), packBytes(b[2]), packBytes(b[3]),
		packBytes(b[4]), packBytes(b[5]), packBytes(b[6]), packBytes(b[7]),
	)
	r0, r1, r2, r3, r4, r5, r6, r7 := transposeBytes8(
		packBytes(b[8]), packBytes(b[9]), packBytes(b[10]), packBytes(b[11]),
		packBytes(b[12]), packBytes(b[13]), packBytes(b[14]), z,
	)
	left := [8]archsimd.Uint8x16{l0, l1, l2, l3, l4, l5, l6, l7}
	right := [8]archsimd.Uint8x16{r0, r1, r2, r3, r4, r5, r6, r7}
	for y := range 8 {
		L := left[y].ReshapeToUint64s().GetElem(0)
		R := right[y].ReshapeToUint64s().GetElem(0)
		out := p[off-7+y*pitch : off+7+y*pitch]
		binary.LittleEndian.PutUint64(out[0:8], (L>>8)|(R<<56))
		binary.LittleEndian.PutUint64(out[6:14], (L>>56)|(R<<8))
	}
}
func any16(m archsimd.Mask16x8) bool {
	v := m.ToInt16x8().ToBits().ReshapeToUint64s()
	return v.GetElem(0)|v.GetElem(1) != 0
}
func all16(m archsimd.Mask16x8) bool {
	v := m.ToInt16x8().ToBits().ReshapeToUint64s()
	return v.GetElem(0)&v.GetElem(1) == ^uint64(0)
}
func maskDiff16(a, b archsimd.Mask16x8) bool {
	x := a.ToInt16x8().Xor(b.ToInt16x8()).ToBits().ReshapeToUint64s()
	return x.GetElem(0)|x.GetElem(1) != 0
}
func lfClamp(v archsimd.Int16x8) archsimd.Int16x8 {
	return v.Max(archsimd.BroadcastInt16x8(-128)).Min(archsimd.BroadcastInt16x8(127))
}
func loopFilter8(p []byte, off, step, outer, blimit, limit, thresh, kind int) {
	var a, b [16]archsimd.Int16x8
	// The narrow window always has p1,p0,q0,q1. Wider masks also inspect
	// p3,p2,q2,q3, while the flat16 samples are loaded only after the mask.
	first, last := 4, 12
	if kind == 2 {
		first, last = 6, 10
	}
	tileFirst, tileWidth := first, last-first
	tiled := step == 1 && outer > 1
	if tiled {
		loadEdgeTile(p, off+first-8, outer, tileWidth, &a, first)
	} else {
		for i := first; i < last; i++ {
			a[i] = edgeLoad(p, off+(i-8)*step, outer)
		}
	}
	if kind == 8 {
		same := a[4].Xor(a[5])
		for i := 6; i < 12; i++ {
			same = same.Or(a[4].Xor(a[i]))
		}
		if !any16(same.NotEqual(archsimd.Int16x8{})) {
			return
		}
	}
	for i := first; i < last; i++ {
		b[i] = a[i]
	}
	p1, p0, q0, q1 := a[6], a[7], a[8], a[9]
	boundary := p0.Sub(q0).Abs().ShiftAllLeft(1).Add(p1.Sub(q1).Abs().ShiftAllRight(1))
	need := boundary.LessEqual(archsimd.BroadcastInt16x8(int16(blimit)))
	if kind != 2 {
		maxdiff := a[4].Sub(a[5]).Abs().Max(a[5].Sub(p1).Abs()).Max(p1.Sub(p0).Abs()).Max(q1.Sub(q0).Abs()).Max(a[10].Sub(q1).Abs()).Max(a[11].Sub(a[10]).Abs())
		need = need.And(maxdiff.LessEqual(archsimd.BroadcastInt16x8(int16(limit))))
	}
	if !any16(need) {
		return
	}
	one := archsimd.BroadcastInt16x8(1)
	if kind == 8 || kind == 16 {
		flat := a[4].Sub(p0).Abs().Max(a[5].Sub(p0).Abs()).Max(p1.Sub(p0).Abs()).Max(q1.Sub(q0).Abs()).Max(a[10].Sub(q0).Abs()).Max(a[11].Sub(q0).Abs()).LessEqual(one).And(need)
		hasFlat := any16(flat)
		var flat2 archsimd.Mask16x8
		hasFlat2 := false
		if kind == 16 && hasFlat {
			far := archsimd.Int16x8{}
			if tiled {
				loadFarEdgeTiles(p, off, outer, &a)
			} else {
				for i := range 4 {
					a[i] = edgeLoad(p, off+(i-8)*step, outer)
					a[12+i] = edgeLoad(p, off+(i+4)*step, outer)
				}
			}
			for i := range 4 {
				far = far.Max(a[i].Sub(p0).Abs()).Max(a[12+i].Sub(q0).Abs())
			}
			flat2 = flat.And(far.LessEqual(one))
			hasFlat2 = any16(flat2)
			if hasFlat2 && !all16(flat2) {
				b[1], b[2], b[3] = a[1], a[2], a[3]
				b[12], b[13], b[14] = a[12], a[13], a[14]
			}
		}
		if !hasFlat || maskDiff16(need, flat) {
			hev := p1.Sub(p0).Abs().Max(q1.Sub(q0).Abs()).Greater(archsimd.BroadcastInt16x8(int16(thresh)))
			center := archsimd.BroadcastInt16x8(128)
			three := archsimd.BroadcastInt16x8(3)
			four := archsimd.BroadcastInt16x8(4)
			f := lfClamp(p1.Sub(q1)).Masked(hev)
			f = lfClamp(f.Add(q0.Sub(p0).Mul(three))).Masked(need)
			f1 := lfClamp(f.Add(four)).ShiftAllRight(3)
			f2 := lfClamp(f.Add(three)).ShiftAllRight(3)
			b[7] = lfClamp(p0.Sub(center).Add(f2)).Add(center)
			b[8] = lfClamp(q0.Sub(center).Sub(f1)).Add(center)
			u := f1.Add(one).ShiftAllRight(1).Masked(hev.ToInt16x8().Equal(archsimd.Int16x8{}))
			b[6] = lfClamp(p1.Sub(center).Add(u)).Add(center)
			b[9] = lfClamp(q1.Sub(center).Sub(u)).Add(center)
		}
		first, last = 6, 10
		if hasFlat {
			if !hasFlat2 || maskDiff16(flat, flat2) {
				flatFilter7(&a, &b, flat)
			}
			first, last = 5, 11
			if hasFlat2 {
				flatFilter15(&a, &b, flat2)
				first, last = 1, 15
			}
		}
	} else {
		hev := p1.Sub(p0).Abs().Max(q1.Sub(q0).Abs()).Greater(archsimd.BroadcastInt16x8(int16(thresh)))
		center := archsimd.BroadcastInt16x8(128)
		three := archsimd.BroadcastInt16x8(3)
		four := archsimd.BroadcastInt16x8(4)
		f := lfClamp(p1.Sub(q1))
		if kind != 2 && kind != 6 {
			f = f.Masked(hev)
		}
		f = lfClamp(f.Add(q0.Sub(p0).Mul(three))).Masked(need)
		if kind == 6 {
			fh := f.Masked(hev)
			f1 := lfClamp(fh.Add(four)).ShiftAllRight(3)
			f2 := lfClamp(fh.Add(three)).ShiftAllRight(3)
			ps0 := lfClamp(p0.Sub(center).Add(f2))
			qs0 := lfClamp(q0.Sub(center).Sub(f1))
			ff := f.Masked(hev.ToInt16x8().Equal(archsimd.Int16x8{}))
			bias := archsimd.BroadcastInt16x8(63)
			u := ff.Mul(archsimd.BroadcastInt16x8(27)).Add(bias).ShiftAllRight(7)
			b[7] = lfClamp(ps0.Add(u)).Add(center)
			b[8] = lfClamp(qs0.Sub(u)).Add(center)
			u = ff.Mul(archsimd.BroadcastInt16x8(18)).Add(bias).ShiftAllRight(7)
			b[6] = lfClamp(p1.Sub(center).Add(u)).Add(center)
			b[9] = lfClamp(q1.Sub(center).Sub(u)).Add(center)
			u = ff.Mul(archsimd.BroadcastInt16x8(9)).Add(bias).ShiftAllRight(7)
			b[5] = lfClamp(a[5].Sub(center).Add(u)).Add(center)
			b[10] = lfClamp(a[10].Sub(center).Sub(u)).Add(center)
			first, last = 5, 11
		} else {
			f1 := lfClamp(f.Add(four)).ShiftAllRight(3)
			f2 := lfClamp(f.Add(three)).ShiftAllRight(3)
			b[7] = lfClamp(p0.Sub(center).Add(f2)).Add(center)
			b[8] = lfClamp(q0.Sub(center).Sub(f1)).Add(center)
			first, last = 7, 9
			if kind != 2 {
				u := f1.Add(one).ShiftAllRight(1).Masked(hev.ToInt16x8().Equal(archsimd.Int16x8{}))
				b[6] = lfClamp(p1.Sub(center).Add(u)).Add(center)
				b[9] = lfClamp(q1.Sub(center).Sub(u)).Add(center)
				first, last = 6, 10
			}
		}
	}
	if tiled && first >= tileFirst && last <= tileFirst+tileWidth {
		storeEdgeTile(p, off+tileFirst-8, outer, tileFirst, tileWidth, first, last, &b)
	} else if tiled && first == 1 && last == 15 {
		storeEdgeTile14(p, off, outer, &b)
	} else {
		for i := first; i < last; i++ {
			edgeStore(p, off+(i-8)*step, outer, b[i])
		}
	}
}

func flatFilter(a, b *[16]archsimd.Int16x8, start, end, radius int, mask archsimd.Mask16x8) {
	if radius == 3 {
		flatFilter7(a, b, mask)
		return
	}
	flatFilter15(a, b, mask)
}

// flatFilter7 computes the 7-tap flat filter over columns 5..10 using
// constant-indexed sliding sums (zero bounds checks or min/max branches).
func flatFilter7(a, b *[16]archsimd.Int16x8, mask archsimd.Mask16x8) {
	a4, a5, a6, a7 := a[4], a[5], a[6], a[7]
	a8, a9, a10, a11 := a[8], a[9], a[10], a[11]
	round := archsimd.BroadcastInt16x8(4)
	sum := a4.ShiftAllLeft(1).Add(a4).Add(a5).Add(a6).Add(a7).Add(a8)
	if all16(mask) {
		b[5] = sum.Add(a5).Add(round).ShiftAllRight(3)
		sum = sum.Sub(a4).Add(a9)
		b[6] = sum.Add(a6).Add(round).ShiftAllRight(3)
		sum = sum.Sub(a4).Add(a10)
		b[7] = sum.Add(a7).Add(round).ShiftAllRight(3)
		sum = sum.Sub(a4).Add(a11)
		b[8] = sum.Add(a8).Add(round).ShiftAllRight(3)
		sum = sum.Sub(a5).Add(a11)
		b[9] = sum.Add(a9).Add(round).ShiftAllRight(3)
		sum = sum.Sub(a6).Add(a11)
		b[10] = sum.Add(a10).Add(round).ShiftAllRight(3)
		return
	}
	b[5] = sum.Add(a5).Add(round).ShiftAllRight(3).IfElse(mask, b[5])
	sum = sum.Sub(a4).Add(a9)
	b[6] = sum.Add(a6).Add(round).ShiftAllRight(3).IfElse(mask, b[6])
	sum = sum.Sub(a4).Add(a10)
	b[7] = sum.Add(a7).Add(round).ShiftAllRight(3).IfElse(mask, b[7])
	sum = sum.Sub(a4).Add(a11)
	b[8] = sum.Add(a8).Add(round).ShiftAllRight(3).IfElse(mask, b[8])
	sum = sum.Sub(a5).Add(a11)
	b[9] = sum.Add(a9).Add(round).ShiftAllRight(3).IfElse(mask, b[9])
	sum = sum.Sub(a6).Add(a11)
	b[10] = sum.Add(a10).Add(round).ShiftAllRight(3).IfElse(mask, b[10])
}

// flatFilter15 computes the 15-tap flat2 filter over columns 1..14 using
// constant-indexed sliding sums with a fast path when all 8 lanes take flat2.
func flatFilter15(a, b *[16]archsimd.Int16x8, mask archsimd.Mask16x8) {
	a0, a1, a2, a3 := a[0], a[1], a[2], a[3]
	a4, a5, a6, a7 := a[4], a[5], a[6], a[7]
	a8, a9, a10, a11 := a[8], a[9], a[10], a[11]
	a12, a13, a14, a15 := a[12], a[13], a[14], a[15]
	round := archsimd.BroadcastInt16x8(8)
	// Initial sum for i=1: 7*a[0] + a[1] + ... + a[8].
	sum := a0.ShiftAllLeft(3).Sub(a0).Add(a1).Add(a2).Add(a3).Add(a4).Add(a5).Add(a6).Add(a7).Add(a8)
	if all16(mask) {
		b[1] = sum.Add(a1).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a0).Add(a9)
		b[2] = sum.Add(a2).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a0).Add(a10)
		b[3] = sum.Add(a3).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a0).Add(a11)
		b[4] = sum.Add(a4).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a0).Add(a12)
		b[5] = sum.Add(a5).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a0).Add(a13)
		b[6] = sum.Add(a6).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a0).Add(a14)
		b[7] = sum.Add(a7).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a0).Add(a15)
		b[8] = sum.Add(a8).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a1).Add(a15)
		b[9] = sum.Add(a9).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a2).Add(a15)
		b[10] = sum.Add(a10).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a3).Add(a15)
		b[11] = sum.Add(a11).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a4).Add(a15)
		b[12] = sum.Add(a12).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a5).Add(a15)
		b[13] = sum.Add(a13).Add(round).ShiftAllRight(4)
		sum = sum.Sub(a6).Add(a15)
		b[14] = sum.Add(a14).Add(round).ShiftAllRight(4)
		return
	}
	b[1] = sum.Add(a1).Add(round).ShiftAllRight(4).IfElse(mask, b[1])
	sum = sum.Sub(a0).Add(a9)
	b[2] = sum.Add(a2).Add(round).ShiftAllRight(4).IfElse(mask, b[2])
	sum = sum.Sub(a0).Add(a10)
	b[3] = sum.Add(a3).Add(round).ShiftAllRight(4).IfElse(mask, b[3])
	sum = sum.Sub(a0).Add(a11)
	b[4] = sum.Add(a4).Add(round).ShiftAllRight(4).IfElse(mask, b[4])
	sum = sum.Sub(a0).Add(a12)
	b[5] = sum.Add(a5).Add(round).ShiftAllRight(4).IfElse(mask, b[5])
	sum = sum.Sub(a0).Add(a13)
	b[6] = sum.Add(a6).Add(round).ShiftAllRight(4).IfElse(mask, b[6])
	sum = sum.Sub(a0).Add(a14)
	b[7] = sum.Add(a7).Add(round).ShiftAllRight(4).IfElse(mask, b[7])
	sum = sum.Sub(a0).Add(a15)
	b[8] = sum.Add(a8).Add(round).ShiftAllRight(4).IfElse(mask, b[8])
	sum = sum.Sub(a1).Add(a15)
	b[9] = sum.Add(a9).Add(round).ShiftAllRight(4).IfElse(mask, b[9])
	sum = sum.Sub(a2).Add(a15)
	b[10] = sum.Add(a10).Add(round).ShiftAllRight(4).IfElse(mask, b[10])
	sum = sum.Sub(a3).Add(a15)
	b[11] = sum.Add(a11).Add(round).ShiftAllRight(4).IfElse(mask, b[11])
	sum = sum.Sub(a4).Add(a15)
	b[12] = sum.Add(a12).Add(round).ShiftAllRight(4).IfElse(mask, b[12])
	sum = sum.Sub(a5).Add(a15)
	b[13] = sum.Add(a13).Add(round).ShiftAllRight(4).IfElse(mask, b[13])
	sum = sum.Sub(a6).Add(a15)
	b[14] = sum.Add(a14).Add(round).ShiftAllRight(4).IfElse(mask, b[14])
}

// LoopFilterUV8 joins two disjoint eight-position chroma edges into one
// sixteen-lane packet. Aliased planes retain sequential scalar/native calls.
func LoopFilterUV8(u, v []byte, step, outer, blimit, limit, thresh, kind int) bool {
	if !Enabled || (kind != 4 && kind != 6) || blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 {
		return false
	}
	if !((outer == 1 && step >= 8) || (step == 1 && outer >= 8)) {
		return false
	}
	// An eight-by-eight tile ends at 7*pitch+7.
	pitch := max(step, outer)
	if uint(pitch) >= 1<<28 || 7*pitch+8 > min(len(u), len(v)) {
		return false
	}
	if slicesOverlap(u, v, len(u), len(v)) {
		return false
	}
	loopFilterUVBytes(u, v, 4*step, step, outer, blimit, limit, thresh, kind)
	return true
}
func loadLFUVByte(u, v []byte, off int) archsimd.Uint8x16 {
	return joinLFBytes(loadLFByte(u, off, 8), loadLFByte(v, off, 8))
}
func loadLFUVColumns(u, v []byte, off, pitch int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	a0, a1, a2, a3, a4, a5, a6, a7 := loadLFColumnTile[[8]byte](u, off, pitch)
	b0, b1, b2, b3, b4, b5, b6, b7 := loadLFColumnTile[[8]byte](v, off, pitch)
	return joinLFBytes(a0, b0), joinLFBytes(a1, b1), joinLFBytes(a2, b2), joinLFBytes(a3, b3), joinLFBytes(a4, b4), joinLFBytes(a5, b5), joinLFBytes(a6, b6), joinLFBytes(a7, b7)
}
func storeLFUV(u, v []byte, off, step, outer, count int, a, b, c, d, e, f archsimd.Uint8x16) {
	if outer == 1 {
		store := func(at int, x archsimd.Uint8x16) {
			words := x.ReshapeToUint64s()
			binary.LittleEndian.PutUint64(u[at:], words.GetElem(0))
			binary.LittleEndian.PutUint64(v[at:], words.GetElem(1))
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
	storeLFByteTile(u, off, outer, count, a, b, c, d, e, f)
	storeLFByteTile(v, off, outer, count, highBytes(a), highBytes(b), highBytes(c), highBytes(d), highBytes(e), highBytes(f))
}
