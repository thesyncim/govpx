package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

func copy8Oracle(src []byte, ss int, dst []byte, ds int) {
	for y := 0; y < 8; y++ {
		d := dst[y*ds : y*ds+8]
		s := src[y*ss : y*ss+8]
		var row [8]byte
		for x := 0; x < 8; x++ {
			row[x] = s[x]
		}
		for x := 0; x < 8; x++ {
			d[x] = row[x]
		}
	}
}

func TestCopy8RowsPublicState(t *testing.T) {
	strides := [][2]int{{0, 0}, {0, 17}, {1, 7}, {7, 1}, {15, 8}, {8, 15}, {8, 8}, {17, 31}, {32, 37}, {1312, 1320}, {-1, 8}, {8, -1}, {1 << 30, 8}, {8, 1 << 30}, {int(^uint(0) >> 1), 8}}
	for _, pitch := range strides {
		for _, shift := range []int{-33, -8, -1, 0, 1, 8, 33, 22000} {
			for mode := 0; mode < 7; mode++ {
				ss, ds := pitch[0], pitch[1]
				so, do := 64, 64+shift
				original := make([]byte, 45000)
				for i := range original {
					original[i] = byte((i*37 + 13) % 251)
				}
				got, want := append([]byte(nil), original...), append([]byte(nil), original...)
				sl, dl := 8, 8
				if ss >= 0 && ss < 1<<20 {
					sl = 7*ss + 8
				}
				if ds >= 0 && ds < 1<<20 {
					dl = 7*ds + 8
				}
				if mode == 1 || mode == 3 {
					sl--
				}
				if mode == 2 || mode == 3 {
					dl--
				}
				if mode == 4 {
					sl = 0
				}
				if mode == 5 {
					dl = 0
				}
				run := func(back []byte, op func([]byte, int, []byte, int)) (panicked bool) {
					defer func() {
						if recover() != nil {
							panicked = true
						}
					}()
					s, d := back[so:so+sl], back[do:do+dl]
					if mode < 4 || mode == 6 {
						s = s[:len(s):len(s)]
						d = d[:len(d):len(d)]
					}
					op(s, ss, d, ds)
					return
				}
				gp := run(got, copy8Public)
				wp := run(want, copy8Oracle)
				if gp != wp || !bytes.Equal(got, want) {
					t.Fatalf("pitch%v shift%d mode%d panic%v/%v backingequal%v", pitch, shift, mode, gp, wp, bytes.Equal(got, want))
				}
			}
		}
	}
}

var copy8RowsSink byte

func BenchmarkCopy8RowsNative(b *testing.B) {
	for _, p := range [][2]int{{8, 8}, {32, 37}, {1312, 1320}, {0, 0}, {1, 7}, {17, 31}} {
		for _, alias := range []bool{false, true} {
			if alias && p[0] != p[1] {
				continue
			}
			b.Run(fmt.Sprintf("pitch%d_%d/alias%v", p[0], p[1], alias), func(b *testing.B) {
				src, dst := make([]byte, 7*p[0]+8), make([]byte, 7*p[1]+8+2)
				for i := range src {
					src[i] = byte(i*37 + 11)
				}
				dst[0], dst[len(dst)-1] = 101, 103
				target := dst[1 : len(dst)-1]
				if alias {
					target = src
				}
				want := append([]byte(nil), target...)
				copy8Oracle(src, p[0], want, p[1])
				saved := append([]byte(nil), src...)
				copy8Public(src, p[0], target, p[1])
				if !bytes.Equal(target, want) || !bytes.Equal(src, saved) {
					b.Fatal("scalar mismatch")
				}
				b.ReportAllocs()
				for b.Loop() {
					copy8Public(src, p[0], target, p[1])
				}
				copy8RowsSink = target[0] ^ target[len(target)-1]
				if !bytes.Equal(target, want) || !bytes.Equal(src, saved) || dst[0] != 101 || dst[len(dst)-1] != 103 {
					b.Fatal("final scalar/canary/input mismatch")
				}
			})
		}
	}
}

func copy8Public(s []byte, ss int, d []byte, ds int) { Copy8x8(s, ss, d, ds) }
