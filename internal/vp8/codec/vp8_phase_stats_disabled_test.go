//go:build !govpx_phase_stats

package codec

import (
	"reflect"
	"testing"
)

func TestVP8PhaseStatsDisabledOptionShape(t *testing.T) {
	if vp8PhaseStatsEnabled {
		t.Fatal("default build unexpectedly enabled VP8 phase stats")
	}
	if _, ok := reflect.TypeFor[EncoderOptions]().FieldByName("PhaseStats"); ok {
		t.Fatal("default EncoderOptions exposes PhaseStats")
	}
	if got := reflect.TypeFor[encoderPhaseStatsOptions]().Size(); got != 0 {
		t.Fatalf("disabled phase stats option size = %d, want 0", got)
	}

	type withDisabledStats struct {
		enabled bool
		encoderPhaseStatsOptions
		trailing int
	}
	type withoutDisabledStats struct {
		enabled  bool
		trailing int
	}
	if got, want := reflect.TypeFor[withDisabledStats]().Size(), reflect.TypeFor[withoutDisabledStats]().Size(); got != want {
		t.Fatalf("disabled phase stats changed struct size: got %d want %d", got, want)
	}
}

func TestVP8PhaseStatsDisabledCompilesOutHelpers(t *testing.T) {
	var enc VP8Encoder

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
}
