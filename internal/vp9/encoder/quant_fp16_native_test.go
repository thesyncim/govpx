package encoder

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

var fp16NativeSink uint64

// fp16NativeInt16Window allocates an int16 buffer with canary elements before
// and after the returned sub-slice.
func fp16NativeInt16Window(n, offset int) ([]int16, []int16, int) {
	backing := make([]int16, n+64)
	for i := range backing {
		backing[i] = -0x5859 // 0xa7a7
	}
	start := 16 + offset
	return backing, backing[start : start+n : start+n], start
}

// fp16NativeByteWindow allocates a byte buffer with canary bytes before and
// after the returned sub-slice.
func fp16NativeByteWindow(n, offset int) ([]byte, []byte, int) {
	backing := bytes.Repeat([]byte{0xa7}, n+128)
	start := 32 + offset
	return backing, backing[start : start+n : start+n], start
}

// fp16NativeReference is an independent integer implementation of the pinned
// libvpx v1.16.0 FP operation and energy-class thresholds. It calls no SIMD,
// production quantization, token mapping, or clipping helpers.
func fp16NativeReference(coeff, iscan, qo, dq []int16, cl []byte, round, quant, dequant [2]int16) int {
	eob := 0
	for i, c16 := range coeff {
		k := 1
		if i == 0 {
			k = 0
		}
		c := int(c16)
		magnitude := c
		if magnitude < 0 {
			magnitude = -magnitude
		}
		rounded := magnitude + int(round[k])
		value := 0
		if rounded >= int(dequant[k]) {
			if rounded > 32767 {
				rounded = 32767
			}
			value = rounded * int(quant[k]) / 65536
		}
		signed := value
		if c < 0 {
			signed = -signed
		}
		qo[i] = int16(signed)
		dq[i] = int16(signed * int(dequant[k]))
		if value != 0 && int(iscan[i]) > eob {
			eob = int(iscan[i])
		}
		if cl != nil {
			class := byte(0)
			for _, bound := range [...]int{0, 1, 2, 4, 10} {
				if value > bound {
					class++
				}
			}
			cl[i] = class
		}
	}
	return eob
}

// BenchmarkVP9FP16Native overlays unchanged on assembly, parent and current.
// Only coeff/q/dq/classes alignment is controlled; the real immutable
// ScanOrder's IScan is used by the actual public encoder operation.
func BenchmarkVP9FP16Native(b *testing.B) {
	for _, size := range []struct {
		n  int
		tx common.TxSize
	}{{16, common.Tx4x4}, {64, common.Tx8x8}, {256, common.Tx16x16}} {
		for _, classesOn := range []bool{false, true} {
			for _, pattern := range []string{"zero", "sparse", "dense"} {
				for _, unaligned := range []bool{false, true} {
					mode, alignment := "no-classes", "aligned32"
					if classesOn {
						mode = "classes"
					}
					if unaligned {
						alignment = "offset2-classes1"
					}
					b.Run(fmt.Sprintf("n%d/%s/%s/%s", size.n, mode, pattern, alignment), func(b *testing.B) {
						n := size.n
						int16Offset := 0
						classOffset := 0
						if unaligned {
							int16Offset = 1
							classOffset = 1
						}
						cb, coeff, _ := fp16NativeInt16Window(n, int16Offset)
						qb, qo, qOffset := fp16NativeInt16Window(n, int16Offset)
						db, dq, dOffset := fp16NativeInt16Window(n, int16Offset)
						lb, classes, lOffset := fp16NativeByteWindow(n, classOffset)
						for i := range coeff {
							switch pattern {
							case "zero":
								coeff[i] = 0
							case "sparse":
								coeff[i] = 0
								if i%11 == 0 {
									coeff[i] = int16((i*271)%1024 - 512)
								}
							case "dense":
								coeff[i] = int16((i*347)%65536 - 32768)
							}
						}
						scan := &common.ScanOrders[size.tx][common.DctDct]
						scanBefore, iscanBefore := append([]int16(nil), scan.Scan[:n]...), append([]int16(nil), scan.IScan[:n]...)
						dequant := [2]int16{23, 41}
						tables := QuantizeFPTablesForDequant(dequant)
						cbefore := slices.Clone(cb)
						qw, dw, lw := slices.Clone(qb), slices.Clone(db), bytes.Clone(lb)
						qwant := qw[qOffset : qOffset+n : qOffset+n]
						dwant := dw[dOffset : dOffset+n : dOffset+n]
						var lwant []byte
						if classesOn {
							lwant = lw[lOffset : lOffset+n : lOffset+n]
						}
						wantEOB := fp16NativeReference(coeff, scan.IScan[:n], qwant, dwant, lwant, tables.RoundFP, tables.QuantFP, dequant)
						var eob int
						check := func() {
							b.Helper()
							if eob != wantEOB || !slices.Equal(cb, cbefore) || !slices.Equal(qb, qw) || !slices.Equal(db, dw) || !bytes.Equal(lb, lw) {
								b.Fatalf("full backing/EOB mismatch got%d want%d", eob, wantEOB)
							}
							for i := range n {
								if scan.Scan[i] != scanBefore[i] || scan.IScan[i] != iscanBefore[i] {
									b.Fatal("ScanOrder mutated")
								}
							}
						}
						// Same actual production caller fallback as the existing composite fixture.
						fallbackClasses := func() {
							for i, c := range qo {
								m := int(c)
								if m < 0 {
									m = -m
								}
								token, _ := TokenForAbsCoeff(m)
								classes[i] = PtEnergyClass[token]
							}
						}
						if classesOn {
							var produced bool
							eob, produced = QuantizeFPWithQTablesScanOrderPtrTokenClasses(coeff, n, dequant, tables, scan, qo, dq, classes)
							if !produced {
								fallbackClasses()
							}
						} else {
							eob = QuantizeFPWithQTablesScanOrderPtrValidated(coeff, n, dequant, tables, scan, qo, dq)
						}
						check()
						b.ReportAllocs()
						for b.Loop() {
							if classesOn {
								var produced bool
								eob, produced = QuantizeFPWithQTablesScanOrderPtrTokenClasses(coeff, n, dequant, tables, scan, qo, dq, classes)
								if !produced {
									for i, c := range qo {
										m := int(c)
										if m < 0 {
											m = -m
										}
										token, _ := TokenForAbsCoeff(m)
										classes[i] = PtEnergyClass[token]
									}
								}
							} else {
								eob = QuantizeFPWithQTablesScanOrderPtrValidated(coeff, n, dequant, tables, scan, qo, dq)
							}
						}
						check()
						sum := uint64(eob)
						for i := range qo {
							sum += uint64(uint16(qo[i])) + uint64(uint16(dq[i]))
							if classesOn {
								sum += uint64(classes[i])
							}
						}
						fp16NativeSink = sum
					})
				}
			}
		}
	}
}
