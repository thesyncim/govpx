package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// VP9Encoder is the public entry point for VP9 profile 0 stream encoding.
type VP9Encoder struct {
	opts     VP9EncoderOptions
	closed   bool
	temporal temporalState
	rc       vp9RateControlState
	twoPass  vp9TwoPassState
	cyclicAQ encoder.CyclicRefreshState
	// cyclicCountMapSnap* reuse buffers for saveVP9CyclicRefreshMapsForCounts
	// so steady-state cyclic inter frames stay allocation-free.
	cyclicCountSegMapSnap     []uint8
	cyclicCountRefreshMapSnap []int8
	perceptualAQ              encoder.PerceptualAQState
	vp9OracleTraceHolder
	// spatialScalabilityLocked is set for encoders owned by
	// VP9SpatialSVCEncoder; the parent owns spatial layer metadata.
	spatialScalabilityLocked bool
	// temporalScalabilityLocked is set for encoders owned by
	// VP9SpatialSVCEncoder; the parent owns access-unit temporal metadata.
	temporalScalabilityLocked bool

	// svc mirrors the subset of libvpx SVC layer-context state read by the
	// speed-features dispatcher and other ported consumers. Single-layer
	// encoders leave it at encoder.DefaultSVCState() so e.svc.UseSvc reports cpi->use_svc
	// = 0 and number_spatial_layers = number_temporal_layers = 1.
	//
	// libvpx: vp9_svc_layercontext.h SVC struct.
	svc encoder.SVCState

	// maxCopiedFrame mirrors cpi->max_copied_frame, written by the
	// speed-features dispatcher at speeds 7-8 to bound the number of
	// consecutive frames whose partition can be copied from the prior frame.
	//
	// libvpx: vp9_encoder.h cpi->max_copied_frame,
	// vp9_speed_features.c:721,728,733,758.
	maxCopiedFrame int

	// lastFrameDropped mirrors VP9_COMP::last_frame_dropped. The realtime
	// speed-feature cascade disables partition copy on the frame following a
	// pre-encode or post-encode drop.
	//
	// libvpx:
	//   - vp9_ratectrl.c:596,642 set after post/pre encode drops
	//   - vp9_encoder.c:5529 clear after a coded frame
	//   - vp9_speed_features.c:721-728 copy-partition gate
	lastFrameDropped bool

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
	// seeded by encoder.NoiseEstimateState.Init in NewVP9Encoder (libvpx:
	// vp9_encoder.c:1528). The enabled flag is recomputed by
	// vp9NoiseEstimateRefreshEnabled before each speed-features dispatch
	// so the consumer at vp9_speed_features.c:777-782 reads the same
	// predicate libvpx evaluates.
	// libvpx ref: vp9/encoder/vp9_noise_estimate.h:30-40.
	noiseEstimate encoder.NoiseEstimateState

	// frameIndex tracks the frame number for the key-frame cadence
	// gate. Mirrors libvpx's cpi->common.current_video_frame.
	frameIndex int

	// sourceTS mirrors the libvpx source-timestamp bookkeeping that drives
	// adjust_frame_rate (vp9/encoder/vp9_encoder.c:5753). The implicit per-call
	// PTS is vp9PTS (incremented once per encode call, duration 1), converted to
	// ticks through the current g_timebase ratio. adjust_frame_rate re-derives
	// cpi->framerate from the inter-frame timestamp delta, which is how a
	// mid-stream fps change actually reaches rate control.
	sourceTS encoderSourceTimestampState
	// vp9PTS is the implicit presentation timestamp in timebase units, matching
	// the vpxenc driver's `pts += frame_duration` (frame_duration == 1 under the
	// exact-fps timebase). It advances once per encode call.
	vp9PTS uint64
	// framesSinceKey tracks committed and dropped frames since the last
	// keyframe for adaptive keyframe min-distance gating.
	framesSinceKey uint16
	// forceKeyFrame is a sticky one-shot request consumed by the next
	// successfully committed frame.
	forceKeyFrame bool

	// deadlineModePreviousFrame mirrors VP9_COMP::deadline_mode_previous_frame.
	// libvpx uses it in one-pass rate control to force a key frame after a
	// GOOD/BEST <-> REALTIME mode transition, and in the realtime speed-feature
	// cascade to force nonrd_keyframe on the transition frame.
	//
	// libvpx:
	//   - vp9_ratectrl.c:2133,2310,2510 mode-change keyframe tests
	//   - vp9_speed_features.c:863-866 nonrd_keyframe mode-change override
	//   - vp9_ratectrl.c:2003,2024 postencode/drop latch
	deadlineModePreviousFrame    vp9DeadlineMode
	deadlineModePreviousFrameSet bool

	// fc carries the per-frame entropy context across frames.
	// Reset on every keyframe via ResetFrameContext.
	frameContexts [common.FrameContexts]vp9dec.FrameContext
	fc            vp9dec.FrameContext
	// vp9NonrdModeCostFc mirrors the mode/filter cost tables libvpx stores
	// on VP9_COMP for the realtime nonrd path. vp9_initialize_rd_consts
	// (vp9_rd.c:435-437) refreshes fill_mode_costs on keyframes, on non-nonrd
	// frames, and on current_video_frame&7 == 1; nonrd pickmode reuses the
	// snapshot between refreshes.
	vp9NonrdModeCostFc      vp9dec.FrameContext
	vp9NonrdModeCostFcValid bool
	// vp9NonrdMvCostFc mirrors the x->nmvcost MV-entropy cost table libvpx
	// rebuilds via vp9_build_nmv_cost_table. Unlike fill_mode_costs, that
	// rebuild is additionally guarded by !frame_is_intra_only
	// (vp9_rd.c:439-443): it runs only on (!use_nonrd_pick_mode ||
	// current_video_frame&7 == 1) AND a non-intra frame. The table is
	// vpx_calloc'd (zeroed) at create time (vp9_encoder.c:2441) and only
	// populated by that build, so until the first non-intra build runs the
	// nonrd subpel motion search costs MVs with a ZERO entropy table. This
	// state is reachable when two adjacent keyframes precede the first inter
	// frame (a forced KF at frame 1): neither keyframe builds the table and
	// the first inter frame (frame 2, &7 != 1) does not either, so its NEWMV
	// subpel refinement runs with zero MV cost (picking lowest pure variance).
	// vp9NonrdMvCostFcValid stays false until the first build; while false the
	// subpel MV cost is zero, matching the calloc'd table.
	vp9NonrdMvCostFc      vp9dec.FrameContext
	vp9NonrdMvCostFcValid bool
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
	// 8x8 cell (row, col). Populated by encoder.ChoosePartitioning on SB
	// entry; consumed by pickVP9CBRVariancePartitionBlockSize /
	// pickVP9KeyframeVariancePartitionBlockSize to derive the
	// per-call partition decision. varPartSBComputed[(sbRow*sbCols+sbCol]]
	// tracks which 64x64 superblocks have already been populated this
	// frame so the picker fires once per SB.
	//
	// libvpx ref: vp9/encoder/vp9_encodeframe.c:1253-1763
	// (choose_partitioning) writes the partition tree; nonrd_use_partition
	// (vp9_encodeframe.c:4854) consumes it.
	varPartGrid                []vp9dec.NeighborMi
	varPartSBComputed          []bool
	varPartFrameValid          bool
	varPartSBUseMvPart         []bool
	varPartSBMvPart            []vp9dec.MV
	varPartSBPredLast          []vp9dec.MV
	varPartSBPredValid         []bool
	varPartSBVarLow            [][25]uint8
	varPartSBContentState      []encoder.ContentStateSB
	varPartSBContentStateValid []bool
	varPartSBZeroTempSADSource []bool
	varPartSBColorSensitivity  [][2]bool
	// varPartSBLastHighContent caches x->last_sb_high_content per SB before
	// avg_source_sad mutates content_state_sb_fd (vp9_encodeframe.c:1346-1347
	// read precedes vp9_encodeframe.c:1238-1244 update).
	varPartSBLastHighContent      []uint8
	varPartSBLastHighContentValid []bool
	varPartTreeScratch            encoder.V64x64
	varPartTreeLowResScratch      [16]encoder.V16x16

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
	refWidth     [common.RefFrames]uint32
	refHeight    [common.RefFrames]uint32
	refValid     [common.RefFrames]bool
	refMap       [common.RefFrames]int
	nextRefMapID int

	// planes carries coefficient entropy contexts for source-backed frames.
	planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane

	// vp9SBEntropyAbove / vp9SBEntropyLeft snapshot the plane entropy context
	// (pd->above_context / pd->left_context) at 64x64 superblock entry, for the
	// libvpx x->skip_encode search-context freeze. On the deep full-RD
	// use-partition path, when sf->skip_encode_frame is set and base_qindex <
	// QIDX_SKIP_THRESH, libvpx's per-leaf intermediate encode_superblock (the
	// output_enabled==0 RD-search-phase encode) early-returns BEFORE updating
	// the entropy context (vp9/encoder/vp9_encodeframe.c:6112-6115), so every
	// leaf in the SB runs its RD search against the SB-entry context rather than
	// the running committed context. govpx fuses search+commit per leaf, so it
	// restores the live context to this SB-entry snapshot around each leaf's RD
	// search and re-threads it for the real coefficient commit. See
	// vp9SnapshotSBSearchEntropy / vp9WithSBSearchEntropy.
	vp9SBEntropyAbove   [vp9dec.MaxMbPlane][]uint8
	vp9SBEntropyLeft    [vp9dec.MaxMbPlane][]uint8
	vp9SBEntropyValid   bool
	vp9SBEntropySaveBuf [vp9dec.MaxMbPlane][]uint8

	intraScratch             vp9dec.IntraPredictorScratch
	modeScratch              [1024]byte
	blockScratch             [64 * 64]byte
	intraSkipPredScratch     [32 * 32]byte
	nonrdOrigPredScratch     [64 * 64]byte
	nonrdBestPredScratch     [64 * 64]byte
	partitionReconScratch    []byte
	partitionReconScratchTop int
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

	refFrames [common.RefFrames]vp9ReferenceFrame
	// refFrameIndex stamps each reference-frame-map slot with the encoder's
	// frameIndex (current_video_frame) at the moment it was refreshed,
	// mirroring libvpx set_frame_index (vp9/encoder/vp9_encoder.c:5029-5038).
	// vp9InterRefSignBias reads it back to reproduce set_ref_sign_bias
	// (vp9_encoder.c:4806-4821): sign bias is set iff the current frame
	// references a buffer stamped with a later video-frame number.
	refFrameIndex [common.RefFrames]int

	// extRefresh mirrors the libvpx VP9_COMP ext_refresh_*_frame and
	// refresh_*_frame state machine (vp9_encoder.h:650-660). It is
	// populated by vp9ApplyEncodingFlags (libvpx vp9_encoder.c:6812-6843
	// vp9_apply_encoding_flags) at EncodeIntoWithFlagsResult entry and
	// latched onto the post-override refresh mask by setExtOverrides
	// (libvpx vp9_encoder.c:4761-4775) at the start of each per-frame
	// encode_frame_to_data_rate. See vp9_ext_overrides.go for the full
	// citation chain.
	extRefresh vp9ExtRefreshState

	// lastBordered is the per-encoder border-padded mirror of the LAST_FRAME
	// luma plane consumed by choose_partitioning's low_res inter-predictor
	// path (vp9_int_pro_motion_estimation). Lazily allocated on first
	// refreshVP9EncoderRefs after LAST is written, then reused across
	// frames. The visible plane lives at (lastBordered.Border,
	// lastBordered.Border); the surrounding common.VP9EncBorderInPixels are
	// edge-replicated by common.YV12BuildBorderedPlane.
	//
	// libvpx counterpart: the LAST_FRAME YV12_BUFFER_CONFIG always carries
	// VP9_ENC_BORDER_IN_PIXELS=160 of padding on every plane
	// (vpx_scale/yv12config.h:26, vp9/encoder/vp9_encoder.c:1297), maintained
	// by vpx_extend_frame_borders_c after each frame's reconstruction
	// (vp9/encoder/vp9_encoder.c:3102 / 3167 / 3424 / 3470).
	lastBordered      common.YV12BorderBuffer
	lastBorderedValid bool
	// subpelRefBordered is the on-demand YV12-border mirror for non-LAST
	// references that the nonrd subpel variance search scores against.
	// LAST uses lastBordered so choose_partitioning and pickmode read the
	// same padded allocation.
	subpelRefBordered      common.YV12BorderBuffer
	subpelRefBorderedSlot  int
	subpelRefBorderedValid bool

	// intProSrcBordered is the per-encoder border-padded mirror of the
	// current frame's source luma plane. choose_partitioning's int_pro
	// reads up to (bw>>1) pixels before the SB origin on the source as
	// well as the reference. Built lazily inside vp9EnsureSBPartitionChosen
	// when the libvpx picker fires on an inter frame.
	intProSrcBordered      common.YV12BorderBuffer
	intProSrcBorderedValid bool

	// intProEstPred is the 64x64 luma predictor scratch built by
	// vp9_build_inter_predictors_sb (vp9_reconinter.c:253-258) from the
	// int_pro-resolved MV. Mirrors libvpx's xd->plane[0].dst.buf at
	// vp9_encodeframe.c:1487 (which writes a 64x64 dst with stride 64).
	intProEstPred [64 * 64]uint8

	prevFrameMvs      []vp9dec.MvRef
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
	blockQCoeffs   [vp9dec.MaxMbPlane][vp9EncoderBlockCoeffSlots]int16
	coefScratch    [1024]int16
	qCoefScratch   [1024]int16
	residueScratch [1024]int16
	// sub8x8PredScratch backs the genuine sub-8x8 RD producer's per-label
	// inter predictor (vp9_fullrd_inter_sub8x8_segment.go, encode_inter_mb_segment).
	// Allocated lazily; only used behind the gated-off deep sub-8x8 path.
	sub8x8PredScratch []byte
	txCoeffScratch    [1024]int16
	qCoeffScratch     [1024]int16
	dqCoeffScratch    [1024]int16
	// vp9BlockYrdScratch backs encoder.BlockYrd's src_diff + per-tx-unit
	// coeff/qcoeff/dqcoeff scratch. Sized for the realtime nonrd worst
	// case: BLOCK_64X64 + TX_16X16 = 4096 src_diff + 16 tx units × 256
	// coeffs × 3 (coeff/qcoeff/dqcoeff) = 16384 int16. libvpx clamps
	// tx_size <= TX_16X16 for nonrd_pickmode (vp9_pickmode.c:2361) so
	// the TX_8X8 / TX_4X4 paths fit within this allocation too.
	vp9BlockYrdScratch [16384]int16
	// cyclicPost{IsInter,MvRow,MvCol} back vp9_cyclic_refresh_postencode's
	// low-content walk without per-frame allocation on typical mi grids.
	cyclicPostIsInter []uint8
	cyclicPostMvRow   []int16
	cyclicPostMvCol   []int16
	// cyclicResizePending mirrors libvpx cpi->resize_pending for cyclic
	// refresh: set by applyVP9ResolutionChange (SetRealtimeTarget resize)
	// and latched into cyclicResizeFramePending for the next encode.
	cyclicResizePending bool
	// cyclicResizeFramePending is true only on the first encoded frame
	// after a resize (setup ResetResize + postencode forced GF).
	cyclicResizeFramePending bool
	dqScratch                vp9dec.DequantTables
	frameCounts              encoder.FrameCounts
	vp9HeaderScratch         vp9dec.UncompressedHeader
	vp9InterIntraHdr         vp9dec.UncompressedHeader
	vp9CountWorkers          []VP9Encoder
	vp9CountCounts           []encoder.FrameCounts
	vp9CountJobs             []vp9CountTileJob
	vp9TilePool              *vp9TileWorkerPool
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
	// vp9LeafInterRDDecisions is the SEARCH->WRITE replay cache for the
	// depth-first full-RD inter partition recursion (pickVP9InterPartitionRD).
	// Populated only while vp9InterUseDeepRDPartition is active: the search
	// commits each chosen leaf's vp9InterModeDecision as it fills the mi grid,
	// and the bitstream write descent replays the cached decision instead of
	// re-picking it with a desynced x->pred_mv context. Unused (never
	// populated) when the flag is off, so production stays byte-identical.
	vp9LeafInterRDDecisions     []vp9LeafInterRDDecisionEntry
	vp9LeafInterRDDecisionsRows int
	vp9LeafInterRDDecisionsCols int
	vp9LeafInterRDDecisionsVer  uint32
	// vp9InterPartitionRDDecisions is the partition-tree half of the deep
	// full-RD inter SEARCH->WRITE replay (the leaf-mode half is
	// vp9LeafInterRDDecisions). pickVP9InterPartitionRD records its committed
	// per-node child block size here so the writer descends the search's tree
	// instead of re-deciding each node with its divergent early-exit picker.
	// Allocated/used only under vp9InterUseDeepRDPartition; production leaves it
	// nil. Mirrors vp9KeyframePartitionDecisions.
	vp9InterPartitionRDDecisions     []vp9InterPartitionRDDecisionEntry
	vp9InterPartitionRDDecisionsRows int
	vp9InterPartitionRDDecisionsCols int
	vp9InterPartitionRDDecisionsVer  uint32
	// vp9LeafKeyframeDecisions mirrors the same count-pass to write-pass
	// replay for intra keyframe leaves. The stored values are just the mode
	// decisions; residue and entropy contexts are rebuilt in each pass.
	vp9LeafKeyframeDecisions     []vp9LeafKeyframeDecisionEntry
	vp9LeafKeyframeDecisionsRows int
	vp9LeafKeyframeDecisionsCols int
	vp9LeafKeyframeDecisionsVer  uint32
	// vp9KeyframePartitionDecisions mirrors libvpx's PC_TREE replay for
	// keyframe partition choices across the count and write passes.
	vp9KeyframePartitionDecisions     []vp9KeyframePartitionDecisionEntry
	vp9KeyframePartitionDecisionsRows int
	vp9KeyframePartitionDecisionsCols int
	vp9KeyframePartitionDecisionsVer  uint32
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

	// vp9BlockFilterRDScores carries the current final leaf's full-RD
	// interpolation-filter score table from the picker to the residue write
	// site. It is transient and consumed before the decision enters the
	// same-frame leaf cache.
	vp9BlockFilterRDScores [vp9dec.SwitchableFilterContexts]uint64
	vp9BlockFilterRDValid  bool

	lookahead      []vp9LookaheadEntry
	lookaheadRead  uint8
	lookaheadWrite uint8
	lookaheadCount uint8

	autoAltRefPending    vp9LookaheadEntry
	autoAltRefPendingSet bool
	autoAltRefEmitted    bool
	vp9ARNRScratch       image.YCbCr
	vp9ARNRRefs          [maxARNRFrames]encoder.TemporalFilterFrame

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
	lastLoopFilterLevel   uint8
	lastLoopFilterValid   bool

	// tpl carries the per-encoder TPL quality-pass state when EnableTPL
	// is true.  Slabs are sized at construction or on resolution change.
	tpl encoder.TPLState

	// cbRdmult mirrors libvpx's MACROBLOCK::cb_rdmult.  Each per-SB mode
	// picker (libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248) writes
	// this once from the base rdmult biased by the per-SB AQ/TPL deltas
	// and candidate-scoring helpers read it directly. When zero, callers
	// fall back to the per-frame rd.rdmult.
	cbRdmult int

	// fullRDPredMv mirrors libvpx's MACROBLOCK::pred_mv[MAX_REF_FRAMES]
	// (vp9/encoder/vp9_block.h). The full-RD inter NEWMV single_motion_search
	// tail stores its SUBPEL result here (vp9_rdopt.c:2750
	// x->pred_mv[ref] = tmp_mv->as_mv); the next-smaller block's vp9_mv_pred
	// (vp9_rd.c:613 pred_mv[2] = x->pred_mv[ref_frame]) reads it as the third
	// motion-vector-predictor candidate to seed mvp_full. The depth-first
	// rd_pick_partition recursion snapshots/restores it across partition arms
	// (store_pred_mv/load_pred_mv, vp9_encodeframe.c:3913/3932) so each child
	// arm seeds from the parent NONE block's NEWMV. Reset to the INT16_MAX
	// sentinel (vp9InterPredMvSentinel) per-SB (vp9_encodeframe.c:4215-4218).
	// Consumed ONLY when vp9InterUseDeepRDSub8x8 is active (the full deep-RD
	// inter stack the production cpu0/cpu4 enable turns on together with the
	// deep partition recursion + genuine this_rd); the deep-partition-only
	// SEARCH->WRITE round-trip harness and production (flags off) keep
	// candidate[2] sourced from the var-part cache, so those paths are
	// byte-identical.
	fullRDPredMv [vp9dec.MaxRefFrames]vp9dec.MV

	// rdThresh carries libvpx's RD-thresh / per-tile thresh_freq_fact
	// state. Lazily allocated by vp9EncoderInitializeRDConsts on first
	// frame init. Mirrors:
	//
	//   - RD_OPT::thresh_mult[MAX_MODES]               (vp9_rd.h:116)
	//   - RD_OPT::threshes[1][BLOCK_SIZES][MAX_MODES]  (vp9_rd.h:119)
	//   - TileDataEnc::thresh_freq_fact[BLOCK_SIZES][MAX_MODES]
	//                                                 (vp9_block.h)
	//
	// Single-tile collapse: govpx's current realtime encoder path is
	// single-tile, so libvpx's per-tile state collapses to a single tile
	// plane. Single-segment: segmentation is disabled for this path, so
	// [MAX_SEGMENTS=8] collapses to segment_id=0.
	rdThresh encoder.RDThreshState

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

	// lastSource mirrors libvpx's cpi->Last_Source /
	// unscaled_last_source reach for realtime avg_source_sad. It stores the
	// previous committed show-frame source, matching the
	// vp9_lookahead_peek(..., -1) frame libvpx exposes to the speed >= 5/6
	// source-SAD gates.
	lastSource      image.YCbCr
	lastSourceValid bool

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
		svc:           encoder.DefaultSVCState(),
		refFrameFlags: encoder.AllRefFlags,
		sourceTS:      newEncoderSourceTimestampState(vp9TimingStateFromOptions(opts)),
	}
	e.vp9LatchDeadlineModePreviousFrame()
	e.twoPass.configureWithCorpus(opts.TwoPassStats, rc.bitsPerFrame,
		opts.TwoPassVBRBiasPct, opts.TwoPassMinPct, opts.TwoPassMaxPct,
		opts.Height, opts.VBRCorpusComplexity)
	e.initVP9Lookahead(opts.Width, opts.Height, opts.LookaheadFrames)
	// libvpx initializes rc->gfu_boost to DEFAULT_GF_BOOST (2000) outside
	// the two-pass path so adjust_arnr_filter's adaptive strength/window
	// is fed even when no first-pass stats are available. Without this
	// seed, vp9_arnr.go uses the fixed-window fallback even when
	// LookaheadFrames > 0.
	// libvpx: vp9/encoder/vp9_ratectrl.c:2082 (one-pass VBR set) and
	// vp9_ratectrl.h:31 DEFAULT_GF_BOOST.
	if opts.LookaheadFrames > 0 {
		e.rc.gfuBoost = uint16(encoder.DefaultGFBoost)
	}
	e.cyclicAQ.Configure(opts.AQMode == VP9AQCyclicRefresh, opts.Width, opts.Height)
	e.perceptualAQ.Configure(opts.AQMode == VP9AQPerceptual)
	e.tpl.Configure(opts.EnableTPL, opts.Width, opts.Height, opts.LookaheadFrames)
	e.lfi = vp9dec.NewLoopFilterInfoN()
	vp9dec.LoopFilterInit(&e.lfi, 0)
	e.initVP9TileWorkerPool()
	// libvpx: vp9_encoder.c:1528 — vp9_noise_estimate_init runs at
	// encoder setup. vp9ApplySpeedFeatures below also refreshes
	// ne->enabled via vp9NoiseEstimateRefreshEnabled (mirroring the
	// vp9_update_noise_estimate assignment that precedes
	// the speed-features dispatch in libvpx).
	e.noiseEstimate.Init(opts.Width, opts.Height)
	// Populate the SPEED_FEATURES struct so consumers can read e.sf.<field>
	// before the first frame. libvpx: vp9_encoder.c:2635 also runs the
	// framesize-independent + framesize-dependent dispatch in setup before
	// the first frame is encoded.
	e.vp9ApplySpeedFeatures(e.vp9DefaultSpeedFrameContext())
	return e, nil
}
