//go:build govpx_oracle_trace

package govpx

// Threading non-determinism quarantine for the libvpx oracle.
//
// Background — why this file exists
// ---------------------------------
// libvpx's encoder, when invoked with `--threads>=2`, is not deterministic
// across processes for VP8 in several modes (multi-thread loopfilter, MB-row
// thread scheduling, autoSpeed wall-clock timing). The ARNR pin-hold campaign
// (#207/#227 → #332) burnt ~50 audit tasks because traces kept conflating two
// orthogonal effects:
//
//  1. genuine govpx-vs-libvpx algorithmic divergence (the thing we want to
//     fix), AND
//  2. libvpx-internal threading nondeterminism (the thing that varies between
//     two consecutive oracle invocations even when the inputs are identical).
//
// At least four tasks were misattributed because of this trap:
//
//   * #297 (pretrellis UV bisect)
//   * #298 (splitmv RD bisect)
//   * #304 (BestARNR/GoodARNR rate_y gap chasing)
//   * #324 (chroma residual upstream audit)
//
// Per the campaign closure note (memory: feedback-vp8-arnr-milestone-closure):
//
//	"NEVER compare threads=4 libvpx scoreboards against threads=1 govpx.
//	 The MT-LF state contaminates the comparison. This trap poisoned
//	 #297/#298/#304 AND re-poisoned #324 under different framing."
//
// Phase 1 (task #349) inspection summary
// --------------------------------------
// All existing oracle invocations in /private/tmp/govpx-task-349 go through
// `encodeFramesWithLibvpxOracle` (oracle_encoder_stream_parity_test.go:1441).
// That helper:
//
//   - takes `extraArgs []string` and passes them verbatim, so `--threads=N`
//     can come from either the test cohort definition (parity tests) or from
//     the audit-test cohort `extraArgs := libvpxEndUsageArgs(...)` block
//     (BestARNR/GoodARNR audits explicitly pass `--threads=4`).
//   - shells out once to vpxenc-oracle and returns the parsed IVF frame
//     payloads.
//   - does NOT re-run for determinism, does NOT pin threads=1, does NOT
//     compare hashes across invocations.
//
// The oracle binary is pinned by SHA in
// `internal/coracle/oracle_sha_test.go`, but that pin only guards the BUILD
// pipeline (libvpx version, configure flags, patch stamp). It does NOT
// detect runtime threading nondeterminism.
//
// Phase 2 (task #349) design — combined Option B + Option C
// ---------------------------------------------------------
// We implement two complementary tools here:
//
//   * `encodeFramesWithLibvpxOracleReproducible` — re-runs the oracle N times
//     and fails the test if the per-frame payload bytes diverge across runs.
//     This is the "Option B" runtime check: catches new scenarios as they
//     hit threading nondeterminism, regardless of which `--threads=N` value
//     is in extraArgs.
//
//   * `requireOracleArgsReproducibleOrSerial` — inspects extraArgs and warns
//     (via t.Logf, optionally t.Fatal under GOVPX_ORACLE_THREADS_QUARANTINE
//     =strict) when an oracle invocation passes `--threads=N` with N>=2. This
//     is the "Option C" boundary documentation: makes the threading trap a
//     visible policy rather than tribal knowledge.
//
// Either helper can be opted into without changing existing callers. The
// pre-existing serial-vs-MT comparison sites (e.g. TestVP8Task332Threads
// Validation) are intentionally NOT migrated to the strict helper — they
// rely on threads>=2 by design and would defeat their own purpose if forced
// to threads=1.
//
// Phase 4 sentinel — TestVP8OracleReproducibilityCpu8Threads4
// -----------------------------------------------------------
// A new sentinel test (oracle_threading_quarantine_sentinel_test.go) exercises
// the known-unstable scenario (RT cpu_used=8 panning at threads=4) twice and
// asserts that:
//
//   - the wrapper produces consistent SHAs across two consecutive invocations
//     under threads=1 (the deterministic mode), AND
//   - the wrapper detects and reports divergence when the same scenario is
//     allowed to run at threads=4 with the controlled re-run (the "expected
//     trap" mode that gates future regressions in the quarantine wrapper
//     itself).

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// EncodeFramesWithLibvpxOracleReproducibleRuns is the default re-run count
// for the reproducibility check. Two runs is the minimum needed to detect
// any divergence; the cost is one extra subprocess per audit test, which is
// negligible compared to the audit's encode time on a 1280x720 keyframe.
const EncodeFramesWithLibvpxOracleReproducibleRuns = 2

// encodeFramesWithLibvpxOracleReproducible wraps encodeFramesWithLibvpxOracle
// with a re-run sanity check: it invokes the oracle `runs` times against the
// same inputs and fails the test if any per-frame payload bytes differ across
// runs. The intent is to fail LOUDLY when an audit's oracle output is in fact
// nondeterministic, rather than silently propagating a flake into the
// downstream byte comparison.
//
// Callers that already accept --threads>=2 as part of their cohort (e.g. the
// BestARNR/GoodARNR audits where `--threads=4` is the seed-encoded option)
// should use this helper. If the oracle is in fact deterministic for those
// options on the current host, the helper is a no-op. If it is not, the
// helper turns the contamination from an invisible audit artefact into a
// visible test failure with a SHA log.
//
// `runs` must be >= 2; runs < 2 falls back to runs=2 (a single run cannot
// detect divergence). Pass `runs == EncodeFramesWithLibvpxOracleReproducible
// Runs` (== 2) for the canonical setting.
func encodeFramesWithLibvpxOracleReproducible(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string, runs int) [][]byte {
	t.Helper()
	if runs < 2 {
		runs = EncodeFramesWithLibvpxOracleReproducibleRuns
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
			firstSums = framePayloadSHAs(frames)
			continue
		}
		if len(frames) != len(first) {
			t.Fatalf("libvpx oracle threading-quarantine: run %d returned %d frames, run 0 returned %d (extraArgs=%v)",
				run, len(frames), len(first), extraArgs)
		}
		sums := framePayloadSHAs(frames)
		for i := range sums {
			if sums[i] != firstSums[i] {
				t.Fatalf(`libvpx oracle threading-quarantine: NONDETERMINISTIC OUTPUT detected.

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
produced ~50 misattributed task fixes (#297/#298/#304/#324 etc).

Remediation options:
  1) Drop --threads=N from extraArgs (force the oracle to serial).
  2) Pin the per-run output to a specific run-0 SHA in the test (the
     test is now flaky and needs explicit retry-with-SHA-pinning).
  3) Move the comparison to a higher level (e.g. SSIM/PSNR vs IVF
     payload SHA), where minor MT-LF reordering is invisible.

See feedback-vp8-arnr-milestone-closure.md lesson #2.`,
					name, i, firstSums[i], run, i, sums[i], extraArgs)
			}
		}
	}
	return first
}

// framePayloadSHAs returns a per-frame "<sha8>:<len>" string slice. Truncated
// to the first 8 hex chars to keep failure messages readable; the leading 8
// chars are sufficient to detect any divergence in practice (collision
// probability < 2^-32 per frame, vs the < 2^-256 of the full hash).
func framePayloadSHAs(frames [][]byte) []string {
	out := make([]string, len(frames))
	for i, p := range frames {
		h := sha256.Sum256(p)
		out[i] = hex.EncodeToString(h[:8]) + ":" + itoaPositive(len(p))
	}
	return out
}

// extraArgsRequestsParallelOracle returns true iff `extraArgs` contains a
// `--threads=N` argument with N >= 2. Used by requireOracleArgsReproducible
// OrSerial to flag the known threading-nondeterminism trap.
func extraArgsRequestsParallelOracle(extraArgs []string) (threads int, ok bool) {
	for _, a := range extraArgs {
		if !strings.HasPrefix(a, "--threads=") {
			continue
		}
		v := strings.TrimPrefix(a, "--threads=")
		n := 0
		for _, c := range v {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		return n, n >= 2
	}
	return 0, false
}

// requireOracleArgsReproducibleOrSerial inspects extraArgs and, when the
// oracle is being invoked with --threads>=2, surfaces the threading boundary
// via t.Logf so the audit's interpretation explicitly acknowledges the trap.
//
// When GOVPX_ORACLE_THREADS_QUARANTINE=strict the helper instead fails the
// test, forcing the caller to either drop --threads from extraArgs or switch
// to encodeFramesWithLibvpxOracleReproducible. This mode is intended for
// fresh audits (where the test author has not yet decided which side of the
// trade-off applies); existing tests that rely on threads>=2 by design
// should keep using the non-strict default.
func requireOracleArgsReproducibleOrSerial(t *testing.T, extraArgs []string) {
	t.Helper()
	threads, parallel := extraArgsRequestsParallelOracle(extraArgs)
	if !parallel {
		return
	}
	msg := "libvpx oracle threading-quarantine: this audit passes --threads=" +
		itoaPositive(threads) + " to vpxenc-oracle; libvpx's encoder is " +
		"NOT byte-reproducible across runs at threads>=2 for several VP8 " +
		"configurations. Treat any byte-level divergence against govpx " +
		"as suspect until the oracle has been independently verified " +
		"reproducible at this scenario (see encodeFramesWithLibvpxOracle" +
		"Reproducible). See feedback-vp8-arnr-milestone-closure.md " +
		"lesson #2."
	if os.Getenv("GOVPX_ORACLE_THREADS_QUARANTINE") == "strict" {
		t.Fatal(msg)
	}
	t.Logf("%s", msg)
}
