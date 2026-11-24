//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestCopy8RowsHandledAndDeclines(t *testing.T) {
	for _, p := range [][2]int{{0, 0}, {1, 7}, {7, 1}, {15, 8}, {8, 15}, {8, 8}, {17, 31}, {32, 37}, {1312, 1320}} {
		for _, shift := range []int{-33, -1, 0, 1, 33, 22000} {
			got := make([]byte, 45000)
			for i := range got {
				got[i] = byte(i*37 + 13)
			}
			want := append([]byte(nil), got...)
			so, do := 64, 64+shift
			for y := 0; y < 8; y++ {
				var row [8]byte
				for x := 0; x < 8; x++ {
					row[x] = want[so+y*p[0]+x]
				}
				for x := 0; x < 8; x++ {
					want[do+y*p[1]+x] = row[x]
				}
			}
			if !Copy8Rows(got[so:so+7*p[0]+8], p[0], got[do:do+7*p[1]+8], p[1]) {
				t.Fatalf("declined valid%v shift%d", p, shift)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("mismatch%v shift%d", p, shift)
			}
		}
	}
	for _, p := range [][2]int{{-1, 8}, {8, -1}, {1 << 30, 8}, {8, 1 << 30}, {int(^uint(0) >> 1), 8}} {
		got := make([]byte, 1024)
		for i := range got {
			got[i] = byte(i)
		}
		want := append([]byte(nil), got...)
		if Copy8Rows(got[:512], p[0], got[512:], p[1]) || !bytes.Equal(got, want) {
			t.Fatalf("invalid handled%v", p)
		}
	}
	for _, lens := range [][2]int{{63, 64}, {64, 63}, {0, 64}, {64, 0}, {0, 0}} {
		got := make([]byte, 1024)
		for i := range got {
			got[i] = byte(i)
		}
		want := append([]byte(nil), got...)
		if Copy8Rows(got[:lens[0]], 8, got[512:512+lens[1]], 8) || !bytes.Equal(got, want) {
			t.Fatalf("short handled%v", lens)
		}
	}
	if Copy8Rows(nil, 8, nil, 8) {
		t.Fatal("nil handled")
	}
}

func TestCopy8RowsUnaligned(t *testing.T) {
	for so := range 16 {
		for do := range 16 {
			for _, pitches := range [][2]int{{8, 8}, {31, 37}} {
				for _, alias := range []bool{false, true} {
					back := make([]byte, 2048)
					for i := range back {
						back[i] = byte(i*37 + 13)
					}
					want := append([]byte(nil), back...)
					s, d := 32+so, 1024+do
					if alias {
						d = 32 + do
					}
					ss, ds := pitches[0], pitches[1]
					for y := range 8 {
						var row [8]byte
						for x := range 8 {
							row[x] = want[s+y*ss+x]
						}
						for x := range 8 {
							want[d+y*ds+x] = row[x]
						}
					}
					if !Copy8Rows(back[s:s+7*ss+8:s+7*ss+8], ss, back[d:d+7*ds+8:d+7*ds+8], ds) || !bytes.Equal(back, want) {
						t.Fatalf("source alignment%d destination%d pitches%v alias%v", so, do, pitches, alias)
					}
				}
			}
		}
	}
}

func TestCopy8RowsExactAliasGuard(t *testing.T) {
	for _, pitch := range []int{0, 1, 7, 8, 31} {
		span := 7*pitch + 8
		back := make([]byte, span+13)
		for i := range back {
			back[i] = byte(i*37 + 13)
		}
		before := bytes.Clone(back)
		if !Copy8Rows(back[:span:span], pitch, back[:span:span], pitch) || !bytes.Equal(back, before) {
			t.Fatal("valid exact alias changed")
		}
		for _, lens := range [][2]int{{span - 1, span}, {span, span - 1}, {span - 1, span - 1}} {
			if Copy8Rows(back[:lens[0]], pitch, back[:lens[1]], pitch) || !bytes.Equal(back, before) {
				t.Fatal("short exact alias bypassed guard")
			}
		}
	}
}
