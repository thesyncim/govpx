package testutil

import "testing"

func TestFirstByteDiff(t *testing.T) {
	tests := []struct {
		name string
		a    []byte
		b    []byte
		want int
	}{
		{name: "equal", a: []byte{1, 2, 3}, b: []byte{1, 2, 3}, want: -1},
		{name: "first", a: []byte{1, 2, 3}, b: []byte{9, 2, 3}, want: 0},
		{name: "middle", a: []byte{1, 2, 3}, b: []byte{1, 9, 3}, want: 1},
		{name: "length", a: []byte{1, 2}, b: []byte{1, 2, 3}, want: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstByteDiff(tc.a, tc.b); got != tc.want {
				t.Fatalf("FirstByteDiff = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMatchedFramePrefixLength(t *testing.T) {
	got := [][]byte{{1}, {2}, {3}}
	want := [][]byte{{1}, {2}, {9}}
	if prefix := MatchedFramePrefixLength(got, want); prefix != 2 {
		t.Fatalf("matched prefix = %d, want 2", prefix)
	}
	if prefix := MatchedFramePrefixLength(got[:2], want); prefix != 2 {
		t.Fatalf("short matched prefix = %d, want 2", prefix)
	}
}

func TestFramePayloadSHA8s(t *testing.T) {
	frames := [][]byte{{1, 2, 3}, {4, 5}}
	got := FramePayloadSHA8s(frames)
	if len(got) != 2 {
		t.Fatalf("summary length = %d, want 2", len(got))
	}
	if got[0] != "039058c6f2c0cb49:3" {
		t.Fatalf("summary[0] = %q, want sha8:length", got[0])
	}
	if got[1] != "2fa1b377bf67309f:2" {
		t.Fatalf("summary[1] = %q, want sha8:length", got[1])
	}
}
