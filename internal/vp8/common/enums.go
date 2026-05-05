package common

// Ported enum values from libvpx v1.16.0:
// - vp8/common/blockd.h
// - vp8/common/entropymode.h
// - vp8/common/onyx.h
// - vp8/common/onyxc_int.h

type FrameType int

const (
	KeyFrame FrameType = iota
	InterFrame
)

type MBPredictionMode int

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

type MBLvlFeature int

const (
	MBLvlAltQ MBLvlFeature = iota
	MBLvlAltLF
	MBLvlMax
)

type BPredictionMode int

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

type MVReferenceFrame int

const (
	IntraFrame MVReferenceFrame = iota
	LastFrame
	GoldenFrame
	AltRefFrame
	MaxRefFrames
)

type TokenPartition int

const (
	OnePartition TokenPartition = iota
	TwoPartition
	FourPartition
	EightPartition
)

type ClampType int

const (
	ReconClampRequired ClampType = iota
	ReconClampNotRequired
)

type SubMVRef int

const (
	SubMVRefNormal SubMVRef = iota
	SubMVRefLeftZero
	SubMVRefAboveZero
	SubMVRefLeftAboveSame
	SubMVRefLeftAboveZero
	SubMVRefCount
)

type EndUsage int

const (
	UsageLocalFilePlayback EndUsage = iota
	UsageStreamFromServer
	UsageConstrainedQuality
	UsageConstantQuality
)

type EncoderMode int

const (
	ModeRealtime EncoderMode = iota
	ModeGoodQuality
	ModeBestQuality
	ModeFirstPass
	ModeSecondPass
	ModeSecondPassBest
)

type FrameTypeFlags int

const (
	FrameFlagsKey FrameTypeFlags = 1 << iota
	FrameFlagsGolden
	FrameFlagsAltRef
)
