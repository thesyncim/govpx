//go:build arm64 && !purego

package dsp

// libvpx v1.16.0 baseline: vp8/encoder/variance.c one-axis bilinear
// subpel variance reduces the identity axis before variance accumulation.
const useDirectOneAxisSubpelVariance = true
