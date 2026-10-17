//go:build goexperiment.simd && (amd64 || arm64) && !purego

package dsp

import (
	"testing"
	"unsafe"
)

func TestSADLimitX4FullWindowGuard(t *testing.T) {
	arena := make([]byte, 8192)
	out := (*[4]uint32)(unsafe.Pointer(&arena[1024]))
	limits := (*[4]int32)(unsafe.Pointer(&arena[6000]))
	base := [5]*byte{&arena[2000], &arena[3000], &arena[3600], &arena[4200], &arena[4800]}
	allowed := func(p [5]*byte, ss, rs int, l *[4]int32, o *[4]uint32) bool {
		return sadX4FullDisjoint(p[0], ss, p[1], p[2], p[3], p[4], rs, l, o)
	}
	if !allowed(base, 32, 35, limits, out) {
		t.Fatal("disjoint full windows declined")
	}
	for i := range base {
		span := 15*35 + 16
		if i == 0 {
			span = 15*32 + 16
		}
		for _, tc := range []struct {
			off  int
			want bool
		}{{1024 + 16, true}, {1024 + 15, false}, {1024 - span, true}, {1024 - span + 1, false}} {
			p := base
			p[i] = &arena[tc.off]
			if got := allowed(p, 32, 35, limits, out); got != tc.want {
				t.Fatalf("input%d offset%d got%v want%v", i, tc.off, got, tc.want)
			}
		}
		p := base
		p[i] = nil
		if allowed(p, 32, 35, limits, out) {
			t.Fatalf("nil input%d accepted", i)
		}
	}
	for _, tc := range []struct {
		off  int
		want bool
	}{{1024 + 16, true}, {1024 + 12, false}, {1024 - 16, true}, {1024 - 12, false}} {
		p := (*[4]int32)(unsafe.Pointer(&arena[tc.off]))
		if got := allowed(base, 32, 35, p, out); got != tc.want {
			t.Fatalf("limit offset%d got%v want%v", tc.off, got, tc.want)
		}
	}
	if allowed(base, 32, 35, limits, nil) {
		t.Fatal("nil output accepted")
	}
	for _, n := range []int{-1, 1 << 48, int(^uint(0) >> 1)} {
		if allowed(base, n, 35, limits, out) || allowed(base, 32, n, limits, out) {
			t.Fatalf("stride%d accepted", n)
		}
	}
}
