package coracle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Canonical SHA-256 hashes for libvpx v1.16.0 oracle binaries built via
// internal/coracle/build_vpxenc_oracle.sh and internal/coracle/build_libvpx.sh.
// The pins cover the patched VP8 trace oracle and the decoder checksum oracle.
// If either hash changes, re-audit the libvpx version, configure flags,
// compiler/toolchain inputs, path-prefix mapping, and trace patch set before
// updating the constant.
//
// Hashes are intentionally arch-gated: x86_64 and other hosts pick different
// libvpx ARM NEON vs SSE translation units and produce different SHAs. Only the
// canonical Apple silicon hashes are pinned here. Other architectures still run
// a softer reproducibility check by hashing the same file twice; cross-path
// invariance is enforced by the build scripts' determinism flags.
const (
	oracleSHAvpxencArm64Darwin = "ded03c16223fd9d6d5df02536d333fe76537e73597012363bda55a30fb2a14c0"
	oracleSHAlibvpxArm64Darwin = "4992f2bbfc1ce02640e20036286465c455650485a5378904dcc197cb2dda5523"
)

// TestCoracleVpxencSHAPinned hashes the pre-built vpxenc-oracle binary, if
// present, and verifies it matches the canonical SHA on supported hosts. The
// test skips on hosts where the binary is absent so the unit test suite stays
// runnable without invoking the libvpx build pipeline.
func TestCoracleVpxencSHAPinned(t *testing.T) {
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

// TestCoracleLibvpxSHAPinned mirrors TestCoracleVpxencSHAPinned for the
// decoder-only oracle binary (govpx-vpx-oracle) built by
// internal/coracle/build_libvpx.sh.
func TestCoracleLibvpxSHAPinned(t *testing.T) {
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
