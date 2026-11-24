package pixel

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"testing"
)

func TestTMPredictBytesSelected(t *testing.T) {
	selected := Enabled && runtime.GOARCH == "arm64"
	t.Logf("Enabled=%v GOARCH=%s native-bytes=%v", Enabled, runtime.GOARCH, selected)
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && !Enabled {
		t.Fatal("required SIMD unavailable")
	}
}

func tmBytesCapture(f func()) (text string) {
	defer func() {
		if p := recover(); p != nil {
			text = fmt.Sprint(p)
		}
	}()
	f()
	return
}

func TestTMPredictBytesArithmetic(t *testing.T) {
	if !(Enabled && runtime.GOARCH == "arm64") {
		t.Skip("native ARM-only arithmetic")
	}
	const w = 8
	dst := bytes.Repeat([]byte{91}, w*w+17)
	above, left := make([]byte, w), make([]byte, w)
	for a := 0; a < 256; a++ {
		for i := range above {
			above[i] = byte(a)
		}
		for l := 0; l < 256; l++ {
			for i := range left {
				left[i] = byte(l)
			}
			for _, top := range []byte{0, 128, 255} {
				want := byte(min(max(a+l-int(top), 0), 255))
				if !TMPredictBytes(dst[:w*w:w*w], w, above, left, top, w) {
					t.Fatal("eligible arithmetic declined")
				}
				for i, v := range dst {
					expected := byte(91)
					if i < w*w {
						expected = want
					}
					if v != expected {
						t.Fatalf("a%d l%d top%d i%d got%d want%d", a, l, top, i, v, expected)
					}
				}
			}
		}
	}
}

func TestTMPredictBytesFullBackingAliases(t *testing.T) {
	selected := Enabled && runtime.GOARCH == "arm64"
	for _, w := range []int{8, 16} {
		for _, ds := range []int{0, 1, w - 1, w, w + 3, 37, 1312} {
			for align := 0; align < 16; align++ {
				for mode := 0; mode < 12; mode++ {
					span := (w-1)*ds + w
					off := 64 + align
					ao, lo := off+span+37, off+span+89
					switch mode {
					case 1:
						ao = off
					case 2:
						ao = off + span - w
					case 3:
						lo = off
					case 4:
						lo = off + span - w
					case 5:
						ao = off
						lo = off
					case 6:
						lo = ao + 1
					case 7:
						ao = off + 1
						lo = off + 3
					case 8:
						lo = off + 1
					case 9:
						lo = off + min(ds, span-w)
					case 10:
						ao = off - w
					case 11:
						ao = off + span
					}
					original := make([]byte, off+span+2*w+157)
					for i := range original {
						original[i] = byte(i*37 + i/7 + 11)
					}
					want := bytes.Clone(original)
					direct := bytes.Clone(original)
					got := bytes.Clone(original)
					top := byte(align * 17)
					tmBytesPrior(want[off:off+span:off+span], ds, want[ao:ao+w:ao+w], want[lo:lo+w:lo+w], top, w)
					overlap := ao < off+span && off < ao+w
					eligible := selected && !overlap
					handled := TMPredictBytes(direct[off:off+span:off+span], ds, direct[ao:ao+w:ao+w], direct[lo:lo+w:lo+w], top, w)
					if handled != eligible {
						t.Fatalf("w%d ds%d align%d mode%d handled%v want%v", w, ds, align, mode, handled, eligible)
					}
					directWant := original
					if eligible {
						directWant = want
					}
					if !bytes.Equal(direct, directWant) {
						t.Fatalf("direct backing w%d ds%d align%d mode%d", w, ds, align, mode)
					}
					TMPredict(got[off:off+span:off+span], ds, got[ao:ao+w:ao+w], got[lo:lo+w:lo+w], top, w)
					if !bytes.Equal(got, want) {
						t.Fatalf("public backing w%d ds%d align%d mode%d", w, ds, align, mode)
					}
				}
			}
		}
	}
}

func TestTMPredictBytesIndependentAlignments(t *testing.T) {
	selected := Enabled && runtime.GOARCH == "arm64"
	for _, w := range []int{8, 16} {
		for _, ds := range []int{w, 37} {
			for a := 0; a < 16; a++ {
				for l := 0; l < 16; l++ {
					span := (w-1)*ds + w
					dst := bytes.Repeat([]byte{37}, span+31)
					want := bytes.Clone(dst)
					above, left := make([]byte, w+31), make([]byte, w+31)
					for i := range above {
						above[i] = byte(i*37 + 11)
						left[i] = byte(i*19 + 251)
					}
					abefore, lbefore := bytes.Clone(above), bytes.Clone(left)
					if selected {
						tmBytesPrior(want[7:7+span:7+span], ds, above[a:a+w:a+w], left[l:l+w:l+w], 128, w)
					}
					ok := TMPredictBytes(dst[7:7+span:7+span], ds, above[a:a+w:a+w], left[l:l+w:l+w], 128, w)
					if ok != selected || !bytes.Equal(dst, want) || !bytes.Equal(above, abefore) || !bytes.Equal(left, lbefore) {
						t.Fatalf("w%d ds%d align%d/%d", w, ds, a, l)
					}
				}
			}
		}
	}
}

func TestTMPredictBytesFallbackPanicState(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, w := range []int{8, 16} {
		for _, ds := range []int{-1, 0, 1, w - 1, w, 37, 1312, (1 << 30) - 1, 1 << 30, maxInt, -maxInt - 1} {
			span := 256
			if ds >= 0 && ds <= 1312 {
				span = (w-1)*ds + w
			}
			for _, dlen := range []int{0, 1, span - 1, span} {
				for _, alen := range []int{0, 1, w - 1, w} {
					for _, llen := range []int{0, 1, w - 1, w} {
						for _, clip := range []bool{false, true} {
							for alias := 0; alias < 4; alias++ {
								off := 64
								ao, lo := off+span+37, off+span+89
								switch alias {
								case 1:
									ao = off
								case 2:
									lo = off
								case 3:
									ao = off
									lo = off + 1
								}
								original := make([]byte, off+span+2*w+157)
								for i := range original {
									original[i] = byte(i*37 + i/7 + 11)
								}
								got, want := bytes.Clone(original), bytes.Clone(original)
								dc, ac, lc := span, w, w
								if clip {
									dc, ac, lc = dlen, alen, llen
								}
								gp := tmBytesCapture(func() { TMPredict(got[off:off+dlen:off+dc], ds, got[ao:ao+alen:ao+ac], got[lo:lo+llen:lo+lc], 255, w) })
								wp := tmBytesCapture(func() {
									tmBytesPrior(want[off:off+dlen:off+dc], ds, want[ao:ao+alen:ao+ac], want[lo:lo+llen:lo+lc], 255, w)
								})
								if gp != wp || !bytes.Equal(got, want) {
									t.Fatalf("w%d ds%d dst%d/%d a%d/%d l%d/%d alias%d panic%q/%q", w, ds, dlen, dc, alen, ac, llen, lc, alias, gp, wp)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestTMPredictBytesUnsupportedWidths(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, w := range []int{-1, 0, 1, 4, 7, 9, 17, 32, maxInt} {
		for _, ds := range []int{-1, 0, 4, 37} {
			got, want := bytes.Repeat([]byte{19}, 128), bytes.Repeat([]byte{19}, 128)
			a, l := make([]byte, 64), make([]byte, 64)
			gp := tmBytesCapture(func() { TMPredict(got, ds, a, l, 128, w) })
			wp := tmBytesCapture(func() { tmBytesPrior(want, ds, a, l, 128, w) })
			if gp != wp || !bytes.Equal(got, want) {
				t.Fatalf("w%d ds%d panic%q/%q", w, ds, gp, wp)
			}
			before := bytes.Clone(got)
			if TMPredictBytes(got, ds, a, l, 128, w) || !bytes.Equal(got, before) {
				t.Fatal("unsupported helper accepted/wrote")
			}
		}
	}
}
