package govpx

// VP9 speed features ported byte-for-byte from libvpx v1.16.0
// vp9/encoder/vp9_speed_features.{h,c}. Every enum value, struct field, and
// switch-case assignment in this file is cited inline as
// `// libvpx: vp9_speed_features.{h,c}:<line>`.
//
// Some SPEED_FEATURES fields land in govpx with `// TODO: consumer requires
// <libvpx function>` because the corresponding mode-decision / RD code path has
// not been ported yet. The field is still ported, with the libvpx-supplied
// default, so the configurator is identical to libvpx's switch-case dispatch.
//
// The configurator entry points mirror libvpx's two-step protocol:
//
//   vp9_set_speed_features_framesize_independent(cpi, speed)
//   vp9_set_speed_features_framesize_dependent  (cpi, speed)
//
// Both are called from libvpx encode_frame_to_data_rate() in vp9_encoder.c.
// govpx invokes vp9ApplySpeedFeatures() at frame setup so the per-frame state
// (frame_type, show_frame, base_qindex, frames_since_key) feeds the SF picks
// the same way libvpx feeds them at top-of-encode.

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// Mode bitmasks used by SPEED_FEATURES.intra_y_mode_mask /
// intra_uv_mode_mask / intra_y_mode_bsize_mask. Values mirror the bit shifts
// used in libvpx (1 << DC_PRED, 1 << V_PRED, ...). Numbering matches the
// PredictionMode enum in internal/vp9/common/enums.go.
//
// libvpx: vp9_speed_features.h:20-29.
const (
	sfIntraAll = (1 << common.DcPred) | (1 << common.VPred) | (1 << common.HPred) |
		(1 << common.D45Pred) | (1 << common.D135Pred) | (1 << common.D117Pred) |
		(1 << common.D153Pred) | (1 << common.D207Pred) | (1 << common.D63Pred) |
		(1 << common.TmPred)
	sfIntraDC     = 1 << common.DcPred
	sfIntraDCTm   = (1 << common.DcPred) | (1 << common.TmPred)
	sfIntraDCHV   = (1 << common.DcPred) | (1 << common.VPred) | (1 << common.HPred)
	sfIntraDCTmHV = (1 << common.DcPred) | (1 << common.TmPred) |
		(1 << common.VPred) | (1 << common.HPred)
)

// Inter-mode bitmasks for SPEED_FEATURES.inter_mode_mask, mirroring libvpx's
// (1 << NEARESTMV) / (1 << NEARMV) / (1 << ZEROMV) / (1 << NEWMV) bit
// positions. PredictionMode places NEARESTMV at 10..NEWMV at 13.
//
// libvpx: vp9_speed_features.h:31-39.
const (
	sfInterAll = (1 << common.NearestMv) | (1 << common.NearMv) |
		(1 << common.ZeroMv) | (1 << common.NewMv)
	sfInterNearest         = 1 << common.NearestMv
	sfInterNearestNew      = (1 << common.NearestMv) | (1 << common.NewMv)
	sfInterNearestZero     = (1 << common.NearestMv) | (1 << common.ZeroMv)
	sfInterNearestNewZero  = (1 << common.NearestMv) | (1 << common.ZeroMv) | (1 << common.NewMv)
	sfInterNearestNearNew  = (1 << common.NearestMv) | (1 << common.NearMv) | (1 << common.NewMv)
	sfInterNearestNearZero = (1 << common.NearestMv) | (1 << common.NearMv) | (1 << common.ZeroMv)
)

// THR_MODES_SUB8X8 bit positions for SPEED_FEATURES.disable_split_mask. The
// numeric ordering must match libvpx's enum in vp9_rd.h.
//
// libvpx: vp9_rd.h:95-102.
const (
	sfThrLast      = 0
	sfThrGold      = 1
	sfThrAltr      = 2
	sfThrCompLA    = 3
	sfThrCompGA    = 4
	sfThrIntra     = 5
	sfMaxRefs      = 6
	sfMaxMeshSteps = 4
)

// disable_split_mask presets. libvpx: vp9_speed_features.h:42-51.
const (
	sfDisableAllInterSplit = (1 << sfThrCompGA) | (1 << sfThrCompLA) |
		(1 << sfThrAltr) | (1 << sfThrGold) | (1 << sfThrLast)
	sfDisableAllSplit       = (1 << sfThrIntra) | sfDisableAllInterSplit
	sfDisableCompoundSplit  = (1 << sfThrCompGA) | (1 << sfThrCompLA)
	sfLastAndIntraSplitOnly = (1 << sfThrCompGA) | (1 << sfThrCompLA) |
		(1 << sfThrAltr) | (1 << sfThrGold)
)

// SearchMethods enumerates the full-pixel motion-search method picked by the
// SPEED_FEATURES dispatcher. Values mirror libvpx's SEARCH_METHODS enum.
//
// libvpx: vp9_speed_features.h:53-62.
type SearchMethods uint8

const (
	SearchMethodDiamond     SearchMethods = 0
	SearchMethodNStep       SearchMethods = 1
	SearchMethodHex         SearchMethods = 2
	SearchMethodBigDia      SearchMethods = 3
	SearchMethodSquare      SearchMethods = 4
	SearchMethodFastHex     SearchMethods = 5
	SearchMethodFastDiamond SearchMethods = 6
	SearchMethodMesh        SearchMethods = 7
)

// RecodeLoopType maps to libvpx RECODE_LOOP_TYPE.
//
// libvpx: vp9_speed_features.h:64-75.
type RecodeLoopType uint8

const (
	RecodeLoopDisallow     RecodeLoopType = 0
	RecodeLoopAllowKfMaxBw RecodeLoopType = 1
	RecodeLoopAllowKfArfGf RecodeLoopType = 2
	RecodeLoopAllowFirst   RecodeLoopType = 3
	RecodeLoopAllow        RecodeLoopType = 4
)

// SubpelSearchMethods maps to libvpx SUBPEL_SEARCH_METHODS.
//
// libvpx: vp9_speed_features.h:77-83.
type SubpelSearchMethods uint8

const (
	SubpelTree               SubpelSearchMethods = 0
	SubpelTreePruned         SubpelSearchMethods = 1
	SubpelTreePrunedMore     SubpelSearchMethods = 2
	SubpelTreePrunedEvenMore SubpelSearchMethods = 3
)

// MotionThreshold maps to libvpx MOTION_THRESHOLD.
//
// libvpx: vp9_speed_features.h:85-88.
type MotionThreshold uint8

const (
	NoMotionThreshold  MotionThreshold = 0
	LowMotionThreshold MotionThreshold = 7
)

// TxSizeSearchMethod maps to libvpx TX_SIZE_SEARCH_METHOD.
//
// libvpx: vp9_speed_features.h:90-94.
type TxSizeSearchMethod uint8

const (
	UseFullRD     TxSizeSearchMethod = 0
	UseLargestAll TxSizeSearchMethod = 1
	UseTx8x8      TxSizeSearchMethod = 2
)

// AutoMinMaxMode maps to libvpx AUTO_MIN_MAX_MODE.
//
// libvpx: vp9_speed_features.h:96-100.
type AutoMinMaxMode uint8

const (
	AutoMinMaxNotInUse           AutoMinMaxMode = 0
	AutoMinMaxRelaxedNeighboring AutoMinMaxMode = 1
	AutoMinMaxStrictNeighboring  AutoMinMaxMode = 2
)

// LpfPickMethod maps to libvpx LPF_PICK_METHOD.
//
// libvpx: vp9_speed_features.h:102-111.
type LpfPickMethod uint8

const (
	LpfPickFromFullImage LpfPickMethod = 0
	LpfPickFromSubImage  LpfPickMethod = 1
	LpfPickFromQ         LpfPickMethod = 2
	LpfPickMinimalLpf    LpfPickMethod = 3
)

// ModeSearchSkipFlags maps to libvpx MODE_SEARCH_SKIP_LOGIC.
//
// libvpx: vp9_speed_features.h:113-130.
const (
	FlagEarlyTerminate       = 1 << 0
	FlagSkipCompBestIntra    = 1 << 1
	FlagSkipIntraBestInter   = 1 << 3
	FlagSkipIntraDirMismatch = 1 << 4
	FlagSkipIntraLowVar      = 1 << 5
)

// InterpFilterMask maps to libvpx INTERP_FILTER_MASK. The shift positions
// match govpx's vp9dec.InterpEighttap / InterpEighttapSmooth / InterpEighttapSharp.
//
// libvpx: vp9_speed_features.h:132-136.
const (
	FlagSkipEighttap       = 1 << vp9dec.InterpEighttap
	FlagSkipEighttapSmooth = 1 << vp9dec.InterpEighttapSmooth
	FlagSkipEighttapSharp  = 1 << vp9dec.InterpEighttapSharp
)

// PartitionSearchType maps to libvpx PARTITION_SEARCH_TYPE.
//
// libvpx: vp9_speed_features.h:138-153.
type PartitionSearchType uint8

const (
	SearchPartition    PartitionSearchType = 0
	FixedPartition     PartitionSearchType = 1
	ReferencePartition PartitionSearchType = 2
	VarBasedPartition  PartitionSearchType = 3
	MlBasedPartition   PartitionSearchType = 4
)

// FastCoeffUpdate maps to libvpx FAST_COEFF_UPDATE.
//
// libvpx: vp9_speed_features.h:155-163.
type FastCoeffUpdate uint8

const (
	TwoLoop        FastCoeffUpdate = 0
	OneLoopReduced FastCoeffUpdate = 1
)

// SubpelForceStop maps to libvpx SUBPEL_FORCE_STOP.
//
// libvpx: vp9_speed_features.h:165.
type SubpelForceStop uint8

const (
	EighthPel  SubpelForceStop = 0
	QuarterPel SubpelForceStop = 1
	HalfPel    SubpelForceStop = 2
	FullPel    SubpelForceStop = 3
)

// AdaptSubpelForceStop maps to libvpx ADAPT_SUBPEL_FORCE_STOP.
//
// libvpx: vp9_speed_features.h:167-176.
type AdaptSubpelForceStop struct {
	MvThresh       int
	ForceStopBelow SubpelForceStop
	ForceStopAbove SubpelForceStop
}

// MvSpeedFeatures maps to libvpx MV_SPEED_FEATURES.
//
// libvpx: vp9_speed_features.h:178-214.
type MvSpeedFeatures struct {
	SearchMethod                  SearchMethods
	ReduceFirstStepSize           int
	AutoMvStepSize                int
	SubpelSearchMethod            SubpelSearchMethods
	SubpelSearchLevel             int
	SubpelForceStop               SubpelForceStop
	EnableAdaptiveSubpelForceStop int
	AdaptSubpelForceStop          AdaptSubpelForceStop
	FullpelSearchStepParam        int
	UseDownsampledSad             int
}

// PartitionSearchBreakoutThr maps to libvpx PARTITION_SEARCH_BREAKOUT_THR.
//
// libvpx: vp9_speed_features.h:216-219.
type PartitionSearchBreakoutThr struct {
	Dist int64
	Rate int
}

// MeshPattern maps to libvpx MESH_PATTERN.
//
// libvpx: vp9_speed_features.h:223-226.
type MeshPattern struct {
	Range    int
	Interval int
}

// OvershootDetectionCbrRt maps to libvpx OVERSHOOT_DETECTION_CBR_RT.
//
// libvpx: vp9_speed_features.h:228-241.
type OvershootDetectionCbrRt uint8

const (
	OvershootNoDetection       OvershootDetectionCbrRt = 0
	OvershootFastDetectionMaxQ OvershootDetectionCbrRt = 1
	OvershootReEncodeMaxQ      OvershootDetectionCbrRt = 2
)

// SubpelSearchType maps to libvpx SUBPEL_SEARCH_TYPE.
//
// libvpx: vp9_speed_features.h:243-248.
type SubpelSearchType uint8

const (
	Use2Taps      SubpelSearchType = 0
	Use4Taps      SubpelSearchType = 1
	Use8Taps      SubpelSearchType = 2
	Use8TapsSharp SubpelSearchType = 3
)

// EnableTrellisOptMethod maps to libvpx ENABLE_TRELLIS_OPT_METHOD.
//
// libvpx: vp9_speed_features.h:250-261.
type EnableTrellisOptMethod uint8

const (
	DisableTrellisOpt               EnableTrellisOptMethod = 0
	EnableTrellisOptM               EnableTrellisOptMethod = 1
	EnableTrellisOptTxRdSrcVar      EnableTrellisOptMethod = 2
	EnableTrellisOptTxRdResidualMse EnableTrellisOptMethod = 3
)

// TrellisOptControl maps to libvpx TRELLIS_OPT_CONTROL.
//
// libvpx: vp9_speed_features.h:263-266.
type TrellisOptControl struct {
	Method EnableTrellisOptMethod
	Thresh float64
}

// RdMlPartition mirrors the anonymous struct embedded as
// SPEED_FEATURES.rd_ml_partition.
//
// libvpx: vp9_speed_features.h:542-562.
type RdMlPartition struct {
	SearchBreakout         int
	SearchBreakoutThresh   [3]float32
	SearchEarlyTermination int
	VarPruning             int
	PruneRectThresh        [4]int
}

// SpeedFeatures mirrors libvpx SPEED_FEATURES. Every field is present, in the
// declaration order of vp9_speed_features.h:268-654, so the verbatim
// configurator can assign to e.sf.<field> exactly the way libvpx assigns to
// sf-><field>.
//
// libvpx: vp9_speed_features.h:268-654.
type SpeedFeatures struct {
	Mv MvSpeedFeatures

	FrameParameterUpdate int

	RecodeLoop RecodeLoopType

	OptimizeCoefficients int

	StaticSegmentation int

	CompInterJointSearchIterLevel int

	AdaptiveRdThresh      int
	AdaptiveRdThreshRowMt int

	SkipEncodeSb    int
	SkipEncodeFrame int
	AllowSkipRecode int

	CoeffProbAppxStep int

	TrellisOptTxRd TrellisOptControl

	AllowAcl int

	EnableTplModel int

	AllowTxfmDomainDistortion int
	TxDomainThresh            float64

	LfMotionThreshold MotionThreshold

	TxSizeSearchMethod TxSizeSearchMethod

	TxSizeSearchDepth int

	UseLp32x32Fdct int

	ModeSkipStart int

	ReferenceMasking int

	PartitionSearchType PartitionSearchType

	AlwaysThisBlockSize common.BlockSize

	LessRectangularCheck int

	UseSquarePartitionOnly  int
	UseSquareOnlyThreshHigh common.BlockSize
	UseSquareOnlyThreshLow  common.BlockSize

	PruneRefFrameForRectPartitions int

	AutoMinMaxPartitionSize AutoMinMaxMode
	RdAutoPartitionMinLimit common.BlockSize

	DefaultMinPartitionSize common.BlockSize
	DefaultMaxPartitionSize common.BlockSize

	AdjustPartitioningFromLastFrame int

	LastPartitioningRedoFrequency int

	DisableSplitMask int

	AdaptiveMotionSearch int

	EnhancedFullPixelMotionSearch int

	ExhaustiveSearchesThresh int

	MeshPatterns [sfMaxMeshSteps]MeshPattern

	ScheduleModeSearch int

	AdaptivePredInterpFilter int

	AdaptiveModeSearch int

	PruneSingleModeBasedOnMvDiffModeRate int

	CbPredFilterSearch int

	EarlyTermInterpSearchPlaneRd int

	CbPartitionSearch int

	MotionFieldModeSearch int

	AltRefSearchFp int

	UseQuantFp int

	ForceFrameBoost int

	MaxDeltaQindex int

	ModeSearchSkipFlags uint

	DisableFilterSearchVarThresh uint

	IntraYModeMask  [common.TxSizes]int
	IntraUvModeMask [common.TxSizes]int

	IntraYModeBsizeMask [common.BlockSizes]int

	UseRdBreakout int

	UseUvIntraRdEstimate int

	LpfPick LpfPickMethod

	UseFastCoefUpdates FastCoeffUpdate

	UseNonrdPickMode int

	InterModeMask [common.BlockSizes]int

	UseFastCoefCosting int

	RecodeToleranceLow  int
	RecodeToleranceHigh int

	MaxIntraBsize common.BlockSize

	ReuseInterPredSby int

	EncodeBreakoutThresh int

	DefaultInterpFilter vp9dec.InterpFilter

	TxSizeSearchBreakout int

	AdaptiveInterpFilterSearch int

	InterpFilterSearchMask int

	PartitionSearchBreakoutThr PartitionSearchBreakoutThr

	RdMlPartition RdMlPartition

	SimpleModelRdFromVar int

	ShortCircuitFlatBlocks int

	ShortCircuitLowTempVar int

	LimitNewmvEarlyExit int

	BiasGolden int

	BaseMvAggressive int

	CopyPartitionFlag int

	UseSourceSad int

	UseSimpleBlockYrd int

	AdaptPartitionSourceSad int
	AdaptPartitionThresh    int

	UseAltrefOnepass int

	UseCompoundNonrdPickmode int

	NonrdKeyframe int

	SvcUseLowresPart int

	OvershootDetectionCbrRt OvershootDetectionCbrRt

	Disable16x16PartNonKey int

	DisableGoldenRef int

	UseAccurateSubpelSearch SubpelSearchType

	TemporalFilterSearchMethod SearchMethods

	NonrdUseMlPartition int

	VariancePartThreshMult int

	ForceSmoothInterpol int

	RtIntraDcOnlyLowContent int

	AllowSkipTxfmAcDc int
}

// vp9SpeedDispatchContent maps govpx's ScreenContentMode int8 to the libvpx
// VP9E_CONTENT enum integer used by the configurator. The numeric values
// (DEFAULT=0, SCREEN=1, FILM=2) match libvpx.
//
// libvpx: vp9_encoder.h vp9e_tune_content enum.
type vp9SpeedDispatchContent int

const (
	vp9ContentDefault vp9SpeedDispatchContent = 0
	vp9ContentScreen  vp9SpeedDispatchContent = 1
	vp9ContentFilm    vp9SpeedDispatchContent = 2
)

// vp9FrameContentType mirrors libvpx FRAME_CONTENT_TYPE.
//
// libvpx: vp9/encoder/vp9_firstpass.h:73-77.
type vp9FrameContentType int

const (
	vp9FCNormal            vp9FrameContentType = 0
	vp9FCGraphicsAnimation vp9FrameContentType = 1
)

const vp9FCAnimationThresh = 0.15

// vp9MeshDensityLevels is the count of mesh-density rows in libvpx's
// good_quality_mesh_patterns table.
//
// libvpx: vp9_speed_features.c:28.
const vp9MeshDensityLevels = 3

// Mesh search patterns. libvpx: vp9_speed_features.c:21-34.
var vp9BestQualityMeshPattern = [2][sfMaxMeshSteps]MeshPattern{
	{{Range: 64, Interval: 4}, {Range: 28, Interval: 2}, {Range: 15, Interval: 1}, {Range: 7, Interval: 1}},
	{{Range: 64, Interval: 8}, {Range: 28, Interval: 4}, {Range: 15, Interval: 1}, {Range: 7, Interval: 1}},
}

var vp9GoodQualityMeshPatterns = [vp9MeshDensityLevels][sfMaxMeshSteps]MeshPattern{
	{{Range: 64, Interval: 8}, {Range: 28, Interval: 4}, {Range: 15, Interval: 1}, {Range: 7, Interval: 1}},
	{{Range: 64, Interval: 8}, {Range: 14, Interval: 2}, {Range: 7, Interval: 1}, {Range: 7, Interval: 1}},
	{{Range: 64, Interval: 16}, {Range: 24, Interval: 8}, {Range: 12, Interval: 4}, {Range: 7, Interval: 1}},
}

// tx_dom_thresholds. libvpx: vp9_speed_features.c:216.
var vp9TxDomThresholds = [6]float64{99.0, 14.0, 12.0, 8.0, 4.0, 0.0}

// qopt_thresholds. libvpx: vp9_speed_features.c:217.
var vp9QoptThresholds = [6]float64{99.0, 12.0, 10.0, 4.0, 2.0, 0.0}

// vp9SpeedFrameContext carries the per-frame state libvpx reads via cpi->common
// and cpi->rc when the configurator runs. The encoder fills this in at frame
// setup time so the framesize-dependent step sees the exact same inputs libvpx
// uses on the corresponding frame.
//
// refreshAltRefFrame / refreshGoldenFrame / isSrcFrameAltRef mirror
// cpi->refresh_alt_ref_frame / cpi->refresh_golden_frame /
// cpi->rc.is_src_frame_alt_ref. They feed frame_is_kf_gf_arf() so the
// configurator's boosted-frame branches activate on GF/ARF frames, not just
// keyframes.
//
// libvpx: vp9_encoder.h:1013-1016 frame_is_kf_gf_arf().
type vp9SpeedFrameContext struct {
	width               int
	height              int
	showFrame           bool
	frameType           common.FrameType
	intraOnly           bool
	refreshAltRefFrame  bool
	refreshGoldenFrame  bool
	isSrcFrameAltRef    bool
	baseQIndex          int
	framesSinceKey      int
	avgFrameLowMotion   int
	avgFrameQindexInter int
	currentVideoFrame   int
	frContentType       vp9FrameContentType
	internalImageEdge   bool

	// svc carries the per-frame SVC state libvpx reads via cpi->svc / cpi->use_svc
	// from the speed-features dispatcher. Single-layer encoders see
	// vp9SVCDefault() (NumberSpatialLayers=NumberTemporalLayers=1, UseSvc=false).
	//
	// libvpx: vp9_speed_features.c set_rt_speed_feature_framesize_independent
	// reads SVC *svc = &cpi->svc.
	svc vp9SVCState

	// externalResize mirrors cpi->external_resize. libvpx sets the flag in
	// vp9_change_config() when the encoder's frame size shrinks without
	// triggering a full re-allocation (sf.use_source_sad / reference_masking /
	// copy_partition_flag gates).
	//
	// libvpx: vp9_encoder.c:2153, vp9_speed_features.c:519, 659, 723.
	externalResize bool

	// lastFrameDropped mirrors cpi->last_frame_dropped, set after a frame-drop
	// decision in the rate-control loop. Used by sf->copy_partition_flag at
	// speed 7 to gate partition-copy across the just-dropped boundary.
	//
	// libvpx: vp9_encoder.h cpi->last_frame_dropped,
	// vp9_speed_features.c:722.
	lastFrameDropped bool

	// resizeStateOrig mirrors `cpi->resize_state == ORIG`. govpx does not run
	// the libvpx internal dynamic-resize state machine; the configurator path
	// only consults ORIG so resizeStateOrig defaults to true for single-layer
	// encoders. libvpx sets ORIG when no internal downscale is active.
	//
	// libvpx: vp9_encoder.h cpi->resize_state RESIZE_STATE enum.
	resizeStateOrig bool

	// disableOvershootMaxqCbr mirrors cpi->rc.disable_overshoot_maxq_cbr. Used
	// only on the rate-control overshoot detection path.
	//
	// libvpx: vp9_ratectrl.h rc->disable_overshoot_maxq_cbr.
	disableOvershootMaxqCbr bool
}

// vp9DefaultSpeedFrameContext returns the configurator context built from
// VP9EncoderOptions only. Per-frame state defaults to "key-frame intra-only at
// frame 0", which matches the libvpx framesize_independent path that runs
// before the first frame is encoded.
func (e *VP9Encoder) vp9DefaultSpeedFrameContext() vp9SpeedFrameContext {
	ctx := vp9SpeedFrameContext{
		width:           e.opts.Width,
		height:          e.opts.Height,
		showFrame:       true,
		frameType:       common.KeyFrame,
		intraOnly:       true,
		resizeStateOrig: true,
	}
	if e != nil {
		ctx.svc = e.svc
	} else {
		ctx.svc = vp9SVCDefault()
	}
	return ctx
}

// vp9PerFrameSpeedContextArgs carries the per-frame inputs that drive
// frame_is_kf_gf_arf() and the framesize-dependent SF picks. Mirrors the
// subset of cpi->common / cpi->refresh_*_frame / cpi->rc fields libvpx reads
// at the top of encode_frame_to_data_rate.
//
// libvpx: vp9_encoder.h:1013-1016 frame_is_kf_gf_arf,
// vp9_speed_features.c:919-1096 vp9_set_speed_features_framesize_independent.
type vp9PerFrameSpeedContextArgs struct {
	IsKey              bool
	IntraOnly          bool
	ShowFrame          bool
	RefreshGoldenFrame bool
	RefreshAltRefFrame bool
	IsSrcFrameAltRef   bool
	BaseQIndex         int
}

// vp9PerFrameSpeedContext builds a configurator context for a specific frame.
// The caller supplies the per-frame state libvpx reads via cpi->common,
// cpi->refresh_alt_ref_frame, cpi->refresh_golden_frame, and
// cpi->rc.is_src_frame_alt_ref. The remaining encoder-state fields
// (framesSinceKey, avgFrameLowMotion, avgFrameQindexInter, currentVideoFrame)
// are pulled from the live rate-control / frame counters so the framesize-
// dependent dispatcher sees the same inputs libvpx feeds it.
//
// libvpx: vp9_encoder.c:2635 / 3754 — same two-step protocol invoked per
// frame at top-of-encode.
func (e *VP9Encoder) vp9PerFrameSpeedContext(args vp9PerFrameSpeedContextArgs) vp9SpeedFrameContext {
	frameType := common.InterFrame
	if args.IsKey {
		frameType = common.KeyFrame
	}
	ctx := vp9SpeedFrameContext{
		width:               e.opts.Width,
		height:              e.opts.Height,
		showFrame:           args.ShowFrame,
		frameType:           frameType,
		intraOnly:           args.IntraOnly,
		refreshAltRefFrame:  args.RefreshAltRefFrame,
		refreshGoldenFrame:  args.RefreshGoldenFrame,
		isSrcFrameAltRef:    args.IsSrcFrameAltRef,
		baseQIndex:          args.BaseQIndex,
		framesSinceKey:      int(e.rc.framesSinceKey),
		avgFrameLowMotion:   100,
		avgFrameQindexInter: int(e.rc.avgFrameQIndexInter),
		currentVideoFrame:   e.frameIndex,
		frContentType:       e.vp9FrameContentTypeForSpeedFeatures(),
		internalImageEdge:   e.vp9InternalImageEdgeForSpeedFeatures(),
		svc:                 e.svc,
		// govpx's runtime resize always triggers a full re-allocation in
		// applyVP9ResolutionChange(), so libvpx's external_resize (set only
		// when the resize *did not* realloc) is never observable here.
		// libvpx: vp9_encoder.c:2153-2166.
		externalResize: false,
		// libvpx: vp9_encoder.h cpi->last_frame_dropped.
		lastFrameDropped: e.lastFrameDropped,
		// govpx does not run libvpx's internal dynamic-resize loop, so
		// cpi->resize_state == ORIG always.
		resizeStateOrig: true,
		// Mirror VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR into the per-frame
		// speed-feature context so overshoot-detection speed gates see the
		// same runtime bit as rate control.
		// libvpx: vp9_ratectrl.h rc->disable_overshoot_maxq_cbr.
		disableOvershootMaxqCbr: e.rc.disableOvershootMaxQCBR,
	}
	return ctx
}

func (e *VP9Encoder) vp9FrameContentTypeForSpeedFeatures() vp9FrameContentType {
	if e == nil || !e.twoPass.enabled() {
		return vp9FCNormal
	}
	row := e.twoPass.statsForFrame()
	if row.IntraSkipPct >= vp9FCAnimationThresh {
		return vp9FCGraphicsAnimation
	}
	return vp9FCNormal
}

func (e *VP9Encoder) vp9InternalImageEdgeForSpeedFeatures() bool {
	if e == nil || !e.twoPass.enabled() {
		return false
	}
	row := e.twoPass.statsForFrame()
	return row.InactiveZoneRows > 0 || row.InactiveZoneCols > 0
}

func (e *VP9Encoder) vp9DeadlineModeChanged() bool {
	if e == nil || !e.deadlineModePreviousFrameSet || e.frameIndex == 0 {
		return false
	}
	return vp9ResolveDeadlineMode(e.opts.Deadline) != e.deadlineModePreviousFrame
}

func (e *VP9Encoder) vp9LatchDeadlineModePreviousFrame() {
	if e == nil {
		return
	}
	e.deadlineModePreviousFrame = vp9ResolveDeadlineMode(e.opts.Deadline)
	e.deadlineModePreviousFrameSet = true
}

// vp9ApplySpeedFeatures runs the libvpx framesize-independent and
// framesize-dependent configurators. It must be called whenever speed-affecting
// options change (CpuUsed, Deadline, ScreenContentMode, RateControlMode), and
// also at frame setup so the framesize-dependent SF picks see the actual
// per-frame state.
//
// libvpx: vp9_encoder.c:2635 / 3754 / 3765 — same two-step protocol.
func (e *VP9Encoder) vp9ApplySpeedFeatures(ctx vp9SpeedFrameContext) {
	if e == nil {
		return
	}
	// libvpx: vp9_noise_estimate.c:129 — ne->enabled is recomputed by
	// vp9_update_noise_estimate at the top of encode_frame_to_data_rate
	// (vp9_encoder.c:4142), which runs before the speed-features dispatch
	// at vp9_encoder.c:3754. Refresh here so the consumer at
	// vp9_speed_features.c:777-782 reads the same predicate libvpx
	// evaluates whether the SF dispatch fires at setup, on options
	// change, or per-frame at top-of-encode.
	e.vp9NoiseEstimateRefreshEnabled()
	vp9SetSpeedFeaturesFramesizeIndependent(e, &e.sf, e.vp9SpeedFeatureCPUUsed(), ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &e.sf, e.vp9SpeedFeatureCPUUsed(), ctx)
}

// vp9DeadlineMode is the libvpx MODE selector govpx maps from the
// DeadlineBestQuality / DeadlineGoodQuality / DeadlineRealtime enum.
//
// libvpx: vp9_encoder.h MODE enum (GOOD=0, BEST=1, REALTIME=2).
type vp9DeadlineMode int

const (
	vp9ModeGood     vp9DeadlineMode = 0
	vp9ModeBest     vp9DeadlineMode = 1
	vp9ModeRealtime vp9DeadlineMode = 2
)

// vp9ResolveDeadlineMode maps govpx Deadline to the libvpx MODE picked by the
// SPEED_FEATURES dispatcher. DeadlineBestQuality maps to GOOD (libvpx's GOOD
// path serves the best-quality preset for cpu_used==0), DeadlineGoodQuality to
// GOOD, and DeadlineRealtime to REALTIME, matching govpx's existing oracle
// rate-control routing.
func vp9ResolveDeadlineMode(d Deadline) vp9DeadlineMode {
	switch d {
	case DeadlineRealtime:
		return vp9ModeRealtime
	default:
		return vp9ModeGood
	}
}

// vp9ResolveContent maps govpx's ScreenContentMode int8 to the libvpx
// vp9e_tune_content enum value used by the configurator.
func vp9ResolveContent(c int8) vp9SpeedDispatchContent {
	switch c {
	case int8(VP9ScreenContentScreen):
		return vp9ContentScreen
	case int8(VP9ScreenContentFilm):
		return vp9ContentFilm
	default:
		return vp9ContentDefault
	}
}

// vp9MinDim returns VPXMIN(width, height).
func vp9MinDim(w, h int) int {
	if w < h {
		return w
	}
	return h
}

// vp9FrameIsIntraOnly mirrors libvpx's frame_is_intra_only().
func vp9FrameIsIntraOnly(ctx vp9SpeedFrameContext) bool {
	return ctx.frameType == common.KeyFrame || ctx.intraOnly
}

// vp9FrameIsKfGfArf mirrors libvpx's frame_is_boosted(), which delegates to
// frame_is_kf_gf_arf():
//
//	return frame_is_intra_only(&cpi->common) || cpi->refresh_alt_ref_frame ||
//	       (cpi->refresh_golden_frame && !cpi->rc.is_src_frame_alt_ref);
//
// libvpx: vp9_speed_features.c:38-40 (frame_is_boosted),
// vp9_encoder.h:1013-1016 (frame_is_kf_gf_arf).
func vp9FrameIsKfGfArf(ctx vp9SpeedFrameContext) bool {
	if vp9FrameIsIntraOnly(ctx) {
		return true
	}
	if ctx.refreshAltRefFrame {
		return true
	}
	if ctx.refreshGoldenFrame && !ctx.isSrcFrameAltRef {
		return true
	}
	return false
}

// vp9SetPartitionMinLimit mirrors set_partition_min_limit().
//
// libvpx: vp9_speed_features.c:48-62.
func vp9SetPartitionMinLimit(width, height int) common.BlockSize {
	screenArea := width * height
	if screenArea < 1280*720 {
		return common.Block4x4
	}
	if screenArea < 1920*1080 {
		return common.Block8x8
	}
	return common.Block16x16
}

// vp9SetSpeedFeaturesFramesizeIndependent is the libvpx
// vp9_set_speed_features_framesize_independent() port. It first applies the
// best-quality defaults, then dispatches to set_rt_speed_feature_framesize_
// independent or set_good_speed_feature_framesize_independent, and finally
// applies the pass-0 / pass-1 / lossless fixups.
//
// libvpx: vp9_speed_features.c:919-1096.
func vp9SetSpeedFeaturesFramesizeIndependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	// best quality defaults. libvpx: vp9_speed_features.c:928-1020.
	sf.FrameParameterUpdate = 1
	sf.Mv.SearchMethod = SearchMethodNStep
	sf.RecodeLoop = RecodeLoopAllowFirst
	sf.Mv.SubpelSearchMethod = SubpelTree
	sf.Mv.SubpelSearchLevel = 2
	sf.Mv.SubpelForceStop = EighthPel
	if e.opts.Lossless {
		sf.OptimizeCoefficients = 0
	} else {
		sf.OptimizeCoefficients = 1
	}
	sf.Mv.ReduceFirstStepSize = 0
	sf.CoeffProbAppxStep = 1
	sf.Mv.AutoMvStepSize = 0
	sf.Mv.FullpelSearchStepParam = 6
	sf.Mv.UseDownsampledSad = 0
	sf.CompInterJointSearchIterLevel = 0
	sf.TxSizeSearchMethod = UseFullRD
	sf.UseLp32x32Fdct = 0
	sf.AdaptiveMotionSearch = 0
	sf.EnhancedFullPixelMotionSearch = 1
	sf.AdaptivePredInterpFilter = 0
	sf.AdaptiveModeSearch = 0
	sf.PruneSingleModeBasedOnMvDiffModeRate = 0
	sf.CbPredFilterSearch = 0
	sf.EarlyTermInterpSearchPlaneRd = 0
	sf.CbPartitionSearch = 0
	sf.MotionFieldModeSearch = 0
	sf.AltRefSearchFp = 0
	sf.UseQuantFp = 0
	sf.ReferenceMasking = 0
	sf.PartitionSearchType = SearchPartition
	sf.LessRectangularCheck = 0
	sf.UseSquarePartitionOnly = 0
	sf.UseSquareOnlyThreshHigh = common.BlockSizes
	sf.UseSquareOnlyThreshLow = common.Block4x4
	sf.AutoMinMaxPartitionSize = AutoMinMaxNotInUse
	sf.RdAutoPartitionMinLimit = common.Block4x4
	sf.DefaultMaxPartitionSize = common.Block64x64
	sf.DefaultMinPartitionSize = common.Block4x4
	sf.AdjustPartitioningFromLastFrame = 0
	sf.LastPartitioningRedoFrequency = 4
	sf.DisableSplitMask = 0
	sf.ModeSearchSkipFlags = 0
	sf.ForceFrameBoost = 0
	sf.MaxDeltaQindex = 0
	sf.DisableFilterSearchVarThresh = 0
	sf.AdaptiveInterpFilterSearch = 0
	sf.AllowTxfmDomainDistortion = 0
	sf.TxDomainThresh = 99.0
	if sf.OptimizeCoefficients != 0 {
		sf.TrellisOptTxRd.Method = EnableTrellisOptM
	} else {
		sf.TrellisOptTxRd.Method = DisableTrellisOpt
	}
	sf.TrellisOptTxRd.Thresh = 99.0
	sf.AllowAcl = 1
	if e.opts.EnableTPL {
		sf.EnableTplModel = 1
	} else {
		sf.EnableTplModel = 0
	}
	sf.PruneRefFrameForRectPartitions = 0
	sf.TemporalFilterSearchMethod = SearchMethodMesh
	sf.AllowSkipTxfmAcDc = 0

	for i := range common.TxSizes {
		sf.IntraYModeMask[i] = sfIntraAll
		sf.IntraUvModeMask[i] = sfIntraAll
	}
	sf.UseRdBreakout = 0
	sf.SkipEncodeSb = 0
	sf.UseUvIntraRdEstimate = 0
	sf.AllowSkipRecode = 0
	sf.LpfPick = LpfPickFromFullImage
	sf.UseFastCoefUpdates = TwoLoop
	sf.UseFastCoefCosting = 0
	sf.ModeSkipStart = 30 // MAX_MODES, libvpx: vp9_rd.h:41.
	sf.ScheduleModeSearch = 0
	sf.UseNonrdPickMode = 0
	for i := range common.BlockSizes {
		sf.InterModeMask[i] = sfInterAll
	}
	sf.MaxIntraBsize = common.Block64x64
	sf.ReuseInterPredSby = 0
	sf.AlwaysThisBlockSize = common.Block16x16
	sf.EncodeBreakoutThresh = 0
	sf.RecodeToleranceLow = 12
	sf.RecodeToleranceHigh = 25
	sf.DefaultInterpFilter = vp9dec.InterpSwitchable
	sf.SimpleModelRdFromVar = 0
	sf.ShortCircuitFlatBlocks = 0
	sf.ShortCircuitLowTempVar = 0
	sf.LimitNewmvEarlyExit = 0
	sf.BiasGolden = 0
	sf.BaseMvAggressive = 0
	sf.RdMlPartition.PruneRectThresh[0] = -1
	sf.RdMlPartition.PruneRectThresh[1] = -1
	sf.RdMlPartition.PruneRectThresh[2] = -1
	sf.RdMlPartition.PruneRectThresh[3] = -1
	sf.RdMlPartition.VarPruning = 0
	sf.UseAccurateSubpelSearch = Use8Taps

	// libvpx: vp9_speed_features.c:1022-1025 — speed-up defaults even at best
	// quality.
	sf.AdaptiveRdThresh = 1
	sf.TxSizeSearchBreakout = 1
	sf.TxSizeSearchDepth = 2

	// libvpx: vp9_speed_features.c:1027-1039. govpx does not track
	// twopass.fr_content_type yet — assume non-graphics content so the
	// exhaustive search threshold falls into the INT_MAX bucket.
	sf.ExhaustiveSearchesThresh = math.MaxInt32
	meshDensityLevel := 1
	for i := range sfMaxMeshSteps {
		sf.MeshPatterns[i] = vp9BestQualityMeshPattern[meshDensityLevel][i]
	}

	mode := vp9ResolveDeadlineMode(e.opts.Deadline)
	switch mode {
	case vp9ModeRealtime:
		vp9SetRtSpeedFeatureFramesizeIndependent(e, sf, speed, vp9ResolveContent(e.opts.ScreenContentMode), ctx)
	case vp9ModeGood:
		vp9SetGoodSpeedFeatureFramesizeIndependent(e, sf, speed, ctx)
	}
	// libvpx GOOD-mode dispatch also covers BEST in practice — see
	// vp9_speed_features.c:1041-1046 (only GOOD/REALTIME branches exist; BEST
	// inherits the framesize-independent defaults above).

	// libvpx: vp9_speed_features.c:1052 — pass==1 disables coefficient
	// optimization. govpx's two-pass first pass is opts.TwoPassFirstPass.
	if e.vp9SpeedIsFirstPass() {
		sf.OptimizeCoefficients = 0
	}

	// libvpx: vp9_speed_features.c:1055-1058 — pass==0 (one-pass).
	if e.vp9SpeedIsOnePass() {
		sf.RecodeLoop = RecodeLoopDisallow
		sf.OptimizeCoefficients = 0
	}

	// libvpx: vp9_speed_features.c:1083-1086.
	if !e.opts.FramePeriodicBoost {
		sf.MaxDeltaQindex = 0
	}

	// libvpx: vp9_speed_features.c:1093-1095 — row_mt bit-exactness override.
	// govpx's row-mt is bit-exact by construction (single goroutine per tile
	// column), so this only triggers when adaptive_rd_thresh_row_mt is off and
	// max_threads > 1, matching libvpx.
	if sf.AdaptiveRdThreshRowMt == 0 && e.opts.Threads > 1 && e.opts.RowMT {
		sf.AdaptiveRdThresh = 0
	}
}

// vp9SetSpeedFeaturesFramesizeDependent ports
// vp9_set_speed_features_framesize_dependent(). The "best quality defaults"
// reset partition_search_breakout_thr and rd_ml_partition fields at the top,
// then dispatch to the realtime / good handler, and finally apply the
// disable_split_mask interaction with adaptive_pred_interp_filter.
//
// libvpx: vp9_speed_features.c:873-917.
func vp9SetSpeedFeaturesFramesizeDependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	// best quality defaults. libvpx: vp9_speed_features.c:881-884.
	sf.PartitionSearchBreakoutThr.Dist = 1 << 19
	sf.PartitionSearchBreakoutThr.Rate = 80
	sf.RdMlPartition.SearchEarlyTermination = 0
	sf.RdMlPartition.SearchBreakout = 0

	mode := vp9ResolveDeadlineMode(e.opts.Deadline)
	switch mode {
	case vp9ModeRealtime:
		vp9SetRtSpeedFeatureFramesizeDependent(e, sf, speed, ctx)
	case vp9ModeGood:
		vp9SetGoodSpeedFeatureFramesizeDependent(e, sf, speed, ctx)
	}

	// libvpx: vp9_speed_features.c:893-895.
	if sf.DisableSplitMask == sfDisableAllSplit {
		sf.AdaptivePredInterpFilter = 0
	}

	// libvpx: vp9_speed_features.c:914-916.
	if sf.AdaptiveRdThreshRowMt == 0 && e.opts.Threads > 1 && e.opts.RowMT {
		sf.AdaptiveRdThresh = 0
	}
}

// vp9SetGoodSpeedFeatureFramesizeDependent ports
// set_good_speed_feature_framesize_dependent().
//
// libvpx: vp9_speed_features.c:64-214.
func vp9SetGoodSpeedFeatureFramesizeDependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	minFrameSize := vp9MinDim(ctx.width, ctx.height)
	is480pOrLarger := minFrameSize >= 480
	is720pOrLarger := minFrameSize >= 720
	is1080pOrLarger := minFrameSize >= 1080
	is2160pOrLarger := minFrameSize >= 2160
	boosted := vp9FrameIsKfGfArf(ctx)

	// speed 0 features. libvpx: vp9_speed_features.c:76-79.
	sf.PartitionSearchBreakoutThr.Dist = 1 << 20
	sf.PartitionSearchBreakoutThr.Rate = 80
	sf.UseSquareOnlyThreshHigh = common.BlockSizes
	sf.UseSquareOnlyThreshLow = common.Block4x4

	if is480pOrLarger {
		// libvpx: vp9_speed_features.c:81-86.
		sf.RdMlPartition.SearchEarlyTermination = 1
		sf.RecodeToleranceHigh = 45
	} else {
		sf.UseSquareOnlyThreshHigh = common.Block32x32
	}
	if is720pOrLarger {
		sf.AltRefSearchFp = 1
	}

	if !is1080pOrLarger {
		// libvpx: vp9_speed_features.c:93-104.
		sf.RdMlPartition.SearchBreakout = 1
		if is720pOrLarger {
			sf.RdMlPartition.SearchBreakoutThresh[0] = 0.0
			sf.RdMlPartition.SearchBreakoutThresh[1] = 0.0
			sf.RdMlPartition.SearchBreakoutThresh[2] = 0.0
		} else {
			sf.RdMlPartition.SearchBreakoutThresh[0] = 2.5
			sf.RdMlPartition.SearchBreakoutThresh[1] = 1.5
			sf.RdMlPartition.SearchBreakoutThresh[2] = 1.5
		}
	}

	if !is720pOrLarger {
		// libvpx: vp9_speed_features.c:106-111.
		if is480pOrLarger {
			if boosted {
				sf.PruneSingleModeBasedOnMvDiffModeRate = 0
			} else {
				sf.PruneSingleModeBasedOnMvDiffModeRate = 1
			}
		} else {
			sf.PruneSingleModeBasedOnMvDiffModeRate = 1
		}
	}

	if speed >= 1 {
		// libvpx: vp9_speed_features.c:113-142.
		sf.RdMlPartition.SearchEarlyTermination = 0
		sf.RdMlPartition.SearchBreakout = 1
		if is480pOrLarger {
			sf.UseSquareOnlyThreshHigh = common.Block64x64
		} else {
			sf.UseSquareOnlyThreshHigh = common.Block32x32
		}
		sf.UseSquareOnlyThreshLow = common.Block16x16
		if is720pOrLarger {
			if ctx.showFrame {
				sf.DisableSplitMask = sfDisableAllSplit
			} else {
				sf.DisableSplitMask = sfDisableAllInterSplit
			}
			sf.PartitionSearchBreakoutThr.Dist = 1 << 22
			sf.RdMlPartition.SearchBreakoutThresh[0] = -5.0
			sf.RdMlPartition.SearchBreakoutThresh[1] = -5.0
			sf.RdMlPartition.SearchBreakoutThresh[2] = -9.0
		} else {
			sf.DisableSplitMask = sfDisableCompoundSplit
			sf.PartitionSearchBreakoutThr.Dist = 1 << 21
			sf.RdMlPartition.SearchBreakoutThresh[0] = -1.0
			sf.RdMlPartition.SearchBreakoutThresh[1] = -1.0
			sf.RdMlPartition.SearchBreakoutThresh[2] = -1.0
		}
	}

	if speed >= 2 {
		// libvpx: vp9_speed_features.c:144-174.
		sf.UseSquareOnlyThreshHigh = common.Block4x4
		sf.UseSquareOnlyThreshLow = common.BlockSizes
		if is720pOrLarger {
			if ctx.showFrame {
				sf.DisableSplitMask = sfDisableAllSplit
			} else {
				sf.DisableSplitMask = sfDisableAllInterSplit
			}
			sf.AdaptivePredInterpFilter = 0
			sf.PartitionSearchBreakoutThr.Dist = 1 << 24
			sf.PartitionSearchBreakoutThr.Rate = 120
			sf.RdMlPartition.SearchBreakout = 0
		} else {
			sf.DisableSplitMask = sfLastAndIntraSplitOnly
			sf.PartitionSearchBreakoutThr.Dist = 1 << 22
			sf.PartitionSearchBreakoutThr.Rate = 100
			sf.RdMlPartition.SearchBreakoutThresh[0] = 0.0
			sf.RdMlPartition.SearchBreakoutThresh[1] = -1.0
			sf.RdMlPartition.SearchBreakoutThresh[2] = -4.0
		}
		sf.RdAutoPartitionMinLimit = vp9SetPartitionMinLimit(ctx.width, ctx.height)

		if is2160pOrLarger {
			// libvpx: vp9_speed_features.c:165-173.
			sf.UseSquarePartitionOnly = 1
			sf.IntraYModeMask[common.Tx32x32] = sfIntraDC
			sf.IntraUvModeMask[common.Tx32x32] = sfIntraDC
			sf.AltRefSearchFp = 1
			sf.CbPredFilterSearch = 2
			sf.AdaptiveInterpFilterSearch = 1
			sf.DisableSplitMask = sfDisableAllSplit
		}
	}

	if speed >= 3 {
		// libvpx: vp9_speed_features.c:176-190.
		sf.RdMlPartition.SearchBreakout = 0
		if is720pOrLarger {
			sf.DisableSplitMask = sfDisableAllSplit
			if ctx.baseQIndex < 220 {
				sf.ScheduleModeSearch = 1
			} else {
				sf.ScheduleModeSearch = 0
			}
			sf.PartitionSearchBreakoutThr.Dist = 1 << 25
			sf.PartitionSearchBreakoutThr.Rate = 200
		} else {
			sf.MaxIntraBsize = common.Block32x32
			sf.DisableSplitMask = sfDisableAllInterSplit
			if ctx.baseQIndex < 175 {
				sf.ScheduleModeSearch = 1
			} else {
				sf.ScheduleModeSearch = 0
			}
			sf.PartitionSearchBreakoutThr.Dist = 1 << 23
			sf.PartitionSearchBreakoutThr.Rate = 120
		}
	}

	// libvpx: vp9_speed_features.c:195-199.
	if speed >= 1 && e.twoPass.enabled() &&
		(ctx.frContentType == vp9FCGraphicsAnimation || ctx.internalImageEdge) {
		sf.DisableSplitMask = sfDisableCompoundSplit
	}

	if speed >= 4 {
		// libvpx: vp9_speed_features.c:201-209.
		sf.PartitionSearchBreakoutThr.Rate = 300
		if is720pOrLarger {
			sf.PartitionSearchBreakoutThr.Dist = 1 << 26
		} else {
			sf.PartitionSearchBreakoutThr.Dist = 1 << 24
		}
		sf.DisableSplitMask = sfDisableAllSplit
	}

	if speed >= 5 {
		// libvpx: vp9_speed_features.c:211-213.
		sf.PartitionSearchBreakoutThr.Rate = 500
	}
}

// vp9SetGoodSpeedFeatureFramesizeIndependent ports
// set_good_speed_feature_framesize_independent().
//
// libvpx: vp9_speed_features.c:219-411.
func vp9SetGoodSpeedFeatureFramesizeIndependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	boosted := vp9FrameIsKfGfArf(ctx)

	// libvpx: vp9_speed_features.c:227-256.
	sf.AdaptiveInterpFilterSearch = 1
	sf.AdaptivePredInterpFilter = 1
	sf.AdaptiveRdThresh = 1
	sf.AdaptiveRdThreshRowMt = 0
	sf.AllowSkipRecode = 1
	sf.LessRectangularCheck = 1
	sf.Mv.AutoMvStepSize = 1
	sf.Mv.UseDownsampledSad = 1
	sf.PruneRefFrameForRectPartitions = 1
	sf.TemporalFilterSearchMethod = SearchMethodNStep
	sf.TxSizeSearchBreakout = 1
	if boosted {
		sf.UseSquarePartitionOnly = 0
	} else {
		sf.UseSquarePartitionOnly = 1
	}
	sf.EarlyTermInterpSearchPlaneRd = 1
	sf.CbPredFilterSearch = 1
	if sf.OptimizeCoefficients != 0 {
		sf.TrellisOptTxRd.Method = EnableTrellisOptTxRdResidualMse
	} else {
		sf.TrellisOptTxRd.Method = DisableTrellisOpt
	}
	if boosted {
		sf.TrellisOptTxRd.Thresh = 4.0
	} else {
		sf.TrellisOptTxRd.Thresh = 3.0
	}

	sf.IntraYModeMask[common.Tx32x32] = sfIntraDCHV
	sf.CompInterJointSearchIterLevel = 1

	// libvpx: vp9_speed_features.c:249-250 — reference masking unsupported in
	// dynamic resize. govpx does not currently expose resize_mode; assume
	// non-dynamic so reference_masking = 1.
	sf.ReferenceMasking = 1

	sf.RdMlPartition.VarPruning = 1
	sf.RdMlPartition.PruneRectThresh[0] = -1
	sf.RdMlPartition.PruneRectThresh[1] = 350
	sf.RdMlPartition.PruneRectThresh[2] = 325
	sf.RdMlPartition.PruneRectThresh[3] = 250

	// libvpx: vp9_speed_features.c:258-262.
	if ctx.frContentType == vp9FCGraphicsAnimation {
		sf.ExhaustiveSearchesThresh = 1 << 22
	} else {
		sf.ExhaustiveSearchesThresh = math.MaxInt32
	}

	for i := range sfMaxMeshSteps {
		sf.MeshPatterns[i] = vp9GoodQualityMeshPatterns[0][i]
	}

	if speed >= 1 {
		// libvpx: vp9_speed_features.c:272-316.
		if boosted {
			sf.RdMlPartition.VarPruning = 0
		} else {
			sf.RdMlPartition.VarPruning = 1
		}
		sf.RdMlPartition.PruneRectThresh[1] = 225
		sf.RdMlPartition.PruneRectThresh[2] = 225
		sf.RdMlPartition.PruneRectThresh[3] = 225

		// libvpx: vp9_speed_features.c:278-288.
		if e.twoPass.enabled() &&
			(ctx.frContentType == vp9FCGraphicsAnimation || ctx.internalImageEdge) {
			sf.UseSquarePartitionOnly = boolToInt(!boosted)
		} else {
			sf.UseSquarePartitionOnly = boolToInt(!vp9FrameIsIntraOnly(ctx))
		}

		sf.AllowTxfmDomainDistortion = 1
		idx := speed
		if idx >= 6 {
			idx = 5
		}
		sf.TxDomainThresh = vp9TxDomThresholds[idx]
		if sf.OptimizeCoefficients != 0 {
			sf.TrellisOptTxRd.Method = EnableTrellisOptTxRdSrcVar
		} else {
			sf.TrellisOptTxRd.Method = DisableTrellisOpt
		}
		sf.TrellisOptTxRd.Thresh = vp9QoptThresholds[idx]
		sf.LessRectangularCheck = 1
		sf.UseRdBreakout = 1
		sf.AdaptiveMotionSearch = 1
		sf.AdaptiveRdThresh = 2
		sf.Mv.SubpelSearchLevel = 1
		if vp9ResolveContent(e.opts.ScreenContentMode) != vp9ContentFilm {
			sf.ModeSkipStart = 10
		}
		sf.AllowAcl = 0

		sf.IntraUvModeMask[common.Tx32x32] = sfIntraDCHV
		if vp9ResolveContent(e.opts.ScreenContentMode) != vp9ContentFilm {
			sf.IntraYModeMask[common.Tx16x16] = sfIntraDCHV
			sf.IntraUvModeMask[common.Tx16x16] = sfIntraDCHV
		}

		sf.RecodeToleranceLow = 15
		sf.RecodeToleranceHigh = 30

		// libvpx: vp9_speed_features.c:313-315.
		if ctx.frContentType == vp9FCGraphicsAnimation {
			sf.ExhaustiveSearchesThresh = 1 << 23
		} else {
			sf.ExhaustiveSearchesThresh = math.MaxInt32
		}
		sf.UseAccurateSubpelSearch = Use4Taps
	}

	if speed >= 2 {
		// libvpx: vp9_speed_features.c:319-356.
		sf.RdMlPartition.VarPruning = 0
		// libvpx: vp9_speed_features.c:321-324 — oxcf->vbr_corpus_complexity
		// fork. When corpus VBR is active, libvpx widens the recode loop to
		// ALLOW_RECODE_FIRST (loop after the first encode attempt) so the
		// per-frame Q can hit the corpus-relative target; otherwise the
		// non-corpus path uses ALLOW_RECODE_KFARFGF.
		if e.opts.VBRCorpusComplexity != 0 {
			sf.RecodeLoop = RecodeLoopAllowFirst
		} else {
			sf.RecodeLoop = RecodeLoopAllowKfArfGf
		}

		if vp9FrameIsKfGfArf(ctx) {
			sf.TxSizeSearchMethod = UseFullRD
		} else {
			sf.TxSizeSearchMethod = UseLargestAll
		}

		if ctx.frameType == common.KeyFrame {
			sf.ModeSearchSkipFlags = 0
		} else {
			sf.ModeSearchSkipFlags = FlagSkipIntraDirMismatch | FlagSkipIntraBestInter |
				FlagSkipCompBestIntra | FlagSkipIntraLowVar
		}
		sf.DisableFilterSearchVarThresh = 100
		sf.CompInterJointSearchIterLevel = 2
		sf.AutoMinMaxPartitionSize = AutoMinMaxRelaxedNeighboring
		sf.RecodeToleranceHigh = 45
		sf.EnhancedFullPixelMotionSearch = 0
		sf.PruneRefFrameForRectPartitions = 0
		sf.RdMlPartition.PruneRectThresh[1] = -1
		sf.RdMlPartition.PruneRectThresh[2] = -1
		sf.RdMlPartition.PruneRectThresh[3] = -1
		sf.Mv.SubpelSearchLevel = 0

		// libvpx: vp9_speed_features.c:345-353.
		if ctx.frContentType == vp9FCGraphicsAnimation {
			for i := range sfMaxMeshSteps {
				sf.MeshPatterns[i] = vp9GoodQualityMeshPatterns[1][i]
			}
		}

		sf.UseAccurateSubpelSearch = Use2Taps
	}

	if speed >= 3 {
		// libvpx: vp9_speed_features.c:358-383.
		if vp9FrameIsIntraOnly(ctx) {
			sf.UseSquarePartitionOnly = 0
		} else {
			sf.UseSquarePartitionOnly = 1
		}
		if vp9FrameIsIntraOnly(ctx) {
			sf.TxSizeSearchMethod = UseFullRD
		} else {
			sf.TxSizeSearchMethod = UseLargestAll
		}
		sf.Mv.SubpelSearchMethod = SubpelTreePruned
		sf.AdaptivePredInterpFilter = 0
		sf.AdaptiveModeSearch = 1
		if boosted {
			sf.CbPartitionSearch = 0
		} else {
			sf.CbPartitionSearch = 1
		}
		sf.CbPredFilterSearch = 2
		sf.AltRefSearchFp = 1
		sf.RecodeLoop = RecodeLoopAllowKfMaxBw
		sf.AdaptiveRdThresh = 3
		sf.ModeSkipStart = 6
		sf.IntraYModeMask[common.Tx32x32] = sfIntraDC
		sf.IntraUvModeMask[common.Tx32x32] = sfIntraDC
		if ctx.frContentType == vp9FCGraphicsAnimation {
			for i := range sfMaxMeshSteps {
				sf.MeshPatterns[i] = vp9GoodQualityMeshPatterns[2][i]
			}
		}
	}

	if speed >= 4 {
		// libvpx: vp9_speed_features.c:385-398.
		sf.UseSquarePartitionOnly = 1
		sf.TxSizeSearchMethod = UseLargestAll
		sf.Mv.SearchMethod = SearchMethodBigDia
		sf.Mv.SubpelSearchMethod = SubpelTreePrunedMore
		sf.AdaptiveRdThresh = 4
		if ctx.frameType != common.KeyFrame {
			sf.ModeSearchSkipFlags |= FlagEarlyTerminate
		}
		sf.DisableFilterSearchVarThresh = 200
		sf.UseLp32x32Fdct = 1
		sf.UseFastCoefUpdates = OneLoopReduced
		sf.UseFastCoefCosting = 1
		if boosted {
			sf.MotionFieldModeSearch = 0
		} else {
			sf.MotionFieldModeSearch = 1
		}
	}

	if speed >= 5 {
		// libvpx: vp9_speed_features.c:400-410.
		sf.OptimizeCoefficients = 0
		sf.Mv.SearchMethod = SearchMethodHex
		sf.DisableFilterSearchVarThresh = 500
		for i := range common.TxSizes {
			sf.IntraYModeMask[i] = sfIntraDC
			sf.IntraUvModeMask[i] = sfIntraDC
		}
		sf.Mv.ReduceFirstStepSize = 1
		sf.SimpleModelRdFromVar = 1
	}
}

// vp9SetRtSpeedFeatureFramesizeDependent ports
// set_rt_speed_feature_framesize_dependent().
//
// libvpx: vp9_speed_features.c:414-450.
func vp9SetRtSpeedFeatureFramesizeDependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	minDim := vp9MinDim(ctx.width, ctx.height)

	if speed >= 1 {
		// libvpx: vp9_speed_features.c:419-426.
		if minDim >= 720 {
			if ctx.showFrame {
				sf.DisableSplitMask = sfDisableAllSplit
			} else {
				sf.DisableSplitMask = sfDisableAllInterSplit
			}
		} else {
			sf.DisableSplitMask = sfDisableCompoundSplit
		}
	}

	if speed >= 2 {
		// libvpx: vp9_speed_features.c:428-435.
		if minDim >= 720 {
			if ctx.showFrame {
				sf.DisableSplitMask = sfDisableAllSplit
			} else {
				sf.DisableSplitMask = sfDisableAllInterSplit
			}
		} else {
			sf.DisableSplitMask = sfLastAndIntraSplitOnly
		}
	}

	if speed >= 5 {
		// libvpx: vp9_speed_features.c:437-444.
		sf.PartitionSearchBreakoutThr.Rate = 200
		if minDim >= 720 {
			sf.PartitionSearchBreakoutThr.Dist = 1 << 25
		} else {
			sf.PartitionSearchBreakoutThr.Dist = 1 << 23
		}
	}

	if speed >= 7 {
		// libvpx: vp9_speed_features.c:446-449.
		if minDim >= 720 {
			sf.EncodeBreakoutThresh = 800
		} else {
			sf.EncodeBreakoutThresh = 300
		}
	}
}

// vp9SetRtSpeedFeatureFramesizeIndependent ports
// set_rt_speed_feature_framesize_independent().
//
// libvpx: vp9_speed_features.c:452-871.
func vp9SetRtSpeedFeatureFramesizeIndependent(e *VP9Encoder, sf *SpeedFeatures, speed int, content vp9SpeedDispatchContent, ctx vp9SpeedFrameContext) {
	isKeyframe := ctx.frameType == common.KeyFrame
	var framesSinceKey int
	if !isKeyframe {
		framesSinceKey = ctx.framesSinceKey
	}

	// libvpx: vp9_speed_features.c:458-483.
	sf.StaticSegmentation = 0
	sf.AdaptiveRdThresh = 1
	sf.AdaptiveRdThreshRowMt = 0
	sf.UseFastCoefCosting = 1
	sf.ExhaustiveSearchesThresh = math.MaxInt32
	sf.AllowAcl = 0
	sf.CopyPartitionFlag = 0
	sf.UseSourceSad = 0
	sf.UseSimpleBlockYrd = 0
	sf.AdaptPartitionSourceSad = 0
	sf.UseAltrefOnepass = 0
	sf.UseCompoundNonrdPickmode = 0
	sf.NonrdKeyframe = 0
	sf.SvcUseLowresPart = 0
	sf.OvershootDetectionCbrRt = OvershootNoDetection
	sf.Disable16x16PartNonKey = 0
	sf.DisableGoldenRef = 0
	sf.EnableTplModel = 0
	sf.EnhancedFullPixelMotionSearch = 0
	sf.UseAccurateSubpelSearch = Use2Taps
	sf.NonrdUseMlPartition = 0
	sf.VariancePartThreshMult = 1
	sf.CbPredFilterSearch = 0
	sf.ForceSmoothInterpol = 0
	sf.RtIntraDcOnlyLowContent = 0
	sf.Mv.EnableAdaptiveSubpelForceStop = 0

	if speed >= 1 {
		// libvpx: vp9_speed_features.c:485-504.
		sf.AllowTxfmDomainDistortion = 1
		sf.TxDomainThresh = 0.0
		sf.TrellisOptTxRd.Method = DisableTrellisOpt
		sf.TrellisOptTxRd.Thresh = 0.0
		if vp9FrameIsIntraOnly(ctx) {
			sf.UseSquarePartitionOnly = 0
		} else {
			sf.UseSquarePartitionOnly = 1
		}
		sf.LessRectangularCheck = 1
		if vp9FrameIsIntraOnly(ctx) {
			sf.TxSizeSearchMethod = UseFullRD
		} else {
			sf.TxSizeSearchMethod = UseLargestAll
		}

		sf.UseRdBreakout = 1

		sf.AdaptiveMotionSearch = 1
		sf.AdaptivePredInterpFilter = 1
		sf.Mv.AutoMvStepSize = 1
		sf.AdaptiveRdThresh = 2
		sf.IntraYModeMask[common.Tx32x32] = sfIntraDCHV
		sf.IntraUvModeMask[common.Tx32x32] = sfIntraDCHV
		sf.IntraUvModeMask[common.Tx16x16] = sfIntraDCHV
	}

	if speed >= 2 {
		// libvpx: vp9_speed_features.c:506-542.
		if ctx.frameType == common.KeyFrame {
			sf.ModeSearchSkipFlags = 0
		} else {
			sf.ModeSearchSkipFlags = FlagSkipIntraDirMismatch | FlagSkipIntraBestInter |
				FlagSkipCompBestIntra | FlagSkipIntraLowVar
		}
		sf.AdaptivePredInterpFilter = 2

		// libvpx: vp9_speed_features.c:514-531 — reference masking, SVC
		// downscale check. Enabled when there is exactly one spatial layer; the
		// dynamic-resize / vp9_is_scaled() inner check that libvpx adds for
		// resize_mode==RESIZE_DYNAMIC or external_resize==1 is a no-op in govpx
		// because applyVP9ResolutionChange() always invalidates every reference
		// frame (refValid[] = false), so the per-ref scale check would skip all
		// slots regardless.
		if ctx.svc.NumberSpatialLayers == 1 {
			sf.ReferenceMasking = 1
		} else {
			sf.ReferenceMasking = 0
		}
		// libvpx: vp9_speed_features.c:518-530 — inner per-reference
		// vp9_is_scaled() loop only fires when reference_masking==1 AND
		// (external_resize==1 OR resize_mode==RESIZE_DYNAMIC). govpx has no
		// dynamic-resize mode and external_resize is never observable (see
		// vp9SpeedFrameContext.externalResize), so the inner clear is a no-op.

		sf.DisableFilterSearchVarThresh = 50
		sf.CompInterJointSearchIterLevel = 2
		sf.AutoMinMaxPartitionSize = AutoMinMaxRelaxedNeighboring
		sf.LfMotionThreshold = LowMotionThreshold
		sf.AdjustPartitioningFromLastFrame = 1
		sf.LastPartitioningRedoFrequency = 3
		sf.UseLp32x32Fdct = 1
		sf.ModeSkipStart = 11
		sf.IntraYModeMask[common.Tx16x16] = sfIntraDCHV
	}

	if speed >= 3 {
		// libvpx: vp9_speed_features.c:544-556.
		sf.UseSquarePartitionOnly = 1
		sf.DisableFilterSearchVarThresh = 100
		sf.UseUvIntraRdEstimate = 1
		sf.SkipEncodeSb = 1
		sf.Mv.SubpelSearchLevel = 0
		sf.AdaptiveRdThresh = 4
		sf.ModeSkipStart = 6
		sf.AllowSkipRecode = 0
		sf.OptimizeCoefficients = 0
		sf.DisableSplitMask = sfDisableAllSplit
		sf.LpfPick = LpfPickFromQ
	}

	if speed >= 4 {
		// libvpx: vp9_speed_features.c:558-583.
		if e.opts.RateControlMode == RateControlVBR && e.opts.LookaheadFrames > 0 {
			sf.UseAltrefOnepass = 1
		}
		sf.Mv.SubpelForceStop = QuarterPel
		for i := range common.TxSizes {
			sf.IntraYModeMask[i] = sfIntraDCHV
			sf.IntraUvModeMask[i] = sfIntraDC
		}
		sf.IntraYModeMask[common.Tx32x32] = sfIntraDC
		sf.FrameParameterUpdate = 0
		sf.Mv.SearchMethod = SearchMethodFastHex
		sf.AllowSkipRecode = 0
		sf.MaxIntraBsize = common.Block32x32
		sf.UseFastCoefCosting = 0
		if isKeyframe {
			sf.UseQuantFp = 0
		} else {
			sf.UseQuantFp = 1
		}
		sf.InterModeMask[common.Block32x32] = sfInterNearestNewZero
		sf.InterModeMask[common.Block32x64] = sfInterNearestNewZero
		sf.InterModeMask[common.Block64x32] = sfInterNearestNewZero
		sf.InterModeMask[common.Block64x64] = sfInterNearestNewZero
		sf.AdaptiveRdThresh = 2
		if isKeyframe {
			sf.UseFastCoefUpdates = TwoLoop
		} else {
			sf.UseFastCoefUpdates = OneLoopReduced
		}
		sf.ModeSearchSkipFlags = FlagSkipIntraDirMismatch
		if isKeyframe {
			sf.TxSizeSearchMethod = UseLargestAll
		} else {
			sf.TxSizeSearchMethod = UseTx8x8
		}
		sf.PartitionSearchType = VarBasedPartition
	}

	if speed >= 5 {
		// libvpx: vp9_speed_features.c:585-660.
		sf.UseAltrefOnepass = 0
		if isKeyframe {
			sf.UseQuantFp = 0
		} else {
			sf.UseQuantFp = 1
		}
		if isKeyframe {
			sf.AutoMinMaxPartitionSize = AutoMinMaxRelaxedNeighboring
		} else {
			sf.AutoMinMaxPartitionSize = AutoMinMaxStrictNeighboring
		}
		sf.DefaultMaxPartitionSize = common.Block32x32
		sf.DefaultMinPartitionSize = common.Block8x8
		if isKeyframe ||
			(sf.LastPartitioningRedoFrequency != 0 &&
				framesSinceKey%(sf.LastPartitioningRedoFrequency<<1) == 1) {
			sf.ForceFrameBoost = 1
		} else {
			sf.ForceFrameBoost = 0
		}
		if isKeyframe {
			sf.MaxDeltaQindex = 20
		} else {
			sf.MaxDeltaQindex = 15
		}
		sf.PartitionSearchType = ReferencePartition
		// libvpx: vp9_speed_features.c:597-600 — is_src_frame_alt_ref VBR
		// override:
		//
		//   if (cpi->oxcf.rc_mode == VPX_VBR && cpi->oxcf.lag_in_frames > 0 &&
		//       cpi->rc.is_src_frame_alt_ref) {
		//     sf->partition_search_type = VAR_BASED_PARTITION;
		//   }
		//
		// ctx.isSrcFrameAltRef threads rc->is_src_frame_alt_ref through the
		// per-frame configurator context (vp9PerFrameSpeedContextArgs /
		// vp9SpeedFrameContext).
		if e.opts.RateControlMode == RateControlVBR && e.opts.LookaheadFrames > 0 &&
			ctx.isSrcFrameAltRef {
			sf.PartitionSearchType = VarBasedPartition
		}

		sf.UseNonrdPickMode = 1
		sf.AllowSkipRecode = 0
		sf.InterModeMask[common.Block32x32] = sfInterNearestNewZero
		sf.InterModeMask[common.Block32x64] = sfInterNearestNewZero
		sf.InterModeMask[common.Block64x32] = sfInterNearestNewZero
		sf.InterModeMask[common.Block64x64] = sfInterNearestNewZero
		sf.AdaptiveRdThresh = 2
		sf.ReuseInterPredSby = 1
		sf.CoeffProbAppxStep = 4
		if isKeyframe {
			sf.UseFastCoefUpdates = TwoLoop
		} else {
			sf.UseFastCoefUpdates = OneLoopReduced
		}
		sf.ModeSearchSkipFlags = FlagSkipIntraDirMismatch
		if isKeyframe {
			sf.TxSizeSearchMethod = UseLargestAll
		} else {
			sf.TxSizeSearchMethod = UseTx8x8
		}
		sf.SimpleModelRdFromVar = 1
		if e.opts.RateControlMode == RateControlVBR {
			sf.Mv.SearchMethod = SearchMethodNStep
		}

		if !isKeyframe {
			// libvpx: vp9_speed_features.c:617-633.
			if content == vp9ContentScreen {
				for i := range common.BlockSizes {
					if i >= common.Block32x32 {
						sf.IntraYModeBsizeMask[i] = sfIntraDCHV
					} else {
						sf.IntraYModeBsizeMask[i] = sfIntraDCTmHV
					}
				}
			} else {
				for i := range common.BlockSizes {
					if i > common.Block16x16 {
						sf.IntraYModeBsizeMask[i] = sfIntraDC
					} else {
						sf.IntraYModeBsizeMask[i] = sfIntraDCHV
					}
				}
			}
		}
		if content == vp9ContentScreen {
			sf.ShortCircuitFlatBlocks = 1
		}
		if e.opts.RateControlMode == RateControlCBR && content != vp9ContentScreen {
			// libvpx: vp9_speed_features.c:637-641.
			sf.LimitNewmvEarlyExit = 1
			if !ctx.svc.UseSvc {
				sf.BiasGolden = 1
			}
		}
		// libvpx: vp9_speed_features.c:642-644 — Keep nonrd_keyframe = 1 for
		// non-base spatial layers to prevent increase in encoding time.
		if ctx.svc.UseSvc && ctx.svc.SpatialLayerID > 0 {
			sf.NonrdKeyframe = 1
		}

		// libvpx: vp9_speed_features.c:645-652 — CBR overshoot detection.
		// libvpx adds use_svc to the inner RE_ENCODE_MAXQ gate so that SVC
		// non-base resolutions skip the recode path. mirror that.
		if ctx.frameType != common.KeyFrame && ctx.resizeStateOrig &&
			e.opts.RateControlMode == RateControlCBR && !ctx.disableOvershootMaxqCbr {
			if ctx.width*ctx.height <= 352*288 && !ctx.svc.UseSvc &&
				content != vp9ContentScreen {
				sf.OvershootDetectionCbrRt = OvershootReEncodeMaxQ
			} else {
				sf.OvershootDetectionCbrRt = OvershootFastDetectionMaxQ
			}
		}
		if e.opts.RateControlMode == RateControlVBR && e.opts.LookaheadFrames > 0 &&
			ctx.width <= 1280 && ctx.height <= 720 {
			sf.UseAltrefOnepass = 1
			sf.UseCompoundNonrdPickmode = 1
		}
		if ctx.width*ctx.height > 1280*720 {
			sf.CbPredFilterSearch = 2
		}
		// libvpx: vp9_speed_features.c:659 — if (!cpi->external_resize) sf->use_source_sad = 1;
		if !ctx.externalResize {
			sf.UseSourceSad = 1
		}
	}

	if speed >= 6 {
		// libvpx: vp9_speed_features.c:662-697.
		if e.opts.RateControlMode == RateControlVBR && e.opts.LookaheadFrames > 0 {
			sf.UseAltrefOnepass = 1
			sf.UseCompoundNonrdPickmode = 1
		}
		sf.PartitionSearchType = VarBasedPartition
		sf.Mv.SearchMethod = SearchMethodNStep
		sf.Mv.ReduceFirstStepSize = 1
		sf.SkipEncodeSb = 0

		if sf.UseSourceSad != 0 {
			sf.AdaptPartitionSourceSad = 1
			if ctx.width*ctx.height <= 640*360 {
				sf.AdaptPartitionThresh = 40000
			} else {
				sf.AdaptPartitionThresh = 60000
			}
			// libvpx: vp9_speed_features.c:676-683 — content_state_sb_fd alloc:
			//
			//   if (cpi->content_state_sb_fd == NULL &&
			//       (!cpi->use_svc ||
			//        svc->spatial_layer_id == svc->number_spatial_layers - 1)) {
			//     CHECK_MEM_ERROR(&cm->error, cpi->content_state_sb_fd,
			//         (uint8_t *)vpx_calloc(
			//             (cm->mi_stride >> 3) * ((cm->mi_rows >> 3) + 1),
			//             sizeof(uint8_t)));
			//   }
			//
			// govpx is single-layer so the !use_svc clause is always
			// satisfied. vp9EnsureContentStateSbFd is the libvpx allocation
			// body, sized from the frame mi grid via vp9MiDimensionsForFrame.
			e.vp9EnsureContentStateSbFd(ctx.width, ctx.height)
		}
		if e.opts.RateControlMode == RateControlCBR && content != vp9ContentScreen {
			sf.ShortCircuitLowTempVar = 1
		}
		// libvpx: vp9_speed_features.c:689-693.
		if ctx.svc.TemporalLayerID > 0 {
			sf.AdaptiveRdThresh = 4
			sf.LimitNewmvEarlyExit = 0
			sf.BaseMvAggressive = 1
		}

		if ctx.frameType != common.KeyFrame && ctx.resizeStateOrig &&
			e.opts.RateControlMode == RateControlCBR && !ctx.disableOvershootMaxqCbr {
			sf.OvershootDetectionCbrRt = OvershootFastDetectionMaxQ
		}
	}

	if speed >= 7 {
		// libvpx: vp9_speed_features.c:699-749.
		sf.AdaptPartitionSourceSad = 0
		sf.AdaptiveRdThresh = 3
		sf.Mv.SearchMethod = SearchMethodFastDiamond
		sf.Mv.FullpelSearchStepParam = 10
		// libvpx: vp9_speed_features.c:704-711 — For SVC: use better mv search
		// on base temporal layer, and only on base spatial layer if highest
		// resolution is above 640x360.
		if ctx.svc.NumberTemporalLayers > 2 && ctx.svc.TemporalLayerID == 0 &&
			(ctx.svc.SpatialLayerID == 0 ||
				e.opts.Width*e.opts.Height <= 640*360) {
			sf.Mv.SearchMethod = SearchMethodNStep
			sf.Mv.FullpelSearchStepParam = 6
		}
		// libvpx: vp9_speed_features.c:712-716.
		if ctx.svc.TemporalLayerID > 0 || ctx.svc.SpatialLayerID > 1 {
			sf.UseSimpleBlockYrd = 1
			if ctx.svc.NonReferenceFrame {
				sf.Mv.SubpelSearchMethod = SubpelTreePrunedEvenMore
			}
		}
		if ctx.svc.UseSvc && e.opts.RowMT && e.opts.Threads > 1 {
			// libvpx: vp9_speed_features.c:717-718.
			sf.AdaptiveRdThreshRowMt = 1
		}
		// libvpx: vp9_speed_features.c:721-734 — partition-copy plumbing.
		e.maxCopiedFrame = 0
		if !ctx.lastFrameDropped && ctx.resizeStateOrig && !ctx.externalResize &&
			(!ctx.svc.UseSvc ||
				(ctx.svc.SpatialLayerID == ctx.svc.NumberSpatialLayers-1 &&
					!ctx.svc.LastLayerDropped[ctx.svc.NumberSpatialLayers-1])) {
			sf.CopyPartitionFlag = 1
			e.maxCopiedFrame = 2
			// The top temporal enhancement layer (for number of temporal
			// layers > 1) are non-reference frames, so use large/max value for
			// max_copied_frame.
			if ctx.svc.NumberTemporalLayers > 1 &&
				ctx.svc.TemporalLayerID == ctx.svc.NumberTemporalLayers-1 {
				e.maxCopiedFrame = 255
			}
		}
		// libvpx: vp9_speed_features.c:735-741 — For SVC: enable use of lower
		// resolution partition for higher resolution, only for 3 spatial
		// layers and when config/top resolution is above VGA. Enable only for
		// non-base temporal layer frames.
		if ctx.svc.UseSvc && ctx.svc.UsePartitionReuse &&
			ctx.svc.NumberSpatialLayers == 3 && ctx.svc.TemporalLayerID > 0 &&
			e.opts.Width*e.opts.Height > 640*480 {
			sf.SvcUseLowresPart = 1
		}
		// libvpx: vp9_speed_features.c:742-747 — For SVC when golden is used
		// as second temporal reference: to avoid encode time increase only use
		// this feature on base temporal layer.
		if ctx.svc.UseSvc && ctx.svc.UseGfTemporalRefCurrentLayer &&
			ctx.svc.TemporalLayerID > 0 {
			e.refFrameFlags &^= vp9GoldFlag
		}
		if ctx.width*ctx.height > 640*480 {
			sf.CbPredFilterSearch = 2
		}
	}

	if speed >= 8 {
		// libvpx: vp9_speed_features.c:751-793.
		sf.AdaptiveRdThresh = 4
		sf.SkipEncodeSb = 1
		// libvpx: vp9_speed_features.c:754-757.
		if ctx.svc.NumberSpatialLayers > 1 && !ctx.svc.SimulcastMode {
			sf.NonrdKeyframe = 0
		} else {
			sf.NonrdKeyframe = 1
		}
		// libvpx: vp9_speed_features.c:758 — if (!cpi->use_svc) cpi->max_copied_frame = 4;
		if !ctx.svc.UseSvc {
			e.maxCopiedFrame = 4
		}

		if e.opts.RowMT && e.opts.Threads > 1 {
			sf.AdaptiveRdThreshRowMt = 1
		}

		if !vp9FrameIsIntraOnly(ctx) && ctx.width*ctx.height <= 352*288 {
			sf.NonrdUseMlPartition = 1
		}

		if content == vp9ContentScreen {
			sf.Mv.SubpelForceStop = HalfPel
		}
		sf.RtIntraDcOnlyLowContent = 1
		// libvpx: vp9_speed_features.c:771-789 — !cpi->use_svc gate so SVC at
		// speed 8 does not engage the aggressive short-circuit / adaptive_rd
		// reduction path.
		if !ctx.svc.UseSvc && e.opts.RateControlMode == RateControlCBR &&
			content != vp9ContentScreen {
			sf.ShortCircuitLowTempVar = 3
			// libvpx: vp9_speed_features.c:777-782 — for HD CBR, drop
			// short_circuit_low_temp_var to level 2 when the noise
			// estimator flags the source as medium-or-higher noise:
			//
			//	if (cpi->noise_estimate.enabled && cm->width >= 1280 &&
			//	    cm->height >= 720) {
			//	  NOISE_LEVEL noise_level =
			//	      vp9_noise_estimate_extract_level(&cpi->noise_estimate);
			//	  if (noise_level >= kMedium) sf->short_circuit_low_temp_var = 2;
			//	}
			if e.noiseEstimate.Enabled && ctx.width >= 1280 && ctx.height >= 720 {
				noiseLevel := e.noiseEstimate.ExtractLevel()
				if noiseLevel >= encoder.NoiseLevelMedium {
					sf.ShortCircuitLowTempVar = 2
				}
			}
			if ctx.width*ctx.height > 352*288 {
				sf.AdaptiveRdThresh = 1
			} else {
				sf.AdaptiveRdThresh = 2
			}
		}
		sf.LimitNewmvEarlyExit = 0
		sf.UseSimpleBlockYrd = 1
		if ctx.width*ctx.height > 352*288 {
			sf.CbPredFilterSearch = 2
		}
	}

	if speed >= 9 {
		// libvpx: vp9_speed_features.c:795-814.
		if !isKeyframe {
			for i := range common.BlockSizes {
				sf.IntraYModeBsizeMask[i] = sfIntraDC
			}
		}
		sf.CbPredFilterSearch = 2
		sf.Mv.EnableAdaptiveSubpelForceStop = 1
		sf.Mv.AdaptSubpelForceStop.MvThresh = 1
		sf.Mv.AdaptSubpelForceStop.ForceStopBelow = QuarterPel
		sf.Mv.AdaptSubpelForceStop.ForceStopAbove = HalfPel
		if ctx.frameType != common.KeyFrame && ctx.width >= 320 && ctx.height >= 240 {
			sf.Disable16x16PartNonKey = 1
		}
		if e.opts.RateControlMode == RateControlCBR {
			sf.DisableGoldenRef = 1
		}
		if ctx.avgFrameLowMotion < 70 {
			sf.DefaultInterpFilter = vp9dec.InterpBilinear
		}
		if ctx.width*ctx.height >= 640*360 {
			sf.VariancePartThreshMult = 2
		}
	}

	// libvpx: vp9_speed_features.c:819-823 — low-res low-Q disable for var
	// partition. Applies to all speeds.
	if ctx.frameType != common.KeyFrame && ctx.width*ctx.height <= 320*240 &&
		sf.PartitionSearchType == VarBasedPartition &&
		ctx.avgFrameQindexInter > 208 && ctx.currentVideoFrame > 8 {
		sf.Disable16x16PartNonKey = 1
	}

	// libvpx: vp9_speed_features.c:825-826.
	if sf.NonrdUseMlPartition != 0 {
		sf.PartitionSearchType = MlBasedPartition
	}

	// libvpx: vp9_speed_features.c:828-844 — altref-onepass FIXED_PARTITION
	// override + ARF usage counter allocation:
	//
	//   if (sf->use_altref_onepass) {
	//     if (cpi->rc.is_src_frame_alt_ref && cm->frame_type != KEY_FRAME) {
	//       sf->partition_search_type = FIXED_PARTITION;
	//       sf->always_this_block_size = BLOCK_64X64;
	//     }
	//     if (cpi->count_arf_frame_usage == NULL) {
	//       CHECK_MEM_ERROR(&cm->error, cpi->count_arf_frame_usage,
	//           (uint8_t *)vpx_calloc((cm->mi_stride >> 3) *
	//                                  ((cm->mi_rows >> 3) + 1),
	//                                  sizeof(*cpi->count_arf_frame_usage)));
	//     }
	//     if (cpi->count_lastgolden_frame_usage == NULL)
	//       CHECK_MEM_ERROR(&cm->error, cpi->count_lastgolden_frame_usage,
	//           (uint8_t *)vpx_calloc((cm->mi_stride >> 3) *
	//                                  ((cm->mi_rows >> 3) + 1),
	//                                  sizeof(*cpi->count_lastgolden_frame_usage)));
	//   }
	//
	// vp9EnsureArfFrameUsage allocates both counters with the libvpx
	// calc_mi_size-derived shape. ctx.isSrcFrameAltRef threads
	// rc->is_src_frame_alt_ref from the per-frame configurator context.
	if sf.UseAltrefOnepass != 0 {
		if ctx.isSrcFrameAltRef && ctx.frameType != common.KeyFrame {
			sf.PartitionSearchType = FixedPartition
			sf.AlwaysThisBlockSize = common.Block64x64
		}
		e.vp9EnsureArfFrameUsage(ctx.width, ctx.height)
	}

	// libvpx: vp9_speed_features.c:845-848.
	if ctx.svc.PreviousFrameIsIntraOnly {
		sf.PartitionSearchType = FixedPartition
		sf.AlwaysThisBlockSize = common.Block64x64
	}
	// libvpx: vp9_speed_features.c:849-857 — Special case for screen content:
	// increase motion search on base spatial layer when high motion is detected
	// or previous SL0 frame was dropped.
	if e.opts.ScreenContentMode == int8(VP9ScreenContentScreen) &&
		e.vp9SpeedFeatureCPUUsed() >= 5 &&
		(ctx.svc.HighNumBlocksWithMotion || ctx.svc.LastLayerDropped[0]) {
		sf.Mv.SearchMethod = SearchMethodNStep
		sf.Mv.FullpelSearchStepParam = 2
	}

	// libvpx: vp9_speed_features.c:858-861 — speed<=3 disables CYCLIC_REFRESH.
	if speed <= 3 && e.opts.AQMode == VP9AQCyclicRefresh {
		// libvpx writes back to cpi->oxcf.aq_mode = 0. govpx mirrors this by
		// clearing the encoder's AQ mode for subsequent frames so cyclic
		// refresh stops engaging at low speeds.
		e.opts.AQMode = VP9AQNone
		e.cyclicAQ.configure(false, e.opts.Width, e.opts.Height)
	}

	// libvpx: vp9_speed_features.c:863-866 — deadline switch nonrd_keyframe.
	if e.vp9DeadlineModeChanged() {
		sf.NonrdKeyframe = 1
	}

	// libvpx: vp9_speed_features.c:868-870 — forced off for SVC lowres-part.
	sf.SvcUseLowresPart = 0
}

// vp9SpeedIsFirstPass returns true when libvpx's oxcf->pass would be 1.
// govpx does not currently surface an explicit first-pass option; the
// corresponding libvpx fixup (force optimize_coefficients = 0) is therefore a
// no-op here.
//
// TODO: consumer requires opts.TwoPassFirstPass. libvpx:
// vp9_speed_features.c:1052.
func (e *VP9Encoder) vp9SpeedIsFirstPass() bool {
	return false
}

// vp9SpeedIsOnePass returns true when libvpx's oxcf->pass would be 0
// (one-pass encoding). govpx is one-pass unless TwoPassStats marks a second
// pass.
func (e *VP9Encoder) vp9SpeedIsOnePass() bool {
	if e == nil {
		return true
	}
	return len(e.opts.TwoPassStats) == 0
}
