package govpx

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
			return true
		}
	}
	return false
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
