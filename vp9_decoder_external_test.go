package govpx

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

const (
	defaultVP9ExternalTestDataDir       = "internal/coracle/build/test-data/vp9"
	defaultVP9IVFTestDataMinimum        = 7
	defaultVP9InvalidIVFTestDataMinimum = 17
	defaultVP9Profile0WebMTestMinimum   = 101
	defaultVP9ProfileWebMTestMinimum    = 11
)

var defaultVP9Profile0WebMTestNames = map[string]struct{}{
	"vp90-2-01-sharpness-1.webm":                  {},
	"vp90-2-01-sharpness-2.webm":                  {},
	"vp90-2-01-sharpness-3.webm":                  {},
	"vp90-2-01-sharpness-4.webm":                  {},
	"vp90-2-01-sharpness-5.webm":                  {},
	"vp90-2-01-sharpness-6.webm":                  {},
	"vp90-2-01-sharpness-7.webm":                  {},
	"vp90-2-02-size-08x08.webm":                   {},
	"vp90-2-02-size-08x10.webm":                   {},
	"vp90-2-02-size-10x08.webm":                   {},
	"vp90-2-02-size-16x16.webm":                   {},
	"vp90-2-02-size-16x18.webm":                   {},
	"vp90-2-02-size-18x16.webm":                   {},
	"vp90-2-02-size-32x32.webm":                   {},
	"vp90-2-02-size-32x34.webm":                   {},
	"vp90-2-02-size-34x32.webm":                   {},
	"vp90-2-02-size-64x64.webm":                   {},
	"vp90-2-02-size-64x66.webm":                   {},
	"vp90-2-02-size-66x64.webm":                   {},
	"vp90-2-02-size-130x132.webm":                 {},
	"vp90-2-02-size-132x130.webm":                 {},
	"vp90-2-02-size-180x180.webm":                 {},
	"vp90-2-03-deltaq.webm":                       {},
	"vp90-2-06-bilinear.webm":                     {},
	"vp90-2-07-frame_parallel.webm":               {},
	"vp90-2-08-tile_1x4.webm":                     {},
	"vp90-2-08-tile_1x8.webm":                     {},
	"vp90-2-08-tile_1x2_frame_parallel.webm":      {},
	"vp90-2-09-aq2.webm":                          {},
	"vp90-2-09-lf_deltas.webm":                    {},
	"vp90-2-10-show-existing-frame.webm":          {},
	"vp90-2-11-size-351x287.webm":                 {},
	"vp90-2-14-resize-10frames-fp-tiles-1-2.webm": {},
	"vp90-2-14-resize-10frames-fp-tiles-1-4.webm": {},
	"vp90-2-15-segkey.webm":                       {},
	"vp90-2-16-intra-only.webm":                   {},
	"vp90-2-19-skip.webm":                         {},
}

func init() {
	for q := 0; q < 64; q++ {
		defaultVP9Profile0WebMTestNames[fmt.Sprintf("vp90-2-00-quantizer-%02d.webm", q)] = struct{}{}
	}
}

func TestVP9DecoderDefaultProfile0WebMCorpusMinimumMatchesList(t *testing.T) {
	if got := len(defaultVP9Profile0WebMTestNames); got != defaultVP9Profile0WebMTestMinimum {
		t.Fatalf("default VP9 Profile 0 WebM corpus list = %d, minimum = %d",
			got, defaultVP9Profile0WebMTestMinimum)
	}
}

func TestVP9DecoderOfficialIVFTestDataMatchesLibvpx(t *testing.T) {
	root, ok := externalVP9IVFTestDataRoot(t)
	if !ok {
		return
	}
	requireVP9VpxdecOracle(t)
	paths := findVP9IVFTestData(t, root, false)
	if len(paths) == 0 {
		t.Fatalf("no VP90 IVF files found under %s", root)
	}
	assertExternalVP9IVFTestDataMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
			if err != nil {
				t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
			}
			got, err := decodeVP9IVFVisibleI420(ivf)
			if (errors.Is(err, ErrVP9NotImplemented) || errors.Is(err, ErrInvalidVP9Data)) &&
				os.Getenv("GOVPX_VP9_TEST_DATA_STRICT") != "1" {
				t.Skipf("%s is a valid official VP90 IVF stream but needs unsupported VP9 decoder features", filepath.Base(path))
			}
			if err != nil {
				t.Fatalf("Decode VP90 IVF returned error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for official VP90 IVF %s\nlibvpx=%s\ngovpx=%s",
					filepath.Base(path),
					testutil.MD5Hex(md5.Sum(want)),
					testutil.MD5Hex(md5.Sum(got)))
			}
		})
	}
}

func TestVP9DecoderOfficialProfile0WebMTestDataMatchesLibvpx(t *testing.T) {
	root, ok := externalVP9Profile0WebMTestDataRoot(t)
	if !ok {
		return
	}
	requireVP9VpxdecOracle(t)
	paths := findVP9Profile0WebMTestData(t, root)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_REQUIRED") == "1" ||
			externalVP9Profile0WebMTestMinimum(t, root) > 0 {
			t.Fatalf("no official VP9 Profile 0 WebM files found under %s", root)
		}
		t.Skipf("no official VP9 Profile 0 WebM files found under %s", root)
	}
	assertExternalVP9Profile0WebMTestDataMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want, diag, err := coracle.VpxdecVP9DecodeWebMI420(webm)
			if err != nil {
				t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
			}
			got, err := decodeVP9WebMVisibleI420(webm)
			if err != nil {
				t.Fatalf("Decode VP9 Profile 0 WebM returned error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for official VP9 Profile 0 WebM %s\nlibvpx=%s\ngovpx=%s",
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
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			packets, err := extractVP9WebMPackets(webm)
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

func TestVP9DecoderOfficialInvalidIVFTestDataRejectedLikeLibvpx(t *testing.T) {
	root, ok := externalVP9InvalidIVFTestDataRoot(t)
	if !ok {
		return
	}
	requireVP9VpxdecOracle(t)
	paths := findVP9IVFTestData(t, root, true)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED") == "1" ||
			externalVP9InvalidIVFTestMinimum(t, root) > 0 {
			t.Fatalf("no invalid VP90 IVF files found under %s", root)
		}
		t.Skipf("no invalid VP90 IVF files found under %s", root)
	}
	assertExternalVP9InvalidIVFTestDataMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			if _, _, err := coracle.VpxdecVP9DecodeI420(ivf); err == nil {
				t.Fatalf("libvpx vpxdec accepted invalid VP90 IVF")
			}
			if err := decodeVP9IVFExpectErrorForTest(ivf); err == nil {
				t.Fatalf("Decode accepted invalid VP90 IVF that libvpx rejected")
			}
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
		return defaultVP9ExternalTestDataDir, true
	}
	if os.Getenv("GOVPX_VP9_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_VP9_TEST_DATA_REQUIRED=1 but neither GOVPX_VP9_TEST_DATA_PATH nor %s is present", defaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_TEST_DATA_PATH to official VP90 IVF data or run make fetch-vp9-test-data to populate %s", defaultVP9ExternalTestDataDir)
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
		return defaultVP9ExternalTestDataDir, true
	}
	if os.Getenv("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED=1 but neither GOVPX_VP9_INVALID_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", defaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_INVALID_TEST_DATA_PATH to invalid official VP90 IVF data or run make fetch-vp9-test-data to populate %s", defaultVP9ExternalTestDataDir)
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
		return defaultVP9ExternalTestDataDir, true
	}
	profile0Minimum, _ := externalVP9IVFMinimumFromEnv(t,
		"GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN")
	if os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_REQUIRED") == "1" ||
		profile0Minimum > 0 {
		t.Fatalf("VP9 Profile 0 WebM test data is required but neither GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", defaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_PATH to official VP9 Profile 0 WebM data or run make fetch-vp9-test-data to populate %s", defaultVP9ExternalTestDataDir)
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
		return defaultVP9ExternalTestDataDir, true
	}
	profileMinimum, _ := externalVP9IVFMinimumFromEnv(t, "GOVPX_VP9_PROFILE_TEST_DATA_MIN")
	if os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_REQUIRED") == "1" ||
		profileMinimum > 0 {
		t.Fatalf("VP9 profile WebM test data is required but neither GOVPX_VP9_PROFILE_TEST_DATA_PATH, GOVPX_VP9_TEST_DATA_PATH, nor %s is present", defaultVP9ExternalTestDataDir)
	}
	t.Skipf("set GOVPX_VP9_PROFILE_TEST_DATA_PATH to official VP9 profile WebM data or run make fetch-vp9-test-data to populate %s", defaultVP9ExternalTestDataDir)
	return "", false
}

func externalVP9DefaultTestDataExists() bool {
	info, err := os.Stat(defaultVP9ExternalTestDataDir)
	return err == nil && info.IsDir()
}

func findVP9IVFTestData(t *testing.T, root string, invalid bool) []string {
	t.Helper()
	limit := externalVP9IVFTestLimit(t, invalid)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	accept := func(path string) bool {
		if isInvalidVP9IVFTestDataName(path) != invalid {
			return false
		}
		if invalid {
			return true
		}
		return isVP9IVFTestData(t, path)
	}
	if info.Mode().IsRegular() {
		if accept(root) {
			paths = append(paths, root)
		}
		return paths
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a regular file or directory", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ivf") {
			return nil
		}
		if accept(path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func findVP9Profile0WebMTestData(t *testing.T, root string) []string {
	t.Helper()
	limit := externalVP9Profile0WebMTestLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	accept := func(path string) bool {
		name := filepath.Base(path)
		if !strings.EqualFold(filepath.Ext(name), ".webm") {
			return false
		}
		_, ok := defaultVP9Profile0WebMTestNames[name]
		return ok
	}
	var paths []string
	if info.Mode().IsRegular() {
		if accept(root) {
			paths = append(paths, root)
		}
		return paths
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a regular file or directory", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !accept(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func findVP9ProfileWebMTestData(t *testing.T, root string) []string {
	t.Helper()
	limit := externalVP9ProfileWebMTestLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	accept := func(path string) bool {
		name := strings.ToLower(filepath.Base(path))
		return strings.EqualFold(filepath.Ext(path), ".webm") &&
			(strings.HasPrefix(name, "vp91-") ||
				strings.HasPrefix(name, "vp92-") ||
				strings.HasPrefix(name, "vp93-"))
	}
	if info.Mode().IsRegular() {
		if accept(root) {
			paths = append(paths, root)
		}
		return paths
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a regular file or directory", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !accept(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func isInvalidVP9IVFTestDataName(path string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(path)), "invalid-")
}

func externalVP9IVFTestLimit(t *testing.T, invalid bool) int {
	t.Helper()
	name := "GOVPX_VP9_TEST_DATA_LIMIT"
	if invalid {
		name = "GOVPX_VP9_INVALID_TEST_DATA_LIMIT"
	}
	raw := os.Getenv(name)
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		t.Fatalf("%s = %q, want a non-negative integer", name, raw)
	}
	return limit
}

func externalVP9Profile0WebMTestLimit(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_LIMIT")
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		t.Fatalf("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_LIMIT = %q, want a non-negative integer", raw)
	}
	return limit
}

func externalVP9ProfileWebMTestLimit(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_LIMIT")
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		t.Fatalf("GOVPX_VP9_PROFILE_TEST_DATA_LIMIT = %q, want a non-negative integer", raw)
	}
	return limit
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
	return defaultVP9IVFTestDataMinimum
}

func externalVP9InvalidIVFTestMinimum(t *testing.T, root string) int {
	t.Helper()
	return externalVP9CorpusMinimum(t, root, "GOVPX_VP9_INVALID_TEST_DATA_MIN",
		defaultVP9InvalidIVFTestDataMinimum)
}

func externalVP9Profile0WebMTestMinimum(t *testing.T, root string) int {
	t.Helper()
	return externalVP9CorpusMinimum(t, root, "GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN",
		defaultVP9Profile0WebMTestMinimum)
}

func externalVP9ProfileWebMTestMinimum(t *testing.T, root string) int {
	t.Helper()
	return externalVP9CorpusMinimum(t, root, "GOVPX_VP9_PROFILE_TEST_DATA_MIN",
		defaultVP9ProfileWebMTestMinimum)
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
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false
	}
	minimum, err := strconv.Atoi(raw)
	if err != nil || minimum < 0 {
		t.Fatalf("%s = %q, want a non-negative integer", name, raw)
	}
	return minimum, true
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

func isVP9IVFTestData(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open %s returned error: %v", path, err)
	}
	defer file.Close()
	header := make([]byte, testutil.IVFFileHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			t.Fatalf("%s is not valid IVF data: %v", path, testutil.ErrInvalidIVF)
		}
		t.Fatalf("ReadFull %s returned error: %v", path, err)
	}
	if !vp9ExternalIVFHeaderLooksValid(header) {
		return false
	}
	return true
}

func decodeVP9IVFVisibleI420ForTest(t *testing.T, ivf []byte) []byte {
	t.Helper()
	out, err := decodeVP9IVFVisibleI420(ivf)
	if err != nil {
		t.Fatalf("decodeVP9IVFVisibleI420 returned error: %v", err)
	}
	return out
}

func decodeVP9IVFVisibleI420(ivf []byte) ([]byte, error) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		return nil, err
	}
	if !vp9ExternalIVFHeaderLooksValid(ivf) {
		return nil, testutil.ErrInvalidIVF
	}
	offset := testutil.IVFFileHeaderSize
	var out []byte
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
	packets, err := extractVP9WebMPackets(webm)
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
	if !vp9ExternalIVFHeaderLooksValid(ivf) {
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

func vp9ExternalIVFHeaderLooksValid(data []byte) bool {
	return len(data) >= testutil.IVFFileHeaderSize &&
		data[0] == 'D' && data[1] == 'K' && data[2] == 'I' && data[3] == 'F' &&
		data[6] == byte(testutil.IVFFileHeaderSize) && data[7] == 0 &&
		data[8] == 'V' && data[9] == 'P' && data[10] == '9' && data[11] == '0'
}

const (
	webmIDSegment     = 0x18538067
	webmIDTracks      = 0x1654AE6B
	webmIDTrackEntry  = 0xAE
	webmIDTrackNumber = 0xD7
	webmIDTrackType   = 0x83
	webmIDCodecID     = 0x86
	webmIDCluster     = 0x1F43B675
	webmIDSimpleBlock = 0xA3
	webmIDBlockGroup  = 0xA0
	webmIDBlock       = 0xA1
)

type webmElement struct {
	id        uint64
	dataStart int
	dataEnd   int
}

type webmTrackEntry struct {
	number uint64
	video  bool
	codec  string
}

func extractVP9WebMPackets(data []byte) ([][]byte, error) {
	tracks := make(map[uint64]bool)
	if err := walkWebMElements(data, 0, len(data), func(elem webmElement) error {
		if elem.id != webmIDTrackEntry {
			return nil
		}
		track, err := parseWebMTrackEntry(data[elem.dataStart:elem.dataEnd])
		if err != nil {
			return err
		}
		if track.number != 0 && track.video && track.codec == "V_VP9" {
			tracks[track.number] = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, errors.New("no V_VP9 video track found")
	}

	var packets [][]byte
	if err := walkWebMElements(data, 0, len(data), func(elem webmElement) error {
		if elem.id != webmIDSimpleBlock && elem.id != webmIDBlock {
			return nil
		}
		track, frames, err := parseWebMBlock(data[elem.dataStart:elem.dataEnd])
		if err != nil {
			return err
		}
		if !tracks[track] {
			return nil
		}
		packets = append(packets, frames...)
		return nil
	}); err != nil {
		return nil, err
	}
	return packets, nil
}

func walkWebMElements(data []byte, start, end int, visit func(webmElement) error) error {
	for pos := start; pos < end; {
		elem, next, err := readWebMElement(data, pos, end)
		if err != nil {
			return err
		}
		if err := visit(elem); err != nil {
			return err
		}
		if isWebMMasterElement(elem.id) {
			if err := walkWebMElements(data, elem.dataStart, elem.dataEnd, visit); err != nil {
				return err
			}
		}
		pos = next
	}
	return nil
}

func isWebMMasterElement(id uint64) bool {
	switch id {
	case webmIDSegment, webmIDTracks, webmIDTrackEntry, webmIDCluster, webmIDBlockGroup:
		return true
	default:
		return false
	}
}

func readWebMElement(data []byte, pos, limit int) (webmElement, int, error) {
	id, idLen, err := readWebMID(data, pos, limit)
	if err != nil {
		return webmElement{}, 0, err
	}
	size, sizeLen, unknown, err := readWebMSize(data, pos+idLen, limit)
	if err != nil {
		return webmElement{}, 0, err
	}
	dataStart := pos + idLen + sizeLen
	dataEnd := limit
	if !unknown {
		if size > uint64(limit-dataStart) {
			return webmElement{}, 0, fmt.Errorf("WebM element 0x%x size exceeds input", id)
		}
		dataEnd = dataStart + int(size)
	}
	return webmElement{id: id, dataStart: dataStart, dataEnd: dataEnd}, dataEnd, nil
}

func readWebMID(data []byte, pos, limit int) (uint64, int, error) {
	if pos >= limit {
		return 0, 0, io.ErrUnexpectedEOF
	}
	first := data[pos]
	mask := byte(0x80)
	length := 1
	for length <= 4 && first&mask == 0 {
		mask >>= 1
		length++
	}
	if length > 4 || pos+length > limit {
		return 0, 0, errors.New("invalid WebM element id")
	}
	var id uint64
	for i := 0; i < length; i++ {
		id = (id << 8) | uint64(data[pos+i])
	}
	return id, length, nil
}

func readWebMSize(data []byte, pos, limit int) (uint64, int, bool, error) {
	value, length, err := readWebMVint(data, pos, limit)
	if err != nil {
		return 0, 0, false, err
	}
	unknown := value == (uint64(1)<<(7*length))-1
	return value, length, unknown, nil
}

func readWebMVint(data []byte, pos, limit int) (uint64, int, error) {
	if pos >= limit {
		return 0, 0, io.ErrUnexpectedEOF
	}
	first := data[pos]
	mask := byte(0x80)
	length := 1
	for length <= 8 && first&mask == 0 {
		mask >>= 1
		length++
	}
	if length > 8 || pos+length > limit {
		return 0, 0, errors.New("invalid WebM vint")
	}
	value := uint64(first & ^mask)
	for i := 1; i < length; i++ {
		value = (value << 8) | uint64(data[pos+i])
	}
	return value, length, nil
}

func readWebMSignedVint(data []byte, pos, limit int) (int64, int, error) {
	value, length, err := readWebMVint(data, pos, limit)
	if err != nil {
		return 0, 0, err
	}
	bias := (int64(1) << (7*length - 1)) - 1
	return int64(value) - bias, length, nil
}

func parseWebMTrackEntry(data []byte) (webmTrackEntry, error) {
	var track webmTrackEntry
	if err := walkWebMTrackEntryFields(data, func(elem webmElement) error {
		switch elem.id {
		case webmIDTrackNumber:
			track.number = readWebMUnsigned(data[elem.dataStart:elem.dataEnd])
		case webmIDTrackType:
			track.video = readWebMUnsigned(data[elem.dataStart:elem.dataEnd]) == 1
		case webmIDCodecID:
			track.codec = string(data[elem.dataStart:elem.dataEnd])
		}
		return nil
	}); err != nil {
		return webmTrackEntry{}, err
	}
	return track, nil
}

func walkWebMTrackEntryFields(data []byte, visit func(webmElement) error) error {
	for pos := 0; pos < len(data); {
		elem, next, err := readWebMElement(data, pos, len(data))
		if err != nil {
			return err
		}
		if err := visit(elem); err != nil {
			return err
		}
		pos = next
	}
	return nil
}

func readWebMUnsigned(data []byte) uint64 {
	var value uint64
	for _, b := range data {
		value = (value << 8) | uint64(b)
	}
	return value
}

func parseWebMBlock(data []byte) (uint64, [][]byte, error) {
	track, n, err := readWebMVint(data, 0, len(data))
	if err != nil {
		return 0, nil, err
	}
	if n+3 > len(data) {
		return 0, nil, io.ErrUnexpectedEOF
	}
	flags := data[n+2]
	frames, err := splitWebMBlockFrames(data[n+3:], int((flags&0x06)>>1))
	if err != nil {
		return 0, nil, err
	}
	return track, frames, nil
}

func splitWebMBlockFrames(data []byte, lacing int) ([][]byte, error) {
	switch lacing {
	case 0:
		return [][]byte{data}, nil
	case 1:
		return splitWebMXiphLacedFrames(data)
	case 2:
		return splitWebMFixedLacedFrames(data)
	case 3:
		return splitWebMEBMLLacedFrames(data)
	default:
		return nil, errors.New("invalid WebM lacing mode")
	}
}

func splitWebMXiphLacedFrames(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	frameCount := int(data[0]) + 1
	pos := 1
	sizes := make([]int, frameCount)
	for i := 0; i < frameCount-1; i++ {
		for {
			if pos >= len(data) {
				return nil, io.ErrUnexpectedEOF
			}
			b := int(data[pos])
			pos++
			sizes[i] += b
			if b != 255 {
				break
			}
		}
	}
	return sliceWebMLacedFrames(data[pos:], sizes)
}

func splitWebMFixedLacedFrames(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	frameCount := int(data[0]) + 1
	if (len(data)-1)%frameCount != 0 {
		return nil, errors.New("invalid fixed-size WebM lacing")
	}
	size := (len(data) - 1) / frameCount
	sizes := make([]int, frameCount)
	for i := range sizes {
		sizes[i] = size
	}
	return sliceWebMLacedFrames(data[1:], sizes)
}

func splitWebMEBMLLacedFrames(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	frameCount := int(data[0]) + 1
	pos := 1
	sizes := make([]int, frameCount)
	first, n, err := readWebMVint(data, pos, len(data))
	if err != nil {
		return nil, err
	}
	pos += n
	sizes[0] = int(first)
	prev := int64(first)
	for i := 1; i < frameCount-1; i++ {
		delta, n, err := readWebMSignedVint(data, pos, len(data))
		if err != nil {
			return nil, err
		}
		pos += n
		prev += delta
		if prev < 0 {
			return nil, errors.New("invalid negative WebM lace size")
		}
		sizes[i] = int(prev)
	}
	return sliceWebMLacedFrames(data[pos:], sizes)
}

func sliceWebMLacedFrames(data []byte, sizes []int) ([][]byte, error) {
	if len(sizes) == 0 {
		return nil, errors.New("missing WebM lace sizes")
	}
	total := 0
	for _, size := range sizes[:len(sizes)-1] {
		if size < 0 || size > len(data)-total {
			return nil, errors.New("invalid WebM lace size")
		}
		total += size
	}
	sizes[len(sizes)-1] = len(data) - total
	frames := make([][]byte, len(sizes))
	pos := 0
	for i, size := range sizes {
		if size < 0 || pos+size > len(data) {
			return nil, errors.New("invalid WebM lace frame bounds")
		}
		frames[i] = data[pos : pos+size]
		pos += size
	}
	return frames, nil
}
