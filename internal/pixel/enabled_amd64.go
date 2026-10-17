//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// Keep feature checks in the compiler's feature domain, as in goav1.
var Enabled = archsimd.X86.AVX2()

// Native 649237be timing selects the general vertical internal-filter body
// on x64; ARM retains its measured exact-width store specialization.
const internalVerticalGeneric = true

// Retain the independently measured x64 edge paths.
const internalFused = false

const narrowPixelStats16 = true
const transposeLFColumns16 = false
