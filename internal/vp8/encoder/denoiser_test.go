package encoder

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

func TestDenoiserAverageOnlyMatchesScalar(t *testing.T) {
	handledCases := 0
	for _, stride := range []int{16, 23} {
		for _, motion := range []uint32{0, 24, 25, 100} {
			for _, increase := range []bool{false, true} {
				for seed := range 35 {
					mc, sig, avg := make([]byte, 16*stride), make([]byte, 16*stride), make([]byte, 16*stride)
					for i := range sig {
						sig[i] = byte(i*31 + seed*17)
						mc[i] = byte(int(sig[i]) + seed - 17)
						avg[i] = byte(i*19 + seed)
					}
					before := append([]byte(nil), sig...)
					wantSig := append([]byte(nil), sig...)
					wantAvg := append([]byte(nil), avg...)
					want := DenoiserFilterYScalar(mc, stride, wantAvg, stride, wantSig, stride, motion, increase)
					got, handled := DenoiserFilterYAverageOnly(mc, stride, avg, stride, sig, stride, motion, increase)
					if !handled {
						if !bytes.Equal(sig, before) {
							t.Fatal("decline signal write")
						}
						continue
					}
					handledCases++
					if got != want || !bytes.Equal(avg, wantAvg) || !bytes.Equal(sig, before) {
						t.Fatalf("stride=%d motion=%d increase=%t seed=%d got=%d want=%d", stride, motion, increase, seed, got, want)
					}
				}
			}
		}
	}
	if pixel.Enabled && handledCases == 0 {
		t.Fatal("average-only SIMD was never reached")
	}
}

// Reset signal pixels inside the timed operation so filtering cannot turn
// later iterations into an already-converged block. The scalar result and
// both observable output buffers are checked after the live benchmark loop.
func BenchmarkDenoiserFilterYNative(b *testing.B) {
	for _, mode := range []string{"smooth", "mixed", "saturating"} {
		b.Run(mode, func(b *testing.B) {
			const stride = 19
			size := 15*stride + 16
			mc, seed, avg := make([]byte, size), make([]byte, size), bytes.Repeat([]byte{71}, size)
			pairs := [][2]byte{{0, 255}, {255, 0}, {127, 128}, {121, 128}, {120, 128}, {113, 128}, {112, 128}, {255, 253}}
			for y := range 16 {
				for x := range 16 {
					i := y*stride + x
					switch mode {
					case "smooth":
						seed[i] = byte(96 + x%8)
						mc[i] = seed[i] + 2
					case "mixed":
						p := pairs[(x+y)%len(pairs)]
						mc[i], seed[i] = p[0], p[1]
					case "saturating":
						mc[i], seed[i] = 255, 0
					}
				}
			}
			wantSig, wantAvg := bytes.Clone(seed), bytes.Clone(avg)
			want := DenoiserFilterYScalar(mc, stride, wantAvg, stride, wantSig, stride, 0, true)
			sig := bytes.Clone(seed)
			got := -1
			b.ReportAllocs()
			b.SetBytes(256)
			for b.Loop() {
				copy(sig, seed)
				got = DenoiserFilterY(mc, stride, avg, stride, sig, stride, 0, true)
			}
			if got != want || !bytes.Equal(avg, wantAvg) || !bytes.Equal(sig, wantSig) {
				b.Fatalf("decision%d/%d or output differs", got, want)
			}
		})
	}
}

func TestDenoiserModeMappingMatchesLibvpx(t *testing.T) {
	cases := []struct {
		level    int
		wantMode int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 3},
		{4, 2},
		{5, 2},
		{6, 2},
	}
	for _, c := range cases {
		if got := DenoiserModeForSensitivity(c.level); got != c.wantMode {
			t.Fatalf("noise_sensitivity %d -> mode %d, want %d", c.level, got, c.wantMode)
		}
	}
}

func TestDenoiserSetParametersMatchesLibvpxModes(t *testing.T) {
	for _, mode := range []int{1, 2} {
		kind, params := DenoiserSetParameters(mode)
		if mode == 1 && kind != DenoiserOnYOnly {
			t.Fatalf("mode=1 kind = %d, want DenoiserOnYOnly", kind)
		}
		if mode == 2 && kind != DenoiserOnYUV {
			t.Fatalf("mode=2 kind = %d, want DenoiserOnYUV", kind)
		}
		if params.ScaleSSEThresh != 1 || params.ScaleMotionThresh != 8 || params.ScaleIncreaseFilter != 0 || params.DenoiseMVBias != 95 || params.PickmodeMVBias != 100 || params.QPThresh != 0 {
			t.Fatalf("non-aggressive params for mode=%d = %+v, want libvpx defaults", mode, params)
		}
	}
	kind, params := DenoiserSetParameters(3)
	if kind != DenoiserOnYUVAggressive {
		t.Fatalf("mode=3 kind = %d, want DenoiserOnYUVAggressive", kind)
	}
	if params.ScaleSSEThresh != 2 || params.ScaleMotionThresh != 16 || params.ScaleIncreaseFilter != 1 || params.DenoiseMVBias != 60 || params.PickmodeMVBias != 75 || params.QPThresh != 80 || params.ConsecZeroLast != 15 {
		t.Fatalf("aggressive params = %+v, want libvpx aggressive defaults", params)
	}
}

func TestDenoiserFilterYReturnsCopyForSharpDifference(t *testing.T) {
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		mc[i] = 250
	}
	for i := range sig {
		sig[i] = 0
	}
	if got := DenoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false); got != DenoiserCopyBlock {
		t.Fatalf("max-divergence filter decision = %d, want COPY_BLOCK", got)
	}
}

func TestDenoiserFilterYUsesMCWhenAbsdiffSmall(t *testing.T) {
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		mc[i] = 130
	}
	for i := range sig {
		sig[i] = 128
	}
	if got := DenoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false); got != DenoiserFilterBlock {
		t.Fatalf("small-diff filter decision = %d, want FILTER_BLOCK", got)
	}
	for i := range avg {
		if avg[i] != 130 {
			t.Fatalf("avg[%d] = %d, want 130 (mc value taken when |diff|<=3)", i, avg[i])
		}
	}
}

func TestDenoiserFilterUVCopiesNearNeutralBlocks(t *testing.T) {
	mc := make([]byte, 8*8)
	avg := make([]byte, 8*8)
	sig := make([]byte, 8*8)
	for i := range sig {
		sig[i] = 128
	}
	if got := DenoiserFilterUV(mc, 8, avg, 8, sig, 8, 0, false); got != DenoiserCopyBlock {
		t.Fatalf("near-neutral UV filter = %d, want COPY_BLOCK", got)
	}
}
