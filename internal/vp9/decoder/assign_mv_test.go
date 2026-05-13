package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestIsMvValid(t *testing.T) {
	cases := []struct {
		mv   MV
		want bool
	}{
		{MV{0, 0}, true},
		{MV{1, -1}, true},
		// MvUpp = (1 << 14) - 1 = 16383; valid range is (-16384, 16383) exclusive.
		{MV{int16(MvUpp - 1), int16(MvUpp - 1)}, true},
		{MV{int16(MvUpp), 0}, false},
		{MV{int16(MvLow), 0}, false},
		{MV{int16(MvLow + 1), 0}, true},
	}
	for i, c := range cases {
		if got := IsMvValid(c.mv); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

func TestCopyMvPair(t *testing.T) {
	src := [2]MV{{1, 2}, {3, 4}}
	var dst [2]MV
	CopyMvPair(&dst, &src)
	if dst != src {
		t.Errorf("got %v, want %v", dst, src)
	}
}

func TestZeroMvPair(t *testing.T) {
	dst := [2]MV{{1, 2}, {3, 4}}
	ZeroMvPair(&dst)
	if dst != ([2]MV{}) {
		t.Errorf("got %v, want zero pair", dst)
	}
}

// TestAssignMvZeromv writes zeros to both halves regardless of refMv
// state and consumes nothing from the reader.
func TestAssignMvZeromv(t *testing.T) {
	var mv, refMv, near [2]MV
	refMv[0] = MV{10, 20}
	near[0] = MV{30, 40}
	var fc FrameContext
	var r bitstream.Reader
	if got := AssignMv(common.ZeroMv, &mv, &refMv, &near, 0, true, &r, &fc); got != 1 {
		t.Errorf("ret = %d, want 1", got)
	}
	if mv != ([2]MV{}) {
		t.Errorf("mv = %v, want zero pair", mv)
	}
}

// TestAssignMvNearestNearMv: both copy from `near_nearest_mv` without
// touching the reader.
func TestAssignMvNearestNearMv(t *testing.T) {
	var fc FrameContext
	var r bitstream.Reader
	src := [2]MV{{5, -7}, {11, -13}}
	for _, mode := range []common.PredictionMode{common.NearestMv, common.NearMv} {
		var mv, refMv [2]MV
		near := src
		if got := AssignMv(mode, &mv, &refMv, &near, 1, true, &r, &fc); got != 1 {
			t.Errorf("mode %d: ret = %d", mode, got)
		}
		if mv != src {
			t.Errorf("mode %d: mv = %v, want %v", mode, mv, src)
		}
	}
}

// TestAssignMvNewmvSingle: NEWMV reads one MV against ref_mv[0].
func TestAssignMvNewmvSingle(t *testing.T) {
	var fc FrameContext
	// Seed NMV joints + components with the libvpx defaults so ReadMv
	// produces a deterministic walk.
	fc.Nmvc = defaultNmvContext()

	ref := MV{0, 0}                     // useMvHp(ref) → true (|0|<8)
	want := MV{Row: 4 + 3, Col: -2 - 7} // ref(4,-2) + delta(3,-7) → no... ref=(0,0) + delta
	// Reset to make the math easy: encode a tiny delta against ref=(0,0).
	want = MV{Row: 3, Col: -7}

	buf := make([]byte, 128)
	var w bitstream.Writer
	w.Start(buf)
	encodeMvAgainstRef(t, &w, &fc.Nmvc, ref, want, true)
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var mv, refMv, near [2]MV
	refMv[0] = ref
	if got := AssignMv(common.NewMv, &mv, &refMv, &near, 0, true, &r, &fc); got != 1 {
		t.Errorf("ret = %d, want 1", got)
	}
	if mv[0] != want {
		t.Errorf("mv[0] = %v, want %v", mv[0], want)
	}
}

// TestReadIsInterBlockSegmentOverride: SEG_LVL_REF_FRAME forces the
// per-block bit; non-INTRA → 1, INTRA → 0.
func TestReadIsInterBlockSegmentOverride(t *testing.T) {
	var fc FrameContext
	var r bitstream.Reader
	seg := SegmentationParams{Enabled: true}
	seg.FeatureMask[2] = 1 << SegLvlRefFrame

	seg.FeatureData[2][SegLvlRefFrame] = LastFrame
	if got := ReadIsInterBlock(&r, &seg, 2, &fc, nil, nil); got != 1 {
		t.Errorf("non-intra seg ref: got %d, want 1", got)
	}
	seg.FeatureData[2][SegLvlRefFrame] = IntraFrame
	if got := ReadIsInterBlock(&r, &seg, 2, &fc, nil, nil); got != 0 {
		t.Errorf("intra seg ref: got %d, want 0", got)
	}
}

// TestReadIsInterBlockReadsBit: no override → reads against
// fc.IntraInterProb[ctx].
func TestReadIsInterBlockReadsBit(t *testing.T) {
	var fc FrameContext
	fc.IntraInterProb[0] = 128
	var seg SegmentationParams

	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(1, 128)
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	if got := ReadIsInterBlock(&r, &seg, 0, &fc, nil, nil); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

// encodeMvAgainstRef writes the joint + per-axis MV deltas for `want
// = ref + delta` using the NmvContext PMFs. Mirrors libvpx's
// write_mv layout — joint then per-axis components.
func encodeMvAgainstRef(t *testing.T, w *bitstream.Writer, ctx *NmvContext, ref, want MV, allowHp bool) {
	t.Helper()
	dRow := int(want.Row - ref.Row)
	dCol := int(want.Col - ref.Col)
	useHp := allowHp && useMvHp(&ref)

	var joint int
	switch {
	case dRow != 0 && dCol != 0:
		joint = 3
	case dCol != 0:
		joint = 2
	case dRow != 0:
		joint = 1
	default:
		joint = 0
	}
	bits, idx, _ := findTreePath(jointTreeForTest(), joint)
	for k := range bits {
		w.Write(bits[k], uint32(ctx.Joints[idx[k]]))
	}
	if dRow != 0 {
		writeComponentToWriter(t, w, &ctx.Comps[0], dRow, useHp)
	}
	if dCol != 0 {
		writeComponentToWriter(t, w, &ctx.Comps[1], dCol, useHp)
	}
}

// jointTreeForTest mirrors tables.MvJointTree; importing the table
// here would create a dependency loop in tests.
func jointTreeForTest() []int8 {
	// MvJointTree = [6]int8{-0, 2, -1, 4, -2, -3}
	return []int8{0, 2, -1, 4, -2, -3}
}

// writeComponentToWriter is the per-axis sibling of writeComp in
// mv_test.go, refactored to take a *Writer so encodeMvAgainstRef
// can compose joint + components on a shared writer.
func writeComponentToWriter(t *testing.T, w *bitstream.Writer, c *NmvComponent, val int, useHp bool) {
	t.Helper()
	sign := uint32(0)
	if val < 0 {
		sign = 1
		val = -val
	}
	v := val - 1
	hp := v & 1
	fr := (v >> 1) & 3
	dShifted := v >> 3
	mvClass, d := classifyMv(val)
	if mvClass == 0 {
		d = dShifted
	} else {
		mag := Class0Size << uint(mvClass+2)
		d = dShifted - (mag >> 3)
	}
	bits, idx, _ := findTreePath(mvClassTreeForTest(), mvClass)
	w.Write(sign, uint32(c.Sign))
	for k := range bits {
		w.Write(bits[k], uint32(c.Classes[idx[k]]))
	}
	if mvClass == 0 {
		w.Write(uint32(d), uint32(c.Class0[0]))
	} else {
		n := mvClass + Class0Bits - 1
		for i := range n {
			w.Write(uint32((d>>uint(i))&1), uint32(c.Bits[i]))
		}
	}
	var fpProbs []uint8
	if mvClass == 0 {
		fpProbs = c.Class0Fp[d][:]
	} else {
		fpProbs = c.Fp[:]
	}
	frBits, frIdx, _ := findTreePath(mvFpTreeForTest(), fr)
	for k := range frBits {
		w.Write(frBits[k], uint32(fpProbs[frIdx[k]]))
	}
	if useHp {
		var hpProb uint8
		if mvClass == 0 {
			hpProb = c.Class0Hp
		} else {
			hpProb = c.Hp
		}
		w.Write(uint32(hp), uint32(hpProb))
	}
}

func mvClassTreeForTest() []int8 {
	// Same layout as tables.MvClassTree (see mv_default.go).
	return []int8{-0, 2, -1, 4, 6, 8, -2, -3, -4, -5, 10, 12, -6, -7, -8, -9, 14, 16, 18, -10}
}

func mvFpTreeForTest() []int8 {
	// MvFpTree = [6]int8{-0, 2, -1, 4, -2, -3}.
	return []int8{0, 2, -1, 4, -2, -3}
}
