package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
)

// TestIdct4x4AddDispatchesByEob: eob==1 uses DC-only; eob>1 uses
// full kernel. Compare side-by-side outputs with the direct DSP
// call.
func TestIdct4x4AddDispatchesByEob(t *testing.T) {
	in := make([]int16, 16)
	in[0] = 32
	in[1] = 0 // EOB 1: only DC.

	got := make([]uint8, 16)
	for i := range got {
		got[i] = 64
	}
	want := make([]uint8, 16)
	copy(want, got)

	Idct4x4Add(in, got, 4, 1)
	dsp.Idct4x4_1Add(in, want, 4)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %d want %d", i, got[i], want[i])
		}
	}

	// eob > 1: full kernel.
	in[1] = 4
	for i := range got {
		got[i] = 64
		want[i] = 64
	}
	Idct4x4Add(in, got, 4, 5)
	dsp.Idct4x4_16Add(in, want, 4)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("eob>1 [%d]: got %d want %d", i, got[i], want[i])
		}
	}
}

// TestIdct8x8AddTiers: eob bins drive the right kernel pick.
func TestIdct8x8AddTiers(t *testing.T) {
	in := make([]int16, 64)
	in[0] = 16

	cases := []struct {
		eob int
		kn  func([]int16, []uint8, int)
	}{
		{1, dsp.Idct8x8_1Add},
		{12, dsp.Idct8x8_12Add},
		{13, dsp.Idct8x8_64Add},
	}
	for _, c := range cases {
		got := make([]uint8, 64)
		want := make([]uint8, 64)
		for i := range got {
			got[i] = 80
			want[i] = 80
		}
		Idct8x8Add(in, got, 8, c.eob)
		c.kn(in, want, 8)
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("eob=%d [%d]: got %d want %d", c.eob, i, got[i], want[i])
				break
			}
		}
	}
}

// TestIht4x4AddDctDctFallsThrough confirms DctDct → IdctNxN dispatch.
func TestIht4x4AddDctDctFallsThrough(t *testing.T) {
	in := make([]int16, 16)
	in[0] = 16

	got := make([]uint8, 16)
	want := make([]uint8, 16)
	for i := range got {
		got[i] = 50
		want[i] = 50
	}
	Iht4x4Add(common.DctDct, in, got, 4, 1)
	dsp.Idct4x4_1Add(in, want, 4)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %d want %d", i, got[i], want[i])
		}
	}
}

// TestIht8x8AddNonDct dispatches to the hybrid kernel.
func TestIht8x8AddNonDct(t *testing.T) {
	in := make([]int16, 64)
	in[0] = 16

	got := make([]uint8, 64)
	want := make([]uint8, 64)
	for i := range got {
		got[i] = 60
		want[i] = 60
	}
	Iht8x8Add(common.AdstDct, in, got, 8, 5)
	dsp.Iht8x8_64Add(in, want, 8, int(common.AdstDct))
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %d want %d", i, got[i], want[i])
			break
		}
	}
}

// TestInverseTransformBlockLosslessUsesIwht: 4x4 + lossless picks
// the WHT path regardless of txType.
func TestInverseTransformBlockLosslessUsesIwht(t *testing.T) {
	in := make([]int16, 16)
	in[0] = 4

	got := make([]uint8, 16)
	want := make([]uint8, 16)
	for i := range got {
		got[i] = 100
		want[i] = 100
	}
	InverseTransformBlock(in, got, 4, common.Tx4x4, common.DctDct, 1, true)
	dsp.Iwht4x4_1Add(in, want, 4)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %d want %d", i, got[i], want[i])
		}
	}
}

// TestInverseTransformBlockTx32 always uses Idct32x32 (no IHT for
// 32x32 in VP9).
func TestInverseTransformBlockTx32(t *testing.T) {
	in := make([]int16, 32*32)
	in[0] = 32

	got := make([]uint8, 32*32)
	want := make([]uint8, 32*32)
	for i := range got {
		got[i] = 120
		want[i] = 120
	}
	InverseTransformBlock(in, got, 32, common.Tx32x32, common.AdstAdst, 1, false)
	dsp.Idct32x32_1Add(in, want, 32)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %d want %d", i, got[i], want[i])
			break
		}
	}
}
