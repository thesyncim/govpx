//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"fmt"
	"os"
	"simd/archsimd"
	"slices"
	"testing"
)

func TestFP16Selected(t *testing.T) {
	t.Logf("Enabled=%v AVX2=%v", Enabled, archsimd.X86.AVX2())
	if os.Getenv("GOVPX_REQUIRE_FP16_AVX_OFF") == "1" && (Enabled || archsimd.X86.AVX2()) {
		t.Fatal("AVX2 requested off but still enabled")
	}
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && (!Enabled || !archsimd.X86.AVX2()) {
		t.Fatal("required AVX2 unavailable")
	}
}

func fp16ScalarIndependent(c, scan []int16, qout, dqout []int16, classes []byte, n int, r, q, d [2]int16) int {
	eob := 0
	for i := 0; i < n; i++ {
		k := 1
		if i == 0 {
			k = 0
		}
		v := int(c[i])
		mag := v
		if mag < 0 {
			mag = -mag
		}
		mag += int(r[k])
		qm := 0
		if mag >= int(d[k]) {
			if mag > 32767 {
				mag = 32767
			}
			qm = (mag * int(q[k])) >> 16
		}
		v = qm
		if c[i] < 0 {
			v = -v
		}
		if qout != nil {
			qout[i] = int16(v)
		}
		dqout[i] = int16(v * int(d[k]))
		if v != 0 && int(scan[i]) > eob {
			eob = int(scan[i])
		}
		if classes != nil {
			cl := byte(0)
			for _, threshold := range []int{0, 1, 2, 4, 10} {
				if qm > threshold {
					cl++
				}
			}
			classes[i] = cl
		}
	}
	return eob
}

func fp16CheckDisjoint(t *testing.T, n, seed, options int, r, q, d [2]int16) {
	t.Helper()
	coeff, scan := make([]int16, n+14), make([]int16, n+18)
	qgot, dqgot := make([]int16, n+22), make([]int16, n+26)
	clgot := bytes.Repeat([]byte{0xa7}, n+37)
	for i := range coeff {
		coeff[i] = int16(seed*347 + i*771 - 32768)
	}
	for i := range scan {
		scan[i] = int16((i*37 + seed*11) % 32768)
	}
	for i := range qgot {
		qgot[i] = int16(0x4a4a)
	}
	for i := range dqgot {
		dqgot[i] = int16(0x5b5b)
	}
	qwant, dqwant := append([]int16(nil), qgot...), append([]int16(nil), dqgot...)
	clwant := bytes.Clone(clgot)
	beforeC, beforeS := append([]int16(nil), coeff...), append([]int16(nil), scan...)
	qg, qw, cg, cw := qgot[:n], qwant[:n], clgot[:n], clwant[:n]
	if options&1 != 0 {
		qg, qw = nil, nil
	}
	if options&2 != 0 {
		cg, cw = nil, nil
	}
	if !fp16WindowsDisjoint(coeff[:n], scan[:n], qg, dqgot[:n], cg, n) {
		t.Fatal("disjoint windows declined")
	}
	want := fp16ScalarIndependent(coeff, scan, qw, dqwant, cw, n, r, q, d)
	got := QuantizeFPBlock(coeff[:n], scan[:n], qg, dqgot[:n], n, r, q, d, cg)
	eq16 := func(a, b []int16) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	if got != want || !eq16(qgot, qwant) || !eq16(dqgot, dqwant) || !bytes.Equal(clgot, clwant) || !eq16(coeff, beforeC) || !eq16(scan, beforeS) {
		t.Fatalf("n%d seed%d options%d EOB%d/%d", n, seed, options, got, want)
	}
}

func TestFP16DisjointScalar(t *testing.T) {
	tables := [][3][2]int16{
		{{0, 0}, {0, 0}, {1, 1}}, {{32767, 32767}, {32767, 32767}, {1, 32767}},
		{{0, 17}, {32767, 12345}, {23, 41}}, {{32767, 0}, {1, 32767}, {32767, 1}},
	}
	for _, n := range []int{64, 128, 256, 1024} {
		for seed := range 17 {
			for options := range 4 {
				for _, p := range tables {
					fp16CheckDisjoint(t, n, seed, options, p[0], p[1], p[2])
				}
			}
		}
	}
}

func TestFP16AllSignedCoefficients(t *testing.T) {
	const n = 256
	c, scan, qgot, dqgot := make([]int16, n), make([]int16, n), make([]int16, n), make([]int16, n)
	qw, dqw := make([]int16, n), make([]int16, n)
	cl, clw := make([]byte, n), make([]byte, n)
	r, q, d := [2]int16{32767, 17}, [2]int16{32767, 12345}, [2]int16{1, 23}
	for i := range scan {
		scan[i] = int16(n - i)
	}
	for first := -32768; first <= 32767; first += n {
		for i := range c {
			c[i] = int16(first + i)
		}
		want := fp16ScalarIndependent(c, scan, qw, dqw, clw, n, r, q, d)
		got := QuantizeFPBlock(c, scan, qgot, dqgot, n, r, q, d, cl)
		for i := range c {
			if qgot[i] != qw[i] || dqgot[i] != dqw[i] || cl[i] != clw[i] {
				t.Fatalf("coefficient%d", first+i)
			}
		}
		if got != want {
			t.Fatal("EOB")
		}
	}
}

func fp16Views(storage16 []int16, storage8 []byte, offs [5]int, n int) ([]int16, []int16, []int16, []int16, []byte) {
	load := func(i int) []int16 { return storage16[offs[i] : offs[i]+n] }
	return load(0), load(1), load(2), load(3), storage8[offs[4] : offs[4]+n]
}

func TestFP16UnalignedMinimum(t *testing.T) {
	const n = 64
	defaults := [5]int{256, 384, 512, 640, 256}
	r, q, d := [2]int16{9, 29}, [2]int16{12345, 30000}, [2]int16{23, 41}
	for window := 0; window < 5; window++ {
		limit := 16
		if window == 4 {
			limit = 32
		}
		for align := 0; align < limit; align++ {
			off := defaults
			off[window] += align
			got16 := make([]int16, 1024)
			for i := range got16 {
				got16[i] = int16(i*37 + i/7 + 11)
			}
			got8 := make([]byte, 512)
			for i := range got8 {
				got8[i] = byte(i*19 + 5)
			}
			want16 := slices.Clone(got16)
			want8 := bytes.Clone(got8)
			c, s, qo, dqo, cl := fp16Views(got16, got8, off, n)
			wc, ws, wq, wd, wcl := fp16Views(want16, want8, off, n)
			if !fp16WindowsDisjoint(c, s, qo, dqo, cl, n) {
				t.Fatal("minimum disjoint declined")
			}
			we := fp16OriginalPublic(wc, ws, wq, wd, n, r, q, d, wcl)
			ge := QuantizeFPBlock(c, s, qo, dqo, n, r, q, d, cl)
			if ge != we || !slices.Equal(got16, want16) || !bytes.Equal(got8, want8) {
				t.Fatalf("window%d alignment%d eob%d/%d", window, align, ge, we)
			}
		}
	}
}

func TestFP16AliasedStoreOrder(t *testing.T) {
	const n = 64
	defaults := [5]int{256, 384, 512, 640, 256}
	r, q, d := [2]int16{9, 29}, [2]int16{12345, 30000}, [2]int16{23, 41}
	for a := 0; a < 4; a++ {
		for b := a + 1; b < 4; b++ {
			for _, delta := range []int{0, 1, -1, n - 1, -n + 1} {
				off := defaults
				off[b] = off[a] + delta
				got16 := make([]int16, 1024)
				for i := range got16 {
					got16[i] = int16(i*37 + i/7 + 11)
				}
				got8 := make([]byte, 512)
				for i := range got8 {
					got8[i] = byte(i*19 + 5)
				}
				want16 := slices.Clone(got16)
				want8 := bytes.Clone(got8)
				c, s, qo, dqo, cl := fp16Views(got16, got8, off, n)
				wc, ws, wq, wd, wcl := fp16Views(want16, want8, off, n)
				if fp16WindowsDisjoint(c, s, qo, dqo, cl, n) {
					t.Fatalf("alias accepted pair%d/%d delta%d", a, b, delta)
				}
				we := fp16OriginalPublic(wc, ws, wq, wd, n, r, q, d, wcl)
				ge := QuantizeFPBlock(c, s, qo, dqo, n, r, q, d, cl)
				if ge != we || !slices.Equal(got16, want16) || !bytes.Equal(got8, want8) {
					t.Fatalf("store order pair%d/%d delta%d eob%d/%d", a, b, delta, ge, we)
				}
			}
		}
	}
	// Adjacent touched ranges must be eligible.
	off := [5]int{128, 192, 256, 320, 128}
	v16 := make([]int16, 512)
	v8 := make([]byte, 256)
	c, s, qo, dqo, cl := fp16Views(v16, v8, off, n)
	if !fp16WindowsDisjoint(c, s, qo, dqo, cl, n) {
		t.Fatal("adjacent disjoint windows declined")
	}
}

func fp16Capture(f func() int) (value int, panicText string) {
	defer func() {
		if v := recover(); v != nil {
			panicText = fmt.Sprint(v)
		}
	}()
	value = f()
	return
}

func TestFP16OriginalFallback(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 7, 8, 15, 16, 17, 31, 32, 63, 65, 79, 1025} {
		size := max(n, 1)
		c, s, q, d := make([]int16, size), make([]int16, size), make([]int16, size), make([]int16, size)
		cl := make([]byte, size)
		for i := range c {
			c[i] = int16(i*771 - 32768)
			s[i] = int16(size - i)
		}
		got := QuantizeFPBlock(c, s, q, d, n, [2]int16{9, 17}, [2]int16{12345, 30000}, [2]int16{23, 41}, cl)
		wq, wd, wcl := make([]int16, size), make([]int16, size), make([]byte, size)
		want := fp16OriginalPublic(c, s, wq, wd, n, [2]int16{9, 17}, [2]int16{12345, 30000}, [2]int16{23, 41}, wcl)
		if got != want {
			t.Fatalf("n%d eob", n)
		}
		for i := range q {
			if q[i] != wq[i] || d[i] != wd[i] || cl[i] != wcl[i] {
				t.Fatalf("n%d i%d", n, i)
			}
		}
	}
	for short := 0; short < 5; short++ {
		n := 64
		c, s, q, d := make([]int16, n), make([]int16, n), make([]int16, n), make([]int16, n)
		cl := make([]byte, n)
		switch short {
		case 0:
			c = c[:n-1]
		case 1:
			s = s[:n-1]
		case 2:
			d = d[:n-1]
		case 3:
			q = q[:n-1]
		case 4:
			cl = cl[:n-1]
		}
		before := append([]int16(nil), d...)
		got, gp := fp16Capture(func() int {
			return QuantizeFPBlock(c, s, q, d, n, [2]int16{9, 17}, [2]int16{12345, 30000}, [2]int16{23, 41}, cl)
		})
		want, wp := fp16Capture(func() int {
			return fp16OriginalPublic(c, s, q, d, n, [2]int16{9, 17}, [2]int16{12345, 30000}, [2]int16{23, 41}, cl)
		})
		if gp == "" || gp != wp || got != want {
			t.Fatalf("short%d panic %q/%q", short, gp, wp)
		}
		for i := range d {
			if d[i] != before[i] {
				t.Fatal("short window wrote output")
			}
		}
	}
	// Invalid tables retain the original scalar path before any wide setup.
	for _, params := range [][3][2]int16{{{-1, 0}, {7, 9}, {23, 41}}, {{0, 0}, {-1, 9}, {23, 41}}, {{0, 0}, {7, 9}, {0, 41}}, {{0, 0}, {7, 9}, {-32768, 41}}} {
		c, s, q, d := make([]int16, 64), make([]int16, 64), make([]int16, 64), make([]int16, 64)
		for i := range c {
			c[i] = int16(i*771 - 32768)
			s[i] = int16(64 - i)
		}
		q2, d2 := make([]int16, 64), make([]int16, 64)
		cl, cl2 := make([]byte, 64), make([]byte, 64)
		a := QuantizeFPBlock(c, s, q, d, 64, params[0], params[1], params[2], cl)
		b := fp16OriginalPublic(c, s, q2, d2, 64, params[0], params[1], params[2], cl2)
		if a != b {
			t.Fatal("invalid parameters EOB")
		}
		for i := range q {
			if q[i] != q2[i] || d[i] != d2[i] || cl[i] != cl2[i] {
				t.Fatal("invalid parameters output")
			}
		}
	}
}

func TestFP16Disabled(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	TestFP16OriginalFallback(t)
	TestFP16AliasedStoreOrder(t)
}

// Exact frozen public validation and8-lane body preserve alias/panic contracts.
func fp16OriginalPublic(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {
	if n <= 0 {
		return 0
	}
	_ = coeff[n-1]
	_ = iscan[n-1]
	_ = dqcoeff[n-1]
	if qcoeff != nil {
		_ = qcoeff[n-1]
	}
	if classes != nil {
		_ = classes[n-1]
	}
	if Enabled && r[0] >= 0 && r[1] >= 0 && q[0] >= 0 && q[1] >= 0 && d[0] > 0 && d[1] > 0 {
		return fp16Original8(coeff, iscan, qcoeff, dqcoeff, n, r, q, d, classes)
	}
	return quantizeFPBlockTail(coeff, iscan, qcoeff, dqcoeff, 0, n, r, q, d, classes)
}

func fp16Original8(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {
	acR, acQ, acD := archsimd.BroadcastInt16x8(r[1]), archsimd.BroadcastInt16x8(q[1]), archsimd.BroadcastInt16x8(d[1])
	dcR, dcQ, dcD := acR.SetElem(0, r[0]), acQ.SetElem(0, q[0]), acD.SetElem(0, d[0])
	max16 := archsimd.BroadcastUint16x8(32767)
	zero := archsimd.Int16x8{}
	last := zero
	lut := archsimd.LoadUint8x16Array(&fpClassLUT)
	i := 0
	for ; i+8 <= n; i += 8 {
		vr, vq, vd := acR, acQ, acD
		if i == 0 {
			vr, vq, vd = dcR, dcQ, dcD
		}
		c := archsimd.LoadInt16x8(coeff[i:])
		// Unsigned magnitude handles -32768; adding a nonnegative int16 round
		// fits uint16 exactly, then clips to the scalar signed-int16 bound.
		a := c.Abs().ToBits().Add(vr.ToBits()).Min(max16).BitsToInt16()
		t := fpMulHigh16(a, vq).Masked(a.GreaterEqual(vd))
		magnitude := t
		t = zero.Sub(t).IfElse(c.Less(zero), t)
		if qcoeff != nil {
			t.Store(qcoeff[i:])
		}
		t.Mul(vd).Store(dqcoeff[i:])
		last = last.Max(archsimd.LoadInt16x8(iscan[i:]).Masked(t.NotEqual(zero)))
		if classes != nil {
			indices := fpClassIndices(magnitude.Min(archsimd.BroadcastInt16x8(11)))
			storeBytes8(classes[i:], permute16(lut, indices))
		}
	}
	return max(fpReduceMax16(last), quantizeFPBlockTail(coeff, iscan, qcoeff, dqcoeff, i, n, r, q, d, classes))
}
