package encoder

import "testing"

func TestMacroblockCountRoundsMIGridTo16x16Blocks(t *testing.T) {
	tests := []struct {
		name       string
		miRows     int
		miCols     int
		macroblock int
	}{
		{name: "empty", miRows: 0, miCols: 0, macroblock: 0},
		{name: "single mi", miRows: 1, miCols: 1, macroblock: 1},
		{name: "one macroblock", miRows: 2, miCols: 2, macroblock: 1},
		{name: "round odd rows and cols", miRows: 3, miCols: 5, macroblock: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MacroblockCount(tt.miRows, tt.miCols); got != tt.macroblock {
				t.Fatalf("MacroblockCount(%d, %d) = %d, want %d",
					tt.miRows, tt.miCols, got, tt.macroblock)
			}
		})
	}
}

func TestNormalizeRateCorrectionFactorClampsLibvpxRange(t *testing.T) {
	tests := []struct {
		factor float64
		want   float64
	}{
		{factor: -1, want: 1},
		{factor: 0, want: 1},
		{factor: MinBPBFactor / 2, want: MinBPBFactor},
		{factor: 1.25, want: 1.25},
		{factor: MaxBPBFactor * 2, want: MaxBPBFactor},
	}
	for _, tt := range tests {
		if got := NormalizeRateCorrectionFactor(tt.factor); got != tt.want {
			t.Fatalf("NormalizeRateCorrectionFactor(%g) = %g, want %g",
				tt.factor, got, tt.want)
		}
	}
}

func TestBitsPerMBFallsAsQuantizerRises(t *testing.T) {
	lowQ := BitsPerMB(false, 32, 1)
	midQ := BitsPerMB(false, 128, 1)
	highQ := BitsPerMB(false, 240, 1)
	if !(lowQ > midQ && midQ > highQ) {
		t.Fatalf("BitsPerMB monotonicity = q32:%d q128:%d q240:%d",
			lowQ, midQ, highQ)
	}
	if intra := BitsPerMB(true, 128, 1); intra <= midQ {
		t.Fatalf("intra BitsPerMB(128) = %d, want above inter %d", intra, midQ)
	}
}

func TestRegulatedQuantizerRespectsActiveRange(t *testing.T) {
	if got := RegulatedQuantizer(false, 0, 16, 12, 64, 1); got != 12 {
		t.Fatalf("zero target quantizer = %d, want active best", got)
	}
	q := RegulatedQuantizer(false, 4000, 16, 12, 64, 1)
	if q < 12 || q > 64 {
		t.Fatalf("regulated q = %d, want inside [12,64]", q)
	}
	coarser := RegulatedQuantizer(false, 4000, 16, 20, 64, 1)
	if coarser < 20 || coarser > 64 {
		t.Fatalf("regulated q with raised active best = %d, want inside [20,64]",
			coarser)
	}
}

func TestRegulatedQuantizerMatchesBitsPerMBModel(t *testing.T) {
	cases := []struct {
		name             string
		intraOnly        bool
		targetBits       int
		macroblocks      int
		activeBest       int
		activeWorst      int
		correctionFactor float64
	}{
		{name: "inter low target", targetBits: 4000, macroblocks: 16, activeBest: 12, activeWorst: 64, correctionFactor: 1.0},
		{name: "inter high correction", targetBits: 9000, macroblocks: 48, activeBest: 8, activeWorst: 160, correctionFactor: 2.5},
		{name: "intra", intraOnly: true, targetBits: 12000, macroblocks: 64, activeBest: 4, activeWorst: 180, correctionFactor: 0.75},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RegulatedQuantizer(tc.intraOnly, tc.targetBits, tc.macroblocks,
				tc.activeBest, tc.activeWorst, tc.correctionFactor)
			want := RegulatedQuantizerWithBitsPerMB(tc.intraOnly, tc.targetBits,
				tc.macroblocks, tc.activeBest, tc.activeWorst, func(qindex int) int {
					return BitsPerMB(tc.intraOnly, qindex, tc.correctionFactor)
				})
			if got != want {
				t.Fatalf("RegulatedQuantizer = %d, callback model = %d", got, want)
			}
		})
	}
}

func TestComputeQDeltaByRateDirection(t *testing.T) {
	if got := ComputeQDeltaByRate(0, 255, false, 96, 0, 1); got != 0 {
		t.Fatalf("invalid ratio delta = %d, want 0", got)
	}
	moreBits := ComputeQDeltaByRate(0, 255, false, 96, 2, 1)
	fewerBits := ComputeQDeltaByRate(0, 255, false, 96, 1, 2)
	if moreBits >= 0 {
		t.Fatalf("2x-rate delta = %d, want lower qindex", moreBits)
	}
	if fewerBits <= 0 {
		t.Fatalf("half-rate delta = %d, want higher qindex", fewerBits)
	}
}

func TestActiveQualityHelpersReturnValidQIndex(t *testing.T) {
	for _, fn := range []struct {
		name string
		call func(int) int
	}{
		{name: "rtc", call: RTCMinQ},
		{name: "inter", call: InterMinQ},
		{name: "keyframe", call: KFActiveQuality},
		{name: "golden", call: GFActiveQuality},
		{name: "golden low motion", call: GFLowMotionActiveQuality},
		{name: "golden high motion", call: GFHighMotionActiveQuality},
	} {
		t.Run(fn.name, func(t *testing.T) {
			q := fn.call(140)
			if q < 0 || q > 255 {
				t.Fatalf("%s active quality = %d, want valid qindex", fn.name, q)
			}
			if q > 140 {
				t.Fatalf("%s active quality = %d, want no worse than input", fn.name, q)
			}
		})
	}
}
