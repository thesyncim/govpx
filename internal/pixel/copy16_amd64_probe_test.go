package pixel

import (
	"bytes"
	"os"
	"runtime"
	"testing"
)

func copy16ProbeOracle(src []byte, ss int, dst []byte, ds int) {
	for y := 0; y < 16; y++ {
		var row [16]byte
		for x := 0; x < 16; x++ {
			row[x] = src[y*ss+x]
		}
		for x := 0; x < 16; x++ {
			dst[y*ds+x] = row[x]
		}
	}
}
func copy16ProbeEnabled(t *testing.T) bool {
	t.Helper()
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && !Enabled {
		t.Fatal("SIMD selected run did not enable backend")
	}
	if os.Getenv("GOVPX_REQUIRE_SCALAR") == "1" && Enabled {
		t.Fatal("CPU-off run did not disable backend")
	}
	return Enabled && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
}
func TestCopy16AMDProbeRowsAndAliases(t *testing.T) {
	selected := copy16ProbeEnabled(t)
	for _, p := range [][2]int{{0, 0}, {0, 17}, {1, 7}, {7, 1}, {15, 16}, {16, 15}, {16, 16}, {17, 31}, {32, 37}, {1312, 1320}} {
		for _, shift := range []int{-33, -16, -1, 0, 1, 16, 33, 22000} {
			for _, align := range []int{0, 1, 7, 15} {
				so, do := 64+align, 64+align+shift
				sl, dl := 15*p[0]+16, 15*p[1]+16
				got := make([]byte, 45000)
				for i := range got {
					got[i] = byte((i*37 + i/17 + 13) % 251)
				}
				want := bytes.Clone(got)
				if selected {
					copy16ProbeOracle(want[so:so+sl:so+sl], p[0], want[do:do+dl:do+dl], p[1])
				}
				handled := Copy16Rows(got[so:so+sl:so+sl], p[0], got[do:do+dl:do+dl], p[1])
				if handled != selected || !bytes.Equal(got, want) {
					t.Fatalf("pitch%v shift%d align%d handled%v/%v", p, shift, align, handled, selected)
				}
			}
		}
	}
}
func TestCopy16AMDProbeEveryAlignment(t *testing.T) {
	selected := copy16ProbeEnabled(t)
	for so := 0; so < 16; so++ {
		for do := 0; do < 16; do++ {
			for _, p := range [][2]int{{16, 16}, {31, 37}} {
				for _, alias := range []bool{false, true} {
					got := make([]byte, 2048)
					for i := range got {
						got[i] = byte((i*37 + i/17 + 13) % 251)
					}
					want := bytes.Clone(got)
					s, d := 32+so, 1024+do
					if alias {
						d = 32 + do
					}
					sl, dl := 15*p[0]+16, 15*p[1]+16
					if selected {
						copy16ProbeOracle(want[s:s+sl:s+sl], p[0], want[d:d+dl:d+dl], p[1])
					}
					handled := Copy16Rows(got[s:s+sl:s+sl], p[0], got[d:d+dl:d+dl], p[1])
					if handled != selected || !bytes.Equal(got, want) {
						t.Fatalf("so%d do%d pitch%v alias%v", so, do, p, alias)
					}
				}
			}
		}
	}
}
func TestCopy16AMDProbeBoundsAndDisabled(t *testing.T) {
	selected := copy16ProbeEnabled(t)
	maxInt := int(^uint(0) >> 1)
	for _, p := range [][2]int{{0, 0}, {0, 17}, {1, 7}, {15, 16}, {16, 16}, {17, 31}, {1312, 1320}, {-1, 16}, {16, -1}, {1 << 30, 16}, {16, 1 << 30}, {(1 << 30) - 1, 16}, {16, (1 << 30) - 1}, {maxInt, 16}, {16, maxInt}, {-maxInt - 1, 16}, {16, -maxInt - 1}} {
		sl, dl := 16, 16
		if p[0] >= 0 && p[0] <= 1312 {
			sl = 15*p[0] + 16
		}
		if p[1] >= 0 && p[1] <= 1320 {
			dl = 15*p[1] + 16
		}
		for _, sw := range [][2]int{{0, 0}, {0, sl}, {sl - 1, sl - 1}, {sl - 1, sl}, {sl, sl}} {
			for _, dw := range [][2]int{{0, 0}, {0, dl}, {dl - 1, dl - 1}, {dl - 1, dl}, {dl, dl}} {
				got := make([]byte, 45000)
				for i := range got {
					got[i] = byte((i*37 + i/17 + 13) % 251)
				}
				want := bytes.Clone(got)
				const so = 64
				const do = 22064
				eligible := selected && (uint(p[0])|uint(p[1]))>>30 == 0 && 15*p[0]+16 <= sw[0] && 15*p[1]+16 <= dw[0]
				if eligible {
					copy16ProbeOracle(want[so:so+sw[0]:so+sw[1]], p[0], want[do:do+dw[0]:do+dw[1]], p[1])
				}
				handled := Copy16Rows(got[so:so+sw[0]:so+sw[1]], p[0], got[do:do+dw[0]:do+dw[1]], p[1])
				if handled != eligible || !bytes.Equal(got, want) {
					t.Fatalf("pitch%v src%v dst%v handled%v/%v", p, sw, dw, handled, eligible)
				}
			}
		}
	}
	if Copy16Rows(nil, 16, nil, 16) {
		t.Fatal("nil handled")
	}
}
