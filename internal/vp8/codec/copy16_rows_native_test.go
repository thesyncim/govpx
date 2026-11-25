package codec

import (
	"bytes"
	"fmt"
	"testing"
)

// Frozen exact pre-helper row-copy fallback, including original evaluation order.
func copy16FrozenPrior(src []byte, ss int, dst []byte, ds int) {
	for y := range 16 {
		copy(dst[y*ds:y*ds+16], src[y*ss:y*ss+16])
	}
}
func copy16ProbePanic(f func()) (panicText string) {
	defer func() {
		if v := recover(); v != nil {
			panicText = fmt.Sprint(v)
		}
	}()
	f()
	return
}
func TestCopy16FrozenPartialPanic(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, p := range [][2]int{{0, 0}, {0, 17}, {1, 7}, {7, 1}, {15, 16}, {16, 15}, {16, 16}, {17, 31}, {32, 37}, {1312, 1320}, {-1, 16}, {16, -1}, {1 << 30, 16}, {16, 1 << 30}, {maxInt, 16}, {16, maxInt}, {-maxInt - 1, 16}, {16, -maxInt - 1}} {
		sl, dl := 16, 16
		if p[0] >= 0 && p[0] <= 1312 {
			sl = 15*p[0] + 16
		}
		if p[1] >= 0 && p[1] <= 1320 {
			dl = 15*p[1] + 16
		}
		for _, shift := range []int{-33, -16, -1, 0, 1, 16, 33, 22000} {
			for mode := 0; mode < 8; mode++ {
				sw, dw := [2]int{sl, sl}, [2]int{dl, dl}
				switch mode {
				case 1:
					sw = [2]int{sl - 1, sl}
				case 2:
					dw = [2]int{dl - 1, dl}
				case 3:
					sw = [2]int{sl - 1, sl - 1}
				case 4:
					dw = [2]int{dl - 1, dl - 1}
				case 5:
					sw = [2]int{0, 0}
				case 6:
					dw = [2]int{0, 0}
				case 7:
					sw = [2]int{0, sl}
					dw = [2]int{0, dl}
				}
				got := make([]byte, 45000)
				for i := range got {
					got[i] = byte((i*37 + i/17 + 13) % 251)
				}
				want := bytes.Clone(got)
				so, do := 64, 64+shift
				gp := copy16ProbePanic(func() { copy16Public(got[so:so+sw[0]:so+sw[1]], p[0], got[do:do+dw[0]:do+dw[1]], p[1]) })
				wp := copy16ProbePanic(func() { copy16FrozenPrior(want[so:so+sw[0]:so+sw[1]], p[0], want[do:do+dw[0]:do+dw[1]], p[1]) })
				if gp != wp || !bytes.Equal(got, want) {
					t.Fatalf("pitch%v shift%d mode%d panic%q/%q backing%v", p, shift, mode, gp, wp, bytes.Equal(got, want))
				}
			}
		}
	}
}

func copy16Oracle(src []byte, ss int, dst []byte, ds int) {
	for y := 0; y < 16; y++ {
		d := dst[y*ds : y*ds+16]
		s := src[y*ss : y*ss+16]
		var row [16]byte
		for x := 0; x < 16; x++ {
			row[x] = s[x]
		}
		for x := 0; x < 16; x++ {
			d[x] = row[x]
		}
	}
}

func TestCopy16RowsPublicState(t *testing.T) {
	strides := [][2]int{{0, 0}, {0, 17}, {1, 7}, {7, 1}, {15, 16}, {16, 15}, {16, 16}, {17, 31}, {32, 37}, {1312, 1320}, {-1, 16}, {16, -1}, {1 << 30, 16}, {16, 1 << 30}, {int(^uint(0) >> 1), 16}}
	for _, pitch := range strides {
		for _, shift := range []int{-33, -16, -1, 0, 1, 16, 33, 22000} {
			for mode := 0; mode < 7; mode++ {
				ss, ds := pitch[0], pitch[1]
				so, do := 64, 64+shift
				original := make([]byte, 45000)
				for i := range original {
					original[i] = byte((i*37 + 13) % 251)
				}
				got, want := append([]byte(nil), original...), append([]byte(nil), original...)
				sl, dl := 16, 16
				if ss >= 0 && ss < 1<<20 {
					sl = 15*ss + 16
				}
				if ds >= 0 && ds < 1<<20 {
					dl = 15*ds + 16
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
				gp := run(got, copy16Public)
				wp := run(want, copy16Oracle)
				if gp != wp || !bytes.Equal(got, want) {
					t.Fatalf("pitch%v shift%d mode%d panic%v/%v backingequal%v", pitch, shift, mode, gp, wp, bytes.Equal(got, want))
				}
			}
		}
	}
}

var copy16RowsSink byte

func BenchmarkCopy16RowsNative(b *testing.B) {
	for _, p := range [][2]int{{16, 16}, {32, 37}, {1312, 1320}, {0, 0}, {1, 7}, {17, 31}} {
		for _, alias := range []bool{false, true} {
			if alias && p[0] != p[1] {
				continue
			}
			b.Run(fmt.Sprintf("pitch%d_%d/alias%v", p[0], p[1], alias), func(b *testing.B) {
				src, dst := make([]byte, 15*p[0]+16), make([]byte, 15*p[1]+16+2)
				for i := range src {
					src[i] = byte(i*37 + 11)
				}
				dst[0], dst[len(dst)-1] = 101, 103
				target := dst[1 : len(dst)-1]
				if alias {
					target = src
				}
				want := append([]byte(nil), target...)
				copy16Oracle(src, p[0], want, p[1])
				saved := append([]byte(nil), src...)
				copy16Public(src, p[0], target, p[1])
				if !bytes.Equal(target, want) || !bytes.Equal(src, saved) {
					b.Fatal("scalar mismatch")
				}
				b.ReportAllocs()
				for b.Loop() {
					copy16Public(src, p[0], target, p[1])
				}
				copy16RowsSink = target[0] ^ target[len(target)-1]
				if !bytes.Equal(target, want) || !bytes.Equal(src, saved) || dst[0] != 101 || dst[len(dst)-1] != 103 {
					b.Fatal("final scalar/canary/input mismatch")
				}
			})
		}
	}
}

func copy16Public(s []byte, ss int, d []byte, ds int) { copyMacroblockY(d, ds, s, ss) }
