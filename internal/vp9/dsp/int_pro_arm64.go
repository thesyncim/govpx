//go:build arm64 && !purego

package dsp

import "unsafe"

// NEON wrappers for the integer-projection kernels (libvpx v1.16.0
// vpx_dsp/arm/avg_neon.c vpx_int_pro_row_neon / vpx_int_pro_col_neon).
// The wrappers validate the read windows so the no-allocation kernels
// never touch out-of-bounds memory, and fall back to the scalar port
// outside the NEON kernels' domain.

//go:noescape
func intProRowStripsNEON(hbuf *int16, ref *byte, refStride, height, strips int)

//go:noescape
func intProColsNEON(vbuf *int16, ref *byte, refStride, width, rows, shift int)

//go:noescape
func vectorVarNEON(ref *int16, src *int16, width int) (sse, mean int32)

// vectorVarAsm ports libvpx v1.16.0 vpx_dsp/arm/avg_neon.c
// vpx_vector_var_neon. The int16 difference/accumulation domain matches
// the vpx_int_pro_row/col output contract (|diff| <= 510, width <= 64),
// where every intermediate fits its lane exactly.
func vectorVarAsm(ref, src []int16, bwl int) (int, bool) {
	width := 4 << bwl
	sse, mean := vectorVarNEON(unsafe.SliceData(ref), unsafe.SliceData(src), width)
	return int(sse) - ((int(mean) * int(mean)) >> (bwl + 2)), true
}

// intProRowStripsAsm mirrors `strips` back-to-back vpx_int_pro_row_neon
// calls with ref advancing 16 columns per call. The NEON row kernel's
// `>> ((height >> 5) + 3)` normalisation matches the scalar
// `/ (height >> 1)` only for the power-of-two heights the encoder
// uses, so anything else takes the scalar path.
func intProRowStripsAsm(hbuf []int16, ref []uint8, refOff, refStride, height, strips int) bool {
	if strips <= 0 || refOff < 0 || refStride <= 0 {
		return false
	}
	switch height {
	case 16, 32, 64:
	default:
		return false
	}
	if len(hbuf) < strips*16 {
		return false
	}
	end := refOff + (height-1)*refStride + (strips-1)*16 + 16
	if end < refOff || end > len(ref) {
		return false
	}
	intProRowStripsNEON(&hbuf[0], unsafe.SliceData(ref[refOff:]),
		refStride, height, strips)
	return true
}

// intProColsAsm mirrors `rows` back-to-back vpx_int_pro_col_neon calls
// with ref advancing one stride per call and the caller's
// `>> normFactor` applied to each row sum. Row sums are at most
// 64 * 255 = 16320, so the uint16 lane accumulators and the int16
// result cannot overflow.
func intProColsAsm(vbuf []int16, ref []uint8, refOff, refStride, width, rows, normFactor int) bool {
	if rows <= 0 || refOff < 0 || refStride <= 0 || normFactor < 0 {
		return false
	}
	if width <= 0 || width > 64 || width&15 != 0 {
		return false
	}
	if len(vbuf) < rows {
		return false
	}
	end := refOff + (rows-1)*refStride + width
	if end < refOff || end > len(ref) {
		return false
	}
	intProColsNEON(&vbuf[0], unsafe.SliceData(ref[refOff:]),
		refStride, width, rows, normFactor)
	return true
}
