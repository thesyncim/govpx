//go:build !govpx_phase_stats

package govpx

import "testing"

func TestVP8PhaseStatsDisabledIgnoresRuntimeOption(t *testing.T) {
	var enc VP8Encoder
	var stats EncoderPhaseStats
	enc.opts.PhaseStats = &stats

	allocs := testing.AllocsPerRun(1000, func() {
		if enc.phaseStats() != nil {
			t.Fatal("phaseStats returned non-nil in default build")
		}
		if start := enc.phaseStart(); start != 0 {
			t.Fatalf("phaseStart = %d, want 0 in default build", start)
		}
		enc.phaseEnd(encoderPhasePacketWrite, 1)
		enc.phaseCountAttempt(true)
	})
	if allocs != 0 {
		t.Fatalf("disabled PhaseStats helpers allocated %v times per run, want 0", allocs)
	}
	if stats.KeyAttempts != 0 || stats.PacketWriteNS != 0 {
		t.Fatalf("disabled PhaseStats mutated counters: %+v", stats)
	}
}
