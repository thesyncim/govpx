//go:build amd64 && !purego

package dsp

// Adapters preserve the original seeded assembly differential cases while
// testing the new production Go dispatch against the independent scalar port.
import (
	"github.com/thesyncim/govpx/internal/pixel"
)

func sadBlock16x16Native(src []byte, ss int, ref []byte, rs int) int32 {
	return int32(pixel.SAD(src, ss, ref, rs, 16, 16))
}
func sadBlock16x16Reference(src []byte, ss int, ref []byte, rs int) int32 {
	return int32(scalarSAD(src, ss, ref, rs, 16, 16))
}
func sadBlock16x8Native(src []byte, ss int, ref []byte, rs int) int32 {
	return int32(pixel.SAD(src, ss, ref, rs, 16, 8))
}
func sadBlock16x8Reference(src []byte, ss int, ref []byte, rs int) int32 {
	return int32(scalarSAD(src, ss, ref, rs, 16, 8))
}
func sadBlock8x16Native(src []byte, ss int, ref []byte, rs int) int32 {
	return int32(pixel.SAD(src, ss, ref, rs, 8, 16))
}
func sadBlock8x16Reference(src []byte, ss int, ref []byte, rs int) int32 {
	return int32(scalarSAD(src, ss, ref, rs, 8, 16))
}
func sadBlock8x8Native(src []byte, ss int, ref []byte, rs int) int32 {
	return int32(pixel.SAD(src, ss, ref, rs, 8, 8))
}
func sadBlock8x8Reference(src []byte, ss int, ref []byte, rs int) int32 {
	return int32(scalarSAD(src, ss, ref, rs, 8, 8))
}
func sadBlock16x16x4Native(src []byte, ss int, r0, r1, r2, r3 []byte, rs int, out *[4]uint32) {
	SAD16x16x4Fast(src, ss, r0, r1, r2, r3, rs, out)
}
func sixTapPredict16x16Native(dst []byte, ds int, src []byte, ss int, hf, vf *[6]int16, tmp *[21 * 16]byte) {
	pixel.Filter(src, ss, tmp[:], 16, 16, 21, 1, hf[:])
	pixel.Filter(tmp[:], 16, dst, ds, 16, 16, 16, vf[:])
}
func loopFilterEdgeH16Native(src []byte, ss int, b, l, t byte) {
	loopFilterHorizontalEdgeDispatch(src, ss, b, l, t, 2)
}
func mbLoopFilterEdgeH16Native(src []byte, ss int, b, l, t byte) {
	mbLoopFilterHorizontalEdgeDispatch(src, ss, b, l, t, 2)
}
func varianceBlock16xNNative(src []byte, ss int, ref []byte, rs, h int, sum *int32, sse *uint32) {
	su, se := pixel.Stats(src, ss, ref, rs, 16, h)
	*sum, *sse = int32(su), se
}
func varianceBlock8x16Native(src []byte, ss int, ref []byte, rs int, sum *int32, sse *uint32) {
	varianceBlock8xNNative(src, ss, ref, rs, 16, sum, sse)
}
func varianceBlock8xNNative(src []byte, ss int, ref []byte, rs, h int, sum *int32, sse *uint32) {
	su, se := pixel.Stats(src, ss, ref, rs, 8, h)
	*sum, *sse = int32(su), se
}
func varianceBlock16x16Native(src []byte, ss int, ref []byte, rs int, sum *int32, sse *uint32) {
	varianceBlock16xNNative(src, ss, ref, rs, 16, sum, sse)
}
func varFilterBlock2DBilinearFirstPass16Native(src []byte, ss int, dst []uint16, h int, f0, f1 uint64) {
	pixel.BilinearFirst(src, ss, dst, 16, h, [2]int16{int16(f0), int16(f1)})
}

func varFilterBlock2DBilinearSecondPass16Native(src *[17 * 16]uint16, dst []byte, h int, f0, f1 uint64) {
	varFilterBlock2DBilinearSecondPass16(src, dst, h, [2]int16{int16(f0), int16(f1)})
}
func sixTapPredict16x16Reference(dst []byte, ds int, src []byte, ss int, hf, vf *[6]int16, tmp *[21 * 16]byte) {
	for y := range 21 {
		for x := range 16 {
			v := 64
			for k := range 6 {
				v += int(src[y*ss+x+k]) * int(hf[k])
			}
			tmp[y*16+x] = ClipPixel(v >> 7)
		}
	}
	for y := range 16 {
		for x := range 16 {
			v := 64
			for k := range 6 {
				v += int(tmp[(y+k)*16+x]) * int(vf[k])
			}
			dst[y*ds+x] = ClipPixel(v >> 7)
		}
	}
}
func loopFilterEdgeH16Reference(src []byte, ss int, b, l, t byte) {
	loopFilterHorizontalEdgeScalar(src, ss, b, l, t, 2)
}
func mbLoopFilterEdgeH16Reference(src []byte, ss int, b, l, t byte) {
	mbLoopFilterHorizontalEdgeScalar(src, ss, b, l, t, 2)
}
func varianceBlock16xNReference(src []byte, ss int, ref []byte, rs, h int, sum *int32, sse *uint32) {
	su, se := varianceBlock16xNScalar(src, ss, ref, rs, h)
	*sum, *sse = int32(su), uint32(se)
}
func varianceBlock8x16Reference(src []byte, ss int, ref []byte, rs int, sum *int32, sse *uint32) {
	varianceBlock8xNReference(src, ss, ref, rs, 16, sum, sse)
}
func varianceBlock8xNReference(src []byte, ss int, ref []byte, rs, h int, sum *int32, sse *uint32) {
	su, se := varianceBlock8xNScalar(src, ss, ref, rs, h)
	*sum, *sse = int32(su), uint32(se)
}
func varianceBlock16x16Reference(src []byte, ss int, ref []byte, rs int, sum *int32, sse *uint32) {
	varianceBlock16xNReference(src, ss, ref, rs, 16, sum, sse)
}
func varFilterBlock2DBilinearFirstPass16Reference(src []byte, ss int, dst []uint16, h int, f0, f1 uint64) {
	for y := range h {
		for x := range 16 {
			dst[y*16+x] = uint16((int(src[y*ss+x])*int(uint16(f0)) + int(src[y*ss+x+1])*int(uint16(f1)) + 64) >> 7)
		}
	}
}
func varFilterBlock2DBilinearSecondPass16Reference(src *[17 * 16]uint16, dst []byte, h int, f0, f1 uint64) {
	varFilterBlock2DBilinearSecondPass16Scalar(src, dst, h, [2]int16{int16(f0), int16(f1)})
}
