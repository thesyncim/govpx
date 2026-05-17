package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim pins the libvpx SPEED_FEATURES
// produced by set_rt_speed_feature_framesize_independent + best-quality
// defaults at cpu_used == 0 (REALTIME mode, speed == 0). Every assertion has
// a libvpx file:line citation so the verbatim correspondence is auditable.
//
// At speed == 0 NONE of the `if (speed >= N)` cascades in
// set_rt_speed_feature_framesize_independent (libvpx
// vp9_speed_features.c:485, 506, 544, 558, 585, 662, 699, 751, 795) or in
// set_rt_speed_feature_framesize_dependent (libvpx
// vp9_speed_features.c:419, 428, 437, 446) fire, so the resulting
// SPEED_FEATURES is the union of:
//
//   - best-quality defaults from vp9_set_speed_features_framesize_independent
//     (libvpx vp9_speed_features.c:928-1029)
//   - RT baseline overrides from set_rt_speed_feature_framesize_independent
//     (libvpx vp9_speed_features.c:458-483)
//   - best-quality defaults from vp9_set_speed_features_framesize_dependent
//     (libvpx vp9_speed_features.c:881-884)
//   - post-dispatch fixups (libvpx vp9_speed_features.c:1052, 1055-1058,
//     1060-1085, 1093-1095 — the one-pass branch, framesize-dependent
//     disable_split_mask interaction, etc.)
//
// libvpx: vp9_speed_features.c:452-483, 873-1096.
func TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    320,
		Height:   240,
		Deadline: DeadlineRealtime,
		CpuUsed:  0,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if got := e.vp9SpeedFeatureCPUUsed(); got != 0 {
		t.Fatalf("vp9SpeedFeatureCPUUsed = %d, want 0 (CpuUsed=0 untouched)", got)
	}

	var sf SpeedFeatures
	ctx := e.vp9DefaultSpeedFrameContext()
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 0, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 0, ctx)

	// libvpx: vp9_speed_features.c:458-483 — RT baseline overrides.
	if sf.StaticSegmentation != 0 {
		t.Errorf("StaticSegmentation = %d, want 0 (libvpx vp9_speed_features.c:458)", sf.StaticSegmentation)
	}
	if sf.AdaptiveRdThresh != 1 {
		t.Errorf("AdaptiveRdThresh = %d, want 1 (libvpx vp9_speed_features.c:459)", sf.AdaptiveRdThresh)
	}
	if sf.AdaptiveRdThreshRowMt != 0 {
		t.Errorf("AdaptiveRdThreshRowMt = %d, want 0 (libvpx vp9_speed_features.c:460)", sf.AdaptiveRdThreshRowMt)
	}
	if sf.UseFastCoefCosting != 1 {
		t.Errorf("UseFastCoefCosting = %d, want 1 (libvpx vp9_speed_features.c:461)", sf.UseFastCoefCosting)
	}
	if sf.AllowAcl != 0 {
		t.Errorf("AllowAcl = %d, want 0 (libvpx vp9_speed_features.c:463)", sf.AllowAcl)
	}
	if sf.CopyPartitionFlag != 0 {
		t.Errorf("CopyPartitionFlag = %d, want 0 (libvpx vp9_speed_features.c:464)", sf.CopyPartitionFlag)
	}
	if sf.UseSourceSad != 0 {
		t.Errorf("UseSourceSad = %d, want 0 (libvpx vp9_speed_features.c:465)", sf.UseSourceSad)
	}
	if sf.UseSimpleBlockYrd != 0 {
		t.Errorf("UseSimpleBlockYrd = %d, want 0 (libvpx vp9_speed_features.c:466)", sf.UseSimpleBlockYrd)
	}
	if sf.AdaptPartitionSourceSad != 0 {
		t.Errorf("AdaptPartitionSourceSad = %d, want 0 (libvpx vp9_speed_features.c:467)", sf.AdaptPartitionSourceSad)
	}
	if sf.UseAltrefOnepass != 0 {
		t.Errorf("UseAltrefOnepass = %d, want 0 (libvpx vp9_speed_features.c:468)", sf.UseAltrefOnepass)
	}
	if sf.UseCompoundNonrdPickmode != 0 {
		t.Errorf("UseCompoundNonrdPickmode = %d, want 0 (libvpx vp9_speed_features.c:469)", sf.UseCompoundNonrdPickmode)
	}
	if sf.NonrdKeyframe != 0 {
		t.Errorf("NonrdKeyframe = %d, want 0 (libvpx vp9_speed_features.c:470)", sf.NonrdKeyframe)
	}
	if sf.SvcUseLowresPart != 0 {
		t.Errorf("SvcUseLowresPart = %d, want 0 (libvpx vp9_speed_features.c:471, 870)", sf.SvcUseLowresPart)
	}
	if sf.OvershootDetectionCbrRt != OvershootNoDetection {
		t.Errorf("OvershootDetectionCbrRt = %d, want %d (libvpx vp9_speed_features.c:472)", sf.OvershootDetectionCbrRt, OvershootNoDetection)
	}
	if sf.Disable16x16PartNonKey != 0 {
		t.Errorf("Disable16x16PartNonKey = %d, want 0 (libvpx vp9_speed_features.c:473)", sf.Disable16x16PartNonKey)
	}
	if sf.DisableGoldenRef != 0 {
		t.Errorf("DisableGoldenRef = %d, want 0 (libvpx vp9_speed_features.c:474)", sf.DisableGoldenRef)
	}
	if sf.EnableTplModel != 0 {
		t.Errorf("EnableTplModel = %d, want 0 (libvpx vp9_speed_features.c:475)", sf.EnableTplModel)
	}
	if sf.EnhancedFullPixelMotionSearch != 0 {
		t.Errorf("EnhancedFullPixelMotionSearch = %d, want 0 (libvpx vp9_speed_features.c:476)", sf.EnhancedFullPixelMotionSearch)
	}
	if sf.UseAccurateSubpelSearch != Use2Taps {
		t.Errorf("UseAccurateSubpelSearch = %d, want %d (libvpx vp9_speed_features.c:477)", sf.UseAccurateSubpelSearch, Use2Taps)
	}
	if sf.NonrdUseMlPartition != 0 {
		t.Errorf("NonrdUseMlPartition = %d, want 0 (libvpx vp9_speed_features.c:478)", sf.NonrdUseMlPartition)
	}
	if sf.VariancePartThreshMult != 1 {
		t.Errorf("VariancePartThreshMult = %d, want 1 (libvpx vp9_speed_features.c:479)", sf.VariancePartThreshMult)
	}
	if sf.CbPredFilterSearch != 0 {
		t.Errorf("CbPredFilterSearch = %d, want 0 (libvpx vp9_speed_features.c:480)", sf.CbPredFilterSearch)
	}
	if sf.ForceSmoothInterpol != 0 {
		t.Errorf("ForceSmoothInterpol = %d, want 0 (libvpx vp9_speed_features.c:481)", sf.ForceSmoothInterpol)
	}
	if sf.RtIntraDcOnlyLowContent != 0 {
		t.Errorf("RtIntraDcOnlyLowContent = %d, want 0 (libvpx vp9_speed_features.c:482)", sf.RtIntraDcOnlyLowContent)
	}
	if sf.Mv.EnableAdaptiveSubpelForceStop != 0 {
		t.Errorf("Mv.EnableAdaptiveSubpelForceStop = %d, want 0 (libvpx vp9_speed_features.c:483)", sf.Mv.EnableAdaptiveSubpelForceStop)
	}

	// libvpx: vp9_speed_features.c:929-1029 — best-quality defaults survive the
	// RT baseline because RT does not overwrite them.
	if sf.Mv.SearchMethod != SearchMethodNStep {
		t.Errorf("Mv.SearchMethod = %d, want %d (libvpx vp9_speed_features.c:930)", sf.Mv.SearchMethod, SearchMethodNStep)
	}
	if sf.Mv.SubpelSearchMethod != SubpelTree {
		t.Errorf("Mv.SubpelSearchMethod = %d, want %d (libvpx vp9_speed_features.c:932)", sf.Mv.SubpelSearchMethod, SubpelTree)
	}
	if sf.Mv.SubpelSearchLevel != 2 {
		t.Errorf("Mv.SubpelSearchLevel = %d, want 2 (libvpx vp9_speed_features.c:933)", sf.Mv.SubpelSearchLevel)
	}
	if sf.Mv.SubpelForceStop != EighthPel {
		t.Errorf("Mv.SubpelForceStop = %d, want %d (libvpx vp9_speed_features.c:934)", sf.Mv.SubpelForceStop, EighthPel)
	}
	if sf.Mv.AutoMvStepSize != 0 {
		t.Errorf("Mv.AutoMvStepSize = %d, want 0 (libvpx vp9_speed_features.c:938)", sf.Mv.AutoMvStepSize)
	}
	if sf.Mv.FullpelSearchStepParam != 6 {
		t.Errorf("Mv.FullpelSearchStepParam = %d, want 6 (libvpx vp9_speed_features.c:939)", sf.Mv.FullpelSearchStepParam)
	}
	if sf.TxSizeSearchMethod != UseFullRD {
		t.Errorf("TxSizeSearchMethod = %d, want %d (libvpx vp9_speed_features.c:942)", sf.TxSizeSearchMethod, UseFullRD)
	}
	if sf.AdaptiveMotionSearch != 0 {
		t.Errorf("AdaptiveMotionSearch = %d, want 0 (libvpx vp9_speed_features.c:944)", sf.AdaptiveMotionSearch)
	}
	if sf.AdaptivePredInterpFilter != 0 {
		t.Errorf("AdaptivePredInterpFilter = %d, want 0 (libvpx vp9_speed_features.c:946)", sf.AdaptivePredInterpFilter)
	}
	if sf.UseQuantFp != 0 {
		t.Errorf("UseQuantFp = %d, want 0 (libvpx vp9_speed_features.c:954)", sf.UseQuantFp)
	}
	if sf.ReferenceMasking != 0 {
		t.Errorf("ReferenceMasking = %d, want 0 (libvpx vp9_speed_features.c:955)", sf.ReferenceMasking)
	}
	if sf.PartitionSearchType != SearchPartition {
		t.Errorf("PartitionSearchType = %d, want %d (libvpx vp9_speed_features.c:956)", sf.PartitionSearchType, SearchPartition)
	}
	if sf.LessRectangularCheck != 0 {
		t.Errorf("LessRectangularCheck = %d, want 0 (libvpx vp9_speed_features.c:957)", sf.LessRectangularCheck)
	}
	if sf.UseSquarePartitionOnly != 0 {
		t.Errorf("UseSquarePartitionOnly = %d, want 0 (libvpx vp9_speed_features.c:958)", sf.UseSquarePartitionOnly)
	}
	if sf.AutoMinMaxPartitionSize != AutoMinMaxNotInUse {
		t.Errorf("AutoMinMaxPartitionSize = %d, want %d (libvpx vp9_speed_features.c:961)", sf.AutoMinMaxPartitionSize, AutoMinMaxNotInUse)
	}
	if sf.DefaultMaxPartitionSize != common.Block64x64 {
		t.Errorf("DefaultMaxPartitionSize = %d, want %d (libvpx vp9_speed_features.c:963)", sf.DefaultMaxPartitionSize, common.Block64x64)
	}
	if sf.DefaultMinPartitionSize != common.Block4x4 {
		t.Errorf("DefaultMinPartitionSize = %d, want %d (libvpx vp9_speed_features.c:964)", sf.DefaultMinPartitionSize, common.Block4x4)
	}
	if sf.DisableSplitMask != 0 {
		t.Errorf("DisableSplitMask = %d, want 0 (libvpx vp9_speed_features.c:967)", sf.DisableSplitMask)
	}
	if sf.AllowTxfmDomainDistortion != 0 {
		t.Errorf("AllowTxfmDomainDistortion = %d, want 0 (libvpx vp9_speed_features.c:973)", sf.AllowTxfmDomainDistortion)
	}
	if sf.TxDomainThresh != 99.0 {
		t.Errorf("TxDomainThresh = %v, want 99.0 (libvpx vp9_speed_features.c:974)", sf.TxDomainThresh)
	}
	// libvpx: vp9_speed_features.c:975-977 — trellis_opt_tx_rd.method depends
	// on optimize_coefficients. At cpu_used=0 RT, the post-dispatch one-pass
	// fixup (line 1055-1058) zeroes OptimizeCoefficients, so trellis_opt_tx_rd
	// was set during the best-quality defaults (line 975) BEFORE the one-pass
	// fixup runs. The TrellisOptTxRd.Method we see is therefore the value
	// libvpx wrote at line 975-977 with optimize_coefficients == 1 (non-lossless).
	if sf.TrellisOptTxRd.Method != EnableTrellisOptM {
		t.Errorf("TrellisOptTxRd.Method = %d, want %d (libvpx vp9_speed_features.c:975-977 with optimize_coefficients=1 pre-fixup)", sf.TrellisOptTxRd.Method, EnableTrellisOptM)
	}
	if sf.TrellisOptTxRd.Thresh != 99.0 {
		t.Errorf("TrellisOptTxRd.Thresh = %v, want 99.0 (libvpx vp9_speed_features.c:977)", sf.TrellisOptTxRd.Thresh)
	}
	for i := range common.TxSizes {
		if sf.IntraYModeMask[i] != sfIntraAll {
			t.Errorf("IntraYModeMask[%d] = %#x, want %#x (libvpx vp9_speed_features.c:985)", i, sf.IntraYModeMask[i], sfIntraAll)
		}
		if sf.IntraUvModeMask[i] != sfIntraAll {
			t.Errorf("IntraUvModeMask[%d] = %#x, want %#x (libvpx vp9_speed_features.c:986)", i, sf.IntraUvModeMask[i], sfIntraAll)
		}
	}
	if sf.UseRdBreakout != 0 {
		t.Errorf("UseRdBreakout = %d, want 0 (libvpx vp9_speed_features.c:988)", sf.UseRdBreakout)
	}
	if sf.SkipEncodeSb != 0 {
		t.Errorf("SkipEncodeSb = %d, want 0 (libvpx vp9_speed_features.c:989)", sf.SkipEncodeSb)
	}
	if sf.AllowSkipRecode != 0 {
		t.Errorf("AllowSkipRecode = %d, want 0 (libvpx vp9_speed_features.c:991)", sf.AllowSkipRecode)
	}
	if sf.LpfPick != LpfPickFromFullImage {
		t.Errorf("LpfPick = %d, want %d (libvpx vp9_speed_features.c:992)", sf.LpfPick, LpfPickFromFullImage)
	}
	if sf.UseFastCoefUpdates != TwoLoop {
		t.Errorf("UseFastCoefUpdates = %d, want %d (libvpx vp9_speed_features.c:993)", sf.UseFastCoefUpdates, TwoLoop)
	}
	if sf.UseNonrdPickMode != 0 {
		t.Errorf("UseNonrdPickMode = %d, want 0 (libvpx vp9_speed_features.c:997)", sf.UseNonrdPickMode)
	}
	for i := range common.BlockSizes {
		if sf.InterModeMask[i] != sfInterAll {
			t.Errorf("InterModeMask[%d] = %#x, want %#x (libvpx vp9_speed_features.c:998)", i, sf.InterModeMask[i], sfInterAll)
		}
	}
	if sf.MaxIntraBsize != common.Block64x64 {
		t.Errorf("MaxIntraBsize = %d, want %d (libvpx vp9_speed_features.c:999)", sf.MaxIntraBsize, common.Block64x64)
	}
	if sf.DefaultInterpFilter != vp9dec.InterpSwitchable {
		t.Errorf("DefaultInterpFilter = %d, want %d (libvpx vp9_speed_features.c:1008)", sf.DefaultInterpFilter, vp9dec.InterpSwitchable)
	}

	// libvpx: vp9_speed_features.c:1055-1058 — one-pass fixup zeros recode_loop
	// and optimize_coefficients. govpx is one-pass by default (TwoPassStats is
	// empty), so the fixup must fire.
	if sf.RecodeLoop != RecodeLoopDisallow {
		t.Errorf("RecodeLoop = %d, want %d (libvpx vp9_speed_features.c:1056)", sf.RecodeLoop, RecodeLoopDisallow)
	}
	if sf.OptimizeCoefficients != 0 {
		t.Errorf("OptimizeCoefficients = %d, want 0 (libvpx vp9_speed_features.c:1057)", sf.OptimizeCoefficients)
	}

	// libvpx: vp9_speed_features.c:881-884 — best-quality framesize-dependent
	// defaults (no `speed >= N` block in set_rt_speed_feature_framesize_dependent
	// fires at speed=0).
	if sf.PartitionSearchBreakoutThr.Dist != 1<<19 {
		t.Errorf("PartitionSearchBreakoutThr.Dist = %d, want %d (libvpx vp9_speed_features.c:881)", sf.PartitionSearchBreakoutThr.Dist, 1<<19)
	}
	if sf.PartitionSearchBreakoutThr.Rate != 80 {
		t.Errorf("PartitionSearchBreakoutThr.Rate = %d, want 80 (libvpx vp9_speed_features.c:882)", sf.PartitionSearchBreakoutThr.Rate)
	}
	if sf.RdMlPartition.SearchEarlyTermination != 0 {
		t.Errorf("RdMlPartition.SearchEarlyTermination = %d, want 0 (libvpx vp9_speed_features.c:883)", sf.RdMlPartition.SearchEarlyTermination)
	}
	if sf.RdMlPartition.SearchBreakout != 0 {
		t.Errorf("RdMlPartition.SearchBreakout = %d, want 0 (libvpx vp9_speed_features.c:884)", sf.RdMlPartition.SearchBreakout)
	}
	// libvpx: vp9_speed_features.c:1004 — encode_breakout_thresh stays 0 because
	// no `speed >= 7` block fires.
	if sf.EncodeBreakoutThresh != 0 {
		t.Errorf("EncodeBreakoutThresh = %d, want 0 (libvpx vp9_speed_features.c:1004)", sf.EncodeBreakoutThresh)
	}
}

// TestVP9SetRtSpeedFeaturesCPUUsed4Verbatim pins the libvpx SPEED_FEATURES
// produced by set_rt_speed_feature_framesize_independent + best-quality
// defaults at cpu_used == 4 (REALTIME mode, speed == 4). The `speed >= 1`,
// `speed >= 2`, `speed >= 3`, `speed >= 4` cascades all fire; speed >= 5 and
// later cascades do not.
//
// libvpx: vp9_speed_features.c:485-583 (speed >= 1..4 cascades),
//
//	vp9_speed_features.c:419-450 (framesize-dependent speed >= 1..2 cascades only).
func TestVP9SetRtSpeedFeaturesCPUUsed4Verbatim(t *testing.T) {
	// Use a small (sub-720p) frame so the framesize-dependent
	// disable_split_mask at speed >= 2 picks LAST_AND_INTRA_SPLIT_ONLY
	// (libvpx vp9_speed_features.c:433), not DISABLE_ALL_*_SPLIT.
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    w,
		Height:   h,
		Deadline: DeadlineRealtime,
		CpuUsed:  4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if got := e.vp9SpeedFeatureCPUUsed(); got != 4 {
		t.Fatalf("vp9SpeedFeatureCPUUsed = %d, want 4", got)
	}

	// Drive the configurator with an inter (non-key) frame so the is_keyframe
	// gates pick the inter branches that diverge at speed == 4.
	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false
	ctx.showFrame = true

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 4, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 4, ctx)

	// libvpx: vp9_speed_features.c:485-504 — speed >= 1 framesize-independent.
	if sf.AllowTxfmDomainDistortion != 1 {
		t.Errorf("AllowTxfmDomainDistortion = %d, want 1 (libvpx vp9_speed_features.c:486)", sf.AllowTxfmDomainDistortion)
	}
	if sf.TxDomainThresh != 0.0 {
		t.Errorf("TxDomainThresh = %v, want 0.0 (libvpx vp9_speed_features.c:487)", sf.TxDomainThresh)
	}
	if sf.TrellisOptTxRd.Method != DisableTrellisOpt {
		t.Errorf("TrellisOptTxRd.Method = %d, want %d (libvpx vp9_speed_features.c:488)", sf.TrellisOptTxRd.Method, DisableTrellisOpt)
	}
	if sf.TrellisOptTxRd.Thresh != 0.0 {
		t.Errorf("TrellisOptTxRd.Thresh = %v, want 0.0 (libvpx vp9_speed_features.c:489)", sf.TrellisOptTxRd.Thresh)
	}
	// libvpx: vp9_speed_features.c:490 — use_square_partition_only =
	// !frame_is_intra_only(cm). speed >= 3 overrides this to 1, which still
	// matches the inter-only path at speed == 4.
	if sf.UseSquarePartitionOnly != 1 {
		t.Errorf("UseSquarePartitionOnly = %d, want 1 (libvpx vp9_speed_features.c:490 -> 545)", sf.UseSquarePartitionOnly)
	}
	if sf.LessRectangularCheck != 1 {
		t.Errorf("LessRectangularCheck = %d, want 1 (libvpx vp9_speed_features.c:491)", sf.LessRectangularCheck)
	}
	if sf.UseRdBreakout != 1 {
		t.Errorf("UseRdBreakout = %d, want 1 (libvpx vp9_speed_features.c:495)", sf.UseRdBreakout)
	}
	if sf.AdaptiveMotionSearch != 1 {
		t.Errorf("AdaptiveMotionSearch = %d, want 1 (libvpx vp9_speed_features.c:497)", sf.AdaptiveMotionSearch)
	}
	if sf.Mv.AutoMvStepSize != 1 {
		t.Errorf("Mv.AutoMvStepSize = %d, want 1 (libvpx vp9_speed_features.c:499)", sf.Mv.AutoMvStepSize)
	}
	// libvpx: vp9_speed_features.c:501-503 — intra mode mask narrows from
	// INTRA_ALL to INTRA_DC_H_V on TX_32X32 (y, uv) and TX_16X16 (uv). speed
	// >= 4 then narrows them further (line 564-567).
	// libvpx: vp9_speed_features.c:503 — intra_uv_mode_mask[TX_16X16] = DC_H_V
	// at speed >= 1, then DC at speed >= 4 (line 565). We expect DC here.
	if sf.IntraUvModeMask[common.Tx16x16] != sfIntraDC {
		t.Errorf("IntraUvModeMask[Tx16x16] = %#x, want %#x (libvpx vp9_speed_features.c:565 overrides line 503)", sf.IntraUvModeMask[common.Tx16x16], sfIntraDC)
	}

	// libvpx: vp9_speed_features.c:506-542 — speed >= 2 framesize-independent.
	// Inter frame -> mode_search_skip_flags = FLAG_SKIP_INTRA_DIRMISMATCH |
	// FLAG_SKIP_INTRA_BESTINTER | FLAG_SKIP_COMP_BESTINTRA | FLAG_SKIP_INTRA_LOWVAR
	// at speed >= 2 (line 508-511), then narrowed to FLAG_SKIP_INTRA_DIRMISMATCH
	// at speed >= 4 (line 580).
	if sf.ModeSearchSkipFlags != FlagSkipIntraDirMismatch {
		t.Errorf("ModeSearchSkipFlags = %#x, want %#x (libvpx vp9_speed_features.c:580 overrides line 508-511)", sf.ModeSearchSkipFlags, FlagSkipIntraDirMismatch)
	}
	if sf.AdaptivePredInterpFilter != 2 {
		t.Errorf("AdaptivePredInterpFilter = %d, want 2 (libvpx vp9_speed_features.c:512)", sf.AdaptivePredInterpFilter)
	}
	// libvpx: vp9_speed_features.c:517 — reference_masking = 1 (single spatial
	// layer, no dynamic resize).
	if sf.ReferenceMasking != 1 {
		t.Errorf("ReferenceMasking = %d, want 1 (libvpx vp9_speed_features.c:517)", sf.ReferenceMasking)
	}
	if sf.DisableFilterSearchVarThresh != 100 {
		t.Errorf("DisableFilterSearchVarThresh = %d, want 100 (libvpx vp9_speed_features.c:546 overrides line 533)", sf.DisableFilterSearchVarThresh)
	}
	if sf.CompInterJointSearchIterLevel != 2 {
		t.Errorf("CompInterJointSearchIterLevel = %d, want 2 (libvpx vp9_speed_features.c:534)", sf.CompInterJointSearchIterLevel)
	}
	if sf.AutoMinMaxPartitionSize != AutoMinMaxRelaxedNeighboring {
		t.Errorf("AutoMinMaxPartitionSize = %d, want %d (libvpx vp9_speed_features.c:535)", sf.AutoMinMaxPartitionSize, AutoMinMaxRelaxedNeighboring)
	}
	if sf.LfMotionThreshold != LowMotionThreshold {
		t.Errorf("LfMotionThreshold = %d, want %d (libvpx vp9_speed_features.c:536)", sf.LfMotionThreshold, LowMotionThreshold)
	}
	if sf.AdjustPartitioningFromLastFrame != 1 {
		t.Errorf("AdjustPartitioningFromLastFrame = %d, want 1 (libvpx vp9_speed_features.c:537)", sf.AdjustPartitioningFromLastFrame)
	}
	if sf.LastPartitioningRedoFrequency != 3 {
		t.Errorf("LastPartitioningRedoFrequency = %d, want 3 (libvpx vp9_speed_features.c:538)", sf.LastPartitioningRedoFrequency)
	}
	if sf.UseLp32x32Fdct != 1 {
		t.Errorf("UseLp32x32Fdct = %d, want 1 (libvpx vp9_speed_features.c:539)", sf.UseLp32x32Fdct)
	}
	if sf.ModeSkipStart != 6 {
		t.Errorf("ModeSkipStart = %d, want 6 (libvpx vp9_speed_features.c:551 overrides line 540)", sf.ModeSkipStart)
	}
	// libvpx: vp9_speed_features.c:541 — intra_y_mode_mask[TX_16X16] = DC_H_V
	// at speed >= 2, then narrowed to DC_H_V at speed >= 4 (line 564) which is
	// the same value; we expect DC_H_V.
	if sf.IntraYModeMask[common.Tx16x16] != sfIntraDCHV {
		t.Errorf("IntraYModeMask[Tx16x16] = %#x, want %#x (libvpx vp9_speed_features.c:564)", sf.IntraYModeMask[common.Tx16x16], sfIntraDCHV)
	}

	// libvpx: vp9_speed_features.c:544-556 — speed >= 3 framesize-independent.
	if sf.UseUvIntraRdEstimate != 1 {
		t.Errorf("UseUvIntraRdEstimate = %d, want 1 (libvpx vp9_speed_features.c:547)", sf.UseUvIntraRdEstimate)
	}
	if sf.SkipEncodeSb != 1 {
		t.Errorf("SkipEncodeSb = %d, want 1 (libvpx vp9_speed_features.c:548)", sf.SkipEncodeSb)
	}
	if sf.Mv.SubpelSearchLevel != 0 {
		t.Errorf("Mv.SubpelSearchLevel = %d, want 0 (libvpx vp9_speed_features.c:549)", sf.Mv.SubpelSearchLevel)
	}
	// libvpx: vp9_speed_features.c:550 — adaptive_rd_thresh = 4 at speed >= 3,
	// then overridden to 2 at speed >= 4 (line 578).
	if sf.AdaptiveRdThresh != 2 {
		t.Errorf("AdaptiveRdThresh = %d, want 2 (libvpx vp9_speed_features.c:578 overrides line 550)", sf.AdaptiveRdThresh)
	}
	// libvpx: vp9_speed_features.c:552 — allow_skip_recode = 0 at speed >= 3,
	// re-asserted at speed >= 4 (line 570).
	if sf.AllowSkipRecode != 0 {
		t.Errorf("AllowSkipRecode = %d, want 0 (libvpx vp9_speed_features.c:552 / 570)", sf.AllowSkipRecode)
	}
	// libvpx: vp9_speed_features.c:553 — optimize_coefficients = 0 at speed >= 3.
	// The post-dispatch one-pass fixup also forces 0, so the result must be 0.
	if sf.OptimizeCoefficients != 0 {
		t.Errorf("OptimizeCoefficients = %d, want 0 (libvpx vp9_speed_features.c:553 / 1057)", sf.OptimizeCoefficients)
	}
	if sf.LpfPick != LpfPickFromQ {
		t.Errorf("LpfPick = %d, want %d (libvpx vp9_speed_features.c:555)", sf.LpfPick, LpfPickFromQ)
	}

	// libvpx: vp9_speed_features.c:558-583 — speed >= 4 framesize-independent.
	// At speed >= 4 sf.use_altref_onepass is gated on VBR + lag_in_frames > 0.
	// The test encoder defaults to CBR with lag = 0, so use_altref_onepass
	// must stay at the baseline 0 set at line 468.
	if sf.UseAltrefOnepass != 0 {
		t.Errorf("UseAltrefOnepass = %d, want 0 (libvpx vp9_speed_features.c:560-561 CBR / lag=0 -> baseline 468)", sf.UseAltrefOnepass)
	}
	if sf.Mv.SubpelForceStop != QuarterPel {
		t.Errorf("Mv.SubpelForceStop = %d, want %d (libvpx vp9_speed_features.c:562)", sf.Mv.SubpelForceStop, QuarterPel)
	}
	for i := range common.TxSizes {
		wantY := sfIntraDCHV
		if i == common.Tx32x32 {
			// libvpx: vp9_speed_features.c:567 — TX_32X32 narrowed to DC.
			wantY = sfIntraDC
		}
		if sf.IntraYModeMask[i] != wantY {
			t.Errorf("IntraYModeMask[%d] = %#x, want %#x (libvpx vp9_speed_features.c:564 / 567)", i, sf.IntraYModeMask[i], wantY)
		}
		if sf.IntraUvModeMask[i] != sfIntraDC {
			t.Errorf("IntraUvModeMask[%d] = %#x, want %#x (libvpx vp9_speed_features.c:565)", i, sf.IntraUvModeMask[i], sfIntraDC)
		}
	}
	if sf.FrameParameterUpdate != 0 {
		t.Errorf("FrameParameterUpdate = %d, want 0 (libvpx vp9_speed_features.c:568)", sf.FrameParameterUpdate)
	}
	if sf.Mv.SearchMethod != SearchMethodFastHex {
		t.Errorf("Mv.SearchMethod = %d, want %d (libvpx vp9_speed_features.c:569)", sf.Mv.SearchMethod, SearchMethodFastHex)
	}
	if sf.MaxIntraBsize != common.Block32x32 {
		t.Errorf("MaxIntraBsize = %d, want %d (libvpx vp9_speed_features.c:571)", sf.MaxIntraBsize, common.Block32x32)
	}
	if sf.UseFastCoefCosting != 0 {
		t.Errorf("UseFastCoefCosting = %d, want 0 (libvpx vp9_speed_features.c:572 overrides line 461)", sf.UseFastCoefCosting)
	}
	// libvpx: vp9_speed_features.c:573 — use_quant_fp = !is_keyframe (inter
	// frame here -> 1).
	if sf.UseQuantFp != 1 {
		t.Errorf("UseQuantFp = %d, want 1 (libvpx vp9_speed_features.c:573 inter)", sf.UseQuantFp)
	}
	// libvpx: vp9_speed_features.c:574-577 — INTER_NEAREST_NEW_ZERO on
	// BLOCK_32X32+.
	for _, bs := range []common.BlockSize{common.Block32x32, common.Block32x64, common.Block64x32, common.Block64x64} {
		if sf.InterModeMask[bs] != sfInterNearestNewZero {
			t.Errorf("InterModeMask[%d] = %#x, want %#x (libvpx vp9_speed_features.c:574-577)", bs, sf.InterModeMask[bs], sfInterNearestNewZero)
		}
	}
	// libvpx: vp9_speed_features.c:579 — use_fast_coef_updates = ONE_LOOP_REDUCED
	// for inter frames.
	if sf.UseFastCoefUpdates != OneLoopReduced {
		t.Errorf("UseFastCoefUpdates = %d, want %d (libvpx vp9_speed_features.c:579 inter)", sf.UseFastCoefUpdates, OneLoopReduced)
	}
	// libvpx: vp9_speed_features.c:581 — tx_size_search_method = USE_TX_8X8 for
	// inter frames.
	if sf.TxSizeSearchMethod != UseTx8x8 {
		t.Errorf("TxSizeSearchMethod = %d, want %d (libvpx vp9_speed_features.c:581 inter)", sf.TxSizeSearchMethod, UseTx8x8)
	}
	// libvpx: vp9_speed_features.c:582 — partition_search_type = VAR_BASED_PARTITION.
	if sf.PartitionSearchType != VarBasedPartition {
		t.Errorf("PartitionSearchType = %d, want %d (libvpx vp9_speed_features.c:582)", sf.PartitionSearchType, VarBasedPartition)
	}

	// libvpx: framesize-independent runs first (vp9_encoder.c:2635/3754), then
	// framesize-dependent (vp9_encoder.c:2636/3765). The framesize-dep speed>=2
	// branch on a sub-720p frame (vp9_speed_features.c:432-434) writes
	// LAST_AND_INTRA_SPLIT_ONLY AFTER the framesize-indep speed>=3 branch
	// (vp9_speed_features.c:554) wrote DISABLE_ALL_SPLIT, so the final value at
	// sub-720p speed=4 is LAST_AND_INTRA_SPLIT_ONLY.
	if sf.DisableSplitMask != sfLastAndIntraSplitOnly {
		t.Errorf("DisableSplitMask = %#x, want %#x (libvpx vp9_speed_features.c:433 framesize-dep speed>=2 sub-720p overrides framesize-indep speed>=3)", sf.DisableSplitMask, sfLastAndIntraSplitOnly)
	}

	// libvpx: vp9_speed_features.c:893-895 — DISABLE_ALL_SPLIT -> clear
	// adaptive_pred_interp_filter. Since the framesize-dep override at sub-720p
	// lands on LAST_AND_INTRA_SPLIT_ONLY (not DISABLE_ALL_SPLIT), the
	// adaptive_pred_interp_filter clear gate does NOT fire and the speed>=2
	// value (line 512) of 2 survives.
	if sf.AdaptivePredInterpFilter != 2 {
		t.Errorf("AdaptivePredInterpFilter = %d, want 2 (libvpx vp9_speed_features.c:512, no DISABLE_ALL_SPLIT clear at sub-720p)", sf.AdaptivePredInterpFilter)
	}
	// libvpx: vp9_speed_features.c:446-449 — encode_breakout_thresh only set at
	// speed >= 7 framesize-dependent. Stays at 0 here.
	if sf.EncodeBreakoutThresh != 0 {
		t.Errorf("EncodeBreakoutThresh = %d, want 0 (libvpx vp9_speed_features.c:446-449, speed<7)", sf.EncodeBreakoutThresh)
	}

	// libvpx: vp9_speed_features.c:1055-1058 — one-pass fixup clears
	// recode_loop + optimize_coefficients.
	if sf.RecodeLoop != RecodeLoopDisallow {
		t.Errorf("RecodeLoop = %d, want %d (libvpx vp9_speed_features.c:1056 one-pass)", sf.RecodeLoop, RecodeLoopDisallow)
	}
}

// TestVP9SetRtSpeedFeaturesCPUUsed4Verbatim720p covers the 720p+ branch of
// set_rt_speed_feature_framesize_dependent at cpu_used == 4, where speed >= 2
// (libvpx vp9_speed_features.c:429-432) picks DISABLE_ALL_SPLIT on a visible
// frame (show_frame=true). This branch is structurally separate from the
// sub-720p branch the prior test covers.
//
// libvpx: vp9_speed_features.c:429-432 (framesize-dep speed >= 2, min_dim >= 720, show_frame).
func TestVP9SetRtSpeedFeaturesCPUUsed4Verbatim720p(t *testing.T) {
	const w, h = 1280, 720
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    w,
		Height:   h,
		Deadline: DeadlineRealtime,
		CpuUsed:  4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false
	ctx.showFrame = true

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 4, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 4, ctx)

	// libvpx: vp9_speed_features.c:431 — show_frame + min_dim >= 720 ->
	// disable_split_mask = DISABLE_ALL_SPLIT. The framesize-indep speed>=3
	// branch (line 554) sets the same value, so framesize-dep speed>=2
	// preserves it.
	if sf.DisableSplitMask != sfDisableAllSplit {
		t.Errorf("DisableSplitMask = %#x, want %#x (libvpx vp9_speed_features.c:431 show_frame + 720p+)", sf.DisableSplitMask, sfDisableAllSplit)
	}

	// libvpx: vp9_speed_features.c:893-895 — DISABLE_ALL_SPLIT clears
	// adaptive_pred_interp_filter. Both speed >= 2 (line 512) sets
	// adaptive_pred_interp_filter = 2 first; the final clear must zero it.
	if sf.AdaptivePredInterpFilter != 0 {
		t.Errorf("AdaptivePredInterpFilter = %d, want 0 (libvpx vp9_speed_features.c:893-895 720p+ DISABLE_ALL_SPLIT clear)", sf.AdaptivePredInterpFilter)
	}

	// On a non-show frame (e.g. ARF) the show_frame branch picks
	// DISABLE_ALL_INTER_SPLIT instead.
	ctxHidden := ctx
	ctxHidden.showFrame = false
	var sfH SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sfH, 4, ctxHidden)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sfH, 4, ctxHidden)
	if sfH.DisableSplitMask != sfDisableAllInterSplit {
		t.Errorf("hidden DisableSplitMask = %#x, want %#x (libvpx vp9_speed_features.c:431 !show_frame + 720p+)", sfH.DisableSplitMask, sfDisableAllInterSplit)
	}
	// On a non-show frame the disable_split_mask is DISABLE_ALL_INTER_SPLIT
	// (not DISABLE_ALL_SPLIT), so the line 893-895 clear does NOT fire and the
	// speed>=2 value of 2 (line 512) survives.
	if sfH.AdaptivePredInterpFilter != 2 {
		t.Errorf("hidden AdaptivePredInterpFilter = %d, want 2 (libvpx vp9_speed_features.c:512, no DISABLE_ALL_SPLIT clear)", sfH.AdaptivePredInterpFilter)
	}
}
