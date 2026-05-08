package govpx

import (
	"errors"
	"io"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

type Deadline int

const (
	DeadlineBestQuality Deadline = iota
	DeadlineGoodQuality
	DeadlineRealtime
)

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

type EncoderOptions struct {
	Width  int
	Height int

	// Convenience framerate model.
	// If FPS is set, TimebaseNum/TimebaseDen may be derived.
	FPS int

	// Explicit timing model.
	TimebaseNum int
	TimebaseDen int

	Threads int

	// Rate control.
	RateControlMode   RateControlMode
	TargetBitrateKbps int
	MinBitrateKbps    int
	MaxBitrateKbps    int

	MinQuantizer int
	MaxQuantizer int
	// CQLevel mirrors libvpx's VP8E_SET_CQ_LEVEL. In RateControlCQ mode,
	// zero uses libvpx's default level unless MinQuantizer is also zero.
	CQLevel int

	UndershootPct int
	OvershootPct  int

	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int
	MaxIntraBitratePct  int
	GFCBRBoostPct       int

	DropFrameAllowed bool

	TemporalScalability TemporalScalabilityConfig

	// Realtime/performance behavior.
	Deadline Deadline
	CpuUsed  int

	// GOP/keyframe behavior.
	KeyFrameInterval int
	// LookaheadFrames enables buffered encoding. When positive, EncodeInto
	// queues input frames and returns ErrFrameNotReady until enough future
	// frames are available; FlushInto drains the queue at end of stream.
	LookaheadFrames int
	// AutoAltRef gates the automatic alternate-reference scheduling driver
	// (libvpx vp8/encoder/onyx_if.c oxcf.play_alternate). When true and the
	// encoder is configured with LookaheadFrames > 1 and !ErrorResilient, the
	// driver inserts hidden alt-ref frames pulled from the lookahead window
	// and flips the alt-ref sign bias on the matching deferred show frame.
	AutoAltRef bool
	// AdaptiveKeyFrames enables one-pass scene-cut detection. When a large
	// source/reference error shift is detected, the frame is promoted to a
	// keyframe before rate control and mode decision run; non-realtime
	// one-pass encodes also mirror libvpx's post-inter auto-key recode when
	// the committed inter-mode map crosses the intra-percentage thresholds.
	AdaptiveKeyFrames bool

	// VP8 behavior.
	ErrorResilient bool
	// TokenPartitions is VP8's token partition selector: 0=one, 1=two, 2=four, 3=eight.
	TokenPartitions int

	// Quality knobs.
	Sharpness int
	// NoiseSensitivity mirrors libvpx's VP8E_SET_NOISE_SENSITIVITY: 0=off,
	// 1=Y denoise, 2=YUV denoise, 3..6=more aggressive YUV denoise.
	NoiseSensitivity int
	// ARNRMaxFrames/ARNRStrength/ARNRType mirror libvpx's ARNR controls.
	// ARNRType is 1=backward, 2=forward, 3=centered; zero-valued
	// EncoderOptions default to libvpx's centered filter.
	ARNRMaxFrames int
	ARNRStrength  int
	ARNRType      int
	// TwoPassStats enables second-pass VBR planning when non-empty.
	TwoPassStats      []FirstPassFrameStats
	TwoPassVBRBiasPct int
	TwoPassMinPct     int
	TwoPassMaxPct     int
	// ScreenContentMode mirrors libvpx's VP8E_SET_SCREEN_CONTENT_MODE:
	// 0=off, 1=on, 2=on with more aggressive rate control.
	ScreenContentMode int
	// StaticThreshold mirrors libvpx's VP8E_SET_STATIC_THRESHOLD /
	// oxcf.encode_breakout for first-pass and inter-frame static skips.
	StaticThreshold int

	// OracleTraceWriter is an off-by-default oracle harness output. When
	// non-nil, the encoder writes a deterministic JSON Lines trace describing
	// per-frame state and per-macroblock decisions for inter frames. The
	// schema is intended to be compared against equivalent output instrumented
	// from libvpx for parity validation. Leave nil for normal encoding; no
	// allocation or work is performed when nil.
	OracleTraceWriter io.Writer
}

type EncodeResult struct {
	Data []byte

	KeyFrame bool
	Dropped  bool
	// Droppable reports libvpx's encoded-frame discardability signal: true
	// when the frame updates no reference, entropy, or segmentation state.
	Droppable bool
	// SceneCut reports that adaptive or two-pass scene-cut logic promoted this
	// frame to a keyframe.
	SceneCut bool
	// LookaheadDepth reports queued future frames remaining after this frame.
	LookaheadDepth int
	ARNRFiltered   bool
	Denoised       bool
	// FirstPassStats is populated from TwoPassStats when second-pass planning
	// drives this frame.
	FirstPassStats FirstPassFrameStats
	// TwoPassFrameTargetBits reports the second-pass VBR target when
	// TwoPassStats drives the frame.
	TwoPassFrameTargetBits int

	PTS      uint64
	Duration uint64

	Quantizer int

	SizeBytes int

	TargetBitrateKbps int
	FrameTargetBits   int
	BufferLevelBits   int

	TemporalLayerID                int
	TemporalLayerCount             int
	TemporalLayerSync              bool
	TL0PICIDX                      uint8
	TemporalLayerTargetBitrateKbps int
	// TemporalLayerCumulativeBitrateKbps reports the cumulative libvpx
	// ts_target_bitrate[] entry for TemporalLayerID.
	TemporalLayerCumulativeBitrateKbps int
	TemporalLayerFrameBandwidthBits    int
	TemporalLayerBufferLevelBits       int
	TemporalLayerMaximumBufferBits     int
	TemporalLayerInputFrames           int
	TemporalLayerEncodedFrames         int
	TemporalLayerTotalEncodedFrames    int
	TemporalLayerEncodedBits           int

	PSNRHint float64
}

type VP8Encoder struct {
	opts EncoderOptions

	timing   timingState
	rc       rateControlState
	temporal temporalState

	closed        bool
	forceKeyFrame bool
	frameCount    uint64

	// libvpx vp8/encoder/onyx_if.c forced-key bookkeeping. this_key_frame_forced
	// is set when the encoder is producing a key frame whose timing was
	// dictated by the fixed-interval scheduler (rather than a content scene
	// cut); the recode loop uses it to drive the SS-error feedback Q
	// adjustment (encode_frame_to_data_rate around line 4065). ambient_err
	// stores the just-encoded SS error (Y plane vs source) of the frame
	// preceding a forced key, set by the next_key_frame_forced branch at the
	// tail of the previous frame's encode (line 4282); the forced KF recode
	// branch compares the new attempt's kf_err against this baseline.
	thisKeyFrameForced bool
	ambientErr         int

	// savedContext mirrors cpi->coding_context. Populated by
	// saveCodingContext at the top of each recode loop and consumed by
	// restoreCodingContext when an attempt is rejected.
	savedContext savedCodingContext

	cyclicRefreshIndex      int
	cyclicRefreshMap        []int8
	cyclicRefreshAttemptMap []int8
	skinMap                 []uint8
	consecZeroLast          []uint8
	lastInterZeroMVCount    int
	lastInterSkipCount      int

	// libvpx active-map: when enabled, MBs flagged 0 skip mode decision in
	// inter frames and code as ZEROMV-LAST with skip=1 (see pickinter.c
	// evaluate_inter_mode and rdopt.c rd_pick_inter_mode active_ptr checks).
	// Key frames ignore the map.
	activeMap        []uint8
	activeMapEnabled bool

	// libvpx dot-artifact suppression: count of MBs that have biased
	// against ZEROMV-LAST this frame (capped at MBs/10), and the current
	// frame's temporal layer ID. See vp8/encoder/pickinter.c
	// check_dot_artifact_candidate. consecZeroLastMVBias mirrors
	// cpi->consec_zero_last_mvbias and resets to 0 on any MB that was
	// dot-suppression-checked this frame, so the threshold gate gives the
	// same MB a fresh chance after num_frames have passed.
	mbsZeroLastDotSuppress int
	currentTemporalLayer   int
	consecZeroLastMVBias   []uint8
	dotArtifactChecked     []bool

	// forceMaxQuantizer mirrors libvpx's cpi->force_maxqp. It is set when an
	// overshoot drop forces the *next* inter frame to be encoded at max Q
	// (vp8/encoder/ratectrl.c vp8_drop_encodedframe_overshoot) and disables
	// cyclic background refresh while it is set (vp8/encoder/onyx_if.c). The
	// flag is cleared after the next non-dropped frame commits.
	forceMaxQuantizer bool

	// Cross-frame inter-mode reference-frame probabilities. libvpx
	// (onyx_if.c init) seeds these with 63/128/128 and updates them after each
	// inter frame from observed mb_ref_frame counts (see
	// vp8_estimate_entropy_savings); RD scoring needs the previous-frame
	// values, not the per-frame static 128 default.
	refProbIntra  uint8
	refProbLast   uint8
	refProbGolden uint8
	// libvpx update_rd_ref_frame_probs (onyx_if.c) heuristically biases the
	// reference-frame probabilities used by the *current* frame's RD scoring
	// based on the upcoming refresh policy. It tracks frames_since_golden and
	// source_alt_ref_active to drive the bumps; see update_golden_frame_stats
	// for the lifecycle of those counters.
	framesSinceGolden  int
	sourceAltRefActive bool
	// libvpx vp8/encoder/onyx_if.c automatic ARF scheduling state:
	// source_alt_ref_pending is set when the encoder has decided to
	// insert a hidden ARF on a future frame; alt_ref_source identifies
	// the lookahead entry that will become the ARF source so the
	// later show-frame can detect is_src_frame_alt_ref.
	// framesTillAltRefFrame counts down from the current ARF section
	// length so the encoder knows when to emit the hidden frame.
	sourceAltRefPending   bool
	altRefSourcePTS       uint64
	altRefSourceValid     bool
	framesTillAltRefFrame int
	// autoAltRefStash holds a single input frame deferred by the auto
	// alternate-reference driver. Emitting a hidden ARF in EncodeInto does
	// not pop the lookahead, leaving it at capacity; the user's source frame
	// is stashed here and flushed into the lookahead on the next call. The
	// stash is populated/consumed exclusively by encoder_altref_driver.go.
	autoAltRefStashValid    bool
	autoAltRefStashFrame    vp8common.FrameBuffer
	autoAltRefStashPTS      uint64
	autoAltRefStashDuration uint64
	autoAltRefStashFlags    EncodeFlags
	// currentSourcePTS mirrors libvpx onyx_if.c's per-frame
	// `cpi->source` PTS so isSrcFrameAltRef can detect the deferred
	// show frame after a hidden ARF.
	currentSourcePTS uint64
	// libvpx vp8/encoder/onyx_if.c decide_key_frame heuristic compares
	// this_frame_percent_intra against last_frame_percent_intra; track
	// the rolling lookback here so the helper sees the same state libvpx
	// would.
	lastFramePercentIntra int
	// libvpx also carries a skip-false probability for inter RD costing. The
	// packet writer adapts the final value from this frame's skip counts; mode
	// decision uses the previous refreshed reference's value, clamped away from
	// the extremes.
	probSkipFalse      uint8
	lastSkipFalseProbs [3]uint8
	baseSkipFalseProbs [vp8common.QIndexRange]uint8

	lookahead []lookaheadEntry

	preprocess     vp8common.FrameBuffer
	arnrScratch    vp8common.FrameBuffer
	arnrLastSource vp8common.FrameBuffer
	arnrLastReady  bool
	// libvpx-style temporal denoiser. Maintains a parallel running_avg
	// stream (per reference) that mc-filters the source toward the picked
	// motion-compensated prediction, plus a per-MB FILTER/COPY/NoFilter
	// state machine. See vp8/encoder/denoising.c.
	denoiser denoiserState

	firstPassLastRef    vp8common.FrameBuffer
	firstPassGoldenRef  vp8common.FrameBuffer
	firstPassLastSource vp8common.FrameBuffer
	firstPassNewRef     vp8common.FrameBuffer
	firstPassCount      uint64

	twoPass twoPassState

	lookaheadRead  int
	lookaheadWrite int
	lookaheadCount int

	keyFrameModes   []vp8enc.KeyFrameMacroblockMode
	interFrameModes []vp8enc.InterFrameMacroblockMode
	// libvpx's improved MV predictor reads the previous inter frame's
	// MODE_INFO grid (lfmv/lf_ref_frame) when the last coded frame was inter.
	lastFrameInterModes      []vp8enc.InterFrameMacroblockMode
	lastFrameInterModeBias   []bool
	lastFrameInterModesValid bool
	keyFrameCoeffs           []vp8enc.MacroblockCoefficients
	tokenAbove               []vp8enc.TokenContextPlanes

	interRDThreshMult       [libvpxInterModeCount]int
	interRDThreshTouched    [libvpxInterModeCount]bool
	interModeCheckFreq      [libvpxInterModeCount]int
	interModeTestHitCounts  [libvpxInterModeCount]int
	interMBsTestedSoFar     int
	interModeErrorBins      [1024]uint32
	interModeSpeedErrorBins [1024]uint32
	interRDFrameActive      bool

	current   vp8common.FrameBuffer
	analysis  vp8common.FrameBuffer
	lastRef   vp8common.FrameBuffer
	goldenRef vp8common.FrameBuffer
	altRef    vp8common.FrameBuffer
	// Mirrors libvpx cpi->current_ref_frames[] for closest-reference policy.
	// Values are frameCount values from the encoded frame that last refreshed
	// or was copied into each reference buffer.
	referenceFrameNumbers [vp8common.MaxRefFrames]uint64
	// Mirrors libvpx gold_is_last / alt_is_last / gold_is_alt. These flags
	// prune duplicate reference candidates after refreshes make buffers alias.
	goldenRefAliasesLast bool
	altRefAliasesLast    bool
	goldenRefAliasesAlt  bool

	loopFilterPick     vp8common.FrameBuffer
	reconstructModes   []vp8dec.MacroblockMode
	reconstructTokens  []vp8dec.MacroblockTokens
	dequantTables      vp8common.FrameDequantTables
	dequants           [vp8common.MaxMBSegments]vp8common.MacroblockDequant
	reconstructScratch vp8dec.IntraReconstructionScratch
	loopInfo           vp8common.LoopFilterInfo
	loopFilterLevel    uint8
	coefProbs          vp8tables.CoefficientProbs
	modeProbs          vp8dec.ModeProbs

	// oracleTraceMBBuffer accumulates per-MB oracle trace rows for the inter
	// frame currently being built. Rows from intermediate recode attempts are
	// reset; the final committed attempt's rows are flushed to
	// EncoderOptions.OracleTraceWriter at frame end. Unused (nil) when the
	// oracle trace is disabled.
	oracleTraceMBBuffer []oracleTraceMBRow
	// oracleTraceInterCandidateBuffer accumulates evaluated inter-mode
	// candidate rows for the same accepted encode attempt as oracleTraceMBBuffer.
	oracleTraceInterCandidateBuffer []oracleTraceInterCandidateRow

	// oracleTraceRecodeLoopCount counts encode-attempt iterations within the
	// in-flight key/inter recode loop. Reset to 0 at the start of each loop
	// and incremented per attempt; consumed when the rate/recode rows are
	// emitted at frame commit. Unused outside the trace harness.
	oracleTraceRecodeLoopCount int
	oracleTraceRecodeReason    string

	// oracleTraceTotalByteCount accumulates the total bytes emitted across
	// every committed frame, mirroring libvpx's cpi->total_byte_count for
	// the rate-row oracle trace. Updated only when the trace is enabled.
	oracleTraceTotalByteCount int64
}

const encoderQuantizerFeedbackMaxAttempts = 8

// savedCodingContext mirrors libvpx vp8/encoder/ratectrl.c CODING_CONTEXT.
// vp8_save_coding_context snapshots these fields before the recode do-loop in
// encode_frame_to_data_rate; vp8_restore_coding_context puts them back when a
// recode attempt is rejected (Loop==1) so the next attempt sees the same
// pre-encode state. govpx defers entropy/skip-prob commits past the loop, so
// most fields are also untouched mid-loop, but the snapshot/restore pair makes
// that contract explicit and verifiable.
type savedCodingContext struct {
	// libvpx CODING_CONTEXT scalars.
	framesSinceKey        int
	filterLevel           uint8
	framesTillGFUpdateDue int
	framesSinceGolden     int
	thisFramePercentIntra int
	// libvpx CODING_CONTEXT array fields (mvc/ymode_prob/uv_mode_prob).
	// govpx stores MV/Y/UV/B mode probabilities in e.modeProbs (vp8dec.ModeProbs)
	// and coefficient probabilities in e.coefProbs.
	modeProbs vp8dec.ModeProbs
	coefProbs vp8tables.CoefficientProbs
	// libvpx also tracks ref-frame and skip-false probabilities across the
	// recode loop via cpi->prob_intra/prob_last/prob_gf and
	// cpi->prob_skip_false; govpx's per-attempt deferred restore covers them
	// inside one attempt, but the recode-loop snapshot pins the values that
	// survive a rejected attempt.
	refProbIntra       uint8
	refProbLast        uint8
	refProbGolden      uint8
	probSkipFalse      uint8
	lastSkipFalseProbs [3]uint8
	valid              bool
}

// saveCodingContext mirrors libvpx vp8_save_coding_context. Called once before
// the encode/recode do-loop; the snapshot is consumed by restoreCodingContext
// when a recode attempt is rejected.
func (e *VP8Encoder) saveCodingContext() {
	e.savedContext = savedCodingContext{
		framesSinceKey:        e.rc.framesSinceKeyframe,
		filterLevel:           e.loopFilterLevel,
		framesTillGFUpdateDue: e.rc.framesTillGFUpdateDue,
		framesSinceGolden:     e.framesSinceGolden,
		thisFramePercentIntra: e.rc.thisFramePercentIntra,
		modeProbs:             e.modeProbs,
		coefProbs:             e.coefProbs,
		refProbIntra:          e.refProbIntra,
		refProbLast:           e.refProbLast,
		refProbGolden:         e.refProbGolden,
		probSkipFalse:         e.probSkipFalse,
		lastSkipFalseProbs:    e.lastSkipFalseProbs,
		valid:                 true,
	}
}

// restoreCodingContext mirrors libvpx vp8_restore_coding_context. Called when
// the recode loop decides to re-encode at a different Q so the next attempt
// starts from the pre-encode state captured by saveCodingContext.
func (e *VP8Encoder) restoreCodingContext() {
	if !e.savedContext.valid {
		return
	}
	e.rc.framesSinceKeyframe = e.savedContext.framesSinceKey
	e.loopFilterLevel = e.savedContext.filterLevel
	e.rc.framesTillGFUpdateDue = e.savedContext.framesTillGFUpdateDue
	e.framesSinceGolden = e.savedContext.framesSinceGolden
	e.rc.thisFramePercentIntra = e.savedContext.thisFramePercentIntra
	e.modeProbs = e.savedContext.modeProbs
	e.coefProbs = e.savedContext.coefProbs
	e.refProbIntra = e.savedContext.refProbIntra
	e.refProbLast = e.savedContext.refProbLast
	e.refProbGolden = e.savedContext.refProbGolden
	e.probSkipFalse = e.savedContext.probSkipFalse
	e.lastSkipFalseProbs = e.savedContext.lastSkipFalseProbs
}

type keyFrameEncodeAttempt struct {
	FrameCoefProbs      vp8tables.CoefficientProbs
	Size                int
	ProjectedSizeBits   int
	LoopFilterLevel     uint8
	SharpnessLevel      uint8
	LFDeltaEnabled      bool
	LFDeltaUpdate       bool
	RefLFDeltas         [vp8common.MaxRefLFDeltas]int8
	ModeLFDeltas        [vp8common.MaxModeLFDeltas]int8
	RefreshEntropyProbs bool
	SegmentationEnabled bool
}

type interFrameEncodeAttempt struct {
	Config                 vp8enc.InterFrameStateConfig
	FrameCoefProbs         vp8tables.CoefficientProbs
	FrameYModeProbs        [vp8tables.YModeProbCount]uint8
	FrameUVModeProbs       [vp8tables.UVModeProbCount]uint8
	FrameMVProbs           [2][vp8tables.MVPCount]uint8
	RefFrame               vp8common.MVReferenceFrame
	Ref                    *vp8common.Image
	Size                   int
	ProjectedSizeBits      int
	ZeroReference          bool
	CyclicRefresh          bool
	CyclicRefreshNextIndex int
}

func NewVP8Encoder(opts EncoderOptions) (*VP8Encoder, error) {
	normalized, timing, err := normalizeEncoderOptions(opts)
	if err != nil {
		return nil, err
	}

	cfg := defaultRateControlConfig(normalized)
	e := &VP8Encoder{
		opts:                    normalized,
		timing:                  timing,
		cyclicRefreshMap:        make([]int8, encoderMacroblockCount(normalized.Width, normalized.Height)),
		cyclicRefreshAttemptMap: make([]int8, encoderMacroblockCount(normalized.Width, normalized.Height)),
		skinMap:                 make([]uint8, encoderMacroblockCount(normalized.Width, normalized.Height)),
		consecZeroLast:          make([]uint8, encoderMacroblockCount(normalized.Width, normalized.Height)),
		consecZeroLastMVBias:    make([]uint8, encoderMacroblockCount(normalized.Width, normalized.Height)),
		dotArtifactChecked:      make([]bool, encoderMacroblockCount(normalized.Width, normalized.Height)),
		activeMap:               make([]uint8, encoderMacroblockCount(normalized.Width, normalized.Height)),
		keyFrameModes:           make([]vp8enc.KeyFrameMacroblockMode, encoderMacroblockCount(normalized.Width, normalized.Height)),
		interFrameModes:         make([]vp8enc.InterFrameMacroblockMode, encoderMacroblockCount(normalized.Width, normalized.Height)),
		lastFrameInterModes:     make([]vp8enc.InterFrameMacroblockMode, encoderMacroblockCount(normalized.Width, normalized.Height)),
		lastFrameInterModeBias:  make([]bool, encoderMacroblockCount(normalized.Width, normalized.Height)),
		keyFrameCoeffs:          make([]vp8enc.MacroblockCoefficients, encoderMacroblockCount(normalized.Width, normalized.Height)),
		tokenAbove:              make([]vp8enc.TokenContextPlanes, encoderMacroblockCols(normalized.Width)),

		reconstructModes:   make([]vp8dec.MacroblockMode, encoderMacroblockCount(normalized.Width, normalized.Height)),
		reconstructTokens:  make([]vp8dec.MacroblockTokens, encoderMacroblockCount(normalized.Width, normalized.Height)),
		coefProbs:          vp8tables.DefaultCoefProbs,
		refProbIntra:       63,
		refProbLast:        128,
		refProbGolden:      128,
		probSkipFalse:      128,
		baseSkipFalseProbs: libvpxBaseSkipFalseProbs,
	}
	e.resetInterRDThresholdMultipliers()
	vp8dec.ResetModeProbs(&e.modeProbs)
	if err := e.initReferenceFrames(normalized.Width, normalized.Height); err != nil {
		return nil, err
	}
	if err := e.initPreprocessFrames(normalized.Width, normalized.Height); err != nil {
		return nil, err
	}
	if err := e.initLookahead(normalized.Width, normalized.Height, normalized.LookaheadFrames); err != nil {
		return nil, err
	}
	if err := e.rc.applyConfig(cfg, timing); err != nil {
		return nil, err
	}
	// libvpx vp8/encoder/ratectrl.c estimate_keyframe_frequency uses
	// cpi->output_framerate plus cpi->oxcf.auto_key/key_freq for the
	// first-keyframe bootstrap; seed the options that are not part of the
	// public rate-control config so early kf_bitrate_adjustment matches
	// libvpx.
	e.rc.keyFrameFrequency = normalized.KeyFrameInterval
	e.rc.autoKeyFrames = normalized.AdaptiveKeyFrames
	// libvpx vp8/encoder/onyx_if.c sets cpi->min_frame_bandwidth =
	// av_per_frame_bandwidth * two_pass_vbrmin_section / 100; mirror
	// that so calc_pframe_target_size's min_frame_target floor and
	// calcGFParams allocation_chunks see the same value.
	e.rc.minFrameBandwidth = vbrMinFrameBandwidthBits(e.rc.bitsPerFrame, normalized.TwoPassMinPct)
	// libvpx's one-pass VBR/CQ setup seeds a provisional GF countdown from
	// DEFAULT_GF_INTERVAL before the first key frame. The key-frame GOLDEN
	// refresh then decrements it, preventing an immediate frame-1 auto_gold
	// refresh.
	if e.rc.mode != RateControlCBR && len(normalized.TwoPassStats) == 0 {
		e.rc.framesTillGFUpdateDue = libvpxDefaultGFInterval
	}
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = e.rc.cqLevel
	} else {
		e.rc.currentQuantizer = e.rc.minQuantizer
	}
	e.rc.lastQuantizer = e.rc.currentQuantizer
	e.rc.lastInterQuantizer = e.rc.currentQuantizer
	e.opts.MinQuantizer = cfg.MinQuantizer
	e.opts.MaxQuantizer = cfg.MaxQuantizer
	e.opts.CQLevel = normalizedCQLevel(cfg.CQLevel, cfg.MinQuantizer)
	if err := e.temporal.configure(normalized.TemporalScalability, e.rc.targetBitrateKbps); err != nil {
		return nil, err
	}
	e.opts.TemporalScalability = e.temporal.config
	e.twoPass.configure(normalized.TwoPassStats, e.rc.bitsPerFrame, normalized.TwoPassVBRBiasPct, normalized.TwoPassMinPct, normalized.TwoPassMaxPct)
	return e, nil
}

func (e *VP8Encoder) EncodeInto(dst []byte, src Image, pts uint64, duration uint64, flags EncodeFlags) (EncodeResult, error) {
	if e == nil || e.closed {
		return EncodeResult{}, ErrClosed
	}
	if !src.validForEncode(e.opts.Width, e.opts.Height) {
		return EncodeResult{}, ErrInvalidConfig
	}
	if len(dst) == 0 {
		return EncodeResult{}, ErrBufferTooSmall
	}
	if e.lookaheadEnabled() {
		if result, ok, err := e.autoAltRefMaybeEncode(dst, src, pts, duration, flags); ok {
			return result, err
		}
		result, err := e.encodeLookaheadInto(dst, src, pts, duration, flags)
		if err == nil {
			e.autoAltRefMaybeSchedule()
		}
		return result, err
	}
	return e.encodeSourceInto(dst, sourceImageFromImage(src), pts, duration, flags, encodeSourceMetadata{})
}

func (e *VP8Encoder) FlushInto(dst []byte) (EncodeResult, error) {
	if e == nil || e.closed {
		return EncodeResult{}, ErrClosed
	}
	if len(dst) == 0 {
		return EncodeResult{}, ErrBufferTooSmall
	}
	if !e.lookaheadEnabled() {
		return EncodeResult{}, ErrFrameNotReady
	}
	if result, ok, err := e.autoAltRefMaybeEmitHiddenOnFlush(dst); ok {
		return result, err
	}
	if e.lookaheadSize() == 0 {
		return EncodeResult{}, ErrFrameNotReady
	}
	entry, ok := e.popLookahead(true)
	if !ok {
		return EncodeResult{}, ErrFrameNotReady
	}
	meta := encodeSourceMetadata{lookaheadDepth: e.lookaheadSize()}
	result, err := e.encodeSourceInto(dst, sourceImageFromVP8(&entry.frame.Img), entry.pts, entry.duration, entry.flags, meta)
	e.clearPoppedLookahead(entry)
	if err == nil {
		e.autoAltRefMaybeSchedule()
	}
	return result, err
}

func (e *VP8Encoder) encodeSourceInto(dst []byte, source vp8enc.SourceImage, pts uint64, duration uint64, flags EncodeFlags, meta encodeSourceMetadata) (EncodeResult, error) {
	e.currentSourcePTS = pts
	temporalFrame := e.temporal.nextFrame(e.timing)
	flags |= temporalFrame.Flags
	if err := validateEncodeFlags(flags); err != nil {
		return EncodeResult{}, err
	}
	e.currentTemporalLayer = 0
	if temporalFrame.Enabled {
		e.currentTemporalLayer = temporalFrame.LayerID
	}
	e.mbsZeroLastDotSuppress = 0
	forcedKeyFrame := e.forceKeyFrameRequested(flags)
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	required := rows * cols
	preprocessed, preprocessMeta := e.preprocessSource(source, flags, meta)
	source = preprocessed
	keyFrame := e.shouldEncodeKeyFrame(flags)
	sceneCutKeyFrame := false
	twoPassSceneCut := false
	if !keyFrame && e.twoPass.shouldKeyFrame(e.frameCount, e.rc.framesSinceKeyframe, e.opts.KeyFrameInterval) {
		keyFrame = true
		sceneCutKeyFrame = true
		twoPassSceneCut = true
	}
	if !keyFrame && e.shouldEncodeSceneCutKeyFrame(source, flags, temporalFrame.Enabled, rows, cols) {
		keyFrame = true
		sceneCutKeyFrame = true
	}
	temporalReferenceControl := temporalFrame.Enabled && temporalFrame.LayerCount > 1
	goldenCBRRefresh := e.shouldRefreshGoldenFrameCBR(keyFrame, temporalReferenceControl, flags, rows, cols)
	// libvpx auto_gold one-pass non-CBR refresh decision: VBR/CQ
	// triggers GF refresh when frames_till_gf_update_due==0 and
	// pct_intra<15 || gf_frame_usage>=5. govpx funnels it through the
	// same goldenCBRRefresh local so the existing CBR-shaped code path
	// (rc bookkeeping, header copy, and post-encode GF accounting)
	// applies uniformly.
	if !goldenCBRRefresh && e.shouldRefreshGoldenFrameOnePassNonCBR(keyFrame, temporalReferenceControl, flags, rows, cols) {
		goldenCBRRefresh = true
	}
	invisible := flags&EncodeInvisibleFrame != 0
	hiddenAltRefFrame := flags&(EncodeInvisibleFrame|EncodeForceAltRefFrame) == EncodeInvisibleFrame|EncodeForceAltRefFrame
	sourceIsAltRef := !temporalFrame.Enabled && !invisible && e.isSrcFrameAltRef(pts)
	finishSourceAltRef := func() {
		if sourceIsAltRef {
			e.altRefSourceValid = false
			e.altRefSourcePTS = 0
		}
	}
	boostedReferenceFrame := boostedReferenceRateControlFrame(goldenCBRRefresh, flags)
	// libvpx vp8/encoder/ratectrl.c calc_pframe_target_size sets
	// frames_till_gf_update_due=baseline_gf_interval (== gf_interval_onepass_cbr)
	// and current_gf_interval before update_golden_frame_stats accumulates
	// gf_overspend_bits. Mirror that for govpx's CBR refresh.
	// calc_gf_params populates last_boost AFTER the per-frame target
	// (and small +/- last_boost section adjustment) has been computed,
	// so we defer the calcGFParams call until pickGoldenFrameBoost
	// runs below — populating last_boost early would feed the small
	// +/- branch with this frame's boost instead of the prior GF's.
	if goldenCBRRefresh {
		gfInterval := e.goldenFrameCBRInterval(rows, cols)
		e.rc.framesTillGFUpdateDue = gfInterval
		e.rc.currentGFInterval = gfInterval
	}
	if temporalFrame.Enabled && !keyFrame {
		e.rc.beginFrameWithTargetAndContext(false, temporalFrame.LayerFrameTargetBits, rateControlFrameContext{
			temporalLayerCount: temporalFrame.LayerCount,
			timing:             e.timing,
		})
	} else {
		e.rc.beginFrameWithTargetAndContext(keyFrame, e.rc.bitsPerFrame, rateControlFrameContext{
			firstFrame:         e.frameCount == 0,
			forcedKeyFrame:     forcedKeyFrame,
			temporalLayerCount: temporalFrame.LayerCount,
			timing:             e.timing,
		})
	}
	twoPassTargetBits := e.twoPass.frameTargetBits(e.frameCount, keyFrame, e.rc.frameTargetBits)
	if twoPassTargetBits > 0 {
		e.rc.frameTargetBits = twoPassTargetBits
		// libvpx vp8/encoder/firstpass.c Pass2Encode re-clamps the per-frame
		// target through the buffer-state adjustment for CBR
		// (USAGE_STREAM_FROM_SERVER); apply that here so the two-pass
		// override does not erase the buffer-aware shaping.
		e.rc.frameTargetBits = e.rc.applyPass2CBRBufferAdjustment(e.rc.frameTargetBits, keyFrame)
	}
	// libvpx vp8/encoder/firstpass.c define_gf_group ARF-pending decision:
	// when second-pass stats indicate the upcoming GF section is high
	// motion / high-quality predicted, arm a hidden alt-ref so the
	// auto-ARF driver can emit it at the predicted offset.
	e.pass2MaybeArmAltRefPending(e.frameCount, pts, keyFrame)
	if goldenCBRRefresh {
		// libvpx vp8/encoder/ratectrl.c calc_pframe_target_size: when the
		// GF refresh fires, calc_gf_params runs FIRST (auto_adjust_gold_quantizer=1
		// is the default) and updates cpi->last_boost AND
		// cpi->frames_till_gf_update_due. Then the GF target formula
		// consumes those just-computed values. Mirror that order here so
		// the non-CBR boost path below sees the new boost / interval.
		gfOut := calcGFParams(gfParamsInput{
			Q:                     e.rc.lastInterQuantizer,
			RecentRefIntra:        e.rc.recentRefFrameUsageIntra,
			RecentRefLast:         e.rc.recentRefFrameUsageLast,
			RecentRefGolden:       e.rc.recentRefFrameUsageGolden,
			RecentRefAltRef:       e.rc.recentRefFrameUsageAltRef,
			GFActiveCount:         e.rc.gfActiveCount,
			Macroblocks:           required,
			ThisFramePercentIntra: e.rc.thisFramePercentIntra,
			BaselineGFInterval:    e.rc.framesTillGFUpdateDue,
			MaxGFInterval:         e.rc.framesTillGFUpdateDue,
		})
		e.rc.lastBoost = gfOut.Boost
		if e.rc.mode == RateControlCBR {
			// One-pass CBR: libvpx multiplies this_frame_target by
			// (100 + gf_cbr_boost_pct) / 100 (vp8/encoder/ratectrl.c
			// gf_update_onepass_cbr branch).
			e.rc.frameTargetBits = boostedFrameTargetBits(e.rc.frameTargetBits, e.rc.gfCBRBoostPct)
		} else {
			// One-pass VBR/CQ: libvpx splits the upcoming GF section
			// across (frames_till_gf_update_due+1) frames, weighting the
			// GF by `last_boost`. See libvpxGoldenFrameTargetBits for the
			// exact formula. Falls back to the previous boostPct path if
			// inter_frame_target was not yet recorded (i.e. the first
			// inter frame after a key) - in that case the small +/- branch
			// has not yet seeded interFrameTarget so use bitsPerFrame.
			interFrameTarget := e.rc.interFrameTarget
			if interFrameTarget <= 0 {
				interFrameTarget = e.rc.bitsPerFrame
			}
			boosted := libvpxGoldenFrameTargetBits(gfOut.Boost, gfOut.FramesTillUpdate, interFrameTarget)
			if boosted > 0 {
				e.rc.frameTargetBits = boosted
			}
		}
		// Propagate the just-computed GF interval into rc state so the
		// next non-GF frame's small +/- branch sees the right value.
		// Mirrors libvpx's calc_gf_params tail (cpi->frames_till_gf_update_due
		// = baseline_gf_interval; cpi->current_gf_interval = ...).
		e.rc.framesTillGFUpdateDue = gfOut.FramesTillUpdate
		e.rc.currentGFInterval = gfOut.FramesTillUpdate
	}
	e.rc.selectQuantizerForFrameKindWithScreenContent(keyFrame, boostedReferenceFrame, required, e.opts.ScreenContentMode)
	// libvpx vp8/encoder/firstpass.c estimate_max_q applies a CQ floor
	// (`USAGE_CONSTRAINED_QUALITY -> Q = max(Q, cq_target_quality)`)
	// AFTER the second-pass Q regulation. Re-assert it here so the
	// regulated quantizer never falls below the configured CQ target.
	e.rc.applyCQFloor()

	result := EncodeResult{
		KeyFrame:                           keyFrame,
		SceneCut:                           sceneCutKeyFrame,
		LookaheadDepth:                     preprocessMeta.lookaheadDepth,
		ARNRFiltered:                       preprocessMeta.arnrFiltered,
		Denoised:                           preprocessMeta.denoised,
		FirstPassStats:                     e.twoPass.statsForFrame(e.frameCount),
		TwoPassFrameTargetBits:             twoPassTargetBits,
		PTS:                                pts,
		Duration:                           duration,
		Quantizer:                          libvpxQIndexToPublicQuantizer(e.rc.currentQuantizer),
		TargetBitrateKbps:                  e.rc.targetBitrateKbps,
		FrameTargetBits:                    e.rc.frameTargetBits,
		BufferLevelBits:                    e.rc.bufferLevelBits,
		TemporalLayerID:                    temporalFrame.LayerID,
		TemporalLayerCount:                 temporalFrame.LayerCount,
		TemporalLayerSync:                  temporalFrame.LayerSync,
		TL0PICIDX:                          temporalFrame.TL0PICIDX,
		TemporalLayerTargetBitrateKbps:     temporalFrame.LayerTargetBitrateKbps,
		TemporalLayerCumulativeBitrateKbps: temporalFrame.LayerCumulativeBitrateKbps,
	}
	if !keyFrame && !invisible && e.rc.shouldDropInterFrame() {
		e.rc.postDropFrame()
		e.twoPass.finishFrame(0)
		result.Dropped = true
		result.BufferLevelBits = e.rc.bufferLevelBits
		e.forceKeyFrame = false
		// libvpx vp8_drop_encodedframe_overshoot sets force_maxqp=1 on the
		// dropped frame so the next encoded frame is forced to max Q and
		// cyclic refresh segmentation is suppressed for that frame.
		e.forceMaxQuantizer = true
		e.temporal.finishDroppedFrame(temporalFrame, e.temporalBufferConfig())
		e.populateTemporalLayerBufferResult(&result, temporalFrame)
		e.frameCount++
		finishSourceAltRef()
		return result, nil
	}

	staticSegmentationAllowed := !temporalFrame.Enabled || temporalFrame.LayerID == 0
	if !keyFrame {
		attempt, err := e.encodeInterFrameWithQuantizerFeedback(dst, source, rows, cols, required, flags, temporalReferenceControl, goldenCBRRefresh, boostedReferenceFrame, staticSegmentationAllowed, sourceIsAltRef)
		if err != nil {
			return EncodeResult{}, err
		}
		if thisFramePercentIntra, recodeKeyFrame := e.shouldRecodeInterAttemptAsKeyFrame(required, attempt.Config.RefreshGolden, temporalFrame.Enabled, invisible); recodeKeyFrame {
			keyFrame = true
			sceneCutKeyFrame = true
			goldenCBRRefresh = false
			boostedReferenceFrame = false
			e.rc.thisFramePercentIntra = thisFramePercentIntra
			// libvpx clears source_alt_ref_active before restarting the
			// encode as a key frame; the normal key-frame commit below will
			// reset the rest of the golden-frame/alt-ref lifecycle.
			e.sourceAltRefActive = false
			e.resetOracleMBTraceBuffer()
			e.rc.beginFrameWithTargetAndContext(true, e.rc.bitsPerFrame, rateControlFrameContext{
				temporalLayerCount: temporalFrame.LayerCount,
				timing:             e.timing,
			})
			twoPassTargetBits = e.twoPass.frameTargetBits(e.frameCount, true, e.rc.frameTargetBits)
			if twoPassTargetBits > 0 {
				e.rc.frameTargetBits = twoPassTargetBits
				e.rc.frameTargetBits = e.rc.applyPass2CBRBufferAdjustment(e.rc.frameTargetBits, true)
			}
			e.rc.selectQuantizerForFrameKindWithScreenContent(true, false, required, e.opts.ScreenContentMode)
			e.rc.applyCQFloor()
			result.KeyFrame = true
			result.SceneCut = true
			result.TwoPassFrameTargetBits = twoPassTargetBits
			result.FrameTargetBits = e.rc.frameTargetBits
			result.BufferLevelBits = e.rc.bufferLevelBits
			result.Quantizer = libvpxQIndexToPublicQuantizer(e.rc.currentQuantizer)
		} else {
			finalQuantizer := e.rc.currentQuantizer
			e.commitInterFrameAttempt(attempt)
			e.loopFilterLevel = attempt.Config.LoopFilterLevel
			result.Data = dst[:attempt.Size]
			result.SizeBytes = attempt.Size
			result.Quantizer = libvpxQIndexToPublicQuantizer(finalQuantizer)
			result.Droppable = interFrameDroppable(attempt.Config)
			e.emitOracleRateAndRecodeTrace(vp8common.InterFrame, finalQuantizer, attempt.Size, attempt.ProjectedSizeBits)
			e.rc.postEncodeFrameWithPacketContext(attempt.Size, rateControlPostEncodeContext{
				goldenFrame:           attempt.Config.RefreshGolden,
				altRefFrame:           attempt.Config.RefreshAltRef,
				macroblocks:           required,
				showFrame:             !invisible,
				skipPostPackOverspend: e.twoPass.enabled(),
			})
			if hiddenAltRefFrame {
				e.twoPass.chargeAltRefFrameBits(encodedSizeBits(attempt.Size))
			} else {
				e.twoPass.finishFrame(encodedSizeBits(attempt.Size))
			}
			e.rc.clampScreenContentBufferDebt(e.opts.ScreenContentMode)
			result.BufferLevelBits = e.rc.bufferLevelBits
			e.forceKeyFrame = false
			if attempt.CyclicRefresh {
				e.commitCyclicRefresh(rows, cols, attempt.CyclicRefreshNextIndex, e.interFrameModes[:required])
			}
			e.lastInterZeroMVCount = countLastZeroMVInterFrameModes(e.interFrameModes[:required])
			e.lastInterSkipCount = countSkippedInterFrameModes(e.interFrameModes[:required])
			e.updateConsecutiveZeroLast(e.interFrameModes[:required])
			// libvpx vp8/encoder/onyx_if.c update_golden_frame_stats: track
			// per-frame ref usage so calc_gf_params and the auto_gold
			// refresh decision read the same `recent_ref_frame_usage`
			// libvpx would. On GF refresh the encoder resets the counters
			// to {1,1,1,1} via resetRecentRefFrameUsage; otherwise the
			// counts accumulate (skipping the immediate post-GF frame).
			intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes[:required])
			if attempt.Config.RefreshGolden {
				e.rc.resetRecentRefFrameUsage(required)
			} else {
				e.rc.updateRecentRefFrameUsage(intra, last, golden, alt)
			}
			if required > 0 {
				e.rc.thisFramePercentIntra = (100 * intra) / required
			}
			// libvpx vp8/encoder/onyx_if.c rolls last_frame_percent_intra
			// AFTER decide_key_frame consumes this_frame_percent_intra.
			// Keep that ordering here: lastFramePercentIntra captures the
			// just-encoded frame's value for the next frame's heuristic.
			e.lastFramePercentIntra = e.rc.thisFramePercentIntra
			e.temporal.finishFrame(temporalFrame, false, !invisible, temporalReferenceRefresh{
				Last:   attempt.Config.RefreshLast,
				Golden: attempt.Config.RefreshGolden,
				AltRef: attempt.Config.RefreshAltRef,
			}, encodedSizeBits(attempt.Size), e.temporalBufferConfig())
			e.populateTemporalLayerBufferResult(&result, temporalFrame)
			e.emitOracleFrameTrace(oracleTraceFrameSummary{
				FrameType:            vp8common.InterFrame,
				BaseQIndex:           int(attempt.Config.BaseQIndex),
				LoopFilter:           int(attempt.Config.LoopFilterLevel),
				SharpnessLevel:       int(attempt.Config.SharpnessLevel),
				RefLFDeltas:          attempt.Config.RefLFDeltas,
				ModeLFDeltas:         attempt.Config.ModeLFDeltas,
				ModeRefLFDeltaEnable: attempt.Config.LFDeltaEnabled,
				ModeRefLFDeltaUpdate: attempt.Config.LFDeltaUpdate,
				RefreshLast:          attempt.Config.RefreshLast,
				RefreshGolden:        attempt.Config.RefreshGolden,
				RefreshAltRef:        attempt.Config.RefreshAltRef,
				GoldenSignBias:       attempt.Config.GoldenSignBias,
				AltRefSignBias:       attempt.Config.AltRefSignBias,
				SegEnabled:           attempt.Config.Segmentation.Enabled,
				SizeBytes:            attempt.Size,
			})
			e.flushOracleMBTraceBuffer()
			// libvpx onyx_if.c end-of-encode: record ambient_err if the next
			// frame will be a forced KF so the forced-KF recode branch has a
			// baseline to compare against.
			e.updateNextKeyFrameForcedAfterCommit(source, rows, cols)
			if !hiddenAltRefFrame {
				e.frameCount++
			}
			finishSourceAltRef()
			return result, nil
		}
	}

	// libvpx vp8/encoder/onyx_if.c sets cpi->this_key_frame_forced when the
	// key frame is timing-driven (max-interval forced) rather than content-
	// driven. The recode loop reads it to engage the SS-error feedback Q
	// adjustment branch around line 4065.
	e.thisKeyFrameForced = forcedKeyFrame && !sceneCutKeyFrame && e.frameCount > 0
	keyAttempt, err := e.encodeKeyFrameWithQuantizerFeedback(dst, source, rows, cols, required, invisible, staticSegmentationAllowed)
	if err != nil {
		return EncodeResult{}, err
	}
	finalQuantizer := e.rc.currentQuantizer
	e.commitKeyFrameEntropy(keyAttempt)
	// Mirror libvpx onyx_if.c key-frame branch: clear source_alt_ref_active
	// and frames_since_golden so the next inter frame's RD ref-prob
	// heuristic starts from a clean slate.
	e.resetGoldenFrameStats()
	e.refreshKeyFrameReferencesFromAnalysis()
	// Seed denoiser running averages from the key-frame source (libvpx
	// onyx_if.c update_reference_frames key-frame branch).
	e.initDenoiserAvgFromKeyFrame(source)
	// Key frames consume any pending force_maxqp gate without applying it
	// (cyclic refresh is already keyframe-reset).
	e.forceMaxQuantizer = false
	e.loopFilterLevel = keyAttempt.LoopFilterLevel
	result.Data = dst[:keyAttempt.Size]
	result.SizeBytes = keyAttempt.Size
	result.Quantizer = libvpxQIndexToPublicQuantizer(finalQuantizer)
	e.emitOracleRateAndRecodeTrace(vp8common.KeyFrame, finalQuantizer, keyAttempt.Size, keyAttempt.ProjectedSizeBits)
	e.rc.postEncodeFrameWithPacketContext(keyAttempt.Size, rateControlPostEncodeContext{
		keyFrame:              true,
		macroblocks:           required,
		showFrame:             !invisible,
		skipPostPackOverspend: e.twoPass.enabled(),
	})
	if twoPassSceneCut {
		e.twoPass.markKeyFrame(e.frameCount)
	}
	e.twoPass.finishFrame(encodedSizeBits(keyAttempt.Size))
	e.rc.clampScreenContentBufferDebt(e.opts.ScreenContentMode)
	result.BufferLevelBits = e.rc.bufferLevelBits
	e.forceKeyFrame = false
	e.cyclicRefreshIndex = 0
	clearUint8Map(e.consecZeroLast)
	clearUint8Map(e.consecZeroLastMVBias)
	clearBoolMap(e.dotArtifactChecked)
	e.lastInterZeroMVCount = 0
	e.lastInterSkipCount = 0
	// libvpx vp8/encoder/onyx_if.c key-frame path resets the rolling
	// recent_ref_frame_usage counters to 1 each (the same as a GF
	// refresh) so the next GF section starts with a clean baseline.
	e.rc.resetRecentRefFrameUsage(required)
	if e.rc.framesTillGFUpdateDue > 0 {
		e.rc.currentGFInterval = e.rc.framesTillGFUpdateDue
		e.rc.framesTillGFUpdateDue--
	}
	e.rc.thisFramePercentIntra = 100
	// libvpx vp8/encoder/onyx_if.c sets last_frame_percent_intra=100
	// after every key frame, mirroring the encoder's expectation that
	// the next inter frame starts from an "all-intra" baseline.
	e.lastFramePercentIntra = 100
	e.resetInterRDThresholdMultipliers()
	e.interRDFrameActive = false
	e.temporal.finishFrame(temporalFrame, true, !invisible, temporalReferenceRefresh{Last: true, Golden: true, AltRef: true}, encodedSizeBits(keyAttempt.Size), e.temporalBufferConfig())
	e.populateTemporalLayerBufferResult(&result, temporalFrame)
	e.emitOracleFrameTrace(oracleTraceFrameSummary{
		FrameType:            vp8common.KeyFrame,
		BaseQIndex:           e.rc.currentQuantizer,
		LoopFilter:           int(keyAttempt.LoopFilterLevel),
		SharpnessLevel:       int(keyAttempt.SharpnessLevel),
		RefLFDeltas:          keyAttempt.RefLFDeltas,
		ModeLFDeltas:         keyAttempt.ModeLFDeltas,
		ModeRefLFDeltaEnable: keyAttempt.LFDeltaEnabled,
		ModeRefLFDeltaUpdate: keyAttempt.LFDeltaUpdate,
		RefreshLast:          true,
		RefreshGolden:        true,
		RefreshAltRef:        true,
		SegEnabled:           keyAttempt.SegmentationEnabled,
		SizeBytes:            keyAttempt.Size,
	})
	e.flushOracleMBTraceBuffer()
	// libvpx onyx_if.c, end-of-encode: clear this_key_frame_forced after the
	// frame has been committed; the next forced KF will set it again. Update
	// the next_key_frame_forced bookkeeping for the following frame's
	// ambient_err capture.
	e.thisKeyFrameForced = false
	e.updateNextKeyFrameForcedAfterCommit(source, rows, cols)
	e.frameCount++
	finishSourceAltRef()
	return result, nil
}

// updateNextKeyFrameForcedAfterCommit ports the libvpx
// vp8/encoder/onyx_if.c `if (cpi->next_key_frame_forced && frames_to_key == 0)`
// branch at the end of encode_frame_to_data_rate (around line 4282). When the
// just-encoded frame is the one *immediately before* a forced KF, the encoder
// stores the SS error of its reconstruction so the upcoming forced-KF recode
// loop can compare against it via forcedKeyFrameRecodeQuantizer.
func (e *VP8Encoder) updateNextKeyFrameForcedAfterCommit(source vp8enc.SourceImage, rows int, cols int) {
	interval := e.opts.KeyFrameInterval
	if interval <= 0 {
		return
	}
	// For govpx one-pass, the "next frame is a forced KF" predicate matches
	// libvpx's twopass.frames_to_key == 0 hand-off: with a fixed
	// KeyFrameInterval, frames at indices that are multiples of the interval
	// (after the bootstrap) are forced key frames. So the *current* frame's
	// frameCount being one less than such an index means we should capture
	// ambient_err now.
	nextIndex := e.frameCount + 1
	if nextIndex == 0 || nextIndex%uint64(interval) != 0 {
		return
	}
	e.ambientErr = calcKeyFrameSSError(source, &e.current.Img, rows, cols)
}

func (e *VP8Encoder) populateTemporalLayerBufferResult(result *EncodeResult, meta temporalFrame) {
	if result == nil || !meta.Enabled || meta.LayerID < 0 || meta.LayerID >= meta.LayerCount || meta.LayerID >= MaxTemporalLayers {
		return
	}
	accounting := e.temporal.accounting[meta.LayerID]
	result.TemporalLayerFrameBandwidthBits = accounting.FrameBandwidthBits
	result.TemporalLayerBufferLevelBits = accounting.BufferLevelBits
	result.TemporalLayerMaximumBufferBits = accounting.MaximumBufferBits
	result.TemporalLayerInputFrames = accounting.InputFrames
	result.TemporalLayerEncodedFrames = accounting.EncodedFrames
	result.TemporalLayerTotalEncodedFrames = accounting.TotalEncodedFrames
	result.TemporalLayerEncodedBits = accounting.EncodedBits
}

func (e *VP8Encoder) temporalBufferConfig() temporalBufferConfig {
	return temporalBufferConfig{
		timing:              e.timing,
		bufferInitialSizeMs: e.rc.bufferInitialSizeMs,
		bufferSizeMs:        e.rc.bufferSizeMs,
	}
}

func (e *VP8Encoder) encodeInterFrame(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags) (int, error) {
	attempt, err := e.encodeInterFrameAttempt(dst, source, rows, cols, required, flags, false, false, true, false)
	if err != nil {
		return 0, err
	}
	e.commitInterFrameAttempt(attempt)
	e.loopFilterLevel = attempt.Config.LoopFilterLevel
	if attempt.CyclicRefresh {
		e.commitCyclicRefresh(rows, cols, attempt.CyclicRefreshNextIndex, e.interFrameModes[:required])
	}
	return attempt.Size, nil
}

func (e *VP8Encoder) encodeKeyFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, invisible bool, staticSegmentationAllowed bool) (keyFrameEncodeAttempt, error) {
	recode := e.rc.newFrameSizeRecodeState(true, false)
	// libvpx vp8/encoder/onyx_if.c encode_frame_to_data_rate snapshots the
	// coding context once before entering the recode do-loop. Each rejected
	// attempt restores this snapshot so the next attempt re-encodes from the
	// same pre-attempt entropy/skip-prob state.
	e.saveCodingContext()
	e.oracleTraceRecodeLoopCount = 0
	e.oracleTraceRecodeReason = ""
	for attempt := 0; ; attempt++ {
		e.oracleTraceRecodeLoopCount++
		result, err := e.encodeKeyFrameAttempt(dst, source, rows, cols, required, invisible, staticSegmentationAllowed)
		if err != nil {
			return keyFrameEncodeAttempt{}, err
		}
		if attempt+1 >= encoderQuantizerFeedbackMaxAttempts {
			return result, nil
		}
		// libvpx forced-key-frame special-case branch
		// (encode_frame_to_data_rate around line 4065): when the encoder is
		// emitting a forced KF and the ambient_err baseline from the prior
		// frame is available, drive Q based on the SS-error gap rather than
		// the normal projected-size recode logic.
		if e.thisKeyFrameForced && e.ambientErr > 0 {
			kfErr := calcKeyFrameSSError(source, &e.analysis.Img, rows, cols)
			nextQ, recoded := e.rc.forcedKeyFrameRecodeQuantizer(kfErr, e.ambientErr, &recode)
			if !recoded {
				return result, nil
			}
			e.oracleTraceRecodeReason = "kf_forced_quality"
			e.rc.currentQuantizer = nextQ
			e.restoreCodingContext()
			continue
		}
		if !e.updateQuantizerForProjectedFrameSize(result.ProjectedSizeBits, true, false, required, &recode) {
			return result, nil
		}
		e.oracleTraceRecodeReason = "size_recode"
		// Recode accepted: restore the pre-loop snapshot before re-encoding.
		e.restoreCodingContext()
	}
}

func (e *VP8Encoder) encodeKeyFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, invisible bool, staticSegmentationAllowed bool) (keyFrameEncodeAttempt, error) {
	if len(e.keyFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return keyFrameEncodeAttempt{}, ErrInvalidConfig
	}
	quantDeltas := libvpxFrameQuantDeltas(e.rc.currentQuantizer, e.opts.ScreenContentMode)
	segmentation := vp8enc.SegmentationConfig{}
	if staticSegmentationAllowed {
		segmentation = e.cyclicRefreshSegmentationConfig(true)
	}
	var err error
	projectedRate := 0
	if segmentation.Enabled {
		assignKeyFrameStaticSegments(rows, cols, e.keyFrameModes[:required])
		projectedRate, err = e.buildReconstructingKeyFrameCoefficientsWithSegmentation(source, e.rc.currentQuantizer, segmentation, true, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	} else {
		projectedRate, err = e.buildReconstructingKeyFrameCoefficients(source, e.rc.currentQuantizer, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	}
	if err != nil {
		return keyFrameEncodeAttempt{}, translateEncoderError(err)
	}
	lfLevel, lfSharpness := e.encoderLoopFilter(vp8common.KeyFrame)
	lfLevel, err = e.pickLoopFilterLevel(source, vp8common.KeyFrame, lfLevel, lfSharpness, rows, cols, required)
	if err != nil {
		return keyFrameEncodeAttempt{}, err
	}
	lfHeader := e.encoderLoopFilterHeader(lfLevel, lfSharpness)
	if err := e.applyReconstructionLoopFilter(vp8common.KeyFrame, lfHeader, segmentation, rows, cols, required); err != nil {
		return keyFrameEncodeAttempt{}, err
	}
	if segmentation.Enabled {
		updateKeyFrameSegmentationTreeProbs(&segmentation, e.keyFrameModes[:required])
	}

	cfg := vp8enc.KeyFrameStateConfig{
		InvisibleFrame:      invisible,
		SimpleLoopFilter:    lfHeader.Type == vp8dec.SimpleLoopFilter,
		TokenPartition:      vp8common.TokenPartition(e.opts.TokenPartitions),
		BaseQIndex:          uint8(e.rc.currentQuantizer),
		QuantDeltas:         quantDeltas,
		LoopFilterLevel:     lfLevel,
		SharpnessLevel:      lfSharpness,
		LFDeltaEnabled:      lfHeader.DeltaEnabled,
		LFDeltaUpdate:       lfHeader.DeltaUpdate,
		RefLFDeltas:         lfHeader.RefDeltas,
		ModeLFDeltas:        lfHeader.ModeDeltas,
		Segmentation:        segmentation,
		RefreshEntropyProbs: true,
		IndependentContexts: e.opts.ErrorResilient,
	}
	n, frameCoefProbs, err := vp8enc.WriteCoefficientKeyFrameWithProbabilityBase(dst, e.opts.Width, e.opts.Height, cfg, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs)
	if err != nil {
		return keyFrameEncodeAttempt{}, translateEncoderError(err)
	}
	projectedBits := e.projectedFrameSizeBitsFromRate(true, required, projectedRate)
	return keyFrameEncodeAttempt{FrameCoefProbs: frameCoefProbs, Size: n, ProjectedSizeBits: projectedBits, LoopFilterLevel: lfLevel, SharpnessLevel: lfSharpness, LFDeltaEnabled: cfg.LFDeltaEnabled, LFDeltaUpdate: cfg.LFDeltaUpdate, RefLFDeltas: cfg.RefLFDeltas, ModeLFDeltas: cfg.ModeLFDeltas, RefreshEntropyProbs: cfg.RefreshEntropyProbs, SegmentationEnabled: segmentation.Enabled}, nil
}

func (e *VP8Encoder) encodeInterFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool, boostedReferenceFrame bool, staticSegmentationAllowed bool, sourceIsAltRef bool) (interFrameEncodeAttempt, error) {
	recode := e.rc.newFrameSizeRecodeState(false, boostedReferenceFrame)
	// libvpx vp8/encoder/onyx_if.c snapshots the coding context once before
	// the recode do-loop and restores it on every rejected attempt; mirror
	// that here so the inter recode loop has the same pre-attempt invariants
	// as libvpx.
	e.saveCodingContext()
	e.oracleTraceRecodeLoopCount = 0
	e.oracleTraceRecodeReason = ""
	// libvpx gates the inter recode loop on `cpi->sf.recode_loop`:
	// 0 -> no recode, 1 -> recode all, 2 -> recode key/golden/altref only.
	// At realtime (`compressor_speed == 2`) recode_loop is always 0, so a
	// non-boosted inter frame never recodes; at good-quality the threshold
	// rises with cpu-used. Without this gate the inter frame keeps cycling
	// through Q values driven by the picker's pre-pack rate estimate, which
	// is per-design coarse - the resulting Q drifts well below libvpx's at
	// constrained bitrates. See libvpx vp8/encoder/onyx_if.c
	// `recode_loop_test` and `set_speed_features` case 1/2/3.
	allowRecode := e.libvpxInterRecodeLoopActive(boostedReferenceFrame)
	for attempt := 0; ; attempt++ {
		e.oracleTraceRecodeLoopCount++
		result, err := e.encodeInterFrameAttempt(dst, source, rows, cols, required, flags, temporalActive, goldenCBRRefresh, staticSegmentationAllowed, sourceIsAltRef)
		if err != nil {
			return interFrameEncodeAttempt{}, err
		}
		if !allowRecode || attempt+1 >= encoderQuantizerFeedbackMaxAttempts || !e.updateQuantizerForProjectedFrameSize(result.ProjectedSizeBits, false, boostedReferenceFrame, required, &recode) {
			return result, nil
		}
		e.oracleTraceRecodeReason = "size_recode"
		// Recode accepted: restore the pre-loop snapshot before re-encoding.
		e.restoreCodingContext()
	}
}

// libvpxInterRecodeLoopActive returns true when libvpx's inter recode loop
// would run for this frame, mirroring `cpi->sf.recode_loop` in the encoder
// speed-feature table. Realtime always returns false (recode_loop = 0).
// Good-quality returns true when cpu-used <= 3 for plain inter frames, and
// when cpu-used <= 4 for boosted (golden/altref) frames. Best-quality
// returns true unconditionally.
func (e *VP8Encoder) libvpxInterRecodeLoopActive(boostedReferenceFrame bool) bool {
	if e == nil {
		return true
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return false
	case DeadlineGoodQuality:
		speed := e.libvpxCPUUsed()
		if boostedReferenceFrame {
			return speed <= 4
		}
		return speed <= 3
	default:
		return true
	}
}

func (e *VP8Encoder) encodeInterFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool, staticSegmentationAllowed bool, sourceIsAltRef bool) (interFrameEncodeAttempt, error) {
	cfg := vp8enc.DefaultInterFrameStateConfig(uint8(e.rc.currentQuantizer))
	cfg.InvisibleFrame = flags&EncodeInvisibleFrame != 0
	cfg.TokenPartition = vp8common.TokenPartition(e.opts.TokenPartitions)
	cfg.QuantDeltas = libvpxFrameQuantDeltas(e.rc.currentQuantizer, e.opts.ScreenContentMode)
	cfg.LoopFilterLevel, cfg.SharpnessLevel = e.encoderLoopFilter(vp8common.InterFrame)
	cfg.SimpleLoopFilter = e.encoderUsesSimpleLoopFilter()
	cfg.RefreshEntropyProbs = flags&EncodeNoUpdateEntropy == 0 && !e.opts.ErrorResilient
	cfg.IndependentContexts = e.opts.ErrorResilient
	cfg.RefreshLast = flags&EncodeNoUpdateLast == 0
	// Match libvpx's normal interframe shape: LAST advances by default while
	// golden/altref remain long-lived references unless a future policy updates them.
	cfg.RefreshGolden = false
	cfg.RefreshAltRef = false
	if temporalActive {
		cfg.RefreshGolden = flags&EncodeNoUpdateGolden == 0
		cfg.RefreshAltRef = flags&EncodeNoUpdateAltRef == 0
	} else if goldenCBRRefresh || flags&EncodeForceGoldenFrame != 0 {
		cfg.RefreshGolden = true
	}
	if flags&EncodeForceAltRefFrame != 0 {
		cfg.RefreshAltRef = true
	}
	signBias := e.interFrameSignBias()
	cfg.GoldenSignBias = signBias[vp8common.GoldenFrame]
	cfg.AltRefSignBias = signBias[vp8common.AltRefFrame]
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(e.opts.ErrorResilient, goldenCBRRefresh, flags) {
		cfg.CopyBufferToAltRef = 2
	}
	// Enforce libvpx onyx_if.c update_reference_frames ARF invariants
	// before validation: assert(!cm->copy_buffer_to_arf) on hidden ARF
	// frames and clear both copy fields on the deferred show frame.
	suppressInterFrameCopyBuffersOnAltRefEdges(&cfg, e.isSrcFrameAltRef(e.currentSourcePTS))
	cfg.ProbSkipFalse = e.interFrameAnalysisSkipFalseProb(e.rc.currentQuantizer, cfg.RefreshGolden, cfg.RefreshAltRef, sourceIsAltRef)
	previousProbSkipFalse := e.probSkipFalse
	e.probSkipFalse = cfg.ProbSkipFalse
	defer func() {
		e.probSkipFalse = previousProbSkipFalse
	}()
	segmentation := vp8enc.SegmentationConfig{}
	if staticSegmentationAllowed {
		segmentation = e.cyclicRefreshSegmentationConfig(cfg.RefreshGolden)
	}
	if segmentation.Enabled {
		cfg.Segmentation = segmentation
	}
	if cfg.LoopFilterLevel == 0 && !segmentation.Enabled {
		refFrame, ref, ok := e.matchingZeroInterFrameReference(source, flags)
		if ok {
			if len(e.interFrameModes) < required {
				return interFrameEncodeAttempt{}, ErrInvalidConfig
			}
			fillZeroInterFrameModes(e.interFrameModes[:required], refFrame)
			cfg.ProbSkipFalse = interFrameModeSkipFalseProbability(rows, cols, e.interFrameModes[:required], cfg.ProbSkipFalse)
			n, err := vp8enc.WriteZeroReferenceInterFrame(dst, e.opts.Width, e.opts.Height, cfg, refFrame)
			if err != nil {
				return interFrameEncodeAttempt{}, translateEncoderError(err)
			}
			return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: e.coefProbs, FrameYModeProbs: e.modeProbs.YMode, FrameUVModeProbs: e.modeProbs.UVMode, FrameMVProbs: e.modeProbs.MV, RefFrame: refFrame, Ref: ref, Size: n, ProjectedSizeBits: encodedSizeBits(n), ZeroReference: true}, nil
		}
	}
	if len(e.interFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return interFrameEncodeAttempt{}, ErrInvalidConfig
	}
	// Mirror libvpx update_rd_ref_frame_probs: bias the previous-frame
	// reference-frame probabilities for *this* frame's RD scoring based on
	// the upcoming refresh policy. The base values are restored on every
	// return so commitInterFrameAttempt's updateRefFrameProbsFromAttempt
	// recomputes them from this frame's mb_ref_frame counts (the equivalent
	// of vp8_convert_rfct_to_prob at packet write time).
	previousRefProbIntra := e.refProbIntra
	previousRefProbLast := e.refProbLast
	previousRefProbGolden := e.refProbGolden
	if !e.opts.TemporalScalability.Enabled {
		e.applyRdRefFrameProbHeuristics(cfg.RefreshAltRef)
	}
	defer func() {
		e.refProbIntra = previousRefProbIntra
		e.refProbLast = previousRefProbLast
		e.refProbGolden = previousRefProbGolden
	}()
	var err error
	projectedRate := 0
	cyclicRefreshNextIndex := e.cyclicRefreshIndex
	if segmentation.Enabled {
		cyclicRefreshNextIndex = e.assignInterFrameStaticSegments(source, rows, cols, e.interFrameModes[:required])
		projectedRate, err = e.buildReconstructingInterFrameCoefficientsWithSegmentation(source, e.rc.currentQuantizer, segmentation, true, e.interFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols, flags)
	} else {
		projectedRate, err = e.buildReconstructingInterFrameCoefficients(source, e.rc.currentQuantizer, e.interFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols, flags)
	}
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	// libvpx denoiser runs per-MB after mode decision and reconstruction.
	// Output goes to denoiser.runningAvg[INTRA] which propagates to
	// reference-aligned buffers in commitInterFrameAttempt.
	e.applyDenoiserToInterFrame(source, rows, cols)
	cfg.LoopFilterLevel, err = e.pickLoopFilterLevel(source, vp8common.InterFrame, cfg.LoopFilterLevel, cfg.SharpnessLevel, rows, cols, required)
	if err != nil {
		return interFrameEncodeAttempt{}, err
	}
	lfHeader := e.encoderLoopFilterHeader(cfg.LoopFilterLevel, cfg.SharpnessLevel)
	cfg.SimpleLoopFilter = lfHeader.Type == vp8dec.SimpleLoopFilter
	cfg.LFDeltaEnabled = lfHeader.DeltaEnabled
	cfg.LFDeltaUpdate = lfHeader.DeltaUpdate
	cfg.RefLFDeltas = lfHeader.RefDeltas
	cfg.ModeLFDeltas = lfHeader.ModeDeltas
	if err := e.applyReconstructionLoopFilter(vp8common.InterFrame, lfHeader, segmentation, rows, cols, required); err != nil {
		return interFrameEncodeAttempt{}, err
	}
	if segmentation.Enabled {
		updateInterFrameSegmentationTreeProbs(&segmentation, e.interFrameModes[:required])
		cfg.Segmentation = segmentation
	}
	cfg.ProbSkipFalse = interFrameModeSkipFalseProbability(rows, cols, e.interFrameModes[:required], cfg.ProbSkipFalse)
	n, frameCoefProbs, frameYModeProbs, frameUVModeProbs, frameMVProbs, err := vp8enc.WriteCoefficientInterFrameWithProbabilityBase(dst, e.opts.Width, e.opts.Height, cfg, e.interFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols], &e.coefProbs, e.modeProbs.YMode, e.modeProbs.UVMode, e.modeProbs.MV)
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	projectedBits := e.projectedFrameSizeBitsFromRate(false, required, projectedRate)
	return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: frameCoefProbs, FrameYModeProbs: frameYModeProbs, FrameUVModeProbs: frameUVModeProbs, FrameMVProbs: frameMVProbs, Size: n, ProjectedSizeBits: projectedBits, CyclicRefresh: segmentation.Enabled, CyclicRefreshNextIndex: cyclicRefreshNextIndex}, nil
}

func (e *VP8Encoder) updateQuantizerForProjectedFrameSize(projectedBits int, keyFrame bool, goldenFrame bool, macroblocks int, recode *frameSizeRecodeState) bool {
	next, ok := e.rc.frameSizeRecodeQuantizerWithContextBits(projectedBits, keyFrame, goldenFrame, macroblocks, recode)
	if !ok {
		return false
	}
	if next == e.rc.currentQuantizer {
		e.rc.currentZbinOverQuant = recode.zbinOverQuant
		return false
	}
	e.rc.currentQuantizer = next
	e.rc.currentZbinOverQuant = recode.zbinOverQuant
	return true
}

func (e *VP8Encoder) projectedFrameSizeBitsFromRate(keyFrame bool, macroblocks int, projectedRate int) int {
	if projectedRate <= 0 {
		return 0
	}
	projectedBits := projectedRate >> 8
	projectedBits -= e.estimatedEntropySavingsBits(keyFrame, macroblocks)
	if projectedBits < 0 {
		return 0
	}
	return projectedBits
}

func (e *VP8Encoder) estimatedEntropySavingsBits(keyFrame bool, macroblocks int) int {
	savings := e.coefficientEntropySavingsBits(keyFrame, macroblocks)
	if keyFrame || macroblocks <= 0 || len(e.interFrameModes) < macroblocks {
		return savings
	}
	intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes[:macroblocks])
	return savings + libvpxRefFrameEntropySavings(false, intra, last, golden, alt, int(e.refProbIntra), int(e.refProbLast), int(e.refProbGolden))
}

func (e *VP8Encoder) coefficientEntropySavingsBits(keyFrame bool, macroblocks int) int {
	if e == nil || macroblocks <= 0 {
		return 0
	}
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	if rows <= 0 || cols <= 0 || rows*cols != macroblocks || len(e.tokenAbove) < cols {
		return 0
	}
	if keyFrame {
		if len(e.keyFrameModes) < macroblocks || len(e.keyFrameCoeffs) < macroblocks {
			return 0
		}
		if e.opts.ErrorResilient {
			savings, err := vp8enc.KeyFrameCoefficientEntropySavingsIndependent(rows, cols, e.keyFrameModes[:macroblocks], e.keyFrameCoeffs[:macroblocks], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs)
			if err != nil {
				return 0
			}
			return savings
		}
		savings, err := vp8enc.KeyFrameCoefficientEntropySavings(rows, cols, e.keyFrameModes[:macroblocks], e.keyFrameCoeffs[:macroblocks], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs)
		if err != nil {
			return 0
		}
		return savings
	}
	if len(e.interFrameModes) < macroblocks || len(e.keyFrameCoeffs) < macroblocks {
		return 0
	}
	if e.opts.ErrorResilient {
		savings, err := vp8enc.InterCoefficientEntropySavingsIndependent(rows, cols, e.interFrameModes[:macroblocks], e.keyFrameCoeffs[:macroblocks], e.tokenAbove[:cols], &e.coefProbs)
		if err != nil {
			return 0
		}
		return savings
	}
	savings, err := vp8enc.InterCoefficientEntropySavings(rows, cols, e.interFrameModes[:macroblocks], e.keyFrameCoeffs[:macroblocks], e.tokenAbove[:cols], &e.coefProbs)
	if err != nil {
		return 0
	}
	return savings
}

func (e *VP8Encoder) commitKeyFrameEntropy(attempt keyFrameEncodeAttempt) {
	e.coefProbs = vp8tables.DefaultCoefProbs
	vp8dec.ResetModeProbs(&e.modeProbs)
	if attempt.RefreshEntropyProbs {
		e.coefProbs = attempt.FrameCoefProbs
	}
}

func (e *VP8Encoder) commitInterFrameAttempt(attempt interFrameEncodeAttempt) {
	e.commitInterFrameEntropy(attempt)
	e.commitInterFrameSkipFalseProb(attempt)
	e.updateRefFrameProbsFromAttempt(attempt)
	// Track libvpx update_golden_frame_stats / update_alt_ref_frame_stats
	// counters used by applyRdRefFrameProbHeuristics next frame.
	e.updateGoldenFrameStats(attempt.Config.RefreshGolden, attempt.Config.RefreshAltRef)
	if attempt.ZeroReference {
		e.refreshZeroInterFrameReferences(attempt.Config, attempt.Ref, attempt.RefFrame)
	} else {
		e.refreshInterFrameReferencesFromAnalysis(attempt.Config)
	}
	// Mirror libvpx onyx_if.c update_reference_frames denoiser branch: copy
	// the denoised running_avg[INTRA] into LAST/GOLDEN/ALTREF running_avg
	// buffers per the frame's refresh policy.
	e.copyDenoiserAvgForRefresh(attempt.Config.RefreshLast, attempt.Config.RefreshGolden, attempt.Config.RefreshAltRef)
	e.rememberLastFrameInterModes(interFrameStateConfigSignBias(attempt.Config))
	// Once an inter frame has been encoded under the post-drop max-Q gate,
	// clear it; libvpx leaves force_maxqp set only until the next frame
	// consumes it.
	e.forceMaxQuantizer = false
}

// updateRefFrameProbsFromAttempt mirrors libvpx vp8_convert_rfct_to_prob:
// ref-frame probs for the next frame's RD scoring are derived from observed
// mode counts after normal single-layer inter frames, and after all
// multi-layer inter frames. Single-layer GF/ARF refresh frames deliberately
// keep the previous probabilities and let update_rd_ref_frame_probs apply the
// refresh heuristics on the next frame.
func (e *VP8Encoder) updateRefFrameProbsFromAttempt(attempt interFrameEncodeAttempt) {
	if !libvpxShouldConvertRefCountsToProb(e.libvpxTemporalLayerCount(), attempt.Config.RefreshGolden, attempt.Config.RefreshAltRef) {
		return
	}
	var rfct [4]int
	for i := range e.interFrameModes {
		switch e.interFrameModes[i].RefFrame {
		case vp8common.IntraFrame:
			rfct[0]++
		case vp8common.LastFrame:
			rfct[1]++
		case vp8common.GoldenFrame:
			rfct[2]++
		case vp8common.AltRefFrame:
			rfct[3]++
		}
	}
	rfIntra := rfct[0]
	rfInter := rfct[1] + rfct[2] + rfct[3]
	if rfIntra+rfInter == 0 {
		return
	}
	newIntra := rfIntra * 255 / (rfIntra + rfInter)
	if newIntra == 0 {
		newIntra = 1
	}
	newLast := 128
	if rfInter > 0 {
		newLast = rfct[1] * 255 / rfInter
		if newLast == 0 {
			newLast = 1
		}
	}
	newGarf := 128
	if rfct[2]+rfct[3] > 0 {
		newGarf = rfct[2] * 255 / (rfct[2] + rfct[3])
		if newGarf == 0 {
			newGarf = 1
		}
	}
	e.refProbIntra = uint8(newIntra)
	e.refProbLast = uint8(newLast)
	e.refProbGolden = uint8(newGarf)
}

func libvpxShouldConvertRefCountsToProb(temporalLayerCount int, refreshGolden bool, refreshAltRef bool) bool {
	return temporalLayerCount > 1 || (!refreshGolden && !refreshAltRef)
}

func (e *VP8Encoder) commitInterFrameEntropy(attempt interFrameEncodeAttempt) {
	if !attempt.Config.RefreshEntropyProbs {
		return
	}
	e.coefProbs = attempt.FrameCoefProbs
	e.modeProbs.YMode = attempt.FrameYModeProbs
	e.modeProbs.UVMode = attempt.FrameUVModeProbs
	e.modeProbs.MV = attempt.FrameMVProbs
}

// applyRdRefFrameProbHeuristics ports the heuristic adjustments in libvpx
// vp8/encoder/onyx_if.c update_rd_ref_frame_probs that bias prob_intra/
// prob_last/prob_gf for the *current* inter frame's RD scoring. The base
// probabilities themselves are kept fresh by updateRefFrameProbsFromAttempt
// (the equivalent of vp8_convert_rfct_to_prob) at packet write time, so this
// function only stamps the per-frame heuristic adjustments on top.
//
// In libvpx these bumps are gated by `oxcf.number_of_layers == 1`; govpx's
// temporal-scalability path runs through interReferenceFrameRatesForFlags
// special cases instead, so the layer guard is enforced by the call site.
func (e *VP8Encoder) applyRdRefFrameProbHeuristics(refreshAltRef bool) {
	if refreshAltRef {
		probIntra := int(e.refProbIntra) + 40
		if probIntra > 255 {
			probIntra = 255
		}
		e.refProbIntra = uint8(probIntra)
		e.refProbLast = 200
		e.refProbGolden = 1
	} else if e.framesSinceGolden == 0 {
		e.refProbLast = 214
	} else if e.framesSinceGolden == 1 {
		e.refProbLast = 192
		e.refProbGolden = 220
	} else if e.sourceAltRefActive {
		probGolden := int(e.refProbGolden) - 20
		if probGolden < 10 {
			probGolden = 10
		}
		e.refProbGolden = uint8(probGolden)
	}
	if !e.sourceAltRefActive {
		e.refProbGolden = 255
	}
}

// updateGoldenFrameStats tracks libvpx's frames_since_golden /
// source_alt_ref_active counters used by update_rd_ref_frame_probs. It is the
// govpx counterpart to vp8/encoder/onyx_if.c update_golden_frame_stats minus
// the auto-arf bookkeeping that govpx's flag-driven alt-ref does not exercise.
func (e *VP8Encoder) updateGoldenFrameStats(refreshGolden bool, refreshAltRef bool) {
	if refreshAltRef {
		e.framesSinceGolden = 0
		e.sourceAltRefActive = true
		// libvpx vp8/encoder/onyx_if.c update_alt_ref_frame_stats clears
		// source_alt_ref_pending after the hidden ARF is encoded.
		e.sourceAltRefPending = false
		return
	}
	if refreshGolden {
		e.framesSinceGolden = 0
		// libvpx onyx_if.c: `if (!source_alt_ref_pending)
		// source_alt_ref_active = 0`. Refreshing golden in the absence of
		// a pending alt-ref schedule clears the active alt-ref.
		if !e.sourceAltRefPending {
			e.sourceAltRefActive = false
		}
		return
	}
	if e.framesSinceGolden < int(^uint(0)>>1) {
		e.framesSinceGolden++
	}
	// libvpx onyx_if.c counts down frames_till_alt_ref_frame on every
	// non-refresh inter frame; when it hits 0 the encoder consumes the
	// pending ARF on the next frame.
	if e.framesTillAltRefFrame > 0 {
		e.framesTillAltRefFrame--
	}
}

// resetGoldenFrameStats mirrors libvpx's key-frame branch in onyx_if.c, which
// clears source_alt_ref_active and resets frames_since_golden so the next
// inter frame's RD scoring starts from a clean slate. libvpx also clears
// source_alt_ref_pending and resets frames_till_alt_ref_frame on key frame
// reset since the prior ARF schedule is invalidated.
func (e *VP8Encoder) resetGoldenFrameStats() {
	e.framesSinceGolden = 0
	e.sourceAltRefActive = false
	e.sourceAltRefPending = false
	e.altRefSourceValid = false
	e.framesTillAltRefFrame = 0
}

// scheduleAltRefSource ports the libvpx
// vp8/encoder/onyx_if.c automatic ARF scheduling decision: when an ARF
// is pending and the lookahead has the future source available, the
// encoder marks the lookahead entry as the alt_ref_source and arms the
// hidden-frame insertion. This helper just records the schedule; the
// actual hidden-frame encode path is a follow-up.
func (e *VP8Encoder) scheduleAltRefSource(altRefSourcePTS uint64, framesTillUpdate int) {
	e.sourceAltRefPending = true
	e.altRefSourcePTS = altRefSourcePTS
	e.altRefSourceValid = true
	e.framesTillAltRefFrame = framesTillUpdate
}

// isSrcFrameAltRef ports the libvpx is_src_frame_alt_ref check:
// after popping a lookahead entry, the encoder marks it as the ARF
// source frame if its PTS matches the previously scheduled
// alt_ref_source. The check is gated on altRefSourceValid because
// scheduleAltRefSource has not yet been called for the first ARF
// section (libvpx's `cpi->alt_ref_source != NULL` guard).
func (e *VP8Encoder) isSrcFrameAltRef(framePTS uint64) bool {
	return e.altRefSourceValid && framePTS == e.altRefSourcePTS
}

func (e *VP8Encoder) interFrameSignBias() [vp8common.MaxRefFrames]bool {
	if e == nil {
		return [vp8common.MaxRefFrames]bool{}
	}
	signBias := [vp8common.MaxRefFrames]bool{}
	signBias[vp8common.AltRefFrame] = e.sourceAltRefActive
	return signBias
}

func interFrameStateConfigSignBias(cfg vp8enc.InterFrameStateConfig) [vp8common.MaxRefFrames]bool {
	return [vp8common.MaxRefFrames]bool{
		vp8common.GoldenFrame: cfg.GoldenSignBias,
		vp8common.AltRefFrame: cfg.AltRefSignBias,
	}
}

func interFrameDroppable(cfg vp8enc.InterFrameStateConfig) bool {
	if cfg.RefreshLast || cfg.RefreshGolden || cfg.RefreshAltRef ||
		cfg.CopyBufferToGolden != 0 || cfg.CopyBufferToAltRef != 0 ||
		cfg.RefreshEntropyProbs {
		return false
	}
	if cfg.Segmentation.Enabled && (cfg.Segmentation.UpdateMap || cfg.Segmentation.UpdateData) {
		return false
	}
	return true
}

func (e *VP8Encoder) matchingZeroInterFrameReference(source vp8enc.SourceImage, flags EncodeFlags) (vp8common.MVReferenceFrame, *vp8common.Image, bool) {
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	if lastEnabled && sourceImageMatchesReference(source, &e.lastRef.Img) {
		return vp8common.LastFrame, &e.lastRef.Img, true
	}
	if goldenEnabled && sourceImageMatchesReference(source, &e.goldenRef.Img) {
		return vp8common.GoldenFrame, &e.goldenRef.Img, true
	}
	if altEnabled && sourceImageMatchesReference(source, &e.altRef.Img) {
		return vp8common.AltRefFrame, &e.altRef.Img, true
	}
	return vp8common.IntraFrame, nil, false
}

func fillZeroInterFrameModes(modes []vp8enc.InterFrameMacroblockMode, refFrame vp8common.MVReferenceFrame) {
	for i := range modes {
		modes[i] = vp8enc.InterFrameMacroblockMode{
			MBSkipCoeff: true,
			RefFrame:    refFrame,
			Mode:        vp8common.ZeroMV,
		}
	}
}

func countLastZeroMVInterFrameModes(modes []vp8enc.InterFrameMacroblockMode) int {
	count := 0
	for _, mode := range modes {
		if mode.RefFrame == vp8common.LastFrame && mode.Mode == vp8common.ZeroMV {
			count++
		}
	}
	return count
}

func countSkippedInterFrameModes(modes []vp8enc.InterFrameMacroblockMode) int {
	count := 0
	for _, mode := range modes {
		if mode.MBSkipCoeff {
			count++
		}
	}
	return count
}

// countInterFrameRefUsage mirrors libvpx's count_mb_ref_frame_usage: a
// per-MB tally of which reference each MB selected (intra, last, golden,
// altref). The four return values match the libvpx INTRA/LAST/GOLDEN/ALTREF
// indexing of cpi->mb.count_mb_ref_frame_usage. Modes with intra
// prediction (Mode < NearestMV, i.e. DCPred/VPred/HPred/TMPred/BPred)
// count as INTRA_FRAME in libvpx; otherwise the MB carries an inter ref.
func countInterFrameRefUsage(modes []vp8enc.InterFrameMacroblockMode) (intra, last, golden, alt int) {
	for _, mode := range modes {
		if mode.Mode < vp8common.NearestMV {
			intra++
			continue
		}
		switch mode.RefFrame {
		case vp8common.LastFrame:
			last++
		case vp8common.GoldenFrame:
			golden++
		case vp8common.AltRefFrame:
			alt++
		default:
			intra++
		}
	}
	return intra, last, golden, alt
}

func (e *VP8Encoder) shouldRecodeInterAttemptAsKeyFrame(required int, refreshGoldenFrame bool, temporalEnabled bool, invisible bool) (int, bool) {
	if e == nil ||
		!e.opts.AdaptiveKeyFrames ||
		e.twoPass.enabled() ||
		temporalEnabled ||
		invisible ||
		e.interAnalysisCompressorSpeed() == 2 ||
		required <= 0 ||
		len(e.interFrameModes) < required {
		return 0, false
	}
	intra, _, _, _ := countInterFrameRefUsage(e.interFrameModes[:required])
	thisFramePercentIntra := (100 * intra) / required
	return thisFramePercentIntra, libvpxDecideKeyFrame(thisFramePercentIntra, e.lastFramePercentIntra, refreshGoldenFrame)
}

func validateEncodeFlags(flags EncodeFlags) error {
	if flags&EncodeForceGoldenFrame != 0 && flags&EncodeNoUpdateGolden != 0 {
		return ErrInvalidConfig
	}
	if flags&EncodeForceAltRefFrame != 0 && flags&EncodeNoUpdateAltRef != 0 {
		return ErrInvalidConfig
	}
	return nil
}

func boostedReferenceRateControlFrame(goldenCBRRefresh bool, flags EncodeFlags) bool {
	return goldenCBRRefresh || flags&(EncodeForceGoldenFrame|EncodeForceAltRefFrame) != 0
}

func shouldCopyOldGoldenToAltRefOnGoldenRefresh(errorResilient bool, goldenCBRRefresh bool, flags EncodeFlags) bool {
	if errorResilient || !goldenCBRRefresh {
		return false
	}
	return flags&(EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef|EncodeForceGoldenFrame|EncodeForceAltRefFrame) == 0
}

// suppressInterFrameCopyBuffersOnAltRefEdges enforces libvpx
// onyx_if.c update_reference_frames invariants for the ARF edge cases:
// hidden ARF frames assert `!cm->copy_buffer_to_arf` (the ARF buffer
// is populated by the frame itself), and the deferred show frame
// after a hidden ARF (is_src_frame_alt_ref) leaves both
// copy_buffer_to_arf and copy_buffer_to_gf at their zero default
// because the references are already correctly populated.
func suppressInterFrameCopyBuffersOnAltRefEdges(cfg *vp8enc.InterFrameStateConfig, isSrcFrameAltRef bool) {
	if cfg == nil {
		return
	}
	if cfg.RefreshAltRef {
		cfg.CopyBufferToAltRef = 0
	}
	if isSrcFrameAltRef {
		cfg.CopyBufferToAltRef = 0
		cfg.CopyBufferToGolden = 0
	}
}

func (e *VP8Encoder) anyInterReferenceAvailable(flags EncodeFlags) bool {
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	return lastEnabled || goldenEnabled || altEnabled
}

func (e *VP8Encoder) interReferenceAvailability(flags EncodeFlags) (last bool, golden bool, alt bool) {
	last = flags&EncodeNoReferenceLast == 0
	golden = flags&EncodeNoReferenceGolden == 0
	alt = flags&EncodeNoReferenceAltRef == 0
	if e == nil {
		return last, golden, alt
	}
	if e.goldenRefAliasesLast {
		golden = false
	}
	if e.altRefAliasesLast || e.goldenRefAliasesAlt {
		alt = false
	}
	return last, golden, alt
}

func (e *VP8Encoder) shouldEncodeKeyFrame(flags EncodeFlags) bool {
	if e.frameCount == 0 || e.forceKeyFrame || flags&EncodeForceKeyFrame != 0 {
		return true
	}
	if !e.anyInterReferenceAvailable(flags) {
		return true
	}
	if e.opts.KeyFrameInterval > 0 && e.frameCount%uint64(e.opts.KeyFrameInterval) == 0 {
		return true
	}
	return false
}

func (e *VP8Encoder) forceKeyFrameRequested(flags EncodeFlags) bool {
	if e.forceKeyFrame || flags&EncodeForceKeyFrame != 0 {
		return true
	}
	return !e.anyInterReferenceAvailable(flags)
}

func (e *VP8Encoder) shouldRefreshGoldenFrameCBR(keyFrame bool, temporalActive bool, flags EncodeFlags, rows int, cols int) bool {
	if keyFrame ||
		temporalActive ||
		e.opts.ErrorResilient ||
		e.rc.mode != RateControlCBR ||
		flags&(EncodeInvisibleFrame|EncodeNoUpdateGolden) != 0 {
		return false
	}
	if required := rows * cols; required <= 0 || e.lastInterZeroMVCount <= required/2 {
		return false
	}
	interval := e.goldenFrameCBRInterval(rows, cols)
	return interval > 0 && e.rc.framesSinceKeyframe > 0 && e.rc.framesSinceKeyframe%interval == 0
}

// shouldRefreshGoldenFrameOnePassNonCBR ports the libvpx auto_gold
// one-pass non-CBR GF refresh trigger from
// vp8/encoder/ratectrl.c calc_pframe_target_size:
//
//	if (cpi->oxcf.error_resilient_mode == 0 &&
//	    (cpi->frames_till_gf_update_due == 0) && !cpi->drop_frame) {
//	    if (!cpi->gf_update_onepass_cbr) {
//	        ... compute gf_frame_usage ...
//	        if (cpi->auto_gold) {
//	            if ((cpi->pass == 0) &&
//	                (cpi->this_frame_percent_intra < 15 ||
//	                 gf_frame_usage >= 5)) {
//	                cpi->common.refresh_golden_frame = 1;
//	            }
//	        }
//	    }
//	}
//
// govpx routes CBR through `shouldRefreshGoldenFrameCBR`; this method
// covers VBR and CQ. Returns true when libvpx would force a GF
// refresh on this frame.
func (e *VP8Encoder) shouldRefreshGoldenFrameOnePassNonCBR(keyFrame bool, temporalActive bool, flags EncodeFlags, rows int, cols int) bool {
	if keyFrame ||
		temporalActive ||
		e.opts.ErrorResilient ||
		e.rc.mode == RateControlCBR ||
		flags&(EncodeInvisibleFrame|EncodeNoUpdateGolden) != 0 {
		return false
	}
	if e.rc.framesTillGFUpdateDue > 0 {
		return false
	}
	required := rows * cols
	if required <= 0 {
		return false
	}
	return libvpxAutoGoldOnePassRefreshDecision(
		e.rc.thisFramePercentIntra,
		e.rc.recentRefFrameUsageIntra,
		e.rc.recentRefFrameUsageLast,
		e.rc.recentRefFrameUsageGolden,
		e.rc.recentRefFrameUsageAltRef,
		e.rc.gfActiveCount,
		required,
	)
}

func (e *VP8Encoder) goldenFrameCBRInterval(rows int, cols int) int {
	interval := 10
	refreshCount := cyclicRefreshMaxMBsPerFrame(rows, cols)
	if refreshCount > 0 {
		interval = (2 * rows * cols) / refreshCount
	}
	if interval < 6 {
		return 6
	}
	if interval > 40 {
		return 40
	}
	return interval
}

func (e *VP8Encoder) SetBitrateKbps(kbps int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextRC := e.rc
	if err := nextRC.setBitrateKbps(kbps, e.timing); err != nil {
		return err
	}
	nextTemporal := e.temporal
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

func (e *VP8Encoder) SetRateControl(cfg RateControlConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextRC := e.rc
	if err := nextRC.applyConfig(cfg, e.timing); err != nil {
		return err
	}
	nextTemporal := e.temporal
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.opts.RateControlMode = cfg.Mode
	e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
	e.opts.MinBitrateKbps = cfg.MinBitrateKbps
	e.opts.MaxBitrateKbps = cfg.MaxBitrateKbps
	e.opts.MinQuantizer = cfg.MinQuantizer
	e.opts.MaxQuantizer = cfg.MaxQuantizer
	e.opts.CQLevel = normalizedCQLevel(cfg.CQLevel, cfg.MinQuantizer)
	e.opts.UndershootPct = cfg.UndershootPct
	e.opts.OvershootPct = cfg.OvershootPct
	e.opts.BufferSizeMs = cfg.BufferSizeMs
	e.opts.BufferInitialSizeMs = cfg.BufferInitialSizeMs
	e.opts.BufferOptimalSizeMs = cfg.BufferOptimalSizeMs
	e.opts.DropFrameAllowed = cfg.DropFrameAllowed
	e.opts.MaxIntraBitratePct = cfg.MaxIntraBitratePct
	e.opts.GFCBRBoostPct = cfg.GFCBRBoostPct
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

func (e *VP8Encoder) SetCQLevel(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if level < 0 || level > maxQuantizer {
		return ErrInvalidQuantizer
	}
	if e.rc.mode == RateControlCQ && (level < e.opts.MinQuantizer || level > e.opts.MaxQuantizer) {
		return ErrInvalidQuantizer
	}
	qIndex := libvpxPublicQuantizerToQIndex(level)
	e.rc.cqLevel = qIndex
	e.opts.CQLevel = level
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = qIndex
		e.rc.lastQuantizer = qIndex
		e.rc.lastInterQuantizer = qIndex
	}
	return nil
}

func (e *VP8Encoder) SetMaxIntraBitratePct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.rc.maxIntraBitratePct = pct
	e.opts.MaxIntraBitratePct = pct
	return nil
}

func (e *VP8Encoder) SetGFCBRBoostPct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.rc.gfCBRBoostPct = pct
	e.opts.GFCBRBoostPct = pct
	return nil
}

func (e *VP8Encoder) SetTokenPartitions(partitions int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if partitions < int(vp8common.OnePartition) || partitions > int(vp8common.EightPartition) {
		return ErrInvalidConfig
	}
	e.opts.TokenPartitions = partitions
	return nil
}

func (e *VP8Encoder) SetSharpness(sharpness int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if sharpness < 0 || sharpness > 7 {
		return ErrInvalidConfig
	}
	e.opts.Sharpness = sharpness
	return nil
}

func (e *VP8Encoder) SetStaticThreshold(threshold int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if threshold < 0 {
		return ErrInvalidConfig
	}
	e.opts.StaticThreshold = threshold
	return nil
}

func (e *VP8Encoder) SetScreenContentMode(mode int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if mode < 0 || mode > 2 {
		return ErrInvalidConfig
	}
	e.opts.ScreenContentMode = mode
	return nil
}

func (e *VP8Encoder) SetRealtimeTarget(target RealtimeTarget) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if target.BitrateKbps < 0 || target.FPS < 0 || target.Width < 0 || target.Height < 0 {
		return ErrInvalidConfig
	}
	if target.MinQuantizer < 0 || target.MaxQuantizer < 0 || target.MinQuantizer > maxQuantizer || target.MaxQuantizer > maxQuantizer {
		return ErrInvalidQuantizer
	}
	if target.MinQuantizer > 0 && target.MaxQuantizer > 0 && target.MinQuantizer > target.MaxQuantizer {
		return ErrInvalidQuantizer
	}
	if target.Width > 0 || target.Height > 0 {
		if target.Width <= 0 || target.Height <= 0 || !validDimension(target.Width) || !validDimension(target.Height) {
			return ErrInvalidConfig
		}
		if target.Width != e.opts.Width || target.Height != e.opts.Height {
			return ErrInvalidConfig
		}
		e.opts.Width = target.Width
		e.opts.Height = target.Height
	}
	if target.FPS > 0 {
		e.opts.FPS = target.FPS
		e.opts.TimebaseNum = 1
		e.opts.TimebaseDen = target.FPS
		e.timing = timingState{timebaseNum: 1, timebaseDen: target.FPS, frameDuration: 1}
	}
	nextMinQuantizer := e.opts.MinQuantizer
	nextMaxQuantizer := e.opts.MaxQuantizer
	if target.MinQuantizer != 0 {
		nextMinQuantizer = target.MinQuantizer
	}
	if target.MaxQuantizer != 0 {
		nextMaxQuantizer = target.MaxQuantizer
	}
	if nextMinQuantizer > nextMaxQuantizer {
		return ErrInvalidQuantizer
	}
	if e.rc.mode == RateControlCQ && (e.opts.CQLevel < nextMinQuantizer || e.opts.CQLevel > nextMaxQuantizer) {
		return ErrInvalidQuantizer
	}
	e.rc.minQuantizer = libvpxPublicQuantizerToQIndex(nextMinQuantizer)
	e.rc.maxQuantizer = libvpxPublicQuantizerToQIndex(nextMaxQuantizer)
	e.opts.MinQuantizer = nextMinQuantizer
	e.opts.MaxQuantizer = nextMaxQuantizer
	e.rc.clampQuantizer()
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = e.rc.cqLevel
		e.rc.lastQuantizer = e.rc.cqLevel
		e.rc.lastInterQuantizer = e.rc.cqLevel
	}
	e.rc.dropFrameAllowed = target.AllowFrameDrop
	nextTemporal := e.temporal
	if target.BitrateKbps > 0 {
		nextRC := e.rc
		if err := nextRC.setBitrateKbps(target.BitrateKbps, e.timing); err != nil {
			return err
		}
		if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
			return err
		}
		e.rc = nextRC
		e.temporal = nextTemporal
		e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
		e.opts.TemporalScalability = nextTemporal.config
		return nil
	}
	nextRC := e.rc
	if err := nextRC.setBitrateKbps(e.rc.targetBitrateKbps, e.timing); err != nil {
		return err
	}
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

func (e *VP8Encoder) SetTemporalScalability(cfg TemporalScalabilityConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextTemporal := temporalState{}
	if err := nextTemporal.configure(cfg, e.rc.targetBitrateKbps); err != nil {
		return err
	}
	e.temporal = nextTemporal
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

func (e *VP8Encoder) SetTemporalLayerID(layerID int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	return e.temporal.setLayerID(layerID)
}

func (e *VP8Encoder) SetDeadline(deadline Deadline) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if deadline < DeadlineBestQuality || deadline > DeadlineRealtime {
		return ErrInvalidConfig
	}
	e.opts.Deadline = deadline
	e.opts.CpuUsed = libvpxEffectiveCPUUsed(deadline, e.opts.CpuUsed)
	return nil
}

func (e *VP8Encoder) SetCPUUsed(cpuUsed int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if cpuUsed < -16 || cpuUsed > 16 {
		return ErrInvalidConfig
	}
	e.opts.CpuUsed = libvpxEffectiveCPUUsed(e.opts.Deadline, cpuUsed)
	return nil
}

func (e *VP8Encoder) libvpxCPUUsed() int {
	if e == nil {
		return 0
	}
	return libvpxSpeedFeatureCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
}

func libvpxEffectiveCPUUsed(deadline Deadline, cpuUsed int) int {
	if cpuUsed < -16 {
		cpuUsed = -16
	} else if cpuUsed > 16 {
		cpuUsed = 16
	}
	if deadline == DeadlineGoodQuality {
		if cpuUsed < -5 {
			return -5
		}
		if cpuUsed > 5 {
			return 5
		}
	}
	return cpuUsed
}

func libvpxSpeedFeatureCPUUsed(deadline Deadline, cpuUsed int) int {
	cpuUsed = libvpxEffectiveCPUUsed(deadline, cpuUsed)
	if deadline == DeadlineRealtime {
		if cpuUsed < 0 {
			return -cpuUsed
		}
		return 4
	}
	return cpuUsed
}

func (e *VP8Encoder) SetKeyFrameInterval(frames int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if frames < 0 {
		return ErrInvalidConfig
	}
	e.opts.KeyFrameInterval = frames
	// Mirror libvpx oxcf.key_freq for estimate_keyframe_frequency.
	e.rc.keyFrameFrequency = frames
	return nil
}

func (e *VP8Encoder) SetAdaptiveKeyFrames(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.AdaptiveKeyFrames = enabled
	e.rc.autoKeyFrames = enabled
	return nil
}

// SetActiveMap installs a per-macroblock activity map. Cells equal to 0 mark
// inactive macroblocks; in inter frames those MBs skip mode decision and code
// as ZEROMV-LAST with skip=1, matching libvpx vp8_set_active_map (onyx_if.c)
// and the active_ptr early-exit in pickinter.c/rdopt.c. Pass a nil map to
// disable. Key frames ignore the map.
//
// rows and cols must equal the encoder's macroblock dimensions; len(activeMap)
// must equal rows*cols.
func (e *VP8Encoder) SetActiveMap(activeMap []uint8, rows int, cols int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if activeMap == nil {
		e.activeMapEnabled = false
		return nil
	}
	expectedRows := encoderMacroblockRows(e.opts.Height)
	expectedCols := encoderMacroblockCols(e.opts.Width)
	if rows != expectedRows || cols != expectedCols {
		return ErrInvalidConfig
	}
	if len(activeMap) < rows*cols {
		return ErrInvalidConfig
	}
	if len(e.activeMap) < rows*cols {
		e.activeMap = make([]uint8, rows*cols)
	}
	copy(e.activeMap[:rows*cols], activeMap[:rows*cols])
	e.activeMapEnabled = true
	return nil
}

func (e *VP8Encoder) SetNoiseSensitivity(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if level < 0 || level > 6 {
		return ErrInvalidConfig
	}
	e.opts.NoiseSensitivity = level
	if level == 0 {
		e.denoiser.reset()
	}
	return nil
}

func (e *VP8Encoder) SetARNR(maxFrames int, strength int, filterType int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if maxFrames < 0 || maxFrames > maxARNRFrames || strength < 0 || strength > 6 || filterType < 1 || filterType > 3 {
		return ErrInvalidConfig
	}
	e.opts.ARNRMaxFrames = maxFrames
	e.opts.ARNRStrength = strength
	e.opts.ARNRType = filterType
	return nil
}

func (e *VP8Encoder) SetTwoPassStats(stats []FirstPassFrameStats) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.TwoPassStats = stats
	e.twoPass.configure(stats, e.rc.bitsPerFrame, e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct, e.opts.TwoPassMaxPct)
	return nil
}

func (e *VP8Encoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	e.forceKeyFrame = true
}

func (e *VP8Encoder) Reset() {
	if e == nil {
		return
	}
	e.forceKeyFrame = false
	e.frameCount = 0
	e.cyclicRefreshIndex = 0
	e.lookaheadRead = 0
	e.lookaheadWrite = 0
	e.lookaheadCount = 0
	e.arnrLastReady = false
	e.denoiser.reset()
	e.firstPassCount = 0
	clearCyclicRefreshMap(e.cyclicRefreshMap)
	clearCyclicRefreshMap(e.cyclicRefreshAttemptMap)
	clearUint8Map(e.skinMap)
	clearUint8Map(e.consecZeroLast)
	clearUint8Map(e.consecZeroLastMVBias)
	clearBoolMap(e.dotArtifactChecked)
	e.lastInterZeroMVCount = 0
	e.lastInterSkipCount = 0
	e.lastFrameInterModesValid = false
	e.resetGoldenFrameStats()
	e.resetInterRDThresholdMultipliers()
	e.interRDFrameActive = false
	e.probSkipFalse = 128
	e.lastSkipFalseProbs = [3]uint8{}
	e.baseSkipFalseProbs = libvpxBaseSkipFalseProbs
	e.goldenRefAliasesLast = false
	e.altRefAliasesLast = false
	e.goldenRefAliasesAlt = false
	e.referenceFrameNumbers = [vp8common.MaxRefFrames]uint64{}
	e.rc.framesSinceKeyframe = 0
	e.rc.currentTemporalLayers = 0
	e.rc.resetRollingBitAverages()
	e.rc.bufferLevelBits = e.rc.bufferInitialBits
	e.rc.frameDropPressure = 0
	e.rc.avgFrameQuantizer = e.rc.maxQuantizer
	e.rc.normalInterQuantizerTotal = 0
	e.rc.normalInterFrames = 0
	e.rc.normalInterAvgQuantizer = e.rc.maxQuantizer
	e.rc.rateCorrectionFactor = 1.0
	e.rc.keyFrameCorrectionFactor = 1.0
	e.rc.goldenCorrectionFactor = 1.0
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = e.rc.cqLevel
		e.rc.lastQuantizer = e.rc.cqLevel
		e.rc.lastInterQuantizer = e.rc.cqLevel
	} else {
		e.rc.currentQuantizer = e.rc.minQuantizer
		e.rc.lastQuantizer = e.rc.minQuantizer
		e.rc.lastInterQuantizer = e.rc.minQuantizer
	}
	e.rc.frameTargetBits = e.rc.bitsPerFrame
	e.temporal.frameIndex = 0
	e.temporal.tl0PicIdx = 0
	e.temporal.tl0Valid = false
	e.temporal.refLayer = [temporalReferenceCount]int{}
	e.temporal.accounting = [MaxTemporalLayers]temporalLayerAccounting{}
	e.temporal.buffersSet = false
	e.twoPass.configure(e.opts.TwoPassStats, e.rc.bitsPerFrame, e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct, e.opts.TwoPassMaxPct)
	e.coefProbs = vp8tables.DefaultCoefProbs
	vp8dec.ResetModeProbs(&e.modeProbs)
	e.current.Reset()
	e.analysis.Reset()
	e.lastRef.Reset()
	e.goldenRef.Reset()
	e.altRef.Reset()
}

func (e *VP8Encoder) Close() error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.Reset()
	e.closed = true
	return nil
}

func normalizeEncoderOptions(opts EncoderOptions) (EncoderOptions, timingState, error) {
	if !validDimension(opts.Width) || !validDimension(opts.Height) {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.Threads < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.FPS < 0 || opts.TimebaseNum < 0 || opts.TimebaseDen < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.TimebaseNum == 0 && opts.TimebaseDen != 0 || opts.TimebaseNum != 0 && opts.TimebaseDen == 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.FPS == 0 && opts.TimebaseNum == 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.RateControlMode < RateControlVBR || opts.RateControlMode > RateControlCQ {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.TargetBitrateKbps <= 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidBitrate
	}
	if opts.MaxIntraBitratePct < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.GFCBRBoostPct < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.MinQuantizer < 0 || opts.MaxQuantizer < 0 || opts.MinQuantizer > maxQuantizer || opts.MaxQuantizer > maxQuantizer {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.MinQuantizer > opts.MaxQuantizer && opts.MaxQuantizer != 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.CQLevel < 0 || opts.CQLevel > maxQuantizer {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.Deadline < DeadlineBestQuality || opts.Deadline > DeadlineRealtime {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.CpuUsed < -16 || opts.CpuUsed > 16 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	opts.CpuUsed = libvpxEffectiveCPUUsed(opts.Deadline, opts.CpuUsed)
	if opts.KeyFrameInterval < 0 || opts.LookaheadFrames < 0 || opts.LookaheadFrames > maxLookaheadFrames || opts.TokenPartitions < int(vp8common.OnePartition) || opts.TokenPartitions > int(vp8common.EightPartition) {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.ARNRType == 0 {
		opts.ARNRType = 3
	}
	if opts.Sharpness < 0 || opts.Sharpness > 7 ||
		opts.NoiseSensitivity < 0 || opts.NoiseSensitivity > 6 ||
		opts.ARNRMaxFrames < 0 || opts.ARNRMaxFrames > maxARNRFrames ||
		opts.ARNRStrength < 0 || opts.ARNRStrength > 6 ||
		opts.ARNRType < 1 || opts.ARNRType > 3 ||
		opts.TwoPassVBRBiasPct < 0 || opts.TwoPassMinPct < 0 || opts.TwoPassMaxPct < 0 ||
		opts.ScreenContentMode < 0 || opts.ScreenContentMode > 2 || opts.StaticThreshold < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}

	timing := timingState{frameDuration: 1}
	if opts.TimebaseNum > 0 {
		timing.timebaseNum = opts.TimebaseNum
		timing.timebaseDen = opts.TimebaseDen
	} else {
		timing.timebaseNum = 1
		timing.timebaseDen = opts.FPS
		opts.TimebaseNum = 1
		opts.TimebaseDen = opts.FPS
	}
	if opts.FPS == 0 && timing.timebaseNum == 1 {
		opts.FPS = timing.timebaseDen
	}
	if opts.KeyFrameInterval == 0 {
		opts.KeyFrameInterval = 120
	}
	return opts, timing, nil
}

func validDimension(v int) bool {
	return v > 0 && v <= maxVP8Dimension
}

func translateEncoderError(err error) error {
	switch {
	case errors.Is(err, vp8enc.ErrBufferTooSmall):
		return ErrBufferTooSmall
	case errors.Is(err, vp8enc.ErrInvalidPacketConfig), errors.Is(err, vp8enc.ErrModeBufferTooSmall):
		return ErrInvalidConfig
	default:
		return err
	}
}

func (e *VP8Encoder) initReferenceFrames(width int, height int) error {
	if err := e.current.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.analysis.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.lastRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.goldenRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.altRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.loopFilterPick.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP8Encoder) encoderLoopFilter(frameType vp8common.FrameType) (uint8, uint8) {
	level := libvpxInitialLoopFilterLevel(e.rc.currentQuantizer)
	if frameType == vp8common.InterFrame {
		level = int(e.loopFilterLevel)
	}
	level = libvpxClampLoopFilterLevel(e.rc.currentQuantizer, level)
	if level > 63 {
		level = 63
	}
	sharpness := e.opts.Sharpness
	if frameType == vp8common.KeyFrame {
		sharpness = 0
	}
	return uint8(level), uint8(sharpness)
}

func (e *VP8Encoder) encoderLoopFilterHeader(level uint8, sharpness uint8) vp8dec.LoopFilterHeader {
	header := vp8dec.LoopFilterHeader{
		Level:          level,
		SharpnessLevel: sharpness,
	}
	if e.encoderUsesSimpleLoopFilter() {
		header.Type = vp8dec.SimpleLoopFilter
	}
	if level == 0 {
		return header
	}
	header.DeltaEnabled = true
	header.DeltaUpdate = true
	header.RefDeltas = [vp8common.MaxRefLFDeltas]int8{2, 0, -2, -2}
	header.ModeDeltas = [vp8common.MaxModeLFDeltas]int8{4, e.encoderLoopFilterInterModeDelta(), 2, 4}
	return header
}

func (e *VP8Encoder) encoderUsesSimpleLoopFilter() bool {
	return e != nil && e.opts.Deadline == DeadlineRealtime && e.libvpxCPUUsed() >= 14
}

func (e *VP8Encoder) encoderLoopFilterInterModeDelta() int8 {
	if e != nil && e.opts.Deadline == DeadlineRealtime {
		return -12
	}
	return -2
}

func (e *VP8Encoder) pickLoopFilterLevel(src vp8enc.SourceImage, frameType vp8common.FrameType, seedLevel uint8, sharpness uint8, rows int, cols int, required int) (uint8, error) {
	if len(e.reconstructModes) < required {
		return 0, ErrInvalidConfig
	}
	if seedLevel == 0 {
		return 0, nil
	}
	if e.loopFilterUsesFastSearch() {
		return e.pickLoopFilterLevelFast(src, frameType, seedLevel, sharpness, rows, cols, required)
	}
	return e.pickLoopFilterLevelFull(src, frameType, seedLevel, sharpness, rows, cols, required)
}

func (e *VP8Encoder) loopFilterUsesFastSearch() bool {
	if e == nil {
		return false
	}
	speed := e.libvpxCPUUsed()
	switch e.opts.Deadline {
	case DeadlineGoodQuality:
		return speed > 4
	case DeadlineRealtime:
		return speed == 3 || speed > 4
	default:
		return false
	}
}

func (e *VP8Encoder) pickLoopFilterLevelFast(src vp8enc.SourceImage, frameType vp8common.FrameType, seedLevel uint8, sharpness uint8, rows int, cols int, required int) (uint8, error) {
	minLevel := libvpxMinLoopFilterLevel(e.rc.currentQuantizer)
	maxLevel := libvpxMaxLoopFilterLevel(e.rc.currentQuantizer)
	level := clampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	bestLevel := level
	bestErr, err := e.loopFilterTrialLumaSSE(src, frameType, level, sharpness, rows, cols, required, true)
	if err != nil {
		return 0, err
	}

	filtLevel := level - loopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr, err := e.loopFilterTrialLumaSSE(src, frameType, filtLevel, sharpness, rows, cols, required, true)
		if err != nil {
			return 0, err
		}
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= loopFilterSearchStep(filtLevel)
	}

	filtLevel = level + loopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr, err := e.loopFilterTrialLumaSSE(src, frameType, filtLevel, sharpness, rows, cols, required, true)
			if err != nil {
				return 0, err
			}
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += loopFilterSearchStep(filtLevel)
		}
	}
	return uint8(clampLoopFilterPickLevel(bestLevel, minLevel, maxLevel)), nil
}

func (e *VP8Encoder) pickLoopFilterLevelFull(src vp8enc.SourceImage, frameType vp8common.FrameType, seedLevel uint8, sharpness uint8, rows int, cols int, required int) (uint8, error) {
	minLevel := libvpxMinLoopFilterLevel(e.rc.currentQuantizer)
	maxLevel := libvpxMaxLoopFilterLevel(e.rc.currentQuantizer)
	filtMid := clampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	filterStep := 4
	if filtMid >= 16 {
		filterStep = filtMid / 4
	}
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	score := func(level int) (int, error) {
		if ssSet[level] {
			return ssErr[level], nil
		}
		trialErr, err := e.loopFilterTrialLumaSSE(src, frameType, level, sharpness, rows, cols, required, false)
		if err != nil {
			return 0, err
		}
		ssErr[level] = trialErr
		ssSet[level] = true
		return trialErr, nil
	}

	bestErr, err := score(filtMid)
	if err != nil {
		return 0, err
	}
	filtBest := filtMid
	filtDirection := 0
	for filterStep > 0 {
		bias := 0
		filtHigh := filtMid + filterStep
		if filtHigh > maxLevel {
			filtHigh = maxLevel
		}
		filtLow := filtMid - filterStep
		if filtLow < minLevel {
			filtLow = minLevel
		}

		if filtDirection <= 0 && filtLow != filtMid {
			filtErr, err := score(filtLow)
			if err != nil {
				return 0, err
			}
			if filtErr-bias < bestErr {
				if filtErr < bestErr {
					bestErr = filtErr
				}
				filtBest = filtLow
			}
		}
		if filtDirection >= 0 && filtHigh != filtMid {
			filtErr, err := score(filtHigh)
			if err != nil {
				return 0, err
			}
			if filtErr < bestErr-bias {
				bestErr = filtErr
				filtBest = filtHigh
			}
		}
		if filtBest == filtMid {
			filterStep /= 2
			filtDirection = 0
		} else {
			if filtBest < filtMid {
				filtDirection = -1
			} else {
				filtDirection = 1
			}
			filtMid = filtBest
		}
	}
	return uint8(filtBest), nil
}

func (e *VP8Encoder) loopFilterTrialLumaSSE(src vp8enc.SourceImage, frameType vp8common.FrameType, level int, sharpness uint8, rows int, cols int, required int, partial bool) (int, error) {
	if partial {
		startRow, rowCount := loopFilterPartialFrameWindow(rows)
		copyLoopFilterPartialLuma(&e.loopFilterPick.Img, &e.analysis.Img, startRow, rowCount)
		if level > 0 {
			header := e.encoderLoopFilterHeader(uint8(level), sharpness)
			if err := vp8dec.ApplyLoopFilterPartial(&e.loopFilterPick.Img, rows, cols, e.reconstructModes[:required], frameType, header, vp8dec.SegmentationHeader{}, &e.loopInfo, startRow, rowCount); err != nil {
				return 0, ErrInvalidConfig
			}
		}
		return loopFilterLumaSSE(src, &e.loopFilterPick.Img, rows, cols, true), nil
	}
	copyFrameImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
	if level > 0 {
		header := e.encoderLoopFilterHeader(uint8(level), sharpness)
		if err := vp8dec.ApplyLoopFilterFullLuma(&e.loopFilterPick.Img, rows, cols, e.reconstructModes[:required], frameType, header, vp8dec.SegmentationHeader{}, &e.loopInfo); err != nil {
			return 0, ErrInvalidConfig
		}
	}
	return loopFilterLumaSSE(src, &e.loopFilterPick.Img, rows, cols, false), nil
}

// copyLoopFilterPartialLuma refreshes the luma plane window the partial-frame
// loop-filter trial reads. It mirrors libvpx's yv12_copy_partial_frame: copy
// the [startRow, startRow+rowCount) MB rows plus 4 luma lines above so the
// macroblock horizontal edge filter has fresh context to read.
func copyLoopFilterPartialLuma(dst *vp8common.Image, src *vp8common.Image, startRow int, rowCount int) {
	if rowCount <= 0 {
		return
	}
	startY := startRow * 16
	if startY > 4 {
		startY -= 4
	} else {
		startY = 0
	}
	endY := (startRow + rowCount) * 16
	if endY > src.CodedHeight {
		endY = src.CodedHeight
	}
	if endY > dst.CodedHeight {
		endY = dst.CodedHeight
	}
	if endY <= startY {
		return
	}
	width := src.CodedWidth
	if dst.CodedWidth < width {
		width = dst.CodedWidth
	}
	if src.YStride == dst.YStride && width == src.YStride {
		// Fast path: contiguous copy when strides and full coded width match.
		copy(dst.Y[startY*dst.YStride:endY*dst.YStride], src.Y[startY*src.YStride:endY*src.YStride])
		return
	}
	for row := startY; row < endY; row++ {
		copy(dst.Y[row*dst.YStride:row*dst.YStride+width], src.Y[row*src.YStride:row*src.YStride+width])
	}
}

// calcKeyFrameSSError ports libvpx vp8/encoder/onyx_if.c vp8_calc_ss_err over
// the Y plane: full-frame sum of squared 16x16 luma differences between the
// encoded source and the reconstructed frame. Used by the forced-key recode
// branch to compare against ambient_err.
func calcKeyFrameSSError(src vp8enc.SourceImage, recon *vp8common.Image, rows int, cols int) int {
	if rows <= 0 || cols <= 0 {
		return 0
	}
	return loopFilterLumaSSE(src, recon, rows, cols, false)
}

func loopFilterLumaSSE(src vp8enc.SourceImage, img *vp8common.Image, rows int, cols int, partial bool) int {
	startRow, rowCount := 0, rows
	if partial {
		startRow, rowCount = loopFilterPartialFrameWindow(rows)
	}
	total := 0
	for mbRow := startRow; mbRow < startRow+rowCount && mbRow < rows; mbRow++ {
		baseY := mbRow * 16
		for mbCol := 0; mbCol < cols; mbCol++ {
			baseX := mbCol * 16
			if baseY+16 <= src.Height && baseX+16 <= src.Width && baseY+16 <= img.CodedHeight && baseX+16 <= img.CodedWidth {
				total += dsp.SSE16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, img.Y[baseY*img.YStride+baseX:], img.YStride)
				continue
			}
			total += loopFilterLumaBlockSSE(src, img, baseY, baseX)
		}
	}
	return total
}

func loopFilterLumaBlockSSE(src vp8enc.SourceImage, img *vp8common.Image, baseY int, baseX int) int {
	sse := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		imgY := clampEncodeCoord(baseY+row, img.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			imgX := clampEncodeCoord(baseX+col, img.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(img.Y[imgY*img.YStride+imgX])
			sse += diff * diff
		}
	}
	return sse
}

func loopFilterPartialFrameWindow(rows int) (int, int) {
	if rows <= 0 {
		return 0, 0
	}
	start := rows / 2
	count := rows / vp8common.PartialFrameFraction
	if count == 0 {
		count = 1
	}
	if start+count > rows {
		count = rows - start
	}
	return start, count
}

func loopFilterSearchStep(level int) int {
	if level > 10 {
		return 2
	}
	return 1
}

func clampLoopFilterPickLevel(level int, minLevel int, maxLevel int) int {
	if level < minLevel {
		return minLevel
	}
	if level > maxLevel {
		return maxLevel
	}
	return level
}

func libvpxClampLoopFilterLevel(qIndex int, level int) int {
	minLevel := libvpxMinLoopFilterLevel(qIndex)
	maxLevel := libvpxMaxLoopFilterLevel(qIndex)
	if level < minLevel {
		return minLevel
	}
	if level > maxLevel {
		return maxLevel
	}
	return level
}

func libvpxMinLoopFilterLevel(qIndex int) int {
	if qIndex <= 6 {
		return 0
	}
	if qIndex <= 16 {
		return 1
	}
	return qIndex / 8
}

func libvpxMaxLoopFilterLevel(qIndex int) int {
	_ = qIndex
	return 63
}

func libvpxInitialLoopFilterLevel(qIndex int) int {
	if qIndex <= 0 {
		return 0
	}
	level := qIndex * 3 / 8
	if level > 63 {
		return 63
	}
	return level
}

func (e *VP8Encoder) applyReconstructionLoopFilter(frameType vp8common.FrameType, header vp8dec.LoopFilterHeader, segmentation vp8enc.SegmentationConfig, rows int, cols int, required int) error {
	if header.Level == 0 {
		return nil
	}
	if len(e.reconstructModes) < required {
		return ErrInvalidConfig
	}
	// libvpx vp8_loop_filter_frame_init reads the ALT_LF feature data
	// when cm->segmentation.enabled is set, so the reconstruction-side
	// LF must see the same per-segment deltas the bitstream signals.
	if err := vp8dec.ApplyLoopFilter(&e.analysis.Img, rows, cols, e.reconstructModes[:required], frameType, header, loopFilterSegmentationHeader(segmentation), &e.loopInfo); err != nil {
		return ErrInvalidConfig
	}
	e.analysis.ExtendBorders()
	return nil
}

func (e *VP8Encoder) refreshKeyFrameReferencesFromAnalysis() {
	e.resetGoldenFrameStats()
	copyFrameImage(&e.current.Img, &e.analysis.Img)
	e.current.ExtendBorders()
	copyFrameImage(&e.lastRef.Img, &e.current.Img)
	e.lastRef.ExtendBorders()
	copyFrameImage(&e.goldenRef.Img, &e.current.Img)
	e.goldenRef.ExtendBorders()
	copyFrameImage(&e.altRef.Img, &e.current.Img)
	e.altRef.ExtendBorders()
	e.lastFrameInterModesValid = false
	e.goldenRefAliasesLast = true
	e.altRefAliasesLast = true
	e.goldenRefAliasesAlt = true
	e.updateKeyFrameReferenceFrameNumbers()
}

func (e *VP8Encoder) rememberLastFrameInterModes(signBias [vp8common.MaxRefFrames]bool) {
	if e == nil || len(e.interFrameModes) == 0 {
		return
	}
	if len(e.lastFrameInterModes) != len(e.interFrameModes) {
		e.lastFrameInterModes = make([]vp8enc.InterFrameMacroblockMode, len(e.interFrameModes))
	}
	if len(e.lastFrameInterModeBias) != len(e.interFrameModes) {
		e.lastFrameInterModeBias = make([]bool, len(e.interFrameModes))
	}
	copy(e.lastFrameInterModes, e.interFrameModes)
	for i := range e.interFrameModes {
		ref := e.interFrameModes[i].RefFrame
		e.lastFrameInterModeBias[i] = ref > vp8common.IntraFrame && ref < vp8common.MaxRefFrames && signBias[ref]
	}
	e.lastFrameInterModesValid = true
}

func (e *VP8Encoder) refreshZeroInterFrameReferences(cfg vp8enc.InterFrameStateConfig, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame) {
	copyFrameImage(&e.current.Img, ref)
	e.current.ExtendBorders()
	e.copyInterFrameReferences(cfg)
	if cfg.RefreshLast && refFrame != vp8common.LastFrame {
		copyFrameImage(&e.lastRef.Img, &e.current.Img)
		e.lastRef.ExtendBorders()
	}
	if cfg.RefreshGolden && refFrame != vp8common.GoldenFrame {
		copyFrameImage(&e.goldenRef.Img, &e.current.Img)
		e.goldenRef.ExtendBorders()
	}
	if cfg.RefreshAltRef && refFrame != vp8common.AltRefFrame {
		copyFrameImage(&e.altRef.Img, &e.current.Img)
		e.altRef.ExtendBorders()
	}
	e.updateInterReferenceAliases(cfg)
	e.updateInterReferenceFrameNumbers(cfg)
}

func (e *VP8Encoder) refreshInterFrameReferencesFromAnalysis(cfg vp8enc.InterFrameStateConfig) {
	copyFrameImage(&e.current.Img, &e.analysis.Img)
	e.current.ExtendBorders()
	e.copyInterFrameReferences(cfg)
	if cfg.RefreshLast {
		copyFrameImage(&e.lastRef.Img, &e.current.Img)
		e.lastRef.ExtendBorders()
	}
	if cfg.RefreshGolden {
		copyFrameImage(&e.goldenRef.Img, &e.current.Img)
		e.goldenRef.ExtendBorders()
	}
	if cfg.RefreshAltRef {
		copyFrameImage(&e.altRef.Img, &e.current.Img)
		e.altRef.ExtendBorders()
	}
	e.updateInterReferenceAliases(cfg)
	e.updateInterReferenceFrameNumbers(cfg)
}

func (e *VP8Encoder) updateInterReferenceAliases(cfg vp8enc.InterFrameStateConfig) {
	if cfg.RefreshLast && cfg.RefreshGolden {
		e.goldenRefAliasesLast = true
	} else if cfg.RefreshLast != cfg.RefreshGolden {
		e.goldenRefAliasesLast = false
	}
	if cfg.RefreshLast && cfg.RefreshAltRef {
		e.altRefAliasesLast = true
	} else if cfg.RefreshLast != cfg.RefreshAltRef {
		e.altRefAliasesLast = false
	}
	if cfg.RefreshAltRef && cfg.RefreshGolden {
		e.goldenRefAliasesAlt = true
	} else if cfg.RefreshAltRef != cfg.RefreshGolden {
		e.goldenRefAliasesAlt = false
	}
}

func (e *VP8Encoder) copyInterFrameReferences(cfg vp8enc.InterFrameStateConfig) {
	switch cfg.CopyBufferToAltRef {
	case 1:
		copyFrameImage(&e.altRef.Img, &e.lastRef.Img)
		e.altRef.ExtendBorders()
	case 2:
		copyFrameImage(&e.altRef.Img, &e.goldenRef.Img)
		e.altRef.ExtendBorders()
	}
	switch cfg.CopyBufferToGolden {
	case 1:
		copyFrameImage(&e.goldenRef.Img, &e.lastRef.Img)
		e.goldenRef.ExtendBorders()
	case 2:
		copyFrameImage(&e.goldenRef.Img, &e.altRef.Img)
		e.goldenRef.ExtendBorders()
	}
}

func (e *VP8Encoder) updateKeyFrameReferenceFrameNumbers() {
	if e == nil {
		return
	}
	frameNumber := e.frameCount
	e.referenceFrameNumbers[vp8common.LastFrame] = frameNumber
	e.referenceFrameNumbers[vp8common.GoldenFrame] = frameNumber
	e.referenceFrameNumbers[vp8common.AltRefFrame] = frameNumber
}

func (e *VP8Encoder) updateInterReferenceFrameNumbers(cfg vp8enc.InterFrameStateConfig) {
	if e == nil {
		return
	}
	frameNumber := e.frameCount

	if cfg.RefreshAltRef {
		e.referenceFrameNumbers[vp8common.AltRefFrame] = frameNumber
	} else {
		switch cfg.CopyBufferToAltRef {
		case 1:
			e.referenceFrameNumbers[vp8common.AltRefFrame] = e.referenceFrameNumbers[vp8common.LastFrame]
		case 2:
			e.referenceFrameNumbers[vp8common.AltRefFrame] = e.referenceFrameNumbers[vp8common.GoldenFrame]
		}
	}

	if cfg.RefreshGolden {
		e.referenceFrameNumbers[vp8common.GoldenFrame] = frameNumber
	} else {
		switch cfg.CopyBufferToGolden {
		case 1:
			e.referenceFrameNumbers[vp8common.GoldenFrame] = e.referenceFrameNumbers[vp8common.LastFrame]
		case 2:
			e.referenceFrameNumbers[vp8common.GoldenFrame] = e.referenceFrameNumbers[vp8common.AltRefFrame]
		}
	}

	if cfg.RefreshLast {
		e.referenceFrameNumbers[vp8common.LastFrame] = frameNumber
	}
}

func convertKeyFrameMode(src *vp8enc.KeyFrameMacroblockMode, dst *vp8dec.MacroblockMode) {
	*dst = vp8dec.MacroblockMode{
		SegmentID: src.SegmentID,
		RefFrame:  vp8common.IntraFrame,
		Mode:      src.YMode,
		UVMode:    src.UVMode,
		Is4x4:     src.YMode == vp8common.BPred,
		BModes:    src.BModes,
	}
}

func convertInterFrameMode(src *vp8enc.InterFrameMacroblockMode, dst *vp8dec.MacroblockMode) {
	*dst = vp8dec.MacroblockMode{
		SegmentID:   src.SegmentID,
		RefFrame:    convertInterFrameReference(src),
		Mode:        src.Mode,
		UVMode:      src.UVMode,
		Is4x4:       interFrameModeUses4x4Tokens(src.Mode),
		BModes:      src.BModes,
		MV:          vp8dec.MotionVector{Row: src.MV.Row, Col: src.MV.Col},
		MBSkipCoeff: src.MBSkipCoeff,
		Partition:   src.Partition,
	}
	for i := range src.BlockMV {
		dst.BlockMV[i] = vp8dec.MotionVector{Row: src.BlockMV[i].Row, Col: src.BlockMV[i].Col}
	}
}

func convertInterFrameReference(mode *vp8enc.InterFrameMacroblockMode) vp8common.MVReferenceFrame {
	if mode.Mode >= vp8common.DCPred && mode.Mode <= vp8common.BPred {
		return vp8common.IntraFrame
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return vp8common.LastFrame
	}
	return mode.RefFrame
}

func convertMacroblockCoefficients(src *vp8enc.MacroblockCoefficients, is4x4 bool, dst *vp8dec.MacroblockTokens) {
	dst.EOB = [25]uint8{}
	if !is4x4 {
		eob := src.EOB[24]
		dst.EOB[24] = eob
		copyQCoeffForEOB(&src.QCoeff[24], eob, &dst.QCoeff[24])
		for i := 0; i < 16; i++ {
			eob := src.EOB[i]
			if eob < 1 {
				eob = 1
			}
			dst.EOB[i] = eob
			copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
		}
	} else {
		for i := 0; i < 16; i++ {
			eob := src.EOB[i]
			dst.EOB[i] = eob
			copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
		}
	}
	for i := 16; i < 24; i++ {
		eob := src.EOB[i]
		dst.EOB[i] = eob
		copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
	}
}

func interFrameModeUses4x4Tokens(mode vp8common.MBPredictionMode) bool {
	return mode == vp8common.BPred || mode == vp8common.SplitMV
}

func copyQCoeffForEOB(src *[16]int16, eob uint8, dst *[16]int16) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dst[0] = src[0]
		return
	}
	*dst = *src
}

func encoderMacroblockCount(width int, height int) int {
	return encoderMacroblockRows(height) * encoderMacroblockCols(width)
}

func encoderMacroblockRows(height int) int {
	return (height + 15) >> 4
}

func encoderMacroblockCols(width int) int {
	return (width + 15) >> 4
}
