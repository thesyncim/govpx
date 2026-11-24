//go:build !govpx_phase_stats

package benchcmd

import govpx "github.com/thesyncim/govpx"

const phaseTimingEnabled = false

type phaseStatsState struct{}

func (s *phaseStatsState) configure(*govpx.EncoderOptions, bool) {}

func (s *phaseStatsState) configureVP9(*govpx.VP9EncoderOptions, bool) {}

func (s *phaseStatsState) reset() {}

func (s *phaseStatsState) report() *govpx.EncoderPhaseStats {
	return nil
}
