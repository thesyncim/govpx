//go:build govpx_phase_stats

package benchcmd

import govpx "github.com/thesyncim/govpx"

const phaseTimingEnabled = true

type phaseStatsState struct {
	stats   govpx.EncoderPhaseStats
	enabled bool
}

func (s *phaseStatsState) configure(opts *govpx.EncoderOptions, enabled bool) {
	if !enabled {
		return
	}
	opts.PhaseStats = &s.stats
	s.enabled = true
}

func (s *phaseStatsState) configureVP9(opts *govpx.VP9EncoderOptions, enabled bool) {
	if !enabled {
		return
	}
	opts.PhaseStats = &s.stats
	s.enabled = true
}

func (s *phaseStatsState) reset() {
	if s.enabled {
		s.stats.Reset()
	}
}

func (s *phaseStatsState) report() *govpx.EncoderPhaseStats {
	if !s.enabled {
		return nil
	}
	return &s.stats
}
