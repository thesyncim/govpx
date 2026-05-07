package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestLibvpxBaseSkipFalseProbTable(t *testing.T) {
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
		if got := libvpxBaseSkipFalseProb(tt.qIndex); got != tt.want {
			t.Fatalf("base skip false prob q=%d = %d, want %d", tt.qIndex, got, tt.want)
		}
	}
}

func TestInterFrameAnalysisSkipFalseProbMirrorsLibvpxHistorySelection(t *testing.T) {
	e := &VP8Encoder{baseSkipFalseProbs: libvpxBaseSkipFalseProbs}
	if got := e.interFrameAnalysisSkipFalseProb(0, false, false); got != 250 {
		t.Fatalf("base skip false prob = %d, want q0 base clamped to 250", got)
	}
	if got := e.interFrameAnalysisSkipFalseProb(127, false, false); got != 16 {
		t.Fatalf("base skip false prob = %d, want q127 base 16", got)
	}

	e.lastSkipFalseProbs = [3]uint8{77, 66, 3}
	if got := e.interFrameAnalysisSkipFalseProb(127, false, false); got != 77 {
		t.Fatalf("last-frame skip false prob = %d, want 77", got)
	}
	if got := e.interFrameAnalysisSkipFalseProb(127, true, false); got != 66 {
		t.Fatalf("golden skip false prob = %d, want 66", got)
	}
	if got := e.interFrameAnalysisSkipFalseProb(127, false, true); got != 5 {
		t.Fatalf("altref skip false prob = %d, want clamp-to-5", got)
	}
}

func TestInterFrameModeSkipFalseProbabilityMatchesWriterCounts(t *testing.T) {
	modes := []vp8enc.InterFrameMacroblockMode{
		{MBSkipCoeff: false},
		{MBSkipCoeff: true},
		{MBSkipCoeff: true},
		{MBSkipCoeff: true},
	}
	if got := interFrameModeSkipFalseProbability(1, 4, modes, 128); got != 64 {
		t.Fatalf("skip false prob = %d, want 64", got)
	}
	if got := interFrameModeSkipFalseProbability(1, 0, nil, 77); got != 77 {
		t.Fatalf("empty skip false prob = %d, want fallback", got)
	}
	if got := interFrameModeSkipFalseProbability(1, 1, []vp8enc.InterFrameMacroblockMode{{MBSkipCoeff: true}}, 128); got != 1 {
		t.Fatalf("all-skipped skip false prob = %d, want libvpx clipped 1", got)
	}
}

func TestCommitInterFrameSkipFalseProbUpdatesReferenceAndBaseHistory(t *testing.T) {
	e := &VP8Encoder{baseSkipFalseProbs: libvpxBaseSkipFalseProbs}
	e.commitInterFrameSkipFalseProb(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{
		BaseQIndex:    64,
		ProbSkipFalse: 91,
		RefreshLast:   true,
	}})
	if e.probSkipFalse != 91 || e.lastSkipFalseProbs[0] != 91 || e.baseSkipFalseProbs[64] != 91 {
		t.Fatalf("last refresh history = prob:%d last:%d base:%d, want 91/91/91", e.probSkipFalse, e.lastSkipFalseProbs[0], e.baseSkipFalseProbs[64])
	}

	e.commitInterFrameSkipFalseProb(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{
		BaseQIndex:    64,
		ProbSkipFalse: 82,
		RefreshGolden: true,
	}})
	if e.lastSkipFalseProbs[1] != 82 || e.baseSkipFalseProbs[64] != 91 {
		t.Fatalf("golden refresh history = golden:%d base:%d, want 82 and unchanged base 91", e.lastSkipFalseProbs[1], e.baseSkipFalseProbs[64])
	}

	e.commitInterFrameSkipFalseProb(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{
		BaseQIndex:    64,
		ProbSkipFalse: 73,
		RefreshAltRef: true,
	}})
	if e.lastSkipFalseProbs[2] != 73 || e.baseSkipFalseProbs[64] != 91 {
		t.Fatalf("altref refresh history = alt:%d base:%d, want 73 and unchanged base 91", e.lastSkipFalseProbs[2], e.baseSkipFalseProbs[64])
	}
}

func TestResetRestoresSkipFalseProbabilityState(t *testing.T) {
	e := &VP8Encoder{
		probSkipFalse:      91,
		lastSkipFalseProbs: [3]uint8{91, 82, 73},
		baseSkipFalseProbs: libvpxBaseSkipFalseProbs,
	}
	e.baseSkipFalseProbs[64] = 91
	e.Reset()
	if e.probSkipFalse != 128 || e.lastSkipFalseProbs != ([3]uint8{}) || e.baseSkipFalseProbs != libvpxBaseSkipFalseProbs {
		t.Fatalf("reset skip false state = prob:%d last:%v base64:%d", e.probSkipFalse, e.lastSkipFalseProbs, e.baseSkipFalseProbs[64])
	}
}

func TestSkipFalseTableHasVP8QIndexRangeLength(t *testing.T) {
	if len(libvpxBaseSkipFalseProbs) != vp8common.QIndexRange {
		t.Fatalf("skip false table length = %d, want %d", len(libvpxBaseSkipFalseProbs), vp8common.QIndexRange)
	}
}
