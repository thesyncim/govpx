//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"simd/archsimd"
	"slices"
	"testing"
)

func TestFP32PublicHost(t *testing.T) {
	t.Logf("FP32PublicHost AVX512=%v AVX2=%v Enabled=%v", archsimd.X86.AVX512(), archsimd.X86.AVX2(), Enabled)
	if os.Getenv("GOVPX_REQUIRE_FP32_NATIVE") == "1" && (!Enabled || !archsimd.X86.AVX2() || !archsimd.X86.AVX512()) {
		t.Fatal("actual native AVX512 SIMD backend required")
	}
}

func fp32OptionalWindowBytes(size, offset int) ([]byte, []byte, int) {
	p := bytes.Repeat([]byte{0xa7}, size+192)
	base := reflect.ValueOf(p).Pointer()
	start := 64 + int((-base)&63) + offset
	return p, p[start : start+size : start+size], start
}

func fp32OptionalWindowInt16(n, byteOffset int) ([]int16, []int16, int) {
	p := make([]int16, n+96)
	for i := range p {
		p[i] = int16(0x5757)
	}
	base := reflect.ValueOf(p).Pointer()
	start := (64 + int((-base)&63) + byteOffset) / 2
	return p, p[start : start+n : start+n], start
}

// Independent fixed-point equations and scalar energy thresholds; no production
// FP, token, clipping or vector helpers. Optional q is deliberately supported.
func fp32OptionalExpected(c, scan, q, dq []int16, cl []byte, r, qt, d [2]int16) int {
	eob := 0
	for i, v := range c {
		k := 1
		if i == 0 {
			k = 0
		}
		a := int(v)
		if a < 0 {
			a = -a
		}
		a += int(r[k])
		mag := 0
		if a >= int(d[k]) {
			if a > 32767 {
				a = 32767
			}
			mag = a * int(qt[k]) / 65536
		}
		signed := mag
		if v < 0 {
			signed = -signed
		}
		if q != nil {
			q[i] = int16(signed)
		}
		dq[i] = int16(signed * int(d[k]))
		if signed != 0 && int(scan[i]) > eob {
			eob = int(scan[i])
		}
		if cl != nil {
			value := byte(0)
			for _, bound := range []int{0, 1, 2, 4, 10} {
				if mag > bound {
					value++
				}
			}
			cl[i] = value
		}
	}
	return eob
}

var fp32OptionalSink uint64

// This measures the real public pixel API, not an encoder qnil caller. The
// encoder APIs require qcoeff and are separately covered by unchanged fixtures.
func BenchmarkFP32PublicOptionalNative(b *testing.B) {
	for _, n := range []int{64, 256} {
		for _, qnil := range []bool{false, true} {
			for _, classes := range []bool{false, true} {
				for _, pattern := range []string{"zero", "sparse", "dense"} {
					for _, unaligned := range []bool{false, true} {
						alignment := "aligned64"
						if unaligned {
							alignment = "offset2-classes1"
						}
						b.Run(fmt.Sprintf("n%d/qnil%v/classes%v/%s/%s", n, qnil, classes, pattern, alignment), func(b *testing.B) {
							offset, coff := 0, 0
							if unaligned {
								offset, coff = 2, 1
							}
							var backing16, want16 [4][]int16
							var views16 [4][]int16
							var off16 [4]int
							for k := 0; k < 4; k++ {
								backing16[k], views16[k], off16[k] = fp32OptionalWindowInt16(n, offset)
								if reflect.ValueOf(views16[k]).Pointer()&63 != uintptr(offset) {
									b.Fatal("fixture alignment")
								}
							}
							classBacking, cl, classOff := fp32OptionalWindowBytes(n, coff)
							if reflect.ValueOf(cl).Pointer()&63 != uintptr(coff) {
								b.Fatal("fixture alignment")
							}
							c, s, q, dq := views16[0], views16[1], views16[2], views16[3]
							for i := range c {
								if pattern == "zero" {
									c[i] = 0
								} else if pattern == "dense" {
									c[i] = int16((i*347)%65536 - 32768)
								} else if i%11 == 0 {
									c[i] = int16((i*271)%1024 - 512)
								} else {
									c[i] = 0
								}
								s[i] = int16((i*37)%n + 1)
							}
							for k := range backing16 {
								want16[k] = slices.Clone(backing16[k])
							}
							wantClass := bytes.Clone(classBacking)
							wc, ws, wq, wd, wcl := want16[0][off16[0]:off16[0]+n], want16[1][off16[1]:off16[1]+n], want16[2][off16[2]:off16[2]+n], want16[3][off16[3]:off16[3]+n], wantClass[classOff:classOff+n]
							if qnil {
								q, wq = nil, nil
							}
							if !classes {
								cl, wcl = nil, nil
							}
							r, qt, dequant := [2]int16{8, 13}, [2]int16{2849, 1598}, [2]int16{23, 41}
							expected := fp32OptionalExpected(wc, ws, wq, wd, wcl, r, qt, dequant)
							check := func(e int) {
								b.Helper()
								if e != expected {
									b.Fatal("EOB", e, expected)
								}
								for k := range backing16 {
									if !slices.Equal(backing16[k], want16[k]) {
										b.Fatal("full input/output/canary backing", k)
									}
								}
								if !bytes.Equal(classBacking, wantClass) {
									b.Fatal("full input/output/canary backing", 4)
								}
							}
							eob := QuantizeFPBlock(c, s, q, dq, n, r, qt, dequant, cl)
							check(eob)
							b.ReportAllocs()
							for b.Loop() {
								eob = QuantizeFPBlock(c, s, q, dq, n, r, qt, dequant, cl)
							}
							check(eob)
							sum := uint64(eob)
							for _, v := range dq {
								sum += uint64(uint16(v))
							}
							if q != nil {
								for _, v := range q {
									sum += uint64(uint16(v))
								}
							}
							if cl != nil {
								for _, v := range cl {
									sum += uint64(v)
								}
							}
							fp32OptionalSink = sum
						})
					}
				}
			}
		}
	}
}
