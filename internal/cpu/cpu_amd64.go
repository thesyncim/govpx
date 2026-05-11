//go:build amd64 && !purego

package cpu

// CPUID-based AVX2 detection. AVX2 requires:
//   - CPUID.1:ECX[bit 27] = OSXSAVE supported
//   - XCR0 bits 1+2 = OS preserves XMM+YMM state
//   - CPUID.7:EBX[bit 5] = AVX2 supported
//
// We also check CPUID.1:ECX[bit 28] (AVX) for completeness even though
// AVX2 implies AVX in practice on every shipping CPU.
//
// Detection runs once via init().

//go:noescape
func cpuid(eax, ecx uint32) (a, b, c, d uint32)

//go:noescape
func xgetbv(idx uint32) (lo, hi uint32)

func init() {
	a, _, _, _ := cpuid(0, 0)
	if a < 7 {
		return
	}
	_, _, c1, _ := cpuid(1, 0)
	const osxsave = 1 << 27
	const avx = 1 << 28
	if c1&osxsave == 0 || c1&avx == 0 {
		return
	}
	xcr0Lo, _ := xgetbv(0)
	// XCR0 bit 1 = SSE state, bit 2 = AVX (YMM upper half) state.
	const xcr0YMM = (1 << 1) | (1 << 2)
	if xcr0Lo&xcr0YMM != xcr0YMM {
		return
	}
	_, b7, _, _ := cpuid(7, 0)
	const avx2 = 1 << 5
	if b7&avx2 == 0 {
		return
	}
	HasAVX2 = true
}
