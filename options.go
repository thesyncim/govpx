package govpx

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
	// Mode selects VBR, CBR, constrained-quality, or VPX_Q behavior.
	Mode RateControlMode

	// TargetBitrateKbps is the total target bitrate.
	TargetBitrateKbps int
	// MinBitrateKbps and MaxBitrateKbps optionally bound runtime bitrate
	// updates.
	MinBitrateKbps int
	MaxBitrateKbps int

	// MinQuantizer and MaxQuantizer bound the public 0..63 quantizer range.
	MinQuantizer int
	MaxQuantizer int
	// CQLevel is the public quantizer level for RateControlCQ and
	// RateControlQ. RateControlCQ applies it as a floor; RateControlQ
	// mirrors libvpx's VPX_Q validation without applying the CQ floor.
	CQLevel int

	// UndershootPct and OvershootPct cap libvpx-style rate adjustment.
	// Valid range is [0, 100]; zero selects the libvpx default.
	UndershootPct int
	OvershootPct  int

	// BufferSizeMs, BufferInitialSizeMs, and BufferOptimalSizeMs describe the
	// virtual rate-control buffer in milliseconds.
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int

	// DropFrameAllowed enables rate-control frame dropping when
	// DropFrameWaterMark is positive.
	DropFrameAllowed bool
	// DropFrameWaterMark is the buffer-level percentage at which rate
	// control begins dropping frames. Values >100 are clamped to 100; zero
	// disables dropping. Mirrors libvpx's oxcf.drop_frames_water_mark /
	// rc_dropframe_thresh.
	DropFrameWaterMark int

	// MaxIntraBitratePct caps key-frame bitrate as a percentage of target.
	MaxIntraBitratePct int
	// GFCBRBoostPct controls golden-frame boost in CBR mode.
	GFCBRBoostPct int
}

// RealtimeTarget describes a low-latency runtime target update applied
// by [VP8Encoder.SetRealtimeTarget].
//
// Each field uses its zero value as "leave the current setting alone",
// so a bandwidth-estimator update is safe to send as a sparse delta
// (typically only BitrateKbps). All non-zero fields are validated
// before any mutation, and a validation failure leaves the encoder
// fully usable at its previous configuration.
//
// Mirrors libvpx's `vpx_codec_enc_config_set` for the fields a WebRTC sender
// typically updates per BWE step. VP9 consumes BitrateKbps / FPS /
// MinQuantizer / MaxQuantizer / Width / Height through
// VP9Encoder.SetRealtimeTarget; frame-drop controls are accepted by VP9 only
// when the encoder was created with VP9 CBR rate control enabled.
type RealtimeTarget struct {
	// BitrateKbps changes the total target bitrate when non-zero.
	// Equivalent to [VP8Encoder.SetBitrateKbps]. VP9 applies it when the
	// encoder was created with explicit rate control enabled.
	BitrateKbps int
	// FPS changes the timebase to 1/FPS when non-zero. The realtime
	// adaptive-Speed timing window is reset on VP8 so the auto-speed
	// selector recomputes from cold start against the new frame budget.
	FPS int

	// Width and Height drive caller-driven runtime resolution change
	// when both are positive. Setting them to the encoder's current
	// dimensions is a no-op (accepted for sparse BWE deltas that echo
	// the active size). Setting them to a new W x H pair resizes every
	// size-dependent encoder buffer in place (capacity is reused),
	// invalidates the LAST / GOLDEN / ALTREF references, and forces
	// the next encoded frame to be a key frame at the new dimensions.
	//
	// Mirrors libvpx's `vpx_codec_enc_config_set` with a new width /
	// height. The libvpx spatial resampler ([VP8E_SET_SCALEMODE],
	// `rc_resize_*`) is not implemented; callers drive the coded size
	// directly. The decoder also handles key-frame resolution change;
	// see [DecoderOptions.RejectResolutionChange].
	//
	// VP8 resize is refused with [ErrInvalidConfig] when the lookahead
	// queue is non-empty or a hidden alt-ref input is staged; drain the
	// encoder with [VP8Encoder.FlushInto] before resizing in those
	// modes. Invalid dimensions (zero, negative, or larger than the
	// codec maximum) are likewise refused without mutating encoder
	// state.
	Width  int
	Height int

	// MinQuantizer and MaxQuantizer update the public 0..63 quantizer
	// range when non-zero. Mirrors the runtime side of
	// [EncoderOptions.MinQuantizer] / [EncoderOptions.MaxQuantizer].
	MinQuantizer int
	MaxQuantizer int

	// FrameDrop changes realtime frame dropping. The zero value
	// ([RealtimeFrameDropUnchanged]) leaves the current setting
	// unchanged, which is the right default for bitrate-only WebRTC
	// bandwidth-estimation updates that should not accidentally
	// disable dropping.
	FrameDrop RealtimeFrameDropMode
}
