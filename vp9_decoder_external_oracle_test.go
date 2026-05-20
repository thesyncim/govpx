//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/md5"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVP9DecoderOfficialIVFTestDataMatchesLibvpx(t *testing.T) {
	root, ok := externalVP9IVFTestDataRoot(t)
	if !ok {
		return
	}
	coracletest.VpxdecVP9(t)
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
			want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
			if err != nil {
				t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
			}
			got, err := decodeVP9IVFVisibleI420(ivf)
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
	coracletest.VpxdecVP9(t)
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
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
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

func TestVP9DecoderOfficialInvalidIVFTestDataRejectedLikeLibvpx(t *testing.T) {
	root, ok := externalVP9InvalidIVFTestDataRoot(t)
	if !ok {
		return
	}
	coracletest.VpxdecVP9(t)
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
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
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
