package coracletest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadOrWriteJSONBaselineWritesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	current := map[string]int{"a": 1}

	got, wrote := ReadOrWriteJSONBaseline[map[string]int](t, path, current)
	if !wrote {
		t.Fatalf("wrote = false, want true")
	}
	if got != nil {
		t.Fatalf("baseline = %v, want zero value after write", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat written baseline: %v", err)
	}
}

func TestReadOrWriteJSONBaselineReadsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	WriteJSONBaseline(t, path, map[string]int{"a": 1})

	got, wrote := ReadOrWriteJSONBaseline[map[string]int](t, path, map[string]int{"a": 2})
	if wrote {
		t.Fatalf("wrote = true, want false")
	}
	if got["a"] != 1 {
		t.Fatalf("baseline a = %d, want 1", got["a"])
	}
}

func TestReadOrWriteJSONBaselineHonorsUpdateEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	WriteJSONBaseline(t, path, map[string]int{"a": 1})
	t.Setenv(updateBaselinesEnv, "1")

	_, wrote := ReadOrWriteJSONBaseline[map[string]int](t, path, map[string]int{"a": 2})
	if !wrote {
		t.Fatalf("wrote = false, want true")
	}
	var got map[string]int
	ReadJSONBaseline(t, path, &got)
	if got["a"] != 2 {
		t.Fatalf("updated baseline a = %d, want 2", got["a"])
	}
}

func TestReadOptionalJSONBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if got, ok := ReadOptionalJSONBaseline[map[string]int](t, path); ok || got != nil {
		t.Fatalf("missing baseline = (%v, %v), want (nil, false)", got, ok)
	}

	WriteJSONBaseline(t, path, map[string]int{"a": 1})
	got, ok := ReadOptionalJSONBaseline[map[string]int](t, path)
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if got["a"] != 1 {
		t.Fatalf("baseline a = %d, want 1", got["a"])
	}
}

func TestReadOptionalJSONBaselineSkipsWhenUpdating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	WriteJSONBaseline(t, path, map[string]int{"a": 1})
	t.Setenv(updateBaselinesEnv, "1")

	got, ok := ReadOptionalJSONBaseline[map[string]int](t, path)
	if ok || got != nil {
		t.Fatalf("baseline while updating = (%v, %v), want (nil, false)", got, ok)
	}
}
