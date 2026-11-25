//go:build !govpx_phase_stats

// VP8 codec implementation aligned with libvpx v1.16.0.
package codec

const vp8PhaseStatsEnabled = false

func (e *VP8Encoder) phaseStats() *EncoderPhaseStats {
	return nil
}

func (e *VP8Encoder) phaseStart() int64 {
	return 0
}

func (e *VP8Encoder) phaseEnd(encoderPhase, int64) {}

func (e *VP8Encoder) phaseCountAttempt(bool) {}
