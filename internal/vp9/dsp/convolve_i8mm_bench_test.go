//go:build arm64 && !purego

package dsp

import (
	"math/rand/v2"
	"testing"
	"unsafe"

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

func BenchmarkConvolveHoriz8wNEON(b *testing.B) {
	src, dst, stride := benchConvolveSetup(32, 32)
	filterRow := &tables.SubPelFilters8[5]
	b.SetBytes(32 * 32)
	for i := 0; i < b.N; i++ {
		convolveHoriz8wNEON(unsafe.SliceData(src[16*stride:]), stride,
			unsafe.SliceData(dst), stride, &filterRow[0], 32, 32)
	}
}

func BenchmarkConvolveHoriz8I8MM(b *testing.B) {
	src, dst, stride := benchConvolveSetup(32, 32)
	f8, _ := filterTapsInt8(&tables.SubPelFilters8[5])
	b.SetBytes(32 * 32)
	for i := 0; i < b.N; i++ {
		convolveHoriz8I8MM(unsafe.SliceData(src[16*stride:]), stride,
			unsafe.SliceData(dst), stride, &f8[0], 32, 32)
	}
}

func BenchmarkConvolveVert8wNEON(b *testing.B) {
	src, dst, stride := benchConvolveSetup(32, 32)
	filterRow := &tables.SubPelFilters8[5]
	b.SetBytes(32 * 32)
	for i := 0; i < b.N; i++ {
		convolveVert8wNEON(unsafe.SliceData(src[8*stride:]), stride,
			unsafe.SliceData(dst), stride, &filterRow[0], 32, 32)
	}
}

func BenchmarkConvolveVert8I8MM(b *testing.B) {
	src, dst, stride := benchConvolveSetup(32, 32)
	f8, _ := filterTapsInt8(&tables.SubPelFilters8[5])
	b.SetBytes(32 * 32)
	for i := 0; i < b.N; i++ {
		convolveVert8I8MM(unsafe.SliceData(src[8*stride:]), stride,
			unsafe.SliceData(dst), stride, &f8[0], 32, 32)
	}
}
