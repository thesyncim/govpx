package dsp

import (
	"math/rand/v2"
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

func TestVpxIntProRowRandomAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0x175e, 0x9a11))
	for _, height := range []int{2, 16, 32, 64} {
		for _, stride := range []int{16, 24, 65, 128} {
			for trial := range 32 {
				ref := make([]uint8, stride*height+32)
				for i := range ref {
					ref[i] = uint8(r.UintN(256))
				}
				off := int(r.UintN(uint(stride - 16 + 1)))
				var got, want [16]int16
				VpxIntProRow(got[:], ref, off, stride, height)
				referenceVpxIntProRow(want[:], ref, off, stride, height)
				if got != want {
					t.Fatalf("height=%d stride=%d trial=%d off=%d: got %v want %v",
						height, stride, trial, off, got, want)
				}
			}
		}
	}
}

func referenceVpxIntProRow(hbuf []int16, ref []uint8, refOff, refStride, height int) {
	normFactor := int16(height >> 1)
	for idx := range 16 {
		var acc int16
		for i := range height {
			acc += int16(ref[refOff+i*refStride])
		}
		hbuf[idx] = acc / normFactor
		refOff++
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

func TestVpxIntProColRandomAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xc011, 0x70ad))
	for _, width := range []int{1, 5, 16, 32, 64} {
		for trial := range 64 {
			ref := make([]uint8, width+16)
			for i := range ref {
				ref[i] = uint8(r.UintN(256))
			}
			off := int(r.UintN(16))
			got := VpxIntProCol(ref, off, width)
			want := referenceVpxIntProCol(ref, off, width)
			if got != want {
				t.Fatalf("width=%d trial=%d off=%d: got %d want %d",
					width, trial, off, got, want)
			}
		}
	}
}

func referenceVpxIntProCol(ref []uint8, refOff, width int) int16 {
	var sum int16
	for idx := range width {
		sum += int16(ref[refOff+idx])
	}
	return sum
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

func BenchmarkVpxIntProRowHeight16(b *testing.B) {
	benchmarkVpxIntProRow(b, 16)
}

func BenchmarkVpxIntProRowHeight32(b *testing.B) {
	benchmarkVpxIntProRow(b, 32)
}

func BenchmarkVpxIntProRowHeight64(b *testing.B) {
	benchmarkVpxIntProRow(b, 64)
}

func BenchmarkVpxIntProColWidth16(b *testing.B) {
	benchmarkVpxIntProCol(b, 16)
}

func BenchmarkVpxIntProColWidth32(b *testing.B) {
	benchmarkVpxIntProCol(b, 32)
}

func BenchmarkVpxIntProColWidth64(b *testing.B) {
	benchmarkVpxIntProCol(b, 64)
}

func benchmarkVpxIntProRow(b *testing.B, height int) {
	const stride = 128
	ref := make([]uint8, stride*height+16)
	for i := range ref {
		ref[i] = uint8((i*37 + 11) & 255)
	}
	var hbuf [16]int16
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxIntProRow(hbuf[:], ref, i&15, stride, height)
	}
}

func benchmarkVpxIntProCol(b *testing.B, width int) {
	ref := make([]uint8, width+16)
	for i := range ref {
		ref[i] = uint8((i*29 + 7) & 255)
	}
	var sum int16
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sum += VpxIntProCol(ref, i&15, width)
	}
	_ = sum
}
