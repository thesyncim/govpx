package encoder

import "testing"

func TestPublicQuantizerMappingMatchesLibvpxTable(t *testing.T) {
	want := []int{
		0, 4, 8, 12, 16, 20, 24, 28,
		32, 36, 40, 44, 48, 52, 56, 60,
		64, 68, 72, 76, 80, 84, 88, 92,
		96, 100, 104, 108, 112, 116, 120, 124,
		128, 132, 136, 140, 144, 148, 152, 156,
		160, 164, 168, 172, 176, 180, 184, 188,
		192, 196, 200, 204, 208, 212, 216, 220,
		224, 228, 232, 236, 240, 244, 249, 255,
	}
	for q, qindex := range want {
		if got := PublicQuantizerToQIndex(q); got != qindex {
			t.Fatalf("PublicQuantizerToQIndex(%d) = %d, want %d",
				q, got, qindex)
		}
		if got := QIndexToPublicQuantizer(qindex); got != q {
			t.Fatalf("QIndexToPublicQuantizer(%d) = %d, want %d",
				qindex, got, q)
		}
	}
}

func TestPublicQuantizerMappingClamps(t *testing.T) {
	if got := PublicQuantizerToQIndex(-1); got != 0 {
		t.Fatalf("PublicQuantizerToQIndex(-1) = %d, want 0", got)
	}
	if got := PublicQuantizerToQIndex(MaxPublicQuantizer + 1); got != 255 {
		t.Fatalf("PublicQuantizerToQIndex(max+1) = %d, want 255", got)
	}
	if got := QIndexToPublicQuantizer(300); got != MaxPublicQuantizer {
		t.Fatalf("QIndexToPublicQuantizer(300) = %d, want %d", got, MaxPublicQuantizer)
	}
}

func TestPublicQModeInterRateCadence(t *testing.T) {
	cases := []struct {
		frameIndex int
		num        int
		den        int
	}{
		{0, 1, 2},
		{1, 1, 1},
		{2, 85, 100},
		{4, 7, 10},
		{6, 85, 100},
		{8, 1, 2},
	}
	for _, c := range cases {
		num, den := PublicQModeInterRate(c.frameIndex)
		if num != c.num || den != c.den {
			t.Fatalf("PublicQModeInterRate(%d) = %d/%d, want %d/%d",
				c.frameIndex, num, den, c.num, c.den)
		}
	}
}
