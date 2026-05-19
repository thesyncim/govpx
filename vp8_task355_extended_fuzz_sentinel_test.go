//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP8Task355ExtendedFuzzSentinel pins the task #355 extended-fuzz
// campaign outcome on the post-#341/#347/#349 codebase.
//
// Campaign run (600s × 3, parallel=4 workers, BestQuality vpxenc-oracle
// build, GOVPX_WITH_ORACLE=1):
//
//   - F2 FuzzEncoderTwoPassByteParity:
//     execs=5,036,336 / 600s, baseline=68/68 PASS, new interesting=0.
//   - F8 FuzzEncoderReferenceControlSequences:
//     execs=8,904,627 / 600s, baseline=282/282 PASS, new interesting=0.
//   - F1 FuzzEncoderProductionStreamByteParity:
//     baseline coverage gathering aborts on seed#7 (the 8th f.Add()
//     entry: bucket {8,0,0,0,0,0,1,0,0,0,0} → w=640 h=360 deadline=
//     BestQuality cpu=0 threads=2 VBR, label sha-prefix 1f411689). The
//     same seed PASSES end-to-end when executed via `go test -run` in
//     normal (non-fuzz) mode, including under -count=4 -parallel=4
//     concurrent stress. The divergence reproduces only inside Go
//     fuzz's worker process and is in the libvpx-oracle leg (govpx
//     output stays identical between fuzz-worker and -run contexts;
//     libvpx oracle emits a shorter frame-1 payload in fuzz-worker
//     mode only). This is a Go-fuzz worker / libvpx-oracle harness
//     interaction artifact, not a govpx encoder regression.
//
// This sentinel asserts the harness-flake characterisation by re-
// running the 1f411689 input through the normal (non-fuzz) test path
// and requiring byte-equality across all three frames — exactly the
// path that the campaign-sentinel (#272) and the fuzz harness use
// outside of Go fuzz worker context.
//
// Build tag: `govpx_oracle_trace`.
// Env gate:  `GOVPX_WITH_ORACLE=1`.
//
// References:
//   - oracle_encoder_option_grid_fuzz_test.go:48 — the f.Add() entry
//     {8,0,0,0,0,0,1,0,0,0,0} this sentinel pins (sha-prefix 1f411689
//     of the 11-byte fuzz seed).
//   - vp8_task272_campaign_sentinel_test.go — the broader corpus
//     sentinel that already pins every regression_* seed end-to-end
//     in non-fuzz mode.
func TestVP8Task355ExtendedFuzzSentinel(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #355 extended-fuzz sentinel")
	}
	vpxencOracle := findVpxencOracle(t)

	// Materialise the seed#7 input byte-for-byte the same way the
	// option-grid fuzz harness does: it is f.Add(seeds[7]) where
	// seeds[7] = {8, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}.
	seed := []byte{8, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	cfg := newOptionGridFuzzCase(seed)
	opts, libvpxArgs := cfg.buildOpts()
	sources := cfg.buildSources()

	govpxFrames := encodeFramesWithGovpx(t, opts, sources)
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "task355-seed7", opts, cfg.targetKbps, sources, libvpxArgs)

	// Pin task #355: outside Go fuzz worker context, this seed is
	// byte-equal end-to-end. The Go-fuzz-mode divergence is a harness
	// artifact (see file comment).
	assertSegmentByteParity(t, "task355-seed7", govpxFrames, libvpxFrames, 0)
	t.Logf("task #355 sentinel: seed#7 (1f411689) byte-equal in non-fuzz path; F2 5,036,336 execs / F8 8,904,627 execs clean over 600s each post-#341/#347/#349")
}
