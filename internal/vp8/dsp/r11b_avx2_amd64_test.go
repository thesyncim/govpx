//go:build amd64

package dsp

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/cpu"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// AVX2-specific parity tests for the R11-B kernels (sixtap16x16, SAD,
// LF horizontal edges). These bypass the runtime dispatch and call
// the AVX2 entry points directly, comparing against the scalar /
// SSE2 reference. Skipped on hosts without AVX2 (e.g. baseline
// Rosetta without ROSETTA_ADVERTISE_AVX=1).

// === sixtap16x16 ===

func TestSixTapPredict16x16AVX2MatchesScalar(t *testing.T) {
	if !cpu.HasAVX2 {
		t.Skip("AVX2 not available on this host")
	}
	rng := rand.New(rand.NewSource(0x6a31))
	const stride = 32
	const rows = 21
	src := make([]byte, stride*rows)
	for i := range src {
		src[i] = byte(rng.Intn(256))
	}
	for xoff := 0; xoff < 8; xoff++ {
		for yoff := 0; yoff < 8; yoff++ {
			var dst [16 * 16]byte
			ref := make([]byte, 16*16)
			var tmp [21 * 16]byte
			hF := &tables.SubPelFilters[xoff]
			vF := &tables.SubPelFilters[yoff]
			sixTapPredict16x16AVX2(&dst[0], 16, &src[0], stride, hF, vF, &tmp)
			sixTapPredict(src, stride, xoff, yoff, ref, 16, 16, 16)
			for i := 0; i < 16*16; i++ {
				if dst[i] != ref[i] {
					t.Fatalf("xoff=%d yoff=%d off=%d: avx2=%d scalar=%d",
						xoff, yoff, i, dst[i], ref[i])
				}
			}
		}
	}
}

func BenchmarkSixTapPredict16x16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
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
		sixTapPredict16x16AVX2(&dst[0], 16, &src[0], stride, hF, vF, &tmp)
	}
}

func BenchmarkSixTapPredict16x16SSE2Direct(b *testing.B) {
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
		sixTapPredict16x16SSE2(&dst[0], 16, &src[0], stride, hF, vF, &tmp)
	}
}

// === SAD AVX2 parity ===

func TestSADAVX2MatchesScalar(t *testing.T) {
	if !cpu.HasAVX2 {
		t.Skip("AVX2 not available on this host")
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
		fn   func(src *byte, srcStride int, ref *byte, refStride int) int32
		w, h int
	}{
		{"16x16", sadBlock16x16AVX2, 16, 16},
		{"16x8", sadBlock16x8AVX2, 16, 8},
		{"8x16", sadBlock8x16AVX2, 8, 16},
		{"8x8", sadBlock8x8AVX2, 8, 8},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for srcOff := 0; srcOff < 8; srcOff++ {
				for refOff := 0; refOff < 8; refOff++ {
					srcSlice := plane[srcOff*planeStride+srcOff:]
					refSlice := ref[refOff*planeStride+refOff:]
					got := int(c.fn(&srcSlice[0], planeStride, &refSlice[0], planeStride))
					want := scalarSAD(srcSlice, planeStride, refSlice, planeStride, c.w, c.h)
					if got != want {
						t.Fatalf("%s offsets src=%d ref=%d: got %d want %d", c.name, srcOff, refOff, got, want)
					}
				}
			}
		})
	}
}

func BenchmarkSAD16x16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	for i := range src {
		src[i] = byte(i)
		ref[i] = byte(i + 11)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x16AVX2(&src[0], 64, &ref[0], 64)
	}
}

func BenchmarkSAD16x16SSE2Direct(b *testing.B) {
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	for i := range src {
		src[i] = byte(i)
		ref[i] = byte(i + 11)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x16SSE2(&src[0], 64, &ref[0], 64)
	}
}

func BenchmarkSAD16x8AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x8AVX2(&src[0], 64, &ref[0], 64)
	}
}

func BenchmarkSAD16x8SSE2Direct(b *testing.B) {
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock16x8SSE2(&src[0], 64, &ref[0], 64)
	}
}

func BenchmarkSAD8x16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock8x16AVX2(&src[0], 64, &ref[0], 64)
	}
}

func BenchmarkSAD8x16SSE2Direct(b *testing.B) {
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock8x16SSE2(&src[0], 64, &ref[0], 64)
	}
}

func BenchmarkSAD8x8AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock8x8AVX2(&src[0], 64, &ref[0], 64)
	}
}

func BenchmarkSAD8x8SSE2Direct(b *testing.B) {
	src := make([]byte, 64*64)
	ref := make([]byte, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sadBlock8x8SSE2(&src[0], 64, &ref[0], 64)
	}
}

// === LoopFilter AVX2 parity ===

func TestLoopFilterEdgeH16AVX2MatchesSSE2(t *testing.T) {
	if !cpu.HasAVX2 {
		t.Skip("AVX2 not available on this host")
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
		for trial := 0; trial < 12; trial++ {
			base := make([]byte, stride*height)
			for i := range base {
				base[i] = byte(rng.Intn(256))
			}
			gotBuf := append([]byte(nil), base...)
			wantBuf := append([]byte(nil), base...)
			loopFilterEdgeH16AVX2(&gotBuf[0], stride, p.blimit, p.limit, p.thresh)
			loopFilterEdgeH16SSE2(&wantBuf[0], stride, p.blimit, p.limit, p.thresh)
			for i, w := range wantBuf {
				if g := gotBuf[i]; g != w {
					t.Fatalf("blimit=%d limit=%d thresh=%d trial=%d byte %d avx2=%d sse2=%d",
						p.blimit, p.limit, p.thresh, trial, i, g, w)
				}
			}
		}
	}
}

func TestMBLoopFilterEdgeH16AVX2MatchesSSE2(t *testing.T) {
	if !cpu.HasAVX2 {
		t.Skip("AVX2 not available on this host")
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
		for trial := 0; trial < 12; trial++ {
			base := make([]byte, stride*height)
			for i := range base {
				base[i] = byte(rng.Intn(256))
			}
			gotBuf := append([]byte(nil), base...)
			wantBuf := append([]byte(nil), base...)
			mbLoopFilterEdgeH16AVX2(&gotBuf[0], stride, p.blimit, p.limit, p.thresh)
			mbLoopFilterEdgeH16SSE2(&wantBuf[0], stride, p.blimit, p.limit, p.thresh)
			for i, w := range wantBuf {
				if g := gotBuf[i]; g != w {
					t.Fatalf("blimit=%d limit=%d thresh=%d trial=%d byte %d avx2=%d sse2=%d",
						p.blimit, p.limit, p.thresh, trial, i, g, w)
				}
			}
		}
	}
}

func BenchmarkLoopFilterEdgeH16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	const stride = 32
	buf := make([]byte, stride*16)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loopFilterEdgeH16AVX2(&buf[0], stride, 32, 16, 8)
	}
}

func BenchmarkLoopFilterEdgeH16SSE2Direct(b *testing.B) {
	const stride = 32
	buf := make([]byte, stride*16)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loopFilterEdgeH16SSE2(&buf[0], stride, 32, 16, 8)
	}
}

func BenchmarkMBLoopFilterEdgeH16AVX2(b *testing.B) {
	if !cpu.HasAVX2 {
		b.Skip("AVX2 not available on this host")
	}
	const stride = 32
	buf := make([]byte, stride*16)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mbLoopFilterEdgeH16AVX2(&buf[0], stride, 32, 16, 8)
	}
}

func BenchmarkMBLoopFilterEdgeH16SSE2Direct(b *testing.B) {
	const stride = 32
	buf := make([]byte, stride*16)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mbLoopFilterEdgeH16SSE2(&buf[0], stride, 32, 16, 8)
	}
}
