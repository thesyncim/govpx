// Package vp8corpus owns VP8 external corpus discovery for tests.
package vp8corpus

import (
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func IVFRoot(t testing.TB) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if os.Getenv("GOVPX_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_TEST_DATA_REQUIRED=1 but GOVPX_TEST_DATA_PATH is not set")
	}
	t.Skip("set GOVPX_TEST_DATA_PATH to a VP8 IVF file or directory")
	return "", false
}

func InvalidIVFRoot(t testing.TB) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_INVALID_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if os.Getenv("GOVPX_INVALID_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_INVALID_TEST_DATA_REQUIRED=1 but neither GOVPX_INVALID_TEST_DATA_PATH nor GOVPX_TEST_DATA_PATH is set")
	}
	t.Skip("set GOVPX_INVALID_TEST_DATA_PATH to invalid VP8 IVF data or point GOVPX_TEST_DATA_PATH at a full libvpx test-data directory")
	return "", false
}

func FindIVF(t testing.TB, root string) []string {
	t.Helper()
	paths, err := testutil.FindVP8IVFTestData(root, ivfLimit(t), false)
	if err != nil {
		t.Fatalf("FindVP8IVFTestData(%q): %v", root, err)
	}
	return paths
}

func FindInvalidIVF(t testing.TB, root string) []string {
	t.Helper()
	paths, err := testutil.FindVP8IVFTestData(root, invalidIVFLimit(t), true)
	if err != nil {
		t.Fatalf("FindVP8IVFTestData(%q, invalid): %v", root, err)
	}
	return paths
}

func InvalidIVFRequired() bool {
	return os.Getenv("GOVPX_INVALID_TEST_DATA_REQUIRED") == "1"
}

func IVFMinimum(t testing.TB) int {
	t.Helper()
	return envInt(t, "GOVPX_TEST_DATA_MIN")
}

func InvalidIVFMinimum(t testing.TB) int {
	t.Helper()
	return envInt(t, "GOVPX_INVALID_TEST_DATA_MIN")
}

func AssertIVFMinimum(t testing.TB, paths []string) {
	t.Helper()
	minimum := IVFMinimum(t)
	if minimum > 0 && len(paths) < minimum {
		t.Fatalf("VP8 IVF test data count = %d, want at least %d from GOVPX_TEST_DATA_MIN", len(paths), minimum)
	}
}

func AssertInvalidIVFMinimum(t testing.TB, paths []string) {
	t.Helper()
	minimum := InvalidIVFMinimum(t)
	if minimum > 0 && len(paths) < minimum {
		t.Fatalf("invalid VP8 IVF test data count = %d, want at least %d from GOVPX_INVALID_TEST_DATA_MIN", len(paths), minimum)
	}
}

func ivfLimit(t testing.TB) int {
	t.Helper()
	return envInt(t, "GOVPX_TEST_DATA_LIMIT")
}

func invalidIVFLimit(t testing.TB) int {
	t.Helper()
	return envInt(t, "GOVPX_INVALID_TEST_DATA_LIMIT")
}

func envInt(t testing.TB, name string) int {
	t.Helper()
	value, _, err := testutil.NonNegativeEnvInt(name)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
