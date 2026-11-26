//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

import "encoding/binary"

const Enabled = false

func Copy16Rows(src []byte, ss int, dst []byte, ds int) bool { return false }
func Copy8Rows(src []byte, ss int, dst []byte, ds int) bool  { return false }

// CopyBlock copies a w x h byte block from src (stride ss) to dst (stride ds).
func CopyBlock(src []byte, ss int, dst []byte, ds, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	si, di := 0, 0
	switch w {
	case 4:
		for range h {
			binary.LittleEndian.PutUint32(dst[di:di+4], binary.LittleEndian.Uint32(src[si:si+4]))
			si += ss
			di += ds
		}
	case 8:
		for range h {
			binary.LittleEndian.PutUint64(dst[di:di+8], binary.LittleEndian.Uint64(src[si:si+8]))
			si += ss
			di += ds
		}
	case 16:
		for range h {
			s := src[si : si+16]
			d := dst[di : di+16]
			a := binary.LittleEndian.Uint64(s[0:8])
			b := binary.LittleEndian.Uint64(s[8:16])
			binary.LittleEndian.PutUint64(d[0:8], a)
			binary.LittleEndian.PutUint64(d[8:16], b)
			si += ss
			di += ds
		}
	default:
		for range h {
			copy(dst[di:di+w], src[si:si+w])
			si += ss
			di += ds
		}
	}
}

func ExtendPlaneBorder(full []byte, origin, stride, width, height, borderX, borderY int) bool {
	if len(full) == 0 || stride <= 0 || width <= 0 || height <= 0 || borderX <= 0 || borderY <= 0 {
		return false
	}
	if borderX > stride/2 || width > stride-2*borderX {
		return false
	}
	if origin < borderX || borderY > (origin-borderX)/stride {
		return false
	}
	tail := len(full) - origin
	if width+borderX > tail {
		return false
	}
	maxRows := (tail - width - borderX) / stride
	if height-1 > maxRows || borderY > maxRows-(height-1) {
		return false
	}
	rowStart := origin
	for range height {
		leftFill := full[rowStart]
		leftDst := full[rowStart-borderX : rowStart]
		lv64 := uint64(leftFill) * 0x0101010101010101
		i := 0
		for ; i+7 < len(leftDst); i += 8 {
			binary.LittleEndian.PutUint64(leftDst[i:], lv64)
		}
		for ; i < len(leftDst); i++ {
			leftDst[i] = leftFill
		}
		rightFill := full[rowStart+width-1]
		rightDst := full[rowStart+width : rowStart+width+borderX]
		rv64 := uint64(rightFill) * 0x0101010101010101
		i = 0
		for ; i+7 < len(rightDst); i += 8 {
			binary.LittleEndian.PutUint64(rightDst[i:], rv64)
		}
		for ; i < len(rightDst); i++ {
			rightDst[i] = rightFill
		}
		rowStart += stride
	}
	span := width + 2*borderX
	topSrc := full[origin-borderX : origin-borderX+span]
	for y := 1; y <= borderY; y++ {
		dstStart := origin - y*stride - borderX
		copy(full[dstStart:dstStart+span], topSrc)
	}
	botOrigin := origin + (height-1)*stride
	botSrc := full[botOrigin-borderX : botOrigin-borderX+span]
	for y := 1; y <= borderY; y++ {
		dstStart := botOrigin + y*stride - borderX
		copy(full[dstStart:dstStart+span], botSrc)
	}
	return true
}

// DCPairNative is false outside the native SIMD selection; public DSP retains
// its original libvpx v1.16.0 idctllm.c dispatch and scalar fallback.
const DCPairNative = false

func DCPairInt32Native(c0, c1 int32, pred []byte, ps int, dst []byte, ds int) {
	dcPairNativeScalar(c0, c1, pred, ps, dst, ds)
}

// DCPairInPlaceInt32Native is the canonical disabled-DSP fallback outside
// the native SIMD selection of libvpx v1.16.0's paired DC operation.
func DCPairInPlaceInt32Native(c0, c1 int32, b []byte, stride int) {
	dcPairNativeScalar(c0, c1, b, stride, b, stride)
}

func AddConstantPair4(pred []byte, ps int, dst []byte, ds int, d0, d1 int16) bool {
	if ps < 8 || ds < 8 || len(pred) < 8 || ps > (len(pred)-8)/3 || len(dst) < 8 || ds > (len(dst)-8)/3 {
		return false
	}
	if &pred[0] != &dst[0] || ps != ds {
		return false
	}
	a0, a1 := int(d0), int(d1)
	if a0 == 0 && a1 == 0 {
		return true
	}
	addConstPairRow(dst[0:8], binary.LittleEndian.Uint64(dst[0:8]), a0, a1)
	addConstPairRow(dst[ds:ds+8], binary.LittleEndian.Uint64(dst[ds:ds+8]), a0, a1)
	addConstPairRow(dst[2*ds:2*ds+8], binary.LittleEndian.Uint64(dst[2*ds:2*ds+8]), a0, a1)
	addConstPairRow(dst[3*ds:3*ds+8], binary.LittleEndian.Uint64(dst[3*ds:3*ds+8]), a0, a1)
	return true
}

func addConstPairRow(d []byte, w uint64, a0, a1 int) {
	_ = d[7]
	d[0] = byte(min(max(int(w&0xff)+a0, 0), 255))
	d[1] = byte(min(max(int((w>>8)&0xff)+a0, 0), 255))
	d[2] = byte(min(max(int((w>>16)&0xff)+a0, 0), 255))
	d[3] = byte(min(max(int((w>>24)&0xff)+a0, 0), 255))
	d[4] = byte(min(max(int((w>>32)&0xff)+a1, 0), 255))
	d[5] = byte(min(max(int((w>>40)&0xff)+a1, 0), 255))
	d[6] = byte(min(max(int((w>>48)&0xff)+a1, 0), 255))
	d[7] = byte(min(max(int(w>>56)+a1, 0), 255))
}

func diffMetricsSIMD(src []byte, ss int, ref []byte, rs, w, h int) (int, int, uint32) {
	panic("SIMD backend unavailable")
}

func Gather16Fast(_ []byte, _ int, _ []byte, _ int, _ []int16) bool { return false }
func Gather8Fast(_ []byte, _ int, _ []byte, _ int, _ []int16) bool  { return false }

func LoopFilterInternal16(_ []byte, _, _, _, _, _ int) bool { return false }

// LoopFilterMacroInternal16 leaves unsupported targets to the separate edge paths.
func LoopFilterMacroInternal16(p []byte, step, outer, mblimit, blimit, limit, thresh int) bool {
	return false
}

func LoopFilter(_ []byte, _ int, _ int, _ int, _ int, _ int, _ int, _ int, _ int) bool { return false }
func LoopFilterSimpleHorizontal16(_ []byte, _ int, _ byte) bool                        { return false }
func LoopFilterSimpleHorizontal3Inner16(_ []byte, _ int, _ byte) bool                  { return false }
func LoopFilterSimpleVertical16(_ []byte, _ int, _ byte) bool                          { return false }
func LoopFilterSimpleVertical3Inner16(_ []byte, _ int, _ byte) bool                    { return false }
func LoopFilterSimpleVertical4All16(_ []byte, _ int, _, _ byte) bool                   { return false }
func LoopFilterUV8(_, _ []byte, _, _, _, _, _, _ int) bool                             { return false }
func DequantWalshAddLumaDC16x16InPlace(_ []byte, _ int, _, _ *[16]int16) bool          { return false }

func quantizeFPBlockSIMD(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {
	return quantizeFPBlockTail(coeff, iscan, qcoeff, dqcoeff, 0, n, r, q, d, classes)
}

// QuantizeVP8Batch leaves unsupported builds to per-block quantization.
func QuantizeVP8Batch(coeff []int16, round, quant, dequant *[16]int16, qcoeff, dqcoeff []int16, iscan *[16]int16, eobs []uint8, count int) bool {
	return false
}

// QuantizeVP8BatchOnly leaves unsupported builds to per-block quantization.
func QuantizeVP8BatchOnly(coeff []int16, round, quant *[16]int16, qcoeff []int16, iscan *[16]int16, eobs []uint8, count int) bool {
	return false
}

func quantizeVP8SIMD(coeff, round, quant, dequant, qcoeff, dqcoeff, iscan *[16]int16) (int, bool) {
	return 0, false
}

func SSE16(src []byte, ss int, ref []byte, rs, h int) uint32 { return SSE16Scalar(src, ss, ref, rs, h) }
func SSE8(src []byte, ss int, ref []byte, rs, h int) uint32  { return SSE8Scalar(src, ss, ref, rs, h) }

func SSEPlane16(_ []byte, _ int, _ []byte, _ int, _, _ int) (uint64, bool) { return 0, false }

// Stats16x16Const computes (sum, sse) of (src - c) over a 16x16 byte block
// against a constant predictor byte c.
func Stats16x16Const(src []byte, ss int, c byte) (int, int) {
	var sum0, sum1, sse0, sse1, sse2, sse3 int
	ci := int(c)
	si := 0
	for range 16 {
		row := src[si : si+16]
		_ = row[15]
		d0 := int(row[0]) - ci
		d1 := int(row[1]) - ci
		d2 := int(row[2]) - ci
		d3 := int(row[3]) - ci
		sum0 += d0 + d1 + d2 + d3
		sse0 += d0 * d0
		sse1 += d1 * d1
		sse2 += d2 * d2
		sse3 += d3 * d3
		d4 := int(row[4]) - ci
		d5 := int(row[5]) - ci
		d6 := int(row[6]) - ci
		d7 := int(row[7]) - ci
		sum1 += d4 + d5 + d6 + d7
		sse0 += d4 * d4
		sse1 += d5 * d5
		sse2 += d6 * d6
		sse3 += d7 * d7
		d8 := int(row[8]) - ci
		d9 := int(row[9]) - ci
		d10 := int(row[10]) - ci
		d11 := int(row[11]) - ci
		sum0 += d8 + d9 + d10 + d11
		sse0 += d8 * d8
		sse1 += d9 * d9
		sse2 += d10 * d10
		sse3 += d11 * d11
		d12 := int(row[12]) - ci
		d13 := int(row[13]) - ci
		d14 := int(row[14]) - ci
		d15 := int(row[15]) - ci
		sum1 += d12 + d13 + d14 + d15
		sse0 += d12 * d12
		sse1 += d13 * d13
		sse2 += d14 * d14
		sse3 += d15 * d15
		si += ss
	}
	return sum0 + sum1, sse0 + sse1 + sse2 + sse3
}
