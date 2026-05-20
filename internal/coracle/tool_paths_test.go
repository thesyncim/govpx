package coracle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveToolPathUsesFirstConfiguredEnvironment(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOVPX_TEST_TOOL_A", "")
	t.Setenv("GOVPX_TEST_TOOL_B", tool)

	got, err := resolveToolPath(toolPathSpec{
		envNames: []string{"GOVPX_TEST_TOOL_A", "GOVPX_TEST_TOOL_B"},
		notBuilt: errors.New("not built"),
	})
	if err != nil {
		t.Fatalf("resolveToolPath: %v", err)
	}
	if got != tool {
		t.Fatalf("resolveToolPath = %q, want %q", got, tool)
	}
}

func TestResolveToolPathRejectsConfiguredNonExecutable(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOVPX_TEST_TOOL", tool)

	_, err := resolveToolPath(toolPathSpec{
		envNames: []string{"GOVPX_TEST_TOOL"},
		notBuilt: errors.New("not built"),
	})
	if !errors.Is(err, ErrToolPathInvalid) {
		t.Fatalf("resolveToolPath error = %v, want ErrToolPathInvalid", err)
	}
}
