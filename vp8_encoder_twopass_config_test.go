package govpx

import (
	"math"
	"testing"
)

func TestTwoPassFrameTargetBitsUsesLiveVBRMaxWhenBudgetAhead(t *testing.T) {
	stats := makeTwoPassSpikyStats(10)
	var ts twoPassState
	ts.configure(stats, 1000, 100, 50, 200)
	ts.bitsLeft = 20000

	want := libvpxFrameMaxBitsVBR(ts.bitsLeft, int64(len(stats)), ts.maxPct)
	if want != 4000 {
		t.Fatalf("test setup VBR max = %d, want 4000", want)
	}
	if got := ts.frameTargetBits(0, false, 1000); got != want {
		t.Fatalf("two-pass target ahead of budget = %d, want live VBR max %d", got, want)
	}
}

// TestTwoPassFrameTargetBitsUsesLiveVBRMaxWhenBudgetBehind pins the
// symmetric case: when bits_left has fallen below the initial average
// budget, the libvpx frame_max_bits cap tightens.
func TestTwoPassFrameTargetBitsUsesLiveVBRMaxWhenBudgetBehind(t *testing.T) {
	stats := makeTwoPassSpikyStats(10)
	var ts twoPassState
	ts.configure(stats, 1000, 100, 50, 200)
	ts.bitsLeft = 5000

	want := libvpxFrameMaxBitsVBR(ts.bitsLeft, int64(len(stats)), ts.maxPct)
	if want != 1000 {
		t.Fatalf("test setup VBR max = %d, want 1000", want)
	}
	if got := ts.frameTargetBits(0, false, 1000); got != want {
		t.Fatalf("two-pass target behind budget = %d, want live VBR max %d", got, want)
	}
}

func TestTwoPassConfigureConsumesTerminalTotalStats(t *testing.T) {
	frames := []FirstPassFrameStats{
		{CodedError: 100, SSIMWeightedPredErr: 100, Count: 1},
		{CodedError: 900, SSIMWeightedPredErr: 900, Count: 1},
	}
	total := FirstPassFrameStats{CodedError: 1000, SSIMWeightedPredErr: 1000, Count: 2}
	var ts twoPassState
	ts.configure(append(frames, total), 1000, 50, 1, 1000)

	if got := len(ts.stats); got != 2 {
		t.Fatalf("two-pass frame stats length = %d, want terminal total excluded", got)
	}
	if ts.bitsLeft != 1980 {
		t.Fatalf("bitsLeft = %d, want two real frames minus min-frame reserve", ts.bitsLeft)
	}
	want := libvpxCalculateModifiedErr(100, 1000, 2, 50) +
		libvpxCalculateModifiedErr(900, 1000, 2, 50)
	if math.Abs(ts.errorLeft-want) > 1e-9 {
		t.Fatalf("errorLeft = %v, want libvpx terminal-total modified error %v", ts.errorLeft, want)
	}
}

func TestTwoPassConfigureSynthesizesTotalStatsWhenMissing(t *testing.T) {
	stats := []FirstPassFrameStats{
		{CodedError: 100, SSIMWeightedPredErr: 100, Count: 1},
		{CodedError: 900, SSIMWeightedPredErr: 900, Count: 1},
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 1, 1000)

	if ts.totalStats.SSIMWeightedPredErr != 1000 || ts.totalStats.Count != 2 {
		t.Fatalf("total stats = %+v, want synthesized SSIM=1000 Count=2", ts.totalStats)
	}
	want := libvpxCalculateModifiedErr(100, 1000, 2, 50) +
		libvpxCalculateModifiedErr(900, 1000, 2, 50)
	if math.Abs(ts.errorLeft-want) > 1e-9 {
		t.Fatalf("errorLeft = %v, want synthesized-total modified error %v", ts.errorLeft, want)
	}
}
