//go:build !govpx_phase_stats

package govpx

const vp8PhaseStatsEnabled = false

type encoderPhaseStatsOptions struct{}

func (e *VP8Encoder) phaseStats() *EncoderPhaseStats {
	return nil
}

func (e *VP8Encoder) phaseStart() int64 {
	return 0
}

func (e *VP8Encoder) phaseEnd(encoderPhase, int64) {}

func (e *VP8Encoder) phaseCountAttempt(bool) {}
