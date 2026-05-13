package encoder

import (
	"math"
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

// TestVP9ProbCostMatchesFormula spot-checks a few entries against
// the documented formula round(-log2(i/256.0) * (1 << 9)). The
// first entry (i=0) is a sentinel and not formula-derived.
func TestVP9ProbCostMatchesFormula(t *testing.T) {
	for _, i := range []int{1, 16, 64, 128, 192, 255} {
		want := math.Round(-math.Log2(float64(i)/256.0) * float64(int(1)<<VP9ProbCostShift))
		if math.Abs(float64(VP9ProbCost[i])-want) > 0.5 {
			t.Errorf("[%d] table=%d formula=%g", i, VP9ProbCost[i], want)
		}
	}
}

// TestCostBranch256Symmetry: cost(ct={a, b}, p=128) must equal
// cost(ct={b, a}, p=128) because vp9_cost_zero(128) ==
// vp9_cost_one(128).
func TestCostBranch256Symmetry(t *testing.T) {
	got1 := CostBranch256([2]uint32{30, 70}, 128)
	got2 := CostBranch256([2]uint32{70, 30}, 128)
	if got1 != got2 {
		t.Errorf("p=128 not symmetric: %d vs %d", got1, got2)
	}
}

// TestProbDiffUpdateSavingsSearchAcceptsImprovement: a count pair
// strongly biased away from oldp should produce a positive savings
// and shift bestp.
func TestProbDiffUpdateSavingsSearchAcceptsImprovement(t *testing.T) {
	ct := [2]uint32{1000, 10} // mostly zeros → low new-p is the right pick
	old := uint8(200)         // wildly mis-tuned
	best := GetBinaryProb(ct[0], ct[1])
	gotBest := best
	savings := ProbDiffUpdateSavingsSearch(ct, old, &gotBest, DiffUpdateProb)
	if savings <= 0 {
		t.Errorf("savings = %d, want > 0", savings)
	}
	if gotBest == old {
		t.Errorf("bestp unchanged at %d", gotBest)
	}
}

// TestProbDiffUpdateSavingsSearchRejectsNoise: when the counts
// already match `oldp` closely, the savings search keeps oldp.
func TestProbDiffUpdateSavingsSearchRejectsNoise(t *testing.T) {
	ct := [2]uint32{0, 0}
	old := uint8(128)
	best := GetBinaryProb(ct[0], ct[1])
	gotBest := best
	savings := ProbDiffUpdateSavingsSearch(ct, old, &gotBest, DiffUpdateProb)
	if savings != 0 {
		t.Errorf("zero counts savings = %d, want 0", savings)
	}
	if gotBest != old {
		t.Errorf("zero counts: bestp = %d, want %d", gotBest, old)
	}
}

// TestGetBinaryProbBounds: clamps to [1, 255], 0/0 returns 128.
func TestGetBinaryProbBounds(t *testing.T) {
	if got := GetBinaryProb(0, 0); got != 128 {
		t.Errorf("0/0 = %d, want 128", got)
	}
	if got := GetBinaryProb(1000, 0); got != 255 {
		t.Errorf("1000/0 = %d, want 255", got)
	}
	if got := GetBinaryProb(0, 1000); got != 1 {
		t.Errorf("0/1000 = %d, want 1", got)
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
