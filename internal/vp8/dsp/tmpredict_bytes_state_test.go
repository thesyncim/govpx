package dsp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
)

// Frozen original call order, with independent integer arithmetic. SIMD
// captures left[y] once and loads/stores each four-byte packet atomically;
// the DSP disabled scalar path prechecks the full window and reads above per pixel.
// This differs from the pixel-level scalar API, whose left read is per pixel.
func tmPublicPrior(dst []byte, ds int, a, l []byte, top byte, w int) {
	if w == 4 {
		_ = a[3]
		_ = l[3]
		_ = dst[3*ds+3]
	}
	if !pixel.Enabled {
		_ = a[w-1]
		_ = l[w-1]
		_ = dst[(w-1)*ds+w-1]
		for y := range w {
			delta := int(l[y]) - int(top)
			for x := range w {
				dst[y*ds+x] = byte(min(max(int(a[x])+delta, 0), 255))
			}
		}
		return
	}
	for y := range w {
		delta := int(l[y]) - int(top)
		for x := 0; x < w; x += 4 {
			word := binary.LittleEndian.Uint32(a[x:])
			var packed uint32
			for k := 0; k < 4; k++ {
				packed |= uint32(byte(min(max(int(byte(word>>uint(8*k)))+delta, 0), 255))) << uint(8*k)
			}
			binary.LittleEndian.PutUint32(dst[y*ds+x:], packed)
		}
	}
}
func tmPublicCall(dst []byte, ds int, a, l []byte, top byte, w int) {
	if w == 4 {
		Intra4x4TMPredict(dst, ds, a, l, top)
	} else if w == 8 {
		IntraTMPredict8x8(dst, ds, a, l, top)
	} else {
		IntraTMPredict16x16(dst, ds, a, l, top)
	}
}
func tmPublicPanic(f func()) (text string) {
	defer func() {
		if p := recover(); p != nil {
			text = fmt.Sprint(p)
		}
	}()
	f()
	return
}

func TestTMPredictPublicFullState(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, w := range []int{4, 8, 16} {
		for _, ds := range []int{-1, 0, 1, w - 1, w, 37, 1312, 1 << 30, maxInt} {
			span := 256
			if ds >= 0 && ds <= 1312 {
				span = (w-1)*ds + w
			}
			for _, align := range []int{0, 1, 7, 15} {
				for _, length := range []int{0, 1, span - 1, span} {
					for _, alen := range []int{w - 1, w} {
						for _, llen := range []int{w - 1, w} {
							for _, clip := range []bool{false, true} {
								for alias := 0; alias < 5; alias++ {
									off := 64 + align
									ao, lo := off+span+37, off+span+89
									switch alias {
									case 1:
										ao = off
									case 2:
										lo = off
									case 3:
										ao = off
										lo = off + 1
									case 4:
										lo = ao + 1
									}
									got := make([]byte, off+span+2*w+157)
									for i := range got {
										got[i] = byte(i*37 + i/7 + 11)
									}
									want := bytes.Clone(got)
									dc, ac, lc := span, w, w
									if clip {
										dc, ac, lc = length, alen, llen
									}
									gp := tmPublicPanic(func() {
										tmPublicCall(got[off:off+length:off+dc], ds, got[ao:ao+alen:ao+ac], got[lo:lo+llen:lo+lc], 128, w)
									})
									wp := tmPublicPanic(func() {
										tmPublicPrior(want[off:off+length:off+dc], ds, want[ao:ao+alen:ao+ac], want[lo:lo+llen:lo+lc], 128, w)
									})
									if gp != wp || !bytes.Equal(got, want) {
										t.Fatalf("w%d ds%d align%d dst%d/%d a%d/%d l%d/%d alias%d panic%q/%q", w, ds, align, length, dc, alen, ac, llen, lc, alias, gp, wp)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}
