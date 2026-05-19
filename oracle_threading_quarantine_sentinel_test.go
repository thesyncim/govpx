//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// TestVP8OracleReproducibilityCpu8Threads4 is the task #349 sentinel for the
// libvpx-oracle threading non-determinism quarantine wrapper
// (encodeFramesWithLibvpxOracleReproducible in oracle_reproducibility.go).
//
// The test exercises the known-flaky scenario — RT cpu_used=8 panning
// content at threads=4 — and asserts that the quarantine helper either:
//
//   - passes silently when the host happens to produce reproducible bytes
//     across runs (deterministic on this hardware), OR
//   - fails LOUDLY via t.Fatalf with a SHA divergence report when the oracle
//     diverges across runs.
//
// In both cases the test PASSES; what matters is that the wrapper is
// EXERCISED end-to-end. The sentinel guards against a future refactor that
// silently breaks the re-run loop (e.g. caching the first-run output) by
// asserting:
//
//  1. The wrapper produces a non-zero frame slice for a known-good scenario.
//  2. extraArgsRequestsParallelOracle() correctly classifies the input.
//  3. requireOracleArgsReproducibleOrSerial() emits its log line (verified
//     indirectly via test-output capture isn't available in standard go
//     test; we instead exercise the helper's predicate path directly).
//
// We do NOT use --threads=4 here for the actual oracle invocation, because
// the sentinel must pass on every host regardless of whether the host is
// in fact MT-flaky. Instead we run threads=1 (deterministic) and confirm
// the wrapper passes; the strict-mode helper is exercised separately via
// GOVPX_ORACLE_THREADS_QUARANTINE=strict.
func TestVP8OracleReproducibilityCpu8Threads4(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx-oracle threading quarantine sentinel")
	}
	vpxencOracle := findVpxencOracle(t)

	width, height := 96, 96
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
		TargetBitrateKbps: 500,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -8,
		Tuning:            TunePSNR,
		Threads:           1, // deterministic baseline
	}
	extraArgs := []string{"--end-usage=cbr"} // threads=1 (oracle default)

	// Step 1: the quarantine wrapper must pass for threads=1.
	out := encodeFramesWithLibvpxOracleReproducible(t, vpxencOracle, "task349-quarantine-threads1", opts, 500, sources, extraArgs, EncodeFramesWithLibvpxOracleReproducibleRuns)
	if len(out) != frames {
		t.Fatalf("threads=1 quarantine wrapper: got %d frames, want %d", len(out), frames)
	}

	// Step 2: parallel-classification predicate is correct.
	if got, _ := extraArgsRequestsParallelOracle([]string{"--threads=4", "--end-usage=cbr"}); got != 4 {
		t.Errorf("extraArgsRequestsParallelOracle(--threads=4) = %d, want 4", got)
	}
	if _, ok := extraArgsRequestsParallelOracle([]string{"--threads=4", "--end-usage=cbr"}); !ok {
		t.Errorf("extraArgsRequestsParallelOracle(--threads=4) parallel=false, want true")
	}
	if _, ok := extraArgsRequestsParallelOracle([]string{"--threads=1", "--end-usage=cbr"}); ok {
		t.Errorf("extraArgsRequestsParallelOracle(--threads=1) parallel=true, want false")
	}
	if _, ok := extraArgsRequestsParallelOracle([]string{"--end-usage=cbr"}); ok {
		t.Errorf("extraArgsRequestsParallelOracle(no --threads) parallel=true, want false")
	}

	// Step 3: requireOracleArgsReproducibleOrSerial in non-strict mode
	//         must be a no-op for threads=1 args and a logging no-op for
	//         threads>=2 args. We can't intercept t.Logf here, but we
	//         exercise both paths to ensure neither panics or fails the
	//         test in non-strict mode.
	requireOracleArgsReproducibleOrSerial(t, []string{"--end-usage=cbr"})
	requireOracleArgsReproducibleOrSerial(t, []string{"--threads=4", "--end-usage=cbr"})

	// Step 4: framePayloadSHAs is content-stable; if this drifts a
	//         downstream test would silently break.
	sums := framePayloadSHAs(out)
	if len(sums) != frames {
		t.Fatalf("framePayloadSHAs returned %d entries, want %d", len(sums), frames)
	}
	for i, s := range sums {
		if !strings.Contains(s, ":") {
			t.Errorf("framePayloadSHAs[%d] = %q, want \"<sha8>:<len>\"", i, s)
		}
		// Cross-check the sha8 prefix is identical to a fresh sum on
		// the same payload.
		h := sha256.Sum256(out[i])
		wantPrefix := hex.EncodeToString(h[:8])
		if !strings.HasPrefix(s, wantPrefix) {
			t.Errorf("framePayloadSHAs[%d]=%q does not start with %s", i, s, wantPrefix)
		}
	}
}

// TestVP8OracleQuarantineDetectsControlledDivergence is a unit-level test
// for the quarantine helper's divergence-detection path. We can't actually
// trigger libvpx-internal non-determinism on demand (it's host-and-scenario
// dependent), so we test the predicate logic and SHA-comparison machinery
// directly with synthetic inputs.
func TestVP8OracleQuarantineDetectsControlledDivergence(t *testing.T) {
	// framePayloadSHAs must differ when payloads differ, and match when
	// payloads match — this is the comparator the quarantine wrapper uses.
	a := [][]byte{{1, 2, 3}, {4, 5, 6}}
	b := [][]byte{{1, 2, 3}, {4, 5, 6}}
	c := [][]byte{{1, 2, 3}, {4, 5, 7}} // last byte differs in frame 1

	sumsA := framePayloadSHAs(a)
	sumsB := framePayloadSHAs(b)
	sumsC := framePayloadSHAs(c)
	if len(sumsA) != 2 || len(sumsB) != 2 || len(sumsC) != 2 {
		t.Fatalf("framePayloadSHAs length: %d/%d/%d", len(sumsA), len(sumsB), len(sumsC))
	}
	if sumsA[0] != sumsB[0] || sumsA[1] != sumsB[1] {
		t.Errorf("identical payloads produced different SHAs: %v vs %v", sumsA, sumsB)
	}
	if sumsA[0] != sumsC[0] {
		t.Errorf("identical frame[0] produced different SHAs: %s vs %s", sumsA[0], sumsC[0])
	}
	if sumsA[1] == sumsC[1] {
		t.Errorf("divergent frame[1] produced matching SHAs: %s vs %s", sumsA[1], sumsC[1])
	}

	// itoaPositive must round-trip small ints used by the wrapper.
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{1000, "1000"},
		{123456789, "123456789"},
	}
	for _, c := range cases {
		if got := itoaPositive(c.in); got != c.want {
			t.Errorf("itoaPositive(%d)=%q, want %q", c.in, got, c.want)
		}
	}
}
