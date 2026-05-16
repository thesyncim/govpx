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
	// ScreenContentMode selects VP9 content tuning: 0 is default video, 1 is
	// screen content, and 2 is film/grain content. Screen content enables the
	// broader no-reference intra mode search used by realtime VP9.
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

	// Segmentation enables static VP9 profile 0 segmentation metadata.
	// When UpdateMap is set, every encoded block is assigned SegmentID.
	// This supports AltQ, AltLF, forced inter-reference, and forced-skip
	// segment features.
	Segmentation VP9SegmentationOptions
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
	opts     VP9EncoderOptions
	closed   bool
	temporal temporalState
	rc       vp9RateControlState
	twoPass  vp9TwoPassState
	cyclicAQ     vp9CyclicRefreshState
	perceptualAQ vp9PerceptualAQState
	// spatialScalabilityLocked is set for encoders owned by
	// VP9SpatialSVCEncoder; the parent owns spatial layer metadata.
	spatialScalabilityLocked bool
	// temporalScalabilityLocked is set for encoders owned by
	// VP9SpatialSVCEncoder; the parent owns access-unit temporal metadata.
	temporalScalabilityLocked bool

	activeMap        []uint8
	activeMapMiRows  int
	activeMapMiCols  int
	activeMapEnabled bool
	roi              vp9ROIMapState
	denoiser         vp9DenoiserState

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

	blockCoeffs      [vp9dec.MaxMbPlane][vp9EncoderBlockCoeffSlots]int16
	coefScratch      [1024]int16
	residueScratch   [1024]int16
	txCoeffScratch   [1024]int16
	dqCoeffScratch   [1024]int16
	dqScratch        vp9dec.DequantTables
	frameCounts      encoder.FrameCounts
	vp9HeaderScratch vp9dec.UncompressedHeader
	vp9CountWorkers  []VP9Encoder
	vp9CountCounts   []encoder.FrameCounts
	vp9CountJobs     []vp9CountTileJob
	vp9TilePool      *vp9TileWorkerPool
	// vp9RowMTSync is set when the worker is dispatched as a tile-column body
	// with RowMT enabled. The pointer aliases an entry inside
	// vp9TileWorkerPool.rowMTSyncs and lives for the duration of the per-frame
	// encode; writeVP9ModesTileBounds reads it to drive the wavefront primitive.
	vp9RowMTSync *vp9RowMTSync
	lfi              vp9dec.LoopFilterInfoN
	lfRefDeltas      [vp9dec.MaxRefLfDeltas]int8
	lfModeDeltas     [vp9dec.MaxModeLfDeltas]int8

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
	e := &VP9Encoder{opts: opts, temporal: temporal, rc: rc}
	e.twoPass.configure(opts.TwoPassStats, rc.bitsPerFrame,
		opts.TwoPassVBRBiasPct, opts.TwoPassMinPct, opts.TwoPassMaxPct,
		opts.Height)
	e.initVP9Lookahead(opts.Width, opts.Height, opts.LookaheadFrames)
	e.cyclicAQ.configure(opts.AQMode == VP9AQCyclicRefresh, opts.Width, opts.Height)
	e.perceptualAQ.configure(opts.AQMode == VP9AQPerceptual)
	e.lfi = vp9dec.NewLoopFilterInfoN()
	vp9dec.LoopFilterInit(&e.lfi, 0)
	e.initVP9TileWorkerPool()
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
	if opts.ScreenContentMode < 0 || opts.ScreenContentMode > 2 {
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
	if opts.DeltaQUV < -15 || opts.DeltaQUV > 15 {
		return ErrInvalidQuantizer
	}
	if opts.Lossless && opts.DeltaQUV != 0 {
		return ErrInvalidQuantizer
	}
	if _, err := normalizeVP9SpatialScalabilityConfig(opts.SpatialScalability,
		opts.Width, opts.Height); err != nil {
		return err
	}
	if opts.LookaheadFrames > 0 {
		if opts.RateControlModeSet || opts.TemporalScalability.Enabled {
			return ErrInvalidConfig
		}
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
		for i := 0; i < VP9RTPMaxSpatialLayers; i++ {
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
			qindex := int(seg.AltQ[i])
			if qindex < 0 {
				qindex = 0
			}
			if qindex != 0 {
				return ErrInvalidQuantizer
			}
		}
		if seg.AltLFEnabled[i] {
			filterLevel := int(seg.AltLF[i])
			if filterLevel < 0 {
				filterLevel = 0
			}
			if filterLevel != 0 {
				return ErrInvalidConfig
			}
		}
	}
	return nil
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
		seg := vp9VarianceAQSegmentationParams(baseQIndex)
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
	if e.opts.AQMode == VP9AQEquator360 {
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

func vp9VarianceAQSegmentationParams(baseQIndex int) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	initVP9SegmentationProbDefaults(&seg)
	for i, ratio := range vp9VarianceAQRateRatios {
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
	for miRow := 0; miRow < miRows; miRow++ {
		row := e.prevSegmentMap[miRow*miCols:]
		for miCol := 0; miCol < miCols; miCol++ {
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
	if isKey && flags&vp9NoUpdateRefFlags != 0 {
		return VP9EncodeResult{}, ErrInvalidConfig
	}
	if intraOnly && vp9InterRefreshFrameFlags(flags) == 0 {
		return VP9EncodeResult{}, ErrInvalidConfig
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
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Tile = vp9EncoderTileInfo(miCols, e.opts.Threads, e.opts.Log2TileRows)
	macroblocks := vp9MacroblockCount(miRows, miCols)
	qindex := e.vp9EncoderFrameQIndex(isKey, header.IntraOnly, flags, macroblocks)
	if e.rc.enabled {
		e.vp9ModeDecisionQIndex = uint8(qindex)
		e.vp9ModeDecisionQIndexSet = true
		defer func() {
			e.vp9ModeDecisionQIndexSet = false
		}()
	}
	header.Quant.BaseQindex = int16(qindex)
	header.Quant.UvDcDeltaQ = int8(e.opts.DeltaQUV)
	header.Quant.UvAcDeltaQ = int8(e.opts.DeltaQUV)
	header.Quant.Lossless = qindex == 0 &&
		header.Quant.YDcDeltaQ == 0 &&
		header.Quant.UvDcDeltaQ == 0 &&
		header.Quant.UvAcDeltaQ == 0
	resetLoopfilterDeltas := isKey || intraOnly || e.opts.ErrorResilient
	header.Loopfilter = vp9EncoderLoopFilterParams(qindex, isKey,
		resetLoopfilterDeltas, header.Quant.Lossless, e.opts.Sharpness)
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
	header.InterpFilter = vp9EncoderFrameInterpFilter(isKey, header.IntraOnly,
		header.Quant.Lossless)
	if !isKey && !header.IntraOnly && vp9InterReferenceMask(flags) == 0 {
		header.InterpFilter = vp9dec.InterpSwitchable
	}
	header.AllowHighPrecisionMv = vp9EncoderFrameAllowHighPrecisionMv(isKey, header.IntraOnly)

	txMode := vp9EncoderFrameTxMode(isKey, header.IntraOnly, header.Quant.Lossless)
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
	if (isKey || intraOnly) && baseMi.TxSize > common.Tx16x16 {
		baseMi.TxSize = common.Tx16x16
	}
	if !isKey && !intraOnly {
		baseMi.Mode = common.ZeroMv
		baseMi.InterpFilter = uint8(vp9dec.InterpEighttap)
		baseMi.RefFrame = [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}
	}
	e.cyclicAQ.prepareFrame(!isKey && !intraOnly && showFrame, miRows, miCols)
	if e.opts.AQMode == VP9AQPerceptual {
		e.perceptualAQ.prepareFrame(img, int(header.Quant.BaseQindex))
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
	if reducedTxMode := vp9EncoderFrameTxModeFromCounts(txMode,
		header.Quant.Lossless, counts); reducedTxMode != txMode {
		if (isKey || header.IntraOnly) && reducedTxMode < common.Allow16x16 {
			reducedTxMode = common.Allow16x16
		}
		txMode = reducedTxMode
		baseMi.TxSize = common.TxModeToBiggestTxSize[txMode]
		denoiserCountState = e.saveVP9DenoiserForCounts(interState)
		counts = e.collectVP9EncodeFrameCounts(int(width), int(height), miRows, miCols,
			header.Tile, &partitionProbs, &seg, baseMi, txMode, isKey,
			header.IntraOnly, keyState, interState)
		e.restoreVP9DenoiserAfterCounts(denoiserCountState)
	}
	header.Seg = seg

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
	e.rc.postEncodeFrame(n, header.ShowFrame, qindex, isKey || intraOnly,
		header.RefreshFrameFlags, macroblocks)
	if header.ShowFrame {
		e.twoPass.finishFrame()
	}
	e.temporal.finishFrame(temporalFrame, isKey, header.ShowFrame,
		vp9TemporalReferenceRefresh(header.RefreshFrameFlags),
		encodedSizeBits(n), e.vp9TemporalBufferConfig())
	e.vp9FinishKeyFrameDistance(isKey)
	e.frameIndex++
	if isKey {
		e.forceKeyFrame = false
	}
	spatialLayerID, spatialLayerCount, interLayerDependency,
		notRefForUpperSpatialLayer, scalabilityStructurePresent,
		spatialScalabilityStructure := e.vp9SpatialResultFields()
	result = VP9EncodeResult{
		Data:                        dst[:n],
		KeyFrame:                    isKey,
		IntraOnly:                   intraOnly,
		ShowFrame:                   header.ShowFrame,
		Droppable:                   !isKey && header.RefreshFrameFlags == 0 && !header.RefreshFrameContext,
		Quantizer:                   vp9QIndexToPublicQuantizer(qindex),
		InternalQuantizer:           qindex,
		SizeBytes:                   n,
		TargetBitrateKbps:           e.vp9ResultTargetBitrateKbps(),
		FrameTargetBits:             e.rc.frameTargetBits,
		BufferLevelBits:             e.rc.bufferLevelBits,
		RefreshFrameFlags:           header.RefreshFrameFlags,
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
	if err := validateEncodeFlags(flags); err != nil {
		return err
	}
	return nil
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
	for i := 0; i < need; i++ {
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
	for i := 0; i < need; i++ {
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

func vp9EncoderFrameTxMode(isKey, intraOnly, lossless bool) common.TxMode {
	if lossless {
		return common.Only4x4
	}
	if isKey || intraOnly {
		return common.Allow32x32
	}
	return common.TxModeSelect
}

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

func vp9EncoderFrameInterpFilter(isKey, intraOnly, lossless bool) vp9dec.InterpFilter {
	return vp9dec.InterpEighttap
}

func vp9EncoderFrameAllowHighPrecisionMv(isKey, intraOnly bool) bool {
	return !isKey && !intraOnly
}

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

func vp9EncoderLoopFilterParams(qindex int, isKey, resetDeltas, lossless bool,
	sharpness uint8,
) vp9dec.LoopfilterParams {
	level := vp9EncoderLoopFilterLevel(qindex, isKey)
	if lossless {
		level = 0
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

var (
	vp9EighttapInterpFilterOrder = [...]vp9dec.InterpFilter{vp9dec.InterpEighttap}
	vp9SmoothInterpFilterOrder   = [...]vp9dec.InterpFilter{vp9dec.InterpEighttapSmooth}
	vp9SharpInterpFilterOrder    = [...]vp9dec.InterpFilter{vp9dec.InterpEighttapSharp}
	vp9BilinearInterpFilterOrder = [...]vp9dec.InterpFilter{vp9dec.InterpBilinear}
)

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
	for i := 0; i < nBits; i++ {
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
			rowMT.write(sbRow, sbCol, tileSbCols)
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
		if e.opts.AQMode == VP9AQVariance && key != nil && key.img != nil &&
			e.vp9DynamicSegmentMapActive() {
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
	for y := 0; y < h; y++ {
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

func (e *VP9Encoder) vp9CBRKeyframeVariancePartitionEnabled(key *vp9KeyframeEncodeState) bool {
	return key != nil && key.dq != nil && key.hdr != nil &&
		key.hdr.FrameType == common.KeyFrame && !key.lossless &&
		e.rc.enabled && e.rc.mode == RateControlCBR && !e.vp9FixedPublicQuantizer()
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
	if varianceSize, ok := e.pickVP9CBRVariancePartitionBlockSize(inter,
		miRows, miCols, miRow, miCol, root); ok {
		return varianceSize
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

	bsl := int(common.BWidthLog2Lookup[root])
	bs := (1 << uint(bsl)) / 4
	hasRows := miRow+bs < miRows
	hasCols := miCol+bs < miCols
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)
	qindex := e.vp9EncoderModeDecisionQIndex()
	bestSize := root
	bestScore := vp9AddModeDecisionRate(full.score,
		vp9PartitionRateCost(partitionProbs, ctx, common.PartitionNone,
			hasRows, hasCols), qindex)

	if hasRows {
		if score, ok := e.scoreVP9InterPartitionPair(inter, tile, miRows, miCols,
			miRow, miCol, horzSize, bs, 0); ok {
			score = vp9AddModeDecisionRate(score,
				vp9PartitionRateCost(partitionProbs, ctx, common.PartitionHorz,
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
			score = vp9AddModeDecisionRate(score,
				vp9PartitionRateCost(partitionProbs, ctx, common.PartitionVert,
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
			score = vp9AddModeDecisionRate(score,
				vp9PartitionRateCost(partitionProbs, ctx, common.PartitionSplit,
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

func (e *VP9Encoder) pickVP9CBRVariancePartitionBlockSize(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (common.BlockSize, bool) {
	if !e.vp9CBRVariancePartitionEnabled(inter) {
		return common.BlockInvalid, false
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

func (e *VP9Encoder) vp9CBRVariancePartitionEnabled(inter *vp9InterEncodeState) bool {
	if inter == nil || inter.dq == nil || inter.lossless ||
		!e.rc.enabled || e.rc.mode != RateControlCBR || !e.rc.dropFrameAllowed {
		return false
	}
	return !e.vp9FixedPublicQuantizer()
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
	threshold := int(yAcDequant) << 1
	if threshold < 1000 {
		threshold = 1000
	}
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
		key != nil {
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
		cur.Mode = e.pickVP9KeyframeMode(key, tile, miRows, miCols,
			miRow, miCol, reconBsize, &cur)
		uvMode := e.pickVP9KeyframeUvMode(key, tile, miRows, miCols,
			miRow, miCol, reconBsize, &cur)
		segID := vp9EncoderMiSegmentID(&cur)
		segmentSkip := vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip)
		hasResidue := false
		if segmentSkip {
			cur.Skip = 1
		} else {
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
	encoder.WriteKeyframeBlock(bw, encoder.WriteKeyframeBlockArgs{
		Seg:       seg,
		Mi:        &cur,
		AboveMi:   above,
		LeftMi:    left,
		TxMode:    txMode,
		MaxTxSize: common.MaxTxsizeLookup[bsize],
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
	return e != nil && (e.roi.enabled ||
		e.opts.AQMode == VP9AQVariance ||
		(e.cyclicAQ.enabled && e.cyclicAQ.apply) ||
		e.activeMapEnabled)
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
	if e.opts.AQMode == VP9AQVariance {
		if segID, ok := e.vp9VarianceAQSegmentID(img, miRow, miCol); ok {
			if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
				return vp9ActiveMapSegmentInactive, true
			}
			return segID, true
		}
	}
	if e.opts.AQMode == VP9AQEquator360 {
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
	variance := vp9BlockSourceVariance128(src, stride, x0, y0, w, h)
	scaled := (uint64(256) * variance) / uint64(w*h)
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
	for plane := 0; plane < vp9dec.MaxMbPlane; plane++ {
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

	bestMode := common.DcPred
	bestScore, ok := e.scoreVP9KeyframeMode(key, bestMode,
		yModeCosts[bestMode], qindex, tile, miRows, miCols, miRow, miCol,
		bsize, mi)
	if !ok {
		return bestMode
	}
	// The realtime keyframe picker mirrors vp9_pick_intra_mode and only
	// evaluates DC, V, and H for >=8x8 blocks.
	for mode := common.DcPred + 1; mode <= common.HPred; mode++ {
		score, ok := e.scoreVP9KeyframeMode(key, mode, yModeCosts[mode],
			qindex, tile, miRows, miCols, miRow, miCol, bsize, mi)
		if ok && score < bestScore {
			bestScore = score
			bestMode = mode
		}
	}
	return bestMode
}

func (e *VP9Encoder) scoreVP9KeyframeMode(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, rate, qindex int, tile vp9dec.TileBounds,
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
	return vp9RDCost(vp9KeyframeRDMul(qindex), vp9RDDivBits, rate, distortion), true
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

	txSize := mi.TxSize
	if txSize > common.Tx16x16 {
		txSize = common.Tx16x16
	}
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
	for y := 0; y < h; y++ {
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
	for i := 0; i < n; i++ {
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
	for i := 0; i < n; i++ {
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
	for idx := 0; idx < 8; idx++ {
		vp9HadamardCol8(src[idx:], stride, buffer[idx*8:])
	}
	for idx := 0; idx < 8; idx++ {
		vp9HadamardCol8(buffer[idx:], 8, buffer2[idx*8:])
	}
	copy(coeff[:64], buffer2[:])
}

func vp9Hadamard16x16Into(src []int16, stride int, coeff []int16) {
	vp9Hadamard8x8Into(src, stride, coeff[:64])
	vp9Hadamard8x8Into(src[8:], stride, coeff[64:128])
	vp9Hadamard8x8Into(src[8*stride:], stride, coeff[128:192])
	vp9Hadamard8x8Into(src[8*stride+8:], stride, coeff[192:256])
	for idx := 0; idx < 64; idx++ {
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
	return vp9ModeDecisionScore(distortion, rate, qindex), true
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
			bsize, mi.TxSize)
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
			bsize, mi.TxSize)
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
	} else if decision, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow, miCol, bsize); ok {
		picked = decision
		mi.Mode = decision.mode
		mi.Mv = decision.mv
		mi.RefFrame = [2]int8{decision.refFrame, decision.secondRefFrame}
		mi.InterpFilter = uint8(decision.interpFilter)
		inter.ref = &e.refFrames[decision.refSlot]
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
	bsize common.BlockSize, maxTx common.TxSize,
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
	if maxTx == common.Tx8x8 && sse > pixels*512 && activity > pixels*128 {
		return maxTx
	}
	// The realtime oracle keeps smooth changed inter blocks below 32x32, while
	// still allowing textured residuals to use the scored Tx32 path below.
	if sse <= pixels*512 || activity <= pixels*16 {
		if maxTx > common.Tx16x16 {
			return common.Tx16x16
		}
		return maxTx
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
	minTx := maxTx - 1
	if minTx < common.Tx4x4 {
		minTx = common.Tx4x4
	}
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
		score := vp9ModeDecisionScore(distortion, rate, qindex)
		if score < bestScore || (score == bestScore && rate < bestRate) {
			bestScore = score
			bestRate = rate
			bestTx = tx
		}
	}
	e.restoreVP9PartitionReconSnapshot(reconSnap)
	return bestTx
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
	for plane := 0; plane < 1; plane++ {
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

	for plane := 0; plane < 1; plane++ {
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
	interAdjusted := interScore + vp9ModeDecisionRateScore(
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
	for i := 0; i < modeCount; i++ {
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
			score:  vp9ModeDecisionScore(distortion, rate, qindex),
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
	maxTx = clampVP9TxSizeForBlock(maxTx, bsize)
	if maxTx > common.Tx16x16 {
		maxTx = common.Tx16x16
	}
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
			score:  vp9ModeDecisionScore(yDist+uvDist, rate, qindex),
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

func (e *VP9Encoder) pickVP9InterReferenceMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if inter == nil {
		return vp9InterModeDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)

	bestSet := false
	var best vp9InterModeDecision
	for _, refFrame := range [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
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
	if inter.compoundAllowed && inter.referenceMode != vp9dec.SingleReference {
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
			score:          vp9InterModeScore(distortion, rate, qindex),
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}

	e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
		refFrame, refSlot, common.ZeroMv, [2]vp9dec.MV{},
		[2]vp9dec.MV{}, consider)

	for _, mode := range [...]common.PredictionMode{common.NearestMv, common.NearMv} {
		var mv [2]vp9dec.MV
		ok := true
		for ref := 0; ref < 2; ref++ {
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

	var newMv, newRefMv [2]vp9dec.MV
	newOK := true
	newHasMotion := false
	for ref := 0; ref < 2; ref++ {
		inter.ref = &e.refFrames[refSlot[ref]]
		newMv[ref], _, newOK = e.pickVP9InterMvAllowZero(inter, miRows, miCols,
			miRow, miCol, bsize, refFrame[ref])
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
	useResidualScore := e.vp9InterPreferVarianceRoot(inter, miRows, miCols,
		miRow, miCol, bsize)
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
			score:        vp9InterModeScore(distortion, rate, qindex),
		}
		if useResidualScore && refFrame == vp9dec.LastFrame {
			if rdDist, rdRate, ok := e.scoreVP9InterModeResidual(inter, miRows,
				miCols, miRow, miCol, bsize, mode, refFrame, mv, filter); ok {
				cand.distortion = rdDist
				cand.rate = rate + rdRate
				cand.score = vp9InterModeScore(cand.distortion, cand.rate, qindex)
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
	filters := vp9InterInterpFilterCandidates(inter)
	for _, filter := range filters {
		consider(common.ZeroMv, vp9dec.MV{}, vp9dec.MV{}, filter,
			zeroDistortion)
	}

	for _, mode := range [...]common.PredictionMode{common.NearestMv, common.NearMv} {
		mv, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, inter.allowHP,
			inter.refSignBias)
		if !ok {
			continue
		}
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

	if mv, _, ok := e.pickVP9InterMv(inter, miRows, miCols,
		miRow, miCol, bsize, refFrame); ok {
		refMv, _ := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, common.NewMv, refFrame, inter.allowHP,
			inter.refSignBias)
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
	mv, score, ok := e.pickVP9InterMvAllowZero(inter, miRows, miCols,
		miRow, miCol, bsize, refFrame)
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

	const (
		searchRadius = 16
		coarseStep   = 8
		minStep      = 1
	)
	for dy := -searchRadius; dy <= searchRadius; dy += coarseStep {
		for dx := -searchRadius; dx <= searchRadius; dx += coarseStep {
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
					if dx < -searchRadius || dx > searchRadius ||
						dy < -searchRadius || dy > searchRadius {
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
	mv, bestScore = e.refineVP9InterSubpelMv(inter, miRows, miCols,
		miRow, miCol, bsize, refFrame, mv, bestScore)
	return mv, bestScore, true
}

func (e *VP9Encoder) refineVP9InterSubpelMv(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	refFrame int8, best vp9dec.MV, bestScore uint64,
) (vp9dec.MV, uint64) {
	minStep := int16(2)
	if inter != nil && inter.allowHP {
		minStep = 1
	}
	for step := int16(4); step >= minStep; step >>= 1 {
		improved := true
		for improved {
			improved = false
			center := best
			for row := center.Row - step; row <= center.Row+step; row += step {
				for col := center.Col - step; col <= center.Col+step; col += step {
					cand := vp9dec.MV{Row: row, Col: col}
					vp9ClampMvRef(&cand, miRows, miCols, miRow, miCol, bsize)
					vp9dec.LowerMvPrecision(&cand, inter != nil && inter.allowHP)
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
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, false
	}
	return vp9BlockSAD(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, scoreW, scoreH, limit), true
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

func (e *VP9Encoder) vp9EncoderFrameQIndex(isKey, intraOnly bool, flags EncodeFlags, macroblocks int) int {
	if vp9OracleTraceBuild {
		e.resetVP9OracleRateSelectionTrace()
	}
	if e.opts.Lossless {
		return 0
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
	qindex := cq + vp9ComputeQDelta(best, worst, cq, num, den)
	if qindex < best {
		qindex = best
	}
	if qindex > worst {
		qindex = worst
	}
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
			cqLevel = vp9DefaultCQLevel
			if cqLevel < minQ {
				cqLevel = minQ
			}
			if cqLevel > maxQ {
				cqLevel = maxQ
			}
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

func vp9InterModeScore(sad uint64, rate, qindex int) uint64 {
	return vp9ModeDecisionScore(sad, rate, qindex)
}

func vp9ModeDecisionScore(distortion uint64, rate, qindex int) uint64 {
	return (distortion << encoder.VP9ProbCostShift) +
		vp9ModeDecisionRateScore(rate, qindex)
}

func vp9AddModeDecisionRate(score uint64, rate, qindex int) uint64 {
	return score + vp9ModeDecisionRateScore(rate, qindex)
}

func vp9ModeDecisionRateScore(rate, qindex int) uint64 {
	if rate < 0 {
		rate = 0
	}
	lambda := 1
	if qindex > 0 {
		lambda += qindex / 32
	}
	return uint64(rate * lambda)
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
	if limit == ^uint64(0) {
		if sad, ok := vp9BlockSADNoLimit(src, srcStride, ref, refStride,
			srcX, srcY, refX, refY, w, h); ok {
			return uint64(sad)
		}
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
	refFinder := VP9Decoder{
		miGrid:          e.miGrid,
		usePrevFrameMvs: e.useVP9EncoderPrevFrameMvs(miRows, miCols),
		prevFrameMvs:    e.prevFrameMvs,
		prevFrameMvRows: e.prevFrameMvRows,
		prevFrameMvCols: e.prevFrameMvCols,
	}
	refList, refCount := refFinder.vp9FindInterMvRefs(tile, miRows, miCols,
		miRow, miCol, bsize, mode, refFrame, signBias)
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
	return ok && !predictor.unsupportedReconstruct
}

func (e *VP9Encoder) clearVP9PlaneBlockCoeffs(plane int, bsize common.BlockSize) {
	if plane < 0 || plane >= vp9dec.MaxMbPlane || bsize >= common.BlockSizes {
		return
	}
	n := int(common.Num4x4BlocksWideLookup[bsize]) *
		int(common.Num4x4BlocksHighLookup[bsize]) * vp9EncoderTxCoeffSlots
	if n > len(e.blockCoeffs[plane]) {
		n = len(e.blockCoeffs[plane])
	}
	for i := range e.blockCoeffs[plane][:n] {
		e.blockCoeffs[plane][i] = 0
	}
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
	size := headerSlack + raw420*4
	if size < 65536 {
		size = 65536
	}
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
func (e *VP9Encoder) Close() error {
	if e == nil {
		return ErrClosed
	}
	if vp9OracleTraceBuild {
		e.resetVP9OracleTraceState()
	}
	if e.vp9TilePool != nil {
		e.vp9TilePool.shutdownPool()
		e.vp9TilePool = nil
	}
	e.closed = true
	return nil
}

// Codec reports the codec this encoder targets.
func (e *VP9Encoder) Codec() Codec { return CodecVP9 }
