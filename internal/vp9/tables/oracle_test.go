package tables

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// findLibvpxSource walks up from this test file to find the pinned
// libvpx checkout under internal/coracle/build. Returns "" if it isn't
// present — the oracle skips in that case so CI builds without the
// checkout still pass the rest of the package.
func findLibvpxSource(rel string) string {
	_, here, _, _ := runtime.Caller(0)
	dir := filepath.Dir(here)
	for {
		path := filepath.Join(dir, "internal", "coracle", "build", "libvpx-v1.16.0", rel)
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// extractCArray reads a contiguous brace-delimited integer initializer
// from a C source. It is intentionally narrow: only enough to parse the
// flat dc_qlookup* / ac_qlookup* tables. Returns nil if the marker is
// missing.
func extractCArray(src, marker string) []int {
	idx := strings.Index(src, marker)
	if idx < 0 {
		return nil
	}
	open := strings.Index(src[idx:], "{")
	if open < 0 {
		return nil
	}
	open += idx
	close := strings.Index(src[open:], "}")
	if close < 0 {
		return nil
	}
	close += open
	body := src[open+1 : close]
	tokens := regexp.MustCompile(`-?\d+`).FindAllString(body, -1)
	out := make([]int, 0, len(tokens))
	for _, t := range tokens {
		v, err := strconv.Atoi(t)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

func compareTable(t *testing.T, name string, got []int16, want []int) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: got %d entries, want %d", name, len(got), len(want))
	}
	for i := range got {
		if int(got[i]) != want[i] {
			t.Fatalf("%s[%d] = %d, libvpx says %d", name, i, got[i], want[i])
		}
	}
}

// TestQuantTablesMatchLibvpxSource reads vp9_quant_common.c straight off
// disk and asserts that every entry of every dequant table matches
// byte-for-byte. This is the strongest parity guarantee we can give
// without a C oracle binary: the canonical libvpx source is the
// reference.
func TestQuantTablesMatchLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSource("vp9/common/vp9_quant_common.c")
	if srcPath == "" {
		t.Skip("libvpx checkout not present under internal/coracle/build; run `make oracle-tools` to enable")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	src := string(raw)

	cases := []struct {
		name   string
		marker string
		got    []int16
	}{
		{"DcQLookup8", "dc_qlookup[QINDEX_RANGE]", DcQLookup8[:]},
		{"DcQLookup10", "dc_qlookup_10[QINDEX_RANGE]", DcQLookup10[:]},
		{"DcQLookup12", "dc_qlookup_12[QINDEX_RANGE]", DcQLookup12[:]},
		{"AcQLookup8", "ac_qlookup[QINDEX_RANGE]", AcQLookup8[:]},
		{"AcQLookup10", "ac_qlookup_10[QINDEX_RANGE]", AcQLookup10[:]},
		{"AcQLookup12", "ac_qlookup_12[QINDEX_RANGE]", AcQLookup12[:]},
	}
	for _, tc := range cases {
		want := extractCArray(src, tc.marker)
		if want == nil {
			t.Errorf("%s: marker %q not found in libvpx source", tc.name, tc.marker)
			continue
		}
		compareTable(t, tc.name, tc.got, want)
	}
}

// TestScanTablesMatchLibvpxSource validates every scan-order, inverse
// scan, and neighbor table in scan.go against libvpx's vp9_scan.c. This
// guards against accidental hand-edits to the generated file and against
// regressions in gen_scan.go itself.
func TestScanTablesMatchLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSource("vp9/common/vp9_scan.c")
	if srcPath == "" {
		t.Skip("libvpx checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	src := string(raw)

	cases := []struct {
		name string
		got  []int16
	}{
		{"default_scan_4x4", DefaultScan4x4[:]},
		{"default_scan_8x8", DefaultScan8x8[:]},
		{"default_scan_16x16", DefaultScan16x16[:]},
		{"default_scan_32x32", DefaultScan32x32[:]},
		{"col_scan_4x4", ColScan4x4[:]},
		{"col_scan_8x8", ColScan8x8[:]},
		{"col_scan_16x16", ColScan16x16[:]},
		{"row_scan_4x4", RowScan4x4[:]},
		{"row_scan_8x8", RowScan8x8[:]},
		{"row_scan_16x16", RowScan16x16[:]},
		{"vp9_default_iscan_4x4", DefaultIScan4x4[:]},
		{"vp9_default_iscan_8x8", DefaultIScan8x8[:]},
		{"vp9_default_iscan_16x16", DefaultIScan16x16[:]},
		{"vp9_default_iscan_32x32", DefaultIScan32x32[:]},
		{"vp9_col_iscan_4x4", ColIScan4x4[:]},
		{"vp9_col_iscan_8x8", ColIScan8x8[:]},
		{"vp9_col_iscan_16x16", ColIScan16x16[:]},
		{"vp9_row_iscan_4x4", RowIScan4x4[:]},
		{"vp9_row_iscan_8x8", RowIScan8x8[:]},
		{"vp9_row_iscan_16x16", RowIScan16x16[:]},
		{"default_scan_4x4_neighbors", DefaultScan4x4Neighbors[:]},
		{"default_scan_8x8_neighbors", DefaultScan8x8Neighbors[:]},
		{"default_scan_16x16_neighbors", DefaultScan16x16Neighbors[:]},
		{"default_scan_32x32_neighbors", DefaultScan32x32Neighbors[:]},
		{"col_scan_4x4_neighbors", ColScan4x4Neighbors[:]},
		{"col_scan_8x8_neighbors", ColScan8x8Neighbors[:]},
		{"col_scan_16x16_neighbors", ColScan16x16Neighbors[:]},
		{"row_scan_4x4_neighbors", RowScan4x4Neighbors[:]},
		{"row_scan_8x8_neighbors", RowScan8x8Neighbors[:]},
		{"row_scan_16x16_neighbors", RowScan16x16Neighbors[:]},
	}
	for _, tc := range cases {
		want := extractScanArray(src, tc.name)
		if want == nil {
			t.Errorf("%s: marker not found in libvpx source", tc.name)
			continue
		}
		compareTable(t, tc.name, tc.got, want)
	}
}

// extractScanArray finds a single DECLARE_ALIGNED int16_t table by name
// regardless of whether the declared length is a literal or an
// expression. extractCArray's "name[N]" marker form doesn't work for the
// scan tables because they use "name[N * MAX_NEIGHBORS]" forms.
func extractScanArray(src, name string) []int {
	needle := name + "["
	idx := strings.Index(src, needle)
	if idx < 0 {
		return nil
	}
	open := strings.Index(src[idx:], "{")
	if open < 0 {
		return nil
	}
	open += idx
	close := strings.Index(src[open:], "}")
	if close < 0 {
		return nil
	}
	close += open
	body := src[open+1 : close]
	tokens := regexp.MustCompile(`-?\d+`).FindAllString(body, -1)
	out := make([]int, 0, len(tokens))
	for _, t := range tokens {
		v, err := strconv.Atoi(t)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

// TestVpxNormMatchesLibvpxSource is the same oracle check for the
// boolean coder's normalization table.
func TestVpxNormMatchesLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSource("vpx_dsp/prob.c")
	if srcPath == "" {
		t.Skip("libvpx checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	want := extractCArray(string(raw), "vpx_norm[256]")
	if want == nil {
		t.Fatal("vpx_norm[256] marker not found in libvpx source")
	}
	got := VpxNorm[:]
	if len(want) != len(got) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range got {
		if int(got[i]) != want[i] {
			t.Fatalf("VpxNorm[%d] = %d, libvpx says %d", i, got[i], want[i])
		}
	}
}
