//go:build arm64 && !purego

package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"math/rand/v2"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func benchConvolveSetup(w, h int) (src []byte, dst []byte, stride int) {
	stride = w + 32
	r := rand.New(rand.NewPCG(1, 2))
	src = make([]byte, stride*(h+32))
	for i := range src {
		src[i] = uint8(r.UintN(256))
	}
	dst = make([]byte, stride*h)
	return src, dst, stride
}

func BenchmarkConvolveHorizGoSIMD(b *testing.B) {
	src, dst, stride := benchConvolveSetup(32, 32)
	filterRow := &tables.SubPelFilters8[5]
	_ = filterRow
	b.SetBytes(32 * 32)
	for i := 0; i < b.N; i++ {
		VpxConvolve8Horiz(src, stride, dst, stride, &tables.SubPelFilters8, 5, 16, 0, 16, 32, 32, 16*stride+3)
	}
}

func BenchmarkConvolveHorizDispatched(b *testing.B) {
	src, dst, stride := benchConvolveSetup(32, 32)
	filterRow := &tables.SubPelFilters8[5]
	_ = filterRow
	b.SetBytes(32 * 32)
	for i := 0; i < b.N; i++ {
		pixel.Filter(src[16*stride:], stride, dst, stride, 32, 32, 1, filterRow[:])
	}
}

func BenchmarkConvolveVertGoSIMD(b *testing.B) {
	src, dst, stride := benchConvolveSetup(32, 32)
	filterRow := &tables.SubPelFilters8[5]
	_ = filterRow
	b.SetBytes(32 * 32)
	for i := 0; i < b.N; i++ {
		VpxConvolve8Vert(src, stride, dst, stride, &tables.SubPelFilters8, 0, 16, 5, 16, 32, 32, 11*stride)
	}
}

func BenchmarkConvolveVertDispatched(b *testing.B) {
	src, dst, stride := benchConvolveSetup(32, 32)
	filterRow := &tables.SubPelFilters8[5]
	_ = filterRow
	b.SetBytes(32 * 32)
	for i := 0; i < b.N; i++ {
		pixel.Filter(src[8*stride:], stride, dst, stride, 32, 32, stride, filterRow[:])
	}
}
