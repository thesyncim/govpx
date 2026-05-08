//go:build amd64

package dsp

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/cpu"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// AVX2-specific parity tests that bypass the runtime dispatch and call
// the AVX2 entry points directly, comparing against the scalar
// reference. Skipped on hosts without AVX2 (e.g. Rosetta).

func TestVarianceBlock16xNAVX2MatchesScalar(t *testing.T) {
	if !cpu.HasAVX2 {
		t.Skip("AVX2 not available on this host")
	}
	const seed int64 = 0xa1b2c3d4
	rng := rand.New(rand.NewSource(seed))

	// Heights covered by the picker: 16 (16x16) and 8 (16x8). Also
	// exercise other even heights to stress the loop.
	heights := []int{2, 4, 6, 8, 10, 12, 14, 16}
	for _, h := range heights {
		for iter := 0; iter < 25; iter++ {
			src := make([]byte, 64*64)
			ref := make([]byte, 64*64)
			for i := range src {
				src[i] = byte(rng.Intn(256))
				ref[i] = byte(rng.Intn(256))
			}
			srcStride := 16 + rng.Intn(48)
			refStride := 16 + rng.Intn(48)
			srcOff := rng.Intn(64*64 - h*srcStride - 16)
			refOff := rng.Intn(64*64 - h*refStride - 16)
			var sum int32
			var sse uint32
			varianceBlock16xNAVX2(&src[srcOff], srcStride, &ref[refOff], refStride, h, &sum, &sse)
			wantSum, wantSSE := referenceVarianceBlockSized(src[srcOff:], srcStride, ref[refOff:], refStride, 16, h)
			if int(sum) != wantSum || int(sse) != wantSSE {
				t.Fatalf("h=%d iter=%d: got (%d,%d), want (%d,%d)", h, iter, sum, sse, wantSum, wantSSE)
			}
		}
	}
}

func TestVarianceBlock8x16AVX2MatchesScalar(t *testing.T) {
	if !cpu.HasAVX2 {
		t.Skip("AVX2 not available on this host")
	}
	const seed int64 = 0xb2c3d4e5
	rng := rand.New(rand.NewSource(seed))

	for iter := 0; iter < 50; iter++ {
		src := make([]byte, 64*64)
		ref := make([]byte, 64*64)
		for i := range src {
			src[i] = byte(rng.Intn(256))
			ref[i] = byte(rng.Intn(256))
		}
		srcStride := 8 + rng.Intn(48)
		refStride := 8 + rng.Intn(48)
		srcOff := rng.Intn(64*64 - 16*srcStride - 8)
		refOff := rng.Intn(64*64 - 16*refStride - 8)
		var sum int32
		var sse uint32
		varianceBlock8x16AVX2(&src[srcOff], srcStride, &ref[refOff], refStride, &sum, &sse)
		wantSum, wantSSE := referenceVarianceBlockSized(src[srcOff:], srcStride, ref[refOff:], refStride, 8, 16)
		if int(sum) != wantSum || int(sse) != wantSSE {
			t.Fatalf("iter=%d: got (%d,%d), want (%d,%d)", iter, sum, sse, wantSum, wantSSE)
		}
	}
}

func TestFirstPass16AVX2MatchesScalar(t *testing.T) {
	if !cpu.HasAVX2 {
		t.Skip("AVX2 not available on this host")
	}
	const seed int64 = 0xc3d4e5f6
	rng := rand.New(rand.NewSource(seed))

	for f := 0; f < 8; f++ {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 17; height++ {
			for trial := 0; trial < 3; trial++ {
				const stride = 32
				src := make([]byte, stride*(height+2))
				for i := range src {
					src[i] = byte(rng.Intn(256))
				}
				var got, want [17 * 16]uint16
				f0u := uint64(uint16(filter[0])) * 0x0001000100010001
				f1u := uint64(uint16(filter[1])) * 0x0001000100010001
				varFilterBlock2DBilinearFirstPass16AVX2(&src[0], stride, &got[0], height, f0u, f1u)
				referenceFirstPass16(src, stride, &want, height, filter)
				for i := 0; i < height*16; i++ {
					if got[i] != want[i] {
						t.Fatalf("filter=%d height=%d trial=%d off=%d: got %d, want %d",
							f, height, trial, i, got[i], want[i])
					}
				}
			}
		}
	}
}

func TestSecondPass16AVX2MatchesScalar(t *testing.T) {
	if !cpu.HasAVX2 {
		t.Skip("AVX2 not available on this host")
	}
	const seed int64 = 0xd4e5f607
	rng := rand.New(rand.NewSource(seed))

	for f := 0; f < 8; f++ {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 16; height++ {
			for trial := 0; trial < 3; trial++ {
				var src [17 * 16]uint16
				for i := range src {
					src[i] = uint16(rng.Intn(256))
				}
				got := make([]byte, 16*16)
				want := make([]byte, 16*16)
				f0u := uint64(uint16(filter[0])) * 0x0001000100010001
				f1u := uint64(uint16(filter[1])) * 0x0001000100010001
				varFilterBlock2DBilinearSecondPass16AVX2(&src, &got[0], height, f0u, f1u)
				referenceSecondPass16(&src, want, height, filter)
				for i := 0; i < height*16; i++ {
					if got[i] != want[i] {
						t.Fatalf("filter=%d height=%d trial=%d off=%d: got %d, want %d",
							f, height, trial, i, got[i], want[i])
					}
				}
			}
		}
	}
}

// BenchmarkVarianceBlock16x16AVX2 measures the AVX2 16x16 variance
// block kernel directly. Falls through to the SSE2 path on hosts
// without AVX2 (the dispatch wrapper picks).
func BenchmarkVarianceBlock16x16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock16xNAVX2(&src[0], 64, &ref[0], 64, 16, &sum, &sse)
	}
}

func BenchmarkVarianceBlock16x16SSE2Direct(b *testing.B) {
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock16x16SSE2(&src[0], 64, &ref[0], 64, &sum, &sse)
	}
}

func BenchmarkVarianceBlock16x8AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock16xNAVX2(&src[0], 64, &ref[0], 64, 8, &sum, &sse)
	}
}

func BenchmarkVarianceBlock16x8SSE2Direct(b *testing.B) {
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock16xNSSE2(&src[0], 64, &ref[0], 64, 8, &sum, &sse)
	}
}

func BenchmarkVarianceBlock8x16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock8x16AVX2(&src[0], 64, &ref[0], 64, &sum, &sse)
	}
}

func BenchmarkVarianceBlock8x16SSE2Direct(b *testing.B) {
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock8xNSSE2(&src[0], 64, &ref[0], 64, 16, &sum, &sse)
	}
}

func BenchmarkBilinearFirstPass16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	src, _ := benchSrc16x16()
	var dst [17 * 16]uint16
	filter := tables.BilinearFilters[3]
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearFirstPass16AVX2(&src[0], 64, &dst[0], 17, f0u, f1u)
	}
}

func BenchmarkBilinearFirstPass16SSE2Direct(b *testing.B) {
	src, _ := benchSrc16x16()
	var dst [17 * 16]uint16
	filter := tables.BilinearFilters[3]
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearFirstPass16SSE2(&src[0], 64, &dst[0], 17, f0u, f1u)
	}
}

func BenchmarkBilinearSecondPass16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	var src [17 * 16]uint16
	for i := range src {
		src[i] = uint16(i)
	}
	dst := make([]byte, 16*16)
	filter := tables.BilinearFilters[5]
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearSecondPass16AVX2(&src, &dst[0], 16, f0u, f1u)
	}
}

func BenchmarkBilinearSecondPass16SSE2Direct(b *testing.B) {
	var src [17 * 16]uint16
	for i := range src {
		src[i] = uint16(i)
	}
	dst := make([]byte, 16*16)
	filter := tables.BilinearFilters[5]
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearSecondPass16SSE2(&src, &dst[0], 16, f0u, f1u)
	}
}
