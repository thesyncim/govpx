package govpx

import (
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestInterFrameAnalysisSkipFalseProbMirrorsLibvpxHistorySelection(t *testing.T) {
	e := &VP8Encoder{baseSkipFalseProbs: vp8enc.DefaultBaseSkipFalseProbs}
	if got := e.interFrameAnalysisSkipFalseProb(0, false, false, false); got != 250 {
		t.Fatalf("base skip false prob = %d, want q0 base clamped to 250", got)
	}
	if got := e.interFrameAnalysisSkipFalseProb(127, false, false, false); got != 16 {
		t.Fatalf("base skip false prob = %d, want q127 base 16", got)
	}

	e.lastSkipFalseProbs = [3]uint8{77, 66, 3}
	if got := e.interFrameAnalysisSkipFalseProb(127, false, false, false); got != 77 {
		t.Fatalf("last-frame skip false prob = %d, want 77", got)
	}
	if got := e.interFrameAnalysisSkipFalseProb(127, true, false, false); got != 66 {
		t.Fatalf("golden skip false prob = %d, want 66", got)
	}
	if got := e.interFrameAnalysisSkipFalseProb(127, false, true, false); got != 5 {
		t.Fatalf("altref skip false prob = %d, want clamp-to-5", got)
	}
	if got := e.interFrameAnalysisSkipFalseProb(127, false, true, true); got != 1 {
		t.Fatalf("visible alt-ref source skip false prob = %d, want forced 1", got)
	}
}

func TestCommitInterFrameSkipFalseProbUpdatesReferenceAndBaseHistory(t *testing.T) {
	e := &VP8Encoder{baseSkipFalseProbs: vp8enc.DefaultBaseSkipFalseProbs}
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
		baseSkipFalseProbs: vp8enc.DefaultBaseSkipFalseProbs,
	}
	e.baseSkipFalseProbs[64] = 91
	e.Reset()
	if e.probSkipFalse != 128 || e.lastSkipFalseProbs != ([3]uint8{}) || e.baseSkipFalseProbs != vp8enc.DefaultBaseSkipFalseProbs {
		t.Fatalf("reset skip false state = prob:%d last:%v base64:%d", e.probSkipFalse, e.lastSkipFalseProbs, e.baseSkipFalseProbs[64])
	}
}
