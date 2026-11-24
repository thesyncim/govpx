package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestRateControlSelectQuantizerUsesLibvpxBitsPerMBModel(t *testing.T) {
	if got := vp8enc.LibvpxRegulatedQuantizer(false, 12000, 60, 4, 56, 1.0); got != 24 {
		t.Fatalf("inter regulated quantizer = %d, want libvpx table q24", got)
	}
	if got := vp8enc.LibvpxRegulatedQuantizer(true, 72000, 60, 4, 56, 1.0); got != 4 {
		t.Fatalf("key regulated quantizer = %d, want min-clamped q4", got)
	}

	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  4,
		bitsPerFrame:      12000,
		frameTargetBits:   12000,
		bufferOptimalBits: 60000,
		bufferLevelBits:   48000,
	}
	rc.selectQuantizerForFrame(false, 60)
	if rc.currentQuantizer != 24 {
		t.Fatalf("selected quantizer = %d, want q24", rc.currentQuantizer)
	}
}
