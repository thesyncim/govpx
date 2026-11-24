//go:build govpx_oracle_trace

package encoder

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestPtEnergyClassMatchesLibvpxSource pins PtEnergyClass byte-for-byte
// against the libvpx v1.16.0 declaration at vp9/common/vp9_entropy.c:95.
// The table is small, but token-cache lookups are sensitive enough that
// one-byte drift would shift downstream coefficient cost decisions.
func TestPtEnergyClassMatchesLibvpxSource(t *testing.T) {
	srcPath := findVP9EncoderSource("vp9/common/vp9_entropy.c")
	if srcPath == "" {
		t.Skip("libvpx VP9 checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	src := string(raw)

	idx := strings.Index(src, "vp9_pt_energy_class[ENTROPY_TOKENS]")
	if idx < 0 {
		t.Fatal("vp9_pt_energy_class marker not found in libvpx source")
	}
	open := strings.Index(src[idx:], "{")
	if open < 0 {
		t.Fatal("vp9_pt_energy_class: opening brace not found")
	}
	closeIdx := strings.Index(src[idx+open:], "};")
	if closeIdx < 0 {
		t.Fatal("vp9_pt_energy_class: closing brace not found")
	}
	body := src[idx+open+1 : idx+open+closeIdx]
	body = regexp.MustCompile(`/\*[\s\S]*?\*/`).ReplaceAllString(body, "")
	tokens := regexp.MustCompile(`\d+`).FindAllString(body, -1)
	if len(tokens) != EntropyTokens {
		t.Fatalf("vp9_pt_energy_class: got %d entries from source, want %d",
			len(tokens), EntropyTokens)
	}
	for i, tok := range tokens {
		want, err := strconv.Atoi(tok)
		if err != nil {
			t.Fatalf("parse entry %d: %v", i, err)
		}
		if int(PtEnergyClass[i]) != want {
			t.Errorf("PtEnergyClass[%d] = %d, libvpx says %d", i, PtEnergyClass[i], want)
		}
	}
}
