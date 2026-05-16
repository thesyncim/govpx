package dsp

import (
	"math/rand/v2"
	"testing"
)

// Agreement tests for the full-coefficient VP9 inverse transforms.
// On arm64 (non-purego) these exercise the NEON SRSHR + saturating
// narrow column-add against the canonical scalar reference. On any
// other build the SIMD wrapper *is* the scalar, so this is a self
// check.

type fullCase struct {
	name      string
	size      int
	maxCoef   int // sparsity hint: how many top-left rows to randomize
	wrapper   func(in []int16, dest []uint8, stride int)
	reference func(in []int16, dest []uint8, stride int)
}

type fullIhtCase struct {
	name      string
	size      int
	txType    int
	wrapper   func(in []int16, dest []uint8, stride int, txType int)
	reference func(in []int16, dest []uint8, stride int, txType int)
}

func vp9IdctFullCases() []fullCase {
	return []fullCase{
		{"Idct4x4_16Add", 4, 4, Idct4x4_16Add, idct4x4_16AddScalar},
		{"Idct8x8_64Add", 8, 8, Idct8x8_64Add, idct8x8_64AddScalar},
		{"Idct8x8_12Add", 8, 4, Idct8x8_12Add, idct8x8_12AddScalar},
		{"Idct16x16_256Add", 16, 16, Idct16x16_256Add, idct16x16_256AddScalar},
		{"Idct16x16_38Add", 16, 8, Idct16x16_38Add, idct16x16_38AddScalar},
		{"Idct16x16_10Add", 16, 4, Idct16x16_10Add, idct16x16_10AddScalar},
		{"Idct32x32_1024Add", 32, 32, Idct32x32_1024Add, idct32x32_1024AddScalar},
		{"Idct32x32_135Add", 32, 16, Idct32x32_135Add, idct32x32_135AddScalar},
		{"Idct32x32_34Add", 32, 8, Idct32x32_34Add, idct32x32_34AddScalar},
	}
}

func vp9IhtFullCases() []fullIhtCase {
	out := []fullIhtCase{}
	for _, tx := range []int{1, 2, 3} {
		out = append(out, fullIhtCase{name: "Iht4x4_16Add_t" + itoa(tx), size: 4, txType: tx, wrapper: Iht4x4_16Add, reference: iht4x4_16AddScalar})
		out = append(out, fullIhtCase{name: "Iht8x8_64Add_t" + itoa(tx), size: 8, txType: tx, wrapper: Iht8x8_64Add, reference: iht8x8_64AddScalar})
		out = append(out, fullIhtCase{name: "Iht16x16_256Add_t" + itoa(tx), size: 16, txType: tx, wrapper: Iht16x16_256Add, reference: iht16x16_256AddScalar})
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// fillRandomCoefs writes random int16 coefficients into the first
// rowLimit rows of input. Other rows are zeroed so the sparse fast
// paths see the same input layout the decoder would emit.
func fillRandomCoefs(r *rand.Rand, input []int16, size, rowLimit, maxMag int) {
	for i := range input {
		input[i] = 0
	}
	for j := range rowLimit {
		for i := range rowLimit {
			v := int16(r.Int32N(int32(maxMag*2+1)) - int32(maxMag))
			input[j*size+i] = v
		}
	}
}

func TestVP9IdctFullSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xc0ffeebabe, 0xdeadbeef))
	for _, c := range vp9IdctFullCases() {
		t.Run(c.name, func(t *testing.T) {
			for trial := range 12 {
				stride := c.size + int(r.UintN(11))
				bufSize := stride * c.size
				dest := filledDestPattern(bufSize, trial*13+1)
				ref := make([]uint8, bufSize)
				copy(ref, dest)

				input := make([]int16, c.size*c.size)
				// Mix small and large coefficient magnitudes; cap at a few
				// hundred to keep the residual within the byte-add range
				// libvpx tests with.
				maxMag := []int{32, 256, 1024}[trial%3]
				fillRandomCoefs(r, input, c.size, c.maxCoef, maxMag)

				c.wrapper(input, dest, stride)
				c.reference(input, ref, stride)

				if !equalBytes(dest, ref) {
					t.Fatalf("trial %d maxMag=%d: NEON diverged from scalar\n  got  %v\n  want %v",
						trial, maxMag, dest, ref)
				}
			}
		})
	}
}

func TestVP9IdctFullSimdSingleCoefficient(t *testing.T) {
	// One non-zero coefficient per trial, sampled at positions across
	// the input grid; catches lane-ordering / transpose bugs that random
	// inputs may not hit.
	r := rand.New(rand.NewPCG(0xab1d, 0x7e57))
	for _, c := range vp9IdctFullCases() {
		t.Run(c.name, func(t *testing.T) {
			positions := samplePositions(c.size*c.maxCoef, 20, r)
			for _, pos := range positions {
				stride := c.size + 3
				bufSize := stride * c.size
				dest := filledDestPattern(bufSize, pos+7)
				ref := make([]uint8, bufSize)
				copy(ref, dest)

				input := make([]int16, c.size*c.size)
				// Only positions in the top-left (rowLimit*size) range
				// are meaningful for the sparse fast paths.
				if pos >= c.maxCoef*c.size {
					pos %= c.maxCoef * c.size
				}
				// Reshape pos to (j, i) within the maxCoef x maxCoef
				// upper-left, then map back into the size x size grid.
				j := pos / c.maxCoef
				i := pos % c.maxCoef
				input[j*c.size+i] = 1024

				c.wrapper(input, dest, stride)
				c.reference(input, ref, stride)

				if !equalBytes(dest, ref) {
					t.Fatalf("pos=(%d,%d): NEON diverged from scalar\n  got  %v\n  want %v",
						j, i, dest, ref)
				}
			}
		})
	}
}

func TestVP9IhtFullSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xa11a5, 0xfeed))
	for _, c := range vp9IhtFullCases() {
		t.Run(c.name, func(t *testing.T) {
			for trial := range 8 {
				stride := c.size + int(r.UintN(7))
				bufSize := stride * c.size
				dest := filledDestPattern(bufSize, trial*17+5)
				ref := make([]uint8, bufSize)
				copy(ref, dest)

				input := make([]int16, c.size*c.size)
				maxMag := []int{64, 512}[trial%2]
				fillRandomCoefs(r, input, c.size, c.size, maxMag)

				c.wrapper(input, dest, stride, c.txType)
				c.reference(input, ref, stride, c.txType)

				if !equalBytes(dest, ref) {
					t.Fatalf("trial %d txType=%d maxMag=%d: NEON diverged from scalar\n  got  %v\n  want %v",
						trial, c.txType, maxMag, dest, ref)
				}
			}
		})
	}
}

func samplePositions(maxPos, count int, r *rand.Rand) []int {
	if count >= maxPos {
		out := make([]int, maxPos)
		for i := range out {
			out[i] = i
		}
		return out
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, count)
	for len(out) < count {
		p := int(r.UintN(uint(maxPos)))
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// Benchmarks for the full-coefficient kernels. Measured against the
// scalar reference via -tags purego.
func benchFull(b *testing.B, c fullCase) {
	stride := c.size * 3
	bufSize := stride * c.size
	dest := filledDestPattern(bufSize, 11)
	input := make([]int16, c.size*c.size)
	r := rand.New(rand.NewPCG(0xb1ec, 0xc01a))
	fillRandomCoefs(r, input, c.size, c.maxCoef, 256)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.wrapper(input, dest, stride)
	}
}

func BenchmarkVP9Idct4x4_16Add(b *testing.B)    { benchFull(b, vp9IdctFullCases()[0]) }
func BenchmarkVP9Idct8x8_64Add(b *testing.B)    { benchFull(b, vp9IdctFullCases()[1]) }
func BenchmarkVP9Idct8x8_12Add(b *testing.B)    { benchFull(b, vp9IdctFullCases()[2]) }
func BenchmarkVP9Idct16x16_256Add(b *testing.B) { benchFull(b, vp9IdctFullCases()[3]) }
func BenchmarkVP9Idct16x16_38Add(b *testing.B)  { benchFull(b, vp9IdctFullCases()[4]) }
func BenchmarkVP9Idct16x16_10Add(b *testing.B)  { benchFull(b, vp9IdctFullCases()[5]) }
func BenchmarkVP9Idct32x32_1024Add(b *testing.B) {
	benchFull(b, vp9IdctFullCases()[6])
}
func BenchmarkVP9Idct32x32_135Add(b *testing.B) { benchFull(b, vp9IdctFullCases()[7]) }
func BenchmarkVP9Idct32x32_34Add(b *testing.B)  { benchFull(b, vp9IdctFullCases()[8]) }

func benchIht(b *testing.B, c fullIhtCase) {
	stride := c.size * 3
	bufSize := stride * c.size
	dest := filledDestPattern(bufSize, 23)
	input := make([]int16, c.size*c.size)
	r := rand.New(rand.NewPCG(0xc01a, 0xb1ec))
	fillRandomCoefs(r, input, c.size, c.size, 256)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.wrapper(input, dest, stride, c.txType)
	}
}

func BenchmarkVP9Iht4x4AdstAdst(b *testing.B) {
	benchIht(b, fullIhtCase{size: 4, txType: 3, wrapper: Iht4x4_16Add, reference: iht4x4_16AddScalar})
}
func BenchmarkVP9Iht8x8AdstAdst(b *testing.B) {
	benchIht(b, fullIhtCase{size: 8, txType: 3, wrapper: Iht8x8_64Add, reference: iht8x8_64AddScalar})
}
func BenchmarkVP9Iht16x16AdstAdst(b *testing.B) {
	benchIht(b, fullIhtCase{size: 16, txType: 3, wrapper: Iht16x16_256Add, reference: iht16x16_256AddScalar})
}
