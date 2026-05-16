package dsp

import (
	"math/rand/v2"
	"testing"
)

// TestVP9IdctDcSimdAgreement validates each Idct/Iwht DC-only fast
// path produces byte-identical output to its always-on scalar
// reference. On a purego build the exported wrappers are themselves
// the scalar reference, so this is a self-check; on arm64 it
// exercises the NEON kernel against the scalar.

func filledDestPattern(n, seed int) []uint8 {
	dest := make([]uint8, n)
	for i := range dest {
		dest[i] = uint8((i*31 + seed) & 0xff)
	}
	return dest
}

type dcCase struct {
	name      string
	size      int                                        // block side
	wrapper   func(in []int16, dest []uint8, stride int) // exported entry point under test
	reference func(in []int16, dest []uint8, stride int) // always-scalar reference
}

func vp9IdctDcCases() []dcCase {
	return []dcCase{
		{"Idct4x4_1Add", 4, Idct4x4_1Add, idct4x4_1AddScalar},
		{"Idct8x8_1Add", 8, Idct8x8_1Add, idct8x8_1AddScalar},
		{"Idct16x16_1Add", 16, Idct16x16_1Add, idct16x16_1AddScalar},
		{"Idct32x32_1Add", 32, Idct32x32_1Add, idct32x32_1AddScalar},
		{"Iwht4x4_1Add", 4, Iwht4x4_1Add, iwht4x4_1AddScalar},
	}
}

func TestVP9IdctDcSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0x1d07c, 0xdec0de))
	for _, c := range vp9IdctDcCases() {
		t.Run(c.name, func(t *testing.T) {
			// 10 random trials per case, exercising a range of DC
			// magnitudes that hit both positive and negative
			// branches of the saturating add.
			for trial := range 10 {
				stride := c.size + int(r.UintN(8))
				bufSize := stride * c.size
				dest := filledDestPattern(bufSize, trial*7)
				ref := make([]uint8, bufSize)
				copy(ref, dest)

				dc := int16(r.Int32N(0xfff) - 0x800)
				input := make([]int16, c.size*c.size)
				input[0] = dc

				c.wrapper(input, dest, stride)
				c.reference(input, ref, stride)

				if !equalBytes(dest, ref) {
					t.Fatalf("trial %d dc=%d: NEON output diverged from scalar reference\n  got  %v\n  want %v",
						trial, dc, dest, ref)
				}
			}
		})
	}
}

func TestVP9IdctDcSimdEdgeCases(t *testing.T) {
	type edge struct {
		name string
		dest uint8
		dc   int16
	}
	edges := []edge{
		{"zeroDestPositiveDc", 0, 1024},
		{"saturatedDestPositiveDc", 255, 32767},
		{"saturatedDestNegativeDc", 255, -32768},
		{"midDestZeroDc", 128, 0},
		{"midDestExtremePositive", 128, 32767},
		{"midDestExtremeNegative", 128, -32768},
	}
	for _, c := range vp9IdctDcCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, e := range edges {
				stride := c.size
				bufSize := stride * c.size
				dest := make([]uint8, bufSize)
				ref := make([]uint8, bufSize)
				for i := range dest {
					dest[i] = e.dest
					ref[i] = e.dest
				}
				input := make([]int16, c.size*c.size)
				input[0] = e.dc

				c.wrapper(input, dest, stride)
				c.reference(input, ref, stride)

				if !equalBytes(dest, ref) {
					t.Fatalf("%s dc=%d destFill=%d: NEON output diverged\n  got  %v\n  want %v",
						e.name, e.dc, e.dest, dest, ref)
				}
			}
		})
	}
}

// TestVP9IdctDcSimdStrideLargerThanWidth exercises the common decoder
// case where dest is a slice into a wider plane (stride > block side).
func TestVP9IdctDcSimdStrideLargerThanWidth(t *testing.T) {
	for _, c := range vp9IdctDcCases() {
		t.Run(c.name, func(t *testing.T) {
			const extra = 13
			stride := c.size + extra
			bufSize := stride * c.size
			dest := filledDestPattern(bufSize, 99)
			ref := make([]uint8, bufSize)
			copy(ref, dest)

			input := make([]int16, c.size*c.size)
			input[0] = 73

			c.wrapper(input, dest, stride)
			c.reference(input, ref, stride)

			if !equalBytes(dest, ref) {
				t.Fatalf("%s: NEON diverged from scalar at stride=%d", c.name, stride)
			}
		})
	}
}

func equalBytes(a, b []uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Benchmarks per kernel — measured against the scalar reference via
// -tags purego.

func benchDc(b *testing.B, c dcCase) {
	const stride = 96
	bufSize := stride * c.size
	dest := filledDestPattern(bufSize, 1)
	input := make([]int16, c.size*c.size)
	input[0] = 123
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.wrapper(input, dest, stride)
	}
}

func BenchmarkVP9Idct4x4_1Add(b *testing.B)   { benchDc(b, vp9IdctDcCases()[0]) }
func BenchmarkVP9Idct8x8_1Add(b *testing.B)   { benchDc(b, vp9IdctDcCases()[1]) }
func BenchmarkVP9Idct16x16_1Add(b *testing.B) { benchDc(b, vp9IdctDcCases()[2]) }
func BenchmarkVP9Idct32x32_1Add(b *testing.B) { benchDc(b, vp9IdctDcCases()[3]) }
func BenchmarkVP9Iwht4x4_1Add(b *testing.B)   { benchDc(b, vp9IdctDcCases()[4]) }
