package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

func dcPredictorFrozen57(dst []uint8, stride, bs int, above, left []uint8) {
	sum := 0
	for i := range bs {
		sum += int(above[i])
		sum += int(left[i])
	}
	count := 2 * bs
	expected := uint8((sum + (count >> 1)) / count)
	fillBlockConstantFrozen57(dst, stride, bs, expected)
}

func dcLeftPredictorFrozen57(dst []uint8, stride, bs int, above, left []uint8) {
	_ = above
	sum := 0
	for i := range bs {
		sum += int(left[i])
	}
	expected := uint8((sum + (bs >> 1)) / bs)
	fillBlockConstantFrozen57(dst, stride, bs, expected)
}

func dcTopPredictorFrozen57(dst []uint8, stride, bs int, above, left []uint8) {
	_ = left
	sum := 0
	for i := range bs {
		sum += int(above[i])
	}
	expected := uint8((sum + (bs >> 1)) / bs)
	fillBlockConstantFrozen57(dst, stride, bs, expected)
}

func dc128PredictorFrozen57(dst []uint8, stride, bs int, above, left []uint8) {
	_ = above
	_ = left
	fillBlockConstantFrozen57(dst, stride, bs, 128)
}

func vPredictorFrozen57(dst []uint8, stride, bs int, above, left []uint8) {
	_ = left
	for r := range bs {
		copy(dst[r*stride:r*stride+bs], above[:bs])
	}
}

func hPredictorFrozen57(dst []uint8, stride, bs int, above, left []uint8) {
	_ = above
	for r := range bs {
		row := dst[r*stride : r*stride+bs]
		v := left[r]
		for i := range row {
			row[i] = v
		}
	}
}

func hPredictor4Frozen57(dst []uint8, stride int, left []uint8) {
	for r := range 4 {
		row := dst[r*stride:]
		_ = row[3]
		v := left[r]
		row[0], row[1], row[2], row[3] = v, v, v, v
	}
}

func hPredictor8Frozen57(dst []uint8, stride int, left []uint8) {
	for r := range 8 {
		row := dst[r*stride:]
		_ = row[7]
		v := left[r]
		row[0], row[1], row[2], row[3] = v, v, v, v
		row[4], row[5], row[6], row[7] = v, v, v, v
	}
}

func hPredictor16Frozen57(dst []uint8, stride int, left []uint8) {
	for r := range 16 {
		row := dst[r*stride:]
		_ = row[15]
		v := left[r]
		row[0], row[1], row[2], row[3] = v, v, v, v
		row[4], row[5], row[6], row[7] = v, v, v, v
		row[8], row[9], row[10], row[11] = v, v, v, v
		row[12], row[13], row[14], row[15] = v, v, v, v
	}
}

func hPredictor32Frozen57(dst []uint8, stride int, left []uint8) {
	for r := range 32 {
		row := dst[r*stride:]
		_ = row[31]
		v := left[r]
		row[0], row[1], row[2], row[3] = v, v, v, v
		row[4], row[5], row[6], row[7] = v, v, v, v
		row[8], row[9], row[10], row[11] = v, v, v, v
		row[12], row[13], row[14], row[15] = v, v, v, v
		row[16], row[17], row[18], row[19] = v, v, v, v
		row[20], row[21], row[22], row[23] = v, v, v, v
		row[24], row[25], row[26], row[27] = v, v, v, v
		row[28], row[29], row[30], row[31] = v, v, v, v
	}
}

func fillBlockConstantFrozen57(dst []uint8, stride, bs int, v uint8) {
	row0 := dst[:bs]
	for i := range row0 {
		row0[i] = v
	}
	for r := 1; r < bs; r++ {
		copy(dst[r*stride:r*stride+bs], row0)
	}
}

// tmPredictorFrozen57 implements True-Motion: pixel(r,c) = clip(left[r] +
// above[c] - above[-1]). Callers must pass a slice whose backing array
// has at least one byte before `above[0]` so the -1 lookup is valid.
func tmPredictorFrozen57(dst []uint8, stride, bs int, above, left []uint8) {
	// above is provided with above[0]..above[bs+...], and aboveTopLeft is
	// passed via above[-1] in libvpx. To keep that contract in Go we
	// expect callers to pass a sub-slice where index 0 is the top-left
	// corner pixel; ie. the parameter naming changes by one. To preserve
	// the same call shape as libvpx, the public entry points below
	// reslice into the right offset before invoking the helper.
	topLeft := int(above[0])
	above = above[1:]
	minAbove, maxAbove := int(above[0]), int(above[0])
	for i := 1; i < bs; i++ {
		a := int(above[i])
		if a < minAbove {
			minAbove = a
		} else if a > maxAbove {
			maxAbove = a
		}
	}
	for r := range bs {
		base := int(left[r]) - topLeft
		row := dst[r*stride : r*stride+bs]
		if base+minAbove >= 0 && base+maxAbove <= 255 {
			for c := range row {
				row[c] = uint8(base + int(above[c]))
			}
			continue
		}
		for c := range row {
			v := base + int(above[c])
			if v < 0 {
				row[c] = 0
			} else if v > 255 {
				row[c] = 255
			} else {
				row[c] = uint8(v)
			}
		}
	}
}

// The exported wrappers fix the block size at the type level so the
// compiler can constant-fold the inner loops. above is passed at index
// -1 in libvpx; the Go contract uses a 1-byte prefix so above[0] in
// these functions is the top-left corner and above[1..bs] is the row
// above. left[0..bs-1] is the column to the left.

// Vpx*Predictor4x4 etc match libvpx's vpx_<type>_predictor_4x4_c.

func VpxDcPredictor4x4Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcPredictorFrozen57(dst, stride, 4, above[1:], left)
}
func VpxDcPredictor8x8Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcPredictorFrozen57(dst, stride, 8, above[1:], left)
}
func VpxDcPredictor16x16Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcPredictorFrozen57(dst, stride, 16, above[1:], left)
}
func VpxDcPredictor32x32Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcPredictorFrozen57(dst, stride, 32, above[1:], left)
}

func VpxDcLeftPredictor4x4Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcLeftPredictorFrozen57(dst, stride, 4, above[1:], left)
}
func VpxDcLeftPredictor8x8Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcLeftPredictorFrozen57(dst, stride, 8, above[1:], left)
}
func VpxDcLeftPredictor16x16Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcLeftPredictorFrozen57(dst, stride, 16, above[1:], left)
}
func VpxDcLeftPredictor32x32Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcLeftPredictorFrozen57(dst, stride, 32, above[1:], left)
}

func VpxDcTopPredictor4x4Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcTopPredictorFrozen57(dst, stride, 4, above[1:], left)
}
func VpxDcTopPredictor8x8Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcTopPredictorFrozen57(dst, stride, 8, above[1:], left)
}
func VpxDcTopPredictor16x16Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcTopPredictorFrozen57(dst, stride, 16, above[1:], left)
}
func VpxDcTopPredictor32x32Frozen57(dst []uint8, stride int, above, left []uint8) {
	dcTopPredictorFrozen57(dst, stride, 32, above[1:], left)
}

func VpxDc128Predictor4x4Frozen57(dst []uint8, stride int, above, left []uint8) {
	dc128PredictorFrozen57(dst, stride, 4, above[1:], left)
}
func VpxDc128Predictor8x8Frozen57(dst []uint8, stride int, above, left []uint8) {
	dc128PredictorFrozen57(dst, stride, 8, above[1:], left)
}
func VpxDc128Predictor16x16Frozen57(dst []uint8, stride int, above, left []uint8) {
	dc128PredictorFrozen57(dst, stride, 16, above[1:], left)
}
func VpxDc128Predictor32x32Frozen57(dst []uint8, stride int, above, left []uint8) {
	dc128PredictorFrozen57(dst, stride, 32, above[1:], left)
}

func VpxVPredictor4x4Frozen57(dst []uint8, stride int, above, left []uint8) {
	vPredictorFrozen57(dst, stride, 4, above[1:], left)
}
func VpxVPredictor8x8Frozen57(dst []uint8, stride int, above, left []uint8) {
	vPredictorFrozen57(dst, stride, 8, above[1:], left)
}
func VpxVPredictor16x16Frozen57(dst []uint8, stride int, above, left []uint8) {
	vPredictorFrozen57(dst, stride, 16, above[1:], left)
}
func VpxVPredictor32x32Frozen57(dst []uint8, stride int, above, left []uint8) {
	vPredictorFrozen57(dst, stride, 32, above[1:], left)
}

func VpxHPredictor4x4Frozen57(dst []uint8, stride int, above, left []uint8) {
	_ = above[0]
	hPredictor4Frozen57(dst, stride, left)
}
func VpxHPredictor8x8Frozen57(dst []uint8, stride int, above, left []uint8) {
	_ = above[0]
	hPredictor8Frozen57(dst, stride, left)
}
func VpxHPredictor16x16Frozen57(dst []uint8, stride int, above, left []uint8) {
	_ = above[0]
	hPredictor16Frozen57(dst, stride, left)
}
func VpxHPredictor32x32Frozen57(dst []uint8, stride int, above, left []uint8) {
	_ = above[0]
	hPredictor32Frozen57(dst, stride, left)
}

func VpxTmPredictor4x4Frozen57(dst []uint8, stride int, above, left []uint8) {
	tmPredictorFrozen57(dst, stride, 4, above, left)
}
func VpxTmPredictor8x8Frozen57(dst []uint8, stride int, above, left []uint8) {
	tmPredictorFrozen57(dst, stride, 8, above, left)
}
func VpxTmPredictor16x16Frozen57(dst []uint8, stride int, above, left []uint8) {
	tmPredictorFrozen57(dst, stride, 16, above, left)
}
func VpxTmPredictor32x32Frozen57(dst []uint8, stride int, above, left []uint8) {
	tmPredictorFrozen57(dst, stride, 32, above, left)
}

type intra57case struct {
	name    string
	n       int
	fn, ref func([]byte, int, []byte, []byte)
}

func intra57cases() []intra57case {
	return []intra57case{
		{"tm4", 4, VpxTmPredictor4x4, VpxTmPredictor4x4Frozen57},
		{"tm8", 8, VpxTmPredictor8x8, VpxTmPredictor8x8Frozen57},
		{"tm16", 16, VpxTmPredictor16x16, VpxTmPredictor16x16Frozen57},
		{"tm32", 32, VpxTmPredictor32x32, VpxTmPredictor32x32Frozen57},
		{"h4", 4, VpxHPredictor4x4, VpxHPredictor4x4Frozen57},
		{"h8", 8, VpxHPredictor8x8, VpxHPredictor8x8Frozen57},
		{"h16", 16, VpxHPredictor16x16, VpxHPredictor16x16Frozen57},
		{"h32", 32, VpxHPredictor32x32, VpxHPredictor32x32Frozen57},
		{"v4", 4, VpxVPredictor4x4, VpxVPredictor4x4Frozen57},
		{"v8", 8, VpxVPredictor8x8, VpxVPredictor8x8Frozen57},
		{"v16", 16, VpxVPredictor16x16, VpxVPredictor16x16Frozen57},
		{"v32", 32, VpxVPredictor32x32, VpxVPredictor32x32Frozen57},
		{"dc4", 4, VpxDcPredictor4x4, VpxDcPredictor4x4Frozen57},
		{"dc8", 8, VpxDcPredictor8x8, VpxDcPredictor8x8Frozen57},
		{"dc16", 16, VpxDcPredictor16x16, VpxDcPredictor16x16Frozen57},
		{"dc32", 32, VpxDcPredictor32x32, VpxDcPredictor32x32Frozen57},
		{"dcl4", 4, VpxDcLeftPredictor4x4, VpxDcLeftPredictor4x4Frozen57},
		{"dcl8", 8, VpxDcLeftPredictor8x8, VpxDcLeftPredictor8x8Frozen57},
		{"dcl16", 16, VpxDcLeftPredictor16x16, VpxDcLeftPredictor16x16Frozen57},
		{"dcl32", 32, VpxDcLeftPredictor32x32, VpxDcLeftPredictor32x32Frozen57},
		{"dct4", 4, VpxDcTopPredictor4x4, VpxDcTopPredictor4x4Frozen57},
		{"dct8", 8, VpxDcTopPredictor8x8, VpxDcTopPredictor8x8Frozen57},
		{"dct16", 16, VpxDcTopPredictor16x16, VpxDcTopPredictor16x16Frozen57},
		{"dct32", 32, VpxDcTopPredictor32x32, VpxDcTopPredictor32x32Frozen57},
		{"dc04", 4, VpxDc128Predictor4x4, VpxDc128Predictor4x4Frozen57},
		{"dc08", 8, VpxDc128Predictor8x8, VpxDc128Predictor8x8Frozen57},
		{"dc016", 16, VpxDc128Predictor16x16, VpxDc128Predictor16x16Frozen57},
		{"dc032", 32, VpxDc128Predictor32x32, VpxDc128Predictor32x32Frozen57},
	}
}
func intra57panic(fn func()) (pan bool) { defer func() { pan = recover() != nil }(); fn(); return }
func TestIntraBridge57(t *testing.T) {
	for _, tc := range intra57cases() {
		for _, stride := range []int{tc.n, tc.n + 7} {
			for pattern := 0; pattern < 5; pattern++ {
				got := make([]byte, 4096)
				want := make([]byte, 4096)
				for i := range got {
					got[i] = byte(i*17 + i/11 + pattern*43)
					want[i] = got[i]
				}
				off, ao, lo := 99, 3000, 3200
				if pattern == 1 {
					got[ao] = 255
					want[ao] = 255
				}
				if pattern == 2 {
					got[ao] = 0
					want[ao] = 0
				}
				for _, short := range []bool{false, true} {
					g := append([]byte(nil), got...)
					w := append([]byte(nil), want...)
					size := (tc.n-1)*stride + tc.n
					if short {
						size--
					}
					gp := intra57panic(func() { tc.fn(g[off:off+size:off+size], stride, g[ao:ao+tc.n+1:ao+tc.n+1], g[lo:lo+tc.n:lo+tc.n]) })
					wp := intra57panic(func() { tc.ref(w[off:off+size:off+size], stride, w[ao:ao+tc.n+1:ao+tc.n+1], w[lo:lo+tc.n:lo+tc.n]) })
					if gp != wp || !bytes.Equal(g, w) {
						t.Fatalf("%s stride%d pattern%d short%t panic%t/%t backing differs", tc.name, stride, pattern, short, gp, wp)
					}
				}
			}
		}
	}
}
func BenchmarkIntraBridge57(b *testing.B) {
	for _, tc := range intra57cases() {
		patterns := 1
		if tc.name[:2] == "tm" {
			patterns = 2
		}
		for pattern := 0; pattern < patterns; pattern++ {
			b.Run(fmt.Sprintf("%s-p%d", tc.name, pattern), func(b *testing.B) {
				above, left := intraBenchEdges()
				if pattern == 1 {
					above, left = intraClipEdges()
				}
				dst := make([]byte, (tc.n+7)*tc.n)
				b.ReportAllocs()
				for b.Loop() {
					tc.fn(dst, tc.n+7, above, left)
				}
			})
		}
	}
}
