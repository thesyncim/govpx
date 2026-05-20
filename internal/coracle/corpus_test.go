package coracle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestFindVP8IVFTestDataFiltersByFourCCAndInvalidPrefix(t *testing.T) {
	dir := t.TempDir()
	vp8Path := filepath.Join(dir, "vp8.ivf")
	if err := os.WriteFile(vp8Path, testIVF(testutil.IVFFourCCVP8), 0o600); err != nil {
		t.Fatalf("WriteFile VP8 returned error: %v", err)
	}
	vp9Path := filepath.Join(dir, "vp9.ivf")
	if err := os.WriteFile(vp9Path, testIVF(testutil.IVFFourCCVP9), 0o600); err != nil {
		t.Fatalf("WriteFile VP9 returned error: %v", err)
	}
	invalidPath := filepath.Join(dir, "invalid-vp8.ivf")
	if err := os.WriteFile(invalidPath, testIVF(testutil.IVFFourCCVP8), 0o600); err != nil {
		t.Fatalf("WriteFile invalid VP8 returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("not ivf"), 0o600); err != nil {
		t.Fatalf("WriteFile note returned error: %v", err)
	}

	paths, err := FindVP8IVFTestData(dir, 0, false)
	if err != nil {
		t.Fatalf("FindVP8IVFTestData valid returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != vp8Path {
		t.Fatalf("valid paths = %v, want [%s]", paths, vp8Path)
	}
	paths, err = FindVP8IVFTestData(dir, 0, true)
	if err != nil {
		t.Fatalf("FindVP8IVFTestData invalid returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != invalidPath {
		t.Fatalf("invalid paths = %v, want [%s]", paths, invalidPath)
	}
}

func TestFindVP9CorpusTestData(t *testing.T) {
	dir := t.TempDir()
	vp9Path := filepath.Join(dir, "vp90-2-05-resize.ivf")
	if err := os.WriteFile(vp9Path, testIVF(testutil.IVFFourCCVP9), 0o600); err != nil {
		t.Fatalf("WriteFile VP9 returned error: %v", err)
	}
	vp8Path := filepath.Join(dir, "vp8.ivf")
	if err := os.WriteFile(vp8Path, testIVF(testutil.IVFFourCCVP8), 0o600); err != nil {
		t.Fatalf("WriteFile VP8 returned error: %v", err)
	}
	webmPath := filepath.Join(dir, "vp90-2-01-sharpness-1.webm")
	if err := os.WriteFile(webmPath, []byte{0x1a, 0x45, 0xdf, 0xa3}, 0o600); err != nil {
		t.Fatalf("WriteFile WebM returned error: %v", err)
	}
	profilePath := filepath.Join(dir, "vp91-profile.webm")
	if err := os.WriteFile(profilePath, []byte{0x1a, 0x45, 0xdf, 0xa3}, 0o600); err != nil {
		t.Fatalf("WriteFile profile WebM returned error: %v", err)
	}

	paths, err := FindVP9IVFTestData(dir, 0, false)
	if err != nil {
		t.Fatalf("FindVP9IVFTestData returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != vp9Path {
		t.Fatalf("VP9 IVF paths = %v, want [%s]", paths, vp9Path)
	}
	paths, err = FindVP9Profile0WebMTestData(dir, 0)
	if err != nil {
		t.Fatalf("FindVP9Profile0WebMTestData returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != webmPath {
		t.Fatalf("Profile 0 WebM paths = %v, want [%s]", paths, webmPath)
	}
	paths, err = FindVP9ProfileWebMTestData(dir, 0)
	if err != nil {
		t.Fatalf("FindVP9ProfileWebMTestData returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != profilePath {
		t.Fatalf("profile WebM paths = %v, want [%s]", paths, profilePath)
	}
}

func TestNonNegativeEnvInt(t *testing.T) {
	t.Setenv("GOVPX_TEST_LIMIT_FOR_CORACLE", "3")
	value, set, err := NonNegativeEnvInt("GOVPX_TEST_LIMIT_FOR_CORACLE")
	if err != nil {
		t.Fatalf("NonNegativeEnvInt returned error: %v", err)
	}
	if !set || value != 3 {
		t.Fatalf("NonNegativeEnvInt = %d,%v, want 3,true", value, set)
	}
	t.Setenv("GOVPX_TEST_LIMIT_FOR_CORACLE", "-1")
	if _, _, err := NonNegativeEnvInt("GOVPX_TEST_LIMIT_FOR_CORACLE"); err == nil {
		t.Fatalf("NonNegativeEnvInt accepted a negative value")
	}
}

func TestSafeCorpusTestName(t *testing.T) {
	root := filepath.Join("corpus", "root")
	path := filepath.Join(root, "nested", "vp90-2-05-resize.ivf")
	if got := SafeCorpusTestName(root, path); got != "nested_vp90-2-05-resize" {
		t.Fatalf("SafeCorpusTestName = %q, want nested_vp90-2-05-resize", got)
	}
}

func testIVF(fourCC [4]byte) []byte {
	return testutil.BuildIVF(testutil.IVFHeader{
		FourCC:              fourCC,
		Width:               16,
		Height:              16,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
	}, [][]byte{{1}})
}
