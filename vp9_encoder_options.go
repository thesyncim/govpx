package govpx

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

const (
	vp9EncoderTxCoeffSlots           = 1024
	vp9EncoderBlockCoeffSlots        = 256 * vp9EncoderTxCoeffSlots
	vp9MinEncodeIntoBuffer           = 64
	vp9MaxPartitionReconScratch      = 64*64 + 2*32*32
	vp9MaxPartitionReconScratchStack = 2*vp9MaxPartitionReconScratch +
		32*32 + 2*16*16 +
		16*16 + 2*8*8 +
		8*8 + 2*4*4 +
		4*4
	vp9DefaultMinQuantizer = 4
	vp9DefaultMaxQuantizer = 56
	vp9DefaultCQLevel      = 32
	// vp9DefaultBaseQIndex pins the packet-path default to the first-frame
	// base_qindex emitted by pinned libvpx vpxenc-vp9 with the repo's realtime
	// CQ oracle knobs (--end-usage=q --cq-level=32 --min-q=4 --max-q=56).
	vp9DefaultBaseQIndex = 37
	// The same oracle emits the CQ-level qindex for the first visible inter
	// frame in the packet path after the keyframe.
	vp9DefaultInterBaseQIndex = 128
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

// vp9CoefUpdateModeForFrame selects libvpx's coefficient-probability update
// emitter (TWO_LOOP vs ONE_LOOP_REDUCED) from SPEED_FEATURES.
// use_fast_coef_updates. libvpx's update_coef_probs_common switches on this
// field at vp9_bitstream.c:556. The default is TWO_LOOP
// (vp9_speed_features.c:993); it is only flipped to ONE_LOOP_REDUCED at:
//
//   - GOOD speed >= 4 (vp9_speed_features.c:395) — any frame_type
//   - REALTIME speed >= 4 / >= 5 (vp9_speed_features.c:579 / :611) —
//     non-keyframes only (is_keyframe ? TWO_LOOP : ONE_LOOP_REDUCED)
//
// Previously this returned ONE_LOOP_REDUCED for ANY non-key frame regardless
// of speed, which over-fired the one-loop emitter at REALTIME speed=3 (cpu=-3)
// where libvpx still uses TWO_LOOP. The two emitters produce divergent wire
// bits whenever any update fires because ONE_LOOP_REDUCED elides the no-update
// run before the first slot. Consult e.sf directly so the per-frame
// vp9ApplySpeedFeatures dispatch (vp9_encoder.go:2593) drives the mode.
//
// libvpx: vp9_bitstream.c:556 (switch (cpi->sf.use_fast_coef_updates)).
func (e *VP9Encoder) vp9CoefUpdateModeForFrame() encoder.CoefUpdateMode {
	if e != nil && e.sf.UseFastCoefUpdates == OneLoopReduced {
		return encoder.CoefUpdateOneLoopReduced
	}
	return encoder.CoefUpdateTwoLoop
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
	// options select realtime cpu-used 8, matching the default path most
	// oracle parity tests exercise; call SetDeadline after construction to
	// force explicit best-quality cpu 0.
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
	// configuration. VP9 reports the requested value, while its internal
	// frame-budget and buffer math follow libvpx's raw-target-rate cap:
	// width * height * 8 * 3 * frame_rate / 1000 kbps, with frame rates
	// above 180 Hz treated as 30 Hz. When RateControlModeSet is false, the
	// packet path keeps the existing public-Q mode and only stores this
	// value as metadata.
	TargetBitrateKbps int

	// RateControlModeSet enables VP9 rate-control bookkeeping. It is explicit
	// because RateControlVBR is the zero value while the default VP9 option
	// set uses libvpx VPX_Q-style public-Q mode. RateControlCBR drives
	// one-pass CBR qindex selection, tracks a buffer, and can drop visible
	// inter frames when DropFrameAllowed is enabled. RateControlVBR,
	// RateControlCQ, and RateControlQ drive one-pass VP9 rate/quality qindex
	// selection without frame dropping.
	RateControlModeSet bool
	// RateControlMode selects the VP9 rate-control mode when
	// RateControlModeSet is true.
	RateControlMode RateControlMode
	// BufferSizeMs, BufferInitialSizeMs, and BufferOptimalSizeMs configure the
	// VP9 CBR virtual buffer. Zero option values use libvpx's default config
	// values; SetRateControlBuffer can still pass literal zero values at
	// runtime to select libvpx's target_bandwidth/8 maximum and optimal
	// buffers.
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

	// TargetLevel constrains encode decisions to respect a fixed VP9 level.
	// Mirrors libvpx's VP9E_SET_TARGET_LEVEL control. Valid values are 255
	// (unconstrained), 1 (auto), 0 (unspecified), and the canonical level
	// codes 10, 11, 20, 21, 30, 31, 40, 41, 50, 51, 52, 60, 61, 62 (Level
	// N.M encoded as 10*N + M). Fixed levels clamp the effective
	// rate-control target to 80% of the level average bitrate, clamp
	// overshoot, force worst quantizer to public q63, raise the golden-frame
	// interval floor, and limit tile columns. They do not reject otherwise
	// valid dimensions or bitrates.
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
	// values are in [0, 24]; zero leaves libvpx's framerate-
	// derived default in place.
	MinGFInterval int
	// MaxGFInterval mirrors libvpx's VP9E_SET_MAX_GF_INTERVAL control. It
	// bounds the encoder-selected golden-frame interval from above. Valid
	// values are zero or in [2, 24]; zero leaves libvpx's framerate-
	// derived default in place. When both bounds are non-zero, MinGFInterval
	// must not exceed MaxGFInterval.
	MaxGFInterval int

	// FramePeriodicBoost mirrors libvpx's VP9E_SET_FRAME_PERIODIC_BOOST
	// control. When true, periodic golden-frame refreshes receive a
	// stronger active-best-Q reduction so the boosted GF/ALTREF achieves
	// a tighter target qindex.
	FramePeriodicBoost bool

	// AltRefAQ mirrors libvpx's VP9E_SET_ALT_REF_AQ control. In libvpx
	// v1.16.0 the VP9 alt-ref AQ implementation is a stub, so govpx records
	// the control but leaves coding decisions unchanged.
	AltRefAQ bool

	// PostEncodeDrop mirrors libvpx's VP9E_SET_POSTENCODE_DROP_CBR control.
	// When true (and CBR rate control is enabled), visible inter frames
	// whose packed size would underflow the CBR buffer are dropped from the
	// output after encoding. This is separate from DropFrameAllowed, which
	// controls the pre-encode watermark dropper.
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
	// Zero or one keeps one source frame in flight.
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
	// InterPicturePredicted reports whether the coded frame uses prediction
	// from an earlier picture. This maps to the VP9 RTP descriptor P bit.
	InterPicturePredicted bool

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

	interPicturePredictedKnown bool
}

// RTPPayloadDescriptor returns a non-flexible VP9 RTP descriptor populated
// from temporal and spatial layer metadata. Picture ID is intentionally left
// unset so callers can choose their own RTP picture-id policy.
func (r VP9EncodeResult) RTPPayloadDescriptor() VP9RTPPayloadDescriptor {
	desc := VP9RTPPayloadDescriptor{
		InterPicturePredicted: r.vp9RTPInterPicturePredicted(),
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

func (r VP9EncodeResult) vp9RTPInterPicturePredicted() bool {
	if r.interPicturePredictedKnown {
		return r.InterPicturePredicted
	}
	return !r.KeyFrame && !r.IntraOnly
}

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
	VP9MaxSpatialLayers = encoder.MaxSpatialLayers
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
