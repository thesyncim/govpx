//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"os"
	"testing"
)

func TestStats16NativeSelected(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("SIMD unavailable")
	}
	if !narrowPixelStats16 {
		t.Fatal("full-row stats not selected")
	}
	TestStats16NarrowRowsAndBounds(t)
}
