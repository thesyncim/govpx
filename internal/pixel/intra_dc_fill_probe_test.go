package pixel

import (
	"bytes"
	"runtime"
	"testing"
)

func TestIntraDCFillSelectedAndDeclines(t *testing.T) {
	selected := Enabled && runtime.GOARCH == "arm64"
	maxInt := int(^uint(0) >> 1)
	for _, n := range []int{-1, 0, 4, 8, 16, 32, maxInt} {
		for _, ds := range []int{-1, 0, 1, 16, 37, 1 << 30, maxInt, -maxInt - 1} {
			for mode := range 4 {
				up, lf := mode&1 != 0, mode&2 != 0
				for _, nref := range []int{0, 7, 8, 15, 16} {
					dst := make([]byte, 1024)
					for i := range dst {
						dst[i] = 19
					}
					before := bytes.Clone(dst)
					above, left := make([]byte, nref), make([]byte, nref)
					for i := range above {
						above[i] = 251
						left[i] = 255
					}
					eligible := selected && (n == 8 || n == 16) && uint(ds)>>30 == 0 && (n-1)*ds+n <= len(dst) && (!up || len(above) >= n) && (!lf || len(left) >= n)
					got := IntraDCFill(dst, ds, above, left, n, up, lf)
					if got != eligible {
						t.Fatalf("n%d ds%d mode%d ref%d got%v want%v", n, ds, mode, nref, got, eligible)
					}
					if !got && !bytes.Equal(dst, before) {
						t.Fatal("decline wrote")
					}
				}
			}
		}
	}
	// Every incomplete destination length must decline before any write, even with spare capacity.
	for _, n := range []int{8, 16} {
		need := (n-1)*37 + n
		for length := 0; length < need; length++ {
			storage := make([]byte, need+11)
			for i := range storage {
				storage[i] = 77
			}
			before := bytes.Clone(storage)
			if IntraDCFill(storage[:length], 37, make([]byte, n), make([]byte, n), n, true, true) || !bytes.Equal(storage, before) {
				t.Fatalf("shortlen%d n%d accepted/wrote", length, n)
			}
		}
	}
}
