package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

var fusedFPBenchmarkSink int

func BenchmarkVP9FusedFPClassesNative(b *testing.B) {
	for _, size := range []struct {
		name string
		tx   common.TxSize
		n    int
	}{{"4x4", common.Tx4x4, 16}, {"8x8", common.Tx8x8, 64}, {"16x16", common.Tx16x16, 256}} {
		for _, pattern := range []string{"zero", "sparse", "dense"} {
			b.Run(fmt.Sprintf("%s/%s", size.name, pattern), func(b *testing.B) {
				scan := &common.ScanOrders[size.tx][common.DctDct]
				coeff, qc, dqc, classes := make([]int16, size.n), make([]int16, size.n), make([]int16, size.n), make([]byte, size.n)
				for i := range coeff {
					switch pattern {
					case "sparse":
						if i%11 == 0 {
							coeff[i] = int16((i*271)%1024 - 512)
						}
					case "dense":
						coeff[i] = int16((i*347)%65536 - 32768)
					}
				}
				dequant := [2]int16{23, 41}
				tables := QuantizeFPTablesForDequant(dequant)
				b.ReportAllocs()
				var last int
				var produced bool
				for b.Loop() {
					last, produced = QuantizeFPWithQTablesScanOrderPtrTokenClasses(coeff, size.n, dequant, tables, scan, qc, dqc, classes)
					// Older assembly references leave classes to the production caller.
					if !produced {
						for i, c := range qc {
							m := int(c)
							if m < 0 {
								m = -m
							}
							token, _ := TokenForAbsCoeff(m)
							classes[i] = PtEnergyClass[token]
						}
					}
				}
				fusedFPBenchmarkSink = last + int(qc[0]) + int(dqc[size.n-1]) + int(classes[size.n-1])
			})
		}
	}
}

// The no-class control keeps the unchanged optional-output path visible.
func BenchmarkVP9FPNoClassesNative(b *testing.B) {
	const n = 64
	scan := &common.ScanOrders[common.Tx8x8][common.DctDct]
	coeff, qc, dqc := make([]int16, n), make([]int16, n), make([]int16, n)
	for i := range coeff {
		if i%11 == 0 {
			coeff[i] = int16(i*37 - 511)
		}
	}
	dequant := [2]int16{23, 41}
	tables := QuantizeFPTablesForDequant(dequant)
	b.ReportAllocs()
	var last int
	for b.Loop() {
		last = QuantizeFPWithQTablesScanOrderPtrValidated(coeff, n, dequant, tables, scan, qc, dqc)
	}
	fusedFPBenchmarkSink = last + int(qc[0]) + int(dqc[n-1])
}
