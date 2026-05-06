package libgopx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInternalVP8SourcesCiteLibvpxBaseline(t *testing.T) {
	const root = "internal/vp8"
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(src), "libvpx v1.16.0") {
			t.Fatalf("%s does not cite libvpx v1.16.0 baseline", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir returned error: %v", err)
	}
}
