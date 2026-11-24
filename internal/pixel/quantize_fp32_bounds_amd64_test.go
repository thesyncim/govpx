//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"reflect"
	"slices"
	"testing"
)

func TestFP32EligibleExactWindows(t *testing.T) {
	const n = 256
	for align := 0; align < 32; align++ {
		for _, trail := range []int{0, 19} {
			for options := 0; options < 4; options++ {
				var got16, want16 [4][]int16
				var offsets16 [4]int
				for k := 0; k < 4; k++ {
					got16[k] = make([]int16, 64+n+trail)
					for i := range got16[k] {
						got16[k][i] = int16(0x5757)
					}
					base := reflect.ValueOf(got16[k]).Pointer() & 63
					desired := ((align*(2*k+1) + k) % 32) * 2
					offsets16[k] = ((desired - int(base) + 64) % 64) / 2
					if reflect.ValueOf(got16[k][offsets16[k]:]).Pointer()&63 != uintptr(desired) {
						t.Fatal("alignment fixture")
					}
				}
				got8 := bytes.Repeat([]byte{0xa7}, 128+n+trail)
				base8 := reflect.ValueOf(got8).Pointer() & 63
				desired8 := (align*9 + 4) % 64
				off8 := (desired8 - int(base8) + 64) % 64
				if reflect.ValueOf(got8[off8:]).Pointer()&63 != uintptr(desired8) {
					t.Fatal("alignment fixture")
				}
				c, s := got16[0][offsets16[0]:offsets16[0]+n], got16[1][offsets16[1]:offsets16[1]+n]
				for i := 0; i < n; i++ {
					c[i] = int16(i*771 + align*347 - 32768)
					s[i] = int16(i*127 - 16384)
				}
				for k := range got16 {
					want16[k] = slices.Clone(got16[k])
				}
				want8 := bytes.Clone(got8)
				qg, dg := got16[2][offsets16[2]:offsets16[2]+n], got16[3][offsets16[3]:offsets16[3]+n]
				qw, dw := want16[2][offsets16[2]:offsets16[2]+n], want16[3][offsets16[3]:offsets16[3]+n]
				cg, cw := got8[off8:off8+n:off8+n], want8[off8:off8+n:off8+n]
				if options&1 != 0 {
					qg, qw = nil, nil
				}
				if options&2 != 0 {
					cg, cw = nil, nil
				}
				if !fp16WindowsDisjoint(c, s, qg, dg, cg, n) {
					t.Fatal("eligible exact windows unexpectedly overlap")
				}
				r, q, d := [2]int16{32767, 17}, [2]int16{32767, 12345}, [2]int16{1, 23}
				we := fp16ScalarIndependent(want16[0][offsets16[0]:offsets16[0]+n], want16[1][offsets16[1]:offsets16[1]+n], qw, dw, cw, n, r, q, d)
				ge := QuantizeFPBlock(c, s, qg, dg, n, r, q, d, cg)
				if ge != we {
					t.Fatalf("align%d options%d EOB", align, options)
				}
				for k := range got16 {
					if !slices.Equal(got16[k], want16[k]) {
						t.Fatalf("align%d trail%d options%d backing%d", align, trail, options, k)
					}
				}
				if !bytes.Equal(got8, want8) {
					t.Fatalf("align%d trail%d options%d backing4", align, trail, options)
				}
			}
		}
	}
}

func TestFP32EligibleAliasedDeclines(t *testing.T) {
	const n = 256
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			for _, shift := range []int{0, 1, n - 1} {
				offsets := [5]int{32, 544, 1056, 1568, 64}
				offsets[j] = offsets[i] + shift
				got16 := make([]int16, 3072)
				for k := range got16 {
					got16[k] = int16(k*37 + k/3)
				}
				got8 := make([]byte, 512)
				for k := range got8 {
					got8[k] = byte(k*19 + 5)
				}
				want16 := slices.Clone(got16)
				want8 := bytes.Clone(got8)
				c, s, q, d, cl := fp16Views(got16, got8, offsets, n)
				cl = cl[:n:n]
				if fp16WindowsDisjoint(c, s, q, d, cl, n) {
					t.Fatalf("pair%d/%d shift%d accepted overlap", i, j, shift)
				}
				wc, ws, wq, wd, wcl := fp16Views(want16, want8, offsets, n)
				wcl = wcl[:n:n]
				r, qt, dt := [2]int16{17, 32767}, [2]int16{12345, 32767}, [2]int16{23, 1}
				ge, gp := fp16Capture(func() int { return QuantizeFPBlock(c, s, q, d, n, r, qt, dt, cl) })
				we, wp := fp16Capture(func() int { return fp16OriginalPublic(wc, ws, wq, wd, n, r, qt, dt, wcl) })
				if ge != we || gp != wp || !slices.Equal(got16, want16) || !bytes.Equal(got8, want8) {
					t.Fatalf("pair%d/%d shift%d changed frozen state/panic", i, j, shift)
				}
			}
		}
	}
}

func TestFP32EligiblePanicWindows(t *testing.T) {
	const n = 256
	for short := 0; short < 5; short++ {
		for _, length := range []int{0, 1, n - 1} {
			for _, clipped := range []bool{false, true} {
				initial16 := make([]int16, 3072)
				for k := range initial16 {
					initial16[k] = int16(k*37 + 11)
				}
				initial8 := make([]byte, 512)
				for k := range initial8 {
					initial8[k] = byte(k*19 + 5)
				}
				got16, want16 := slices.Clone(initial16), slices.Clone(initial16)
				got8, want8 := bytes.Clone(initial8), bytes.Clone(initial8)
				offs := [5]int{32, 544, 1056, 1568, 64}
				run := func(storage16 []int16, storage8 []byte, old bool) (int, string) {
					c, s, q, d, cl := fp16Views(storage16, storage8, offs, n)
					cl = cl[:n:n]
					slices16 := []*[]int16{&c, &s, &q, &d}
					if short < 4 {
						p := slices16[short]
						if clipped {
							*p = (*p)[:length:length]
						} else {
							*p = (*p)[:length]
						}
					} else if clipped {
						cl = cl[:length:length]
					} else {
						cl = cl[:length]
					}
					return fp16Capture(func() int {
						if old {
							return fp16OriginalPublic(c, s, q, d, n, [2]int16{0, 17}, [2]int16{32767, 12345}, [2]int16{23, 41}, cl)
						}
						return QuantizeFPBlock(c, s, q, d, n, [2]int16{0, 17}, [2]int16{32767, 12345}, [2]int16{23, 41}, cl)
					})
				}
				ge, gp := run(got16, got8, false)
				we, wp := run(want16, want8, true)
				if gp == "" || ge != we || gp != wp || !slices.Equal(got16, want16) || !bytes.Equal(got8, want8) {
					t.Fatalf("short%d length%d clipped%v panic%q/%q", short, length, clipped, gp, wp)
				}
			}
		}
	}
}
