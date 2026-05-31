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

func TestBitsPerFrameRoundsLikeLibvpx(t *testing.T) {
	cases := []struct {
		name      string
		bandwidth int
		fps       float64
		num       int
		den       int
		dur       int
		want      int
	}{
		{name: "rational rounds up", bandwidth: 100_000, num: 1, den: 60, dur: 1, want: 1667},
		{name: "rational exact", bandwidth: 900_000, num: 1, den: 30, dur: 1, want: 30000},
		{name: "float rounds up", bandwidth: 100_000, fps: 60, want: 1667},
		{name: "float exact", bandwidth: 900_000, fps: 30, want: 30000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BitsPerFrame(tc.bandwidth, tc.fps, tc.num, tc.den, tc.dur)
			if got != tc.want {
				t.Fatalf("BitsPerFrame(...) = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestClampToRawTargetRateKbpsUsesLibvpxEnvelope(t *testing.T) {
	// libvpx computes raw_target_rate as width * height * bit_depth * 3 *
	// framerate / 1000 and truncates the result to an integer kbps value.
	if got := RawTargetRateKbps(32, 32, 8, 30); got != 737 {
		t.Fatalf("RawTargetRateKbps(32x32@30) = %d, want 737", got)
	}
	if got := ClampToRawTargetRateKbps(10_000, 32, 32, 8, 30); got != 737 {
		t.Fatalf("ClampToRawTargetRateKbps high rate = %d, want 737", got)
	}
	if got := ClampToRawTargetRateKbps(300, 32, 32, 8, 30); got != 300 {
		t.Fatalf("ClampToRawTargetRateKbps low rate = %d, want unchanged 300", got)
	}
	if got := ClampToRawTargetRateKbps(300, 0, 32, 8, 30); got != 300 {
		t.Fatalf("ClampToRawTargetRateKbps missing dimensions = %d, want unchanged 300", got)
	}
}
