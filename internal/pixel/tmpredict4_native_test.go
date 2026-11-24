package pixel

import (
	"bytes"
	"testing"
)

func TestTMPredict4TileFullBackingAliases(t *testing.T) {
	const w = 4
	for _, ds := range []int{0, 1, 2, 3, 4, 7, 37, 1312} {
		for align := 0; align < 16; align++ {
			for mode := 0; mode < 10; mode++ {
				span, off := 3*ds+w, 64+align
				ao, lo := off+span+7, off+span+17
				switch mode {
				case 1:
					ao = off
				case 2:
					lo = off
				case 3:
					ao, lo = off, off
				case 4:
					lo = off + 1
				case 5:
					ao = off + 1
				case 6:
					lo = ao + 1
				case 7:
					ao, lo = off-w, off+span
				case 8:
					ao, lo = off+span, off-w
				case 9:
					ao, lo = off-w-1, off-w
				}
				initial := make([]byte, off+span+47)
				for i := range initial {
					initial[i] = byte(i*37 + i/3)
				}
				want := bytes.Clone(initial)
				top := byte(align * 17)
				tmBytesPrior(want[off:off+span:off+span], ds, want[ao:ao+w:ao+w], want[lo:lo+w:lo+w], top, w)
				for entry, call := range []func([]byte, int, []byte, []byte, byte){tm4NativeUnchecked, tm4NativeChecked, func(d []byte, s int, a, l []byte, top byte) { TMPredict(d, s, a, l, top, 4) }} {
					got := bytes.Clone(initial)
					call(got[off:off+span:off+span], ds, got[ao:ao+w:ao+w], got[lo:lo+w:lo+w], top)
					if !bytes.Equal(got, want) {
						t.Fatalf("entry%d ds%d align%d mode%d: backing/alias/order", entry, ds, align, mode)
					}
				}
			}
		}
	}
}

func TestTMPredict4TileArithmetic(t *testing.T) {
	dst := make([]byte, 33)
	a, l := make([]byte, 4), make([]byte, 4)
	for delta := -255; delta <= 255; delta++ {
		av, top := byte(max(delta, 0)), byte(max(-delta, 0))
		for i := range a {
			a[i] = av
		}
		for lv := 0; lv < 256; lv++ {
			for i := range l {
				l[i] = byte(lv)
			}
			tm4NativeUnchecked(dst[:16:16], 4, a, l, top)
			v := byte(min(max(delta+lv, 0), 255))
			for i, got := range dst {
				want := v
				if i >= 16 {
					want = 0
				}
				if got != want {
					t.Fatalf("delta%d left%d i%d got%d want%d", delta, lv, i, got, want)
				}
			}
		}
	}
}

func TestTMPredict4TileFallbackPanicState(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, ds := range []int{-maxInt - 1, -1, 0, 1, 3, 4, 37, 1312, (1 << 30) - 1, 1 << 30, maxInt} {
		span := 64
		if ds >= 0 && ds <= 1312 {
			span = 3*ds + 4
		}
		for _, dlen := range []int{0, 1, span - 1, span} {
			for _, alen := range []int{0, 1, 3, 4} {
				for _, llen := range []int{0, 1, 3, 4} {
					for _, clip := range []bool{false, true} {
						for mode := 0; mode < 4; mode++ {
							off, ao, lo := 32, 32+span+7, 32+span+17
							switch mode {
							case 1:
								ao = off
							case 2:
								lo = off
							case 3:
								ao, lo = off, off
							}
							initial := make([]byte, off+span+47)
							for i := range initial {
								initial[i] = byte(i*37 + 11)
							}
							want := bytes.Clone(initial)
							dc, ac, lc := span, 4, 4
							if clip {
								dc, ac, lc = dlen, alen, llen
							}
							wp := tmBytesCapture(func() {
								tmBytesPrior(want[off:off+dlen:off+dc], ds, want[ao:ao+alen:ao+ac], want[lo:lo+llen:lo+lc], 255, 4)
							})
							for entry, call := range []func([]byte, int, []byte, []byte, byte){tm4NativeUnchecked, func(d []byte, s int, a, l []byte, top byte) { TMPredict(d, s, a, l, top, 4) }} {
								got := bytes.Clone(initial)
								gp := tmBytesCapture(func() { call(got[off:off+dlen:off+dc], ds, got[ao:ao+alen:ao+ac], got[lo:lo+llen:lo+lc], 255) })
								if gp != wp || !bytes.Equal(got, want) {
									t.Fatalf("entry%d ds%d dst%d/%d above%d/%d left%d/%d mode%d panic%q/%q", entry, ds, dlen, dc, alen, ac, llen, lc, mode, gp, wp)
								}
							}
						}
					}
				}
			}
		}
	}
}

func tm4NativeUnchecked(dst []byte, ds int, above, left []byte, top byte) {
	TMPredict4Native(dst, ds, above, left, top, false)
}

func tm4NativeChecked(dst []byte, ds int, above, left []byte, top byte) {
	TMPredict4Native(dst, ds, above, left, top, true)
}

func TestTMPredict4TileCheckedPanicState(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, ds := range []int{-maxInt - 1, -1, 0, 1, 3, 4, 37, 1312, 1 << 30, maxInt} {
		span := 64
		if ds >= 0 && ds <= 1312 {
			span = 3*ds + 4
		}
		for _, dl := range []int{0, 1, span - 1, span} {
			for _, al := range []int{0, 1, 3, 4} {
				for _, ll := range []int{0, 1, 3, 4} {
					for _, clip := range []bool{false, true} {
						for mode := 0; mode < 4; mode++ {
							off, ao, lo := 32, 32+span+7, 32+span+17
							if mode == 1 {
								ao = off
							}
							if mode == 2 {
								lo = off
							}
							if mode == 3 {
								ao, lo = off, off
							}
							initial := make([]byte, off+span+47)
							for i := range initial {
								initial[i] = byte(i*37 + 11)
							}
							got, want := bytes.Clone(initial), bytes.Clone(initial)
							dc, ac, lc := span, 4, 4
							if clip {
								dc, ac, lc = dl, al, ll
							}
							wp := tmBytesCapture(func() {
								d, a, l := want[off:off+dl:off+dc], want[ao:ao+al:ao+ac], want[lo:lo+ll:lo+lc]
								_ = a[3]
								_ = l[3]
								_ = d[3*ds+3]
								tmBytesPrior(d, ds, a, l, 255, 4)
							})
							gp := tmBytesCapture(func() {
								TMPredict4Native(got[off:off+dl:off+dc], ds, got[ao:ao+al:ao+ac], got[lo:lo+ll:lo+lc], 255, true)
							})
							if gp != wp || !bytes.Equal(got, want) {
								t.Fatalf("ds%d lengths%d/%d/%d clip%v mode%d panic%q/%q", ds, dl, al, ll, clip, mode, gp, wp)
							}
						}
					}
				}
			}
		}
	}
}
