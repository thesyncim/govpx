package govpx

import (
	vp9codec "github.com/thesyncim/govpx/internal/vp9/codec"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

type (
	VP9Decryptor                     = vp9codec.VP9Decryptor
	VP9ExternalFrameBuffer           = vp9codec.VP9ExternalFrameBuffer
	VP9GetFrameBufferFunc            = vp9codec.VP9GetFrameBufferFunc
	VP9ReleaseFrameBufferFunc        = vp9codec.VP9ReleaseFrameBufferFunc
	VP9DecoderOptions                = vp9codec.VP9DecoderOptions
	VP9FrameInfo                     = vp9codec.VP9FrameInfo
	VP9Decoder                       = vp9codec.VP9Decoder
	VP9Encoder                       = vp9codec.VP9Encoder
	VP9AQMode                        = vp9codec.VP9AQMode
	VP9ColorSpace                    = vp9codec.VP9ColorSpace
	VP9ColorRange                    = vp9codec.VP9ColorRange
	VP9ScreenContent                 = vp9codec.VP9ScreenContent
	VP9DisableLoopfilter             = vp9codec.VP9DisableLoopfilter
	VP9EncoderOptions                = vp9codec.VP9EncoderOptions
	VP9SegmentationOptions           = vp9codec.VP9SegmentationOptions
	VP9EncodeResult                  = vp9codec.VP9EncodeResult
	VP9SpatialScalabilityConfig      = vp9codec.VP9SpatialScalabilityConfig
	VP9FirstPassFrameStats           = vp9codec.VP9FirstPassFrameStats
	VP9MultiResolutionLayerOptions   = vp9codec.VP9MultiResolutionLayerOptions
	VP9MultiResolutionEncoderOptions = vp9codec.VP9MultiResolutionEncoderOptions
	VP9MultiResolutionEncoder        = vp9codec.VP9MultiResolutionEncoder
	VP9RTPPayloadDescriptor          = vp9codec.VP9RTPPayloadDescriptor
	VP9RTPScalabilityStructure       = vp9codec.VP9RTPScalabilityStructure
	VP9RTPPictureGroup               = vp9codec.VP9RTPPictureGroup
	VP9SDPReceiverCapabilities       = vp9codec.VP9SDPReceiverCapabilities
	VP9SpatialSVCEncoderOptions      = vp9codec.VP9SpatialSVCEncoderOptions
	VP9SpatialSVCEncodeResult        = vp9codec.VP9SpatialSVCEncodeResult
	VP9SpatialSVCEncoder             = vp9codec.VP9SpatialSVCEncoder
	SearchMethods                    = vp9codec.SearchMethods
	RecodeLoopType                   = vp9codec.RecodeLoopType
	SubpelSearchMethods              = vp9codec.SubpelSearchMethods
	MotionThreshold                  = vp9codec.MotionThreshold
	TxSizeSearchMethod               = vp9codec.TxSizeSearchMethod
	AutoMinMaxMode                   = vp9codec.AutoMinMaxMode
	LpfPickMethod                    = vp9codec.LpfPickMethod
	PartitionSearchType              = vp9codec.PartitionSearchType
	FastCoeffUpdate                  = vp9codec.FastCoeffUpdate
	SubpelForceStop                  = vp9codec.SubpelForceStop
	AdaptSubpelForceStop             = vp9codec.AdaptSubpelForceStop
	MvSpeedFeatures                  = vp9codec.MvSpeedFeatures
	PartitionSearchBreakoutThr       = vp9codec.PartitionSearchBreakoutThr
	MeshPattern                      = vp9codec.MeshPattern
	OvershootDetectionCbrRt          = vp9codec.OvershootDetectionCbrRt
	SubpelSearchType                 = vp9codec.SubpelSearchType
	EnableTrellisOptMethod           = vp9codec.EnableTrellisOptMethod
	TrellisOptControl                = vp9codec.TrellisOptControl
	RdMlPartition                    = vp9codec.RdMlPartition
	SpeedFeatures                    = vp9codec.SpeedFeatures
	VP9TPLFrameDelta                 = vp9codec.VP9TPLFrameDelta
	VP9WebRTCPacketizer              = vp9codec.VP9WebRTCPacketizer
)

const (
	VP9AQNone                       = vp9codec.VP9AQNone
	VP9AQVariance                   = vp9codec.VP9AQVariance
	VP9AQComplexity                 = vp9codec.VP9AQComplexity
	VP9AQEquator360                 = vp9codec.VP9AQEquator360
	VP9AQCyclicRefresh              = vp9codec.VP9AQCyclicRefresh
	VP9AQPerceptual                 = vp9codec.VP9AQPerceptual
	VP9ColorSpaceUnknown            = vp9codec.VP9ColorSpaceUnknown
	VP9ColorSpaceBT601              = vp9codec.VP9ColorSpaceBT601
	VP9ColorSpaceBT709              = vp9codec.VP9ColorSpaceBT709
	VP9ColorSpaceSMPTE170           = vp9codec.VP9ColorSpaceSMPTE170
	VP9ColorSpaceSMPTE240           = vp9codec.VP9ColorSpaceSMPTE240
	VP9ColorSpaceBT2020             = vp9codec.VP9ColorSpaceBT2020
	VP9ColorSpaceReserved           = vp9codec.VP9ColorSpaceReserved
	VP9ColorSpaceSRGB               = vp9codec.VP9ColorSpaceSRGB
	VP9ColorRangeStudio             = vp9codec.VP9ColorRangeStudio
	VP9ColorRangeFull               = vp9codec.VP9ColorRangeFull
	VP9ScreenContentDefault         = vp9codec.VP9ScreenContentDefault
	VP9ScreenContentScreen          = vp9codec.VP9ScreenContentScreen
	VP9ScreenContentFilm            = vp9codec.VP9ScreenContentFilm
	VP9LoopfilterEnabled            = vp9codec.VP9LoopfilterEnabled
	VP9LoopfilterDisableInter       = vp9codec.VP9LoopfilterDisableInter
	VP9LoopfilterDisableAll         = vp9codec.VP9LoopfilterDisableAll
	VP9MaxSegments                  = vp9codec.VP9MaxSegments
	VP9RefFrameIntra                = vp9codec.VP9RefFrameIntra
	VP9RefFrameLast                 = vp9codec.VP9RefFrameLast
	VP9RefFrameGolden               = vp9codec.VP9RefFrameGolden
	VP9RefFrameAltRef               = vp9codec.VP9RefFrameAltRef
	VP9MaxSpatialLayers             = vp9codec.VP9MaxSpatialLayers
	MaxMultiResLayers               = vp9codec.MaxMultiResLayers
	VP9RTPMaxReferenceIndices       = vp9codec.VP9RTPMaxReferenceIndices
	VP9RTPMaxSpatialLayers          = vp9codec.VP9RTPMaxSpatialLayers
	VP9RTPMediaType                 = vp9codec.VP9RTPMediaType
	VP9RTPEncodingName              = vp9codec.VP9RTPEncodingName
	VP9RTPClockRate                 = vp9codec.VP9RTPClockRate
	VP9SDPFmtpProfileID             = vp9codec.VP9SDPFmtpProfileID
	VP9SDPFmtpMaxFrameRate          = vp9codec.VP9SDPFmtpMaxFrameRate
	VP9SDPFmtpMaxFrameSize          = vp9codec.VP9SDPFmtpMaxFrameSize
	VP9SDPFmtpProfile0              = vp9codec.VP9SDPFmtpProfile0
	VP9RTPPictureID15BitMask        = vp9codec.VP9RTPPictureID15BitMask
	SearchMethodDiamond             = vp9codec.SearchMethodDiamond
	SearchMethodNStep               = vp9codec.SearchMethodNStep
	SearchMethodHex                 = vp9codec.SearchMethodHex
	SearchMethodBigDia              = vp9codec.SearchMethodBigDia
	SearchMethodSquare              = vp9codec.SearchMethodSquare
	SearchMethodFastHex             = vp9codec.SearchMethodFastHex
	SearchMethodFastDiamond         = vp9codec.SearchMethodFastDiamond
	SearchMethodMesh                = vp9codec.SearchMethodMesh
	RecodeLoopDisallow              = vp9codec.RecodeLoopDisallow
	RecodeLoopAllowKfMaxBw          = vp9codec.RecodeLoopAllowKfMaxBw
	RecodeLoopAllowKfArfGf          = vp9codec.RecodeLoopAllowKfArfGf
	RecodeLoopAllowFirst            = vp9codec.RecodeLoopAllowFirst
	RecodeLoopAllow                 = vp9codec.RecodeLoopAllow
	SubpelTree                      = vp9codec.SubpelTree
	SubpelTreePruned                = vp9codec.SubpelTreePruned
	SubpelTreePrunedMore            = vp9codec.SubpelTreePrunedMore
	SubpelTreePrunedEvenMore        = vp9codec.SubpelTreePrunedEvenMore
	NoMotionThreshold               = vp9codec.NoMotionThreshold
	LowMotionThreshold              = vp9codec.LowMotionThreshold
	UseFullRD                       = vp9codec.UseFullRD
	UseLargestAll                   = vp9codec.UseLargestAll
	UseTx8x8                        = vp9codec.UseTx8x8
	AutoMinMaxNotInUse              = vp9codec.AutoMinMaxNotInUse
	AutoMinMaxRelaxedNeighboring    = vp9codec.AutoMinMaxRelaxedNeighboring
	AutoMinMaxStrictNeighboring     = vp9codec.AutoMinMaxStrictNeighboring
	LpfPickFromFullImage            = vp9codec.LpfPickFromFullImage
	LpfPickFromSubImage             = vp9codec.LpfPickFromSubImage
	LpfPickFromQ                    = vp9codec.LpfPickFromQ
	LpfPickMinimalLpf               = vp9codec.LpfPickMinimalLpf
	FlagEarlyTerminate              = vp9codec.FlagEarlyTerminate
	FlagSkipCompBestIntra           = vp9codec.FlagSkipCompBestIntra
	FlagSkipIntraBestInter          = vp9codec.FlagSkipIntraBestInter
	FlagSkipIntraDirMismatch        = vp9codec.FlagSkipIntraDirMismatch
	FlagSkipIntraLowVar             = vp9codec.FlagSkipIntraLowVar
	FlagSkipEighttap                = vp9codec.FlagSkipEighttap
	FlagSkipEighttapSmooth          = vp9codec.FlagSkipEighttapSmooth
	FlagSkipEighttapSharp           = vp9codec.FlagSkipEighttapSharp
	SearchPartition                 = vp9codec.SearchPartition
	FixedPartition                  = vp9codec.FixedPartition
	ReferencePartition              = vp9codec.ReferencePartition
	VarBasedPartition               = vp9codec.VarBasedPartition
	MlBasedPartition                = vp9codec.MlBasedPartition
	TwoLoop                         = vp9codec.TwoLoop
	OneLoopReduced                  = vp9codec.OneLoopReduced
	EighthPel                       = vp9codec.EighthPel
	QuarterPel                      = vp9codec.QuarterPel
	HalfPel                         = vp9codec.HalfPel
	FullPel                         = vp9codec.FullPel
	OvershootNoDetection            = vp9codec.OvershootNoDetection
	OvershootFastDetectionMaxQ      = vp9codec.OvershootFastDetectionMaxQ
	OvershootReEncodeMaxQ           = vp9codec.OvershootReEncodeMaxQ
	Use2Taps                        = vp9codec.Use2Taps
	Use4Taps                        = vp9codec.Use4Taps
	Use8Taps                        = vp9codec.Use8Taps
	Use8TapsSharp                   = vp9codec.Use8TapsSharp
	DisableTrellisOpt               = vp9codec.DisableTrellisOpt
	EnableTrellisOptM               = vp9codec.EnableTrellisOptM
	EnableTrellisOptTxRdSrcVar      = vp9codec.EnableTrellisOptTxRdSrcVar
	EnableTrellisOptTxRdResidualMse = vp9codec.EnableTrellisOptTxRdResidualMse
	VP9TargetLevelUnspecified       = vp9codec.VP9TargetLevelUnspecified
	VP9TargetLevelAuto              = vp9codec.VP9TargetLevelAuto
	VP9TargetLevelUnconstrained     = vp9codec.VP9TargetLevelUnconstrained
)

var (
	ErrVP9NotImplemented = vp9codec.ErrVP9NotImplemented
)

func NewVP9Decoder(opts VP9DecoderOptions) (*VP9Decoder, error) {
	return vp9codec.NewVP9Decoder(opts)
}

func NewVP9Encoder(opts VP9EncoderOptions) (*VP9Encoder, error) {
	return vp9codec.NewVP9Encoder(opts)
}

func VP9RealtimeCBRAutoThreadHint(width, height int) int {
	return vp9codec.VP9RealtimeCBRAutoThreadHint(width, height)
}

func FinalizeVP9FirstPassStats(stats []VP9FirstPassFrameStats) []VP9FirstPassFrameStats {
	return vp9codec.FinalizeVP9FirstPassStats(stats)
}

func RDPartitionCost(
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	ctx int, partition common.PartitionType,
) int {
	return vp9codec.RDPartitionCost(partitionProbs, ctx, partition)
}

func VP9RDCost(rdmult int, rate int, distortion uint64) uint64 {
	return vp9codec.VP9RDCost(rdmult, rate, distortion)
}

func NewVP9MultiResolutionEncoder(opts VP9MultiResolutionEncoderOptions) (*VP9MultiResolutionEncoder, error) {
	return vp9codec.NewVP9MultiResolutionEncoder(opts)
}

func ParseVP9RTPPayloadDescriptor(packet []byte) (VP9RTPPayloadDescriptor, []byte, error) {
	return vp9codec.ParseVP9RTPPayloadDescriptor(packet)
}

func VP9RTPPayloadSize(desc VP9RTPPayloadDescriptor, payload []byte) (int, error) {
	return vp9codec.VP9RTPPayloadSize(desc, payload)
}

func PackVP9RTPPayloadInto(dst []byte, desc VP9RTPPayloadDescriptor, payload []byte) (int, error) {
	return vp9codec.PackVP9RTPPayloadInto(dst, desc, payload)
}

func PackVP9RTPPayload(desc VP9RTPPayloadDescriptor, payload []byte) ([]byte, error) {
	return vp9codec.PackVP9RTPPayload(desc, payload)
}

func VP9RTPFramePacketizationSize(desc VP9RTPPayloadDescriptor, frame []byte, mtu int) (int, int, error) {
	return vp9codec.VP9RTPFramePacketizationSize(desc, frame, mtu)
}

func PacketizeVP9RTPFrameInto(dst []RTPPayloadFragment, payloadBuf []byte,
	desc VP9RTPPayloadDescriptor, frame []byte, mtu int,
) (int, int, error) {
	return vp9codec.PacketizeVP9RTPFrameInto(dst, payloadBuf, desc, frame, mtu)
}

func PacketizeVP9RTPFrame(desc VP9RTPPayloadDescriptor, frame []byte, mtu int) ([]RTPPayloadFragment, error) {
	return vp9codec.PacketizeVP9RTPFrame(desc, frame, mtu)
}

func VP9RTPFrameAssemblySize(payloads []RTPPayloadFragment) (int, error) {
	return vp9codec.VP9RTPFrameAssemblySize(payloads)
}

func AssembleVP9RTPFrameInto(dst []byte, payloads []RTPPayloadFragment) (int, error) {
	return vp9codec.AssembleVP9RTPFrameInto(dst, payloads)
}

func AssembleVP9RTPFrame(payloads []RTPPayloadFragment) ([]byte, error) {
	return vp9codec.AssembleVP9RTPFrame(payloads)
}

func AppendVP9SDPFmtpProfile0(dst []byte) []byte {
	return vp9codec.AppendVP9SDPFmtpProfile0(dst)
}

func VP9SDPFrameSizeMacroblocks(width int, height int) (int, error) {
	return vp9codec.VP9SDPFrameSizeMacroblocks(width, height)
}

func ParseVP9SDPFmtp(fmtp string) (VP9SDPReceiverCapabilities, error) {
	return vp9codec.ParseVP9SDPFmtp(fmtp)
}

func VP9SDPFmtpContainsProfile0(params string) bool {
	return vp9codec.VP9SDPFmtpContainsProfile0(params)
}

func VP9SDPNegotiatesProfile0(sdp string) bool {
	return vp9codec.VP9SDPNegotiatesProfile0(sdp)
}

func VP9SDPOffersProfile0Receive(sdp string) bool {
	return vp9codec.VP9SDPOffersProfile0Receive(sdp)
}

func VP9SDPOffersProfile0ReceiveFrame(
	sdp string,
	width int,
	height int,
	fps int,
) bool {
	return vp9codec.VP9SDPOffersProfile0ReceiveFrame(sdp, width, height, fps)
}

func VP9SDPAnswersProfile0Send(sdp string) bool {
	return vp9codec.VP9SDPAnswersProfile0Send(sdp)
}

func NewVP9SpatialSVCEncoder(opts VP9SpatialSVCEncoderOptions) (*VP9SpatialSVCEncoder, error) {
	return vp9codec.NewVP9SpatialSVCEncoder(opts)
}

func NextVP9RTPPictureID(id uint16) uint16 {
	return vp9codec.NextVP9RTPPictureID(id)
}

func VP9WebRTCSpatialLayerChangeNeedsKeyFrame(
	currentLayerCount int,
	nextLayerCount int,
) bool {
	return vp9codec.VP9WebRTCSpatialLayerChangeNeedsKeyFrame(currentLayerCount, nextLayerCount)
}

func VP9SuperframeSize(frames ...[]byte) (int, error) {
	return vp9codec.VP9SuperframeSize(frames...)
}

func PackVP9SuperframeInto(dst []byte, frames ...[]byte) (int, error) {
	return vp9codec.PackVP9SuperframeInto(dst, frames...)
}

func NewVP9WebRTCPacketizer(initialPictureID uint16) VP9WebRTCPacketizer {
	return vp9codec.NewVP9WebRTCPacketizer(initialPictureID)
}
