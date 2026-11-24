package pixel

import (
	"bytes"
	"math/rand/v2"
	"os"
	"runtime"
	"testing"
)

func TestBackendSelected(t *testing.T) {
	t.Logf("GOARCH=%s native SIMD=%v", runtime.GOARCH, Enabled)
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && !Enabled {
		t.Fatal("this validation run requires live native SIMD dispatch")
	}
}
func TestPixelArithmetic(t *testing.T) {
	r := rand.New(rand.NewPCG(0x67123, 0x12369))
	for _, w := range []int{1, 3, 4, 7, 8, 9, 15, 16, 17, 32, 64} {
		for _, h := range []int{1, 3, 8, 16, 64} {
			for off := range 4 {
				ss, rs := w+3, w+7
				src, ref := make([]byte, off+(h-1)*ss+w), make([]byte, off+(h-1)*rs+w)
				for i := range src {
					src[i] = byte(r.Uint32())
				}
				for i := range ref {
					ref[i] = byte(r.Uint32())
				}
				a, b := src[off:], ref[off:]
				wantSAD, wantSum := 0, 0
				var wantSSE uint32
				for y := range h {
					for x := range w {
						d := int(a[y*ss+x]) - int(b[y*rs+x])
						wantSAD += max(d, -d)
						wantSum += d
						wantSSE += uint32(d * d)
					}
				}
				if got := SAD(a, ss, b, rs, w, h); got != wantSAD {
					t.Fatalf("SAD %dx%d off=%d got=%d want=%d", w, h, off, got, wantSAD)
				}
				if sad, sum, sse := DiffMetrics(a, ss, b, rs, w, h); sad != wantSAD || sum != wantSum || sse != wantSSE {
					t.Fatalf("DiffMetrics %dx%d off=%d: got (%d,%d,%d), want (%d,%d,%d)", w, h, off, sad, sum, sse, wantSAD, wantSum, wantSSE)
				}
				sum, sse := Stats(a, ss, b, rs, w, h)
				if sum != wantSum || sse != wantSSE {
					t.Fatalf("Stats %dx%d off=%d got=(%d,%d) want=(%d,%d)", w, h, off, sum, sse, wantSum, wantSSE)
				}
				out := make([]int16, (h-1)*(w+2)+w)
				nz := Subtract(a, ss, b, rs, out, w+2, w, h)
				if nz != (wantSAD != 0) {
					t.Fatal("nonzero mismatch")
				}
				for y := range h {
					for x := range w {
						if out[y*(w+2)+x] != int16(a[y*ss+x])-int16(b[y*rs+x]) {
							t.Fatal("residual mismatch")
						}
					}
				}
			}
		}
	}
}
func TestFilterSignedTapsAndTails(t *testing.T) {
	r := rand.New(rand.NewPCG(138, 964))
	filters := [][]int16{{128, 0}, {48, 80}, {2, -10, 30, 100, 10, -4}, {-1, 4, -11, 40, 104, -12, 5, -1}}
	for _, w := range []int{4, 7, 8, 9, 15, 16, 17, 32, 64} {
		for _, h := range []int{1, 4, 16} {
			for _, tap := range filters {
				for _, vertical := range []bool{false, true} {
					ss, ds := w+len(tap)+3, w+5
					step := 1
					if vertical {
						step = ss
					}
					src := make([]byte, (h-1)*ss+w+(len(tap)-1)*step)
					for i := range src {
						src[i] = byte(r.Uint32())
					}
					dst := bytes.Repeat([]byte{0xab}, (h-1)*ds+w)
					want := append([]byte(nil), dst...)
					for y := range h {
						for x := range w {
							v := 64
							for k, c := range tap {
								v += int(src[y*ss+x+k*step]) * int(c)
							}
							want[y*ds+x] = byte(min(max(v>>7, 0), 255))
						}
					}
					Filter(src, ss, dst, ds, w, h, step, tap)
					if !bytes.Equal(dst, want) {
						t.Fatalf("filter w=%d h=%d taps=%d vertical=%v", w, h, len(tap), vertical)
					}
				}
			}
		}
	}
}
func TestErrorFullSignedDomainAndTails(t *testing.T) {
	values := []int16{-32768, 32767, -32767, 0, -1, 1, 255, -255, 4096, -8192}
	for n := range 130 {
		a, b := make([]int16, n), make([]int16, n)
		var want, energy uint64
		abs := 0
		for i := range n {
			a[i] = values[i%len(values)]
			b[i] = values[(i+3)%len(values)]
			d := int64(a[i]) - int64(b[i])
			want += uint64(d * d)
			v := int64(a[i])
			energy += uint64(v * v)
			abs += max(int(a[i]), -int(a[i]))
		}
		if got := Error(a, b, n); got != want {
			t.Fatalf("n=%d error=%d want=%d", n, got, want)
		}
		if Energy(a) != energy || AbsSum(a, n) != abs {
			t.Fatalf("n=%d energy or absolute sum mismatch", n)
		}
	}
}
func TestPixelAliasingAndAllocations(t *testing.T) {
	a := make([]byte, 16*16)
	for i := range a {
		a[i] = byte(i)
	}
	if got := SAD(a, 16, a, 16, 16, 16); got != 0 {
		t.Fatal("SAD alias")
	}
	if sum, sse := Stats(a, 16, a, 16, 16, 16); sum != 0 || sse != 0 {
		t.Fatal("Stats alias")
	}
	before := append([]byte(nil), a...)
	Average(a, 16, a, 16, 16, 16)
	if !bytes.Equal(a, before) {
		t.Fatal("average alias")
	}
	ref := make([]byte, 16*16)
	out := make([]int16, 256)
	if allocs := testing.AllocsPerRun(100, func() {
		SAD(a, 16, ref, 16, 16, 16)
		Stats(a, 16, ref, 16, 16, 16)
		Subtract(a, 16, ref, 16, out, 16, 16, 16)
		Gather(a, 16, ref, 16, out, 16)
	}); allocs != 0 {
		t.Fatalf("pixel calls allocate: %v", allocs)
	}
}

// Minimal allocations exercise the extra bilinear row/column without relying
// on a frame border, for every coefficient pair and both vector widths.
func TestSubpelMinimalBounds(t *testing.T) {
	r := rand.New(rand.NewPCG(4242, 9191))
	for _, w := range []int{4, 8, 16, 32, 64} {
		for _, h := range []int{1, 3, 16} {
			for off := range 4 {
				ss, rs := w+3, w+5
				src, ref := make([]byte, off+h*ss+w+1), make([]byte, off+(h-1)*rs+w)
				for i := range src {
					src[i] = byte(r.Uint32())
				}
				for i := range ref {
					ref[i] = byte(r.Uint32())
				}
				for x := 0; x <= 128; x += 16 {
					for y := 0; y <= 128; y += 16 {
						fx, fy := [2]int16{int16(128 - x), int16(x)}, [2]int16{int16(128 - y), int16(y)}
						a, b := SubpelStats(src[off:], ss, ref[off:], rs, w, h, fx, fy)
						wa, wb := SubpelStatsScalar(src[off:], ss, ref[off:], rs, w, h, fx, fy)
						if a != wa || b != wb {
							t.Fatalf("%dx%d off=%d phase=%d,%d got=%d,%d want=%d,%d", w, h, off, x, y, a, b, wa, wb)
						}
					}
				}
			}
		}
	}
}

func TestSAD16MaxContrast(t *testing.T) {
	for _, h := range []int{1, 3, 8, 16, 32, 64, 65} {
		a, b := make([]byte, (h-1)*19+16), bytes.Repeat([]byte{255}, (h-1)*23+16)
		want := 16 * h * 255
		if got := SAD16(a, 19, b, 23, h); got != want {
			t.Fatalf("h=%d SAD=%d want=%d", h, got, want)
		}
		var out [4]uint32
		SAD16x4(a, 19, b, b, b, b, 23, h, &out)
		if out != [4]uint32{uint32(want), uint32(want), uint32(want), uint32(want)} {
			t.Fatalf("h=%d x4=%v", h, out)
		}
		for _, limit := range []int{-1, 0, 1, want / 2, want, want + 1} {
			got := SAD16Limit(a, 19, b, 23, h, limit)
			expected := SAD16LimitScalar(a, 19, b, 23, h, limit)
			if got != expected {
				t.Fatalf("h=%d limit=%d got=%d want=%d", h, limit, got, expected)
			}
		}
	}
}

func TestDiffMetricsAndStatsExtremeSums(t *testing.T) {
	for _, w := range []int{4, 8, 16, 32, 64} {
		for _, h := range []int{1, 16, 32, 64} {
			for _, pair := range [][2]byte{{0, 255}, {255, 0}, {255, 255}} {
				a, b := bytes.Repeat([]byte{pair[0]}, w*h), bytes.Repeat([]byte{pair[1]}, w*h)
				d := int(pair[0]) - int(pair[1])
				count := w * h
				sad, sum, sse := DiffMetrics(a, w, b, w, w, h)
				if sad != max(d, -d)*count || sum != d*count || sse != uint32(d*d*count) {
					t.Fatalf("metrics extreme %dx%d %v: (%d,%d,%d)", w, h, pair, sad, sum, sse)
				}
				sum, sse = Stats(a, w, b, w, w, h)
				if sum != d*count || sse != uint32(d*d*count) {
					t.Fatalf("stats extreme %dx%d %v: (%d,%d)", w, h, pair, sum, sse)
				}
			}
		}
	}
}

func TestStatsTallStrips(t *testing.T) {
	for _, w := range []int{4, 8, 16, 48, 64} {
		for _, h := range []int{65, 720, 4096, 4101} {
			ss, rs := w+3, w+7
			for _, pair := range [][2]byte{{0, 255}, {255, 0}, {255, 255}} {
				a := bytes.Repeat([]byte{pair[0]}, (h-1)*ss+w+1)
				b := bytes.Repeat([]byte{pair[1]}, (h-1)*rs+w+3)
				gotSum, gotSSE := Stats(a[1:], ss, b[3:], rs, w, h)
				wantSum, wantSSE := StatsScalar(a[1:], ss, b[3:], rs, w, h)
				if gotSum != wantSum || gotSSE != wantSSE {
					t.Fatalf("%dx%d %v: (%d,%d), want (%d,%d)", w, h, pair, gotSum, gotSSE, wantSum, wantSSE)
				}
			}
		}
	}
}

func BenchmarkStatsTallStrip(b *testing.B) {
	a, ref := make([]byte, 720*1312), make([]byte, 720*1344)
	for i := range a {
		a[i] = byte(i*37 + 11)
	}
	for i := range ref {
		ref[i] = byte(i*19 + 53)
	}
	b.ReportAllocs()
	for b.Loop() {
		Stats(a, 1312, ref, 1344, 16, 720)
	}
}

func TestGatherMinimalWindows(t *testing.T) {
	for _, w := range []int{8, 16} {
		for _, ss := range []int{w, w + 1, w + 15} {
			rs := ss + 3
			for align := range 8 {
				a, b := make([]byte, align+(w-1)*ss+w), make([]byte, align+(w-1)*rs+w)
				for i := range a {
					a[i] = byte(i*37 + 11)
				}
				for i := range b {
					b[i] = byte(i*19 + 53)
				}
				originalA, originalB := bytes.Clone(a), bytes.Clone(b)
				out, want := make([]int16, w*w+2), make([]int16, w*w)
				out[0], out[len(out)-1] = 12345, -12345
				Gather(a[align:], ss, b[align:], rs, out[1:len(out)-1], w)
				GatherScalar(a[align:], ss, b[align:], rs, want, w)
				for i, v := range want {
					if out[i+1] != v {
						t.Fatalf("w%d stride%d align%d lane%d", w, ss, align, i)
					}
				}
				if out[0] != 12345 || out[len(out)-1] != -12345 || !bytes.Equal(a, originalA) || !bytes.Equal(b, originalB) {
					t.Fatal("gather changed sentinel or input")
				}
				Gather(a[align:], ss, a[align:], ss, out[1:len(out)-1], w)
				for _, v := range out[1 : len(out)-1] {
					if v != 0 {
						t.Fatal("read-only input alias")
					}
				}
			}
		}
	}
}
