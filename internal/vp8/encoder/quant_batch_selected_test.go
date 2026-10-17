package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"os"
	"testing"
)

func TestQuantBatchNativeSelected(t *testing.T) {
	if !pixel.Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD backend unavailable")
		}
		t.Skip("native unavailable")
	}
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = 10
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	var c, q, dq [128]int16
	var e [8]uint8
	if !pixel.QuantizeVP8Batch(c[:], &quant.Round, &quant.QuantFast, &quant.Dequant, q[:], dq[:], &tables.DefaultInvZigZag, e[:], 8) {
		t.Fatal("native batch declined eligible work")
	}
}
