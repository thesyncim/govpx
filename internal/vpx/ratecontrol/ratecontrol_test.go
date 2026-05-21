package ratecontrol

import "testing"

func TestEncodedSizeBitsSaturates(t *testing.T) {
	if got := EncodedSizeBits(0); got != 0 {
		t.Fatalf("EncodedSizeBits(0) = %d, want 0", got)
	}
	if got := EncodedSizeBits(-1); got != 0 {
		t.Fatalf("EncodedSizeBits(-1) = %d, want 0", got)
	}
	if got := EncodedSizeBits(12); got != 96 {
		t.Fatalf("EncodedSizeBits(12) = %d, want 96", got)
	}
	if got := EncodedSizeBits(maxInt()); got != maxInt() {
		t.Fatalf("EncodedSizeBits(maxInt) = %d, want maxInt", got)
	}
}

func TestNormalizePercentUsesFallbackOnlyForZero(t *testing.T) {
	if got := NormalizePercent(0, 100); got != 100 {
		t.Fatalf("NormalizePercent(0, 100) = %d, want 100", got)
	}
	if got := NormalizePercent(-5, 100); got != -5 {
		t.Fatalf("NormalizePercent(-5, 100) = %d, want -5", got)
	}
	if got := NormalizePercent(125, 100); got != 125 {
		t.Fatalf("NormalizePercent(125, 100) = %d, want 125", got)
	}
}
