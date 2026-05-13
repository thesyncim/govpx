package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"sync/atomic"
	_ "unsafe" // for go:linkname
)

// nanotime returns the monotonic clock in nanoseconds. Linked to
// runtime.nanotime to avoid time.Now()'s per-call wall+mono allocation.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// runtimeProcYield issues the runtime's architecture-specific processor
// pause/yield instruction without handing the P back to the scheduler.
// Row-thread wave-front waits use it for short producer/consumer gaps.
//
//go:linkname runtimeProcYield runtime.procyield
func runtimeProcYield(cycles uint32)

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

// Tuning selects the encoder's visual quality model.
type Tuning int

const (
	// TunePSNR selects the default PSNR-oriented rate-distortion model.
	TunePSNR Tuning = iota
	// TuneSSIM selects libvpx-style SSIM activity masking.
	TuneSSIM
)

// EncodeFlags controls per-frame VP8 reference and packet behavior on a
// single [VP8Encoder.EncodeInto] call. The zero value asks the encoder
// to use its configured defaults. Flag bits mirror libvpx's
// VPX_EFLAG_FORCE_* / VP8_EFLAG_NO_REF_* / VP8_EFLAG_NO_UPD_* /
// VP8_EFLAG_NO_UPD_ENTROPY / VP8_EFLAG_FORCE_GF / VP8_EFLAG_FORCE_ARF.
type EncodeFlags uint32

const (
	// EncodeForceKeyFrame forces the next encoded packet to be a key frame.
	EncodeForceKeyFrame EncodeFlags = 1 << iota

	// EncodeInvisibleFrame encodes a hidden frame that updates references but
	// is not displayed.
	EncodeInvisibleFrame

	// EncodeNoReferenceLast prevents inter prediction from LAST.
	EncodeNoReferenceLast
	// EncodeNoReferenceGolden prevents inter prediction from GOLDEN.
	EncodeNoReferenceGolden
	// EncodeNoReferenceAltRef prevents inter prediction from ALTREF.
	EncodeNoReferenceAltRef

	// EncodeNoUpdateLast prevents refreshing LAST from this frame.
	EncodeNoUpdateLast
	// EncodeNoUpdateGolden prevents refreshing GOLDEN from this frame.
	EncodeNoUpdateGolden
	// EncodeNoUpdateAltRef prevents refreshing ALTREF from this frame.
	EncodeNoUpdateAltRef

	// EncodeNoUpdateEntropy prevents the frame from committing updated entropy
	// probabilities.
	EncodeNoUpdateEntropy

	// EncodeForceGoldenFrame forces a GOLDEN refresh from this frame.
	EncodeForceGoldenFrame
	// EncodeForceAltRefFrame forces an ALTREF refresh from this frame.
	EncodeForceAltRefFrame
)

// EncoderPhaseStats accumulates opt-in coarse encoder phase timing in
// nanoseconds and counters for the SAD/subpel search hot path. The
// encoder updates the caller-owned value only when
// EncoderOptions.PhaseStats is non-nil; normal encodes do not read the
// clock or atomically update these counters. The caller owns the value,
// may [EncoderPhaseStats.Reset] it between warmup and measurement, and
// may sample it concurrently with EncodeInto only under its own
// synchronization.
type EncoderPhaseStats struct {
	// InterReconstructNS is time spent rebuilding inter-frame prediction and
	// residual planes.
	InterReconstructNS int64 `json:"inter_reconstruct_ns"`
	// KeyReconstructNS is time spent rebuilding key-frame prediction and
	// residual planes.
	KeyReconstructNS int64 `json:"key_reconstruct_ns"`
	// LoopFilterPickNS is time spent selecting loop-filter parameters.
	LoopFilterPickNS int64 `json:"loop_filter_pick_ns"`
	// LoopFilterApplyNS is time spent applying the loop filter to committed
	// frames.
	LoopFilterApplyNS int64 `json:"loop_filter_apply_ns"`
	// PacketWriteNS is time spent packing VP8 frame data.
	PacketWriteNS int64 `json:"packet_write_ns"`
	// InterAttempts counts inter-frame encode attempts, including recodes.
	InterAttempts int64 `json:"inter_attempts"`
	// KeyAttempts counts key-frame encode attempts, including recodes.
	KeyAttempts int64 `json:"key_attempts"`

	// LoopFilterTrials counts candidate filter-strength evaluations.
	LoopFilterTrials int64 `json:"loop_filter_trials"`
	// LoopFilterTrialCopyNS is time spent copying trial frame buffers.
	LoopFilterTrialCopyNS int64 `json:"loop_filter_trial_copy_ns"`
	// LoopFilterTrialFilterNS is time spent filtering trial frame buffers.
	LoopFilterTrialFilterNS int64 `json:"loop_filter_trial_filter_ns"`
	// LoopFilterTrialSSENS is time spent scoring loop-filter trials.
	LoopFilterTrialSSENS int64 `json:"loop_filter_trial_sse_ns"`

	// InterRDCoeffCacheRequests counts inter RD coefficient-cache lookups.
	InterRDCoeffCacheRequests int64 `json:"inter_rd_coeff_cache_requests"`
	// InterRDCoeffCacheDCTHits counts cached DCT/residual reuse hits.
	InterRDCoeffCacheDCTHits int64 `json:"inter_rd_coeff_cache_dct_hits"`
	// InterCoefTokenRecords counts token-rate records produced during inter RD.
	InterCoefTokenRecords int64 `json:"inter_coef_token_records"`

	// FullPelSADCalls counts full-pixel SAD search invocations.
	FullPelSADCalls int64 `json:"fullpel_sad_calls"`
	// FullPelSADCandidates counts full-pixel candidate positions scored.
	FullPelSADCandidates int64 `json:"fullpel_sad_candidates"`
	// FullPelBatchCalls counts vectorized multi-candidate SAD batches.
	FullPelBatchCalls int64 `json:"fullpel_batch_calls"`
	// FullPelBoundsRejects counts full-pixel candidates outside legal bounds.
	FullPelBoundsRejects int64 `json:"fullpel_bounds_rejects"`
	// FullPelEarlyBreaks counts SAD evaluations stopped by an existing best.
	FullPelEarlyBreaks int64 `json:"fullpel_early_breaks"`
	// SubpelCandidates counts sub-pixel candidate positions scored.
	SubpelCandidates int64 `json:"subpel_candidates"`
	// SubpelVarianceCalls counts sub-pixel variance evaluations.
	SubpelVarianceCalls int64 `json:"subpel_variance_calls"`
	// SubpelCacheHits counts sub-pixel predictor-cache hits.
	SubpelCacheHits int64 `json:"subpel_cache_hits"`
	// SubpelBoundsRejects counts sub-pixel candidates outside legal bounds.
	SubpelBoundsRejects int64 `json:"subpel_bounds_rejects"`
	// SubpelEarlyBreaks counts sub-pixel evaluations stopped by an existing best.
	SubpelEarlyBreaks int64 `json:"subpel_early_breaks"`
}

// Reset clears all accumulated phase counters.
func (s *EncoderPhaseStats) Reset() {
	if s == nil {
		return
	}
	*s = EncoderPhaseStats{}
}

type encoderPhase uint8

const (
	encoderPhaseInterReconstruct encoderPhase = iota
	encoderPhaseKeyReconstruct
	encoderPhaseLoopFilterPick
	encoderPhaseLoopFilterApply
	encoderPhasePacketWrite
)

// EncoderOptions configures a VP8 encoder.
type EncoderOptions struct {
	// Width and Height are the fixed visible dimensions accepted by EncodeInto.
	Width  int
	Height int

	// FPS sets a 1/FPS timebase when TimebaseNum and TimebaseDen are zero.
	FPS int

	// TimebaseNum is the numerator of the caller timebase.
	TimebaseNum int
	// TimebaseDen is the denominator of the caller timebase.
	TimebaseDen int

	// Threads selects the worker-goroutine count for the inter-frame
	// row-threaded macroblock pipeline.
	//
	//   - 0 is treated as 1.
	//   - 1 uses the serial reference path: no row-worker pool, no
	//     atomics or channel work in the reconstruction hot path.
	//   - Values >1 allocate a persistent row-worker pool and enable
	//     the libvpx-style wave-front inter-frame encode when the
	//     frame is large enough and oracle tracing is disabled.
	//     Thread counts are deterministic but produce a bitstream
	//     that may differ from Threads=1, matching libvpx.
	//   - Negative values return [ErrInvalidConfig].
	//
	// Mirrors libvpx's --threads / cpi->oxcf.multi_threaded.
	Threads int

	// RateControlMode selects VBR, CBR, constrained-quality, or VPX_Q behavior.
	RateControlMode RateControlMode
	// TargetBitrateKbps is the total target bitrate in kbps. Required.
	TargetBitrateKbps int
	// MinBitrateKbps optionally bounds runtime bitrate updates from below.
	// Zero means no lower bound.
	MinBitrateKbps int
	// MaxBitrateKbps optionally bounds runtime bitrate updates from above.
	// Zero means no upper bound.
	MaxBitrateKbps int

	// MinQuantizer is the lowest public 0..63 quantizer the encoder may use.
	MinQuantizer int
	// MaxQuantizer is the highest public 0..63 quantizer the encoder may use.
	MaxQuantizer int
	// QuantizerRangeSet forces MinQuantizer/MaxQuantizer to be honored even
	// when both are zero. When false, the all-zero range uses libvpx's default
	// 4..56 vpxenc range so zero-valued EncoderOptions remain useful.
	QuantizerRangeSet bool
	// CQLevel mirrors libvpx's VP8E_SET_CQ_LEVEL. [RateControlCQ] applies
	// it as a floor; [RateControlQ] validates and stores it like libvpx
	// VPX_Q. Zero uses libvpx's default level after MinQuantizer
	// resolution (a zero MinQuantizer is itself promoted to 4 first).
	CQLevel int

	// UndershootPct caps libvpx-style downward rate adjustment as a percentage
	// of the target frame size; valid range is [0, 100]. Zero uses the libvpx
	// default.
	UndershootPct int
	// OvershootPct caps libvpx-style upward rate adjustment as a percentage of
	// the target frame size; valid range is [0, 1000]. Zero uses the libvpx
	// default.
	OvershootPct int

	// BufferSizeMs is the virtual rate-control buffer size in milliseconds.
	// Zero selects libvpx's default sizing.
	BufferSizeMs int
	// BufferInitialSizeMs is the initial virtual buffer fill in milliseconds.
	// Zero selects libvpx's default.
	BufferInitialSizeMs int
	// BufferOptimalSizeMs is the target virtual buffer fill in milliseconds.
	// Zero selects libvpx's default.
	BufferOptimalSizeMs int
	// MaxIntraBitratePct caps key-frame bitrate as a percentage of the
	// per-frame target. Zero disables the cap; valid range is non-negative.
	MaxIntraBitratePct int
	// GFCBRBoostPct controls golden-frame boost in CBR mode as a percentage of
	// the per-frame target. Zero uses libvpx's default; valid range is
	// non-negative.
	GFCBRBoostPct int

	// DropFrameAllowed enables rate-control frame dropping.
	DropFrameAllowed bool
	// DropFrameWaterMark is the percentage of optimal_buffer_level at
	// which rate control begins dropping frames. When DropFrameAllowed
	// is true this defaults to 60 if left zero; when DropFrameAllowed
	// is false no frame drops fire regardless of this value. Mirrors
	// libvpx's rc_dropframe_thresh / oxcf.drop_frames_water_mark.
	DropFrameWaterMark int

	// TemporalScalability configures automatic temporal-layer scheduling.
	TemporalScalability TemporalScalabilityConfig

	// Deadline selects the encoder speed/quality operating mode.
	Deadline Deadline
	// CpuUsed selects the libvpx-style speed preset in [-16, 16].
	CpuUsed int
	// Tuning selects the PSNR or SSIM visual quality model.
	Tuning Tuning

	// KeyFrameInterval is the maximum GOP distance in frames. Zero disables
	// interval-forced key frames.
	KeyFrameInterval int
	// LookaheadFrames enables buffered encoding. When positive, EncodeInto
	// queues input frames and returns ErrFrameNotReady until enough future
	// frames are available; FlushInto drains the queue at end of stream.
	LookaheadFrames int
	// AutoAltRef gates automatic alternate-reference scheduling. When true,
	// LookaheadFrames > 1, and ErrorResilient is false, the driver inserts
	// hidden alt-ref frames from the lookahead window and flips the
	// alt-ref sign bias on the matching deferred show frame. Mirrors
	// libvpx's oxcf.play_alternate.
	AutoAltRef bool
	// AdaptiveKeyFrames enables one-pass scene-cut detection. When a large
	// source/reference error shift is detected, the frame is promoted to a
	// keyframe before rate control and mode decision run; non-realtime
	// one-pass encodes also mirror libvpx's post-inter auto-key recode when
	// the committed inter-mode map crosses the intra-percentage thresholds.
	AdaptiveKeyFrames bool

	// ErrorResilient writes frames that reset inter-frame entropy adaptation.
	ErrorResilient bool
	// ErrorResilientPartitions mirrors libvpx's
	// VPX_ERROR_RESILIENT_PARTITIONS bit (set via `--error-resilient=2` or
	// `--error-resilient=3`). It enables the independent-prev-coef-context
	// algorithm in vp8_update_coef_probs (bitstream.c:879-928) so a lost
	// partition cannot corrupt the per-context coefficient-probability
	// tables. Off by default; the plain ErrorResilient bool maps to
	// VPX_ERROR_RESILIENT_DEFAULT, which does NOT touch the per-context
	// algorithm.
	ErrorResilientPartitions bool
	// TokenPartitions is VP8's token partition selector: 0=one, 1=two, 2=four, 3=eight.
	TokenPartitions int

	// FastLoopFilterPick switches the loop-filter fast-pick gate to the
	// partial-frame trial picker whenever speed >= 4 (libvpx uses > 4).
	// Off by default. Opt-in departure from libvpx parity that recovers
	// ~20-30% of EncodeInto wall time at cold-start
	// realtime+positive-cpu_used. Turn on only after verifying the
	// resulting loop-filter level drift is acceptable for the use case.
	FastLoopFilterPick bool
	// Sharpness is the VP8 loop-filter sharpness level in [0, 7].
	Sharpness int
	// NoiseSensitivity mirrors libvpx's VP8E_SET_NOISE_SENSITIVITY: 0=off,
	// 1=Y denoise, 2=YUV denoise, 3..6=more aggressive YUV denoise.
	NoiseSensitivity int
	// ARNRMaxFrames is the alt-ref noise reduction temporal window in frames.
	// Mirrors libvpx's VP8E_SET_ARNR_MAXFRAMES. Zero disables ARNR.
	ARNRMaxFrames int
	// ARNRStrength is the alt-ref noise reduction filter strength in [0, 6].
	// Mirrors libvpx's VP8E_SET_ARNR_STRENGTH.
	ARNRStrength int
	// ARNRType selects the alt-ref filter direction: 1=backward, 2=forward,
	// 3=centered. Mirrors libvpx's VP8E_SET_ARNR_TYPE. Zero defaults to 3
	// (centered), matching libvpx.
	ARNRType int
	// TwoPassStats enables second-pass VBR planning when non-empty.
	TwoPassStats []FirstPassFrameStats
	// TwoPassVBRBiasPct controls second-pass VBR bias when stats are present.
	TwoPassVBRBiasPct int
	// TwoPassMinPct sets the second-pass minimum section bitrate percentage.
	TwoPassMinPct int
	// TwoPassMaxPct sets the second-pass maximum section bitrate percentage.
	TwoPassMaxPct int
	// ScreenContentMode mirrors libvpx's VP8E_SET_SCREEN_CONTENT_MODE:
	// 0=off, 1=on, 2=on with more aggressive rate control.
	ScreenContentMode int
	// RTCExternalRateControl mirrors libvpx's VP8E_SET_RTC_EXTERNAL_RATECTRL.
	// For VP8 this disables cyclic refresh and post-encode overshoot recode
	// while keeping rate-correction-factor updates active.
	RTCExternalRateControl bool
	// StaticThreshold mirrors libvpx's VP8E_SET_STATIC_THRESHOLD /
	// oxcf.encode_breakout for first-pass and inter-frame static skips.
	StaticThreshold int

	// PhaseStats, when non-nil, receives coarse per-attempt encoder phase
	// timings and SAD/subpel hot-path counters during EncodeInto. The
	// caller owns the pointed-to value and may [EncoderPhaseStats.Reset]
	// it between warmup and measured passes. Leave nil in normal builds
	// to skip all clock reads and counter updates.
	PhaseStats *EncoderPhaseStats
}

// EncodeResult is the value returned by [VP8Encoder.EncodeInto] and
// [VP8Encoder.FlushInto] for one call. Data is empty when the call
// produced no output (frame dropped by rate control or buffered by
// lookahead). PTS and Duration echo the caller-supplied values; the
// rate-control and temporal-layer accounting fields are populated even
// when Data is empty so callers can drive feedback loops on dropped
// frames.
type EncodeResult struct {
	// Data aliases the caller-provided output buffer passed to EncodeInto or
	// FlushInto. Copy it if it must outlive that buffer.
	Data []byte

	// KeyFrame reports whether Data is a VP8 key frame.
	KeyFrame bool
	// Dropped reports that rate control intentionally emitted no packet.
	Dropped bool
	// Droppable reports libvpx's encoded-frame discardability signal: true
	// when the frame updates no reference, entropy, or segmentation state.
	Droppable bool
	// SceneCut reports that adaptive or two-pass scene-cut logic promoted this
	// frame to a keyframe.
	SceneCut bool
	// LookaheadDepth reports queued future frames remaining after this frame.
	LookaheadDepth int
	// ARNRFiltered reports that the frame used alt-ref noise reduction.
	ARNRFiltered bool
	// Denoised reports that denoiser preprocessing changed the source.
	Denoised bool
	// FirstPassStats is populated from TwoPassStats when second-pass planning
	// drives this frame.
	FirstPassStats FirstPassFrameStats
	// TwoPassFrameTargetBits reports the second-pass VBR target when
	// TwoPassStats drives the frame.
	TwoPassFrameTargetBits int

	// PTS and Duration echo the caller-provided frame timing values.
	PTS      uint64
	Duration uint64

	// Quantizer is the public 0..63 quantizer. InternalQuantizer is the VP8
	// base qindex reported by libvpx's VP8E_GET_LAST_QUANTIZER control.
	Quantizer         int
	InternalQuantizer int

	// SizeBytes is len(Data) for emitted frames.
	SizeBytes int

	// TargetBitrateKbps is the active total target bitrate.
	TargetBitrateKbps int
	// FrameTargetBits is the final rate-control target for this frame.
	FrameTargetBits int
	// BufferLevelBits is the post-frame rate-control buffer level.
	BufferLevelBits int

	// TemporalLayerID is the emitted frame's temporal-layer index.
	TemporalLayerID int
	// TemporalLayerCount is the active temporal layer count.
	TemporalLayerCount int
	// TemporalLayerSync reports a WebRTC-style temporal sync frame.
	TemporalLayerSync bool
	// TL0PICIDX is the temporal-layer-zero picture index.
	TL0PICIDX uint8
	// TemporalLayerTargetBitrateKbps is the instantaneous bitrate assigned to
	// TemporalLayerID.
	TemporalLayerTargetBitrateKbps int
	// TemporalLayerCumulativeBitrateKbps reports the cumulative libvpx
	// ts_target_bitrate[] entry for TemporalLayerID.
	TemporalLayerCumulativeBitrateKbps int
	// TemporalLayerFrameBandwidthBits is the per-frame budget for the layer.
	TemporalLayerFrameBandwidthBits int
	// TemporalLayerBufferLevelBits is the post-frame layer buffer level.
	TemporalLayerBufferLevelBits int
	// TemporalLayerMaximumBufferBits is the layer buffer cap.
	TemporalLayerMaximumBufferBits int
	// TemporalLayerInputFrames counts input frames assigned to the layer.
	TemporalLayerInputFrames int
	// TemporalLayerEncodedFrames counts non-dropped frames assigned to the layer.
	TemporalLayerEncodedFrames int
	// TemporalLayerTotalEncodedFrames counts all encoded frames up to this frame.
	TemporalLayerTotalEncodedFrames int
	// TemporalLayerEncodedBits accumulates layer output bits.
	TemporalLayerEncodedBits int

	// PSNRHint is reserved for a future per-frame quality hint. It is
	// not currently populated and always reads as zero.
	PSNRHint float64
}

// VP8Encoder encodes caller-provided I420 images into raw VP8 frame
// payloads. A VP8Encoder is not safe for concurrent use by multiple
// goroutines; the optional worker pool selected by EncoderOptions.Threads
// is internal to a single encode call.
type VP8Encoder struct {
	opts EncoderOptions

	timing   timingState
	rc       rateControlState
	temporal temporalState

	closed        bool
	forceKeyFrame bool
	frameCount    uint64

	lastQuantizerPublic   int
	lastQuantizerInternal int
	lastQuantizerValid    bool

	// libvpx vp8_auto_select_speed (rdopt.c:261) state. Mirrored exactly:
	// at realtime+positive-cpu_used, at the start of each encode_mb_row
	// libvpx runs vp8_auto_select_speed, which evolves cpi->Speed in
	// [4,16] based on avg_pick_mode_time and avg_encode_time vs the
	// (1e6/framerate)*(16-cpu_used)/16 budget. After each frame's encode
	// the timers IIR-update (7*avg + duration)>>3. Cold start: Speed=4,
	// timers=0. Govpx tracks the same state and feeds e.autoSpeed to
	// libvpxCPUUsed for realtime+positive-cpu_used branches.
	autoSpeed             int // current adaptive Speed (libvpx cpi->Speed)
	avgPickModeTime       int // microseconds (libvpx cpi->avg_pick_mode_time, signed int per onyx_int.h)
	avgEncodeTime         int // microseconds (libvpx cpi->avg_encode_time, signed int per onyx_int.h)
	autoSpeedFrameStartNS int64

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
	roi              roiMapState
	activityMap      []uint32
	activityAvg      uint32
	activityMapValid bool

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

	// framePredictionError mirrors libvpx's cpi->mb.prediction_error for
	// the current inter encode attempt. The overshoot-drop gate reads it
	// before the caller updates lastPredErrorMB, matching onyx_if.c.
	framePredictionError int64

	// lastPredErrorMB mirrors libvpx's cpi->last_pred_err_mb
	// (vp8/encoder/onyx_int.h). After every non-key frame, libvpx records
	// `cpi->mb.prediction_error / cpi->common.MBs` into this field at
	// vp8/encoder/onyx_if.c:3978-3980; the next frame's
	// vp8_drop_encodedframe_overshoot consults it via the
	// `pred_err_mb > 2 * last_pred_err_mb` gate. govpx does not yet
	// accumulate cpi->mb.prediction_error during inter mode picking, so
	// this field stays at 0; the overshoot drop's inner gate therefore
	// never fires until pred_err tracking lands. Outer state management
	// (frames_since_last_drop_overshoot, force_maxqp clears) runs
	// regardless, matching libvpx for the common no-drop case.
	lastPredErrorMB int

	// Cross-frame inter-mode reference-frame probabilities. libvpx
	// (onyx_if.c init) seeds these with 63/128/128 and updates them after each
	// inter frame from observed mb_ref_frame counts (see
	// vp8_estimate_entropy_savings); RD scoring needs the previous-frame
	// values, not the per-frame static 128 default.
	refProbIntra                   uint8
	refProbLast                    uint8
	refProbGolden                  uint8
	refProbUseDefaultOnNextInterRD bool
	// libvpx update_rd_ref_frame_probs (onyx_if.c) adjusts the reference-frame
	// probabilities used by the *current* frame's RD scoring based on the
	// upcoming refresh policy. It tracks frames_since_golden and
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

	preprocess vp8common.FrameBuffer
	// arnrScratch is govpx's analogue of libvpx's `cpi->alt_ref_buffer`
	// (vp8/encoder/onyx_int.h, allocated in vp8/encoder/onyx_if.c with
	// VP8BORDERINPIXELS=32). When the auto-ARF driver fires the hidden
	// alt-ref encode, applyARNRFilter writes the temporal-filter output
	// into this buffer and preprocessSource redirects the encode source
	// to it (the libvpx `cpi->Source = force_src_buffer ? force_src_buffer
	// : &cpi->source->img;` branch in vp8_get_compressed_data). Every
	// downstream reader that consumes the source pixels for the ARF encode
	// — motion search, RD picker, inter-frame reconstruction, loop-filter
	// trial SSE — therefore reads the filtered pixels rather than the raw
	// lookahead source.
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
	// reconstructAboveTok is a per-frame scratch buffer reused by the
	// reconstructing key-frame and inter-frame builders to track each row's
	// above-token context without allocating per call. Sized by
	// NewVP8Encoder to encoderMacroblockCols(width) and zeroed at the start
	// of every reconstruction pass. Distinct from tokenAbove to avoid
	// aliasing with the bitstream-pack stage that consumes tokenAbove
	// downstream of the build.
	reconstructAboveTok []vp8enc.TokenContextPlanes

	// partScratch holds reusable per-token-partition byte buffers for the
	// multi-token-partition packet writer (TokenPartitions in {1,2,3} maps
	// to 2/4/8 partitions). Single-partition mode (TokenPartitions=0, the
	// default) does not consult this; the field stays zero-valued. The
	// buffers grow to len(dst) lazily on the first multi-partition frame
	// and are reused thereafter so steady-state encodes hit zero
	// allocations even with --token-parts=N>1.
	partScratch vp8enc.PartitionScratch

	interRDThreshMult       [libvpxInterModeCount]int
	interRDThreshTouched    [libvpxInterModeCount]bool
	interModeCheckFreq      [libvpxInterModeCount]int
	interModeTestHitCounts  [libvpxInterModeCount]int
	interMBsTestedSoFar     int
	interModeErrorBins      [1024]uint32
	interModeSpeedErrorBins [1024]uint32
	interRDFrameActive      bool

	// Per-frame cached baseline threshold tables for the fast/RD inter-mode
	// pickers. Libvpx initializes cpi->rd_baseline_thresh once per frame
	// from cm->base_qindex before per-MB segment quant selection, so the
	// picker thresholds are frame-level even when cyclic refresh changes the
	// macroblock's quantizer. The generation counter is bumped at each
	// beginInterRDModeDecisionFrame so we don't have to clear the table every
	// frame.
	interRDThreshBaselineGen   uint32
	interRDThreshBaselineSlots [interRDThreshBaselineSlotCount]interRDThreshBaselineSlot
	interRDFrameBaseQIndex     int
	// Per-frame search-order (refs are constant per frame) so the
	// per-MB picker doesn't recompute it in every loop body.
	interRDFrameRefSearchOrder      [4]int
	interRDFrameRefSearchOrderValid bool

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

	loopFilterPick vp8common.FrameBuffer
	loopFilterBest vp8common.FrameBuffer
	// loopFilterPickAlt is the second LF-trial scratch buffer used by the
	// full picker when running filt_low/filt_high trials concurrently at
	// Threads >= 2. Resized only when the row-worker pool exists; remains
	// zero-sized on Threads=1 so the canonical serial path stays
	// byte-identical with zero added cost.
	loopFilterPickAlt   vp8common.FrameBuffer
	loopFilterPickReady bool
	loopFilterPickLevel uint8
	loopFilterPickBest  bool
	loopFilterSegmentLF [vp8common.MaxMBSegments]int8
	reconstructModes    []vp8dec.MacroblockMode
	reconstructTokens   []vp8dec.MacroblockTokens
	dequantTables       vp8common.FrameDequantTables
	dequants            [vp8common.MaxMBSegments]vp8common.MacroblockDequant
	reconstructScratch  vp8dec.IntraReconstructionScratch
	// keyFrameCoefTokenCounts is populated by the threaded key-frame
	// reconstruction pass with the same per-thread count accumulation libvpx
	// later sums for vp8_update_coef_probs. Packet write consumes it only
	// when valid; serial keyframes keep the established row-major count walk.
	keyFrameCoefTokenCounts      vp8enc.InterCoefficientTokenCounts
	keyFrameCoefTokenCountsValid bool
	// interCoefTokenCounts is the Lane D per-frame coefficient token count
	// cache. Populated during single-threaded inter-frame accepted-MB
	// reconstruction (buildReconstructingInterFrameCoefficientsWithSegmentation)
	// and consumed by InterFramePacket.Write via PrebuiltCoefCounts so the
	// packet writer skips its own count walk. Reset at the start of each
	// reconstruction pass. interCoefTokenCountsValid gates consumption so
	// threaded reconstruct paths (which do not populate the cache) fall back
	// to recomputing during Write.
	interCoefTokenCounts      vp8enc.InterCoefficientTokenCounts
	interCoefTokenCountsValid bool
	// interCoefTokenRecords is the matching Lane D coefficient token stream.
	// Populated during the same accepted-MB walk as interCoefTokenCounts so
	// InterFramePacket.Write can emit coefficient tokens without re-walking
	// QCoeff/EOB/context state. Only valid for the single-threaded builder.
	interCoefTokenRecords      vp8enc.InterCoefficientTokenRecords
	interCoefTokenRecordsValid bool
	// interRDCoeffCache stages the winning RD candidate's post-FDCT DCT
	// inputs across the picker → accepted-path boundary in
	// selectRDInterFrameModeDecision /
	// buildReconstructingInterFrameCoefficientsWithSegmentation. Two slots
	// alternate: the picker writes each candidate's DCTs into the
	// non-winner slot, and when a candidate becomes best the slot index
	// flips (no data copy). The accepted-path reads from the winner slot
	// to skip predict + residual gather + FDCT for the winning inter mode.
	interRDCoeffCacheSlots         [2]interRDCoeffCacheState
	interRDCoeffCacheWinner        uint8
	interRDCoeffCacheScratchTarget *interRDCoeffCacheState
	loopInfo                       vp8common.LoopFilterInfo
	// loopInfoAlt is the worker-private LoopFilterInfo for the
	// parallel filt_low/filt_high trials run by pickFull at Threads >= 2.
	// vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked mutates the
	// passed *LoopFilterInfo (via InitLoopFilterFrame) so the parallel
	// trial cannot share loopInfo with the calling goroutine's trial.
	loopInfoAlt     vp8common.LoopFilterInfo
	loopFilterLevel uint8
	// Mirror libvpx vp8/encoder/onyx_if.c set_default_lf_deltas /
	// vp8/encoder/bitstream.c pack_lf_deltas: the encoder signals the
	// mode/ref loop-filter deltas with `update=1` on the very first packed
	// frame (when xd->mode_ref_lf_delta_update is 1 from set_default_lf_deltas)
	// and clears the flag after the bitstream is written, so subsequent
	// frames pack `update=0` until something changes the deltas. We mirror
	// that by tracking whether the deltas have been signaled at least once
	// and what values were last signaled; the bitstream writer compares the
	// frame's deltas against that snapshot to decide whether to emit
	// `mode_ref_lf_delta_update`. The snapshot is committed to the encoder
	// state only on the accepted attempt (see commitKeyFrameAttempt /
	// commitInterFrameAttempt) so recode iterations see consistent state.
	lfDeltasSignaledOnce     bool
	lastSignaledRefLFDeltas  [vp8common.MaxRefLFDeltas]int8
	lastSignaledModeLFDeltas [vp8common.MaxModeLFDeltas]int8
	coefProbs                vp8tables.CoefficientProbs
	// coefProbsLast/Golden/AltRef mirror libvpx vp8/encoder/onyx.h cpi->lfc_n,
	// cpi->lfc_g, cpi->lfc_a: per-reference snapshots of cm->fc.coef_probs
	// captured at the END of the most recent frame that refreshed that slot.
	// They are seeded to default at every keyframe (vp8_setup_key_frame copies
	// cpi->common.fc into all three) and updated independently per
	// refresh_last/refresh_golden/refresh_alt_ref_frame flags after a frame's
	// bitstream is packed.
	//
	// vp8_initialize_rd_consts (rdopt.c) feeds the RD picker's per-frame
	// fill_token_costs from one of these snapshots, choosing
	//
	//	l = refresh_alt_ref_frame ? lfc_a
	//	  : refresh_golden_frame  ? lfc_g
	//	  : lfc_n
	//
	// govpx's RD picker (selectRDInterFrameModeDecision via
	// buildPredictedMacroblockCoefficientsRD) needs this same selection so
	// frames that boost golden/altref score against the colder lfc_g/lfc_a
	// snapshot — which is exactly the condition that lets SPLITMV's
	// rd_threshes gate fire. See parity-close-r3-h-rd-scale.
	coefProbsLast           vp8tables.CoefficientProbs
	coefProbsGolden         vp8tables.CoefficientProbs
	coefProbsAltRef         vp8tables.CoefficientProbs
	coefProbsSnapshotsValid bool
	// rdPickerCoefProbsActive is set to one of {coefProbsLast, coefProbsGolden,
	// coefProbsAltRef} during inter-frame RD picker passes (see
	// encodeInterFrameAttempt). When non-nil, picker call sites read from it
	// instead of e.coefProbs so token costs match libvpx's per-reference
	// fill_token_costs source. nil during key-frame and committed-encode paths.
	rdPickerCoefProbsActive *vp8tables.CoefficientProbs
	modeProbs               vp8dec.ModeProbs
	mvCostTables            vp8enc.MotionVectorCostTables
	mvCostProbs             [2][vp8tables.MVPCount]uint8
	mvCostTablesValid       bool

	// threadedRowsActive marks the worker-private encoder view used by the
	// row-threaded inter-frame builder. It is false on the canonical encoder
	// state so Threads=1 remains byte-identical and does not branch into
	// threaded-only search features.
	threadedRowsActive bool

	// rowWorkers is the row-parallel encoder worker pool. Allocated
	// only when EncoderOptions.Threads >= 2 so the canonical
	// Threads=1 path stays zero-cost (no goroutine spawn, no
	// atomic ops, no channel allocation, no per-row scratch
	// allocation). Mirrors libvpx vp8/encoder/ethreading.c's
	// cpi->mb_row_ei + cpi->encoding_thread_count layout. nil at
	// Threads=1 so picker / reconstruct hot paths can branch on a
	// single nil-check before any threading code path executes.
	rowWorkers *rowWorkerPool

	// threadedDotArtifactBudget points at the row-worker pool's shared
	// per-frame dot-artifact suppression counter while threaded rows are
	// active. Nil on the serial path so the hot single-thread encoder does
	// not pay for atomic coordination.
	threadedDotArtifactBudget *atomic.Int32
}

const encoderQuantizerFeedbackMaxAttempts = 8

func (e *VP8Encoder) phaseStart() int64 {
	if e.opts.PhaseStats == nil {
		return 0
	}
	return nanotime()
}

func (e *VP8Encoder) phaseEnd(phase encoderPhase, start int64) {
	if start == 0 || e.opts.PhaseStats == nil {
		return
	}
	elapsed := nanotime() - start
	stats := e.opts.PhaseStats
	switch phase {
	case encoderPhaseInterReconstruct:
		stats.InterReconstructNS += elapsed
	case encoderPhaseKeyReconstruct:
		stats.KeyReconstructNS += elapsed
	case encoderPhaseLoopFilterPick:
		stats.LoopFilterPickNS += elapsed
	case encoderPhaseLoopFilterApply:
		stats.LoopFilterApplyNS += elapsed
	case encoderPhasePacketWrite:
		stats.PacketWriteNS += elapsed
	}
}

func (e *VP8Encoder) phaseCountAttempt(keyFrame bool) {
	if e.opts.PhaseStats == nil {
		return
	}
	if keyFrame {
		e.opts.PhaseStats.KeyAttempts++
	} else {
		e.opts.PhaseStats.InterAttempts++
	}
}

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
	refProbIntra                   uint8
	refProbLast                    uint8
	refProbGolden                  uint8
	refProbUseDefaultOnNextInterRD bool
	probSkipFalse                  uint8
	lastSkipFalseProbs             [3]uint8
	valid                          bool
}

// saveCodingContext mirrors libvpx vp8_save_coding_context. Called once before
// the encode/recode do-loop; the snapshot is consumed by restoreCodingContext
// when a recode attempt is rejected.
func (e *VP8Encoder) saveCodingContext() {
	e.savedContext = savedCodingContext{
		framesSinceKey:                 e.rc.framesSinceKeyframe,
		filterLevel:                    e.loopFilterLevel,
		framesTillGFUpdateDue:          e.rc.framesTillGFUpdateDue,
		framesSinceGolden:              e.framesSinceGolden,
		thisFramePercentIntra:          e.rc.thisFramePercentIntra,
		modeProbs:                      e.modeProbs,
		coefProbs:                      e.coefProbs,
		refProbIntra:                   e.refProbIntra,
		refProbLast:                    e.refProbLast,
		refProbGolden:                  e.refProbGolden,
		refProbUseDefaultOnNextInterRD: e.refProbUseDefaultOnNextInterRD,
		probSkipFalse:                  e.probSkipFalse,
		lastSkipFalseProbs:             e.lastSkipFalseProbs,
		valid:                          true,
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
	e.refProbUseDefaultOnNextInterRD = e.savedContext.refProbUseDefaultOnNextInterRD
	e.probSkipFalse = e.savedContext.probSkipFalse
	e.lastSkipFalseProbs = e.savedContext.lastSkipFalseProbs
}

type keyFrameEncodeAttempt struct {
	FrameCoefProbs    vp8tables.CoefficientProbs
	Size              int
	ProjectedSizeBits int
	// CoefSavingsBits and RefFrameSavingsBits expose the entropy-savings
	// breakdown that fed projectedFrameSizeBitsFromRate for this attempt.
	// Used by the oracle trace to localize entropy-savings parity gaps.
	// On key frames RefFrameSavingsBits is always 0.
	CoefSavingsBits     int
	RefFrameSavingsBits int
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
	Config            vp8enc.InterFrameStateConfig
	FrameCoefProbs    vp8tables.CoefficientProbs
	FrameYModeProbs   [vp8tables.YModeProbCount]uint8
	FrameUVModeProbs  [vp8tables.UVModeProbCount]uint8
	FrameMVProbs      [2][vp8tables.MVPCount]uint8
	RefFrame          vp8common.MVReferenceFrame
	Ref               *vp8common.Image
	Size              int
	ProjectedSizeBits int
	// Entropy-savings breakdown (see keyFrameEncodeAttempt).
	CoefSavingsBits        int
	RefFrameSavingsBits    int
	ZeroReference          bool
	CyclicRefresh          bool
	CyclicRefreshNextIndex int
}

// NewVP8Encoder creates a VP8 encoder with validated options. Allocations
// happen here, not on the hot path: per-frame buffers, the row-worker
// pool when EncoderOptions.Threads > 1, and rate-control state are sized
// for EncoderOptions.Width and EncoderOptions.Height.
//
// opts is validated up front; invalid combinations return
// [ErrInvalidConfig], [ErrInvalidBitrate], or [ErrInvalidQuantizer]
// without allocating any encoder state. Width, Height, FPS (or
// TimebaseNum/Den), and TargetBitrateKbps are required.
func NewVP8Encoder(opts EncoderOptions) (*VP8Encoder, error) {
	normalized, timing, err := normalizeEncoderOptions(opts)
	if err != nil {
		return nil, err
	}

	cfg := defaultRateControlConfig(normalized)
	e := &VP8Encoder{
		opts:               normalized,
		timing:             timing,
		coefProbs:          vp8tables.DefaultCoefProbs,
		refProbIntra:       63,
		refProbLast:        128,
		refProbGolden:      128,
		probSkipFalse:      128,
		baseSkipFalseProbs: libvpxBaseSkipFalseProbs,
	}
	if err := e.reallocateForDimensions(normalized.Width, normalized.Height); err != nil {
		return nil, err
	}
	e.resetInterRDThresholdMultipliers()
	vp8dec.ResetModeProbs(&e.modeProbs)
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
	e.twoPass.configureFrameDims(e.opts.Width, e.opts.Height)
	if err := e.ensureRowWorkerPool(normalized.Width, normalized.Height); err != nil {
		return nil, err
	}
	return e, nil
}
