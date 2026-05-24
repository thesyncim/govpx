package vp8corpus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVP8CorpusRootsUseExplicitEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOVPX_TEST_DATA_PATH", dir)
	root, ok := IVFRoot(t)
	if !ok || root != dir {
		t.Fatalf("IVFRoot = %q, %t; want %q, true", root, ok, dir)
	}

	invalid := filepath.Join(dir, "invalid")
	if err := os.Mkdir(invalid, 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	t.Setenv("GOVPX_INVALID_TEST_DATA_PATH", invalid)
	root, ok = InvalidIVFRoot(t)
	if !ok || root != invalid {
		t.Fatalf("InvalidIVFRoot = %q, %t; want %q, true", root, ok, invalid)
	}
}

func TestVP8CorpusInvalidRootFallsBackToValidCorpus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOVPX_TEST_DATA_PATH", dir)
	root, ok := InvalidIVFRoot(t)
	if !ok || root != dir {
		t.Fatalf("InvalidIVFRoot fallback = %q, %t; want %q, true", root, ok, dir)
	}
}

func TestVP8CorpusFindIVFUsesLimitsAndInvalidPrefix(t *testing.T) {
	dir := t.TempDir()
	validA := filepath.Join(dir, "a.ivf")
	validB := filepath.Join(dir, "b.ivf")
	invalid := filepath.Join(dir, "invalid-a.ivf")
	for _, path := range []string{validA, validB, invalid} {
		if err := os.WriteFile(path, vp8IVFHeader(), 0o600); err != nil {
			t.Fatalf("WriteFile %s returned error: %v", path, err)
		}
	}

	t.Setenv("GOVPX_TEST_DATA_LIMIT", "1")
	paths := FindIVF(t, dir)
	if len(paths) != 1 || paths[0] != validA {
		t.Fatalf("FindIVF = %v, want [%s]", paths, validA)
	}

	paths = FindInvalidIVF(t, dir)
	if len(paths) != 1 || paths[0] != invalid {
		t.Fatalf("FindInvalidIVF = %v, want [%s]", paths, invalid)
	}
}

func TestVP8CorpusMinimumsUseEnvironment(t *testing.T) {
	t.Setenv("GOVPX_TEST_DATA_MIN", "3")
	t.Setenv("GOVPX_INVALID_TEST_DATA_MIN", "2")
	if got := IVFMinimum(t); got != 3 {
		t.Fatalf("IVFMinimum = %d, want 3", got)
	}
	if got := InvalidIVFMinimum(t); got != 2 {
		t.Fatalf("InvalidIVFMinimum = %d, want 2", got)
	}
}

func vp8IVFHeader() []byte {
	return testutil.WriteIVFHeader(testutil.IVFHeader{
		FourCC:              testutil.IVFFourCCVP8,
		Width:               16,
		Height:              16,
		TimebaseNumerator:   1,
		TimebaseDenominator: 30,
	})
}
