package govpx_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCodecOracleTestsUseCoracleProcessLibrary(t *testing.T) {
	for _, pattern := range []string{
		"*_test.go",
		filepath.Join("benchmarks", "*_test.go"),
	} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("Glob(%q): %v", pattern, err)
		}
		for _, path := range files {
			assertTestFileDoesNotImport(t, path, "os/exec",
				"oracle subprocess helpers belong in internal/coracle")
		}
	}
}

func TestCoracleImportsStayInOracleTraceBuild(t *testing.T) {
	for _, pattern := range []string{
		"*_test.go",
		filepath.Join("benchmarks", "*_test.go"),
	} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("Glob(%q): %v", pattern, err)
		}
		for _, path := range files {
			if !testFileImports(t, path, "github.com/thesyncim/govpx/internal/coracle") &&
				!testFileImports(t, path, "github.com/thesyncim/govpx/internal/coracle/coracletest") {
				continue
			}
			if !testFileHasBuildTag(t, path, "govpx_oracle_trace") {
				t.Fatalf("%s imports internal/coracle outside the govpx_oracle_trace build", path)
			}
		}
	}
}

func TestRootOracleTestsUseCodecHarnessPackages(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("Glob(%q): %v", "*_test.go", err)
	}
	for _, path := range files {
		for _, importPath := range []string{
			"github.com/thesyncim/govpx/internal/coracle",
			"github.com/thesyncim/govpx/internal/coracle/coracletest",
		} {
			assertTestFileDoesNotImport(t, path, importPath,
				"root tests should use internal/testutil/vp8test or internal/testutil/vp9test")
		}
	}
}

func TestDefaultBuildProductionFilesAvoidTestHarnessImports(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".claude", ".git", "internal/coracle/build", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if testFileHasBuildTag(t, path, "govpx_oracle_trace") ||
			testFileHasBuildTag(t, path, "govpx_phase_stats") {
			return nil
		}
		for _, importPath := range goFileImports(t, path) {
			switch importPath {
			case "testing":
				if !strings.HasPrefix(path, "internal/testutil/") {
					t.Fatalf("%s imports testing in the default production build", path)
				}
			case "github.com/thesyncim/govpx/internal/testutil":
				if !strings.HasPrefix(path, "internal/testutil/") {
					t.Fatalf("%s imports %q in the default production build",
						path, importPath)
				}
			case "github.com/thesyncim/govpx/internal/coracle/coracletest":
				t.Fatalf("%s imports %q in the default production build",
					path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(.): %v", err)
	}
}

func TestVP9OracleSourceTestsStayTagged(t *testing.T) {
	oracleProbeText := []string{
		"libvpx checkout not present under internal/coracle/build",
		"libvpx VP9 checkout not present under internal/coracle/build",
		"bash internal/coracle/build_",
	}
	err := filepath.WalkDir(filepath.Join("internal", "vp9"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(data)
		for _, probe := range oracleProbeText {
			if !strings.Contains(src, probe) {
				continue
			}
			if !testFileHasBuildTag(t, path, "govpx_oracle_trace") {
				t.Fatalf("%s probes libvpx oracle/source assets outside the govpx_oracle_trace build", path)
			}
			break
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(internal/vp9): %v", err)
	}
}

func TestCleanedVP9AndBenchFilesDoNotUseTrackerLabels(t *testing.T) {
	files := vp9AndBenchGoFiles(t)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		src := string(data)
		for _, marker := range []string{"Task #", "task #", "audit #"} {
			if strings.Contains(src, marker) {
				t.Fatalf("%s contains tracker-era marker %q", path, marker)
			}
		}
		for lineNo, line := range strings.Split(src, "\n") {
			if strings.Contains(line, "func TestVP9") &&
				strings.Contains(line, "Scoreboard") {
				t.Fatalf("%s:%d contains scoreboard in a VP9 test name",
					path, lineNo+1)
			}
		}
	}
}

func TestRootVPxOracleTestsUseObjectiveNames(t *testing.T) {
	files, err := filepath.Glob("vp[89]*_test.go")
	if err != nil {
		t.Fatalf("Glob(%q): %v", "vp[89]*_test.go", err)
	}
	for _, path := range files {
		if strings.Contains(filepath.Base(path), "scoreboard") {
			t.Fatalf("%s uses a scoreboard-era filename; root parity tests need objective names", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "func TestVP") &&
				strings.Contains(line, "Scoreboard") {
				t.Fatalf("%s:%d uses a scoreboard-era test name",
					path, lineNo+1)
			}
		}
	}
}

func TestRootVP9TestsUseHarnessBitstreamHelpers(t *testing.T) {
	files, err := filepath.Glob("vp9*_test.go")
	if err != nil {
		t.Fatalf("Glob(%q): %v", "vp9*_test.go", err)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		src := string(data)
		for _, marker := range []string{
			"type vp9BitPacker",
			"func vp9ShowExistingFramePacketForTest",
			"func vp9SuperframePacketForTest",
			"func enrichVP9RateTraceRowFromPacket",
			"func readVP9CompressedHeaderForOracleTest",
			"func vp9ReferenceMaskFromLibvpxFrameFlags",
		} {
			if strings.Contains(src, marker) {
				t.Fatalf("%s contains root VP9 bitstream helper %q; use internal/testutil/vp9test", path, marker)
			}
		}
	}
}

func assertTestFileDoesNotImport(t *testing.T, path string, importPath string,
	reason string,
) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", path, err)
	}
	for _, spec := range file.Imports {
		got, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("Unquote(%s import): %v", path, err)
		}
		if got == importPath {
			t.Fatalf("%s imports %q; %s", path, importPath, reason)
		}
	}
}

func testFileImports(t *testing.T, path string, importPath string) bool {
	t.Helper()
	for _, got := range goFileImports(t, path) {
		if got == importPath {
			return true
		}
	}
	return false
}

func vp9AndBenchGoFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("vp9*.go")
	if err != nil {
		t.Fatalf("Glob(%q): %v", "vp9*.go", err)
	}
	files = append(files, walkGoFiles(t, filepath.Join("internal", "vp9"))...)
	files = append(files, walkGoFiles(t, filepath.Join("cmd", "govpx-bench", "benchcmd"))...)
	return files
}

func walkGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s): %v", root, err)
	}
	return files
}

func goFileImports(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", path, err)
	}
	imports := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		got, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("Unquote(%s import): %v", path, err)
		}
		imports = append(imports, got)
	}
	return imports
}

func testFileHasBuildTag(t *testing.T, path string, tag string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.Contains(line, "go:build") && strings.Contains(line, tag)
	}
	return false
}
