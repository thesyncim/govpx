package codec

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/codecapi"
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

const (
	UpstreamLibvpxVersion        = codecapi.UpstreamLibvpxVersion
	CodecVP8                     = codecapi.CodecVP8
	CodecVP9                     = codecapi.CodecVP9
	maxVP8Dimension              = codecapi.MaxVP8Dimension
	maxVP9Dimension              = codecapi.MaxVP9Dimension
	maxQuantizer                 = codecapi.MaxQuantizer
	maxARNRFrames                = codecapi.MaxARNRFrames
	defaultDropFramesWaterMark   = codecapi.DefaultDropFramesWaterMark
	libvpxDefaultBufferSizeMs    = codecapi.LibvpxDefaultBufferSizeMs
	libvpxDefaultBufferInitialMs = codecapi.LibvpxDefaultBufferInitialMs
	libvpxDefaultBufferOptimalMs = codecapi.LibvpxDefaultBufferOptimalMs
	maxRateControlOvershootPct   = codecapi.MaxRateControlOvershootPct
	maxRateControlUndershootPct  = codecapi.MaxRateControlUndershootPct

	libvpxTimestampTicksPerSecond = codecapi.LibvpxTimestampTicksPerSecond
	maxInt64Value                 = codecapi.MaxInt64Value
	rowWorkerIdleSpinBudget       = codecapi.RowWorkerIdleSpinBudget
	vp9WorkerIdleSpinBudget       = codecapi.VP9WorkerIdleSpinBudget
	rowWorkerIdleSchedulerBackoff = codecapi.RowWorkerIdleSchedulerBackoff
)

type Codec = codecapi.Codec
type ChromaSubsampling = codecapi.ChromaSubsampling
type Image = codecapi.Image
type StreamInfo = codecapi.StreamInfo
type ReferenceFlags = codecapi.ReferenceFlags
type ReferenceFrame = codecapi.ReferenceFrame
type FrameInfo = codecapi.FrameInfo
type VP9StreamInfo = codecapi.VP9StreamInfo
type RateControlMode = codecapi.RateControlMode
type RateControlConfig = codecapi.RateControlConfig
type RealtimeFrameDropMode = codecapi.RealtimeFrameDropMode
type RealtimeTarget = codecapi.RealtimeTarget
type Deadline = codecapi.Deadline
type ScalingMode = codecapi.ScalingMode
type Tuning = codecapi.Tuning
type EncodeFlags = codecapi.EncodeFlags
type PostProcessFlag = codecapi.PostProcessFlag
type VP8Decryptor = codecapi.VP8Decryptor
type ROIMap = codecapi.ROIMap
type EncoderPhaseStats = codecapi.EncoderPhaseStats
type FirstPassFrameStats = codecapi.FirstPassFrameStats
type RTPPayloadFragment = codecapi.RTPPayloadFragment
type encoderSourceTimestampState = codecapi.EncoderSourceTimestampState
type workerWakeSlot = codecapi.WorkerWakeSlot
type encoderPhaseStatsOptions = codecapi.EncoderPhaseStatsOptions

const (
	Subsampling420 = codecapi.Subsampling420
	Subsampling422 = codecapi.Subsampling422
	Subsampling440 = codecapi.Subsampling440
	Subsampling444 = codecapi.Subsampling444

	ReferenceFlagLast   = codecapi.ReferenceFlagLast
	ReferenceFlagGolden = codecapi.ReferenceFlagGolden
	ReferenceFlagAltRef = codecapi.ReferenceFlagAltRef
	ReferenceLast       = codecapi.ReferenceLast
	ReferenceGolden     = codecapi.ReferenceGolden
	ReferenceAltRef     = codecapi.ReferenceAltRef

	RateControlVBR = codecapi.RateControlVBR
	RateControlCBR = codecapi.RateControlCBR
	RateControlCQ  = codecapi.RateControlCQ
	RateControlQ   = codecapi.RateControlQ

	RealtimeFrameDropUnchanged = codecapi.RealtimeFrameDropUnchanged
	RealtimeFrameDropDisabled  = codecapi.RealtimeFrameDropDisabled
	RealtimeFrameDropEnabled   = codecapi.RealtimeFrameDropEnabled

	DeadlineBestQuality = codecapi.DeadlineBestQuality
	DeadlineGoodQuality = codecapi.DeadlineGoodQuality
	DeadlineRealtime    = codecapi.DeadlineRealtime

	ScalingNormal    = codecapi.ScalingNormal
	ScalingFourFive  = codecapi.ScalingFourFive
	ScalingThreeFive = codecapi.ScalingThreeFive
	ScalingOneTwo    = codecapi.ScalingOneTwo

	TunePSNR = codecapi.TunePSNR
	TuneSSIM = codecapi.TuneSSIM

	EncodeForceKeyFrame     = codecapi.EncodeForceKeyFrame
	EncodeInvisibleFrame    = codecapi.EncodeInvisibleFrame
	EncodeNoReferenceLast   = codecapi.EncodeNoReferenceLast
	EncodeNoReferenceGolden = codecapi.EncodeNoReferenceGolden
	EncodeNoReferenceAltRef = codecapi.EncodeNoReferenceAltRef
	EncodeNoUpdateLast      = codecapi.EncodeNoUpdateLast
	EncodeNoUpdateGolden    = codecapi.EncodeNoUpdateGolden
	EncodeNoUpdateAltRef    = codecapi.EncodeNoUpdateAltRef
	EncodeNoUpdateEntropy   = codecapi.EncodeNoUpdateEntropy
	EncodeForceGoldenFrame  = codecapi.EncodeForceGoldenFrame
	EncodeForceAltRefFrame  = codecapi.EncodeForceAltRefFrame

	PostProcessDeblock      = codecapi.PostProcessDeblock
	PostProcessDemacroblock = codecapi.PostProcessDemacroblock
	PostProcessAddNoise     = codecapi.PostProcessAddNoise
	PostProcessMFQE         = codecapi.PostProcessMFQE
	allPostProcessFlags     = codecapi.AllPostProcessFlags
)

var (
	ErrInvalidData      = vpxerrors.ErrInvalidData
	ErrNeedKeyFrame     = vpxerrors.ErrNeedKeyFrame
	ErrFrameNotReady    = vpxerrors.ErrFrameNotReady
	ErrBufferTooSmall   = vpxerrors.ErrBufferTooSmall
	ErrFrameRejected    = vpxerrors.ErrFrameRejected
	ErrInvalidConfig    = vpxerrors.ErrInvalidConfig
	ErrInvalidBitrate   = vpxerrors.ErrInvalidBitrate
	ErrInvalidQuantizer = vpxerrors.ErrInvalidQuantizer
	ErrClosed           = vpxerrors.ErrClosed
	ErrInvalidVP9Data   = vpxerrors.ErrInvalidVP9Data
)

func maxInt() int                                 { return codecapi.MaxInt() }
func validRateControlMode(m RateControlMode) bool { return codecapi.ValidRateControlMode(m) }
func validateEncodeFlags(f EncodeFlags) error     { return codecapi.ValidateEncodeFlags(f) }
func vbrMinFrameBandwidthBits(b, pct int) int     { return codecapi.VBRMinFrameBandwidthBits(b, pct) }
func encoderMacroblockCols(w int) int             { return codecapi.EncoderMacroblockCols(w) }
func encoderMacroblockRows(h int) int             { return codecapi.EncoderMacroblockRows(h) }
func nanotime() int64                             { return codecapi.Nanotime() }
func runtimeProcYield(cycles uint32)              { codecapi.RuntimeProcYield(cycles) }
func saturatingAddUint64(a, b uint64) uint64      { return codecapi.SaturatingAddUint64(a, b) }

func newEncoderSourceTimestampState(t timingState) encoderSourceTimestampState {
	return codecapi.NewEncoderSourceTimestampState(codecapi.TimingState{
		TimebaseNum:   t.timebaseNum,
		TimebaseDen:   t.timebaseDen,
		FrameDuration: t.frameDuration,
		FrameRate:     t.frameRate,
	})
}

func publicImageFromVP8(src *vp8common.Image) Image {
	return codecapi.PublicImageFromVP8(src)
}

func PeekVP9StreamInfo(packet []byte) (VP9StreamInfo, error) {
	return codecapi.PeekVP9StreamInfo(packet)
}

func vp9StreamInfoFromInternal(info vp9dec.StreamInfo) VP9StreamInfo {
	return codecapi.VP9StreamInfoFromInternal(info)
}

func FinalizeFirstPassStats(stats []FirstPassFrameStats) []FirstPassFrameStats {
	return codecapi.FinalizeFirstPassStats(stats)
}
