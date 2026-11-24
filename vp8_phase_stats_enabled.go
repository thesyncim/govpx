//go:build govpx_phase_stats

package govpx

const vp8PhaseStatsEnabled = true

type encoderPhaseStatsOptions struct {
	// PhaseStats receives coarse per-attempt encoder phase timings and
	// SAD/subpel hot-path counters during EncodeInto. The caller owns the
	// pointed-to value and may [EncoderPhaseStats.Reset] it between warmup and
	// measured passes.
	PhaseStats *EncoderPhaseStats
}

func (e *VP8Encoder) phaseStats() *EncoderPhaseStats {
	if e == nil {
		return nil
	}
	return e.opts.PhaseStats
}

func (e *VP8Encoder) phaseStart() int64 {
	if e.phaseStats() == nil {
		return 0
	}
	return nanotime()
}

func (e *VP8Encoder) phaseEnd(phase encoderPhase, start int64) {
	if start == 0 {
		return
	}
	stats := e.phaseStats()
	if stats == nil {
		return
	}
	elapsed := nanotime() - start
	switch phase {
	case encoderPhaseInterReconstruct:
		stats.InterReconstructNS += elapsed
	case encoderPhaseKeyReconstruct:
		stats.KeyReconstructNS += elapsed
	case encoderPhaseLoopFilterPick:
		stats.LoopFilterPickNS += elapsed
	case encoderPhaseLoopFilterApply:
		stats.LoopFilterApplyNS += elapsed
	case encoderPhasePacketWrite:
		stats.PacketWriteNS += elapsed
	}
}

func (e *VP8Encoder) phaseCountAttempt(keyFrame bool) {
	stats := e.phaseStats()
	if stats == nil {
		return
	}
	if keyFrame {
		stats.KeyAttempts++
	} else {
		stats.InterAttempts++
	}
}
