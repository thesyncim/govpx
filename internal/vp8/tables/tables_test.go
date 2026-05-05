package tables

import "testing"

func TestEntropyTableSentinels(t *testing.T) {
	if BoolNorm[0] != 0 || BoolNorm[1] != 7 || BoolNorm[127] != 1 || BoolNorm[128] != 0 {
		t.Fatalf("BoolNorm sentinels changed")
	}
	if sumU8(CoefBandsTable[:]) != 76 {
		t.Fatalf("CoefBands checksum = %d, want 76", sumU8(CoefBandsTable[:]))
	}
	if sumU8(PrevTokenClass[:]) != 19 {
		t.Fatalf("PrevTokenClass checksum = %d, want 19", sumU8(PrevTokenClass[:]))
	}
	if sumI16(DefaultZigZag1D[:]) != 120 {
		t.Fatalf("DefaultZigZag1D checksum = %d, want 120", sumI16(DefaultZigZag1D[:]))
	}
	if sumI16(DefaultInvZigZag[:]) != 136 {
		t.Fatalf("DefaultInvZigZag checksum = %d, want 136", sumI16(DefaultInvZigZag[:]))
	}
	if sumI16(DefaultZigZagMask[:]) != -1 {
		t.Fatalf("DefaultZigZagMask checksum = %d, want -1", sumI16(DefaultZigZagMask[:]))
	}
	if sumI8(MBFeatureDataBits[:]) != 13 {
		t.Fatalf("MBFeatureDataBits checksum = %d, want 13", sumI8(MBFeatureDataBits[:]))
	}
	if CoefTree[0] != -DCTEOBToken || CoefTree[21] != -DCTValCategory6 {
		t.Fatalf("CoefTree sentinels changed")
	}
	if CoefEncodings[ZeroToken] != (Token{Value: 2, Len: 2}) || CoefEncodings[DCTEOBToken] != (Token{Value: 0, Len: 1}) {
		t.Fatalf("CoefEncodings sentinels changed")
	}
	if ExtraBitsTable[DCTValCategory6].BaseVal != 67 || ExtraBitsTable[DCTValCategory6].Len != 11 {
		t.Fatalf("category 6 extra bits changed")
	}
	if sumCoefUpdateProbs() != 268469 {
		t.Fatalf("CoefUpdateProbs checksum = %d, want 268469", sumCoefUpdateProbs())
	}
	if CoefUpdateProbs[0][1][0][0] != 176 || CoefUpdateProbs[3][7][1][0] != 254 {
		t.Fatalf("CoefUpdateProbs sentinels changed")
	}
	if sumDefaultCoefProbs() != 174918 {
		t.Fatalf("DefaultCoefProbs checksum = %d, want 174918", sumDefaultCoefProbs())
	}
	if DefaultCoefProbs[0][0][0][0] != 128 || DefaultCoefProbs[3][7][2][10] != 128 {
		t.Fatalf("DefaultCoefProbs sentinels changed")
	}
}

func TestModeTableSentinels(t *testing.T) {
	if sumU8(MBSplits[0][:]) != 8 || sumU8(MBSplits[3][:]) != 120 {
		t.Fatalf("MBSplits checksums changed")
	}
	if sumI8(MBSplitCount[:]) != 24 {
		t.Fatalf("MBSplitCount checksum = %d, want 24", sumI8(MBSplitCount[:]))
	}
	if sumU8(MBSplitProbs[:]) != 371 {
		t.Fatalf("MBSplitProbs checksum = %d, want 371", sumU8(MBSplitProbs[:]))
	}
	if BModeTree[0] != 0 || BModeTree[17] != -9 {
		t.Fatalf("BModeTree sentinels changed")
	}
	if MVRefTree[0] != -7 || MVRefTree[7] != -9 {
		t.Fatalf("MVRefTree sentinels changed")
	}
	if SubMVRefProb2[4][0] != 208 || SubMVRefProb2[4][2] != 1 {
		t.Fatalf("SubMVRefProb2 sentinels changed")
	}
}

func TestMVTableSentinels(t *testing.T) {
	if len(MVUpdateProbs[0]) != MVPCount || len(DefaultMVContext[0]) != MVPCount {
		t.Fatalf("MV table widths changed")
	}
	if MVUpdateProbs[0][0] != 237 || MVUpdateProbs[1][18] != 254 {
		t.Fatalf("MVUpdateProbs sentinels changed")
	}
	if DefaultMVContext[0][0] != 162 || DefaultMVContext[1][8] != 228 {
		t.Fatalf("DefaultMVContext sentinels changed")
	}
}

func TestFilterTableSentinels(t *testing.T) {
	if sumFilter2(BilinearFilters[:]) != 1024 {
		t.Fatalf("BilinearFilters checksum = %d, want 1024", sumFilter2(BilinearFilters[:]))
	}
	if sumFilter6(SubPelFilters[:]) != 1024 {
		t.Fatalf("SubPelFilters checksum = %d, want 1024", sumFilter6(SubPelFilters[:]))
	}
	if SubPelFilters[4] != ([6]int16{3, -16, 77, 77, -16, 3}) {
		t.Fatalf("half-pel filter changed")
	}
}

func TestTableAccessAllocatesZero(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		_ = BoolNorm[1]
		_ = CoefBandsTable[15]
		_ = DefaultMVContext[1][18]
		_ = SubPelFilters[4][2]
	})
	if allocs != 0 {
		t.Fatalf("table access allocs = %v, want 0", allocs)
	}
}

func sumU8(v []uint8) int {
	sum := 0
	for _, x := range v {
		sum += int(x)
	}
	return sum
}

func sumI8(v []int8) int {
	sum := 0
	for _, x := range v {
		sum += int(x)
	}
	return sum
}

func sumI16(v []int16) int {
	sum := 0
	for _, x := range v {
		sum += int(x)
	}
	return sum
}

func sumFilter2(v [][2]int16) int {
	sum := 0
	for _, row := range v {
		sum += int(row[0]) + int(row[1])
	}
	return sum
}

func sumFilter6(v [][6]int16) int {
	sum := 0
	for _, row := range v {
		for _, x := range row {
			sum += int(x)
		}
	}
	return sum
}

func sumCoefUpdateProbs() int {
	sum := 0
	for block := range CoefUpdateProbs {
		for band := range CoefUpdateProbs[block] {
			for ctx := range CoefUpdateProbs[block][band] {
				for _, v := range CoefUpdateProbs[block][band][ctx] {
					sum += int(v)
				}
			}
		}
	}
	return sum
}

func sumDefaultCoefProbs() int {
	sum := 0
	for block := range DefaultCoefProbs {
		for band := range DefaultCoefProbs[block] {
			for ctx := range DefaultCoefProbs[block][band] {
				for _, v := range DefaultCoefProbs[block][band][ctx] {
					sum += int(v)
				}
			}
		}
	}
	return sum
}
