package dsp

import "testing"

// TestIdct4x4ZeroInputIdentity checks that the empty-residual case is a
// no-op on dest — the most common path through the decoder for blocks
// that detokenize to all zeros.
func TestIdct4x4ZeroInputIdentity(t *testing.T) {
	var input [16]int16
	dest := [16]uint8{
		10, 20, 30, 40,
		50, 60, 70, 80,
		90, 100, 110, 120,
		130, 140, 150, 160,
	}
	want := dest

	Idct4x4_16Add(input[:], dest[:], 4)
	if dest != want {
		t.Errorf("Idct4x4_16Add with zero input changed dest: got %v want %v", dest, want)
	}

	dest = want
	Idct4x4_1Add(input[:], dest[:], 4)
	if dest != want {
		t.Errorf("Idct4x4_1Add with zero input changed dest: got %v want %v", dest, want)
	}
}

// TestIdct4x4DcOnlyMatchesFullPath checks the fast DC-only path produces
// the same pixels as the full 16-coefficient path when only input[0] is
// non-zero. This is the libvpx fast-path invariant.
func TestIdct4x4DcOnlyMatchesFullPath(t *testing.T) {
	for _, dc := range []int16{-64, -8, -1, 1, 8, 64, 128, 256} {
		input := [16]int16{}
		input[0] = dc

		destFull := [16]uint8{
			128, 128, 128, 128,
			128, 128, 128, 128,
			128, 128, 128, 128,
			128, 128, 128, 128,
		}
		destFast := destFull

		Idct4x4_16Add(input[:], destFull[:], 4)
		Idct4x4_1Add(input[:], destFast[:], 4)

		if destFull != destFast {
			t.Errorf("dc=%d: full=%v fast=%v", dc, destFull, destFast)
		}
	}
}

// TestIdct4x4Stride exercises the stride parameter — many decoder
// callers pass a stride wider than 4 (the frame's plane stride).
func TestIdct4x4Stride(t *testing.T) {
	const stride = 16
	dest := make([]uint8, stride*4)
	for i := range dest {
		dest[i] = uint8(i & 0xff)
	}
	want := append([]uint8(nil), dest...)

	var input [16]int16
	Idct4x4_16Add(input[:], dest, stride)
	for i := range dest {
		if dest[i] != want[i] {
			t.Fatalf("at %d: got %d want %d", i, dest[i], want[i])
		}
	}
}

// TestIwht4x4ZeroInputIdentity is the lossless-mode equivalent of the
// IDCT zero-input check.
func TestIwht4x4ZeroInputIdentity(t *testing.T) {
	var input [16]int16
	dest := [16]uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	want := dest

	Iwht4x4_16Add(input[:], dest[:], 4)
	if dest != want {
		t.Errorf("Iwht4x4_16Add with zero input changed dest: got %v want %v", dest, want)
	}
}

// TestIdct4x4Alloc verifies the kernel runs without allocations.
func TestIdct4x4Alloc(t *testing.T) {
	var input [16]int16
	for i := range input {
		input[i] = int16((i * 17) - 64)
	}
	dest := make([]uint8, 16)

	allocs := testing.AllocsPerRun(200, func() {
		Idct4x4_16Add(input[:], dest, 4)
	})
	if allocs != 0 {
		t.Fatalf("Idct4x4_16Add: got %v allocs/op, want 0", allocs)
	}
}
