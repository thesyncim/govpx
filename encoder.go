package govpx

import (
	"errors"
	"io"
	"os"
	_ "unsafe" // for go:linkname

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// nanotime returns the monotonic clock in nanoseconds. Linked to
// runtime.nanotime to avoid time.Now()'s per-call wall+mono allocation.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

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

	// Threads selects the worker-goroutine count for the per-frame
	// macroblock pipeline. Mirrors libvpx's VP8E_SET_TOKEN_PARTITIONS / the
	// `--threads` vpxenc flag and the `cpi->oxcf.multi_threaded` knob in
	// vp8/encoder/onyx_if.c.
	//
	// Semantics:
	//   - 0 (default in zero-initialized opts) is treated as 1: a single
	//     goroutine drives the macroblock loop, byte-identical to the
	//     historical govpx encoder.
	//   - 1 is the pinned single-threaded reference path. All 16 oracle
	//     scoreboards and parity tests run with this default; the
	//     committed bitstream and reconstructed pixels are the canonical
	//     output the parity baselines lock against.
	//   - Values >1 are accepted by the configuration validator and
	//     reserved for the libvpx-style row-threaded macroblock pipeline
	//     (see internal/coracle/build/libvpx-v1.16.0/vp8/encoder/ethreading.c
	//     thread_encoding_proc and vp8cx_create_encoder_threads). The
	//     current encoder collapses any Threads >= 1 onto the same serial
	//     loop because the per-MB adaptive RD-threshold state
	//     (interRDThreshMult, interMBsTestedSoFar, interModeErrorBins,
	//     consecZeroLastMVBias, dotArtifactChecked, mbsZeroLastDotSuppress)
	//     is read and mutated in raster order during mode decision; libvpx
	//     copies that state per worker (setup_mbby_copy in
	//     vp8/encoder/ethreading.c) and accepts that the threaded encoder
	//     produces a different bitstream than the single-threaded one.
	//     govpx's parity scoreboards pin against the single-threaded
	//     output, so a future row-threaded path must either land behind
	//     the same flag (and refresh baselines) or stage the adaptive RD
	//     state into per-row shadows that the post-row merge replays in
	//     deterministic order. The validator accepts the value today so
	//     callers can plumb the option through and so cmd/govpx-bench /
	//     scripts can sweep it as soon as the parallel implementation
	//     lands; until then the bench-reported ns/frame is independent of
	//     this field.
	//   - A negative value is rejected with ErrInvalidConfig.
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
	// DropFrameWaterMark mirrors libvpx's rc_dropframe_thresh / oxcf
	// drop_frames_water_mark (vpx_codec_enc_cfg_t). It is the
	// percentage of optimal_buffer_level at which the libvpx decimation
	// drop branch (vp8_check_drop_buffer in vp8/encoder/onyx_if.c)
	// starts engaging the 1->2->3 decimation factor ladder. When
	// DropFrameAllowed is true and this is zero, govpx defaults to 60
	// (the typical realtime CBR knob). When DropFrameAllowed is false,
	// no decimation ever fires.
	DropFrameWaterMark int

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

	// OracleTracePredictorDump enables per-MB inter-prediction predictor
	// rows in the oracle trace. When true (and OracleTraceWriter is non-nil)
	// the encoder writes one "predictor" row per Y/U/V plane for MB(0,0) of
	// each inter frame, capturing the post-sub-pel pre-residual buffer.
	// Mirrors the libvpx-side GOVPX_ORACLE_PREDICTOR_DUMP env-var gate; off
	// by default since the diagnostic only fires for chroma sub-pel rounding
	// gap localization at sizes >64x64. No effect on encoded bytes.
	OracleTracePredictorDump bool

	// OracleTracePredictorDumpAllRows widens the predictor/reconstructed
	// dump scope to every MB row of every inter frame (instead of MB
	// row 0 only). Has effect only when OracleTracePredictorDump is also
	// true. Mirrors the libvpx-side GOVPX_ORACLE_PREDICTOR_DUMP_ALL_ROWS
	// env-var gate. Used when chasing divergences whose root cause lives
	// outside the first MB row (e.g. a loop-filter level picker that
	// scores the partial-frame window at rows/2).
	OracleTracePredictorDumpAllRows bool

	// AutoSpeedGoOverheadCalibration toggles the Go-vs-C overhead
	// calibration applied to vp8_auto_select_speed's avg_encode_time /
	// avg_pick_mode_time inputs. Off by default to preserve parity with
	// libvpx oracle binaries (oracle scoreboards exercise the patched
	// libvpx whose per-frame wall-clock overhead matches govpx's Go
	// runtime overhead, so calibration would *break* their convergence).
	// On for cmd/govpx-bench and any production caller comparing against
	// stock libvpx.
	//
	// When on, govpx replaces the wall-clock duration that feeds the
	// auto-select IIR filter with a deterministic synthetic value
	// proportional to the frame's macroblock count
	// (libvpxAutoSpeedSynthCostUS). The synthetic duration drives
	// libvpx's auto_speed_thresh comparisons into the Speed bucket at
	// which govpx output (kbps, PSNR, interframe bytes) matches stock
	// libvpx's, independent of the host machine's actual encode speed.
	// The trajectory becomes a pure function of (cpu_used, framerate,
	// width, height) so parity tests are reproducible across boxes.
	//
	// R12-D's earlier wall-clock-scaling calibration (constant 24/10
	// ratio of govpx vs libvpx per-frame time) was tuned for 720p only
	// and broke at 1080p where govpx parked at Speed=6/7 instead of the
	// Speed=4 floor, driving a 1.04x interframe-byte / -0.31 dB PSNR
	// divergence (R13). The synthetic-duration model fixes that by
	// scaling implicitly with MBs/frame so the Speed=4 floor holds at
	// every supported resolution.
	//
	// Cold-start (avg_pick_mode_time==0) and the explicit-Speed branches
	// are unaffected.
	AutoSpeedGoOverheadCalibration bool
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

	// Per-MB snapshots of the picker mutator state, used by the cyclic-
	// refresh segment-fallback path so the segmentID=0 fallback picker
	// call does not see the segmentID-guess call's mutations. Stored on
	// the encoder (not on the stack) so the fallback path stays
	// zero-alloc; libvpx runs the picker once per MB regardless of
	// segment, so this snapshot/restore mirrors that single-call
	// invariant. See encoder_reconstruct.go encodeInterFrameAttempt's
	// per-MB loop.
	interRDThreshMultSnapshot      [libvpxInterModeCount]int
	interRDThreshTouchedSnapshot   [libvpxInterModeCount]bool
	interModeTestHitCountsSnapshot [libvpxInterModeCount]int
	interMBsTestedSoFarSnapshot    int

	// Per-frame cached baseline threshold tables for the fast/RD inter-mode
	// pickers. Within a frame the only input that changes per-MB is qIndex
	// (via cyclic-refresh segmentation), so the baseline output of
	// libvpxInterModeRDThresholdsForContext is invariant per qIndex. The
	// generation counter is bumped at each beginInterRDModeDecisionFrame so
	// we don't have to clear the table every frame.
	interRDThreshBaselineGen   uint32
	interRDThreshBaselineSlots [interRDThreshBaselineSlotCount]interRDThreshBaselineSlot
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

	loopFilterPick     vp8common.FrameBuffer
	reconstructModes   []vp8dec.MacroblockMode
	reconstructTokens  []vp8dec.MacroblockTokens
	dequantTables      vp8common.FrameDequantTables
	dequants           [vp8common.MaxMBSegments]vp8common.MacroblockDequant
	reconstructScratch vp8dec.IntraReconstructionScratch
	loopInfo           vp8common.LoopFilterInfo
	loopFilterLevel    uint8
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

	// rowWorkers is the row-parallel encoder worker pool. Allocated
	// only when EncoderOptions.Threads >= 2 so the canonical
	// Threads=1 path stays zero-cost (no goroutine spawn, no
	// atomic ops, no channel allocation, no per-row scratch
	// allocation). Mirrors libvpx vp8/encoder/ethreading.c's
	// cpi->mb_row_ei + cpi->encoding_thread_count layout. nil at
	// Threads=1 so picker / reconstruct hot paths can branch on a
	// single nil-check before any threading code path executes.
	rowWorkers *rowWorkerPool
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
		reconstructAboveTok:     make([]vp8enc.TokenContextPlanes, encoderMacroblockCols(normalized.Width)),

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
	e.twoPass.configureFrameDims(e.opts.Width, e.opts.Height)
	// Allocate the row-parallel worker pool only when Threads >= 2.
	// Threads=1 stays byte-identical and zero-cost: no pool, no
	// goroutines, no atomic ops, no per-row scratch allocation.
	// Mirrors libvpx vp8cx_create_encoder_threads early return when
	// cpi->oxcf.multi_threaded < 2.
	if eff := e.effectiveThreadCount(); eff >= 2 {
		mbRows := encoderMacroblockRows(e.opts.Height)
		mbCols := encoderMacroblockCols(e.opts.Width)
		e.rowWorkers = newRowWorkerPool(eff, mbRows, mbCols)
	}
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
	// libvpx vp8/encoder/encodeframe.c:685-691 -- vp8_auto_select_speed runs
	// at the top of encode_mb_row for realtime+positive-cpu_used, evolving
	// cpi->Speed based on cumulative timing. Mirror the same call point.
	e.libvpxAutoSelectSpeed()
	e.autoSpeedFrameStartNS = nowMonotonicNS()
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
	// libvpx vp8/encoder/onyx_if.c vp8_check_drop_buffer adjusts
	// cpi->decimation_factor from the post-encode buffer level of the
	// previous frame BEFORE vp8_pick_frame_size / vp8_regulate_q runs, then
	// boosts cpi->per_frame_bandwidth (1->3/2, 2->5/4, 3->5/4) so the
	// boosted target flows through calc_pframe_target_size into
	// vp8_regulate_q. Mirror that ordering here: refresh the decimation
	// factor first, then feed the boosted bits-per-frame into
	// beginFrameWithTargetAndContext so the rate-control regulator sees the
	// same target-size baseline as libvpx on frames that follow a
	// decimation drop.
	e.rc.prepareDecimationForFrame()
	// Decimation drop check runs BEFORE beginFrameWithTargetAndContext to
	// mirror libvpx's encode_frame_to_data_rate ordering exactly: libvpx
	// calls vp8_check_drop_buffer at the top of the function (line 3561 in
	// vp8/encoder/onyx_if.c) and returns BEFORE vp8_pick_frame_size /
	// calc_pframe_target_size run. calc_pframe_target_size is what drains
	// kf_overspend_bits / gf_overspend_bits via the
	// kf_bitrate_adjustment / non_gf_bitrate_adjustment per-frame
	// drains; if we drained those before deciding to drop, libvpx does
	// not, and the post-drop frames see a depleted overspend pool, which
	// pulls this_frame_target up (because applyOnePassPFrameOverspendRecovery
	// has less left to subtract) and pulls the regulated Q down. Closing
	// this gap is what fixes post_drop_q_max_drift on the 30f tight-buffer
	// CBR fixture (govpx Q ran 8-10 indices below libvpx because
	// kf_overspend was draining on every dropped frame too).
	if !invisible && e.rc.checkDropBuffer(keyFrame) {
		e.rc.postDecimationDropFrame()
		e.twoPass.finishFrame(0)
		e.forceKeyFrame = false
		// libvpx's decimation drop does NOT set force_maxqp: only the
		// post-encode overshoot drop does that. Mirror that exactly so
		// the next inter frame's Q regulation runs through the normal
		// path instead of being clamped at max-Q. cyclicRefresh
		// suppression also belongs to overshoot drops only.
		droppedResult := EncodeResult{
			Dropped:                            true,
			BufferLevelBits:                    e.rc.bufferLevelBits,
			FrameTargetBits:                    e.rc.frameTargetBits,
			TargetBitrateKbps:                  e.rc.targetBitrateKbps,
			PTS:                                pts,
			Duration:                           duration,
			TemporalLayerID:                    temporalFrame.LayerID,
			TemporalLayerCount:                 temporalFrame.LayerCount,
			TemporalLayerSync:                  temporalFrame.LayerSync,
			TL0PICIDX:                          temporalFrame.TL0PICIDX,
			TemporalLayerTargetBitrateKbps:     temporalFrame.LayerTargetBitrateKbps,
			TemporalLayerCumulativeBitrateKbps: temporalFrame.LayerCumulativeBitrateKbps,
		}
		e.temporal.finishDroppedFrame(temporalFrame, e.temporalBufferConfig())
		e.populateTemporalLayerBufferResult(&droppedResult, temporalFrame)
		e.emitOracleDroppedFrameTrace("decimation")
		e.frameCount++
		finishSourceAltRef()
		return droppedResult, nil
	}
	if temporalFrame.Enabled && !keyFrame {
		e.rc.beginFrameWithTargetAndContext(false, temporalFrame.LayerFrameTargetBits, rateControlFrameContext{
			temporalLayerCount: temporalFrame.LayerCount,
			timing:             e.timing,
		})
	} else {
		e.rc.beginFrameWithTargetAndContext(keyFrame, e.rc.decimationBoostedBitsPerFrame(), rateControlFrameContext{
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
	// libvpx vp8/encoder/firstpass.c vp8_second_pass first-frame branch:
	// estimate_max_q sets cpi->active_worst_quality. Push the seeded
	// override into the rate controller so the regulator's worst-Q
	// ceiling matches libvpx for the upcoming Q regulation. Without
	// this, the regulator picks Q values much lower than libvpx for
	// the same per-frame target on real-content pass-2 fixtures
	// (q_match=8% on desktopqvga while target_match=100%).
	if q, ok := e.twoPass.pass2ActiveWorstQOverride(); ok {
		e.rc.pass2ActiveWorstQOverride = q
		e.rc.pass2ActiveWorstQValid = true
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
	// libvpx vp8/encoder/ratectrl.c vp8_regulate_q forces Q to
	// `cpi->worst_quality` (the configured maxQuantizer) on the next frame
	// after vp8_drop_encodedframe_overshoot fires - the post-encode
	// overshoot drop signals the next frame to ramp Q to the floor of
	// quality so the buffer can recover. govpx must mirror that override
	// after the regulator has settled, otherwise the overshoot-drop signal
	// is observed (cyclic refresh suppression) but the next frame's Q is
	// still picked from the rate model and undoes the buffer recovery.
	if e.forceMaxQuantizer {
		e.rc.currentQuantizer = e.rc.maxQuantizer
		e.rc.currentZbinOverQuant = 0
	}
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
	// Decimation drop check moved earlier (before beginFrameWithTargetAndContext)
	// to mirror libvpx's vp8_check_drop_buffer ordering. The buffer-underrun
	// drop below stays here because libvpx checks it INSIDE
	// calc_pframe_target_size (i.e. after the kf_overspend drain).
	if !keyFrame && !invisible && e.rc.shouldDropInterFrame() {
		e.rc.postDropFrame()
		e.twoPass.finishFrame(0)
		result.Dropped = true
		result.BufferLevelBits = e.rc.bufferLevelBits
		e.forceKeyFrame = false
		// libvpx's buffer-underrun drop in vp8/encoder/ratectrl.c
		// calc_pframe_target_size only sets cpi->drop_frame=1 and updates
		// the buffer level - it does NOT touch cpi->force_maxqp. force_maxqp
		// is the post-encode-overshoot signal from vp8_drop_encodedframe_overshoot
		// (a different drop path with screen_content_mode==2 / drop_frames_allowed
		// gating). Setting forceMaxQuantizer here on the buffer-underrun
		// branch therefore spuriously disables cyclic refresh on the frame
		// after a buffer-underrun drop (cyclicRefreshModeEnabled gates on
		// !forceMaxQuantizer, mirroring libvpx's force_maxqp==0 check).
		e.temporal.finishDroppedFrame(temporalFrame, e.temporalBufferConfig())
		e.populateTemporalLayerBufferResult(&result, temporalFrame)
		// Oracle trace: emit a dropped-frame row before frameCount advances.
		// libvpx's parity oracle emits the same row from
		// build_vpxenc_oracle.sh at the buffer-underrun return path inside
		// encode_frame_to_data_rate. govpx's drop trigger
		// (rc.shouldDropInterFrame) gates on bufferLevelBits<0 which is the
		// libvpx-equivalent calc_pframe_target_size buffer-underrun branch,
		// so the reason is "buffer_underrun".
		e.emitOracleDroppedFrameTrace("buffer_underrun")
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
		// libvpx vp8/encoder/onyx_if.c:3970-3982 runs
		// vp8_drop_encodedframe_overshoot after vp8_encode_frame on
		// one-pass CBR. When it returns 1 the encoded frame is discarded
		// and the next frame is forced to max-Q via cpi->force_maxqp.
		// The function only fires under screen_content_mode==2 or with
		// drop_frames_allowed plus a starved rate-correction-factor; for
		// the common non-screen-content / drop-disabled config it just
		// advances frames_since_last_drop_overshoot so the rcf-watchdog
		// branch can arm next time.
		if !invisible && e.vp8DropEncodedframeOvershoot(e.rc.currentQuantizer, attempt.Size, required, false) {
			e.twoPass.finishFrame(0)
			result.Dropped = true
			result.SizeBytes = 0
			result.BufferLevelBits = e.rc.bufferLevelBits
			result.FrameTargetBits = e.rc.frameTargetBits
			e.forceKeyFrame = false
			// libvpx: cpi->frames_since_key++ on overshoot drop; mirror
			// it so the next-keyframe distance heuristic stays aligned.
			e.rc.framesSinceKeyframe++
			e.temporal.finishDroppedFrame(temporalFrame, e.temporalBufferConfig())
			e.populateTemporalLayerBufferResult(&result, temporalFrame)
			e.emitOracleDroppedFrameTrace("overshoot")
			e.frameCount++
			finishSourceAltRef()
			return result, nil
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
			e.rc.beginFrameWithTargetAndContext(true, e.rc.decimationBoostedBitsPerFrame(), rateControlFrameContext{
				temporalLayerCount: temporalFrame.LayerCount,
				timing:             e.timing,
			})
			twoPassTargetBits = e.twoPass.frameTargetBits(e.frameCount, true, e.rc.frameTargetBits)
			if twoPassTargetBits > 0 {
				e.rc.frameTargetBits = twoPassTargetBits
				e.rc.frameTargetBits = e.rc.applyPass2CBRBufferAdjustment(e.rc.frameTargetBits, true)
			}
			e.rc.selectQuantizerForFrameKindWithScreenContent(true, false, required, e.opts.ScreenContentMode)
			// Same force_maxqp regulator gate as the primary path
			// above: if the prior frame's overshoot drop set the flag,
			// libvpx vp8_regulate_q honors it on the next frame
			// regardless of frame type, including a scene-cut KF
			// promoted from this auto-key recode path.
			if e.forceMaxQuantizer {
				e.rc.currentQuantizer = e.rc.maxQuantizer
				e.rc.currentZbinOverQuant = 0
			}
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
			e.emitOracleRateAndRecodeTrace(vp8common.InterFrame, finalQuantizer, attempt.Size, attempt.ProjectedSizeBits, attempt.CoefSavingsBits, attempt.RefFrameSavingsBits)
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
				e.finishAutoSpeedTiming(false)
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
	// libvpx vp8/encoder/ratectrl.c vp8_setup_key_frame seeds the next GF
	// section countdown to baseline_gf_interval and asserts
	// refresh_golden_frame=1 / refresh_alt_ref_frame=1 on every key frame
	// before encoding. update_golden_frame_stats reads this on the
	// post-encode path to compute non_gf_bitrate_adjustment =
	// gf_overspend_bits / frames_till_gf_update_due, which the next inter
	// frame's calc_pframe_target_size drains. Without seeding it here,
	// govpx's CBR / multi-keyframe paths leave frames_till_gf_update_due at
	// 0 across the keyframe boundary, so non_gf_bitrate_adjustment stays at
	// 0 and the gf_overspend_bits drain never fires - causing per-frame
	// target bits to drift higher than libvpx's, which lowers Q on the
	// inter-recode path at good-quality cpu5 128x128.
	//
	// libvpx onyx_if.c sets baseline_gf_interval to gf_interval_onepass_cbr
	// (==goldenFrameCBRInterval below) for realtime CBR but resets it back
	// to DEFAULT_GF_INTERVAL on subsequent vp8_change_config invocations
	// that don't take the realtime branch (line 1547). vpxenc invokes
	// vp8_change_config after vp8_create_compressor, so good-quality CBR
	// observes baseline_gf_interval=DEFAULT_GF_INTERVAL=7 at first-keyframe
	// time while realtime CBR observes the cyclic-refresh gf_interval.
	e.rc.framesTillGFUpdateDue = e.libvpxKeyFrameSetupGFInterval(rows, cols)
	keyAttempt, err := e.encodeKeyFrameWithQuantizerFeedback(dst, source, rows, cols, required, invisible, staticSegmentationAllowed)
	if err != nil {
		return EncodeResult{}, err
	}
	finalQuantizer := e.rc.currentQuantizer
	e.commitKeyFrameEntropy(keyAttempt)
	// Mirror libvpx onyx_if.c key-frame branch: zero frames_since_golden,
	// drop source_alt_ref_active when no ARF schedule is pending, and
	// decrement frames_till_alt_ref_frame. Carried out by
	// `refreshKeyFrameReferencesFromAnalysis -> resetGoldenFrameStats`,
	// which is the single keyframe-path call point. Calling it twice
	// (legacy code did) would double-decrement framesTillAltRefFrame and
	// silently shorten any pass2-armed ARF schedule.
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
	e.emitOracleRateAndRecodeTrace(vp8common.KeyFrame, finalQuantizer, keyAttempt.Size, keyAttempt.ProjectedSizeBits, keyAttempt.CoefSavingsBits, keyAttempt.RefFrameSavingsBits)
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
	// libvpx vp8/encoder/onyx_if.c does NOT reset cyclic_refresh_mode_index
	// on key frames — only on init/resize (see lines 1213/1870 vs the
	// frame_type != KEY_FRAME gate around the loop at line 534). The
	// persistent index is preserved so the first inter frame after each
	// keyframe continues the rolling refresh from where it left off.
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
	e.finishAutoSpeedTiming(true)
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
		// libvpx gates the size-recode branch on cpi->sf.recode_loop in
		// recode_loop_test (vp8/encoder/onyx_if.c). Mode 2 (realtime) and
		// good-quality cpu_used >= 4 set recode_loop=0 in set_speed_features,
		// so libvpx accepts the regulator's first Q and lets the
		// rate-correction-factor reconcile across subsequent frames. govpx
		// mirrors that gate via libvpxKeyFrameRecodeLoopActive; the
		// recode_loop_test in libvpx itself feeds the pre-pack
		// `cpi->projected_frame_size` (totalrate>>8 minus entropy savings)
		// from vp8_encode_frame, which is what result.ProjectedSizeBits
		// already mirrors.
		if !e.libvpxKeyFrameRecodeLoopActive() {
			return result, nil
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
	lfLevel, err = e.pickLoopFilterLevel(source, vp8common.KeyFrame, lfLevel, lfSharpness, rows, cols, required, segmentation)
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
		LFDeltaUpdate:       e.computeLFDeltaUpdateBit(lfHeader.DeltaEnabled, lfHeader.RefDeltas, lfHeader.ModeDeltas),
		RefLFDeltas:         lfHeader.RefDeltas,
		ModeLFDeltas:        lfHeader.ModeDeltas,
		Segmentation:        segmentation,
		RefreshEntropyProbs: true,
		IndependentContexts: e.opts.ErrorResilientPartitions,
	}
	n, frameCoefProbs, err := vp8enc.WriteCoefficientKeyFrameWithProbabilityBaseScratch(dst, e.opts.Width, e.opts.Height, cfg, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs, &e.partScratch)
	if err != nil {
		return keyFrameEncodeAttempt{}, translateEncoderError(err)
	}
	projectedBits, coefSavings, refFrameSavings := e.projectedFrameSizeBitsFromRateWithSavings(true, required, projectedRate, false, false)
	return keyFrameEncodeAttempt{FrameCoefProbs: frameCoefProbs, Size: n, ProjectedSizeBits: projectedBits, CoefSavingsBits: coefSavings, RefFrameSavingsBits: refFrameSavings, LoopFilterLevel: lfLevel, SharpnessLevel: lfSharpness, LFDeltaEnabled: cfg.LFDeltaEnabled, LFDeltaUpdate: cfg.LFDeltaUpdate, RefLFDeltas: cfg.RefLFDeltas, ModeLFDeltas: cfg.ModeLFDeltas, RefreshEntropyProbs: cfg.RefreshEntropyProbs, SegmentationEnabled: segmentation.Enabled}, nil
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
// speed-feature table at vp8/encoder/onyx_if.c set_speed_features and the
// recode_loop_test in the same file. The libvpx mapping is:
//
//   - Mode == 2 (realtime):                       recode_loop = 0 (off)
//   - Mode == 1 (good), Speed in 0..2:            recode_loop = 1 (recode all)
//   - Mode == 1 (good), Speed == 3:               recode_loop = 2 (KF/GF/AR only)
//   - Mode == 1 (good), Speed >= 4:               recode_loop = 0 (off)
//   - Mode == 0 (best):                           recode_loop = 1 (recode all)
//
// recode_loop_test returns true when:
//   - recode_loop == 1, OR
//   - recode_loop == 2 AND (KEY || refresh_golden || refresh_alt_ref)
//
// govpx encodes the KF path separately, so this helper covers the inter
// branch only. boostedReferenceFrame mirrors `(cm->refresh_golden_frame
// || cm->refresh_alt_ref_frame)`.
func (e *VP8Encoder) libvpxInterRecodeLoopActive(boostedReferenceFrame bool) bool {
	if e == nil {
		return true
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return false
	case DeadlineGoodQuality:
		speed := e.libvpxCPUUsed()
		switch {
		case speed <= 2:
			return true
		case speed == 3:
			return boostedReferenceFrame
		default:
			return false
		}
	default:
		return true
	}
}

// libvpxKeyFrameRecodeLoopActive mirrors recode_loop_test for KEY_FRAME:
// the size_recode branch fires when recode_loop is 1 (or 2, since KEY_FRAME
// satisfies the second clause). At realtime (recode_loop=0) and good-quality
// cpu_used >= 4 (also recode_loop=0), libvpx skips KF size recoding entirely
// and accepts the regulator's first Q. The forced-KF SS-error special path
// (vp8_special_case_for_forced_key_frame) is independent of recode_loop and
// is gated separately at the call site.
func (e *VP8Encoder) libvpxKeyFrameRecodeLoopActive() bool {
	if e == nil {
		return true
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return false
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() <= 3
	default:
		return true
	}
}

// vp8DropEncodedframeOvershoot ports vp8/encoder/ratectrl.c
// vp8_drop_encodedframe_overshoot: a post-encode drop that fires when an
// inter frame at low Q badly overshoots the buffer budget on screen-
// content / drop-frame-allowed configurations. When it fires, the encoded
// frame is discarded, the buffer is reset to the optimal level, the
// rate-correction-factor is bumped (capped at 2x or MAX_BPB_FACTOR), and
// `cpi->force_maxqp` is set so the next frame is forced to worst_quality.
//
// libvpx's call site (vp8/encoder/onyx_if.c:3970-3982) is gated on
// `pass==0 AND end_usage==USAGE_STREAM_FROM_SERVER AND
// rt_drop_recode_on_overshoot==1`. govpx invokes this only for non-key
// inter frames in CBR mode (the libvpx-equivalent of one-pass CBR).
//
// The inner drop test requires `pred_err_mb > thresh_pred_err_mb` and
// `pred_err_mb > 2 * cpi->last_pred_err_mb`. govpx does not yet
// accumulate `cpi->mb.prediction_error` during inter mode picking, so
// `lastPredErrorMB` is permanently 0 and the inner gate currently never
// fires. The outer state management
// (frames_since_last_drop_overshoot increment, force_maxqp clears) runs
// regardless and matches libvpx for the common no-drop case; that keeps
// the gate ready for a future pred-err-tracking patch.
//
// Inputs: Q is the frame's chosen quantizer, projectedSizeBytes is the
// final packed bitstream length (libvpx's
// `cpi->projected_frame_size = (*size) << 3` post-pack value, here in
// bytes for convenience), macroblocks is the frame MB count, and
// keyFrame skips the gate so libvpx's `frame_type != KEY_FRAME` check
// is honored. Returns true when the caller must discard the frame.
func (e *VP8Encoder) vp8DropEncodedframeOvershoot(Q int, projectedSizeBytes int, macroblocks int, keyFrame bool) bool {
	if e == nil {
		return false
	}
	// Only fires in one-pass CBR with the rt-drop-recode signal active.
	// libvpx's `cpi->rt_drop_recode_on_overshoot` is enabled by default and
	// only cleared when an external rate controller takes over (see
	// vp8/vp8_cx_iface.c VP8E_SET_RTC_EXTERNAL_RATECTRL); govpx has no
	// external RC concept so it stays equivalent to 1 here.
	if e.rc.mode != RateControlCBR || e.twoPass.enabled() {
		return false
	}
	if keyFrame {
		// libvpx skips the function body entirely on KFs (frame_type !=
		// KEY_FRAME guard). Counters do not advance on KFs either.
		return false
	}
	// Outer gate (vp8/encoder/ratectrl.c lines ~1505-1510):
	//   screen_content_mode == 2  OR
	//   (drop_frames_allowed AND
	//    (force_drop_overshoot OR
	//     (rate_correction_factor < 8*MIN_BPB_FACTOR AND
	//      frames_since_last_drop_overshoot > framerate)))
	// govpx does not surface multi-resolution force_drop_overshoot, so
	// the inner OR collapses to the rcf+timing branch.
	rcf := e.rc.rateCorrectionFactorForFrame(false, false)
	framerate := outputFrameRate(e.timing)
	rcThresholdMet := rcf < 8.0*libvpxMinBPBFactor &&
		framerate > 0 &&
		float64(e.rc.framesSinceLastDropOvershoot) > framerate
	outerGate := e.opts.ScreenContentMode == 2 ||
		(e.rc.dropFrameAllowed && rcThresholdMet)
	if !outerGate {
		// Outside the outer gate libvpx still resets force_maxqp and
		// advances the post-drop counter so the rcf-watchdog branch can
		// arm next time.
		e.forceMaxQuantizer = false
		e.rc.framesSinceLastDropOvershoot++
		return false
	}
	// Inner drop trigger (vp8/encoder/ratectrl.c lines ~1532-1543).
	const threshPredErrMB = 200 << 4 // libvpx: thresh_pred_err_mb = (200 << 4)
	threshQ := (3 * e.rc.maxQuantizer) >> 2
	avBytesPerFrame := e.rc.bitsPerFrame >> 3
	threshRate := 2 * avBytesPerFrame
	if e.rc.dropFrameAllowed && e.lastPredErrorMB > (threshPredErrMB<<4) {
		// libvpx widens the trigger when the prior frame already showed
		// extreme prediction error: thresh_rate >>= 3.
		threshRate >>= 3
	}
	predErrMB := 0 // pending pred-err accumulation; see field comment above
	if Q < threshQ &&
		projectedSizeBytes > threshRate &&
		predErrMB > threshPredErrMB &&
		predErrMB > 2*e.lastPredErrorMB {
		// Drop fires.
		e.forceMaxQuantizer = true
		// libvpx resets buffer_level + bits_off_target to optimal so the
		// next-frame target estimator does not try to "earn back" the
		// overspent bits on a single frame.
		if e.rc.bufferOptimalBits > 0 {
			e.rc.bufferLevelBits = e.rc.bufferOptimalBits
		}
		// Bump rate_correction_factor toward the target/worst-quality
		// ratio, clamped at min(2*current, MAX_BPB_FACTOR).
		if macroblocks > 0 && e.rc.maxQuantizer >= 0 && e.rc.maxQuantizer < len(libvpxBitsPerMB[1]) {
			targetBitsPerMB := libvpxTargetBitsPerMB(e.rc.bitsPerFrame, macroblocks)
			worstBitsPerMB := libvpxBitsPerMB[1][e.rc.maxQuantizer]
			if worstBitsPerMB > 0 {
				newCF := float64(targetBitsPerMB) / float64(worstBitsPerMB)
				if newCF > rcf {
					capped := 2.0 * rcf
					if newCF > capped {
						newCF = capped
					}
					if newCF > libvpxMaxBPBFactor {
						newCF = libvpxMaxBPBFactor
					}
					e.rc.setRateCorrectionFactorForFrame(false, false, newCF)
				}
			}
		}
		e.rc.framesSinceLastDropOvershoot = 0
		return true
	}
	e.forceMaxQuantizer = false
	e.rc.framesSinceLastDropOvershoot++
	return false
}

func (e *VP8Encoder) encodeInterFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool, staticSegmentationAllowed bool, sourceIsAltRef bool) (interFrameEncodeAttempt, error) {
	cfg := vp8enc.DefaultInterFrameStateConfig(uint8(e.rc.currentQuantizer))
	cfg.InvisibleFrame = flags&EncodeInvisibleFrame != 0
	cfg.TokenPartition = vp8common.TokenPartition(e.opts.TokenPartitions)
	cfg.QuantDeltas = libvpxFrameQuantDeltas(e.rc.currentQuantizer, e.opts.ScreenContentMode)
	cfg.LoopFilterLevel, cfg.SharpnessLevel = e.encoderLoopFilter(vp8common.InterFrame)
	cfg.SimpleLoopFilter = e.encoderUsesSimpleLoopFilter()
	cfg.RefreshEntropyProbs = flags&EncodeNoUpdateEntropy == 0 && !e.opts.ErrorResilient
	cfg.IndependentContexts = e.opts.ErrorResilientPartitions
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
	// Mirror libvpx vp8/encoder/rdopt.c vp8_initialize_rd_consts: the RD
	// picker's per-frame fill_token_costs reads from cpi->lfc_a, cpi->lfc_g,
	// or cpi->lfc_n depending on which reference the current frame refreshes —
	// NOT from cm->fc.coef_probs (which is what govpx's e.coefProbs mirrors).
	// Frames that refresh golden/altref score against a colder snapshot
	// (e.g. lfc_g, last touched at the previous keyframe) which raises every
	// candidate's rate, lifts bestScore over rd_threshes[SPLITMV], and lets
	// SPLITMV evaluate. Without this swap, govpx's RD scores run ~0.5x of
	// libvpx's on golden-refresh frames and SPLITMV's gate spuriously fires.
	//
	// We stash the picker-side snapshot on e.rdPickerCoefProbs so the picker
	// helpers (selectInterFrameModeDecision et al.) read from it; the
	// committed encode path keeps using e.coefProbs, mirroring libvpx's
	// tokenize.c which reads cm->fc.coef_probs.
	previousRDPickerCoefProbs := e.rdPickerCoefProbsActive
	e.rdPickerCoefProbsActive = e.rdPickerCoefProbs(cfg.RefreshGolden, cfg.RefreshAltRef)
	defer func() {
		e.rdPickerCoefProbsActive = previousRDPickerCoefProbs
	}()
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
	cfg.LoopFilterLevel, err = e.pickLoopFilterLevel(source, vp8common.InterFrame, cfg.LoopFilterLevel, cfg.SharpnessLevel, rows, cols, required, segmentation)
	if err != nil {
		return interFrameEncodeAttempt{}, err
	}
	lfHeader := e.encoderLoopFilterHeader(cfg.LoopFilterLevel, cfg.SharpnessLevel)
	cfg.SimpleLoopFilter = lfHeader.Type == vp8dec.SimpleLoopFilter
	cfg.LFDeltaEnabled = lfHeader.DeltaEnabled
	cfg.LFDeltaUpdate = e.computeLFDeltaUpdateBit(lfHeader.DeltaEnabled, lfHeader.RefDeltas, lfHeader.ModeDeltas)
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
	n, frameCoefProbs, frameYModeProbs, frameUVModeProbs, frameMVProbs, err := vp8enc.WriteCoefficientInterFrameWithProbabilityBaseScratch(dst, e.opts.Width, e.opts.Height, cfg, e.interFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols], &e.coefProbs, e.modeProbs.YMode, e.modeProbs.UVMode, e.modeProbs.MV, &e.partScratch)
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	projectedBits, coefSavings, refFrameSavings := e.projectedFrameSizeBitsFromRateWithSavings(false, required, projectedRate, cfg.RefreshGolden, cfg.RefreshAltRef)
	return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: frameCoefProbs, FrameYModeProbs: frameYModeProbs, FrameUVModeProbs: frameUVModeProbs, FrameMVProbs: frameMVProbs, Size: n, ProjectedSizeBits: projectedBits, CoefSavingsBits: coefSavings, RefFrameSavingsBits: refFrameSavings, CyclicRefresh: segmentation.Enabled, CyclicRefreshNextIndex: cyclicRefreshNextIndex}, nil
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

// projectedFrameSizeBitsFromRateWithSavings projects the post-savings
// frame-size bits and returns the per-component entropy-savings breakdown
// alongside it. Used by the oracle trace to localize entropy-savings
// parity gaps. The breakdown is the PRE-clamp value: when the
// post-savings projection would underflow, the bits return clamps to 0
// but the savings scalars still reflect what was subtracted.
// refreshGolden / refreshAltRef mirror libvpx
// cm->refresh_golden_frame / cm->refresh_alt_ref_frame for the in-flight
// inter-frame attempt; the values gate the libvpx vp8_convert_rfct_to_prob
// hook documented in refFrameEntropySavingsBitsForFrame. Key frames pass
// false/false (libvpx skips the hook for KEY frames anyway).
func (e *VP8Encoder) projectedFrameSizeBitsFromRateWithSavings(keyFrame bool, macroblocks int, projectedRate int, refreshGolden bool, refreshAltRef bool) (bits int, coefSavings int, refFrameSavings int) {
	if projectedRate <= 0 {
		return 0, 0, 0
	}
	projectedBits := projectedRate >> 8
	coefSavings = e.coefficientEntropySavingsBits(keyFrame, macroblocks)
	refFrameSavings = e.refFrameEntropySavingsBitsForFrame(keyFrame, macroblocks, refreshGolden, refreshAltRef)
	projectedBits -= coefSavings + refFrameSavings
	if projectedBits < 0 {
		projectedBits = 0
	}
	return projectedBits, coefSavings, refFrameSavings
}

// refFrameEntropySavingsBitsForFrame mirrors libvpx's inter-frame ref-frame
// branch of vp8_estimate_entropy_savings (vp8/encoder/bitstream.c) for the
// CURRENT frame's accepted attempt. Crucial parity nuance:
// vp8/encoder/encodeframe.c:vp8_encode_frame (around line 980) calls
// vp8_convert_rfct_to_prob(cpi) at the tail of the encode pass for any
// inter frame that is NOT a single-layer GF/ARF refresh, which OVERWRITES
// cpi->prob_intra_coded / prob_last_coded / prob_gf_coded with the
// probabilities derived from THIS frame's count_mb_ref_frame_usage --
// before vp8_estimate_entropy_savings runs at onyx_if.c line 3996. Since
// the same rfct then drives both the "old" cost (post-overwrite) and the
// "new" cost inside vp8_estimate_entropy_savings, the inter-frame branch
// returns zero savings on every frame where the convert hook fired.
//
// govpx's previous behaviour subtracted the heuristic-biased
// e.refProb{Intra,Last,Golden} values, which produced spurious savings of
// up to ~64 bits per inter frame and was the residual gap behind
// projected_frame_size in TestOracleEncoderTraceDecisionCompare. Mirroring
// the libvpx convert hook here zeros that out for the same gate libvpx
// uses (libvpxShouldConvertRefCountsToProb) and keeps the heuristic-biased
// fallback for the GF/ARF refresh branch (single-layer, refresh) where
// libvpx skips the convert hook.
func (e *VP8Encoder) refFrameEntropySavingsBitsForFrame(keyFrame bool, macroblocks int, refreshGolden bool, refreshAltRef bool) int {
	if keyFrame || macroblocks <= 0 || len(e.interFrameModes) < macroblocks {
		return 0
	}
	if libvpxShouldConvertRefCountsToProb(e.libvpxTemporalLayerCount(), refreshGolden, refreshAltRef) {
		return 0
	}
	intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes[:macroblocks])
	return libvpxRefFrameEntropySavings(false, intra, last, golden, alt, int(e.refProbIntra), int(e.refProbLast), int(e.refProbGolden))
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
		if e.opts.ErrorResilientPartitions {
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
	if e.opts.ErrorResilientPartitions {
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
	// Mirror libvpx vp8/encoder/ratectrl.c vp8_setup_key_frame: after
	// vp8_default_coef_probs resets cm->fc, cpi->lfc_a/lfc_g/lfc_n are all
	// seeded from cm->fc — that is, from the *default* probabilities, BEFORE
	// the keyframe encode pass adapts cm->fc via vp8_update_coef_probs. The
	// end-of-frame `lfc_X = cm->fc` assignments overwrite each slot only when
	// the corresponding refresh_X flag is set; on a keyframe all three flags
	// are set, so lfc_a/lfc_g/lfc_n end up holding the *post-adaptation*
	// keyframe fc — but in practice short clips' adaptation barely moves the
	// table, and the slots that DO move differ block-by-block between libvpx
	// and govpx (govpx's keyframe intra-mode picker still has BPred residual
	// divergences pinned in earlier rounds). Seeding the snapshots from
	// e.coefProbs (the post-keyframe-adaptation table) is what libvpx does;
	// the lingering keyframe-adaptation gap is tracked separately. The
	// important property here is that the RD picker on later golden/altref
	// refresh frames reads from this seed instead of from e.coefProbs, which
	// keeps following inter-frame adaptations from polluting the
	// long-reference RD scoring.
	e.coefProbsLast = e.coefProbs
	// Seed lfc_g/lfc_a with the *default* coefficient table rather than the
	// keyframe-adapted e.coefProbs: govpx's keyframe intra-mode picks still
	// diverge from libvpx in pinned BPred residual cases (see
	// docs/vp8_encoder_parity.md), so the post-keyframe adaptation is
	// noticeably stronger in govpx than in libvpx for affected clips. Using
	// the unadapted default as the long-reference snapshot is the
	// closest-to-libvpx proxy until the upstream BPred residual gap closes —
	// libvpx's lfc_g is an "almost default" table for short clips where the
	// keyframe is the only thing seeding it, so the SPLITMV-gate parity
	// reasoning is the same regardless of whether we use the precise
	// libvpx-side adapted value or the default seed.
	e.coefProbsGolden = vp8tables.DefaultCoefProbs
	e.coefProbsAltRef = vp8tables.DefaultCoefProbs
	e.coefProbsSnapshotsValid = true
	// Mirror libvpx vp8/encoder/bitstream.c pack_lf_deltas: after a frame
	// is packed, last_*_lf_deltas mirror the just-signaled deltas so the
	// next frame's send_update bit reflects whether anything actually
	// changed. The keyframe is the first packed frame in a clip, so this
	// is also where lfDeltasSignaledOnce flips to true.
	e.updateLastSignaledLFDeltas(attempt.LFDeltaEnabled, attempt.RefLFDeltas, attempt.ModeLFDeltas)
}

func (e *VP8Encoder) commitInterFrameAttempt(attempt interFrameEncodeAttempt) {
	e.commitInterFrameEntropy(attempt)
	e.commitInterFrameSkipFalseProb(attempt)
	e.updateRefFrameProbsFromAttempt(attempt)
	// Mirror libvpx vp8/encoder/bitstream.c pack_lf_deltas: after a frame
	// is packed, last_*_lf_deltas mirror the just-signaled deltas so the
	// next frame's send_update bit reflects whether anything actually
	// changed. We snapshot the accepted attempt's deltas to match.
	e.updateLastSignaledLFDeltas(attempt.Config.LFDeltaEnabled, attempt.Config.RefLFDeltas, attempt.Config.ModeLFDeltas)
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

// pickerCoefProbs returns the coefficient prob table the inter-frame RD picker
// should feed into rate estimation. When the per-reference snapshot stack is
// valid AND a picker pass is active (rdPickerCoefProbsActive set by
// encodeInterFrameAttempt), returns that snapshot; otherwise falls back to
// the live encoder coefProbs (used for key frames, committed-encode paths,
// and pre-snapshot transient state).
func (e *VP8Encoder) pickerCoefProbs() *vp8tables.CoefficientProbs {
	if e != nil && e.rdPickerCoefProbsActive != nil {
		return e.rdPickerCoefProbsActive
	}
	return &e.coefProbs
}

// rdPickerCoefProbs returns the snapshot the inter-frame RD picker should
// feed into fill_token_costs (the rate side of every coefficientBlockTokenRate
// call inside the picker), mirroring libvpx vp8/encoder/rdopt.c
// vp8_initialize_rd_consts:
//
//	l = refresh_alt_ref_frame ? &cpi->lfc_a
//	  : refresh_golden_frame  ? &cpi->lfc_g
//	  : &cpi->lfc_n
//
// `lfc_n` was last refreshed by the previous frame (so e.coefProbs already
// reflects that context — leaving picker probs == e.coefProbs is fine for
// the no-refresh-boost branch). For golden/altref-refresh frames the picker
// has to run against a "colder" snapshot (the keyframe-vintage adapted fc),
// because every intervening inter frame's last-refresh-only updates skipped
// lfc_g/lfc_a. Without this swap, govpx's RD scoring on golden/altref
// boost frames runs against e.coefProbs (the heavily-adapted fc) and the
// resulting low rates spuriously trip rd_threshes[SPLITMV] off, letting
// NEARESTMV win modes that libvpx reaches via SPLITMV. See
// parity-close-r3-h-rd-scale.
//
// Returns nil before the first commitKeyFrameEntropy seeds the snapshots
// (which on a keyframe-led clip is impossible to hit on an inter frame), or
// when none of the per-reference snapshots have been valid yet — in which
// case the caller falls back to e.coefProbs.
func (e *VP8Encoder) rdPickerCoefProbs(refreshGolden, refreshAltRef bool) *vp8tables.CoefficientProbs {
	if e == nil || !e.coefProbsSnapshotsValid {
		return nil
	}
	switch {
	case refreshAltRef:
		return &e.coefProbsAltRef
	case refreshGolden:
		return &e.coefProbsGolden
	default:
		// LAST snapshot mirrors e.coefProbs already (commitInterFrameEntropy
		// updates them in lockstep when refresh_last_frame=1). Returning nil
		// to fall back to e.coefProbs is equivalent and avoids a redundant
		// pointer indirection on the picker hot path.
		return nil
	}
}

func (e *VP8Encoder) commitInterFrameEntropy(attempt interFrameEncodeAttempt) {
	if !attempt.Config.RefreshEntropyProbs {
		// Mirror libvpx onyx_if.c encode_frame_to_data_rate
		// `if (refresh_entropy_probs == 0) cm->fc = cm->lfc;` rollback: when
		// the bitstream did NOT carry a refresh, the post-frame fc is reset
		// to the pre-frame snapshot (lfc). govpx's e.coefProbs already
		// reflects that pre-frame snapshot in this branch, so the
		// per-reference lfc_X snapshots below also see it.
	} else {
		e.coefProbs = attempt.FrameCoefProbs
		e.modeProbs.YMode = attempt.FrameYModeProbs
		e.modeProbs.UVMode = attempt.FrameUVModeProbs
		e.modeProbs.MV = attempt.FrameMVProbs
	}
	// Mirror libvpx onyx_if.c lines 5151-5157: the per-reference frame-context
	// snapshots are updated independently from each refresh flag, AFTER the
	// (optional) `cm->fc = cm->lfc` rollback above. Together with the keyframe
	// seed in commitKeyFrameEntropy, this gives the RD picker a stable
	// `last refresh of {alt,golden,last}` view of cm->fc to feed
	// fill_token_costs from on the NEXT frame.
	if !e.coefProbsSnapshotsValid {
		e.coefProbsLast = e.coefProbs
		e.coefProbsGolden = e.coefProbs
		e.coefProbsAltRef = e.coefProbs
		e.coefProbsSnapshotsValid = true
	}
	if attempt.Config.RefreshAltRef {
		e.coefProbsAltRef = e.coefProbs
	}
	if attempt.Config.RefreshGolden {
		e.coefProbsGolden = e.coefProbs
	}
	if attempt.Config.RefreshLast {
		e.coefProbsLast = e.coefProbs
	}
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
		probIntra := min(int(e.refProbIntra)+40, 255)
		e.refProbIntra = uint8(probIntra)
		e.refProbLast = 200
		e.refProbGolden = 1
	} else if e.framesSinceGolden == 0 {
		e.refProbLast = 214
	} else if e.framesSinceGolden == 1 {
		e.refProbLast = 192
		e.refProbGolden = 220
	} else if e.sourceAltRefActive {
		probGolden := max(int(e.refProbGolden)-20, 10)
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

// resetGoldenFrameStats mirrors libvpx vp8/encoder/onyx_if.c
// `update_golden_frame_stats`'s `cm->refresh_golden_frame` branch, which is
// the routine that runs after every keyframe (vp8_setup_key_frame asserts
// `cm->refresh_golden_frame=1`). The libvpx update:
//
//   - frames_since_golden = 0
//   - if (!source_alt_ref_pending) source_alt_ref_active = 0
//   - if (frames_till_gf_update_due > 0) frames_till_gf_update_due--
//
// It leaves `source_alt_ref_pending` and `alt_ref_source` intact so that
// `define_gf_group` can arm a fresh ARF schedule from inside `vp8_second_pass`
// (which runs at the top of Pass2Encode for the keyframe) and have it survive
// the post-encode lifecycle bookkeeping. govpx mirrors that here so a
// pass2-armed ARF schedule produced during keyframe encoding is not clobbered
// before the next frame's `autoAltRefMaybeEncode` reads it.
//
// For full state reset (Reset(), encoder init), use `clearAltRefSchedule` to
// also drop `source_alt_ref_pending`/`altRefSourceValid`/`framesTillAltRefFrame`.
func (e *VP8Encoder) resetGoldenFrameStats() {
	e.framesSinceGolden = 0
	if !e.sourceAltRefPending {
		e.sourceAltRefActive = false
	}
	if e.framesTillAltRefFrame > 0 {
		e.framesTillAltRefFrame--
	}
}

// clearAltRefSchedule drops any pending auto-ARF schedule, mirroring the
// libvpx `cpi->source_alt_ref_pending=0; cpi->alt_ref_source=NULL` reset that
// runs from `vp8_create_compressor` and on explicit reconfiguration paths
// (encoder init, Reset(), error-resilient enable). It is the lifecycle
// counterpart to `resetGoldenFrameStats`, which is the libvpx-faithful
// per-keyframe stats update and intentionally preserves the schedule.
func (e *VP8Encoder) clearAltRefSchedule() {
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

// libvpxKeyFrameSetupGFInterval returns the value libvpx's vp8_setup_key_frame
// would assign to cpi->frames_till_gf_update_due (== baseline_gf_interval) at
// the time the next key frame is being encoded.
//
// One-pass: libvpx onyx_if.c vp8_create_compressor sets baseline_gf_interval =
// gf_interval_onepass_cbr for any (Mode <= 2 && CBR && !error_resilient)
// compressor (line ~1886); vp8_change_config later resets baseline_gf_interval
// to DEFAULT_GF_INTERVAL for non-realtime modes (line ~1542) and only re-arms
// the gf_interval_onepass_cbr value for realtime CBR (line ~1547). vpxenc
// invokes vp8_change_config after vp8_create_compressor, so the effective
// value at first-keyframe time is:
//   - realtime CBR: gf_interval_onepass_cbr (cyclic-refresh derived, [6,40])
//   - good/best quality CBR: DEFAULT_GF_INTERVAL == 7
//   - non-CBR (one-pass): DEFAULT_GF_INTERVAL == 7
//
// Two-pass: libvpx vp8/encoder/firstpass.c find_next_key_frame zeroes
// frames_till_gf_update_due (line ~2521); define_gf_group then runs and
// derives baseline_gf_interval from the per-frame motion stats walk
// (line ~1860/1906/1910). calc_iframe_target_size finally seeds
// frames_till_gf_update_due = baseline_gf_interval (vp8/encoder/ratectrl.c
// line ~513). Govpx's twoPassState mirrors that derivation: prepareKFGroup
// + defineGFGroup populate t.framesTillGFUpdate with the two-pass-derived
// baseline_gf_interval before this function is consulted. Returning the
// libvpx-derived value here (instead of the one-pass DEFAULT_GF_INTERVAL
// fallback) avoids a spurious mid-section GF refresh at frame
// DEFAULT_GF_INTERVAL when libvpx's section runs longer.
func (e *VP8Encoder) libvpxKeyFrameSetupGFInterval(rows int, cols int) int {
	if e.opts.Deadline == DeadlineRealtime && e.rc.mode == RateControlCBR && !e.opts.ErrorResilient {
		return e.goldenFrameCBRInterval(rows, cols)
	}
	if e.twoPass.enabled() && e.twoPass.gfGroupValid && e.twoPass.framesTillGFUpdate > 0 {
		return e.twoPass.framesTillGFUpdate
	}
	return libvpxDefaultGFInterval
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
	e.opts.DropFrameWaterMark = cfg.DropFrameWaterMark
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
	// libvpx encodeframe.c:685-691: realtime mode runs vp8_auto_select_speed
	// which evolves cpi->Speed. Mirror that: for realtime+positive-cpu_used,
	// return the adaptive autoSpeed (seeded to 4 at cold start, cf.
	// libvpxAutoSelectSpeed). For realtime+negative-cpu_used and other
	// deadlines, fall back to the static formula.
	if e.opts.Deadline == DeadlineRealtime {
		cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
		if cpuUsed >= 0 {
			if e.autoSpeed == 0 {
				return 4 // cold start before first encode_mb_row
			}
			return e.autoSpeed
		}
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

// libvpx vp8/encoder/rdopt.c:65 auto_speed_thresh table indexed by
// cpi->Speed (0..16). vp8_auto_select_speed lowers Speed when budget
// dwarfs avg_encode_time: ms_for_compress*100 > avg_encode_time*thresh.
var libvpxAutoSpeedThresh = [17]int{
	1000, 200, 150, 130, 150, 125,
	120, 115, 115, 115, 115, 115,
	115, 115, 115, 115, 105,
}

func nowMonotonicNS() int64 { return nanotime() }

// libvpxAutoSelectSpeedActive returns true when the realtime adaptive
// Speed selector is in charge of cpi->Speed (cpu_used >= 0 in realtime).
// When cpu_used < 0 libvpx pins Speed=-cpu_used directly per
// encodeframe.c:686-687, bypassing auto-select.
func (e *VP8Encoder) libvpxAutoSelectSpeedActive() bool {
	if e == nil || e.opts.Deadline != DeadlineRealtime {
		return false
	}
	return e.opts.CpuUsed >= 0
}

// libvpxAutoSelectSpeed mirrors libvpx vp8/encoder/rdopt.c:261
// vp8_auto_select_speed exactly. Called at the top of each encode_mb_row
// for realtime+positive-cpu_used. Cold start (avg_pick_mode_time==0):
// Speed=4. Otherwise raise/lower based on the (1e6/framerate)*(16-cpu)/16
// ms budget vs cumulative timer state, capped at [4,16].
func (e *VP8Encoder) libvpxAutoSelectSpeed() {
	if e == nil {
		return
	}
	if e.opts.Deadline != DeadlineRealtime {
		return
	}
	cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
	if cpuUsed < 0 {
		// libvpx encodeframe.c:686-687: explicit-Speed branch (no auto-select).
		e.autoSpeed = -cpuUsed
		return
	}
	fps := e.opts.FPS
	if fps <= 0 {
		fps = 30
	}
	msForCompress := 1000000 / fps
	msForCompress = msForCompress * (16 - cpuUsed) / 16
	// Note: avgPickModeTime and avgEncodeTime are signed int to mirror
	// libvpx's signed-int avg_pick_mode_time / avg_encode_time
	// (vp8/encoder/onyx_int.h:455-456). The (avgEncodeTime - avgPickModeTime)
	// difference is signed-negative on the first inter frame after a key
	// frame (avg_encode_time is skipped for KFs while avg_pick_mode_time is
	// updated, so EncodeTime=0 < PickModeTime). Using uint64 here would let
	// the subtraction underflow to a huge unsigned value, fail the
	// "< msForCompress" guard, and force the bump branch (Speed += 4) on
	// frame 1, breaking the auto-select trajectory libvpx follows.
	if e.avgPickModeTime < msForCompress &&
		(e.avgEncodeTime-e.avgPickModeTime) < msForCompress {
		if e.avgPickModeTime == 0 {
			e.autoSpeed = 4
		} else {
			if msForCompress*100 < e.avgEncodeTime*95 {
				e.autoSpeed += 2
				e.avgPickModeTime = 0
				e.avgEncodeTime = 0
				if e.autoSpeed > 16 {
					e.autoSpeed = 16
				}
			}
			if e.autoSpeed >= 0 && e.autoSpeed < len(libvpxAutoSpeedThresh) &&
				msForCompress*100 > e.avgEncodeTime*libvpxAutoSpeedThresh[e.autoSpeed] {
				e.autoSpeed -= 1
				e.avgPickModeTime = 0
				e.avgEncodeTime = 0
				if e.autoSpeed < 4 {
					e.autoSpeed = 4
				}
			}
		}
	} else {
		e.autoSpeed += 4
		if e.autoSpeed > 16 {
			e.autoSpeed = 16
		}
		e.avgPickModeTime = 0
		e.avgEncodeTime = 0
	}
}

// libvpxAutoSpeedSynthCostUS is the synthetic per-macroblock encode cost
// (in microseconds) that govpx feeds into avg_encode_time / avg_pick_mode_time
// when AutoSpeedGoOverheadCalibration is enabled. It replaces the wall-clock
// duration entirely so the Speed trajectory is a pure function of frame size
// and not the host machine's encode speed.
//
// Calibration target: govpx's auto_select must converge to a Speed at which
// the govpx encoder produces output (kbps, PSNR, interframe bytes) that
// matches stock libvpx's output, NOT necessarily the same Speed value
// libvpx itself parks at. Empirically (R13 bench at 320p/480p/720p/1080p),
// govpx's Speed=4 path produces libvpx-equivalent output across all
// resolutions, while govpx's Speed=16 path overshoots libvpx's bitrate by
// 1.30x-1.50x because the higher-Speed mode-decision shortcuts diverge
// between the two implementations. So we synthesise a per-frame duration
// that keeps govpx's auto_select pinned at Speed=4 at every resolution.
//
// Stability bound: vp8_auto_select_speed (rdopt.c:261) bumps Speed up
// when ms_for_compress*100 < avg_encode_time*95, which simplifies to
// avg_encode_time > ms_for_compress * 100/95 ≈ 17544 us at fps=30,
// cpu_used=8. To stay at Speed=4 we need
// frame_duration < 17544 us at every supported resolution, including
// 1080p (8160 MBs). cost_per_MB = 2 us gives frame_duration = 16320 us at
// 1080p (under the 17544 threshold) and 600 us at 320x240 (well under).
//
// Frame-0 keyframe behaviour is preserved: avg_encode_time updates skip
// keyframes (mirroring onyx_if.c:5110), and avg_pick_mode_time uses the
// synthetic duration2 = duration/2 just like the C path.
//
// The R12-D wall-clock-scaling approach (constant 24/10 ratio) achieved
// the same Speed=4 outcome at 720p but parked govpx at Speed=6/7 at 1080p
// because frame wall-clock scales with MB count and the fixed ratio could
// not absorb the resolution-dependent overhead. The synthetic-cost model
// scales implicitly with MBs/frame so the Speed=4 floor holds at every
// resolution.
const libvpxAutoSpeedSynthCostUS = 2

// finishAutoSpeedTiming mirrors libvpx onyx_if.c:5103-5128: at end of frame
// encode in realtime, IIR-update avg_encode_time (skipped for keyframes) and
// avg_pick_mode_time (duration2 = duration/2 by libvpx convention).
//
// When AutoSpeedGoOverheadCalibration is on, govpx replaces the measured
// wall-clock duration with a synthetic value proportional to the frame's
// macroblock count (libvpxAutoSpeedSynthCostUS). This makes the Speed
// trajectory a pure function of (cpu_used, framerate, width, height) and
// drops the host-machine-dependence that broke the R12-D wall-clock-scaling
// approach at 1080p. See the libvpxAutoSpeedSynthCostUS comment for the
// empirical calibration contract.
//
// When calibration is off, we still measure wall-clock and feed it directly,
// matching the patched libvpx oracle behaviour the parity scoreboards rely on.
func (e *VP8Encoder) finishAutoSpeedTiming(isKeyFrame bool) {
	if e == nil || e.autoSpeedFrameStartNS == 0 || e.opts.Deadline != DeadlineRealtime {
		return
	}
	durationNS := nowMonotonicNS() - e.autoSpeedFrameStartNS
	e.autoSpeedFrameStartNS = 0
	if durationNS < 0 {
		durationNS = 0
	}
	var duration int // microseconds
	if e.opts.AutoSpeedGoOverheadCalibration && e.libvpxAutoSelectSpeedActive() {
		// Synthetic duration: cost_per_MB * MBs. Resolution-aware so the
		// Speed trajectory tracks libvpx across all frame sizes; not
		// host-clock dependent so parity is reproducible across machines.
		mbs := encoderMacroblockCount(e.opts.Width, e.opts.Height)
		duration = mbs * libvpxAutoSpeedSynthCostUS
	} else {
		duration = int(durationNS / 1000)
	}
	duration2 := duration / 2
	if !isKeyFrame {
		if e.avgEncodeTime == 0 {
			e.avgEncodeTime = duration
		} else {
			e.avgEncodeTime = (7*e.avgEncodeTime + duration) >> 3
		}
	}
	if duration2 > 0 {
		if e.avgPickModeTime == 0 {
			e.avgPickModeTime = duration2
		} else {
			e.avgPickModeTime = (7*e.avgPickModeTime + duration2) >> 3
		}
	}
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
	e.twoPass.configureFrameDims(e.opts.Width, e.opts.Height)
	return nil
}

func (e *VP8Encoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	e.forceKeyFrame = true
}

// Reset returns the encoder to a NewVP8Encoder-equivalent cold-start state
// without re-allocating the per-MB scratch buffers. This is what bench
// harnesses use to run a warmup encode followed by a measured one without
// repeating the allocation cost.
//
// R15-E note: previously Reset cleared a hand-curated subset of state, which
// meant fields touched only by the encode loop (rc.kfOverspendBits, the
// inter-RD threshold snapshots, the per-reference probabilities, etc.) leaked
// values from the warmup pass into the measured pass. At 320x240 that drove a
// 7% kbps undershoot vs stock libvpx (govpx 1017 kbps vs libvpx 1089 kbps
// against the same target) because rate-correction state was warmed-up before
// the measured run started. The fix: zero the rateControlState struct,
// re-apply applyConfig, and explicitly reset every encoder-level field that
// NewVP8Encoder seeds.
func (e *VP8Encoder) Reset() {
	if e == nil {
		return
	}
	// Encoder-level scalars / flags.
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
	e.clearAltRefSchedule()
	e.resetGoldenFrameStats()
	// Zero the inter-RD threshold-cache generation BEFORE
	// resetInterRDThresholdMultipliers bumps it back to 1 so cold-start
	// parity is preserved (NewVP8Encoder seeds gen=1 via this same call).
	e.interRDThreshBaselineGen = 0
	e.resetInterRDThresholdMultipliers()
	e.interRDFrameActive = false
	e.probSkipFalse = 128
	e.lastSkipFalseProbs = [3]uint8{}
	e.baseSkipFalseProbs = libvpxBaseSkipFalseProbs
	// libvpx vp8/encoder/onyx_if.c init_config seeds these per-reference
	// probabilities; without restoring them on Reset the warmed values
	// from a prior run leak into the next encode, biasing rate control.
	e.refProbIntra = 63
	e.refProbLast = 128
	e.refProbGolden = 128
	e.goldenRefAliasesLast = false
	e.altRefAliasesLast = false
	e.goldenRefAliasesAlt = false
	e.referenceFrameNumbers = [vp8common.MaxRefFrames]uint64{}
	e.thisKeyFrameForced = false
	e.ambientErr = 0
	e.mbsZeroLastDotSuppress = 0
	e.currentTemporalLayer = 0
	e.lastFramePercentIntra = 0
	e.framesSinceGolden = 0
	e.sourceAltRefActive = false
	e.sourceAltRefPending = false
	e.altRefSourceValid = false
	e.altRefSourcePTS = 0
	e.framesTillAltRefFrame = 0
	e.autoAltRefStashValid = false
	e.autoAltRefStashPTS = 0
	e.autoAltRefStashDuration = 0
	e.autoAltRefStashFlags = 0
	e.currentSourcePTS = 0
	e.savedContext = savedCodingContext{}
	// Re-zero every per-MB and per-row decision/coefficient buffer.
	for i := range e.keyFrameModes {
		e.keyFrameModes[i] = vp8enc.KeyFrameMacroblockMode{}
	}
	for i := range e.interFrameModes {
		e.interFrameModes[i] = vp8enc.InterFrameMacroblockMode{}
	}
	for i := range e.lastFrameInterModes {
		e.lastFrameInterModes[i] = vp8enc.InterFrameMacroblockMode{}
	}
	for i := range e.lastFrameInterModeBias {
		e.lastFrameInterModeBias[i] = false
	}
	for i := range e.keyFrameCoeffs {
		e.keyFrameCoeffs[i] = vp8enc.MacroblockCoefficients{}
	}
	for i := range e.tokenAbove {
		e.tokenAbove[i] = vp8enc.TokenContextPlanes{}
	}
	for i := range e.reconstructAboveTok {
		e.reconstructAboveTok[i] = vp8enc.TokenContextPlanes{}
	}
	for i := range e.reconstructModes {
		e.reconstructModes[i] = vp8dec.MacroblockMode{}
	}
	for i := range e.reconstructTokens {
		e.reconstructTokens[i] = vp8dec.MacroblockTokens{}
	}
	e.dequantTables = vp8common.FrameDequantTables{}
	e.dequants = [vp8common.MaxMBSegments]vp8common.MacroblockDequant{}
	e.reconstructScratch = vp8dec.IntraReconstructionScratch{}
	e.loopInfo = vp8common.LoopFilterInfo{}
	e.loopFilterLevel = 0
	e.lfDeltasSignaledOnce = false
	e.lastSignaledRefLFDeltas = [vp8common.MaxRefLFDeltas]int8{}
	e.lastSignaledModeLFDeltas = [vp8common.MaxModeLFDeltas]int8{}
	e.coefProbsLast = vp8tables.CoefficientProbs{}
	e.coefProbsGolden = vp8tables.CoefficientProbs{}
	e.coefProbsAltRef = vp8tables.CoefficientProbs{}
	e.coefProbsSnapshotsValid = false
	e.rdPickerCoefProbsActive = nil
	// Inter-RD state captured by snapshot/restore around the recode loop.
	e.interRDThreshMultSnapshot = [libvpxInterModeCount]int{}
	e.interRDThreshTouchedSnapshot = [libvpxInterModeCount]bool{}
	e.interModeTestHitCountsSnapshot = [libvpxInterModeCount]int{}
	e.interMBsTestedSoFarSnapshot = 0
	// resetInterRDThresholdMultipliers() bumped interRDThreshBaselineGen
	// once during the encoder-level scalars block above; leave it at 1 to
	// match NewVP8Encoder's cold-start trajectory.
	e.interRDThreshBaselineSlots = [interRDThreshBaselineSlotCount]interRDThreshBaselineSlot{}
	e.interRDFrameRefSearchOrder = [4]int{}
	e.interRDFrameRefSearchOrderValid = false
	e.interMBsTestedSoFar = 0
	e.interModeCheckFreq = [libvpxInterModeCount]int{}
	e.interModeTestHitCounts = [libvpxInterModeCount]int{}
	e.interModeErrorBins = [1024]uint32{}
	e.interModeSpeedErrorBins = [1024]uint32{}
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
	e.oracleTraceInterCandidateBuffer = e.oracleTraceInterCandidateBuffer[:0]
	e.oracleTraceRecodeLoopCount = 0
	e.oracleTraceRecodeReason = ""
	e.oracleTraceTotalByteCount = 0
	// Rate-control: zero the entire struct then re-apply config so every
	// field NewVP8Encoder ever touches lands at the cold-start value.
	e.rc = rateControlState{}
	cfg := defaultRateControlConfig(e.opts)
	_ = e.rc.applyConfig(cfg, e.timing)
	e.rc.keyFrameFrequency = e.opts.KeyFrameInterval
	e.rc.autoKeyFrames = e.opts.AdaptiveKeyFrames
	e.rc.minFrameBandwidth = vbrMinFrameBandwidthBits(e.rc.bitsPerFrame, e.opts.TwoPassMinPct)
	if e.rc.mode != RateControlCBR && len(e.opts.TwoPassStats) == 0 {
		e.rc.framesTillGFUpdateDue = libvpxDefaultGFInterval
	}
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = e.rc.cqLevel
	} else {
		e.rc.currentQuantizer = e.rc.minQuantizer
	}
	e.rc.lastQuantizer = e.rc.currentQuantizer
	e.rc.lastInterQuantizer = e.rc.currentQuantizer
	e.rc.bufferLevelBits = e.rc.bufferInitialBits
	e.rc.avgFrameQuantizer = e.rc.maxQuantizer
	e.rc.normalInterAvgQuantizer = e.rc.maxQuantizer
	e.rc.frameTargetBits = e.rc.bitsPerFrame
	// libvpx vp8_create_compressor seeds cpi->force_maxqp = 0 and
	// cpi->frames_since_last_drop_overshoot = 0; mirror that on Reset
	// so a sequence re-init does not leak overshoot-drop state from the
	// previous run.
	e.forceMaxQuantizer = false
	e.lastPredErrorMB = 0
	// Temporal layer state.
	e.temporal.frameIndex = 0
	e.temporal.tl0PicIdx = 0
	e.temporal.tl0Valid = false
	e.temporal.refLayer = [temporalReferenceCount]int{}
	e.temporal.accounting = [MaxTemporalLayers]temporalLayerAccounting{}
	e.temporal.buffersSet = false
	e.twoPass.configure(e.opts.TwoPassStats, e.rc.bitsPerFrame, e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct, e.opts.TwoPassMaxPct)
	e.twoPass.configureFrameDims(e.opts.Width, e.opts.Height)
	e.coefProbs = vp8tables.DefaultCoefProbs
	vp8dec.ResetModeProbs(&e.modeProbs)
	// libvpx vp8_create_compressor seeds cpi->Speed=0 and avg_pick_mode_time
	// /avg_encode_time = 0 (zero-initialised under calloc). Mirror that on
	// Reset() so a sequence re-init does not leak the warmed adaptive Speed
	// from the previous run; otherwise the bench harness's warmup pass
	// drives e.autoSpeed away from the cold-start seed of 4 before the
	// measured pass starts, producing a non-deterministic per-frame size
	// distribution and an inflated avg_interframe_bytes ratio vs libvpx
	// (which always starts cold under vpxenc).
	e.autoSpeed = 0
	e.avgPickModeTime = 0
	e.avgEncodeTime = 0
	e.autoSpeedFrameStartNS = 0
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
	if e.rowWorkers != nil {
		e.rowWorkers.shutdownPool()
		e.rowWorkers = nil
	}
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
	if opts.Threads == 0 {
		// Mirror libvpx default (vp8/encoder/onyx_if.c init: oxcf
		// multi_threaded defaults to 0/1). Threads=0 is the historical
		// zero-initialized govpx default; collapse it onto 1 so internal
		// code can call effectiveThreadCount() without re-checking.
		opts.Threads = 1
	}
	if opts.Threads > maxEncoderThreads {
		opts.Threads = maxEncoderThreads
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
	level = min(libvpxClampLoopFilterLevel(e.rc.currentQuantizer, level), 63)
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

// computeLFDeltaUpdateBit mirrors libvpx vp8/encoder/bitstream.c pack_lf_deltas:
//
//	int send_update = xd->mode_ref_lf_delta_update || cpi->oxcf.error_resilient_mode;
//
// libvpx's `mode_ref_lf_delta_update` flag is set once at init in
// set_default_lf_deltas and cleared after every packed frame (see
// vp8/encoder/onyx_if.c). In effect, libvpx writes `update=1` on the very
// first packed frame (when last_*_lf_deltas are still the all-zero memset
// from setup_features) and `update=0` thereafter, since the default deltas
// never change at runtime. We mirror that by also re-emitting `update=1`
// when the encoder's chosen deltas drift away from the last-signaled values
// or in error-resilient mode. The "signaled once" gate covers the keyframe
// invariant: until we have packed a frame at all, the deltas have not been
// communicated to the decoder.
func (e *VP8Encoder) computeLFDeltaUpdateBit(deltaEnabled bool, refDeltas [vp8common.MaxRefLFDeltas]int8, modeDeltas [vp8common.MaxModeLFDeltas]int8) bool {
	if !deltaEnabled {
		return false
	}
	if e == nil {
		return true
	}
	if e.opts.ErrorResilient {
		return true
	}
	if !e.lfDeltasSignaledOnce {
		return true
	}
	return refDeltas != e.lastSignaledRefLFDeltas || modeDeltas != e.lastSignaledModeLFDeltas
}

// updateLastSignaledLFDeltas commits the per-frame loop-filter delta
// snapshot that future frames compare against to decide whether to set
// mode_ref_lf_delta_update. Called from the keyframe / inter-frame commit
// paths so recode iterations within a frame see the pre-frame state.
func (e *VP8Encoder) updateLastSignaledLFDeltas(deltaEnabled bool, refDeltas [vp8common.MaxRefLFDeltas]int8, modeDeltas [vp8common.MaxModeLFDeltas]int8) {
	if e == nil || !deltaEnabled {
		return
	}
	e.lastSignaledRefLFDeltas = refDeltas
	e.lastSignaledModeLFDeltas = modeDeltas
	e.lfDeltasSignaledOnce = true
}

func (e *VP8Encoder) encoderLoopFilterInterModeDelta() int8 {
	if e != nil && e.opts.Deadline == DeadlineRealtime {
		return -12
	}
	return -2
}

func (e *VP8Encoder) pickLoopFilterLevel(src vp8enc.SourceImage, frameType vp8common.FrameType, seedLevel uint8, sharpness uint8, rows int, cols int, required int, segmentation vp8enc.SegmentationConfig) (uint8, error) {
	if len(e.reconstructModes) < required {
		return 0, ErrInvalidConfig
	}
	if seedLevel == 0 {
		return 0, nil
	}
	if e.loopFilterUsesFastSearch() {
		return e.pickLoopFilterLevelFast(src, frameType, seedLevel, sharpness, rows, cols, required, segmentation)
	}
	return e.pickLoopFilterLevelFull(src, frameType, seedLevel, sharpness, rows, cols, required, segmentation)
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

func (e *VP8Encoder) pickLoopFilterLevelFast(src vp8enc.SourceImage, frameType vp8common.FrameType, seedLevel uint8, sharpness uint8, rows int, cols int, required int, segmentation vp8enc.SegmentationConfig) (uint8, error) {
	minLevel := libvpxMinLoopFilterLevel(e.rc.currentQuantizer)
	maxLevel := libvpxMaxLoopFilterLevel(e.rc.currentQuantizer)
	level := clampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	bestLevel := level
	// Diagnostic: when GOVPX_LF_DEBUG=1, also score the unfiltered (level=0)
	// partial-frame Y SSE against the source so the per-trial-level diff
	// harness can pin whether the gap is in the LF math or in the
	// pre-filter reconstruction itself. This scores the partial-frame
	// window only and the result is otherwise unused.
	if os.Getenv("GOVPX_LF_DEBUG") == "1" {
		preErr, perr := e.loopFilterTrialLumaSSE(src, frameType, 0, sharpness, rows, cols, required, true, segmentation)
		if perr == nil {
			e.emitOracleLFTrial("pre", 0, preErr)
		}
	}
	bestErr, err := e.loopFilterTrialLumaSSE(src, frameType, level, sharpness, rows, cols, required, true, segmentation)
	if err != nil {
		return 0, err
	}
	e.emitOracleLFTrial("seed", level, bestErr)

	filtLevel := level - loopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr, err := e.loopFilterTrialLumaSSE(src, frameType, filtLevel, sharpness, rows, cols, required, true, segmentation)
		if err != nil {
			return 0, err
		}
		e.emitOracleLFTrial("down", filtLevel, filtErr)
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
			filtErr, err := e.loopFilterTrialLumaSSE(src, frameType, filtLevel, sharpness, rows, cols, required, true, segmentation)
			if err != nil {
				return 0, err
			}
			e.emitOracleLFTrial("up", filtLevel, filtErr)
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

func (e *VP8Encoder) pickLoopFilterLevelFull(src vp8enc.SourceImage, frameType vp8common.FrameType, seedLevel uint8, sharpness uint8, rows int, cols int, required int, segmentation vp8enc.SegmentationConfig) (uint8, error) {
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
		trialErr, err := e.loopFilterTrialLumaSSE(src, frameType, level, sharpness, rows, cols, required, false, segmentation)
		if err != nil {
			return 0, err
		}
		ssErr[level] = trialErr
		ssSet[level] = true
		e.emitOracleLFTrial("full", level, trialErr)
		return trialErr, nil
	}

	// Diagnostic: when GOVPX_LF_DEBUG=1, also score level 0 to expose the
	// pre-filter SSE so the per-trial divergence harness can localize whether
	// the gap is in the unfiltered reconstruction (matches at 0 -> LF math
	// diverges) or in the source itself (mismatched at 0 -> upstream recon
	// gap). This level-0 score is otherwise unused because the picker never
	// considers a level below min_filter_level.
	if os.Getenv("GOVPX_LF_DEBUG") == "1" {
		_, _ = score(0)
	}

	bestErr, err := score(filtMid)
	if err != nil {
		return 0, err
	}
	filtBest := filtMid
	filtDirection := 0
	for filterStep > 0 {
		// Mirror libvpx vp8/encoder/picklpf.c vp8cx_pick_filter_level
		// (Bias = (best_err >> (15 - (filt_mid / 8))) * filter_step). The
		// shift saturates at zero (filt_mid/8 >= 15 only when filt_mid >=
		// 120, which is above MAX_LOOP_FILTER=63), so it always preserves
		// some bias against raising the filter level. govpx's full picker
		// previously hard-coded bias=0, which silently dropped libvpx's
		// "prefer lower filter level" tie-breaker and steered the picker
		// to a different filt_best on inter frames where multiple trials
		// score within the bias delta of best_err (e.g. the 128x128 panning
		// CBR cpu8 fixture frame 1: govpx picked level 11, libvpx 5).
		bias := loopFilterFullPickerBias(bestErr, filtMid, filterStep, e.twoPass.sectionIntraRating)
		filtHigh := min(filtMid+filterStep, maxLevel)
		filtLow := max(filtMid-filterStep, minLevel)

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

// loopFilterFullPickerBias mirrors libvpx vp8/encoder/picklpf.c
// vp8cx_pick_filter_level's `Bias = (best_err >> (15 - (filt_mid / 8))) *
// filter_step;` followed by `if (section_intra_rating < 20) Bias = Bias *
// section_intra_rating / 20`. The shift amount is `15 - filt_mid/8`. For
// filt_mid in [0, 63] the shift ranges [8, 15].
//
// Critically, libvpx's twopass.section_intra_rating is in the cpi->twopass
// struct which is calloc'd; in one-pass / realtime / CBR encodes it is
// never written so it stays at 0. The unconditional VP8 guard
// `if (section_intra_rating < 20) Bias = Bias * section_intra_rating / 20;`
// then forces Bias = 0 every iteration of the full picker. (VP9's analogue
// adds an `oxcf.pass == 2` predicate, but VP8 does not — the two-pass
// guard is implicit via the zero default.) Mirroring govpx's previous
// "fall through and use unscaled bias" behaviour caused the realtime CBR
// full picker at q=17 / prev_lf=5 to converge on a different filt_best
// than libvpx because the nonzero bias rejected high-side trials that
// libvpx accepted, and accepted low-side trials that libvpx rejected. The
// `section_intra_rating` argument is the integer computed by libvpx's
// `section_intra_rating = section_intra_error / section_coded_error` (or
// 0 in one-pass).
func loopFilterFullPickerBias(bestErr int, filtMid int, filterStep int, sectionIntraRating int) int {
	shift := max(15-(filtMid/8), 0)
	bias := (bestErr >> uint(shift)) * filterStep
	if sectionIntraRating < 20 {
		bias = bias * sectionIntraRating / 20
	}
	return bias
}

// loopFilterTrialLumaSSE applies the candidate loop-filter level to a copy
// of the analysis image (full frame or partial-frame window depending on
// `partial`) and returns the Y SSE between the source and the filtered
// buffer. Level 0 skips the copy and scores the analysis image directly
// because no filtering can mutate the trial buffer. The supplied segmentation
// must mirror the segmentation that
// actually fires at reconstruction time so the per-segment LF deltas
// (e.g. cyclic refresh's MB_LVL_ALT_LF[1] = lf_adjustment) modulate the
// per-MB filter level the same way libvpx's vp8cx_set_alt_lf_level +
// vp8_loop_filter_frame_init does inside vp8cx_pick_filter_level.
func (e *VP8Encoder) loopFilterTrialLumaSSE(src vp8enc.SourceImage, frameType vp8common.FrameType, level int, sharpness uint8, rows int, cols int, required int, partial bool, segmentation vp8enc.SegmentationConfig) (int, error) {
	if level == 0 {
		return loopFilterLumaSSE(src, &e.analysis.Img, rows, cols, partial), nil
	}
	if partial {
		startRow, rowCount := loopFilterPartialFrameWindow(rows)
		copyLoopFilterPartialLuma(&e.loopFilterPick.Img, &e.analysis.Img, startRow, rowCount)
		header := e.encoderLoopFilterHeader(uint8(level), sharpness)
		if err := vp8dec.ApplyLoopFilterPartial(&e.loopFilterPick.Img, rows, cols, e.reconstructModes[:required], frameType, header, loopFilterSegmentationHeader(segmentation), &e.loopInfo, startRow, rowCount); err != nil {
			return 0, ErrInvalidConfig
		}
		return loopFilterLumaSSE(src, &e.loopFilterPick.Img, rows, cols, true), nil
	}
	copyFrameImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
	header := e.encoderLoopFilterHeader(uint8(level), sharpness)
	if err := vp8dec.ApplyLoopFilterFullLuma(&e.loopFilterPick.Img, rows, cols, e.reconstructModes[:required], frameType, header, loopFilterSegmentationHeader(segmentation), &e.loopInfo); err != nil {
		return 0, ErrInvalidConfig
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
	endY := min(min((startRow+rowCount)*16, src.CodedHeight), dst.CodedHeight)
	if endY <= startY {
		return
	}
	width := min(dst.CodedWidth, src.CodedWidth)
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
	srcY := src.Y
	imgY := img.Y
	srcStride := src.YStride
	imgStride := img.YStride
	srcW := src.Width
	srcH := src.Height
	imgW := img.CodedWidth
	imgH := img.CodedHeight
	// Pre-compute the column gating for the hot row (every MB in a fully
	// in-bounds row is covered by the SSE16x16PtrFast SIMD-bypass path).
	// 1280x720 / 1920x1080 / aligned-width frames pass this check for
	// every column, so the inner loop collapses to a tight call sequence.
	colsAllAligned := cols > 0 && cols*16 <= srcW && cols*16 <= imgW
	for mbRow := startRow; mbRow < startRow+rowCount && mbRow < rows; mbRow++ {
		baseY := mbRow * 16
		// Hoist the per-row Y bounds + base offset out of the column loop;
		// once baseY clears the row check, every MB on the row uses the
		// same vertical clearance.
		if baseY+16 <= srcH && baseY+16 <= imgH {
			srcRowOff := baseY * srcStride
			imgRowOff := baseY * imgStride
			if colsAllAligned {
				// Hot path: every MB on the row is fully in-bounds for
				// both src and img — no per-column bounds check needed.
				for mbCol := range cols {
					baseX := mbCol * 16
					total += dsp.SSE16x16PtrFast(&srcY[srcRowOff+baseX], srcStride, &imgY[imgRowOff+baseX], imgStride)
				}
				continue
			}
			for mbCol := range cols {
				baseX := mbCol * 16
				if baseX+16 <= srcW && baseX+16 <= imgW {
					total += dsp.SSE16x16PtrFast(&srcY[srcRowOff+baseX], srcStride, &imgY[imgRowOff+baseX], imgStride)
					continue
				}
				total += loopFilterLumaBlockSSE(src, img, baseY, baseX)
			}
			continue
		}
		for mbCol := range cols {
			baseX := mbCol * 16
			total += loopFilterLumaBlockSSE(src, img, baseY, baseX)
		}
	}
	return total
}

func loopFilterLumaBlockSSE(src vp8enc.SourceImage, img *vp8common.Image, baseY int, baseX int) int {
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		imgY := clampEncodeCoord(baseY+row, img.CodedHeight)
		for col := range 16 {
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
	if !is4x4 {
		eob := src.EOB[24]
		dst.EOB[24] = eob
		copyQCoeffForEOB(&src.QCoeff[24], eob, &dst.QCoeff[24])
		for i := range 16 {
			eob := max(src.EOB[i], 1)
			dst.EOB[i] = eob
			copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
		}
	} else {
		dst.EOB[24] = 0
		for i := range 16 {
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
