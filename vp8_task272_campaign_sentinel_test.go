//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestVP8Task272CampaignSentinel is the campaign-closure regression gate.
//
// Tasks #262 and #263 closed every known byte-divergent F1 fuzz seed that the
// FuzzEncoderProductionStreamByteParity option-grid harness had captured into
// the corpus, plus the 1280×720 / 854×480 / 640×360 inter-divergence seeds
// that lived alongside as standalone regression files. This single test is
// the permanent regression gate for that closure: it iterates every
// regression seed file in
// testdata/fuzz/FuzzEncoderProductionStreamByteParity and runs each one
// against the libvpx-vpxenc-oracle binary, asserting strict per-frame
// bytes.Equal across the seed's full encoded clip.
//
// It also re-runs the byte-exact audit-test cohort that lived as standalone
// audit tests prior to closure (TestVP8Byte0KF1280x720SSIMAudit,
// TestVP8Byte0KF1280x720SSIMGoodCBRArnrClosed, TestVP8Byte49Frame2DivergenceClosure,
// TestVP8Byte58Frame2DivergenceAudit) — these audit configurations are
// byte-exact end-to-end today and become subtests under the sentinel so a
// single test invocation proves the campaign-closure state.
//
// If any subtest fails, the sentinel reports the seed/config label, frame
// index, govpx-vs-libvpx length pair, first-byte-diff offset and the
// surrounding SHA8 prefixes — enough to triage which port regressed without
// rerunning the underlying fuzz harness.
//
// Build tag: `govpx_oracle_trace`.
// Env gate:  `GOVPX_WITH_ORACLE=1`. Both match the existing oracle-parity
// pattern so this test is opt-in and only runs when the patched libvpx
// vpxenc-oracle binary is built (Makefile target `make oracle-tools`).
//
// References:
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:427-438 — segmentation-enabled
//     vp8cx_mb_init_quantizer call (#262 closure root cause).
//   - libvpx v1.16.0 vp8/encoder/onyx_if.c:3779 — cyclic_background_refresh
//     flipping segmentation_enabled on every CBR keyframe (#262/#263).
//   - testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_* —
//     the live corpus this sentinel re-enumerates each run.
func TestVP8Task272CampaignSentinel(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the campaign-closure sentinel")
	}
	vpxencOracle := findVpxencOracle(t)

	t.Run("corpus", func(t *testing.T) {
		runVP8Task272CorpusSubtests(t, vpxencOracle)
	})
	t.Run("audit-cohort", func(t *testing.T) {
		runVP8Task272AuditCohortSubtests(t, vpxencOracle)
	})
}

// runVP8Task272CorpusSubtests enumerates every regression_* file in the
// FuzzEncoderProductionStreamByteParity corpus, decodes each seed via
// newOptionGridFuzzCase (the same dispatch the live fuzz target uses) and
// asserts every produced frame is byte-identical to the libvpx oracle.
func runVP8Task272CorpusSubtests(t *testing.T, vpxencOracle string) {
	corpusDir := filepath.Join("testdata", "fuzz", "FuzzEncoderProductionStreamByteParity")
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("read corpus dir %s: %v", corpusDir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "regression_") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("no regression_* files found under %s", corpusDir)
	}
	t.Logf("task #272 sentinel: %d regression corpus seeds enumerated", len(names))

	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			seedPath := filepath.Join(corpusDir, name)
			seed, err := readGoFuzzCorpusByteSeed(seedPath)
			if err != nil {
				t.Fatalf("parse seed %s: %v", seedPath, err)
			}
			cfg := newOptionGridFuzzCase(seed)
			opts, libvpxArgs := cfg.buildOpts()
			sources := cfg.buildSources()

			sum := sha256.Sum256(seed)
			label := "task272-" + cfg.name + "-" + hex.EncodeToString(sum[:4])
			t.Logf("%s w=%d h=%d deadline=%v cpu=%d threads=%d rc=%v sharp=%d tune=%v sc=%d er=%t token=%d arnr=%d/%d/%d frames=%d",
				label, opts.Width, opts.Height, opts.Deadline, opts.CpuUsed,
				opts.Threads, opts.RateControlMode, opts.Sharpness, opts.Tuning,
				opts.ScreenContentMode, opts.ErrorResilient, opts.TokenPartitions,
				opts.ARNRMaxFrames, opts.ARNRStrength, opts.ARNRType, len(sources))

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			// Task #349: route through the libvpx threading
			// non-determinism quarantine wrapper. Some seeds in
			// this sentinel decode to threads>=2 cohorts; the
			// wrapper is a no-op for serial seeds and a re-run
			// SHA check for parallel seeds.
			libvpxFrames := encodeFramesWithLibvpxOracleReproducible(t, vpxencOracle, label, opts, cfg.targetKbps, sources, libvpxArgs, EncodeFramesWithLibvpxOracleReproducibleRuns)

			assertVP8Task272StrictByteParity(t, name, govpxFrames, libvpxFrames)
		})
	}
}

// runVP8Task272AuditCohortSubtests re-runs the byte-exact audit-test
// cohort. Only audits whose existing pin asserts identical govpx/libvpx
// frame lengths across every frame are included; audits with documented
// open inter-gaps (e.g. TestVP8Byte0KF1280x720SSIMBestARNRAudit,
// TestVP8Byte0KF1280x720SSIMGoodARNRAudit which pin Δ=5/6-byte inter
// drifts) are left to their own test files since they are not yet
// byte-closed and do not belong in the byte-exact campaign sentinel.
func runVP8Task272AuditCohortSubtests(t *testing.T, vpxencOracle string) {
	type auditCase struct {
		name      string
		opts      EncoderOptions
		extraArgs []string
		frames    int
		targetKbp int
	}
	cases := []auditCase{
		{
			// Companion live regression:
			//   regression_option_grid_94eb71d5 (seed bytes "A1200000")
			// 1280x720 / GoodQuality / cpu=0 / CBR / TuneSSIM / arnr=1/0/1.
			// Closed by task #213 (activityProbeStaleActZbinAdj) — pinned
			// in TestVP8Byte0KF1280x720SSIMAudit.
			name: "ssim-audit-1280x720-cbr-cpu0",
			opts: EncoderOptions{
				Width: 1280, Height: 720, FPS: 30,
				RateControlMode: RateControlCBR, TargetBitrateKbps: 700,
				MinQuantizer: 4, MaxQuantizer: 56, KeyFrameInterval: 999,
				Deadline: DeadlineGoodQuality, CpuUsed: 0, Tuning: TuneSSIM,
				ARNRMaxFrames: 1, ARNRStrength: 2, ARNRType: 1,
			},
			extraArgs: libvpxEndUsageArgs([]string{
				"--end-usage=cbr", "--tune=ssim",
				"--arnr-maxframes=1", "--arnr-strength=2", "--arnr-type=1",
			}),
			frames: 2, targetKbp: 700,
		},
		{
			// Companion live regression:
			//   regression_option_grid_22f3d67c (seed bytes "A120")
			// 1280x720 / GoodQuality / cpu=0 / threads=4 / token=1 / sc=1
			// / TuneSSIM / CBR / arnr=1/2/1. Closed by task #262 — pinned
			// in TestVP8Byte0KF1280x720SSIMGoodCBRArnrClosed.
			name: "ssim-good-cbr-arnr-closed-1280x720-threads4",
			opts: EncoderOptions{
				Width: 1280, Height: 720, FPS: 30,
				RateControlMode: RateControlCBR, TargetBitrateKbps: 700,
				MinQuantizer: 4, MaxQuantizer: 56, KeyFrameInterval: 999,
				Deadline: DeadlineGoodQuality, CpuUsed: 0, Tuning: TuneSSIM,
				ScreenContentMode: 1, TokenPartitions: 1, Threads: 4,
				ARNRMaxFrames: 1, ARNRStrength: 2, ARNRType: 1,
			},
			extraArgs: libvpxEndUsageArgs([]string{
				"--end-usage=cbr", "--screen-content-mode=1",
				"--token-parts=1", "--threads=4", "--tune=ssim",
				"--arnr-maxframes=1", "--arnr-strength=2", "--arnr-type=1",
			}),
			frames: 2, targetKbp: 700,
		},
		{
			// Companion live regression:
			//   regression_option_grid_a438fec8 (seed bytes "1200000")
			// 128x128 / BestQuality / cpu=4 / CBR / TuneSSIM, 6 frames.
			// Closed by task #201 — pinned in
			// TestVP8Byte49Frame2DivergenceClosure.
			name: "byte49-frame2-closure-128x128-best-cpu4",
			opts: EncoderOptions{
				Width: 128, Height: 128, FPS: 30,
				RateControlMode: RateControlCBR, TargetBitrateKbps: 700,
				MinQuantizer: 4, MaxQuantizer: 56, KeyFrameInterval: 999,
				Deadline: DeadlineBestQuality, CpuUsed: 4, Tuning: TuneSSIM,
			},
			extraArgs: libvpxEndUsageArgs([]string{
				"--end-usage=cbr", "--tune=ssim",
			}),
			frames: 6, targetKbp: 700,
		},
		{
			// Companion live regression:
			//   regression_option_grid_75578e9f (seed bytes "21200000")
			// 160x96 / GoodQuality / cpu=0 / CBR / TuneSSIM / arnr=1/2/1,
			// 6 frames. Closed by task #201 — pinned in
			// TestVP8Byte58Frame2DivergenceAudit.
			name: "byte58-frame2-audit-160x96-good-cpu0",
			opts: EncoderOptions{
				Width: 160, Height: 96, FPS: 30,
				RateControlMode: RateControlCBR, TargetBitrateKbps: 700,
				MinQuantizer: 4, MaxQuantizer: 56, KeyFrameInterval: 999,
				Deadline: DeadlineGoodQuality, CpuUsed: 0, Tuning: TuneSSIM,
				ARNRMaxFrames: 1, ARNRStrength: 2, ARNRType: 1,
			},
			extraArgs: libvpxEndUsageArgs([]string{
				"--end-usage=cbr", "--tune=ssim",
				"--arnr-maxframes=1", "--arnr-strength=2", "--arnr-type=1",
			}),
			frames: 6, targetKbp: 700,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(tc.opts.Width, tc.opts.Height, i)
			}
			label := "task272-" + tc.name
			govpxFrames := encodeFramesWithGovpx(t, tc.opts, sources)
			// Task #349 quarantine: re-run wrapper catches any
			// libvpx-side threading nondeterminism that would
			// otherwise contaminate the strict byte comparison.
			libvpxFrames := encodeFramesWithLibvpxOracleReproducible(t, vpxencOracle, label, tc.opts, tc.targetKbp, sources, tc.extraArgs, EncodeFramesWithLibvpxOracleReproducibleRuns)
			assertVP8Task272StrictByteParity(t, tc.name, govpxFrames, libvpxFrames)
		})
	}
}

// assertVP8Task272StrictByteParity is the strict-byte-equality assertion
// used by every campaign-sentinel subtest. Frame count, per-frame length,
// per-frame bytes and first-byte-diff offset are all reported on failure.
func assertVP8Task272StrictByteParity(t *testing.T, label string, govpxFrames, libvpxFrames [][]byte) {
	t.Helper()
	if len(govpxFrames) != len(libvpxFrames) {
		t.Fatalf("%s: frame count mismatch: govpx=%d libvpx=%d",
			label, len(govpxFrames), len(libvpxFrames))
	}
	if len(govpxFrames) == 0 {
		t.Fatalf("%s: zero frames encoded", label)
	}
	for i := range govpxFrames {
		gv := govpxFrames[i]
		lv := libvpxFrames[i]
		gSHA := sha256.Sum256(gv)
		lSHA := sha256.Sum256(lv)
		if bytes.Equal(gv, lv) {
			gFP, gIsKey := parseVP8FramePartitionSizes(gv)
			t.Logf("%s frame %d byte MATCH: len=%d first_part=%d keyframe=%t sha=%s",
				label, i, len(gv), gFP, gIsKey, hex.EncodeToString(gSHA[:8]))
			continue
		}
		diff := firstByteDiff(gv, lv)
		t.Fatalf("%s frame %d byte MISMATCH: govpx_len=%d libvpx_len=%d first_diff=%d govpx_sha=%s libvpx_sha=%s",
			label, i, len(gv), len(lv), diff,
			hex.EncodeToString(gSHA[:8]), hex.EncodeToString(lSHA[:8]))
	}
}

// readGoFuzzCorpusByteSeed parses a Go fuzz corpus file of the form
//
//	go test fuzz v1
//	[]byte("...")
//
// and returns the decoded byte slice. Only the single-arg []byte form is
// supported (every regression_* file in
// FuzzEncoderProductionStreamByteParity matches this shape).
func readGoFuzzCorpusByteSeed(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "[]byte(") || !strings.HasSuffix(ln, ")") {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(ln, "[]byte("), ")")
		unquoted, err := strconv.Unquote(inner)
		if err != nil {
			return nil, fmt.Errorf("unquote %q: %w", inner, err)
		}
		return []byte(unquoted), nil
	}
	return nil, fmt.Errorf("no []byte(...) line found in %s", path)
}
