//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

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
		}
	}
}
func any16(m archsimd.Mask16x8) bool {
	v := m.ToInt16x8().ToBits().ReshapeToUint64s()
	return v.GetElem(0)|v.GetElem(1) != 0
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
	hev := p1.Sub(p0).Abs().Max(q1.Sub(q0).Abs()).Greater(archsimd.BroadcastInt16x8(int16(thresh)))
	center := archsimd.BroadcastInt16x8(128)
	three := archsimd.BroadcastInt16x8(3)
	four := archsimd.BroadcastInt16x8(4)
	one := archsimd.BroadcastInt16x8(1)
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
	if kind == 8 || kind == 16 {
		flat := a[4].Sub(p0).Abs().Max(a[5].Sub(p0).Abs()).Max(p1.Sub(p0).Abs()).Max(q1.Sub(q0).Abs()).Max(a[10].Sub(q0).Abs()).Max(a[11].Sub(q0).Abs()).LessEqual(one).And(need)
		if any16(flat) {
			flatFilter(&a, &b, 4, 12, 3, flat)
			first, last = 5, 11
		}
		if kind == 16 && any16(flat) {
			far := archsimd.Int16x8{}
			for i := range 4 {
				a[i] = edgeLoad(p, off+(i-8)*step, outer)
				a[12+i] = edgeLoad(p, off+(i+4)*step, outer)
				b[i] = a[i]
				b[12+i] = a[12+i]
				far = far.Max(a[i].Sub(p0).Abs()).Max(a[12+i].Sub(q0).Abs())
			}
			flat2 := flat.And(far.LessEqual(one))
			if any16(flat2) {
				flatFilter(&a, &b, 0, 16, 7, flat2)
				first, last = 1, 15
			}
		}
	}
	if tiled && first >= tileFirst && last <= tileFirst+tileWidth {
		storeEdgeTile(p, off+tileFirst-8, outer, tileFirst, tileWidth, first, last, &b)
	} else {
		for i := first; i < last; i++ {
			edgeStore(p, off+(i-8)*step, outer, b[i])
		}
	}
}

// Flat filters have a doubled center and repeated endpoint samples. A
// sliding vector sum computes the 7/15 tap windows with two updates each.
func flatFilter(a, b *[16]archsimd.Int16x8, start, end, radius int, mask archsimd.Mask16x8) {
	sum := archsimd.Int16x8{}
	for j := start + 1 - radius; j <= start+1+radius; j++ {
		sum = sum.Add(a[min(max(j, start), end-1)])
	}
	shift := uint64(3)
	if radius == 7 {
		shift = 4
	}
	round := archsimd.BroadcastInt16x8(int16(1 << (shift - 1)))
	for i := start + 1; i < end-1; i++ {
		v := sum.Add(a[i]).Add(round).ShiftAllRight(shift)
		b[i] = v.IfElse(mask, b[i])
		sum = sum.Sub(a[max(i-radius, start)]).Add(a[min(i+radius+1, end-1)])
	}
}
