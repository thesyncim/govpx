package tables

// Ported static mode tables from libvpx v1.16.0:
// - vp8/common/entropymode.c
// - vp8/common/entropymode.h
// - vp8/common/vp8_entropymodedata.h

const (
	NumMBSplits   = 4
	SubMVRefCount = 5

	YModeProbCount  = 4
	UVModeProbCount = 3
	BModeProbCount  = 9
)

var DefaultYModeProbs = [YModeProbCount]uint8{112, 86, 140, 37}

var KeyFrameYModeProbs = [YModeProbCount]uint8{145, 156, 163, 128}

var DefaultUVModeProbs = [UVModeProbCount]uint8{162, 101, 204}

var KeyFrameUVModeProbs = [UVModeProbCount]uint8{142, 114, 183}

var DefaultBModeProbs = [BModeProbCount]uint8{120, 90, 79, 133, 87, 85, 80, 111, 151}

var SubMVRefProb2 = [SubMVRefCount][3]uint8{
	{147, 136, 18},
	{106, 145, 1},
	{179, 121, 1},
	{223, 1, 34},
	{208, 1, 1},
}

var MBSplits = [NumMBSplits][16]uint8{
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 1, 2, 2, 3, 3, 2, 2, 3, 3},
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
}

var MBSplitCount = [NumMBSplits]int8{2, 2, 4, 16}

var MBSplitProbs = [NumMBSplits - 1]uint8{110, 111, 150}

var BModeTree = [18]int16{
	0, 2,
	-1, 4,
	-2, 6,
	8, 12,
	-3, 10,
	-5, -6,
	-4, 14,
	-7, 16,
	-8, -9,
}

var YModeTree = [8]int16{-0, 2, 4, 6, -1, -2, -3, -4}

var KeyFrameYModeTree = [8]int16{-4, 2, 4, 6, -0, -1, -2, -3}

var UVModeTree = [6]int16{-0, 2, -1, 4, -2, -3}

var MBSplitTree = [6]int16{-3, 2, -2, 4, -0, -1}

var MVRefTree = [8]int16{-7, 2, -5, 4, -6, 6, -8, -9}

var SubMVRefTree = [6]int16{-10, 2, -11, 4, -12, -13}

var SmallMVTree = [14]int16{2, 8, 4, 6, -0, -1, -2, -3, 10, 12, -4, -5, -6, -7}
