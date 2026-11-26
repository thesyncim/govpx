// Shared codec API aliases for libvpx v1.16.0 VP8 codec implementation.
package codec

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vpx/codecapi"
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

const (
	UpstreamLibvpxVersion = codecapi.UpstreamLibvpxVersion
	CodecVP8              = codecapi.CodecVP8
	CodecVP9              = codecapi.CodecVP9
	maxVP8Dimension       = codecapi.MaxVP8Dimension
	maxQuantizer          = codecapi.MaxQuantizer
)

type Codec = codecapi.Codec
type Image = codecapi.Image
type StreamInfo = codecapi.StreamInfo
type ReferenceFlags = codecapi.ReferenceFlags
type ReferenceFrame = codecapi.ReferenceFrame
type FrameInfo = codecapi.FrameInfo
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
type workerWakeSlot = codecapi.WorkerWakeSlot
type encoderPhaseStatsOptions = codecapi.EncoderPhaseStatsOptions

const (
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

	encoderCacheLineSize    = codecapi.EncoderCacheLineSize
	rowWorkerIdleSpinBudget = codecapi.RowWorkerIdleSpinBudget
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

func PeekVP8StreamInfo(packet []byte) (StreamInfo, error) {
	return codecapi.PeekVP8StreamInfo(packet)
}

func peekVP8FrameHeader(packet []byte) (vp8dec.FrameHeader, StreamInfo, error) {
	return codecapi.PeekVP8FrameHeader(packet)
}

func publicImageFromVP8(src *vp8common.Image) Image {
	return codecapi.PublicImageFromVP8(src)
}

func nanotime() int64                { return codecapi.Nanotime() }
func runtimeProcYield(cycles uint32) { codecapi.RuntimeProcYield(cycles) }

func FinalizeFirstPassStats(stats []FirstPassFrameStats) []FirstPassFrameStats {
	return codecapi.FinalizeFirstPassStats(stats)
}

func accumulateFirstPassStats(section *FirstPassFrameStats, frame FirstPassFrameStats) {
	codecapi.AccumulateFirstPassStats(section, frame)
}

func subtractFirstPassStats(section *FirstPassFrameStats, frame FirstPassFrameStats) {
	codecapi.SubtractFirstPassStats(section, frame)
}
