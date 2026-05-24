//go:build govpx_oracle_trace

package coracletest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const updateBaselinesEnv = "GOVPX_UPDATE_BASELINES"

// UpdateBaselines reports whether oracle scoreboard baselines should be
// rewritten for the current test run.
func UpdateBaselines() bool {
	return os.Getenv(updateBaselinesEnv) == "1"
}

// ReadOrWriteJSONBaseline writes current when the baseline is missing or when
// GOVPX_UPDATE_BASELINES=1 is set. Otherwise it decodes and returns the
// existing baseline.
func ReadOrWriteJSONBaseline[T any](t testing.TB, path string, current T) (baseline T, wrote bool) {
	t.Helper()
	baseline, ok := ReadOptionalJSONBaseline[T](t, path)
	if !ok {
		WriteJSONBaseline(t, path, current)
		return baseline, true
	}
	return baseline, false
}

// ReadOptionalJSONBaseline decodes an existing JSON baseline. It returns false
// when the baseline is missing or baseline updates were requested.
func ReadOptionalJSONBaseline[T any](t testing.TB, path string) (baseline T, ok bool) {
	t.Helper()
	if UpdateBaselines() {
		return baseline, false
	}
	_, statErr := os.Stat(path)
	if os.IsNotExist(statErr) {
		return baseline, false
	}
	if statErr != nil {
		t.Fatalf("stat %s: %v", path, statErr)
	}
	ReadJSONBaseline(t, path, &baseline)
	return baseline, true
}

// ReadJSONBaseline decodes a JSON baseline file into dst.
func ReadJSONBaseline(t testing.TB, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("Unmarshal baseline: %v", err)
	}
}

// WriteJSONBaseline writes v as stable indented JSON with a trailing newline.
func WriteJSONBaseline(t testing.TB, path string, v any) {
	t.Helper()
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("Marshal JSON baseline: %v", err)
	}
	buf = append(buf, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	t.Logf("wrote baseline %s", path)
}
