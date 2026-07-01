//go:build !govpx_phase_stats

package govpx

const vp9PhaseStatsEnabled = false

type vp9DecoderPhaseStatsOptions struct{}

func (e *VP9Encoder) vp9PhaseStats() *EncoderPhaseStats {
	return nil
}

func (e *VP9Encoder) vp9PhaseCountAttempt(bool) {}

func (e *VP9Encoder) vp9PhaseIncModeBlock() {}

func (e *VP9Encoder) vp9PhaseIncInterModePick() {}

func (e *VP9Encoder) vp9PhaseIncInterPredictionBlock() {}

func (e *VP9Encoder) vp9PhaseIncInterPredictionVariance() {}

func (e *VP9Encoder) vp9PhaseAddFullPelSAD(int64, bool) {}

func (d *VP9Decoder) setVP9PhaseStats(*EncoderPhaseStats) {}

func (d *VP9Decoder) vp9PhaseIncInterPredictPlane() {}

func (d *VP9Decoder) vp9PhaseCountInterPredictor(int) {}
