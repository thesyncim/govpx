package common

// Ported enum values from libvpx v1.16.0:
// - vp8/common/blockd.h
// - vp8/common/entropymode.h
// - vp8/common/onyx.h
// - vp8/common/onyxc_int.h

type FrameType uint8

const (
	KeyFrame FrameType = iota
	InterFrame
)

type MBPredictionMode uint8

const (
	DCPred MBPredictionMode = iota
	VPred
	HPred
	TMPred
	BPred
	NearestMV
	NearMV
	ZeroMV
	NewMV
	SplitMV
	MBModeCount
)

type MBLvlFeature uint8

const (
	MBLvlAltQ MBLvlFeature = iota
	MBLvlAltLF
	MBLvlMax
)

type BPredictionMode uint8

const (
	BDCPred BPredictionMode = iota
	BTMPred
	BVEPred
	BHEPred
	BLDPred
	BRDPred
	BVRPred
	BVLPred
	BHDPred
	BHUPred
	Left4x4
	Above4x4
	Zero4x4
	New4x4
	BModeCount
)

type MVReferenceFrame uint8

const (
	IntraFrame MVReferenceFrame = iota
	LastFrame
	GoldenFrame
	AltRefFrame
	MaxRefFrames
)

type TokenPartition uint8

const (
	OnePartition TokenPartition = iota
	TwoPartition
	FourPartition
	EightPartition
)

type ClampType uint8

const (
	ReconClampRequired ClampType = iota
	ReconClampNotRequired
)

type SubMVRef uint8

const (
	SubMVRefNormal SubMVRef = iota
	SubMVRefLeftZero
	SubMVRefAboveZero
	SubMVRefLeftAboveSame
	SubMVRefLeftAboveZero
	SubMVRefCount
)

type EndUsage uint8

const (
	UsageLocalFilePlayback EndUsage = iota
	UsageStreamFromServer
	UsageConstrainedQuality
	UsageConstantQuality
)

type EncoderMode uint8

const (
	ModeRealtime EncoderMode = iota
	ModeGoodQuality
	ModeBestQuality
	ModeFirstPass
	ModeSecondPass
	ModeSecondPassBest
)

type FrameTypeFlags uint8

const (
	FrameFlagsKey FrameTypeFlags = 1 << iota
	FrameFlagsGolden
	FrameFlagsAltRef
)
