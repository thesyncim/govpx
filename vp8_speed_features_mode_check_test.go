package govpx

import "testing"

// TestVP8SpeedFeaturesNew1ModeCheckFreqMirrorsLibvpxSpeed10 pins the
// libvpx onyx_if.c:877-879 special-case: at cpi->Speed == 10 (cpu_used
// = -10) and Mode == 2 (realtime), the mode_check_freq[THR_NEW1]
// speed_map lookup uses Speed2 = RT(9) = 16 instead of the natural
// continuous-Speed lookup (which would be RT(10) = 17). This caps the
// NEW1 throttle one step shy of the Speed=10 rate so libvpx keeps
// testing NEW1 even after raising other thresholds.
//
// govpx mirror: libvpxInterModeCheckFrequenciesForCPISpeed
// (vp8_encoder_inter_speed.go:780) substitutes new1Speed=16 when
// deadline==DeadlineRealtime && speed==10.
func TestVP8SpeedFeaturesNew1ModeCheckFreqMirrorsLibvpxSpeed10(t *testing.T) {
	// At Speed=10 (RT cpu_used=-10), new1Speed lookup uses 16 (= RT(9)).
	// libvpxModeCheckFreqMapNew1 = {0, 17, 2, 18, 4, 19, 8, SpeedMapMax}.
	// At speed=16: 16<17 → return 0. (Without the override speed=17
	// would yield 2.)
	freq := libvpxInterModeCheckFrequenciesForCPISpeed(DeadlineRealtime, 10)
	if got, want := freq[libvpxThrNew1], 0; got != want {
		t.Fatalf("mode_check_freq[THR_NEW1] at Speed=10 = %d, want %d (libvpx onyx_if.c:877-879; speed2=RT(9)=16 → map yields 0)", got, want)
	}

	// At Speed=11 (no override): continuous=18 → map walk yields 4
	// ({0, RT(10)=17, 2, RT(11)=18, 4, RT(12)=19, ...}; at speed=18
	// the do-while continues past 17 and 18 and stops on 19 with
	// res=4).
	freq11 := libvpxInterModeCheckFrequenciesForCPISpeed(DeadlineRealtime, 11)
	if got, want := freq11[libvpxThrNew1], 4; got != want {
		t.Fatalf("mode_check_freq[THR_NEW1] at Speed=11 = %d, want %d (libvpx onyx_if.c:879; no override, continuous=18 → map yields 4)", got, want)
	}

	// At Speed=9 (no override): continuous=16 → map yields 0.
	freq9 := libvpxInterModeCheckFrequenciesForCPISpeed(DeadlineRealtime, 9)
	if got, want := freq9[libvpxThrNew1], 0; got != want {
		t.Fatalf("mode_check_freq[THR_NEW1] at Speed=9 = %d, want %d (no override, continuous=16 → map yields 0)", got, want)
	}
}
