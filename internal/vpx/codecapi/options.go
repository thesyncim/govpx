package codecapi

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

// RTPPayloadFragment is one RTP payload body plus the RTP marker-bit value
// the caller should put in the RTP header for that body.
type RTPPayloadFragment = vpxrtp.PayloadFragment

// RateControlMode selects the encoder bitrate-control strategy.
type RateControlMode int

const (
	// RateControlVBR selects variable bitrate mode.
	RateControlVBR RateControlMode = iota
	// RateControlCBR selects constant bitrate mode.
	RateControlCBR
	// RateControlCQ selects constrained-quality mode.
	RateControlCQ
	// RateControlQ selects libvpx VPX_Q constant-quality mode.
	RateControlQ
)

func ValidRateControlMode(mode RateControlMode) bool {
	return mode >= RateControlVBR && mode <= RateControlQ
}

// RealtimeFrameDropMode selects how SetRealtimeTarget changes frame dropping.
type RealtimeFrameDropMode int

const (
	// RealtimeFrameDropUnchanged leaves the current frame-drop setting intact.
	RealtimeFrameDropUnchanged RealtimeFrameDropMode = iota
	// RealtimeFrameDropDisabled disables realtime frame dropping.
	RealtimeFrameDropDisabled
	// RealtimeFrameDropEnabled enables realtime frame dropping.
	RealtimeFrameDropEnabled
)

// RateControlConfig is the runtime-updatable subset of encoder rate-control
// options.
type RateControlConfig struct {
	Mode                RateControlMode
	TargetBitrateKbps   int
	MinBitrateKbps      int
	MaxBitrateKbps      int
	MinQuantizer        int
	MaxQuantizer        int
	CQLevel             int
	UndershootPct       int
	OvershootPct        int
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int
	DropFrameAllowed    bool
	DropFrameWaterMark  int
	MaxIntraBitratePct  int
	GFCBRBoostPct       int
}

// RealtimeTarget describes a low-latency runtime target update applied
// by SetRealtimeTarget.
type RealtimeTarget struct {
	BitrateKbps  int
	FPS          int
	Width        int
	Height       int
	MinQuantizer int
	MaxQuantizer int
	FrameDrop    RealtimeFrameDropMode
}

// Deadline selects the encoder speed/quality operating mode.
type Deadline int

const (
	// DeadlineBestQuality favors quality and exhaustive decisions.
	DeadlineBestQuality Deadline = iota
	// DeadlineGoodQuality favors quality with bounded speed features.
	DeadlineGoodQuality
	// DeadlineRealtime favors low-latency realtime decisions.
	DeadlineRealtime
)

// ScalingMode mirrors libvpx's vpx_scaling_mode_t enum (vpx/vp8cx.h).
type ScalingMode int

const (
	ScalingNormal    ScalingMode = 0
	ScalingFourFive  ScalingMode = 1
	ScalingThreeFive ScalingMode = 2
	ScalingOneTwo    ScalingMode = 3
)

// Tuning selects the encoder's visual quality model.
type Tuning int

const (
	TunePSNR Tuning = iota
	TuneSSIM
)

// EncodeFlags controls per-frame reference and packet behavior.
type EncodeFlags uint32

const (
	EncodeForceKeyFrame EncodeFlags = 1 << iota
	EncodeInvisibleFrame
	EncodeNoReferenceLast
	EncodeNoReferenceGolden
	EncodeNoReferenceAltRef
	EncodeNoUpdateLast
	EncodeNoUpdateGolden
	EncodeNoUpdateAltRef
	EncodeNoUpdateEntropy
	EncodeForceGoldenFrame
	EncodeForceAltRefFrame
)

func ValidateEncodeFlags(flags EncodeFlags) error {
	if flags&EncodeForceGoldenFrame != 0 && flags&EncodeNoUpdateGolden != 0 {
		return vpxerrors.ErrInvalidConfig
	}
	if flags&EncodeForceAltRefFrame != 0 && flags&EncodeNoUpdateAltRef != 0 {
		return vpxerrors.ErrInvalidConfig
	}
	return nil
}

// PostProcessFlag selects optional libvpx-style decoder postprocessing.
type PostProcessFlag uint32

const (
	PostProcessDeblock PostProcessFlag = 1 << iota
	PostProcessDemacroblock
	PostProcessAddNoise
	PostProcessMFQE

	AllPostProcessFlags = PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise | PostProcessMFQE
)

// VP8Decryptor mirrors libvpx v1.16.0 vpx_decrypt_cb (vpx/vpx_decoder.h).
type VP8Decryptor func(state any, src []byte, dst []byte, count int)

// ROIMap configures libvpx-style region-of-interest segments.
type ROIMap struct {
	Enabled         bool
	Rows            int
	Cols            int
	SegmentID       []uint8
	DeltaQuantizer  [vp8common.MaxMBSegments]int
	DeltaLoopFilter [vp8common.MaxMBSegments]int
	StaticThreshold [vp8common.MaxMBSegments]int
	Skip            [vp8common.MaxMBSegments]int
	RefFrame        [vp8common.MaxMBSegments]int
}
