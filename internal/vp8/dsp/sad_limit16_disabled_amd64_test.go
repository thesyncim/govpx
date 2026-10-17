//go:build goexperiment.simd && amd64 && !purego

package dsp

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

func TestSADLimitPointerAMDDisabled(t *testing.T) {
	old := pixel.Enabled
	pixel.Enabled = false
	defer func() { pixel.Enabled = old }()
	for _, strides := range [][2]int{{0, 0}, {0, 19}, {19, 0}, {-16, -16}, {-19, 23}, {19, -23}, {-1, 3}, {1, -3}} {
		ss, rs := strides[0], strides[1]
		as, bs := ss, rs
		if as < 0 {
			as = -as
		}
		if bs < 0 {
			bs = -bs
		}
		a, b := make([]byte, 3+15*as+16), make([]byte, 5+15*bs+16)
		for i := range a {
			a[i] = byte(i*37 + 11)
		}
		for i := range b {
			b[i] = byte(i*19 + 53)
		}
		ac, bc := bytes.Clone(a), bytes.Clone(b)
		ao, bo := 3, 5
		if ss < 0 {
			ao += 15 * as
		}
		if rs < 0 {
			bo += 15 * bs
		}
		rows := func(h, limit int) int {
			sum := 0
			for y := range h {
				for x := range 16 {
					d := int(a[ao+y*ss+x]) - int(b[bo+y*rs+x])
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
		limits := []int{-1, 0, 1024, 65279, 65280, int(^uint(0) >> 1)}
		for _, h := range []int{1, 2, 4, 8, 15} {
			v := rows(h, 1<<20)
			limits = append(limits, v-1, v, v+1)
		}
		for _, limit := range limits {
			want := rows(16, limit)
			if got := SAD16x16LimitPtrFast(&a[ao], ss, &b[bo], rs, limit); got != want {
				t.Fatalf("strides%v limit%d got%d want%d", strides, limit, got, want)
			}
		}
		if !bytes.Equal(a, ac) || !bytes.Equal(b, bc) {
			t.Fatal("input changed")
		}
	}
}
