//go:build govpx_oracle_trace

package govpx

// The libvpx VP8 oracle can emit different bytes across subprocess runs when
// --threads>=2 is combined with realtime content whose auto-speed path reads
// wall-clock timing. These helpers make that nondeterminism explicit: one
// wrapper fails if repeated oracle runs disagree, and the matching wrapper
// searches a bounded retry budget when govpx is expected to match any valid
// threaded libvpx output.

import (
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// VP8OracleReproducibleRuns is the default re-run count for the VP8 oracle
// reproducibility check. Two runs is the minimum needed to detect divergence;
// the cost is one extra subprocess per oracle test, which is negligible
// compared to the oracle test's encode time on a 1280x720 keyframe.
const VP8OracleReproducibleRuns = 2

// encodeVP8FramesWithLibvpxOracleReproducible wraps encodeFramesWithLibvpxOracle
// with a re-run sanity check: it invokes the oracle `runs` times against the
// same inputs and fails the test if any per-frame payload bytes differ across
// runs. The intent is to fail when an oracle test's oracle output is in fact
// nondeterministic, rather than silently propagating a flake into the
// downstream byte comparison.
//
// Callers that already accept --threads>=2 as part of their cohort (e.g. the
// BestARNR/GoodARNR oracle tests where `--threads=4` is the seed-encoded option)
// should use this helper. If the oracle is in fact deterministic for those
// options on the current host, the helper is a no-op. If it is not, the
// helper turns the contamination from an invisible oracle artifact into a
// visible test failure with a SHA log.
//
// `runs` must be >= 2; runs < 2 falls back to runs=2 because a single run
// cannot detect divergence. Pass `runs == VP8OracleReproducibleRuns` for the
// canonical setting.
func encodeVP8FramesWithLibvpxOracleReproducible(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string, runs int) [][]byte {
	t.Helper()
	if runs < 2 {
		runs = VP8OracleReproducibleRuns
	}
	var first [][]byte
	var firstSums []string
	for run := 0; run < runs; run++ {
		// Use a distinct per-run name so the helper's t.TempDir() ivf
		// path is unique; otherwise multiple runs in the same test
		// would overwrite each other's debug artefacts.
		runName := name + "-quarantine-run-" + itoaPositive(run)
		frames := encodeFramesWithLibvpxOracle(t, vpxencOracle, runName, opts, targetKbps, sources, extraArgs)
		if run == 0 {
			first = frames
			firstSums = testutil.FramePayloadSHA8s(frames)
			continue
		}
		if len(frames) != len(first) {
			t.Fatalf("libvpx oracle threading reproducibility: run %d returned %d frames, run 0 returned %d (extraArgs=%v)",
				run, len(frames), len(first), extraArgs)
		}
		sums := testutil.FramePayloadSHA8s(frames)
		for i := range sums {
			if sums[i] != firstSums[i] {
				t.Fatalf(`libvpx oracle threading reproducibility: NONDETERMINISTIC OUTPUT detected.

Same input, same extraArgs, two invocations produced different bytes:

  test:       %s
  run 0 SHA:  frame %d %s
  run %d SHA: frame %d %s
  extraArgs:  %v

This means the libvpx oracle is producing different bytes for the same
input across consecutive runs, almost certainly because of threading
nondeterminism (MT-LF state, MB-row scheduling, autoSpeed wall-clock
timing). Comparing govpx (deterministic) byte output against libvpx
output under these conditions is apples-to-oranges and historically
produced non-actionable byte-parity diagnoses.

Remediation options:
  1) Drop --threads=N from extraArgs (force the oracle to serial).
  2) Pin the per-run output to a specific run-0 SHA in the test (the
     test is now flaky and needs explicit retry-with-SHA-pinning).
  3) Move the comparison to a higher level (e.g. SSIM/PSNR vs IVF
     payload SHA), where minor MT-LF reordering is invisible.

Re-run the oracle serially or compare against any reproducible threaded run
before treating this as a codec divergence.`,
					name, i, firstSums[i], run, i, sums[i], extraArgs)
			}
		}
	}
	return first
}

// VP8OracleMatchingGovpxMaxRetries is the cap on how many
// times encodeVP8FramesWithLibvpxOracleMatchingGovpx will re-invoke the libvpx
// oracle while searching for a run whose payload bytes match govpx exactly.
const VP8OracleMatchingGovpxMaxRetries = 6

// encodeVP8FramesWithLibvpxOracleMatchingGovpx invokes the libvpx oracle once,
// and if its bytes do not byte-equal `govpxFrames`, re-invokes the oracle up
// to `VP8OracleMatchingGovpxMaxRetries` more times in
// search of a matching run. The first matching run's frames are returned.
// When no matching run is found, the first run's frames are returned along
// with a t.Logf diagnostic that records the distribution of distinct oracle
// outputs observed across the retry budget (callers downstream assert byte
// parity against the returned frames and will surface the real divergence as
// an ordinary mismatch).
//
// This helper is the targeted remedy for libvpx-side threading non-
// determinism in the F1 byte-parity fuzz: govpx (deterministic) must match
// one of libvpx's valid outputs at the configured threads=N, not the
// specific output a given subprocess invocation happened to produce on this
// run. Without the retry, F1 byte-parity is flaky on seeds where
// `vp8_auto_select_speed`'s wall-clock IIR crosses a branch boundary across
// invocations. The threaded option-grid parity test covers that case with a
// known 640x360 realtime seed.
//
// When extraArgs does NOT contain --threads>=2 the helper is a single-call
// pass-through to encodeFramesWithLibvpxOracle, preserving the existing
// behaviour for serial-oracle callers.
func encodeVP8FramesWithLibvpxOracleMatchingGovpx(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string, govpxFrames [][]byte) [][]byte {
	t.Helper()
	if _, parallel := vp8test.VP8VpxencThreadsArg(extraArgs); !parallel {
		// Serial oracle invocations are deterministic; one run suffices.
		return encodeFramesWithLibvpxOracle(t, vpxencOracle, name, opts, targetKbps, sources, extraArgs)
	}
	govpxSums := testutil.FramePayloadSHA8s(govpxFrames)
	maxRetries := VP8OracleMatchingGovpxMaxRetries
	var firstFrames [][]byte
	seen := map[string]int{}
	for attempt := 0; attempt <= maxRetries; attempt++ {
		runName := name + "-mtgovpx-attempt-" + itoaPositive(attempt)
		frames := encodeFramesWithLibvpxOracle(t, vpxencOracle, runName, opts, targetKbps, sources, extraArgs)
		if attempt == 0 {
			firstFrames = frames
		}
		sums := testutil.FramePayloadSHA8s(frames)
		seen[strings.Join(sums, ",")]++
		if len(sums) == len(govpxSums) {
			match := true
			for i := range sums {
				if sums[i] != govpxSums[i] {
					match = false
					break
				}
			}
			if match {
				if attempt > 0 {
					t.Logf("libvpx oracle threading reproducibility: %s matched govpx after %d retries (extraArgs=%v); distinct oracle outputs observed=%d",
						name, attempt, extraArgs, len(seen))
				}
				return frames
			}
		}
	}
	// No matching run found within budget — return the first run and let
	// the caller's byte-parity assertion surface the real divergence with
	// the diagnostic context attached.
	keys := make([]string, 0, len(seen))
	for k, c := range seen {
		keys = append(keys, k+"(x"+itoaPositive(c)+")")
	}
	t.Logf("libvpx oracle threading reproducibility: %s NO matching run found across %d attempts (extraArgs=%v); govpx_sums=%v; oracle distinct outputs=%v",
		name, maxRetries+1, extraArgs, govpxSums, keys)
	return firstFrames
}

// requireVP8OracleArgsReproducibleOrSerial inspects extraArgs and, when the
// oracle is being invoked with --threads>=2, surfaces the threading boundary
// via t.Logf so the oracle test's interpretation explicitly acknowledges the trap.
//
// When GOVPX_ORACLE_THREADS_QUARANTINE=strict the helper instead fails the
// test, forcing the caller to either drop --threads from extraArgs or switch
// to encodeVP8FramesWithLibvpxOracleReproducible. This mode is intended for
// fresh oracle tests (where the test author has not yet decided which side of the
// trade-off applies); existing tests that rely on threads>=2 by design
// should keep using the non-strict default.
func requireVP8OracleArgsReproducibleOrSerial(t *testing.T, extraArgs []string) {
	t.Helper()
	threads, parallel := vp8test.VP8VpxencThreadsArg(extraArgs)
	if !parallel {
		return
	}
	msg := "libvpx oracle threading reproducibility: this test passes --threads=" +
		itoaPositive(threads) + " to vpxenc-oracle; libvpx's encoder is " +
		"NOT byte-reproducible across runs at threads>=2 for several VP8 " +
		"configurations. Treat any byte-level divergence against govpx " +
		"as suspect until the oracle has been independently verified " +
		"reproducible at this scenario (see " +
		"encodeVP8FramesWithLibvpxOracleReproducible)."
	if vp8test.StrictThreadedOracleQuarantine() {
		t.Fatal(msg)
	}
	t.Logf("%s", msg)
}
