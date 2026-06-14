package encoder

import (
	"bytes"
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
	gotErr, gotEnergy := TransformBlockErrorWithEnergy(coeffs, dqcoeffs, common.Tx4x4)
	if gotErr != 14 || gotEnergy != 22 {
		t.Fatalf("TransformBlockErrorWithEnergy 4x4 = (%d,%d), want (14,22)",
			gotErr, gotEnergy)
	}
	gotErr, gotEnergy = TransformBlockErrorWithEnergy(coeffs, dqcoeffs, common.Tx32x32)
	if gotErr != 56 || gotEnergy != 89 {
		t.Fatalf("TransformBlockErrorWithEnergy 32x32 = (%d,%d), want (56,89)",
			gotErr, gotEnergy)
	}
	if got := ResidualSSE([]int16{3, -4, 12}); got != 169 {
		t.Fatalf("ResidualSSE = %d, want 169", got)
	}
}

func TestTransformBlockErrorWithEnergyUsesPairedWindow(t *testing.T) {
	coeffs := []int16{3, -4, 1000}
	dqcoeffs := []int16{1, -1}
	gotErr, gotEnergy := TransformBlockErrorWithEnergy(coeffs, dqcoeffs, common.Tx32x32)
	if gotErr != 13 || gotEnergy != 25 {
		t.Fatalf("TransformBlockErrorWithEnergy paired window = (%d,%d), want (13,25)",
			gotErr, gotEnergy)
	}
}
