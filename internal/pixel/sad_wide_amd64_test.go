//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"fmt"
	"os"
	"simd/archsimd"
	"testing"
)

func sadWideScalarOracle(src []byte, ss int, ref []byte, rs, w, h int) int {
	sum := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := int(src[y*ss+x]) - int(ref[y*rs+x])
			if d < 0 {
				d = -d
			}
			sum += d
		}
	}
	return sum
}

func TestSADWideAVX2Selected(t *testing.T) {
	t.Logf("Enabled=%v actual AVX2=%v", Enabled, archsimd.X86.AVX2())
	if os.Getenv("GOVPX_REQUIRE_SAD_AVX2_OFF") == "1" && (Enabled || archsimd.X86.AVX2()) {
		t.Fatal("requested CPU-off validation still has AVX2 enabled")
	}
	if !Enabled || !archsimd.X86.AVX2() {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required AVX2 SAD backend unavailable")
		}
		t.Skip("AVX2 backend unavailable; row/fallback tests still run")
	}
}

func sadWideCheckRows(t *testing.T, w, h, ss, rs, alignS, alignR, pattern int, alias bool) {
	t.Helper()
	ns, nr := (h-1)*ss+w, (h-1)*rs+w
	so := 32 + alignS
	ro := (so + ns + 63) &^ 31
	ro += alignR
	if alias {
		ro = so + alignR
	}
	storage := make([]byte, max(so+ns, ro+nr)+37)
	for i := range storage {
		storage[i] = byte(i*37 + i/7 + 11)
	}
	src, ref := storage[so:so+ns], storage[ro:ro+nr]
	if pattern == 1 {
		for i := range storage {
			storage[i] = 0
		}
	}
	if pattern == 2 && !alias {
		for y := range h {
			for x := range w {
				src[y*ss+x] = 0
				ref[y*rs+x] = 255
			}
		}
	}
	before := bytes.Clone(storage)
	want := sadWideScalarOracle(src, ss, ref, rs, w, h)
	got := SAD(src, ss, ref, rs, w, h)
	if got != want || !bytes.Equal(storage, before) {
		t.Fatalf("w%d h%d strides%d/%d align%d/%d pattern%d alias%v got%d want%d", w, h, ss, rs, alignS, alignR, pattern, alias, got, want)
	}
}

func TestSADWideAVX2Rows(t *testing.T) {
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && (!Enabled || !archsimd.X86.AVX2()) {
		t.Fatal("required AVX2 backend unavailable")
	}
	for _, w := range []int{32, 64} {
		for _, h := range []int{1, 2, 3, 8, 16, 31, 32, 63, 64, 65} {
			for _, strides := range [][2]int{{0, 0}, {1, 7}, {w - 1, w - 3}, {w, w}, {w + 5, w + 11}, {1312, 1320}} {
				for pattern := range 3 {
					sadWideCheckRows(t, w, h, strides[0], strides[1], 3, 7, pattern, false)
				}
				for offset := range 4 {
					sadWideCheckRows(t, w, h, strides[0], strides[1], 1, offset, 0, true)
				}
			}
		}
		// All32×32 source/reference byte alignments with exact minimum windows.
		for a := range 32 {
			for b := range 32 {
				for _, s := range [][2]int{{w, w}, {w + 5, w + 11}} {
					sadWideCheckRows(t, w, 3, s[0], s[1], a, b, 0, false)
				}
			}
		}
	}
}

func sadWideCapture(f func() int) (value int, panicked bool, diagnostic string) {
	defer func() {
		if v := recover(); v != nil {
			panicked = true
			diagnostic = fmt.Sprint(v)
		}
	}()
	value = f()
	return
}

func TestSADWideAVX2FallbackAndBounds(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	type testCase struct{ w, h, ss, rs, ns, nr int }
	cases := []testCase{
		{32, 0, -1, maxInt, 0, 0}, {64, -1, maxInt, -1, 0, 0},
		{32, 1, -1, maxInt, 32, 32}, {64, 1, maxInt, -1, 64, 64},
		{32, 2, 1 << 30, 32, 64, 64}, {32, 2, 32, 1 << 30, 64, 64},
		{64, 2, maxInt, 64, 128, 128}, {64, 2, 64, maxInt, 128, 128},
		{32, 2, -1, 32, 64, 64}, {32, 2, 32, -1, 64, 64},
	}
	for _, w := range []int{0, 4, 8, 16, 24, 31, 32, 48, 64, 65} {
		cases = append(cases, testCase{w, 3, 80, 84, 240, 252})
	}
	for _, w := range []int{32, 64} {
		for _, h := range []int{1, 3, 64, 65} {
			ns, nr := (h-1)*(w+5)+w, (h-1)*(w+11)+w
			cases = append(cases, testCase{w, h, w + 5, w + 11, ns, nr})
			for _, short := range []int{0, 1, -1} {
				na := short
				if short < 0 {
					na = ns - 1
				}
				nb := short
				if short < 0 {
					nb = nr - 1
				}
				cases = append(cases, testCase{w, h, w + 5, w + 11, na, nr}, testCase{w, h, w + 5, w + 11, ns, nb}, testCase{w, h, w + 5, w + 11, na, nb})
			}
		}
	}
	for n, c := range cases {
		// Spare capacity must not make an incomplete visible slice eligible.
		a, b := make([]byte, max(c.ns, 0)+67), make([]byte, max(c.nr, 0)+71)
		for i := range a {
			a[i] = byte(i*41 + 3)
		}
		for i := range b {
			b[i] = byte(i*29 + 7)
		}
		beforeA, beforeB := bytes.Clone(a), bytes.Clone(b)
		av, bv := a[:c.ns], b[:c.nr]
		got, gp, gd := sadWideCapture(func() int { return SAD(av, c.ss, bv, c.rs, c.w, c.h) })
		want, wp, wd := sadWideCapture(func() int { return sadWideOriginal(av, c.ss, bv, c.rs, c.w, c.h) })
		if got != want || gp != wp || gd != wd || !bytes.Equal(a, beforeA) || !bytes.Equal(b, beforeB) {
			t.Fatalf("case%d %+v got%d panic%v %q want%d panic%v %q", n, c, got, gp, gd, want, wp, wd)
		}
	}
}

func TestSADWideAVX2NilInputs(t *testing.T) {
	for _, a := range [][]byte{nil, make([]byte, 160)} {
		for _, b := range [][]byte{nil, make([]byte, 160)} {
			for _, h := range []int{0, 1, 3} {
				got, gp, gd := sadWideCapture(func() int { return SAD(a, 64, b, 64, 32, h) })
				want, wp, wd := sadWideCapture(func() int { return sadWideOriginal(a, 64, b, 64, 32, h) })
				if got != want || gp != wp || gd != wd {
					t.Fatalf("nil%v/%v h%d got%d %v %q want%d %v %q", a == nil, b == nil, h, got, gp, gd, want, wp, wd)
				}
			}
		}
	}
}

func TestSADWideAVX2Disabled(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	for _, w := range []int{32, 64} {
		for _, h := range []int{1, 3, 16, 64} {
			for _, s := range [][2]int{{0, 0}, {1, 7}, {w, w + 3}} {
				sadWideCheckRows(t, w, h, s[0], s[1], 1, 3, 0, true)
			}
		}
	}
	TestSADWideAVX2FallbackAndBounds(t)
}

// Exact pre-probe public entry retains unusual-shape and panic behavior.
func sadWideOriginal(src []byte, ss int, ref []byte, rs, w, h int) int {
	if !Enabled {
		return SADScalar(src, ss, ref, rs, w, h)
	}
	if w == 8 && (h == 8 || h == 16) && ss >= 0 && rs >= 0 {
		_ = src[(h-1)*ss+7]
		_ = ref[(h-1)*rs+7]
		value := sad8x8Slice(src, ss, ref, rs)
		if h == 16 {
			value += sad8x8Slice(src[8*ss:], ss, ref[8*rs:], rs)
		}
		return value
	}
	return sadGeneric(src, ss, ref, rs, w, h)
}
