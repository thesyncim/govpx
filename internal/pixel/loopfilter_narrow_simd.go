//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Narrow filters keep samples in named registers. The flat VP9 filter needs
// two 16-vector arrays, but reserving and clearing those on every VP8 edge
// dominates its loop-filter workload.
func loopFilterNarrow8(p []byte, off, step, outer, blimit, limit, thresh, kind int) {
	var p3, p2, p1, p0, q0, q1, q2, q3 archsimd.Int16x8
	if step == 1 && outer > 1 {
		start, width := off-4, 8
		if kind == 2 {
			start, width = off-2, 4
		}
		c0, c1, c2, c3, c4, c5, c6, c7 := loadEdgeRows(p, start, outer, width)
		if kind == 2 {
			p1, p0, q0, q1 = widen8(c0), widen8(c1), widen8(c2), widen8(c3)
		} else {
			p3, p2, p1, p0, q0, q1, q2, q3 = widen8(c0), widen8(c1), widen8(c2), widen8(c3), widen8(c4), widen8(c5), widen8(c6), widen8(c7)
		}
	} else {
		p1, p0, q0, q1 = edgeLoad(p, off-2*step, outer), edgeLoad(p, off-step, outer), edgeLoad(p, off, outer), edgeLoad(p, off+step, outer)
		if kind != 2 {
			p3, p2, q2, q3 = edgeLoad(p, off-4*step, outer), edgeLoad(p, off-3*step, outer), edgeLoad(p, off+2*step, outer), edgeLoad(p, off+3*step, outer)
		}
	}
	boundary := p0.Sub(q0).Abs().ShiftAllLeft(1).Add(p1.Sub(q1).Abs().ShiftAllRight(1))
	need := boundary.LessEqual(archsimd.BroadcastInt16x8(int16(blimit)))
	if kind != 2 {
		delta := p3.Sub(p2).Abs().Max(p2.Sub(p1).Abs()).Max(p1.Sub(p0).Abs()).Max(q1.Sub(q0).Abs()).Max(q2.Sub(q1).Abs()).Max(q3.Sub(q2).Abs())
		need = need.And(delta.LessEqual(archsimd.BroadcastInt16x8(int16(limit))))
	}
	if !any16(need) {
		return
	}
	hev := p1.Sub(p0).Abs().Max(q1.Sub(q0).Abs()).Greater(archsimd.BroadcastInt16x8(int16(thresh)))
	center, three, four := archsimd.BroadcastInt16x8(128), archsimd.BroadcastInt16x8(3), archsimd.BroadcastInt16x8(4)
	f := lfClamp(p1.Sub(q1))
	if kind == 4 {
		f = f.Masked(hev)
	}
	f = lfClamp(f.Add(q0.Sub(p0).Mul(three))).Masked(need)
	zero := archsimd.Int16x8{}
	if kind == 6 {
		fh := f.Masked(hev)
		ps0 := lfClamp(p0.Sub(center).Add(lfShift3(lfClamp(fh.Add(three)))))
		qs0 := lfClamp(q0.Sub(center).Sub(lfShift3(lfClamp(fh.Add(four)))))
		ff := f.Masked(hev.ToInt16x8().Equal(zero))
		bias := archsimd.BroadcastInt16x8(63)
		u := filterShift7(ff.Mul(archsimd.BroadcastInt16x8(27)).Add(bias))
		o2, o3 := lfClamp(ps0.Add(u)).Add(center), lfClamp(qs0.Sub(u)).Add(center)
		u = filterShift7(ff.Mul(archsimd.BroadcastInt16x8(18)).Add(bias))
		o1, o4 := lfClamp(p1.Sub(center).Add(u)).Add(center), lfClamp(q1.Sub(center).Sub(u)).Add(center)
		u = filterShift7(ff.Mul(archsimd.BroadcastInt16x8(9)).Add(bias))
		o0, o5 := lfClamp(p2.Sub(center).Add(u)).Add(center), lfClamp(q2.Sub(center).Sub(u)).Add(center)
		storeNarrowEdge(p, off-3*step, step, outer, 6, o0, o1, o2, o3, o4, o5)
		return
	}
	f1, f2 := lfShift3(lfClamp(f.Add(four))), lfShift3(lfClamp(f.Add(three)))
	o0, o1 := lfClamp(p0.Sub(center).Add(f2)).Add(center), lfClamp(q0.Sub(center).Sub(f1)).Add(center)
	if kind == 2 {
		storeNarrowEdge(p, off-step, step, outer, 2, o0, o1, zero, zero, zero, zero)
	} else {
		u := lfShift1(f1.Add(archsimd.BroadcastInt16x8(1))).Masked(hev.ToInt16x8().Equal(zero))
		oP1, oQ1 := lfClamp(p1.Sub(center).Add(u)).Add(center), lfClamp(q1.Sub(center).Sub(u)).Add(center)
		storeNarrowEdge(p, off-2*step, step, outer, 4, oP1, o0, o1, oQ1, zero, zero)
	}
}
func storeNarrowEdge(p []byte, off, step, outer, count int, v0, v1, v2, v3, v4, v5 archsimd.Int16x8) {
	if step != 1 || outer == 1 {
		edgeStore(p, off, outer, v0)
		edgeStore(p, off+step, outer, v1)
		if count >= 4 {
			edgeStore(p, off+2*step, outer, v2)
			edgeStore(p, off+3*step, outer, v3)
		}
		if count == 6 {
			edgeStore(p, off+4*step, outer, v4)
			edgeStore(p, off+5*step, outer, v5)
		}
		return
	}
	z := archsimd.Uint8x16{}
	r0, r1, r2, r3, r4, r5, r6, r7 := transposeBytes8(packBytes(v0), packBytes(v1), packBytes(v2), packBytes(v3), packBytes(v4), packBytes(v5), z, z)
	// Exactly the original 2/4/6 modified columns are written; no neighboring
	// bytes are rewritten when parallel workers filter other row regions.
	rows := [8]uint64{r0.ReshapeToUint64s().GetElem(0), r1.ReshapeToUint64s().GetElem(0), r2.ReshapeToUint64s().GetElem(0), r3.ReshapeToUint64s().GetElem(0), r4.ReshapeToUint64s().GetElem(0), r5.ReshapeToUint64s().GetElem(0), r6.ReshapeToUint64s().GetElem(0), r7.ReshapeToUint64s().GetElem(0)}
	switch count {
	case 2:
		for y, v := range rows {
			binary.LittleEndian.PutUint16(p[off+y*outer:], uint16(v))
		}
	case 4:
		for y, v := range rows {
			binary.LittleEndian.PutUint32(p[off+y*outer:], uint32(v))
		}
	case 6:
		for y, v := range rows {
			row := p[off+y*outer : off+y*outer+6]
			binary.LittleEndian.PutUint32(row, uint32(v))
			binary.LittleEndian.PutUint16(row[4:], uint16(v>>32))
		}
	}
}
