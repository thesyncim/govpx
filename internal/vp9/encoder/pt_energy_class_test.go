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
// Mirrors the source-text pattern that TestVP9ProbCostMatchesLibvpxSource
// uses for vp9_prob_cost; the table is small (12 bytes) but the encoder
// path (vp9_rdopt.c::cost_coeffs token_cache lookups) is sensitive enough
// that even a single-byte drift would silently shift every downstream
// (band, ctx) pick.
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

// TestPtEnergyClassPositionalAnchors enforces the structural invariants
// the libvpx comment block in vp9_entropy.h:27-38 promises: ZERO maps
// to class 0, ONE/TWO each get their own class, THREE and FOUR share
// class 3, CAT1/CAT2 share class 4, CAT3..CAT6 + EOB all live at the
// terminal class 5. These shape checks catch a drift that the
// source-text pin can't (e.g. permuted indices that still total the
// right number of distinct values).
func TestPtEnergyClassPositionalAnchors(t *testing.T) {
	type anchor struct {
		idx  int
		want uint8
		name string
	}
	cases := []anchor{
		{ZeroToken, 0, "ZERO_TOKEN"},
		{OneToken, 1, "ONE_TOKEN"},
		{TwoToken, 2, "TWO_TOKEN"},
		{ThreeToken, 3, "THREE_TOKEN"},
		{FourToken, 3, "FOUR_TOKEN"},
		{Category1Tok, 4, "CATEGORY1_TOKEN"},
		{Category2Tok, 4, "CATEGORY2_TOKEN"},
		{Category3Tok, 5, "CATEGORY3_TOKEN"},
		{Category4Tok, 5, "CATEGORY4_TOKEN"},
		{Category5Tok, 5, "CATEGORY5_TOKEN"},
		{Category6Tok, 5, "CATEGORY6_TOKEN"},
		{EobToken, 5, "EOB_TOKEN"},
	}
	for _, c := range cases {
		if got := PtEnergyClass[c.idx]; got != c.want {
			t.Errorf("PtEnergyClass[%s=%d] = %d, want %d", c.name, c.idx, got, c.want)
		}
	}
}
