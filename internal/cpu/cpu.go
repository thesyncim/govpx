// Package cpu exposes a small set of runtime CPU feature flags used by
// the SIMD dispatch helpers in internal/vp8/dsp. Detection runs once at
// init time. The flags are read-only after init.
//
// Why a private package instead of golang.org/x/sys/cpu: govpx aims to
// keep its module graph empty (see go.mod) so the encoder can be
// vendored as a single package without dragging in dependencies.
package cpu

// HasAVX2 is true iff the host CPU advertises AVX2 and the OS is
// XSAVE-enabled for YMM state. On non-amd64 builds this is always
// false (the per-arch init below sets it).
var HasAVX2 bool

// HasARM64DotProd is true iff the host arm64 CPU advertises the ASIMD
// dot-product extension. On non-arm64 builds this is always false.
var HasARM64DotProd bool

// HasARM64I8MM is true iff the host arm64 CPU advertises the FEAT_I8MM
// 8-bit integer matrix-multiply extension (which includes the USDOT
// mixed-sign dot product). On non-arm64 builds this is always false.
var HasARM64I8MM bool
