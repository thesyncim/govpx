package encoder

import "testing"

func TestRateControlTableRegulatedQuantizerUsesLibvpxBitsPerMBModel(t *testing.T) {
	if got := LibvpxRegulatedQuantizer(false, 12000, 60, 4, 56, 1.0); got != 24 {
		t.Fatalf("inter regulated quantizer = %d, want libvpx table q24", got)
	}
	if got := LibvpxRegulatedQuantizer(true, 72000, 60, 4, 56, 1.0); got != 4 {
		t.Fatalf("key regulated quantizer = %d, want min-clamped q4", got)
	}
}

func TestRateControlTableRegulatedQuantizerTracksLibvpxZbinOverQuant(t *testing.T) {
	q, zbin := LibvpxRegulatedQuantizerWithZbin(false, false, 1, 1, 4, 127, 1.0)
	if q != 127 || zbin != libvpxZbinOverQuantMax {
		t.Fatalf("max inter regulated q/zbin = %d/%d, want 127/%d", q, zbin, libvpxZbinOverQuantMax)
	}

	q, zbin = LibvpxRegulatedQuantizerWithZbin(false, true, 1, 1, 4, 127, 1.0)
	if q != 127 || zbin != 16 {
		t.Fatalf("golden regulated q/zbin = %d/%d, want 127/16", q, zbin)
	}

	q, zbin = LibvpxRegulatedQuantizerWithZbin(true, false, 1, 1, 4, 127, 1.0)
	if q != 127 || zbin != 0 {
		t.Fatalf("key regulated q/zbin = %d/%d, want 127/0", q, zbin)
	}

	q, zbin = LibvpxRegulatedQuantizerWithZbin(false, false, 12000, 60, 4, 127, 1.0)
	if q != 24 || zbin != 0 {
		t.Fatalf("ordinary inter regulated q/zbin = %d/%d, want 24/0", q, zbin)
	}
}

func TestRateControlTableEstimatedBitsAtQuantizerMatchesLibvpxFormula(t *testing.T) {
	// libvpx carries fractional bits-per-MB through the multiplication by
	// macroblock count before truncating; doing the truncate earlier shifts
	// long recode-loop correction-factor trajectories.
	for _, mb := range []int{1, 60, 1024, (1 << 11) + 1, 3600} {
		for _, q := range []int{0, 24, 64, 96, 127} {
			rcf := 1.5
			want := int((0.5+rcf*float64(LibvpxBitsPerMB[1][q]))*float64(mb)) >> libvpxBPerMBNormBits
			if got := LibvpxEstimatedBitsAtQuantizer(1, q, mb, rcf); got != want {
				t.Fatalf("estimate(q=%d, mb=%d) = %d, want %d", q, mb, got, want)
			}
		}
	}
}

func TestRateControlTableEstimatedBitsAtQuantizerWithZbinAppliesLibvpxFactorWalk(t *testing.T) {
	frameType := 1
	q := 96
	macroblocks := 60
	correctionFactor := 1.5
	base := LibvpxEstimatedBitsAtQuantizer(frameType, q, macroblocks, correctionFactor)

	if got := LibvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, correctionFactor, 0); got != base {
		t.Fatalf("zbin=0 estimate = %d, want unchanged %d", got, base)
	}

	want := base
	factor := 0.99
	const factorAdjustment = 0.01 / 256.0
	for z := 4; z > 0; z-- {
		want = int(factor * float64(want))
		factor += factorAdjustment
		if factor >= 0.999 {
			factor = 0.999
		}
	}
	if got := LibvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, correctionFactor, 4); got != want {
		t.Fatalf("zbin=4 estimate = %d, want %d", got, want)
	}

	prev := base
	for z := 1; z <= 16; z++ {
		got := LibvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, correctionFactor, z)
		if got > prev {
			t.Fatalf("zbin=%d estimate %d exceeds zbin=%d estimate %d", z, got, z-1, prev)
		}
		prev = got
	}
}
