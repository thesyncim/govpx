//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
)

// loopFilterFlat16Eligible reports whether a 16-position VP9 flat filter
// (kind 8 or 16) can use loopFilterFlat16 for this geometry.
func loopFilterFlat16Eligible(step, outer, n, kind int) bool {
	if (kind != 8 && kind != 16) || n < 16 || n%16 != 0 {
		return false
	}
	// Vertical edges (step == 1) transpose two 8-row tiles and measured
	// slower than the 8-lane path (join/split of the tiles outweighs the
	// wider arithmetic), so only horizontal edges take this kernel.
	return outer == 1 && step >= 16
}

// loopFilterFlatWide filters n (a multiple of 16) edge positions with the
// VP9 flat filters, sixteen positions per AVX2 vector. It matches the
// 8-lane path exactly: a half whose lanes need no flat filtering still goes
// through the byte-domain narrow filter, and a flat half is never filtered
// twice.
func loopFilterFlatWide(p []byte, q0, step, outer, n, blimit, limit, thresh, kind int) {
	defer archsimd.ClearAVXUpperBits()
	for base := 0; base < n; base += 16 {
		off := q0 + base*outer
		half := off + 8*outer
		if loopFilterNonFlat(p, off, step, outer, blimit, limit, thresh) {
			if !loopFilterNonFlat(p, half, step, outer, blimit, limit, thresh) {
				loopFilter8(p, half, step, outer, blimit, limit, thresh, kind)
			}
			continue
		}
		loopFilterFlat16(p, off, step, outer, blimit, limit, thresh, kind)
		archsimd.ClearAVXUpperBits()
	}
}

func any16w(m archsimd.Mask16x16) bool { return anyWide16(m) }
func all16w(m archsimd.Mask16x16) bool {
	return !anyWide16(m.ToInt16x16().Equal(archsimd.Int16x16{}))
}
func maskDiff16w(a, b archsimd.Mask16x16) bool {
	return !a.ToInt16x16().Xor(b.ToInt16x16()).IsZero()
}

func loadWide16(p []byte, off, step, outer int) archsimd.Int16x16 {
	// Only contiguous horizontal samples reach here.
	return edgeLoad16(p, off, outer)
}

func storeWide16(p []byte, off int, v archsimd.Int16x16) {
	wideBytes(packBytes(v.GetLo()), packBytes(v.GetHi())).Store(p[off:])
}

// loopFilterFlat16 is loopFilter8 for sixteen contiguous positions of a
// horizontal edge (outer == 1; kinds 8 and 16).
func loopFilterFlat16(p []byte, off, step, outer, blimit, limit, thresh, kind int) {
	var a, b [16]archsimd.Int16x16
	const first0, last0 = 4, 12
	for i := first0; i < last0; i++ {
		a[i] = loadWide16(p, off+(i-8)*step, step, outer)
	}
	zero := archsimd.Int16x16{}
	if kind == 8 {
		same := a[4].Xor(a[5])
		for i := 6; i < 12; i++ {
			same = same.Or(a[4].Xor(a[i]))
		}
		if !any16w(same.NotEqual(zero)) {
			return
		}
	}
	for i := first0; i < last0; i++ {
		b[i] = a[i]
	}
	p1, p0, q0, q1 := a[6], a[7], a[8], a[9]
	bc := func(v int) archsimd.Int16x16 { return archsimd.BroadcastInt16x16(int16(v)) }
	boundary := p0.Sub(q0).Abs().ShiftAllLeft(1).Add(p1.Sub(q1).Abs().ShiftAllRight(1))
	need := boundary.LessEqual(bc(blimit))
	maxdiff := a[4].Sub(a[5]).Abs().Max(a[5].Sub(p1).Abs()).Max(p1.Sub(p0).Abs()).Max(q1.Sub(q0).Abs()).Max(a[10].Sub(q1).Abs()).Max(a[11].Sub(a[10]).Abs())
	need = need.And(maxdiff.LessEqual(bc(limit)))
	if !any16w(need) {
		return
	}
	one := bc(1)
	flat := a[4].Sub(p0).Abs().Max(a[5].Sub(p0).Abs()).Max(p1.Sub(p0).Abs()).Max(q1.Sub(q0).Abs()).Max(a[10].Sub(q0).Abs()).Max(a[11].Sub(q0).Abs()).LessEqual(one).And(need)
	hasFlat := any16w(flat)
	var flat2 archsimd.Mask16x16
	hasFlat2 := false
	if kind == 16 && hasFlat {
		for i := range 4 {
			a[i] = loadWide16(p, off+(i-8)*step, step, outer)
			a[12+i] = loadWide16(p, off+(i+4)*step, step, outer)
		}
		far := zero
		for i := range 4 {
			far = far.Max(a[i].Sub(p0).Abs()).Max(a[12+i].Sub(q0).Abs())
		}
		flat2 = flat.And(far.LessEqual(one))
		hasFlat2 = any16w(flat2)
		if hasFlat2 && !all16w(flat2) {
			b[1], b[2], b[3] = a[1], a[2], a[3]
			b[12], b[13], b[14] = a[12], a[13], a[14]
		}
	}
	if !hasFlat || maskDiff16w(need, flat) {
		hev := p1.Sub(p0).Abs().Max(q1.Sub(q0).Abs()).Greater(bc(thresh))
		center := bc(128)
		three := bc(3)
		four := bc(4)
		f := lfClamp16(p1.Sub(q1)).Masked(hev)
		f = lfClamp16(f.Add(q0.Sub(p0).Mul(three))).Masked(need)
		f1 := lfClamp16(f.Add(four)).ShiftAllRight(3)
		f2 := lfClamp16(f.Add(three)).ShiftAllRight(3)
		b[7] = lfClamp16(p0.Sub(center).Add(f2)).Add(center)
		b[8] = lfClamp16(q0.Sub(center).Sub(f1)).Add(center)
		u := f1.Add(one).ShiftAllRight(1).Masked(hev.ToInt16x16().Equal(zero))
		b[6] = lfClamp16(p1.Sub(center).Add(u)).Add(center)
		b[9] = lfClamp16(q1.Sub(center).Sub(u)).Add(center)
	}
	first, last := 6, 10
	if hasFlat {
		if !hasFlat2 || maskDiff16w(flat, flat2) {
			flatFilter7w(&a, &b, flat)
		}
		first, last = 5, 11
		if hasFlat2 {
			flatFilter15w(&a, &b, flat2)
			first, last = 1, 15
		}
	}
	for i := first; i < last; i++ {
		storeWide16(p, off+(i-8)*step, b[i])
	}
}

// flatFilter7w is flatFilter7 over sixteen lanes.
func flatFilter7w(a, b *[16]archsimd.Int16x16, mask archsimd.Mask16x16) {
	a4, a5, a6, a7 := a[4], a[5], a[6], a[7]
	a8, a9, a10, a11 := a[8], a[9], a[10], a[11]
	round := archsimd.BroadcastInt16x16(4)
	sum := a4.ShiftAllLeft(1).Add(a4).Add(a5).Add(a6).Add(a7).Add(a8)
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

// flatFilter15w is flatFilter15 over sixteen lanes.
func flatFilter15w(a, b *[16]archsimd.Int16x16, mask archsimd.Mask16x16) {
	round := archsimd.BroadcastInt16x16(8)
	a0 := a[0]
	sum := a0.ShiftAllLeft(3).Sub(a0)
	for i := 1; i <= 8; i++ {
		sum = sum.Add(a[i])
	}
	a15 := a[15]
	for i := 1; i <= 14; i++ {
		// sum holds the window centered on i before the tap entering/leaving.
		b[i] = sum.Add(a[i]).Add(round).ShiftAllRight(4).IfElse(mask, b[i])
		if i < 8 {
			sum = sum.Sub(a0).Add(a[i+8])
		} else {
			sum = sum.Sub(a[i-7]).Add(a15)
		}
	}
}
