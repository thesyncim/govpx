package encoder

import "testing"

func TestClampEncodeCoordClampsToVisibleEdge(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int
		limit int
		want  int
	}{
		{name: "below", value: -5, limit: 8, want: 0},
		{name: "first", value: 0, limit: 8, want: 0},
		{name: "inside", value: 5, limit: 8, want: 5},
		{name: "last", value: 7, limit: 8, want: 7},
		{name: "past", value: 12, limit: 8, want: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampEncodeCoord(tc.value, tc.limit); got != tc.want {
				t.Fatalf("ClampEncodeCoord(%d, %d) = %d, want %d", tc.value, tc.limit, got, tc.want)
			}
		})
	}
}
