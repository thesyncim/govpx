package dsp

import "testing"

func TestDequantizeBlock(t *testing.T) {
	qcoeff := [16]int16{
		-8, -7, -6, -5,
		-4, -3, -2, -1,
		0, 1, 2, 3,
		4, 5, 6, 7,
	}
	dequant := [16]int16{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
		13, 14, 15, 16,
	}
	var dqcoeff [16]int16

	DequantizeBlock(&qcoeff, &dequant, &dqcoeff)

	for i := 0; i < 16; i++ {
		want := qcoeff[i] * dequant[i]
		if dqcoeff[i] != want {
			t.Fatalf("dqcoeff[%d] = %d, want %d", i, dqcoeff[i], want)
		}
	}
}

func TestDequantIDCT4x4AddMatchesManualAndZerosInput(t *testing.T) {
	input := [16]int16{
		80, -3, 2, -1,
		4, 0, -2, 1,
		3, -1, 0, 2,
		-2, 1, 0, -1,
	}
	original := input
	dequant := [16]int16{
		2, 3, 4, 5,
		6, 7, 8, 9,
		10, 11, 12, 13,
		14, 15, 16, 17,
	}
	dst := make([]byte, 8*8)
	want := make([]byte, 8*8)
	for i := range dst {
		dst[i] = byte((i*7 + 30) & 255)
		want[i] = dst[i]
	}

	var manual [16]int16
	for i := 0; i < 16; i++ {
		manual[i] = original[i] * dequant[i]
	}
	IDCT4x4Add(&manual, want, 8, want, 8)

	DequantIDCT4x4Add(&input, &dequant, dst, 8)

	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst[%d] = %d, want %d", i, dst[i], want[i])
		}
	}
	for i, v := range input {
		if v != 0 {
			t.Fatalf("input[%d] = %d, want zero", i, v)
		}
	}
}

func TestDequantAllocatesZero(t *testing.T) {
	var input [16]int16
	var dequant [16]int16
	var dqcoeff [16]int16
	dst := make([]byte, 8*8)
	allocs := testing.AllocsPerRun(1000, func() {
		DequantizeBlock(&input, &dequant, &dqcoeff)
		DequantIDCT4x4Add(&input, &dequant, dst, 8)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkDequantizeBlock(b *testing.B) {
	var qcoeff [16]int16
	var dequant [16]int16
	var dqcoeff [16]int16
	for i := range qcoeff {
		qcoeff[i] = int16(i*3 - 20)
		dequant[i] = int16(i + 1)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DequantizeBlock(&qcoeff, &dequant, &dqcoeff)
	}
}

func BenchmarkDequantIDCT4x4Add(b *testing.B) {
	var input [16]int16
	var dequant [16]int16
	dst := make([]byte, 8*8)
	for i := range input {
		input[i] = int16(i*3 - 20)
		dequant[i] = int16(i + 1)
	}
	seed := input
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		input = seed
		DequantIDCT4x4Add(&input, &dequant, dst, 8)
	}
}
