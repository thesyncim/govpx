//go:build govpx_phase_stats

package codecapi

type EncoderPhaseStatsOptions struct {
	// PhaseStats receives coarse per-attempt encoder phase timings and
	// SAD/subpel hot-path counters during EncodeInto. The caller owns the
	// pointed-to value and may [EncoderPhaseStats.Reset] it between warmup and
	// measured passes.
	PhaseStats *EncoderPhaseStats
}
