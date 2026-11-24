// Scalar entry points for non-experimental and purego builds.
//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func SAD(src []byte, ss int, ref []byte, rs, w, h int) (sum int) {
	return SADScalar(src, ss, ref, rs, w, h)
}
func Stats(src []byte, ss int, ref []byte, rs, w, h int) (sum int, sse uint32) {
	return StatsScalar(src, ss, ref, rs, w, h)
}
func Subtract(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) (nonzero bool) {
	return SubtractScalar(src, ss, pred, ps, out, os, w, h)
}
func Filter(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) {
	FilterScalar(src, ss, dst, ds, w, h, step, taps)
}
func Average(src []byte, ss int, dst []byte, ds, w, h int) { AverageScalar(src, ss, dst, ds, w, h) }
func Error(a, b []int16, n int) (sum uint64)               { return ErrorScalar(a, b, n) }
func Quantize(coeff, iscan, qcoeff, dqcoeff []int16, n, zbin, round, quant, shift, dequant int, fp bool) (eob int) {
	return QuantizeScalar(coeff, iscan, qcoeff, dqcoeff, n, zbin, round, quant, shift, dequant, fp)
}
func Gather(src []byte, ss int, pred []byte, ps int, out []int16, w int) {
	GatherScalar(src, ss, pred, ps, out, w)
}
func Temporal(src []byte, ss int, pred []byte, ps, w, strength, weight int, acc, count []uint32) {
	TemporalScalar(src, ss, pred, ps, w, strength, weight, acc, count)
}
func VP8Quant(c, r, q, d, qout, dout []int16) { VP8QuantScalar(c, r, q, d, qout, dout) }
func AddConstant(pred []byte, ps int, dst []byte, ds, w, h, delta int) {
	AddConstantScalar(pred, ps, dst, ds, w, h, delta)
}
func TMPredict(dst []byte, ds int, above, left []byte, top byte, w int) {
	TMPredictScalar(dst, ds, above, left, top, w)
}
func SubpelStats(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (sum int, sse uint32) {
	return SubpelStatsScalar(src, ss, ref, rs, w, h, fx, fy)
}
func BilinearFirst(src []byte, ss int, dst []uint16, w, h int, f [2]int16) {
	BilinearFirstScalar(src, ss, dst, w, h, f)
}
func BilinearSecond(src []uint16, dst []byte, w, h int, f [2]int16) {
	BilinearSecondScalar(src, dst, w, h, f)
}
func Sum(src []byte, ss, w, h int) (sum int) { return SumScalar(src, ss, w, h) }
func Energy(src []int16) (sum uint64)        { return EnergyScalar(src) }
func AbsSum(src []int16, n int) (sum int)    { return AbsSumScalar(src, n) }
func ProjectionRow(src []byte, ss, h int, out []int16, shift uint64) {
	ProjectionRowScalar(src, ss, h, out, shift)
}
func VectorVariance(ref, src []int16, n, shift int) int {
	return VectorVarianceScalar(ref, src, n, shift)
}
func DenoiseFirst(mc []byte, ms int, avg []byte, as int, sig []byte, ss, w, inc, level int) int {
	return DenoiseFirstScalar(mc, ms, avg, as, sig, ss, w, inc, level)
}
func Intra4(dst []byte, ds int, b *[16]byte, indices *[4][16]byte) { Intra4Scalar(dst, ds, b, indices) }
func FillBlock(dst []byte, ds, w, h int, v byte)                   { FillBlockScalar(dst, ds, w, h, v) }
func RepeatRow(dst []byte, ds int, a []byte, w int)                { RepeatRowScalar(dst, ds, a, w) }
func Horizontal(dst []byte, ds int, l []byte, w int)               { HorizontalScalar(dst, ds, l, w) }
func Mul16(a, b, out []int16, n int)                               { Mul16Scalar(a, b, out, n) }
func SAD16(src []byte, ss int, ref []byte, rs, h int) int          { return SAD16Scalar(src, ss, ref, rs, h) }
func SAD16Limit(src []byte, ss int, ref []byte, rs, h, limit int) (sum int) {
	return SAD16LimitScalar(src, ss, ref, rs, h, limit)
}
func SAD16x4(src []byte, ss int, r0, r1, r2, r3 []byte, rs, h int, out *[4]uint32) {
	SAD16x4Scalar(src, ss, r0, r1, r2, r3, rs, h, out)
}

func ProjectionCols16(src []byte, ss, w, rows, norm int, out []int16) bool { return false }
func AverageQuad16(src []byte, ss int, out *[4]int32) bool                 { return false }
