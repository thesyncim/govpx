package dsp

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"slices"
	"testing"
)

var dsp01PRSink int

func dsp01PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func dsp01PRBilinear(w int, s []byte, ss, x, y int, d []byte, ds int) {
	if w == 4 {
		BilinearPredict4x4(s, ss, x, y, d, ds)
	} else {
		BilinearPredict8x4(s, ss, x, y, d, ds)
	}
}
func dsp01PRBilinearRef(w int, s []byte, ss, x, y int, d []byte, ds int) {
	var tmp [5 * 8]int
	for r := range 5 {
		for c := range w {
			tmp[r*w+c] = (int(s[r*ss+c])*(128-x*16) + int(s[r*ss+c+1])*x*16 + 64) >> 7
		}
	}
	for r := range 4 {
		for c := range w {
			d[r*ds+c] = byte((tmp[r*w+c]*(128-y*16) + tmp[(r+1)*w+c]*y*16 + 64) >> 7)
		}
	}
}
func TestDSP01PRBilinear(t *testing.T) {
	for _, w := range []int{4, 8} {
		for _, pad := range []int{0, 11} {
			ss, ds := w+1+pad, w+pad
			for x := range 8 {
				for y := range 8 {
					s := dsp01PRData(5 * ss)
					a, b := dsp01PRData(4*ds), dsp01PRData(4*ds)
					dsp01PRBilinear(w, s, ss, x, y, a, ds)
					dsp01PRBilinearRef(w, s, ss, x, y, b, ds)
					if !slices.Equal(a, b) {
						t.Fatalf("w%d %d,%d", w, x, y)
					}
				}
			}
		}
		for _, off := range []int{0, 1, 9} {
			a, b := dsp01PRData(256), dsp01PRData(256)
			dsp01PRBilinear(w, a, 24, 3, 5, a[off:], 16)
			dsp01PRBilinearRef(w, b, 24, 3, 5, b[off:], 16)
			if !slices.Equal(a, b) {
				t.Fatalf("alias%d", off)
			}
		}
	}
}
func BenchmarkDSP01PRBilinear(b *testing.B) {
	for _, w := range []int{4, 8} {
		for _, xy := range [][2]int{{0, 0}, {4, 0}, {0, 4}, {3, 5}} {
			b.Run(fmt.Sprintf("w%d/%d_%d", w, xy[0], xy[1]), func(b *testing.B) {
				s, d := dsp01PRData(5*24), make([]byte, 4*16)
				b.ReportAllocs()
				for b.Loop() {
					dsp01PRBilinear(w, s, 24, xy[0], xy[1], d, 16)
				}
				dsp01PRSink = int(d[7])
			})
		}
	}
}

var dsp02PRSink int

func dsp02PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func dsp02PRResidualRef(d []byte, ds int, r *[16]int16) {
	for y := range 4 {
		row := (*[4]byte)(d[y*ds : y*ds+4])
		for x := range 4 {
			row[x] = ClipPixel(int(row[x]) + int(r[y*4+x]))
		}
	}
}
func TestDSP02PRResidual(t *testing.T) {
	for k := range 1000 {
		var r [16]int16
		for i := range r {
			r[i] = int16(k*1517 + i*3791)
		}
		for _, ds := range []int{4, 7, 24} {
			a, c := dsp02PRData(4*ds), dsp02PRData(4*ds)
			AddResidual4x4(a, ds, &r)
			dsp02PRResidualRef(c, ds, &r)
			if !slices.Equal(a, c) {
				t.Fatalf("k%d ds%d", k, ds)
			}
		}
	}
}
func BenchmarkDSP02PRResidual(b *testing.B) {
	for _, ds := range []int{4, 24} {
		for _, dense := range []bool{false, true} {
			b.Run(fmt.Sprintf("stride%d/dense%v", ds, dense), func(b *testing.B) {
				d := dsp02PRData(4 * ds)
				var r [16]int16
				for i := range r {
					r[i] = int16(i - 7)
					if dense {
						r[i] = int16(i * 4000)
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					AddResidual4x4(d, ds, &r)
				}
				dsp02PRSink = int(d[0])
			})
		}
	}
}

var dsp03rPRSink int

func dsp03rPRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func TestDSP03RPRZeroDC(t *testing.T) {
	for _, c := range []int16{-32768, -10, -5, -4, -1, 0, 1, 3, 4, 10, 32767} {
		for _, ds := range []int{4, 24} {
			for _, same := range []bool{false, true} {
				s := dsp03rPRData(4 * ds)
				a, z := slices.Clone(s), slices.Clone(s)
				p := s
				if same {
					p = a
				}
				DCOnlyIDCT4x4Add(c, p, ds, a, ds)
				p = s
				if same {
					p = z
				}
				dcOnlyIDCT4x4AddScalar(c, p, ds, z, ds)
				if !slices.Equal(a, z) {
					t.Fatalf("c%d ds%d", c, ds)
				}
			}
		}
	}
}
func BenchmarkDSP03RPRZeroDC(b *testing.B) {
	for _, c := range []int16{0, 8} {
		for _, same := range []bool{true, false} {
			b.Run(fmt.Sprintf("dc%d/inplace%v", c, same), func(b *testing.B) {
				s, d := dsp03rPRData(4*24), dsp03rPRData(4*24)
				if same {
					s = d
				}
				b.ReportAllocs()
				for b.Loop() {
					DCOnlyIDCT4x4Add(c, s, 24, d, 24)
				}
				dsp03rPRSink = int(d[0])
			})
		}
	}
}

var dsp04PRSink int

func dsp04PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func TestDSP04PRDequant(t *testing.T) {
	for k := range 1000 {
		var q, d, a, z [16]int16
		for i := range q {
			q[i] = int16(k*1517 + i*3791)
			d[i] = int16(k*913 + i*1777)
			z[i] = q[i] * d[i]
		}
		DequantizeBlock(&q, &d, &a)
		if a != z {
			t.Fatalf("k%d", k)
		}
		DequantizeBlock(&q, &d, &q)
		if q != z {
			t.Fatalf("inplace k%d", k)
		}
	}
	for _, off := range []int{1, 7, 15} {
		var a, z [48]int16
		for i := range a {
			a[i] = int16(i * 7851)
			z[i] = a[i]
		}
		DequantizeBlock((*[16]int16)(a[:]), (*[16]int16)(a[32:]), (*[16]int16)(a[off:]))
		for i := range 16 {
			z[off+i] = z[i] * z[32+i]
		}
		if a != z {
			t.Fatalf("alias%d", off)
		}
	}
}
func BenchmarkDSP04PRDequant(b *testing.B) {
	for _, same := range []bool{false, true} {
		b.Run(fmt.Sprintf("inplace%v", same), func(b *testing.B) {
			var q, d, o [16]int16
			for i := range q {
				q[i] = int16(i * 1517)
				d[i] = int16(i * 1777)
			}
			out := &o
			if same {
				out = &q
			}
			b.ReportAllocs()
			for b.Loop() {
				DequantizeBlock(&q, &d, out)
			}
			dsp04PRSink = int(out[0])
		})
	}
}

var dsp05PRSink int

func dsp05PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func TestDSP05PRWalsh(t *testing.T) {
	for k := range 3000 {
		var c [16]int16
		for i := range c {
			c[i] = int16(k*1517 + i*3791)
		}
		a, z := make([]int16, 256), make([]int16, 256)
		InverseWalsh4x4(&c, a)
		inverseWalsh4x4Scalar(&c, z)
		if !slices.Equal(a, z) {
			t.Fatalf("k%d", k)
		}
	}
}
func BenchmarkDSP05PRWalsh(b *testing.B) {
	var c [16]int16
	for i := range c {
		c[i] = int16(i * 141)
	}
	o := make([]int16, 256)
	b.ReportAllocs()
	for b.Loop() {
		InverseWalsh4x4(&c, o)
	}
	dsp05PRSink = int(o[16])
}

var dsp06PRSink int

func dsp06PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func TestDSP06PRSparseDequant(t *testing.T) {
	for _, dc := range []int16{-32768, -32765, -1000, -4, 0, 3, 1000, 32767} {
		for _, dq := range []int16{-32768, -17, 0, 1, 17, 32767} {
			for _, dense := range []bool{false, true} {
				var a, z, q [16]int16
				a[0] = dc
				for i := range q {
					q[i] = dq
					if dense {
						a[i] = int16(i * 1517)
					}
				}
				z = a
				x, y := dsp06PRData(4*24), dsp06PRData(4*24)
				DequantIDCT4x4Add(&a, &q, x, 24)
				for i := range z {
					z[i] *= q[i]
				}
				IDCT4x4Add(&z, y, 24, y, 24)
				clear(z[:])
				if a != z || !slices.Equal(x, y) {
					t.Fatalf("dc%d dq%d dense%v", dc, dq, dense)
				}
			}
		}
	}
}
func BenchmarkDSP06PRSparseDequant(b *testing.B) {
	for _, kind := range []string{"zero", "dc", "dense"} {
		b.Run(kind, func(b *testing.B) {
			var in, q, seed [16]int16
			for i := range q {
				q[i] = 117
				if kind == "dense" {
					seed[i] = int16(i*71 - 300)
				}
			}
			if kind == "dc" {
				seed[0] = 17
			}
			d := dsp06PRData(4 * 24)
			b.ReportAllocs()
			for b.Loop() {
				in = seed
				DequantIDCT4x4Add(&in, &q, d, 24)
			}
			dsp06PRSink = int(d[0])
		})
	}
}

var dsp07PRSink int

func dsp07PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func dsp07PRSplitRef(src []byte, ss, x, y int, ref []byte, rs, w, h int) (int, int) {
	tmp := make([]byte, (h+1)*w)
	f := make([]byte, h*w)
	for r := 0; r < h+1; r++ {
		for c := range w {
			tmp[r*w+c] = byte((int(src[r*ss+c])*(128-x*16) + int(src[r*ss+c+1])*x*16 + 64) >> 7)
		}
	}
	for r := range h {
		for c := range w {
			f[r*w+c] = byte((int(tmp[r*w+c])*(128-y*16) + int(tmp[(r+1)*w+c])*y*16 + 64) >> 7)
		}
	}
	sum, sse := varianceBlockGeneric(f, w, ref, rs, w, h)
	return sse - sum*sum/(w*h), sse
}
func dsp07PRSplit(s []byte, ss, x, y int, r []byte, rs, w, h int) (int, int) {
	if w == 4 && h == 4 {
		return SubpelVariance4x4(s, ss, x, y, r, rs)
	}
	if w == 4 {
		return SubpelVariance4x8(s, ss, x, y, r, rs)
	}
	if h == 4 {
		return SubpelVariance8x4(s, ss, x, y, r, rs)
	}
	if h == 8 {
		return SubpelVariance8x8(s, ss, x, y, r, rs)
	}
	return SubpelVariance8x16(s, ss, x, y, r, rs)
}
func TestDSP07PRSplit(t *testing.T) {
	for _, wh := range [][2]int{{4, 4}, {4, 8}, {8, 4}, {8, 8}, {8, 16}} {
		w, h := wh[0], wh[1]
		s, r := dsp07PRData((h+1)*24), dsp07PRData(h*32)
		for x := range 8 {
			for y := range 8 {
				a, b := dsp07PRSplit(s, 24, x, y, r, 32, w, h)
				c, d := dsp07PRSplitRef(s, 24, x, y, r, 32, w, h)
				if a != c || b != d {
					t.Fatalf("%dx%d %d,%d", w, h, x, y)
				}
			}
		}
	}
}
func BenchmarkDSP07PRSplit(b *testing.B) {
	for _, wh := range [][2]int{{4, 4}, {4, 8}, {8, 4}, {8, 8}, {8, 16}} {
		for _, xy := range [][2]int{{4, 0}, {0, 4}, {3, 5}} {
			b.Run(fmt.Sprintf("%dx%d/%d_%d", wh[0], wh[1], xy[0], xy[1]), func(b *testing.B) {
				s, r := dsp07PRData(17*24), dsp07PRData(16*32)
				a, z := 0, 0
				b.ReportAllocs()
				for b.Loop() {
					a, z = dsp07PRSplit(s, 24, xy[0], xy[1], r, 32, wh[0], wh[1])
				}
				dsp07PRSink = a + z
			})
		}
	}
}

var dsp09rPRSink int

func dsp09rPRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func dsp09rPRDirectionalRef(mode int, d []byte, ds int, a, l []byte, t byte) {
	switch mode {
	case 2:
		intra4x4VEPredictScalar(d, ds, a, t)
	case 3:
		intra4x4HEPredictScalar(d, ds, l, t)
	case 4:
		intra4x4LDPredictScalar(d, ds, a)
	case 5:
		intra4x4RDPredictScalar(d, ds, a, l, t)
	case 6:
		intra4x4VRPredictScalar(d, ds, a, l, t)
	case 7:
		intra4x4VLPredictScalar(d, ds, a)
	case 8:
		intra4x4HDPredictScalar(d, ds, a, l, t)
	case 9:
		intra4x4HUPredictScalar(d, ds, l)
	}
}
func dsp09rPRDirectionalFrozen(mode int, d []byte, ds int, a, l []byte, t byte) {
	if !pixel.Enabled {
		dsp09rPRDirectionalRef(mode, d, ds, a, l, t)
		return
	}
	var border [16]byte
	copy(border[:8], a)
	copy(border[8:12], l)
	border[12] = t
	pixel.Intra4(d, ds, &border, &intra4Selectors[mode-2])
}
func TestDSP09RPRDirectional(t *testing.T) {
	for k := range 100 {
		a, l := dsp09rPRData(8), dsp09rPRData(4)
		for i := range a {
			a[i] += byte(k * 11)
		}
		for i := range l {
			l[i] += byte(k * 31)
		}
		for mode := 2; mode < 10; mode++ {
			x, z := dsp09rPRData(4*24), dsp09rPRData(4*24)
			Intra4x4Predict(x, 24, common.BPredictionMode(mode), a, l, byte(k))
			dsp09rPRDirectionalRef(mode, z, 24, a, l, byte(k))
			if !slices.Equal(x, z) {
				t.Fatalf("mode%d k%d", mode, k)
			}
		}
	}
}
func TestDSP09RPRDirectionalAlias(t *testing.T) {
	for mode := 2; mode < 10; mode++ {
		for _, ds := range []int{0, 1, 3, 4, 24} {
			for _, off := range []int{0, 1, 4, 8, 31} {
				x, z := dsp09rPRData(256), dsp09rPRData(256)
				a, l := x[off:off+8], x[off+1:off+5]
				c, e := z[off:off+8], z[off+1:off+5]
				Intra4x4Predict(x, ds, common.BPredictionMode(mode), a, l, 127)
				dsp09rPRDirectionalFrozen(mode, z, ds, c, e, 127)
				if !slices.Equal(x, z) {
					t.Fatalf("mode%d stride%d offset%d", mode, ds, off)
				}
			}
		}
	}
}

func BenchmarkDSP09RPRDirectional(b *testing.B) {
	for mode := 2; mode < 10; mode++ {
		b.Run(fmt.Sprintf("mode%d", mode), func(b *testing.B) {
			a, l, d := dsp09rPRData(8), dsp09rPRData(4), dsp09rPRData(4*24)
			b.ReportAllocs()
			for b.Loop() {
				Intra4x4Predict(d, 24, common.BPredictionMode(mode), a, l, 127)
			}
			dsp09rPRSink = int(d[0])
		})
	}
}

var dsp11PRSink int

func dsp11PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func TestDSP11PRSAD4(t *testing.T) {
	for _, ss := range []int{0, 4, 24} {
		for _, rs := range []int{0, 4, 31} {
			s, r := dsp11PRData(4*24), dsp11PRData(4*31)
			for k := range 100 {
				for i := range s {
					s[i] += byte(k * 11)
				}
				for i := range r {
					r[i] += byte(k * 31)
				}
				x, y := SAD4x4(s, ss, r, rs), sadBlockScalar(s, ss, r, rs, 4, 4)
				if x != y {
					t.Fatalf("k%d", k)
				}
			}
		}
	}
}
func BenchmarkDSP11PRSAD4(b *testing.B) {
	for _, stride := range []int{4, 24} {
		b.Run(fmt.Sprintf("stride%d", stride), func(b *testing.B) {
			s, r := dsp11PRData(4*stride), dsp11PRData(4*stride)
			v := 0
			b.ReportAllocs()
			for b.Loop() {
				v = SAD4x4(s, stride, r, stride)
			}
			dsp11PRSink = v
		})
	}
}

var dsp12PRSink int

func dsp12PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func TestDSP12PRSSE4(t *testing.T) {
	for _, h := range []int{4, 8} {
		for _, ss := range []int{0, 4, 24} {
			for _, rs := range []int{0, 4, 31} {
				s, r := dsp12PRData(h*24), dsp12PRData(h*31)
				for k := range 100 {
					for i := range s {
						s[i] += byte(k * 11)
					}
					for i := range r {
						r[i] += byte(k * 31)
					}
					x := SSE4x4(s, ss, r, rs)
					if h == 8 {
						x = SSE4x8(s, ss, r, rs)
					}
					_, y := varianceBlockGeneric(s, ss, r, rs, 4, h)
					if x != y {
						t.Fatalf("h%d k%d", h, k)
					}
				}
			}
		}
	}
}
func BenchmarkDSP12PRSSE4(b *testing.B) {
	for _, h := range []int{4, 8} {
		for _, stride := range []int{4, 24} {
			b.Run(fmt.Sprintf("h%d/stride%d", h, stride), func(b *testing.B) {
				s, r := dsp12PRData(h*stride), dsp12PRData(h*stride)
				v := 0
				b.ReportAllocs()
				for b.Loop() {
					if h == 4 {
						v = SSE4x4(s, stride, r, stride)
					} else {
						v = SSE4x8(s, stride, r, stride)
					}
				}
				dsp12PRSink = v
			})
		}
	}
}
