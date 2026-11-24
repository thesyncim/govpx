package decoder

import "testing"

func TestApplyCorruptInterFrameRefreshKeepsOnlyLastRefresh(t *testing.T) {
	state := StateHeader{
		Refresh: RefreshHeader{
			RefreshGolden:       true,
			RefreshAltRef:       true,
			CopyBufferToGolden:  2,
			CopyBufferToAltRef:  1,
			RefreshEntropyProbs: true,
		},
	}

	ApplyCorruptInterFrameRefresh(&state)

	if !state.Refresh.RefreshLast {
		t.Fatalf("RefreshLast = false, want true")
	}
	if state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef {
		t.Fatalf("RefreshGolden/RefreshAltRef = %t/%t, want false/false", state.Refresh.RefreshGolden, state.Refresh.RefreshAltRef)
	}
	if state.Refresh.CopyBufferToGolden != 0 || state.Refresh.CopyBufferToAltRef != 0 {
		t.Fatalf("copy buffers = %d/%d, want 0/0", state.Refresh.CopyBufferToGolden, state.Refresh.CopyBufferToAltRef)
	}
	if state.Refresh.RefreshEntropyProbs {
		t.Fatalf("RefreshEntropyProbs = true, want false")
	}
}
