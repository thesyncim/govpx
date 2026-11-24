package common

import "testing"

// TestDcAcQuantEndpoints checks the corner values of the dequant tables
// match libvpx exactly. A single byte off in these tables would shift
// every reconstructed coefficient on the affected qindex, so these are
// our canary values.
func TestDcAcQuantEndpoints(t *testing.T) {
	tests := []struct {
		name   string
		bd     BitDepth
		dc0    int16
		dc255  int16
		ac0    int16
		ac255  int16
		dcMid  int16
		acMid  int16
		dcMidQ int
		acMidQ int
	}{
		// Values read directly out of libvpx v1.16.0 vp9_quant_common.c at
		// the specified qindex. Endpoints (q=0, q=255) bracket the table;
		// the mid point catches a row-boundary miscount.
		{"8bit", Bits8, 4, 1336, 4, 1828, 37, 42, 35, 35},
		{"10bit", Bits10, 4, 5347, 4, 7312, 120, 136, 35, 35},
		{"12bit", Bits12, 4, 21387, 4, 29247, 453, 511, 35, 35},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DcQuant(0, 0, tc.bd); got != tc.dc0 {
				t.Errorf("DcQuant(0)=%d, want %d", got, tc.dc0)
			}
			if got := DcQuant(255, 0, tc.bd); got != tc.dc255 {
				t.Errorf("DcQuant(255)=%d, want %d", got, tc.dc255)
			}
			if got := AcQuant(0, 0, tc.bd); got != tc.ac0 {
				t.Errorf("AcQuant(0)=%d, want %d", got, tc.ac0)
			}
			if got := AcQuant(255, 0, tc.bd); got != tc.ac255 {
				t.Errorf("AcQuant(255)=%d, want %d", got, tc.ac255)
			}
			if got := DcQuant(tc.dcMidQ, 0, tc.bd); got != tc.dcMid {
				t.Errorf("DcQuant(%d)=%d, want %d", tc.dcMidQ, got, tc.dcMid)
			}
			if got := AcQuant(tc.acMidQ, 0, tc.bd); got != tc.acMid {
				t.Errorf("AcQuant(%d)=%d, want %d", tc.acMidQ, got, tc.acMid)
			}
		})
	}
}

// TestQuantClamps verifies the delta + clamp path against the table
// endpoints in both directions.
func TestQuantClamps(t *testing.T) {
	if got := ClampQIndex(-10); got != MinQ {
		t.Fatalf("ClampQIndex(-10) = %d, want %d", got, MinQ)
	}
	if got := ClampQIndex(MaxQ + 10); got != MaxQ {
		t.Fatalf("ClampQIndex(MaxQ+10) = %d, want %d", got, MaxQ)
	}
	if got := ClampQIndex(37); got != 37 {
		t.Fatalf("ClampQIndex(37) = %d, want 37", got)
	}
	if DcQuant(0, -10, Bits8) != DcQuant(0, 0, Bits8) {
		t.Error("DcQuant should clamp negative below MinQ")
	}
	if DcQuant(250, 100, Bits8) != DcQuant(255, 0, Bits8) {
		t.Error("DcQuant should clamp above MaxQ")
	}
	if AcQuant(0, -10, Bits10) != AcQuant(0, 0, Bits10) {
		t.Error("AcQuant should clamp negative below MinQ in 10-bit")
	}
	if AcQuant(250, 100, Bits12) != AcQuant(255, 0, Bits12) {
		t.Error("AcQuant should clamp above MaxQ in 12-bit")
	}
}

// TestQuantAlloc asserts the lookup is allocation-free.
func TestQuantAlloc(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		for q := range 256 {
			_ = DcQuant(q, 0, Bits8)
			_ = AcQuant(q, 0, Bits10)
		}
	})
	if allocs != 0 {
		t.Fatalf("DcQuant/AcQuant: got %v allocs/op, want 0", allocs)
	}
}
