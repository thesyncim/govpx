package govpx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestOracleExternalIVFTestDataMatchesLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run external libvpx conformance tests")
	}
	root, ok := externalIVFTestDataRoot(t, "set GOVPX_TEST_DATA_PATH to a VP8 IVF file or directory")
	if !ok {
		return
	}
	oracle := findChecksumOracle(t)
	paths := findVP8IVFTestData(t, root)
	if len(paths) == 0 {
		t.Fatalf("no VP8 IVF files found under %s", root)
	}
	assertExternalIVFTestDataMinimum(t, paths)

	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := runLibvpxChecksumOracleFile(t, oracle, path)
			got := decodeIVFChecksums(t, ivf)
			if len(got) != len(want) {
				t.Fatalf("frame count = %d, want %d from libvpx", len(got), len(want))
			}
			for i := range want {
				if !testutil.SameFrameChecksum(got[i], want[i]) {
					t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(want[i]), formatChecksum(got[i]))
				}
			}
		})
	}
}

func TestOracleExternalIVFTestDataDecodeIntoMatchesLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run external libvpx DecodeInto conformance tests")
	}
	root, ok := externalIVFTestDataRoot(t, "set GOVPX_TEST_DATA_PATH to a VP8 IVF file or directory")
	if !ok {
		return
	}
	oracle := findChecksumOracle(t)
	paths := findVP8IVFTestData(t, root)
	if len(paths) == 0 {
		t.Fatalf("no VP8 IVF files found under %s", root)
	}
	assertExternalIVFTestDataMinimum(t, paths)

	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := runLibvpxChecksumOracleFile(t, oracle, path)
			got := decodeIVFIntoChecksums(t, ivf)
			if len(got) != len(want) {
				t.Fatalf("DecodeInto frame count = %d, want %d from libvpx", len(got), len(want))
			}
			for i := range want {
				if !testutil.SameFrameChecksum(got[i], want[i]) {
					t.Fatalf("DecodeInto frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(want[i]), formatChecksum(got[i]))
				}
			}
		})
	}
}

func TestOracleExternalInvalidIVFTestDataRejectedLikeLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run external invalid libvpx conformance tests")
	}
	root, ok := externalInvalidIVFTestDataRoot(t)
	if !ok {
		return
	}
	oracle := findChecksumOracle(t)
	paths := findInvalidVP8IVFTestData(t, root)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_INVALID_TEST_DATA_REQUIRED") == "1" || externalInvalidIVFTestMinimum(t) > 0 {
			t.Fatalf("no invalid VP8 IVF files found under %s", root)
		}
		t.Skipf("no invalid VP8 IVF files found under %s", root)
	}
	assertExternalInvalidIVFTestDataMinimum(t, paths)

	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			if err := runLibvpxChecksumOracleFileExpectError(t, oracle, path); err == nil {
				t.Fatalf("libvpx oracle decoded invalid VP8 IVF without error")
			}
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			if err := decodeIVFExpectError(t, ivf, DecoderOptions{}); err == nil {
				t.Fatalf("Decode accepted invalid VP8 IVF that libvpx rejected")
			}
			if err := decodeIVFIntoExpectError(t, ivf); err == nil {
				t.Fatalf("DecodeInto accepted invalid VP8 IVF that libvpx rejected")
			}
		})
	}
}

func TestOracleGeneratedLibvpxCorpusMatchesLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run generated libvpx conformance tests")
	}
	oracle := findChecksumOracle(t)
	vpxenc := findVpxenc(t)
	dir := t.TempDir()

	cases := []generatedLibvpxCorpusCase{
		{name: "baseline", width: 32, height: 32, frames: 6, checkProfile: true, wantProfile: 0, checkTokenPartition: true, wantTokenPartition: vp8common.OnePartition},
		{name: "narrow", width: 48, height: 24, frames: 6},
		{name: "profile1", width: 32, height: 32, frames: 6, args: []string{"--profile=1"}, checkProfile: true, wantProfile: 1},
		{name: "narrow-profile2", width: 48, height: 24, frames: 6, args: []string{"--profile=2"}, checkProfile: true, wantProfile: 2},
		{name: "profile3", width: 32, height: 32, frames: 3, args: []string{"--profile=3"}, checkProfile: true, wantProfile: 3},
		{name: "token-two", width: 32, height: 32, frames: 6, args: []string{"--token-parts=1"}, checkTokenPartition: true, wantTokenPartition: vp8common.TwoPartition},
		{name: "token-four", width: 32, height: 32, frames: 6, args: []string{"--token-parts=2"}, checkTokenPartition: true, wantTokenPartition: vp8common.FourPartition},
		{name: "token-eight", width: 32, height: 32, frames: 6, args: []string{"--token-parts=3"}, checkTokenPartition: true, wantTokenPartition: vp8common.EightPartition},
		{name: "token-eight-tall", width: 32, height: 128, frames: 6, args: []string{"--token-parts=3"}, checkTokenPartition: true, wantTokenPartition: vp8common.EightPartition, checkAllTokenPartitionsActive: true},
		{name: "error-resilient", width: 32, height: 32, frames: 6, args: []string{"--error-resilient=1"}},
		{name: "cyclic-refresh-error-resilient", width: 80, height: 80, frames: 8, args: []string{"--error-resilient=1"}, checkSegmentationMap: true},
		{name: "sharpness7", width: 32, height: 32, frames: 6, args: []string{"--sharpness=7"}},
		{name: "static-threshold", width: 64, height: 64, frames: 8, args: []string{"--static-thresh=1000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ivfPath := generateLibvpxCorpusIVF(t, vpxenc, dir, tc)
			ivf, err := os.ReadFile(ivfPath)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			assertGeneratedLibvpxCorpusFeatures(t, ivf, tc)
			want := runLibvpxChecksumOracleFile(t, oracle, ivfPath)
			got := decodeIVFChecksums(t, ivf)
			gotInto := decodeIVFIntoChecksums(t, ivf)
			assertFrameChecksumsEqual(t, "Decode", got, want)
			assertFrameChecksumsEqual(t, "DecodeInto", gotInto, want)
		})
	}
}

func TestFindVP8IVFTestData(t *testing.T) {
	dir := t.TempDir()
	vp8Path := filepath.Join(dir, "vp8.ivf")
	if err := os.WriteFile(vp8Path, makeIVF(16, 16, 30, 1, [][]byte{{1}}), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	vp9Path := filepath.Join(dir, "vp9.ivf")
	vp9 := makeIVF(16, 16, 30, 1, [][]byte{{1}})
	copy(vp9[8:12], []byte("VP90"))
	if err := os.WriteFile(vp9Path, vp9, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("not ivf"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	paths := findVP8IVFTestData(t, dir)
	if len(paths) != 1 || paths[0] != vp8Path {
		t.Fatalf("paths = %v, want [%s]", paths, vp8Path)
	}
}

func TestExternalIVFTestMinimum(t *testing.T) {
	t.Setenv("GOVPX_TEST_DATA_MIN", "3")

	if got := externalIVFTestMinimum(t); got != 3 {
		t.Fatalf("minimum = %d, want 3", got)
	}
}
