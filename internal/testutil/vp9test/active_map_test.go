package vp9test

import "testing"

func TestActiveMapPatterns(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		want    []uint8
	}{
		{name: "all", pattern: "all", want: []uint8{1, 1, 1, 1}},
		{name: "checker", pattern: "checker", want: []uint8{1, 0, 0, 1}},
		{name: "left off", pattern: "left-off", want: []uint8{0, 1, 0, 1}},
		{name: "right off", pattern: "right-off", want: []uint8{1, 0, 1, 0}},
		{name: "border off", pattern: "border-off", want: []uint8{0, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, rows, cols := ActiveMap(32, 32, tc.pattern)
			if rows != 2 || cols != 2 {
				t.Fatalf("macroblock dims = %dx%d, want 2x2", rows, cols)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len(activeMap) = %d, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("activeMap[%d] = %d, want %d; map=%v",
						i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

func TestActiveMapUnknownPatternPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ActiveMap did not panic on an unknown pattern")
		}
	}()
	ActiveMap(16, 16, "unknown")
}
