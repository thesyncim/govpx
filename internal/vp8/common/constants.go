package common

// Ported constants from libvpx v1.16.0:
// - vp8/common/blockd.h
// - vp8/common/entropymv.h
// - vp8/common/filter.h
// - vp8/common/onyxc_int.h

const (
	DCPredSimThresh = 0
	DCPredCntThresh = 3

	MBFeatureTreeProbs = 3
	MaxMBSegments      = 4

	MaxRefLFDeltas  = 4
	MaxModeLFDeltas = 4

	SegmentDeltaData = 0
	SegmentAbsData   = 1

	SegmentAltQ  = 0x01
	SegmentAltLF = 0x02
)

const (
	PlaneTypeYNoDC   = 0
	PlaneTypeY2      = 1
	PlaneTypeUV      = 2
	PlaneTypeYWithDC = 3
)

const (
	VP8YModes      = int(BPred) + 1
	VP8UVModes     = int(TMPred) + 1
	VP8MVRefs      = 1 + int(SplitMV) - int(NearestMV)
	VP8BIntraModes = int(BHUPred) + 1
	VP8SubMVRefs   = 1 + int(New4x4) - int(Left4x4)
)

const (
	MinQ        = 0
	MaxQ        = 127
	QIndexRange = MaxQ + 1

	NumYV12Buffers = 4
	MaxPartitions  = 9
)

const (
	MVMax        = 1023
	MVVals       = 2*MVMax + 1
	MVFullPxMax  = 255
	MVFullPxVals = 2*MVFullPxMax + 1

	MVLongWidth = 10
	MVNumShort  = 8

	MVPIsShort = 0
	MVPSign    = MVPIsShort + 1
	MVPShort   = MVPSign + 1
	MVPBits    = MVPShort + MVNumShort - 1
	MVPCount   = MVPBits + MVLongWidth
)

const (
	BlockHeightWidth = 4
	VP8FilterWeight  = 128
	VP8FilterShift   = 7
)
