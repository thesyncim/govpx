package dsp

import (
	"math/rand"
	"testing"
)

// Pinning tests for the integer-projection DSP kernels in int_pro.go.
// Reference values are hand-computed from the libvpx v1.16.0 reference
// implementations in vpx_dsp/avg.c.

// TestVpxIntProRowBasicHeight2 verifies the simplest height (= 2)
// projection: hbuf[idx] = (ref[0,idx] + ref[refStride,idx]) / 1.
func TestVpxIntProRowBasicHeight2(t *testing.T) {
	// 2-row, 16-column input. Row 0 = 0..15, Row 1 = 16..31.
	refStride := 16
	ref := make([]uint8, refStride*2)
	for i := 0; i < refStride*2; i++ {
		ref[i] = uint8(i)
	}
	var hbuf [16]int16
	VpxIntProRow(hbuf[:], ref, 0, refStride, 2)
	// norm_factor = height/2 = 1, so hbuf[idx] = ref[idx] + ref[refStride+idx].
	for idx := range 16 {
		want := int16(ref[idx]) + int16(ref[refStride+idx])
		if hbuf[idx] != want {
			t.Errorf("hbuf[%d]: got %d want %d", idx, hbuf[idx], want)
		}
	}
}

// TestVpxIntProRowHeight16NormFactor8 verifies the realistic
// BLOCK_16x16 case (height=16, norm_factor=8) — 16 rows of value 200
// project to hbuf[idx] = (200*16)/8 = 400.
func TestVpxIntProRowHeight16NormFactor8(t *testing.T) {
	refStride := 16
	ref := make([]uint8, refStride*16)
	for i := range ref {
		ref[i] = 200
	}
	var hbuf [16]int16
	VpxIntProRow(hbuf[:], ref, 0, refStride, 16)
	// Each column-sum = 200*16 = 3200; / 8 = 400.
	for idx := range 16 {
		if hbuf[idx] != 400 {
			t.Errorf("hbuf[%d]: got %d want 400", idx, hbuf[idx])
		}
	}
}

// TestVpxIntProRowAsymmetricColumns verifies that the column offset
// stride is correct: each hbuf[idx] sees only column idx (not the
// adjacent columns), so a "comb" pattern alternating between 0 and
// 100 must produce hbuf alternating between 0 and 100 (height=16,
// norm_factor=8, 16 rows of identical value).
func TestVpxIntProRowAsymmetricColumns(t *testing.T) {
	refStride := 16
	ref := make([]uint8, refStride*16)
	for y := range 16 {
		for x := range 16 {
			if x%2 == 0 {
				ref[y*refStride+x] = 0
			} else {
				ref[y*refStride+x] = 100
			}
		}
	}
	var hbuf [16]int16
	VpxIntProRow(hbuf[:], ref, 0, refStride, 16)
	for idx := range 16 {
		// column idx sums to 16*v / 8 = 2*v.
		var want int16
		if idx%2 == 0 {
			want = 0
		} else {
			want = 200
		}
		if hbuf[idx] != want {
			t.Errorf("hbuf[%d]: got %d want %d", idx, hbuf[idx], want)
		}
	}
}

// TestVpxIntProColUniform verifies the basic accumulation for the
// simplest case: width=64 of value 250 -> 250*64 = 16000.
func TestVpxIntProColUniform(t *testing.T) {
	ref := make([]uint8, 64)
	for i := range ref {
		ref[i] = 250
	}
	got := VpxIntProCol(ref, 0, 64)
	if got != 16000 {
		t.Errorf("VpxIntProCol width=64,val=250: got %d want 16000", got)
	}
}

// TestVpxIntProColWidth16 verifies width=16 with a known pattern.
func TestVpxIntProColWidth16(t *testing.T) {
	ref := make([]uint8, 16)
	for i := range ref {
		ref[i] = uint8(i * 2) // 0, 2, 4, ..., 30; sum = 2*(0+1+..+15) = 240.
	}
	got := VpxIntProCol(ref, 0, 16)
	if got != 240 {
		t.Errorf("VpxIntProCol pattern: got %d want 240", got)
	}
}

// TestIntProRowStripsMatchesScalar cross-checks the batched
// (NEON-accelerated on arm64) strip helper against per-strip scalar
// VpxIntProRow over the search geometries vp9_int_pro_motion_estimation
// uses (heights 16/32/64, 1..8 strips) plus off-domain heights that
// must take the scalar fallback.
func TestIntProRowStripsMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, height := range []int{2, 8, 16, 32, 64} {
		for _, strips := range []int{1, 2, 4, 8} {
			refStride := strips*16 + 7 // deliberately non-multiple padding
			ref := make([]uint8, refStride*height+32)
			for i := range ref {
				ref[i] = uint8(rng.Intn(256))
			}
			refOff := 3
			got := make([]int16, strips*16)
			want := make([]int16, strips*16)
			IntProRowStrips(got, ref, refOff, refStride, height, strips)
			for s := range strips {
				VpxIntProRow(want[s*16:s*16+16], ref, refOff+s*16, refStride, height)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("height=%d strips=%d: hbuf[%d] got %d want %d",
						height, strips, i, got[i], want[i])
				}
			}
		}
	}
}

// TestIntProColsMatchesScalar cross-checks the batched
// (NEON-accelerated on arm64) column helper against per-row scalar
// VpxIntProCol over the widths/rows/norm-factors
// vp9_int_pro_motion_estimation uses.
func TestIntProColsMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for _, width := range []int{16, 32, 64} {
		normFactor := 3 + (width >> 5)
		for _, rows := range []int{1, 16, 32, 64, 128} {
			refStride := width + 5
			ref := make([]uint8, refStride*rows+16)
			for i := range ref {
				ref[i] = uint8(rng.Intn(256))
			}
			refOff := 2
			got := make([]int16, rows)
			want := make([]int16, rows)
			IntProCols(got, ref, refOff, refStride, width, rows, normFactor)
			for idx := range rows {
				want[idx] = VpxIntProCol(ref, refOff+idx*refStride, width) >> uint(normFactor)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("width=%d rows=%d: vbuf[%d] got %d want %d",
						width, rows, i, got[i], want[i])
				}
			}
		}
	}
}

// Benchmarks mirror the BLOCK_64X64 geometry of one int-pro motion
// search: 8 hbuf strips at height 64 and 128 column projections at
// width 64.

func BenchmarkIntProRowStrips64(b *testing.B) {
	const height, strips, refStride = 64, 8, 160
	ref := make([]uint8, refStride*height+strips*16)
	for i := range ref {
		ref[i] = uint8(i * 7)
	}
	hbuf := make([]int16, strips*16)
	b.ReportAllocs()
	for b.Loop() {
		IntProRowStrips(hbuf, ref, 0, refStride, height, strips)
	}
}

func BenchmarkIntProRowStripsScalar64(b *testing.B) {
	const height, strips, refStride = 64, 8, 160
	ref := make([]uint8, refStride*height+strips*16)
	for i := range ref {
		ref[i] = uint8(i * 7)
	}
	hbuf := make([]int16, strips*16)
	b.ReportAllocs()
	for b.Loop() {
		for s := range strips {
			VpxIntProRow(hbuf[s*16:s*16+16], ref, s*16, refStride, height)
		}
	}
}

func BenchmarkIntProCols64(b *testing.B) {
	const width, rows, refStride = 64, 128, 160
	ref := make([]uint8, refStride*rows+width)
	for i := range ref {
		ref[i] = uint8(i * 5)
	}
	vbuf := make([]int16, rows)
	b.ReportAllocs()
	for b.Loop() {
		IntProCols(vbuf, ref, 0, refStride, width, rows, 5)
	}
}

func BenchmarkIntProColsScalar64(b *testing.B) {
	const width, rows, refStride = 64, 128, 160
	ref := make([]uint8, refStride*rows+width)
	for i := range ref {
		ref[i] = uint8(i * 5)
	}
	vbuf := make([]int16, rows)
	b.ReportAllocs()
	for b.Loop() {
		for idx := range rows {
			vbuf[idx] = VpxIntProCol(ref, idx*refStride, width) >> 5
		}
	}
}

// TestVpxVectorVarZeroDiff verifies that ref == src produces var = 0.
func TestVpxVectorVarZeroDiff(t *testing.T) {
	ref := make([]int16, 64)
	src := make([]int16, 64)
	for i := range ref {
		ref[i] = int16(100 + i)
		src[i] = ref[i]
	}
	for _, bwl := range []int{2, 3, 4} {
		got := VpxVectorVar(ref, src, bwl)
		if got != 0 {
			t.Errorf("VpxVectorVar bwl=%d zero-diff: got %d want 0", bwl, got)
		}
	}
}

// TestVpxVectorVarConstantDiff verifies behaviour on a uniform diff:
// every element differs by k, so mean = k*width, sse = k*k*width, and
// var = k*k*width - ((k*width)^2 >> (bwl+2)) = 0 (since
// width = 4 << bwl and (k*width)^2 / (4*width) = k*k*width).
func TestVpxVectorVarConstantDiff(t *testing.T) {
	for _, bwl := range []int{2, 3, 4} {
		width := 4 << bwl
		ref := make([]int16, width)
		src := make([]int16, width)
		for i := range ref {
			ref[i] = 200
			src[i] = 150 // diff = 50.
		}
		got := VpxVectorVar(ref, src, bwl)
		if got != 0 {
			t.Errorf("VpxVectorVar bwl=%d constant-diff: got %d want 0", bwl, got)
		}
	}
}

// TestVpxVectorVarKnownPattern verifies a hand-computed nonzero
// result. With bwl=2 (width=16), ref = [10,20,...,160] and src all
// zero:
//
//	diffs = [10, 20, ..., 160]; mean = sum = 10*16*17/2 = 1360
//	sse  = 10^2 * (1^2 + 2^2 + ... + 16^2) = 100 * 1496 = 149600
//	var  = 149600 - (1360^2 >> 4)  = 149600 - 115600 = 34000
func TestVpxVectorVarKnownPattern(t *testing.T) {
	bwl := 2
	width := 4 << bwl // 16
	ref := make([]int16, width)
	src := make([]int16, width)
	for i := range width {
		ref[i] = int16(10 * (i + 1))
		src[i] = 0
	}
	got := VpxVectorVar(ref, src, bwl)
	if got != 34000 {
		t.Errorf("VpxVectorVar known-pattern: got %d want 34000", got)
	}
}

// TestVpxVectorVarSimdMatchesScalar drives the NEON vector-var kernel
// against the scalar port across the int_pro projection domain
// (values in [0, 510], the vpx_int_pro_row/col output range).
func TestVpxVectorVarSimdMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x77a5))
	for bwl := 0; bwl <= 4; bwl++ {
		width := 4 << bwl
		for trial := range 200 {
			ref := make([]int16, width)
			src := make([]int16, width)
			for i := range ref {
				ref[i] = int16(rng.Intn(511))
				src[i] = int16(rng.Intn(511))
			}
			got := VpxVectorVar(ref, src, bwl)
			want := vpxVectorVarScalar(ref, src, bwl)
			if got != want {
				t.Fatalf("bwl=%d trial=%d: got %d want %d", bwl, trial, got, want)
			}
		}
	}
}

func BenchmarkVpxVectorVar(b *testing.B) {
	rng := rand.New(rand.NewSource(9))
	const bwl = 4
	width := 4 << bwl
	ref := make([]int16, width)
	src := make([]int16, width)
	for i := range ref {
		ref[i] = int16(rng.Intn(511))
		src[i] = int16(rng.Intn(511))
	}
	b.ReportAllocs()
	acc := 0
	for i := 0; i < b.N; i++ {
		acc += VpxVectorVar(ref, src, bwl)
	}
	if acc == 0 {
		b.Fatal("unexpected zero accumulator")
	}
}
