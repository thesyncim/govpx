package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var sadLimitNativeSink int

func sadLimitRowsOracle(a []byte, ss int, b []byte, rs, h, limit int) int {
	sum := 0
	for y := range h {
		for x := range 16 {
			d := int(a[y*ss+x]) - int(b[y*rs+x])
			if d < 0 {
				d = -d
			}
			sum += d
		}
		if sum > limit {
			return sum
		}
	}
	return sum
}

func TestSADLimitPointerRows(t *testing.T) {
	for _, stride := range []int{0, 1, 15, 16, 19, 1312} {
		for mode := range 3 {
			a, ref := make([]byte, 3+15*stride+16)[3:], make([]byte, 5+15*(stride+3)+16)[5:]
			for i := range a {
				if mode == 0 {
					a[i] = byte(i*37 + 11)
				} else if mode == 1 {
					a[i] = 255
				}
			}
			for i := range ref {
				if mode == 0 {
					ref[i] = byte(i*19 + 53)
				} else if mode == 2 {
					ref[i] = 255
				}
			}
			ac, rc := bytes.Clone(a), bytes.Clone(ref)
			limits := []int{-1, 0, 1024, 65279, 65280, 65281, 1 << 20}
			for _, h := range []int{1, 2, 4, 8, 15} {
				prefix := sadLimitRowsOracle(a, stride, ref, stride+3, h, 1<<20)
				limits = append(limits, prefix-1, prefix, prefix+1)
			}
			for _, limit := range limits {
				want := sadLimitRowsOracle(a, stride, ref, stride+3, 16, limit)
				if got := SAD16x16LimitPtrFast(&a[0], stride, &ref[0], stride+3, limit); got != want {
					t.Fatalf("stride%d mode%d limit%d got%d want%d", stride, mode, limit, got, want)
				}
			}
			if !bytes.Equal(a, ac) || !bytes.Equal(ref, rc) {
				t.Fatal("input changed")
			}
		}
	}
}

func BenchmarkSADLimitNative(b *testing.B) {
	for _, pitch := range []int{32, 1312} {
		for _, extreme := range []bool{false, true} {
			for _, tc := range []struct {
				name  string
				limit int
			}{
				{"bound", 16 * 16 * 255}, {"wide", 1 << 20}, {"near", 16*16*255 - 1}, {"early", 1024}, {"middle", 16384},
			} {
				b.Run(fmt.Sprintf("pitch%d/extreme%v/%s", pitch, extreme, tc.name), func(b *testing.B) {
					ss, rs := pitch, pitch+32
					a, ref := make([]byte, 3+15*ss+16)[3:], make([]byte, 5+15*rs+16)[5:]
					for i := range a {
						if !extreme {
							a[i] = byte(i*37 + 11)
						}
					}
					for i := range ref {
						if extreme {
							ref[i] = 255
						} else {
							ref[i] = byte(i*19 + 53)
						}
					}
					want := sadLimitRowsOracle(a, ss, ref, rs, 16, tc.limit)
					got := 0
					b.ReportAllocs()
					for b.Loop() {
						got = SAD16x16LimitPtrFast(&a[0], ss, &ref[0], rs, tc.limit)
					}
					if got != want {
						b.Fatalf("got %d want %d", got, want)
					}
					sadLimitNativeSink = got
				})
			}
		}
	}
}
