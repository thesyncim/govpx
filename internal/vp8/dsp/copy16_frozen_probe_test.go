package dsp

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
