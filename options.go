package govpx

import "github.com/thesyncim/govpx/internal/vpx/codecapi"

// RateControlMode selects the encoder bitrate-control strategy.
type RateControlMode = codecapi.RateControlMode

const (
	RateControlVBR = codecapi.RateControlVBR
	RateControlCBR = codecapi.RateControlCBR
	RateControlCQ  = codecapi.RateControlCQ
	RateControlQ   = codecapi.RateControlQ
)

// RealtimeFrameDropMode selects how SetRealtimeTarget changes frame dropping.
type RealtimeFrameDropMode = codecapi.RealtimeFrameDropMode

const (
	RealtimeFrameDropUnchanged = codecapi.RealtimeFrameDropUnchanged
	RealtimeFrameDropDisabled  = codecapi.RealtimeFrameDropDisabled
	RealtimeFrameDropEnabled   = codecapi.RealtimeFrameDropEnabled
)

// RateControlConfig is the runtime-updatable subset of encoder rate-control options.
type RateControlConfig = codecapi.RateControlConfig

// RealtimeTarget describes a low-latency runtime target update.
type RealtimeTarget = codecapi.RealtimeTarget

// Deadline selects the encoder speed/quality operating mode.
type Deadline = codecapi.Deadline

const (
	DeadlineBestQuality = codecapi.DeadlineBestQuality
	DeadlineGoodQuality = codecapi.DeadlineGoodQuality
	DeadlineRealtime    = codecapi.DeadlineRealtime
)

// ScalingMode mirrors libvpx's vpx_scaling_mode_t enum.
type ScalingMode = codecapi.ScalingMode

const (
	ScalingNormal    = codecapi.ScalingNormal
	ScalingFourFive  = codecapi.ScalingFourFive
	ScalingThreeFive = codecapi.ScalingThreeFive
	ScalingOneTwo    = codecapi.ScalingOneTwo
)

// Tuning selects the encoder's visual quality model.
type Tuning = codecapi.Tuning

const (
	TunePSNR = codecapi.TunePSNR
	TuneSSIM = codecapi.TuneSSIM
)

// EncodeFlags controls per-frame reference and packet behavior.
type EncodeFlags = codecapi.EncodeFlags

const (
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
)

// PostProcessFlag selects optional libvpx-style decoder postprocessing.
type PostProcessFlag = codecapi.PostProcessFlag

const (
	PostProcessDeblock      = codecapi.PostProcessDeblock
	PostProcessDemacroblock = codecapi.PostProcessDemacroblock
	PostProcessAddNoise     = codecapi.PostProcessAddNoise
	PostProcessMFQE         = codecapi.PostProcessMFQE
	allPostProcessFlags     = codecapi.AllPostProcessFlags
)

// VP8Decryptor mirrors libvpx v1.16.0 vpx_decrypt_cb.
type VP8Decryptor = codecapi.VP8Decryptor

// ROIMap configures libvpx-style region-of-interest segments.
type ROIMap = codecapi.ROIMap

// EncoderPhaseStats accumulates opt-in coarse encoder phase timing and counters.
type EncoderPhaseStats = codecapi.EncoderPhaseStats

// FirstPassFrameStats mirrors libvpx FIRSTPASS_STATS.
type FirstPassFrameStats = codecapi.FirstPassFrameStats

// FinalizeFirstPassStats appends the terminal total-stats record.
func FinalizeFirstPassStats(stats []FirstPassFrameStats) []FirstPassFrameStats {
	return codecapi.FinalizeFirstPassStats(stats)
}
