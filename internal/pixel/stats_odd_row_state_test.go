//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"fmt"
	"os"
	"testing"
)

type oddRowResult struct {
	sum       int
	sse       uint32
	panicText string
}

func oddRowRun(fn func() (int, uint32)) (r oddRowResult) {
	defer func() {
		if v := recover(); v != nil {
			r.panicText = fmt.Sprint(v)
		}
	}()
	r.sum, r.sse = fn()
	return
}
func oddRowCheck(t *testing.T, blob []byte, a []byte, ss int, b []byte, rs, h int, scalar bool) {
	t.Helper()
	before := bytes.Clone(blob)
	got := oddRowRun(func() (int, uint32) { return Stats(a, ss, b, rs, 8, h) })
	want := oddRowRun(func() (int, uint32) { return frozenOddRowStats(a, ss, b, rs, 8, h) })
	if got != want || !bytes.Equal(blob, before) {
		t.Fatalf("enabled%v ss%d rs%d h%d lens%d/%d got%+v want%+v backing=%v", Enabled, ss, rs, h, len(a), len(b), got, want, bytes.Equal(blob, before))
	}
	if scalar {
		ind := oddRowRun(func() (int, uint32) { return StatsScalar(a, ss, b, rs, 8, h) })
		if got != ind {
			t.Fatalf("scalar ss%d rs%d h%d got%+v scalar%+v", ss, rs, h, got, ind)
		}
	}
}
func oddRowRequire(t *testing.T) {
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && !Enabled {
		t.Fatal("required SIMD not selected")
	}
}
func TestStatsOddRowAllBytePairs(t *testing.T) {
	oddRowRequire(t)
	count := 0
	for aa := 0; aa < 256; aa++ {
		for bb := 0; bb < 256; bb++ {
			blob := make([]byte, 40)
			for i := 0; i < 8; i++ {
				blob[1+i] = byte(aa)
				blob[19+i] = byte(bb)
			}
			oddRowCheck(t, blob, blob[1:9:9], 0, blob[19:27:27], 0, 1, true)
			count++
		}
	}
	t.Logf("enabled%v exhaustive byte pairs=%d", Enabled, count)
}
func oddRowRows(t *testing.T, h, ss, rs, align, pattern int, alias bool) {
	sn, rn := 8, 8
	if h > 0 {
		sn = (h-1)*ss + 8
		rn = (h-1)*rs + 8
	}
	ao := 16 + align
	bo := ao + sn + 31
	if alias {
		bo = ao + 3
	}
	blob := make([]byte, max(ao+sn, bo+rn)+17)
	for i := range blob {
		switch pattern {
		case 0:
			blob[i] = 0
		case 1:
			blob[i] = 255
		case 2:
			blob[i] = byte(i*73 + i/11 + 19)
		}
	}
	if pattern == 3 {
		for i := 0; i < sn; i++ {
			blob[ao+i] = 255
		}
	}
	oddRowCheck(t, blob, blob[ao:ao+sn:ao+sn], ss, blob[bo:bo+rn:bo+rn], rs, h, true)
}
func TestStatsOddRowRowsAndTall(t *testing.T) {
	oddRowRequire(t)
	count := 0
	for _, h := range []int{-1, 0, 1, 2, 3, 7, 8, 9, 63, 64, 65} {
		for _, strides := range [][2]int{{0, 0}, {1, 2}, {7, 7}, {8, 8}, {13, 17}, {37, 41}} {
			for _, align := range []int{0, 1, 7, 15} {
				for _, alias := range []bool{false, true} {
					for pattern := 0; pattern < 4; pattern++ {
						oddRowRows(t, h, strides[0], strides[1], align, pattern, alias)
						count++
					}
				}
			}
		}
	}
	for _, h := range []int{4095, 4096, 4097, 8191, 8192, 8193} {
		for _, strides := range [][2]int{{0, 0}, {8, 8}, {13, 17}} {
			for _, align := range []int{0, 7} {
				for _, alias := range []bool{false, true} {
					for _, pattern := range []int{0, 2, 3} {
						oddRowRows(t, h, strides[0], strides[1], align, pattern, alias)
						count++
					}
				}
			}
		}
	}
	for _, h := range []int{65535, 65536, 65537} {
		for _, align := range []int{0, 15} {
			for _, pattern := range []int{0, 2, 3} {
				oddRowRows(t, h, 0, 0, align, pattern, false)
				count++
			}
		}
	}
	t.Logf("odd/even/tall/minspan/readonly-alias rows=%d", count)
}
func TestStatsOddRowMalformedWindows(t *testing.T) {
	oddRowRequire(t)
	mx := int(^uint(0) >> 1)
	count := 0
	for _, h := range []int{-1, 0, 1, 2, 3, 7, 65} {
		for _, ss := range []int{0, 1, 7, 8, 13, -1, -8, 1 << 30, 1 << 62, mx, -mx - 1} {
			for _, rs := range []int{0, ss} {
				for _, na := range []int{0, 1, 4, 7, 8, 9, 16, 31, 64} {
					for _, nb := range []int{0, 1, 7, 8, 9, 64} {
						for _, clip := range []bool{false, true} {
							blob := make([]byte, 180)
							for i := range blob {
								blob[i] = byte(i*31 + 7)
							}
							ca, cb := 64, 64
							if clip {
								ca, cb = na, nb
							}
							oddRowCheck(t, blob, blob[1:1+na:1+ca], ss, blob[99:99+nb:99+cb], rs, h, false)
							count++
						}
					}
				}
			}
		}
	}
	t.Logf("negative/extreme stride/shortlen/sparecap/panic-order cases=%d", count)
}
