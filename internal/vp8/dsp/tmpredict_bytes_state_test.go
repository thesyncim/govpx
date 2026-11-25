package dsp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

var tmBytesNativeSink uint64

// Scalar arithmetic oracle; the benchmark's reference windows are disjoint.
func tmBytesNativeOracle(dst []byte, ds, w int, a, l []byte, top byte) {
	for y := 0; y < w; y++ {
		for x := 0; x < w; x++ {
			v := int(a[x]) + int(l[y]) - int(top)
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			dst[y*ds+x] = byte(v)
		}
	}
}

func BenchmarkTMPredictRowsNative(b *testing.B) {
	for _, w := range []int{4, 8, 16} {
		for _, ds := range []int{w, 37, 1312} {
			for _, pattern := range []string{"flat", "mixed", "clipped"} {
				b.Run(fmt.Sprintf("%dx%d/pitch%d/%s", w, w, ds, pattern), func(b *testing.B) {
					span := (w-1)*ds + w
					storage := bytes.Repeat([]byte{0x6b}, span+31)
					above, left := make([]byte, w+31), make([]byte, w+31)
					top := byte(128)
					for i := range above {
						switch pattern {
						case "flat":
							above[i], left[i] = 128, 128
						case "mixed":
							above[i], left[i] = byte(i*37+11), byte(i*29+251)
							top = 137
						case "clipped":
							above[i], left[i] = byte((i&1)*255), byte((i&1)*255)
						}
					}
					a, l := above[7:7+w:7+w], left[11:11+w:11+w]
					abefore, lbefore := bytes.Clone(above), bytes.Clone(left)
					expected := bytes.Clone(storage)
					dst := storage[7 : 7+span : 7+span]
					tmBytesNativeOracle(expected[7:7+span:7+span], ds, w, a, l, top)
					// Preflight and post-loop comparisons include every output byte and canary.
					if w == 4 {
						Intra4x4TMPredict(dst, ds, a, l, top)
					} else if w == 8 {
						IntraTMPredict8x8(dst, ds, a, l, top)
					} else {
						IntraTMPredict16x16(dst, ds, a, l, top)
					}
					if !bytes.Equal(storage, expected) || !bytes.Equal(above, abefore) || !bytes.Equal(left, lbefore) {
						b.Fatal("preflight full state mismatch")
					}
					b.ReportAllocs()
					for b.Loop() {
						if w == 4 {
							Intra4x4TMPredict(dst, ds, a, l, top)
						} else if w == 8 {
							IntraTMPredict8x8(dst, ds, a, l, top)
						} else {
							IntraTMPredict16x16(dst, ds, a, l, top)
						}
					}
					if !bytes.Equal(storage, expected) || !bytes.Equal(above, abefore) || !bytes.Equal(left, lbefore) {
						b.Fatal("full output/input/canary mismatch")
					}
					var checksum uint64
					for y := 0; y < w; y++ {
						for x := 0; x < w; x++ {
							checksum += uint64(dst[y*ds+x])
						}
					}
					tmBytesNativeSink = checksum
				})
			}
		}
	}
}

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

// This common reference test uses only complete windows, disjoint input/output
// planes and nonoverlapping output rows. Historical assembly and current Go
// kernels have different malformed-window/alias contracts; those remain covered
// by the current-only frozen-state test. This file imports no pixel internals.
func TestTMPredictRowsNativeState(t *testing.T) {
	for _, w := range []int{4, 8, 16} {
		for _, ds := range []int{w, 37, 1312} {
			for _, pattern := range []string{"flat", "mixed", "clipped"} {
				for _, align := range []int{0, 1, 7, 15} {
					for _, top := range []byte{0, 128, 255} {
						span := (w-1)*ds + w
						off, ao, lo := 7+align, 7+align, 11+15-align
						storage := bytes.Repeat([]byte{0x6b}, off+span+31)
						above, left := make([]byte, w+47), make([]byte, w+47)
						for i := range above {
							above[i], left[i] = byte(i*37+11), byte(i*29+251)
						}
						for i := 0; i < w; i++ {
							switch pattern {
							case "flat":
								above[ao+i], left[lo+i] = 128, 128
							case "clipped":
								above[ao+i], left[lo+i] = byte((i&1)*255), byte((i&1)*255)
							}
						}
						a, l := above[ao:ao+w:ao+w], left[lo:lo+w:lo+w]
						abefore, lbefore := bytes.Clone(above), bytes.Clone(left)
						expected := bytes.Clone(storage)
						for y := 0; y < w; y++ {
							for x := 0; x < w; x++ {
								v := int(a[x]) + int(l[y]) - int(top)
								if v < 0 {
									v = 0
								}
								if v > 255 {
									v = 255
								}
								expected[off+y*ds+x] = byte(v)
							}
						}
						dst := storage[off : off+span : off+span]
						if w == 4 {
							Intra4x4TMPredict(dst, ds, a, l, top)
						} else if w == 8 {
							IntraTMPredict8x8(dst, ds, a, l, top)
						} else {
							IntraTMPredict16x16(dst, ds, a, l, top)
						}
						if !bytes.Equal(storage, expected) || !bytes.Equal(above, abefore) || !bytes.Equal(left, lbefore) {
							t.Fatalf("w%d ds%d pattern%s align%d top%d full state mismatch", w, ds, pattern, align, top)
						}
					}
				}
			}
		}
	}
}
