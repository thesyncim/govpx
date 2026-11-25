//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

const Enabled = false

func Copy16Rows(src []byte, ss int, dst []byte, ds int) bool { return false }
func Copy8Rows(src []byte, ss int, dst []byte, ds int) bool  { return false }

// CopyBlock copies a w x h byte block from src (stride ss) to dst (stride ds).
func CopyBlock(src []byte, ss int, dst []byte, ds, w, h int) {
	for y := range h {
		copy(dst[y*ds:y*ds+w], src[y*ss:y*ss+w])
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
		for i := range leftDst {
			leftDst[i] = leftFill
		}
		rightFill := full[rowStart+width-1]
		rightDst := full[rowStart+width : rowStart+width+borderX]
		for i := range rightDst {
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

func AddConstantPair4(_ []byte, _ int, _ []byte, _ int, _, _ int16) bool { return false }

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
	sum, sse := 0, 0
	ci := int(c)
	for y := range 16 {
		row := src[y*ss : y*ss+16]
		for x := range 16 {
			d := int(row[x]) - ci
			sum += d
			sse += d * d
		}
	}
	return sum, sse
}
