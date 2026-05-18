// govpx-fuzz-rename renames Go-fuzz-discovered seeds under
// testdata/fuzz/<FuzzName>/ from their default 16-hex SHA filename to a
// descriptive regression_<case>_<hash8> form. Idempotent: files that
// already start with "regression_" are left alone. Invoked from the
// Makefile via `make fuzz-rename`.
//
// The tool reads the seed body (Go fuzz corpus format:
//
//	go test fuzz v1
//	[]byte("…")
//
// ), classifies it per parent fuzz target, then `git mv`s the file to its
// new name. New corpora can be plumbed in via the dispatch table below.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// hashName matches Go's default fuzz seed filename (16 lower-hex chars).
var hashName = regexp.MustCompile(`^[0-9a-f]{16}$`)

// byteLiteral captures the second line of a v1 fuzz corpus seed,
// e.g. `[]byte("foo\xff")`. The Go-quoted body is recovered via
// strconv.Unquote.
var byteLiteral = regexp.MustCompile(`(?m)^\s*\[\]byte\(("(?:[^"\\]|\\.)*")\)\s*$`)

// oracleRuntimeFullPermutationSeed mirrors the dispatcher constant in
// oracle_encoder_runtime_controls_fuzz_test.go. Keep in sync.
var oracleRuntimeFullPermutationSeed = []byte{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

// classifier turns a raw seed body into the case-name suffix used in
// regression_<case>_<hash8>. Returning ("", err) aborts the run.
type classifier func(data []byte) (string, error)

// dispatch maps a fuzz target directory name to its classifier. Adding a
// new fuzz corpus is a one-line change: register the classifier here.
var dispatch = map[string]classifier{
	"FuzzOracleEncoderRuntimeControlTransitions": classifyOracleRuntimeControls,
	"FuzzEncoderRandomStrides":                   constantCase("strides"),
	"FuzzEncoderReferenceControlSequences":       constantCase("refctrl"),
	"FuzzEncoderTwoPassByteParity":               constantCase("twopass"),
	"FuzzEncoderLongFixtureRateControl":          classifyLongFixtureRateControl,
	"FuzzEncoderProductionStreamByteParity":      constantCase("option_grid"),
	"FuzzDecoderAgainstLibvpx":                   classifyDecoderAgainstLibvpx,
	// VP9 fuzz family — sibling targets registered here mirror their VP8
	// counterparts. Classifiers reuse the same body shape so the resulting
	// regression filenames carry the same scoreboard semantics.
	"FuzzRTPVP9RoundTrip":                      constantCase("vp9_rtp"),
	"FuzzVP9OracleEncoderOptions":              constantCase("vp9_options"),
	"FuzzVP9OracleEncoderRuntimeControls":      constantCase("vp9_runtime_controls"),
	"FuzzVP9EncoderProductionStreamByteParity": constantCase("vp9_option_grid"),
	"FuzzVP9EncoderTwoPassByteParity":          constantCase("vp9_twopass"),
	"FuzzVP9EncoderLongFixtureRateControl":     classifyLongFixtureRateControl,
	"FuzzVP9DecoderAgainstLibvpx":              classifyDecoderAgainstLibvpx,
	"FuzzVP9EncoderRandomStrides":              constantCase("vp9_strides"),
	"FuzzVP9EncoderReferenceControlSequences":  constantCase("vp9_refctrl"),
	"FuzzEncoderFrameFlags":                    constantCase("frame_flags"),
	"FuzzDecoderThreaded":                      constantCase("threaded"),
	"FuzzDecoderErrorConcealment":              constantCase("error_concealment"),
	"FuzzExtendBorders":                        constantCase("extend_borders"),
}

func constantCase(name string) classifier {
	return func(_ []byte) (string, error) { return name, nil }
}

// classifyOracleRuntimeControls mirrors
// oracleRuntimeControlFuzzCaseFromBytes verbatim: exact-match repros
// first, then data[0]%3 picks general/temporal/invalid_noop.
func classifyOracleRuntimeControls(data []byte) (string, error) {
	if string(data) == "02000y0" {
		return "fps_bitrate_repro", nil
	}
	if string(data) == "\xff" {
		return "kfi_zero_repro", nil
	}
	if bytesEqual(data, oracleRuntimeFullPermutationSeed) {
		return "full_perm", nil
	}
	if len(data) == 0 {
		// pick(3) on an empty stream returns 0 -> general.
		return "general", nil
	}
	switch int(data[0]) % 3 {
	case 1:
		return "temporal", nil
	case 2:
		return "invalid_noop", nil
	default:
		return "general", nil
	}
}

// classifyLongFixtureRateControl mirrors newLongFixtureFuzzCase in
// oracle_encoder_long_fixture_fuzz_test.go: the seed bytes drive a
// `pick(n)` cursor (next-byte % n) across seven pools — rcMode,
// targetKbps, kfInterval, buffer, fixture, deadline, cpuUsed. We
// compose the regression suffix from the resulting bucket choices so
// each new failing seed reads like the existing curated names
// (regression_<rc>_<kbps>kbps_kf<n>_<fixture>...).
func classifyLongFixtureRateControl(data []byte) (string, error) {
	r := bucketCursor{data: data}
	rc := []string{"cbr", "vbr"}[r.pick(2)]
	kbps := []int{300, 700, 1200}[r.pick(3)]
	kf := []int{999, 30, 60}[r.pick(3)]
	buf := []string{"defbuf", "tightbuf"}[r.pick(2)]
	fixture := []string{"panning", "splitmv"}[r.pick(2)]
	deadline := []string{"rt", "good"}[r.pick(2)]
	// cpuPool = {-3, 0, -8} in the test; cpu suffix distinguishes
	// otherwise-identical bucket tuples that diverge only on cpu_used.
	cpu := []string{"cpum3", "cpu0", "cpum8"}[r.pick(3)]
	return fmt.Sprintf("%s_%dkbps_kf%d_%s_%s_%s_%s", rc, kbps, kf, fixture, buf, deadline, cpu), nil
}

// classifyDecoderAgainstLibvpx tags fuzz finds by the structural shape
// of the input rather than the precise divergence reason (the
// fuzz-driver log carries the latter, and parsing the IVF header here
// would duplicate testutil parsing logic). "ivf_vp80" marks streams
// that survive minimal IVF magic/codec checks (likely an asymmetric
// decoder bug); "non_ivf" marks malformed-container inputs that one
// side still latched onto.
func classifyDecoderAgainstLibvpx(data []byte) (string, error) {
	if len(data) >= 12 && string(data[0:4]) == "DKIF" && string(data[8:12]) == "VP80" {
		return "ivf_vp80_accept_disagreement", nil
	}
	return "non_ivf_accept_disagreement", nil
}

// bucketCursor mirrors oracleRuntimeControlFuzzBytes.pick semantics:
// each call advances a wrapping byte cursor and returns `b % n`. Used
// by classifiers that have to replay the same bucket-selection logic
// the fuzz target itself runs.
type bucketCursor struct {
	data []byte
	pos  int
}

func (r *bucketCursor) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[r.pos%len(r.data)]
	r.pos++
	return b
}

func (r *bucketCursor) pick(n int) int {
	if n <= 1 {
		return 0
	}
	return int(r.next()) % n
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// parseSeed extracts the raw bytes from a Go fuzz corpus v1 file.
func parseSeed(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := byteLiteral.FindSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("%s: no []byte(...) literal found", path)
	}
	body, err := strconv.Unquote(string(m[1]))
	if err != nil {
		return nil, fmt.Errorf("%s: unquote %q: %w", path, m[1], err)
	}
	return []byte(body), nil
}

func renameOne(corpusDir, fuzzName, hashFile string, cls classifier) (string, error) {
	src := filepath.Join(corpusDir, hashFile)
	data, err := parseSeed(src)
	if err != nil {
		return "", err
	}
	caseName, err := cls(data)
	if err != nil {
		return "", fmt.Errorf("%s: classify: %w", src, err)
	}
	dstName := fmt.Sprintf("regression_%s_%s", caseName, hashFile[:8])
	dst := filepath.Join(corpusDir, dstName)
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("%s: destination already exists: %s", src, dst)
	}
	tracked, err := gitIsTracked(src)
	if err != nil {
		return "", err
	}
	if tracked {
		if out, err := exec.Command("git", "mv", src, dst).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git mv %s %s: %v: %s", src, dst, err, strings.TrimSpace(string(out)))
		}
	} else {
		// Fresh fuzz discoveries are untracked. git mv refuses those,
		// so fall back to a plain rename followed by `git add` so the
		// new name shows up in `git status` ready to stage.
		if err := os.Rename(src, dst); err != nil {
			return "", fmt.Errorf("rename %s -> %s: %w", src, dst, err)
		}
		if out, err := exec.Command("git", "add", "--", dst).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git add %s: %v: %s", dst, err, strings.TrimSpace(string(out)))
		}
	}
	return fmt.Sprintf("%s/%s -> %s", fuzzName, hashFile, dstName), nil
}

// gitIsTracked reports whether path is tracked in the git index.
func gitIsTracked(path string) (bool, error) {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, fmt.Errorf("git ls-files %s: %w", path, err)
	}
	return true, nil
}

func run(root string) error {
	base := filepath.Join(root, "testdata", "fuzz")
	entries, err := os.ReadDir(base)
	if err != nil {
		return fmt.Errorf("read %s: %w", base, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var firstErr error
	renamed := 0
	for _, fuzzName := range names {
		corpusDir := filepath.Join(base, fuzzName)
		files, err := os.ReadDir(corpusDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", corpusDir, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		fileNames := make([]string, 0, len(files))
		for _, f := range files {
			if !f.IsDir() {
				fileNames = append(fileNames, f.Name())
			}
		}
		sort.Strings(fileNames)
		for _, name := range fileNames {
			if strings.HasPrefix(name, "regression_") {
				continue
			}
			if !hashName.MatchString(name) {
				continue
			}
			cls, ok := dispatch[fuzzName]
			if !ok {
				err := fmt.Errorf("%s/%s: no classifier registered for fuzz target %q", fuzzName, name, fuzzName)
				fmt.Fprintln(os.Stderr, err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			msg, err := renameOne(corpusDir, fuzzName, name, cls)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			fmt.Println(msg)
			renamed++
		}
	}
	if renamed == 0 && firstErr == nil {
		fmt.Println("govpx-fuzz-rename: no hash-named seeds found; corpus already clean")
	}
	return firstErr
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if err := run(root); err != nil {
		os.Exit(1)
	}
}
