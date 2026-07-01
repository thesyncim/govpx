//go:build govpx_phase_stats

package govpx

import "sync/atomic"

const vp9PhaseStatsEnabled = true

type vp9DecoderPhaseStatsOptions struct {
	phaseStats *EncoderPhaseStats
}

func (e *VP9Encoder) vp9PhaseStats() *EncoderPhaseStats {
	if e == nil {
		return nil
	}
	return e.opts.PhaseStats
}

func (e *VP9Encoder) vp9PhaseCountAttempt(keyFrame bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	if keyFrame {
		atomic.AddInt64(&stats.KeyAttempts, 1)
	} else {
		atomic.AddInt64(&stats.InterAttempts, 1)
	}
}

func (e *VP9Encoder) vp9PhaseIncModeBlock() {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9ModeBlocks, 1)
	}
}

func (e *VP9Encoder) vp9PhaseIncInterModePick() {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9InterModePicks, 1)
	}
}

func (e *VP9Encoder) vp9PhaseIncInterPredictionBlock() {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9InterPredictionBlocks, 1)
	}
}

func (e *VP9Encoder) vp9PhaseIncInterPredictionVariance() {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9InterPredictionVarianceCalls, 1)
	}
}

func (e *VP9Encoder) vp9PhaseAddFullPelSAD(candidates int64, batch bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.FullPelSADCalls, 1)
	atomic.AddInt64(&stats.FullPelSADCandidates, candidates)
	if batch {
		atomic.AddInt64(&stats.FullPelBatchCalls, 1)
	}
}

func (d *VP9Decoder) setVP9PhaseStats(stats *EncoderPhaseStats) {
	if d != nil {
		d.phaseStats = stats
	}
}

func (d *VP9Decoder) vp9PhaseIncInterPredictPlane() {
	if d == nil || d.phaseStats == nil {
		return
	}
	atomic.AddInt64(&d.phaseStats.VP9InterPredictPlaneCalls, 1)
}

func (d *VP9Decoder) vp9PhaseCountInterPredictor(key int) {
	if d == nil || d.phaseStats == nil {
		return
	}
	stats := d.phaseStats
	switch key {
	case 0:
		atomic.AddInt64(&stats.VP9InterPredictorCopy, 1)
	case 1:
		atomic.AddInt64(&stats.VP9InterPredictorAvg, 1)
	case 2:
		atomic.AddInt64(&stats.VP9InterPredictorVert, 1)
	case 3:
		atomic.AddInt64(&stats.VP9InterPredictorAvgVert, 1)
	case 4:
		atomic.AddInt64(&stats.VP9InterPredictorHoriz, 1)
	case 5:
		atomic.AddInt64(&stats.VP9InterPredictorAvgHoriz, 1)
	case 6:
		atomic.AddInt64(&stats.VP9InterPredictor2D, 1)
	case 7:
		atomic.AddInt64(&stats.VP9InterPredictorAvg2D, 1)
	}
}
