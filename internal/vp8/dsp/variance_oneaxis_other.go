//go:build !arm64 || purego

package dsp

// libvpx v1.16.0 baseline: keep unmeasured non-ARM64 and purego builds on
// the generic two-pass subpel variance path until they have local benchmarks.
const useDirectOneAxisSubpelVariance = false
