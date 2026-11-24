//go:build govpx_oracle_trace

package decoder

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestExtendModesMatchLibvpxSource validates ExtendModes byte-for-byte
// against the libvpx source. The C table uses identifier expressions
// (NEED_ABOVE | NEED_LEFT, etc.) so we parse the identifier sequence
// and recompose with the same enum constants on the Go side.
func TestExtendModesMatchLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSourceForDecoder("vp9/common/vp9_reconintra.c")
	if srcPath == "" {
		t.Skip("libvpx VP9 checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	body := braceBodyForTest(string(raw), "extend_modes[INTRA_MODES]")
	if body == "" {
		t.Fatal("extend_modes body not found")
	}
	tokenRe := regexp.MustCompile(`NEED_[A-Z]+`)
	idMap := map[string]uint8{
		"NEED_LEFT":       NeedLeft,
		"NEED_ABOVE":      NeedAbove,
		"NEED_ABOVERIGHT": NeedAboveRight,
	}
	entries := splitTopLevelCommas(body)
	if len(entries) != int(common.IntraModes) {
		t.Fatalf("got %d entries, want %d", len(entries), common.IntraModes)
	}
	for i, e := range entries {
		ids := tokenRe.FindAllString(e, -1)
		var want uint8
		for _, id := range ids {
			v, ok := idMap[id]
			if !ok {
				t.Fatalf("unknown id %q in entry %d", id, i)
			}
			want |= v
		}
		if ExtendModes[i] != want {
			t.Errorf("[%d] got %#x, libvpx says %#x", i, ExtendModes[i], want)
		}
	}
}

// findLibvpxSourceForDecoder walks up to the libvpx VP9 build root.
func findLibvpxSourceForDecoder(rel string) string {
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

// braceBodyForTest pulls the brace-delimited initializer body and
// strips comments. Mirrors the tables-package helper.
func braceBodyForTest(src, marker string) string {
	idx := indexFor(src, marker)
	if idx < 0 {
		return ""
	}
	open := indexFor(src[idx:], "{")
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
	return regexp.MustCompile(`//[^\n]*`).ReplaceAllString(src[open+1:end], "")
}

func indexFor(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// splitTopLevelCommas splits the flat extend_modes initializer.
func splitTopLevelCommas(body string) []string {
	parts := []string{}
	cur := ""
	for _, ch := range body {
		if ch == ',' {
			s := stripSpaces(cur)
			if s != "" {
				parts = append(parts, s)
			}
			cur = ""
			continue
		}
		cur += string(ch)
	}
	s := stripSpaces(cur)
	if s != "" {
		parts = append(parts, s)
	}
	return parts
}

func stripSpaces(s string) string {
	var out strings.Builder
	for _, ch := range s {
		if ch != ' ' && ch != '\t' && ch != '\n' {
			out.WriteString(string(ch))
		}
	}
	return out.String()
}
