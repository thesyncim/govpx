//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9corpus"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9DecoderOfficialIVFTestDataMatchesLibvpx(t *testing.T) {
	root, ok := vp9corpus.IVFRoot(t)
	if !ok {
		return
	}
	coracletest.VpxdecVP9(t)
	paths := vp9corpus.FindIVF(t, root, false)
	if len(paths) == 0 {
		t.Fatalf("no VP90 IVF files found under %s", root)
	}
	vp9corpus.AssertIVFMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := vp9test.VpxdecI420(t, ivf)
			got, err := decodeVP9IVFVisibleI420(ivf)
			if err != nil {
				t.Fatalf("Decode VP90 IVF returned error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for official VP90 IVF %s\nlibvpx=%s\ngovpx=%s",
					filepath.Base(path),
					vp9test.MD5Hex(want),
					vp9test.MD5Hex(got))
			}
		})
	}
}

func TestVP9DecoderOfficialProfile0WebMTestDataMatchesLibvpx(t *testing.T) {
	root, ok := vp9corpus.Profile0WebMRoot(t)
	if !ok {
		return
	}
	coracletest.VpxdecVP9(t)
	paths := vp9corpus.FindProfile0WebM(t, root)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_REQUIRED") == "1" ||
			vp9corpus.Profile0WebMMinimum(t, root) > 0 {
			t.Fatalf("no official VP9 Profile 0 WebM files found under %s", root)
		}
		t.Skipf("no official VP9 Profile 0 WebM files found under %s", root)
	}
	vp9corpus.AssertProfile0WebMMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := vp9test.VpxdecWebMI420(t, webm)
			got, err := decodeVP9WebMVisibleI420(webm)
			if err != nil {
				t.Fatalf("Decode VP9 Profile 0 WebM returned error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for official VP9 Profile 0 WebM %s\nlibvpx=%s\ngovpx=%s",
					filepath.Base(path),
					vp9test.MD5Hex(want),
					vp9test.MD5Hex(got))
			}
		})
	}
}

func TestVP9DecoderOfficialInvalidIVFTestDataRejectedLikeLibvpx(t *testing.T) {
	root, ok := vp9corpus.InvalidIVFRoot(t)
	if !ok {
		return
	}
	coracletest.VpxdecVP9(t)
	paths := vp9corpus.FindIVF(t, root, true)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_VP9_INVALID_TEST_DATA_REQUIRED") == "1" ||
			vp9corpus.InvalidIVFMinimum(t, root) > 0 {
			t.Fatalf("no invalid VP90 IVF files found under %s", root)
		}
		t.Skipf("no invalid VP90 IVF files found under %s", root)
	}
	vp9corpus.AssertInvalidIVFMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			vp9test.VpxdecRejectsI420(t, ivf)
			if err := decodeVP9IVFExpectErrorForTest(ivf); err == nil {
				t.Fatalf("Decode accepted invalid VP90 IVF that libvpx rejected")
			}
		})
	}
}
