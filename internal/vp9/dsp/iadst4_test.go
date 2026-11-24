package dsp

import "testing"

// TestIadst4ZeroInputZeroOutput is the libvpx fast-path invariant — all
// zeros into iadst4_c must give all zeros out.
func TestIadst4ZeroInputZeroOutput(t *testing.T) {
	var input, output [4]int16
	output[0] = 0x1234
	Iadst4(input[:], output[:])
	for i, v := range output {
		if v != 0 {
			t.Errorf("output[%d] = %d, want 0", i, v)
		}
	}
}

// TestIht4x4DispatchMatchesIdct4x4 checks that Iht4x4_16Add with the
// DCT_DCT txType produces the same pixels as the dedicated
// Idct4x4_16Add fast path — the contract the detokenizer relies on.
func TestIht4x4DispatchMatchesIdct4x4(t *testing.T) {
	input := [16]int16{
		64, -32, 16, -8,
		-16, 8, -4, 2,
		8, -4, 2, -1,
		-4, 2, -1, 0,
	}
	destA := [16]uint8{
		100, 110, 120, 130,
		140, 150, 160, 170,
		180, 190, 200, 210,
		220, 230, 240, 250,
	}
	destB := destA

	Idct4x4_16Add(input[:], destA[:], 4)
	Iht4x4_16Add(input[:], destB[:], 4, 0) // 0 = DCT_DCT

	if destA != destB {
		t.Errorf("DCT_DCT dispatch diverges: idct=%v iht=%v", destA, destB)
	}
}

// TestIht4x4ZeroInputIdentity checks every TxType is a no-op on a
// zero-coefficient block — same invariant as the DCT-only case.
func TestIht4x4ZeroInputIdentity(t *testing.T) {
	var input [16]int16
	want := [16]uint8{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
	}
	for tx := range 4 {
		dest := want
		Iht4x4_16Add(input[:], dest[:], 4, tx)
		if dest != want {
			t.Errorf("tx=%d zero input changed dest: %v", tx, dest)
		}
	}
}

// TestIdct8x8ZeroInputIdentity is the 8x8 version of the zero-residual
// no-op invariant.
func TestIdct8x8ZeroInputIdentity(t *testing.T) {
	var input [64]int16
	var dest [64]uint8
	for i := range dest {
		dest[i] = uint8(i & 0xff)
	}
	want := dest
	Idct8x8_64Add(input[:], dest[:], 8)
	if dest != want {
		t.Error("Idct8x8_64Add with zero input changed dest")
	}
	dest = want
	Idct8x8_12Add(input[:], dest[:], 8)
	if dest != want {
		t.Error("Idct8x8_12Add with zero input changed dest")
	}
	dest = want
	Idct8x8_1Add(input[:], dest[:], 8)
	if dest != want {
		t.Error("Idct8x8_1Add with zero input changed dest")
	}
}

// TestIdct8x8DcOnlyMatchesFullPath checks the fast-path produces the
// same output as the full-path when only the DC coefficient is set.
func TestIdct8x8DcOnlyMatchesFullPath(t *testing.T) {
	for _, dc := range []int16{-128, -16, -1, 1, 16, 128, 512} {
		input := [64]int16{}
		input[0] = dc

		destFull := [64]uint8{}
		destFast := [64]uint8{}
		for i := range destFull {
			destFull[i] = 128
			destFast[i] = 128
		}

		Idct8x8_64Add(input[:], destFull[:], 8)
		Idct8x8_1Add(input[:], destFast[:], 8)

		if destFull != destFast {
			t.Errorf("dc=%d: full=%v fast=%v", dc, destFull, destFast)
		}
	}
}

// TestIdct8x8Alloc enforces zero-allocation steady state for the 8x8
// path — every coefficient flows through caller-owned scratch.
func TestIdct8x8Alloc(t *testing.T) {
	var input [64]int16
	for i := range input {
		input[i] = int16((i * 13) - 200)
	}
	dest := make([]uint8, 64)

	allocs := testing.AllocsPerRun(100, func() {
		Idct8x8_64Add(input[:], dest, 8)
	})
	if allocs != 0 {
		t.Fatalf("Idct8x8_64Add: got %v allocs/op, want 0", allocs)
	}
}
