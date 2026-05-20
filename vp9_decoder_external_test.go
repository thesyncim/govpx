package govpx

import (
	"bytes"
	"crypto/md5"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVP9DecoderDefaultProfile0WebMCorpusMinimumMatchesList(t *testing.T) {
	if got := testutil.DefaultVP9Profile0WebMTestNameCount(); got != testutil.DefaultVP9Profile0WebMTestMinimum {
		t.Fatalf("default VP9 Profile 0 WebM corpus list = %d, minimum = %d",
			got, testutil.DefaultVP9Profile0WebMTestMinimum)
	}
}

func TestVP9DecoderDefaultIVFCorpusMinimumMatchesList(t *testing.T) {
	if got := testutil.DefaultVP9IVFTestNameCount(); got != testutil.DefaultVP9IVFTestDataMinimum {
		t.Fatalf("default VP90 IVF corpus list = %d, minimum = %d",
			got, testutil.DefaultVP9IVFTestDataMinimum)
	}
}

func TestVP9DecoderOfficialIVFTestDataThreadedMatchesSerial(t *testing.T) {
	root, ok := externalVP9IVFTestDataRoot(t)
	if !ok {
		return
	}
	paths := findVP9IVFTestData(t, root, false)
	if len(paths) == 0 {
		t.Fatalf("no VP90 IVF files found under %s", root)
	}
	assertExternalVP9IVFTestDataMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want, err := decodeVP9IVFVisibleI420WithOptions(ivf,
				VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("serial Decode VP90 IVF returned error: %v", err)
			}
			got, err := decodeVP9IVFVisibleI420WithOptions(ivf,
				VP9DecoderOptions{Threads: 3})
			if err != nil {
				t.Fatalf("threaded Decode VP90 IVF returned error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("threaded VP90 IVF I420 mismatch for %s\nserial=%s\nthreaded=%s",
					filepath.Base(path),
					testutil.MD5Hex(md5.Sum(want)),
					testutil.MD5Hex(md5.Sum(got)))
			}
		})
	}
}

func TestVP9DecoderOfficialProfileWebMTestDataReturnsUnsupported(t *testing.T) {
	root, ok := externalVP9ProfileWebMTestDataRoot(t)
	if !ok {
		return
	}
	paths := findVP9ProfileWebMTestData(t, root)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_REQUIRED") == "1" ||
			externalVP9ProfileWebMTestMinimum(t, root) > 0 {
			t.Fatalf("no official VP9 profile WebM files found under %s", root)
		}
		t.Skipf("no official VP9 profile WebM files found under %s", root)
	}
	assertExternalVP9ProfileWebMTestDataMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			packets, err := testutil.ExtractVP9WebMPackets(webm)
			if err != nil {
				t.Fatalf("extract VP9 WebM packets returned error: %v", err)
			}
			if len(packets) == 0 {
				t.Fatalf("official VP9 profile WebM contained no VP9 packets")
			}

			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder returned error: %v", err)
			}
			for i, packet := range packets {
				err := d.Decode(packet)
				if errors.Is(err, ErrVP9NotImplemented) {
					return
				}
				if err != nil {
					t.Fatalf("Decode official unsupported-profile WebM packet %d returned %v, want ErrVP9NotImplemented", i, err)
				}
				if img, ok := d.NextFrame(); ok {
					t.Fatalf("Decode official unsupported-profile WebM packet %d produced %dx%d I420 output", i, img.Width, img.Height)
				}
			}
			t.Fatalf("Decode accepted %d official unsupported-profile VP9 WebM packets without ErrVP9NotImplemented", len(packets))
		})
	}
}

func externalVP9IVFTestDataRoot(t *testing.T) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_VP9_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if externalVP9DefaultTestDataExists() {
		return testutil.DefaultVP9ExternalTestDataDir, true
	}
	if os.Getenv("GOVPX_VP9_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_VP9_TEST_DATA_REQUIRED=1 but neither GOVPX_VP9_TEST_DATA_PATH nor %s is present", testutil.DefaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_TEST_DATA_PATH to official VP90 IVF data or run make fetch-vp9-test-data to populate %s", testutil.DefaultVP9ExternalTestDataDir)
	return "", false
}

func externalVP9InvalidIVFTestDataRoot(t *testing.T) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_VP9_INVALID_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_VP9_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if externalVP9DefaultTestDataExists() {
		return testutil.DefaultVP9ExternalTestDataDir, true
	}
	if os.Getenv("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED=1 but neither GOVPX_VP9_INVALID_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", testutil.DefaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_INVALID_TEST_DATA_PATH to invalid official VP90 IVF data or run make fetch-vp9-test-data to populate %s", testutil.DefaultVP9ExternalTestDataDir)
	return "", false
}

func externalVP9Profile0WebMTestDataRoot(t *testing.T) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_VP9_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if externalVP9DefaultTestDataExists() {
		return testutil.DefaultVP9ExternalTestDataDir, true
	}
	profile0Minimum, _ := externalVP9IVFMinimumFromEnv(t,
		"GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN")
	if os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_REQUIRED") == "1" ||
		profile0Minimum > 0 {
		t.Fatalf("VP9 Profile 0 WebM test data is required but neither GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", testutil.DefaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_PATH to official VP9 Profile 0 WebM data or run make fetch-vp9-test-data to populate %s", testutil.DefaultVP9ExternalTestDataDir)
	return "", false
}

func externalVP9ProfileWebMTestDataRoot(t *testing.T) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_VP9_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if externalVP9DefaultTestDataExists() {
		return testutil.DefaultVP9ExternalTestDataDir, true
	}
	profileMinimum, _ := externalVP9IVFMinimumFromEnv(t, "GOVPX_VP9_PROFILE_TEST_DATA_MIN")
	if os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_REQUIRED") == "1" ||
		profileMinimum > 0 {
		t.Fatalf("VP9 profile WebM test data is required but neither GOVPX_VP9_PROFILE_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", testutil.DefaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_PROFILE_TEST_DATA_PATH to official VP9 profile WebM data or run make fetch-vp9-test-data to populate %s", testutil.DefaultVP9ExternalTestDataDir)
	return "", false
}

func externalVP9DefaultTestDataExists() bool {
	return testutil.DefaultVP9TestDataExists()
}

func findVP9IVFTestData(t *testing.T, root string, invalid bool) []string {
	t.Helper()
	paths, err := testutil.FindVP9IVFTestData(root, externalVP9IVFTestLimit(t, invalid), invalid)
	if err != nil {
		t.Fatalf("FindVP9IVFTestData(%q): %v", root, err)
	}
	return paths
}

func findVP9Profile0WebMTestData(t *testing.T, root string) []string {
	t.Helper()
	paths, err := testutil.FindVP9Profile0WebMTestData(root, externalVP9Profile0WebMTestLimit(t))
	if err != nil {
		t.Fatalf("FindVP9Profile0WebMTestData(%q): %v", root, err)
	}
	return paths
}

func findVP9ProfileWebMTestData(t *testing.T, root string) []string {
	t.Helper()
	paths, err := testutil.FindVP9ProfileWebMTestData(root, externalVP9ProfileWebMTestLimit(t))
	if err != nil {
		t.Fatalf("FindVP9ProfileWebMTestData(%q): %v", root, err)
	}
	return paths
}

func externalVP9IVFTestLimit(t *testing.T, invalid bool) int {
	t.Helper()
	name := "GOVPX_VP9_TEST_DATA_LIMIT"
	if invalid {
		name = "GOVPX_VP9_INVALID_TEST_DATA_LIMIT"
	}
	return mustCoracleEnvInt(t, name)
}

func externalVP9Profile0WebMTestLimit(t *testing.T) int {
	t.Helper()
	return mustCoracleEnvInt(t, "GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_LIMIT")
}

func externalVP9ProfileWebMTestLimit(t *testing.T) int {
	t.Helper()
	return mustCoracleEnvInt(t, "GOVPX_VP9_PROFILE_TEST_DATA_LIMIT")
}

func externalVP9IVFTestMinimum(t *testing.T, root string) int {
	t.Helper()
	minimum, set := externalVP9IVFMinimumFromEnv(t, "GOVPX_VP9_TEST_DATA_MIN")
	if set {
		return minimum
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0
	}
	return testutil.DefaultVP9IVFTestDataMinimum
}

func externalVP9InvalidIVFTestMinimum(t *testing.T, root string) int {
	t.Helper()
	return externalVP9CorpusMinimum(t, root, "GOVPX_VP9_INVALID_TEST_DATA_MIN",
		testutil.DefaultVP9InvalidIVFTestDataMinimum)
}

func externalVP9Profile0WebMTestMinimum(t *testing.T, root string) int {
	t.Helper()
	return externalVP9CorpusMinimum(t, root, "GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN",
		testutil.DefaultVP9Profile0WebMTestMinimum)
}

func externalVP9ProfileWebMTestMinimum(t *testing.T, root string) int {
	t.Helper()
	return externalVP9CorpusMinimum(t, root, "GOVPX_VP9_PROFILE_TEST_DATA_MIN",
		testutil.DefaultVP9ProfileWebMTestMinimum)
}

func externalVP9CorpusMinimum(t *testing.T, root, envName string, defaultMinimum int) int {
	t.Helper()
	minimum, set := externalVP9IVFMinimumFromEnv(t, envName)
	if set {
		return minimum
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0
	}
	return defaultMinimum
}

func externalVP9IVFMinimumFromEnv(t *testing.T, name string) (int, bool) {
	t.Helper()
	value, set, err := testutil.NonNegativeEnvInt(name)
	if err != nil {
		t.Fatal(err)
	}
	return value, set
}

func mustCoracleEnvInt(t *testing.T, name string) int {
	t.Helper()
	value, _, err := testutil.NonNegativeEnvInt(name)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertExternalVP9IVFTestDataMinimum(t *testing.T, root string, paths []string) {
	t.Helper()
	minimum := externalVP9IVFTestMinimum(t, root)
	if minimum > 0 && len(paths) < minimum {
		source := "default VP9 decoder corpus floor"
		if os.Getenv("GOVPX_VP9_TEST_DATA_MIN") != "" {
			source = "GOVPX_VP9_TEST_DATA_MIN"
		}
		t.Fatalf("VP90 IVF test data count = %d, want at least %d from %s", len(paths), minimum, source)
	}
}

func assertExternalVP9InvalidIVFTestDataMinimum(t *testing.T, root string, paths []string) {
	t.Helper()
	minimum := externalVP9InvalidIVFTestMinimum(t, root)
	if minimum > 0 && len(paths) < minimum {
		source := "default VP9 invalid decoder corpus floor"
		if os.Getenv("GOVPX_VP9_INVALID_TEST_DATA_MIN") != "" {
			source = "GOVPX_VP9_INVALID_TEST_DATA_MIN"
		}
		t.Fatalf("invalid VP90 IVF test data count = %d, want at least %d from %s", len(paths), minimum, source)
	}
}

func assertExternalVP9Profile0WebMTestDataMinimum(t *testing.T, root string, paths []string) {
	t.Helper()
	minimum := externalVP9Profile0WebMTestMinimum(t, root)
	if minimum > 0 && len(paths) < minimum {
		source := "default VP9 Profile 0 WebM corpus floor"
		if os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN") != "" {
			source = "GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN"
		}
		t.Fatalf("VP9 Profile 0 WebM test data count = %d, want at least %d from %s", len(paths), minimum, source)
	}
}

func assertExternalVP9ProfileWebMTestDataMinimum(t *testing.T, root string, paths []string) {
	t.Helper()
	minimum := externalVP9ProfileWebMTestMinimum(t, root)
	if minimum > 0 && len(paths) < minimum {
		source := "default VP9 profile WebM corpus floor"
		if os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_MIN") != "" {
			source = "GOVPX_VP9_PROFILE_TEST_DATA_MIN"
		}
		t.Fatalf("VP9 profile WebM test data count = %d, want at least %d from %s", len(paths), minimum, source)
	}
}

func decodeVP9IVFVisibleI420(ivf []byte) ([]byte, error) {
	return decodeVP9IVFVisibleI420WithOptions(ivf, VP9DecoderOptions{})
}

func decodeVP9IVFVisibleI420WithOptions(ivf []byte, opts VP9DecoderOptions) (out []byte, err error) {
	d, err := NewVP9Decoder(opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := d.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if !testutil.VP9IVFHeaderLooksValid(ivf) {
		return nil, testutil.ErrInvalidIVF
	}
	offset := testutil.IVFFileHeaderSize
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return nil, err
		}
		if err := d.Decode(frame.Data); err != nil {
			return nil, err
		}
		if img, ok := d.NextFrame(); ok {
			out = appendVP9I420(out, img)
		}
		offset = next
	}
	return out, nil
}

func decodeVP9WebMVisibleI420(webm []byte) ([]byte, error) {
	packets, err := testutil.ExtractVP9WebMPackets(webm)
	if err != nil {
		return nil, err
	}
	if len(packets) == 0 {
		return nil, ErrInvalidVP9Data
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, packet := range packets {
		if err := d.Decode(packet); err != nil {
			return nil, err
		}
		if img, ok := d.NextFrame(); ok {
			out = appendVP9I420(out, img)
		}
	}
	return out, nil
}

func decodeVP9IVFExpectErrorForTest(ivf []byte) error {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		return err
	}
	if !testutil.VP9IVFHeaderLooksValid(ivf) {
		return testutil.ErrInvalidIVF
	}
	offset := testutil.IVFFileHeaderSize
	var firstErr error
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return err
		}
		if err := d.Decode(frame.Data); err != nil {
			firstErr = err
			break
		}
		offset = next
	}
	return firstErr
}
