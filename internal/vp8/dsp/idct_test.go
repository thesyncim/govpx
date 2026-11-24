package dsp

import "testing"

func TestDCOnlyIDCT4x4AddMatchesFullIDCTForDCOnly(t *testing.T) {
	for dc := int16(-512); dc <= 512; dc += 17 {
		var input [16]int16
		input[0] = dc
		pred := make([]byte, 8*8)
		for i := range pred {
			pred[i] = byte((i*7 + 13) & 255)
		}
		full := make([]byte, 8*8)
		dcOnly := make([]byte, 8*8)

		IDCT4x4Add(&input, pred, 8, full, 8)
		DCOnlyIDCT4x4Add(dc, pred, 8, dcOnly, 8)

		for y := range 4 {
			for x := range 4 {
				if full[y*8+x] != dcOnly[y*8+x] {
					t.Fatalf("dc=%d pixel[%d,%d] full=%d dcOnly=%d", dc, x, y, full[y*8+x], dcOnly[y*8+x])
				}
			}
		}
	}
}

func TestIDCT4x4AddClips(t *testing.T) {
	var input [16]int16
	input[0] = 4096
	pred := make([]byte, 4*4)
	dst := make([]byte, 4*4)
	for i := range pred {
		pred[i] = 250
	}

	IDCT4x4Add(&input, pred, 4, dst, 4)
	for i, v := range dst {
		if v != 255 {
			t.Fatalf("dst[%d] = %d, want 255", i, v)
		}
	}
}

func TestDCOnlyIDCT4x4AddInt32MatchesScalar(t *testing.T) {
	pred := make([]byte, 8*8)
	for i := range pred {
		pred[i] = byte((i*11 + 5) & 255)
	}
	cases := []int32{
		-1 << 20,
		-4096,
		-2049,
		-2048,
		-17,
		-1,
		0,
		1,
		17,
		2047,
		2048,
		4096,
		1 << 20,
	}
	for _, dc := range cases {
		got := make([]byte, 8*8)
		want := make([]byte, 8*8)
		DCOnlyIDCT4x4AddInt32(dc, pred, 8, got, 8)
		dcOnlyIDCT4x4AddInt32Scalar(dc, pred, 8, want, 8)
		for y := range 4 {
			for x := range 4 {
				if got[y*8+x] != want[y*8+x] {
					t.Fatalf("dc=%d [%d,%d]: got=%d want=%d", dc, x, y, got[y*8+x], want[y*8+x])
				}
			}
		}
	}
}

func TestDCOnlyIDCT4x4AddPairInt32MatchesSingles(t *testing.T) {
	pred := make([]byte, 8*8)
	for i := range pred {
		pred[i] = byte((i*13 + 9) & 255)
	}
	cases := [][2]int32{
		{-1 << 20, 1 << 20},
		{-4096, 4096},
		{-2048, 2047},
		{-17, 17},
		{0, 1},
		{128 * 132, -91 * 132},
	}
	for _, tc := range cases {
		got := make([]byte, 8*8)
		want := make([]byte, 8*8)
		copy(got, pred)
		copy(want, pred)

		DCOnlyIDCT4x4AddPairInt32(tc[0], tc[1], got, 8, got, 8)
		DCOnlyIDCT4x4AddInt32(tc[0], want, 8, want, 8)
		DCOnlyIDCT4x4AddInt32(tc[1], want[4:], 8, want[4:], 8)

		for y := range 4 {
			for x := range 8 {
				if got[y*8+x] != want[y*8+x] {
					t.Fatalf("dc=(%d,%d) [%d,%d]: got=%d want=%d", tc[0], tc[1], x, y, got[y*8+x], want[y*8+x])
				}
			}
		}
	}
}

func TestIDCTAllocatesZero(t *testing.T) {
	var input [16]int16
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	allocs := testing.AllocsPerRun(1000, func() {
		IDCT4x4Add(&input, pred, 8, dst, 8)
		DCOnlyIDCT4x4Add(input[0], pred, 8, dst, 8)
		DCOnlyIDCT4x4AddPairInt32(128*132, -64*132, pred, 8, dst, 8)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func dcOnlyIDCT4x4AddInt32Scalar(inputDC int32, pred []byte, predStride int, dst []byte, dstStride int) {
	a1 := int((inputDC + 4) >> 3)
	for y := range 4 {
		dstRow := dst[y*dstStride : y*dstStride+4 : y*dstStride+4]
		predRow := pred[y*predStride : y*predStride+4 : y*predStride+4]
		dstRow[0] = ClipPixel(a1 + int(predRow[0]))
		dstRow[1] = ClipPixel(a1 + int(predRow[1]))
		dstRow[2] = ClipPixel(a1 + int(predRow[2]))
		dstRow[3] = ClipPixel(a1 + int(predRow[3]))
	}
}

func BenchmarkIDCT4x4Add(b *testing.B) {
	var input [16]int16
	for i := range input {
		input[i] = int16(i*9 - 40)
	}
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IDCT4x4Add(&input, pred, 8, dst, 8)
	}
}

func BenchmarkDCOnlyIDCT4x4Add(b *testing.B) {
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DCOnlyIDCT4x4Add(128, pred, 8, dst, 8)
	}
}

func BenchmarkDCOnlyIDCT4x4AddInt32(b *testing.B) {
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DCOnlyIDCT4x4AddInt32(128*132, pred, 8, dst, 8)
	}
}

func BenchmarkDCOnlyIDCT4x4AddPairInt32(b *testing.B) {
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DCOnlyIDCT4x4AddPairInt32(128*132, -64*132, pred, 8, dst, 8)
	}
}

func BenchmarkDCOnlyIDCT4x4AddPairInt32Singles(b *testing.B) {
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DCOnlyIDCT4x4AddInt32(128*132, pred, 8, dst, 8)
		DCOnlyIDCT4x4AddInt32(-64*132, pred[4:], 8, dst[4:], 8)
	}
}

func BenchmarkDCOnlyIDCT4x4AddInt32Scalar(b *testing.B) {
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dcOnlyIDCT4x4AddInt32Scalar(128*132, pred, 8, dst, 8)
	}
}
