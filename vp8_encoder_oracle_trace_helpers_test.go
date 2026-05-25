//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"
)

func requireOracleTraceBuild(t *testing.T) {
	t.Helper()
	if !oracleTraceBuild {
		t.Skip("oracle tracing is compiled out; run with -tags govpx_oracle_trace")
	}
}

func splitNonEmptyLines(b []byte) [][]byte {
	var out [][]byte
	for line := range bytes.SplitSeq(b, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}
