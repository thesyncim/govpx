package govpx

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestCodecOracleTestsUseCoracleProcessLibrary(t *testing.T) {
	for _, pattern := range []string{
		"vp8*_test.go",
		"vp9*_test.go",
		filepath.Join("benchmarks", "*_test.go"),
	} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("Glob(%q): %v", pattern, err)
		}
		for _, path := range files {
			assertTestFileDoesNotImport(t, path, "os/exec")
		}
	}
}

func assertTestFileDoesNotImport(t *testing.T, path string, importPath string) {
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
			t.Fatalf("%s imports %q; oracle subprocess helpers belong in internal/coracle", path, importPath)
		}
	}
}
