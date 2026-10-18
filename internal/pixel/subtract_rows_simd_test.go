//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"os"
	"simd/archsimd"
	"slices"
	"testing"
	"unsafe"
)

func subtractRowsSelected(t *testing.T) {
	t.Helper()
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD disabled")
	}
}
func TestSubtractRowsScalar(t *testing.T) {
	subtractRowsSelected(t)
	for _, w := range []int{4, 8, 16, 32, 48, 64} {
		for _, h := range []int{1, 2, 3, 8, 16, 63, 64, 65} {
			for _, strides := range [][2]int{{0, 0}, {1, 3}, {w, w}, {w + 7, w + 9}} {
				for _, os := range []int{0, 1, w - 1, w, w + 11} {
					for pattern := range 3 {
						for _, alias := range []bool{false, true} {
							ss, ps := strides[0], strides[1]
							sn, pn, on := (h-1)*ss+w, (h-1)*ps+w, (h-1)*os+w
							sb, pb := make([]byte, 3+sn+13), make([]byte, 5+pn+17)
							if alias {
								shared := make([]byte, max(len(sb), len(pb)))
								sb, pb = shared, shared
							}
							for i := range sb {
								sb[i] = byte(i*37 + i/11 + 19)
							}
							for i := range pb {
								pb[i] = byte(i*71 + i/7 + 31)
							}
							if !alias && pattern == 1 {
								clear(sb)
								for i := range pb {
									pb[i] = 255
								}
							} else if !alias && pattern == 2 {
								for y := range h {
									copy(pb[5+y*ps:5+y*ps+w], sb[3+y*ss:3+y*ss+w])
								}
							}
							a, p := sb[3:3+sn], pb[5:5+pn]
							beforeA, beforeP := bytes.Clone(sb), bytes.Clone(pb)
							out := make([]int16, 1+on+7)
							for i := range out {
								out[i] = 12345
							}
							want := slices.Clone(out)
							wantNZ := SubtractScalar(a, ss, p, ps, want[1:1+on], os, w, h)
							if h <= 64 {
								nz, ok := subtractRowsBounded(a, ss, p, ps, out[1:1+on], os, w, h)
								if !ok || nz != wantNZ || !slices.Equal(out, want) {
									t.Fatalf("selected w%d h%d strides%v os%d alias%v pattern%d", w, h, strides, os, alias, pattern)
								}
							}
							for i := range out {
								out[i] = 12345
							}
							nz := Subtract(a, ss, p, ps, out[1:1+on], os, w, h)
							if nz != wantNZ || !slices.Equal(out, want) || !bytes.Equal(sb, beforeA) || !bytes.Equal(pb, beforeP) {
								t.Fatalf("public w%d h%d strides%v os%d alias%v pattern%d", w, h, strides, os, alias, pattern)
							}
						}
					}
				}
			}
		}
	}
}
func TestSubtractRowsDeclinesOutputAliases(t *testing.T) {
	subtractRowsSelected(t)
	for _, w := range []int{4, 8, 16} {
		for _, which := range []int{0, 1} {
			for _, delta := range []int{-4 * w, -2 * w, -1, 0, 1, w / 2, w, 2*w - 1, 2 * w, 4*w - 1, 4 * w} {
				run := func(legacy bool) ([]byte, bool) {
					backing := make([]byte, 512)
					for i := range backing {
						backing[i] = byte(i*37 + i/11)
					}
					// Aligned output starts at byte128; input starts at128+delta.
					// Their spans are2*w and4*w bytes. Test both input aliases.
					overlap := backing[128+delta : 128+delta+2*w]
					other := bytes.Repeat([]byte{93}, 2*w)
					src, pred := overlap, other
					if which == 1 {
						src, pred = other, overlap
					}
					out := unsafe.Slice((*int16)(unsafe.Pointer(&backing[128])), 2*w)
					before := bytes.Clone(backing)
					_, ok := subtractRowsBounded(src, w, pred, w, out, w, w, 2)
					actualOverlap := delta < 4*w && delta+2*w > 0
					if actualOverlap {
						if ok || !bytes.Equal(backing, before) {
							t.Fatalf("alias not declined without effects which%d delta%d", which, delta)
						}
					}
					// The direct selected probe can write on adjacent/non-overlap geometry.
					copy(backing, before)
					var nz bool
					if legacy {
						nz = subtractRowsLegacy(src, w, pred, w, out, w, w, 2)
					} else {
						nz = Subtract(src, w, pred, w, out, w, w, 2)
					}
					return backing, nz
				}
				a, an := run(false)
				b, bn := run(true)
				if an != bn || !bytes.Equal(a, b) {
					t.Fatalf("alias fallback differs which%d delta%d", which, delta)
				}
			}
		}
	}
}
func TestSubtractRowsShortPanicParity(t *testing.T) {
	subtractRowsSelected(t)
	for _, w := range []int{4, 8, 16} {
		for _, h := range []int{1, 2, 3, 16, 64} {
			for short := range 3 {
				run := func(legacy bool) ([]int16, bool) {
					ss, ps, os := 23, 29, 21
					src, pred, out := make([]byte, (h-1)*ss+w), make([]byte, (h-1)*ps+w), make([]int16, (h-1)*os+w)
					for i := range src {
						src[i] = 255
					}
					for i := range out {
						out[i] = 12345
					}
					a, b, d := src, pred, out
					if short == 0 {
						a = a[:len(a)-1]
					} else if short == 1 {
						b = b[:len(b)-1]
					} else {
						d = d[:len(d)-1]
					}
					before := slices.Clone(out)
					if _, ok := subtractRowsBounded(a, ss, b, ps, d, os, w, h); ok || !slices.Equal(out, before) {
						t.Fatal("short geometry changed output")
					}
					panicked := false
					func() {
						defer func() { panicked = recover() != nil }()
						if legacy {
							subtractRowsLegacy(a, ss, b, ps, d, os, w, h)
						} else {
							Subtract(a, ss, b, ps, d, os, w, h)
						}
					}()
					return out, panicked
				}
				a, ap := run(false)
				b, bp := run(true)
				if !ap || !bp || !slices.Equal(a, b) {
					t.Fatalf("partial-write panic differs h%d short%d", h, short)
				}
			}
		}
	}
}

func subtractRowsLegacy(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) bool {
	if !Enabled {
		return SubtractScalar(src, ss, pred, ps, out, os, w, h)
	}

	var bits archsimd.Int16x8
	tail := int16(0)
	for y := range h {
		a, b, dst := src[y*ss:], pred[y*ps:], out[y*os:]
		x := 0
		for ; x+8 <= w; x += 8 {
			d := widen8(load8(a[x:])).Sub(widen8(load8(b[x:])))
			d.Store(dst[x:])
			bits = bits.Or(d)
		}
		for ; x < w; x++ {
			d := int16(a[x]) - int16(b[x])
			dst[x] = d
			tail |= d
		}
	}
	return bits.ToBits().ReshapeToUint64s().GetElem(0)|bits.ToBits().ReshapeToUint64s().GetElem(1) != 0 || tail != 0
}
