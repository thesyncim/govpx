//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestCopy16RowsHandledAndDeclines(t *testing.T) {
	for _, p := range [][2]int{{0, 0}, {1, 7}, {7, 1}, {15, 16}, {16, 15}, {16, 16}, {17, 31}, {32, 37}, {1312, 1320}} {
		for _, shift := range []int{-33, -1, 0, 1, 33, 22000} {
			got := make([]byte, 45000)
			for i := range got {
				got[i] = byte(i*37 + 13)
			}
			want := append([]byte(nil), got...)
			so, do := 64, 64+shift
			for y := 0; y < 16; y++ {
				var row [16]byte
				for x := 0; x < 16; x++ {
					row[x] = want[so+y*p[0]+x]
				}
				for x := 0; x < 16; x++ {
					want[do+y*p[1]+x] = row[x]
				}
			}
			if !Copy16Rows(got[so:so+15*p[0]+16], p[0], got[do:do+15*p[1]+16], p[1]) {
				t.Fatalf("declined valid%v shift%d", p, shift)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("mismatch%v shift%d", p, shift)
			}
		}
	}
	for _, p := range [][2]int{{-1, 16}, {16, -1}, {1 << 30, 16}, {16, 1 << 30}, {int(^uint(0) >> 1), 16}} {
		got := make([]byte, 1024)
		for i := range got {
			got[i] = byte(i)
		}
		want := append([]byte(nil), got...)
		if Copy16Rows(got[:512], p[0], got[512:], p[1]) || !bytes.Equal(got, want) {
			t.Fatalf("invalid handled%v", p)
		}
	}
	for _, lens := range [][2]int{{255, 256}, {256, 255}, {0, 256}, {256, 0}, {0, 0}} {
		got := make([]byte, 1024)
		for i := range got {
			got[i] = byte(i)
		}
		want := append([]byte(nil), got...)
		if Copy16Rows(got[:lens[0]], 16, got[512:512+lens[1]], 16) || !bytes.Equal(got, want) {
			t.Fatalf("short handled%v", lens)
		}
	}
	if Copy16Rows(nil, 16, nil, 16) {
		t.Fatal("nil handled")
	}
}

func TestCopy16RowsUnaligned(t *testing.T) {
	for so := range 16 {
		for do := range 16 {
			for _, pitches := range [][2]int{{16, 16}, {31, 37}} {
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
					for y := range 16 {
						var row [16]byte
						for x := range 16 {
							row[x] = want[s+y*ss+x]
						}
						for x := range 16 {
							want[d+y*ds+x] = row[x]
						}
					}
					if !Copy16Rows(back[s:s+15*ss+16:s+15*ss+16], ss, back[d:d+15*ds+16:d+15*ds+16], ds) || !bytes.Equal(back, want) {
						t.Fatalf("source alignment%d destination%d pitches%v alias%v", so, do, pitches, alias)
					}
				}
			}
		}
	}
}
