//go:build govpx_oracle_trace

package tables

import (
	"os"
	"regexp"
	"testing"
)

// TestMode2CounterMatchesLibvpxSource validates mode_2_counter
// against vp9/common/vp9_mvref_common.h. Source uses identifier-only
// entries (all 9s for intra; 0,0,3,1 for inter), so extractBracedArray's
// raw-int regex returns exactly the inter sub-list; we anchor on
// libvpx's structural shape rather than the full array.
func TestMode2CounterMatchesLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSourceVP9("vp9/common/vp9_mvref_common.h")
	if srcPath == "" {
		t.Skip("libvpx VP9 checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	want := extractBracedArray(string(raw), "mode_2_counter")
	if want == nil {
		t.Fatal("mode_2_counter marker not found in libvpx source")
	}
	if len(want) != len(Mode2Counter) {
		t.Fatalf("got %d entries from source, want %d", len(want), len(Mode2Counter))
	}
	for i, v := range want {
		if int(Mode2Counter[i]) != v {
			t.Errorf("[%d] got %d, libvpx says %d", i, Mode2Counter[i], v)
		}
	}
}

// TestCounterToContextMatchesLibvpxSource validates counter_to_context
// against the libvpx source. Source comments reference the matching
// motion_vector_context identifiers — extractBracedArray's regex only
// returns the numeric position comments (0..18). We resolve identifier
// references by mapping them through the enum constants in Go.
func TestCounterToContextMatchesLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSourceVP9("vp9/common/vp9_mvref_common.h")
	if srcPath == "" {
		t.Skip("libvpx VP9 checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	// Pull the brace-delimited body verbatim and parse identifiers in
	// declaration order.
	src := string(raw)
	body := braceBody(src, "counter_to_context[19]")
	if body == "" {
		t.Fatal("counter_to_context body not found")
	}
	tokenRe := regexp.MustCompile(`[A-Z_][A-Z0-9_]+`)
	ids := tokenRe.FindAllString(body, -1)
	if len(ids) != len(CounterToContext) {
		t.Fatalf("got %d ids from source, want %d", len(ids), len(CounterToContext))
	}
	idMap := map[string]uint8{
		"BOTH_ZERO":            BothZero,
		"ZERO_PLUS_PREDICTED":  ZeroPlusPredicted,
		"BOTH_PREDICTED":       BothPredicted,
		"NEW_PLUS_NON_INTRA":   NewPlusNonIntra,
		"BOTH_NEW":             BothNew,
		"INTRA_PLUS_NON_INTRA": IntraPlusNonIntra,
		"BOTH_INTRA":           BothIntra,
		"INVALID_CASE":         InvalidCase,
	}
	for i, id := range ids {
		want, ok := idMap[id]
		if !ok {
			t.Fatalf("unknown id %q at position %d", id, i)
		}
		if CounterToContext[i] != want {
			t.Errorf("[%d] got %d (id %s wants %d)", i, CounterToContext[i], id, want)
		}
	}
}

// braceBody returns the brace-delimited initializer body following
// `marker`. Comments are stripped.
func braceBody(src, marker string) string {
	idx := indexOf(src, marker)
	if idx < 0 {
		return ""
	}
	open := indexOf(src[idx:], "{")
	if open < 0 {
		return ""
	}
	open += idx
	depth := 0
	end := -1
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return ""
	}
	body := src[open+1 : end]
	body = regexp.MustCompile(`//[^\n]*`).ReplaceAllString(body, "")
	return body
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
