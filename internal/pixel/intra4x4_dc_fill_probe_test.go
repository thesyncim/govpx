package pixel

import (
	"bytes"
	"runtime"
	"testing"
)

func dc4PixelOracle(dst []byte, ds int, above, left []byte) {
	sum := 0
	for i := 0; i < 4; i++ {
		sum += int(above[i]) + int(left[i])
	}
	value := byte((sum + 4) >> 3)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			dst[y*ds+x] = value
		}
	}
}
func TestIntra4x4DCFillAllSums(t *testing.T) {
	selected := Enabled && runtime.GOARCH == "arm64"
	for total := 0; total <= 2040; total++ {
		above, left := make([]byte, 4), make([]byte, 4)
		rem := total
		for i := 0; i < 8; i++ {
			v := min(255, rem)
			rem -= v
			if i < 4 {
				above[i] = byte(v)
			} else {
				left[i-4] = byte(v)
			}
		}
		for align := 0; align < 16; align++ {
			const ds = 7
			const span = 3*ds + 4
			got := bytes.Repeat([]byte{91}, align+span+17)
			want := bytes.Clone(got)
			if selected {
				dc4PixelOracle(want[align:align+span:align+span], ds, above, left)
			}
			ok := Intra4x4DCFill(got[align:align+span:align+span], ds, above, left)
			if ok != selected || !bytes.Equal(got, want) {
				t.Fatalf("sum%d align%d selected%v/%v", total, align, ok, selected)
			}
		}
	}
}
func TestIntra4x4DCFillAliases(t *testing.T) {
	selected := Enabled && runtime.GOARCH == "arm64"
	for _, ds := range []int{0, 1, 3, 4, 7, 37, 1312} {
		for align := 0; align < 16; align++ {
			for alias := 0; alias < 5; alias++ {
				span := 3*ds + 4
				off := 31 + align
				got := make([]byte, off+span+71)
				for i := range got {
					got[i] = byte(i*37 + i/7 + 11)
				}
				want := bytes.Clone(got)
				ao, lo := off+span+11, off+span+17
				if alias == 1 {
					ao = off
				}
				if alias == 2 {
					lo = off
				}
				if alias == 3 {
					ao = off
					lo = off
				}
				if alias == 4 {
					lo = ao + 1
				}
				if selected {
					dc4PixelOracle(want[off:off+span:off+span], ds, want[ao:ao+4:ao+4], want[lo:lo+4:lo+4])
				}
				ok := Intra4x4DCFill(got[off:off+span:off+span], ds, got[ao:ao+4:ao+4], got[lo:lo+4:lo+4])
				if ok != selected || !bytes.Equal(got, want) {
					t.Fatalf("ds%d align%d alias%d", ds, align, alias)
				}
			}
		}
	}
}
func TestIntra4x4DCFillDeclines(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, ds := range []int{-1, 0, 1, 3, 4, 7, 37, 1312, (1 << 30) - 1, 1 << 30, maxInt, -maxInt - 1} {
		span := 4
		if ds >= 0 && ds <= 1312 {
			span = 3*ds + 4
		}
		for _, win := range [][2]int{{0, 0}, {0, span}, {span - 1, span - 1}, {span - 1, span}, {span, span}} {
			for _, alen := range []int{0, 1, 3, 4} {
				for _, llen := range []int{0, 1, 3, 4} {
					got := bytes.Repeat([]byte{139}, span+17)
					want := bytes.Clone(got)
					above, left := []byte{1, 3, 5, 7}, []byte{9, 11, 13, 15}
					eligible := Enabled && runtime.GOARCH == "arm64" && uint(ds)>>30 == 0 && 3*ds+4 <= win[0] && alen >= 4 && llen >= 4
					if eligible {
						dc4PixelOracle(want[:win[0]:win[1]], ds, above, left)
					}
					ok := Intra4x4DCFill(got[:win[0]:win[1]], ds, above[:alen:alen], left[:llen:llen])
					if ok != eligible || !bytes.Equal(got, want) {
						t.Fatalf("ds%d win%v refs%d/%d", ds, win, alen, llen)
					}
				}
			}
		}
	}
}
