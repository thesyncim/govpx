//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"os"
	"testing"
)

func TestSubpelAxisWrappedCoefficients(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD unavailable")
	}
	identity := [2]int16{128, 0}
	filters := [][2]int16{{128, 0}, {112, 16}, {0, 128}, {1, -1}, {-32768, 32767}, {32767, 32767}, {-32768, -32768}, {255, 256}, {-129, 127}}
	for _, w := range []int{16, 32, 48, 64} {
		for _, h := range []int{1, 3, 16, 64} {
			for _, strides := range [][2]int{{0, 0}, {1, 3}, {w + 1, w + 7}} {
				ss, rs := strides[0], strides[1]
				sn, rn := h*ss+w+1, (h-1)*rs+w
				shared := make([]byte, max(3+sn, 5+rn)+19)
				for i := range shared {
					shared[i] = byte(i*73 + i/7 + 19)
				}
				src, ref := shared[3:3+sn], shared[5:5+rn]
				before := bytes.Clone(shared)
				for _, f := range filters {
					for _, vertical := range []bool{false, true} {
						fx, fy := f, identity
						if vertical {
							fx, fy = identity, f
						}
						gotSum, gotSSE := SubpelStats(src, ss, ref, rs, w, h, fx, fy)
						// Preserve the original SIMD int16 wrap semantics even for
						// coefficients outside the codec's normalized filter table.
						wantSum, wantSSE := subpelStatsKernel[[8]byte](src, ss, ref, rs, w, h, fx, fy)
						if gotSum != wantSum || gotSSE != wantSSE {
							t.Fatalf("w%d h%d strides%v vertical%v f%v got%d,%d want%d,%d", w, h, strides, vertical, f, gotSum, gotSSE, wantSum, wantSSE)
						}
					}
				}
				if !bytes.Equal(shared, before) {
					t.Fatal("aliased read-only inputs changed")
				}
			}
		}
	}
}

func TestSubpelAxisPreservesBilinearWindow(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD unavailable")
	}
	for _, w := range []int{16, 32, 48, 64} {
		for _, h := range []int{1, 16, 64} {
			ss, rs := w+1, w+7
			src := make([]byte, h*ss+w+1)
			ref := make([]byte, (h-1)*rs+w)
			for _, vertical := range []bool{false, true} {
				fx, fy := [2]int16{112, 16}, [2]int16{128, 0}
				if vertical {
					fx, fy = fy, fx
				}
				for _, shortSource := range []bool{false, true} {
					a, b := src, ref
					if shortSource {
						a = a[:len(a)-1]
					} else {
						b = b[:len(b)-1]
					}
					func() {
						defer func() {
							if recover() == nil {
								t.Fatalf("short bilinear window accepted w%d h%d vertical%v source%v", w, h, vertical, shortSource)
							}
						}()
						SubpelStats(a, ss, b, rs, w, h, fx, fy)
					}()
				}
			}
		}
	}
}

func TestSubpelAxisNarrowSumBoundary(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD unavailable")
	}
	for _, w := range []int{16, 32, 48, 64} {
		for _, h := range []int{1, 3, 63, 64, 65} {
			ss, rs := w+1, w+7
			src := bytes.Repeat([]byte{1}, h*ss+w+1)
			ref := bytes.Repeat([]byte{255}, (h-1)*rs+w)
			for _, vertical := range []bool{false, true} {
				fx, fy := [2]int16{-32768, 0}, [2]int16{128, 0}
				if vertical {
					fx, fy = fy, fx
				}
				// Wrapped interpolation produces -256, then the reference
				// difference is -511. At h=64 each separate narrow sum
				// reaches -32704; h=65 must use the unchanged wider path.
				sum, sse := SubpelStats(src, ss, ref, rs, w, h, fx, fy)
				if sum != -511*w*h || sse != uint32(511*511*w*h) {
					t.Fatalf("boundary w%d h%d vertical%v got%d,%d", w, h, vertical, sum, sse)
				}
			}
		}
	}
}
