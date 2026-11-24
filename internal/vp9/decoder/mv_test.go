package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// defaultNmvContext seeds an NmvContext from the canonical libvpx
// default_nmv_context blob (joints + 2 components). Mirrors the
// per-frame seed every VP9 decoder uses when frame_context_idx forces
// a context reset.
func defaultNmvContext() NmvContext {
	var ctx NmvContext
	copy(ctx.Joints[:], tables.DefaultNmvJoints[:])
	for i := range 2 {
		src := &tables.DefaultNmvComps[i]
		dst := &ctx.Comps[i]
		dst.Sign = src.Sign
		copy(dst.Classes[:], src.Classes[:])
		copy(dst.Class0[:], src.Class0[:])
		copy(dst.Bits[:], src.Bits[:])
		for j := range Class0Size {
			copy(dst.Class0Fp[j][:], src.Class0Fp[j][:])
		}
		copy(dst.Fp[:], src.Fp[:])
		dst.Class0Hp = src.Class0Hp
		dst.Hp = src.Hp
	}
	return ctx
}

// encodeMvComponent emits the boolean-coded bytes that decode back to
// `value` under ReadMvComponent with component `c` and the supplied
// high-precision gate. Mirrors libvpx's write_mv_component layout, but
// here we only need a faithful inverse of the decode walk.
func encodeMvComponent(t *testing.T, c *NmvComponent, value int, usehp bool) []byte {
	t.Helper()
	if value == 0 {
		t.Fatal("encodeMvComponent: 0 is not a valid component value")
	}
	sign := uint32(0)
	if value < 0 {
		sign = 1
		value = -value
	}
	// value = mag + ((d<<3) | (fr<<1) | hp) + 1
	v := value - 1
	hp := v & 1
	fr := (v >> 1) & 3
	dShifted := v >> 3
	mvClass, d := classifyMv(value)
	if mvClass == tables.MvClass0 {
		d = dShifted
	} else {
		mag := Class0Size << uint(mvClass+2)
		d = dShifted - (mag >> 3)
	}

	if !usehp && hp != 1 {
		t.Fatalf("encodeMvComponent: usehp=false requires hp=1, got value=%d hp=%d", value, hp)
	}

	bits, idx, ok := findTreePath(tables.MvClassTree[:], mvClass)
	if !ok {
		t.Fatalf("class %d not reachable", mvClass)
	}
	frBits, frIdx, ok := findTreePath(tables.MvFpTree[:], fr)
	if !ok {
		t.Fatalf("fr %d not reachable", fr)
	}

	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(sign, uint32(c.Sign))
	for k := range bits {
		w.Write(bits[k], uint32(c.Classes[idx[k]]))
	}
	if mvClass == tables.MvClass0 {
		w.Write(uint32(d), uint32(c.Class0[0]))
	} else {
		n := mvClass + Class0Bits - 1
		for i := range n {
			w.Write(uint32((d>>uint(i))&1), uint32(c.Bits[i]))
		}
	}
	var fpProbs []uint8
	if mvClass == tables.MvClass0 {
		fpProbs = c.Class0Fp[d][:]
	} else {
		fpProbs = c.Fp[:]
	}
	for k := range frBits {
		w.Write(frBits[k], uint32(fpProbs[frIdx[k]]))
	}
	if usehp {
		var hpProb uint8
		if mvClass == tables.MvClass0 {
			hpProb = c.Class0Hp
		} else {
			hpProb = c.Hp
		}
		w.Write(uint32(hp), uint32(hpProb))
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	return buf[:size]
}

// TestReadMvComponentRoundTripDefault round-trips a spread of magnitudes
// through ReadMvComponent using the canonical default NMV component
// (vertical axis). This anchors the decode walk against the seed
// probabilities every VP9 decoder starts from.
func TestReadMvComponentRoundTripDefault(t *testing.T) {
	ctx := defaultNmvContext()
	cc := &ctx.Comps[0]

	cases := []struct {
		name  string
		value int
		usehp bool
	}{
		{"class0-positive", +3, true},
		{"class0-negative", -7, true},
		{"class1-small", +17, true},
		{"class1-no-hp", -19, false},
		{"class2", +37, true},
		{"class3", +71, true},
		{"class10-large", +511, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hp must be 1 if usehp=false; force the helper to pick a
			// hp-compatible magnitude by adjusting value if necessary.
			v := tc.value
			if !tc.usehp && (abs(v)-1)&1 != 1 {
				v++ // bump so encoded hp bit lands on 1
			}
			data := encodeMvComponent(t, cc, v, tc.usehp)
			var r bitstream.Reader
			if err := r.Init(data); err != nil {
				t.Fatalf("Init: %v", err)
			}
			got := ReadMvComponent(&r, cc, tc.usehp)
			if got != v {
				t.Errorf("got %d, want %d", got, v)
			}
		})
	}
}

// TestReadMvRoundTripDefault drives ReadMv with the joint+component
// walk and verifies the ref+delta sum.
func TestReadMvRoundTripDefault(t *testing.T) {
	ctx := defaultNmvContext()
	ref := MV{Row: 4, Col: -2}
	useHp := useMvHp(&ref) // matches libvpx's gate exactly

	// Encode joint=HnzVnz then both components.
	dRow, dCol := 3, -7

	jointBits, jointIdx, _ := findTreePath(tables.MvJointTree[:], tables.MvJointHnzVnz)
	buf := make([]byte, 128)
	var w bitstream.Writer
	w.Start(buf)
	for k := range jointBits {
		w.Write(jointBits[k], uint32(ctx.Joints[jointIdx[k]]))
	}
	// writeComp emits one component into the shared writer. Mirrors
	// encodeMvComponent's body but lets the joint + both axes share a
	// single boolean-coder partition.
	writeComp := func(c *NmvComponent, val int) {
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
		if mvClass == tables.MvClass0 {
			d = dShifted
		} else {
			mag := Class0Size << uint(mvClass+2)
			d = dShifted - (mag >> 3)
		}
		bits, idx, _ := findTreePath(tables.MvClassTree[:], mvClass)
		w.Write(sign, uint32(c.Sign))
		for k := range bits {
			w.Write(bits[k], uint32(c.Classes[idx[k]]))
		}
		if mvClass == tables.MvClass0 {
			w.Write(uint32(d), uint32(c.Class0[0]))
		} else {
			n := mvClass + Class0Bits - 1
			for i := range n {
				w.Write(uint32((d>>uint(i))&1), uint32(c.Bits[i]))
			}
		}
		var fpProbs []uint8
		if mvClass == tables.MvClass0 {
			fpProbs = c.Class0Fp[d][:]
		} else {
			fpProbs = c.Fp[:]
		}
		frBits, frIdx, _ := findTreePath(tables.MvFpTree[:], fr)
		for k := range frBits {
			w.Write(frBits[k], uint32(fpProbs[frIdx[k]]))
		}
		if useHp {
			var hpProb uint8
			if mvClass == tables.MvClass0 {
				hpProb = c.Class0Hp
			} else {
				hpProb = c.Hp
			}
			w.Write(uint32(hp), uint32(hpProb))
		}
	}
	writeComp(&ctx.Comps[0], dRow)
	writeComp(&ctx.Comps[1], dCol)
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var mv MV
	ReadMv(&r, &mv, &ref, &ctx, true)
	wantRow := ref.Row + int16(dRow)
	wantCol := ref.Col + int16(dCol)
	if mv.Row != wantRow || mv.Col != wantCol {
		t.Errorf("got (%d,%d), want (%d,%d)", mv.Row, mv.Col, wantRow, wantCol)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// classifyMv mirrors libvpx's vp9_get_mv_class on a 1-indexed positive
// magnitude. Returns (class, _) — the offset is recomputed by the
// callers from the encoded d/fr/hp split.
func classifyMv(value int) (int, int) {
	z := value - 1
	switch {
	case z < Class0Size*8:
		return tables.MvClass0, 0
	case z < Class0Size*16:
		return tables.MvClass1, 0
	case z < Class0Size*32:
		return 2, 0
	case z < Class0Size*64:
		return 3, 0
	case z < Class0Size*128:
		return 4, 0
	case z < Class0Size*256:
		return 5, 0
	case z < Class0Size*512:
		return 6, 0
	case z < Class0Size*1024:
		return 7, 0
	case z < Class0Size*2048:
		return 8, 0
	case z < Class0Size*4096:
		return 9, 0
	default:
		return tables.MvClass10, 0
	}
}
