package dsp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
)

// Independent integer reference: explicitly wrap input+4 to signed32 before
// shifting. No production rounding/clamp or SIMD helper is used.
func dcPairReferenceDelta(x int32) int {
	u := uint32(x) + uint32(4)
	v := int64(u)
	if u >= 1<<31 {
		v -= 1 << 32
	}
	v >>= 3
	if v < -255 {
		v = -255
	}
	if v > 255 {
		v = 255
	}
	return int(v)
}
func dcPairReferenceBlock(delta int, pred []byte, ps int, dst []byte, ds int) {
	for y := range 4 {
		if !pixel.Enabled {
			// Exact original DSP scalar row-check/capture order, distinct from
			// pixel's per-pixel scalar loop on invalid or overlapping windows.
			d := dst[y*ds : y*ds+4 : y*ds+4]
			p := pred[y*ps : y*ps+4 : y*ps+4]
			for x := range 4 {
				d[x] = byte(min(max(int(p[x])+delta, 0), 255))
			}
		} else {
			// Original enabled fallback evaluates destination slice first, then
			// captures all four prediction bytes before the destination word store.
			d := dst[y*ds:]
			a := binary.LittleEndian.Uint32(pred[y*ps:])
			var word uint32
			for x := range 4 {
				v := byte(min(max(int(byte(a>>uint(8*x)))+delta, 0), 255))
				word |= uint32(v) << uint(8*x)
			}
			binary.LittleEndian.PutUint32(d, word)
		}
	}
}
func dcPairOriginalReference(c0, c1 int32, pred []byte, ps int, dst []byte, ds int) {
	// The original fast pair is selected only for full windows with independent
	// rows and either exact in-place/equal pitches or disjoint buffers. In that
	// domain sequential four-pixel blocks have identical final state; every
	// other case already uses these two sequential original blocks.
	d0, d1 := dcPairReferenceDelta(c0), dcPairReferenceDelta(c1)
	dcPairReferenceBlock(d0, pred, ps, dst, ds)
	dcPairReferenceBlock(d1, pred[4:], ps, dst[4:], ds)
}
func dcPairPanic(f func()) (text string) {
	defer func() {
		if p := recover(); p != nil {
			text = fmt.Sprint(p)
		}
	}()
	f()
	return
}

func TestDCPairNativePublicState(t *testing.T) { dcPairNativeRunStates(t) }
func dcPairNativeRunStates(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	strides := [][2]int{{8, 8}, {9, 37}, {37, 9}, {1312, 1312}, {0, 0}, {1, 1}, {3, 7}, {-1, 8}, {8, -1}, {1 << 30, 8}, {8, maxInt}, {-maxInt - 1, 8}, {maxInt, -maxInt - 1}}
	values := [][2]int32{{0, 0}, {87, -99}, {2040, -2040}, {-2147483648, 2147483647}, {2147483644, -2147483645}, {99999999, -9999999}}
	checks := 0
	for _, strides := range strides {
		ps, ds := strides[0], strides[1]
		pn, dn := 64, 64
		if ps >= 0 && ps <= 1312 {
			pn = 3*ps + 8
		}
		if ds >= 0 && ds <= 1312 {
			dn = 3*ds + 8
		}
		for _, align := range []int{0, 1, 7, 15} {
			for _, pl := range []int{0, 1, 7, pn - 1, pn} {
				for _, dl := range []int{0, 1, 7, dn - 1, dn} {
					for _, clip := range []bool{false, true} {
						for mode := 0; mode < 5; mode++ {
							po, do := 64+align, 64+align+pn+17
							switch mode {
							case 1:
								do = po
							case 2:
								do = po + 1
							case 3:
								do = po - 1
							case 4:
								do = po + 4
							}
							initial := make([]byte, max(po+pn, do+dn)+47)
							for i := range initial {
								initial[i] = byte(i*37 + i/5 + 11)
							}
							pc, dc := pn, dn
							if clip {
								pc, dc = pl, dl
							}
							for _, cs := range values {
								want := bytes.Clone(initial)
								wp := dcPairPanic(func() { dcPairOriginalReference(cs[0], cs[1], want[po:po+pl:po+pc], ps, want[do:do+dl:do+dc], ds) })
								calls := []func(int32, int32, []byte, int, []byte, int){DCOnlyIDCT4x4AddPairInt32}
								if pixel.DCPairNative {
									calls = append(calls, pixel.DCPairInt32Native)
								}
								for entry, call := range calls {
									got := bytes.Clone(initial)
									gp := dcPairPanic(func() { call(cs[0], cs[1], got[po:po+pl:po+pc], ps, got[do:do+dl:do+dc], ds) })
									checks++
									if gp != wp || !bytes.Equal(got, want) {
										t.Fatalf("Enabled%v entry%d strides%v align%d lengths%d/%d caps%d/%d mode%d dc%v panic%q/%q", pixel.Enabled, entry, strides, align, pl, dl, pc, dc, mode, cs, gp, wp)
									}
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("Enabled=%v native=%v full-backing/panic checks=%d", pixel.Enabled, pixel.DCPairNative, checks)
}
