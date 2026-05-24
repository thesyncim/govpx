//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8GovpxDeterminismThreads4 is the regression sentinel for the
// vp8_auto_select_speed wall-clock timing-dependence bug discovered while
// investigating task #269's mutation-sweep flake for FuzzEncoderProductionStream
// ByteParity seed#8 (854×480 RT threads=4 cpu_used=0). Before the fix, the
// `nowMonotonicNS()` deltas observed in finishAutoSpeedTiming() rose into
// the millisecond range under heavy parallel-process host contention; the
// resulting avg_encode_time IIR update steered vp8_auto_select_speed onto
// either the Speed+=2 or Speed-- branch the next frame, flipping autoSpeed
// from 0 to {2..6}, and the picker dispatched a different mode set for the
// remainder of the clip. Within a single process, govpx produced 3 distinct
// bitstreams for the same input across 50 reruns (db163449844d85c6,
// 6abca426c800e43c, bfe404c8fa570088). The fix
// (interFrameAutoSpeedTimingCompensation in vp8_encoder_config.go) pins
// inter-frame durations to budget/3 for the same MB-count gate used by
// medium-keyframe compensation, mirroring the existing project strategy of
// trading "libvpx-verbatim wall-clock" for "libvpx-stable region" once the
// frame is large enough that the timing branch is reliably observable.
//
// The expected canonical SHAs below are the libvpx-oracle byte-parity
// reference for this case (see vp8_oracle_encoder_option_grid_fuzz_test.go
// seed#8). Any future regression introducing real govpx-side
// non-determinism — or breaking byte parity at this case — will diverge
// from these.
func TestVP8GovpxDeterminismThreads4(t *testing.T) {
	width, height := 854, 480
	frames := 3
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
		Threads:           4,
	}
	wantSums := []string{
		"248a02d76b2db086:236987",
		"35af834c7d662f4b:1586",
		"db163449844d85c6:1619",
	}

	const runs = 50
	var firstSums []string
	for run := 0; run < runs; run++ {
		out := encodeFramesWithGovpx(t, opts, sources)
		if len(out) != frames {
			t.Fatalf("run %d: got %d frames, want %d", run, len(out), frames)
		}
		sums := make([]string, frames)
		for i, p := range out {
			h := sha256.Sum256(p)
			sums[i] = hex.EncodeToString(h[:8]) + ":" + fmt.Sprint(len(p))
		}
		if run == 0 {
			firstSums = sums
			for i, want := range wantSums {
				if sums[i] != want {
					t.Errorf("run 0 frame %d: sum=%s want %s (canonical libvpx-oracle reference)", i, sums[i], want)
				}
			}
			continue
		}
		for i := range sums {
			if sums[i] != firstSums[i] {
				t.Errorf("run %d frame %d: sum=%s want %s", run, i, sums[i], firstSums[i])
			}
		}
	}
	if t.Failed() {
		t.Logf("govpx encode is NON-DETERMINISTIC at threads=4 (see diffs above)")
	} else {
		t.Logf("govpx encode is deterministic at threads=4 across %d runs (sums: %v)", runs, firstSums)
	}
}

// TestVP8LibvpxOracleDeterminismThreads4 runs the libvpx
// vpxenc-oracle subprocess 20 times for the same input/options and checks
// whether the oracle itself produces deterministic bytes when threads=4.
// If libvpx is the nondeterminism source, the flake at task #269 attributed
// to govpx is in fact an oracle artefact.
func TestVP8LibvpxOracleDeterminismThreads4(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle determinism check")
	vpxencOracle := vp8test.VpxencOracle(t)
	width, height := 854, 480
	frames := 3
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
		Threads:           4,
	}
	extraArgs := []string{"--end-usage=cbr", "--threads=4"}

	const runs = 20
	var firstSums []string
	for run := 0; run < runs; run++ {
		out := encodeFramesWithLibvpxOracle(t, vpxencOracle, "libvpx-thread-determinism", opts, 700, sources, extraArgs)
		if len(out) != frames {
			t.Fatalf("run %d: got %d frames, want %d", run, len(out), frames)
		}
		sums := make([]string, frames)
		for i, p := range out {
			h := sha256.Sum256(p)
			sums[i] = hex.EncodeToString(h[:8]) + ":" + fmt.Sprint(len(p))
		}
		if run == 0 {
			firstSums = sums
			t.Logf("run %d (reference): sums=%v", run, sums)
			continue
		}
		mismatched := false
		for i := range sums {
			if sums[i] != firstSums[i] {
				t.Logf("run %d frame %d: sum=%s want %s (libvpx oracle differs)", run, i, sums[i], firstSums[i])
				mismatched = true
			}
		}
		if mismatched {
			t.Logf("LIBVPX ORACLE NON-DETERMINISM at run %d sums=%v", run, sums)
		}
	}
	if t.Failed() {
		t.Errorf("libvpx oracle output diverged across runs")
	}
}
