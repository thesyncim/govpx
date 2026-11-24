//go:build amd64 && !purego

package dsp

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Native-specific parity tests for the R11-B kernels (sixtap16x16, SAD,
// LF horizontal edges). These bypass the runtime dispatch and call
// the Native entry points directly, comparing against the scalar /
// Reference reference. Skipped on hosts without Native (e.g. baseline
// Rosetta without ROSETTA_ADVERTISE_AVX=1).

// === sixtap16x16 ===

func TestSixTapPredict16x16NativeMatchesScalar(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	rng := rand.New(rand.NewSource(0x6a31))
	const stride = 32
	const rows = 21
	src := make([]byte, stride*rows)
	for i := range src {
		src[i] = byte(rng.Intn(256))
	}
	for xoff := range 8 {
		for yoff := range 8 {
			var dst [16 * 16]byte
			ref := make([]byte, 16*16)
			var tmp [21 * 16]byte
			hF := &tables.SubPelFilters[xoff]
			vF := &tables.SubPelFilters[yoff]
			sixTapPredict16x16Native(dst[:], 16, src, stride, hF, vF, &tmp)
			sixTapPredict(src, stride, xoff, yoff, ref, 16, 16, 16)
			for i := range 16 * 16 {
				if dst[i] != ref[i] {
					t.Fatalf("xoff=%d yoff=%d off=%d: avx2=%d scalar=%d",
						xoff, yoff, i, dst[i], ref[i])
				}
			}
		}
	}
}

func BenchmarkSixTapPredict16x16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	const stride = 32
	src := make([]byte, stride*21)
	for i := range src {
		src[i] = byte(i * 37)
	}
	var dst [16 * 16]byte
	var tmp [21 * 16]byte
	hF := &tables.SubPelFilters[3]
	vF := &tables.SubPelFilters[5]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sixTapPredict16x16Native(dst[:], 16, src, stride, hF, vF, &tmp)
	}
}

func BenchmarkSixTapPredict16x16ReferenceDirect(b *testing.B) {
	const stride = 32
	src := make([]byte, stride*21)
	for i := range src {
		src[i] = byte(i * 37)
	}
	var dst [16 * 16]byte
	var tmp [21 * 16]byte
	hF := &tables.SubPelFilters[3]
	vF := &tables.SubPelFilters[5]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sixTapPredict16x16Reference(dst[:], 16, src, stride, hF, vF, &tmp)
	}
}

// === SAD Native parity ===

func TestSADNativeMatchesScalar(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	const planeStride = 64
	const planeRows = 64
	plane := make([]byte, planeStride*planeRows)
	ref := make([]byte, planeStride*planeRows)
	rng := rand.New(rand.NewSource(0xC0DEFACE))
	for i := range plane {
		plane[i] = byte(rng.Intn(256))
		ref[i] = byte(rng.Intn(256))
	}
	cases := []struct {
		name string
		fn   func(src []byte, srcStride int, ref []byte, refStride int) int32
		w, h int
	}{
		{"16x16", sadBlock16x16Native, 16, 16},
		{"16x8", sadBlock16x8Native, 16, 8},
		{"8x16", sadBlock8x16Native, 8, 16},
		{"8x8", sadBlock8x8Native, 8, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for srcOff := range 8 {
				for refOff := range 8 {
					srcSlice := plane[srcOff*planeStride+srcOff:]
					refSlice := ref[refOff*planeStride+refOff:]
					got := int(c.fn(srcSlice, planeStride, refSlice, planeStride))
					want := scalarSAD(srcSlice, planeStride, refSlice, planeStride, c.w, c.h)
					if got != want {
						t.Fatalf("%s offsets src=%d ref=%d: got %d want %d", c.name, srcOff, refOff, got, want)
					}
				}
			}
		})
	}
}

func TestSAD16x16x4NativeMatchesScalar(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	const planeStride = 64
	const planeRows = 64
	plane := make([]byte, planeStride*planeRows)
	ref := make([]byte, planeStride*planeRows)
	rng := rand.New(rand.NewSource(0x5ad4d))
	for i := range plane {
		plane[i] = byte(rng.Intn(256))
		ref[i] = byte(rng.Intn(256))
	}
	for srcOff := range 8 {
		for refOff := range 8 {
			srcSlice := plane[srcOff*planeStride+srcOff:]
			ref0 := ref[refOff*planeStride+refOff:]
			ref1 := ref[refOff*planeStride+refOff+1:]
			ref2 := ref[(refOff+1)*planeStride+refOff:]
			ref3 := ref[(refOff+1)*planeStride+refOff+1:]
			var got [4]uint32
			sadBlock16x16x4Native(srcSlice, planeStride, ref0,
				ref1, ref2, ref3, planeStride, &got)
			refs := [][]byte{ref0, ref1, ref2, ref3}
			for i, refSlice := range refs {
				want := scalarSAD(srcSlice, planeStride, refSlice,
					planeStride, 16, 16)
				if int(got[i]) != want {
					t.Fatalf("offsets src=%d ref=%d lane=%d: got %d want %d",
						srcOff, refOff, i, got[i], want)
				}
			}
		}
	}
}

func BenchmarkSAD16x16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	for i := range src {
		src[i] = byte(i)
		ref[i] = byte(i + 11)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x16Native(src, 64, ref, 64)
	}
}

func BenchmarkSAD16x16x4Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	const stride = 64
	src := make([]byte, stride*32)
	ref := make([]byte, stride*32)
	for i := range src {
		src[i] = byte(i*3 + 7)
		ref[i] = byte(i*5 + 11)
	}
	srcSlice := src[3*stride+5:]
	ref0 := ref[2*stride+7:]
	ref1 := ref[3*stride+9:]
	ref2 := ref[4*stride+11:]
	ref3 := ref[5*stride+13:]
	var out [4]uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x16x4Native(srcSlice, stride, ref0, ref1, ref2, ref3,
			stride, &out)
	}
}

func BenchmarkSAD16x16ReferenceDirect(b *testing.B) {
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	for i := range src {
		src[i] = byte(i)
		ref[i] = byte(i + 11)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x16Reference(src, 64, ref, 64)
	}
}

func BenchmarkSAD16x8Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x8Native(src, 64, ref, 64)
	}
}

func BenchmarkSAD16x8ReferenceDirect(b *testing.B) {
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x8Reference(src, 64, ref, 64)
	}
}

func BenchmarkSAD8x16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock8x16Native(src, 64, ref, 64)
	}
}

func BenchmarkSAD8x16ReferenceDirect(b *testing.B) {
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock8x16Reference(src, 64, ref, 64)
	}
}

func BenchmarkSAD8x8Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock8x8Native(src, 64, ref, 64)
	}
}

func BenchmarkSAD8x8ReferenceDirect(b *testing.B) {
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock8x8Reference(src, 64, ref, 64)
	}
}

// === LoopFilter Native parity ===

func TestLoopFilterEdgeH16NativeMatchesReference(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	rng := rand.New(rand.NewSource(0xACEFACE))
	type params struct {
		blimit, limit, thresh byte
	}
	cases := []params{
		{0, 0, 0},
		{8, 4, 0},
		{16, 8, 4},
		{32, 16, 8},
		{64, 32, 16},
		{128, 64, 32},
		{255, 63, 7},
	}
	const stride = 32
	const height = 16
	for _, p := range cases {
		for trial := range 12 {
			base := make([]byte, stride*height)
			for i := range base {
				base[i] = byte(rng.Intn(256))
			}
			gotBuf := append([]byte(nil), base...)
			wantBuf := append([]byte(nil), base...)
			loopFilterEdgeH16Native(gotBuf, stride, p.blimit, p.limit, p.thresh)
			loopFilterEdgeH16Reference(wantBuf, stride, p.blimit, p.limit, p.thresh)
			for i, w := range wantBuf {
				if g := gotBuf[i]; g != w {
					t.Fatalf("blimit=%d limit=%d thresh=%d trial=%d byte %d avx2=%d sse2=%d",
						p.blimit, p.limit, p.thresh, trial, i, g, w)
				}
			}
		}
	}
}

func TestMBLoopFilterEdgeH16NativeMatchesReference(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("Native not available on this host")
	}
	rng := rand.New(rand.NewSource(0xBEEFBABE))
	type params struct {
		blimit, limit, thresh byte
	}
	cases := []params{
		{0, 0, 0},
		{8, 4, 0},
		{16, 8, 4},
		{32, 16, 8},
		{64, 32, 16},
		{128, 64, 32},
		{255, 63, 7},
	}
	const stride = 32
	const height = 16
	for _, p := range cases {
		for trial := range 12 {
			base := make([]byte, stride*height)
			for i := range base {
				base[i] = byte(rng.Intn(256))
			}
			gotBuf := append([]byte(nil), base...)
			wantBuf := append([]byte(nil), base...)
			mbLoopFilterEdgeH16Native(gotBuf, stride, p.blimit, p.limit, p.thresh)
			mbLoopFilterEdgeH16Reference(wantBuf, stride, p.blimit, p.limit, p.thresh)
			for i, w := range wantBuf {
				if g := gotBuf[i]; g != w {
					t.Fatalf("blimit=%d limit=%d thresh=%d trial=%d byte %d avx2=%d sse2=%d",
						p.blimit, p.limit, p.thresh, trial, i, g, w)
				}
			}
		}
	}
}

func BenchmarkLoopFilterEdgeH16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	const stride = 32
	buf := make([]byte, stride*16)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loopFilterEdgeH16Native(buf, stride, 32, 16, 8)
	}
}

func BenchmarkLoopFilterEdgeH16ReferenceDirect(b *testing.B) {
	const stride = 32
	buf := make([]byte, stride*16)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loopFilterEdgeH16Reference(buf, stride, 32, 16, 8)
	}
}

func BenchmarkMBLoopFilterEdgeH16Native(b *testing.B) {
	if !pixel.Enabled {
		b.Skip("Native not available on this host")
	}
	const stride = 32
	buf := make([]byte, stride*16)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mbLoopFilterEdgeH16Native(buf, stride, 32, 16, 8)
	}
}

func BenchmarkMBLoopFilterEdgeH16ReferenceDirect(b *testing.B) {
	const stride = 32
	buf := make([]byte, stride*16)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mbLoopFilterEdgeH16Reference(buf, stride, 32, 16, 8)
	}
}
