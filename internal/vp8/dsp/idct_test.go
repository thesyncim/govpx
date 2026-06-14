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

func TestIDCT4x4AddInvalidWindowPanicsInScalar(t *testing.T) {
	var input [16]int16
	cases := []struct {
		name string
		fn   func()
	}{
		{
			name: "full-short-pred",
			fn: func() {
				IDCT4x4Add(&input, make([]byte, 3), 4, make([]byte, 16), 4)
			},
		},
		{
			name: "full-short-dst",
			fn: func() {
				IDCT4x4Add(&input, make([]byte, 16), 4, make([]byte, 3), 4)
			},
		},
		{
			name: "full-negative-pred-stride",
			fn: func() {
				IDCT4x4Add(&input, make([]byte, 16), -4, make([]byte, 16), 4)
			},
		},
		{
			name: "full-negative-dst-stride",
			fn: func() {
				IDCT4x4Add(&input, make([]byte, 16), 4, make([]byte, 16), -4)
			},
		},
		{
			name: "dc-short-pred",
			fn: func() {
				DCOnlyIDCT4x4Add(16, make([]byte, 3), 4, make([]byte, 16), 4)
			},
		},
		{
			name: "dc-short-dst",
			fn: func() {
				DCOnlyIDCT4x4Add(16, make([]byte, 16), 4, make([]byte, 3), 4)
			},
		},
		{
			name: "dc-negative-pred-stride",
			fn: func() {
				DCOnlyIDCT4x4Add(16, make([]byte, 16), -4, make([]byte, 16), 4)
			},
		},
		{
			name: "dc-negative-dst-stride",
			fn: func() {
				DCOnlyIDCT4x4Add(16, make([]byte, 16), 4, make([]byte, 16), -4)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected scalar bounds panic")
				}
			}()
			tc.fn()
		})
	}
}

func TestIDCTAllocatesZero(t *testing.T) {
	var input [16]int16
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	allocs := testing.AllocsPerRun(1000, func() {
		IDCT4x4Add(&input, pred, 8, dst, 8)
		DCOnlyIDCT4x4Add(input[0], pred, 8, dst, 8)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
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
