//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9corpus"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"os"
	"path/filepath"
	"testing"
)

func TestVP9DecoderOfficialIVFTestDataMatchesLibvpx(t *testing.T) {
	root, ok := vp9corpus.IVFRoot(t)
	if !ok {
		return
	}
	vp9test.RequireVpxdec(t)
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
			got, err := vp9oracle.DecodeIVFVisibleI420(ivf)
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
	vp9test.RequireVpxdec(t)
	paths := vp9corpus.FindProfile0WebM(t, root)
	vp9corpus.RequireProfile0WebMFiles(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := vp9test.VpxdecWebMI420(t, webm)
			got, err := vp9oracle.DecodeWebMVisibleI420(webm)
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
	vp9test.RequireVpxdec(t)
	paths := vp9corpus.FindIVF(t, root, true)
	vp9corpus.RequireInvalidIVFFiles(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			vp9test.VpxdecRejectsI420(t, ivf)
			if err := vp9oracle.DecodeIVFExpectError(ivf); err == nil {
				t.Fatalf("Decode accepted invalid VP90 IVF that libvpx rejected")
			}
		})
	}
}
