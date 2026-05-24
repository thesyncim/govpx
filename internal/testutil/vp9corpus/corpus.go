package vp9corpus

import (
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func IVFRoot(t testing.TB) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_VP9_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if testutil.DefaultVP9TestDataExists() {
		return testutil.DefaultVP9ExternalTestDataDir, true
	}
	if testutil.EnvFlag("GOVPX_VP9_TEST_DATA_REQUIRED") {
		t.Fatalf("GOVPX_VP9_TEST_DATA_REQUIRED=1 but neither GOVPX_VP9_TEST_DATA_PATH nor %s is present", testutil.DefaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_TEST_DATA_PATH to official VP90 IVF data or run make fetch-vp9-test-data to populate %s", testutil.DefaultVP9ExternalTestDataDir)
	return "", false
}

func InvalidIVFRoot(t testing.TB) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_VP9_INVALID_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_VP9_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if testutil.DefaultVP9TestDataExists() {
		return testutil.DefaultVP9ExternalTestDataDir, true
	}
	if testutil.EnvFlag("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED") {
		t.Fatalf("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED=1 but neither GOVPX_VP9_INVALID_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", testutil.DefaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_INVALID_TEST_DATA_PATH to invalid official VP90 IVF data or run make fetch-vp9-test-data to populate %s", testutil.DefaultVP9ExternalTestDataDir)
	return "", false
}

func Profile0WebMRoot(t testing.TB) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_VP9_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if testutil.DefaultVP9TestDataExists() {
		return testutil.DefaultVP9ExternalTestDataDir, true
	}
	profile0Minimum, _ := minimumFromEnv(t, "GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN")
	if testutil.EnvFlag("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_REQUIRED") ||
		profile0Minimum > 0 {
		t.Fatalf("VP9 Profile 0 WebM test data is required but neither GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", testutil.DefaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_PATH to official VP9 Profile 0 WebM data or run make fetch-vp9-test-data to populate %s", testutil.DefaultVP9ExternalTestDataDir)
	return "", false
}

func ProfileWebMRoot(t testing.TB) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_VP9_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if testutil.DefaultVP9TestDataExists() {
		return testutil.DefaultVP9ExternalTestDataDir, true
	}
	profileMinimum, _ := minimumFromEnv(t, "GOVPX_VP9_PROFILE_TEST_DATA_MIN")
	if testutil.EnvFlag("GOVPX_VP9_PROFILE_TEST_DATA_REQUIRED") ||
		profileMinimum > 0 {
		t.Fatalf("VP9 profile WebM test data is required but neither GOVPX_VP9_PROFILE_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", testutil.DefaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_PROFILE_TEST_DATA_PATH to official VP9 profile WebM data or run make fetch-vp9-test-data to populate %s", testutil.DefaultVP9ExternalTestDataDir)
	return "", false
}

func FindIVF(t testing.TB, root string, invalid bool) []string {
	t.Helper()
	paths, err := testutil.FindVP9IVFTestData(root, ivfLimit(t, invalid), invalid)
	if err != nil {
		t.Fatalf("FindVP9IVFTestData(%q): %v", root, err)
	}
	return paths
}

func FindProfile0WebM(t testing.TB, root string) []string {
	t.Helper()
	paths, err := testutil.FindVP9Profile0WebMTestData(root, profile0WebMLimit(t))
	if err != nil {
		t.Fatalf("FindVP9Profile0WebMTestData(%q): %v", root, err)
	}
	return paths
}

func FindProfileWebM(t testing.TB, root string) []string {
	t.Helper()
	paths, err := testutil.FindVP9ProfileWebMTestData(root, profileWebMLimit(t))
	if err != nil {
		t.Fatalf("FindVP9ProfileWebMTestData(%q): %v", root, err)
	}
	return paths
}

func RequireInvalidIVFFiles(t testing.TB, root string, paths []string) {
	t.Helper()
	if len(paths) == 0 {
		if testutil.EnvFlag("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED") ||
			InvalidIVFMinimum(t, root) > 0 {
			t.Fatalf("no invalid VP90 IVF files found under %s", root)
		}
		t.Skipf("no invalid VP90 IVF files found under %s", root)
	}
	AssertInvalidIVFMinimum(t, root, paths)
}

func RequireProfile0WebMFiles(t testing.TB, root string, paths []string) {
	t.Helper()
	if len(paths) == 0 {
		if testutil.EnvFlag("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_REQUIRED") ||
			Profile0WebMMinimum(t, root) > 0 {
			t.Fatalf("no official VP9 Profile 0 WebM files found under %s", root)
		}
		t.Skipf("no official VP9 Profile 0 WebM files found under %s", root)
	}
	AssertProfile0WebMMinimum(t, root, paths)
}

func RequireProfileWebMFiles(t testing.TB, root string, paths []string) {
	t.Helper()
	if len(paths) == 0 {
		if testutil.EnvFlag("GOVPX_VP9_PROFILE_TEST_DATA_REQUIRED") ||
			ProfileWebMMinimum(t, root) > 0 {
			t.Fatalf("no official VP9 profile WebM files found under %s", root)
		}
		t.Skipf("no official VP9 profile WebM files found under %s", root)
	}
	AssertProfileWebMMinimum(t, root, paths)
}

func IVFMinimum(t testing.TB, root string) int {
	t.Helper()
	minimum, set := minimumFromEnv(t, "GOVPX_VP9_TEST_DATA_MIN")
	if set {
		return minimum
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0
	}
	return testutil.DefaultVP9IVFTestDataMinimum
}

func InvalidIVFMinimum(t testing.TB, root string) int {
	t.Helper()
	return corpusMinimum(t, root, "GOVPX_VP9_INVALID_TEST_DATA_MIN",
		testutil.DefaultVP9InvalidIVFTestDataMinimum)
}

func Profile0WebMMinimum(t testing.TB, root string) int {
	t.Helper()
	return corpusMinimum(t, root, "GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN",
		testutil.DefaultVP9Profile0WebMTestMinimum)
}

func ProfileWebMMinimum(t testing.TB, root string) int {
	t.Helper()
	return corpusMinimum(t, root, "GOVPX_VP9_PROFILE_TEST_DATA_MIN",
		testutil.DefaultVP9ProfileWebMTestMinimum)
}

func AssertIVFMinimum(t testing.TB, root string, paths []string) {
	t.Helper()
	minimum := IVFMinimum(t, root)
	if minimum > 0 && len(paths) < minimum {
		source := "default VP9 decoder corpus floor"
		if os.Getenv("GOVPX_VP9_TEST_DATA_MIN") != "" {
			source = "GOVPX_VP9_TEST_DATA_MIN"
		}
		t.Fatalf("VP90 IVF test data count = %d, want at least %d from %s", len(paths), minimum, source)
	}
}

func AssertInvalidIVFMinimum(t testing.TB, root string, paths []string) {
	t.Helper()
	minimum := InvalidIVFMinimum(t, root)
	if minimum > 0 && len(paths) < minimum {
		source := "default VP9 invalid decoder corpus floor"
		if os.Getenv("GOVPX_VP9_INVALID_TEST_DATA_MIN") != "" {
			source = "GOVPX_VP9_INVALID_TEST_DATA_MIN"
		}
		t.Fatalf("invalid VP90 IVF test data count = %d, want at least %d from %s", len(paths), minimum, source)
	}
}

func AssertProfile0WebMMinimum(t testing.TB, root string, paths []string) {
	t.Helper()
	minimum := Profile0WebMMinimum(t, root)
	if minimum > 0 && len(paths) < minimum {
		source := "default VP9 Profile 0 WebM corpus floor"
		if os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN") != "" {
			source = "GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN"
		}
		t.Fatalf("VP9 Profile 0 WebM test data count = %d, want at least %d from %s", len(paths), minimum, source)
	}
}

func AssertProfileWebMMinimum(t testing.TB, root string, paths []string) {
	t.Helper()
	minimum := ProfileWebMMinimum(t, root)
	if minimum > 0 && len(paths) < minimum {
		source := "default VP9 profile WebM corpus floor"
		if os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_MIN") != "" {
			source = "GOVPX_VP9_PROFILE_TEST_DATA_MIN"
		}
		t.Fatalf("VP9 profile WebM test data count = %d, want at least %d from %s", len(paths), minimum, source)
	}
}

func ivfLimit(t testing.TB, invalid bool) int {
	t.Helper()
	name := "GOVPX_VP9_TEST_DATA_LIMIT"
	if invalid {
		name = "GOVPX_VP9_INVALID_TEST_DATA_LIMIT"
	}
	return envInt(t, name)
}

func profile0WebMLimit(t testing.TB) int {
	t.Helper()
	return envInt(t, "GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_LIMIT")
}

func profileWebMLimit(t testing.TB) int {
	t.Helper()
	return envInt(t, "GOVPX_VP9_PROFILE_TEST_DATA_LIMIT")
}

func corpusMinimum(t testing.TB, root, envName string, defaultMinimum int) int {
	t.Helper()
	minimum, set := minimumFromEnv(t, envName)
	if set {
		return minimum
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0
	}
	return defaultMinimum
}

func minimumFromEnv(t testing.TB, name string) (int, bool) {
	t.Helper()
	value, set, err := testutil.NonNegativeEnvInt(name)
	if err != nil {
		t.Fatal(err)
	}
	return value, set
}

func envInt(t testing.TB, name string) int {
	t.Helper()
	value, _, err := testutil.NonNegativeEnvInt(name)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
