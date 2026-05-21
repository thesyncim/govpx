package testutil

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadGoFuzzCorpusByteSeed parses a Go fuzz corpus file containing a single
// []byte argument and returns the decoded payload.
func ReadGoFuzzCorpusByteSeed(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[]byte(") || !strings.HasSuffix(line, ")") {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(line, "[]byte("), ")")
		unquoted, err := strconv.Unquote(inner)
		if err != nil {
			return nil, fmt.Errorf("unquote %q: %w", inner, err)
		}
		return []byte(unquoted), nil
	}
	return nil, fmt.Errorf("no []byte(...) line found in %s", path)
}
