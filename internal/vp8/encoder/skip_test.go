package encoder

import (
	"testing"

	common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestBaseSkipFalseProbTableMatchesLibvpx(t *testing.T) {
	tests := []struct {
		qIndex int
		want   uint8
	}{
		{qIndex: -1, want: 255},
		{qIndex: 0, want: 255},
		{qIndex: 55, want: 255},
		{qIndex: 56, want: 251},
		{qIndex: 57, want: 248},
		{qIndex: 96, want: 101},
		{qIndex: 98, want: 95},
		{qIndex: 127, want: 16},
		{qIndex: 128, want: 16},
	}
	for _, tt := range tests {
		if got := BaseSkipFalseProb(tt.qIndex); got != tt.want {
			t.Fatalf("base skip false prob q=%d = %d, want %d", tt.qIndex, got, tt.want)
		}
	}
}

func TestInterFrameModeSkipFalseProbabilityMatchesWriterCounts(t *testing.T) {
	modes := []InterFrameMacroblockMode{
		{MBSkipCoeff: false},
		{MBSkipCoeff: true},
		{MBSkipCoeff: true},
		{MBSkipCoeff: true},
	}
	if got := InterFrameModeSkipFalseProbability(1, 4, modes, 128); got != 64 {
		t.Fatalf("skip false prob = %d, want 64", got)
	}
	modes = []InterFrameMacroblockMode{
		{MBSkipCoeff: false},
		{MBSkipCoeff: false},
		{MBSkipCoeff: true},
	}
	if got := InterFrameModeSkipFalseProbability(1, 3, modes, 128); got != 170 {
		t.Fatalf("2/3 skip false prob = %d, want libvpx floor 170", got)
	}
	if got := InterFrameModeSkipFalseProbability(1, 0, nil, 77); got != 77 {
		t.Fatalf("empty skip false prob = %d, want fallback", got)
	}
	if got := InterFrameModeSkipFalseProbability(1, 1, []InterFrameMacroblockMode{{MBSkipCoeff: true}}, 128); got != 1 {
		t.Fatalf("all-skipped skip false prob = %d, want libvpx clipped 1", got)
	}
}

func TestDefaultBaseSkipFalseTableHasQIndexRangeLength(t *testing.T) {
	if len(DefaultBaseSkipFalseProbs) != common.QIndexRange {
		t.Fatalf("skip false table length = %d, want %d", len(DefaultBaseSkipFalseProbs), common.QIndexRange)
	}
}
