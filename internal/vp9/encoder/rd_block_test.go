package encoder

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestConditionalSkipIntraDirectionalPruning(t *testing.T) {
	if !ConditionalSkipIntra(common.D117Pred, common.D45Pred) {
		t.Fatal("D117 should be skipped after an unrelated best mode")
	}
	if ConditionalSkipIntra(common.D117Pred, common.VPred) {
		t.Fatal("D117 should remain after V_PRED")
	}
	if ConditionalSkipIntra(common.DcPred, common.VPred) {
		t.Fatal("non-directional modes should not be pruned")
	}
}

func TestRestorePlaneRectCopiesPackedRows(t *testing.T) {
	data := []byte{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
	}
	saved := []byte{
		50, 51,
		52, 53,
	}
	RestorePlaneRect(data, 4, 1, 1, 2, 2, saved)
	want := []byte{
		1, 2, 3, 4,
		5, 50, 51, 8,
		9, 52, 53, 12,
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("RestorePlaneRect = %v, want %v", data, want)
	}
}

func TestTransformBlockMetricsShiftForSub32x32(t *testing.T) {
	coeffs := []int16{8, -4, 3}
	dqcoeffs := []int16{2, -6, -1}
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx4x4); got != 14 {
		t.Fatalf("TransformBlockError 4x4 = %d, want 14", got)
	}
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx32x32); got != 56 {
		t.Fatalf("TransformBlockError 32x32 = %d, want 56", got)
	}
	if got := TransformBlockErrorShifted(coeffs, dqcoeffs); got != 14 {
		t.Fatalf("TransformBlockErrorShifted = %d, want 14", got)
	}
	if got := TransformBlockEnergy(coeffs, common.Tx8x8); got != 22 {
		t.Fatalf("TransformBlockEnergy 8x8 = %d, want 22", got)
	}
	if got := ResidualSSE([]int16{3, -4, 12}); got != 169 {
		t.Fatalf("ResidualSSE = %d, want 169", got)
	}
}

func TestTransformBlockMetricsExactInt16Extremes(t *testing.T) {
	coeffs := make([]int16, 64)
	dqcoeffs := make([]int16, 64)
	residue := make([]int16, 64)
	for i := range coeffs {
		if i&1 == 0 {
			coeffs[i] = -32768
			dqcoeffs[i] = 32767
			residue[i] = -32768
		} else {
			coeffs[i] = 32767
			dqcoeffs[i] = -32768
			residue[i] = 32767
		}
	}

	wantErr := transformBlockErrorScalar(coeffs, dqcoeffs, len(coeffs))
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx32x32); got != wantErr {
		t.Fatalf("TransformBlockError extremes = %d, want %d", got, wantErr)
	}
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx8x8); got != wantErr>>2 {
		t.Fatalf("TransformBlockError shifted extremes = %d, want %d", got, wantErr>>2)
	}

	wantEnergy := transformBlockEnergyScalar(coeffs)
	if got := TransformBlockEnergy(coeffs, common.Tx32x32); got != wantEnergy {
		t.Fatalf("TransformBlockEnergy extremes = %d, want %d", got, wantEnergy)
	}
	if got := TransformBlockEnergy(coeffs, common.Tx4x4); got != wantEnergy>>2 {
		t.Fatalf("TransformBlockEnergy shifted extremes = %d, want %d", got, wantEnergy>>2)
	}

	wantSSE := transformBlockEnergyScalar(residue)
	if got := ResidualSSE(residue); got != wantSSE {
		t.Fatalf("ResidualSSE extremes = %d, want %d", got, wantSSE)
	}
}

func TestTransformBlockMetricsOddLengthsUseScalarSemantics(t *testing.T) {
	coeffs := []int16{9, -17, 23, -31, 47, -55, 63, -71, 89}
	dqcoeffs := []int16{-3, 5, -7, 11, -13, 17, -19, 23, -29, 31}
	wantErr := transformBlockErrorScalar(coeffs, dqcoeffs, len(coeffs))
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx32x32); got != wantErr {
		t.Fatalf("TransformBlockError odd length = %d, want %d", got, wantErr)
	}
	wantEnergy := transformBlockEnergyScalar(coeffs)
	if got := TransformBlockEnergy(coeffs, common.Tx32x32); got != wantEnergy {
		t.Fatalf("TransformBlockEnergy odd length = %d, want %d", got, wantEnergy)
	}
}

var rdBlockMetricSink uint64

func BenchmarkTransformBlockMetrics(b *testing.B) {
	for _, n := range []int{16, 64, 256, 1024} {
		coeffs := make([]int16, n)
		dqcoeffs := make([]int16, n)
		for i := range coeffs {
			coeffs[i] = int16((i*37)%8192 - 4096)
			dqcoeffs[i] = int16((i*19)%8192 - 4096)
		}

		b.Run(fmt.Sprintf("error_n%d", n), func(b *testing.B) {
			var sum uint64
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sum += TransformBlockError(coeffs, dqcoeffs, common.Tx32x32)
			}
			rdBlockMetricSink = sum
		})
		b.Run(fmt.Sprintf("energy_n%d", n), func(b *testing.B) {
			var sum uint64
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sum += TransformBlockEnergy(coeffs, common.Tx32x32)
			}
			rdBlockMetricSink = sum
		})
	}
}
