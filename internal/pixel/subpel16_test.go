//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestSubpelSixteenLaneExtremes(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	for _, w := range []int{16, 32, 64} {
		for _, h := range []int{1, 3, 16, 65} {
			for _, strides := range [][2]int{{0, 0}, {1, 3}, {w, w}, {w + 1, w + 7}} {
				ss, rs := strides[0], strides[1]
				for align := range 4 {
					for _, values := range [][2]byte{{0, 255}, {255, 0}, {255, 255}} {
						src := bytes.Repeat([]byte{values[0]}, align+h*ss+w+1)
						ref := bytes.Repeat([]byte{values[1]}, align+(h-1)*rs+w)
						for x := 0; x <= 128; x += 16 {
							for y := 0; y <= 128; y += 16 {
								fx, fy := [2]int16{int16(128 - x), int16(x)}, [2]int16{int16(128 - y), int16(y)}
								a, b := SubpelStats(src[align:], ss, ref[align:], rs, w, h, fx, fy)
								wa, wb := SubpelStatsScalar(src[align:], ss, ref[align:], rs, w, h, fx, fy)
								if a != wa || b != wb {
									t.Fatalf("w=%d h=%d strides=%v align=%d phase=%d,%d values=%v: %d,%d want %d,%d", w, h, strides, align, x, y, values, a, b, wa, wb)
								}
							}
						}
					}
				}
			}
		}
	}
}
