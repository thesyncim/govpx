// Package scale ports libvpx v1.16.0 vpx_scale/generic spatial-resampling
// kernels used by the VP8 encoder's VP8E_SET_SCALEMODE path
// (vp8_set_internal_size + scale_and_extend_source in
// vp8/encoder/onyx_if.c).
//
// The kernels here are verbatim Go translations of the C reference
// implementations in vpx_scale/generic/gen_scalers.c and
// vpx_scale/generic/vpx_scale.c. No new heuristics or simplifications.
package scale

// Mode mirrors libvpx's VPX_SCALING_MODE enum (vpx/vp8cx.h:
// VP8E_NORMAL=0, VP8E_FOURFIVE=1, VP8E_THREEFIVE=2, VP8E_ONETWO=3).
type Mode int

// Mode constants matching libvpx's VPX_SCALING_MODE enum verbatim.
const (
	ModeNormal    Mode = 0 // 1:1, no scaling
	ModeFourFive  Mode = 1 // 4:5 (output * 5 = source * 4)
	ModeThreeFive Mode = 2 // 3:5 (output * 5 = source * 3)
	ModeOneTwo    Mode = 3 // 1:2 (output * 2 = source * 1)
)

// Valid reports whether m is one of the libvpx-defined scaling modes.
func (m Mode) Valid() bool {
	return m >= ModeNormal && m <= ModeOneTwo
}

// Scale2Ratio mirrors libvpx's static INLINE Scale2Ratio in
// vp8/common/onyx.h:52-74. mode → (hr, hs) where the output dimension
// is computed by libvpx vp8/encoder/onyx_if.c:1681-1685 as
// (hs - 1 + input * hr) / hs.
func Scale2Ratio(mode Mode) (hr, hs int) {
	switch mode {
	case ModeNormal:
		return 1, 1
	case ModeFourFive:
		return 4, 5
	case ModeThreeFive:
		return 3, 5
	case ModeOneTwo:
		return 1, 2
	default:
		return 1, 1
	}
}

// ScaledDimension returns the scaled output dimension for the given
// source dimension and Mode. Mirrors libvpx vp8/encoder/onyx_if.c:1681
// rounding: dim = (hs - 1 + src * hr) / hs.
func ScaledDimension(src int, mode Mode) int {
	hr, hs := Scale2Ratio(mode)
	return (hs - 1 + src*hr) / hs
}

// horizontalLine54 ports vp8_horizontal_line_5_4_scale_c
// (vpx_scale/generic/gen_scalers.c:36-62). 5-input pixels collapse into
// 4-output pixels per iteration with the published 192/64, 128/128,
// 64/192 weights and +128 rounding.
func horizontalLine54(src []byte, srcWidth int, dst []byte) {
	for i := 0; i+5 <= srcWidth; i += 5 {
		a := uint32(src[0])
		b := uint32(src[1])
		c := uint32(src[2])
		d := uint32(src[3])
		e := uint32(src[4])
		dst[0] = byte(a)
		dst[1] = byte((b*192 + c*64 + 128) >> 8)
		dst[2] = byte((c*128 + d*128 + 128) >> 8)
		dst[3] = byte((d*64 + e*192 + 128) >> 8)
		src = src[5:]
		dst = dst[4:]
	}
}

// verticalBand54 ports vp8_vertical_band_5_4_scale_c
// (vpx_scale/generic/gen_scalers.c:64-88). For each of destWidth
// columns, sample 5 source rows and emit 4 output rows.
func verticalBand54(src []byte, srcPitch int, dst []byte, dstPitch int, destWidth int) {
	for i := 0; i < destWidth; i++ {
		a := uint32(src[0])
		b := uint32(src[srcPitch])
		c := uint32(src[2*srcPitch])
		d := uint32(src[3*srcPitch])
		e := uint32(src[4*srcPitch])
		dst[0] = byte(a)
		dst[dstPitch] = byte((b*192 + c*64 + 128) >> 8)
		dst[2*dstPitch] = byte((c*128 + d*128 + 128) >> 8)
		dst[3*dstPitch] = byte((d*64 + e*192 + 128) >> 8)
		src = src[1:]
		dst = dst[1:]
	}
}

// horizontalLine53 ports vp8_horizontal_line_5_3_scale_c
// (vpx_scale/generic/gen_scalers.c:110-135). 5-input → 3-output with
// 85/171 weights.
func horizontalLine53(src []byte, srcWidth int, dst []byte) {
	for i := 0; i+5 <= srcWidth; i += 5 {
		a := uint32(src[0])
		b := uint32(src[1])
		c := uint32(src[2])
		d := uint32(src[3])
		e := uint32(src[4])
		dst[0] = byte(a)
		dst[1] = byte((b*85 + c*171 + 128) >> 8)
		dst[2] = byte((d*171 + e*85 + 128) >> 8)
		src = src[5:]
		dst = dst[3:]
	}
}

// verticalBand53 ports vp8_vertical_band_5_3_scale_c
// (vpx_scale/generic/gen_scalers.c:137-160).
func verticalBand53(src []byte, srcPitch int, dst []byte, dstPitch int, destWidth int) {
	for i := 0; i < destWidth; i++ {
		a := uint32(src[0])
		b := uint32(src[srcPitch])
		c := uint32(src[2*srcPitch])
		d := uint32(src[3*srcPitch])
		e := uint32(src[4*srcPitch])
		dst[0] = byte(a)
		dst[dstPitch] = byte((b*85 + c*171 + 128) >> 8)
		dst[2*dstPitch] = byte((d*171 + e*85 + 128) >> 8)
		src = src[1:]
		dst = dst[1:]
	}
}

// horizontalLine21 ports vp8_horizontal_line_2_1_scale_c
// (vpx_scale/generic/gen_scalers.c:181-198). Point-sample every other
// source pixel.
func horizontalLine21(src []byte, srcWidth int, dst []byte) {
	for i := 0; i+2 <= srcWidth; i += 2 {
		dst[0] = src[0]
		src = src[2:]
		dst = dst[1:]
	}
}

// verticalBand21 ports vp8_vertical_band_2_1_scale_c
// (vpx_scale/generic/gen_scalers.c:200-207). Plain copy of one source
// row into the destination band.
func verticalBand21(src []byte, dst []byte, destWidth int) {
	copy(dst[:destWidth], src[:destWidth])
}

// verticalBand21Interpolated ports vp8_vertical_band_2_1_scale_i_c
// (vpx_scale/generic/gen_scalers.c:209-228). Uses three rows (above,
// current, below) with 3/10/3 weights and +8/>>4 rounding to produce
// one row. mid is the offset of the current row's first pixel inside
// buf; the kernel reads buf[mid+i-srcPitch] (above), buf[mid+i]
// (current), and buf[mid+i+srcPitch] (below) for i in [0, destWidth).
// The Go port takes (buf, mid) where libvpx takes a single source
// pointer because Go does not allow negative slice indexing.
func verticalBand21Interpolated(buf []byte, mid int, srcPitch int, dst []byte, destWidth int) {
	for i := 0; i < destWidth; i++ {
		temp := 8
		temp += int(buf[mid+i-srcPitch]) * 3
		temp += int(buf[mid+i]) * 10
		temp += int(buf[mid+i+srcPitch]) * 3
		temp >>= 4
		dst[i] = byte(temp)
	}
}

// The generic 1D kernels (scale1d_c, scale1d_2t1_i, scale1d_2t1_ps in
// vpx_scale/generic/vpx_scale.c lines 68-194) are intentionally omitted.
// They are unreachable for the four published VP8 modes when ScaleFrame
// validates symmetric mode pairs at the public surface, and porting
// them adds untested code surface. Re-add if a non-VP8 caller needs
// arbitrary scaling ratios.
