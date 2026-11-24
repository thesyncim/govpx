package govpx_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryGoSourcesDoNotUseCgo(t *testing.T) {
	forbidden := []string{
		"import " + `"C"`,
		"#" + "cgo",
		"//" + "export",
	}
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if shouldSkipNoCgoDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		for _, pattern := range forbidden {
			if strings.Contains(text, pattern) {
				t.Fatalf("%s contains forbidden cgo marker %q", path, pattern)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir returned error: %v", err)
	}
}

func shouldSkipNoCgoDir(path string) bool {
	switch path {
	case ".git", ".gocache", filepath.Join("internal", "coracle", "build"):
		return true
	default:
		return false
	}
}
