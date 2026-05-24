//go:build govpx_oracle_trace

package vp8test

import "testing"

func TestStrictThreadedOracleQuarantine(t *testing.T) {
	t.Setenv("GOVPX_ORACLE_THREADS_QUARANTINE", "")
	if StrictThreadedOracleQuarantine() {
		t.Fatal("empty quarantine mode enabled strict handling")
	}

	t.Setenv("GOVPX_ORACLE_THREADS_QUARANTINE", "log")
	if StrictThreadedOracleQuarantine() {
		t.Fatal("log quarantine mode enabled strict handling")
	}

	t.Setenv("GOVPX_ORACLE_THREADS_QUARANTINE", "strict")
	if !StrictThreadedOracleQuarantine() {
		t.Fatal("strict quarantine mode did not enable strict handling")
	}
}
