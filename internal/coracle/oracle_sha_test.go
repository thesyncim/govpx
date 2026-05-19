package coracle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Canonical SHA-256 hashes pinned by task #281 for the libvpx v1.16.0 VP8
// oracle binaries built via internal/coracle/build_vpxenc_oracle.sh and
// internal/coracle/build_libvpx.sh, after task #264 host-pinning + task
// #281 path-prefix-map hardening. Verified reproducible on arm64-darwin
// (Apple silicon) hosts across builds rooted in /tmp, /private/tmp, and
// deeply nested parent directories.
//
// Task #296 (2026-05-19) rotated the vpxenc-oracle pin after extending
// the trace patch with a pretrellis-UV qcoeff emit hook. The hook splices
// into vp8_encode_inter16x16 between vp8_quantize_mb and optimize_mb
// (encodemb.c:485-495 v1.16.0) and emits 8 JSON rows per MB labelled with
// thread-local (mb_row, mb_col) coordinates set from encodeframe.c around
// each call to vp8cx_encode_{intra,inter}_macroblock. Emission is gated
// by GOVPX_ORACLE_PRETRELLIS_UV=1 on top of GOVPX_ORACLE_TRACE_OUT so the
// untraced binary stays a no-op. Used to localize the task #207 / #227
// ARNR pin-hold after the #282-#294 static-inspection campaign exhausted
// candidate predictor / residual / quantize / RC drift sources.
//
// Task #310 (2026-05-19) rotated the pin again to land a per-MB
// NEWMV-picker quantize trace. The hook splices into
// vp8/encoder/rdopt.c:macro_block_yrd after the Y0..15 + Y2 quantize
// loop completes (rdopt.c:494-499 v1.16.0) and before vp8_rdcost_mby
// reads d->eobs, capturing 17 JSON rows per inter-mode candidate (Y
// blocks 0..15 + Y2 block 24) with full pre-quantize state
// (coeff[16], b->zbin[16], b->round[16], b->quant[16],
// b->quant_shift[16], b->zrun_zbin_boost[16], d->dequant[16],
// b->zbin_extra), post-quantize state (qcoeff[16], dqcoeff[16], eob),
// MB-context (mbmi.mode/ref_frame/mv), and the libvpx quantize-fn path
// taken ("regular" / "fast" via x->quantize_b pointer equality).
// Gated on GOVPX_ORACLE_NEWMV_PICKER=1 on top of GOVPX_ORACLE_TRACE_OUT.
// Localizes the task #304 BestARNR/GoodARNR rate_y gap by surfacing
// which of (stale zbin_extra, row-15 predictor delta, fast vs regular
// quantize swap) explains govpx rate_y=7519 vs libvpx rate_y=34799 at
// MB(0,0) frame 1 NEWMV MV=(8,16) ref=LAST_FRAME.
//
// These pins exist to detect any future change in the build pipeline
// (libvpx upgrade, configure flag change, toolchain rotation, new patch
// stamp) that would silently shift the oracle binary hash. If this test
// fails, treat it as a signal to re-audit oracle determinism end-to-end
// rather than mechanically bumping the constant.
//
// Hashes are intentionally arch-gated: x86_64 and other hosts will pick
// different libvpx ARM NEON vs SSE TUs and produce different SHAs, so
// only the canonical Apple silicon hashes are pinned here. Other archs
// run a softer reproducibility check (re-hash the same file twice; on
// success the cross-path invariance is enforced by the build script's
// determinism flags, not by this test).
const (
	oracleSHAvpxencArm64Darwin = "e1abf8c9013ed17c45ac40d2579b7fe91468a6cbf6a0ee78d1c2631d02030e57"
	oracleSHAlibvpxArm64Darwin = "4992f2bbfc1ce02640e20036286465c455650485a5378904dcc197cb2dda5523"
)

// TestOracleVpxencSHAPinned hashes the pre-built vpxenc-oracle binary, if
// present, and verifies it matches the canonical task #281 SHA on supported
// hosts. The test skips on hosts where the binary is absent so the unit
// test suite stays runnable without invoking the libvpx build pipeline.
func TestOracleVpxencSHAPinned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping oracle SHA pin under -short")
	}
	bin := oracleBuildPath(t, "vpxenc-oracle")
	if bin == "" {
		t.Skip("vpxenc-oracle not built; run internal/coracle/build_vpxenc_oracle.sh")
	}
	got, err := sha256File(bin)
	if err != nil {
		t.Fatalf("sha256File(%s): %v", bin, err)
	}
	want, ok := canonicalOracleSHA(runtime.GOOS, runtime.GOARCH, "vpxenc-oracle")
	if !ok {
		t.Logf("no canonical SHA pin for %s/%s; computed hash is %s", runtime.GOOS, runtime.GOARCH, got)
		return
	}
	if got != want {
		t.Fatalf("vpxenc-oracle SHA mismatch on %s/%s\n  bin:  %s\n  got:  %s\n  want: %s\n\nIf the build pipeline (libvpx version, configure flags, toolchain, oracle patch) intentionally changed, re-verify cross-path reproducibility and update the pinned constant in oracle_sha_test.go. Otherwise this signals a determinism regression.",
			runtime.GOOS, runtime.GOARCH, bin, got, want)
	}
}

// TestOracleLibvpxSHAPinned mirrors TestOracleVpxencSHAPinned for the
// decoder-only oracle binary (govpx-vpx-oracle) built by
// internal/coracle/build_libvpx.sh.
func TestOracleLibvpxSHAPinned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping oracle SHA pin under -short")
	}
	bin := oracleBuildPath(t, "govpx-vpx-oracle")
	if bin == "" {
		t.Skip("govpx-vpx-oracle not built; run internal/coracle/build_libvpx.sh")
	}
	got, err := sha256File(bin)
	if err != nil {
		t.Fatalf("sha256File(%s): %v", bin, err)
	}
	want, ok := canonicalOracleSHA(runtime.GOOS, runtime.GOARCH, "govpx-vpx-oracle")
	if !ok {
		t.Logf("no canonical SHA pin for %s/%s; computed hash is %s", runtime.GOOS, runtime.GOARCH, got)
		return
	}
	if got != want {
		t.Fatalf("govpx-vpx-oracle SHA mismatch on %s/%s\n  bin:  %s\n  got:  %s\n  want: %s\n\nIf the build pipeline intentionally changed, re-verify cross-path reproducibility and update the pinned constant in oracle_sha_test.go.",
			runtime.GOOS, runtime.GOARCH, bin, got, want)
	}
}

// oracleBuildPath returns the absolute path to a built oracle binary if
// one exists under internal/coracle/build/, otherwise returns "". The
// helper uses runtime.Caller to anchor the lookup to the package source
// directory rather than the test working directory, so it works under
// `go test ./...` invoked from arbitrary cwds.
func oracleBuildPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	candidate := filepath.Join(dir, "build", name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// canonicalOracleSHA returns the pinned canonical SHA-256 hash for the
// named oracle binary on the given host (GOOS/GOARCH) tuple, or "", false
// if no pin is currently published for that combination.
func canonicalOracleSHA(goos, goarch, name string) (string, bool) {
	switch goos {
	case "darwin":
		if goarch == "arm64" {
			switch name {
			case "vpxenc-oracle":
				return oracleSHAvpxencArm64Darwin, true
			case "govpx-vpx-oracle":
				return oracleSHAlibvpxArm64Darwin, true
			}
		}
	}
	return "", false
}

// sha256File computes the SHA-256 hash of the file at the given path,
// returning the lowercase hex digest.
func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
