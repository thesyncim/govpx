//go:build goexperiment.simd && (amd64 || arm64) && !purego

package pixel

import (
	"bytes"
	"fmt"
	"simd/archsimd"
	"testing"
)

// These fixtures use identical complete backing allocations for every variant.
// Timed reset copies are included in both sides of the component comparison.
func BenchmarkX64Round2UV(b *testing.B) {
	for _, vertical := range []bool{false, true} {
		for _, kind := range []int{4, 6} {
			for _, pattern := range []string{"flat", "edge", "dense"} {
				b.Run(fmt.Sprintf("vertical%v/kind%d/%s", vertical, kind, pattern), func(b *testing.B) {
					var original, u, v [1024]byte
					step, outer := 32, 1
					if vertical {
						step, outer = 1, 32
					}
					for i := range original {
						original[i] = 127
						if pattern == "edge" {
							original[i] += byte((i / step) % 3)
						}
						if pattern == "dense" {
							original[i] = byte(i*19 + i/7)
						}
					}
					b.ReportAllocs()
					for b.Loop() {
						u, v = original, original
						if !LoopFilterUV8(u[:], v[:], step, outer, 64, 32, 8, kind) {
							b.Fatal("declined")
						}
					}
				})
			}
		}
	}
}

// Independent scalar VP8 equations from libvpx loopfilter_filters.c.
func round2ScalarUV(p []byte, off, step, outer, blim, lim, thr, kind int) {
	clamp := func(x int) int { return max(-128, min(127, x)) }
	abs := func(x int) int { return max(x, -x) }
	for lane := range 8 {
		o := off + lane*outer
		var a [8]int
		for k := range a {
			a[k] = int(p[o+(k-4)*step])
		}
		need := 2*abs(a[3]-a[4])+abs(a[2]-a[5])/2 <= blim
		for k := range 3 {
			need = need && abs(a[k]-a[k+1]) <= lim && abs(a[k+4]-a[k+5]) <= lim
		}
		if !need {
			continue
		}
		hev := abs(a[2]-a[3]) > thr || abs(a[5]-a[4]) > thr
		f := clamp(a[2] - a[5])
		if kind == 4 && !hev {
			f = 0
		}
		f = clamp(f + 3*(a[4]-a[3]))
		fh := f
		if kind == 6 && !hev {
			fh = 0
		}
		f1, f2 := clamp(fh+4)>>3, clamp(fh+3)>>3
		p0, q0 := clamp(a[3]-128+f2), clamp(a[4]-128-f1)
		if kind == 6 {
			ff := f
			if hev {
				ff = 0
			}
			for k, weight := range []int{9, 18, 27} {
				v := (weight*ff + 63) >> 7
				left, right := a[1+k]-128, a[6-k]-128
				if k == 2 {
					left, right = p0, q0
				}
				p[o+(k-3)*step] = byte(clamp(left+v) + 128)
				p[o+(2-k)*step] = byte(clamp(right-v) + 128)
			}
		} else {
			p[o-step], p[o] = byte(p0+128), byte(q0+128)
			u := (f1 + 1) >> 1
			if hev {
				u = 0
			}
			p[o-2*step], p[o+step] = byte(clamp(a[2]-128+u)+128), byte(clamp(a[5]-128-u)+128)
		}
	}
}
func TestX64Round2UVBoundary(t *testing.T) {
	clamp := func(x int) int { return max(-128, min(127, x)) }
	for _, kind := range []int{4, 6} {
		for value := -128; value <= 127; value++ {
			for lane := range 16 {
				var values [16]int8
				values[lane] = int8(value)
				if uvRoundedZero(archsimd.LoadInt8x16Array(&values), kind) {
					identity := clamp(value+4)>>3 == 0 && clamp(value+3)>>3 == 0
					if kind == 6 {
						for _, w := range []int{9, 18, 27} {
							identity = identity && (w*value+63)>>7 == 0
						}
					}
					if !identity {
						t.Fatalf("unsafe zero kind%d f%d lane%d", kind, value, lane)
					}
				}
			}
		}
	}
	for _, vertical := range []bool{false, true} {
		for _, pitch := range []int{8, 9, 32, 672} {
			for align := range 8 {
				for _, kind := range []int{4, 6} {
					for pattern := range 32 {
						step, outer := pitch, 1
						if vertical {
							step, outer = 1, pitch
						}
						u, v := bytes.Repeat([]byte{167}, align+7*step+7*outer+14), bytes.Repeat([]byte{185}, align+7*step+7*outer+14)
						for axis := range 8 {
							for lane := range 8 {
								value := 128
								if pattern < 4 {
									value = []int{0, 127, 128, 255}[pattern]
								} else if pattern < 16 {
									value = 128 + (axis/4)*(pattern-10)
									if axis == 2 {
										value += lane%3 - 1
									}
								} else {
									value = (axis*53 + lane*29 + pattern*17) & 255
								}
								u[align+axis*step+lane*outer] = byte(value)
								v[align+axis*step+lane*outer] = byte(255 - value)
							}
						}
						wu, wv := bytes.Clone(u), bytes.Clone(v)
						round2ScalarUV(wu, align+4*step, step, outer, 254, 255, 0, kind)
						round2ScalarUV(wv, align+4*step, step, outer, 254, 255, 0, kind)
						if !LoopFilterUV8(u[align:len(u)-13], v[align:len(v)-13], step, outer, 254, 255, 0, kind) || !bytes.Equal(u, wu) || !bytes.Equal(v, wv) {
							t.Fatalf("UV vertical%v pitch%d align%d kind%d pattern%d", vertical, pitch, align, kind, pattern)
						}
					}
				}
			}
		}
	}
}

var round2IntSink int
var round2UintSink uint64

func BenchmarkX64Round2FP(b *testing.B) {
	for _, n := range []int{16, 32, 64, 256} {
		for _, pattern := range []string{"zero", "dc", "dense"} {
			for mode := range 4 {
				b.Run(fmt.Sprintf("n%d/%s/optional%d", n, pattern, mode), func(b *testing.B) {
					var c, scan, qc, dc [256]int16
					var classes [256]byte
					for i := range n {
						scan[i] = int16(n - i)
						if pattern == "dense" {
							c[i] = int16((i*173)%8191 - 4095)
						}
					}
					if pattern == "dc" {
						c[0] = 791
					}
					var qout []int16
					var clout []byte
					if mode&1 != 0 {
						qout = qc[:n]
					}
					if mode&2 != 0 {
						clout = classes[:n]
					}
					b.ReportAllocs()
					for b.Loop() {
						round2IntSink = QuantizeFPBlock(c[:n], scan[:n], qout, dc[:n], n, [2]int16{36, 40}, [2]int16{675, 532}, [2]int16{97, 123}, clout)
					}
				})
			}
		}
	}
}
func BenchmarkX64Round2Plane(b *testing.B) {
	for _, w := range []int{16, 64, 128} {
		for _, h := range []int{16, 64} {
			for _, pad := range []int{0, 17} {
				b.Run(fmt.Sprintf("w%d/h%d/pad%d", w, h, pad), func(b *testing.B) {
					var a, c [10000]byte
					for i := range a {
						a[i] = byte(i * 17)
						c[i] = byte(i * 29)
					}
					b.ReportAllocs()
					for b.Loop() {
						round2UintSink, _ = SSEPlane16(a[:], w+pad, c[:], w+pad, w, h)
					}
				})
			}
		}
	}
}
func TestX64Round2PlaneContiguous(t *testing.T) {
	for _, w := range []int{16, 32, 64} {
		for _, h := range []int{1, 64, 1024, 4096} {
			a, c := make([]byte, w*h), make([]byte, w*h)
			for i := range a {
				a[i] = 255
			}
			want := uint64(w * h * 255 * 255)
			got, ok := SSEPlane16(a, w, c, w, w, h)
			if !ok || got != want {
				t.Fatalf("plane%d/%d got%d want%d", w, h, got, want)
			}
		}
	}
}
func BenchmarkX64Round2Subpel(b *testing.B) {
	for _, n := range []int{16, 64} {
		for _, half := range []bool{false, true} {
			for _, axis := range []int{0, 1, 2} {
				b.Run(fmt.Sprintf("n%d/half%v/axis%d", n, half, axis), func(b *testing.B) {
					var a, c [65 * 80]byte
					for i := range a {
						a[i] = byte(i * 17)
						c[i] = byte(i * 29)
					}
					f := [2]int16{48, 80}
					if half {
						f = [2]int16{64, 64}
					}
					fx, fy := f, f
					if axis == 0 {
						fy = [2]int16{128, 0}
					}
					if axis == 1 {
						fx = [2]int16{128, 0}
					}
					b.ReportAllocs()
					for b.Loop() {
						sum, sse := SubpelStats(a[:], 80, c[:], 80, n, n, fx, fy)
						round2IntSink = sum
						round2UintSink = uint64(sse)
					}
				})
			}
		}
	}
}
func TestX64Round2SADLimit(t *testing.T) {
	var a, c [16 * 32]byte
	for mode := range 4 {
		for i := range a {
			a[i] = byte(i * 17)
			c[i] = byte(i * 29)
			if mode == 1 {
				a[i] = 255
				c[i] = 0
			}
			if mode == 2 {
				c[i] = a[i]
			}
			if mode == 3 {
				a[i] = 128
				c[i] = 127
			}
		}
		for _, limit := range []int{-1, 0, 1, 2000, 10000, 40000, 65000, 65280} {
			want := 0
			for y := range 16 {
				for x := range 16 {
					d := int(a[y*32+x]) - int(c[y*32+x])
					want += max(d, -d)
				}
				if want > limit {
					break
				}
			}
			if got := SAD16Limit(a[:], 32, c[:], 32, 16, limit); got != want {
				t.Fatalf("SAD mode%d limit%d got%d want%d", mode, limit, got, want)
			}
		}
	}
}
func BenchmarkX64Round2SADLimit(b *testing.B) {
	for _, limit := range []int{2000, 10000, 40000, 65000} {
		b.Run(fmt.Sprint(limit), func(b *testing.B) {
			var a, c [16 * 32]byte
			for i := range a {
				a[i] = byte(i * 17)
				c[i] = byte(i * 29)
			}
			b.ReportAllocs()
			for b.Loop() {
				round2IntSink = SAD16Limit(a[:], 32, c[:], 32, 16, limit)
			}
		})
	}
}
func round2Copy8(src []byte, ss int, dst []byte, ds int) {
	if !Copy8Rows(src, ss, dst, ds) {
		for y := range 8 {
			copy(dst[y*ds:y*ds+8], src[y*ss:y*ss+8])
		}
	}
}
func TestX64Round2Copy8(t *testing.T) {
	for _, stride := range []int{0, 1, 7, 8, 17, 32} {
		for delta := -9; delta <= 9; delta++ {
			var got, want [512]byte
			for i := range got {
				got[i] = byte(i * 17)
			}
			want = got
			so, do := 64, 64+delta
			for y := range 8 {
				copy(want[do+y*stride:do+y*stride+8], want[so+y*stride:so+y*stride+8])
			}
			round2Copy8(got[so:], stride, got[do:], stride)
			if got != want {
				t.Fatalf("copy stride%d delta%d", stride, delta)
			}
		}
	}
}
func BenchmarkX64Round2Copy8(b *testing.B) {
	for _, stride := range []int{8, 32} {
		b.Run(fmt.Sprint(stride), func(b *testing.B) {
			var a, c [512]byte
			for i := range a {
				a[i] = byte(i)
			}
			b.ReportAllocs()
			for b.Loop() {
				round2Copy8(a[:], stride, c[:], stride)
			}
		})
	}
}
func TestX64Round2Projection(t *testing.T) {
	var a [8192]byte
	for i := range a {
		a[i] = byte(i * 17)
	}
	for _, w := range []int{16, 32, 64} {
		for _, h := range []int{1, 3, 16, 64} {
			var out [64]int16
			if !ProjectionCols16(a[:], 80, w, h, 2, out[:]) {
				t.Fatal("projection declined")
			}
			for y := range h {
				sum := 0
				for x := range w {
					sum += int(a[y*80+x])
				}
				if out[y] != int16(sum)>>2 {
					t.Fatal("projection mismatch")
				}
			}
		}
		var out [4]int32
		if !AverageQuad16(a[:], 80, &out) {
			t.Fatal("average declined")
		}
		for k := range out {
			sum := 0
			for y := range 8 {
				for x := range 8 {
					sum += int(a[(y+(k/2)*8)*80+x+(k%2)*8])
				}
			}
			if out[k] != int32((sum+32)>>6) {
				t.Fatal("average mismatch")
			}
		}
	}
}
func BenchmarkX64Round2Projection(b *testing.B) {
	var a [8192]byte
	for i := range a {
		a[i] = byte(i * 17)
	}
	for _, w := range []int{16, 64} {
		b.Run(fmt.Sprintf("cols%d", w), func(b *testing.B) {
			var out [64]int16
			b.ReportAllocs()
			for b.Loop() {
				ProjectionCols16(a[:], 80, w, 64, 2, out[:])
			}
		})
	}
	b.Run("average", func(b *testing.B) {
		var out [4]int32
		b.ReportAllocs()
		for b.Loop() {
			AverageQuad16(a[:], 80, &out)
		}
	})
}
func TestX64Round2Variance(t *testing.T) {
	var a, c [256]int16
	for mode := range 3 {
		for i := range a {
			a[i] = int16(i * 173)
			c[i] = int16(i * 271)
			if mode == 1 {
				a[i] = 32767
				c[i] = -32768
			}
			if mode == 2 {
				a[i] = int16(i % 17)
				c[i] = int16(i % 9)
			}
		}
		for _, n := range []int{8, 16, 64, 256} {
			var sum, sse int32
			for i := range n {
				d := int32(a[i]) - int32(c[i])
				sum += d
				sse += d * d
			}
			want := int(sse) - (int(sum) * int(sum) >> 3)
			if got := VectorVariance(a[:], c[:], n, 3); got != want {
				t.Fatalf("variance mode%d n%d got%d want%d", mode, n, got, want)
			}
		}
	}
}
func BenchmarkX64Round2Variance(b *testing.B) {
	var a, c [256]int16
	for i := range a {
		a[i] = int16(i * 173)
		c[i] = int16(i * 271)
	}
	for _, n := range []int{16, 64, 256} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				round2IntSink = VectorVariance(a[:], c[:], n, 3)
			}
		})
	}
}
func BenchmarkX64Round2Macro(b *testing.B) {
	for _, vertical := range []bool{false, true} {
		for _, pattern := range []string{"flat", "edge", "dense"} {
			b.Run(fmt.Sprintf("vertical%v/%s", vertical, pattern), func(b *testing.B) {
				var original, p [1024]byte
				for i := range original {
					original[i] = 127
					if pattern == "edge" {
						original[i] += byte((i / 4) % 3)
					}
					if pattern == "dense" {
						original[i] = byte(i*19 + i/7)
					}
				}
				step, outer := 32, 1
				if vertical {
					step, outer = 1, 32
				}
				b.ReportAllocs()
				for b.Loop() {
					p = original
					if !LoopFilterMacroInternal16(p[:], step, outer, 64, 32, 32, 8) {
						b.Fatal("macro declined")
					}
				}
			})
		}
	}
}
func BenchmarkX64Round2Flat(b *testing.B) {
	for _, kind := range []int{8, 16} {
		for _, vertical := range []bool{false, true} {
			for _, pattern := range []string{"flat", "nearflat", "dense"} {
				b.Run(fmt.Sprintf("kind%d/vertical%v/%s", kind, vertical, pattern), func(b *testing.B) {
					var original, p [1024]byte
					for i := range original {
						original[i] = 128
						if pattern == "nearflat" {
							original[i] += byte(i % 3)
						}
						if pattern == "dense" {
							original[i] = byte(i*19 + i/7)
						}
					}
					step, outer := 32, 1
					if vertical {
						step, outer = 1, 32
					}
					b.ReportAllocs()
					for b.Loop() {
						p = original
						LoopFilter(p[:], 8*step, step, outer, 8, 64, 32, 8, kind)
					}
				})
			}
		}
	}
}

// Frozen b57 full filter: independent of candidate prepass and identity exits.
func round2LoopFilterFrozen(p []byte, off, step, outer, blimit, limit, thresh, kind int) {
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
func TestX64Round2FlatParity(t *testing.T) {
	for _, kind := range []int{8, 16} {
		for _, vertical := range []bool{false, true} {
			for _, pitch := range []int{16, 19, 32} {
				for pattern := range 16 {
					var original [2048]byte
					for i := range original {
						original[i] = 128
						if pattern > 0 && pattern < 8 {
							original[i] = byte(120 + (i*17+i/19)%(pattern+1))
						}
						if pattern >= 8 {
							original[i] = byte(i*37 + i/7 + pattern)
						}
					}
					for _, bl := range []int{0, 64, 254, 255} {
						got, want := original, original
						step, outer := pitch, 1
						if vertical {
							step, outer = 1, pitch
						}
						off := 3 + 8*step
						LoopFilter(got[:], off, step, outer, 16, bl, 32, 8, kind)
						for lane := 0; lane < 16; lane += 8 {
							round2LoopFilterFrozen(want[:], off+lane*outer, step, outer, bl, 32, 8, kind)
						}
						if got != want {
							t.Fatalf("flat kind%d vertical%v pitch%d pattern%d limit%d", kind, vertical, pitch, pattern, bl)
						}
					}
				}
			}
		}
	}
}
func TestX64Round2MacroScalar(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		for _, pitch := range []int{20, 23, 32} {
			for pattern := range 16 {
				var original [2048]byte
				for i := range original {
					original[i] = 128
					if pattern > 0 && pattern < 8 {
						original[i] = byte(120 + (i*17+i/19)%(pattern+1))
					}
					if pattern >= 8 {
						original[i] = byte(i*37 + i/7 + pattern)
					}
				}
				got, want := original, original
				step, outer := pitch, 1
				if vertical {
					step, outer = 1, pitch
				}
				off := 3
				if !LoopFilterMacroInternal16(got[off:], step, outer, 64, 32, 32, 8) {
					t.Fatal("macro declined")
				}
				for edge := 4; edge <= 16; edge += 4 {
					kind, bl := 4, 32
					if edge == 4 {
						kind, bl = 6, 64
					}
					for lane := 0; lane < 16; lane += 8 {
						round2ScalarUV(want[:], off+edge*step+lane*outer, step, outer, bl, 32, 8, kind)
					}
				}
				if got != want {
					t.Fatalf("macro vertical%v pitch%d pattern%d", vertical, pitch, pattern)
				}
			}
		}
	}

}
