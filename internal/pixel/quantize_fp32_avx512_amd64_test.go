//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"os"
	"simd/archsimd"
	"testing"
)

func TestFP32ActualFeature(t *testing.T) {
	t.Logf("Enabled=%v AVX2=%v AVX512=%v", Enabled, archsimd.X86.AVX2(), archsimd.X86.AVX512())
	if os.Getenv("GOVPX_REQUIRE_FP32_NATIVE") == "1" && !archsimd.X86.AVX512() {
		t.Fatal("native selected512 required but unavailable")
	}
	if os.Getenv("GOVPX_REQUIRE_FP32_OFF") == "1" && archsimd.X86.AVX512() {
		t.Fatal("512 requested off but enabled")
	}
}
func TestFP32NativeSelectedOracles(t *testing.T) {
	if !Enabled || !archsimd.X86.AVX512() {
		t.Skip("actualAVX512 unavailable; selected512 unvalidated here")
	}
	for _, n := range []int{256, 512, 768, 1024} {
		for seed := range 17 {
			for options := range 4 {
				fp16CheckDisjoint(t, n, seed, options, [2]int16{32767, 17}, [2]int16{32767, 12345}, [2]int16{1, 23})
			}
		}
	}
}
func TestFP32DispatchBoundaries(t *testing.T) {
	for _, n := range []int{224, 255, 256, 257, 288, 511, 512, 768, 1023, 1024, 1025} {
		for options := range 4 {
			fp16CheckDisjoint(t, n, 3, options, [2]int16{0, 32767}, [2]int16{32767, 1}, [2]int16{32767, 1})
		}
	}
}
func TestFP32WordClasses(t *testing.T) {
	for m := 0; m <= 32767; m++ {
		c := uint16(0)
		for _, x := range []int{0, 1, 2, 4, 10} {
			if m > x {
				c++
			}
		}
		index := min(m, 11)
		if fpClassWordLUT32[index] != c {
			t.Fatal(m)
		}
	}
}
