package pixel

import (
	"bytes"
	"math/rand"
	"testing"
)

func vp9DenoiseScalarRef(
	sig []byte, sigStride int,
	mcAvg []byte, mcAvgStride int,
	avg []byte, avgStride int,
	blockW, blockH, numPelsLog2 int,
	adj0, adj1, adj2, absdiffThresh, strongThresh, weakThresh int,
) bool {
	totalAdj := 0
	for y := range blockH {
		for x := range blockW {
			diff := int(mcAvg[y*mcAvgStride+x]) - int(sig[y*sigStride+x])
			absdiff := diff
			if absdiff < 0 {
				absdiff = -absdiff
			}
			if absdiff <= absdiffThresh {
				avg[y*avgStride+x] = mcAvg[y*mcAvgStride+x]
				totalAdj += diff
				continue
			}
			var adjustment int
			switch {
			case absdiff >= 4 && absdiff <= 7:
				adjustment = adj0
			case absdiff >= 8 && absdiff <= 15:
				adjustment = adj1
			default:
				adjustment = adj2
			}
			if diff > 0 {
				avg[y*avgStride+x] = byte(min(255, int(sig[y*sigStride+x])+adjustment))
				totalAdj += adjustment
			} else {
				avg[y*avgStride+x] = byte(max(0, int(sig[y*sigStride+x])-adjustment))
				totalAdj -= adjustment
			}
		}
	}
	absTotal := totalAdj
	if absTotal < 0 {
		absTotal = -absTotal
	}
	if absTotal <= strongThresh {
		return true
	}
	delta := ((absTotal - strongThresh) >> uint(numPelsLog2)) + 1
	if delta >= 4 {
		return false
	}
	for y := range blockH {
		for x := range blockW {
			diff := int(mcAvg[y*mcAvgStride+x]) - int(sig[y*sigStride+x])
			absdiff := diff
			if absdiff < 0 {
				absdiff = -absdiff
			}
			adjustment := min(absdiff, delta)
			if diff > 0 {
				avg[y*avgStride+x] = byte(max(0, int(avg[y*avgStride+x])-adjustment))
				totalAdj -= adjustment
			} else {
				avg[y*avgStride+x] = byte(min(255, int(avg[y*avgStride+x])+adjustment))
				totalAdj += adjustment
			}
		}
	}
	absTotal = totalAdj
	if absTotal < 0 {
		absTotal = -absTotal
	}
	return absTotal <= weakThresh
}

func TestVP9DenoisePlaneMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	sizes := []struct {
		w, h, log2 int
	}{
		{8, 8, 6},
		{16, 16, 8},
		{32, 32, 10},
		{64, 64, 12},
		{16, 8, 7},
		{32, 16, 9},
	}
	for _, sz := range sizes {
		stride := sz.w + 16
		sig := make([]byte, sz.h*stride)
		mc := make([]byte, sz.h*stride)
		for trial := range 20 {
			bias := (trial%5 - 2) * 3
			for i := range sig {
				s := rng.Intn(256)
				m := max(0, min(255, s+bias+rng.Intn(21)-10))
				sig[i] = byte(s)
				mc[i] = byte(m)
			}
			avgRef := make([]byte, sz.h*stride)
			avgSIMD := make([]byte, sz.h*stride)
			adj0, adj1, adj2 := 4, 5, 7
			thresh := 3
			strong := (1 << uint(sz.log2)) * 2
			if trial%2 == 1 {
				adj0, adj1, adj2 = 5, 6, 8
				thresh = 4
				strong = (1 << uint(sz.log2)) * 3
			}
			wantFilter := vp9DenoiseScalarRef(sig, stride, mc, stride, avgRef, stride, sz.w, sz.h, sz.log2, adj0, adj1, adj2, thresh, strong, strong)
			gotFilter, handled := VP9DenoisePlane(sig, stride, mc, stride, avgSIMD, stride, sz.w, sz.h, sz.log2, adj0, adj1, adj2, thresh, strong, strong)
			if Enabled && !handled {
				t.Fatalf("%dx%d: expected SIMD handled=true", sz.w, sz.h)
			}
			if !handled {
				continue
			}
			if gotFilter != wantFilter {
				t.Fatalf("%dx%d trial=%d: filter=%v, want %v", sz.w, sz.h, trial, gotFilter, wantFilter)
			}
			if wantFilter && !bytes.Equal(avgRef, avgSIMD) {
				t.Fatalf("%dx%d trial=%d: avg bytes mismatch", sz.w, sz.h, trial)
			}
		}
	}
}

func BenchmarkVP9DenoisePlane64x64(b *testing.B) {
	const w, h, stride = 64, 64, 80
	sig := make([]byte, h*stride)
	mc := make([]byte, h*stride)
	avg := make([]byte, h*stride)
	for i := range sig {
		sig[i] = byte(i * 7)
		mc[i] = byte(i*7 + 3)
	}
	b.Run("SIMD", func(b *testing.B) {
		for range b.N {
			VP9DenoisePlane(sig, stride, mc, stride, avg, stride, w, h, 12, 4, 5, 7, 3, 8192, 8192)
		}
	})
	b.Run("Scalar", func(b *testing.B) {
		for range b.N {
			vp9DenoiseScalarRef(sig, stride, mc, stride, avg, stride, w, h, 12, 4, 5, 7, 3, 8192, 8192)
		}
	})
}
