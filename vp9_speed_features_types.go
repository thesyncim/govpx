package govpx

// VP9 speed features ported byte-for-byte from libvpx v1.16.0
// vp9/encoder/vp9_speed_features.{h,c}. Every enum value, struct field, and
// switch-case assignment in these files is cited inline as
// `// libvpx: vp9_speed_features.{h,c}:<line>`.

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

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
	// encoder.DefaultSVCState() (NumberSpatialLayers=NumberTemporalLayers=1, UseSvc=false).
	//
	// libvpx: vp9_speed_features.c set_rt_speed_feature_framesize_independent
	// reads SVC *svc = &cpi->svc.
	svc encoder.SVCState

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
