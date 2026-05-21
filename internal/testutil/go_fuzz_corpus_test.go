package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadGoFuzzCorpusByteSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed")
	if err := os.WriteFile(path, []byte("go test fuzz v1\n[]byte(\"\\x00abc\")\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ReadGoFuzzCorpusByteSeed(path)
	if err != nil {
		t.Fatalf("ReadGoFuzzCorpusByteSeed: %v", err)
	}
	want := []byte{0, 'a', 'b', 'c'}
	if string(got) != string(want) {
		t.Fatalf("payload = %v, want %v", got, want)
	}
}

func TestReadGoFuzzCorpusByteSeedRejectsMissingPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed")
	if err := os.WriteFile(path, []byte("go test fuzz v1\nint(3)\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := ReadGoFuzzCorpusByteSeed(path); err == nil {
		t.Fatalf("ReadGoFuzzCorpusByteSeed succeeded without []byte payload")
	}
}
