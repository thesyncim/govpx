package dsp

import (
	"math/rand"
	"testing"
)

// TestVpxAvg8x8Known pins the scalar port against hand-computed
// values from vpx_avg_8x8_c (vpx_dsp/avg.c).
func TestVpxAvg8x8Known(t *testing.T) {
	src := make([]uint8, 8*20)
	for i := range src {
		src[i] = 100
	}
	// All-100 block: (6400 + 32) >> 6 = 100.
	if got := VpxAvg8x8(src, 0, 20); got != 100 {
		t.Fatalf("uniform avg8x8: got %d want 100", got)
	}
}

// TestAvg8x8QuadMatchesScalar cross-checks the batched
// (NEON-accelerated on arm64) 16x16 quad helper against four scalar
// VpxAvg8x8 calls over random content, strides and offsets.
func TestAvg8x8QuadMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, stride := range []int{16, 17, 64, 1280} {
		src := make([]uint8, 18*stride+16)
		for i := range src {
			src[i] = uint8(rng.Intn(256))
		}
		for _, off := range []int{0, 1, stride + 3} {
			var got [4]int32
			Avg8x8Quad(src, off, stride, &got)
			want := [4]int32{
				int32(VpxAvg8x8(src, off, stride)),
				int32(VpxAvg8x8(src, off+8, stride)),
				int32(VpxAvg8x8(src, off+8*stride, stride)),
				int32(VpxAvg8x8(src, off+8*stride+8, stride)),
			}
			if got != want {
				t.Fatalf("stride=%d off=%d: got %v want %v", stride, off, got, want)
			}
		}
	}
}

func BenchmarkAvg8x8Quad(b *testing.B) {
	const stride = 1280
	src := make([]uint8, 16*stride+16)
	for i := range src {
		src[i] = uint8(i * 3)
	}
	var out [4]int32
	b.ReportAllocs()
	for b.Loop() {
		Avg8x8Quad(src, 0, stride, &out)
	}
}

func BenchmarkAvg8x8QuadScalar(b *testing.B) {
	const stride = 1280
	src := make([]uint8, 16*stride+16)
	for i := range src {
		src[i] = uint8(i * 3)
	}
	var out [4]int32
	b.ReportAllocs()
	for b.Loop() {
		out[0] = int32(VpxAvg8x8(src, 0, stride))
		out[1] = int32(VpxAvg8x8(src, 8, stride))
		out[2] = int32(VpxAvg8x8(src, 8*stride, stride))
		out[3] = int32(VpxAvg8x8(src, 8*stride+8, stride))
	}
}
