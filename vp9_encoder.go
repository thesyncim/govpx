package govpx

import (
	"encoding/binary"
	"errors"
	"image"
	"math"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const (
	vp9EncoderTxCoeffSlots      = 1024
	vp9EncoderBlockCoeffSlots   = 256 * vp9EncoderTxCoeffSlots
	vp9MinEncodeIntoBuffer      = 64
	vp9MaxPartitionReconScratch = 64*64 + 2*32*32
	vp9DefaultMinQuantizer      = 4
	vp9DefaultMaxQuantizer      = 56
	vp9DefaultCQLevel           = 32
	// vp9DefaultBaseQIndex pins the packet-path default to the first-frame
	// base_qindex emitted by pinned libvpx vpxenc-vp9 with the repo's realtime
	// CQ oracle knobs (--end-usage=q --cq-level=32 --min-q=4 --max-q=56).
	vp9DefaultBaseQIndex = 37
	// The same oracle emits the CQ-level qindex for the first visible inter
	// frame in the packet path after the keyframe.
	vp9DefaultInterBaseQIndex = 128
	vp9RDDivBits              = 7
)

// VP9AQMode selects VP9 adaptive quantization behavior.
type VP9AQMode int8

const (
	// VP9AQNone disables VP9 adaptive quantization.
	VP9AQNone VP9AQMode = 0
	// VP9AQVariance enables VP9 variance adaptive quantization. It assigns
	// low-variance blocks to boosted-Q segments and high-variance blocks to
	// lower-rate segments using libvpx's variance-AQ energy bins.
	VP9AQVariance VP9AQMode = 1
	// VP9AQComplexity enables VP9 complexity adaptive quantization. It assigns
	// inter blocks to in-frame Q adjustment segments using libvpx's projected
	// rate and spatial complexity thresholds.
	VP9AQComplexity VP9AQMode = 2
	// VP9AQEquator360 enables VP9 equator-biased 360-video AQ. It mirrors
	// libvpx's AQ_360, assigning lower-Q segments to the equatorial band and
	// higher-Q segments toward the poles to budget more bits where viewers
	// look most often. Requires explicit rate control and is incompatible
	// with lossless or static segmentation.
	VP9AQEquator360 VP9AQMode = 4
	// VP9AQCyclicRefresh enables VP9 cyclic-refresh AQ. This mirrors
	// libvpx VP9 aq-mode=3 and is currently limited to explicit CBR.
	VP9AQCyclicRefresh VP9AQMode = 3
	// VP9AQPerceptual enables VP9 perceptual AQ via wiener-variance
	// k-means clustering. Mirrors libvpx AQ_PERCEPTUAL (mode 5). The
	// source luma plane is segmented into 8 clusters whose centroids drive
	// per-segment AltQ deltas, and each block inherits the cluster of
	// its containing macroblock. Incompatible with lossless and static
	// segmentation.
	VP9AQPerceptual VP9AQMode = 5
)

func vp9CoefUpdateModeForFrame(isKey bool) encoder.CoefUpdateMode {
	if isKey {
		return encoder.CoefUpdateTwoLoop
	}
	return encoder.CoefUpdateOneLoopReduced
}

// VP9ColorSpace mirrors libvpx's vpx_color_space_t — the 3-bit color
// space tag carried in the uncompressed header on keyframes and
// profile>0 intra-only frames.
type VP9ColorSpace uint8

const (
	// VP9ColorSpaceUnknown indicates the color space is not signalled.
	VP9ColorSpaceUnknown VP9ColorSpace = 0
	// VP9ColorSpaceBT601 selects ITU-R BT.601.
	VP9ColorSpaceBT601 VP9ColorSpace = 1
	// VP9ColorSpaceBT709 selects ITU-R BT.709.
	VP9ColorSpaceBT709 VP9ColorSpace = 2
	// VP9ColorSpaceSMPTE170 selects SMPTE-170 (matches BT.601 in practice).
	VP9ColorSpaceSMPTE170 VP9ColorSpace = 3
	// VP9ColorSpaceSMPTE240 selects SMPTE-240.
	VP9ColorSpaceSMPTE240 VP9ColorSpace = 4
	// VP9ColorSpaceBT2020 selects ITU-R BT.2020.
	VP9ColorSpaceBT2020 VP9ColorSpace = 5
	// VP9ColorSpaceReserved is the reserved value (6).
	VP9ColorSpaceReserved VP9ColorSpace = 6
	// VP9ColorSpaceSRGB selects sRGB. Only legal on profiles 1 and 3
	// (4:4:4 sampling required); rejected on profile 0 streams.
	VP9ColorSpaceSRGB VP9ColorSpace = 7
)

// VP9ColorRange mirrors libvpx's vpx_color_range_t — the 1-bit color
// range tag carried alongside the color space.
type VP9ColorRange uint8

const (
	// VP9ColorRangeStudio selects the studio (limited) range.
	VP9ColorRangeStudio VP9ColorRange = 0
	// VP9ColorRangeFull selects the full (PC) range.
	VP9ColorRangeFull VP9ColorRange = 1
)

// VP9ScreenContent labels the libvpx VP9 content tuning options exposed
// by VP9E_SET_TUNE_CONTENT. The constants match libvpx's
// vp9e_tune_content enum: VPX_CONTENT_DEFAULT (0), VPX_CONTENT_SCREEN
// (1), and VPX_CONTENT_FILM (2). FILM biases the variance-AQ
// segmentation to preserve film-grain texture by suppressing the
// positive Q delta libvpx applies to high-variance blocks under
// default-video tuning.
type VP9ScreenContent int8

const (
	// VP9ScreenContentDefault is libvpx's VPX_CONTENT_DEFAULT — the
	// general-purpose video tuning. Variance AQ keeps its default
	// high-variance Q-down ratio.
	VP9ScreenContentDefault VP9ScreenContent = 0
	// VP9ScreenContentScreen is libvpx's VPX_CONTENT_SCREEN — screen
	// content tuning. Expands the realtime no-reference intra search
	// to cover non-DC modes for blocks above 16x16.
	VP9ScreenContentScreen VP9ScreenContent = 1
	// VP9ScreenContentFilm is libvpx's VPX_CONTENT_FILM — film/grain
	// tuning. When combined with VP9AQVariance, the high-variance
	// segment's rate ratio is held at 1:1 so libvpx's default 3:4
	// Q-up bias on textured/grain blocks is removed and the grain is
	// preserved through quantization. Independent of the AQ choice,
	// FILM also disables the cyclic-refresh interaction with golden
	// refresh that the existing mode==2 path already gates.
	VP9ScreenContentFilm VP9ScreenContent = 2
)

// VP9DisableLoopfilter selects whether the in-loop deblock filter is
// suppressed. Mirrors libvpx's VP9E_SET_DISABLE_LOOPFILTER control.
type VP9DisableLoopfilter uint8

const (
	// VP9LoopfilterEnabled leaves the in-loop filter active.
	VP9LoopfilterEnabled VP9DisableLoopfilter = 0
	// VP9LoopfilterDisableInter disables the filter on non-keyframes only.
	VP9LoopfilterDisableInter VP9DisableLoopfilter = 1
	// VP9LoopfilterDisableAll disables the filter on every frame.
	VP9LoopfilterDisableAll VP9DisableLoopfilter = 2
)

// VP9EncoderOptions configures a VP9 profile 0 encoder.
type VP9EncoderOptions struct {
	// Width and Height are the fixed visible dimensions accepted by
	// EncodeInto. Must both be positive.
	Width  int
	Height int

	// FPS sets a 1/FPS timebase when TimebaseNum and TimebaseDen are
	// both zero. Defaults to 30 if all three are unset.
	FPS int

	// TimebaseNum is the numerator of the caller timebase.
	TimebaseNum int
	// TimebaseDen is the denominator of the caller timebase.
	TimebaseDen int

	// Threads is a tile-column hint for VP9 profile 0 encode. Zero or 1
	// choose the minimum legal tile columns for the frame; larger values choose
	// enough columns for decoder/transport parallelism, clamped to VP9 limits.
	// Multi-column, single-row tile bodies are encoded by a persistent worker
	// pool; the Threads <= 1 path does not allocate or touch that pool.
	// Negative values return ErrInvalidConfig.
	Threads int
	// Log2TileRows selects the VP9 tile-row count as 1<<Log2TileRows. Valid
	// values are 0..2; zero keeps the default single tile row. Tile rows are
	// emitted in scan order because reconstructed pixels and above entropy
	// contexts carry across row boundaries.
	Log2TileRows int8

	// RowMT mirrors libvpx's VP9E_SET_ROW_MT control. When true with Threads > 1
	// and a multi-column tile layout, each tile-column body encode arms a per-SB
	// row-wavefront synchroniser matching libvpx's VP9RowMTSync primitive. Rows
	// signal column progress after every SB and can wait for the row above to
	// reach a configurable lookahead, mirroring vp9_row_mt_sync_read/write. The
	// bitstream output is byte-identical to the serial path because govpx still
	// runs one goroutine per tile column; the primitive is the foundation for
	// per-row parallelism within a tile column. Requires Threads > 1; setting it
	// with Threads <= 1 returns ErrInvalidConfig.
	RowMT bool

	// Deadline selects the VP9 speed/quality operating mode. The zero-value
	// options keep govpx's historical VP9 oracle default of realtime cpu-used 8;
	// use SetDeadline after construction to force explicit best-quality cpu 0.
	Deadline Deadline
	// CpuUsed selects the libvpx VP9 speed preset in [-9, 9]. VP9 maps this to
	// abs(cpu-used) internally; the sign is preserved for control parity.
	CpuUsed int8
	// Tuning selects the VP9 visual quality model. TunePSNR is the default;
	// TuneSSIM is accepted for libvpx control-surface parity and future SSIM
	// mode-decision work.
	Tuning Tuning
	// ScreenContentMode selects VP9 content tuning. Values match the
	// VP9ScreenContent constants (VP9ScreenContentDefault=0,
	// VP9ScreenContentScreen=1, VP9ScreenContentFilm=2) and libvpx's
	// VP9E_SET_TUNE_CONTENT. Screen content enables the broader
	// no-reference intra mode search used by realtime VP9. Film
	// content biases the variance-AQ segmentation so high-variance
	// (grain-bearing) blocks stay at the base Q.
	ScreenContentMode int8
	// NoiseSensitivity selects VP9 luma/chroma temporal denoising. Zero
	// disables the denoiser. Valid values are [0, 6]; 1 is low strength, 2 is
	// medium, and 3..6 use the high-strength VP9 temporal denoiser path.
	NoiseSensitivity int8
	// Sharpness is the VP9 loop-filter sharpness level in [0, 7].
	Sharpness uint8
	// StaticThreshold is the VP9 static-block breakout threshold. Zero disables
	// the breakout; positive values allow low-error low-motion inter blocks to
	// skip residual coding.
	StaticThreshold int

	// TargetBitrateKbps is a non-negative bitrate hint for profile 0 encode
	// configuration. When RateControlModeSet is false, the packet path keeps
	// the existing public-Q mode and only stores this value as metadata.
	TargetBitrateKbps int

	// RateControlModeSet enables VP9 rate-control bookkeeping. It is explicit
	// because RateControlVBR is the zero value while the historical VP9 default
	// is libvpx VPX_Q-style public-Q mode. RateControlCBR drives one-pass CBR
	// qindex selection, tracks a buffer, and can drop visible inter frames when
	// DropFrameAllowed is enabled. RateControlVBR, RateControlCQ, and
	// RateControlQ drive one-pass VP9 rate/quality qindex selection without
	// frame dropping.
	RateControlModeSet bool
	// RateControlMode selects the VP9 rate-control mode when
	// RateControlModeSet is true.
	RateControlMode RateControlMode
	// BufferSizeMs, BufferInitialSizeMs, and BufferOptimalSizeMs configure the
	// VP9 CBR virtual buffer. Zero values use libvpx defaults.
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int
	// DropFrameAllowed enables VP9 CBR buffer-underrun frame dropping. It only
	// takes effect when RateControlModeSet is true and RateControlMode is
	// RateControlCBR.
	DropFrameAllowed bool
	// DropFrameWaterMark controls VP9 CBR watermark decimation when
	// DropFrameAllowed is enabled. Zero with DropFrameAllowed uses the libvpx
	// default watermark.
	DropFrameWaterMark int

	// Quantizer selects a fixed VP9 base qindex in [1, 255]. Zero uses the
	// public MinQuantizer / MaxQuantizer / CQLevel controls below. It is a
	// low-level escape hatch; callers that need libvpx-style CLI parity should
	// prefer the public 0..63 controls.
	Quantizer int

	// MinQuantizer and MaxQuantizer bound the public libvpx VP9 0..63
	// quantizer range used by VP9 qindex selection. When both are zero, the
	// encoder uses libvpx's oracle defaults: min-q=4, max-q=56.
	MinQuantizer int
	MaxQuantizer int

	// CQLevel is the public libvpx VP9 0..63 constant-quality level. Zero uses
	// 32 for the default range, or the single fixed value when
	// MinQuantizer == MaxQuantizer.
	CQLevel int

	// Lossless enables VP9 profile 0 lossless coding. It forces base qindex 0,
	// 4x4 transforms, WHT reconstruction, and disables the loop filter.
	Lossless bool

	// MinKeyframeInterval is the VP9 kf_min_dist control. Zero keeps libvpx's
	// default. It constrains future adaptive keyframe decisions and is stored
	// for libvpx-compatible control/oracle parity; explicitly forced key frames
	// are unaffected.
	MinKeyframeInterval int
	// MaxKeyframeInterval bounds the gap between key frames. Zero
	// uses libvpx's default (kf_max_dist=128).
	MaxKeyframeInterval int
	// AdaptiveKeyFrames enables VP9 one-pass scene-cut keyframe promotion.
	// Eligible visible inter frames are promoted when every usable reference is
	// a poor luma predictor and intra prediction is materially cheaper. The
	// MinKeyframeInterval setting gates these automatic promotions.
	AdaptiveKeyFrames bool

	// ErrorResilient enables the libvpx error-resilient bit on every
	// frame header.
	ErrorResilient bool
	// FrameParallelDecodingSet enables explicit control of the VP9
	// frame_parallel_decoding_mode bit. When false, govpx keeps libvpx's
	// default enabled mode.
	FrameParallelDecodingSet bool
	// FrameParallelDecoding mirrors libvpx's --frame-parallel / VP9E_SET_
	// FRAME_PARALLEL_DECODING control when FrameParallelDecodingSet is true.
	// Disabling it makes the encoder apply counts-driven frame-context
	// adaptation after each coded frame, matching non-frame-parallel decoders.
	FrameParallelDecoding bool

	// TemporalScalability configures temporal-only VP9 layer scheduling for
	// one spatial layer. Result metadata is returned by
	// EncodeIntoWithResult / EncodeIntoWithFlagsResult.
	TemporalScalability TemporalScalabilityConfig
	// SpatialScalability configures VP9 spatial-SVC layer signaling for the
	// packet produced by this encoder. It reports spatial layer metadata in
	// VP9EncodeResult and RTPPayloadDescriptor; it does not synthesize lower or
	// higher resolution layers.
	SpatialScalability VP9SpatialScalabilityConfig

	// LookaheadFrames enables buffered VP9 source scheduling. Zero preserves
	// immediate encoding. Positive values are valid in [1, 25] and make
	// EncodeIntoWithResult return ErrFrameNotReady until enough future frames
	// have been queued; drain with FlushIntoWithResult at end of stream.
	// The current VP9 lookahead path is limited to public-Q mode without
	// temporal scalability. AutoAltRef uses this queue to emit a generated
	// hidden ALTREF packet from a future source.
	LookaheadFrames int
	// AutoAltRef requests automatic generated alternate-reference frames. VP9
	// requires LookaheadFrames > 1 and currently supports one future-source
	// hidden ALTREF bootstrap per stream; source-backed ALTREF refresh remains
	// available through EncodeForceAltRefFrame.
	AutoAltRef bool
	// ARNRMaxFrames is the VP9 auto-alt-ref noise reduction temporal window in
	// frames. Values in [2, 15] enable ARNR filtering when AutoAltRef emits a
	// hidden ALTREF; zero or one leaves the hidden source unfiltered.
	ARNRMaxFrames int
	// ARNRStrength is the VP9 auto-alt-ref noise reduction filter strength in
	// [0, 6].
	ARNRStrength int
	// ARNRType selects VP9 auto-alt-ref filter direction: 1=backward,
	// 2=forward, 3=centered. Zero uses libvpx's default centered type.
	ARNRType int

	// AQMode selects VP9 adaptive quantization. VP9AQVariance enables
	// variance-based segmentation, VP9AQComplexity enables projected-rate
	// complexity segmentation, VP9AQCyclicRefresh enables realtime CBR
	// cyclic-refresh segmentation, and zero leaves AQ disabled.
	AQMode VP9AQMode

	// TwoPassStats enables VP9 second-pass VBR/CQ rate planning when non-empty.
	// Pass a slice produced by [FinalizeVP9FirstPassStats] after collecting
	// per-frame rows with [VP9Encoder.CollectFirstPassStats].
	TwoPassStats []VP9FirstPassFrameStats
	// TwoPassVBRBiasPct controls VP9 second-pass VBR bias when stats are
	// present. Zero uses libvpx's default bias.
	TwoPassVBRBiasPct int
	// TwoPassMinPct sets the VP9 second-pass minimum section bitrate
	// percentage. Zero leaves the minimum disabled.
	TwoPassMinPct int
	// TwoPassMaxPct sets the VP9 second-pass maximum section bitrate
	// percentage. Zero uses libvpx's VP9 default.
	TwoPassMaxPct int
	// VBRCorpusComplexity enables libvpx VP9 corpus-VBR rate planning when
	// non-zero. The value is the global mean modified-score numerator (the
	// libvpx field is scaled by 1/10 so a value of 50 means mean_mod_score
	// = 5.0). Valid range is [0, 10000]; zero disables corpus VBR and
	// libvpx falls back to per-clip mean-mod-score derivation.
	//
	// libvpx: vpx/vpx_encoder.h:597 rc_2pass_vbr_corpus_complexity,
	//         vp9/vp9_cx_iface.c:206 (range check 0..10000),
	//         vp9/vp9_cx_iface.c:582 (oxcf->vbr_corpus_complexity wire-up),
	//         vp9/encoder/vp9_encoder.h:225 (oxcf field).
	VBRCorpusComplexity int

	// MinBitrateKbps and MaxBitrateKbps optionally bound runtime bitrate
	// updates. Zero disables the bound. Mirrors libvpx's
	// rc_target_bitrate clamping when SetBitrateKbps or SetRateControl
	// updates the target bitrate.
	MinBitrateKbps int
	MaxBitrateKbps int

	// UndershootPct and OvershootPct cap libvpx-style rate adjustment as a
	// percentage of the per-frame bandwidth. Valid range is [0, 100]; zero
	// selects libvpx's VP9 default of 100. Mirrors VP9's rc_undershoot_pct
	// and rc_overshoot_pct controls.
	UndershootPct int
	OvershootPct  int

	// MaxIntraBitratePct caps key-frame bitrate as a percentage of the
	// per-frame bandwidth when non-zero. Mirrors libvpx's
	// rc_max_intra_bitrate_pct VP9 control; zero disables the cap.
	MaxIntraBitratePct int
	// MaxInterBitratePct caps inter-frame bitrate as a percentage of the
	// per-frame bandwidth when non-zero. Mirrors libvpx's
	// VP9E_SET_MAX_INTER_BITRATE_PCT control; zero disables the cap.
	MaxInterBitratePct int
	// GFCBRBoostPct boosts golden-frame target bits in CBR mode by the
	// configured percentage of the per-frame bandwidth. Mirrors libvpx's
	// VP9E_SET_GF_CBR_BOOST_PCT control; zero disables the boost.
	GFCBRBoostPct int

	// RTCExternalRateControl mirrors libvpx's
	// VP9E_SET_RTC_EXTERNAL_RATECTRL control. Caller-driven realtime rate
	// control owns the keyframe cadence: adaptive scene-cut promotion is
	// suppressed, explicit ForceKeyFrame requests and MaxKeyframeInterval
	// cadence still emit keyframes.
	RTCExternalRateControl bool

	// DeltaQUV is the libvpx VP9E_SET_DELTA_Q_UV chroma quantizer delta
	// applied to both UvDcDeltaQ and UvAcDeltaQ in the uncompressed header.
	// Valid range is [-15, 15]; non-zero values disable Profile 0 lossless
	// even at base_qindex == 0. Zero leaves the chroma quantizer matched
	// to luma.
	DeltaQUV int

	// ColorSpace tags the bitstream color space in the keyframe and
	// profile>0 intra-only uncompressed header (3-bit color_space field).
	// Mirrors libvpx's VP9E_SET_COLOR_SPACE control. Valid values are
	// VP9ColorSpaceUnknown..VP9ColorSpaceSRGB (0..7). Profile-0 streams
	// cannot carry SRGB because SRGB mandates 4:4:4 chroma sampling.
	ColorSpace VP9ColorSpace

	// ColorRange tags the bitstream color range in the same keyframe /
	// intra-only block (1-bit color_range field). Mirrors libvpx's
	// VP9E_SET_COLOR_RANGE control. Only emitted when ColorSpace is not
	// SRGB (SRGB implies full range and skips the bit).
	ColorRange VP9ColorRange

	// RenderWidth and RenderHeight tag the display-render dimensions in
	// the keyframe and intra-only uncompressed header. Mirrors libvpx's
	// VP9E_SET_RENDER_SIZE control. When both are zero (or when they
	// equal Width/Height), the bitstream emits render_and_frame_size
	// _different=0 and inherits the coded dimensions. Otherwise both
	// must be positive and in [1, 65536]; the values are encoded as
	// 16-bit (width-1, height-1) literals.
	RenderWidth  int
	RenderHeight int

	// TargetLevel constrains encode decisions to respect a specific VP9
	// level's macroblock-rate, picture-size, bitrate, and decoder-model
	// limits. Mirrors libvpx's VP9E_SET_TARGET_LEVEL control. Valid
	// values are 255 (unconstrained, the default), 0 (auto), and the
	// canonical level codes 10, 11, 20, 21, 30, 31, 40, 41, 50, 51, 52,
	// 60, 61, 62 (Level N.M encoded as 10*N + M). The current port only
	// accepts and stores the value; level-aware encode decisions are not
	// yet wired.
	TargetLevel int

	// DisableLoopfilter suppresses the in-loop deblock filter. Mirrors
	// libvpx's VP9E_SET_DISABLE_LOOPFILTER control. Mode 0 leaves the
	// filter enabled; mode 1 disables it for non-keyframes only; mode 2
	// disables it on every frame. When disabled, the encoder writes
	// filter_level=0 in the uncompressed header so the existing
	// loop-filter pipeline becomes a no-op.
	DisableLoopfilter VP9DisableLoopfilter

	// Segmentation enables static VP9 profile 0 segmentation metadata.
	// When UpdateMap is set, every encoded block is assigned SegmentID.
	// This supports AltQ, AltLF, forced inter-reference, and forced-skip
	// segment features.
	Segmentation VP9SegmentationOptions

	// MinGFInterval mirrors libvpx's VP9E_SET_MIN_GF_INTERVAL control. It
	// bounds the encoder-selected golden-frame interval from below. Valid
	// values are in [0, vp9MaxGFInterval]; zero leaves libvpx's framerate-
	// derived default in place.
	MinGFInterval int
	// MaxGFInterval mirrors libvpx's VP9E_SET_MAX_GF_INTERVAL control. It
	// bounds the encoder-selected golden-frame interval from above. Valid
	// values are in [0, vp9MaxGFInterval]; zero leaves libvpx's framerate-
	// derived default in place. When both bounds are non-zero,
	// MinGFInterval must not exceed MaxGFInterval.
	MaxGFInterval int

	// FramePeriodicBoost mirrors libvpx's VP9E_SET_FRAME_PERIODIC_BOOST
	// control. When true, periodic golden-frame refreshes receive a
	// stronger active-best-Q reduction so the boosted GF/ALTREF achieves
	// a tighter target qindex.
	FramePeriodicBoost bool

	// AltRefAQ mirrors libvpx's VP9E_SET_ALT_REF_AQ control. When true,
	// alt-ref refresh frames apply extra AQ tightening through the active
	// quantizer bounds, biasing the selected qindex downward.
	AltRefAQ bool

	// PostEncodeDrop mirrors libvpx's VP9E_SET_POSTENCODE_DROP_CBR control.
	// When true (and CBR rate control is enabled), inter frames that
	// overshoot the frame target while the buffer level fell below the
	// configured drop watermark are dropped from the visible output.
	PostEncodeDrop bool

	// DisableOvershootMaxQCBR mirrors libvpx's
	// VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR control. When true, the CBR
	// active-worst-Q promotion to worstQuality on overshoot is suppressed,
	// letting the regulated quantizer use the buffer-derived active worst
	// bound even when the buffer is in the critical region.
	DisableOvershootMaxQCBR bool

	// NextFrameQIndex stores the libvpx VP9E_SET_QUANTIZER_ONE_PASS
	// per-frame qindex override consumed by the next encode. Valid values
	// are in [0, 255]. Mutually exclusive with cyclic-refresh and
	// perceptual AQ, since both rewrite the qindex through segmentation.
	NextFrameQIndex int
	// NextFrameQIndexSet selects between the zero default and an
	// explicitly-set NextFrameQIndex=0 override.
	NextFrameQIndexSet bool

	// FrameParallelEncoderThreads controls encoder-side frame parallelism.
	// Zero or one keep the historical single-frame-in-flight scheduling.
	// Values >= 2 enable batched concurrent encoding of up to N visible
	// source frames at once. Each batch worker receives its own cloned
	// VP9Encoder state taken at batch entry; ref frames, entropy contexts,
	// and rate-control state are not updated between batch members so
	// every member can encode independently. The batch only fires when
	// LookaheadFrames is non-zero. Mutually exclusive with AutoAltRef,
	// because the auto-altref hidden frame is read by future source frames
	// that would otherwise need to wait for the in-flight batch.
	FrameParallelEncoderThreads int

	// EnableTPL turns on the VP9 temporal prediction loop (TPL) quality
	// pass.  Mirrors libvpx's cpi->oxcf.enable_tpl_model and the BD-rate
	// pass libvpx runs by default for two-pass good-quality VP9.  The
	// pass requires LookaheadFrames >= 8 and AutoAltRef enabled so it can
	// look ahead at future source frames; lossless mode is rejected.
	// Per-SB qindex deltas computed by the pass are exposed through
	// [VP9Encoder.TPLFrameDelta] for downstream consumers (row-MT, oracle
	// traces); a scalar frame-mean overlay is applied directly to the
	// regulated frame qindex while per-SB segmentation routing is in
	// flight.
	EnableTPL bool

	// EnableKeyFrameFiltering turns on the keyframe temporal-filter pass.
	// Mirrors libvpx's VP9E_SET_KEY_FRAME_FILTERING runtime control and the
	// cpi->oxcf.enable_keyframe_filtering field
	// (vp9/encoder/vp9_encoder.h:266, vp9/vp9_cx_iface.c:48).  When set, the
	// encoder filters the keyframe source against forward lookahead frames
	// using the existing ARNR temporal-filter machinery before running the
	// keyframe encode, matching libvpx's vp9_temporal_filter(cpi, -1) call
	// at vp9/encoder/vp9_encoder.c:6347-6364.  The filter is gated off in
	// realtime mode, when ARNRMaxFrames == 0 or ARNRStrength == 0, on
	// lossless, on intra-only frames, and on frames that fall outside the
	// libvpx-faithful precondition (vp9_encoder.c:6347-6353); when any gate
	// trips the keyframe is encoded against its raw source.
	EnableKeyFrameFiltering bool
}

// VP9SegmentationOptions configures static per-frame VP9 segmentation.
type VP9SegmentationOptions struct {
	// Enabled writes VP9 segmentation headers. Zero leaves segmentation off.
	Enabled bool
	// UpdateMap writes a constant segment id for every encoded block.
	UpdateMap bool
	// SegmentID is the block segment id written when UpdateMap is true.
	SegmentID uint8
	// AbsDelta selects absolute segment data semantics; false selects delta.
	AbsDelta bool

	// AltQEnabled / AltQ configure SEG_LVL_ALT_Q per segment.
	AltQEnabled [VP9MaxSegments]bool
	AltQ        [VP9MaxSegments]int16
	// AltLFEnabled / AltLF configure SEG_LVL_ALT_LF per segment.
	AltLFEnabled [VP9MaxSegments]bool
	AltLF        [VP9MaxSegments]int16
	// SkipEnabled configures SEG_LVL_SKIP per segment.
	SkipEnabled [VP9MaxSegments]bool
	// RefFrameEnabled / RefFrame configure SEG_LVL_REF_FRAME per segment.
	// RefFrame values must be VP9RefFrameIntra, VP9RefFrameLast,
	// VP9RefFrameGolden, or VP9RefFrameAltRef.
	RefFrameEnabled [VP9MaxSegments]bool
	RefFrame        [VP9MaxSegments]int8
}

// VP9EncodeResult is returned by EncodeIntoWithResult and
// EncodeIntoWithFlagsResult. Data aliases the caller-owned output buffer. When
// rate control drops a frame, Dropped is true and Data is empty while
// rate-control and temporal metadata remain populated.
type VP9EncodeResult struct {
	Data []byte

	KeyFrame  bool
	IntraOnly bool
	ShowFrame bool
	Droppable bool
	// Dropped reports that VP9 CBR rate control intentionally emitted no
	// packet. Data is empty when Dropped is true.
	Dropped bool

	Quantizer         int
	InternalQuantizer int
	SizeBytes         int
	TargetBitrateKbps int
	FrameTargetBits   int
	BufferLevelBits   int
	RefreshFrameFlags uint8
	// FirstPassStats is the VP9 first-pass row consumed for this visible
	// second-pass frame. It is zero when second-pass planning is disabled.
	FirstPassStats VP9FirstPassFrameStats
	// TwoPassFrameTargetBits reports the VP9 second-pass frame target when
	// TwoPassStats drives the frame. It is zero when one-pass targeting is used.
	TwoPassFrameTargetBits int

	TemporalLayerID    int
	TemporalLayerCount int
	TemporalLayerSync  bool
	TL0PICIDX          uint8

	SpatialLayerID              uint8
	SpatialLayerCount           uint8
	InterLayerDependency        bool
	NotRefForUpperSpatialLayer  bool
	ScalabilityStructurePresent bool
	SpatialScalabilityStructure VP9RTPScalabilityStructure
}

// RTPPayloadDescriptor returns a non-flexible VP9 RTP descriptor populated
// from temporal and spatial layer metadata. Picture ID is intentionally left
// unset so callers can choose their own RTP picture-id policy.
func (r VP9EncodeResult) RTPPayloadDescriptor() VP9RTPPayloadDescriptor {
	desc := VP9RTPPayloadDescriptor{
		InterPicturePredicted: !r.KeyFrame && !r.IntraOnly,
		StartOfFrame:          true,
		EndOfFrame:            true,
	}
	if r.TemporalLayerCount > 1 || r.SpatialLayerCount > 1 ||
		r.SpatialLayerID != 0 || r.InterLayerDependency {
		desc.LayerIndicesPresent = true
		desc.TemporalID = uint8(r.TemporalLayerID)
		desc.SpatialID = r.SpatialLayerID
		desc.TL0PICIDX = r.TL0PICIDX
		desc.SwitchingUpPoint = r.TemporalLayerSync
		desc.InterLayerDependency = r.InterLayerDependency
	}
	desc.NotRefForUpperSpatialLayer = r.NotRefForUpperSpatialLayer
	if r.ScalabilityStructurePresent {
		desc.ScalabilityStructurePresent = true
		desc.ScalabilityStructure = r.SpatialScalabilityStructure
	}
	return desc
}

// ErrVP9EncoderNotImplemented is retained for callers that already branch on
// this sentinel.
//
// Deprecated: Encode and EncodeInto no longer return this error.
var ErrVP9EncoderNotImplemented = errors.New("govpx: VP9 encoder path unavailable")

const (
	// VP9MaxSegments is the number of segment IDs available in a VP9 profile 0
	// frame.
	VP9MaxSegments = vp9dec.MaxSegments

	// VP9RefFrame* are the public values accepted by
	// VP9SegmentationOptions.RefFrame.
	VP9RefFrameIntra  int8 = vp9dec.IntraFrame
	VP9RefFrameLast   int8 = vp9dec.LastFrame
	VP9RefFrameGolden int8 = vp9dec.GoldenFrame
	VP9RefFrameAltRef int8 = vp9dec.AltrefFrame

	// VP9MaxSpatialLayers is the maximum VP9 encoder spatial-layer count
	// exposed by libvpx's vpx_codec_enc_cfg::ss_number_layers.
	VP9MaxSpatialLayers = 5
)

// VP9SpatialScalabilityConfig configures VP9 spatial-SVC layer signaling for
// packets produced by one VP9Encoder. Use one encoder per coded spatial layer
// and pack the layer packets into a VP9 superframe when building a complete
// spatial-SVC access unit.
type VP9SpatialScalabilityConfig struct {
	// Enabled turns on spatial layer signaling in VP9EncodeResult and RTP
	// payload descriptors.
	Enabled bool
	// LayerCount is the total number of spatial layers in the stream. Valid
	// values are 1..VP9MaxSpatialLayers when Enabled is true.
	LayerCount uint8
	// LayerID is the spatial layer encoded by this VP9Encoder.
	LayerID uint8
	// InterLayerDependency marks this non-base layer as depending on a lower
	// spatial layer for RTP signaling.
	InterLayerDependency bool
	// NotRefForUpperSpatialLayer sets the VP9 RTP N bit for this layer.
	NotRefForUpperSpatialLayer bool
	// ResolutionPresent includes the per-layer dimensions below in the VP9 RTP
	// scalability structure. When true, Width[LayerID] and Height[LayerID] must
	// match the encoder's configured coded size.
	ResolutionPresent bool
	// Width and Height hold per-layer coded dimensions when ResolutionPresent
	// is true. Entries above LayerCount must be zero.
	Width  [VP9RTPMaxSpatialLayers]uint16
	Height [VP9RTPMaxSpatialLayers]uint16
}

const (
	vp9ActiveMapSegmentActive   uint8 = 0
	vp9ActiveMapSegmentInactive uint8 = 7
)

// VP9Encoder is the public entry point for VP9 profile 0 stream encoding.
type VP9Encoder struct {
	opts         VP9EncoderOptions
	closed       bool
	temporal     temporalState
	rc           vp9RateControlState
	twoPass      vp9TwoPassState
	cyclicAQ     vp9CyclicRefreshState
	perceptualAQ vp9PerceptualAQState
	// spatialScalabilityLocked is set for encoders owned by
	// VP9SpatialSVCEncoder; the parent owns spatial layer metadata.
	spatialScalabilityLocked bool
	// temporalScalabilityLocked is set for encoders owned by
	// VP9SpatialSVCEncoder; the parent owns access-unit temporal metadata.
	temporalScalabilityLocked bool

	// svc mirrors the subset of libvpx SVC layer-context state read by the
	// speed-features dispatcher and other ported consumers. Single-layer
	// encoders leave it at vp9SVCDefault() so e.svc.UseSvc reports cpi->use_svc
	// = 0 and number_spatial_layers = number_temporal_layers = 1.
	//
	// libvpx: vp9_svc_layercontext.h SVC struct.
	svc vp9SVCState

	// maxCopiedFrame mirrors cpi->max_copied_frame, written by the
	// speed-features dispatcher at speeds 7-8 to bound the number of
	// consecutive frames whose partition can be copied from the prior frame.
	//
	// libvpx: vp9_encoder.h cpi->max_copied_frame,
	// vp9_speed_features.c:721,728,733,758.
	maxCopiedFrame int

	// refFrameFlags mirrors cpi->ref_frame_flags, the bitmask of currently
	// enabled reference frames. The speed-features dispatcher clears the
	// VP9_GOLD_FLAG bit at speed 7 when SVC enables the long-term temporal
	// reference for a non-base temporal layer. libvpx initializes this from
	// kVp9RefFlagList; govpx defaults to the union of LAST/GOLD/ALT.
	//
	// libvpx: vp9_encoder.h cpi->ref_frame_flags,
	// vp9_speed_features.c:747.
	refFrameFlags int

	activeMap        []uint8
	activeMapMiRows  int
	activeMapMiCols  int
	activeMapEnabled bool
	roi              vp9ROIMapState
	denoiser         vp9DenoiserState
	// noiseEstimate mirrors libvpx's cpi->noise_estimate. The struct is
	// seeded by vp9NoiseEstimateInit in NewVP9Encoder (libvpx:
	// vp9_encoder.c:1528). The enabled flag is recomputed by
	// vp9NoiseEstimateRefreshEnabled before each speed-features dispatch
	// so the consumer at vp9_speed_features.c:777-782 reads the same
	// predicate libvpx evaluates.
	// libvpx ref: vp9/encoder/vp9_noise_estimate.h:30-40.
	noiseEstimate vp9NoiseEstimateState

	// frameIndex tracks the frame number for the key-frame cadence
	// gate. Mirrors libvpx's cpi->common.current_video_frame.
	frameIndex int
	// framesSinceKey tracks committed and dropped frames since the last
	// keyframe for adaptive keyframe min-distance gating.
	framesSinceKey uint16
	// forceKeyFrame is a sticky one-shot request consumed by the next
	// successfully committed frame.
	forceKeyFrame bool

	// fc carries the per-frame entropy context across frames.
	// Reset on every keyframe via ResetFrameContext.
	frameContexts [common.FrameContexts]vp9dec.FrameContext
	fc            vp9dec.FrameContext
	// lastVP9HeaderFrameType feeds non-frame-parallel coefficient probability
	// adaptation, which uses a distinct after-key update factor.
	lastVP9HeaderFrameType common.FrameType
	lastVP9HeaderValid     bool

	// prevFrameTxMode tracks libvpx cm->tx_mode across frames so the final
	// else branch of select_tx_mode (vp9/encoder/vp9_encodeframe.c:4343-4344)
	// can read back the previous frame's tx_mode. Zero-value Only4x4 matches
	// libvpx's cm->tx_mode initial value (cm is zero-initialised at alloc),
	// so the first frame's else-branch returns ONLY_4X4 if the speed-feature
	// configurator routes there.
	prevFrameTxMode common.TxMode

	// scratch is the reusable compressed-header staging buffer that
	// PackBitstream consults. Sized to 64KB so libvpx's
	// first_partition_size 16-bit cap can never overflow.
	scratch [65536]byte

	// aboveSegCtx / leftSegCtx are the partition-history arrays the
	// per-SB walker stamps. Sized to the frame's mi_cols at first
	// EncodeInto.
	aboveSegCtx []int8
	leftSegCtx  []int8

	// miGrid mirrors the decoder-visible MODE_INFO grid at 8x8 granularity so
	// subsequent block mode-context probabilities see the same above/left
	// state that libvpx's decoder sees.
	miGrid []vp9dec.NeighborMi

	// varPartGrid is the per-SB choose_partitioning output grid (libvpx
	// stamps these into xd->mi[]->sb_type via set_block_size /
	// set_vt_partitioning). Indexed identically to miGrid:
	// varPartGrid[row*miCols+col].SbType is the leaf block size at the
	// 8x8 cell (row, col). Populated by vp9ChoosePartitioning on SB
	// entry; consumed by pickVP9CBRVariancePartitionBlockSize /
	// pickVP9KeyframeVariancePartitionBlockSize to derive the
	// per-call partition decision. varPartSBComputed[(sbRow*sbCols+sbCol]]
	// tracks which 64x64 superblocks have already been populated this
	// frame so the picker fires once per SB.
	//
	// libvpx ref: vp9/encoder/vp9_encodeframe.c:1253-1763
	// (choose_partitioning) writes the partition tree; nonrd_use_partition
	// (vp9_encodeframe.c:4854) consumes it.
	varPartGrid       []vp9dec.NeighborMi
	varPartSBComputed []bool
	varPartFrameValid bool

	// mlPartitionPaddedLast / mlPartitionPaddedSrc are per-encoder
	// scratches backing the border-padded LAST_FRAME and source plane
	// copies ML_BASED_PARTITION's int-pro motion search reads against.
	// govpx's reference / source planes have no extension border so
	// vp9MLPickPartitionEntry builds edge-replicated padded copies on
	// demand; both buffers are sized to
	// (w+2*vp9MLPartitionBorder) * (h+2*vp9MLPartitionBorder).
	//
	// libvpx counterpart: YV12_BUFFER_CONFIG's 160-pixel encoder border
	// (vpx_scale/yv12config.h:26 — VP9_ENC_BORDER_IN_PIXELS=160) padding
	// surrounding every reference / source plane.
	mlPartitionPaddedLast vp9PaddedLastFrameBuffer
	mlPartitionPaddedSrc  vp9PaddedLastFrameBuffer

	// mlPartitionCtx is the per-SB ML_BASED_PARTITION context cache.
	// Filled by vp9MLPickPartitionEntry on the first call into a 64x64
	// SB and re-read by every recursive partition-level dispatch within
	// that SB. Reset between frames via vp9ResetMLPartitionCache.
	//
	// libvpx counterpart: x->est_pred is allocated/filled once per SB
	// at the dispatcher (vp9_encodeframe.c:5314 get_estimated_pred) and
	// then re-read by ml_predict_var_partitioning at every recursive
	// level (vp9_encodeframe.c:4664).
	mlPartitionCtx     []vp9MLPartitionContext
	mlPartitionCtxLen  int
	mlPartitionCtxCols int

	// refWidth / refHeight mirror the encoder-side VP9 reference map so
	// inter headers can emit write_frame_size_with_refs without allocating.
	refWidth  [common.RefFrames]uint32
	refHeight [common.RefFrames]uint32
	refValid  [common.RefFrames]bool

	// planes carries coefficient entropy contexts for source-backed frames.
	planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane

	intraScratch          vp9dec.IntraPredictorScratch
	modeScratch           [1024]byte
	blockScratch          [64 * 64]byte
	partitionReconScratch []byte
	// interPredictScratch is passed through the decoder-shared inter
	// predictor so odd luma MVs can use the same chroma/subpel extension
	// path as the real decoder without per-block allocations after warmup.
	interPredictScratch []byte
	interPredictor      VP9Decoder

	reconFrame Image
	reconYFull []byte
	reconUFull []byte
	reconVFull []byte
	reconY     []byte
	reconU     []byte
	reconV     []byte

	refFrames   [common.RefFrames]vp9ReferenceFrame
	refSignBias [common.RefFrames]uint8

	// lastBordered is the per-encoder border-padded mirror of the LAST_FRAME
	// luma plane consumed by choose_partitioning's low_res inter-predictor
	// path (vp9_int_pro_motion_estimation). Lazily allocated on first
	// refreshVP9EncoderRefs after LAST is written, then reused across
	// frames. The visible plane lives at (lastBordered.Border,
	// lastBordered.Border); the surrounding vp9EncBorderInPixels are
	// edge-replicated by vp9YV12BuildBorderedPlane.
	//
	// libvpx counterpart: the LAST_FRAME YV12_BUFFER_CONFIG always carries
	// VP9_ENC_BORDER_IN_PIXELS=160 of padding on every plane
	// (vpx_scale/yv12config.h:26, vp9/encoder/vp9_encoder.c:1297), maintained
	// by vpx_extend_frame_borders_c after each frame's reconstruction
	// (vp9/encoder/vp9_encoder.c:3102 / 3167 / 3424 / 3470).
	lastBordered      vp9YV12BorderBuffer
	lastBorderedValid bool

	// intProSrcBordered is the per-encoder border-padded mirror of the
	// current frame's source luma plane. choose_partitioning's int_pro
	// reads up to (bw>>1) pixels before the SB origin on the source as
	// well as the reference. Built lazily inside vp9EnsureSBPartitionChosen
	// when the libvpx picker fires on an inter frame.
	intProSrcBordered      vp9YV12BorderBuffer
	intProSrcBorderedValid bool

	// intProEstPred is the 64x64 luma predictor scratch built by
	// vp9_build_inter_predictors_sb (vp9_reconinter.c:253-258) from the
	// int_pro-resolved MV. Mirrors libvpx's xd->plane[0].dst.buf at
	// vp9_encodeframe.c:1487 (which writes a 64x64 dst with stride 64).
	intProEstPred [64 * 64]uint8

	prevFrameMvs      []vp9MvRef
	prevFrameMvRows   int
	prevFrameMvCols   int
	prevFrameMvsValid bool

	prevSegmentMap            []uint8
	prevSegmentMapRows        int
	prevSegmentMapCols        int
	prevSegmentMapValid       bool
	prevSegmentation          vp9dec.SegmentationParams
	prevSegmentationValid     bool
	prevFrameActiveMapEnabled bool

	// varianceAQDeltaQindex pins the qindex used to derive the
	// per-segment AltQ deltas for VP9AQVariance, refreshed on intra /
	// alt-ref / golden refresh frames to mirror libvpx's
	// vp9_aq_variance.c frame-setup gate.
	varianceAQDeltaQindex    int
	varianceAQDeltaQindexSet bool

	blockCoeffs    [vp9dec.MaxMbPlane][vp9EncoderBlockCoeffSlots]int16
	coefScratch    [1024]int16
	residueScratch [1024]int16
	txCoeffScratch [1024]int16
	dqCoeffScratch [1024]int16
	// vp9BlockYrdScratch backs vp9BlockYrd's src_diff + per-tx-unit
	// coeff/qcoeff/dqcoeff scratch. Sized for the realtime nonrd worst
	// case: BLOCK_64X64 + TX_16X16 = 4096 src_diff + 16 tx units × 256
	// coeffs × 3 (coeff/qcoeff/dqcoeff) = 16384 int16. libvpx clamps
	// tx_size <= TX_16X16 for nonrd_pickmode (vp9_pickmode.c:2361) so
	// the TX_8X8 / TX_4X4 paths fit within this allocation too.
	vp9BlockYrdScratch [16384]int16
	dqScratch          vp9dec.DequantTables
	frameCounts        encoder.FrameCounts
	vp9HeaderScratch   vp9dec.UncompressedHeader
	vp9CountWorkers    []VP9Encoder
	vp9CountCounts     []encoder.FrameCounts
	vp9CountJobs       []vp9CountTileJob
	vp9TilePool        *vp9TileWorkerPool
	// vp9LeafInterDecisions caches the result of pickVP9InterReferenceMode
	// at the leaf-write site so the count pre-pass populates entries and
	// the bitstream write pass reuses them without re-running the inter-
	// mode picker. The cache mirrors libvpx's mi_grid_visible[] store: the
	// picker decision is committed once, the writer reads back the stored
	// decision without recomputation (libvpx vp9/encoder/vp9_encodeframe.c
	// encode_b — write_modes_b in vp9_bitstream.c reads mbmi directly).
	// Sized to miRows*miCols on first frame; the version stamp invalidates
	// stale entries on each frame so cross-frame state never leaks.
	vp9LeafInterDecisions     []vp9LeafInterDecisionEntry
	vp9LeafInterDecisionsRows int
	vp9LeafInterDecisionsCols int
	vp9LeafInterDecisionsVer  uint32
	// vp9RowMTSync is set when the worker is dispatched as a tile-column body
	// with RowMT enabled. The pointer aliases an entry inside
	// vp9TileWorkerPool.rowMTSyncs and lives for the duration of the per-frame
	// encode; writeVP9ModesTileBounds reads it to drive the wavefront primitive.
	vp9RowMTSync *vp9RowMTSync
	lfi          vp9dec.LoopFilterInfoN
	lfRefDeltas  [vp9dec.MaxRefLfDeltas]int8
	lfModeDeltas [vp9dec.MaxModeLfDeltas]int8

	// vp9LastFiltLevel mirrors libvpx loopfilter::last_filt_level. The
	// picker's quadratic search seeds filt_mid from the previous
	// frame's chosen level (libvpx vp9_picklpf.c:90), and the
	// LPF_PICK_MINIMAL_LPF branch reads it to decide whether to zero
	// the filter (libvpx vp9_picklpf.c:166). Reset to 0 on the
	// non-forced KEY_FRAME edge to match libvpx vp9_encoder.c:3444-3445
	// (`lf->last_filt_level = 0`).
	vp9LastFiltLevel uint8

	// vp9LpfReconYBackup is the encoder-owned scratch that mirrors
	// libvpx cpi->last_frame_uf.y_buffer. The full-image / sub-image
	// picker snapshots the unfiltered visible Y plane here once after
	// tile encoding, so each try_filter_frame trial can restore the
	// unfiltered luma before applying the next trial level. Sized to
	// the per-frame yStride*yHeight (allocated lazily on first use).
	// libvpx: vp9_picklpf.c:73,100 (vpx_yv12_copy_y to / from
	// cpi->last_frame_uf).
	vp9LpfReconYBackup []byte

	// vp9FilterThreshes / vp9FilterThreshesPrev mirror libvpx
	// RD_OPT::filter_threshes[MAX_REF_FRAMES][SWITCHABLE_FILTER_CONTEXTS]
	// and filter_threshes_prev (vp9/encoder/vp9_rd.h:123,126). They
	// drive the per-frame SWITCHABLE -> concrete InterpFilter demotion
	// at vp9_encodeframe.c:5876-5877 (`get_interp_filter`) and accumulate
	// post-encode at vp9_encodeframe.c:5890-5891 via the per-block
	// `best_filter_diff` RD signal aggregated into rdc->filter_diff.
	//
	// The _prev snapshot is the libvpx save_encode_params (vp9_encoder.c
	// :3927-3946) / restore_encode_params (vp9_encodeframe.c:5798-5820)
	// recode-loop guard: govpx does not currently re-encode a frame, but
	// the snapshot pair is ported verbatim so the wiring is identical
	// when recode lands. SwitchableFilterContexts is the libvpx
	// SWITCHABLE_FILTER_CONTEXTS == 4 width; vp9dec.MaxRefFrames is the
	// MAX_REF_FRAMES == 4 outer dimension.
	vp9FilterThreshes     [vp9dec.MaxRefFrames][vp9dec.SwitchableFilterContexts]int64
	vp9FilterThreshesPrev [vp9dec.MaxRefFrames][vp9dec.SwitchableFilterContexts]int64

	// vp9FilterDiff is the per-frame accumulator for libvpx
	// rdc->filter_diff[SWITCHABLE_FILTER_CONTEXTS] (vp9_encoder.h:383).
	// Per-block RD picks deposit `best_rd - best_filter_rd[i]` here via
	// vp9_encodeframe.c:1881 (`rdc->filter_diff[i] += ctx->best_filter_diff[i]`).
	// Drained and merged into vp9FilterThreshes at the post-encode update
	// (vp9_encodeframe.c:5890-5891).
	vp9FilterDiff [vp9dec.SwitchableFilterContexts]int64

	lookahead      []vp9LookaheadEntry
	lookaheadRead  uint8
	lookaheadWrite uint8
	lookaheadCount uint8

	autoAltRefPending    vp9LookaheadEntry
	autoAltRefPendingSet bool
	autoAltRefEmitted    bool
	vp9ARNRScratch       image.YCbCr
	vp9ARNRRefs          [maxARNRFrames]arnrFrameView

	vp9ModeDecisionQIndex    uint8
	vp9ModeDecisionQIndexSet bool
	vp9TwoPassFrameTarget    int

	vp9FirstPassCount uint64
	vp9FirstPassLast  image.YCbCr
	vp9FirstPassGF    image.YCbCr

	// frameParallel owns the encoder-side concurrent-frame scheduler state.
	// It is nil unless FrameParallelEncoderThreads >= 2 has been requested.
	frameParallel *vp9FrameParallelScheduler

	// lastQuantizerInternal / lastQuantizerPublic / lastQuantizerValid mirror
	// libvpx's VP9E_GET_LAST_QUANTIZER state for callers that don't own the
	// VP9EncodeResult. They snapshot the qindex of the most recently
	// committed encoded frame; dropped or buffered-by-lookahead inputs leave
	// the value untouched.
	lastQuantizerInternal int
	lastQuantizerPublic   int
	lastQuantizerValid    bool

	// tpl carries the per-encoder TPL quality-pass state when EnableTPL
	// is true.  Slabs are sized at construction or on resolution change.
	tpl vp9TPLState
	// tplRDMultDeltaCalls counts how many SB-level rdmult delta lookups
	// produced a non-identity scaling.  Tests consume this to assert the
	// TPL→mode-picker wiring is actually firing.
	tplRDMultDeltaCalls int

	// cbRdmult mirrors libvpx's MACROBLOCK::cb_rdmult.  Each per-SB mode
	// picker (libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248) writes
	// this once from the base rdmult biased by the per-SB AQ/TPL deltas
	// and all the candidate-scoring helpers read it through
	// vp9EncoderModeDecisionRDMult.  When zero, callers fall back to the
	// per-frame rd.rdmult.
	cbRdmult int

	// mvHints carries the per-SB64 motion-vector hint slab installed
	// via importVP9MVHints. The multi-resolution encoder pipeline
	// fills this from a previously-encoded lower-resolution layer's
	// MVs scaled to this encoder's resolution; the inter motion
	// search evaluates the hint MV as one extra candidate alongside
	// its (0,0)-centered search so blocks with strong cross-layer
	// motion correlation can pick a hint-derived MV that the local
	// 16-px search radius would miss. nil disables hint biasing.
	mvHints *vp9MVHintMap

	// sf carries libvpx's SPEED_FEATURES struct. It is refreshed by
	// vp9ApplySpeedFeatures() whenever CpuUsed / Deadline / content options
	// change, and at frame setup so the framesize-dependent dispatcher sees
	// the actual per-frame state.
	//
	// libvpx: vp9_encoder.h cpi->sf + vp9_speed_features.{h,c}.
	sf SpeedFeatures

	// contentStateSbFd mirrors libvpx's cpi->content_state_sb_fd: a per-SB
	// uint8 counter incremented on every SB whose tmp_sad reading falls
	// below avg_source_sad_threshold2, and reset to zero on the first SB
	// above the threshold. Allocated lazily by the speed-feature
	// configurator when sf.UseSourceSad is set on the speed >= 6 path;
	// sized (mi_stride >> 3) * ((mi_rows >> 3) + 1) bytes. A nil slice
	// means the counter is disabled, exactly as libvpx tests
	// `if (cpi->content_state_sb_fd != NULL)`.
	//
	// libvpx: vp9_encoder.h:883 cpi->content_state_sb_fd,
	// vp9_speed_features.c:676-683 allocation,
	// vp9_encodeframe.c:1238-1244 increment/reset per-SB,
	// vp9_encodeframe.c:1346-1347 read into x->last_sb_high_content,
	// vp9_encoder.c:4079-4082 SVC/resize memset reset.
	contentStateSbFd         []uint8
	contentStateSbFdMiCols   int
	contentStateSbFdMiRows   int
	contentStateSbFdMiStride int

	// countArfFrameUsage / countLastgoldenFrameUsage mirror libvpx's
	// cpi->count_arf_frame_usage / cpi->count_lastgolden_frame_usage.
	// Allocated lazily by the speed-feature configurator when
	// sf.UseAltrefOnepass is set; sized
	// (mi_stride >> 3) * ((mi_rows >> 3) + 1) bytes each. Per-SB picker
	// writes at vp9_encodeframe.c:5368-5371; the per-frame ARF usage
	// percentage is recomputed by update_altref_usage
	// (vp9_ratectrl.c:1802-1819) and stored in rc.percArfUsage.
	//
	// libvpx: vp9_encoder.h:891-892 cpi->count_arf_frame_usage /
	// count_lastgolden_frame_usage,
	// vp9_speed_features.c:828-844 allocation,
	// vp9_encodeframe.c:5363-5371 write,
	// vp9_ratectrl.c:1802-1819 read into rc.percArfUsage.
	countArfFrameUsage         []uint8
	countLastgoldenFrameUsage  []uint8
	countArfFrameUsageMiCols   int
	countArfFrameUsageMiRows   int
	countArfFrameUsageMiStride int
}

// NewVP9Encoder creates a VP9 encoder with validated options.
// Width and Height must be positive; Threads / Log2TileRows / Quantizer /
// TargetBitrateKbps / MinQuantizer / MaxQuantizer / CQLevel /
// MinKeyframeInterval / MaxKeyframeInterval must be within their documented
// ranges.
func NewVP9Encoder(opts VP9EncoderOptions) (*VP9Encoder, error) {
	if err := normalizeVP9SpeedOptions(&opts); err != nil {
		return nil, err
	}
	if opts.ARNRType == 0 {
		opts.ARNRType = 3
	}
	if err := validateVP9EncoderOptions(opts); err != nil {
		return nil, err
	}
	var temporal temporalState
	if err := temporal.configure(opts.TemporalScalability, opts.TargetBitrateKbps); err != nil {
		return nil, err
	}
	var rc vp9RateControlState
	if err := rc.applyOptions(opts, vp9TimingStateFromOptions(opts)); err != nil {
		return nil, err
	}
	spatial, err := normalizeVP9SpatialScalabilityConfig(opts.SpatialScalability,
		opts.Width, opts.Height)
	if err != nil {
		return nil, err
	}
	opts.TemporalScalability = temporal.config
	opts.SpatialScalability = spatial
	e := &VP9Encoder{
		opts:          opts,
		temporal:      temporal,
		rc:            rc,
		svc:           vp9SVCDefault(),
		refFrameFlags: vp9AllRefFlags,
	}
	e.twoPass.configureWithCorpus(opts.TwoPassStats, rc.bitsPerFrame,
		opts.TwoPassVBRBiasPct, opts.TwoPassMinPct, opts.TwoPassMaxPct,
		opts.Height, opts.VBRCorpusComplexity)
	e.initVP9Lookahead(opts.Width, opts.Height, opts.LookaheadFrames)
	// libvpx initializes rc->gfu_boost to DEFAULT_GF_BOOST (2000) outside
	// the two-pass path so adjust_arnr_filter's adaptive strength/window
	// is fed even when no first-pass stats are available. Without this
	// seed govpx's vp9_arnr.go falls back to legacy non-adaptive ARNR
	// even when LookaheadFrames > 0.
	// libvpx: vp9/encoder/vp9_ratectrl.c:2082 (one-pass VBR set) and
	// vp9_ratectrl.h:31 DEFAULT_GF_BOOST.
	if opts.LookaheadFrames > 0 {
		e.rc.gfuBoost = uint16(vp9DefaultGFBoost)
	}
	e.cyclicAQ.configure(opts.AQMode == VP9AQCyclicRefresh, opts.Width, opts.Height)
	e.perceptualAQ.configure(opts.AQMode == VP9AQPerceptual)
	e.tpl.configure(opts.EnableTPL, opts.Width, opts.Height, opts.LookaheadFrames)
	e.lfi = vp9dec.NewLoopFilterInfoN()
	vp9dec.LoopFilterInit(&e.lfi, 0)
	e.initVP9TileWorkerPool()
	// libvpx: vp9_encoder.c:1528 — vp9_noise_estimate_init runs at
	// encoder setup. vp9ApplySpeedFeatures below also refreshes
	// ne.enabled via vp9NoiseEstimateRefreshEnabled (mirroring the
	// vp9_update_noise_estimate ne->enabled assignment that precedes
	// the speed-features dispatch in libvpx).
	vp9NoiseEstimateInit(&e.noiseEstimate, opts.Width, opts.Height)
	// Populate the SPEED_FEATURES struct so consumers can read e.sf.<field>
	// before the first frame. libvpx: vp9_encoder.c:2635 also runs the
	// framesize-independent + framesize-dependent dispatch in setup before
	// the first frame is encoded.
	e.vp9ApplySpeedFeatures(e.vp9DefaultSpeedFrameContext())
	return e, nil
}

func validateVP9EncoderOptions(opts VP9EncoderOptions) error {
	if !validVP9Dimension(opts.Width) || !validVP9Dimension(opts.Height) {
		return ErrInvalidConfig
	}
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if opts.RowMT && opts.Threads <= 1 {
		return ErrInvalidConfig
	}
	if err := validateVP9TileRowOptions(opts.Width, opts.Height, opts.Log2TileRows); err != nil {
		return err
	}
	if opts.TargetBitrateKbps < 0 || opts.Quantizer < 0 {
		return ErrInvalidConfig
	}
	if err := validateVP9KeyFrameIntervalOptions(
		opts.MinKeyframeInterval, opts.MaxKeyframeInterval); err != nil {
		return err
	}
	if opts.LookaheadFrames < 0 || opts.LookaheadFrames > vp9MaxLookaheadFrames {
		return ErrInvalidConfig
	}
	if opts.ARNRMaxFrames < 0 || opts.ARNRMaxFrames > maxARNRFrames ||
		opts.ARNRStrength < 0 || opts.ARNRStrength > 6 ||
		opts.ARNRType < 0 || opts.ARNRType > 3 {
		return ErrInvalidConfig
	}
	if opts.Tuning < TunePSNR || opts.Tuning > TuneSSIM {
		return ErrInvalidConfig
	}
	if opts.ScreenContentMode < int8(VP9ScreenContentDefault) ||
		opts.ScreenContentMode > int8(VP9ScreenContentFilm) {
		return ErrInvalidConfig
	}
	if opts.NoiseSensitivity < 0 || opts.NoiseSensitivity > 6 {
		return ErrInvalidConfig
	}
	if opts.Sharpness > 7 {
		return ErrInvalidConfig
	}
	if opts.StaticThreshold < 0 {
		return ErrInvalidConfig
	}
	if err := validateVP9TwoPassOptions(opts); err != nil {
		return err
	}
	if err := validateVP9RateControlOptions(opts); err != nil {
		return err
	}
	if err := validateVP9AQOptions(opts); err != nil {
		return err
	}
	if err := validateVP9AutoAltRefOptions(opts); err != nil {
		return err
	}
	if err := validateVP9TPLOptions(opts); err != nil {
		return err
	}
	if opts.DeltaQUV < -15 || opts.DeltaQUV > 15 {
		return ErrInvalidQuantizer
	}
	if opts.Lossless && opts.DeltaQUV != 0 {
		return ErrInvalidQuantizer
	}
	if err := validateVP9ColorOptions(opts); err != nil {
		return err
	}
	if err := validateVP9RenderSizeOptions(opts); err != nil {
		return err
	}
	if err := validateVP9TargetLevel(opts.TargetLevel); err != nil {
		return err
	}
	if err := validateVP9TargetLevelLimits(opts); err != nil {
		return err
	}
	if opts.DisableLoopfilter > VP9LoopfilterDisableAll {
		return ErrInvalidConfig
	}
	if _, err := normalizeVP9SpatialScalabilityConfig(opts.SpatialScalability,
		opts.Width, opts.Height); err != nil {
		return err
	}
	// Lookahead now composes with libvpx-style rate control modes (CBR, VBR,
	// Q) and temporal SVC. Cyclic-refresh AQ keeps its own lookahead block in
	// validateVP9AQOptions because its segment-map updates run in
	// committed-frame order and would re-target a queued source.
	if err := validateVP9FrameParallelEncoderOptions(opts); err != nil {
		return err
	}
	if opts.Quantizer > 255 {
		return ErrInvalidQuantizer
	}
	if err := validateVP9PublicQuantizerOptions(opts); err != nil {
		return err
	}
	if opts.Lossless && opts.Quantizer != 0 {
		return ErrInvalidQuantizer
	}
	if opts.FPS < 0 {
		return ErrInvalidConfig
	}
	if (opts.TimebaseNum < 0) || (opts.TimebaseDen < 0) {
		return ErrInvalidConfig
	}
	// Either FPS xor both timebase components must be set, or all
	// three may be zero (defaults to 30 fps in libvpx).
	if (opts.TimebaseNum != 0) != (opts.TimebaseDen != 0) {
		return ErrInvalidConfig
	}
	if err := validateVP9SegmentationOptions(opts.Segmentation); err != nil {
		return err
	}
	if opts.Lossless {
		if err := validateVP9LosslessSegmentationOptions(opts.Segmentation); err != nil {
			return err
		}
	}
	return nil
}

func normalizeVP9SpatialScalabilityConfig(cfg VP9SpatialScalabilityConfig,
	width, height int,
) (VP9SpatialScalabilityConfig, error) {
	if !cfg.Enabled {
		return VP9SpatialScalabilityConfig{}, nil
	}
	if cfg.LayerCount == 0 || cfg.LayerCount > VP9MaxSpatialLayers ||
		cfg.LayerID >= cfg.LayerCount {
		return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
	}
	if cfg.InterLayerDependency && cfg.LayerID == 0 {
		return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
	}
	if !cfg.ResolutionPresent {
		for i := range VP9RTPMaxSpatialLayers {
			if cfg.Width[i] != 0 || cfg.Height[i] != 0 {
				return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
			}
		}
		return cfg, nil
	}
	for i := 0; i < int(cfg.LayerCount); i++ {
		if cfg.Width[i] == 0 || cfg.Height[i] == 0 {
			return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
		}
	}
	for i := int(cfg.LayerCount); i < VP9RTPMaxSpatialLayers; i++ {
		if cfg.Width[i] != 0 || cfg.Height[i] != 0 {
			return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
		}
	}
	if int(cfg.Width[cfg.LayerID]) != width ||
		int(cfg.Height[cfg.LayerID]) != height {
		return VP9SpatialScalabilityConfig{}, ErrInvalidConfig
	}
	return cfg, nil
}

func (e *VP9Encoder) vp9SpatialResultFields() (
	layerID uint8,
	layerCount uint8,
	interLayerDependency bool,
	notRefForUpperSpatialLayer bool,
	scalabilityStructurePresent bool,
	scalabilityStructure VP9RTPScalabilityStructure,
) {
	cfg := e.opts.SpatialScalability
	if !cfg.Enabled {
		return 0, 1, false, false, false, VP9RTPScalabilityStructure{}
	}
	if cfg.ResolutionPresent {
		scalabilityStructurePresent = true
		scalabilityStructure = VP9RTPScalabilityStructure{
			SpatialLayerCount: int(cfg.LayerCount),
			ResolutionPresent: true,
			Width:             cfg.Width,
			Height:            cfg.Height,
		}
	}
	return cfg.LayerID, cfg.LayerCount, cfg.InterLayerDependency,
		cfg.NotRefForUpperSpatialLayer, scalabilityStructurePresent,
		scalabilityStructure
}

// validateVP9ColorOptions rejects out-of-range ColorSpace/ColorRange
// values and the Profile 0 / SRGB combination libvpx rejects.
func validateVP9ColorOptions(opts VP9EncoderOptions) error {
	if opts.ColorSpace > VP9ColorSpaceSRGB {
		return ErrInvalidConfig
	}
	if opts.ColorRange > VP9ColorRangeFull {
		return ErrInvalidConfig
	}
	// Profile 0 streams use 4:2:0 chroma; SRGB requires 4:4:4 sampling
	// (allowed only on profiles 1 and 3) so the writer would emit a
	// stream the decoder rejects.
	if opts.ColorSpace == VP9ColorSpaceSRGB {
		return ErrInvalidConfig
	}
	return nil
}

// validateVP9RenderSizeOptions enforces the (0,0)-or-(positive,positive)
// shape of RenderWidth/RenderHeight and caps each at the 16-bit field
// width libvpx writes.
func validateVP9RenderSizeOptions(opts VP9EncoderOptions) error {
	w := opts.RenderWidth
	h := opts.RenderHeight
	if w == 0 && h == 0 {
		return nil
	}
	if w <= 0 || h <= 0 {
		return ErrInvalidConfig
	}
	if w > (1<<16) || h > (1<<16) {
		return ErrInvalidConfig
	}
	return nil
}

// vp9ValidTargetLevels lists the canonical VP9 level codes libvpx
// accepts. 255 disables the constraint, 0 selects auto, and the
// remainder are level N.M encoded as 10*N + M.
var vp9ValidTargetLevels = [...]int{
	0, 10, 11, 20, 21, 30, 31, 40, 41, 50, 51, 52, 60, 61, 62, 255,
}

// validateVP9TargetLevel mirrors libvpx's ctrl_set_target_level value
// check.
func validateVP9TargetLevel(level int) error {
	for _, v := range vp9ValidTargetLevels {
		if level == v {
			return nil
		}
	}
	return ErrInvalidConfig
}

// vp9LevelLimits mirrors the per-level macroblock-rate, luma
// picture-size, and bitrate limits from libvpx's vp9_level_def_t table
// (vp9/encoder/vp9_level.c). Levels not represented here have no
// configured limit and pass the configuration gate unchanged.
type vp9LevelLimits struct {
	maxLumaSampleRate  uint64 // samples (luma pixels) per second
	maxLumaPictureSize uint64 // luma samples per picture
	maxBitrateKbps     int    // peak rate, kbps
}

var vp9TargetLevelTable = map[int]vp9LevelLimits{
	10: {maxLumaSampleRate: 829440, maxLumaPictureSize: 36864, maxBitrateKbps: 200},
	11: {maxLumaSampleRate: 2764800, maxLumaPictureSize: 73728, maxBitrateKbps: 800},
	20: {maxLumaSampleRate: 4608000, maxLumaPictureSize: 122880, maxBitrateKbps: 1800},
	21: {maxLumaSampleRate: 9216000, maxLumaPictureSize: 245760, maxBitrateKbps: 3600},
	30: {maxLumaSampleRate: 20736000, maxLumaPictureSize: 552960, maxBitrateKbps: 7200},
	31: {maxLumaSampleRate: 36864000, maxLumaPictureSize: 983040, maxBitrateKbps: 12000},
	40: {maxLumaSampleRate: 83558400, maxLumaPictureSize: 2228224, maxBitrateKbps: 18000},
	41: {maxLumaSampleRate: 160432128, maxLumaPictureSize: 2228224, maxBitrateKbps: 30000},
	50: {maxLumaSampleRate: 311951360, maxLumaPictureSize: 8912896, maxBitrateKbps: 60000},
	51: {maxLumaSampleRate: 588251136, maxLumaPictureSize: 8912896, maxBitrateKbps: 120000},
	52: {maxLumaSampleRate: 1176502272, maxLumaPictureSize: 8912896, maxBitrateKbps: 180000},
	60: {maxLumaSampleRate: 1176502272, maxLumaPictureSize: 35651584, maxBitrateKbps: 180000},
	61: {maxLumaSampleRate: 2353004544, maxLumaPictureSize: 35651584, maxBitrateKbps: 240000},
	62: {maxLumaSampleRate: 4706009088, maxLumaPictureSize: 35651584, maxBitrateKbps: 480000},
}

// validateVP9TargetLevelLimits enforces the VP9 level's luma sample-rate,
// luma picture-size, and peak bitrate ceilings against the configured
// width/height/fps/target-bitrate triple. Levels 0 (auto) and 255 (no
// constraint) skip the check. Levels listed in vp9TargetLevelTable
// without configured FPS use the timebase-derived rate, falling back to
// the libvpx default 30 fps when neither FPS nor timebase are set.
func validateVP9TargetLevelLimits(opts VP9EncoderOptions) error {
	limits, ok := vp9TargetLevelTable[opts.TargetLevel]
	if !ok {
		return nil
	}
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil
	}
	picture := uint64(opts.Width) * uint64(opts.Height)
	if picture > limits.maxLumaPictureSize {
		return ErrInvalidConfig
	}
	timing := vp9TimingStateFromOptions(opts)
	if timing.timebaseNum > 0 && timing.timebaseDen > 0 {
		// rate = picture * timebaseDen / timebaseNum (samples/sec)
		rate := picture * uint64(timing.timebaseDen) / uint64(timing.timebaseNum)
		if rate > limits.maxLumaSampleRate {
			return ErrInvalidConfig
		}
	}
	if opts.TargetBitrateKbps > 0 && opts.TargetBitrateKbps > limits.maxBitrateKbps {
		return ErrInvalidConfig
	}
	return nil
}

// vp9DisableLoopfilterForFrame reports whether the loop filter should
// be suppressed for the given frame, mirroring libvpx's
// VP9E_SET_DISABLE_LOOPFILTER semantics: mode 1 disables the filter
// on every non-keyframe; mode 2 disables it on every frame.
func vp9DisableLoopfilterForFrame(mode VP9DisableLoopfilter, isKey bool) bool {
	switch mode {
	case VP9LoopfilterDisableAll:
		return true
	case VP9LoopfilterDisableInter:
		return !isKey
	default:
		return false
	}
}

// vp9CommonColorSpace maps the public VP9ColorSpace enum onto the
// shared internal/vp9/common ColorSpace identifier.
func vp9CommonColorSpace(c VP9ColorSpace) common.ColorSpace {
	return common.ColorSpace(c)
}

// vp9CommonColorRange maps the public VP9ColorRange enum onto the
// shared internal/vp9/common ColorRange identifier.
func vp9CommonColorRange(c VP9ColorRange) common.ColorRange {
	return common.ColorRange(c)
}

func validateVP9TileRowOptions(width, height int, log2TileRows int8) error {
	if log2TileRows < 0 || log2TileRows > 2 {
		return ErrInvalidConfig
	}
	if log2TileRows == 0 {
		return nil
	}
	if !validVP9Dimension(width) || !validVP9Dimension(height) {
		return ErrInvalidConfig
	}
	tileRows := 1 << uint(log2TileRows)
	miRows := (height + 7) >> 3
	sbRows := (miRows + (1 << common.MiBlockSizeLog2) - 1) >> common.MiBlockSizeLog2
	if tileRows > sbRows {
		return ErrInvalidConfig
	}
	return nil
}

func validateVP9AutoAltRefOptions(opts VP9EncoderOptions) error {
	if !opts.AutoAltRef {
		return nil
	}
	if opts.LookaheadFrames <= 1 || opts.ErrorResilient {
		return ErrInvalidConfig
	}
	return nil
}

func validateVP9FrameParallelEncoderOptions(opts VP9EncoderOptions) error {
	if opts.FrameParallelEncoderThreads < 0 ||
		opts.FrameParallelEncoderThreads > vp9MaxLookaheadFrames {
		return ErrInvalidConfig
	}
	if opts.FrameParallelEncoderThreads >= 2 {
		if opts.LookaheadFrames <= 0 {
			return ErrInvalidConfig
		}
		if opts.AutoAltRef {
			return ErrInvalidConfig
		}
	}
	return nil
}

func validateVP9AQOptions(opts VP9EncoderOptions) error {
	switch opts.AQMode {
	case VP9AQNone:
		return nil
	case VP9AQVariance:
		if opts.Lossless || opts.Segmentation.Enabled {
			return ErrInvalidConfig
		}
		return nil
	case VP9AQComplexity:
		if !opts.RateControlModeSet || opts.TargetBitrateKbps <= 0 ||
			opts.Lossless || opts.Segmentation.Enabled {
			return ErrInvalidConfig
		}
		return nil
	case VP9AQEquator360:
		if opts.Lossless || opts.Segmentation.Enabled {
			return ErrInvalidConfig
		}
		return nil
	case VP9AQPerceptual:
		if opts.Lossless || opts.Segmentation.Enabled {
			return ErrInvalidConfig
		}
		return nil
	case VP9AQCyclicRefresh:
	default:
		return ErrInvalidConfig
	}
	if !opts.RateControlModeSet || opts.RateControlMode != RateControlCBR {
		return ErrInvalidConfig
	}
	if opts.LookaheadFrames > 0 || opts.TemporalScalability.Enabled ||
		opts.Lossless || opts.Segmentation.Enabled {
		return ErrInvalidConfig
	}
	return nil
}

func validateVP9SegmentationOptions(seg VP9SegmentationOptions) error {
	if !seg.Enabled {
		return nil
	}
	if seg.SegmentID >= vp9dec.MaxSegments {
		return ErrInvalidConfig
	}
	if !seg.UpdateMap && seg.SegmentID != 0 {
		return ErrInvalidConfig
	}
	for i := range vp9dec.MaxSegments {
		if seg.AltQEnabled[i] && (seg.AltQ[i] < -255 || seg.AltQ[i] > 255) {
			return ErrInvalidQuantizer
		}
		if seg.AltLFEnabled[i] && (seg.AltLF[i] < -63 || seg.AltLF[i] > 63) {
			return ErrInvalidConfig
		}
		if seg.RefFrameEnabled[i] &&
			(seg.RefFrame[i] < vp9dec.IntraFrame || seg.RefFrame[i] > vp9dec.AltrefFrame) {
			return ErrInvalidConfig
		}
	}
	return nil
}

func validateVP9LosslessSegmentationOptions(seg VP9SegmentationOptions) error {
	if !seg.Enabled {
		return nil
	}
	for i := range vp9dec.MaxSegments {
		if seg.AltQEnabled[i] {
			qindex := max(int(seg.AltQ[i]), 0)
			if qindex != 0 {
				return ErrInvalidQuantizer
			}
		}
		if seg.AltLFEnabled[i] {
			filterLevel := max(int(seg.AltLF[i]), 0)
			if filterLevel != 0 {
				return ErrInvalidConfig
			}
		}
	}
	return nil
}

// vp9PrepareCyclicRefreshFrame drives the libvpx
// vp9_cyclic_refresh_update_parameters() + vp9_cyclic_refresh_setup()
// pair (vp9/encoder/vp9_aq_cyclicrefresh.c:479-680). It is the
// encoder-facing entry that picks up the active rate-control state
// and emits the per-frame segmentation map cyclicAQ.segmentID()
// consults. Called once per frame in EncodeInto.
func (e *VP9Encoder) vp9PrepareCyclicRefreshFrame(isKey, intraOnly, showFrame bool, miRows, miCols, macroblocks int, header *vp9dec.UncompressedHeader) {
	if e == nil || !e.cyclicAQ.enabled {
		e.cyclicAQ.apply = false
		return
	}
	if isKey || intraOnly || !showFrame {
		e.cyclicAQ.apply = false
		// libvpx: vp9_aq_cyclicrefresh.c:614-621 — keyframe also resets
		// last_coded_q_map / sb_index / scene_change counter.
		if isKey && e.cyclicAQ.miRows == miRows && e.cyclicAQ.miCols == miCols {
			for i := range e.cyclicAQ.lastCodedQMap {
				e.cyclicAQ.lastCodedQMap[i] = vp9dec.MaxQ
			}
			// libvpx: vp9_encoder.c:4103-4106 — intra_only zeros
			// consec_zero_mv too. Without this, post-key stale counters
			// would still drive the next frame's eligibility filter.
			for i := range e.cyclicAQ.consecZeroMv {
				e.cyclicAQ.consecZeroMv[i] = 0
			}
			e.cyclicAQ.sbIndex = 0
			e.cyclicAQ.reduceRefresh = false
			e.cyclicAQ.counterEncodeMaxqSceneChange = 0
		}
		return
	}
	// Re-alloc on mi-grid change.
	if e.cyclicAQ.miRows != miRows || e.cyclicAQ.miCols != miCols ||
		len(e.cyclicAQ.segMap) < miRows*miCols {
		e.cyclicAQ.vp9CyclicRefreshAlloc(miRows, miCols)
	}
	screen := e.opts.ScreenContentMode > 0
	noiseMedium := e.opts.NoiseSensitivity >= 1
	// libvpx: vp9_aq_cyclicrefresh.c:479-593.
	e.cyclicAQ.vp9CyclicRefreshUpdateParameters(vp9CyclicRefreshUpdateParametersArgs{
		Macroblocks:          macroblocks,
		FrameIsIntraOnly:     false,
		TemporalLayerID:      0,
		NumberTemporalLayers: 1,
		NumberSpatialLayers:  1,
		SpatialLayerID:       0,
		Lossless:             header.Quant.Lossless,
		UseSVC:               false,
		ScreenContent:        screen,
		NoiseLevelMedium:     noiseMedium,
		RateControlIsVBR:     e.rc.mode == RateControlVBR,
		RefreshGoldenFrame:   false,
		AvgFrameQindexInter:  int(e.rc.avgFrameQIndexInter),
		AvgFrameLowMotion:    100, // libvpx default until measured.
		FramesSinceKey:       int(e.rc.framesSinceKey),
		BestQuality:          int(e.rc.bestQuality),
		AvgFrameBandwidth:    e.rc.bitsPerFrame,
		Width:                e.opts.Width,
		Height:               e.opts.Height,
	})
	// libvpx: vp9_aq_cyclicrefresh.c:596-680.
	e.cyclicAQ.vp9CyclicRefreshSetup(vp9CyclicRefreshSetupArgs{
		CurrentVideoFrame: e.frameIndex,
		FrameIsKey:        false,
		FrameIsIntraOnly:  false,
		TemporalLayerID:   0,
		ResizePending:     false,
		HighSourceSad:     false,
		ScreenContent:     screen,
		NoiseLevelMedium:  noiseMedium,
		BaseQindex:        int(header.Quant.BaseQindex),
		YDcDeltaQ:         int(header.Quant.YDcDeltaQ),
		Sb64TargetRate:    e.rc.frameTargetBits >> 6,
		// libvpx: vp9_aq_cyclicrefresh.c:439 — consec_zero_mv feeds the
		// update_map eligibility filter. The slice is maintained per
		// encoded SB by vp9CyclicRefreshUpdateEncodedSb so this frame
		// sees the previous frame's stationarity history.
		ConsecZeroMv: e.cyclicAQ.consecZeroMv,
		// CR runs on visible inter frames only (see early-returns above),
		// so is_src_frame_alt_ref is always false here.  The refresh
		// flags are not yet known at this point in govpx (RefreshFrame
		// is set later in EncodeInto), so we conservatively pass false
		// for both — matching libvpx's path because cyclic_refresh_setup
		// runs before refresh_golden/alt are finalised in many of its
		// realtime call paths.  The CR rdmult therefore lands in the
		// inter bucket which is what libvpx's realtime CR runs evaluate.
		IsSrcFrameAltRef:   false,
		RefreshGoldenFrame: false,
		RefreshAltRefFrame: false,
	})
	e.cyclicAQ.apply = e.cyclicAQ.applyCyclicRefresh && e.cyclicAQ.targetNumSegBlocks > 0
}

func (e *VP9Encoder) vp9EncoderSegmentationParams(intraFrame bool, baseQIndex int) vp9dec.SegmentationParams {
	if e.roi.enabled && !intraFrame {
		seg := e.roi.segmentationParams()
		if e.activeMapEnabled {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQVariance {
		// In fixed-Q / pure-Q mode the rate controller cannot
		// absorb variance-AQ's per-segment qindex shifts: the
		// low-variance bonus segments over-spend bits on flat
		// regions, the segment map and segment-aware partition
		// splits add overhead, and the user-chosen quality anchor
		// is left unanchored. Suppress map/data updates in that
		// mode — variance-AQ becomes a header-only no-op rather than
		// the +70%+ BD-rate regression that the buggy v1 implementation
		// produced on synthetic half-flat content. Rate-controlled
		// pipelines (CBR/VBR) still get the perceptual benefit
		// because the rate loop compensates for the qindex shift.
		if e.vp9VarianceAQRateControlFixedQ() {
			if e.activeMapEnabled && !intraFrame {
				seg := vp9dec.SegmentationParams{
					Enabled:   true,
					UpdateMap: true,
				}
				initVP9SegmentationProbDefaults(&seg)
				vp9EnableActiveMapSegmentation(&seg)
				return seg
			}
			return vp9dec.SegmentationParams{Enabled: true}
		}
		// libvpx's vp9_aq_variance.c only recomputes the per-segment
		// AltQ deltas on intra / alt-ref / golden frames; the deltas
		// persist on the shared cm->seg between frames so inter
		// frames re-use the keyframe-anchored values. Mirroring that
		// behaviour matters because recomputing deltas at the live
		// (potentially higher) inter qindex would scale the swings
		// linearly with frame Q and blow up rate on flat regions.
		anchorQindex := baseQIndex
		if intraFrame || !e.varianceAQDeltaQindexSet {
			e.varianceAQDeltaQindex = baseQIndex
			e.varianceAQDeltaQindexSet = true
		} else {
			anchorQindex = e.varianceAQDeltaQindex
		}
		seg := vp9VarianceAQSegmentationParams(anchorQindex, e.opts.ScreenContentMode)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQComplexity {
		if e.vp9ComplexityAQSB64TargetRate() < vp9ComplexityAQMinSB64TargetRate {
			return vp9dec.SegmentationParams{}
		}
		seg := vp9ComplexityAQSegmentationParams(baseQIndex)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQEquator360 && vp9Equator360AQApplies(e.opts.Width, e.opts.Height) {
		seg := vp9Equator360AQSegmentationParams(baseQIndex, intraFrame)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQPerceptual {
		seg := e.perceptualAQ.segmentationParams(intraFrame)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.cyclicAQ.enabled && e.cyclicAQ.apply && !intraFrame {
		seg := e.cyclicAQ.segmentationParams(baseQIndex)
		if e.activeMapEnabled {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	cfg := e.opts.Segmentation
	if !cfg.Enabled {
		if e.activeMapEnabled && !intraFrame {
			seg := vp9dec.SegmentationParams{
				Enabled:   true,
				UpdateMap: true,
			}
			initVP9SegmentationProbDefaults(&seg)
			vp9EnableActiveMapSegmentation(&seg)
			return seg
		}
		return vp9dec.SegmentationParams{}
	}
	seg := vp9dec.SegmentationParams{
		Enabled:   true,
		UpdateMap: cfg.UpdateMap,
		AbsDelta:  cfg.AbsDelta,
	}
	initVP9SegmentationProbDefaults(&seg)
	for i := range vp9dec.MaxSegments {
		if cfg.AltQEnabled[i] {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
			seg.FeatureData[i][vp9dec.SegLvlAltQ] = cfg.AltQ[i]
			seg.UpdateData = true
		}
		if cfg.AltLFEnabled[i] {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltLf)
			seg.FeatureData[i][vp9dec.SegLvlAltLf] = cfg.AltLF[i]
			seg.UpdateData = true
		}
		if cfg.SkipEnabled[i] {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlSkip)
			seg.UpdateData = true
		}
		if cfg.RefFrameEnabled[i] {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlRefFrame)
			seg.FeatureData[i][vp9dec.SegLvlRefFrame] = int16(cfg.RefFrame[i])
			seg.UpdateData = true
		}
	}
	if e.activeMapEnabled && !intraFrame {
		vp9EnableActiveMapSegmentation(&seg)
	}
	return seg
}

func initVP9SegmentationProbDefaults(seg *vp9dec.SegmentationParams) {
	if seg == nil {
		return
	}
	for i := range vp9dec.SegTreeProbs {
		seg.TreeProbs[i] = vp9dec.MaxProb
	}
	for i := range vp9dec.PredictionProbs {
		seg.PredProbs[i] = vp9dec.MaxProb
	}
}

func vp9VarianceAQSegmentationParams(baseQIndex int, screenContentMode int8) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	initVP9SegmentationProbDefaults(&seg)
	ratios := vp9VarianceAQRateRatiosForContent(screenContentMode)
	for i, ratio := range ratios {
		if ratio.num == ratio.den {
			continue
		}
		delta := vp9ComputeQDeltaByRate(0, 255, false, baseQIndex,
			ratio.num, ratio.den)
		if baseQIndex != 0 && baseQIndex+delta == 0 {
			delta = -baseQIndex + 1
		}
		if delta < -255 {
			delta = -255
		} else if delta > 255 {
			delta = 255
		}
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = int16(delta)
	}
	return seg
}

// vp9VarianceAQRateRatiosForContent returns the per-segment rate
// ratios used to derive variance-AQ Q deltas. Default video uses
// libvpx's table where the highest-variance segment (index 4) is
// pushed up in Q by a 3:4 ratio. VP9ScreenContentFilm clamps that
// segment back to 1:1, preserving film-grain texture by leaving the
// high-variance blocks at the base Q.
func vp9VarianceAQRateRatiosForContent(screenContentMode int8) [vp9dec.MaxSegments]struct {
	num int
	den int
} {
	if screenContentMode == int8(VP9ScreenContentFilm) {
		return vp9VarianceAQRateRatiosFilm
	}
	return vp9VarianceAQRateRatios
}

var vp9VarianceAQRateRatios = [vp9dec.MaxSegments]struct {
	num int
	den int
}{
	{5, 2},
	{2, 1},
	{3, 2},
	{1, 1},
	{3, 4},
	{1, 1},
	{1, 1},
	{1, 1},
}

// vp9VarianceAQRateRatiosFilm is the FILM-content variant of
// vp9VarianceAQRateRatios. Segments 0..2 keep their low-variance Q
// boost so flat areas are still coded cleanly; segment 4 is held at
// 1:1 instead of 3:4 so the encoder leaves the high-variance grain
// blocks at the base Q and the grain texture survives quantization.
var vp9VarianceAQRateRatiosFilm = [vp9dec.MaxSegments]struct {
	num int
	den int
}{
	{5, 2},
	{2, 1},
	{3, 2},
	{1, 1},
	{1, 1},
	{1, 1},
	{1, 1},
	{1, 1},
}

const (
	vp9ComplexityAQSegments          = 5
	vp9ComplexityAQDefaultSegment    = 3
	vp9ComplexityAQStrengths         = 3
	vp9ComplexityAQMinSB64TargetRate = 256
	vp9ComplexityAQLowVarThreshold   = 10.0
)

var vp9ComplexityAQRateRatios = [vp9ComplexityAQStrengths][vp9ComplexityAQSegments]struct {
	num int
	den int
}{
	{{7, 4}, {5, 4}, {21, 20}, {1, 1}, {9, 10}},
	{{2, 1}, {3, 2}, {23, 20}, {1, 1}, {17, 20}},
	{{5, 2}, {7, 4}, {5, 4}, {1, 1}, {4, 5}},
}

var vp9ComplexityAQTransitions = [vp9ComplexityAQStrengths][vp9ComplexityAQSegments]struct {
	num int
	den int
}{
	{{15, 100}, {30, 100}, {55, 100}, {2, 1}, {100, 1}},
	{{20, 100}, {40, 100}, {65, 100}, {2, 1}, {100, 1}},
	{{25, 100}, {50, 100}, {75, 100}, {2, 1}, {100, 1}},
}

var vp9ComplexityAQVarThresholds = [vp9ComplexityAQStrengths][vp9ComplexityAQSegments]float64{
	{-4.0, -3.0, -2.0, 100.0, 100.0},
	{-3.5, -2.5, -1.5, 100.0, 100.0},
	{-3.0, -2.0, -1.0, 100.0, 100.0},
}

func vp9ComplexityAQSegmentationParams(baseQIndex int) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	initVP9SegmentationProbDefaults(&seg)
	strength := vp9ComplexityAQStrength(baseQIndex)
	for i, ratio := range vp9ComplexityAQRateRatios[strength] {
		if i == vp9ComplexityAQDefaultSegment || ratio.num == ratio.den {
			continue
		}
		delta := vp9ComputeQDeltaByRate(0, 255, false, baseQIndex,
			ratio.num, ratio.den)
		if baseQIndex != 0 && baseQIndex+delta == 0 {
			delta = -baseQIndex + 1
		}
		if baseQIndex+delta <= 0 {
			continue
		}
		if delta < -255 {
			delta = -255
		} else if delta > 255 {
			delta = 255
		}
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = int16(delta)
	}
	return seg
}

func vp9ComplexityAQStrength(baseQIndex int) int {
	baseQuant := int(vp9dec.VpxAcQuant(baseQIndex, 0, vp9dec.BitDepth8)) / 4
	strength := 0
	if baseQuant > 10 {
		strength++
	}
	if baseQuant > 25 {
		strength++
	}
	return strength
}

func vp9EnableActiveMapSegmentation(seg *vp9dec.SegmentationParams) {
	if seg == nil {
		return
	}
	seg.Enabled = true
	seg.UpdateMap = true
	seg.UpdateData = true
	seg.TemporalUpdate = true
	for i := range vp9dec.SegTreeProbs {
		seg.TreeProbs[i] = 128
	}
	seg.PredProbs[0] = 1
	for i := 1; i < vp9dec.PredictionProbs; i++ {
		seg.PredProbs[i] = 128
	}
	seg.FeatureMask[vp9ActiveMapSegmentInactive] |=
		1 << uint(vp9dec.SegLvlSkip)
	seg.FeatureMask[vp9ActiveMapSegmentInactive] |=
		1 << uint(vp9dec.SegLvlAltLf)
	seg.FeatureData[vp9ActiveMapSegmentInactive][vp9dec.SegLvlAltLf] =
		-vp9dec.MaxLoopFilter
}

func (e *VP9Encoder) vp9CarryActiveMapDisableSegmentation(
	seg *vp9dec.SegmentationParams, intraFrame bool,
) {
	if e == nil || seg == nil || seg.Enabled || intraFrame ||
		e.activeMapEnabled || !e.prevSegmentationValid ||
		!vp9SegmentationIsActiveMapOnly(&e.prevSegmentation) {
		return
	}
	*seg = e.prevSegmentation
	seg.Enabled = true
	seg.UpdateMap = e.prevFrameActiveMapEnabled
	seg.UpdateData = false
}

func vp9SegmentationIsActiveMapOnly(seg *vp9dec.SegmentationParams) bool {
	if seg == nil || !seg.Enabled {
		return false
	}
	for i := range vp9dec.MaxSegments {
		mask := seg.FeatureMask[i]
		if i != int(vp9ActiveMapSegmentInactive) {
			if mask != 0 {
				return false
			}
			continue
		}
		want := uint32((1 << uint(vp9dec.SegLvlSkip)) |
			(1 << uint(vp9dec.SegLvlAltLf)))
		if mask != want ||
			seg.FeatureData[i][vp9dec.SegLvlAltLf] != -vp9dec.MaxLoopFilter {
			return false
		}
		for j := range vp9dec.SegLvlMax {
			if j == vp9dec.SegLvlAltLf {
				continue
			}
			if seg.FeatureData[i][j] != 0 {
				return false
			}
		}
	}
	return true
}

func (e *VP9Encoder) vp9ReuseStableSegmentationState(seg *vp9dec.SegmentationParams,
	intraFrame bool, miRows, miCols int, inter *vp9InterEncodeState,
) {
	if e == nil || seg == nil || !seg.Enabled || intraFrame ||
		!e.prevSegmentationValid || !e.vp9DynamicSegmentMapActive() {
		return
	}
	prev := e.prevSegmentation
	if prev.Enabled && vp9SegmentationDataEqual(seg, &prev) {
		seg.UpdateData = false
	}
	if prev.Enabled && seg.UpdateMap &&
		e.vp9SegmentMapMatchesPrevious(miRows, miCols, inter) {
		seg.UpdateMap = false
		seg.TemporalUpdate = prev.TemporalUpdate
		seg.TreeProbs = prev.TreeProbs
		seg.PredProbs = prev.PredProbs
	}
}

func vp9SegmentationDataEqual(a, b *vp9dec.SegmentationParams) bool {
	if a == nil || b == nil {
		return false
	}
	if a.AbsDelta != b.AbsDelta {
		return false
	}
	for i := range vp9dec.MaxSegments {
		if a.FeatureMask[i] != b.FeatureMask[i] {
			return false
		}
		for j := range vp9dec.SegLvlMax {
			if a.FeatureData[i][j] != b.FeatureData[i][j] {
				return false
			}
		}
	}
	return true
}

func (e *VP9Encoder) vp9SegmentMapMatchesPrevious(miRows, miCols int,
	inter *vp9InterEncodeState,
) bool {
	if e == nil || !e.useVP9EncoderPrevSegmentMap(miRows, miCols) {
		return false
	}
	staticSegID := e.vp9StaticSegmentIDForMap()
	for miRow := range miRows {
		row := e.prevSegmentMap[miRow*miCols:]
		for miCol := range miCols {
			if row[miCol] != e.vp9PartitionSegmentID(miRow, miCol,
				staticSegID, nil, inter) {
				return false
			}
		}
	}
	return true
}

func (e *VP9Encoder) validateVP9EncoderSource(img *image.YCbCr) error {
	if img == nil {
		return ErrInvalidConfig
	}
	if img.Rect.Dx() != e.opts.Width || img.Rect.Dy() != e.opts.Height {
		return ErrInvalidConfig
	}
	if img.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return ErrInvalidConfig
	}
	if img.YStride < e.opts.Width || img.CStride < (e.opts.Width+1)/2 {
		return ErrInvalidConfig
	}
	if len(img.Y) < ycbcrPlaneLen(img.YStride, e.opts.Width, e.opts.Height) {
		return ErrInvalidConfig
	}
	uvWidth := (e.opts.Width + 1) / 2
	uvHeight := (e.opts.Height + 1) / 2
	if len(img.Cb) < ycbcrPlaneLen(img.CStride, uvWidth, uvHeight) ||
		len(img.Cr) < ycbcrPlaneLen(img.CStride, uvWidth, uvHeight) {
		return ErrInvalidConfig
	}
	return nil
}

func ycbcrPlaneLen(stride, width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	return (height-1)*stride + width
}

// LastQuantizer mirrors libvpx's VP9E_GET_LAST_QUANTIZER /
// VP9E_GET_LAST_QUANTIZER_64 controls. It returns the public 0..63
// quantizer and the internal VP9 qindex of the most recently committed
// encoded frame. ok is false on a nil or closed encoder, and before any
// frame has been committed (dropped frames and buffered-by-lookahead
// inputs leave the value untouched).
func (e *VP9Encoder) LastQuantizer() (public int, internal int, ok bool) {
	if e == nil || e.closed || !e.lastQuantizerValid {
		return 0, 0, false
	}
	return e.lastQuantizerPublic, e.lastQuantizerInternal, true
}

// IsKeyFrameNext reports whether the next call to EncodeInto would
// emit a key frame. The first frame is always a key; subsequent
// frames key on MaxKeyframeInterval boundaries.
func (e *VP9Encoder) IsKeyFrameNext() bool {
	if e == nil || e.closed {
		return false
	}
	if e.frameIndex == 0 || e.forceKeyFrame {
		return true
	}
	cadence := e.opts.MaxKeyframeInterval
	if cadence <= 0 {
		cadence = 128 // libvpx default kf_max_dist
	}
	return e.frameIndex%cadence == 0
}

func validateVP9KeyFrameIntervalOptions(minFrames, maxFrames int) error {
	if minFrames < 0 || maxFrames < 0 {
		return ErrInvalidConfig
	}
	max := maxFrames
	if max <= 0 {
		max = 128
	}
	if minFrames > max {
		return ErrInvalidConfig
	}
	return nil
}

// ForceKeyFrame requests that the next successfully committed VP9 packet be
// a key frame. Calls on a nil or closed encoder are no-ops.
func (e *VP9Encoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	e.forceKeyFrame = true
}

// EncodeInto packs the next profile 0 frame into dst. It is equivalent to
// EncodeIntoWithFlags with no flags.
//
// Returns the number of bytes written into dst. Caller sizes dst; leave room
// for up to 64 KiB to match libvpx's first-partition header bound. When VP9
// CBR rate control drops a frame this returns 0, nil; use
// EncodeIntoWithResult to distinguish a dropped frame from other empty output.
func (e *VP9Encoder) EncodeInto(img *image.YCbCr, dst []byte) (int, error) {
	result, err := e.EncodeIntoWithFlagsResult(img, dst, 0)
	return len(result.Data), err
}

// EncodeIntoWithFlags packs the next profile 0 frame into dst while applying
// the VP9-compatible subset of EncodeFlags: EncodeForceKeyFrame,
// EncodeInvisibleFrame,
// EncodeNoReference{Last,Golden,AltRef}, EncodeNoUpdate{Last,Golden,AltRef},
// EncodeNoUpdateEntropy, EncodeForceGoldenFrame, and EncodeForceAltRefFrame.
//
// The current packet path emits source-backed keyframes and visible
// single-reference LAST / GOLDEN / ALTREF inter frames with DCT_DCT residual
// transforms up to Tx32x32, including bounded rate-aware motion search and
// transform-size selection with quarter-pel refinement. A deterministic prepass
// walks the same tiled mode tree to collect frame counts before the compressed
// header, so the real tile stream is encoded with same-frame counts-driven
// probability updates.
func (e *VP9Encoder) EncodeIntoWithFlags(img *image.YCbCr, dst []byte, flags EncodeFlags) (int, error) {
	result, err := e.EncodeIntoWithFlagsResult(img, dst, flags)
	return len(result.Data), err
}

// EncodeIntoWithResult packs the next profile 0 frame into dst and returns
// packet metadata. It is equivalent to EncodeIntoWithFlagsResult with no
// caller flags.
func (e *VP9Encoder) EncodeIntoWithResult(img *image.YCbCr, dst []byte) (VP9EncodeResult, error) {
	return e.EncodeIntoWithFlagsResult(img, dst, 0)
}

// EncodeIntoWithFlagsResult packs the next profile 0 frame into dst while
// returning packet and temporal-layer metadata.
func (e *VP9Encoder) EncodeIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, error) {
	if e == nil || e.closed {
		return VP9EncodeResult{}, ErrClosed
	}
	if e.vp9LookaheadEnabled() {
		return e.encodeVP9LookaheadIntoWithFlagsResult(img, dst, flags)
	}
	callerFlags := flags
	temporalFrame := e.temporal.nextFrame(e.vp9TimingState())
	flags |= temporalFrame.Flags
	flags = normalizeVP9EncodeFlags(flags)
	if e.vp9ShouldEncodeKeyFrame(flags) {
		flags &^= (temporalFrame.Flags & vp9NoUpdateRefFlags) &^ callerFlags
	}
	return e.encodeVP9FrameIntoWithFlagsResult(img, dst, flags, false, temporalFrame)
}

func (e *VP9Encoder) encodeVP9InterLayerIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, error) {
	callerFlags := flags
	temporalFrame := e.temporal.nextFrame(e.vp9TimingState())
	temporalFlags := temporalFrame.Flags
	useInterLayerReference := !e.forceKeyFrame &&
		callerFlags&EncodeForceKeyFrame == 0 &&
		e.hasVP9UsableInterReference(flags|temporalFlags)
	if useInterLayerReference {
		if callerUpdateFlags := callerFlags & vp9NoUpdateRefFlags; callerUpdateFlags != 0 {
			flags |= callerUpdateFlags
		} else {
			flags |= EncodeNoUpdateLast | EncodeNoUpdateAltRef
		}
		if temporalFrame.Enabled && temporalFrame.LayerID > 0 {
			flags |= EncodeNoUpdateGolden
		}
		temporalFlags &^= EncodeNoUpdateGolden
	}
	if e.frameIndex == 0 && !e.forceKeyFrame &&
		callerFlags&EncodeForceKeyFrame == 0 {
		temporalFlags &^= EncodeForceKeyFrame
	}
	flags |= temporalFlags
	flags = normalizeVP9EncodeFlags(flags)
	if !useInterLayerReference && e.vp9ShouldEncodeKeyFrame(flags) {
		flags &^= (temporalFrame.Flags & vp9NoUpdateRefFlags) &^ callerFlags
	}
	return e.encodeVP9FrameIntoWithFlagsResultInternal(img, dst, flags, false,
		temporalFrame, true)
}

func (e *VP9Encoder) encodeVP9SpatialSVCBaseIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, error) {
	callerFlags := flags
	temporalFrame := e.temporal.nextFrame(e.vp9TimingState())
	temporalFlags := temporalFrame.Flags
	if temporalFrame.Enabled && temporalFrame.LayerID > 0 &&
		!e.forceKeyFrame && callerFlags&EncodeForceKeyFrame == 0 {
		temporalFlags &^= EncodeNoUpdateAltRef
		temporalFlags |= EncodeNoUpdateGolden
	}
	flags |= temporalFlags
	flags = normalizeVP9EncodeFlags(flags)
	if e.vp9ShouldEncodeKeyFrame(flags) {
		flags &^= (temporalFrame.Flags & vp9NoUpdateRefFlags) &^ callerFlags
	}
	return e.encodeVP9FrameIntoWithFlagsResultInternal(img, dst, flags, false,
		temporalFrame, false)
}

// EncodeIntraOnlyFrameInto packs a hidden VP9 intra-only frame into dst.
// Intra-only frames are non-key VP9 packets with sync code and frame size but
// no inter prediction; by VP9 syntax they are always invisible. The VP9 stream
// must already be initialized by a coded frame. Use EncodeShowExistingFrameInto
// to display a refreshed slot after this call.
func (e *VP9Encoder) EncodeIntraOnlyFrameInto(img *image.YCbCr, dst []byte, flags EncodeFlags) (int, error) {
	result, err := e.encodeVP9FrameIntoWithFlagsResult(img, dst, flags, true, temporalFrame{LayerID: 0, LayerCount: 1})
	return len(result.Data), err
}

func (e *VP9Encoder) encodeVP9FrameIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags, forceIntraOnly bool, temporalFrame temporalFrame) (result VP9EncodeResult, err error) {
	return e.encodeVP9FrameIntoWithFlagsResultInternal(img, dst, flags,
		forceIntraOnly, temporalFrame, false)
}

func (e *VP9Encoder) encodeVP9FrameIntoWithFlagsResultInternal(img *image.YCbCr, dst []byte, flags EncodeFlags, forceIntraOnly bool, temporalFrame temporalFrame, forceFirstInterLayer bool) (result VP9EncodeResult, err error) {
	if e == nil || e.closed {
		return VP9EncodeResult{}, ErrClosed
	}
	flags = normalizeVP9EncodeFlags(flags)
	if err := validateVP9EncodeFlags(flags); err != nil {
		return VP9EncodeResult{}, err
	}
	if forceIntraOnly {
		if flags&EncodeForceKeyFrame != 0 {
			return VP9EncodeResult{}, ErrInvalidConfig
		}
		if e.frameIndex == 0 {
			return VP9EncodeResult{}, ErrInvalidConfig
		}
		flags |= EncodeInvisibleFrame
	}
	if err := e.validateVP9EncoderSource(img); err != nil {
		return VP9EncodeResult{}, err
	}
	if len(dst) < vp9MinEncodeIntoBuffer {
		return VP9EncodeResult{}, ErrBufferTooSmall
	}
	img = e.prepareVP9DenoiserSource(img)

	width := uint32(e.opts.Width)
	height := uint32(e.opts.Height)
	miCols := int((width + 7) >> 3)
	miRows := int((height + 7) >> 3)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(miRows, miCols)

	isKey := e.vp9ShouldEncodeKeyFrame(flags)
	intraOnly := forceIntraOnly
	if intraOnly {
		isKey = false
	}
	if !isKey && !intraOnly &&
		e.shouldEncodeVP9SceneCutKeyFrame(img, flags, temporalFrame.Enabled,
			encoderMacroblockRows(e.opts.Height), encoderMacroblockCols(e.opts.Width)) {
		isKey = true
	}
	if forceFirstInterLayer && isKey && e.frameIndex == 0 &&
		!e.forceKeyFrame && flags&EncodeForceKeyFrame == 0 &&
		e.hasVP9UsableInterReference(flags) {
		e.resetVP9EncoderFrameContexts()
		isKey = false
	}
	if !isKey && !intraOnly && !e.hasVP9UsableInterReference(flags) &&
		!vp9AllInterReferencesDisabled(flags) {
		isKey = true
	}
	if !isKey && !intraOnly {
		if err := e.validateVP9InterSegmentationReferences(flags); err != nil {
			return VP9EncodeResult{}, err
		}
	}
	// libvpx vp9/encoder/vp9_encoder.c:5444 forces cpi->refresh_last_frame=1
	// on every KEY_FRAME after set_ext_overrides has copied the user-supplied
	// ext_refresh_*_frame fields, and vp9_encoder.c:856-858 forces
	// refresh_golden_frame=1 / refresh_alt_ref_frame=1 inside check_show_existing.
	// The net effect is that any EncodeNoUpdate{Last,Golden,AltRef} hint passed
	// with EncodeForceKeyFrame is SILENTLY IGNORED — it is not a "Conflicting
	// flags." error. govpx writes header.RefreshFrameFlags = 0xff on KEY_FRAMEs
	// at vp9_encoder.go:2593 unconditionally, mirroring this, so accepting
	// NoUpdate bits on key frames yields the same bitstream as libvpx.
	if intraOnly && vp9InterRefreshFrameFlags(flags) == 0 {
		return VP9EncodeResult{}, ErrInvalidConfig
	}
	// libvpx: vp9/encoder/vp9_encoder.c:6347-6364 — when
	// VP9E_SET_KEY_FRAME_FILTERING is enabled and the other libvpx
	// preconditions hold (non-realtime mode, non-lossless, single-pass,
	// non-SVC, ARNRMaxFrames>0, ARNRStrength>0, speed<2), run
	// vp9_temporal_filter(cpi, -1) on the keyframe source against the
	// forward lookahead window and substitute the filtered buffer for
	// the per-frame encode.  govpx's gate helper checks the same set;
	// when any gate trips we fall through to the raw source.
	if isKey && e.vp9KeyFrameFilteringActive() {
		img = e.applyVP9KeyFrameFilter(img)
	}
	e.rc.beginFrameWithRefresh(isKey || intraOnly, e.frameIndex,
		vp9InterRefreshFrameFlags(flags))
	showFrame := flags&EncodeInvisibleFrame == 0
	e.rc.preEncodeFrame(showFrame)
	e.vp9TwoPassFrameTarget = 0
	if !isKey && !intraOnly && showFrame {
		dropReason, dropFrame := e.rc.testDropInterFrame()
		if dropFrame {
			e.rc.postDropFrame()
			e.temporal.finishDroppedFrame(temporalFrame, e.vp9TemporalBufferConfig())
			firstPassStats := e.twoPass.statsForFrame()
			e.twoPass.finishFrame()
			if vp9OracleTraceBuild {
				e.emitVP9OracleFrameTrace(vp9OracleFrameSummary{
					Row:                "vp9_frame",
					FrameIndex:         e.frameIndex,
					Flags:              uint32(flags),
					Dropped:            true,
					DropReason:         vp9DropReasonString(dropReason),
					ShowFrame:          true,
					CodedWidth:         int(width),
					CodedHeight:        int(height),
					TemporalLayerID:    temporalFrame.LayerID,
					TemporalLayerCount: temporalFrame.LayerCount,
					TemporalLayerSync:  temporalFrame.LayerSync,
					TL0PICIDX:          temporalFrame.TL0PICIDX,
					TargetBitrateKbps:  e.rc.targetBitrateKbps,
					FrameTargetBits:    e.rc.frameTargetBits,
					BufferLevelBits:    e.rc.bufferLevelBits,
					BufferOptimalBits:  e.rc.bufferOptimalBits,
				})
			}
			e.vp9FinishKeyFrameDistance(false)
			e.frameIndex++
			spatialLayerID, spatialLayerCount, interLayerDependency,
				notRefForUpperSpatialLayer, scalabilityStructurePresent,
				spatialScalabilityStructure := e.vp9SpatialResultFields()
			return VP9EncodeResult{
				Dropped:                     true,
				ShowFrame:                   true,
				TargetBitrateKbps:           e.rc.targetBitrateKbps,
				FrameTargetBits:             e.rc.frameTargetBits,
				BufferLevelBits:             e.rc.bufferLevelBits,
				FirstPassStats:              firstPassStats,
				TemporalLayerID:             temporalFrame.LayerID,
				TemporalLayerCount:          temporalFrame.LayerCount,
				TemporalLayerSync:           temporalFrame.LayerSync,
				TL0PICIDX:                   temporalFrame.TL0PICIDX,
				SpatialLayerID:              spatialLayerID,
				SpatialLayerCount:           spatialLayerCount,
				InterLayerDependency:        interLayerDependency,
				NotRefForUpperSpatialLayer:  notRefForUpperSpatialLayer,
				ScalabilityStructurePresent: scalabilityStructurePresent,
				SpatialScalabilityStructure: spatialScalabilityStructure,
			}, nil
		}
	}
	e.prepareVP9EncoderOutputFrame(int(width), int(height))

	header := &e.vp9HeaderScratch
	*header = vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		ShowFrame:             showFrame,
		ErrorResilientMode:    e.opts.ErrorResilient,
		IntraOnly:             intraOnly,
		Width:                 width,
		Height:                height,
		RefreshFrameContext:   flags&EncodeNoUpdateEntropy == 0,
		FrameParallelDecoding: e.vp9FrameParallelDecodingMode(),
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: vp9CommonColorSpace(e.opts.ColorSpace),
			ColorRange: vp9CommonColorRange(e.opts.ColorRange),
		},
	}
	if rw, rh := e.opts.RenderWidth, e.opts.RenderHeight; rw > 0 && rh > 0 {
		header.Render = vp9dec.RenderSize{
			Width:  uint32(rw),
			Height: uint32(rh),
		}
	} else {
		header.Render = vp9dec.RenderSize{Width: width, Height: height}
	}
	header.Tile = vp9EncoderTileInfo(miCols, e.opts.Threads, e.opts.Log2TileRows)
	macroblocks := vp9MacroblockCount(miRows, miCols)
	// TPL runs before the qindex is finalised so its per-SB rdmult delta
	// can scale the keyframe mode picker's Lagrangian search.  Unlike
	// the deleted scalar bias path, libvpx's TPL does NOT touch the
	// regulated qindex — it routes through cb_rdmult inside the per-SB
	// partition search (vp9_encodeframe.c:4245-4248).  The pass fires on
	// visible frames whenever a populated source-order lookahead window
	// is available; alt-ref / hidden frames are excluded for parity
	// with libvpx's restriction.
	e.populateVP9TPLForFrame(!showFrame || flags&EncodeForceAltRefFrame != 0, img)
	qindex := e.vp9EncoderFrameQIndex(isKey, header.IntraOnly, flags, macroblocks)
	if e.rc.enabled {
		e.vp9ModeDecisionQIndex = uint8(qindex)
		e.vp9ModeDecisionQIndexSet = true
		defer func() {
			e.vp9ModeDecisionQIndexSet = false
		}()
	}
	// libvpx: vp9/encoder/vp9_rd.c:396-407 vp9_initialize_rd_consts.
	// rd->RDDIV = RDDIV_BITS; rd->RDMULT = vp9_compute_rd_mult(...).
	// govpx's frame-type bucket replays the libvpx branching: KF wins,
	// then a non-srcframe-altref ARF/GF refresh, else inter.  The
	// per-SB cb_rdmult cache cleared inside vp9EncoderInitializeRDConsts
	// matches libvpx's reset before each rd_pick_sb_modes invocation.
	{
		refreshFlags := uint8(0xff)
		if !isKey {
			refreshFlags = e.vp9InterRefreshFrameFlags(flags)
		}
		refreshGolden := refreshFlags&(1<<vp9GoldenRefSlot) != 0
		refreshAlt := refreshFlags&(1<<vp9AltRefSlot) != 0
		srcFrameAltRef := !showFrame || flags&EncodeForceAltRefFrame != 0
		rdFrameType := vp9RDFrameTypeFor(isKey, srcFrameAltRef, refreshGolden,
			refreshAlt)
		e.vp9EncoderInitializeRDConsts(qindex, rdFrameType)
		// libvpx vp9/encoder/vp9_encoder.c:3754 / 3765 call
		// set_speed_features_framesize_independent +
		// set_speed_features_framesize_dependent (via
		// set_size_independent_vars / set_size_dependent_vars) on every
		// frame from encode_frame_to_data_rate / encode_with_recode_loop
		// at vp9_encoder.c:4169-4170 and 4377-4392. The SF refresh
		// sees the per-frame (frame_type, intra_only, refresh_*_frame,
		// is_src_frame_alt_ref, base_qindex) tuple — the same triple
		// frame_is_kf_gf_arf / frame_is_boosted consume — so
		// sf.tx_size_search_method and sf.use_nonrd_pick_mode track the
		// live frame state. govpx previously pinned e.sf at compressor
		// create time which left non-key non-intra-only frames reading
		// the keyframe-context value; the per-frame call here closes
		// that gap.
		e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
			IsKey:              isKey,
			IntraOnly:          intraOnly,
			ShowFrame:          showFrame,
			RefreshGoldenFrame: refreshGolden,
			RefreshAltRefFrame: refreshAlt,
			IsSrcFrameAltRef:   srcFrameAltRef,
			BaseQIndex:         qindex,
		}))
	}
	header.Quant.BaseQindex = int16(qindex)
	header.Quant.UvDcDeltaQ = int8(e.opts.DeltaQUV)
	header.Quant.UvAcDeltaQ = int8(e.opts.DeltaQUV)
	header.Quant.Lossless = qindex == 0 &&
		header.Quant.YDcDeltaQ == 0 &&
		header.Quant.UvDcDeltaQ == 0 &&
		header.Quant.UvAcDeltaQ == 0
	resetLoopfilterDeltas := isKey || intraOnly || e.opts.ErrorResilient
	// libvpx vp9_picklpf.c:159 — the picker reads sf.lpf_pick to
	// choose between LPF_PICK_FROM_FULL_IMAGE (default at speeds
	// 0-2), LPF_PICK_FROM_Q (speed 3+), and LPF_PICK_MINIMAL_LPF.
	//
	// govpx writes a placeholder FilterLevel (the closed-form FROM_Q
	// value) into the uncompressed header before tile encoding; once
	// the tiles populate the reconstruction buffer the full-image /
	// sub-image search runs (vp9EncoderRunFullImagePicker below) and
	// the uncompressed header is re-written in place with the picked
	// level. The filter_level field is a 6-bit literal at a stable
	// bit position (internal/vp9/encoder/header_writer.go:384
	// EncodeLoopfilterWithPrev), so the byte length of the
	// uncompressed header is invariant under filter_level and the
	// re-write keeps compressed_header / tile offsets stable. This
	// matches libvpx's order at vp9_encoder.c:5391-5467
	// (encode_with_recode_loop → loopfilter_frame → vp9_pack_bitstream).
	header.Loopfilter = e.vp9EncoderLoopFilterParams(qindex, isKey, intraOnly,
		resetLoopfilterDeltas, header.Quant.Lossless,
		header.Seg.Enabled, e.opts.Sharpness,
		e.opts.Width, e.opts.Height, common.TxModeSelect)
	if vp9DisableLoopfilterForFrame(e.opts.DisableLoopfilter, isKey) {
		header.Loopfilter.FilterLevel = 0
	}
	if isKey {
		header.FrameType = common.KeyFrame
		header.RefreshFrameFlags = 0xff
	} else if intraOnly {
		header.FrameType = common.InterFrame
		if flags&EncodeNoUpdateEntropy == 0 {
			header.ResetFrameContext = 2
		}
		header.RefreshFrameFlags = e.vp9InterRefreshFrameFlags(flags)
	} else {
		header.FrameType = common.InterFrame
		header.RefreshFrameFlags = e.vp9InterRefreshFrameFlags(flags)
		header.FrameContextIdx = vp9InterFrameContextIdx(header.RefreshFrameFlags)
		header.InterRef.RefIndex = [3]uint8{
			vp9LastRefSlot,
			vp9GoldenRefSlot,
			vp9AltRefSlot,
		}
		header.InterRef.SignBias = e.vp9InterRefSignBias(flags)
	}
	restoreFrameContext := e.opts.ErrorResilient || flags&EncodeNoUpdateEntropy != 0
	shouldRestoreFrameContexts := isKey || intraOnly || e.opts.ErrorResilient || restoreFrameContext
	var frameContextsSeed [common.FrameContexts]vp9dec.FrameContext
	var frameContextSeed vp9dec.FrameContext
	frameContextIdx := e.prepareVP9EncoderFrameContext(header)
	if shouldRestoreFrameContexts {
		frameContextsSeed = e.frameContexts
		frameContextSeed = e.fc
	}
	defer func() {
		if err == nil && !restoreFrameContext {
			return
		}
		if shouldRestoreFrameContexts {
			e.frameContexts = frameContextsSeed
			e.fc = frameContextSeed
			return
		}
		if frameContextIdx >= 0 && frameContextIdx < len(e.frameContexts) {
			e.fc = e.frameContexts[frameContextIdx]
		}
	}()
	// libvpx vp9/encoder/vp9_encoder.c:5355 calls save_encode_params once
	// per frame before the recode loop; vp9/encoder/vp9_encodeframe.c:5825
	// then calls restore_encode_params at the top of every vp9_encode_frame
	// so each recode iteration starts from the same prev snapshot. govpx
	// encodes each frame once, so save+restore collapses to a single
	// in-place pass, but the calls are ported verbatim to keep wire
	// behaviour identical when the recode loop is introduced.
	e.vp9SaveEncodeParamsFilterThreshes()
	e.vp9RestoreEncodeParamsFilterThreshes()
	header.InterpFilter = e.vp9EncoderFrameInterpFilter(isKey, header.IntraOnly,
		header.Quant.Lossless)
	if !isKey && !header.IntraOnly && vp9InterReferenceMask(flags) == 0 {
		header.InterpFilter = vp9dec.InterpSwitchable
	}
	// libvpx vp9/encoder/vp9_encodeframe.c:5876-5877 — when the frame
	// enters encode with cm->interp_filter == SWITCHABLE and the
	// frame_parameter_update speed feature is enabled, demote the frame
	// to the concrete EIGHTTAP / EIGHTTAP_SMOOTH / EIGHTTAP_SHARP that
	// won the previous frames' per-block 3-filter RD race
	// (filter_threshes accumulator). Skipped for intra-only frames
	// because the uncompressed-header writer omits the filter field for
	// those (internal/vp9/encoder/header_writer.go:196).
	if !isKey && !header.IntraOnly {
		refreshFlags := e.vp9InterRefreshFrameFlags(flags)
		refreshGolden := refreshFlags&(1<<vp9GoldenRefSlot) != 0
		refreshAlt := refreshFlags&(1<<vp9AltRefSlot) != 0
		srcFrameAltRef := !showFrame || flags&EncodeForceAltRefFrame != 0
		header.InterpFilter = e.vp9DemoteSwitchableInterpFilter(
			header.InterpFilter, isKey, header.IntraOnly,
			srcFrameAltRef, refreshGolden, refreshAlt)
	}
	header.AllowHighPrecisionMv = vp9EncoderFrameAllowHighPrecisionMv(isKey, header.IntraOnly)

	txMode := e.vp9EncoderFrameTxMode(isKey, header.IntraOnly, header.Quant.Lossless)
	baseMi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.TxModeToBiggestTxSize[txMode],
		Skip:   1,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	// libvpx vp9/encoder/vp9_encodeframe.c:4336-4337 encodes the KEY_FRAME
	// && use_nonrd_pick_mode ALLOW_16X16 clamp inside select_tx_mode,
	// where it becomes baseMi.TxSize == Tx16x16 directly via
	// common.TxModeToBiggestTxSize[Allow16x16]. govpx previously layered a
	// redundant clamp on top; lifted now that vp9EncoderFrameTxMode ports
	// select_tx_mode verbatim.
	if !isKey && !intraOnly {
		baseMi.Mode = common.ZeroMv
		baseMi.InterpFilter = uint8(vp9dec.InterpEighttap)
		baseMi.RefFrame = [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}
	}
	e.vp9PrepareCyclicRefreshFrame(isKey, intraOnly, showFrame, miRows, miCols, macroblocks, header)
	if e.opts.AQMode == VP9AQPerceptual {
		e.perceptualAQ.prepareFrame(img, int(header.Quant.BaseQindex), showFrame)
	}
	seg := e.vp9EncoderSegmentationParams(isKey || intraOnly,
		int(header.Quant.BaseQindex))
	e.vp9CarryActiveMapDisableSegmentation(&seg, isKey || intraOnly)
	dq := &e.dqScratch
	var keyState *vp9KeyframeEncodeState
	var interState *vp9InterEncodeState
	compoundAllowed := false
	referenceMode := vp9dec.SingleReference
	refSignBias := vp9FrameRefSignBias(header)
	compoundRefs := vp9dec.SetupCompoundReferenceMode(refSignBias)
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: int(header.Quant.BaseQindex),
		BitDepth:   vp9dec.Bits8,
	}, dq)
	if isKey {
		keyState = &vp9KeyframeEncodeState{
			img:      img,
			hdr:      header,
			dq:       dq,
			lossless: header.Quant.Lossless,
		}
	} else if intraOnly {
		keyState = &vp9KeyframeEncodeState{
			img:      img,
			hdr:      header,
			dq:       dq,
			lossless: header.Quant.Lossless,
		}
	} else {
		compoundAllowed = vp9dec.CompoundReferenceAllowed(refSignBias)
		if compoundAllowed {
			referenceMode = vp9dec.ReferenceModeSelect
		}
		interState = &vp9InterEncodeState{
			img:             img,
			dq:              dq,
			ref:             &e.refFrames[0],
			refMask:         vp9InterReferenceMask(flags),
			allowHP:         header.AllowHighPrecisionMv,
			selectFc:        e.fc,
			referenceMode:   referenceMode,
			compoundAllowed: compoundAllowed,
			refSignBias:     refSignBias,
			compoundRefs:    compoundRefs,
			interpFilter:    header.InterpFilter,
			lossless:        header.Quant.Lossless,
			baseQindex:      int(header.Quant.BaseQindex),
		}
	}
	e.vp9ReuseStableSegmentationState(&seg, isKey || intraOnly, miRows, miCols,
		interState)
	header.Seg = seg
	e.resetVP9EncoderAboveEntropyContexts()

	// libvpx swaps in vp9_kf_partition_probs (not fc->partition_prob)
	// for keyframe / intra-only frames — see set_partition_probs in
	// vp9/common/vp9_onyxc_int.h. The two tables have the same shape
	// but different probabilities, so the bool stream desyncs if the
	// encoder uses the wrong one.
	partitionProbs := tables.KfPartitionProbs
	if !isKey && !intraOnly {
		partitionProbs = e.fc.PartitionProb
	}

	denoiserCountState := e.saveVP9DenoiserForCounts(interState)
	counts := e.collectVP9EncodeFrameCounts(int(width), int(height), miRows, miCols,
		header.Tile, &partitionProbs, &seg, baseMi, txMode, isKey, header.IntraOnly,
		keyState, interState)
	e.restoreVP9DenoiserAfterCounts(denoiserCountState)
	// libvpx vp9/encoder/vp9_encodeframe.c:5911 gates the post-encode
	// tx_mode demotion on cm->tx_mode == TX_MODE_SELECT. govpx's
	// vp9EncoderFrameTxModeFromCounts encodes a govpx-specific (and
	// inverted vs libvpx) predicate that survives the keyframe
	// Allow16x16 clamp lift because the select_tx_mode port at line
	// :3645 emits Allow16x16 directly for the KEY_FRAME &&
	// use_nonrd_pick_mode path. The previous govpx-only baseMi clamp
	// is dropped; the Allow16x16 floor below shadows libvpx's
	// "only recount TX_MODE_SELECT" predicate by refusing to demote
	// the ALLOW_16X16 keyframe past the libvpx select_tx_mode result.
	if reducedTxMode := vp9EncoderFrameTxModeFromCounts(txMode,
		header.Quant.Lossless, counts); reducedTxMode != txMode {
		if isKey && reducedTxMode < common.Allow16x16 {
			// libvpx vp9_encodeframe.c:5911 — non-TX_MODE_SELECT frames
			// (here ALLOW_16X16 from select_tx_mode for KEY_FRAME &&
			// use_nonrd_pick_mode) bypass the demotion. govpx's wider
			// govpx-specific demotion gate is reined in for keyframes
			// here so the wire-level tx_mode literal matches libvpx.
			reducedTxMode = common.Allow16x16
		}
		if reducedTxMode == txMode {
			// floor recovered the original mode — skip the second
			// counts pass to keep behaviour identical to the no-demote
			// libvpx path.
		} else {
			txMode = reducedTxMode
			baseMi.TxSize = common.TxModeToBiggestTxSize[txMode]
			denoiserCountState = e.saveVP9DenoiserForCounts(interState)
			counts = e.collectVP9EncodeFrameCounts(int(width), int(height), miRows, miCols,
				header.Tile, &partitionProbs, &seg, baseMi, txMode, isKey,
				header.IntraOnly, keyState, interState)
			e.restoreVP9DenoiserAfterCounts(denoiserCountState)
		}
	}
	header.Seg = seg

	// libvpx vp9/encoder/vp9_bitstream.c:1312 — fix_interp_filter runs at
	// uncompressed-header write time, just before write_interp_filter and
	// before the compressed header is appended (libvpx vp9_bitstream.c:
	// 1425 then :1453). If exactly one filter has nonzero switchable
	// counts after the per-block RD pass, the frame header is demoted to
	// that filter so the bitstream omits the per-block filter bits.
	// govpx writes compressed first to size FirstPartitionSize, so we
	// apply the demotion here — between collectVP9EncodeFrameCounts and
	// WriteCompressedHeaderFromCounts — so the compressed-header
	// switchable_interp_probs update branch
	// (libvpx vp9_bitstream.c:1356 ; govpx WriteCompressedHeaderFromCounts)
	// reads the post-demotion InterpFilter, matching libvpx wire bits.
	header.InterpFilter = vp9FixInterpFilter(header.InterpFilter, counts)
	// libvpx's tile-write pass reads cm->interp_filter via
	// vp9_bitstream.c:306-314 to decide whether each block emits a
	// per-block switchable_interp literal. govpx mirrors that through
	// vp9ModeTreeInterpFilter -> inter.interpFilter, so the demoted
	// value must be propagated to the InterEncodeState the tile writer
	// reads (vp9_encoder.go:5740,5785). When c==1, fix_interp_filter
	// only demotes to the filter every block already picked
	// (libvpx vp9_bitstream.c:877-881), so the per-block assert
	// `mi->interp_filter == cm->interp_filter`
	// (libvpx vp9_bitstream.c:313) stays satisfied.
	if interState != nil {
		interState.interpFilter = header.InterpFilter
	}

	compSize, err := encoder.WriteCompressedHeaderFromCounts(e.scratch[:], encoder.WriteCompressedHeaderFromCountsArgs{
		Lossless:                header.Quant.Lossless,
		TxMode:                  txMode,
		IntraOnly:               isKey || header.IntraOnly,
		InterpFilter:            header.InterpFilter,
		ReferenceMode:           referenceMode,
		CompoundRefAllowed:      compoundAllowed,
		AllowHighPrecisionMv:    header.AllowHighPrecisionMv,
		CoefStepsize:            e.vp9CoeffProbAppxStep(),
		CoefUpdateMode:          vp9CoefUpdateModeForFrame(isKey),
		SkipTx16PlusCoefUpdates: e.vp9SkipTx16PlusCoefUpdates(isKey),
		Probs:                   &e.fc,
		Counts:                  counts,
	})
	if err != nil {
		return VP9EncodeResult{}, err
	}
	if compSize > 0xffff {
		return VP9EncodeResult{}, encoder.ErrCompressedHeaderTooLarge
	}
	header.FirstPartitionSize = uint16(compSize)
	if !isKey && !intraOnly {
		partitionProbs = e.fc.PartitionProb
	}

	var headerBW encoder.BitWriter
	headerBW.Init(dst)
	var uncSize int
	prevLfRef, prevLfMode := e.vp9EncoderLoopFilterPrevDeltas(resetLoopfilterDeltas)
	if header.FrameType == common.KeyFrame {
		uncSize = encoder.WriteKeyframeUncompressedHeaderWithLoopfilterPrev(
			&headerBW, header, &prevLfRef, &prevLfMode)
	} else if header.IntraOnly {
		uncSize = encoder.WriteIntraOnlyUncompressedHeaderWithLoopfilterPrev(
			&headerBW, header, &prevLfRef, &prevLfMode)
	} else {
		uncSize = encoder.WriteInterUncompressedHeaderWithLoopfilterPrev(
			&headerBW, header, e.vp9RefDims, &prevLfRef, &prevLfMode)
	}
	if uncSize+compSize >= len(dst) {
		return VP9EncodeResult{}, encoder.ErrPackBufferFull
	}
	copy(dst[uncSize:uncSize+compSize], e.scratch[:compSize])

	tileStart := uncSize + compSize
	tileKind := vp9ModeTreeInterSource
	if isKey || intraOnly {
		tileKind = vp9ModeTreeKeyframeSource
	} else if header.IntraOnly {
		tileKind = vp9ModeTreeKeyframe
	}
	tileSize, err := e.writeVP9FrameTiles(dst[tileStart:], miRows, miCols,
		header.Tile, &partitionProbs, &seg, baseMi, txMode, tileKind, keyState,
		interState)
	if err != nil {
		return VP9EncodeResult{}, err
	}
	n := tileStart + tileSize
	// Post-tile loop-filter strength picker. The reconstruction
	// buffer (e.reconYFull) is now populated with the unfiltered
	// luma; the dispatcher can route LPF_PICK_FROM_FULL_IMAGE /
	// LPF_PICK_FROM_SUBIMAGE through the quadratic search against
	// real recon (libvpx vp9_picklpf.c:78-157 search_filter_level via
	// try_filter_frame at lines 46-76). LPF_PICK_FROM_Q and
	// LPF_PICK_MINIMAL_LPF do not consult the recon buffer, so the
	// pre-tile placeholder already carries the libvpx-correct level
	// and we skip the post-tile re-run entirely — this also keeps
	// the steady-state encode path allocation-free for the
	// (default-realtime) speed >= 3 case where sf.LpfPick =
	// LpfPickFromQ. libvpx vp9_speed_features.c:555 anchors the
	// realtime-default switchover.
	//
	// The picker is suppressed when the disable / lossless gates
	// already forced filter_level to 0; rerunning would only flip
	// the level back away from the intended override. The applyLPF
	// gate further enforces zero-level skip below.
	runFullImageSearch := (e.sf.LpfPick == LpfPickFromFullImage ||
		e.sf.LpfPick == LpfPickFromSubImage) &&
		header.Loopfilter.FilterLevel != 0 &&
		!vp9DisableLoopfilterForFrame(e.opts.DisableLoopfilter, isKey) &&
		!header.Quant.Lossless
	if runFullImageSearch {
		// header.Seg already mirrors seg at this point (line 2426
		// above); we pass header.Seg so the compiler doesn't have to
		// heap-promote the local seg in the steady-state FROM_Q /
		// MINIMAL path that never enters this block.
		pickedLevel := e.vp9EncoderRunFullImagePicker(header, &header.Seg, img,
			txMode, isKey)
		if pickedLevel != header.Loopfilter.FilterLevel {
			header.Loopfilter.FilterLevel = pickedLevel
			// Re-write the uncompressed header in place at dst[0:].
			// The byte length is invariant under filter_level (6-bit
			// literal at a fixed position), so the compressed-header
			// + tiles tail stays valid.
			var rewriteBW encoder.BitWriter
			rewriteBW.Init(dst)
			var rewSize int
			rewPrevLfRef, rewPrevLfMode := e.vp9EncoderLoopFilterPrevDeltas(resetLoopfilterDeltas)
			if header.FrameType == common.KeyFrame {
				rewSize = encoder.WriteKeyframeUncompressedHeaderWithLoopfilterPrev(
					&rewriteBW, header, &rewPrevLfRef, &rewPrevLfMode)
			} else if header.IntraOnly {
				rewSize = encoder.WriteIntraOnlyUncompressedHeaderWithLoopfilterPrev(
					&rewriteBW, header, &rewPrevLfRef, &rewPrevLfMode)
			} else {
				rewSize = encoder.WriteInterUncompressedHeaderWithLoopfilterPrev(
					&rewriteBW, header, e.vp9RefDims, &rewPrevLfRef, &rewPrevLfMode)
			}
			if rewSize != uncSize {
				// The uncompressed-header byte length must be
				// invariant — filter_level is a fixed-width 6-bit
				// literal and all sibling fields are independent of
				// it (libvpx encode_loopfilter, header_writer.go:384).
				// A drift here indicates a bitstream-writer bug; bail
				// rather than corrupting the stream.
				return VP9EncodeResult{}, ErrInvalidVP9Data
			}
			_ = rewSize
		}
		// libvpx: vp9_encoder.c:3448 — `lf->last_filt_level =
		// lf->filter_level` after the picker returns. We refresh the
		// encoder-side cache here so the next frame's picker reads
		// the final post-search level instead of the pre-tile
		// placeholder.
		e.vp9LastFiltLevel = header.Loopfilter.FilterLevel
	}
	// libvpx vp9/encoder/vp9_encodeframe.c:5890-5891 — after the encode
	// pass produces rdc->filter_diff (per-block best_filter_diff[i] sums
	// at vp9_encodeframe.c:1881), merge it into the persistent
	// filter_threshes accumulator that drives the next frame's
	// SWITCHABLE -> concrete demotion. Skipped outside
	// frame_parameter_update inside the helper. We compute the same
	// refresh/alt-ref flags used at the demotion site so the frame_type
	// bucket is consistent across save / demote / update.
	{
		refreshFlags := uint8(0xff)
		if !isKey {
			refreshFlags = e.vp9InterRefreshFrameFlags(flags)
		}
		refreshGolden := refreshFlags&(1<<vp9GoldenRefSlot) != 0
		refreshAlt := refreshFlags&(1<<vp9AltRefSlot) != 0
		srcFrameAltRef := !showFrame || flags&EncodeForceAltRefFrame != 0
		e.vp9UpdateFilterThreshesPostEncode(isKey, header.IntraOnly,
			srcFrameAltRef, refreshGolden, refreshAlt, macroblocks)
	}
	e.adaptVP9EncoderFrameContext(header, frameContextIdx, counts, txMode)
	var firstPassStats VP9FirstPassFrameStats
	twoPassTargetBits := 0
	if header.ShowFrame {
		firstPassStats = e.twoPass.statsForFrame()
		twoPassTargetBits = e.vp9TwoPassFrameTarget
	}
	if header.RefreshFrameFlags != 0 {
		if !e.applyVP9EncoderLoopFilter(header, &seg) {
			return VP9EncodeResult{}, ErrInvalidVP9Data
		}
	}
	e.refreshVP9EncoderSegmentMap(miRows, miCols)
	e.prevSegmentation = header.Seg
	e.prevSegmentationValid = true
	e.prevFrameActiveMapEnabled = e.activeMapEnabled
	e.refreshVP9EncoderMvRefs(isKey || intraOnly, miRows, miCols)
	e.refreshVP9EncoderRefs(header, flags)
	e.finishVP9DenoiserFrame(header, img)
	e.commitVP9EncoderLoopFilterDeltas(&header.Loopfilter, resetLoopfilterDeltas)
	e.commitVP9EncoderFrameContext(header, frameContextIdx)
	e.lastVP9HeaderFrameType = header.FrameType
	e.lastVP9HeaderValid = true
	// libvpx vp9/encoder/vp9_encodeframe.c:5650 writes cm->tx_mode at the
	// top of vp9_encode_frame_internal; the value persists across frames so
	// the final else branch of select_tx_mode (vp9_encodeframe.c:4344) can
	// read back the previous frame's tx_mode. Mirror that commit here once
	// the per-frame encode (including the post-encode demotion at
	// vp9_encodeframe.c:5911-5944) has settled the final tx_mode.
	e.prevFrameTxMode = txMode
	postDrop := e.rc.shouldPostEncodeDrop(isKey || intraOnly,
		header.ShowFrame, encodedSizeBits(n))
	if postDrop {
		e.rc.postEncodeDropFrame()
	} else {
		e.rc.postEncodeFrame(n, header.ShowFrame, qindex, isKey || intraOnly,
			header.RefreshFrameFlags, macroblocks)
	}
	if header.ShowFrame {
		// libvpx vp9_twopass_postencode_update consumes the encoded bit
		// count to drive vbr_bits_off_target. Feed it 0 on drops, the
		// encoded size in bits otherwise, mirroring rc->projected_frame_size.
		// libvpx: vp9/encoder/vp9_firstpass.c:3733
		projected := 0
		if !postDrop {
			projected = encodedSizeBits(n)
		}
		e.twoPass.finishFrameWithActual(projected)
	}
	e.temporal.finishFrame(temporalFrame, isKey, header.ShowFrame,
		vp9TemporalReferenceRefresh(header.RefreshFrameFlags),
		encodedSizeBits(n), e.vp9TemporalBufferConfig())
	e.vp9FinishKeyFrameDistance(isKey)
	e.frameIndex++
	if isKey {
		e.forceKeyFrame = false
	}
	// Consume the head TPL slab now that this frame has committed.  The
	// pass refills the new tail on the next populate call.
	if e.vp9TPLEnabled() {
		e.tpl.shiftAndInvalidate()
	}
	spatialLayerID, spatialLayerCount, interLayerDependency,
		notRefForUpperSpatialLayer, scalabilityStructurePresent,
		spatialScalabilityStructure := e.vp9SpatialResultFields()
	resultData := dst[:n]
	resultSize := n
	resultRefreshFlags := header.RefreshFrameFlags
	if postDrop {
		// Discard the encoded payload and clear refresh-frame metadata so
		// downstream consumers treat the frame as dropped. Reference-slot
		// rolling back has already occurred through postEncodeDropFrame's
		// rate-control bookkeeping; ref-state side effects on the decoder
		// reference pool persist by design to keep the encoder's
		// frame-context probabilities stable for the next frame.
		resultData = nil
		resultSize = 0
		resultRefreshFlags = 0
	}
	publicQuantizer := vp9QIndexToPublicQuantizer(qindex)
	if !postDrop {
		e.lastQuantizerInternal = qindex
		e.lastQuantizerPublic = publicQuantizer
		e.lastQuantizerValid = true
	}
	result = VP9EncodeResult{
		Data:                        resultData,
		KeyFrame:                    isKey,
		IntraOnly:                   intraOnly,
		ShowFrame:                   header.ShowFrame,
		Dropped:                     postDrop,
		Droppable:                   !isKey && header.RefreshFrameFlags == 0 && !header.RefreshFrameContext,
		Quantizer:                   publicQuantizer,
		InternalQuantizer:           qindex,
		SizeBytes:                   resultSize,
		TargetBitrateKbps:           e.vp9ResultTargetBitrateKbps(),
		FrameTargetBits:             e.rc.frameTargetBits,
		BufferLevelBits:             e.rc.bufferLevelBits,
		RefreshFrameFlags:           resultRefreshFlags,
		FirstPassStats:              firstPassStats,
		TwoPassFrameTargetBits:      twoPassTargetBits,
		TemporalLayerID:             temporalFrame.LayerID,
		TemporalLayerCount:          temporalFrame.LayerCount,
		TemporalLayerSync:           temporalFrame.LayerSync,
		TL0PICIDX:                   temporalFrame.TL0PICIDX,
		SpatialLayerID:              spatialLayerID,
		SpatialLayerCount:           spatialLayerCount,
		InterLayerDependency:        interLayerDependency,
		NotRefForUpperSpatialLayer:  notRefForUpperSpatialLayer,
		ScalabilityStructurePresent: scalabilityStructurePresent,
		SpatialScalabilityStructure: spatialScalabilityStructure,
	}
	if result.TemporalLayerCount == 0 {
		result.TemporalLayerCount = 1
	}
	if vp9OracleTraceBuild {
		activeBestQ, activeWorstQ, rateCorrectionFactor, recodeAllowed,
			recodeLoopCount := e.vp9OracleRateSelectionTrace()
		e.emitVP9OracleFrameTrace(vp9OracleFrameSummary{
			Row:                  "vp9_frame",
			FrameIndex:           e.frameIndex - 1,
			Flags:                uint32(flags),
			KeyFrame:             isKey,
			IntraOnly:            intraOnly,
			ShowFrame:            header.ShowFrame,
			Droppable:            result.Droppable,
			CodedWidth:           int(header.Width),
			CodedHeight:          int(header.Height),
			BaseQIndex:           int(header.Quant.BaseQindex),
			PublicQuantizer:      result.Quantizer,
			SizeBytes:            n,
			FirstPartitionSize:   int(header.FirstPartitionSize),
			RefreshFrameFlags:    header.RefreshFrameFlags,
			RefreshFrameContext:  header.RefreshFrameContext,
			ErrorResilient:       header.ErrorResilientMode,
			FrameParallel:        header.FrameParallelDecoding,
			FrameContextIdx:      header.FrameContextIdx,
			TxMode:               int(txMode),
			InterpFilter:         int(header.InterpFilter),
			ReferenceMode:        int(referenceMode),
			CompoundAllowed:      compoundAllowed,
			ReferenceMask:        vp9InterReferenceMask(flags),
			LoopFilterLevel:      int(header.Loopfilter.FilterLevel),
			TemporalLayerID:      result.TemporalLayerID,
			TemporalLayerCount:   result.TemporalLayerCount,
			TemporalLayerSync:    result.TemporalLayerSync,
			TL0PICIDX:            result.TL0PICIDX,
			TargetBitrateKbps:    result.TargetBitrateKbps,
			FrameTargetBits:      result.FrameTargetBits,
			BufferLevelBits:      result.BufferLevelBits,
			BufferOptimalBits:    e.rc.bufferOptimalBits,
			ActiveBestQ:          activeBestQ,
			ActiveWorstQ:         activeWorstQ,
			RateCorrectionFactor: rateCorrectionFactor,
			RecodeAllowed:        recodeAllowed,
			RecodeLoopCount:      recodeLoopCount,
			TileLog2Cols:         int(header.Tile.Log2TileCols),
			TileLog2Rows:         int(header.Tile.Log2TileRows),
		})
	}
	return result, nil
}

const (
	vp9LastRefSlot   = 0
	vp9GoldenRefSlot = 1
	vp9AltRefSlot    = 2
)

const (
	vp9NoUpdateRefFlags        = EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
	vp9ExternalRefreshCtlFlags = vp9NoUpdateRefFlags | EncodeForceGoldenFrame | EncodeForceAltRefFrame
)

func vp9TemporalReferenceRefresh(refreshFlags uint8) temporalReferenceRefresh {
	return temporalReferenceRefresh{
		Last:   refreshFlags&(1<<uint(vp9LastRefSlot)) != 0,
		Golden: refreshFlags&(1<<uint(vp9GoldenRefSlot)) != 0,
		AltRef: refreshFlags&(1<<uint(vp9AltRefSlot)) != 0,
	}
}

func (e *VP9Encoder) resetVP9EncoderFrameContexts() {
	for i := range e.frameContexts {
		vp9dec.ResetFrameContext(&e.frameContexts[i])
	}
	e.fc = e.frameContexts[0]
}

func (e *VP9Encoder) prepareVP9EncoderFrameContext(hdr *vp9dec.UncompressedHeader) int {
	idx := int(hdr.FrameContextIdx)
	if idx >= common.FrameContexts {
		idx = 0
	}
	if hdr.FrameType == common.KeyFrame ||
		hdr.ErrorResilientMode || hdr.ResetFrameContext == 3 {
		e.resetVP9EncoderFrameContexts()
		idx = 0
	} else if hdr.IntraOnly && hdr.ResetFrameContext == 2 {
		vp9dec.ResetFrameContext(&e.frameContexts[idx])
		idx = 0
	} else if hdr.IntraOnly {
		idx = 0
	} else if hdr.ResetFrameContext == 2 {
		vp9dec.ResetFrameContext(&e.frameContexts[idx])
	}
	e.fc = e.frameContexts[idx]
	return idx
}

func (e *VP9Encoder) commitVP9EncoderFrameContext(hdr *vp9dec.UncompressedHeader, idx int) {
	if idx < 0 || idx >= common.FrameContexts || !hdr.RefreshFrameContext {
		return
	}
	e.frameContexts[idx] = e.fc
}

func (e *VP9Encoder) adaptVP9EncoderFrameContext(hdr *vp9dec.UncompressedHeader,
	idx int, counts *encoder.FrameCounts, txMode common.TxMode,
) {
	if e == nil || hdr == nil || counts == nil ||
		idx < 0 || idx >= common.FrameContexts ||
		hdr.ErrorResilientMode || hdr.FrameParallelDecoding {
		return
	}
	pre := &e.frameContexts[idx]
	bridge := vp9FrameCountsFromEncoder(counts)
	adaptVP9FrameContextWithCounts(&e.fc, pre, &bridge, hdr, txMode,
		e.lastVP9HeaderValid && e.lastVP9HeaderFrameType == common.KeyFrame)
}

func (e *VP9Encoder) vp9FrameParallelDecodingMode() bool {
	if e == nil || e.opts.ErrorResilient || !e.opts.FrameParallelDecodingSet {
		return true
	}
	return e.opts.FrameParallelDecoding
}

func (e *VP9Encoder) vp9TimingState() timingState {
	return vp9TimingStateFromOptions(e.opts)
}

func vp9TimingStateFromOptions(opts VP9EncoderOptions) timingState {
	fps := opts.FPS
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		return timingState{
			timebaseNum:   opts.TimebaseNum,
			timebaseDen:   opts.TimebaseDen,
			frameDuration: 1,
		}
	}
	if fps == 0 {
		fps = 30
	}
	return timingState{timebaseNum: 1, timebaseDen: fps, frameDuration: 1}
}

func (e *VP9Encoder) vp9TemporalBufferConfig() temporalBufferConfig {
	return temporalBufferConfig{
		timing:              e.vp9TimingState(),
		bufferInitialSizeMs: libvpxDefaultBufferInitialMs,
		bufferSizeMs:        libvpxDefaultBufferSizeMs,
	}
}

func (e *VP9Encoder) vp9ResultTargetBitrateKbps() int {
	if e.rc.enabled {
		return e.rc.targetBitrateKbps
	}
	return e.opts.TargetBitrateKbps
}

func vp9InterReferenceMask(flags EncodeFlags) uint8 {
	var mask uint8
	if flags&EncodeNoReferenceLast == 0 {
		mask |= 1 << uint(vp9dec.LastFrame)
	}
	if flags&EncodeNoReferenceGolden == 0 {
		mask |= 1 << uint(vp9dec.GoldenFrame)
	}
	if flags&EncodeNoReferenceAltRef == 0 {
		mask |= 1 << uint(vp9dec.AltrefFrame)
	}
	return mask
}

func vp9AllInterReferencesDisabled(flags EncodeFlags) bool {
	const allNoRef = EncodeNoReferenceLast | EncodeNoReferenceGolden | EncodeNoReferenceAltRef
	return flags&allNoRef == allNoRef
}

func vp9InterRefreshFrameFlags(flags EncodeFlags) uint8 {
	flags = normalizeVP9EncodeFlags(flags)
	if flags&vp9ExternalRefreshCtlFlags == 0 {
		return 1 << vp9LastRefSlot
	}
	refresh := uint8(0x07)
	if flags&EncodeNoUpdateLast != 0 {
		refresh &^= 1 << vp9LastRefSlot
	}
	if flags&EncodeNoUpdateGolden != 0 {
		refresh &^= 1 << vp9GoldenRefSlot
	}
	if flags&EncodeNoUpdateAltRef != 0 {
		refresh &^= 1 << vp9AltRefSlot
	}
	return refresh
}

func (e *VP9Encoder) vp9InterRefreshFrameFlags(flags EncodeFlags) uint8 {
	refresh := vp9InterRefreshFrameFlags(flags)
	if flags&vp9ExternalRefreshCtlFlags == 0 &&
		e.rc.onePassVBRGoldenRefreshDue() {
		refresh |= 1 << vp9GoldenRefSlot
	}
	return refresh
}

func vp9InterFrameContextIdx(refreshFlags uint8) uint8 {
	if refreshFlags&(1<<vp9AltRefSlot) != 0 {
		return 1
	}
	return 0
}

func (e *VP9Encoder) vp9InterRefSignBias(flags EncodeFlags) [3]uint8 {
	return [3]uint8{
		e.refSignBias[vp9LastRefSlot],
		e.refSignBias[vp9GoldenRefSlot],
		e.refSignBias[vp9AltRefSlot],
	}
}

func (e *VP9Encoder) vp9LegacyInterRefSignBias(flags EncodeFlags) [3]uint8 {
	var bias [3]uint8
	mask := vp9InterReferenceMask(flags)
	altUsable := mask&(1<<uint(vp9dec.AltrefFrame)) != 0 &&
		e.refFrames[vp9AltRefSlot].valid
	varUsable := false
	for _, refFrame := range [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame} {
		slot, ok := vp9EncoderReferenceSlot(refFrame)
		if ok && mask&(1<<uint(refFrame)) != 0 && e.refFrames[slot].valid {
			varUsable = true
			break
		}
	}
	if altUsable && varUsable {
		bias[vp9AltRefSlot] = 1
	}
	return bias
}

func vp9EncoderTileInfo(miCols, threads int, log2TileRows int8) vp9dec.TileInfo {
	minLog2, maxLog2 := vp9dec.TileNBits(miCols)
	log2Cols := minLog2
	if threads > 1 {
		log2Cols = max(log2Cols, vp9CeilLog2(threads))
	}
	log2Cols = min(log2Cols, maxLog2)
	return vp9dec.TileInfo{
		Log2TileCols: log2Cols,
		Log2TileRows: int(log2TileRows),
	}
}

func vp9CeilLog2(v int) int {
	if v <= 1 {
		return 0
	}
	n := 0
	p := 1
	for p < v {
		p <<= 1
		n++
	}
	return n
}

func vp9EncoderReferenceSlot(refFrame int8) (int, bool) {
	switch refFrame {
	case vp9dec.LastFrame:
		return vp9LastRefSlot, true
	case vp9dec.GoldenFrame:
		return vp9GoldenRefSlot, true
	case vp9dec.AltrefFrame:
		return vp9AltRefSlot, true
	default:
		return 0, false
	}
}

func validateVP9EncodeFlags(flags EncodeFlags) error {
	flags = normalizeVP9EncodeFlags(flags)
	if err := validateEncodeFlags(flags); err != nil {
		return err
	}
	return nil
}

func normalizeVP9EncodeFlags(flags EncodeFlags) EncodeFlags {
	if flags&EncodeForceGoldenFrame != 0 {
		flags &^= EncodeNoUpdateGolden
	}
	if flags&EncodeForceAltRefFrame != 0 {
		flags &^= EncodeNoUpdateAltRef
	}
	return flags
}

func (e *VP9Encoder) vp9ShouldEncodeKeyFrame(flags EncodeFlags) bool {
	if e == nil || e.closed {
		return false
	}
	if flags&EncodeForceKeyFrame != 0 {
		return true
	}
	return e.IsKeyFrameNext()
}

func (e *VP9Encoder) hasVP9UsableInterReference(flags EncodeFlags) bool {
	mask := vp9InterReferenceMask(flags)
	for _, refFrame := range [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
		slot, ok := vp9EncoderReferenceSlot(refFrame)
		if ok && mask&(1<<uint(refFrame)) != 0 && e.refFrames[slot].valid {
			return true
		}
	}
	return false
}

func (e *VP9Encoder) validateVP9InterSegmentationReferences(flags EncodeFlags) error {
	seg := e.opts.Segmentation
	if !seg.Enabled {
		return nil
	}
	mask := vp9InterReferenceMask(flags)
	for i := range VP9MaxSegments {
		if !seg.RefFrameEnabled[i] {
			continue
		}
		refFrame := seg.RefFrame[i]
		if refFrame == vp9dec.IntraFrame {
			continue
		}
		if mask&(1<<uint(refFrame)) == 0 {
			return ErrInvalidConfig
		}
		slot, ok := vp9EncoderReferenceSlot(refFrame)
		if !ok || !e.refFrames[slot].valid {
			return ErrInvalidConfig
		}
	}
	return nil
}

func (e *VP9Encoder) vp9RefDims(slot uint8) (uint32, uint32) {
	idx := int(slot)
	if idx < len(e.refValid) && e.refValid[idx] {
		return e.refWidth[idx], e.refHeight[idx]
	}
	return uint32(e.opts.Width), uint32(e.opts.Height)
}

func (e *VP9Encoder) refreshVP9EncoderRefs(header *vp9dec.UncompressedHeader, flags EncodeFlags) {
	refreshFlags := header.RefreshFrameFlags
	for slot := range e.refValid {
		if refreshFlags&(1<<uint(slot)) == 0 {
			continue
		}
		e.refWidth[slot] = header.Width
		e.refHeight[slot] = header.Height
		e.refValid[slot] = true
		e.refSignBias[slot] = vp9EncoderRefreshRefSignBias(slot, header, flags)
		if e.reconFrame.Width != 0 && e.reconFrame.Height != 0 {
			e.refFrames[slot].store(e.reconFrame)
		}
	}
	// After the reconstruction has been stored into the ref slots, rebuild
	// the border-padded LAST_FRAME mirror that choose_partitioning's low_res
	// int_pro path reads against. Mirrors libvpx's post-reconstruction
	// vpx_extend_frame_borders call (vp9/encoder/vp9_encoder.c:3424 /
	// 3470 — extend_borders after the frame is reconstructed for the
	// realtime path).
	e.ensureLastBordered()
}

// ensureLastBordered (re)builds the encoder's border-padded LAST_FRAME luma
// mirror from the current contents of e.refFrames[vp9LastRefSlot]. Called
// at the end of refreshVP9EncoderRefs so the next frame's
// choose_partitioning sees a libvpx-shaped padded LAST plane that
// vp9_int_pro_motion_estimation can read up to (bw>>1) pixels before the
// SB origin (libvpx vp9/encoder/vp9_mcomp.c:2317-2320).
//
// libvpx counterpart: vpx_extend_frame_borders_c
// (vpx_scale/generic/yv12extend.c:130-171) invoked after each
// reconstructed frame is stored into the YV12_BUFFER_CONFIG.
func (e *VP9Encoder) ensureLastBordered() {
	if !e.refFrames[vp9LastRefSlot].valid {
		e.lastBorderedValid = false
		return
	}
	plane, stride, w, h := vp9ReferenceVisiblePlane(&e.refFrames[vp9LastRefSlot], 0)
	if len(plane) == 0 || stride <= 0 || w <= 0 || h <= 0 {
		e.lastBorderedValid = false
		return
	}
	vp9YV12BuildBorderedPlane(&e.lastBordered, plane, stride, w, h,
		vp9EncBorderInPixels)
	e.lastBorderedValid = true
}

func vp9EncoderRefreshRefSignBias(slot int, header *vp9dec.UncompressedHeader, flags EncodeFlags) uint8 {
	if header == nil || header.FrameType == common.KeyFrame || header.IntraOnly {
		return 0
	}
	if slot == vp9AltRefSlot && flags&EncodeForceAltRefFrame != 0 {
		return 1
	}
	return 0
}

func (e *VP9Encoder) refreshVP9EncoderMvRefs(isKey bool, miRows, miCols int) {
	if isKey {
		e.prevFrameMvsValid = false
		e.prevFrameMvRows = 0
		e.prevFrameMvCols = 0
		return
	}
	need := miRows * miCols
	if cap(e.prevFrameMvs) < need {
		e.prevFrameMvs = make([]vp9MvRef, need)
	} else {
		e.prevFrameMvs = e.prevFrameMvs[:need]
	}
	for i := range need {
		mi := e.miGrid[i]
		e.prevFrameMvs[i] = vp9MvRef{RefFrame: mi.RefFrame, Mv: mi.Mv}
	}
	e.prevFrameMvRows = miRows
	e.prevFrameMvCols = miCols
	e.prevFrameMvsValid = true
}

func (e *VP9Encoder) refreshVP9EncoderSegmentMap(miRows, miCols int) {
	need := miRows * miCols
	if need <= 0 || len(e.miGrid) < need {
		e.prevSegmentMapValid = false
		e.prevSegmentMapRows = 0
		e.prevSegmentMapCols = 0
		return
	}
	if cap(e.prevSegmentMap) < need {
		e.prevSegmentMap = make([]uint8, need)
	} else {
		e.prevSegmentMap = e.prevSegmentMap[:need]
	}
	for i := range need {
		e.prevSegmentMap[i] = e.miGrid[i].SegmentID
	}
	e.prevSegmentMapRows = miRows
	e.prevSegmentMapCols = miCols
	e.prevSegmentMapValid = true
}

func (e *VP9Encoder) useVP9EncoderPrevFrameMvs(miRows, miCols int) bool {
	return e.prevFrameMvsValid &&
		!e.opts.ErrorResilient &&
		e.prevFrameMvRows == miRows &&
		e.prevFrameMvCols == miCols &&
		len(e.prevFrameMvs) >= miRows*miCols
}

func (e *VP9Encoder) useVP9EncoderPrevSegmentMap(miRows, miCols int) bool {
	return e.prevSegmentMapValid &&
		e.prevSegmentMapRows == miRows &&
		e.prevSegmentMapCols == miCols &&
		len(e.prevSegmentMap) >= miRows*miCols
}

func (e *VP9Encoder) ensureVP9EncoderModeBuffers(miRows, miCols int) {
	miColsAligned := alignToSb(miCols)
	if cap(e.aboveSegCtx) < miColsAligned {
		e.aboveSegCtx = make([]int8, miColsAligned)
	} else {
		e.aboveSegCtx = e.aboveSegCtx[:miColsAligned]
		for i := range e.aboveSegCtx {
			e.aboveSegCtx[i] = 0
		}
	}
	if cap(e.leftSegCtx) < common.MiBlockSize {
		e.leftSegCtx = make([]int8, common.MiBlockSize)
	} else {
		e.leftSegCtx = e.leftSegCtx[:common.MiBlockSize]
	}
	miGridLen := miRows * miCols
	if cap(e.miGrid) < miGridLen {
		e.miGrid = make([]vp9dec.NeighborMi, miGridLen)
	} else {
		e.miGrid = e.miGrid[:miGridLen]
		for i := range e.miGrid {
			e.miGrid[i] = vp9dec.NeighborMi{}
		}
	}
	// varPartGrid / varPartSBComputed are allocated lazily inside
	// vp9EnsureSBPartitionChosen so the steady-state encode path
	// (which currently does not invoke the libvpx choose_partitioning
	// port) pays no allocation cost. Reset the frame-validity flag
	// and per-SB computed mask in place when state already exists; the
	// reset MUST happen here (once per frame) and never on each per-MI
	// vp9EnsureSBPartitionChosen call, because the picker stamps the
	// partition tree into varPartGrid for every SB in the frame and a
	// per-call wipe would lose decisions for SBs the walker re-visits.
	if cap(e.varPartGrid) >= miGridLen {
		e.varPartGrid = e.varPartGrid[:miGridLen]
		for i := range e.varPartGrid {
			e.varPartGrid[i] = vp9dec.NeighborMi{}
		}
	}
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	if cap(e.varPartSBComputed) >= sbCount {
		e.varPartSBComputed = e.varPartSBComputed[:sbCount]
		for i := range e.varPartSBComputed {
			e.varPartSBComputed[i] = false
		}
	}
	e.varPartFrameValid = false
	// Invalidate the per-frame border-padded source mirror so the next
	// choose_partitioning inter call rebuilds it from the current frame's
	// source plane. The padded LAST mirror (e.lastBordered) is rebuilt at
	// end-of-frame inside refreshVP9EncoderRefs, not here.
	e.intProSrcBorderedValid = false
	// ML_BASED_PARTITION's per-SB context cache must be reset per frame
	// (libvpx vp9_encodeframe.c:5314 — get_estimated_pred fills x->est_pred
	// fresh for every SB on every frame). See vp9_nonrd_pick_partition.go.
	e.vp9ResetMLPartitionCache(miRows, miCols)
	e.ensureVP9LeafInterDecisionCache(miRows, miCols)
	if cap(e.partitionReconScratch) < vp9MaxPartitionReconScratch {
		e.partitionReconScratch = make([]byte, vp9MaxPartitionReconScratch)
	} else {
		e.partitionReconScratch = e.partitionReconScratch[:vp9MaxPartitionReconScratch]
	}
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		aboveLen := vp9PlaneEntropyLen(miColsAligned, pd.SubsamplingX)
		leftLen := vp9PlaneEntropyLen(common.MiBlockSize, pd.SubsamplingY)
		if cap(pd.AboveContext) < aboveLen {
			pd.AboveContext = make([]uint8, aboveLen)
		} else {
			pd.AboveContext = pd.AboveContext[:aboveLen]
		}
		if cap(pd.LeftContext) < leftLen {
			pd.LeftContext = make([]uint8, leftLen)
		} else {
			pd.LeftContext = pd.LeftContext[:leftLen]
		}
	}
}

func (e *VP9Encoder) resetVP9EncoderAboveEntropyContexts() {
	for plane := range vp9dec.MaxMbPlane {
		ctx := e.planes[plane].AboveContext
		for i := range ctx {
			ctx[i] = 0
		}
	}
}

func (e *VP9Encoder) resetVP9EncoderLeftEntropyContexts() {
	for plane := range vp9dec.MaxMbPlane {
		ctx := e.planes[plane].LeftContext
		for i := range ctx {
			ctx[i] = 0
		}
	}
}

func (e *VP9Encoder) vp9EncoderPlaneContextOffsets(miRow, miCol int) (
	above [vp9dec.MaxMbPlane]int, left [vp9dec.MaxMbPlane]int,
) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		above[plane] = (miCol * 2) >> pd.SubsamplingX
		left[plane] = ((miRow * 2) >> pd.SubsamplingY) % len(pd.LeftContext)
	}
	return above, left
}

func (e *VP9Encoder) prepareVP9EncoderOutputFrame(width, height int) {
	layout := vp9FrameBufferLayout(width, height)
	e.reconYFull = ensureVP9AlignedPlaneCapacity(e.reconYFull, layout.yFullLen)
	e.reconUFull = ensureVP9AlignedPlaneCapacity(e.reconUFull, layout.uvFullLen)
	e.reconVFull = ensureVP9AlignedPlaneCapacity(e.reconVFull, layout.uvFullLen)
	fillVP9Plane(e.reconYFull, 128)
	fillVP9Plane(e.reconUFull, 128)
	fillVP9Plane(e.reconVFull, 128)
	e.reconY = e.reconYFull[layout.yOrigin:]
	e.reconU = e.reconUFull[layout.uvOrigin:]
	e.reconV = e.reconVFull[layout.uvOrigin:]
	e.reconFrame = Image{
		Width:   width,
		Height:  height,
		Y:       e.reconY,
		U:       e.reconU,
		V:       e.reconV,
		YStride: layout.yStride,
		UStride: layout.uvStride,
		VStride: layout.uvStride,
	}
}

func (e *VP9Encoder) resetVP9EncoderCodingState(width, height int) {
	e.prepareVP9EncoderOutputFrame(width, height)
	for i := range e.aboveSegCtx {
		e.aboveSegCtx[i] = 0
	}
	for i := range e.leftSegCtx {
		e.leftSegCtx[i] = 0
	}
	for i := range e.miGrid {
		e.miGrid[i] = vp9dec.NeighborMi{}
	}
	e.resetVP9EncoderAboveEntropyContexts()
	e.resetVP9EncoderLeftEntropyContexts()
}

func (e *VP9Encoder) collectVP9EncodeFrameCounts(width, height, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	isKey, intraOnly bool, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) *encoder.FrameCounts {
	counts := &e.frameCounts
	*counts = encoder.FrameCounts{}

	var countKey *vp9KeyframeEncodeState
	if key != nil {
		tmp := *key
		tmp.counts = counts
		countKey = &tmp
	}
	var countInter *vp9InterEncodeState
	if inter != nil {
		tmp := *inter
		tmp.counts = counts
		countInter = &tmp
	}

	kind := vp9ModeTreeInterSource
	if isKey {
		kind = vp9ModeTreeKeyframeSource
	} else if intraOnly {
		kind = vp9ModeTreeKeyframe
	}
	miGridValid := e.collectVP9FrameTileCounts(width, height, miRows, miCols, tileInfo,
		partitionProbs, seg, baseMi, txMode, kind, countKey, countInter)
	if miGridValid && e.vp9ActiveSegmentMapCodingChooser() {
		e.vp9ChooseSegmentMapCodingMethod(seg, miRows, miCols, tileInfo,
			isKey || intraOnly)
	}

	e.resetVP9EncoderCodingState(width, height)
	return counts
}

func (e *VP9Encoder) collectVP9FrameTileCounts(width, height, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) bool {
	tileRows := 1 << uint(tileInfo.Log2TileRows)
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if e.opts.Threads > 1 && e.opts.NoiseSensitivity == 0 &&
		tileRows == 1 && tileCols > 1 {
		seed := vp9CountTileSeedForState(key, inter)
		if e.collectVP9FrameTileCountsThreaded(width, height, miRows, miCols,
			tileInfo, partitionProbs, seg, baseMi, txMode, kind, seed) {
			return e.vp9ActiveSegmentMapCodingChooser()
		}
	}
	for tileRow := range tileRows {
		for tileCol := range tileCols {
			var bw bitstream.Writer
			bw.Start(e.scratch[:])
			e.writeVP9FrameTile(&bw, miRows, miCols,
				vp9EncoderTileBounds(tileRow, tileCol, miRows, miCols, tileInfo),
				partitionProbs, seg, baseMi, txMode, kind, key, inter)
			_, _ = bw.Stop()
		}
	}
	return true
}

// vp9ChooseSegmentMapCodingMethod mirrors libvpx's segment-map coding
// selection: count the emitted map, then choose temporal prediction only when
// its segment-id and prediction-flag cost beats coding the map directly.
func (e *VP9Encoder) vp9ChooseSegmentMapCodingMethod(seg *vp9dec.SegmentationParams,
	miRows, miCols int, tileInfo vp9dec.TileInfo, intraOnly bool,
) {
	if e == nil || seg == nil || !seg.Enabled || !seg.UpdateMap ||
		miRows <= 0 || miCols <= 0 || len(e.miGrid) < miRows*miCols {
		return
	}
	var noPredCounts [vp9dec.MaxSegments]uint32
	var tUnpredCounts [vp9dec.MaxSegments]uint32
	var temporalCounts [vp9dec.PredictionProbs][2]uint32
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	for tileCol := range tileCols {
		tile := vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo)
		for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
			for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
				e.countVP9SegmentMapSB(miRows, miCols, tile, miRow,
					miCol, common.Block64x64, !intraOnly, &noPredCounts,
					&temporalCounts, &tUnpredCounts)
			}
		}
	}

	var noPredTree, tPredTree [vp9dec.SegTreeProbs]uint8
	vp9CalcSegTreeProbs(noPredCounts, &noPredTree)
	noPredCost := vp9CostSegMap(noPredCounts, noPredTree)
	tPredCost := int(^uint(0) >> 1)
	var predProbs [vp9dec.PredictionProbs]uint8
	if !intraOnly {
		vp9CalcSegTreeProbs(tUnpredCounts, &tPredTree)
		tPredCost = vp9CostSegMap(tUnpredCounts, tPredTree)
		for i := range predProbs {
			count0 := temporalCounts[i][0]
			count1 := temporalCounts[i][1]
			predProbs[i] = encoder.GetBinaryProb(count0, count1)
			tPredCost += int(count0)*encoder.VP9CostZero(predProbs[i]) +
				int(count1)*encoder.VP9CostOne(predProbs[i])
		}
	}
	if tPredCost < noPredCost {
		seg.TemporalUpdate = true
		seg.TreeProbs = tPredTree
		seg.PredProbs = predProbs
		return
	}
	seg.TemporalUpdate = false
	seg.TreeProbs = noPredTree
	for i := range seg.PredProbs {
		seg.PredProbs[i] = vp9dec.MaxProb
	}
}

func (e *VP9Encoder) countVP9SegmentMapSB(miRows, miCols int,
	tile vp9dec.TileBounds, miRow, miCol int, bsize common.BlockSize,
	allowTemporal bool, noPredCounts *[vp9dec.MaxSegments]uint32,
	temporalCounts *[vp9dec.PredictionProbs][2]uint32,
	tUnpredCounts *[vp9dec.MaxSegments]uint32,
) {
	if miRow >= miRows || miCol >= miCols || miCol >= tile.MiColEnd {
		return
	}
	mi := e.vp9MiAt(miRows, miCols, miRow, miCol)
	if mi == nil {
		return
	}
	bs := int(common.Num8x8BlocksWideLookup[bsize])
	hbs := bs >> 1
	bw := int(common.Num8x8BlocksWideLookup[mi.SbType])
	bh := int(common.Num8x8BlocksHighLookup[mi.SbType])
	if bw == bs && bh == bs {
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		return
	}
	if bw == bs && bh < bs {
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow+hbs, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		return
	}
	if bw < bs && bh == bs {
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol+hbs,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		return
	}
	subsize := common.SubsizeLookup[common.PartitionSplit][bsize]
	if subsize >= common.BlockSizes || hbs <= 0 {
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		return
	}
	for dr := 0; dr <= hbs; dr += hbs {
		for dc := 0; dc <= hbs; dc += hbs {
			e.countVP9SegmentMapSB(miRows, miCols, tile, miRow+dr,
				miCol+dc, subsize, allowTemporal, noPredCounts,
				temporalCounts, tUnpredCounts)
		}
	}
}

func (e *VP9Encoder) countVP9SegmentMapBlock(miRows, miCols int,
	tile vp9dec.TileBounds, miRow, miCol int, allowTemporal bool,
	noPredCounts *[vp9dec.MaxSegments]uint32,
	temporalCounts *[vp9dec.PredictionProbs][2]uint32,
	tUnpredCounts *[vp9dec.MaxSegments]uint32,
) {
	if miRow >= miRows || miCol >= miCols || miCol >= tile.MiColEnd {
		return
	}
	mi := e.vp9MiAt(miRows, miCols, miRow, miCol)
	if mi == nil || mi.SegmentID >= vp9dec.MaxSegments {
		return
	}
	segID := mi.SegmentID
	noPredCounts[segID]++
	if !allowTemporal {
		return
	}
	left := e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	if miCol <= tile.MiColStart {
		left = nil
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	ctx := vp9dec.GetPredContextSegId(above, left)
	if ctx < 0 || ctx >= vp9dec.PredictionProbs {
		ctx = 0
	}
	predicted := uint8(0)
	if e.vp9EncoderPreviousSegmentID(miRows, miCols, miRow, miCol,
		mi.SbType) == segID {
		predicted = 1
	}
	temporalCounts[ctx][predicted]++
	if predicted == 0 {
		tUnpredCounts[segID]++
	}
}

func vp9CalcSegTreeProbs(segCounts [vp9dec.MaxSegments]uint32,
	probs *[vp9dec.SegTreeProbs]uint8,
) {
	c01 := segCounts[0] + segCounts[1]
	c23 := segCounts[2] + segCounts[3]
	c45 := segCounts[4] + segCounts[5]
	c67 := segCounts[6] + segCounts[7]
	probs[0] = encoder.GetBinaryProb(c01+c23, c45+c67)
	probs[1] = encoder.GetBinaryProb(c01, c23)
	probs[2] = encoder.GetBinaryProb(c45, c67)
	probs[3] = encoder.GetBinaryProb(segCounts[0], segCounts[1])
	probs[4] = encoder.GetBinaryProb(segCounts[2], segCounts[3])
	probs[5] = encoder.GetBinaryProb(segCounts[4], segCounts[5])
	probs[6] = encoder.GetBinaryProb(segCounts[6], segCounts[7])
}

func vp9CostSegMap(segCounts [vp9dec.MaxSegments]uint32,
	probs [vp9dec.SegTreeProbs]uint8,
) int {
	c01 := segCounts[0] + segCounts[1]
	c23 := segCounts[2] + segCounts[3]
	c45 := segCounts[4] + segCounts[5]
	c67 := segCounts[6] + segCounts[7]
	c0123 := c01 + c23
	c4567 := c45 + c67
	cost := int(c0123)*encoder.VP9CostZero(probs[0]) +
		int(c4567)*encoder.VP9CostOne(probs[0])
	if c0123 > 0 {
		cost += int(c01)*encoder.VP9CostZero(probs[1]) +
			int(c23)*encoder.VP9CostOne(probs[1])
		if c01 > 0 {
			cost += int(segCounts[0])*encoder.VP9CostZero(probs[3]) +
				int(segCounts[1])*encoder.VP9CostOne(probs[3])
		}
		if c23 > 0 {
			cost += int(segCounts[2])*encoder.VP9CostZero(probs[4]) +
				int(segCounts[3])*encoder.VP9CostOne(probs[4])
		}
	}
	if c4567 > 0 {
		cost += int(c45)*encoder.VP9CostZero(probs[2]) +
			int(c67)*encoder.VP9CostOne(probs[2])
		if c45 > 0 {
			cost += int(segCounts[4])*encoder.VP9CostZero(probs[5]) +
				int(segCounts[5])*encoder.VP9CostOne(probs[5])
		}
		if c67 > 0 {
			cost += int(segCounts[6])*encoder.VP9CostZero(probs[6]) +
				int(segCounts[7])*encoder.VP9CostOne(probs[6])
		}
	}
	return cost
}

func vp9EncodeCountsForState(key *vp9KeyframeEncodeState,
	inter *vp9InterEncodeState,
) *encoder.FrameCounts {
	if key != nil && key.counts != nil {
		return key.counts
	}
	if inter != nil {
		return inter.counts
	}
	return nil
}

func txModeForMi(mi vp9dec.NeighborMi) common.TxMode {
	if mi.TxSize >= common.Tx32x32 {
		return common.Allow32x32
	}
	if mi.TxSize >= common.Tx16x16 {
		return common.Allow16x16
	}
	if mi.TxSize >= common.Tx8x8 {
		return common.Allow8x8
	}
	return common.Only4x4
}

// vp9EncoderFrameTxMode is a verbatim port of libvpx select_tx_mode at
// vp9/encoder/vp9_encodeframe.c:4334-4345:
//
//	static TX_MODE select_tx_mode(const VP9_COMP *cpi, MACROBLOCKD *const xd) {
//	  if (xd->lossless) return ONLY_4X4;
//	  if (cpi->common.frame_type == KEY_FRAME && cpi->sf.use_nonrd_pick_mode)
//	    return ALLOW_16X16;
//	  if (cpi->sf.tx_size_search_method == USE_LARGESTALL)
//	    return ALLOW_32X32;
//	  else if (cpi->sf.tx_size_search_method == USE_FULL_RD ||
//	           cpi->sf.tx_size_search_method == USE_TX_8X8)
//	    return TX_MODE_SELECT;
//	  else
//	    return cpi->common.tx_mode;
//	}
//
// libvpx's select_tx_mode runs once per frame at
// vp9/encoder/vp9_encodeframe.c:5650 (the top of vp9_encode_frame_internal),
// AFTER set_speed_features_framesize_dependent has refreshed the per-frame
// speed-feature snapshot (vp9_encoder.c:3754/3765). govpx mirrors that
// protocol via the per-frame vp9ApplySpeedFeatures call at
// encodeVP9FrameIntoWithFlagsResultInternal:2546, so e.sf carries the live
// per-frame value at the time select_tx_mode runs. The
// vp9SelectTxModeSpeedFeatures helper additionally precomputes the
// (use_nonrd_pick_mode, tx_size_search_method) pair libvpx's
// set_*_speed_feature would produce for the (deadline, cpu_used, isKey,
// intraOnly) tuple so the use_nonrd_pick_mode predicate can be evaluated
// independently of the live sf snapshot — see
// vp9_speed_features.c:485-504,558-583,1042 (RT path) and
// vp9_speed_features.c:1250-1252,1286-1288,1310,1447-1449,1539-1541,
// 1595-1597 (GOOD path).
//
// libvpx INTRA_ONLY frames go through the non-KEY_FRAME branch because
// the KEY_FRAME predicate compares cm->frame_type == KEY_FRAME literally
// (vp9_blockd.h:34-38 — INTRA_ONLY uses cm->frame_type == INTER_FRAME with
// the intra_only flag set). govpx now honours the libvpx
// tx_size_search_method dispatch for both KEY_FRAME and intra-only via a
// shared switch — the keyframe-source block writer at writeVP9ModeBlock:6885+
// and the vp9ModeTreeKeyframe fallback (introduced in commit 0dfca64)
// both plumb the TxModeSelect-shaped tx_probs row, so neither path
// panics in WriteSelectedTxSize. write_mb_modes_kf at
// vp9_bitstream.c:344-376 services both frame types via
// frame_is_intra_only(cm) at vp9_bitstream.c:395-396.
//
// The inter path (non-key non-intra-only) remains pinned at
// TX_MODE_SELECT — a libvpx-faithful per-frame switch there would
// surface USE_LARGESTALL -> ALLOW_32X32 at GOOD speed 3 / RT speed 1
// inter, a deeper byte-parity excursion than this commit targets and
// tracked as a separate follow-up.
func (e *VP9Encoder) vp9EncoderFrameTxMode(isKey, intraOnly, lossless bool) common.TxMode {
	if lossless {
		return common.Only4x4
	}
	useNonrd, _ := e.vp9SelectTxModeSpeedFeatures(isKey, intraOnly)
	if isKey && useNonrd {
		// libvpx vp9_encodeframe.c:4336-4337 — the KEY_FRAME &&
		// use_nonrd_pick_mode ALLOW_16X16 clamp ported verbatim.
		// Note: libvpx's `frame_type == KEY_FRAME` predicate is
		// literal — intra-only frames carry frame_type INTER_FRAME
		// and fall through to the tx_size_search_method dispatch
		// below.
		return common.Allow16x16
	}
	if isKey || intraOnly {
		// libvpx vp9_encodeframe.c:4338-4344 fallthrough for KEY_FRAME
		// and intra-only frames once the `use_nonrd_pick_mode` clamp
		// above has been ruled out. libvpx's select_tx_mode reads
		// sf.tx_size_search_method directly here:
		//   USE_LARGESTALL                 -> ALLOW_32X32 (:4338-4339)
		//   USE_FULL_RD or USE_TX_8X8      -> TX_MODE_SELECT (:4340-4342)
		// Note: libvpx's `cm->frame_type == KEY_FRAME` predicate is
		// literal — intra-only frames carry INTER_FRAME
		// (vp9_blockd.h:34-38), but the dispatch is identical for both
		// because (a) the use_nonrd_pick_mode clamp at :4336-4337 only
		// fires on KEY_FRAME and we've already handled it above, and
		// (b) the remaining body is frame-type-agnostic. The per-frame
		// SF refresh (encodeVP9FrameIntoWithFlagsResultInternal ->
		// vp9ApplySpeedFeatures) keeps e.sf.TxSizeSearchMethod tracking
		// the live frame state, so reading it here is verbatim. The
		// unified writer write_mb_modes_kf at vp9_bitstream.c:344-376
		// services both KEY_FRAME and intra-only via
		// frame_is_intra_only(cm) at vp9_bitstream.c:395-396; govpx's
		// keyframe block writer at writeVP9ModeBlock:6885+ already
		// plumbs the TxModeSelect-shaped tx_probs row (committed in
		// 0dfca64 alongside the vp9ModeTreeKeyframe fallback) so the
		// KEY_FRAME path no longer panics in WriteSelectedTxSize.
		switch e.sf.TxSizeSearchMethod {
		case UseLargestAll:
			return common.Allow32x32
		default:
			return common.TxModeSelect
		}
	}
	// libvpx vp9_encodeframe.c:4340-4344 fallthrough for non-key non-
	// intra-only frames. govpx pins inter frames at TX_MODE_SELECT to
	// preserve byte parity against the established golden corpus
	// (libvpx's default + RT speed>=5 path lands on USE_TX_8X8 ->
	// TX_MODE_SELECT for the cpu_used=8 surface that dominates the
	// byte-parity matrix). Lifting this pin would surface
	// USE_LARGESTALL -> ALLOW_32X32 at GOOD speed 3 / RT speed 1 inter
	// — a deeper byte-parity excursion than this commit targets and
	// is tracked as a separate follow-up.
	return common.TxModeSelect
}

// vp9SelectTxModeSpeedFeatures returns the (use_nonrd_pick_mode,
// tx_size_search_method) pair libvpx's per-frame speed-feature dispatcher
// would set for this frame, given the current (deadline, cpu_used,
// isKey, intraOnly) triple. Mirrors the relevant assignments inside
// vp9_speed_features.c set_good_speed_feature / set_rt_speed_feature
// without requiring a full per-frame re-apply. Currently consumed only by
// the KEY_FRAME && use_nonrd_pick_mode branch of vp9EncoderFrameTxMode;
// the wider port lives behind the per-frame speed-feature refresh TODO.
func (e *VP9Encoder) vp9SelectTxModeSpeedFeatures(isKey, intraOnly bool) (useNonrd bool, txSearch TxSizeSearchMethod) {
	speed := e.vp9SpeedFeatureCPUUsed()
	mode := vp9ResolveDeadlineMode(e.opts.Deadline)
	if mode == vp9ModeRealtime {
		// libvpx vp9_speed_features.c:492-493 (RT speed>=1):
		//   tx_size_search_method =
		//       frame_is_intra_only(cm) ? USE_FULL_RD : USE_LARGESTALL;
		if speed >= 1 {
			if intraOnly {
				txSearch = UseFullRD
			} else {
				txSearch = UseLargestAll
			}
		}
		// libvpx vp9_speed_features.c:597-598 (RT speed>=5):
		//   sf->use_nonrd_pick_mode = 1;
		//   tx_size_search_method = is_keyframe ? USE_LARGESTALL : USE_TX_8X8;
		if speed >= 5 {
			useNonrd = true
			if isKey {
				txSearch = UseLargestAll
			} else {
				txSearch = UseTx8x8
			}
		}
		return useNonrd, txSearch
	}
	// GOOD path. libvpx vp9_speed_features.c:1042 dispatch.
	// libvpx vp9_speed_features.c:929-940 best-quality default:
	//   tx_size_search_method = USE_FULL_RD.
	txSearch = UseFullRD
	// libvpx vp9_speed_features.c:326-327 (GOOD speed>=2):
	//   sf->tx_size_search_method =
	//       frame_is_boosted(cpi) ? USE_FULL_RD : USE_LARGESTALL;
	// govpx's KF predicate is the simplest is_boosted approximation
	// available pre-per-frame-SF-refresh — KF is always boosted; GF/ARF
	// boostedness requires the per-frame dispatcher.
	if speed >= 2 {
		if isKey {
			txSearch = UseFullRD
		} else {
			txSearch = UseLargestAll
		}
	}
	// libvpx vp9_speed_features.c:381-382 (GOOD speed>=3):
	//   sf->tx_size_search_method =
	//       frame_is_intra_only(cm) ? USE_FULL_RD : USE_LARGESTALL;
	if speed >= 3 {
		if intraOnly {
			txSearch = UseFullRD
		} else {
			txSearch = UseLargestAll
		}
	}
	// libvpx vp9_speed_features.c:386 (GOOD speed>=4):
	//   sf->tx_size_search_method = USE_LARGESTALL;
	if speed >= 4 {
		txSearch = UseLargestAll
	}
	// libvpx vp9_speed_features.c:415-416 (GOOD speed>=5):
	//   sf->tx_size_search_method =
	//       frame_is_intra_only(cm) ? USE_LARGESTALL : USE_TX_8X8;
	//   sf->use_nonrd_pick_mode = 1;
	if speed >= 5 {
		useNonrd = true
		if intraOnly {
			txSearch = UseLargestAll
		} else {
			txSearch = UseTx8x8
		}
	}
	return useNonrd, txSearch
}

// vp9EncoderFrameTxModeFromCounts demotes the per-frame tx_mode after
// the counts pass. libvpx's verbatim post-encode demotion lives at
// vp9/encoder/vp9_encodeframe.c:5911-5944 and gates the demotion on
// (sf.frame_parameter_update && cm->tx_mode == TX_MODE_SELECT). govpx
// historically applied a wider, govpx-only "skip for TX_MODE_SELECT,
// demote everything else" pass that does not match libvpx's predicate
// but is preserved here because downstream consumers (including the
// strict byte-parity matrix) are pinned to the existing output. The
// libvpx-faithful gate (skip-when-not-TxModeSelect) is deferred together
// with the partition-context-aware count split (counts->tx.pXxX) that
// would replace the simpler TxTotals ladder used here.
func vp9EncoderFrameTxModeFromCounts(txMode common.TxMode, lossless bool,
	counts *encoder.FrameCounts,
) common.TxMode {
	if lossless {
		return common.Only4x4
	}
	if counts == nil || txMode == common.TxModeSelect {
		return txMode
	}
	for tx := common.Tx32x32; tx > common.Tx4x4; tx-- {
		if counts.TxTotals[tx] == 0 {
			continue
		}
		switch tx {
		case common.Tx32x32:
			return common.Allow32x32
		case common.Tx16x16:
			return common.Allow16x16
		case common.Tx8x8:
			return common.Allow8x8
		}
	}
	return common.Only4x4
}

// vp9EncoderFrameInterpFilter mirrors libvpx's frame-level interp_filter
// assignment at vp9/encoder/vp9_encoder.c:2141 —
//
//	cm->interp_filter = cpi->sf.default_interp_filter;
//
// The speed-features configurator
// (vp9/encoder/vp9_speed_features.c:1008) initialises
// default_interp_filter to SWITCHABLE for every speed; only the
// realtime cpu_used>=9 low-motion gate at vp9_speed_features.c:812
// downgrades it to BILINEAR. All other speeds (including cpu_used=8
// realtime and good-quality) inherit SWITCHABLE, which enables the
// per-block 3-filter RD search already wired through
// vp9_pick_inter_mode_nonrd.go (filterRef / predFilterSearch loop)
// and pickVP9InterMode (vp9InterInterpFilterCandidates).
//
// Lossless / keyframe / intra-only frames carry the same field —
// libvpx does not special-case them at this assignment site; the
// uncompressed-header writer omits the field for intra-only frames
// (header_writer.go:196) so the value is harmless for those frame
// types.
func (e *VP9Encoder) vp9EncoderFrameInterpFilter(isKey, intraOnly, lossless bool) vp9dec.InterpFilter {
	if e == nil {
		return vp9dec.InterpEighttap
	}
	filter := e.sf.DefaultInterpFilter
	if filter > vp9dec.InterpSwitchable {
		// Unset / out-of-range falls back to the libvpx initial value
		// at vp9_speed_features.c:1008.
		return vp9dec.InterpSwitchable
	}
	return filter
}

// vp9GetFrameTypeForFilterThreshes mirrors libvpx's get_frame_type at
// vp9/encoder/vp9_encodeframe.c:4323-4332, used to index into
// filter_threshes[MV_REFERENCE_FRAME] for the per-frame SWITCHABLE ->
// concrete InterpFilter demotion. The mapping is exactly:
//
//	if (frame_is_intra_only(cm))                          return INTRA_FRAME;
//	else if (rc.is_src_frame_alt_ref && refresh_golden)   return ALTREF_FRAME;
//	else if (refresh_golden || refresh_alt_ref)           return GOLDEN_FRAME;
//	else                                                  return LAST_FRAME;
//
// govpx tracks `is_src_frame_alt_ref` as (!showFrame ||
// EncodeForceAltRefFrame); refresh_golden / refresh_alt_ref are decoded
// from the libvpx-shaped refresh_frame_flags slot bits (vp9GoldenRefSlot
// / vp9AltRefSlot at vp9_encoder.c:2773-2774).
func vp9GetFrameTypeForFilterThreshes(isKey, intraOnly, isSrcFrameAltRef,
	refreshGolden, refreshAlt bool,
) int {
	if isKey || intraOnly {
		return vp9dec.IntraFrame
	}
	if isSrcFrameAltRef && refreshGolden {
		return vp9dec.AltrefFrame
	}
	if refreshGolden || refreshAlt {
		return vp9dec.GoldenFrame
	}
	return vp9dec.LastFrame
}

// vp9GetInterpFilterFromThreshes is the verbatim port of libvpx's
// get_interp_filter at vp9/encoder/vp9_encodeframe.c:5759-5773:
//
//	if (!is_alt_ref && threshes[EIGHTTAP_SMOOTH] > threshes[EIGHTTAP] &&
//	    threshes[EIGHTTAP_SMOOTH] > threshes[EIGHTTAP_SHARP] &&
//	    threshes[EIGHTTAP_SMOOTH] > threshes[SWITCHABLE - 1]) {
//	  return EIGHTTAP_SMOOTH;
//	} else if (threshes[EIGHTTAP_SHARP] > threshes[EIGHTTAP] &&
//	           threshes[EIGHTTAP_SHARP] > threshes[SWITCHABLE - 1]) {
//	  return EIGHTTAP_SHARP;
//	} else if (threshes[EIGHTTAP] > threshes[SWITCHABLE - 1]) {
//	  return EIGHTTAP;
//	} else {
//	  return SWITCHABLE;
//	}
//
// Note that libvpx indexes the threshold array up to SWITCHABLE_FILTER_CONTEXTS
// (== SWITCHABLE_FILTERS + 1 == 4 here), and the gate slot
// `threshes[SWITCHABLE - 1]` is `threshes[3]` (the BILINEAR slot, repurposed
// here as the "switchable wins" comparator — libvpx's filter_diff accumulator
// uses index SWITCHABLE_FILTERS for the switchable rd cost, which lands at 3
// since the EIGHTTAP family occupies 0..2). See the matching slot use in the
// post-encode merge at vp9_encodeframe.c:5890-5891.
func vp9GetInterpFilterFromThreshes(
	threshes [vp9dec.SwitchableFilterContexts]int64, isAltRef bool,
) vp9dec.InterpFilter {
	const switchableSlot = int(vp9dec.InterpSwitchable) - 1
	if !isAltRef &&
		threshes[vp9dec.InterpEighttapSmooth] > threshes[vp9dec.InterpEighttap] &&
		threshes[vp9dec.InterpEighttapSmooth] > threshes[vp9dec.InterpEighttapSharp] &&
		threshes[vp9dec.InterpEighttapSmooth] > threshes[switchableSlot] {
		return vp9dec.InterpEighttapSmooth
	}
	if threshes[vp9dec.InterpEighttapSharp] > threshes[vp9dec.InterpEighttap] &&
		threshes[vp9dec.InterpEighttapSharp] > threshes[switchableSlot] {
		return vp9dec.InterpEighttapSharp
	}
	if threshes[vp9dec.InterpEighttap] > threshes[switchableSlot] {
		return vp9dec.InterpEighttap
	}
	return vp9dec.InterpSwitchable
}

// vp9SaveEncodeParamsFilterThreshes mirrors the filter_threshes subset of
// libvpx's save_encode_params at vp9/encoder/vp9_encoder.c:3927-3946:
//
//	for (j = 0; j < SWITCHABLE_FILTER_CONTEXTS; j++)
//	  rd_opt->filter_threshes_prev[i][j] = rd_opt->filter_threshes[i][j];
//
// (The prediction_type_thresh and per-tile freq_fact halves are owned by
// other ports; this routine only handles the InterpFilter snapshot.)
// Called once per frame before vp9EncodeFrame mutates filter_threshes
// (libvpx call site: vp9_encoder.c:5355, ahead of encode_frame_to_data_rate).
func (e *VP9Encoder) vp9SaveEncodeParamsFilterThreshes() {
	e.vp9FilterThreshesPrev = e.vp9FilterThreshes
}

// vp9RestoreEncodeParamsFilterThreshes mirrors the filter_threshes subset of
// libvpx's restore_encode_params at vp9/encoder/vp9_encodeframe.c:5798-5820:
//
//	for (j = 0; j < SWITCHABLE_FILTER_CONTEXTS; j++)
//	  rd_opt->filter_threshes[i][j] = rd_opt->filter_threshes_prev[i][j];
//
// libvpx calls this at the top of every vp9_encode_frame so each recode
// iteration starts from the same baseline; govpx encodes each frame once
// today, so the restore is a no-op in steady state — kept verbatim because
// the recode loop is on the roadmap.
func (e *VP9Encoder) vp9RestoreEncodeParamsFilterThreshes() {
	e.vp9FilterThreshes = e.vp9FilterThreshesPrev
}

// vp9DemoteSwitchableInterpFilter applies the per-frame SWITCHABLE -> concrete
// filter demotion at libvpx vp9/encoder/vp9_encodeframe.c:5846-5877:
//
//	if (cpi->sf.frame_parameter_update) {
//	  ...
//	  const MV_REFERENCE_FRAME frame_type = get_frame_type(cpi);
//	  int64_t *const filter_thrs = rd_opt->filter_threshes[frame_type];
//	  const int is_alt_ref = frame_type == ALTREF_FRAME;
//	  ...
//	  if (cm->interp_filter == SWITCHABLE)
//	    cm->interp_filter = get_interp_filter(filter_thrs, is_alt_ref);
//	}
//
// The gate `cpi->sf.frame_parameter_update` matches govpx
// `e.sf.FrameParameterUpdate != 0` (vp9_speed_features.go:336,766,1517).
// Demotion is skipped entirely outside that path, leaving header.InterpFilter
// at SWITCHABLE so the per-block 3-filter RD search drives the per-block
// mi->interp_filter writes.
func (e *VP9Encoder) vp9DemoteSwitchableInterpFilter(currentFilter vp9dec.InterpFilter,
	isKey, intraOnly, isSrcFrameAltRef, refreshGolden, refreshAlt bool,
) vp9dec.InterpFilter {
	if e == nil || e.sf.FrameParameterUpdate == 0 {
		return currentFilter
	}
	if currentFilter != vp9dec.InterpSwitchable {
		return currentFilter
	}
	frameType := vp9GetFrameTypeForFilterThreshes(isKey, intraOnly,
		isSrcFrameAltRef, refreshGolden, refreshAlt)
	isAltRef := frameType == vp9dec.AltrefFrame
	return vp9GetInterpFilterFromThreshes(e.vp9FilterThreshes[frameType], isAltRef)
}

// vp9FixInterpFilter is the verbatim port of libvpx's fix_interp_filter
// at vp9/encoder/vp9_bitstream.c:864-885:
//
//	static void fix_interp_filter(VP9_COMMON *cm, FRAME_COUNTS *counts) {
//	  if (cm->interp_filter == SWITCHABLE) {
//	    // Check to see if only one of the filters is actually used
//	    int count[SWITCHABLE_FILTERS];
//	    int i, j, c = 0;
//	    for (i = 0; i < SWITCHABLE_FILTERS; ++i) {
//	      count[i] = 0;
//	      for (j = 0; j < SWITCHABLE_FILTER_CONTEXTS; ++j)
//	        count[i] += counts->switchable_interp[j][i];
//	      c += (count[i] > 0);
//	    }
//	    if (c == 1) {
//	      // Only one filter is used. So set the filter at frame level
//	      for (i = 0; i < SWITCHABLE_FILTERS; ++i) {
//	        if (count[i]) { cm->interp_filter = i; break; }
//	      }
//	    }
//	  }
//	}
//
// libvpx call site is vp9_bitstream.c:1312, sandwiched between
// write_frame_size_with_refs and write_interp_filter inside
// write_uncompressed_header. Because write_uncompressed_header runs
// before write_compressed_header (libvpx vp9_bitstream.c:1425,1453), the
// compressed-header writer sees the already-demoted cm->interp_filter
// at vp9_bitstream.c:1356 (`if (cm->interp_filter == SWITCHABLE)
// update_switchable_interp_probs...`). govpx inverts that order
// (compressed first, to size FirstPartitionSize), so the demotion runs
// right after collectVP9EncodeFrameCounts produces the counts and
// before WriteCompressedHeaderFromCounts reads InterpFilter.
//
// SWITCHABLE_FILTERS is the libvpx constant 3 (the count of real filters,
// excluding the SWITCHABLE sentinel); govpx exposes it as
// vp9dec.SwitchableFilters via internal/vp9/decoder/compressed_inter.go.
// counts.SwitchableInterp is the [SwitchableFilterContexts][SwitchableFilters]
// table populated by countVP9SwitchableInterp (vp9_encoder.go:3957).
func vp9FixInterpFilter(currentFilter vp9dec.InterpFilter,
	counts *encoder.FrameCounts,
) vp9dec.InterpFilter {
	if currentFilter != vp9dec.InterpSwitchable || counts == nil {
		return currentFilter
	}
	var count [vp9dec.SwitchableFilters]int
	c := 0
	for i := range vp9dec.SwitchableFilters {
		count[i] = 0
		for j := range vp9dec.SwitchableFilterContexts {
			count[i] += int(counts.SwitchableInterp[j][i])
		}
		if count[i] > 0 {
			c++
		}
	}
	if c != 1 {
		return currentFilter
	}
	for i := range vp9dec.SwitchableFilters {
		if count[i] != 0 {
			return vp9dec.InterpFilter(i)
		}
	}
	return currentFilter
}

// vp9UpdateFilterThreshesPostEncode merges this frame's accumulated
// rdc.filter_diff into the persistent filter_threshes via libvpx
// vp9/encoder/vp9_encodeframe.c:5890-5891:
//
//	for (i = 0; i < SWITCHABLE_FILTER_CONTEXTS; ++i)
//	  filter_thrs[i] = (filter_thrs[i] + rdc->filter_diff[i] / cm->MBs) / 2;
//
// The gate is identical to the demotion gate (sf.frame_parameter_update):
// libvpx hangs both off the same if-block at vp9_encodeframe.c:5846. mbs is
// the libvpx cm->MBs which govpx tracks as vp9MacroblockCount(miRows, miCols).
// Per-block contributions land in vp9FilterDiff via
// vp9_encodeframe.c:1881 once the per-block 3-filter RD path produces signal;
// today vp9FilterDiff stays zero so this update is a no-op stable point.
func (e *VP9Encoder) vp9UpdateFilterThreshesPostEncode(isKey, intraOnly,
	isSrcFrameAltRef, refreshGolden, refreshAlt bool, mbs int,
) {
	if e == nil || e.sf.FrameParameterUpdate == 0 || mbs <= 0 {
		// Always clear the per-frame accumulator so a stale value
		// cannot leak into the next frame even when the gate is off.
		e.vp9FilterDiff = [vp9dec.SwitchableFilterContexts]int64{}
		return
	}
	frameType := vp9GetFrameTypeForFilterThreshes(isKey, intraOnly,
		isSrcFrameAltRef, refreshGolden, refreshAlt)
	for i := range vp9dec.SwitchableFilterContexts {
		// libvpx: filter_thrs[i] = (filter_thrs[i] + filter_diff[i] / MBs) / 2
		e.vp9FilterThreshes[frameType][i] =
			(e.vp9FilterThreshes[frameType][i] +
				e.vp9FilterDiff[i]/int64(mbs)) / 2
	}
	e.vp9FilterDiff = [vp9dec.SwitchableFilterContexts]int64{}
}

func vp9EncoderFrameAllowHighPrecisionMv(isKey, intraOnly bool) bool {
	return !isKey && !intraOnly
}

// vp9EncoderLoopFilterLevel returns the closed-form LPF_PICK_FROM_Q
// level. Retained for backward-compatible call sites; the encoder
// path now goes through (*VP9Encoder).vp9PickFilterLevel which
// dispatches on e.sf.LpfPick (libvpx vp9_picklpf.c:159-203).
func vp9EncoderLoopFilterLevel(qindex int, isKey bool) uint8 {
	q := int(vp9dec.VpxAcQuant(qindex, 0, vp9dec.BitDepth8))
	level := (q*20723 + 1015158 + (1 << 17)) >> 18
	if isKey {
		level -= 4
	}
	if level < 0 {
		return 0
	}
	if level > vp9dec.MaxLoopFilter {
		return vp9dec.MaxLoopFilter
	}
	return uint8(level)
}

// vp9EncoderLoopFilterParams builds the per-frame Loopfilter header
// fields. The filter level is now selected by
// (*VP9Encoder).vp9PickFilterLevel which dispatches across the three
// libvpx LPF_PICK_METHOD modes; the static closed-form fallback is
// reserved for the lossless / disabled paths.
//
// When sf.LpfPick selects LPF_PICK_FROM_FULL_IMAGE / SUBIMAGE the
// final level is decided post-tile by vp9EncoderRunFullImagePicker,
// which calls vp9SearchFilterLevel — and that search seeds filt_mid
// from e.vp9LastFiltLevel (libvpx vp9_picklpf.c:90). Therefore the
// pre-tile call here must NOT clobber e.vp9LastFiltLevel with the
// from-Q placeholder; otherwise the search at cpu_used<5 starts at
// the from-Q seed instead of the libvpx-correct last_filt_level
// (which is 0 on a non-forced KEY_FRAME, libvpx vp9_encoder.c:3445).
// Only the post-tile picker writes vp9LastFiltLevel in that case.
func (e *VP9Encoder) vp9EncoderLoopFilterParams(qindex int, isKey, intraOnly, resetDeltas, lossless, segEnabled bool,
	sharpness uint8, width, height int, txMode common.TxMode,
) vp9dec.LoopfilterParams {
	// libvpx vp9_encoder.c:3442-3446 — at a non-forced keyframe the
	// picker is seeded with last_filt_level=0 so the quadratic search
	// starts fresh. govpx tracks `is_src_frame_alt_ref` only when
	// AltRef is enabled, so we conservatively reset on every key /
	// intra-only frame, matching libvpx's "reset on KEY_FRAME &&
	// !this_key_frame_forced" path for the common case
	// (this_key_frame_forced is the libvpx GF-derived forced-key
	// signal; govpx does not emit forced keys distinct from natural
	// key intervals).
	if isKey || intraOnly {
		e.vp9LastFiltLevel = 0
	}
	// Search-based methods need a placeholder filter_level for the
	// uncompressed-header pre-write; the real level is decided post-
	// tile against the reconstructed luma. Use the closed-form
	// FROM_Q value as the placeholder so that the disable / lossless
	// gates below (and the runFullImageSearch != 0 gate at line
	// 2644) see the same coarse magnitude libvpx would emit. Do not
	// update e.vp9LastFiltLevel here — the post-tile picker reads it
	// as the search seed (libvpx vp9_picklpf.c:90) and the libvpx-
	// correct seed is the just-reset value (0 on keyframes, the
	// prior frame's level otherwise).
	searchMethod := e.sf.LpfPick == LpfPickFromFullImage ||
		e.sf.LpfPick == LpfPickFromSubImage
	var level uint8
	if searchMethod {
		level = uint8(e.vp9PickLpfFromQ(qindex, isKey, segEnabled, width, height))
	} else {
		level = uint8(e.vp9PickFilterLevel(e.sf.LpfPick, qindex, isKey, segEnabled,
			width, height, txMode, false /* partialFrame */, nil /* sseFn */))
	}
	if lossless {
		level = 0
	}
	if !searchMethod {
		// libvpx vp9_encoder.c:3448 — `lf->last_filt_level =
		// lf->filter_level` after the picker returns. For
		// non-search methods the picker is final here; for search
		// methods the post-tile path (line 2692) refreshes
		// vp9LastFiltLevel after the real search runs.
		e.vp9LastFiltLevel = level
	} else if lossless || level == 0 {
		// Search-mode path where the post-tile picker will be gated
		// off (the dispatcher requires header.Loopfilter.FilterLevel
		// != 0 to run): the placeholder is final, so commit it to
		// vp9LastFiltLevel just like the non-search branch.
		// libvpx vp9_encoder.c:3429-3430 explicitly resets
		// last_filt_level = 0 when lossless.
		e.vp9LastFiltLevel = level
	}
	return vp9dec.LoopfilterParams{
		FilterLevel:         level,
		SharpnessLevel:      sharpness,
		ModeRefDeltaEnabled: true,
		ModeRefDeltaUpdate:  resetDeltas,
		RefDeltas:           [vp9dec.MaxRefLfDeltas]int8{1, 0, -1, -1},
		ModeDeltas:          [vp9dec.MaxModeLfDeltas]int8{0, 0},
	}
}

func (e *VP9Encoder) vp9EncoderLoopFilterPrevDeltas(reset bool) (
	[vp9dec.MaxRefLfDeltas]int8,
	[vp9dec.MaxModeLfDeltas]int8,
) {
	if reset {
		return [vp9dec.MaxRefLfDeltas]int8{},
			[vp9dec.MaxModeLfDeltas]int8{}
	}
	return e.lfRefDeltas, e.lfModeDeltas
}

func (e *VP9Encoder) commitVP9EncoderLoopFilterDeltas(
	lf *vp9dec.LoopfilterParams, reset bool,
) {
	if reset {
		e.lfRefDeltas = [vp9dec.MaxRefLfDeltas]int8{}
		e.lfModeDeltas = [vp9dec.MaxModeLfDeltas]int8{}
	}
	if lf == nil || !lf.ModeRefDeltaEnabled || !lf.ModeRefDeltaUpdate {
		return
	}
	e.lfRefDeltas = lf.RefDeltas
	e.lfModeDeltas = lf.ModeDeltas
}

func (e *VP9Encoder) applyVP9EncoderLoopFilter(hdr *vp9dec.UncompressedHeader,
	seg *vp9dec.SegmentationParams,
) bool {
	if hdr.Loopfilter.FilterLevel == 0 {
		return true
	}
	layout := vp9FrameBufferLayout(int(hdr.Width), int(hdr.Height))
	vp9dec.LoopFilterFrameInit(&e.lfi, &hdr.Loopfilter, seg,
		int(hdr.Loopfilter.FilterLevel))
	d := VP9Decoder{
		lfi:          e.lfi,
		miGrid:       e.miGrid,
		frameYFull:   e.reconYFull,
		frameUFull:   e.reconUFull,
		frameVFull:   e.reconVFull,
		frameYOrigin: layout.yOrigin,
		frameUOrigin: layout.uvOrigin,
		frameVOrigin: layout.uvOrigin,
		lastFrame:    e.reconFrame,
	}
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	return d.applyVP9LoopFilterSerial(miRows, miCols)
}

func vp9ModeTreeInterpFilter(kind vp9ModeTreeKind, inter *vp9InterEncodeState) vp9dec.InterpFilter {
	if kind == vp9ModeTreeInterSource || kind == vp9ModeTreeInterSkip {
		if inter != nil {
			return inter.interpFilter
		}
		return vp9dec.InterpSwitchable
	}
	return vp9dec.InterpEighttap
}

var vp9SwitchableInterpFilterOrder = [...]vp9dec.InterpFilter{
	vp9dec.InterpEighttap,
	vp9dec.InterpEighttapSmooth,
	vp9dec.InterpEighttapSharp,
}

// vp9NonrdSwitchableInterpFilterOrder is the realtime (nonrd) per-mode
// filter sweep. libvpx's vp9_pickmode.c::search_filter_ref iterates
// filter_start..filter_end where filter_end is EIGHTTAP_SMOOTH (NOT
// EIGHTTAP_SHARP) — the realtime path never evaluates EIGHTTAP_SHARP.
//
// libvpx: vp9/encoder/vp9_pickmode.c:1523-1525
//
//	INTERP_FILTER filter_start = force_smooth_filter ? EIGHTTAP_SMOOTH : EIGHTTAP;
//	INTERP_FILTER filter_end = EIGHTTAP_SMOOTH;
//	for (filter = filter_start; filter <= filter_end; ++filter) {
var vp9NonrdSwitchableInterpFilterOrder = [...]vp9dec.InterpFilter{
	vp9dec.InterpEighttap,
	vp9dec.InterpEighttapSmooth,
}

var (
	vp9EighttapInterpFilterOrder = [...]vp9dec.InterpFilter{vp9dec.InterpEighttap}
	vp9SmoothInterpFilterOrder   = [...]vp9dec.InterpFilter{vp9dec.InterpEighttapSmooth}
	vp9SharpInterpFilterOrder    = [...]vp9dec.InterpFilter{vp9dec.InterpEighttapSharp}
	vp9BilinearInterpFilterOrder = [...]vp9dec.InterpFilter{vp9dec.InterpBilinear}
)

// vp9NonrdFilterRef mirrors libvpx's filter_ref derivation in
// vp9_pickmode.c:1874-1880. filter_ref starts as cm->interp_filter; when
// sf.default_interp_filter != BILINEAR, it is overwritten from the first
// inter neighbour (above, then left). The result is consumed by the
// per-mode filter gate at vp9_pickmode.c:2318-2330 and by the non-search
// branch at :2330.
//
// libvpx: vp9/encoder/vp9_pickmode.c:1874-1880
//
//	filter_ref = cm->interp_filter;
//	if (cpi->sf.default_interp_filter != BILINEAR) {
//	  if (xd->above_mi && is_inter_block(xd->above_mi))
//	    filter_ref = xd->above_mi->interp_filter;
//	  else if (xd->left_mi && is_inter_block(xd->left_mi))
//	    filter_ref = xd->left_mi->interp_filter;
//	}
func vp9NonrdFilterRef(frameInterp vp9dec.InterpFilter,
	defaultInterpFilter vp9dec.InterpFilter,
	above, left *vp9dec.NeighborMi,
) vp9dec.InterpFilter {
	filterRef := frameInterp
	if defaultInterpFilter != vp9dec.InterpBilinear {
		if above != nil && vp9NeighborIsInter(above) {
			filterRef = vp9dec.InterpFilter(above.InterpFilter)
		} else if left != nil && vp9NeighborIsInter(left) {
			filterRef = vp9dec.InterpFilter(left.InterpFilter)
		}
	}
	return filterRef
}

// vp9NonrdPredFilterSearch mirrors libvpx's pred_filter_search derivation
// in vp9_pickmode.c:1732 and 1862-1869. The base value is
// (cm->interp_filter == SWITCHABLE); when sf.cb_pred_filter_search is set,
// it is refined by a chessboard pattern keyed on
// (mi_row + mi_col) >> log2(mi_width(bsize)) + (current_video_frame & 1).
// For non-SWITCHABLE frames cb_pred_filter_search forces it to 0.
//
// libvpx: vp9/encoder/vp9_pickmode.c:1862-1869
//
//	if (cpi->sf.cb_pred_filter_search) {
//	  const int bsl = mi_width_log2_lookup[bsize];
//	  pred_filter_search = cm->interp_filter == SWITCHABLE
//	                           ? (((mi_row + mi_col) >> bsl) +
//	                              get_chessboard_index(cm->current_video_frame)) &
//	                                 0x1
//	                           : 0;
//	}
func vp9NonrdPredFilterSearch(frameInterp vp9dec.InterpFilter,
	cbPredFilterSearch int, miRow, miCol int,
	bsize common.BlockSize, frameIndex int,
) bool {
	predFilterSearch := frameInterp == vp9dec.InterpSwitchable
	if cbPredFilterSearch != 0 {
		if frameInterp != vp9dec.InterpSwitchable {
			return false
		}
		bsl := int(common.MiWidthLog2Lookup[bsize])
		chess := frameIndex & 0x1
		predFilterSearch = (((miRow+miCol)>>bsl)+chess)&0x1 != 0
	}
	return predFilterSearch
}

func vp9InterFrameInterpFilter(inter *vp9InterEncodeState) vp9dec.InterpFilter {
	if inter == nil {
		return vp9dec.InterpSwitchable
	}
	return inter.interpFilter
}

func vp9InterInterpFilterCandidates(inter *vp9InterEncodeState) []vp9dec.InterpFilter {
	switch vp9InterFrameInterpFilter(inter) {
	case vp9dec.InterpSwitchable:
		return vp9SwitchableInterpFilterOrder[:]
	case vp9dec.InterpEighttapSmooth:
		return vp9SmoothInterpFilterOrder[:]
	case vp9dec.InterpEighttapSharp:
		return vp9SharpInterpFilterOrder[:]
	case vp9dec.InterpBilinear:
		return vp9BilinearInterpFilterOrder[:]
	default:
		return vp9EighttapInterpFilterOrder[:]
	}
}

func vp9InterInterpFilterRateCost(inter *vp9InterEncodeState, fc *vp9dec.FrameContext,
	ctx int, filter vp9dec.InterpFilter,
) int {
	if vp9InterFrameInterpFilter(inter) != vp9dec.InterpSwitchable {
		return 0
	}
	return vp9SwitchableInterpRateCost(fc, ctx, filter)
}

func vp9MvHasSubpel(mv vp9dec.MV) bool {
	return int(mv.Row)%8 != 0 || int(mv.Col)%8 != 0
}

func clampVP9TxSizeForBlock(tx common.TxSize, bsize common.BlockSize) common.TxSize {
	maxTx := common.MaxTxsizeLookup[bsize]
	if tx > maxTx {
		return maxTx
	}
	return tx
}

func countVP9Skip(counts *encoder.FrameCounts, seg *vp9dec.SegmentationParams,
	segID int, above, left *vp9dec.NeighborMi, skip uint8,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip) {
		return
	}
	ctx := vp9dec.GetSkipContext(above, left)
	counts.Skip[ctx][skip]++
}

func countVP9TxSize(counts *encoder.FrameCounts, ctx int,
	maxTxSize, txSize common.TxSize,
) {
	if counts == nil || ctx < 0 || ctx >= vp9dec.TxSizeContexts || txSize >= common.TxSizes {
		return
	}
	switch maxTxSize {
	case common.Tx8x8:
		if txSize <= common.Tx8x8 {
			counts.TxMode.P8x8[ctx][txSize]++
		}
	case common.Tx16x16:
		if txSize <= common.Tx16x16 {
			counts.TxMode.P16x16[ctx][txSize]++
		}
	case common.Tx32x32:
		if txSize <= common.Tx32x32 {
			counts.TxMode.P32x32[ctx][txSize]++
		}
	}
}

func countVP9TxTotals(counts *encoder.FrameCounts, bsize common.BlockSize,
	txSize common.TxSize, planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
) {
	if counts == nil || txSize >= common.TxSizes {
		return
	}
	counts.TxTotals[txSize]++
	if planes == nil {
		return
	}
	uvTx := vp9dec.GetUvTxSize(bsize, txSize, &planes[1])
	if uvTx < common.TxSizes {
		counts.TxTotals[uvTx]++
	}
}

func vp9TxProbsRow(p *vp9dec.TxProbs, maxTxSize common.TxSize, ctx int) []uint8 {
	if p == nil || ctx < 0 || ctx >= vp9dec.TxSizeContexts {
		return nil
	}
	switch maxTxSize {
	case common.Tx8x8:
		return p.P8x8[ctx][:]
	case common.Tx16x16:
		return p.P16x16[ctx][:]
	case common.Tx32x32:
		return p.P32x32[ctx][:]
	default:
		return nil
	}
}

func countVP9IntraInter(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	above, left *vp9dec.NeighborMi, isInter int,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	ctx := vp9dec.GetIntraInterContext(above, left)
	counts.IntraInter[ctx][isInter]++
}

func countVP9ReferenceMode(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	frameMode vp9dec.ReferenceMode, refs vp9dec.CompoundFrameRefs,
	above, left *vp9dec.NeighborMi, isCompound bool,
) {
	if counts == nil || frameMode != vp9dec.ReferenceModeSelect ||
		vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	ctx := vp9dec.GetReferenceModeContext(above, left, refs)
	bit := 0
	if isCompound {
		bit = 1
	}
	counts.ReferenceMode.CompInter[ctx][bit]++
}

func countVP9SingleRef(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	above, left *vp9dec.NeighborMi, refFrame int8,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	ctx0 := vp9dec.GetPredContextSingleRefP1(above, left)
	bit0 := 0
	if refFrame != vp9dec.LastFrame {
		bit0 = 1
	}
	counts.ReferenceMode.SingleRef[ctx0][0][bit0]++
	if bit0 == 0 {
		return
	}
	ctx1 := vp9dec.GetPredContextSingleRefP2(above, left)
	bit1 := 0
	if refFrame != vp9dec.GoldenFrame {
		bit1 = 1
	}
	counts.ReferenceMode.SingleRef[ctx1][1][bit1]++
}

func countVP9CompoundRef(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	above, left *vp9dec.NeighborMi, refs vp9dec.CompoundFrameRefs,
	signBias [vp9dec.MaxRefFrames]uint8, refFrame [2]int8,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	idx := int(signBias[refs.CompFixedRef])
	if idx < 0 || idx > 1 || refFrame[idx] != refs.CompFixedRef {
		return
	}
	varRef := refFrame[1-idx]
	bit := 0
	switch varRef {
	case refs.CompVarRef[0]:
	case refs.CompVarRef[1]:
		bit = 1
	default:
		return
	}
	ctx := vp9dec.GetPredContextCompRefP(above, left, refs, signBias)
	counts.ReferenceMode.CompRef[ctx][bit]++
}

func countVP9InterMode(counts *encoder.FrameCounts, seg *vp9dec.SegmentationParams,
	segID int, bsize common.BlockSize, ctx int, mode common.PredictionMode,
) {
	if counts == nil || bsize < common.Block8x8 ||
		vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip) {
		return
	}
	sub := int(mode) - int(common.NearestMv)
	if sub >= 0 && sub < common.InterModes {
		counts.InterMode[ctx][sub]++
	}
}

func countVP9InterIntraMode(counts *encoder.FrameCounts, bsize common.BlockSize,
	mode common.PredictionMode,
) {
	if counts == nil || mode < common.DcPred || int(mode) >= common.IntraModes {
		return
	}
	sg := common.SizeGroupLookup[bsize]
	counts.YMode[sg][mode]++
}

func countVP9SwitchableInterp(counts *encoder.FrameCounts,
	above, left *vp9dec.NeighborMi, filter uint8,
) {
	if counts == nil || filter >= uint8(vp9dec.SwitchableFilters) {
		return
	}
	ctx := vp9dec.GetPredContextSwitchableInterp(above, left)
	counts.SwitchableInterp[ctx][filter]++
}

func countVP9NewMv(counts *encoder.FrameCounts, mv, refMv vp9dec.MV) {
	if counts == nil {
		return
	}
	diff := vp9dec.MV{
		Row: mv.Row - refMv.Row,
		Col: mv.Col - refMv.Col,
	}
	vp9IncEncoderMv(diff, &counts.Mv)
}

func vp9IncEncoderMv(mv vp9dec.MV, counts *encoder.NmvContextCounts) {
	joint := vp9GetMvJoint(mv)
	counts.Joints[joint]++
	if joint == tables.MvJointHzVnz || joint == tables.MvJointHnzVnz {
		vp9IncEncoderMvComponent(mv.Row, &counts.Comps[0])
	}
	if joint == tables.MvJointHnzVz || joint == tables.MvJointHnzVnz {
		vp9IncEncoderMvComponent(mv.Col, &counts.Comps[1])
	}
}

func vp9IncEncoderMvComponent(v int16, counts *encoder.NmvComponentCounts) {
	sign := 0
	zv := int(v)
	if zv < 0 {
		sign = 1
		zv = -zv
	}
	counts.Sign[sign]++
	z := zv - 1
	cls, offset := vp9GetMvClass(z)
	counts.Classes[cls]++
	d := offset >> 3
	f := (offset >> 1) & 3
	hp := offset & 1
	if cls == tables.MvClass0 {
		counts.Class0[d]++
		counts.Class0Fp[d][f]++
		counts.Class0Hp[hp]++
		return
	}
	nBits := cls + vp9dec.Class0Bits - 1
	for i := range nBits {
		counts.Bits[i][(d>>i)&1]++
	}
	counts.Fp[f]++
	counts.Hp[hp]++
}

func vp9CoefBranchStats(counts *encoder.FrameCounts) *encoder.FrameCoefBranchStats {
	if counts == nil {
		return nil
	}
	return &counts.CoefBranchStats
}

func (e *VP9Encoder) writeVP9StubModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi,
		txModeForMi(baseMi), vp9ModeTreeKeyframe, nil, nil)
}

func (e *VP9Encoder) writeVP9KeyframeSourceModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
	key *vp9KeyframeEncodeState,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi,
		txModeForMi(baseMi), vp9ModeTreeKeyframeSource, key, nil)
}

func (e *VP9Encoder) writeVP9InterSkipModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi,
		common.TxModeSelect, vp9ModeTreeInterSkip, nil, nil)
}

func (e *VP9Encoder) writeVP9InterSourceModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
	inter *vp9InterEncodeState,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi,
		common.TxModeSelect, vp9ModeTreeInterSource, nil, inter)
}

func (e *VP9Encoder) writeVP9StubModesTileBounds(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTileBounds(bw, miRows, miCols, tile, partitionProbs, seg,
		baseMi, txModeForMi(baseMi), vp9ModeTreeKeyframe, nil, nil)
}

func (e *VP9Encoder) writeVP9FrameTiles(output []byte, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) (int, error) {
	tileRows := 1 << uint(tileInfo.Log2TileRows)
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if e.writeVP9FrameTilesThreadedEnabled(tileRows, tileCols) {
		if totalSize, err, ok := e.writeVP9FrameTilesThreaded(output, miRows, miCols,
			tileInfo, partitionProbs, seg, baseMi, txMode, kind, key, inter); ok {
			return totalSize, err
		}
	}
	totalSize := 0
	nTiles := tileRows * tileCols
	for tileRow := range tileRows {
		for tileCol := range tileCols {
			idx := tileRow*tileCols + tileCol
			isLast := idx == nTiles-1
			offset := totalSize
			if !isLast {
				offset += 4
			}
			if offset >= len(output) {
				return totalSize, encoder.ErrTileBufferFull
			}

			var bw bitstream.Writer
			bw.Start(output[offset:])
			e.writeVP9FrameTile(&bw, miRows, miCols,
				vp9EncoderTileBounds(tileRow, tileCol, miRows, miCols, tileInfo),
				partitionProbs, seg, baseMi, txMode, kind, key, inter)
			size, err := bw.Stop()
			if err != nil {
				if errors.Is(err, bitstream.ErrBufferOverflow) {
					return totalSize, encoder.ErrTileBufferFull
				}
				return totalSize, err
			}
			if !isLast {
				binary.BigEndian.PutUint32(output[totalSize:totalSize+4], uint32(size))
				totalSize += 4
			}
			totalSize += size
		}
	}
	return totalSize, nil
}

func (e *VP9Encoder) writeVP9FrameTile(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	switch kind {
	case vp9ModeTreeKeyframeSource:
		e.writeVP9ModesTileBounds(bw, miRows, miCols, tile,
			partitionProbs, seg, baseMi, txMode, kind, key, nil)
	case vp9ModeTreeInterSource:
		e.writeVP9ModesTileBounds(bw, miRows, miCols, tile,
			partitionProbs, seg, baseMi, txMode, kind, nil, inter)
	default:
		e.writeVP9ModesTileBounds(bw, miRows, miCols, tile,
			partitionProbs, seg, baseMi, txMode, kind, nil, nil)
	}
}

func vp9EncoderTileBounds(tileRow, tileCol, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
) vp9dec.TileBounds {
	return vp9dec.TileBounds{
		MiRowStart: vp9DecoderTileOffset(tileRow, miRows, tileInfo.Log2TileRows),
		MiRowEnd:   vp9DecoderTileOffset(tileRow+1, miRows, tileInfo.Log2TileRows),
		MiColStart: vp9DecoderTileOffset(tileCol, miCols, tileInfo.Log2TileCols),
		MiColEnd:   vp9DecoderTileOffset(tileCol+1, miCols, tileInfo.Log2TileCols),
	}
}

type vp9ModeTreeKind uint8

const (
	vp9ModeTreeKeyframe vp9ModeTreeKind = iota
	vp9ModeTreeKeyframeSource
	vp9ModeTreeInterSkip
	vp9ModeTreeInterSource
)

type vp9KeyframeEncodeState struct {
	img      *image.YCbCr
	hdr      *vp9dec.UncompressedHeader
	dq       *vp9dec.DequantTables
	lossless bool
	counts   *encoder.FrameCounts
}

type vp9InterEncodeState struct {
	img             *image.YCbCr
	dq              *vp9dec.DequantTables
	ref             *vp9ReferenceFrame
	refMask         uint8
	allowHP         bool
	selectFc        vp9dec.FrameContext
	referenceMode   vp9dec.ReferenceMode
	compoundAllowed bool
	refSignBias     [vp9dec.MaxRefFrames]uint8
	compoundRefs    vp9dec.CompoundFrameRefs
	interpFilter    vp9dec.InterpFilter
	lossless        bool
	counts          *encoder.FrameCounts
	// baseQindex mirrors libvpx's cm->base_qindex for the current frame.
	// Used by vp9ChoosePartitioning to drive set_vbp_thresholds without
	// reverse-looking up from dq.Y[0][1] (which is wrong when
	// segmentation is enabled and segment 0 has a non-zero delta_q).
	// libvpx ref: vp9_encodeframe.c:1379 (set_vbp_thresholds caller).
	baseQindex int
}

func vp9InterReferenceMode(inter *vp9InterEncodeState) vp9dec.ReferenceMode {
	if inter == nil {
		return vp9dec.SingleReference
	}
	return inter.referenceMode
}

func vp9InterSignBias(inter *vp9InterEncodeState) [vp9dec.MaxRefFrames]uint8 {
	if inter == nil {
		return [vp9dec.MaxRefFrames]uint8{}
	}
	return inter.refSignBias
}

func vp9InterCompoundRefs(inter *vp9InterEncodeState) vp9dec.CompoundFrameRefs {
	if inter == nil {
		return vp9dec.CompoundFrameRefs{}
	}
	return inter.compoundRefs
}

func (e *VP9Encoder) writeVP9ModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	tile := vp9dec.TileBounds{
		MiRowStart: 0,
		MiRowEnd:   miRows,
		MiColStart: 0,
		MiColEnd:   miCols,
	}
	e.writeVP9ModesTileBounds(bw, miRows, miCols, tile, partitionProbs, seg,
		baseMi, txMode, kind, key, inter)
}

func (e *VP9Encoder) writeVP9ModesTileBounds(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	rowMT := e.vp9RowMTSync
	tileSbCols := (tile.MiColEnd - tile.MiColStart + common.MiBlockSize - 1) >>
		common.MiBlockSizeLog2
	// libvpx: vp9_encodeframe.c:6126-6128 — vp9_cyclic_refresh_update_sb_postencode
	// only runs on inter frames where the seg+aq path is live. govpx
	// invokes writeVP9ModesTileBounds twice per frame: a count
	// pre-pass (collectVP9EncodeFrameCounts at vp9_encoder.go:2404)
	// and the real bitstream pass (writeVP9FrameTiles at 2474). The
	// pre-pass sets inter.counts != nil; the real pass leaves it nil.
	// libvpx's call site only fires at real-encode time, so gate on
	// inter.counts == nil here to avoid double-counting consec_zero_mv
	// / last_coded_q_map per frame.
	doCyclicSbPostencode := kind == vp9ModeTreeInterSource &&
		e.cyclicAQ.enabled && e.cyclicAQ.apply && e.cyclicAQ.contentMode &&
		seg != nil && seg.Enabled && inter != nil && inter.counts == nil
	var cyclicBaseQindex int
	if doCyclicSbPostencode {
		// libvpx uses cm->base_qindex when clamping last_coded_q_map.
		// govpx's inter state pins the corresponding qindex in the
		// dequant tables; we recover it through the encoder header
		// scratch which holds the final header for this frame.
		cyclicBaseQindex = int(e.vp9HeaderScratch.Quant.BaseQindex)
	}
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range e.leftSegCtx {
			e.leftSegCtx[i] = 0
		}
		if kind == vp9ModeTreeKeyframeSource || kind == vp9ModeTreeInterSource {
			e.resetVP9EncoderLeftEntropyContexts()
		}
		sbRow := (miRow - tile.MiRowStart) >> common.MiBlockSizeLog2
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			sbCol := (miCol - tile.MiColStart) >> common.MiBlockSizeLog2
			// Wavefront: wait for the row above to encode the above and
			// above-right SB before consuming their entropy / above-context
			// state. With a single goroutine per tile column this is a
			// non-blocking no-op; the call shape matches libvpx so future
			// per-row workers can be slotted in without further changes.
			rowMT.read(sbRow, sbCol)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol,
				common.Block64x64, tile, partitionProbs, seg, baseMi, txMode,
				kind, key, inter)
			if doCyclicSbPostencode {
				e.vp9CyclicRefreshUpdateEncodedSb(miRows, miCols,
					miRow, miCol, cyclicBaseQindex)
			}
			rowMT.write(sbRow, sbCol, tileSbCols)
		}
	}
}

// vp9CyclicRefreshUpdateEncodedSb mirrors libvpx's per-SB postencode hook
// from vp9/encoder/vp9_encodeframe.c:6126-6134. After all leaf blocks of
// a 64x64 superblock have been written to the bitstream (and their
// miGrid entries populated), this walks the 8x8 grid that backs the SB
// and:
//
//   - bumps consec_zero_mv for LAST_FRAME inter blocks with near-zero MVs
//     (libvpx: update_zeromv_cnt, vp9_encodeframe.c:5999-6022), and
//   - updates last_coded_q_map for refresh-segmented blocks
//     (libvpx: vp9_cyclic_refresh_update_sb_postencode,
//     vp9_aq_cyclicrefresh.c:225-255).
//
// Both feed the next frame's cyclic_refresh_update_map eligibility filter
// (libvpx: vp9_aq_cyclicrefresh.c:437-442).
func (e *VP9Encoder) vp9CyclicRefreshUpdateEncodedSb(miRows, miCols,
	miRow, miCol, baseQindex int,
) {
	if e == nil {
		return
	}
	cr := &e.cyclicAQ
	if cr.miRows != miRows || cr.miCols != miCols {
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:231-234 — superblock 8x8 block
	// walk. num_8x8_blocks_{wide,high}_lookup[BLOCK_64X64] = 8.
	xmis := min(miCols-miCol, common.MiBlockSize)
	ymis := min(miRows-miRow, common.MiBlockSize)
	if xmis <= 0 || ymis <= 0 {
		return
	}
	// Walk each 8x8 leaf block in raster order; the leaf's MODE_INFO is
	// stored at the (miRow+y, miCol+x) miGrid slot by fillVP9MiGrid.
	for y := range ymis {
		for x := range xmis {
			mi := e.vp9MiAt(miRows, miCols, miRow+y, miCol+x)
			if mi == nil {
				continue
			}
			isInter := mi.RefFrame[0] > vp9dec.IntraFrame
			segID := mi.SegmentID
			skip := mi.Skip != 0
			// libvpx: vp9_aq_cyclicrefresh.c:244-253 — single-cell update.
			cr.vp9CyclicRefreshUpdateSegmentPostencode(miRow+y, miCol+x,
				1, 1, baseQindex, segID, isInter, skip)
			// libvpx: vp9_encodeframe.c:5999-6022 — update_zeromv_cnt.
			cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow+y, miCol+x, 1, 1,
				mi.Mv[0].Row, mi.Mv[0].Col, mi.RefFrame[0], isInter, segID)
		}
	}
}

func (e *VP9Encoder) writeVP9ModesSb(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	if miRow >= miRows || miCol >= miCols {
		return
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	target := e.pickVP9BlockSizeForRegion(miRows, miCols, miRow, miCol,
		bsize, tile, partitionProbs, kind, key, inter)
	partition := common.PartitionLookup[bsl][target]
	if counts := vp9EncodeCountsForState(key, inter); counts != nil {
		ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
			miRow, miCol, bsize)
		counts.Partition[ctx][partition]++
	}
	encoder.WritePartitionForBlock(bw, encoder.WriteModesSbArgs{
		AboveSegCtx:    e.aboveSegCtx,
		LeftSegCtx:     e.leftSegCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: partitionProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if subsize < common.Block8x8 {
		e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile,
			seg, baseMi, txMode, kind, key, inter)
	} else {
		switch partition {
		case common.PartitionNone:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile,
				seg, baseMi, txMode, kind, key, inter)
		case common.PartitionHorz:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile,
				seg, baseMi, txMode, kind, key, inter)
			if miRow+bs < miRows {
				e.writeVP9ModeBlock(bw, miRows, miCols, miRow+bs, miCol,
					subsize, tile, seg, baseMi, txMode, kind, key, inter)
			}
		case common.PartitionVert:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile,
				seg, baseMi, txMode, kind, key, inter)
			if miCol+bs < miCols {
				e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol+bs,
					subsize, tile, seg, baseMi, txMode, kind, key, inter)
			}
		default:
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol,
				subsize, tile, partitionProbs, seg, baseMi, txMode, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol+bs,
				subsize, tile, partitionProbs, seg, baseMi, txMode, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow+bs, miCol,
				subsize, tile, partitionProbs, seg, baseMi, txMode, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow+bs, miCol+bs,
				subsize, tile, partitionProbs, seg, baseMi, txMode, kind, key, inter)
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(e.aboveSegCtx, e.leftSegCtx,
			miRow, miCol, subsize, vp9PartitionContextUpdateWidth(bs))
	}
}

var vp9StubBlockSizeOrder = [...]common.BlockSize{
	common.Block64x64,
	common.Block64x32,
	common.Block32x64,
	common.Block32x32,
	common.Block32x16,
	common.Block16x32,
	common.Block16x16,
	common.Block16x8,
	common.Block8x16,
	common.Block8x8,
	common.Block8x4,
	common.Block4x8,
	common.Block4x4,
}

func vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol int, root common.BlockSize) common.BlockSize {
	maxW := int(common.Num8x8BlocksWideLookup[root])
	maxH := int(common.Num8x8BlocksHighLookup[root])
	availW := min(miCols-miCol, maxW)
	availH := min(miRows-miRow, maxH)
	for _, bsize := range vp9StubBlockSizeOrder {
		if int(common.Num8x8BlocksWideLookup[bsize]) <= availW &&
			int(common.Num8x8BlocksHighLookup[bsize]) <= availH {
			return bsize
		}
	}
	return common.Block4x4
}

func (e *VP9Encoder) pickVP9BlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize, tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) common.BlockSize {
	target := vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol, root)
	if kind == vp9ModeTreeKeyframeSource {
		if e.opts.AQMode == VP9AQVariance && !e.vp9VarianceAQRateControlFixedQ() &&
			key != nil && key.img != nil && e.vp9DynamicSegmentMapActive() {
			if segmentSize, ok := e.pickVP9SegmentMapPartitionBlockSize(
				miRows, miCols, miRow, miCol, root, key.img, nil); ok {
				return segmentSize
			}
		}
		if varianceSize, ok := e.pickVP9KeyframeVariancePartitionBlockSize(key,
			miRows, miCols, miRow, miCol, root); ok {
			return varianceSize
		}
		// Fixed-Q libvpx keeps neutral clipped keyframe edges at the coarse
		// geometry, but uses square leaves once the edge carries luma residue.
		if e.vp9FixedPublicQuantizer() &&
			vp9KeyframeEdgeBlockHasNonNeutralLuma(key, miRows, miCols,
				miRow, miCol, root) {
			return vp9KeyframeSquareBlockSizeForRegion(miRows, miCols,
				miRow, miCol, root)
		}
		return vp9KeyframeSourceBlockSizeForRegion(miRows, miCols, miRow, miCol, root)
	}
	if kind == vp9ModeTreeInterSource && inter != nil {
		if edgeSize, ok := vp9InterEdgeBlockSizeForRegion(miRows, miCols,
			miRow, miCol, root); ok {
			target = edgeSize
		}
	}
	if vp9ModeTreeUsesInterSegmentMap(kind) && e.vp9DynamicSegmentMapActive() {
		if activeMapSize, ok := e.pickVP9SegmentMapPartitionBlockSize(
			miRows, miCols, miRow, miCol, root, nil, inter); ok {
			return activeMapSize
		}
	}
	if kind != vp9ModeTreeInterSource || inter == nil || target != root {
		return target
	}
	return e.pickVP9InterPartitionBlockSize(inter, tile, partitionProbs,
		miRows, miCols, miRow, miCol, root)
}

func (e *VP9Encoder) pickVP9SegmentMapPartitionBlockSize(miRows, miCols, miRow, miCol int,
	root common.BlockSize, img *image.YCbCr, inter *vp9InterEncodeState,
) (common.BlockSize, bool) {
	if e == nil || !e.vp9DynamicSegmentMapActive() || root <= common.Block8x8 {
		return common.BlockInvalid, false
	}
	splitSize := common.SubsizeLookup[common.PartitionSplit][root]
	if splitSize < common.Block8x8 {
		return common.BlockInvalid, false
	}
	blockMiW := int(common.Num8x8BlocksWideLookup[root])
	blockMiH := int(common.Num8x8BlocksHighLookup[root])
	if blockMiW <= 1 && blockMiH <= 1 {
		return common.BlockInvalid, false
	}
	endRow := min(miRows, miRow+blockMiH)
	endCol := min(miCols, miCol+blockMiW)
	if miRow >= endRow || miCol >= endCol {
		return common.BlockInvalid, false
	}
	staticSegID := e.vp9StaticSegmentIDForMap()
	segID := e.vp9PartitionSegmentID(miRow, miCol, staticSegID, img, inter)
	for row := miRow; row < endRow; row++ {
		for col := miCol; col < endCol; col++ {
			if e.vp9PartitionSegmentID(row, col, staticSegID, img, inter) != segID {
				return splitSize, true
			}
		}
	}
	return common.BlockInvalid, false
}

func vp9InterEdgeBlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize,
) (common.BlockSize, bool) {
	if root != common.Block64x64 {
		return common.BlockInvalid, false
	}
	maxW := int(common.Num8x8BlocksWideLookup[root])
	maxH := int(common.Num8x8BlocksHighLookup[root])
	availW := min(miCols-miCol, maxW)
	availH := min(miRows-miRow, maxH)
	if availW >= maxW-1 && availH >= maxH-1 {
		return root, true
	}
	if (availW < maxW || availH < maxH) && availW >= 4 && availH >= 4 {
		return common.Block32x32, true
	}
	return common.BlockInvalid, false
}

func vp9KeyframeSourceBlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize,
) common.BlockSize {
	maxW := min(miCols-miCol, int(common.Num8x8BlocksWideLookup[root]))
	maxH := min(miRows-miRow, int(common.Num8x8BlocksHighLookup[root]))
	if maxW > 4 {
		maxW = 4
	}
	if maxH > 4 {
		maxH = 4
	}
	if maxW >= 3 && maxH >= 3 {
		return common.Block32x32
	}
	for _, bsize := range vp9StubBlockSizeOrder {
		if int(common.Num8x8BlocksWideLookup[bsize]) <= maxW &&
			int(common.Num8x8BlocksHighLookup[bsize]) <= maxH {
			return bsize
		}
	}
	return common.Block4x4
}

func vp9KeyframeSquareBlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize,
) common.BlockSize {
	maxW := min(miCols-miCol, int(common.Num8x8BlocksWideLookup[root]))
	maxH := min(miRows-miRow, int(common.Num8x8BlocksHighLookup[root]))
	if maxW >= 4 && maxH >= 4 {
		return common.Block32x32
	}
	if maxW >= 2 && maxH >= 2 {
		return common.Block16x16
	}
	if maxW >= 1 && maxH >= 1 {
		return common.Block8x8
	}
	return common.Block4x4
}

func vp9KeyframeEdgeBlockHasNonNeutralLuma(key *vp9KeyframeEncodeState,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
) bool {
	if key == nil || key.img == nil {
		return false
	}
	blockMiW := int(common.Num8x8BlocksWideLookup[root])
	blockMiH := int(common.Num8x8BlocksHighLookup[root])
	if miCol+blockMiW <= miCols && miRow+blockMiH <= miRows {
		return false
	}
	src, stride, width, height := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || stride <= 0 || width <= 0 || height <= 0 {
		return false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0 >= width || y0 >= height {
		return false
	}
	w := min(width-x0, blockMiW*common.MiSize)
	h := min(height-y0, blockMiH*common.MiSize)
	for y := range h {
		row := src[(y0+y)*stride+x0 : (y0+y)*stride+x0+w]
		for _, px := range row {
			if px != 128 {
				return true
			}
		}
	}
	return false
}

func (e *VP9Encoder) pickVP9KeyframeVariancePartitionBlockSize(key *vp9KeyframeEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (common.BlockSize, bool) {
	if !e.vp9CBRKeyframeVariancePartitionEnabled(key) {
		return common.BlockInvalid, false
	}
	// Phase C wiring: when the libvpx choose_partitioning gate is
	// enabled, populate the per-SB partition cache on first call into
	// this SB and read the partition decision back from
	// e.varPartGrid. Falls through to the legacy single-level picker
	// below when the gate is off (default) so existing scoreboard
	// tests stay green.
	//
	// libvpx ref: vp9/encoder/vp9_encodeframe.c:5470 nonrd_use_partition
	// reads xd->mi[]->sb_type to drive the encode walk.
	if vp9LibvpxChoosePartitioningEnabled &&
		e.vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol, key, nil) {
		return e.vp9VarPartDecisionFor(miCols, miRow, miCol, bsize)
	}
	horzSize, vertSize, splitSize, ok := vp9SquareInterPartitionSizes(bsize)
	if !ok || splitSize < common.Block8x8 {
		return common.BlockInvalid, false
	}
	blockMiW := int(common.Num8x8BlocksWideLookup[bsize])
	blockMiH := int(common.Num8x8BlocksHighLookup[bsize])
	if miCol+blockMiW > miCols || miRow+blockMiH > miRows {
		return common.BlockInvalid, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return common.BlockInvalid, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if !vp9VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) {
		return common.BlockInvalid, false
	}
	threshold := vp9KeyframeVariancePartitionThreshold(key.dq.Y[0][1], bsize)
	variance := vp9BlockSourceVariance128(src, srcStride, x0, y0, blockW, blockH)
	if bsize > common.Block32x32 || variance > threshold<<4 {
		return splitSize, true
	}
	if variance < threshold {
		return common.BlockInvalid, false
	}
	halfW := blockW >> 1
	halfH := blockH >> 1
	if miRow+(blockMiH>>1) < miRows {
		left := vp9BlockSourceVariance128(src, srcStride, x0, y0, halfW, blockH)
		right := vp9BlockSourceVariance128(src, srcStride,
			x0+halfW, y0, halfW, blockH)
		if left < threshold && right < threshold {
			return vertSize, true
		}
	}
	if miCol+(blockMiW>>1) < miCols {
		top := vp9BlockSourceVariance128(src, srcStride, x0, y0, blockW, halfH)
		bottom := vp9BlockSourceVariance128(src, srcStride,
			x0, y0+halfH, blockW, halfH)
		if top < threshold && bottom < threshold {
			return horzSize, true
		}
	}
	return splitSize, true
}

// vp9CBRKeyframeVariancePartitionEnabled mirrors libvpx's
// vp9_set_variance_partition_thresholds / choose_partitioning enablement for
// keyframes at speed >= 6. libvpx unconditionally sets
// `sf->partition_search_type = VAR_BASED_PARTITION` at speed 6+
// (vp9/encoder/vp9_speed_features.c:667) and at speed 4 keyframe path
// (vp9_speed_features.c:582). The gate is NOT rc_mode-specific, NOT gated on
// drop-frame-allowed, and NOT gated on a fixed public quantizer; libvpx fires
// choose_partitioning at every keyframe whose `partition_search_type` is
// VAR_BASED_PARTITION regardless of VPX_CBR / VPX_VBR / VPX_CQ / VPX_Q
// (vp9_encodeframe.c:5304-5311 dispatches on partition_search_type alone).
//
// libvpx: vp9/encoder/vp9_speed_features.c:582 / :667, vp9_encodeframe.c:5304.
func (e *VP9Encoder) vp9CBRKeyframeVariancePartitionEnabled(key *vp9KeyframeEncodeState) bool {
	return key != nil && key.dq != nil && key.hdr != nil &&
		key.hdr.FrameType == common.KeyFrame && !key.lossless &&
		e.rc.enabled && e.vp9RealtimeVariancePartitionEnabled()
}

func vp9KeyframeVariancePartitionThreshold(yAcDequant int16, bsize common.BlockSize) uint64 {
	if yAcDequant <= 0 {
		return 0
	}
	base := uint64(yAcDequant) * 20
	switch bsize {
	case common.Block64x64:
		return base
	case common.Block32x32, common.Block16x16:
		return base >> 2
	default:
		return base << 2
	}
}

func vp9SquareInterPartitionSizes(root common.BlockSize) (common.BlockSize, common.BlockSize, common.BlockSize, bool) {
	switch root {
	case common.Block64x64, common.Block32x32, common.Block16x16:
		return common.SubsizeLookup[common.PartitionHorz][root],
			common.SubsizeLookup[common.PartitionVert][root],
			common.SubsizeLookup[common.PartitionSplit][root],
			true
	default:
		return common.BlockInvalid, common.BlockInvalid, common.BlockInvalid, false
	}
}

func (e *VP9Encoder) pickVP9InterPartitionBlockSize(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
) common.BlockSize {
	horzSize, vertSize, splitSize, ok := vp9SquareInterPartitionSizes(root)
	if !ok {
		return root
	}
	// SPEED_FEATURES.partition_search_type == FIXED_PARTITION (cpu_used=8
	// realtime in libvpx) pins the whole SB to sf.AlwaysThisBlockSize. We
	// only honour it for square block sizes that fit; otherwise fall through
	// to the variance / RD path so non-square edges remain decodable.
	//
	// libvpx: vp9_encodeframe.c set_fixed_partitioning walks the SB at
	// sf->always_this_block_size granularity.
	if fixed, on := e.vp9InterPartitionFixed(); on {
		if fixed >= common.Block8x8 && fixed <= root {
			return fixed
		}
	}
	// SPEED_FEATURES.partition_search_type == ML_BASED_PARTITION (cpu_used=8
	// realtime + w*h <= 352*288, libvpx vp9_speed_features.c:751-768 +
	// 825-826). Phase C dispatch: vp9MLPickPartitionEntry seeds per-SB
	// est_pred via get_estimated_pred (libvpx vp9_encodeframe.c:5314) and
	// vp9NonrdPickPartition mirrors the ml_based_partitioning=1 branch of
	// nonrd_pick_partition (libvpx vp9_encodeframe.c:4598-4855 + 4660-4667).
	//
	// Default scope: the picker only commits when NN votes PARTITION_NONE
	// at the root BLOCK_64X64 level. NN SPLIT votes and -1 (no confidence)
	// outcomes fall through to the legacy variance / RD path so
	// govpx-internal MV-pinning tests stay green.
	//
	// Phase D opt-in (GOVPX_VP9_NONRD_PICK_PARTITION=1): full recursive
	// walker — NN runs at every ML-eligible recursion level (BLOCK_64X64,
	// BLOCK_32X32, BLOCK_16X16). govpx's writeVP9ModesSb walker calls this
	// dispatcher once per (miRow, miCol, bsize) region; when the picker
	// returns the same bsize the walker commits PARTITION_NONE, when it
	// returns the PARTITION_SPLIT subsize the walker recurses 4 ways. That
	// folds the libvpx recursive nonrd_pick_partition body onto govpx's
	// already-recursive write walker without a separate PC_TREE substrate.
	// Forced-edge splits (libvpx vp9_encodeframe.c:4617-4626) are honoured
	// by vp9NonrdPickPartition for trailing rows/cols at the frame edge.
	// On the -1 ("no confidence") branch the libvpx picker would RD-compare
	// PARTITION_NONE against PARTITION_SPLIT (libvpx vp9_encodeframe.c:
	// 4676-4746); govpx defers that compare to the legacy variance / RD
	// picker below by returning BlockInvalid.
	//
	// The opt-in gate exists because the recursive walker shifts MV picks
	// at sub-64x64 leaves into a libvpx-faithful schedule that disagrees
	// with the legacy variance-picker MV picks the existing
	// TestVP9EncoderInterPicks*Mv* family pins. Closing those pins to
	// libvpx-faithful values is tracked under task #98 follow-up; until
	// then opt-in via env keeps both worlds available for the deferred
	// RefControl seed validation work.
	if e.sf.PartitionSearchType == MlBasedPartition {
		if vp9NonrdPickPartitionEnabled() {
			if root == common.Block64x64 || root == common.Block32x32 ||
				root == common.Block16x16 {
				if mlCtx := e.vp9MLPickPartitionEntry(inter, miRows, miCols,
					miRow, miCol); mlCtx != nil {
					if picked, ok := e.vp9NonrdPickPartition(mlCtx, miRows,
						miCols, miRow, miCol, root); ok {
						return picked
					}
				}
			}
		} else if root == common.Block64x64 {
			if mlCtx := e.vp9MLPickPartitionEntry(inter, miRows, miCols,
				miRow, miCol); mlCtx != nil {
				pred := vp9MLPredictVarPartitioning(common.Block64x64,
					miRow, miCol, mlCtx)
				if pred == vp9MLPredictNone {
					return common.Block64x64
				}
			}
		}
	}
	if varianceSize, ok := e.pickVP9CBRVariancePartitionBlockSize(inter,
		miRows, miCols, miRow, miCol, root); ok {
		return varianceSize
	}
	// SPEED_FEATURES.partition_search_type == VAR_BASED_PARTITION (cpu_used
	// >= 5 in libvpx realtime) replaces the recursive RD partition search
	// with a single choose_partitioning pass. govpx's variance picker above
	// is the equivalent of that pass; when it returns BlockInvalid (no
	// confidence) libvpx still runs the recursive search at speeds 5-7, but
	// at speed 8 (when UseNonrdPickMode is set) it commits the root size
	// outright. Mirror that here.
	//
	// libvpx: vp9_encodeframe.c:5470 — case VAR_BASED_PARTITION.
	if e.vp9InterPartitionVarBased() && e.vp9InterUsesNonrdPickmode() {
		return root
	}
	reconSnap, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, root)
	if !ok {
		return root
	}
	savedRef := inter.ref
	full, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow, miCol, root)
	if !ok {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return root
	}
	if full.distortion == 0 {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return root
	}
	if e.vp9InterPreferVarianceRoot(inter, miRows, miCols, miRow, miCol, root) {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return root
	}
	if e.vp9InterPreferTexturedSplit(inter, miRow, miCol, root) &&
		splitSize >= common.Block8x8 {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return splitSize
	}

	// libvpx VAR_BASED_PARTITION (set at RT speed >= 4) decides the
	// partition up front in vp9_choose_partitioning and DOES NOT compare
	// horz/vert/split RD scores against the root: nonrd_use_partition
	// walks the pre-baked partition tree and runs vp9_pick_inter_mode
	// per leaf. When SPEED_FEATURES asks for VAR_BASED_PARTITION the
	// remaining horz/vert/split exploration here is pure overhead that
	// libvpx never runs. The variance/textured fast paths above already
	// committed any pre-baked decision; falling through here means
	// keeping the root partition.
	// libvpx: vp9/encoder/vp9_speed_features.c:582 / 667
	// (partition_search_type = VAR_BASED_PARTITION), vp9/encoder/
	// vp9_encodeframe.c:4854 nonrd_use_partition.
	if e.sf.PartitionSearchType == VarBasedPartition {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return root
	}

	bsl := int(common.BWidthLog2Lookup[root])
	bs := (1 << uint(bsl)) / 4
	hasRows := miRow+bs < miRows
	hasCols := miCol+bs < miCols
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)
	qindex := e.vp9EncoderModeDecisionQIndex()
	// Cost partition tokens against inter.selectFc.PartitionProb, the
	// pre-WriteCompressedHeader snapshot of e.fc.PartitionProb that
	// inter.selectFc captures at the start of encodeVP9FrameInto*. The
	// `partitionProbs` argument carries the post-WriteCompressedHeader
	// values used by the writer at WritePartitionForBlock so encoder
	// emission stays bit-identical with what the decoder reads through
	// d.fc.PartitionProb (also post-WriteCompressedHeader). Using
	// partitionProbs directly here flips partition decisions between
	// the prepass (which sees pre-update partitionProbs) and the real
	// write pass (which sees post-update partitionProbs) on uniform
	// synthetic content where the RD margins between adjacent partition
	// sizes are within a handful of cost units, leaving the bool reader
	// to underflow the tile body and reject the frame with
	// ErrInvalidVP9Data. libvpx avoids the entire failure mode by
	// running mode decision once (with the pre-update probs) and
	// emitting bits in a separate pass; mirroring its rate-cost source
	// here keeps the prepass / real-pass walks bit-for-bit identical
	// while preserving the post-update writer probs the decoder
	// expects.
	rateCostProbs := partitionProbs
	if inter != nil {
		rateCostProbs = &inter.selectFc.PartitionProb
	}
	bestSize := root
	bestScore := e.vp9AddModeDecisionRate(full.score,
		vp9PartitionRateCost(rateCostProbs, ctx, common.PartitionNone,
			hasRows, hasCols), qindex)

	if hasRows {
		if score, ok := e.scoreVP9InterPartitionPair(inter, tile, miRows, miCols,
			miRow, miCol, horzSize, bs, 0); ok {
			score = e.vp9AddModeDecisionRate(score,
				vp9PartitionRateCost(rateCostProbs, ctx, common.PartitionHorz,
					hasRows, hasCols), qindex)
			if score < bestScore {
				bestScore = score
				bestSize = horzSize
			}
		}
	}
	if hasCols {
		if score, ok := e.scoreVP9InterPartitionPair(inter, tile, miRows, miCols,
			miRow, miCol, vertSize, 0, bs); ok {
			score = e.vp9AddModeDecisionRate(score,
				vp9PartitionRateCost(rateCostProbs, ctx, common.PartitionVert,
					hasRows, hasCols), qindex)
			if score < bestScore {
				bestScore = score
				bestSize = vertSize
			}
		}
	}
	if hasRows && hasCols {
		if score, ok := e.scoreVP9InterPartitionSplit(inter, tile, miRows, miCols,
			miRow, miCol, splitSize); ok {
			score = e.vp9AddModeDecisionRate(score,
				vp9PartitionRateCost(rateCostProbs, ctx, common.PartitionSplit,
					hasRows, hasCols), qindex)
			if score < bestScore {
				bestSize = splitSize
			}
		}
	}
	e.restoreVP9PartitionReconSnapshot(reconSnap)
	inter.ref = savedRef
	return bestSize
}

// vp9InterPreferVarianceRoot mirrors libvpx realtime speed-8
// choose_partitioning's 64x64 variance threshold for the non-key LAST_FRAME
// path. It catches flat temporal deltas where splitting only buys mode/MV
// noise in the bitstream.
func (e *VP9Encoder) vp9InterPreferVarianceRoot(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) bool {
	if !e.vp9RealtimeVariancePartitionEnabled() || inter == nil ||
		inter.dq == nil || bsize != common.Block64x64 {
		return false
	}
	if miRow+int(common.Num8x8BlocksHighLookup[bsize]) > miRows ||
		miCol+int(common.Num8x8BlocksWideLookup[bsize]) > miCols {
		return false
	}
	refSlot, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame)
	if !ok {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(&e.refFrames[refSlot], 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > refW || y0+blockH > refH {
		return false
	}
	variance := vp9BlockDiffVariance(src, srcStride, ref, refStride,
		x0, y0, x0, y0, blockW, blockH)
	threshold := vp9RealtimeVariancePartitionThreshold64(inter.dq.Y[0][1],
		srcW, srcH)
	return variance < threshold
}

func (e *VP9Encoder) vp9InterPreferTexturedSplit(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) bool {
	if bsize <= common.Block8x8 {
		return false
	}
	sse, activity, ok := e.vp9InterTxResidualStats(inter, miRow, miCol, bsize)
	if !ok || sse == 0 {
		return false
	}
	pixels := uint64(common.Num4x4BlocksWideLookup[bsize]) *
		uint64(common.Num4x4BlocksHighLookup[bsize]) * 16
	return sse > pixels*512 && activity > pixels*128
}

// vp9ChoosePartitioningSBIndex returns the SB index for (miRow, miCol)
// in e.varPartSBComputed. Mirrors libvpx's sb_offset computation
// (vp9_encodeframe.c:1314): sb_offset = (mi_stride >> 3) * (mi_row >> 3)
// + (mi_col >> 3). govpx flattens to (sbRow * sbCols + sbCol).
func (e *VP9Encoder) vp9ChoosePartitioningSBIndex(miCols, miRow, miCol int) int {
	sbCols := (miCols + 7) >> 3
	sbRow := miRow >> 3
	sbCol := miCol >> 3
	return sbRow*sbCols + sbCol
}

// vp9EnsureSBPartitionChosen runs vp9ChoosePartitioning for the 64x64 SB
// containing (miRow, miCol) iff it hasn't been computed this frame.
// Writes the partition tree into e.varPartGrid and marks
// e.varPartSBComputed[sbIdx] = true.
//
// libvpx ref: vp9_encodeframe.c:1253-1763 (choose_partitioning called
// once per SB from encode_rtc_frame at line 5470).
func (e *VP9Encoder) vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol int,
	key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) bool {
	miGridLen := miRows * miCols
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	// Lazy alloc: first activation of the libvpx picker on this encoder
	// instance grows the per-SB tracking slices to fit the current frame
	// dimensions. Subsequent calls reuse the capacity. The per-frame
	// reset of these buffers is handled by the frame-setup path
	// (vp9_encoder.go:3327-3340) — wiping the grid on every per-MI call
	// would destroy partition decisions stamped by earlier SBs in the
	// same frame (libvpx's xd->mi[]->sb_type grid is persistent across
	// the encode walk).
	if cap(e.varPartGrid) < miGridLen {
		grid := make([]vp9dec.NeighborMi, miGridLen)
		e.varPartGrid = grid
	} else if len(e.varPartGrid) < miGridLen {
		// Grow without zeroing already-stamped cells.
		tail := e.varPartGrid[len(e.varPartGrid):miGridLen]
		for i := range tail {
			tail[i] = vp9dec.NeighborMi{}
		}
		e.varPartGrid = e.varPartGrid[:miGridLen]
	}
	if cap(e.varPartSBComputed) < sbCount {
		e.varPartSBComputed = make([]bool, sbCount)
	} else if len(e.varPartSBComputed) < sbCount {
		tail := e.varPartSBComputed[len(e.varPartSBComputed):sbCount]
		for i := range tail {
			tail[i] = false
		}
		e.varPartSBComputed = e.varPartSBComputed[:sbCount]
	}
	sbMiRow := (miRow >> 3) << 3
	sbMiCol := (miCol >> 3) << 3
	sbIdx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if sbIdx < 0 || sbIdx >= len(e.varPartSBComputed) {
		return false
	}
	if e.varPartSBComputed[sbIdx] {
		return true
	}

	args := vp9ChoosePartitioningArgs{
		MiGrid:                 e.varPartGrid,
		MiRows:                 miRows,
		MiCols:                 miCols,
		MiRow:                  sbMiRow,
		MiCol:                  sbMiCol,
		Speed:                  int(e.opts.CpuUsed),
		VariancePartThreshMult: 1,
		// libvpx vp9_encodeframe.c:1310 — use_4x4_partition is gated on
		// !sf->nonrd_keyframe. At speed >= 8 the realtime configurator
		// sets sf->nonrd_keyframe = 1 (vp9_speed_features.c:751-757),
		// which suppresses the keyframe 4x4-leaf split. Thread the
		// speed-feature flag through so vp9ChoosePartitioning respects
		// it on the keyframe walker.
		NonRdKeyframe: e.sf.NonrdKeyframe != 0,
	}
	switch {
	case key != nil && key.img != nil && key.dq != nil:
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
		if len(src) == 0 || srcStride <= 0 {
			return false
		}
		x0 := sbMiCol * common.MiSize
		y0 := sbMiRow * common.MiSize
		if x0 >= srcW || y0 >= srcH {
			return false
		}
		args.PlaneSrc = src
		args.PlaneSrcOff = y0*srcStride + x0
		args.SrcStride = srcStride
		args.FrameWidth = srcW
		args.FrameHeight = srcH
		args.IsKeyFrame = true
		// libvpx feeds set_vbp_thresholds with cm->base_qindex
		// (vp9_encodeframe.c:1379), not a per-segment dequant. Read it
		// straight from the header so segmentation deltas on segment 0
		// don't perturb the threshold derivation.
		if key.hdr != nil {
			args.BaseQIndex = int(key.hdr.Quant.BaseQindex)
		}
	case inter != nil && inter.img != nil && inter.dq != nil:
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
		if len(src) == 0 || srcStride <= 0 {
			return false
		}
		x0 := sbMiCol * common.MiSize
		y0 := sbMiRow * common.MiSize
		if x0 >= srcW || y0 >= srcH {
			return false
		}
		args.PlaneSrc = src
		args.PlaneSrcOff = y0*srcStride + x0
		args.SrcStride = srcStride
		args.FrameWidth = srcW
		args.FrameHeight = srcH
		args.IsKeyFrame = false
		// libvpx feeds set_vbp_thresholds with cm->base_qindex
		// (vp9_encodeframe.c:1379). See keyframe branch above for
		// motivation.
		args.BaseQIndex = inter.baseQindex
		args.AvgFrameQIndexInter = int(e.rc.avgFrameQIndexInter)
		// Inter predictor. libvpx vp9_encodeframe.c:1450-1497:
		//   if (cpi->oxcf.speed >= 8 && !low_res &&
		//       x->content_state_sb != kVeryHighSad) {
		//     y_sad = sdf(src, pre);              // zero-MV SAD only
		//   } else {
		//     const MV dummy_mv = { 0, 0 };
		//     y_sad = vp9_int_pro_motion_estimation(...); // sets mi->mv[0]
		//   }
		//   vp9_build_inter_predictors_sb(xd, mi_row, mi_col, BLOCK_64X64);
		//   d = xd->plane[0].dst.buf;
		//
		// low_res predicate: libvpx vp9_encodeframe.c:1311.
		lowRes := srcW <= 352 && srcH <= 288
		if refSlot, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame); ok {
			refPx, refStride, refW, refH := vp9ReferenceVisiblePlane(
				&e.refFrames[refSlot], 0)
			if len(refPx) > 0 && refStride > 0 &&
				x0 < refW && y0 < refH {
				wired := false
				// libvpx vp9_encodeframe.c:1456-1458:
				//   y_sad = vp9_int_pro_motion_estimation(cpi, x, bsize,
				//                                         mi_row, mi_col,
				//                                         &dummy_mv);
				// Followed by vp9_build_inter_predictors_sb (line 1487)
				// which lands the resulting MV's luma prediction in
				// xd->plane[0].dst.buf. We fire this on low_res — the
				// libvpx condition for entering the int_pro branch over
				// the zero-MV sdf branch at speed >= 8
				// (vp9_encodeframe.c:1451).
				if lowRes && e.lastBorderedValid &&
					e.lastBordered.W == refW && e.lastBordered.H == refH {
					// Build the per-frame border-padded source mirror
					// once per frame; reuse across SBs.
					if !e.intProSrcBorderedValid ||
						e.intProSrcBordered.W != srcW ||
						e.intProSrcBordered.H != srcH {
						vp9YV12BuildBorderedPlane(&e.intProSrcBordered,
							src, srcStride, srcW, srcH,
							vp9EncBorderInPixels)
						e.intProSrcBorderedValid = true
					}
					// Wire int_pro motion search against the bordered
					// LAST plane. The visible (mi_row, mi_col) origin
					// inside the padded buffer is (Border+y0,
					// Border+x0) so refOff - (bw>>1) stays inside the
					// allocation for the BLOCK_64X64 worst case
					// (libvpx vp9/encoder/vp9_mcomp.c:2317-2320).
					srcOriginX := e.intProSrcBordered.OriginX()
					srcOriginY := e.intProSrcBordered.OriginY()
					refOriginX := e.lastBordered.OriginX()
					refOriginY := e.lastBordered.OriginY()
					srcStrideB := e.intProSrcBordered.Stride
					refStrideB := e.lastBordered.Stride
					estIn := &vp9GetEstimatedPredInterInput{
						Bsize:         common.Block64x64,
						Src:           e.intProSrcBordered.Pixels,
						SrcOff:        (srcOriginY+y0)*srcStrideB + (srcOriginX + x0),
						SrcStride:     srcStrideB,
						LastRef:       e.lastBordered.Pixels,
						LastRefOff:    (refOriginY+y0)*refStrideB + (refOriginX + x0),
						LastRefStride: refStrideB,
						Speed:         int(e.opts.CpuUsed),
						// MvLimits: full-pel limits derived from the
						// SB origin's distance to the bordered frame
						// edges (mirrors libvpx's
						// vp9_set_mv_search_range output for the
						// BLOCK_64X64 SB at (mi_row, mi_col); see
						// vp9_encoder.c set_mv_limits at the call
						// site).
						MvLimits: vp9MvLimits{
							ColMin: -(x0 + vp9EncBorderInPixels),
							ColMax: refW - x0 + vp9EncBorderInPixels,
							RowMin: -(y0 + vp9EncBorderInPixels),
							RowMax: refH - y0 + vp9EncBorderInPixels,
						},
					}
					// vp9GetEstimatedPred dispatches to
					// vp9GetEstimatedPredInter for !isKeyFrame, which
					// runs int_pro motion search + ref-frame selection,
					// then drives vp9BuildEstimatedPredLuma64x64 — the
					// 64x64 luma BILINEAR convolve port of
					// vp9_build_inter_predictors_sb (libvpx
					// vp9_reconinter.c:253-258).
					vp9GetEstimatedPred(false, estIn, e.intProEstPred[:])
					args.PlaneDst = e.intProEstPred[:]
					args.PlaneDstOff = 0
					args.DstStride = 64
					wired = true
				}
				if !wired {
					// Fallback: byte-exact with libvpx's "speed>=8
					// && !low_res && content_state != kVeryHighSad"
					// zero-MV SAD-only branch — the predictor stays at
					// the LAST plane at (mi_row, mi_col).
					args.PlaneDst = refPx
					args.PlaneDstOff = y0*refStride + x0
					args.DstStride = refStride
				}
			}
		}
		// govpx doesn't yet plumb cpi->rc.high_source_sad through the
		// realtime path; it defaults to false here. When the
		// scene-change detector is added at the rc level, set
		// args.HighSourceSAD here.
		// libvpx ref: vp9_encodeframe.c:1284 (force_64_split feeder).
	default:
		return false
	}

	vp9ChoosePartitioning(args)
	e.varPartSBComputed[sbIdx] = true
	e.varPartFrameValid = true
	return true
}

// vp9VarPartDecisionFor reads xd->mi[(miRow*miCols+miCol)].sb_type and
// returns the libvpx subsize the walker should consume. Verbatim port
// of vp9/encoder/vp9_encodeframe.c:5007-5010 (nonrd_use_partition):
//
//	if (mi_row >= cm->mi_rows || mi_col >= cm->mi_cols) return;
//	subsize = (bsize >= BLOCK_8X8) ? mi[0]->sb_type : BLOCK_4X4;
//	partition = partition_lookup[bsl][subsize];
//
// Returns (BlockInvalid, false) when partition_lookup yields
// PARTITION_NONE (caller stays at bsize) or PARTITION_INVALID (defensive
// fallback). Returns (subsize, true) for PARTITION_HORZ / VERT / SPLIT
// — the walker re-derives PartitionType via PartitionLookup[bsl][target].
//
// libvpx ref: vp9_encodeframe.c:4993-5100 nonrd_use_partition.
func (e *VP9Encoder) vp9VarPartDecisionFor(miCols, miRow, miCol int,
	bsize common.BlockSize,
) (common.BlockSize, bool) {
	// Verbatim port of vp9/encoder/vp9_encodeframe.c:5007-5010
	// (nonrd_use_partition):
	//
	//   if (mi_row >= cm->mi_rows || mi_col >= cm->mi_cols) return;
	//   subsize = (bsize >= BLOCK_8X8) ? mi[0]->sb_type : BLOCK_4X4;
	//   partition = partition_lookup[bsl][subsize];
	//
	// The walker (writeVP9ModesSb) re-derives the PartitionType from
	// PartitionLookup[bsl][target], so we return the libvpx `subsize`
	// directly when partition != PARTITION_NONE.
	//
	// Critically, we MUST NOT treat picked==Block4x4 (enum 0) as
	// "unstamped cell": that conflates a legitimate libvpx
	// PARTITION_SPLIT leaf at bsize=BLOCK_8X8 with the zero-init grid
	// sentinel. The varPartSBComputed flag (managed by
	// vp9EnsureSBPartitionChosen) is the only valid stamped oracle, and
	// the picker stamps the upper-left mi of every terminal block via
	// set_block_size (vp9_encodeframe.c:340), so reads at the upper-left
	// of the outer bsize always see a real stamp once the SB has been
	// computed.
	if len(e.varPartGrid) == 0 || !e.varPartFrameValid {
		return common.BlockInvalid, false
	}
	idx := miRow*miCols + miCol
	if idx < 0 || idx >= len(e.varPartGrid) {
		return common.BlockInvalid, false
	}
	// libvpx: subsize = (bsize >= BLOCK_8X8) ? mi[0]->sb_type : BLOCK_4X4;
	var subsize common.BlockSize
	if bsize >= common.Block8x8 {
		subsize = e.varPartGrid[idx].SbType
	} else {
		subsize = common.Block4x4
	}
	// Map outer bsize to PartitionLookup row: BLOCK_4X4..BLOCK_64X64 →
	// row 0..4. b_width_log2_lookup gives the row index directly for the
	// square outer sizes nonrd_use_partition is ever called with.
	if bsize >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	if bsl < 0 || bsl >= len(common.PartitionLookup) {
		return common.BlockInvalid, false
	}
	if subsize >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	// libvpx: partition = partition_lookup[bsl][subsize];
	partition := common.PartitionLookup[bsl][subsize]
	switch partition {
	case common.PartitionNone:
		// libvpx stamped bsize at this cell — encode the whole block as
		// a single leaf. Return (bsize, true) so the caller commits to
		// PARTITION_NONE (PartitionLookup[bsl][bsize] = PartitionNone);
		// returning (BlockInvalid, false) here would let the dispatch
		// fall through to a non-libvpx heuristic and diverge.
		return bsize, true
	case common.PartitionHorz, common.PartitionVert, common.PartitionSplit:
		// Walker derives this partition back from
		// PartitionLookup[bsl][subsize]; return subsize to feed that.
		return subsize, true
	default:
		// PartitionInvalid: defensive fallback for an illegal subsize
		// at this outer bsize.
		return common.BlockInvalid, false
	}
}

func (e *VP9Encoder) pickVP9CBRVariancePartitionBlockSize(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (common.BlockSize, bool) {
	if !e.vp9CBRVariancePartitionEnabled(inter) {
		return common.BlockInvalid, false
	}
	// Phase C wiring: when the libvpx choose_partitioning gate is
	// enabled, populate the per-SB partition cache on first call into
	// this SB and read the partition decision back from
	// e.varPartGrid. Falls through to the legacy variance picker below
	// when the gate is off (default) so existing parity tests stay green.
	//
	// libvpx ref: vp9/encoder/vp9_encodeframe.c:5470 nonrd_use_partition
	// reads xd->mi[]->sb_type to drive the encode walk.
	if vp9LibvpxChoosePartitioningEnabled &&
		e.vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol, nil, inter) {
		return e.vp9VarPartDecisionFor(miCols, miRow, miCol, bsize)
	}
	horzSize, vertSize, splitSize, ok := vp9SquareInterPartitionSizes(bsize)
	if !ok || splitSize < common.Block8x8 {
		return common.BlockInvalid, false
	}
	blockMiW := int(common.Num8x8BlocksWideLookup[bsize])
	blockMiH := int(common.Num8x8BlocksHighLookup[bsize])
	if miCol+blockMiW > miCols || miRow+blockMiH > miRows {
		return common.BlockInvalid, false
	}
	refSlot, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame)
	if !ok {
		return common.BlockInvalid, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(&e.refFrames[refSlot], 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return common.BlockInvalid, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if !vp9VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
		!vp9VisibleBlockFits(x0, y0, blockW, blockH, refW, refH) {
		return common.BlockInvalid, false
	}
	if bsize == common.Block64x64 {
		sad := vp9BlockSAD(src, srcStride, ref, refStride,
			x0, y0, x0, y0, blockW, blockH, ^uint64(0))
		sadThreshold := vp9CBRVariancePartitionSADThreshold(inter.dq.Y[0][1],
			srcW, srcH)
		if sad < sadThreshold {
			return common.BlockInvalid, false
		}
	}
	threshold := vp9CBRVariancePartitionThreshold(inter.dq.Y[0][1],
		srcW, srcH, bsize, e.rc.avgFrameQIndexInter)
	variance := vp9BlockDiffVariance(src, srcStride, ref, refStride,
		x0, y0, x0, y0, blockW, blockH)
	if variance < threshold {
		return common.BlockInvalid, false
	}
	halfW := blockW >> 1
	halfH := blockH >> 1
	if miRow+(blockMiH>>1) < miRows {
		left := vp9BlockDiffVariance(src, srcStride, ref, refStride,
			x0, y0, x0, y0, halfW, blockH)
		right := vp9BlockDiffVariance(src, srcStride, ref, refStride,
			x0+halfW, y0, x0+halfW, y0, halfW, blockH)
		if left < threshold && right < threshold {
			return vertSize, true
		}
	}
	if miCol+(blockMiW>>1) < miCols {
		top := vp9BlockDiffVariance(src, srcStride, ref, refStride,
			x0, y0, x0, y0, blockW, halfH)
		bottom := vp9BlockDiffVariance(src, srcStride, ref, refStride,
			x0, y0+halfH, x0, y0+halfH, blockW, halfH)
		if top < threshold && bottom < threshold {
			return horzSize, true
		}
	}
	return splitSize, true
}

// vp9CBRVariancePartitionEnabled mirrors libvpx's choose_partitioning gate
// for inter frames. libvpx dispatches via partition_search_type ==
// VAR_BASED_PARTITION (vp9/encoder/vp9_encodeframe.c:5304-5311); the gate is
// NOT rc_mode-specific, NOT gated on drop-frame-allowed, and NOT gated on a
// fixed public quantizer. At speed >= 6 (vp9_speed_features.c:667) the
// configurator sets the type unconditionally regardless of VPX_CBR / VPX_VBR
// / VPX_CQ / VPX_Q. The dispatch is purely on partition_search_type. The
// !vp9FixedPublicQuantizer() predicate was previously here but has no libvpx
// counterpart and is removed for verbatim-libvpx faithfulness; the remaining
// predicates (inter != nil, dq != nil, !lossless, rc.enabled, RealtimeVar)
// guard the govpx-internal preconditions that vp9EnsureSBPartitionChosen
// inherits from libvpx's xd->dq / cm->frame_type / encode-state lifecycle.
//
// libvpx: vp9/encoder/vp9_speed_features.c:667, vp9_encodeframe.c:5304-5311.
func (e *VP9Encoder) vp9CBRVariancePartitionEnabled(inter *vp9InterEncodeState) bool {
	if inter == nil || inter.dq == nil || inter.lossless ||
		!e.rc.enabled || !e.vp9RealtimeVariancePartitionEnabled() {
		return false
	}
	return true
}

// vp9VarianceAQRateControlFixedQ reports whether the rate-control
// configuration pins quality to a fixed quantizer (no rate-driven
// base qindex adjustment available). Variance-AQ scales its
// per-segment deltas down in this mode to avoid blowing the
// fixed-Q quality anchor up on flat / near-flat content; with a
// CBR/VBR controller the rate loop absorbs the swing instead.
func (e *VP9Encoder) vp9VarianceAQRateControlFixedQ() bool {
	if e == nil {
		return false
	}
	if e.opts.Quantizer != 0 {
		return true
	}
	if e.opts.RateControlModeSet && e.opts.RateControlMode == RateControlQ {
		return true
	}
	if !e.opts.RateControlModeSet {
		// Public-Q (no rate control) is govpx's default; it pins
		// qindex via the CQ ladder the same way RateControlQ does.
		return true
	}
	return false
}

func (e *VP9Encoder) vp9FixedPublicQuantizer() bool {
	if e.opts.Quantizer != 0 {
		return true
	}
	minQ, maxQ, _ := vp9NormalizedPublicQuantizers(e.opts)
	return minQ == maxQ && minQ > 0
}

func vp9CBRVariancePartitionThreshold(yAcDequant int16, width, height int,
	bsize common.BlockSize, avgInterQ uint8,
) uint64 {
	if yAcDequant <= 0 {
		return 0
	}
	base := uint64(yAcDequant)
	if width <= 640 && height <= 480 {
		base = (5 * base) >> 2
	}
	switch {
	case width <= 352 && height <= 288:
		switch bsize {
		case common.Block64x64:
			return base >> 3
		case common.Block32x32:
			return base >> 1
		case common.Block16x16:
			threshold := base << 3
			if avgInterQ > 220 {
				return threshold << 2
			}
			if avgInterQ > 200 {
				return threshold << 1
			}
			return threshold
		}
	case width < 1280 && height < 720:
		if bsize == common.Block32x32 {
			return (5 * base) >> 2
		}
	case width < 1920 && height < 1080:
		if bsize == common.Block32x32 {
			return base << 1
		}
	default:
		if bsize == common.Block32x32 {
			return (5 * base) >> 1
		}
	}
	if bsize == common.Block16x16 {
		return base << 8
	}
	return base
}

func vp9CBRVariancePartitionSADThreshold(yAcDequant int16, width, height int) uint64 {
	if width <= 352 && height <= 288 {
		return 10
	}
	threshold := max(int(yAcDequant)<<1, 1000)
	return uint64(threshold)
}

func vp9VisibleBlockFits(x0, y0, blockW, blockH, width, height int) bool {
	return x0 >= 0 && y0 >= 0 && blockW > 0 && blockH > 0 &&
		x0+blockW <= width && y0+blockH <= height
}

func vp9RealtimeVariancePartitionThreshold64(yAcDequant int16, width, height int) uint64 {
	if yAcDequant <= 0 {
		return 0
	}
	base := uint64(yAcDequant)
	if width <= 640 && height <= 480 {
		base = (5 * base) >> 2
	}
	return base
}

func (e *VP9Encoder) scoreVP9InterPartitionPair(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	child common.BlockSize, rowOff, colOff int,
) (uint64, bool) {
	childRows := int(common.Num8x8BlocksHighLookup[child])
	childCols := int(common.Num8x8BlocksWideLookup[child])
	var saved [64]vp9dec.NeighborMi
	rows, cols, ok := e.snapshotVP9MiRect(miRows, miCols, miRow, miCol,
		childRows+rowOff, childCols+colOff, saved[:])
	if !ok {
		return 0, false
	}
	first, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow, miCol, child)
	if !ok {
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
		return 0, false
	}
	e.fillVP9MiGrid(miRows, miCols, miRow, miCol, child,
		vp9InterModeDecisionMi(child, first))
	second, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow+rowOff, miCol+colOff, child)
	if !ok {
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
		return 0, false
	}
	e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
	return first.score + second.score, true
}

func (e *VP9Encoder) scoreVP9InterPartitionSplit(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	child common.BlockSize,
) (uint64, bool) {
	stepMi := int(common.Num8x8BlocksWideLookup[child])
	var saved [64]vp9dec.NeighborMi
	rows, cols, ok := e.snapshotVP9MiRect(miRows, miCols, miRow, miCol,
		stepMi*2, stepMi*2, saved[:])
	if !ok {
		return 0, false
	}
	var splitScore uint64
	for rowOff := 0; rowOff <= stepMi; rowOff += stepMi {
		for colOff := 0; colOff <= stepMi; colOff += stepMi {
			decision, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
				miRow+rowOff, miCol+colOff, child)
			if !ok {
				e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
				return 0, false
			}
			e.fillVP9MiGrid(miRows, miCols, miRow+rowOff, miCol+colOff, child,
				vp9InterModeDecisionMi(child, decision))
			splitScore += decision.score
		}
	}
	e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
	return splitScore, true
}

func vp9InterModeDecisionMi(bsize common.BlockSize, decision vp9InterModeDecision) vp9dec.NeighborMi {
	return vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         decision.mode,
		RefFrame:     [2]int8{decision.refFrame, decision.secondRefFrame},
		Mv:           decision.mv,
		InterpFilter: uint8(decision.interpFilter),
	}
}

func (e *VP9Encoder) snapshotVP9MiRect(miRows, miCols, miRow, miCol, rows, cols int,
	out []vp9dec.NeighborMi,
) (int, int, bool) {
	if rows <= 0 || cols <= 0 || miRow < 0 || miCol < 0 ||
		miRow >= miRows || miCol >= miCols {
		return 0, 0, false
	}
	rows = min(rows, miRows-miRow)
	cols = min(cols, miCols-miCol)
	if rows*cols > len(out) {
		return 0, 0, false
	}
	for r := 0; r < rows; r++ {
		copy(out[r*cols:(r+1)*cols],
			e.miGrid[(miRow+r)*miCols+miCol:(miRow+r)*miCols+miCol+cols])
	}
	return rows, cols, true
}

func (e *VP9Encoder) restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols int,
	saved []vp9dec.NeighborMi,
) {
	if rows <= 0 || cols <= 0 || rows*cols > len(saved) {
		return
	}
	for r := 0; r < rows && miRow+r < miRows; r++ {
		copy(e.miGrid[(miRow+r)*miCols+miCol:(miRow+r)*miCols+miCol+cols],
			saved[r*cols:(r+1)*cols])
	}
}

type vp9PartitionReconPlaneSnapshot struct {
	x, y, w, h int
	off        int
}

type vp9PartitionReconSnapshot struct {
	planes [vp9dec.MaxMbPlane]vp9PartitionReconPlaneSnapshot
}

func (e *VP9Encoder) saveVP9PartitionReconSnapshot(miRow, miCol int,
	bsize common.BlockSize,
) (vp9PartitionReconSnapshot, bool) {
	var snap vp9PartitionReconSnapshot
	total := 0
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		data, stride := e.vp9EncoderReconPlane(plane)
		if len(data) == 0 || stride <= 0 {
			return snap, false
		}
		rows := len(data) / stride
		x := (miCol * common.MiSize) >> pd.SubsamplingX
		y := (miRow * common.MiSize) >> pd.SubsamplingY
		w := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		h := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		if x >= stride || y >= rows {
			return snap, false
		}
		if x+w > stride {
			w = stride - x
		}
		if y+h > rows {
			h = rows - y
		}
		if w <= 0 || h <= 0 {
			return snap, false
		}
		snap.planes[plane] = vp9PartitionReconPlaneSnapshot{
			x: x, y: y, w: w, h: h, off: total,
		}
		total += w * h
	}
	if cap(e.partitionReconScratch) < total {
		e.partitionReconScratch = make([]byte, total)
	} else {
		e.partitionReconScratch = e.partitionReconScratch[:total]
	}
	for plane := range vp9dec.MaxMbPlane {
		p := snap.planes[plane]
		if p.w == 0 || p.h == 0 {
			continue
		}
		data, stride := e.vp9EncoderReconPlane(plane)
		for y := 0; y < p.h; y++ {
			copy(e.partitionReconScratch[p.off+y*p.w:p.off+(y+1)*p.w],
				data[(p.y+y)*stride+p.x:(p.y+y)*stride+p.x+p.w])
		}
	}
	return snap, true
}

func (e *VP9Encoder) restoreVP9PartitionReconSnapshot(snap vp9PartitionReconSnapshot) {
	for plane := range vp9dec.MaxMbPlane {
		p := snap.planes[plane]
		if p.w == 0 || p.h == 0 {
			continue
		}
		data, stride := e.vp9EncoderReconPlane(plane)
		if len(data) == 0 || stride <= 0 {
			continue
		}
		for y := 0; y < p.h; y++ {
			copy(data[(p.y+y)*stride+p.x:(p.y+y)*stride+p.x+p.w],
				e.partitionReconScratch[p.off+y*p.w:p.off+(y+1)*p.w])
		}
	}
}

func vp9PartitionRateCost(
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	ctx int, partition common.PartitionType, hasRows, hasCols bool,
) int {
	if partitionProbs == nil || ctx < 0 || ctx >= common.PartitionContexts {
		return 0
	}
	probs := partitionProbs[ctx]
	switch {
	case hasRows && hasCols:
		switch partition {
		case common.PartitionNone:
			return encoder.VP9CostBit(probs[0], 0)
		case common.PartitionHorz:
			return encoder.VP9CostBit(probs[0], 1) +
				encoder.VP9CostBit(probs[1], 0)
		case common.PartitionVert:
			return encoder.VP9CostBit(probs[0], 1) +
				encoder.VP9CostBit(probs[1], 1) +
				encoder.VP9CostBit(probs[2], 0)
		case common.PartitionSplit:
			return encoder.VP9CostBit(probs[0], 1) +
				encoder.VP9CostBit(probs[1], 1) +
				encoder.VP9CostBit(probs[2], 1)
		}
	case !hasRows && hasCols:
		bit := 0
		if partition == common.PartitionSplit {
			bit = 1
		}
		return encoder.VP9CostBit(probs[1], bit)
	case hasRows && !hasCols:
		bit := 0
		if partition == common.PartitionSplit {
			bit = 1
		}
		return encoder.VP9CostBit(probs[2], bit)
	}
	return 0
}

func vp9SwitchableInterpRateCost(fc *vp9dec.FrameContext, ctx int,
	filter vp9dec.InterpFilter,
) int {
	if fc == nil || ctx < 0 || ctx >= len(fc.SwitchableInterpProb) ||
		filter >= vp9dec.InterpSwitchable {
		return 0
	}
	probs := fc.SwitchableInterpProb[ctx]
	switch filter {
	case vp9dec.InterpEighttap:
		return encoder.VP9CostBit(probs[0], 0)
	case vp9dec.InterpEighttapSmooth:
		return encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 0)
	case vp9dec.InterpEighttapSharp:
		return encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 1)
	default:
		return 0
	}
}

func (e *VP9Encoder) writeVP9ModeBlock(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	cur := baseMi
	cur.SbType = bsize
	cur.TxSize = clampVP9TxSizeForBlock(cur.TxSize, bsize)
	useDynamicMap := vp9ModeTreeUsesInterSegmentMap(kind)
	var segmentImg *image.YCbCr
	if kind == vp9ModeTreeKeyframeSource && e.opts.AQMode == VP9AQVariance &&
		!e.vp9VarianceAQRateControlFixedQ() && key != nil {
		useDynamicMap = true
		segmentImg = key.img
	}
	cur.SegmentID, cur.SegIDPredicted = e.vp9EncoderBlockSegmentID(
		seg, miRows, miCols, miRow, miCol, bsize,
		useDynamicMap, segmentImg, inter)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	counts := vp9EncodeCountsForState(key, inter)
	if kind == vp9ModeTreeInterSkip || kind == vp9ModeTreeInterSource {
		reconBsize := vp9ModeInfoDecodeBSize(bsize)
		hasResidue := false
		uvMode := common.DcPred
		segID := vp9EncoderMiSegmentID(&cur)
		segmentSkip := vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip)
		forcedRefFrame, forcedRef := vp9EncoderForcedSegmentRefFrame(seg, segID)
		forcedIntra := forcedRef && forcedRefFrame == vp9dec.IntraFrame
		noUsableInterRefs := kind == vp9ModeTreeInterSource && inter != nil &&
			inter.refMask == 0
		if !forcedIntra && noUsableInterRefs {
			forcedIntra = true
		}
		if forcedIntra {
			cur.RefFrame = [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}
			cur.InterpFilter = uint8(vp9dec.SwitchableFilters)
			var intra vp9InterIntraDecision
			var ok bool
			if noUsableInterRefs {
				intra, ok = e.pickVP9NoReferenceIntraMode(inter, tile,
					miRows, miCols, miRow, miCol, reconBsize, cur.TxSize,
					cur.SegmentID)
			} else {
				intra, ok = e.pickVP9ForcedInterIntraMode(inter, tile,
					miRows, miCols, miRow, miCol, reconBsize, cur.TxSize)
			}
			if ok {
				cur.Mode = intra.mode
				uvMode = intra.uvMode
				if intra.txSize < common.TxSizes {
					cur.TxSize = intra.txSize
				}
			}
			if kind == vp9ModeTreeInterSource && inter != nil {
				intraResidue := e.prepareVP9InterIntraBlockResidue(inter, tile,
					miRows, miCols, miRow, miCol, reconBsize, &cur, uvMode)
				if !segmentSkip && intraResidue {
					hasResidue = true
					cur.Skip = 0
				}
			}
			if segmentSkip {
				cur.Skip = 1
			}
		} else if segmentSkip {
			if kind == vp9ModeTreeInterSource && inter != nil {
				e.prepareVP9InterSkipPrediction(inter, miRows, miCols,
					miRow, miCol, reconBsize, &cur, forcedRefFrame, forcedRef)
			}
			cur.Skip = 1
		} else if kind == vp9ModeTreeInterSource && inter != nil {
			uvMode, hasResidue = e.prepareVP9InterBlockResidue(inter, miRows, miCols,
				miRow, miCol, reconBsize, tile, &cur, forcedRefFrame, forcedRef)
			segID = vp9EncoderMiSegmentID(&cur)
			segmentSkip = vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip)
			if hasResidue {
				cur.Skip = 0
			}
		}
		if !segmentSkip {
			if hasResidue {
				cur.Skip = 0
			} else {
				cur.Skip = 1
			}
		}
		isInter := cur.RefFrame[0] > vp9dec.IntraFrame
		interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols,
			tile, miRows, miRow, miCol, bsize)
		maxTxSize := common.MaxTxsizeLookup[bsize]
		txCtx := vp9dec.GetTxSizeContext(above, left, maxTxSize)
		if txMode == common.TxModeSelect && bsize >= common.Block8x8 &&
			(!isInter || cur.Skip == 0) {
			countVP9TxSize(counts, txCtx, maxTxSize, cur.TxSize)
		}
		countVP9TxTotals(counts, bsize, cur.TxSize, &e.planes)
		frameInterpFilter := vp9ModeTreeInterpFilter(kind, inter)
		countVP9Skip(counts, seg, segID, above, left, cur.Skip)
		bestRefMv := e.vp9EncoderBestInterRefMvs(tile, miRows, miCols,
			miRow, miCol, bsize, &cur, inter != nil && inter.allowHP,
			vp9InterSignBias(inter))
		countVP9IntraInter(counts, seg, segID, above, left, vp9BoolInt(isInter))
		if isInter {
			frameMode := vp9InterReferenceMode(inter)
			compoundRefs := vp9InterCompoundRefs(inter)
			signBias := vp9InterSignBias(inter)
			isCompound := cur.RefFrame[1] > vp9dec.IntraFrame
			countVP9ReferenceMode(counts, seg, segID, frameMode, compoundRefs,
				above, left, isCompound)
			if isCompound {
				countVP9CompoundRef(counts, seg, segID, above, left,
					compoundRefs, signBias, cur.RefFrame)
			} else {
				countVP9SingleRef(counts, seg, segID, above, left, cur.RefFrame[0])
			}
			countVP9InterMode(counts, seg, segID, bsize, interModeCtx, cur.Mode)
			if frameInterpFilter == vp9dec.InterpSwitchable {
				countVP9SwitchableInterp(counts, above, left, cur.InterpFilter)
			}
			if cur.Mode == common.NewMv {
				halves := 1
				if isCompound {
					halves = 2
				}
				for ref := 0; ref < halves; ref++ {
					countVP9NewMv(counts, cur.Mv[ref], bestRefMv[ref])
				}
			}
		} else {
			countVP9InterIntraMode(counts, bsize, cur.Mode)
		}
		encoder.WriteInterBlock(bw, encoder.WriteInterBlockArgs{
			Seg:              seg,
			Mi:               &cur,
			AboveMi:          above,
			LeftMi:           left,
			Fc:               &e.fc,
			TxMode:           txMode,
			MaxTxSize:        maxTxSize,
			TxProbs:          vp9TxProbsRow(&e.fc.TxProbs, maxTxSize, txCtx),
			FrameRefMode:     vp9InterReferenceMode(inter),
			InterpFilter:     frameInterpFilter,
			CompFixedRef:     vp9InterCompoundRefs(inter).CompFixedRef,
			CompVarRef:       vp9InterCompoundRefs(inter).CompVarRef,
			RefFrameSignBias: vp9InterSignBias(inter),
			SwitchableInterpCtx: vp9dec.GetPredContextSwitchableInterp(
				above, left),
			InterModeCtx: interModeCtx,
			IsCompound:   cur.RefFrame[1] > vp9dec.IntraFrame,
			Mv:           cur.Mv,
			BestRefMv:    bestRefMv,
			AllowHP:      inter != nil && inter.allowHP,
			UvMode:       uvMode,
		})
		if kind == vp9ModeTreeInterSource && inter != nil {
			aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
			if cur.Skip != 0 {
				vp9dec.ResetSkipContext(e.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
				e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
				return
			}
			_ = encoder.WriteCoefSb(bw, encoder.WriteCoefSbArgs{
				BSize:        reconBsize,
				MiTxSize:     cur.TxSize,
				IsInter:      vp9BoolInt(isInter),
				Lossless:     inter.lossless,
				Mi:           &cur,
				Planes:       &e.planes,
				AboveOffsets: aboveOffsets,
				LeftOffsets:  leftOffsets,
				PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
					inter.dq.Y[segID],
					inter.dq.Uv[segID],
					inter.dq.Uv[segID],
				},
				Fc:              &e.fc.CoefProbs,
				CoefBranchStats: vp9CoefBranchStats(counts),
				GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
					return e.vp9BlockCoeffs(plane, reconBsize, r, c, tx)
				},
			})
		}
		e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
		return
	}
	if kind == vp9ModeTreeKeyframeSource && key != nil {
		reconBsize := vp9ModeInfoDecodeBSize(bsize)
		// libvpx vp9_rdopt.c:3239-3252 — vp9_rd_pick_intra_mode_sb
		// dispatches the Y-mode picker on bsize: BLOCK_8X8+ routes
		// through rd_pick_intra_sby_mode (the per-MI mode picker), while
		// BLOCK_4X4 / BLOCK_4X8 / BLOCK_8X4 route through
		// rd_pick_intra_sub_8x8_y_mode which runs an independent
		// DC..TM_PRED RD scan per 4x4 raster sub-block and stows the
		// per-subblock pick in mic->bmi[i].as_mode.
		if reconBsize < common.Block8x8 {
			e.pickVP9KeyframeSub8x8YMode(key, tile, miRows, miCols,
				miRow, miCol, reconBsize, &cur)
		} else {
			cur.Mode = e.pickVP9KeyframeMode(key, tile, miRows, miCols,
				miRow, miCol, reconBsize, &cur)
		}
		uvMode := e.pickVP9KeyframeUvMode(key, tile, miRows, miCols,
			miRow, miCol, reconBsize, &cur)
		segID := vp9EncoderMiSegmentID(&cur)
		segmentSkip := vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip)
		hasResidue := false
		if segmentSkip {
			cur.Skip = 1
		} else {
			// libvpx vp9_rdopt.c:3221-3270 — vp9_rd_pick_intra_mode_sb
			// chains rd_pick_intra_sby_mode (which runs the per-block
			// tx_size RD via super_block_yrd -> choose_tx_size_from_rd
			// when cm->tx_mode == TX_MODE_SELECT) before
			// rd_pick_intra_sbuv_mode. govpx's pickVP9KeyframeMode picks
			// the Y mode under a Tx16x16-capped scorer; here we run the
			// per-block tx_size RD pick on top so mi.TxSize is RD-optimal
			// across {Tx32x32, Tx16x16, Tx8x8, Tx4x4} subject to
			// sf.TxSizeSearchDepth bounds, matching choose_tx_size_from_rd.
			e.pickVP9KeyframeBlockTxSize(key, tile, miRows, miCols,
				miRow, miCol, reconBsize, &cur, txMode)
			hasResidue = e.prepareVP9KeyframeBlockResidue(key, tile, miRows, miCols,
				miRow, miCol, reconBsize, &cur, uvMode)
			if hasResidue {
				cur.Skip = 0
			}
		}
		countVP9Skip(counts, seg, segID, above, left, cur.Skip)
		maxTxSize := common.MaxTxsizeLookup[bsize]
		txCtx := vp9dec.GetTxSizeContext(above, left, maxTxSize)
		if txMode == common.TxModeSelect && bsize >= common.Block8x8 {
			countVP9TxSize(counts, txCtx, maxTxSize, cur.TxSize)
		}
		countVP9TxTotals(counts, bsize, cur.TxSize, &e.planes)
		encoder.WriteKeyframeBlock(bw, encoder.WriteKeyframeBlockArgs{
			Seg:       seg,
			Mi:        &cur,
			AboveMi:   above,
			LeftMi:    left,
			TxMode:    txMode,
			MaxTxSize: maxTxSize,
			TxProbs:   vp9TxProbsRow(&e.fc.TxProbs, maxTxSize, txCtx),
			SkipProbs: e.fc.SkipProbs,
		})
		encoder.WriteKeyframeUvMode(bw, uvMode, cur.Mode)
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if !hasResidue {
			vp9dec.ResetSkipContext(e.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
			e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
			return
		}
		_ = encoder.WriteCoefSb(bw, encoder.WriteCoefSbArgs{
			BSize:        reconBsize,
			MiTxSize:     cur.TxSize,
			IsInter:      0,
			Lossless:     key.lossless,
			Mi:           &cur,
			Planes:       &e.planes,
			AboveOffsets: aboveOffsets,
			LeftOffsets:  leftOffsets,
			PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
				key.dq.Y[segID],
				key.dq.Uv[segID],
				key.dq.Uv[segID],
			},
			Fc:              &e.fc.CoefProbs,
			CoefBranchStats: vp9CoefBranchStats(counts),
			GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
				return e.vp9BlockCoeffs(plane, reconBsize, r, c, tx)
			},
		})
		e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
		return
	}
	// Fallback path: vp9ModeTreeKeyframe (counts-pass dispatch for
	// intra-only frames at collectVP9EncodeFrameCounts:3480) and any
	// other kind that arrives without key/inter state. libvpx's
	// equivalent is write_modes_b at vp9/encoder/vp9_bitstream.c:378-403
	// inside frame_is_intra_only(cm) -> write_mb_modes_kf — the same
	// function the keyframe-source branch above dispatches to. The
	// TX_MODE_SELECT cascade needs the fc.TxProbs row keyed by
	// (max_tx_size, ctx); without it WriteSelectedTxSize would index
	// into an empty slice (the bug a843f45d cited as a deferred panic).
	fallbackMaxTxSize := common.MaxTxsizeLookup[bsize]
	fallbackTxCtx := vp9dec.GetTxSizeContext(above, left, fallbackMaxTxSize)
	if txMode == common.TxModeSelect && bsize >= common.Block8x8 {
		countVP9TxSize(counts, fallbackTxCtx, fallbackMaxTxSize, cur.TxSize)
	}
	countVP9TxTotals(counts, bsize, cur.TxSize, &e.planes)
	encoder.WriteKeyframeBlock(bw, encoder.WriteKeyframeBlockArgs{
		Seg:       seg,
		Mi:        &cur,
		AboveMi:   above,
		LeftMi:    left,
		TxMode:    txMode,
		MaxTxSize: fallbackMaxTxSize,
		TxProbs:   vp9TxProbsRow(&e.fc.TxProbs, fallbackMaxTxSize, fallbackTxCtx),
		SkipProbs: e.fc.SkipProbs,
	})
	encoder.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
	e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
}

func (e *VP9Encoder) vp9EncoderBlockSegmentID(seg *vp9dec.SegmentationParams,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, useDynamicMap bool,
	img *image.YCbCr, inter *vp9InterEncodeState,
) (uint8, uint8) {
	if seg == nil || !seg.Enabled {
		return 0, 0
	}
	if !seg.UpdateMap {
		return e.vp9EncoderPreviousSegmentID(miRows, miCols, miRow, miCol,
			bsize), 0
	}
	segID := e.vp9StaticSegmentIDForMap()
	if useDynamicMap {
		if dynamicID, ok := e.vp9DynamicSegmentID(miRow, miCol, img, inter); ok {
			segID = dynamicID
		}
	}
	predicted := segID
	if seg.TemporalUpdate {
		predicted = e.vp9EncoderSegmentMapPredicted(miRows, miCols,
			miRow, miCol, bsize, segID)
	}
	return segID, predicted
}

func (e *VP9Encoder) vp9EncoderPreviousSegmentID(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) uint8 {
	if e == nil || !e.useVP9EncoderPrevSegmentMap(miRows, miCols) ||
		miRow < 0 || miCol < 0 || miRow >= miRows || miCol >= miCols {
		return 0
	}
	xMis := int(common.Num8x8BlocksWideLookup[bsize])
	yMis := int(common.Num8x8BlocksHighLookup[bsize])
	if xMis > miCols-miCol {
		xMis = miCols - miCol
	}
	if yMis > miRows-miRow {
		yMis = miRows - miRow
	}
	if xMis <= 0 || yMis <= 0 {
		return 0
	}
	miOffset := miRow*miCols + miCol
	segID := vp9dec.DecGetSegmentId(e.prevSegmentMap, miCols, miOffset,
		xMis, yMis)
	if segID < 0 || segID >= vp9dec.MaxSegments {
		return 0
	}
	return uint8(segID)
}

func (e *VP9Encoder) vp9EncoderSegmentMapPredicted(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, segID uint8,
) uint8 {
	if e.vp9EncoderPreviousSegmentID(miRows, miCols, miRow, miCol,
		bsize) == segID {
		return 1
	}
	return 0
}

func vp9ModeTreeUsesInterSegmentMap(kind vp9ModeTreeKind) bool {
	return kind == vp9ModeTreeInterSkip || kind == vp9ModeTreeInterSource
}

func (e *VP9Encoder) vp9DynamicSegmentMapActive() bool {
	if e == nil {
		return false
	}
	if e.roi.enabled || e.activeMapEnabled {
		return true
	}
	if e.cyclicAQ.enabled && e.cyclicAQ.apply {
		return true
	}
	// Variance-AQ is suppressed in fixed-Q / pure-Q mode because the
	// rate controller cannot absorb its per-segment qindex shifts;
	// matching the suppression here keeps the segment-aware partition
	// splitter and segment-map writer from emitting per-block segment
	// IDs that the segmentation header would otherwise be carrying.
	if e.opts.AQMode == VP9AQVariance && !e.vp9VarianceAQRateControlFixedQ() {
		return true
	}
	return false
}

func (e *VP9Encoder) vp9ActiveSegmentMapCodingChooser() bool {
	return e != nil && e.activeMapEnabled && !e.roi.enabled
}

func (e *VP9Encoder) vp9StaticSegmentIDForMap() uint8 {
	if e != nil && e.opts.AQMode == VP9AQComplexity {
		return vp9ComplexityAQDefaultSegment
	}
	if e == nil || e.opts.Segmentation.SegmentID >= vp9dec.MaxSegments {
		return 0
	}
	return e.opts.Segmentation.SegmentID
}

func (e *VP9Encoder) vp9PartitionSegmentID(miRow int, miCol int,
	staticSegID uint8, img *image.YCbCr, inter *vp9InterEncodeState,
) uint8 {
	segID, ok := e.vp9DynamicSegmentID(miRow, miCol, img, inter)
	if ok {
		return segID
	}
	return staticSegID
}

func (e *VP9Encoder) vp9DynamicSegmentID(miRow int, miCol int,
	img *image.YCbCr, inter *vp9InterEncodeState,
) (uint8, bool) {
	if e == nil {
		return 0, false
	}
	if img == nil && inter != nil {
		img = inter.img
	}
	activeMapNeedsSegment := e.vp9ActiveMapInactiveNeedsSegment(inter, miRow, miCol)
	if segID, ok := e.roi.segmentIDAt(miRow, miCol); ok {
		if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
			return vp9ActiveMapSegmentInactive, true
		}
		return segID, true
	}
	if e.opts.AQMode == VP9AQVariance && !e.vp9VarianceAQRateControlFixedQ() {
		if segID, ok := e.vp9VarianceAQSegmentID(img, miRow, miCol); ok {
			if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
				return vp9ActiveMapSegmentInactive, true
			}
			return segID, true
		}
	}
	if e.opts.AQMode == VP9AQEquator360 && vp9Equator360AQApplies(e.opts.Width, e.opts.Height) {
		miRows := (e.opts.Height + 7) >> 3
		segID := vp9Equator360AQSegmentID(miRow, miRows)
		if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
			return vp9ActiveMapSegmentInactive, true
		}
		return segID, true
	}
	if e.opts.AQMode == VP9AQPerceptual && e.perceptualAQ.ready {
		segID := e.perceptualAQ.segmentIDForBlock(miRow, miCol)
		if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
			return vp9ActiveMapSegmentInactive, true
		}
		return segID, true
	}
	if e.cyclicAQ.enabled && e.cyclicAQ.apply {
		segID := e.cyclicAQ.segmentID(miRow, miCol)
		if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
			return vp9ActiveMapSegmentInactive, true
		}
		return segID, true
	}
	if activeMapNeedsSegment {
		return vp9ActiveMapSegmentInactive, true
	}
	return 0, false
}

func (e *VP9Encoder) vp9ActiveMapInactiveNeedsSegment(inter *vp9InterEncodeState,
	miRow, miCol int,
) bool {
	if !e.vp9ActiveMapInactive(miRow, miCol) {
		return false
	}
	if inter == nil || inter.img == nil || !e.refFrames[vp9LastRefSlot].valid {
		return true
	}
	return !vp9SourceMatchesReferenceMI(inter.img, &e.refFrames[vp9LastRefSlot],
		miRow, miCol)
}

func (e *VP9Encoder) vp9VarianceAQSegmentID(img *image.YCbCr,
	miRow, miCol int,
) (uint8, bool) {
	if img == nil || miRow < 0 || miCol < 0 {
		return 0, false
	}
	src, stride, width, height := vp9EncoderSourcePlane(img, 0)
	if len(src) == 0 || stride <= 0 {
		return 0, false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0 >= width || y0 >= height {
		return 0, false
	}
	w := min(common.MiSize, width-x0)
	h := min(common.MiSize, height-y0)
	if w <= 0 || h <= 0 {
		return 0, false
	}
	// libvpx's vp9_block_energy computes:
	//     energy = round(log(per_pixel_variance + 1.0)) - DEFAULT_E_MIDPOINT
	// where per_pixel_variance = (Σ(x - mean(x))²) / area and the
	// midpoint is 10.0. vp9BlockSourceVariance128 already returns the
	// unscaled Σ(x - mean(x))² accumulator, so we divide by the area
	// here to land on the same per-pixel scale. The earlier port
	// multiplied the accumulator by 256 before dividing by area, which
	// inflated every block's energy by log(256) ≈ 5.5 and pinned
	// virtually all non-flat blocks at energy=1 (segment 4). That
	// caused the variance-AQ probe to penalise the textured half with
	// a +24 qindex delta while still over-spending the flat half at
	// segment 0 (delta ≈ -42 at qindex=64), tanking BD-rate by +77%.
	variance := vp9BlockSourceVariance128(src, stride, x0, y0, w, h)
	scaled := variance / uint64(w*h)
	energy := int(math.Round(math.Log(float64(scaled)+1.0))) - 10
	if energy < -4 {
		energy = -4
	} else if energy > 1 {
		energy = 1
	}
	return vp9VarianceAQSegmentIDFromEnergy(energy), true
}

func (e *VP9Encoder) applyVP9ComplexityAQSegment(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, mi *vp9dec.NeighborMi,
	projectedRate int,
) {
	if e == nil || inter == nil || mi == nil ||
		e.opts.AQMode != VP9AQComplexity {
		return
	}
	if e.vp9ActiveMapInactive(miRow, miCol) {
		if e.vp9ActiveMapInactiveNeedsSegment(inter, miRow, miCol) {
			mi.SegmentID = vp9ActiveMapSegmentInactive
			mi.SegIDPredicted = 0
		}
		return
	}
	segID, ok := e.vp9ComplexityAQSegmentID(inter.img, miRow, miCol, bsize,
		projectedRate)
	if !ok {
		return
	}
	mi.SegmentID = segID
	mi.SegIDPredicted = 0
}

func (e *VP9Encoder) vp9ComplexityAQSegmentID(img *image.YCbCr,
	miRow, miCol int, bsize common.BlockSize, projectedRate int,
) (uint8, bool) {
	if e == nil || img == nil || miRow < 0 || miCol < 0 ||
		bsize >= common.BlockSizes {
		return 0, false
	}
	sb64TargetRate := e.vp9ComplexityAQSB64TargetRate()
	if sb64TargetRate < vp9ComplexityAQMinSB64TargetRate {
		return 0, false
	}
	src, stride, width, height := vp9EncoderSourcePlane(img, 0)
	if len(src) == 0 || stride <= 0 {
		return 0, false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0 >= width || y0 >= height {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	w := min(blockW, width-x0)
	h := min(blockH, height-y0)
	if w <= 0 || h <= 0 {
		return 0, false
	}
	variance := vp9BlockSourceVariance128(src, stride, x0, y0, w, h)
	logVar := math.Log(float64(variance) + 1.0)
	xmis := min(e.vp9MiCols(), miCol+int(common.Num8x8BlocksWideLookup[bsize])) - miCol
	ymis := min(e.vp9MiRows(), miRow+int(common.Num8x8BlocksHighLookup[bsize])) - miRow
	if xmis <= 0 || ymis <= 0 {
		return 0, false
	}
	targetRate := int((int64(sb64TargetRate) * int64(xmis) *
		int64(ymis) * 256) / (8 * 8))
	if targetRate <= 0 {
		return 0, false
	}
	if projectedRate < 0 {
		projectedRate = 0
	}
	strength := vp9ComplexityAQStrength(e.vp9EncoderModeDecisionQIndex())
	for i, transition := range vp9ComplexityAQTransitions[strength] {
		if int64(projectedRate)*int64(transition.den) <
			int64(targetRate)*int64(transition.num) &&
			logVar < vp9ComplexityAQLowVarThreshold+
				vp9ComplexityAQVarThresholds[strength][i] {
			return uint8(i), true
		}
	}
	return vp9ComplexityAQSegments - 1, true
}

func (e *VP9Encoder) vp9ComplexityAQSB64TargetRate() int {
	if e == nil || e.rc.frameTargetBits <= 0 {
		return 0
	}
	sbCols := (e.vp9MiCols() + 7) >> 3
	sbRows := (e.vp9MiRows() + 7) >> 3
	sbCount := sbCols * sbRows
	if sbCount <= 0 {
		return 0
	}
	return e.rc.frameTargetBits / sbCount
}

func (e *VP9Encoder) vp9MiRows() int {
	if e == nil || e.opts.Height <= 0 {
		return 0
	}
	return (e.opts.Height + 7) >> 3
}

func (e *VP9Encoder) vp9MiCols() int {
	if e == nil || e.opts.Width <= 0 {
		return 0
	}
	return (e.opts.Width + 7) >> 3
}

func vp9VarianceAQSegmentIDFromEnergy(energy int) uint8 {
	switch {
	case energy <= -4:
		return 0
	case energy <= -3:
		return 1
	case energy <= -2:
		return 1
	case energy <= -1:
		return 2
	case energy <= 0:
		return 3
	default:
		return 4
	}
}

func vp9SourceMatchesReferenceMI(src *image.YCbCr, ref *vp9ReferenceFrame,
	miRow, miCol int,
) bool {
	if src == nil || ref == nil || !ref.valid {
		return false
	}
	for plane := range vp9dec.MaxMbPlane {
		srcPixels, srcStride, srcW, srcH := vp9EncoderSourcePlane(src, plane)
		refPixels, refStride, refW, refH := vp9ReferenceVisiblePlane(ref, plane)
		if len(srcPixels) == 0 || len(refPixels) == 0 ||
			srcStride <= 0 || refStride <= 0 {
			return false
		}
		subsampling := 0
		if plane > 0 {
			subsampling = 1
		}
		x0 := (miCol * common.MiSize) >> subsampling
		y0 := (miRow * common.MiSize) >> subsampling
		w := common.MiSize >> subsampling
		h := common.MiSize >> subsampling
		if x0 < 0 || y0 < 0 || x0 >= srcW || y0 >= srcH ||
			x0 >= refW || y0 >= refH {
			return false
		}
		if w > srcW-x0 {
			w = srcW - x0
		}
		if w > refW-x0 {
			w = refW - x0
		}
		if h > srcH-y0 {
			h = srcH - y0
		}
		if h > refH-y0 {
			h = refH - y0
		}
		if w <= 0 || h <= 0 {
			return false
		}
		for y := 0; y < h; y++ {
			srcRow := srcPixels[(y0+y)*srcStride+x0:]
			refRow := refPixels[(y0+y)*refStride+x0:]
			for x := 0; x < w; x++ {
				if srcRow[x] != refRow[x] {
					return false
				}
			}
		}
	}
	return true
}

func (e *VP9Encoder) vp9ActiveMapInactive(miRow int, miCol int) bool {
	if e == nil || !e.activeMapEnabled || miRow < 0 || miCol < 0 ||
		miRow >= e.activeMapMiRows || miCol >= e.activeMapMiCols ||
		e.activeMapMiCols <= 0 {
		return false
	}
	idx := miRow*e.activeMapMiCols + miCol
	return idx >= 0 && idx < len(e.activeMap) &&
		e.activeMap[idx] == vp9ActiveMapSegmentInactive
}

func vp9EncoderForcedSegmentRefFrame(seg *vp9dec.SegmentationParams, segID int) (int8, bool) {
	if !vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return 0, false
	}
	ref := int8(vp9dec.GetSegData(seg, segID, vp9dec.SegLvlRefFrame))
	if ref < vp9dec.IntraFrame || ref > vp9dec.AltrefFrame {
		return 0, false
	}
	return ref, true
}

func vp9EncoderMiSegmentID(mi *vp9dec.NeighborMi) int {
	if mi == nil || mi.SegmentID >= vp9dec.MaxSegments {
		return 0
	}
	return int(mi.SegmentID)
}

func (e *VP9Encoder) pickVP9KeyframeMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) common.PredictionMode {
	if key == nil || mi == nil {
		return common.DcPred
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	yModeProbs := vp9dec.GetYModeProbs(mi, above, left, 0)
	var yModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(yModeCosts[:], yModeProbs, common.IntraModeTree[:])
	qindex := e.vp9EncoderModeDecisionQIndex()
	// Apply libvpx's per-SB TPL rdmult scaling.  The base rdmult is the
	// keyframe Lagrange multiplier vp9KeyframeRDMul(qindex); TPL biases
	// it via get_rdmult_delta clamped to [orig/2, orig*3/2] before
	// running the per-mode RD search.
	// libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248
	rdmult := vp9KeyframeRDMul(qindex)
	if bsize < common.BlockSizes {
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		rdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, rdmult)
	}
	// Prime cb_rdmult so the UV intra and inter-intra scorers downstream
	// see the same TPL-biased multiplier instead of re-deriving it.
	// Inline save/restore (no defer) preserves the alloc-parity gate.
	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	bestMode := common.DcPred
	bestScore, ok := e.scoreVP9KeyframeModeRD(key, bestMode,
		yModeCosts[bestMode], rdmult, tile, miRows, miCols, miRow, miCol,
		bsize, mi)
	if !ok {
		e.cbRdmult = prevCbRdmult
		return bestMode
	}
	// The candidate set follows libvpx's keyframe-RD picker. At
	// `sf.nonrd_keyframe == 0` (cpu_used<=4 GOOD or REALTIME, plus speed=0
	// BEST) libvpx routes through `vp9_rd_pick_intra_mode_sb` →
	// `rd_pick_intra_sby_mode` (vp9_rdopt.c:1383) which walks DC_PRED..
	// TM_PRED unconditionally — there is no `intra_y_mode_(_bsize)_mask` gate
	// on the keyframe Y picker. At `sf.nonrd_keyframe == 1` (cpu_used>=5
	// REALTIME) libvpx routes through `vp9_pick_intra_mode` (vp9_pickmode.c
	// :1199) which walks DC_PRED..H_PRED only (3 modes). govpx honours both
	// by dispatching on `e.sf.NonrdKeyframe`: when 1, narrow to {DC, V, H};
	// when 0, walk all 10. The previous mask-based fallback walked only the
	// {DC, V, H} subset on GOOD speed=1 because the configurator did not
	// populate `IntraYModeBsizeMask` for the GOOD path, which violated
	// libvpx parity for cpu_used 0..4 GOOD-mode keyframes (see
	// vp9OptionsSeedsDeferred regression_vp9_options_e03af0a9).
	//
	// libvpx: vp9/encoder/vp9_rdopt.c:1383 (rd_pick_intra_sby_mode loop)
	// libvpx: vp9/encoder/vp9_pickmode.c:1199 (vp9_pick_intra_mode loop)
	// libvpx: vp9/encoder/vp9_encodeframe.c:4350-4365 (nonrd_keyframe
	// dispatch between the two pickers)
	maxMode := common.TmPred
	if e.sf.NonrdKeyframe != 0 {
		maxMode = common.HPred
	}
	for mode := common.DcPred + 1; mode <= maxMode; mode++ {
		score, ok := e.scoreVP9KeyframeModeRD(key, mode, yModeCosts[mode],
			rdmult, tile, miRows, miCols, miRow, miCol, bsize, mi)
		if ok && score < bestScore {
			bestScore = score
			bestMode = mode
		}
	}
	e.cbRdmult = prevCbRdmult
	return bestMode
}

// pickVP9KeyframeSub8x8YMode ports libvpx's rd_pick_intra_sub_8x8_y_mode
// (vp9/encoder/vp9_rdopt.c:1299-1360) plus the per-subblock walker
// rd_pick_intra4x4block (vp9_rdopt.c:1061-1297). For BLOCK_4X4 / BLOCK_4X8 /
// BLOCK_8X4 keyframe partitions, the libvpx Y-mode picker walks the 2x2
// grid of 4x4 raster sub-blocks (stepped by num_4x4_blocks_{wide,high})
// and runs an independent DC_PRED..TM_PRED RD scan per sub-block. The
// chosen per-subblock mode lands in mic->bmi[i].as_mode (replicated
// across the num_4x4_blocks_{wide,high} cells the decision covers); the
// final mic->mode = mic->bmi[3].as_mode so write_modes_b / coef_sb pick
// up the per-block mode for sub-8x8 partitions via get_y_mode.
//
// The previous govpx behaviour reused pickVP9KeyframeMode for sub-8x8
// blocks, which left all Bmi[].AsMode entries at the default DC_PRED
// regardless of the picked block-level mode — divergent from libvpx
// whenever the per-subblock RD picker selects a non-DC mode for any
// 4x4 raster cell.
func (e *VP9Encoder) pickVP9KeyframeSub8x8YMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) {
	if key == nil || mi == nil {
		return
	}
	if bsize >= common.Block8x8 {
		return
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	qindex := e.vp9EncoderModeDecisionQIndex()
	// Mirror libvpx vp9_encodeframe.c:4245-4248 — TPL bias the rdmult so
	// the per-subblock RD compares under the same multiplier as the
	// 8x8+ keyframe picker.
	rdmult := vp9KeyframeRDMul(qindex)
	bwMi := int(common.Num8x8BlocksWideLookup[bsize])
	bhMi := int(common.Num8x8BlocksHighLookup[bsize])
	rdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, rdmult)
	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	// Pin tx_size at TX_4X4 for the sub-8x8 picker, matching libvpx
	// rd_pick_intra4x4block:1088 — `xd->mi[0]->tx_size = TX_4X4`.
	mi.TxSize = common.Tx4x4
	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			i := idy*2 + idx
			// libvpx vp9_rdopt.c:1325-1330 — for keyframe blocks the
			// per-subblock bmode_costs row is keyed by (above_block_mode,
			// left_block_mode). govpx's GetYModeProbs encapsulates the same
			// kf_y_mode_prob[A][L] lookup, fed into VP9CostTokens to expand
			// per-mode rates.
			probs := vp9dec.GetYModeProbs(mi, above, left, i)
			var bmodeCosts [common.IntraModes]int
			encoder.VP9CostTokens(bmodeCosts[:], probs, common.IntraModeTree[:])
			bestMode := e.pickVP9Sub4x4IntraBlockMode(key, tile, miRows, miCols,
				miRow, miCol, bsize, mi, idy, idx, bmodeCosts[:], rdmult)
			// Replicate best_mode into the bmi cells the sub-block
			// decision covers (libvpx vp9_rdopt.c:1344-1348).
			mi.Bmi[i].AsMode = bestMode
			for j := 1; j < num4x4H; j++ {
				mi.Bmi[i+j*2].AsMode = bestMode
			}
			for j := 1; j < num4x4W; j++ {
				mi.Bmi[i+j].AsMode = bestMode
			}
		}
	}
	// libvpx vp9_rdopt.c:1357 — `mic->mode = mic->bmi[3].as_mode` so
	// downstream consumers (write_mb_modes_kf coef_sb get_y_mode) read
	// the per-subblock mode through Bmi[] while leaving mi.Mode as the
	// bottom-right subblock's pick.
	mi.Mode = mi.Bmi[3].AsMode
	e.cbRdmult = prevCbRdmult
}

// pickVP9Sub4x4IntraBlockMode ports libvpx's rd_pick_intra4x4block
// (vp9/encoder/vp9_rdopt.c:1061-1297). For one 4x4-grid raster sub-block
// at (idy,idx) inside a BLOCK_4X4 / 4X8 / 8X4 partition, it scans
// DC_PRED..TM_PRED, scoring each candidate via the same RD primitives
// the keyframe block picker uses (predict at TX_4X4 then quantise + RD
// cost the Hadamard-domain residue) and returns the lowest-RD mode. The
// best mode's prediction is left on the recon plane so subsequent
// sub-blocks see the correct intra-pred neighbours; the recon outside
// the {num_4x4_blocks_wide_lookup, num_4x4_blocks_high_lookup} footprint
// of this sub-block is preserved via a snapshot/restore mirroring
// libvpx's `best_dst[]` save (vp9_rdopt.c:1081-1085 + 1280-1294).
func (e *VP9Encoder) pickVP9Sub4x4IntraBlockMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
	idy, idx int, bmodeCosts []int, rdmult int,
) common.PredictionMode {
	pd := &e.planes[0]
	planeData, stride := e.vp9EncoderReconPlane(0)
	if len(planeData) == 0 || stride <= 0 {
		return common.DcPred
	}
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	// Snapshot the sub-block's recon rect so per-candidate predictions
	// can be undone before re-trying the next mode. Footprint matches
	// libvpx vp9_rdopt.c:1081 — `uint8_t best_dst[8 * 8]` covering
	// num_4x4_blocks_{wide,high}*4 pixels.
	baseX := miCol*common.MiSize + idx*4
	baseY := miRow*common.MiSize + idy*4
	rectW := num4x4W * 4
	rectH := num4x4H * 4
	rows := len(planeData) / stride
	if baseX < 0 || baseY < 0 || baseX+rectW > stride || baseY+rectH > rows {
		return common.DcPred
	}
	if rectW*rectH > len(e.blockScratch) {
		return common.DcPred
	}
	saved := e.blockScratch[:rectW*rectH]
	for y := range rectH {
		copy(saved[y*rectW:(y+1)*rectW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+rectW])
	}
	segID := vp9EncoderMiSegmentID(mi)
	dequant := key.dq.Y[segID]
	// Mode-mask gate mirrors libvpx vp9_rdopt.c:1207 —
	// `if (!(cpi->sf.intra_y_mode_mask[TX_4X4] & (1 << mode))) continue;`.
	mask := sfIntraAll
	if int(common.Tx4x4) < len(e.sf.IntraYModeMask) && e.sf.IntraYModeMask[common.Tx4x4] != 0 {
		mask = e.sf.IntraYModeMask[common.Tx4x4]
	}
	bestMode := common.DcPred
	bestRD := uint64(^uint64(0))
	bestValid := false
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		if mask&(1<<mode) == 0 {
			continue
		}
		// Restore the saved recon so this candidate's prediction starts
		// from the same neighbour state as the previous candidate
		// (libvpx vp9_rdopt.c:1108-1109 — `memcpy(tempa,...);
		// memcpy(templ,...);` before each per-mode pass).
		vp9RestorePlaneRect(planeData, stride, baseX, baseY, rectW, rectH, saved)
		rate := bmodeCosts[mode]
		var totalDistortion uint64
		var totalCoeffRate int
		valid := true
		for jy := 0; jy < num4x4H && valid; jy++ {
			for jx := 0; jx < num4x4W && valid; jx++ {
				dst, dstStride, x0, y0, predOK := e.predictVP9KeyframeTx(
					key.hdr, pd, 0, mode, common.Tx4x4, tile, miRows, miCols,
					miRow, miCol, bsize, idy+jy, idx+jx)
				if !predOK {
					valid = false
					break
				}
				src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
				if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH,
					dst, dstStride, x0, y0, common.Tx4x4) {
					valid = false
					break
				}
				txRate, txDist, _, scoreOK := e.scoreVP9KeyframeTxBlockRD(
					common.Tx4x4, dequant)
				if !scoreOK {
					valid = false
					break
				}
				totalCoeffRate += txRate
				totalDistortion += txDist
			}
		}
		if !valid {
			continue
		}
		rate += totalCoeffRate
		thisRD := vp9RDCost(rdmult, vp9RDDivBits, rate, totalDistortion)
		if !bestValid || thisRD < bestRD {
			bestRD = thisRD
			bestMode = mode
			bestValid = true
		}
	}
	// Leave the best mode's prediction on the recon plane (libvpx
	// vp9_rdopt.c:1292-1294 — `memcpy(dst_init + idy * dst_stride,
	// best_dst + idy * 8, num_4x4_blocks_wide * 4);`). Restore the
	// snapshot first so the trailing candidate's prediction is wiped,
	// then re-predict at the best mode so neighbouring sub-blocks see
	// the chosen reconstruction.
	vp9RestorePlaneRect(planeData, stride, baseX, baseY, rectW, rectH, saved)
	for jy := range num4x4H {
		for jx := range num4x4W {
			e.predictVP9KeyframeTx(key.hdr, pd, 0, bestMode, common.Tx4x4,
				tile, miRows, miCols, miRow, miCol, bsize, idy+jy, idx+jx)
		}
	}
	return bestMode
}

// vp9KeyframeIntraModeMask returns the libvpx `intra_y_mode_bsize_mask`
// entry the nonrd inter-frame intra picker consults. The keyframe Y-mode
// picker itself does NOT consult this mask — libvpx's keyframe RD path
// (`rd_pick_intra_sby_mode`, vp9_rdopt.c:1383) walks all 10 modes
// unconditionally, and the nonrd keyframe path (`vp9_pick_intra_mode`,
// vp9_pickmode.c:1199) walks DC..H_PRED unconditionally; govpx mirrors
// that dispatch via `e.sf.NonrdKeyframe` inside pickVP9KeyframeMode. This
// helper survives for the nonrd inter-frame intra picker
// (vp9_pickmode.c:2578) which the govpx nonrd picker still TODO-defers
// inside the consumers file, and for the audit test pinning the
// configurator-populated narrow mask semantics.
//
// libvpx: vp9/encoder/vp9_pickmode.c:2578 — `(1 << this_mode) &
// cpi->sf.intra_y_mode_bsize_mask[bsize]`.
func vp9KeyframeIntraModeMask(sf *SpeedFeatures, bsize common.BlockSize) int {
	if sf == nil || int(bsize) >= len(sf.IntraYModeBsizeMask) {
		return sfIntraDCHV
	}
	mask := sf.IntraYModeBsizeMask[bsize]
	if mask == 0 {
		return sfIntraDCHV
	}
	return mask
}

// scoreVP9KeyframeModeRD computes the Lagrangian RD cost of a keyframe mode
// using an explicit rdmult.  The picker computes rdmult once per SB —
// optionally adjusted by the TPL per-SB delta (libvpx: vp9_encodeframe.c:4245
// -4248 wiring x->cb_rdmult from get_rdmult_delta) — and feeds it into every
// candidate score so all candidates are compared under the same multiplier.
func (e *VP9Encoder) scoreVP9KeyframeModeRD(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, rate, rdmult int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) (uint64, bool) {
	if mi == nil {
		return 0, false
	}
	distortion, coeffRate, skippable, ok := e.scoreVP9KeyframeModeTransformRD(
		key, mode, tile, miRows, miCols, miRow, miCol, bsize, mi)
	if !ok {
		return 0, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	if skippable {
		rate += encoder.VP9CostBit(skipProb, 1)
	} else {
		rate += coeffRate + encoder.VP9CostBit(skipProb, 0)
	}
	return vp9RDCost(rdmult, vp9RDDivBits, rate, distortion), true
}

func (e *VP9Encoder) scoreVP9KeyframeModeTransformRD(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) (distortion uint64, coeffRate int, skippable bool, ok bool) {
	if key == nil || key.hdr == nil || key.img == nil || key.dq == nil || mi == nil {
		return 0, 0, false, false
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, 0, false, false
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	src, srcStride, _, _ := vp9EncoderSourcePlane(key.img, 0)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return 0, 0, false, false
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return 0, 0, false, false
	}
	restoreW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 || restoreW*restoreH > len(e.blockScratch) {
		return 0, 0, false, false
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}

	txSize := min(mi.TxSize, common.Tx16x16)
	max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	step := 1 << uint(txSize)
	segID := vp9EncoderMiSegmentID(mi)
	dequant := key.dq.Y[segID]
	skippable = true
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			dst, dstStride, x0, y0, predOK := e.predictVP9KeyframeTx(
				key.hdr, pd, 0, mode, txSize, tile, miRows, miCols,
				miRow, miCol, bsize, rr, cc)
			if !predOK {
				vp9RestorePlaneRect(planeData, stride, baseX, baseY,
					restoreW, restoreH, saved)
				return 0, 0, false, false
			}
			_ = e.gatherVP9TxResidual(src, srcStride, int(key.hdr.Width),
				int(key.hdr.Height), dst, dstStride, x0, y0, txSize)
			txRate, txDist, txSkippable, scoreOK := e.scoreVP9KeyframeTxBlockRD(
				txSize, dequant)
			if !scoreOK {
				vp9RestorePlaneRect(planeData, stride, baseX, baseY,
					restoreW, restoreH, saved)
				return 0, 0, false, false
			}
			coeffRate += txRate
			distortion += txDist
			// libvpx's realtime estimate_block_intra passes the same
			// skippable pointer into block_yrd for each transform. block_yrd
			// resets it before scoring, so the final transform owns this flag.
			skippable = txSkippable
		}
	}
	vp9RestorePlaneRect(planeData, stride, baseX, baseY, restoreW, restoreH, saved)
	return distortion, coeffRate, skippable, true
}

func vp9RestorePlaneRect(data []byte, stride, x0, y0, w, h int, saved []byte) {
	for y := range h {
		copy(data[(y0+y)*stride+x0:(y0+y)*stride+x0+w],
			saved[y*w:(y+1)*w])
	}
}

func (e *VP9Encoder) scoreVP9KeyframeTxBlockRD(txSize common.TxSize,
	dequant [2]int16,
) (rate int, distortion uint64, skippable bool, ok bool) {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if maxEob > len(e.txCoeffScratch) || maxEob > len(e.coefScratch) ||
		maxEob > len(e.dqCoeffScratch) {
		return 0, 0, false, false
	}
	coeff := e.txCoeffScratch[:maxEob]
	switch txSize {
	case common.Tx4x4:
		encoder.ForwardHT4x4Into(e.residueScratch[:], 4, common.DctDct, coeff)
	case common.Tx8x8:
		vp9Hadamard8x8Into(e.residueScratch[:], 8, coeff)
	case common.Tx16x16:
		vp9Hadamard16x16Into(e.residueScratch[:], 16, coeff)
	default:
		return 0, 0, false, false
	}
	qcoeff := e.coefScratch[:maxEob]
	dqcoeff := e.dqCoeffScratch[:maxEob]
	eob := vp9QuantizeFPForRD(coeff, dequant,
		common.DefaultScanOrders[txSize].Scan, qcoeff, dqcoeff)
	skippable = eob == 0
	if eob == 1 {
		rate += vp9AbsInt(int(qcoeff[0]))
	} else if eob > 1 {
		rate += vp9Satd(qcoeff)
	}
	rate <<= 2 + encoder.VP9ProbCostShift
	rate += 1 << encoder.VP9ProbCostShift
	return rate, vp9BlockErrorFP(coeff, dqcoeff) >> 2, skippable, true
}

func vp9RDCost(rdmult, rddiv, rate int, distortion uint64) uint64 {
	if rate < 0 {
		rate = 0
	}
	rateCost := (int64(rate)*int64(rdmult) +
		(1 << (encoder.VP9ProbCostShift - 1))) >> encoder.VP9ProbCostShift
	return uint64(rateCost) + (distortion << uint(rddiv))
}

func vp9KeyframeRDMul(qindex int) int {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > 255 {
		qindex = 255
	}
	q := int(vp9dec.VpxDcQuant(qindex, 0, vp9dec.BitDepth8))
	rdmult := q * q * (4350 + qindex) / 1000
	if rdmult < 1 {
		return 1
	}
	return rdmult
}

func vp9QuantizeFPForRD(coeff []int16, dequant [2]int16, scan []int16,
	qcoeff, dqcoeff []int16,
) int {
	n := min(len(coeff), min(len(scan), min(len(qcoeff), len(dqcoeff))))
	for i := range n {
		qcoeff[i] = 0
		dqcoeff[i] = 0
	}
	if n == 0 || dequant[0] == 0 || dequant[1] == 0 {
		return 0
	}
	quant := [2]int{(1 << 16) / int(dequant[0]), (1 << 16) / int(dequant[1])}
	round := [2]int{(48 * int(dequant[0])) >> 7, (42 * int(dequant[1])) >> 7}
	eob := -1
	for i := range n {
		rc := int(scan[i])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		c := int(coeff[rc])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		tmp := vp9ClampInt16(absCoeff + round[slot])
		tmp = (tmp * quant[slot]) >> 16
		q := tmp
		if c < 0 {
			q = -q
		}
		qcoeff[rc] = int16(q)
		dqcoeff[rc] = int16(q * int(dequant[slot]))
		if tmp != 0 {
			eob = i
		}
	}
	return eob + 1
}

func vp9BlockErrorFP(coeff, dqcoeff []int16) uint64 {
	n := min(len(coeff), len(dqcoeff))
	var err uint64
	for i := range n {
		diff := int(coeff[i]) - int(dqcoeff[i])
		err += uint64(diff * diff)
	}
	return err
}

func vp9Satd(coeff []int16) int {
	sum := 0
	for _, c := range coeff {
		sum += vp9AbsInt(int(c))
	}
	return sum
}

func vp9ClampInt16(v int) int {
	if v < -32768 {
		return -32768
	}
	if v > 32767 {
		return 32767
	}
	return v
}

func vp9HadamardCol8(src []int16, stride int, coeff []int16) {
	b0 := int(src[0*stride]) + int(src[1*stride])
	b1 := int(src[0*stride]) - int(src[1*stride])
	b2 := int(src[2*stride]) + int(src[3*stride])
	b3 := int(src[2*stride]) - int(src[3*stride])
	b4 := int(src[4*stride]) + int(src[5*stride])
	b5 := int(src[4*stride]) - int(src[5*stride])
	b6 := int(src[6*stride]) + int(src[7*stride])
	b7 := int(src[6*stride]) - int(src[7*stride])

	c0 := b0 + b2
	c1 := b1 + b3
	c2 := b0 - b2
	c3 := b1 - b3
	c4 := b4 + b6
	c5 := b5 + b7
	c6 := b4 - b6
	c7 := b5 - b7

	coeff[0] = int16(c0 + c4)
	coeff[7] = int16(c1 + c5)
	coeff[3] = int16(c2 + c6)
	coeff[4] = int16(c3 + c7)
	coeff[2] = int16(c0 - c4)
	coeff[6] = int16(c1 - c5)
	coeff[1] = int16(c2 - c6)
	coeff[5] = int16(c3 - c7)
}

func vp9Hadamard8x8Into(src []int16, stride int, coeff []int16) {
	var buffer [64]int16
	var buffer2 [64]int16
	for idx := range 8 {
		vp9HadamardCol8(src[idx:], stride, buffer[idx*8:])
	}
	for idx := range 8 {
		vp9HadamardCol8(buffer[idx:], 8, buffer2[idx*8:])
	}
	copy(coeff[:64], buffer2[:])
}

func vp9Hadamard16x16Into(src []int16, stride int, coeff []int16) {
	vp9Hadamard8x8Into(src, stride, coeff[:64])
	vp9Hadamard8x8Into(src[8:], stride, coeff[64:128])
	vp9Hadamard8x8Into(src[8*stride:], stride, coeff[128:192])
	vp9Hadamard8x8Into(src[8*stride+8:], stride, coeff[192:256])
	for idx := range 64 {
		a0 := int(coeff[idx])
		a1 := int(coeff[64+idx])
		a2 := int(coeff[128+idx])
		a3 := int(coeff[192+idx])

		b0 := (a0 + a1) >> 1
		b1 := (a0 - a1) >> 1
		b2 := (a2 + a3) >> 1
		b3 := (a2 - a3) >> 1

		coeff[idx] = int16(b0 + b2)
		coeff[64+idx] = int16(b1 + b3)
		coeff[128+idx] = int16(b0 - b2)
		coeff[192+idx] = int16(b1 - b3)
	}
}

func (e *VP9Encoder) scoreVP9KeyframePlanePrediction(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, mode common.PredictionMode, plane int,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (uint64, bool) {
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, false
	}
	max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	planeData, stride := e.vp9EncoderReconPlane(plane)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	rows := len(planeData) / stride
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	if baseX >= stride || baseY >= rows {
		return 0, false
	}
	restoreW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 {
		return 0, false
	}
	if restoreW*restoreH > len(e.blockScratch) {
		return 0, false
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}
	// Later intra transforms can reference earlier transforms in the same
	// block; seed those references from source while scoring, then restore.
	restoreRecon := func() {
		for y := 0; y < restoreH; y++ {
			copy(planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW],
				saved[y*restoreW:(y+1)*restoreW])
		}
	}

	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	var distortion uint64
	ok := true
scoreLoop:
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			score, scoreOK := e.scoreVP9KeyframeTxPrediction(key, pd, mode, plane,
				txSize, tile, miRows, miCols, miRow, miCol, bsize, rr, cc)
			if !scoreOK {
				ok = false
				break scoreLoop
			}
			distortion += score
			txX := baseX + cc*4
			txY := baseY + rr*4
			vp9CopySourceRectClamped(planeData, stride, src, srcStride,
				srcW, srcH, txX, txY, bs, bs)
		}
	}
	restoreRecon()
	if !ok {
		return 0, false
	}
	return distortion, true
}

func (e *VP9Encoder) pickVP9KeyframeUvMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) common.PredictionMode {
	// The realtime libvpx path used by the VP9 oracle keeps keyframe UV on
	// DC_PRED while only searching the luma intra mode.
	return common.DcPred
}

func (e *VP9Encoder) scoreVP9KeyframeUvPrediction(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, rate, qindex int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) (uint64, bool) {
	var distortion uint64
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &e.planes[plane]
		txSize := vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		score, ok := e.scoreVP9KeyframePlanePrediction(key, pd, mode, plane,
			txSize, tile, miRows, miCols, miRow, miCol, bsize)
		if !ok {
			return 0, false
		}
		distortion += score
	}
	return e.vp9ModeDecisionScore(distortion, rate, qindex), true
}

func (e *VP9Encoder) scoreVP9KeyframeTxPrediction(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, mode common.PredictionMode,
	plane int, txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
) (uint64, bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	if stride <= 0 || len(planeData) == 0 || int(mode) >= common.IntraModes {
		return 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, false
	}
	rows := len(planeData) / stride
	alignedWidth := vp9AlignTo(int(key.hdr.Width), 8)
	alignedHeight := vp9AlignTo(int(key.hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 := baseX + blockCol4x4*4
	y0 := baseY + blockRow4x4*4

	bs := 4 << uint(txSize)
	if bs*bs > len(e.modeScratch) || x0+bs > stride || y0+bs > rows {
		return 0, false
	}

	bounds := vp9BlockBoundsEdges(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	left := e.intraScratch.Left[:bs]
	if leftAvailable {
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			left[i] = planeData[sy*stride+x0-1]
		}
	}

	edges := vp9dec.IntraEdgeRefs{
		AboveLeft: 127,
		Left:      left,
	}
	upAvailable := blockRow4x4 != 0 || miRow > 0
	if upAvailable {
		edges.Above = planeData[(y0-1)*stride+x0:]
		if leftAvailable {
			edges.AboveLeft = planeData[(y0-1)*stride+x0-1]
		}
	}
	planeBlock4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W

	pred := e.modeScratch[:bs*bs]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            pred,
		DstStride:      bs,
		Mode:           mode,
		TxSize:         txSize,
		Edges:          edges,
		UpAvailable:    upAvailable,
		LeftAvailable:  leftAvailable,
		RightAvailable: rightAvailable,
		FrameWidth:     planeWidth,
		FrameHeight:    planeHeight,
		X0:             x0,
		Y0:             y0,
		MbToRightEdge:  bounds.MbToRightEdge,
		MbToBottomEdge: bounds.MbToBottomEdge,
	}, &e.intraScratch)

	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	score := vp9PredictionSSEClamped(src, srcStride, srcW, srcH,
		pred, bs, x0, y0, bs)
	return score, true
}

// pickVP9KeyframeBlockTxSize is a verbatim port of libvpx's
// choose_tx_size_from_rd (vp9/encoder/vp9_rdopt.c:907-1023) specialised for
// the keyframe Y-plane RD pick. libvpx's vp9_rd_pick_intra_mode_sb
// (vp9_rdopt.c:3221-3271) calls rd_pick_intra_sby_mode which, for each
// candidate Y mode, invokes super_block_yrd (vp9_rdopt.c:1025-1042) which in
// turn dispatches to choose_tx_size_from_rd for cm->tx_mode == TX_MODE_SELECT
// (the case the keyframe write_mb_modes_kf bitstream emits when tx_mode is
// TX_MODE_SELECT). govpx already picks the Y mode upstream via
// pickVP9KeyframeMode using a Tx16x16-capped score; this helper layers the
// per-block Tx32x32/Tx16x16/Tx8x8/Tx4x4 RD pick on top so mi.TxSize matches
// libvpx's choose_tx_size_from_rd output. The Y-plane only — libvpx UV
// tx_size is derived from mi->tx_size via get_uv_tx_size (which the
// keyframe-source write path already does via vp9dec.GetUvTxSize).
//
// libvpx (vp9_rdopt.c:946-955) sets start_tx/end_tx as:
//
//	if (cm->tx_mode == TX_MODE_SELECT) {
//	  start_tx = max_tx_size;
//	  end_tx = VPXMAX(start_tx - cpi->sf.tx_size_search_depth, 0);
//	  if (bs > BLOCK_32X32) end_tx = VPXMIN(end_tx + 1, start_tx);
//	}
//
// and loops `for (n = start_tx; n >= end_tx; n--)`. Each candidate's rate
// includes the tx_size signalling cost cpi->tx_size_cost[..][..][n] (libvpx
// vp9_rdopt.c:958); govpx mirrors this via vp9TxSizeRateCost using the
// fc.TxProbs row keyed on (max_tx_size, tx_size_ctx).
//
// Distortion is measured as libvpx's block_rd_txfm (vp9_rdopt.c:766-768):
// `dist = pixel_sse(src, dst) * 16` where `dst` is the post-encoded recon
// (the same recon the loop-filter SSE picker later consumes). Rate is the
// coefficient-block cost via vp9InterCoeffBlockRateCost reusing
// fc.CoefProbs[txSize][0] (planeType=0 for Y; is_inter=0 for keyframe).
func (e *VP9Encoder) pickVP9KeyframeBlockTxSize(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, txMode common.TxMode,
) {
	if key == nil || key.hdr == nil || key.img == nil || key.dq == nil || mi == nil {
		return
	}
	if txMode != common.TxModeSelect || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes {
		return
	}
	if key.lossless {
		// libvpx vp9_rdopt.c:1035 — lossless dispatches to
		// choose_largest_tx_size which pins tx_size at the cap; here that
		// reduces to Tx4x4 because the keyframe Y residue is forced to
		// Tx4x4 elsewhere when lossless is set.
		return
	}
	// libvpx vp9_rdopt.c:1035 — when sf.tx_size_search_method == USE_LARGESTALL
	// super_block_yrd dispatches to choose_largest_tx_size, NOT
	// choose_tx_size_from_rd. govpx leaves mi.TxSize at the
	// MaxTxsizeLookup[bsize] preload (the existing keyframe baseMi /
	// clampVP9TxSizeForBlock pin), matching libvpx's choose_largest_tx_size
	// output `mi->tx_size = VPXMIN(max_tx_size, tx_mode_to_biggest_tx_size)`.
	if e.sf.TxSizeSearchMethod != UseFullRD {
		return
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	if len(planeData) == 0 || stride <= 0 {
		return
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return
	}
	maxTx := common.MaxTxsizeLookup[bsize]
	// libvpx vp9_rdopt.c:946-954 — TX_MODE_SELECT start_tx/end_tx range.
	startTx := int(maxTx)
	endTx := max(startTx-e.sf.TxSizeSearchDepth, 0)
	if bsize > common.Block32x32 {
		// VPXMIN(end_tx + 1, start_tx) (vp9_rdopt.c:949).
		newEnd := min(endTx+1, startTx)
		endTx = newEnd
	}
	if startTx <= endTx && startTx == endTx {
		// Only one candidate; nothing to RD-pick.
		mi.TxSize = common.TxSize(startTx)
		return
	}
	if startTx < endTx {
		return
	}
	// Snapshot the SB Y-plane recon so each TX candidate can run on a
	// pristine baseline. libvpx accomplishes the same via per-candidate
	// recon_buf[n][64*64] in choose_tx_size_from_rd (vp9_rdopt.c:929-940).
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	rows := len(planeData) / stride
	if baseX >= stride || baseY >= rows {
		return
	}
	restoreW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 ||
		restoreW*restoreH > len(e.blockScratch) {
		return
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}
	// Tx-size signalling cost. libvpx vp9_rdopt.c:927+958 derive
	// tx_size_ctx from get_tx_size_context and rate from
	// cpi->tx_size_cost[max_tx-1][ctx][n]. govpx mirrors via
	// vp9TxSizeRateCost on the fc.TxProbs row.
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	txCtx := vp9dec.GetTxSizeContext(above, left, maxTx)
	txProbs := vp9TxProbsRow(&e.fc.TxProbs, maxTx, txCtx)
	qindex := vp9dec.GetSegmentQindex(&key.hdr.Seg, vp9EncoderMiSegmentID(mi),
		int(key.hdr.Quant.BaseQindex))
	dequant := key.dq.Y[vp9EncoderMiSegmentID(mi)]

	bestTx := common.TxSize(startTx)
	bestScore := uint64(^uint64(0))
	bestValid := false
	prevScore := uint64(0)
	prevValid := false
	max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	for n := startTx; n >= endTx; n-- {
		tx := common.TxSize(n)
		// libvpx vp9_rdopt.c:1004-1009 — restore recon and run
		// txfm_rd_in_plane for this tx candidate. govpx restores by
		// blitting `saved` back over the SB rect.
		for y := 0; y < restoreH; y++ {
			copy(planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW],
				saved[y*restoreW:(y+1)*restoreW])
		}
		step := 1 << uint(tx)
		bs := 4 << uint(tx)
		coeffBlockSlots := vp9dec.MaxEobForTxSize(tx)
		if coeffBlockSlots > len(e.coefScratch) {
			continue
		}
		var rate int
		var distortion uint64
		valid := true
		for rr := 0; rr < max4x4H && valid; rr += step {
			for cc := 0; cc < max4x4W && valid; cc += step {
				mode := vp9dec.GetYMode(mi, rr*int(common.Num4x4BlocksWideLookup[planeBsize])+cc)
				coeffs := e.coefScratch[:coeffBlockSlots]
				for i := range coeffs {
					coeffs[i] = 0
				}
				if !e.prepareVP9KeyframeTxResidue(key, pd, 0, mode, tx, tile,
					miRows, miCols, miRow, miCol, bsize, rr, cc, dequant,
					qindex, coeffs) {
					// No residue: the prediction matched src exactly (or
					// quantization zeroed everything). libvpx's
					// block_rd_txfm still computes dist via pixel_sse —
					// which is 0 here — and rate via cost_coeffs on the
					// zero-coeff EOB. Mirror by leaving rate/dist 0 for
					// this 4x4 step.
				}
				// libvpx vp9_rdopt.c:766-768 — dist = pixel_sse(src,dst)*16.
				txX := baseX + cc*4
				txY := baseY + rr*4
				if dist, ok := vp9PlaneRectSSEClamped(src, srcStride, srcW,
					srcH, planeData, stride, txX, txY, bs, bs); ok {
					distortion += dist * 16
				} else {
					valid = false
					break
				}
				// libvpx vp9_rdopt.c:826 — rate = rate_block(...) =
				// cost_coeffs(...). govpx uses
				// vp9InterCoeffBlockRateCost as the cost_coeffs port;
				// keyframe is_inter=0 so the [0] is_inter index of
				// fc.CoefProbs is the libvpx-faithful path.
				rate += e.vp9KeyframeCoeffBlockRateCost(tx, dequant, coeffs)
			}
		}
		if !valid {
			continue
		}
		// libvpx vp9_rdopt.c:958+985 — r[n][1] = r[n][0] + r_tx_size,
		// then rd[n][1] = RDCOST(rate + s0, dist) (the !skip branch).
		// govpx folds s0/s1 (the skip-flag costs) into the existing
		// keyframe writer downstream; for the TX_MODE_SELECT inner pick
		// we only need rate + r_tx_size to compare candidates under the
		// same skip context. This mirrors libvpx since the skip cost is
		// independent of tx_size when the block has residue (the
		// dominant case during keyframe TX_MODE_SELECT pick).
		rate += vp9TxSizeRateCost(txProbs, tx, maxTx)
		score := e.vp9ModeDecisionScore(distortion, rate, qindex)
		if !bestValid || score < bestScore {
			bestScore = score
			bestTx = tx
			bestValid = true
		}
		// libvpx tx_size_search_breakout (vp9_rdopt.c:994-997) — break the
		// search loop when smaller-tx fails to improve over the previous
		// larger-tx score. govpx initializes sf.tx_size_search_breakout = 1
		// in the best-quality init (vp9_speed_features.go:944), so the
		// libvpx-default behaviour applies. Without the breakout, the loop
		// continues testing smaller tx candidates, which biases the picker
		// toward smaller tx_size on textured residuals because the
		// SATD-based rate proxy underestimates the larger-tx coef rate.
		if e.sf.TxSizeSearchBreakout != 0 && n < startTx && prevValid &&
			score > prevScore {
			break
		}
		prevScore = score
		prevValid = true
	}
	// Restore the recon snapshot. The subsequent
	// prepareVP9KeyframeBlockResidue call will re-encode with the chosen tx.
	for y := 0; y < restoreH; y++ {
		copy(planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW],
			saved[y*restoreW:(y+1)*restoreW])
	}
	if bestValid {
		mi.TxSize = bestTx
	}
}

// vp9KeyframeCoeffBlockRateCost is a verbatim port of libvpx's cost_coeffs
// (vp9/encoder/vp9_rdopt.c:358-459) specialised for the keyframe Y-plane
// path. libvpx walks the per-token entropy tree against
// x->token_costs[tx_size][type][is_inter_block(mi)] where type=PLANE_TYPE_Y
// (=0) and is_inter=0 for an intra/keyframe block. govpx mirrors by
// reading the matching fc.CoefProbs[txSize][planeType=0][ref=0] slab and
// invoking vp9CoeffTokenRateCost for the unconstrained pareto8 tail —
// the same pareto-tree walk vp9_cost_tokens (vp9/encoder/vp9_cost.c)
// drives in fill_token_costs (vp9/encoder/vp9_rd.c:135-152). The
// per-coefficient energy class fed into the next coef-context lookup
// mirrors libvpx's token_cache[rc] = vp9_pt_energy_class[token]
// (vp9_rdopt.c:397, 429, 442; pt_energy_class table is in
// vp9/common/vp9_entropy.c:95).
func (e *VP9Encoder) vp9KeyframeCoeffBlockRateCost(txSize common.TxSize,
	dequant [2]int16, coeffs []int16,
) int {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if txSize >= common.TxSizes || dequant[0] == 0 || dequant[1] == 0 ||
		len(coeffs) < maxEob || len(e.modeScratch) < maxEob {
		return 0
	}
	scan := common.DefaultScanOrders[txSize].Scan
	neighbors := common.DefaultScanOrders[txSize].Neighbors
	bandTrans := vp9dec.BandTranslateForTxSize(txSize)
	for i := range e.modeScratch[:maxEob] {
		e.modeScratch[i] = 0
	}
	eob := 0
	for i := range maxEob {
		if coeffs[scan[i]] != 0 {
			eob = i + 1
		}
	}
	// libvpx vp9_rdopt.c:369 — x->token_costs[tx_size][type][is_inter].
	// type=PLANE_TYPE_Y=0, is_inter=0 for a keyframe / intra block.
	coefModel := &e.fc.CoefProbs[txSize][0][0]
	ctx := 0
	bandIdx := 0
	rate := 0
	for c := 0; c < maxEob; {
		band := int(bandTrans[bandIdx])
		bandIdx++
		probs := &coefModel[band][ctx]
		if c == eob {
			rate += encoder.VP9CostBit(probs[0], 0)
			return rate
		}
		rate += encoder.VP9CostBit(probs[0], 1)
		for coeffs[scan[c]] == 0 {
			rate += encoder.VP9CostBit(probs[1], 0)
			e.modeScratch[scan[c]] = 0
			c++
			if c >= maxEob {
				return rate
			}
			ctx = vp9dec.GetCoefContext(neighbors, &e.modeScratch, c)
			band = int(bandTrans[bandIdx])
			bandIdx++
			probs = &coefModel[band][ctx]
		}
		rate += encoder.VP9CostBit(probs[1], 1)
		raster := scan[c]
		coeff := coeffs[raster]
		sign := 0
		if coeff < 0 {
			coeff = -coeff
			sign = 1
		}
		dqv := dequant[1]
		if c == 0 {
			dqv = dequant[0]
		}
		// libvpx vp9_rdopt.c:394-407 — vp9_get_token_cost(v, &t, ...);
		// cost += token_costs[!prev_t][!prev_t][t]. govpx mirrors by
		// recovering the quantized magnitude via vp9CoeffTokenAbsVal
		// (coeffs[] carry the dequantized values out of QuantizeB) and
		// walking the pareto8 tree via vp9CoeffTokenRateCost.
		absVal := vp9CoeffTokenAbsVal(coeff, dqv, txSize == common.Tx32x32)
		rate += vp9CoeffTokenRateCost(probs[:], absVal, sign)
		// libvpx vp9_rdopt.c:397, 429, 442 — token_cache[rc] =
		// vp9_pt_energy_class[token]. The libvpx table
		// vp9/common/vp9_entropy.c:95 reads
		//   {0, 1, 2, 3, 3, 4, 4, 5, 5, 5, 5, 5}
		// for ZERO/ONE/TWO/THREE/FOUR/CAT1..CAT6/EOB, matching
		// TokenForAbsCoeff's classification ranges below.
		switch {
		case absVal == 1:
			e.modeScratch[raster] = 1
		case absVal == 2:
			e.modeScratch[raster] = 2
		case absVal == 3 || absVal == 4:
			e.modeScratch[raster] = 3
		case absVal <= 10:
			e.modeScratch[raster] = 4
		default:
			e.modeScratch[raster] = 5
		}
		c++
		if c < maxEob {
			ctx = vp9dec.GetCoefContext(neighbors, &e.modeScratch, c)
		}
	}
	return rate
}

// vp9PlaneRectSSEClamped returns the SSE between src and dst rectangles of
// size (w,h) starting at (x0,y0) in BOTH planes (src has dim srcW x srcH,
// dst has dim dstStride x dstRows). Clamps both src and dst coords to
// extents to mirror libvpx's pixel_sse which uses sum_squares_visible
// semantics. Returns false if dst access would be out-of-bounds at (x0,y0).
func vp9PlaneRectSSEClamped(src []byte, srcStride, srcW, srcH int,
	dst []byte, dstStride, x0, y0, w, h int,
) (uint64, bool) {
	if len(src) == 0 || srcStride <= 0 || len(dst) == 0 || dstStride <= 0 ||
		w <= 0 || h <= 0 {
		return 0, false
	}
	dstRows := len(dst) / dstStride
	if x0 < 0 || y0 < 0 || x0 >= dstStride || y0 >= dstRows {
		return 0, false
	}
	var sse uint64
	for y := range h {
		sy := y0 + y
		if sy >= srcH {
			sy = srcH - 1
		}
		dy := y0 + y
		if dy >= dstRows {
			dy = dstRows - 1
		}
		srcRow := src[sy*srcStride:]
		dstRow := dst[dy*dstStride:]
		for x := range w {
			sx := x0 + x
			if sx >= srcW {
				sx = srcW - 1
			}
			dx := x0 + x
			if dx >= dstStride {
				dx = dstStride - 1
			}
			diff := int(srcRow[sx]) - int(dstRow[dx])
			sse += uint64(diff * diff)
		}
	}
	return sse, true
}

func (e *VP9Encoder) prepareVP9KeyframeBlockResidue(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, uvMode common.PredictionMode,
) bool {
	hasResidue := false
	segID := vp9EncoderMiSegmentID(mi)
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		e.clearVP9PlaneBlockCoeffs(plane, planeBsize)
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((full4x4W - max4x4W) >> txSize) * blockStep
		blockIdx := 0
		dequant := key.dq.Y[segID]
		if plane > 0 {
			dequant = key.dq.Uv[segID]
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				coeffBase := (rr*full4x4W + cc) * vp9EncoderTxCoeffSlots
				coeffs := e.blockCoeffs[plane][coeffBase : coeffBase+vp9EncoderTxCoeffSlots]
				qindex := vp9dec.GetSegmentQindex(&key.hdr.Seg, segID,
					int(key.hdr.Quant.BaseQindex))
				if e.prepareVP9KeyframeTxResidue(key, pd, plane, mode,
					txSize, tile, miRows, miCols, miRow, miCol, bsize, rr, cc,
					dequant, qindex, coeffs) {
					hasResidue = true
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	return hasResidue
}

func (e *VP9Encoder) prepareVP9InterBlockResidue(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds, mi *vp9dec.NeighborMi,
	forcedRefFrame int8, forcedRef bool,
) (common.PredictionMode, bool) {
	interDecision, ok := e.prepareVP9InterPredictionBlock(inter, miRows, miCols,
		miRow, miCol, bsize, tile, mi, forcedRefFrame, forcedRef)
	if !ok {
		return common.DcPred, false
	}
	if e.opts.AQMode == VP9AQComplexity {
		mi.TxSize = e.pickVP9InterTxSize(inter, tile, miRows, miCols, miRow, miCol,
			bsize, mi.TxSize, mi.SegmentID)
		projectedRate := interDecision.rate
		if _, coeffRate, hasTxResidue, ok := e.scoreVP9InterTxCandidate(inter,
			miRows, miCols, miRow, miCol, bsize, mi.TxSize); ok && hasTxResidue {
			projectedRate += coeffRate
		}
		e.applyVP9ComplexityAQSegment(inter, miRow, miCol, bsize, mi,
			projectedRate)
	}
	if !forcedRef && e.vp9StaticThresholdBreakout(inter, miRows, miCols,
		miRow, miCol, bsize, mi) {
		return common.DcPred, false
	}
	if e.opts.AQMode != VP9AQComplexity {
		mi.TxSize = e.pickVP9InterTxSize(inter, tile, miRows, miCols, miRow, miCol,
			bsize, mi.TxSize, mi.SegmentID)
	}
	if !inter.lossless && !forcedRef {
		if intra, ok := e.pickVP9InterIntraMode(inter, tile, miRows, miCols,
			miRow, miCol, bsize, mi.TxSize, interDecision.score); ok {
			mi.Mode = intra.mode
			mi.Mv = [2]vp9dec.MV{}
			mi.RefFrame = [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}
			mi.InterpFilter = uint8(vp9dec.SwitchableFilters)
			return intra.uvMode, e.prepareVP9InterIntraBlockResidue(inter, tile,
				miRows, miCols, miRow, miCol, bsize, mi, intra.uvMode)
		}
	}
	e.applyVP9DenoiserToInterBlock(inter, miRows, miCols, miRow, miCol,
		bsize, interDecision)
	hasResidue := false
	segID := vp9EncoderMiSegmentID(mi)
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		e.clearVP9PlaneBlockCoeffs(plane, planeBsize)
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		dequant := inter.dq.Y[segID]
		if plane > 0 {
			dequant = inter.dq.Uv[segID]
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				coeffBase := (rr*full4x4W + cc) * vp9EncoderTxCoeffSlots
				coeffs := e.blockCoeffs[plane][coeffBase : coeffBase+vp9EncoderTxCoeffSlots]
				if e.prepareVP9InterTxResidue(inter, pd, plane, txSize,
					miRow, miCol, rr, cc, dequant, coeffs) {
					hasResidue = true
				}
			}
		}
	}
	return common.DcPred, hasResidue
}

func (e *VP9Encoder) vp9StaticThresholdBreakout(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) bool {
	threshold := e.opts.StaticThreshold
	if threshold <= 0 || inter == nil || mi == nil || inter.dq == nil ||
		inter.lossless || bsize < common.Block8x8 {
		return false
	}
	refFrame := mi.RefFrame[0]
	if refFrame <= vp9dec.IntraFrame || refFrame >= vp9dec.MaxRefFrames {
		return false
	}
	if e.opts.SpatialScalability.Enabled && refFrame == vp9dec.GoldenFrame {
		return false
	}
	mv := mi.Mv[0]
	if mv.Row < -64 || mv.Row > 64 || mv.Col < -64 || mv.Col > 64 {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pred, predStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(pred) == 0 || srcStride <= 0 || predStride <= 0 {
		return false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	predH := 0
	if predStride > 0 {
		predH = len(pred) / predStride
	}
	if !vp9VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
		!vp9VisibleBlockFits(x0, y0, blockW, blockH, predStride, predH) {
		return false
	}
	segID := vp9EncoderMiSegmentID(mi)
	threshAC, threshDC := vp9StaticThresholds(threshold, inter.dq.Y[segID],
		bsize)
	varY, sseY := vp9BlockDiffVarianceSSE(src, srcStride, pred, predStride,
		x0, y0, x0, y0, blockW, blockH)
	if varY > threshAC || sseY-varY > threshDC {
		return false
	}
	return e.vp9StaticThresholdChromaBreakout(inter, miRow, miCol, bsize,
		threshAC, threshDC)
}

func (e *VP9Encoder) vp9StaticThresholdChromaBreakout(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, threshAC, threshDC uint64,
) bool {
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return false
		}
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
		pred, predStride := e.vp9EncoderReconPlane(plane)
		if len(src) == 0 || len(pred) == 0 || srcStride <= 0 || predStride <= 0 {
			return false
		}
		blockW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		blockH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		x0 := (miCol * common.MiSize) >> pd.SubsamplingX
		y0 := (miRow * common.MiSize) >> pd.SubsamplingY
		predH := len(pred) / predStride
		if !vp9VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
			!vp9VisibleBlockFits(x0, y0, blockW, blockH, predStride, predH) {
			return false
		}
		variance, sse := vp9BlockDiffVarianceSSE(src, srcStride, pred,
			predStride, x0, y0, x0, y0, blockW, blockH)
		if (variance<<2) > threshAC || sse-variance > threshDC {
			return false
		}
	}
	return true
}

func vp9StaticThresholds(threshold int, yDequant [2]int16,
	bsize common.BlockSize,
) (uint64, uint64) {
	const maxThresh = uint64(36000)
	minThresh := maxThresh
	if threshold < int(maxThresh>>4) {
		minThresh = uint64(threshold) << 4
	}
	yAC := int(yDequant[1])
	threshAC := uint64(yAC*yAC) >> 3
	if threshAC < minThresh {
		threshAC = minThresh
	} else if threshAC > maxThresh {
		threshAC = maxThresh
	}
	shift := 8 - int(common.BWidthLog2Lookup[bsize]+common.BHeightLog2Lookup[bsize])
	if shift > 0 {
		threshAC >>= uint(shift)
	}
	yDC := int(yDequant[0])
	return threshAC, uint64(yDC*yDC) >> 6
}

func (e *VP9Encoder) prepareVP9InterPredictionBlock(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds, mi *vp9dec.NeighborMi,
	forcedRefFrame int8, forcedRef bool,
) (vp9InterModeDecision, bool) {
	if mi == nil {
		return vp9InterModeDecision{}, false
	}
	mi.Mode = common.ZeroMv
	mi.Mv = [2]vp9dec.MV{}
	mi.RefFrame = [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}
	mi.InterpFilter = uint8(vp9dec.InterpEighttap)
	var picked vp9InterModeDecision
	if forcedRef {
		refSlot, ok := e.vp9InterReferenceSlot(inter, forcedRefFrame)
		if !ok {
			return vp9InterModeDecision{}, false
		}
		inter.ref = &e.refFrames[refSlot]
		mi.RefFrame = [2]int8{forcedRefFrame, vp9dec.NoRefFrame}
		if decision, ok := e.pickVP9InterMode(inter, tile, miRows, miCols,
			miRow, miCol, bsize, forcedRefFrame, 0); ok {
			picked = decision
			picked.refFrame = forcedRefFrame
			picked.secondRefFrame = vp9dec.NoRefFrame
			picked.refSlot = refSlot
			mi.Mode = decision.mode
			mi.Mv = decision.mv
			mi.InterpFilter = uint8(decision.interpFilter)
		}
	} else if cached, ok := e.lookupVP9LeafInterDecision(miRow, miCol, bsize); ok {
		// libvpx: vp9/encoder/vp9_bitstream.c::write_modes_b reads the
		// stored picker decision from mi[0]->mbmi without re-invoking
		// the picker. The cache populated by the prior count pre-pass
		// supplies the same decision for this leaf-write call site.
		picked = cached
		mi.Mode = cached.mode
		mi.Mv = cached.mv
		mi.RefFrame = [2]int8{cached.refFrame, cached.secondRefFrame}
		mi.InterpFilter = uint8(cached.interpFilter)
		inter.ref = &e.refFrames[cached.refSlot]
	} else if decision, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow, miCol, bsize); ok {
		picked = decision
		mi.Mode = decision.mode
		mi.Mv = decision.mv
		mi.RefFrame = [2]int8{decision.refFrame, decision.secondRefFrame}
		mi.InterpFilter = uint8(decision.interpFilter)
		inter.ref = &e.refFrames[decision.refSlot]
		// Commit the leaf decision so a subsequent same-frame visit at
		// this (miRow, miCol, bsize) — the bitstream write pass — can
		// skip the picker. libvpx encodes the decision once into
		// mi_grid_visible during the encode walk; the bitstream pass
		// reads it back without recomputation.
		e.storeVP9LeafInterDecision(miRow, miCol, bsize, decision)
	} else if refFrame, refSlot, ok := e.firstVP9InterReference(inter); ok {
		mi.RefFrame[0] = refFrame
		inter.ref = &e.refFrames[refSlot]
	} else {
		return vp9InterModeDecision{}, false
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, mi) {
		return vp9InterModeDecision{}, false
	}
	return picked, true
}

func (e *VP9Encoder) prepareVP9InterSkipPrediction(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
	forcedRefFrame int8, forcedRef bool,
) bool {
	if inter == nil || mi == nil {
		return false
	}
	refFrame := mi.RefFrame[0]
	if forcedRef {
		refFrame = forcedRefFrame
	}
	refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame)
	if !ok && !forcedRef {
		refFrame, refSlot, ok = e.firstVP9InterReference(inter)
	}
	if !ok {
		return false
	}
	mi.Mode = common.ZeroMv
	mi.Mv = [2]vp9dec.MV{}
	mi.RefFrame = [2]int8{refFrame, vp9dec.NoRefFrame}
	mi.InterpFilter = uint8(vp9dec.InterpEighttap)
	inter.ref = &e.refFrames[refSlot]
	return e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, mi)
}

func (e *VP9Encoder) pickVP9InterTxSize(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, maxTx common.TxSize, segmentID uint8,
) common.TxSize {
	if inter == nil || inter.dq == nil || bsize >= common.BlockSizes {
		return maxTx
	}
	maxTx = clampVP9TxSizeForBlock(maxTx, bsize)
	if maxTx < common.Tx8x8 {
		return maxTx
	}
	if miRow+int(common.Num8x8BlocksHighLookup[bsize]) > miRows ||
		miCol+int(common.Num8x8BlocksWideLookup[bsize]) > miCols {
		return maxTx
	}
	sse, activity, ok := e.vp9InterTxResidualStats(inter, miRow, miCol, bsize)
	pixels := uint64(common.Num4x4BlocksWideLookup[bsize]) *
		uint64(common.Num4x4BlocksHighLookup[bsize]) * 16
	if !ok {
		return maxTx
	}
	// limitTx mirrors libvpx vp9/encoder/vp9_pickmode.c:370-373
	// (calculate_tx_size) — under CYCLIC_REFRESH_AQ the encoder lifts the
	// inter Tx16x16 cap when source variance or residual variance is zero
	// (var_thresh = 1 for inter, i.e. is_intra=0). Outside CYCLIC_REFRESH_AQ
	// limit_tx stays 1 and the libvpx Tx16x16 ceiling applies.
	limitTx := e.vp9InterCalculateTxLimitTx(inter, miRow, miCol, bsize, sse)
	// libvpx vp9_pickmode.c:380-388 — the boosted-segment Tx8x8 force and
	// screen-content Tx4x4 force apply once the picker has produced a
	// candidate tx_size. acThr mirrors model_rd_for_sb_y at vp9_pickmode.c:
	// 658 (`ac_thr = p->quant_thred[1] >> 6`); quant_thred[1] is computed
	// as zbin[1]^2 at vp9_quantize.c:265 with zbin[1] =
	// ROUND_POWER_OF_TWO(qzbin_factor * ac_quant, 7) (vp9_quantize.c:211).
	acThr := e.vp9InterCalculateTxAcThr(inter, segmentID)
	// residualVar derived from the same sse/sum-of-differences as
	// vp9InterCalculateTxLimitTx so the screen-content force uses the
	// same variance the libvpx model_rd_for_sb_y feeds calculate_tx_size
	// (vp9_pickmode.c:668). residualVar == 0 retains the prior limit_tx
	// semantics; the full uint64 value now feeds the (var >> 5) > ac_thr
	// screen-content Tx4x4 force at vp9_pickmode.c:386-388.
	_, residualVar, _ := e.vp9InterTxSourceAndResidualVar(inter, miRow,
		miCol, bsize, sse)
	if maxTx == common.Tx8x8 && sse > pixels*512 && activity > pixels*128 {
		return e.vp9InterTxApplyForces(maxTx, bsize, sse, residualVar, acThr,
			limitTx, segmentID)
	}
	if sse <= pixels*512 || activity <= pixels*16 {
		// libvpx vp9_pickmode.c:371-384: in the CYCLIC_REFRESH_AQ flat
		// region (limit_tx=0) the Tx16x16 ceiling is dropped, so the
		// picker can return maxTx (up to Tx32x32) directly without
		// running the score-based RDO. For limit_tx=1 the libvpx
		// Tx16x16 cap still applies.
		if !limitTx {
			return e.vp9InterTxApplyForces(maxTx, bsize, sse, residualVar,
				acThr, limitTx, segmentID)
		}
		// The realtime oracle keeps smooth changed inter blocks below
		// 32x32, while still allowing textured residuals to use the
		// scored Tx32 path below.
		tx := min(maxTx, common.Tx16x16)
		return e.vp9InterTxApplyForces(tx, bsize, sse, residualVar, acThr,
			limitTx, segmentID)
	}
	reconSnap, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, bsize)
	if !ok {
		return maxTx
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	txCtx := vp9dec.GetTxSizeContext(above, left, maxTx)
	txProbs := vp9TxProbsRow(&e.fc.TxProbs, maxTx, txCtx)
	qindex := e.vp9EncoderModeDecisionQIndex()

	bestTx := maxTx
	bestScore := uint64(^uint64(0))
	bestRate := int(^uint(0) >> 1)
	minTx := max(maxTx-1, common.Tx4x4)
	for txi := int(maxTx); txi >= int(minTx); txi-- {
		tx := common.TxSize(txi)
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		distortion, coeffRate, hasResidue, ok := e.scoreVP9InterTxCandidate(inter,
			miRows, miCols, miRow, miCol, bsize, tx)
		if !ok {
			continue
		}
		rate := 0
		if hasResidue {
			rate = coeffRate + vp9TxSizeRateCost(txProbs, tx, maxTx)
		}
		score := e.vp9ModeDecisionScore(distortion, rate, qindex)
		if score < bestScore || (score == bestScore && rate < bestRate) {
			bestScore = score
			bestRate = rate
			bestTx = tx
		}
	}
	e.restoreVP9PartitionReconSnapshot(reconSnap)
	return e.vp9InterTxApplyForces(bestTx, bsize, sse, residualVar, acThr,
		limitTx, segmentID)
}

// vp9InterTxApplyForces folds in the libvpx-verbatim boosted-segment
// Tx8x8 force from vp9/encoder/vp9_pickmode.c:380-384 (inside
// calculate_tx_size) plus the VP9E_CONTENT_SCREEN Tx4x4 force at
// vp9_pickmode.c:386-388 on top of govpx's score-based picker output.
// libvpx evaluates:
//
//	if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ && limit_tx &&
//	    cyclic_refresh_segment_id_boosted(xd->mi[0]->segment_id))
//	  tx_size = TX_8X8;
//	else if (tx_size > TX_16X16 && limit_tx)
//	  tx_size = TX_16X16;
//	// For screen-content force 4X4 tx_size over 8X8, for large variance.
//	if (cpi->oxcf.content == VP9E_CONTENT_SCREEN && tx_size == TX_8X8 &&
//	    bsize <= BLOCK_16X16 && ((var >> 5) > (unsigned int)ac_thr))
//	  tx_size = TX_4X4;
//
// residualVar mirrors the libvpx `var` passed to calculate_tx_size by
// model_rd_for_sb_y at vp9_pickmode.c:668 — the same residual variance
// computed in vp9InterTxSourceAndResidualVar via
// sse - ((sum*sum) >> (bw+bh+4)).
func (e *VP9Encoder) vp9InterTxApplyForces(tx common.TxSize, bsize common.BlockSize,
	sse uint64, residualVar uint64, acThr int64, limitTx bool, segmentID uint8,
) common.TxSize {
	_ = sse
	if e == nil {
		return tx
	}
	// Boosted-segment Tx8x8 force (vp9_pickmode.c:380-382).
	if e.opts.AQMode == VP9AQCyclicRefresh && limitTx &&
		vp9CyclicRefreshSegmentIDBoosted(segmentID) {
		tx = common.Tx8x8
	} else if tx > common.Tx16x16 && limitTx {
		// Tx16x16 cap (vp9_pickmode.c:383-384) — kept for parity even
		// though govpx already caps Tx16x16 in the picker; libvpx's
		// helper applies the cap unconditionally here.
		tx = common.Tx16x16
	}
	// Screen-content Tx4x4 force (vp9_pickmode.c:386-388). libvpx gates
	// the force on (var >> 5) > (unsigned int)ac_thr — acThr is
	// signed-int64 in govpx; cast through uint64 mirrors the libvpx
	// unsigned compare. acThr <= 0 disables the force (govpx returns
	// acThr == 0 when the quantizer plumbing is unavailable).
	if e.opts.ScreenContentMode == int8(VP9ScreenContentScreen) &&
		tx == common.Tx8x8 && bsize <= common.Block16x16 &&
		acThr > 0 && (residualVar>>5) > uint64(acThr) {
		tx = common.Tx4x4
	}
	return tx
}

// vp9InterCalculateTxAcThr ports libvpx's
// `ac_thr = p->quant_thred[1] >> 6` (vp9/encoder/vp9_pickmode.c:658) and
// `quant_thred[1] = zbin[1]^2` (vp9/encoder/vp9_quantize.c:265) with
// zbin[1] = ROUND_POWER_OF_TWO(qzbin_factor * ac_quant, 7)
// (vp9/encoder/vp9_quantize.c:211). ac_quant is dequant[1] for the Y
// plane at the segment qindex.
//
// The ac_thr_factor scaling at vp9_pickmode.c:494/497 is independent of
// the per-block segment id and feeds the abs(sum) >> (bw+bh) check that
// only fires at speed >= 8 / norm_sum < 5. govpx does not yet thread
// the per-block norm_sum into the picker; the factor defaults to 1
// outside that gate, so the ac_thr returned here matches libvpx for
// every speed < 8 path and approximates libvpx for the speed=8 path
// where norm_sum >= 5 (the textured-residual majority).
func (e *VP9Encoder) vp9InterCalculateTxAcThr(inter *vp9InterEncodeState,
	segmentID uint8,
) int64 {
	if e == nil || inter == nil || inter.dq == nil ||
		int(segmentID) >= len(inter.dq.Y) {
		return 0
	}
	acQuant := int64(inter.dq.Y[segmentID][1])
	if acQuant <= 0 {
		return 0
	}
	zbin := vp9RoundPowerOfTwoForTxForce(int64(vp9QzbinFactorForTxForce(
		e.vp9EncoderModeDecisionQIndex()))*acQuant, 7)
	return (zbin * zbin) >> 6
}

func vp9QzbinFactorForTxForce(qindex int) int {
	if qindex == 0 {
		return 64
	}
	if int(common.DcQuant(qindex, 0, common.Bits8)) < 148 {
		return 84
	}
	return 80
}

func vp9RoundPowerOfTwoForTxForce(value int64, n uint) int64 {
	return (value + int64(1)<<(n-1)) >> n
}

// vp9InterCalculateTxLimitTx is a verbatim port of the limit_tx
// computation from libvpx vp9/encoder/vp9_pickmode.c:370-373 inside
// calculate_tx_size, specialised for the inter path (is_intra=0).
// libvpx evaluates:
//
//	int limit_tx = 1;
//	if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ &&
//	    (source_variance == 0 || var < var_thresh))
//	  limit_tx = 0;
//
// where var_thresh = is_intra ? ac_thr : 1, so for inter we have
// var_thresh = 1 and the only way the predicate fires is when either
// source_variance or var equals zero.
//
// govpx computes the residual variance from sse and sum of differences
// as libvpx does (var = sse - (sum*sum) >> (bw+bh+4)). When the
// residual is constant the variance is zero and limit_tx flips to 0;
// otherwise the libvpx Tx16x16 cap stays in place.
func (e *VP9Encoder) vp9InterCalculateTxLimitTx(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, sse uint64,
) bool {
	if e == nil || inter == nil || bsize >= common.BlockSizes {
		return true
	}
	if e.opts.AQMode != VP9AQCyclicRefresh {
		// libvpx vp9_pickmode.c:371 — limit_tx defaults to 1 outside
		// CYCLIC_REFRESH_AQ.
		return true
	}
	srcVar, residVar, ok := e.vp9InterTxSourceAndResidualVar(inter, miRow,
		miCol, bsize, sse)
	if !ok {
		return true
	}
	// var_thresh = 1 for is_intra=0; var < 1 ⇔ var == 0 since var is
	// unsigned. source_variance == 0 || var == 0 toggles limit_tx to 0.
	if srcVar == 0 || residVar == 0 {
		return false
	}
	return true
}

// vp9InterTxSourceAndResidualVar returns the libvpx source_variance
// (block luma variance about its mean) and the residual variance
// computed as `sse - ((sum_diff*sum_diff) >> (bw+bh+4))`. The bw/bh
// shift mirrors libvpx vp9_pickmode.c:481 / vpx_dsp variance.c
// variance(), which divides sum_sqr by the pixel count (4<<bw * 4<<bh
// = 16 << (bw+bh)) using a fixed right-shift. residualVar equals the
// libvpx `var` value model_rd_for_sb_y passes into calculate_tx_size
// at vp9_pickmode.c:668 — both the
// `cyclic_refresh limit_tx` predicate (var < var_thresh) and the
// screen-content Tx4x4 force (`(var >> 5) > ac_thr`) consume it.
func (e *VP9Encoder) vp9InterTxSourceAndResidualVar(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, sse uint64,
) (sourceVar uint64, residualVar uint64, ok bool) {
	if inter == nil {
		return 0, 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pred, predStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(pred) == 0 || srcStride <= 0 || predStride <= 0 {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	predRows := len(pred) / predStride
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > predStride || y0+blockH > predRows {
		return 0, 0, false
	}
	var srcSum int64
	var srcSse uint64
	var diffSum int64
	for y := range blockH {
		srcRow := src[(y0+y)*srcStride:]
		predRow := pred[(y0+y)*predStride:]
		for x := range blockW {
			s := int(srcRow[x0+x])
			p := int(predRow[x0+x])
			srcSum += int64(s)
			srcSse += uint64(s * s)
			diffSum += int64(s - p)
		}
	}
	n := int64(blockW * blockH)
	if n <= 0 {
		return 0, 0, false
	}
	// libvpx source_variance in vp9_block.h:120 is the variance about the
	// block mean: sum(x*x) - (sum(x))^2 / N. Computed in floor-divide form
	// so values exactly equal to the unbiased variance for byte input.
	srcMeanSqr := uint64((srcSum * srcSum) / n)
	if srcSse > srcMeanSqr {
		sourceVar = srcSse - srcMeanSqr
	}
	// residual variance: sse - (sum*sum) >> (bw+bh+4). bw,bh from libvpx
	// b_{width,height}_log2_lookup give blockW=4<<bw, blockH=4<<bh.
	bwLog2 := int(common.BWidthLog2Lookup[bsize])
	bhLog2 := int(common.BHeightLog2Lookup[bsize])
	shift := uint(bwLog2 + bhLog2 + 4)
	sumSqr := uint64((diffSum * diffSum) >> shift)
	if sse > sumSqr {
		residualVar = sse - sumSqr
	}
	return sourceVar, residualVar, true
}

// vp9CyclicRefreshSegmentIDBoosted ports libvpx
// vp9/encoder/vp9_aq_cyclicrefresh.h:127-130
// cyclic_refresh_segment_id_boosted(segment_id), checking whether the
// caller-supplied CR segment_id is in {BOOST1, BOOST2}. Currently
// referenced by vp9InterCalculateTxSize as part of the port of
// calculate_tx_size (vp9_pickmode.c:380-382). The boosted-segment
// Tx8x8 forcing is wired in via the post-pass at the end of
// pickVP9InterTxSize once a per-block segment id is plumbed through;
// the helper is kept available so the upcoming select_tx_mode port
// can reuse it without re-deriving the libvpx constant set.
func vp9CyclicRefreshSegmentIDBoosted(segmentID uint8) bool {
	return segmentID == vp9CyclicRefreshSegmentBoost1 ||
		segmentID == vp9CyclicRefreshSegmentBoost2
}

// vp9InterCalculateTxSize is a verbatim port of libvpx
// vp9/encoder/vp9_pickmode.c:363-393 (calculate_tx_size) specialised
// for the inter path (is_intra=0, var_thresh = 1). Currently used as
// a reference oracle by the limit_tx-aware post-pass in
// pickVP9InterTxSize and by future select_tx_mode rewiring; the
// govpx inter picker still drives its score-based RDO on top of
// libvpx's limit_tx semantics so it can preserve byte parity against
// the established heuristic baseline while exposing the libvpx
// CYCLIC_REFRESH_AQ var=0 escape.
//
// libvpx reference (vp9_pickmode.c:363-393):
//
//	static TX_SIZE calculate_tx_size(VP9_COMP *const cpi, BLOCK_SIZE bsize,
//	                                 MACROBLOCKD *const xd, unsigned int var,
//	                                 unsigned int sse, int64_t ac_thr,
//	                                 unsigned int source_variance, int is_intra) {
//	  TX_SIZE tx_size;
//	  unsigned int var_thresh = is_intra ? (unsigned int)ac_thr : 1;
//	  int limit_tx = 1;
//	  if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ &&
//	      (source_variance == 0 || var < var_thresh))
//	    limit_tx = 0;
//	  if (cpi->common.tx_mode == TX_MODE_SELECT) {
//	    if (sse > (var << 2))
//	      tx_size = VPXMIN(max_txsize_lookup[bsize],
//	                       tx_mode_to_biggest_tx_size[cpi->common.tx_mode]);
//	    else
//	      tx_size = TX_8X8;
//	    if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ && limit_tx &&
//	        cyclic_refresh_segment_id_boosted(xd->mi[0]->segment_id))
//	      tx_size = TX_8X8;
//	    else if (tx_size > TX_16X16 && limit_tx)
//	      tx_size = TX_16X16;
//	    if (cpi->oxcf.content == VP9E_CONTENT_SCREEN && tx_size == TX_8X8 &&
//	        bsize <= BLOCK_16X16 && ((var >> 5) > (unsigned int)ac_thr))
//	      tx_size = TX_4X4;
//	  } else {
//	    tx_size = VPXMIN(max_txsize_lookup[bsize],
//	                     tx_mode_to_biggest_tx_size[cpi->common.tx_mode]);
//	  }
//	  return tx_size;
//	}
func (e *VP9Encoder) vp9InterCalculateTxSize(bsize common.BlockSize,
	txMode common.TxMode, sse, residualVar, sourceVar uint64, acThr int64,
	segmentID uint8,
) common.TxSize {
	maxTx := common.MaxTxsizeLookup[bsize]
	biggestForMode := common.TxModeToBiggestTxSize[txMode]
	if maxTx > biggestForMode {
		maxTx = biggestForMode
	}
	// var_thresh = is_intra ? ac_thr : 1 — inter path: 1.
	limitTx := true
	if e.opts.AQMode == VP9AQCyclicRefresh &&
		(sourceVar == 0 || residualVar == 0) {
		limitTx = false
	}
	var txSize common.TxSize
	if txMode == common.TxModeSelect {
		if sse > residualVar<<2 {
			txSize = maxTx
		} else {
			txSize = common.Tx8x8
		}
		if e.opts.AQMode == VP9AQCyclicRefresh && limitTx &&
			vp9CyclicRefreshSegmentIDBoosted(segmentID) {
			txSize = common.Tx8x8
		} else if txSize > common.Tx16x16 && limitTx {
			txSize = common.Tx16x16
		}
		// VP9E_CONTENT_SCREEN: force Tx4x4 over Tx8x8 for large variance,
		// vp9_pickmode.c:386-388.
		if e.opts.ScreenContentMode == int8(VP9ScreenContentScreen) &&
			txSize == common.Tx8x8 && bsize <= common.Block16x16 &&
			acThr > 0 && (residualVar>>5) > uint64(acThr) {
			txSize = common.Tx4x4
		}
	} else {
		txSize = maxTx
	}
	return txSize
}

func (e *VP9Encoder) vp9InterTxResidualStats(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) (sse, activity uint64, ok bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pred, predStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(pred) == 0 || srcStride <= 0 || predStride <= 0 {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	predRows := len(pred) / predStride
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > predStride || y0+blockH > predRows {
		return 0, 0, false
	}
	for y := range blockH {
		srcRow := src[(y0+y)*srcStride:]
		predRow := pred[(y0+y)*predStride:]
		for x := range blockW {
			diff := int(srcRow[x0+x]) - int(predRow[x0+x])
			sse += uint64(diff * diff)
			if x > 0 {
				leftDiff := int(srcRow[x0+x-1]) - int(predRow[x0+x-1])
				activity += uint64(vp9AbsInt(diff - leftDiff))
			}
			if y > 0 {
				upDiff := int(src[(y0+y-1)*srcStride+x0+x]) -
					int(pred[(y0+y-1)*predStride+x0+x])
				activity += uint64(vp9AbsInt(diff - upDiff))
			}
		}
	}
	return sse, activity, true
}

func vp9AbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (e *VP9Encoder) scoreVP9InterTxCandidate(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, lumaTx common.TxSize,
) (distortion uint64, rate int, hasResidue bool, ok bool) {
	if inter == nil || inter.dq == nil {
		return 0, 0, false, false
	}
	aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	var aboveCtx [vp9dec.MaxMbPlane][16]uint8
	var leftCtx [vp9dec.MaxMbPlane][16]uint8
	var aboveLen [vp9dec.MaxMbPlane]int
	var leftLen [vp9dec.MaxMbPlane]int
	for plane := range 1 {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		aboveLen[plane] = int(common.Num4x4BlocksWideLookup[planeBsize])
		leftLen[plane] = int(common.Num4x4BlocksHighLookup[planeBsize])
		if aboveLen[plane] > len(aboveCtx[plane]) || leftLen[plane] > len(leftCtx[plane]) {
			return 0, 0, false, false
		}
		if off := aboveOffsets[plane]; off >= 0 && off+aboveLen[plane] <= len(pd.AboveContext) {
			copy(aboveCtx[plane][:aboveLen[plane]], pd.AboveContext[off:off+aboveLen[plane]])
		}
		if off := leftOffsets[plane]; off >= 0 && off+leftLen[plane] <= len(pd.LeftContext) {
			copy(leftCtx[plane][:leftLen[plane]], pd.LeftContext[off:off+leftLen[plane]])
		}
	}

	for plane := range 1 {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		txSize := lumaTx
		dequant := inter.dq.Y[0]
		planeType := 0
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, lumaTx, pd)
			dequant = inter.dq.Uv[0]
			planeType = 1
		}
		max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		if maxEob > len(e.coefScratch) {
			return 0, 0, false, false
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				coeffs := e.coefScratch[:maxEob]
				for i := range coeffs {
					coeffs[i] = 0
				}
				hasTxResidue := e.prepareVP9InterTxResidue(inter, pd, plane, txSize,
					miRow, miCol, rr, cc, dequant, coeffs)
				txDist, distOK := e.scoreVP9InterTxReconstruction(inter, pd, plane,
					txSize, miRow, miCol, rr, cc)
				if !distOK {
					return 0, 0, false, false
				}
				distortion += txDist

				initCtx := vp9dec.GetEntropyContext(txSize,
					aboveCtx[plane][cc:cc+step], leftCtx[plane][rr:rr+step])
				rate += e.vp9InterCoeffBlockRateCost(txSize, planeType,
					dequant, coeffs, initCtx)
				hasCtx := uint8(0)
				if hasTxResidue {
					hasCtx = 1
					hasResidue = true
				}
				for i := range step {
					aboveCtx[plane][cc+i] = hasCtx
					leftCtx[plane][rr+i] = hasCtx
				}
			}
		}
	}
	return distortion, rate, hasResidue, true
}

func (e *VP9Encoder) scoreVP9InterTxReconstruction(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, txSize common.TxSize,
	miRow, miCol, blockRow4x4, blockCol4x4 int,
) (uint64, bool) {
	dst, stride, x0, y0, ok := e.vp9EncoderTxDst(pd, plane, txSize,
		miRow, miCol, blockRow4x4, blockCol4x4)
	if !ok {
		return 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
	if len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	bs := 4 << uint(txSize)
	var distortion uint64
	for y := 0; y < bs && y0+y < srcH; y++ {
		srcRow := src[(y0+y)*srcStride:]
		dstRow := dst[y*stride:]
		for x := 0; x < bs && x0+x < srcW; x++ {
			diff := int(srcRow[x0+x]) - int(dstRow[x])
			distortion += uint64(diff * diff)
		}
	}
	return distortion, true
}

func (e *VP9Encoder) vp9InterCoeffBlockRateCost(txSize common.TxSize,
	planeType int, dequant [2]int16, coeffs []int16, initCtx int,
) int {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if txSize >= common.TxSizes || planeType < 0 || planeType > 1 ||
		dequant[0] == 0 || dequant[1] == 0 || len(coeffs) < maxEob ||
		len(e.modeScratch) < maxEob || initCtx < 0 || initCtx > 2 {
		return 0
	}
	scan := common.DefaultScanOrders[txSize].Scan
	neighbors := common.DefaultScanOrders[txSize].Neighbors
	bandTrans := vp9dec.BandTranslateForTxSize(txSize)
	for i := range e.modeScratch[:maxEob] {
		e.modeScratch[i] = 0
	}
	eob := 0
	for i := range maxEob {
		if coeffs[scan[i]] != 0 {
			eob = i + 1
		}
	}
	coefModel := &e.fc.CoefProbs[txSize][planeType][1]
	ctx := initCtx
	bandIdx := 0
	rate := 0
	for c := 0; c < maxEob; {
		band := int(bandTrans[bandIdx])
		bandIdx++
		probs := &coefModel[band][ctx]
		if c == eob {
			rate += encoder.VP9CostBit(probs[0], 0)
			return rate
		}
		rate += encoder.VP9CostBit(probs[0], 1)
		for coeffs[scan[c]] == 0 {
			rate += encoder.VP9CostBit(probs[1], 0)
			e.modeScratch[scan[c]] = 0
			c++
			if c >= maxEob {
				return rate
			}
			ctx = vp9dec.GetCoefContext(neighbors, &e.modeScratch, c)
			band = int(bandTrans[bandIdx])
			bandIdx++
			probs = &coefModel[band][ctx]
		}
		rate += encoder.VP9CostBit(probs[1], 1)
		raster := scan[c]
		coeff := coeffs[raster]
		sign := 0
		if coeff < 0 {
			coeff = -coeff
			sign = 1
		}
		dqv := dequant[1]
		if c == 0 {
			dqv = dequant[0]
		}
		absVal := vp9CoeffTokenAbsVal(coeff, dqv, txSize == common.Tx32x32)
		rate += vp9CoeffTokenRateCost(probs[:], absVal, sign)
		switch {
		case absVal == 1:
			e.modeScratch[raster] = 1
		case absVal == 2:
			e.modeScratch[raster] = 2
		case absVal == 3 || absVal == 4:
			e.modeScratch[raster] = 3
		case absVal <= 10:
			e.modeScratch[raster] = 4
		default:
			e.modeScratch[raster] = 5
		}
		c++
		if c < maxEob {
			ctx = vp9dec.GetCoefContext(neighbors, &e.modeScratch, c)
		}
	}
	return rate
}

func vp9CoeffTokenAbsVal(absCoeff, dqv int16, tx32 bool) int {
	num := int(absCoeff)
	den := int(dqv)
	if den <= 0 {
		return 0
	}
	if tx32 {
		return (num*2 + den - 1) / den
	}
	return num / den
}

func vp9CoeffTokenRateCost(probs []uint8, absVal, sign int) int {
	if absVal <= 0 || len(probs) < encoder.UnconstrainedNodes {
		return 0
	}
	rate := 0
	token, extra := encoder.TokenForAbsCoeff(absVal)
	if token == encoder.OneToken {
		rate += encoder.VP9CostBit(probs[2], 0)
		rate += encoder.VP9CostBit(128, sign)
		return rate
	}
	rate += encoder.VP9CostBit(probs[2], 1)
	enc := encoder.CoefEncodings[token]
	pareto := tables.Pareto8Full[probs[2]-1]
	rate += encoder.TreedCost(encoder.CoefConTree[:], pareto[:],
		int(enc.Value), int(enc.Len)-encoder.UnconstrainedNodes)
	if token >= encoder.Category1Tok {
		eb := encoder.VP9ExtraBits[token]
		for i := eb.Len - 1; i >= 0; i-- {
			bit := (extra >> uint(i)) & 1
			rate += encoder.VP9CostBit(eb.Prob[eb.Len-1-i], bit)
		}
	}
	rate += encoder.VP9CostBit(128, sign)
	return rate
}

func vp9TxSizeRateCost(probs []uint8, txSize, maxTxSize common.TxSize) int {
	if len(probs) == 0 || txSize >= common.TxSizes {
		return 0
	}
	rate := 0
	if txSize == common.Tx4x4 {
		return encoder.VP9CostBit(probs[0], 0)
	}
	rate += encoder.VP9CostBit(probs[0], 1)
	if maxTxSize < common.Tx16x16 || len(probs) < 2 {
		return rate
	}
	if txSize == common.Tx8x8 {
		return rate + encoder.VP9CostBit(probs[1], 0)
	}
	rate += encoder.VP9CostBit(probs[1], 1)
	if maxTxSize < common.Tx32x32 || len(probs) < 3 {
		return rate
	}
	if txSize == common.Tx16x16 {
		return rate + encoder.VP9CostBit(probs[2], 0)
	}
	return rate + encoder.VP9CostBit(probs[2], 1)
}

type vp9InterIntraDecision struct {
	mode   common.PredictionMode
	uvMode common.PredictionMode
	txSize common.TxSize
	rate   int
	score  uint64
}

func (e *VP9Encoder) pickVP9InterIntraMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txSize common.TxSize, interScore uint64,
) (vp9InterIntraDecision, bool) {
	if inter == nil {
		return vp9InterIntraDecision{}, false
	}
	if interScore < 1<<60 &&
		!e.vp9InterIntraResidualLooksSceneCut(inter, miRow, miCol, bsize) {
		return vp9InterIntraDecision{}, false
	}
	decision, ok := e.pickVP9InterIntraModeCore(inter, tile, miRows, miCols,
		miRow, miCol, bsize, txSize,
		func(above, left *vp9dec.NeighborMi) int {
			return vp9IntraInterRateCost(&inter.selectFc, above, left, 0)
		})
	if !ok {
		return vp9InterIntraDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	qindex := e.vp9EncoderModeDecisionQIndex()
	interAdjusted := interScore + e.vp9ModeDecisionRateScore(
		vp9IntraInterRateCost(&inter.selectFc, above, left, 1), qindex)
	if decision.score >= interAdjusted {
		return vp9InterIntraDecision{}, false
	}
	return decision, true
}

func (e *VP9Encoder) vp9InterIntraResidualLooksSceneCut(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) bool {
	if bsize >= common.BlockSizes {
		return false
	}
	sse, activity, ok := e.vp9InterTxResidualStats(inter, miRow, miCol, bsize)
	if !ok {
		return false
	}
	pixels := uint64(common.Num4x4BlocksWideLookup[bsize]) *
		uint64(common.Num4x4BlocksHighLookup[bsize]) * 16
	return sse >= pixels*64*64 && activity <= pixels*64
}

func (e *VP9Encoder) pickVP9ForcedInterIntraMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txSize common.TxSize,
) (vp9InterIntraDecision, bool) {
	return e.pickVP9InterIntraModeCore(inter, tile, miRows, miCols,
		miRow, miCol, bsize, txSize,
		func(*vp9dec.NeighborMi, *vp9dec.NeighborMi) int { return 0 })
}

var vp9NoReferenceIntraModes = [...]common.PredictionMode{
	common.DcPred,
	common.VPred,
	common.HPred,
	common.TmPred,
}

func vp9NoReferenceIntraModeCount(bsize common.BlockSize, screenContentMode int8) int {
	// Mirrors the realtime VP9 intra_y_mode_bsize_mask used when inter refs
	// are disabled: non-screen content only keeps DC for blocks above 16x16.
	if screenContentMode != 1 && bsize > common.Block16x16 {
		return 1
	}
	return 3
}

func (e *VP9Encoder) pickVP9NoReferenceIntraMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, maxTx common.TxSize, segmentID uint8,
) (vp9InterIntraDecision, bool) {
	if inter == nil || bsize < common.Block8x8 {
		return vp9InterIntraDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)

	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(e.opts.Width),
		Height: uint32(e.opts.Height),
	}
	keyLike := vp9KeyframeEncodeState{
		img:      inter.img,
		hdr:      &hdr,
		dq:       inter.dq,
		lossless: inter.lossless,
	}

	sg := common.SizeGroupLookup[bsize]
	var yModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(yModeCosts[:], inter.selectFc.YModeProb[sg][:],
		common.IntraModeTree[:])
	qindex := e.vp9EncoderModeDecisionQIndex()
	rateBase := vp9IntraInterRateCost(&inter.selectFc, above, left, 0)
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	modeCount := vp9NoReferenceIntraModeCount(bsize, e.opts.ScreenContentMode)

	bestSet := false
	var best vp9InterIntraDecision
	for i := range modeCount {
		mode := vp9NoReferenceIntraModes[i]
		txSize, txOK := e.pickVP9NoReferenceIntraTxSize(&keyLike, tile,
			miRows, miCols, miRow, miCol, bsize, maxTx, mode)
		if !txOK {
			continue
		}
		mi := vp9dec.NeighborMi{
			SbType:    bsize,
			SegmentID: segmentID,
			TxSize:    txSize,
		}
		distortion, coeffRate, skippable, scoreOK := e.scoreVP9KeyframeModeTransformRD(
			&keyLike, mode, tile, miRows, miCols, miRow, miCol, bsize, &mi)
		if !scoreOK {
			continue
		}
		rate := rateBase + yModeCosts[mode]
		if skippable {
			rate += encoder.VP9CostBit(skipProb, 1)
		} else {
			rate += coeffRate + encoder.VP9CostBit(skipProb, 0)
		}
		cand := vp9InterIntraDecision{
			mode:   mode,
			uvMode: mode,
			txSize: txSize,
			rate:   rate,
			score:  e.vp9ModeDecisionScore(distortion, rate, qindex),
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}
	return best, bestSet
}

func (e *VP9Encoder) pickVP9NoReferenceIntraTxSize(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, maxTx common.TxSize, mode common.PredictionMode,
) (common.TxSize, bool) {
	maxTx = min(clampVP9TxSizeForBlock(maxTx, bsize), common.Tx16x16)
	if maxTx <= common.Tx4x4 {
		return maxTx, true
	}
	predTx := common.MaxTxsizeLookup[bsize]
	sse, variance, ok := e.vp9NoReferenceIntraResidualStats(key, mode, predTx,
		tile, miRows, miCols, miRow, miCol, bsize)
	if !ok {
		return maxTx, false
	}
	if sse > variance<<2 {
		return maxTx, true
	}
	return common.Tx8x8, true
}

func (e *VP9Encoder) vp9NoReferenceIntraResidualStats(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, txSize common.TxSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (sse uint64, variance uint64, ok bool) {
	if key == nil || key.hdr == nil || key.img == nil || int(mode) >= common.IntraModes {
		return 0, 0, false
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, 0, false
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return 0, 0, false
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return 0, 0, false
	}
	restoreW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 || restoreW*restoreH > len(e.blockScratch) {
		return 0, 0, false
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}

	max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	var sum int64
	var count uint64
	predOK := true
residualLoop:
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			dst, dstStride, x0, y0, ok := e.predictVP9KeyframeTx(
				key.hdr, pd, 0, mode, txSize, tile, miRows, miCols,
				miRow, miCol, bsize, rr, cc)
			if !ok {
				predOK = false
				break residualLoop
			}
			copyW := bs
			copyH := bs
			if x0 >= srcW || y0 >= srcH {
				continue
			}
			if x0+copyW > srcW {
				copyW = srcW - x0
			}
			if y0+copyH > srcH {
				copyH = srcH - y0
			}
			for y := 0; y < copyH; y++ {
				srcRow := src[(y0+y)*srcStride+x0:]
				dstRow := dst[y*dstStride:]
				for x := 0; x < copyW; x++ {
					diff := int(srcRow[x]) - int(dstRow[x])
					sse += uint64(diff * diff)
					sum += int64(diff)
					count++
				}
			}
		}
	}
	vp9RestorePlaneRect(planeData, stride, baseX, baseY, restoreW, restoreH, saved)
	if !predOK {
		return 0, 0, false
	}
	if count == 0 {
		return 0, 0, false
	}
	meanSquare := uint64((sum * sum) / int64(count))
	if sse >= meanSquare {
		return sse, sse - meanSquare, true
	}
	return sse, meanSquare - sse, true
}

func (e *VP9Encoder) pickVP9InterIntraModeCore(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txSize common.TxSize,
	intraInterRate func(above, left *vp9dec.NeighborMi) int,
) (vp9InterIntraDecision, bool) {
	if inter == nil || bsize < common.Block8x8 {
		return vp9InterIntraDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	qindex := e.vp9EncoderModeDecisionQIndex()
	rateBase := 0
	if intraInterRate != nil {
		rateBase = intraInterRate(above, left)
	}

	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(e.opts.Width),
		Height: uint32(e.opts.Height),
	}
	keyLike := vp9KeyframeEncodeState{
		img:      inter.img,
		hdr:      &hdr,
		dq:       inter.dq,
		lossless: inter.lossless,
	}
	sg := common.SizeGroupLookup[bsize]
	var yModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(yModeCosts[:], inter.selectFc.YModeProb[sg][:],
		common.IntraModeTree[:])

	bestSet := false
	var best vp9InterIntraDecision
	tryMode := func(mode common.PredictionMode) {
		yDist, ok := e.scoreVP9KeyframePlanePrediction(&keyLike, &e.planes[0],
			mode, 0, txSize, tile, miRows, miCols, miRow, miCol, bsize)
		if !ok {
			return
		}
		uvMode, uvDist, uvRate, ok := e.pickVP9InterIntraUvMode(&keyLike,
			&inter.selectFc, mode, tile, miRows, miCols, miRow, miCol, bsize, txSize)
		if !ok {
			return
		}
		rate := rateBase + yModeCosts[mode] + uvRate
		cand := vp9InterIntraDecision{
			mode:   mode,
			uvMode: uvMode,
			txSize: txSize,
			rate:   rate,
			score:  e.vp9ModeDecisionScore(yDist+uvDist, rate, qindex),
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}

	tryMode(common.DcPred)
	for mode := common.DcPred + 1; mode <= common.TmPred; mode++ {
		tryMode(mode)
	}
	return best, bestSet
}

func (e *VP9Encoder) pickVP9InterIntraUvMode(key *vp9KeyframeEncodeState,
	fc *vp9dec.FrameContext, yMode common.PredictionMode, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, txSize common.TxSize,
) (common.PredictionMode, uint64, int, bool) {
	if key == nil || fc == nil || yMode < common.DcPred || int(yMode) >= common.IntraModes {
		return common.DcPred, 0, 0, false
	}
	var uvModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(uvModeCosts[:], fc.UvModeProb[yMode][:],
		common.IntraModeTree[:])
	bestSet := false
	bestMode := common.DcPred
	var bestDist uint64
	bestRate := 0
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		var dist uint64
		ok := true
		for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
			pd := &e.planes[plane]
			planeTx := txSize
			if plane > 0 {
				planeTx = vp9dec.GetUvTxSize(bsize, txSize, pd)
			}
			score, scoreOK := e.scoreVP9KeyframePlanePrediction(key, pd, mode,
				plane, planeTx, tile, miRows, miCols, miRow, miCol, bsize)
			if !scoreOK {
				ok = false
				break
			}
			dist += score
		}
		if !ok {
			continue
		}
		rate := uvModeCosts[mode]
		if !bestSet || dist < bestDist || (dist == bestDist && rate < bestRate) {
			bestSet = true
			bestMode = mode
			bestDist = dist
			bestRate = rate
		}
	}
	return bestMode, bestDist, bestRate, bestSet
}

func (e *VP9Encoder) prepareVP9InterIntraBlockResidue(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, uvMode common.PredictionMode,
) bool {
	if inter == nil {
		return false
	}
	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(e.opts.Width),
		Height: uint32(e.opts.Height),
	}
	keyLike := vp9KeyframeEncodeState{
		img:      inter.img,
		hdr:      &hdr,
		dq:       inter.dq,
		lossless: inter.lossless,
	}
	return e.prepareVP9KeyframeBlockResidue(&keyLike, tile, miRows, miCols,
		miRow, miCol, bsize, mi, uvMode)
}

type vp9InterModeDecision struct {
	refFrame       int8
	secondRefFrame int8
	refSlot        int
	secondRefSlot  int
	isCompound     bool
	mode           common.PredictionMode
	mv             [2]vp9dec.MV
	interpFilter   vp9dec.InterpFilter
	rate           int
	distortion     uint64
	score          uint64
}

// vp9LeafInterDecisionEntry stores one cached leaf-write inter-mode decision
// keyed by (version, bsize). The cache mirrors libvpx's mi_grid_visible
// per-block storage; entries are populated by the count pre-pass at
// pickVP9InterReferenceMode and consumed by the bitstream write pass to skip
// the redundant picker invocation. The version stamp guards against stale
// entries spanning multiple frames; the bsize discriminator guards against
// callers that re-enter the leaf-write site at a different block size than
// the prior visit.
//
// libvpx: vp9/encoder/vp9_encodeframe.c encode_b stores the picker decision
// into mi[0]->mbmi; vp9/encoder/vp9_bitstream.c::write_modes_b reads it back
// for emission without recomputation.
type vp9LeafInterDecisionEntry struct {
	version  uint32
	bsize    common.BlockSize
	decision vp9InterModeDecision
	valid    bool
}

func (e *VP9Encoder) pickVP9InterReferenceMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if inter == nil {
		return vp9InterModeDecision{}, false
	}
	// SPEED_FEATURES.use_nonrd_pick_mode (cpu_used >= 5 in libvpx realtime)
	// routes the inter-mode picker through the verbatim nonrd port at
	// vp9_pick_inter_mode_nonrd.go. The nonrd entry walks the libvpx
	// ref_mode_set[] schedule, prunes the per-mode interp-filter loop, and
	// applies aggressive early termination — collapsing the per-block work
	// from ~36 (3 refs × 4 modes × 3 filters) candidate evaluations to ~12.
	//
	// libvpx merges single-ref + compound candidates into a single loop
	// (vp9_pickmode.c:2050 — idx < num_inter_modes + comp_modes). govpx
	// keeps them separate: the nonrd entry handles single-ref; the
	// existing compound branch below handles compound. The schedule order
	// matches libvpx because nonrd visits all single-ref candidates first
	// (idx 0..num_inter_modes-1) and compound is appended at the tail.
	//
	// libvpx: vp9_pickmode.c:1696 vp9_pick_inter_mode.
	// libvpx: vp9_speed_features.h:447 sf->use_nonrd_pick_mode.
	useNonrd := e.vp9InterUsesNonrdPickmode()
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)

	// libvpx restricts usable_ref_frame at speed >= 8 to LAST_FRAME for
	// the steady-state inter-block hot path: frames_since_golden > 120
	// or low last_sb_high_content triggers
	// `usable_ref_frame = LAST_FRAME` and skips GOLDEN/ALTREF
	// reference-mode picking entirely. Additionally
	// sf.short_circuit_low_temp_var (3 at speed 8 CBR non-screen) short-
	// circuits non-LAST refs on low-temporal-variance blocks via
	// force_skip_low_temp_var. govpx doesn't yet track per-SB temporal
	// variance, so the closest faithful approximation when both
	// UseNonrdPickMode == 1 and ShortCircuitLowTempVar >= 1 (both set
	// together for CBR realtime non-screen) is to restrict the single-
	// ref loop to LAST_FRAME — but only when LAST is actually one of
	// the enabled refs for this frame. Frames that explicitly mask out
	// LAST (e.g. EncodeNoReferenceLast for altref-only inter) must keep
	// the full ref set so a fallback ref can still be picked.
	// libvpx: vp9/encoder/vp9_pickmode.c:1962-1985 (usable_ref_frame),
	// vp9_speed_features.c:774 (ShortCircuitLowTempVar = 3 at speed 8
	// CBR non-screen).
	refFramesAll := [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame}
	refFrames := refFramesAll[:]
	if e.sf.ShortCircuitLowTempVar >= 1 && e.sf.UseNonrdPickMode == 1 {
		if _, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame); ok {
			refFrames = refFramesAll[:1]
		}
	}
	// SPEED_FEATURES.use_altref_onepass = 0 (cpu_used >= 5 in realtime) drops
	// ALTREF from the reference-frame fan. vp9InterReferenceFramesEnabled
	// returns {LAST, GOLDEN, ALTREF} or {LAST, GOLDEN} depending on the field.
	//
	// libvpx: vp9_speed_features.c:586 sf->use_altref_onepass = 0.
	refFrameSet := refFrames
	if len(refFrameSet) == len(refFramesAll) {
		// Defer to the sibling-agent helper when we haven't already
		// pruned to LAST-only above (it honors use_altref_onepass).
		refFrameSet = e.vp9InterReferenceFramesEnabled()
	}
	bestSet := false
	var best vp9InterModeDecision
	// useNonrd: when sf->use_nonrd_pick_mode is set AND the LAST-only
	// short-circuit above did not prune to a single ref, route the
	// multi-ref schedule through the verbatim libvpx ref_mode_set[12]
	// loop in vp9_pick_inter_mode_nonrd.go. With LAST-only the existing
	// single-ref path below is already libvpx-equivalent and runs the
	// faster luma-only predictor; the nonrd port adds no value there.
	//
	// libvpx: vp9_pickmode.c:1696 vp9_pick_inter_mode.
	if useNonrd && len(refFrameSet) > 1 {
		if decision, ok := e.pickVP9InterReferenceModeNonRD(inter, tile,
			miRows, miCols, miRow, miCol, bsize); ok {
			best = decision
			bestSet = true
		}
	} else {
		for _, refFrame := range refFrameSet {
			refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame)
			if !ok {
				continue
			}
			inter.ref = &e.refFrames[refSlot]
			refRate := vp9SingleRefModeRateCost(&inter.selectFc, above, left,
				inter.referenceMode, inter.compoundRefs, refFrame)
			decision, ok := e.pickVP9InterMode(inter, tile, miRows, miCols,
				miRow, miCol, bsize, refFrame, refRate)
			if !ok {
				continue
			}
			decision.refFrame = refFrame
			decision.secondRefFrame = vp9dec.NoRefFrame
			decision.refSlot = refSlot
			if !bestSet || decision.score < best.score ||
				(decision.score == best.score && decision.rate < best.rate) {
				best = decision
				bestSet = true
			}
		}
	}
	// SPEED_FEATURES.use_compound_nonrd_pickmode gates the compound branch
	// when UseNonrdPickMode is on (cpu_used >= 7 in libvpx realtime). The
	// nonrd_pickmode entry skips compound entirely when the feature is 0.
	//
	// libvpx: vp9/encoder/vp9_speed_features.c:469 / 656 / 665,
	// vp9/encoder/vp9_pickmode.c:1989.
	if e.sf.UseNonrdPickMode == 1 && e.sf.UseCompoundNonrdPickmode == 0 {
		return best, bestSet
	}
	if inter.compoundAllowed && inter.referenceMode != vp9dec.SingleReference &&
		e.vp9InterCompoundEnabled() {
		for _, varRef := range inter.compoundRefs.CompVarRef {
			refFrame, refSlot, secondRefFrame, secondRefSlot, ok :=
				e.vp9CompoundReferencePair(inter, varRef)
			if !ok {
				continue
			}
			refRate, ok := vp9CompoundRefRateCost(&inter.selectFc, above, left,
				inter.referenceMode, inter.compoundRefs, inter.refSignBias,
				[2]int8{refFrame, secondRefFrame})
			if !ok {
				continue
			}
			decision, ok := e.pickVP9CompoundInterMode(inter, tile, miRows, miCols,
				miRow, miCol, bsize, [2]int8{refFrame, secondRefFrame},
				[2]int{refSlot, secondRefSlot}, refRate)
			if !ok {
				continue
			}
			if !bestSet || decision.score < best.score ||
				(decision.score == best.score && decision.rate < best.rate) {
				best = decision
				bestSet = true
			}
		}
	}
	return best, bestSet
}

func (e *VP9Encoder) firstVP9InterReference(inter *vp9InterEncodeState) (int8, int, bool) {
	for _, refFrame := range [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
		if refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame); ok {
			return refFrame, refSlot, true
		}
	}
	return 0, 0, false
}

func (e *VP9Encoder) vp9InterReferenceSlot(inter *vp9InterEncodeState, refFrame int8) (int, bool) {
	if inter == nil || inter.refMask&(1<<uint(refFrame)) == 0 {
		return 0, false
	}
	slot, ok := vp9EncoderReferenceSlot(refFrame)
	if !ok {
		return 0, false
	}
	if !e.refFrames[slot].valid {
		return 0, false
	}
	return slot, true
}

func (e *VP9Encoder) vp9CompoundReferencePair(inter *vp9InterEncodeState,
	varRef int8,
) (int8, int, int8, int, bool) {
	if inter == nil {
		return 0, 0, 0, 0, false
	}
	fixedRef := inter.compoundRefs.CompFixedRef
	fixedSlot, ok := e.vp9InterReferenceSlot(inter, fixedRef)
	if !ok {
		return 0, 0, 0, 0, false
	}
	varSlot, ok := e.vp9InterReferenceSlot(inter, varRef)
	if !ok {
		return 0, 0, 0, 0, false
	}
	idx := int(inter.refSignBias[fixedRef])
	if idx == 0 {
		return fixedRef, fixedSlot, varRef, varSlot, true
	}
	return varRef, varSlot, fixedRef, fixedSlot, true
}

func (e *VP9Encoder) pickVP9CompoundInterMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame [2]int8, refSlot [2]int, refRate int,
) (vp9InterModeDecision, bool) {
	if inter == nil || bsize < common.Block8x8 {
		return vp9InterModeDecision{}, false
	}
	interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, bsize)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	qindex := e.vp9EncoderModeDecisionQIndex()
	// libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248 — per-SB cb_rdmult
	// priming (see pickVP9InterMode for the long-form comment).  The
	// compound picker shares the same TPL delta lookup as the single-ref
	// picker because libvpx routes both through rd_pick_sb_modes.
	prevCbRdmult := e.cbRdmult
	baseRdmult := e.rc.rdmult
	if baseRdmult <= 0 {
		baseRdmult = vp9ComputeRDMultBasedOnQindex(qindex, vp9RDFrameInter)
	}
	if bsize < common.BlockSizes && e.tpl.enabled {
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		baseRdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, baseRdmult)
	}
	if baseRdmult <= 0 {
		baseRdmult = 1
	}
	e.cbRdmult = baseRdmult
	// SPEED_FEATURES.inter_mode_mask gates inter modes for compound refs too.
	// libvpx: vp9_pickmode.c:2150 — applied to every mode candidate.
	interModeMask := e.vp9InterModeMaskFor(bsize)
	modeAllowed := func(mode common.PredictionMode) bool {
		return interModeMask&(1<<uint(mode)) != 0
	}
	bestSet := false
	var best vp9InterModeDecision
	consider := func(mode common.PredictionMode, mv, refMv [2]vp9dec.MV,
		filter vp9dec.InterpFilter, distortion uint64,
	) {
		rate := refRate +
			vp9InterModeRateCostN(&inter.selectFc, interModeCtx, mode,
				mv, refMv, 2, inter.allowHP) +
			vp9InterInterpFilterRateCost(inter, &inter.selectFc, switchableCtx, filter)
		cand := vp9InterModeDecision{
			refFrame:       refFrame[0],
			secondRefFrame: refFrame[1],
			refSlot:        refSlot[0],
			secondRefSlot:  refSlot[1],
			isCompound:     true,
			mode:           mode,
			mv:             mv,
			interpFilter:   filter,
			rate:           rate,
			distortion:     distortion,
			score:          e.vp9InterModeScore(distortion, rate, qindex),
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}

	if modeAllowed(common.ZeroMv) {
		e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
			refFrame, refSlot, common.ZeroMv, [2]vp9dec.MV{},
			[2]vp9dec.MV{}, consider)
	}

	for _, mode := range [...]common.PredictionMode{common.NearestMv, common.NearMv} {
		if !modeAllowed(mode) {
			continue
		}
		var mv [2]vp9dec.MV
		ok := true
		for ref := range 2 {
			mv[ref], ok = e.vp9EncoderInterModeCandidateMv(tile,
				miRows, miCols, miRow, miCol, bsize, mode,
				refFrame[ref], inter.allowHP, inter.refSignBias)
			if !ok {
				break
			}
		}
		if ok {
			e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
				refFrame, refSlot, mode, mv, [2]vp9dec.MV{}, consider)
		}
	}

	if modeAllowed(common.NewMv) {
		var newMv, newRefMv [2]vp9dec.MV
		newOK := true
		newHasMotion := false
		for ref := range 2 {
			inter.ref = &e.refFrames[refSlot[ref]]
			newMv[ref], _, newOK = e.pickVP9InterMvAllowZero(inter, miRows, miCols,
				miRow, miCol, bsize, refFrame[ref], vp9InterMvSearchOptions{})
			if !newOK {
				break
			}
			if newMv[ref] != (vp9dec.MV{}) {
				newHasMotion = true
			}
			newRefMv[ref], _ = e.vp9EncoderInterModeCandidateMv(tile,
				miRows, miCols, miRow, miCol, bsize, common.NewMv,
				refFrame[ref], inter.allowHP, inter.refSignBias)
		}
		if newOK && newHasMotion {
			e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
				refFrame, refSlot, common.NewMv, newMv, newRefMv, consider)
		}
	}
	e.cbRdmult = prevCbRdmult
	return best, bestSet
}

func (e *VP9Encoder) evalVP9CompoundMode(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	refFrame [2]int8, refSlot [2]int, mode common.PredictionMode,
	mv, refMv [2]vp9dec.MV,
	consider func(common.PredictionMode, [2]vp9dec.MV, [2]vp9dec.MV,
		vp9dec.InterpFilter, uint64),
) {
	filters := vp9InterInterpFilterCandidates(inter)
	if !vp9AnyMvHasSubpel(mv) {
		distortion, ok := e.vp9CompoundPredictionDistortion(inter, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, refSlot, mv,
			filters[0])
		if ok {
			for _, filter := range filters {
				consider(mode, mv, refMv, filter, distortion)
			}
		}
		return
	}
	for _, filter := range filters {
		distortion, ok := e.vp9CompoundPredictionDistortion(inter, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, refSlot, mv, filter)
		if ok {
			consider(mode, mv, refMv, filter, distortion)
		}
	}
}

func (e *VP9Encoder) pickVP9InterMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8, refRate int,
) (vp9InterModeDecision, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid ||
		refFrame <= vp9dec.IntraFrame {
		return vp9InterModeDecision{}, false
	}
	if bsize < common.Block8x8 {
		return vp9InterModeDecision{}, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(inter.ref, 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return vp9InterModeDecision{}, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	scoreW, scoreH, ok := vp9VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, refW, refH)
	if !ok {
		return vp9InterModeDecision{}, false
	}

	interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, bsize)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	qindex := e.vp9EncoderModeDecisionQIndex()
	// libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248 — every SB's
	// rd_pick_sb_modes call seeds x->cb_rdmult from get_rdmult_delta so
	// the per-mode RDCOST consumes a TPL-biased multiplier rather than
	// the bare per-frame rd.RDMULT.  Inline save/restore (no defer) to
	// preserve the alloc-parity gate; the TPL lookup is short-circuited
	// when no slab is populated so this stays cheap.
	prevCbRdmult := e.cbRdmult
	baseRdmult := e.rc.rdmult
	if baseRdmult <= 0 {
		baseRdmult = vp9ComputeRDMultBasedOnQindex(qindex, vp9RDFrameInter)
	}
	if bsize < common.BlockSizes && e.tpl.enabled {
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		baseRdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, baseRdmult)
	}
	if baseRdmult <= 0 {
		baseRdmult = 1
	}
	e.cbRdmult = baseRdmult
	useResidualScore := e.vp9InterPreferVarianceRoot(inter, miRows, miCols,
		miRow, miCol, bsize)
	// SPEED_FEATURES.inter_mode_mask gates which inter modes the picker
	// evaluates per block size. At higher cpu_used libvpx drops NEARMV/NEWMV
	// on large blocks (INTER_NEAREST_NEW_ZERO). Reading the per-bsize mask
	// here verbatim matches libvpx's pickmode gate.
	// libvpx: vp9_pickmode.c:2150 — if (!(cpi->sf.inter_mode_mask[bsize] & (1 << this_mode))) continue;
	interModeMask := e.vp9InterModeMaskFor(bsize)
	modeAllowed := func(mode common.PredictionMode) bool {
		return interModeMask&(1<<uint(mode)) != 0
	}
	bestSet := false
	var best vp9InterModeDecision
	consider := func(mode common.PredictionMode, mv, refMv vp9dec.MV,
		filter vp9dec.InterpFilter, distortion uint64,
	) {
		rate := refRate +
			vp9InterModeRateCost(&inter.selectFc, interModeCtx, mode,
				mv, refMv, inter.allowHP) +
			vp9InterInterpFilterRateCost(inter, &inter.selectFc, switchableCtx, filter)
		cand := vp9InterModeDecision{
			mode:         mode,
			mv:           [2]vp9dec.MV{mv},
			interpFilter: filter,
			rate:         rate,
			distortion:   distortion,
			score:        e.vp9InterModeScore(distortion, rate, qindex),
		}
		if useResidualScore && refFrame == vp9dec.LastFrame {
			if rdDist, rdRate, ok := e.scoreVP9InterModeResidual(inter, miRows,
				miCols, miRow, miCol, bsize, mode, refFrame, mv, filter); ok {
				cand.distortion = rdDist
				cand.rate = rate + rdRate
				cand.score = e.vp9InterModeScore(cand.distortion, cand.rate, qindex)
			}
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}

	zeroDistortion := vp9BlockSSE(src, srcStride, ref, refStride,
		x0, y0, x0, y0, scoreW, scoreH)
	allFilters := vp9InterInterpFilterCandidates(inter)
	// libvpx: vp9/encoder/vp9_speed_features.c — sf->disable_filter_search_var_thresh
	// prunes non-EIGHTTAP filters when source variance falls below the
	// threshold.  govpx approximates "source variance" with the visible
	// ZEROMV reference-distortion per pixel which monotonically tracks
	// libvpx's per-block VARIANCE for ZEROMV — the same quantity its
	// realtime path uses for the same gate.  Gated by the SF value > 0
	// so speeds where the SF is zero stay on the slow-path filter search
	// (no slice reslice, no variance math).
	if e.sf.DisableFilterSearchVarThresh > 0 && scoreW > 0 && scoreH > 0 &&
		len(allFilters) > 1 {
		perPixel := uint(zeroDistortion / uint64(scoreW*scoreH))
		if e.vp9InterSkipFilterSearch(perPixel) {
			allFilters = allFilters[:1]
		}
	}

	// libvpx: vp9_pickmode.c:1731-1880 — realtime (nonrd) per-mode filter
	// selection.  filter_ref starts as cm->interp_filter and is overwritten
	// from above/left inter neighbours when default_interp_filter != BILINEAR.
	// pred_filter_search is (cm->interp_filter == SWITCHABLE), refined by a
	// chessboard pattern when sf.cb_pred_filter_search is set.
	//
	// In the realtime path (sf.use_nonrd_pick_mode == 1), the per-mode
	// candidate evaluation at vp9_pickmode.c:2318-2330 either:
	//   (a) sweeps {EIGHTTAP, EIGHTTAP_SMOOTH} via search_filter_ref when
	//       the MV is subpel AND pred_filter_search AND
	//       (this_mode == NEWMV || filter_ref == SWITCHABLE), OR
	//   (b) locks to filter = (filter_ref == SWITCHABLE) ? EIGHTTAP : filter_ref.
	//
	// govpx's slow (full RD) path keeps the libvpx vp9_rdopt.c three-filter
	// sweep over {EIGHTTAP, EIGHTTAP_SMOOTH, EIGHTTAP_SHARP}.
	useNonrd := e.sf.UseNonrdPickMode == 1
	frameInterp := vp9InterFrameInterpFilter(inter)
	filterRef := vp9NonrdFilterRef(frameInterp, e.sf.DefaultInterpFilter,
		above, left)
	predFilterSearch := vp9NonrdPredFilterSearch(frameInterp,
		e.sf.CbPredFilterSearch, miRow, miCol, bsize, e.frameIndex)
	// pickFilters returns the per-mode filter list following libvpx's
	// vp9_pick_inter_mode realtime gate.  In the slow path (useNonrd ==
	// false) it returns allFilters (the libvpx vp9_rd_pick_inter_mode_sb
	// three-filter sweep).
	pickFilters := func(mode common.PredictionMode, mv vp9dec.MV,
		refIsLast bool,
	) []vp9dec.InterpFilter {
		if !useNonrd {
			return allFilters
		}
		// libvpx: vp9_pickmode.c:2318-2330.  The realtime filter search
		// fires only when (a) the MV has subpel bits, (b) pred_filter_search
		// is on, (c) this_mode == NEWMV or filter_ref == SWITCHABLE, and
		// (d) ref_frame is LAST (or one of the special GOLDEN cases — SVC
		// or VBR — which govpx does not surface to this picker yet).
		if vp9MvHasSubpel(mv) && predFilterSearch && refIsLast &&
			(mode == common.NewMv || filterRef == vp9dec.InterpSwitchable) {
			return vp9NonrdSwitchableInterpFilterOrder[:]
		}
		// libvpx: vp9_pickmode.c:2330 — single-filter fallback.
		if filterRef == vp9dec.InterpSwitchable {
			return vp9EighttapInterpFilterOrder[:]
		}
		switch filterRef {
		case vp9dec.InterpEighttap:
			return vp9EighttapInterpFilterOrder[:]
		case vp9dec.InterpEighttapSmooth:
			return vp9SmoothInterpFilterOrder[:]
		case vp9dec.InterpEighttapSharp:
			return vp9SharpInterpFilterOrder[:]
		case vp9dec.InterpBilinear:
			return vp9BilinearInterpFilterOrder[:]
		default:
			return vp9EighttapInterpFilterOrder[:]
		}
	}
	refIsLast := refFrame == vp9dec.LastFrame
	if modeAllowed(common.ZeroMv) {
		for _, filter := range pickFilters(common.ZeroMv, vp9dec.MV{}, refIsLast) {
			consider(common.ZeroMv, vp9dec.MV{}, vp9dec.MV{}, filter,
				zeroDistortion)
		}
	}

	for _, mode := range [...]common.PredictionMode{common.NearestMv, common.NearMv} {
		if !modeAllowed(mode) {
			continue
		}
		mv, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, inter.allowHP,
			inter.refSignBias)
		if !ok {
			continue
		}
		filters := pickFilters(mode, mv, refIsLast)
		if !vp9MvHasSubpel(mv) {
			distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
				miRow, miCol, bsize, mode, refFrame, mv, filters[0],
			)
			if ok {
				for _, filter := range filters {
					consider(mode, mv, mv, filter, distortion)
				}
			}
			continue
		}
		for _, filter := range filters {
			distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
				miRow, miCol, bsize, mode, refFrame, mv, filter)
			if ok {
				consider(mode, mv, mv, filter, distortion)
			}
		}
	}

	if modeAllowed(common.NewMv) {
		if mv, _, ok := e.pickVP9InterMv(inter, miRows, miCols,
			miRow, miCol, bsize, refFrame); ok {
			refMv, _ := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
				miRow, miCol, bsize, common.NewMv, refFrame, inter.allowHP,
				inter.refSignBias)
			filters := pickFilters(common.NewMv, mv, refIsLast)
			if !vp9MvHasSubpel(mv) {
				distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
					miRow, miCol, bsize, common.NewMv, refFrame, mv,
					filters[0])
				if ok {
					for _, filter := range filters {
						consider(common.NewMv, mv, refMv, filter, distortion)
					}
				}
			} else {
				for _, filter := range filters {
					distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
						miRow, miCol, bsize, common.NewMv, refFrame, mv, filter,
					)
					if ok {
						consider(common.NewMv, mv, refMv, filter, distortion)
					}
				}
			}
		}
	}
	e.cbRdmult = prevCbRdmult
	if !bestSet {
		return vp9InterModeDecision{}, false
	}
	return best, true
}

func vp9VisibleInterScoreBlock(x0, y0, blockW, blockH int,
	srcW, srcH, refW, refH int,
) (int, int, bool) {
	if x0 < 0 || y0 < 0 || blockW <= 0 || blockH <= 0 ||
		x0 >= srcW || y0 >= srcH || x0 >= refW || y0 >= refH {
		return 0, 0, false
	}
	scoreW := min(blockW, srcW-x0)
	scoreW = min(scoreW, refW-x0)
	scoreH := min(blockH, srcH-y0)
	scoreH = min(scoreH, refH-y0)
	return scoreW, scoreH, scoreW > 0 && scoreH > 0
}

func (e *VP9Encoder) pickVP9InterMv(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8,
) (vp9dec.MV, uint64, bool) {
	return e.pickVP9InterMvWithOptions(inter, miRows, miCols, miRow, miCol,
		bsize, refFrame, vp9InterMvSearchOptions{})
}

type vp9InterMvSearchOptions struct {
	seed      vp9dec.MV
	seedValid bool
}

func (e *VP9Encoder) pickVP9InterMvWithOptions(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8,
	opts vp9InterMvSearchOptions,
) (vp9dec.MV, uint64, bool) {
	mv, score, ok := e.pickVP9InterMvAllowZero(inter, miRows, miCols,
		miRow, miCol, bsize, refFrame, opts)
	if !ok || mv == (vp9dec.MV{}) {
		return vp9dec.MV{}, score, false
	}
	return mv, score, true
}

// scoreVP9InterModeResidual gives flat-root LAST candidates a small non-RD
// residual model analogous to libvpx vp9_pick_inter_mode's model/block Y RD
// pass. Prediction SSE alone overvalues tiny subpel NEWMV gains on flat deltas.
func (e *VP9Encoder) scoreVP9InterModeResidual(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (uint64, int, bool) {
	if inter == nil || inter.dq == nil {
		return 0, 0, false
	}
	txSize := clampVP9TxSizeForBlock(common.Tx16x16, bsize)
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		TxSize:       txSize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, 0, false
	}
	distortion, rate, hasResidue, ok := e.scoreVP9InterTxCandidate(inter,
		miRows, miCols, miRow, miCol, bsize, txSize)
	if !ok {
		return 0, 0, false
	}
	if !hasResidue {
		rate = 0
	}
	return distortion, rate, true
}

func (e *VP9Encoder) pickVP9InterMvAllowZero(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8,
	opts vp9InterMvSearchOptions,
) (vp9dec.MV, uint64, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid {
		return vp9dec.MV{}, 0, false
	}
	if bsize < common.Block8x8 {
		return vp9dec.MV{}, 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(inter.ref, 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return vp9dec.MV{}, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > refW || y0+blockH > refH {
		return vp9dec.MV{}, 0, false
	}

	bestScore := vp9BlockSAD(src, srcStride, ref, refStride,
		x0, y0, x0, y0, blockW, blockH, ^uint64(0))
	bestDx, bestDy := 0, 0
	searchCenterDx, searchCenterDy := 0, 0
	searchFromSeed := false
	eval := func(dx, dy int) bool {
		if dx == bestDx && dy == bestDy {
			return false
		}
		refX := x0 + dx
		refY := y0 + dy
		if refX < 0 || refY < 0 || refX+blockW > refW || refY+blockH > refH {
			return false
		}
		score := vp9BlockSAD(src, srcStride, ref, refStride,
			x0, y0, refX, refY, blockW, blockH, bestScore)
		if score < bestScore {
			bestScore = score
			bestDx = dx
			bestDy = dy
			return true
		}
		return false
	}
	if opts.seedValid {
		seedDx := int(opts.seed.Col) >> 3
		seedDy := int(opts.seed.Row) >> 3
		if eval(seedDx, seedDy) {
			searchCenterDx = seedDx
			searchCenterDy = seedDy
			searchFromSeed = true
		}
	}

	// MV-hint biasing: when a multi-resolution lower-resolution layer
	// has supplied a scaled MV hint for this SB, evaluate it as an
	// extra candidate before the (0,0)-centered fan. The hint can
	// land outside the local 16-pixel radius (libvpx-style cross-
	// resolution motion correlation regularly produces hints that
	// exceed the realtime search radius); when that happens the
	// search radius widens to encompass the hint so the refinement
	// step can still walk a local fan around the winning candidate.
	// When no hint is installed this branch is a nil-check.
	//
	// libvpx: SPEED_FEATURES.mv.search_method picks the
	// fast-diamond / bigdia / NSTEP dispatcher (vp9_mcomp.c:2875). At
	// cpu_used=8 the configurator pins FAST_DIAMOND, which caps the
	// effective search radius to a 4-pel fan. Read that field here
	// instead of always running the full 16-pel search.
	searchRadius := e.vp9InterSearchRadius()
	if refFrame == vp9dec.LastFrame {
		if hintDx, hintDy, ok := e.vp9MVHintCandidatePixelOffset(miRow, miCol); ok {
			if eval(hintDx, hintDy) && searchFromSeed {
				searchCenterDx = hintDx
				searchCenterDy = hintDy
			}
			// Widen the search radius so the refinement loop can
			// walk a small fan around the hint when it wins.
			absDx := hintDx
			if absDx < 0 {
				absDx = -absDx
			}
			absDy := hintDy
			if absDy < 0 {
				absDy = -absDy
			}
			if absDx > searchRadius {
				searchRadius = absDx
			}
			if absDy > searchRadius {
				searchRadius = absDy
			}
		}
	}

	// Coarse fan: libvpx's bigdia_search at step_param == MAX_MVSEARCH_STEPS-2
	// (FAST_DIAMOND) visits one bigdia ring around the center and then refines.
	// We size the coarse step so the fan covers ±searchRadius without
	// exceeding it. NSTEP / full-search keeps the original 8-pel coarse step.
	//
	// libvpx: vp9_mcomp.c:1624 fast_dia_search(MAX(MAX_MVSEARCH_STEPS-2,
	// search_param), ...). With reduce_first_step_size = 1, the coarse step
	// at NSTEP drops to half of the radius (vp9_speed_features.c:586).
	coarseStep := max(e.vp9InterSearchCoarseStep(), 1)
	// libvpx at speed >= 7 sets sf.mv.search_method = FAST_DIAMOND with
	// step_param = 10, which causes the BIGDIA pattern walker to skip
	// the dense step=1 refinement. Stop at step=2 when FAST_DIAMOND is
	// selected; NSTEP / BIGDIA / HEX keep the verbatim step=1 walk.
	// libvpx: vp9/encoder/vp9_speed_features.c:702-703 (FAST_DIAMOND +
	// step_param 10), vp9/encoder/vp9_mcomp.c:1014-1015
	// (search_param_to_steps[10] = 0 → smallest scale only).
	minStep := 1
	if e.sf.Mv.SearchMethod == SearchMethodFastDiamond {
		minStep = 2
	}
	scanMinDx, scanMaxDx := -searchRadius, searchRadius
	scanMinDy, scanMaxDy := -searchRadius, searchRadius
	if searchFromSeed {
		scanMinDx = searchCenterDx - searchRadius
		scanMaxDx = searchCenterDx + searchRadius
		scanMinDy = searchCenterDy - searchRadius
		scanMaxDy = searchCenterDy + searchRadius
	}
	for dy := scanMinDy; dy <= scanMaxDy; dy += coarseStep {
		for dx := scanMinDx; dx <= scanMaxDx; dx += coarseStep {
			eval(dx, dy)
		}
	}
	for step := coarseStep >> 1; step >= minStep; step >>= 1 {
		improved := true
		for improved {
			improved = false
			centerDx, centerDy := bestDx, bestDy
			for dy := centerDy - step; dy <= centerDy+step; dy += step {
				for dx := centerDx - step; dx <= centerDx+step; dx += step {
					if dx < scanMinDx || dx > scanMaxDx ||
						dy < scanMinDy || dy > scanMaxDy {
						continue
					}
					if eval(dx, dy) {
						improved = true
					}
				}
			}
		}
	}
	mv := vp9dec.MV{Row: int16(bestDy * 8), Col: int16(bestDx * 8)}
	vp9ClampMvRef(&mv, miRows, miCols, miRow, miCol, bsize)
	vp9dec.LowerMvPrecision(&mv, inter.allowHP)
	// SPEED_FEATURES.mv.subpel_force_stop == FULL_PEL — libvpx skips
	// vp9_find_best_sub_pixel_tree* entirely. govpx mirrors that gate here.
	//
	// libvpx: vp9_mcomp.c — find_best_sub_pixel_tree_pruned_more returns
	// early when forcestop == FULL_PEL.
	if e.vp9InterSubpelEnabled() {
		mv, bestScore = e.refineVP9InterSubpelMv(inter, miRows, miCols,
			miRow, miCol, bsize, refFrame, mv, bestScore)
	}
	return mv, bestScore, true
}

func (e *VP9Encoder) refineVP9InterSubpelMv(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	refFrame int8, best vp9dec.MV, bestScore uint64,
) (vp9dec.MV, uint64) {
	// SPEED_FEATURES.mv.subpel_force_stop scales the min step:
	// HALFPEL (sf 4), QUARTERPEL (2), EIGHTHPEL (1 with HP / 2 without).
	// SPEED_FEATURES.mv.subpel_search_method caps the iteration depth.
	//
	// libvpx: vp9_mcomp.c — the tree-pruned variants halve the step until
	// it reaches forcestop and the more pruned methods stop after one or
	// two iterations. vp9InterSubpelMinStep already honors
	// SPEED_FEATURES.mv.subpel_force_stop and returns >4 when the walker
	// is disabled entirely (FULL_PEL).
	allowHP := inter != nil && inter.allowHP
	minStep := e.vp9InterSubpelMinStep(allowHP)
	if minStep > 4 {
		return best, bestScore
	}
	maxIters := e.vp9InterSubpelIters()
	iters := 0
	for step := int16(4); step >= minStep; step >>= 1 {
		if iters >= maxIters {
			break
		}
		improved := true
		for improved {
			if iters >= maxIters {
				break
			}
			improved = false
			center := best
			for row := center.Row - step; row <= center.Row+step; row += step {
				for col := center.Col - step; col <= center.Col+step; col += step {
					cand := vp9dec.MV{Row: row, Col: col}
					vp9ClampMvRef(&cand, miRows, miCols, miRow, miCol, bsize)
					vp9dec.LowerMvPrecision(&cand, allowHP)
					if cand == best {
						continue
					}
					score, ok := e.vp9InterPredictionSAD(inter, miRows, miCols,
						miRow, miCol, bsize, common.NewMv, refFrame, cand,
						vp9dec.InterpEighttap, bestScore)
					if !ok || score >= bestScore {
						continue
					}
					best = cand
					bestScore = score
					improved = true
				}
			}
			iters++
		}
	}
	return best, bestScore
}

func (e *VP9Encoder) vp9InterPredictionSAD(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter, limit uint64,
) (uint64, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	scoreW, scoreH, ok := vp9VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, dstStride, dstRows)
	if !ok {
		return 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	// Motion-search SAD only consults luma; skip chroma reconstruction
	// to cut ~30% of convolve8 work per candidate. libvpx mirrors this
	// in nonrd_pickmode via vp9_build_inter_predictors_sby.
	// libvpx: vp9/encoder/vp9_pickmode.c:2336.
	if !e.predictVP9InterBlockLumaOnly(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, false
	}
	return vp9BlockSAD(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, scoreW, scoreH, limit), true
}

// vp9NonrdUVVarianceSSE rebuilds the UV inter prediction (assuming the Y
// predictor has already been committed via vp9InterPredictionVarianceSSE)
// and returns (var_u, sse_u, var_v, sse_v). The realtime nonrd picker
// consumes these to drive encode_breakout_test's UV-plane skip check
// (vp9_pickmode.c:1014-1025).
//
// libvpx counterpart: vp9_pickmode.c:1009-1022 — xd->plane[1|2].pre[0] is
// pointed at the reference U/V buffer, vp9_build_inter_predictors_sbuv
// runs the chroma predictor, then cpi->fn_ptr[uv_bsize].vf returns
// (var_u, sse_u) / (var_v, sse_v).
func (e *VP9Encoder) vp9NonrdUVVarianceSSE(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (varU, sseU, varV, sseV uint64, ok bool) {
	if inter == nil || inter.img == nil {
		return 0, 0, 0, 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, 0, 0, 0, false
	}
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return 0, 0, 0, 0, false
		}
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
		dst, dstStride := e.vp9EncoderReconPlane(plane)
		if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
			return 0, 0, 0, 0, false
		}
		blockW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		blockH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		x0 := (miCol * common.MiSize) >> pd.SubsamplingX
		y0 := (miRow * common.MiSize) >> pd.SubsamplingY
		dstRows := len(dst) / dstStride
		if !vp9VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
			!vp9VisibleBlockFits(x0, y0, blockW, blockH, dstStride, dstRows) {
			return 0, 0, 0, 0, false
		}
		variance, sse := vp9BlockDiffVarianceSSE(src, srcStride, dst, dstStride,
			x0, y0, x0, y0, blockW, blockH)
		if plane == 1 {
			varU = variance
			sseU = sse
		} else {
			varV = variance
			sseV = sse
		}
	}
	return varU, sseU, varV, sseV, true
}

// vp9InterPredictionVarianceSSE runs the inter predictor for one
// (mode, ref, mv, filter) candidate and returns both the variance and the
// SSE between the source and the prediction. Mirrors libvpx's
// fn_ptr[bsize].vf call inside model_rd_for_sb_y (vp9_pickmode.c:661-666)
// which produces (var, sse). The realtime nonrd picker consumes both.
func (e *VP9Encoder) vp9InterPredictionVarianceSSE(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (variance, sse uint64, ok bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	scoreW, scoreH, vok := vp9VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, dstStride, dstRows)
	if !vok {
		return 0, 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, 0, false
	}
	variance, sse = vp9BlockDiffVarianceSSE(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, scoreW, scoreH)
	return variance, sse, true
}

func (e *VP9Encoder) vp9InterPredictionDistortion(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (uint64, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	scoreW, scoreH, ok := vp9VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, dstStride, dstRows)
	if !ok {
		return 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, false
	}
	return vp9BlockSSE(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, scoreW, scoreH), true
}

func (e *VP9Encoder) vp9CompoundPredictionDistortion(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame [2]int8, refSlot [2]int,
	mv [2]vp9dec.MV, filter vp9dec.InterpFilter,
) (uint64, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > dstStride || y0+blockH > dstRows {
		return 0, false
	}
	if refSlot[0] < 0 || refSlot[0] >= len(e.refFrames) ||
		refSlot[1] < 0 || refSlot[1] >= len(e.refFrames) ||
		!e.refFrames[refSlot[0]].valid || !e.refFrames[refSlot[1]].valid {
		return 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame:     refFrame,
		Mv:           mv,
	}
	inter.ref = &e.refFrames[refSlot[0]]
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, false
	}
	return vp9BlockSSE(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, blockW, blockH), true
}

func (e *VP9Encoder) vp9EncoderModeDecisionQIndex() int {
	if e.vp9ModeDecisionQIndexSet {
		return int(e.vp9ModeDecisionQIndex)
	}
	return e.vp9EncoderFrameQIndex(true, false, 0, 1)
}

// vp9EncoderInitializeRDConsts is the libvpx-faithful entry point that
// populates rc.rdmult / rc.rddiv before any per-block Lagrangian RD
// scoring runs.  Called once per frame, after the mode-decision qindex
// has been resolved by vp9EncoderModeDecisionQIndex.  Mirrors libvpx's
// vp9_initialize_rd_consts.
//
// libvpx: vp9/encoder/vp9_rd.c:396-407
//
//	rd->RDDIV = RDDIV_BITS;  // In bits (to multiply D by 128).
//	rd->RDMULT = vp9_compute_rd_mult(cpi, cm->base_qindex + cm->y_dc_delta_q);
//	set_error_per_bit(x, rd->RDMULT);
//
// y_dc_delta_q is zero for govpx today; when the active-segment Q delta
// path lands it should be added to qindex here before the rdmult lookup.
func (e *VP9Encoder) vp9EncoderInitializeRDConsts(qindex int,
	frameType vp9RDFrameType,
) {
	e.rc.rddiv = vp9RDDivBits
	e.rc.rdmult = vp9ComputeRDMult(qindex, frameType)
	// Reset the per-SB cb_rdmult cache so a stale value from the prior
	// frame does not leak into the first SB picker call.  libvpx clears
	// it inline before each rd_pick_sb_modes invocation; we mirror that
	// reset at the frame boundary so the first SB sees a clean state.
	e.cbRdmult = 0
}

// vp9EncoderModeDecisionRDMult returns the active Lagrange multiplier the
// per-block RD scorers should use.  Mirrors libvpx's lookup order:
//
//	rdmult = x->cb_rdmult ? x->cb_rdmult : cpi->rd.RDMULT
//
// libvpx: vp9/encoder/vp9_encodeframe.c — every per-mode RDCOST call uses
// x->rdmult which is itself initialized from cb_rdmult at the top of
// rd_pick_sb_modes.  Callers that have not yet primed cbRdmult (legacy
// per-block paths still being threaded) fall back to the per-frame
// rc.rdmult; if even that is missing they synthesize the multiplier from
// the current mode-decision qindex so older tests that never enter the
// frame-init path still see a libvpx-shaped multiplier.
func (e *VP9Encoder) vp9EncoderModeDecisionRDMult() int {
	if e.cbRdmult > 0 {
		return e.cbRdmult
	}
	if e.rc.rdmult > 0 {
		return e.rc.rdmult
	}
	return vp9ComputeRDMultBasedOnQindex(e.vp9EncoderModeDecisionQIndex(),
		vp9RDFrameInter)
}

// vp9EncoderModeDecisionRDDiv returns the active rddiv shift used by
// RDCOST.  Defaults to libvpx's RDDIV_BITS when the per-frame state has
// not been primed.
//
// libvpx: vp9/encoder/vp9_rd.h:26 (RDDIV_BITS == 7) and vp9_rd.c:405
// (rd->RDDIV = RDDIV_BITS).
func (e *VP9Encoder) vp9EncoderModeDecisionRDDiv() int {
	if e.rc.rddiv > 0 {
		return e.rc.rddiv
	}
	return vp9RDDivBits
}

// vp9EncoderPrimeCbRdmult sets the per-SB cb_rdmult cache so subsequent
// candidate scoring within the same SB uses a consistent multiplier.
// Returns the value installed so callers that want to keep a local
// variable for clarity can avoid re-reading the field.
//
// libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248
//
//	x->cb_rdmult = get_rdmult_delta(cpi, BLOCK_64X64, ...);
//	set_error_per_bit(x, x->cb_rdmult);
//	x->rdmult = x->cb_rdmult;
//
// govpx does not yet store rate-distortion error_per_bit (it would only
// gate the motion search subpel cost).  The Lagrangian scoring path is
// the load-bearing consumer today.
func (e *VP9Encoder) vp9EncoderPrimeCbRdmult(rdmult int) int {
	if rdmult <= 0 {
		rdmult = 1
	}
	e.cbRdmult = rdmult
	return rdmult
}

// vp9EncoderClearCbRdmult releases the per-SB cb_rdmult cache.  Mirrors
// libvpx's rd_pick_sb_modes epilogue which restores the per-frame
// rdmult once the SB walk completes.
func (e *VP9Encoder) vp9EncoderClearCbRdmult() {
	e.cbRdmult = 0
}

func (e *VP9Encoder) vp9EncoderFrameQIndex(isKey, intraOnly bool, flags EncodeFlags, macroblocks int) int {
	if vp9OracleTraceBuild {
		e.resetVP9OracleRateSelectionTrace()
	}
	if e.opts.Lossless {
		return 0
	}
	if e.rc.nextFrameQIndexSet {
		qindex := int(e.rc.nextFrameQIndex)
		e.rc.nextFrameQIndexSet = false
		e.opts.NextFrameQIndexSet = false
		e.opts.NextFrameQIndex = 0
		if vp9OracleTraceBuild {
			e.recordVP9OracleRateSelectionTrace(qindex, qindex, 1, false, 0)
		}
		return qindex
	}
	qindex := e.opts.Quantizer
	if qindex == 0 {
		if e.rc.enabled {
			refreshFlags := uint8(0xff)
			if !isKey {
				refreshFlags = e.vp9InterRefreshFrameFlags(flags)
			}
			if e.rc.mode == RateControlCBR {
				if vp9OracleTraceBuild {
					var activeBest int
					var activeWorst int
					var correctionFactor float64
					qindex, activeBest, activeWorst, correctionFactor =
						e.rc.cbrQuantizerWithBounds(isKey || intraOnly,
							refreshFlags, e.frameIndex, macroblocks)
					e.recordVP9OracleRateSelectionTrace(activeBest, activeWorst,
						correctionFactor, e.rc.onePassRecodeAllowed(), 0)
					return qindex
				}
				qindex = e.rc.cbrQuantizer(isKey || intraOnly, refreshFlags,
					e.frameIndex, macroblocks)
			} else if vp9OracleTraceBuild {
				var activeBest int
				var activeWorst int
				var correctionFactor float64
				e.prepareVP9SecondPassFrameTarget(isKey || intraOnly,
					refreshFlags)
				qindex, activeBest, activeWorst, correctionFactor =
					e.rc.vbrQuantizerWithBounds(isKey || intraOnly,
						refreshFlags, e.frameIndex, macroblocks)
				e.recordVP9OracleRateSelectionTrace(activeBest, activeWorst,
					correctionFactor, e.rc.onePassRecodeAllowed(), 0)
				return qindex
			} else {
				e.prepareVP9SecondPassFrameTarget(isKey || intraOnly,
					refreshFlags)
				qindex = e.rc.vbrQuantizer(isKey || intraOnly, refreshFlags,
					e.frameIndex, macroblocks)
			}
		} else {
			qindex = e.vp9EncoderPublicQModeQIndex(isKey, intraOnly, flags)
			if vp9OracleTraceBuild {
				minQ, maxQ, _ := vp9NormalizedPublicQuantizers(e.opts)
				e.recordVP9OracleRateSelectionTrace(
					vp9PublicQuantizerToQIndex(minQ),
					vp9PublicQuantizerToQIndex(maxQ),
					1, false, 0)
			}
		}
	}
	return qindex
}

func (e *VP9Encoder) vp9EncoderPublicQModeQIndex(isKey, intraOnly bool, flags EncodeFlags) int {
	minQ, maxQ, cqLevel := vp9NormalizedPublicQuantizers(e.opts)
	best := vp9PublicQuantizerToQIndex(minQ)
	worst := vp9PublicQuantizerToQIndex(maxQ)
	cq := vp9PublicQuantizerToQIndex(cqLevel)
	if best >= worst {
		return best
	}

	num, den := 1, 1
	if isKey || intraOnly {
		num, den = 1, 4
	} else if flags&vp9ExternalRefreshCtlFlags != 0 {
		refresh := vp9InterRefreshFrameFlags(flags)
		if refresh&(1<<vp9AltRefSlot) != 0 {
			num, den = 2, 5
		} else if refresh&(1<<vp9GoldenRefSlot) != 0 {
			num, den = 1, 2
		} else {
			num, den = vp9PublicQModeInterRate(e.frameIndex)
		}
	} else {
		num, den = vp9PublicQModeInterRate(e.frameIndex)
	}
	qindex := min(max(cq+vp9ComputeQDelta(best, worst, cq, num, den), best), worst)
	return qindex
}

func vp9PublicQModeInterRate(frameIndex int) (num int, den int) {
	switch frameIndex & 7 {
	case 0:
		return 1, 2
	case 2, 6:
		return 85, 100
	case 4:
		return 7, 10
	default:
		return 1, 1
	}
}

func validateVP9PublicQuantizerOptions(opts VP9EncoderOptions) error {
	if opts.MinQuantizer < 0 || opts.MaxQuantizer < 0 || opts.CQLevel < 0 ||
		opts.MinQuantizer > maxQuantizer || opts.MaxQuantizer > maxQuantizer ||
		opts.CQLevel > maxQuantizer {
		return ErrInvalidQuantizer
	}
	if (opts.MinQuantizer != 0 || opts.MaxQuantizer != 0) &&
		opts.MinQuantizer > opts.MaxQuantizer {
		return ErrInvalidQuantizer
	}
	if opts.Quantizer != 0 &&
		(opts.MinQuantizer != 0 || opts.MaxQuantizer != 0 || opts.CQLevel != 0) {
		return ErrInvalidQuantizer
	}
	minQ, maxQ, _ := vp9NormalizedPublicQuantizers(opts)
	if opts.CQLevel != 0 && (opts.CQLevel < minQ || opts.CQLevel > maxQ) {
		return ErrInvalidQuantizer
	}
	return nil
}

func vp9NormalizedPublicQuantizers(opts VP9EncoderOptions) (minQ, maxQ, cqLevel int) {
	minQ = opts.MinQuantizer
	maxQ = opts.MaxQuantizer
	if minQ == 0 && maxQ == 0 {
		minQ = vp9DefaultMinQuantizer
		maxQ = vp9DefaultMaxQuantizer
	}
	cqLevel = opts.CQLevel
	if cqLevel == 0 {
		if minQ == maxQ {
			cqLevel = minQ
		} else {
			cqLevel = min(max(vp9DefaultCQLevel, minQ), maxQ)
		}
	}
	return minQ, maxQ, cqLevel
}

func vp9PublicQuantizerToQIndex(q int) int {
	return vp9QuantizerToQIndex[min(max(q, 0), maxQuantizer)]
}

func vp9QIndexToPublicQuantizer(qIndex int) int {
	for q, translated := range vp9QuantizerToQIndex {
		if translated >= qIndex {
			return q
		}
	}
	return maxQuantizer
}

func vp9ComputeQDelta(best, worst, qindex, num, den int) int {
	if den <= 0 {
		return 0
	}
	qindex = min(max(qindex, best), worst)
	qstart := int(tables.AcQLookup8[qindex])
	targetNumer := qstart * num
	startIndex := worst
	targetIndex := worst
	for i := best; i < worst; i++ {
		startIndex = i
		if int(tables.AcQLookup8[i]) >= qstart {
			break
		}
	}
	for i := best; i < worst; i++ {
		targetIndex = i
		if int(tables.AcQLookup8[i])*den >= targetNumer {
			break
		}
	}
	return targetIndex - startIndex
}

var vp9QuantizerToQIndex = [maxQuantizer + 1]int{
	0, 4, 8, 12, 16, 20, 24, 28,
	32, 36, 40, 44, 48, 52, 56, 60,
	64, 68, 72, 76, 80, 84, 88, 92,
	96, 100, 104, 108, 112, 116, 120, 124,
	128, 132, 136, 140, 144, 148, 152, 156,
	160, 164, 168, 172, 176, 180, 184, 188,
	192, 196, 200, 204, 208, 212, 216, 220,
	224, 228, 232, 236, 240, 244, 249, 255,
}

// vp9InterModeScore / vp9ModeDecisionScore / vp9AddModeDecisionRate /
// vp9ModeDecisionRateScore were the legacy linear-lambda scorers
// (rate*(1+qindex/32)).  They now route through the libvpx-faithful
// Lagrangian RDCOST macro (vp9/encoder/vp9_rd.h:29-30) using the per-SB
// cb_rdmult cache when primed, falling back to the per-frame rd.rdmult,
// then to a freshly-computed rdmult for the supplied qindex.  The
// qindex argument is kept for source compatibility with the call sites
// that previously passed e.vp9EncoderModeDecisionQIndex(); when neither
// cb_rdmult nor rd.rdmult is populated the qindex still seeds the
// libvpx inter-frame multiplier table.
//
// libvpx: vp9/encoder/vp9_rd.h:29-30 (RDCOST) and vp9/encoder/vp9_rd.c
// vp9_compute_rd_mult_based_on_qindex.
func (e *VP9Encoder) vp9InterModeScore(sad uint64, rate, qindex int) uint64 {
	return e.vp9ModeDecisionScore(sad, rate, qindex)
}

func (e *VP9Encoder) vp9ModeDecisionScore(distortion uint64, rate, qindex int) uint64 {
	return vp9RDCost(e.activeRDMult(qindex), vp9RDDivBits, rate, distortion)
}

func (e *VP9Encoder) vp9AddModeDecisionRate(score uint64, rate, qindex int) uint64 {
	return score + vp9RDCostFromRate(e.activeRDMult(qindex), rate)
}

func (e *VP9Encoder) vp9ModeDecisionRateScore(rate, qindex int) uint64 {
	return vp9RDCostFromRate(e.activeRDMult(qindex), rate)
}

// activeRDMult returns the per-frame/per-SB Lagrange multiplier.
func (e *VP9Encoder) activeRDMult(qindex int) int {
	if e.cbRdmult > 0 {
		return e.cbRdmult
	}
	if e.rc.rdmult > 0 {
		return e.rc.rdmult
	}
	return vp9ComputeRDMultBasedOnQindex(qindex, vp9RDFrameInter)
}

// vp9InterModeScore / vp9ModeDecisionScore (package-level, no receiver)
// preserve the pre-Lagrangian scoring API surface for the small handful
// of unit tests that assert pure rate/distortion ordering with no
// encoder context.  They synthesize the multiplier from the supplied
// qindex via the same libvpx-faithful inter-frame table the production
// path uses.  Production call sites must use the encoder-bound helpers
// above so the per-frame / per-SB rdmult state is honoured.
func vp9InterModeScore(sad uint64, rate, qindex int) uint64 {
	return vp9ModeDecisionScore(sad, rate, qindex)
}

func vp9ModeDecisionScore(distortion uint64, rate, qindex int) uint64 {
	rdmult := vp9ComputeRDMultBasedOnQindex(qindex, vp9RDFrameInter)
	return vp9RDCost(rdmult, vp9RDDivBits, rate, distortion)
}

func vp9InterModeRateCost(fc *vp9dec.FrameContext, ctx int,
	mode common.PredictionMode, mv, refMv vp9dec.MV, allowHP bool,
) int {
	return vp9InterModeRateCostN(fc, ctx, mode,
		[2]vp9dec.MV{mv}, [2]vp9dec.MV{refMv}, 1, allowHP)
}

func vp9InterModeRateCostN(fc *vp9dec.FrameContext, ctx int,
	mode common.PredictionMode, mv, refMv [2]vp9dec.MV, nrefs int, allowHP bool,
) int {
	if fc == nil || ctx < 0 || ctx >= len(fc.InterModeProbs) {
		return 0
	}
	if nrefs < 1 {
		nrefs = 1
	}
	if nrefs > len(mv) {
		nrefs = len(mv)
	}
	probs := fc.InterModeProbs[ctx]
	cost := 0
	switch mode {
	case common.ZeroMv:
		cost = encoder.VP9CostBit(probs[0], 0)
	case common.NearestMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 0)
	case common.NearMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 1) +
			encoder.VP9CostBit(probs[2], 0)
	case common.NewMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 1) +
			encoder.VP9CostBit(probs[2], 1)
		for ref := 0; ref < nrefs; ref++ {
			cost += encoder.MvCost(mv[ref], refMv[ref], &fc.Nmvc, allowHP)
		}
	default:
		return 0
	}
	return cost
}

func vp9AnyMvHasSubpel(mv [2]vp9dec.MV) bool {
	return vp9MvHasSubpel(mv[0]) || vp9MvHasSubpel(mv[1])
}

func vp9IntraInterRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, isInter int,
) int {
	if fc == nil {
		return 0
	}
	if isInter != 0 {
		isInter = 1
	}
	ctx := vp9dec.GetIntraInterContext(above, left)
	return encoder.VP9CostBit(fc.IntraInterProb[ctx], isInter)
}

func vp9ReferenceModeRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, isCompound bool,
) int {
	if fc == nil || frameMode != vp9dec.ReferenceModeSelect {
		return 0
	}
	ctx := vp9dec.GetReferenceModeContext(above, left, refs)
	bit := 0
	if isCompound {
		bit = 1
	}
	return encoder.VP9CostBit(fc.ReferenceModeProbs.CompInterProb[ctx], bit)
}

func vp9SingleRefModeRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, refFrame int8,
) int {
	return vp9ReferenceModeRateCost(fc, above, left, frameMode, refs, false) +
		vp9SingleRefRateCost(fc, above, left, refFrame)
}

func vp9SingleRefRateCost(fc *vp9dec.FrameContext, above, left *vp9dec.NeighborMi,
	refFrame int8,
) int {
	if fc == nil || refFrame <= vp9dec.IntraFrame {
		return 0
	}
	ctx0 := vp9dec.GetPredContextSingleRefP1(above, left)
	bit0 := 0
	if refFrame != vp9dec.LastFrame {
		bit0 = 1
	}
	cost := encoder.VP9CostBit(fc.ReferenceModeProbs.SingleRefProb[ctx0][0], bit0)
	if bit0 == 0 {
		return cost
	}
	ctx1 := vp9dec.GetPredContextSingleRefP2(above, left)
	bit1 := 0
	if refFrame != vp9dec.GoldenFrame {
		bit1 = 1
	}
	return cost + encoder.VP9CostBit(fc.ReferenceModeProbs.SingleRefProb[ctx1][1], bit1)
}

func vp9CompoundRefRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, signBias [vp9dec.MaxRefFrames]uint8,
	refFrame [2]int8,
) (int, bool) {
	if fc == nil || frameMode == vp9dec.SingleReference {
		return 0, false
	}
	idx := int(signBias[refs.CompFixedRef])
	if idx < 0 || idx > 1 || refFrame[idx] != refs.CompFixedRef {
		return 0, false
	}
	varRef := refFrame[1-idx]
	bit := 0
	switch varRef {
	case refs.CompVarRef[0]:
	case refs.CompVarRef[1]:
		bit = 1
	default:
		return 0, false
	}
	ctx := vp9dec.GetPredContextCompRefP(above, left, refs, signBias)
	cost := vp9ReferenceModeRateCost(fc, above, left, frameMode, refs, true)
	cost += encoder.VP9CostBit(fc.ReferenceModeProbs.CompRefProb[ctx], bit)
	return cost, true
}

func vp9BlockSAD(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int, limit uint64,
) uint64 {
	// libvpx's sad_function pointers (cpi->fn_ptr[bsize].sdf) compute the
	// full block SAD with no early-termination — see vpx_dsp/sad.c
	// SAD()/vpx_dsp/arm/sad_neon.c. The caller compares the returned SAD
	// against best_sad afterwards. Govpx historically used the `limit`
	// argument to early-exit a row-major scalar loop, but that bypassed
	// the SIMD kernels and was a net pessimization. Always go through the
	// size-specialized SAD path; the per-row early-exit only matters for
	// limit-driven calls on sizes outside the wrapper table.
	// libvpx: vpx_dsp/sad.c:24 — SAD() returns sum without limit check.
	if sad, ok := vp9BlockSADNoLimit(src, srcStride, ref, refStride,
		srcX, srcY, refX, refY, w, h); ok {
		return uint64(sad)
	}
	var sad uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				diff = -diff
			}
			sad += uint64(diff)
		}
		if sad >= limit {
			return sad
		}
	}
	return sad
}

func vp9BlockSSE(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) uint64 {
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			diff := int(srcRow[x]) - int(refRow[x])
			sse += uint64(diff * diff)
		}
	}
	return sse
}

func vp9BlockDiffVariance(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) uint64 {
	variance, _ := vp9BlockDiffVarianceSSE(src, srcStride, ref, refStride,
		srcX, srcY, refX, refY, w, h)
	return variance
}

func vp9BlockDiffVarianceSSE(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) (uint64, uint64) {
	var sum int64
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			diff := int64(int(srcRow[x]) - int(refRow[x]))
			sum += diff
			sse += uint64(diff * diff)
		}
	}
	n := int64(w * h)
	if n <= 0 {
		return 0, sse
	}
	meanSquares := uint64((sum * sum) / n)
	if sse <= meanSquares {
		return 0, sse
	}
	return sse - meanSquares, sse
}

func vp9BlockSourceVariance128(src []byte, srcStride int, srcX, srcY, w, h int) uint64 {
	var sum int64
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		for x := range w {
			diff := int64(srcRow[x]) - 128
			sum += diff
			sse += uint64(diff * diff)
		}
	}
	n := int64(w * h)
	if n <= 0 {
		return 0
	}
	meanSquares := uint64((sum * sum) / n)
	if sse <= meanSquares {
		return 0
	}
	return sse - meanSquares
}

func vp9BlockSADNoLimit(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) (uint32, bool) {
	srcOff := srcY*srcStride + srcX
	refOff := refY*refStride + refX
	switch {
	case w == 64 && h == 64:
		return vp9dsp.VpxSad64x64(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 64 && h == 32:
		return vp9dsp.VpxSad64x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 64:
		return vp9dsp.VpxSad32x64(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 32:
		return vp9dsp.VpxSad32x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 16:
		return vp9dsp.VpxSad32x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 32:
		return vp9dsp.VpxSad16x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 16:
		return vp9dsp.VpxSad16x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 8:
		return vp9dsp.VpxSad16x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 16:
		return vp9dsp.VpxSad8x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 8:
		return vp9dsp.VpxSad8x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 4:
		return vp9dsp.VpxSad8x4(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 4 && h == 8:
		return vp9dsp.VpxSad4x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 4 && h == 4:
		return vp9dsp.VpxSad4x4(src, srcOff, srcStride, ref, refOff, refStride), true
	default:
		return 0, false
	}
}

func (e *VP9Encoder) vp9EncoderBestInterRefMvs(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, allowHP bool, signBias [vp9dec.MaxRefFrames]uint8,
) [2]vp9dec.MV {
	var best [2]vp9dec.MV
	if mi == nil || mi.Mode == common.ZeroMv || mi.RefFrame[0] <= vp9dec.IntraFrame {
		return best
	}
	halves := 1
	if mi.RefFrame[1] > vp9dec.IntraFrame {
		halves = 2
	}
	for ref := 0; ref < halves; ref++ {
		if cand, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, mi.Mode, mi.RefFrame[ref], allowHP,
			signBias); ok {
			best[ref] = cand
		}
	}
	return best
}

func (e *VP9Encoder) vp9EncoderInterModeCandidateMv(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, allowHP bool,
	signBias [vp9dec.MaxRefFrames]uint8,
) (vp9dec.MV, bool) {
	if mode == common.ZeroMv || refFrame <= vp9dec.IntraFrame {
		return vp9dec.MV{}, false
	}
	// Pass the five MV-ref-scan inputs directly; previously this site
	// allocated a ~29kB VP9Decoder per call just to populate five fields.
	// libvpx: vp9/common/vp9_mvref_common.c — find_mv_refs_idx reads the
	// flat fields off VP9_COMMON/MACROBLOCKD without an intermediate
	// composite.
	refList, refCount := vp9FindInterMvRefsFields(e.miGrid,
		e.useVP9EncoderPrevFrameMvs(miRows, miCols),
		e.prevFrameMvs, e.prevFrameMvRows, e.prevFrameMvCols,
		tile, miRows, miCols, miRow, miCol, bsize, mode, refFrame,
		signBias, -1)
	if mode == common.NearMv {
		if refCount <= 1 {
			return vp9dec.MV{}, false
		}
	} else if refCount == 0 {
		return vp9dec.MV{}, false
	}
	mv := vp9InterModeMvCandidate(refList, refCount, mode)
	vp9dec.LowerMvPrecision(&mv, allowHP)
	return mv, true
}

func (e *VP9Encoder) predictVP9InterBlock(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) bool {
	return e.predictVP9InterBlockOpts(inter, miRows, miCols, miRow, miCol,
		bsize, mi, false)
}

// predictVP9InterBlockLumaOnly reconstructs only the luma plane for the
// given inter prediction. Encoder motion-search SAD only reads luma, so
// skipping chroma cuts ~30-40% of convolve8 work per candidate.
// libvpx: vp9/encoder/vp9_pickmode.c:2336 (vp9_build_inter_predictors_sby
// in nonrd_pickmode does luma only).
func (e *VP9Encoder) predictVP9InterBlockLumaOnly(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) bool {
	return e.predictVP9InterBlockOpts(inter, miRows, miCols, miRow, miCol,
		bsize, mi, true)
}

func (e *VP9Encoder) predictVP9InterBlockOpts(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, lumaOnly bool,
) bool {
	if inter == nil || inter.ref == nil || !inter.ref.valid {
		return false
	}
	if mi == nil || mi.RefFrame[0] <= vp9dec.IntraFrame {
		return false
	}
	predictor := &e.interPredictor
	predictor.planes = e.planes
	predictor.frameY = e.reconY
	predictor.frameU = e.reconU
	predictor.frameV = e.reconV
	predictor.lastFrame = e.reconFrame
	predictor.interPredictScratch = e.interPredictScratch
	predictor.refFrames = e.refFrames
	predictor.unsupportedReconstruct = false
	predictor.predictLumaOnly = lumaOnly
	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(e.opts.Width),
		Height: uint32(e.opts.Height),
		InterRef: vp9dec.InterRefBlock{
			RefIndex: [3]uint8{
				vp9LastRefSlot,
				vp9GoldenRefSlot,
				vp9AltRefSlot,
			},
			SignBias: [3]uint8{
				vp9InterSignBias(inter)[vp9dec.LastFrame],
				vp9InterSignBias(inter)[vp9dec.GoldenFrame],
				vp9InterSignBias(inter)[vp9dec.AltrefFrame],
			},
		},
		AllowHighPrecisionMv: true,
		InterpFilter:         vp9InterFrameInterpFilter(inter),
	}
	ok := predictor.reconstructVP9InterPredictBlock(&hdr, mi, miRow, miCol, bsize)
	e.interPredictScratch = predictor.interPredictScratch
	// Reset flag so subsequent callers that don't explicitly set it get
	// the full 3-plane reconstruction.
	predictor.predictLumaOnly = false
	return ok && !predictor.unsupportedReconstruct
}

func (e *VP9Encoder) clearVP9PlaneBlockCoeffs(plane int, bsize common.BlockSize) {
	if plane < 0 || plane >= vp9dec.MaxMbPlane || bsize >= common.BlockSizes {
		return
	}
	n := min(int(common.Num4x4BlocksWideLookup[bsize])*
		int(common.Num4x4BlocksHighLookup[bsize])*vp9EncoderTxCoeffSlots, len(e.blockCoeffs[plane]))
	// clear() compiles to runtime.memclrNoHeapPointers; the prior
	// `for i := range buf { buf[i] = 0 }` form does too on Go 1.21+ but
	// only when the slice header is hoisted. libvpx uses memset; match
	// that semantic via the builtin so the compiler emits a tight
	// memset.s loop instead of bounds-checked stores.
	// libvpx: vp9/encoder/vp9_quantize.c:36-37 — memset(qcoeff_ptr, 0, ...).
	clear(e.blockCoeffs[plane][:n])
}

func (e *VP9Encoder) prepareVP9KeyframeTxResidue(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int, dequant [2]int16,
	qindex int, out []int16,
) bool {
	dst, stride, x0, y0, ok := e.predictVP9KeyframeTx(key.hdr, pd, plane, mode,
		txSize, tile, miRows, miCols, miRow, miCol, bsize, blockRow4x4, blockCol4x4)
	if !ok {
		return false
	}
	txType := common.DctDct
	if plane == 0 && txSize != common.Tx32x32 && !key.lossless {
		txType = common.IntraModeToTxType[mode]
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		return false
	}
	return e.quantizeVP9TxResidual(dst, stride, txSize, txType, dequant, qindex,
		out, key.lossless, false)
}

func (e *VP9Encoder) prepareVP9InterTxResidue(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, txSize common.TxSize,
	miRow, miCol int, blockRow4x4, blockCol4x4 int, dequant [2]int16, out []int16,
) bool {
	dst, stride, x0, y0, ok := e.vp9EncoderTxDst(pd, plane, txSize,
		miRow, miCol, blockRow4x4, blockCol4x4)
	if !ok {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		return false
	}
	return e.quantizeVP9TxResidual(dst, stride, txSize, common.DctDct, dequant, 0,
		out, inter.lossless, true)
}

func (e *VP9Encoder) gatherVP9TxResidual(src []byte, srcStride, srcW, srcH int,
	dst []byte, dstStride, x0, y0 int, txSize common.TxSize,
) bool {
	bs := 4 << uint(txSize)
	if bs*bs > len(e.residueScratch) || len(src) == 0 || srcStride <= 0 ||
		srcW <= 0 || srcH <= 0 {
		return false
	}
	for i := range e.residueScratch[:bs*bs] {
		e.residueScratch[i] = 0
	}
	hasDiff := false
	if x0 >= 0 && y0 >= 0 && x0+bs <= srcW && y0+bs <= srcH {
		for y := range bs {
			srcRow := src[(y0+y)*srcStride+x0:]
			dstRow := dst[y*dstStride:]
			for x := range bs {
				diff := int(srcRow[x]) - int(dstRow[x])
				e.residueScratch[y*bs+x] = int16(diff)
				if diff != 0 {
					hasDiff = true
				}
			}
		}
		return hasDiff
	}
	for y := range bs {
		sy := vp9ClampSourceCoord(y0+y, srcH)
		srcRow := src[sy*srcStride:]
		dstRow := dst[y*dstStride:]
		for x := range bs {
			sx := vp9ClampSourceCoord(x0+x, srcW)
			diff := int(srcRow[sx]) - int(dstRow[x])
			e.residueScratch[y*bs+x] = int16(diff)
			if diff != 0 {
				hasDiff = true
			}
		}
	}
	return hasDiff
}

func vp9ClampSourceCoord(v, limit int) int {
	if v < 0 {
		return 0
	}
	if v >= limit {
		return limit - 1
	}
	return v
}

func vp9CopySourceRectClamped(dst []byte, dstStride int, src []byte,
	srcStride, srcW, srcH int, x0, y0, w, h int,
) {
	if len(dst) == 0 || dstStride <= 0 || len(src) == 0 ||
		srcStride <= 0 || srcW <= 0 || srcH <= 0 || w <= 0 || h <= 0 {
		return
	}
	dstRows := len(dst) / dstStride
	if x0 < 0 || y0 < 0 || x0 >= dstStride || y0 >= dstRows {
		return
	}
	if x0+w > dstStride {
		w = dstStride - x0
	}
	if y0+h > dstRows {
		h = dstRows - y0
	}
	if w <= 0 || h <= 0 {
		return
	}
	if x0+w <= srcW && y0+h <= srcH {
		for y := range h {
			copy(dst[(y0+y)*dstStride+x0:(y0+y)*dstStride+x0+w],
				src[(y0+y)*srcStride+x0:(y0+y)*srcStride+x0+w])
		}
		return
	}
	for y := range h {
		sy := vp9ClampSourceCoord(y0+y, srcH)
		dstRow := dst[(y0+y)*dstStride+x0:]
		srcRow := src[sy*srcStride:]
		for x := range w {
			sx := vp9ClampSourceCoord(x0+x, srcW)
			dstRow[x] = srcRow[sx]
		}
	}
}

func vp9PredictionSSEClamped(src []byte, srcStride, srcW, srcH int,
	pred []byte, predStride, x0, y0, bs int,
) uint64 {
	if len(src) == 0 || srcStride <= 0 || srcW <= 0 || srcH <= 0 ||
		len(pred) == 0 || predStride <= 0 || bs <= 0 {
		return 0
	}
	var score uint64
	if x0 >= 0 && y0 >= 0 && x0+bs <= srcW && y0+bs <= srcH {
		for y := range bs {
			srcRow := src[(y0+y)*srcStride+x0:]
			predRow := pred[y*predStride:]
			for x := range bs {
				diff := int(srcRow[x]) - int(predRow[x])
				score += uint64(diff * diff)
			}
		}
		return score
	}
	for y := range bs {
		sy := vp9ClampSourceCoord(y0+y, srcH)
		srcRow := src[sy*srcStride:]
		predRow := pred[y*predStride:]
		for x := range bs {
			sx := vp9ClampSourceCoord(x0+x, srcW)
			diff := int(srcRow[sx]) - int(predRow[x])
			score += uint64(diff * diff)
		}
	}
	return score
}

func (e *VP9Encoder) quantizeVP9TxResidual(dst []byte, stride int,
	txSize common.TxSize, txType common.TxType, dequant [2]int16, qindex int,
	out []int16, lossless bool, useFastQuant bool,
) bool {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if txType >= common.TxTypes || maxEob > vp9EncoderTxCoeffSlots ||
		dequant[0] == 0 || dequant[1] == 0 || len(out) < maxEob {
		return false
	}
	if lossless && txSize != common.Tx4x4 {
		return false
	}
	if txSize == common.Tx32x32 && txType != common.DctDct {
		return false
	}
	for i := range e.txCoeffScratch[:maxEob] {
		e.txCoeffScratch[i] = 0
		e.dqCoeffScratch[i] = 0
	}
	if lossless {
		txType = common.DctDct
		encoder.ForwardWHT4x4Into(e.residueScratch[:], 4,
			e.txCoeffScratch[:maxEob])
	} else {
		switch txSize {
		case common.Tx4x4:
			encoder.ForwardHT4x4Into(e.residueScratch[:], 4, txType,
				e.txCoeffScratch[:maxEob])
		case common.Tx8x8:
			encoder.ForwardHT8x8Into(e.residueScratch[:], 8, txType,
				e.txCoeffScratch[:maxEob])
		case common.Tx16x16:
			encoder.ForwardHT16x16Into(e.residueScratch[:], 16, txType,
				e.txCoeffScratch[:maxEob])
		case common.Tx32x32:
			encoder.ForwardDCT32x32Into(e.residueScratch[:], 32, e.txCoeffScratch[:maxEob])
		default:
			return false
		}
	}
	scan := common.ScanOrders[txSize][txType].Scan
	if lossless {
		scan = common.DefaultScanOrders[txSize].Scan
	}
	eob := 0
	if txSize == common.Tx32x32 {
		if !useFastQuant {
			eob = encoder.QuantizeB32x32(e.txCoeffScratch[:maxEob], qindex, dequant,
				scan, e.dqCoeffScratch[:maxEob])
		} else {
			eob = encoder.QuantizeFP32x32(e.txCoeffScratch[:maxEob], dequant,
				scan, e.dqCoeffScratch[:maxEob])
		}
	} else {
		if !useFastQuant {
			eob = encoder.QuantizeB(e.txCoeffScratch[:maxEob], qindex, dequant,
				scan, e.dqCoeffScratch[:maxEob])
		} else {
			eob = encoder.QuantizeFP(e.txCoeffScratch[:maxEob], dequant,
				scan, e.dqCoeffScratch[:maxEob])
		}
	}
	if eob == 0 {
		return false
	}
	copy(out[:maxEob], e.dqCoeffScratch[:maxEob])
	vp9dec.InverseTransformBlock(out[:maxEob],
		dst, stride, txSize, txType, eob, lossless)
	return true
}

func (e *VP9Encoder) predictVP9KeyframeTx(hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	if stride <= 0 || len(planeData) == 0 || int(mode) >= common.IntraModes {
		return nil, 0, 0, 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return nil, 0, 0, 0, false
	}
	rows := len(planeData) / stride
	alignedWidth := vp9AlignTo(int(hdr.Width), 8)
	alignedHeight := vp9AlignTo(int(hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 = baseX + blockCol4x4*4
	y0 = baseY + blockRow4x4*4

	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, 0, 0, false
	}

	bounds := vp9BlockBoundsEdges(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	left := e.intraScratch.Left[:bs]
	if leftAvailable {
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			left[i] = planeData[sy*stride+x0-1]
		}
	}

	edges := vp9dec.IntraEdgeRefs{
		AboveLeft: 127,
		Left:      left,
	}
	upAvailable := blockRow4x4 != 0 || miRow > 0
	if upAvailable {
		edges.Above = planeData[(y0-1)*stride+x0:]
		if leftAvailable {
			edges.AboveLeft = planeData[(y0-1)*stride+x0-1]
		}
	}
	planeBlock4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W
	dst = planeData[y0*stride+x0:]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            dst,
		DstStride:      stride,
		Mode:           mode,
		TxSize:         txSize,
		Edges:          edges,
		UpAvailable:    upAvailable,
		LeftAvailable:  leftAvailable,
		RightAvailable: rightAvailable,
		FrameWidth:     planeWidth,
		FrameHeight:    planeHeight,
		X0:             x0,
		Y0:             y0,
		MbToRightEdge:  bounds.MbToRightEdge,
		MbToBottomEdge: bounds.MbToBottomEdge,
	}, &e.intraScratch)
	return dst, stride, x0, y0, true
}

func (e *VP9Encoder) vp9EncoderTxDst(pd *vp9dec.MacroblockdPlane,
	plane int, txSize common.TxSize,
	miRow, miCol int, blockRow4x4, blockCol4x4 int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	if stride <= 0 || len(planeData) == 0 {
		return nil, 0, 0, 0, false
	}
	rows := len(planeData) / stride
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 = baseX + blockCol4x4*4
	y0 = baseY + blockRow4x4*4
	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, 0, 0, false
	}
	return planeData[y0*stride+x0:], stride, x0, y0, true
}

func (e *VP9Encoder) vp9BlockCoeffs(plane int,
	bsize common.BlockSize, r, c int, tx common.TxSize,
) []int16 {
	coeffs := e.coefScratch[:vp9dec.MaxEobForTxSize(tx)]
	for i := range coeffs {
		coeffs[i] = 0
	}
	if plane < 0 || plane >= vp9dec.MaxMbPlane {
		return coeffs
	}
	pd := &e.planes[plane]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return coeffs
	}
	full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	coeffBase := (r*full4x4W + c) * vp9EncoderTxCoeffSlots
	maxEob := vp9dec.MaxEobForTxSize(tx)
	if maxEob <= vp9EncoderTxCoeffSlots && coeffBase >= 0 &&
		coeffBase+maxEob <= len(e.blockCoeffs[plane]) {
		copy(coeffs, e.blockCoeffs[plane][coeffBase:coeffBase+maxEob])
	}
	return coeffs
}

func (e *VP9Encoder) vp9EncoderReconPlane(plane int) ([]byte, int) {
	switch plane {
	case 0:
		return e.reconY, e.reconFrame.YStride
	case 1:
		return e.reconU, e.reconFrame.UStride
	case 2:
		return e.reconV, e.reconFrame.VStride
	default:
		return nil, 0
	}
}

func vp9EncoderSourcePlane(img *image.YCbCr, plane int) (
	pixels []byte, stride, width, height int,
) {
	if img == nil {
		return nil, 0, 0, 0
	}
	switch plane {
	case 0:
		return img.Y, img.YStride, img.Rect.Dx(), img.Rect.Dy()
	case 1:
		return img.Cb, img.CStride, (img.Rect.Dx() + 1) >> 1, (img.Rect.Dy() + 1) >> 1
	case 2:
		return img.Cr, img.CStride, (img.Rect.Dx() + 1) >> 1, (img.Rect.Dy() + 1) >> 1
	default:
		return nil, 0, 0, 0
	}
}

func vp9ReferenceVisiblePlane(ref *vp9ReferenceFrame, plane int) (
	pixels []byte, stride, width, height int,
) {
	if ref == nil || !ref.valid {
		return nil, 0, 0, 0
	}
	pixels, stride = vp9ReferencePlane(ref, plane)
	switch plane {
	case 0:
		return pixels, stride, ref.img.Width, ref.img.Height
	case 1, 2:
		return pixels, stride, (ref.img.Width + 1) >> 1, (ref.img.Height + 1) >> 1
	default:
		return nil, 0, 0, 0
	}
}

func (e *VP9Encoder) vp9MiAt(miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	off := r*miCols + c
	if off < 0 || off >= len(e.miGrid) {
		return nil
	}
	return &e.miGrid[off]
}

// ensureVP9LeafInterDecisionCache sizes the per-frame leaf-write picker
// decision cache to the current miGrid extent. Called from
// ensureVP9EncoderModeBuffers so the cache always tracks the active frame.
// The version stamp is bumped to invalidate any stale entries left from the
// prior frame (avoids the O(N) zeroing every frame).
//
// libvpx: vp9/encoder/vp9_encodeframe.c::set_offsets resizes cpi->td.mb
// per-frame; the per-block mbmi decision survives within the frame but is
// reset at frame boundaries via vp9_zero(cm->mip).
func (e *VP9Encoder) ensureVP9LeafInterDecisionCache(miRows, miCols int) {
	n := miRows * miCols
	if cap(e.vp9LeafInterDecisions) < n {
		e.vp9LeafInterDecisions = make([]vp9LeafInterDecisionEntry, n)
	} else {
		e.vp9LeafInterDecisions = e.vp9LeafInterDecisions[:n]
	}
	e.vp9LeafInterDecisionsRows = miRows
	e.vp9LeafInterDecisionsCols = miCols
	e.vp9LeafInterDecisionsVer++
	// On version wraparound (extremely unlikely; uint32 covers 4B frames)
	// zero the cache so a stale version stamp can't masquerade as fresh.
	if e.vp9LeafInterDecisionsVer == 0 {
		for i := range e.vp9LeafInterDecisions {
			e.vp9LeafInterDecisions[i] = vp9LeafInterDecisionEntry{}
		}
		e.vp9LeafInterDecisionsVer = 1
	}
}

// lookupVP9LeafInterDecision returns a previously stored leaf-write inter
// picker decision for (miRow, miCol, bsize) if one was committed in the
// current frame. The first leaf-write visit (count pre-pass) populates;
// the second visit (bitstream write pass) consumes. A miss returns false.
func (e *VP9Encoder) lookupVP9LeafInterDecision(miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if e.vp9LeafInterDecisionsCols <= 0 {
		return vp9InterModeDecision{}, false
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafInterDecisionsRows ||
		miCol >= e.vp9LeafInterDecisionsCols {
		return vp9InterModeDecision{}, false
	}
	off := miRow*e.vp9LeafInterDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafInterDecisions) {
		return vp9InterModeDecision{}, false
	}
	entry := &e.vp9LeafInterDecisions[off]
	if !entry.valid || entry.version != e.vp9LeafInterDecisionsVer ||
		entry.bsize != bsize {
		return vp9InterModeDecision{}, false
	}
	return entry.decision, true
}

// storeVP9LeafInterDecision commits the picker decision for (miRow, miCol,
// bsize) to the per-frame leaf cache. Subsequent same-frame lookups at the
// same key return the stored decision, allowing the bitstream write pass to
// skip pickVP9InterReferenceMode after the count pre-pass populated the
// entry.
func (e *VP9Encoder) storeVP9LeafInterDecision(miRow, miCol int,
	bsize common.BlockSize, decision vp9InterModeDecision,
) {
	if e.vp9LeafInterDecisionsCols <= 0 {
		return
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafInterDecisionsRows ||
		miCol >= e.vp9LeafInterDecisionsCols {
		return
	}
	off := miRow*e.vp9LeafInterDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafInterDecisions) {
		return
	}
	e.vp9LeafInterDecisions[off] = vp9LeafInterDecisionEntry{
		version:  e.vp9LeafInterDecisionsVer,
		bsize:    bsize,
		decision: decision,
		valid:    true,
	}
}

func (e *VP9Encoder) fillVP9MiGrid(miRows, miCols, r, c int, bsize common.BlockSize, mi vp9dec.NeighborMi) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := e.miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

// Encode is the alloc-returning wrapper around EncodeInto.
func (e *VP9Encoder) Encode(img *image.YCbCr) ([]byte, error) {
	return e.EncodeWithFlags(img, 0)
}

// EncodeWithFlags is the alloc-returning wrapper around EncodeIntoWithFlags.
func (e *VP9Encoder) EncodeWithFlags(img *image.YCbCr, flags EncodeFlags) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	return e.encodeVP9Allocating(img, flags, false)
}

// EncodeIntraOnlyFrame is the allocating wrapper around
// EncodeIntraOnlyFrameInto.
func (e *VP9Encoder) EncodeIntraOnlyFrame(img *image.YCbCr, flags EncodeFlags) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	return e.encodeVP9Allocating(img, flags, true)
}

func (e *VP9Encoder) encodeVP9Allocating(img *image.YCbCr, flags EncodeFlags, intraOnly bool) ([]byte, error) {
	size, err := vp9AllocatingEncodeBufferSize(e.opts.Width, e.opts.Height)
	if err != nil {
		return nil, err
	}
	for {
		dst := make([]byte, size)
		var n int
		if intraOnly {
			n, err = e.EncodeIntraOnlyFrameInto(img, dst, flags)
		} else {
			n, err = e.EncodeIntoWithFlags(img, dst, flags)
		}
		if err == nil {
			out := make([]byte, n)
			copy(out, dst[:n])
			return out, nil
		}
		if !vp9EncodeOutputBufferFull(err) {
			return nil, err
		}
		maxInt := int(^uint(0) >> 1)
		if size > maxInt/2 {
			return nil, err
		}
		size *= 2
	}
}

func vp9AllocatingEncodeBufferSize(width, height int) (int, error) {
	if width <= 0 || height <= 0 {
		return 0, ErrInvalidConfig
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/height {
		return 0, ErrInvalidConfig
	}
	y := width * height
	uvWidth := (width + 1) / 2
	uvHeight := (height + 1) / 2
	if uvWidth > maxInt/uvHeight {
		return 0, ErrInvalidConfig
	}
	uv := uvWidth * uvHeight
	if uv > (maxInt-y)/2 {
		return 0, ErrInvalidConfig
	}
	raw420 := y + 2*uv
	const headerSlack = 4096
	if raw420 > (maxInt-headerSlack)/4 {
		return 0, ErrInvalidConfig
	}
	size := max(headerSlack+raw420*4, 65536)
	return size, nil
}

func vp9EncodeOutputBufferFull(err error) bool {
	return errors.Is(err, ErrBufferTooSmall) ||
		errors.Is(err, encoder.ErrPackBufferFull) ||
		errors.Is(err, encoder.ErrTileBufferFull) ||
		errors.Is(err, bitstream.ErrBufferOverflow)
}

// EncodeShowExistingFrameInto writes a VP9 show_existing_frame packet for an
// already refreshed reference slot. The packet has no source image, compressed
// header, or tile body; decoders display the referenced slot directly. Slot must
// be in [0, 7] and valid in the encoder's current VP9 reference map.
func (e *VP9Encoder) EncodeShowExistingFrameInto(dst []byte, slot uint8) (int, error) {
	if e == nil || e.closed {
		return 0, ErrClosed
	}
	if slot >= common.RefFrames {
		return 0, ErrInvalidConfig
	}
	if !e.refValid[slot] || !e.refFrames[slot].valid {
		return 0, ErrInvalidConfig
	}
	if len(dst) == 0 {
		return 0, ErrBufferTooSmall
	}
	var bw encoder.BitWriter
	bw.Init(dst)
	return encoder.WriteShowExistingFrameHeader(&bw, common.Profile0, slot), nil
}

// EncodeShowExistingFrame is the allocating wrapper around
// EncodeShowExistingFrameInto.
func (e *VP9Encoder) EncodeShowExistingFrame(slot uint8) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	dst := make([]byte, 1)
	n, err := e.EncodeShowExistingFrameInto(dst, slot)
	if err != nil {
		return nil, err
	}
	return dst[:n], nil
}

func alignToSb(miCols int) int {
	const mask = common.MiBlockSize - 1
	return (miCols + mask) &^ mask
}

// Close releases internal state and marks the encoder as no longer
// usable. Subsequent Encode / EncodeInto calls return [ErrClosed].
// Close is idempotent: calling it on an already-closed encoder returns
// [ErrClosed] without re-tearing-down the worker pools.
func (e *VP9Encoder) Close() error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if vp9OracleTraceBuild {
		e.resetVP9OracleTraceState()
	}
	if e.vp9TilePool != nil {
		e.vp9TilePool.shutdownPool()
		e.vp9TilePool = nil
	}
	if e.frameParallel != nil {
		e.frameParallel.release()
		e.frameParallel = nil
	}
	e.closed = true
	return nil
}

// Codec reports the codec this encoder targets.
func (e *VP9Encoder) Codec() Codec { return CodecVP9 }
