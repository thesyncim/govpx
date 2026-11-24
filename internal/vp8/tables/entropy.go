package tables

// Ported static entropy tables from libvpx v1.16.0 vp8/common/entropy.c and
// constants from vp8/common/entropy.h.

const (
	ZeroToken       = 0
	OneToken        = 1
	TwoToken        = 2
	ThreeToken      = 3
	FourToken       = 4
	DCTValCategory1 = 5
	DCTValCategory2 = 6
	DCTValCategory3 = 7
	DCTValCategory4 = 8
	DCTValCategory5 = 9
	DCTValCategory6 = 10
	DCTEOBToken     = 11

	MaxEntropyTokens       = 12
	EntropyNodes           = 11
	ProbUpdateBaselineCost = 7
	MaxProb                = 255
	DCTMaxValue            = 2048
	BlockTypes             = 4
	CoefBands              = 8
	PrevCoefContexts       = 3
)

var BoolNorm = [256]uint8{
	0, 7, 6, 6, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

var CoefBandsTable = [16]uint8{
	0, 1, 2, 3, 6, 4, 5, 6,
	6, 6, 6, 6, 6, 6, 6, 7,
}

var PrevTokenClass = [MaxEntropyTokens]uint8{
	0, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 0,
}

var DefaultZigZag1D = [16]int16{
	0, 1, 4, 8, 5, 2, 3, 6,
	9, 12, 13, 10, 7, 11, 14, 15,
}

var DefaultInvZigZag = [16]int16{
	1, 2, 6, 7, 3, 5, 8, 13,
	4, 9, 12, 14, 10, 11, 15, 16,
}

var DefaultZigZagMask = [16]int16{
	1, 2, 32, 64, 4, 16, 128, 4096,
	8, 256, 2048, 8192, 512, 1024, 16384, -32768,
}

var MBFeatureDataBits = [2]int8{7, 6}

var CoefTree = [22]int16{
	-DCTEOBToken, 2,
	-ZeroToken, 4,
	-OneToken, 6,
	8, 12,
	-TwoToken, 10,
	-ThreeToken, -FourToken,
	14, 16,
	-DCTValCategory1, -DCTValCategory2,
	18, 20,
	-DCTValCategory3, -DCTValCategory4,
	-DCTValCategory5, -DCTValCategory6,
}

var CoefEncodings = [MaxEntropyTokens]Token{
	{Value: 2, Len: 2},
	{Value: 6, Len: 3},
	{Value: 28, Len: 5},
	{Value: 58, Len: 6},
	{Value: 59, Len: 6},
	{Value: 60, Len: 6},
	{Value: 61, Len: 6},
	{Value: 124, Len: 7},
	{Value: 125, Len: 7},
	{Value: 126, Len: 7},
	{Value: 127, Len: 7},
	{Value: 0, Len: 1},
}

var (
	Cat1Prob = [1]uint8{159}
	Cat2Prob = [2]uint8{165, 145}
	Cat3Prob = [3]uint8{173, 148, 140}
	Cat4Prob = [4]uint8{176, 155, 140, 135}
	Cat5Prob = [5]uint8{180, 157, 141, 134, 130}
	Cat6Prob = [11]uint8{254, 254, 243, 230, 196, 177, 153, 140, 133, 130, 129}

	Cat1Tree = [2]int16{0, 0}
	Cat2Tree = [4]int16{2, 2, 0, 0}
	Cat3Tree = [6]int16{2, 2, 4, 4, 0, 0}
	Cat4Tree = [8]int16{2, 2, 4, 4, 6, 6, 0, 0}
	Cat5Tree = [10]int16{2, 2, 4, 4, 6, 6, 8, 8, 0, 0}
	Cat6Tree = [22]int16{2, 2, 4, 4, 6, 6, 8, 8, 10, 10, 12, 12, 14, 14, 16, 16, 18, 18, 20, 20, 0, 0}
)

var ExtraBitsTable = [MaxEntropyTokens]ExtraBits{
	{},
	{BaseVal: 1},
	{BaseVal: 2},
	{BaseVal: 3},
	{BaseVal: 4},
	{Tree: Cat1Tree[:], Prob: Cat1Prob[:], Len: 1, BaseVal: 5},
	{Tree: Cat2Tree[:], Prob: Cat2Prob[:], Len: 2, BaseVal: 7},
	{Tree: Cat3Tree[:], Prob: Cat3Prob[:], Len: 3, BaseVal: 11},
	{Tree: Cat4Tree[:], Prob: Cat4Prob[:], Len: 4, BaseVal: 19},
	{Tree: Cat5Tree[:], Prob: Cat5Prob[:], Len: 5, BaseVal: 35},
	{Tree: Cat6Tree[:], Prob: Cat6Prob[:], Len: 11, BaseVal: 67},
	{},
}
