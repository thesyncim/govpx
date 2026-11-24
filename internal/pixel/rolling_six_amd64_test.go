//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"os"
	"testing"
)

func TestRollingSixTapNativeSelected(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("SIMD unavailable")
	}
	var src [21 * 32]byte
	var dst [16 * 32]byte
	six := []int16{3, -16, 77, 77, -16, 3}
	four := []int16{-6, 123, 12, -1}
	for _, width := range []int{8, 16} {
		if !filterVerticalSix8(src[:], 32, dst[:], 32, width, 16, six) {
			t.Fatal("rolling six-tap declined")
		}
		if !filterVerticalFour8(src[:], 32, dst[:], 32, width, 16, four) {
			t.Fatal("rolling four-tap declined")
		}
	}
}
