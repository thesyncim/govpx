//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// TestVP8RealtimeCorpusMatchesLibvpxBytes is the realtime corpus byte-parity gate.
//
// The realtime parity cleanup closed every known byte-divergent F1 fuzz seed
// that the FuzzEncoderProductionStreamByteParity option-grid harness had
// captured into the corpus, plus the 1280×720 / 854×480 / 640×360
// inter-divergence seeds that lived alongside as standalone regression files.
// This single test is the permanent byte-parity gate for that state: it iterates
// every corpus seed file in
// testdata/fuzz/FuzzEncoderProductionStreamByteParity and runs each one
// against the libvpx-vpxenc-oracle binary, asserting strict per-frame
// bytes.Equal across the seed's full encoded clip.
//
// It also re-runs the byte-exact cohorts that were promoted into the test
// after closure. These configurations are byte-exact end-to-end today and stay
// here as named subtests so a single invocation proves the corpus parity state
// without keeping duplicate standalone files.
//
// If any subtest fails, the test reports the seed/config label, frame
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
//     vp8cx_mb_init_quantizer call honored by govpx.
//   - libvpx v1.16.0 vp8/encoder/onyx_if.c:3779 — cyclic_background_refresh
//     flipping segmentation_enabled on every CBR keyframe.
//   - testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_* —
//     the live corpus this test re-enumerates each run.
func TestVP8RealtimeCorpusMatchesLibvpxBytes(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the corpus byte-parity test")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

	t.Run("corpus", func(t *testing.T) {
		runVP8RealtimeCorpusByteParitySubtests(t, vpxencOracle)
	})
	t.Run("closed-configs", func(t *testing.T) {
		runVP8RealtimeClosedConfigSubtests(t, vpxencOracle)
	})
}

// runVP8RealtimeCorpusByteParitySubtests enumerates every regression_* file in the
// FuzzEncoderProductionStreamByteParity corpus, decodes each seed via
// newOptionGridFuzzCase (the same dispatch the live fuzz target uses) and
// asserts every produced frame is byte-identical to the libvpx oracle.
func runVP8RealtimeCorpusByteParitySubtests(t *testing.T, vpxencOracle string) {
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
	t.Logf("realtime corpus parity test: %d regression corpus seeds enumerated", len(names))

	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			seedPath := filepath.Join(corpusDir, name)
			seed, err := testutil.ReadGoFuzzCorpusByteSeed(seedPath)
			if err != nil {
				t.Fatalf("parse seed %s: %v", seedPath, err)
			}
			cfg := newOptionGridFuzzCase(seed)
			opts, libvpxArgs := cfg.buildOpts()
			sources := cfg.buildSources()

			sum := sha256.Sum256(seed)
			label := "realtime-corpus-" + cfg.name + "-" + hex.EncodeToString(sum[:4])
			t.Logf("%s w=%d h=%d deadline=%v cpu=%d threads=%d rc=%v sharp=%d tune=%v sc=%d er=%t token=%d arnr=%d/%d/%d frames=%d",
				label, opts.Width, opts.Height, opts.Deadline, opts.CpuUsed,
				opts.Threads, opts.RateControlMode, opts.Sharpness, opts.Tuning,
				opts.ScreenContentMode, opts.ErrorResilient, opts.TokenPartitions,
				opts.ARNRMaxFrames, opts.ARNRStrength, opts.ARNRType, len(sources))

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			// Route through the libvpx threading non-determinism
			// quarantine wrapper. Some seeds in this test
			// decode to threads>=2 cohorts; the wrapper is a no-op
			// for serial seeds and a re-run SHA check for parallel
			// seeds.
			libvpxFrames := encodeVP8FramesWithLibvpxOracleReproducible(t, vpxencOracle, label, opts, cfg.targetKbps, sources, libvpxArgs, VP8OracleReproducibleRuns)

			assertVP8RealtimeStrictByteParity(t, name, govpxFrames, libvpxFrames)
		})
	}
}

// runVP8RealtimeClosedConfigSubtests re-runs the byte-exact closed
// configurations. Only compact cohorts live here; longer investigative tests
// with detailed failure history stay in their own files so this test
// remains a readable parity gate.
func runVP8RealtimeClosedConfigSubtests(t *testing.T, vpxencOracle string) {
	type parityCase struct {
		name      string
		opts      EncoderOptions
		extraArgs []string
		frames    int
		targetKbp int
	}
	cases := []parityCase{
		{
			// Companion live regression:
			//   regression_option_grid_94eb71d5 (seed bytes "A1200000")
			// 1280x720 / GoodQuality / cpu=0 / CBR / TuneSSIM / arnr=1/0/1.
			// Retained here as a compact corpus byte-parity subtest.
			name: "ssim-1280x720-cbr-cpu0",
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
			// / TuneSSIM / CBR / arnr=1/2/1.
			name: "ssim-good-cbr-arnr-1280x720-threads4",
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
			// Pinned by TestVP8SSIMActivityMapRecodeBestQualityParity.
			name: "ssim-activity-map-recode-128x128-best-cpu4",
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
			// 6 frames. Pinned by TestVP8SSIMActivityMapRecodeGoodQualityParity.
			name: "ssim-activity-map-recode-160x96-good-cpu0",
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
			label := "realtime-parity-" + tc.name
			govpxFrames := encodeFramesWithGovpx(t, tc.opts, sources)
			// Re-run wrapper catches any libvpx-side threading
			// nondeterminism that would otherwise contaminate the
			// strict byte comparison.
			libvpxFrames := encodeVP8FramesWithLibvpxOracleReproducible(t, vpxencOracle, label, tc.opts, tc.targetKbp, sources, tc.extraArgs, VP8OracleReproducibleRuns)
			assertVP8RealtimeStrictByteParity(t, tc.name, govpxFrames, libvpxFrames)
		})
	}
}

// assertVP8RealtimeStrictByteParity is the strict-byte-equality assertion
// used by every corpus byte-parity subtest. Frame count, per-frame length,
// per-frame bytes and first-byte-diff offset are all reported on failure.
func assertVP8RealtimeStrictByteParity(t *testing.T, label string, govpxFrames, libvpxFrames [][]byte) {
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
		diff := testutil.FirstByteDiff(gv, lv)
		t.Fatalf("%s frame %d byte MISMATCH: govpx_len=%d libvpx_len=%d first_diff=%d govpx_sha=%s libvpx_sha=%s",
			label, i, len(gv), len(lv), diff,
			hex.EncodeToString(gSHA[:8]), hex.EncodeToString(lSHA[:8]))
	}
}
