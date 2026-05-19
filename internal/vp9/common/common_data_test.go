package common

import "testing"

func TestAlignToSB(t *testing.T) {
	for _, tc := range []struct {
		miCount int
		want    int
	}{
		{0, 0},
		{1, MiBlockSize},
		{MiBlockSize - 1, MiBlockSize},
		{MiBlockSize, MiBlockSize},
		{MiBlockSize + 1, 2 * MiBlockSize},
	} {
		if got := AlignToSB(tc.miCount); got != tc.want {
			t.Fatalf("AlignToSB(%d) = %d, want %d", tc.miCount, got, tc.want)
		}
	}
}

// TestBlockGeometryConsistent checks that the four geometry lookups
// (log2 width, log2 height, 4x4 counts, 8x8 counts) all agree with each
// other. A typo in any one of them would make the partition tree
// inconsistent in subtle ways at decode time, so it's worth a unit test.
func TestBlockGeometryConsistent(t *testing.T) {
	for bs := range BlockSizes {
		wLog2 := BWidthLog2Lookup[bs]
		hLog2 := BHeightLog2Lookup[bs]

		// width in 4x4 sub-blocks is 1<<wLog2 (since BLOCK_4X4 has w=4=1*4).
		want4x4w := uint8(1 << wLog2)
		if Num4x4BlocksWideLookup[bs] != want4x4w {
			t.Errorf("Num4x4BlocksWideLookup[%d] = %d, want %d", bs, Num4x4BlocksWideLookup[bs], want4x4w)
		}
		want4x4h := uint8(1 << hLog2)
		if Num4x4BlocksHighLookup[bs] != want4x4h {
			t.Errorf("Num4x4BlocksHighLookup[%d] = %d, want %d", bs, Num4x4BlocksHighLookup[bs], want4x4h)
		}

		// 8x8 counts are 4x4 counts halved (min 1).
		want8x8w := uint8(1)
		if want4x4w > 1 {
			want8x8w = want4x4w >> 1
		}
		if Num8x8BlocksWideLookup[bs] != want8x8w {
			t.Errorf("Num8x8BlocksWideLookup[%d] = %d, want %d", bs, Num8x8BlocksWideLookup[bs], want8x8w)
		}
		want8x8h := uint8(1)
		if want4x4h > 1 {
			want8x8h = want4x4h >> 1
		}
		if Num8x8BlocksHighLookup[bs] != want8x8h {
			t.Errorf("Num8x8BlocksHighLookup[%d] = %d, want %d", bs, Num8x8BlocksHighLookup[bs], want8x8h)
		}

		// Pixel count log2 is wLog2 + hLog2 + 4 (since 4x4 = 16 pixels = 2^4).
		wantPels := wLog2 + hLog2 + 4
		if NumPelsLog2Lookup[bs] != wantPels {
			t.Errorf("NumPelsLog2Lookup[%d] = %d, want %d", bs, NumPelsLog2Lookup[bs], wantPels)
		}
	}
}

// TestSubsizeIdentity ensures PartitionNone recovers the original block
// size for every shape — this is the contract every parser relies on.
func TestSubsizeIdentity(t *testing.T) {
	for bs := range BlockSizes {
		if got := SubsizeLookup[PartitionNone][bs]; got != bs {
			t.Errorf("SubsizeLookup[None][%d] = %d, want %d (identity)", bs, got, bs)
		}
	}
}

// TestSsSizeIdentity checks that the (ss_x=0, ss_y=0) — i.e. 4:4:4
// chroma — projection is the identity for every valid block size,
// matching libvpx's table layout.
func TestSsSizeIdentity(t *testing.T) {
	for bs := range BlockSizes {
		if got := SsSizeLookup[bs][0][0]; got != bs {
			t.Errorf("SsSizeLookup[%d][0][0] = %d, want %d (4:4:4 identity)", bs, got, bs)
		}
	}
}

// TestMaxTxsizeWithinBlock checks that MaxTxsizeLookup never claims a
// transform larger than the block can hold.
func TestMaxTxsizeWithinBlock(t *testing.T) {
	for bs := range BlockSizes {
		txDim := uint8(1 << (uint(MaxTxsizeLookup[bs]) + 2)) // Tx4x4 -> 4
		w := Num4x4BlocksWideLookup[bs] * 4
		h := Num4x4BlocksHighLookup[bs] * 4
		if txDim > w || txDim > h {
			t.Errorf("MaxTxsizeLookup[%d] = %d (%dpx) exceeds block %dx%d", bs, MaxTxsizeLookup[bs], txDim, w, h)
		}
	}
}
