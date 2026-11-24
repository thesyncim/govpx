package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/dsp"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestChromaSubpelPredictorMatchesLibvpx pins the VP8 chroma inter predictor
// mechanics that sit between motion-vector selection and the sub-pixel DSP
// kernels. The decoder and encoder analysis paths both depend on these rules:
// chroma MV derivation, copy-vs-subpel dispatch, and the sixtap/bilinear filter
// tables and arithmetic.
//
// The reference code in this test is written from libvpx v1.16.0
// vp8/common/reconinter.c and vp8/common/filter.c, so the assertions remain
// useful after package moves without keeping the old root-package audit file.
func TestChromaSubpelPredictorMatchesLibvpx(t *testing.T) {
	// (a) Chroma MV derivation — exhaustive sweep over the relevant
	// MV range. Cohort uses non-fullpixel (version=0); we verify
	// both branches anyway to keep the audit pin tight.
	for _, fullPixel := range []bool{false, true} {
		for mvRow := int16(-256); mvRow <= 256; mvRow++ {
			gotRow := fastChromaMVDerivation(int(mvRow), fullPixel)
			wantRow := libvpxChromaMVDerivationReference(int(mvRow), fullPixel)
			if gotRow != wantRow {
				t.Fatalf("chroma MV derivation skew at mvRow=%d fullPixel=%v: govpx=%d libvpx=%d",
					mvRow, fullPixel, gotRow, wantRow)
			}
		}
	}

	// (c) Sixtap/bilinear/copy dispatch decision is a function of
	// (uvMVRow & 7) | (uvMVCol & 7). Exhaustively verify the
	// decision matches libvpx's `_16x16mv.as_int & 0x00070007`
	// idiom for every (uvMVRow, uvMVCol) sub-pel bit combination.
	for uvRow := range 8 {
		for uvCol := range 8 {
			govpxCopy := (uvRow | uvCol) == 0
			// libvpx packs row/col into a uint32 via the int_mv union;
			// negative shorts sign-extend in the union into the upper
			// half of as_int, but `& 0x00070007` only looks at the
			// low 3 bits of each half, so it's equivalent to checking
			// row&7 and col&7 directly.
			libvpxAsInt := uint32(uvRow&0xFFFF) | (uint32(uvCol&0xFFFF) << 16)
			libvpxCopy := (libvpxAsInt & 0x00070007) == 0
			if govpxCopy != libvpxCopy {
				t.Fatalf("chroma dispatch skew at uvRow=%d uvCol=%d: govpxCopy=%v libvpxCopy=%v",
					uvRow, uvCol, govpxCopy, libvpxCopy)
			}
		}
	}

	// (d) Sub-pel filter table parity. libvpx vp8_sub_pel_filters
	// (filter.c:20-31) — 8 phase positions × 6 taps.
	wantSubPel := [8][6]int16{
		{0, 0, 128, 0, 0, 0},
		{0, -6, 123, 12, -1, 0},
		{2, -11, 108, 36, -8, 1},
		{0, -9, 93, 50, -6, 0},
		{3, -16, 77, 77, -16, 3},
		{0, -6, 50, 93, -9, 0},
		{1, -8, 36, 108, -11, 2},
		{0, -1, 12, 123, -6, 0},
	}
	if tables.SubPelFilters != wantSubPel {
		t.Fatalf("SubPelFilters table drift: got=%v want=%v",
			tables.SubPelFilters, wantSubPel)
	}
	wantBilinear := [8][2]int16{
		{128, 0}, {112, 16}, {96, 32}, {80, 48},
		{64, 64}, {48, 80}, {32, 96}, {16, 112},
	}
	if tables.BilinearFilters != wantBilinear {
		t.Fatalf("BilinearFilters table drift: got=%v want=%v",
			tables.BilinearFilters, wantBilinear)
	}
	// libvpx VP8_FILTER_WEIGHT=128, VP8_FILTER_SHIFT=7
	// (vp8/common/filter.h).
	if tables.FilterWeight != 128 {
		t.Fatalf("FilterWeight = %d, want 128", tables.FilterWeight)
	}
	if tables.FilterShift != 7 {
		t.Fatalf("FilterShift = %d, want 7", tables.FilterShift)
	}

	// (d') Filter kernel parity by representative outputs against
	// the algorithm reference. We construct a deterministic 20x20
	// neighborhood, then take a view starting at (4,4) so the
	// sixtap kernel can read src[-2..src+10] in both dims and the
	// bilinear kernel can read src[0..src+8]. The libvpx and govpx
	// kernels both read relative to the supplied src_ptr with
	// pre-decrement on the row stride (sixtap uses src - 2*stride
	// and per-cell -2..+3 column reach); we mirror that by passing
	// srcView and using libvpx's exact relative addressing.
	const srcStride = 20
	src := make([]byte, srcStride*srcStride)
	for i := range src {
		// Deterministic non-trivial pattern.
		src[i] = byte((i*73 ^ (i >> 1) ^ ((i + 5) * 11)) & 0xFF)
	}
	// View into "src" at offset (4,4) so src[-2*stride..src+10] +
	// src[-2..src+10] is valid for an 8x8 sixtap output (1 view base,
	// reach to -2 rows up + -2 cols left, plus 8+3 rows down + 8+3
	// cols right).
	srcView := src[4*srcStride+4:]
	const dstStride = 8
	gotDst := make([]byte, 64)
	wantDst := make([]byte, 64)
	for yoffset := range 8 {
		for xoffset := range 8 {
			if xoffset == 0 && yoffset == 0 {
				continue
			}
			// govpx implementation (the production path).
			for i := range gotDst {
				gotDst[i] = 0
			}
			dsp.SixTapPredict8x8(srcView, srcStride, xoffset, yoffset, gotDst, dstStride)
			// libvpx-faithful reference (recomputed inline).
			for i := range wantDst {
				wantDst[i] = 0
			}
			libvpxSixtap8x8Reference(srcView, srcStride, xoffset, yoffset, wantDst, dstStride)
			for i := range gotDst {
				if gotDst[i] != wantDst[i] {
					t.Fatalf("SixTapPredict8x8 drift at xoffset=%d yoffset=%d byte=%d: govpx=%d libvpx=%d",
						xoffset, yoffset, i, gotDst[i], wantDst[i])
				}
			}
			// And the same for bilinear (the alternate path; cohort
			// uses sixtap but we pin both to catch future regressions).
			for i := range gotDst {
				gotDst[i] = 0
			}
			dsp.BilinearPredict8x8(srcView, srcStride, xoffset, yoffset, gotDst, dstStride)
			for i := range wantDst {
				wantDst[i] = 0
			}
			libvpxBilinear8x8Reference(srcView, srcStride, xoffset, yoffset, wantDst, dstStride)
			for i := range gotDst {
				if gotDst[i] != wantDst[i] {
					t.Fatalf("BilinearPredict8x8 drift at xoffset=%d yoffset=%d byte=%d: govpx=%d libvpx=%d",
						xoffset, yoffset, i, gotDst[i], wantDst[i])
				}
			}
		}
	}
}

// fastChromaMVDerivation mirrors the chroma MV derivation in
// reconstruct_inter_fast.go. mvRow is the post-clamp Y-plane MV row or col;
// the derivation is symmetric in row and col.
func fastChromaMVDerivation(mvRow int, fullPixel bool) int {
	uvMVRow := (mvRow + 1 + 2*(mvRow>>intSignShiftDec)) / 2
	if fullPixel {
		uvMVRow &^= 7
	}
	return uvMVRow
}

// libvpxChromaMVDerivationReference is a fresh reimplementation of
// libvpx v1.16.0 vp8/common/reconinter.c:327-334 written from the
// libvpx source, without consulting the Go port. Used to verify the port is
// byte-faithful.
//
//	_16x16mv.as_mv.row += 1 | (_16x16mv.as_mv.row >> 31);
//	_16x16mv.as_mv.row /= 2;
//	_16x16mv.as_mv.row &= x->fullpixel_mask;
//
// In C, short is promoted to int at the shift; we model that by
// passing mvRow as `int`. Result is also int (C truncates the
// assignment back to short but the values stay in int16 range for
// any clamp-bounded MV).
func libvpxChromaMVDerivationReference(mvRow int, fullPixel bool) int {
	var sign int
	if mvRow < 0 {
		sign = -1
	} else {
		sign = 0
	}
	row := mvRow + (1 | sign) // 1 | -1 = -1, 1 | 0 = 1
	// C truncation-toward-zero division.
	if row >= 0 {
		row = row / 2
	} else {
		// Go division also truncates toward zero, but we model the
		// C division explicitly here to avoid any cross-language
		// shift-direction ambiguity at the boundary.
		row = -((-row) / 2)
	}
	mask := -1 // ~0 for non-fullpixel
	if fullPixel {
		mask = -8 // ~7
	}
	return row & mask
}

// libvpxSixtap8x8Reference is a fresh reimplementation of
// vp8_sixtap_predict8x8_c (libvpx v1.16.0 vp8/common/filter.c:137-154)
// composed with filter_block2d_first_pass (filter.c:33-69) and
// filter_block2d_second_pass (filter.c:71-109). Written from libvpx
// source; reads the 6-tap filter coefficients from a private copy of
// vp8_sub_pel_filters (filter.c:20-31).
//
// The govpx dsp.SixTapPredict8x8 convention is that `src[0]` points
// at the conceptual filter origin minus 2 rows and minus 2 cols
// (i.e., the caller pre-shifts both dimensions; see
// internal/vp8/decoder/reconstruct_inter_fast.go:226-244 where
// uvRow2 -= 2 and uvCol2 -= 2 for the sixtap branch). The govpx
// kernel then reads src[y*stride + x + 0..5] for filter taps. This
// is algebraically identical to libvpx's convention where src_ptr is
// the conceptual filter origin and the C code itself does
// src_ptr - 2*pixel_step inside filter_block2d_first_pass plus
// per-cell src_ptr[-2..+3] indexing — the two pre-shifts (rows by
// the caller, cols inside the kernel) combine to the same effective
// access pattern in govpx.
//
// To make the comparison apples-to-apples, this reference assumes
// the same govpx-style pre-shifted `src` and re-derives the libvpx
// algorithm under that convention. The 6 horizontal taps then read
// src[y*stride + x + 0..5] (taps numbered 0..5 by their position in
// the filter; tap index 2 is the "central" output position) and the
// 6 vertical taps read tmp[y*width + x .. tmp[(y+5)*width + x].
func libvpxSixtap8x8Reference(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	const filterWeight = 128
	const filterShift = 7
	subPelFilters := [8][6]int16{
		{0, 0, 128, 0, 0, 0},
		{0, -6, 123, 12, -1, 0},
		{2, -11, 108, 36, -8, 1},
		{0, -9, 93, 50, -6, 0},
		{3, -16, 77, 77, -16, 3},
		{0, -6, 50, 93, -9, 0},
		{1, -8, 36, 108, -11, 2},
		{0, -1, 12, 123, -6, 0},
	}
	hFilter := subPelFilters[xoffset]
	vFilter := subPelFilters[yoffset]
	const outH = 13
	const outW = 8
	var firstPass [outH * outW]int
	// First pass: horizontal 6-tap on (output_height=13) ×
	// (output_width=8) starting at src[0..]. libvpx's
	// filter_block2d_first_pass clips each output cell to 0..255
	// (filter.c:55-58) and stores the clipped int into the int*
	// buffer.
	for r := range outH {
		rowBase := r * srcStride
		for c := range outW {
			v := int(src[rowBase+c+0])*int(hFilter[0]) +
				int(src[rowBase+c+1])*int(hFilter[1]) +
				int(src[rowBase+c+2])*int(hFilter[2]) +
				int(src[rowBase+c+3])*int(hFilter[3]) +
				int(src[rowBase+c+4])*int(hFilter[4]) +
				int(src[rowBase+c+5])*int(hFilter[5]) +
				(filterWeight >> 1)
			v >>= filterShift
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			firstPass[r*outW+c] = v
		}
	}
	// Second pass: vertical 6-tap. libvpx's
	// filter_block2d_second_pass (filter.c:71-109) clips each
	// output cell to 0..255.
	for r := range 8 {
		for c := range 8 {
			v := firstPass[(r+0)*outW+c]*int(vFilter[0]) +
				firstPass[(r+1)*outW+c]*int(vFilter[1]) +
				firstPass[(r+2)*outW+c]*int(vFilter[2]) +
				firstPass[(r+3)*outW+c]*int(vFilter[3]) +
				firstPass[(r+4)*outW+c]*int(vFilter[4]) +
				firstPass[(r+5)*outW+c]*int(vFilter[5]) +
				(filterWeight >> 1)
			v >>= filterShift
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			dst[r*dstStride+c] = byte(v)
		}
	}
}

// libvpxBilinear8x8Reference is a fresh reimplementation of
// vp8_bilinear_predict8x8_c (libvpx v1.16.0 vp8/common/filter.c:337-350)
// composed with filter_block2d_bil (filter.c:307-320) which chains
// filter_block2d_bil_first_pass (horizontal 2-tap) and
// filter_block2d_bil_second_pass (vertical 2-tap).
func libvpxBilinear8x8Reference(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	const filterWeight = 128
	const filterShift = 7
	bilinearFilters := [8][2]int16{
		{128, 0}, {112, 16}, {96, 32}, {80, 48},
		{64, 64}, {48, 80}, {32, 96}, {16, 112},
	}
	hFilter := bilinearFilters[xoffset]
	vFilter := bilinearFilters[yoffset]
	// First pass: 9x8 horizontal (height+1 = 9 rows).
	const outH = 9
	const outW = 8
	var firstPass [outH * outW]uint16
	for r := range outH {
		rowBase := r * srcStride
		for c := range outW {
			v := int(src[rowBase+c+0])*int(hFilter[0]) +
				int(src[rowBase+c+1])*int(hFilter[1]) +
				(filterWeight >> 1)
			firstPass[r*outW+c] = uint16(v >> filterShift)
		}
	}
	// Second pass: 8x8 vertical (the actual output).
	const outH2 = 8
	for r := range outH2 {
		for c := range outW {
			v := int(firstPass[r*outW+c])*int(vFilter[0]) +
				int(firstPass[(r+1)*outW+c])*int(vFilter[1]) +
				(filterWeight >> 1)
			dst[r*dstStride+c] = byte(v >> filterShift)
		}
	}
}
