//go:build govpx_oracle_trace

package encoder

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestVP9ProbCostMatchesLibvpxSource validates the cost table
// against the libvpx source byte-for-byte.
func TestVP9ProbCostMatchesLibvpxSource(t *testing.T) {
	srcPath := findVP9EncoderSource("vp9/encoder/vp9_cost.c")
	if srcPath == "" {
		t.Skip("libvpx VP9 checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	idx := strings.Index(string(raw), "vp9_prob_cost[256]")
	if idx < 0 {
		t.Fatal("marker not found")
	}
	open := strings.Index(string(raw)[idx:], "{")
	if open < 0 {
		t.Fatal("open brace not found")
	}
	close := strings.Index(string(raw)[idx+open:], "};")
	if close < 0 {
		t.Fatal("close brace not found")
	}
	body := string(raw)[idx+open+1 : idx+open+close]
	body = regexp.MustCompile(`/\*[\s\S]*?\*/`).ReplaceAllString(body, "")
	tokens := regexp.MustCompile(`\d+`).FindAllString(body, -1)
	if len(tokens) > 256 {
		tokens = tokens[:256]
	}
	for i, tok := range tokens {
		want, err := strconv.Atoi(tok)
		if err != nil {
			t.Fatalf("parse tok %d: %v", i, err)
		}
		if int(VP9ProbCost[i]) != want {
			t.Errorf("VP9ProbCost[%d] = %d, want %d", i, VP9ProbCost[i], want)
		}
	}
}

// findVP9EncoderSource walks up to find the libvpx checkout.
func findVP9EncoderSource(rel string) string {
	_, here, _, _ := runtime.Caller(0)
	dir := filepath.Dir(here)
	for _, root := range []string{"libvpx-v1.16.0-vp9", "libvpx-v1.16.0"} {
		d := dir
		for {
			path := filepath.Join(d, "internal", "coracle", "build", root, rel)
			if _, err := os.Stat(path); err == nil {
				return path
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	return ""
}
