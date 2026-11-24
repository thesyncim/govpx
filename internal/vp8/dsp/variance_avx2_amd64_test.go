//go:build amd64 && !purego

package dsp

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Native-specific parity tests that bypass the runtime dispatch and call
// the Native entry points directly, comparing against the scalar
// reference. Skipped on hosts without Native (e.g. Rosetta).

func TestVarianceBlock16xNNativeMatchesScalar(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	const seed int64 = 0xa1b2c3d4
	rng := rand.New(rand.NewSource(seed))

	// Heights covered by the picker: 16 (16x16) and 8 (16x8). Also
	// exercise other even heights to stress the loop.
	heights := []int{2, 4, 6, 8, 10, 12, 14, 16}
	for _, h := range heights {
		for iter := range 25 {
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
			varianceBlock16xNNative(src[srcOff:], srcStride, ref[refOff:], refStride, h, &sum, &sse)
			wantSum, wantSSE := referenceVarianceBlockSized(src[srcOff:], srcStride, ref[refOff:], refStride, 16, h)
			if int(sum) != wantSum || int(sse) != wantSSE {
				t.Fatalf("h=%d iter=%d: got (%d,%d), want (%d,%d)", h, iter, sum, sse, wantSum, wantSSE)
			}
		}
	}
}

func TestVarianceBlock8x16NativeMatchesScalar(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	const seed int64 = 0xb2c3d4e5
	rng := rand.New(rand.NewSource(seed))

	for iter := range 50 {
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
		varianceBlock8x16Native(src[srcOff:], srcStride, ref[refOff:], refStride, &sum, &sse)
		wantSum, wantSSE := referenceVarianceBlockSized(src[srcOff:], srcStride, ref[refOff:], refStride, 8, 16)
		if int(sum) != wantSum || int(sse) != wantSSE {
			t.Fatalf("iter=%d: got (%d,%d), want (%d,%d)", iter, sum, sse, wantSum, wantSSE)
		}
	}
}

func TestFirstPass16NativeMatchesScalar(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	const seed int64 = 0xc3d4e5f6
	rng := rand.New(rand.NewSource(seed))

	for f := range 8 {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 17; height++ {
			for trial := range 3 {
				const stride = 32
				src := make([]byte, stride*(height+2))
				for i := range src {
					src[i] = byte(rng.Intn(256))
				}
				var got, want [17 * 16]uint16
				f0u := uint64(uint16(filter[0])) * 0x0001000100010001
				f1u := uint64(uint16(filter[1])) * 0x0001000100010001
				varFilterBlock2DBilinearFirstPass16Native(src, stride, got[:], height, f0u, f1u)
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

func TestSecondPass16NativeMatchesScalar(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	const seed int64 = 0xd4e5f607
	rng := rand.New(rand.NewSource(seed))

	for f := range 8 {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 16; height++ {
			for trial := range 3 {
				var src [17 * 16]uint16
				for i := range src {
					src[i] = uint16(rng.Intn(256))
				}
				got := make([]byte, 16*16)
				want := make([]byte, 16*16)
				f0u := uint64(uint16(filter[0])) * 0x0001000100010001
				f1u := uint64(uint16(filter[1])) * 0x0001000100010001
				varFilterBlock2DBilinearSecondPass16Native(&src, got, height, f0u, f1u)
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

// BenchmarkVarianceBlock16x16Native measures the Native 16x16 variance
// block kernel directly. Falls through to the Reference path on hosts
// without Native (the dispatch wrapper picks).
func BenchmarkVarianceBlock16x16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock16xNNative(src, 64, ref, 64, 16, &sum, &sse)
	}
}

func BenchmarkVarianceBlock16x16ReferenceDirect(b *testing.B) {
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock16x16Reference(src, 64, ref, 64, &sum, &sse)
	}
}

func BenchmarkVarianceBlock16x8Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock16xNNative(src, 64, ref, 64, 8, &sum, &sse)
	}
}

func BenchmarkVarianceBlock16x8ReferenceDirect(b *testing.B) {
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock16xNReference(src, 64, ref, 64, 8, &sum, &sse)
	}
}

func BenchmarkVarianceBlock8x16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock8x16Native(src, 64, ref, 64, &sum, &sse)
	}
}

func BenchmarkVarianceBlock8x16ReferenceDirect(b *testing.B) {
	src, ref := benchSrc16x16()
	var sum int32
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varianceBlock8xNReference(src, 64, ref, 64, 16, &sum, &sse)
	}
}

func BenchmarkBilinearFirstPass16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	src, _ := benchSrc16x16()
	var dst [17 * 16]uint16
	filter := tables.BilinearFilters[3]
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearFirstPass16Native(src, 64, dst[:], 17, f0u, f1u)
	}
}

func BenchmarkBilinearFirstPass16ReferenceDirect(b *testing.B) {
	src, _ := benchSrc16x16()
	var dst [17 * 16]uint16
	filter := tables.BilinearFilters[3]
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearFirstPass16Reference(src, 64, dst[:], 17, f0u, f1u)
	}
}

func BenchmarkBilinearSecondPass16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
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
		varFilterBlock2DBilinearSecondPass16Native(&src, dst, 16, f0u, f1u)
	}
}

func BenchmarkBilinearSecondPass16ReferenceDirect(b *testing.B) {
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
		varFilterBlock2DBilinearSecondPass16Reference(&src, dst, 16, f0u, f1u)
	}
}
