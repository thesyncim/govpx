package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestClassifyVP8OracleRuntimeControls(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty pick0 -> general", nil, "general"},
		{"fps_bitrate_repro exact match", []byte("02000y0"), "fps_bitrate_repro"},
		{"kfi_zero_repro exact match", []byte{0xff}, "kfi_zero_repro"},
		{"full_perm exact match", vp8OracleRuntimeFullPermutationSeed, "full_perm"},
		// data[0]%3 dispatch: 0->general, 1->temporal, 2->invalid_noop.
		// Avoid 0xff as the first byte (exact match for kfi_zero_repro
		// when len==1, and full_perm when the rest matches), so use
		// 0x30/0x31/0x32 plus a tail byte.
		{"data[0]%3==0 -> general", []byte{0x30, 0x00}, "general"},
		{"data[0]%3==1 -> temporal", []byte{0x31, 0x00}, "temporal"},
		{"data[0]%3==2 -> invalid_noop", []byte{0x32, 0x00}, "invalid_noop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyVP8OracleRuntimeControls(tc.in)
			if err != nil {
				t.Fatalf("classify err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("classify %#v: got %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClassifyVP8OracleProductionRuntimeControls(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{
			name: "empty seed",
			in:   nil,
			want: "prod_640x360_t0_f2_cpu0_src0_300kbps",
		},
		{
			name: "threaded 720p cap",
			in:   []byte{2, 3, 2, 1, 1, 2},
			want: "prod_1280x720_t4_f3_cpum3_src1_1200kbps",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyVP8OracleProductionRuntimeControls(tc.in)
			if err != nil {
				t.Fatalf("classify err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("classify %#v: got %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseSeed(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string
		want []byte
	}{
		{"plain ascii", "go test fuzz v1\n[]byte(\"hello\")\n", []byte("hello")},
		{"escaped byte", "go test fuzz v1\n[]byte(\"\\xff\")\n", []byte{0xff}},
		{"trailing nl absent", "go test fuzz v1\n[]byte(\"02000y0\")", []byte("02000y0")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := parseSeed(path)
			if err != nil {
				t.Fatalf("parseSeed: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("parseSeed: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseSeedRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad")
	if err := os.WriteFile(path, []byte("not a fuzz corpus file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSeed(path); err == nil {
		t.Fatal("expected error on malformed seed, got nil")
	}
}

// initRepo turns dir into a fresh git repo so we can exercise the
// tracked vs. untracked rename branches in renameOne.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-q", "-m", "root"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func gitFiles(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	sort.Strings(lines)
	return lines
}

func TestRunRenamesTrackedAndUntracked(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	corpus := filepath.Join(root, "testdata", "fuzz", "FuzzEncoderTwoPassByteParity")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(corpus, "0123456789abcdef")
	untracked := filepath.Join(corpus, "fedcba9876543210")
	already := filepath.Join(corpus, "regression_twopass_deadbeef")
	for _, p := range []string{tracked, untracked, already} {
		if err := os.WriteFile(p, []byte("go test fuzz v1\n[]byte(\"1\")\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Stage tracked + already; leave untracked out of the index.
	for _, p := range []string{tracked, already} {
		cmd := exec.Command("git", "add", p)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v: %s", err, out)
		}
	}
	cmd := exec.Command("git", "commit", "-q", "-m", "seed")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}

	// Now run the renamer against this fixture root.
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := run("."); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := gitFiles(t, root)
	want := []string{
		"testdata/fuzz/FuzzEncoderTwoPassByteParity/regression_twopass_01234567",
		"testdata/fuzz/FuzzEncoderTwoPassByteParity/regression_twopass_deadbeef",
		"testdata/fuzz/FuzzEncoderTwoPassByteParity/regression_twopass_fedcba98",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("git ls-files after rename:\n got %v\nwant %v", got, want)
	}

	// Idempotent second pass: no changes, no errors.
	if err := run("."); err != nil {
		t.Fatalf("run idempotent: %v", err)
	}
	got2 := gitFiles(t, root)
	if !reflect.DeepEqual(got2, want) {
		t.Fatalf("git ls-files after idempotent pass:\n got %v\nwant %v", got2, want)
	}
}

func TestRunFailsOnUnknownFuzzTarget(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	corpus := filepath.Join(root, "testdata", "fuzz", "FuzzMystery")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(corpus, "0123456789abcdef")
	if err := os.WriteFile(stray, []byte("go test fuzz v1\n[]byte(\"1\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := run("."); err == nil {
		t.Fatal("expected error for unknown fuzz target, got nil")
	}
}

// Sanity: a no-op run on a tree with only properly-named seeds returns
// nil and prints nothing alarming.
func TestRunOnCleanTreeIsNoop(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	corpus := filepath.Join(root, "testdata", "fuzz", "FuzzEncoderRandomStrides")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	clean := filepath.Join(corpus, "regression_strides_aabbccdd")
	if err := os.WriteFile(clean, []byte("go test fuzz v1\n[]byte(\"1\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := run("."); err != nil {
		t.Fatalf("run on clean tree: %v", err)
	}
}

// hint guards against silent drift between dispatcher and classifier.
func TestVP8OracleRuntimeFullPermutationSeedMirrorsTestFile(t *testing.T) {
	want := []byte{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	if !bytes.Equal(vp8OracleRuntimeFullPermutationSeed, want) {
		t.Fatalf("vp8OracleRuntimeFullPermutationSeed drifted: got %v want %v", vp8OracleRuntimeFullPermutationSeed, want)
	}
}
