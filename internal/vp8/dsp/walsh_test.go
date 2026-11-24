package dsp

import "testing"

func TestInverseWalsh4x4KnownVector(t *testing.T) {
	input := [16]int16{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
		13, 14, 15, 16,
	}
	coeff := make([]int16, 16*16)
	for i := range coeff {
		coeff[i] = -999
	}

	InverseWalsh4x4(&input, coeff)

	want := [16]int16{
		17, -2, 0, -1,
		-8, 0, 0, 0,
		0, 0, 0, 0,
		-4, 0, 0, 0,
	}
	for i, w := range want {
		if got := coeff[i*16]; got != w {
			t.Fatalf("coeff[%d] = %d, want %d", i*16, got, w)
		}
	}
	for i, got := range coeff {
		if i%16 == 0 {
			continue
		}
		if got != -999 {
			t.Fatalf("coeff[%d] = %d, want sentinel", i, got)
		}
	}
}

func TestDCOnlyInverseWalsh4x4MatchesFullForDCOnly(t *testing.T) {
	for dc := int16(-512); dc <= 512; dc += 17 {
		var input [16]int16
		input[0] = dc
		full := make([]int16, 16*16)
		dcOnly := make([]int16, 16*16)

		InverseWalsh4x4(&input, full)
		DCOnlyInverseWalsh4x4(dc, dcOnly)

		for i := range 16 {
			if full[i*16] != dcOnly[i*16] {
				t.Fatalf("dc=%d coeff[%d] full=%d dcOnly=%d", dc, i*16, full[i*16], dcOnly[i*16])
			}
		}
	}
}

func TestInverseWalshAllocatesZero(t *testing.T) {
	var input [16]int16
	coeff := make([]int16, 16*16)
	allocs := testing.AllocsPerRun(1000, func() {
		InverseWalsh4x4(&input, coeff)
		DCOnlyInverseWalsh4x4(input[0], coeff)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkInverseWalsh4x4(b *testing.B) {
	var input [16]int16
	for i := range input {
		input[i] = int16(i*7 - 48)
	}
	coeff := make([]int16, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		InverseWalsh4x4(&input, coeff)
	}
}

func BenchmarkDCOnlyInverseWalsh4x4(b *testing.B) {
	coeff := make([]int16, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DCOnlyInverseWalsh4x4(128, coeff)
	}
}
