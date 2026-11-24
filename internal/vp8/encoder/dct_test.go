package encoder

import "testing"

func TestForwardDCT4x4Sentinels(t *testing.T) {
	cases := []struct {
		name  string
		input [16]int16
		want  [16]int16
	}{
		{name: "zero", want: [16]int16{0, 1}},
		{
			name:  "dc",
			input: [16]int16{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
			want:  [16]int16{40, 1},
		},
		{
			name:  "ramp",
			input: [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7},
			want:  [16]int16{-4, -8, 0, 0, -35, 0, 0, 0, 0, 0, 0, 0, -2, 0, 0, 0},
		},
	}

	for _, tc := range cases {
		var got [16]int16
		ForwardDCT4x4(tc.input[:], 4, &got)
		if got != tc.want {
			t.Fatalf("%s DCT = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestForwardDCT8x4MatchesTwoDCT4x4Blocks(t *testing.T) {
	input := [32]int16{
		-8, -7, -6, -5, -4, -3, -2, -1,
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23,
	}
	var got [32]int16
	var left, right [16]int16

	ForwardDCT8x4(input[:], 8, &got)
	ForwardDCT4x4(input[:], 8, &left)
	ForwardDCT4x4(input[4:], 8, &right)

	var want [32]int16
	copy(want[0:16], left[:])
	copy(want[16:32], right[:])
	if got != want {
		t.Fatalf("DCT8x4 = %v, want %v", got, want)
	}
}

func TestForwardWalsh4x4Sentinels(t *testing.T) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var got [16]int16
	ForwardWalsh4x4(input[:], 4, &got)
	want := [16]int16{-3, -8, 0, -4, -32, 0, 0, 0, 0, 0, 0, 0, -16, 0, 0, 0}
	if got != want {
		t.Fatalf("Walsh4x4 = %v, want %v", got, want)
	}
}

func TestForwardTransformsAllocateZero(t *testing.T) {
	input := [32]int16{
		-8, -7, -6, -5, -4, -3, -2, -1,
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23,
	}
	var dct4 [16]int16
	var dct8 [32]int16
	var walsh [16]int16
	allocs := testing.AllocsPerRun(1000, func() {
		ForwardDCT4x4(input[:], 8, &dct4)
		ForwardDCT8x4(input[:], 8, &dct8)
		ForwardWalsh4x4(input[:], 8, &walsh)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkForwardDCT4x4(b *testing.B) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var output [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ForwardDCT4x4(input[:], 4, &output)
	}
}

func BenchmarkForwardWalsh4x4(b *testing.B) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var output [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ForwardWalsh4x4(input[:], 4, &output)
	}
}
