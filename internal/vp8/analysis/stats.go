package analysis

// Threshold constants used by the CPU observer for flag classification.
// These are observation-only labels; the encoder never reads them, so
// the exact values are tuning knobs for downstream consumers (tests,
// future GPU-assist features) rather than parity-critical magic.
//
// All thresholds are expressed as 16x16 SAD or sum-of-deviations on
// 8-bit luma samples.
const (
	staticSADThreshold     uint32 = 32
	flatVarianceThreshold  uint32 = 256
	highMotionSADThreshold uint32 = 4096
	highTextureThreshold   uint16 = 1024
)
