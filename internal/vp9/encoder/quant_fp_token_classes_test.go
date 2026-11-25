package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"math/rand"
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

// referenceTokenClass is the ground-truth energy class for one signed
// quantized coefficient: vp9_pt_energy_class[token(|q|)].
func referenceTokenClass(q int16) uint8 {
	abs := int(q)
	if abs < 0 {
		abs = -abs
	}
	token, _ := TokenForAbsCoeff(abs)
	return PtEnergyClass[token]
}

// TestQuantizeFPTokenClassesMatchesScalar pins the fused token-class
// quantizer against the scalar vp9_quantize_fp port plus the ground-truth
// class mapping for every standard transform size across randomized
// coefficient distributions and dequant tables.
func TestQuantizeFPTokenClassesMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x9a6c0de))
	sizes := []struct {
		tx common.TxSize
		n  int
	}{
		{common.Tx4x4, 16},
		{common.Tx8x8, 64},
		{common.Tx16x16, 256},
	}
	for _, size := range sizes {
		scanOrder := &common.ScanOrders[size.tx][common.DctDct]
		for trial := range 200 {
			dequant := [2]int16{
				int16(4 + rng.Intn(1300)),
				int16(4 + rng.Intn(1800)),
			}
			fp := QuantizeFPTablesForDequant(dequant)
			coeff := make([]int16, size.n)
			switch trial % 4 {
			case 0: // sparse small
				for i := 0; i < size.n/8; i++ {
					coeff[rng.Intn(size.n)] = int16(rng.Intn(200) - 100)
				}
			case 1: // dense small
				for i := range coeff {
					coeff[i] = int16(rng.Intn(64) - 32)
				}
			case 2: // large magnitudes incl. int16 extremes
				for i := range coeff {
					coeff[i] = int16(rng.Intn(65536) - 32768)
				}
			case 3: // all zero
			}
			wantQ := make([]int16, size.n)
			wantDQ := make([]int16, size.n)
			wantEOB := quantizeFPLibvpxScalar(coeff, size.n, fp.RoundFP, fp.QuantFP,
				dequant, scanOrder.Scan[:size.n], scanOrder.IScan[:size.n], wantQ, wantDQ)

			gotQ := make([]int16, size.n)
			gotDQ := make([]int16, size.n)
			classes := make([]uint8, size.n)
			for i := range classes {
				classes[i] = 0xAA // dirty scratch: kernel must overwrite every slot
			}
			gotEOB, produced := QuantizeFPWithQTablesScanOrderPtrTokenClasses(
				coeff, size.n, dequant, fp, scanOrder, gotQ, gotDQ, classes)
			if gotEOB != wantEOB {
				t.Fatalf("tx=%d trial=%d eob got %d want %d", size.tx, trial, gotEOB, wantEOB)
			}
			for i := range coeff {
				if gotQ[i] != wantQ[i] || gotDQ[i] != wantDQ[i] {
					t.Fatalf("tx=%d trial=%d q/dq mismatch at %d: got (%d,%d) want (%d,%d)",
						size.tx, trial, i, gotQ[i], gotDQ[i], wantQ[i], wantDQ[i])
				}
			}
			if !produced {
				if pixel.Enabled {
					t.Fatal("native SIMD did not produce fused token classes")
				}
				continue // builds without the fused kernel keep the fallback walk
			}
			for i := range coeff {
				if want := referenceTokenClass(wantQ[i]); classes[i] != want {
					t.Fatalf("tx=%d trial=%d class mismatch at %d (q=%d): got %d want %d",
						size.tx, trial, i, wantQ[i], classes[i], want)
				}
			}
		}
	}
}

// TestQuantizeFPTokenClassesShortWindowFallsBack pins the classes-window
// precondition: an undersized classes buffer must leave it untouched and
// report produced=false while still quantizing exactly.
func TestQuantizeFPTokenClassesShortWindowFallsBack(t *testing.T) {
	scanOrder := &common.ScanOrders[common.Tx8x8][common.DctDct]
	dequant := [2]int16{32, 41}
	fp := QuantizeFPTablesForDequant(dequant)
	coeff := make([]int16, 64)
	for i := range coeff {
		coeff[i] = int16(i*7 - 100)
	}
	wantQ := make([]int16, 64)
	wantDQ := make([]int16, 64)
	wantEOB := quantizeFPLibvpxScalar(coeff, 64, fp.RoundFP, fp.QuantFP,
		dequant, scanOrder.Scan[:64], scanOrder.IScan[:64], wantQ, wantDQ)
	gotQ := make([]int16, 64)
	gotDQ := make([]int16, 64)
	classes := make([]uint8, 32)
	for i := range classes {
		classes[i] = 0x5B
	}
	gotEOB, produced := QuantizeFPWithQTablesScanOrderPtrTokenClasses(
		coeff, 64, dequant, fp, scanOrder, gotQ, gotDQ, classes)
	if produced {
		t.Fatalf("short classes window must not report produced")
	}
	if gotEOB != wantEOB {
		t.Fatalf("eob got %d want %d", gotEOB, wantEOB)
	}
	for i := range classes {
		if classes[i] != 0x5B {
			t.Fatalf("classes[%d] touched on fallback", i)
		}
	}
}
