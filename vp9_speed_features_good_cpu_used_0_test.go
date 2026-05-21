package govpx

import (
	"math"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9SetGoodSpeedFeaturesCPUUsed0Verbatim pins the libvpx SPEED_FEATURES
// produced by set_good_speed_feature_framesize_independent +
// set_good_speed_feature_framesize_dependent + best-quality defaults at
// cpu_used == 0 (GOOD mode, speed == 0). Mirrors the RT sibling at
// vp9_speed_features_rt_cpu_used_0_4_test.go:TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim.
//
// At speed == 0 NONE of the `if (speed >= N)` cascades in
// set_good_speed_feature_framesize_independent (libvpx
// vp9_speed_features.c:272, 319, 358, 385, 400, 412, 451) or in
// set_good_speed_feature_framesize_dependent (libvpx
// vp9_speed_features.c:113, 144, 176, 201, 211) fire, so the resulting
// SPEED_FEATURES is the union of:
//
//   - best-quality defaults from vp9_set_speed_features_framesize_independent
//     (libvpx vp9_speed_features.c:928-1029)
//   - GOOD baseline overrides from set_good_speed_feature_framesize_independent
//     (libvpx vp9_speed_features.c:227-270)
//   - best-quality defaults from vp9_set_speed_features_framesize_dependent
//     (libvpx vp9_speed_features.c:881-884)
//   - GOOD framesize-dependent at speed=0 (libvpx vp9_speed_features.c:76-111)
//   - post-dispatch fixups (libvpx vp9_speed_features.c:1052, 1055-1058,
//     1084-1085, 1093-1095)
//
// The FuzzVP9OracleEncoderRuntimeControls cpu_used=0 seed cites the
// "GOOD speed-features path govpx has not yet ported"
// (vp9_speed_features.c:140-280) as the unblocker. This test enumerates every
// SPEED_FEATURES field the speed=0 GOOD cascade sets and proves the govpx port
// already matches libvpx verbatim — so the real divergence behind that fuzz
// seed lives downstream (in the encoder body, NOT the speed-features
// configurator).
//
// libvpx: vp9_speed_features.c:64-214 (framesize-dependent),
//
//	vp9_speed_features.c:219-411 (framesize-independent),
//	vp9_speed_features.c:873-1096 (outer wraps).
func TestVP9SetGoodSpeedFeaturesCPUUsed0Verbatim(t *testing.T) {
	// Use a sub-480p frame so is_480p_or_larger / is_720p_or_larger /
	// is_1080p_or_larger / is_2160p_or_larger all evaluate false and the
	// framesize-dependent speed=0 block picks the most basic branch.
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    w,
		Height:   h,
		Deadline: DeadlineGoodQuality,
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

	// libvpx: vp9_speed_features.c:227-256 — GOOD baseline overrides.
	// The default speed frame context is KEY_FRAME / intra_only=true, so
	// frame_is_kf_gf_arf() returns true -> boosted = true.
	if sf.AdaptiveInterpFilterSearch != 1 {
		t.Errorf("AdaptiveInterpFilterSearch = %d, want 1 (libvpx vp9_speed_features.c:227)",
			sf.AdaptiveInterpFilterSearch)
	}
	if sf.AdaptivePredInterpFilter != 1 {
		t.Errorf("AdaptivePredInterpFilter = %d, want 1 (libvpx vp9_speed_features.c:228)",
			sf.AdaptivePredInterpFilter)
	}
	if sf.AdaptiveRdThresh != 1 {
		t.Errorf("AdaptiveRdThresh = %d, want 1 (libvpx vp9_speed_features.c:229)",
			sf.AdaptiveRdThresh)
	}
	if sf.AdaptiveRdThreshRowMt != 0 {
		t.Errorf("AdaptiveRdThreshRowMt = %d, want 0 (libvpx vp9_speed_features.c:230)",
			sf.AdaptiveRdThreshRowMt)
	}
	if sf.AllowSkipRecode != 1 {
		t.Errorf("AllowSkipRecode = %d, want 1 (libvpx vp9_speed_features.c:231)",
			sf.AllowSkipRecode)
	}
	if sf.LessRectangularCheck != 1 {
		t.Errorf("LessRectangularCheck = %d, want 1 (libvpx vp9_speed_features.c:232)",
			sf.LessRectangularCheck)
	}
	if sf.Mv.AutoMvStepSize != 1 {
		t.Errorf("Mv.AutoMvStepSize = %d, want 1 (libvpx vp9_speed_features.c:233)",
			sf.Mv.AutoMvStepSize)
	}
	if sf.Mv.UseDownsampledSad != 1 {
		t.Errorf("Mv.UseDownsampledSad = %d, want 1 (libvpx vp9_speed_features.c:234)",
			sf.Mv.UseDownsampledSad)
	}
	if sf.PruneRefFrameForRectPartitions != 1 {
		t.Errorf("PruneRefFrameForRectPartitions = %d, want 1 (libvpx vp9_speed_features.c:235)",
			sf.PruneRefFrameForRectPartitions)
	}
	if sf.TemporalFilterSearchMethod != SearchMethodNStep {
		t.Errorf("TemporalFilterSearchMethod = %d, want %d (libvpx vp9_speed_features.c:236)",
			sf.TemporalFilterSearchMethod, SearchMethodNStep)
	}
	if sf.TxSizeSearchBreakout != 1 {
		t.Errorf("TxSizeSearchBreakout = %d, want 1 (libvpx vp9_speed_features.c:237)",
			sf.TxSizeSearchBreakout)
	}
	// libvpx: vp9_speed_features.c:238 — use_square_partition_only = !boosted.
	// Default ctx is a keyframe -> boosted=true -> 0.
	if sf.UseSquarePartitionOnly != 0 {
		t.Errorf("UseSquarePartitionOnly = %d, want 0 (libvpx vp9_speed_features.c:238 boosted KF)",
			sf.UseSquarePartitionOnly)
	}
	if sf.EarlyTermInterpSearchPlaneRd != 1 {
		t.Errorf("EarlyTermInterpSearchPlaneRd = %d, want 1 (libvpx vp9_speed_features.c:239)",
			sf.EarlyTermInterpSearchPlaneRd)
	}
	if sf.CbPredFilterSearch != 1 {
		t.Errorf("CbPredFilterSearch = %d, want 1 (libvpx vp9_speed_features.c:240)",
			sf.CbPredFilterSearch)
	}
	// libvpx: vp9_speed_features.c:241-243 — TrellisOptTxRd.Method depends
	// on optimize_coefficients. The post-dispatch one-pass fixup at line
	// 1057 zeroes optimize_coefficients AFTER the GOOD dispatcher runs, so
	// the assignment we see here reflects the GOOD-dispatcher snapshot.
	if sf.TrellisOptTxRd.Method != EnableTrellisOptTxRdResidualMse {
		t.Errorf("TrellisOptTxRd.Method = %d, want %d (libvpx vp9_speed_features.c:241-243 with optimize_coefficients=1 pre-fixup)",
			sf.TrellisOptTxRd.Method, EnableTrellisOptTxRdResidualMse)
	}
	// libvpx: vp9_speed_features.c:244 — boosted ? 4.0 : 3.0.
	if sf.TrellisOptTxRd.Thresh != 4.0 {
		t.Errorf("TrellisOptTxRd.Thresh = %v, want 4.0 (libvpx vp9_speed_features.c:244 boosted KF)",
			sf.TrellisOptTxRd.Thresh)
	}
	if sf.IntraYModeMask[common.Tx32x32] != sfIntraDCHV {
		t.Errorf("IntraYModeMask[TX_32X32] = %#x, want %#x (libvpx vp9_speed_features.c:246)",
			sf.IntraYModeMask[common.Tx32x32], sfIntraDCHV)
	}
	if sf.CompInterJointSearchIterLevel != 1 {
		t.Errorf("CompInterJointSearchIterLevel = %d, want 1 (libvpx vp9_speed_features.c:247)",
			sf.CompInterJointSearchIterLevel)
	}
	// libvpx: vp9_speed_features.c:250 — reference_masking = (resize_mode != DYNAMIC).
	// govpx does not expose dynamic resize, so reference_masking = 1.
	if sf.ReferenceMasking != 1 {
		t.Errorf("ReferenceMasking = %d, want 1 (libvpx vp9_speed_features.c:250)",
			sf.ReferenceMasking)
	}
	if sf.RdMlPartition.VarPruning != 1 {
		t.Errorf("RdMlPartition.VarPruning = %d, want 1 (libvpx vp9_speed_features.c:252)",
			sf.RdMlPartition.VarPruning)
	}
	wantRectThresh := [4]int{-1, 350, 325, 250}
	for i, want := range wantRectThresh {
		if sf.RdMlPartition.PruneRectThresh[i] != want {
			t.Errorf("RdMlPartition.PruneRectThresh[%d] = %d, want %d (libvpx vp9_speed_features.c:253-256)",
				i, sf.RdMlPartition.PruneRectThresh[i], want)
		}
	}
	// libvpx: vp9_speed_features.c:258-262 — non-graphics content has
	// ExhaustiveSearchesThresh = INT_MAX.
	if sf.ExhaustiveSearchesThresh != math.MaxInt32 {
		t.Errorf("ExhaustiveSearchesThresh = %d, want %d (libvpx vp9_speed_features.c:261)",
			sf.ExhaustiveSearchesThresh, math.MaxInt32)
	}
	// libvpx: vp9_speed_features.c:264-270 — mesh_density_level = 0 in GOOD baseline.
	wantMesh := vp9GoodQualityMeshPatterns[0]
	for i := range sfMaxMeshSteps {
		if sf.MeshPatterns[i] != wantMesh[i] {
			t.Errorf("MeshPatterns[%d] = %+v, want %+v (libvpx vp9_speed_features.c:264-270)",
				i, sf.MeshPatterns[i], wantMesh[i])
		}
	}

	// libvpx: vp9_speed_features.c:76-79 — framesize-dependent speed=0
	// baseline (no `speed >= N` block fires).
	if sf.PartitionSearchBreakoutThr.Dist != 1<<20 {
		t.Errorf("PartitionSearchBreakoutThr.Dist = %d, want %d (libvpx vp9_speed_features.c:76)",
			sf.PartitionSearchBreakoutThr.Dist, 1<<20)
	}
	if sf.PartitionSearchBreakoutThr.Rate != 80 {
		t.Errorf("PartitionSearchBreakoutThr.Rate = %d, want 80 (libvpx vp9_speed_features.c:77)",
			sf.PartitionSearchBreakoutThr.Rate)
	}
	// libvpx: vp9_speed_features.c:81-88 — !is_480p_or_larger keeps
	// UseSquareOnlyThreshHigh at BLOCK_32X32 (line 87) and skips
	// RdMlPartition.SearchEarlyTermination = 1 / RecodeToleranceHigh = 45.
	if sf.UseSquareOnlyThreshHigh != common.Block32x32 {
		t.Errorf("UseSquareOnlyThreshHigh = %d, want %d (libvpx vp9_speed_features.c:87 sub-480p)",
			sf.UseSquareOnlyThreshHigh, common.Block32x32)
	}
	if sf.UseSquareOnlyThreshLow != common.Block4x4 {
		t.Errorf("UseSquareOnlyThreshLow = %d, want %d (libvpx vp9_speed_features.c:79)",
			sf.UseSquareOnlyThreshLow, common.Block4x4)
	}
	if sf.RdMlPartition.SearchEarlyTermination != 0 {
		t.Errorf("RdMlPartition.SearchEarlyTermination = %d, want 0 (libvpx vp9_speed_features.c:83 !is_480p)",
			sf.RdMlPartition.SearchEarlyTermination)
	}
	// libvpx: vp9_speed_features.c:93-103 — !is_1080p_or_larger and
	// !is_720p_or_larger picks the (2.5, 1.5, 1.5) thresh tuple.
	if sf.RdMlPartition.SearchBreakout != 1 {
		t.Errorf("RdMlPartition.SearchBreakout = %d, want 1 (libvpx vp9_speed_features.c:94)",
			sf.RdMlPartition.SearchBreakout)
	}
	wantBreakoutThresh := [3]float32{2.5, 1.5, 1.5}
	for i, want := range wantBreakoutThresh {
		if sf.RdMlPartition.SearchBreakoutThresh[i] != want {
			t.Errorf("RdMlPartition.SearchBreakoutThresh[%d] = %v, want %v (libvpx vp9_speed_features.c:100-102 sub-720p)",
				i, sf.RdMlPartition.SearchBreakoutThresh[i], want)
		}
	}
	// libvpx: vp9_speed_features.c:106-111 — !is_720p_or_larger and
	// is_480p_or_larger picks boosted ? 0 : 1. We are sub-480p, so the
	// else branch fires (line 110) -> 1.
	if sf.PruneSingleModeBasedOnMvDiffModeRate != 1 {
		t.Errorf("PruneSingleModeBasedOnMvDiffModeRate = %d, want 1 (libvpx vp9_speed_features.c:110 sub-480p)",
			sf.PruneSingleModeBasedOnMvDiffModeRate)
	}
	// libvpx: vp9_speed_features.c:89-91 — !is_720p_or_larger skips
	// AltRefSearchFp = 1; defaults to 0.
	if sf.AltRefSearchFp != 0 {
		t.Errorf("AltRefSearchFp = %d, want 0 (libvpx vp9_speed_features.c:90 !is_720p)",
			sf.AltRefSearchFp)
	}

	// libvpx: vp9_speed_features.c:881-884 — best-quality framesize-dependent
	// defaults applied BEFORE the GOOD-mode dispatch. The GOOD-mode dispatcher
	// then overwrites PartitionSearchBreakoutThr (verified above).
	if sf.PartitionSearchBreakoutThr.Dist != 1<<20 {
		t.Errorf("PartitionSearchBreakoutThr.Dist (post GOOD) = %d, want %d (libvpx vp9_speed_features.c:76)",
			sf.PartitionSearchBreakoutThr.Dist, 1<<20)
	}

	// libvpx: vp9_speed_features.c:893-895 — disable_split_mask ==
	// DISABLE_ALL_SPLIT zeroes AdaptivePredInterpFilter. At speed=0 GOOD
	// disable_split_mask stays at the default 0, so AdaptivePredInterpFilter
	// remains 1.
	if sf.DisableSplitMask != 0 {
		t.Errorf("DisableSplitMask = %d, want 0 (libvpx vp9_speed_features.c:967 default at speed=0 GOOD)",
			sf.DisableSplitMask)
	}
	// AdaptivePredInterpFilter pinned to 1 above survives the post-dispatch fixup.
	if sf.AdaptivePredInterpFilter != 1 {
		t.Errorf("AdaptivePredInterpFilter (post split-mask fixup) = %d, want 1 (libvpx vp9_speed_features.c:228 + 893-895)",
			sf.AdaptivePredInterpFilter)
	}

	// libvpx: vp9_speed_features.c:1052 + 1055-1058 — one-pass fixup zeros
	// RecodeLoop and OptimizeCoefficients after the GOOD dispatch. govpx is
	// one-pass by default (TwoPassStats is empty), so the fixup must fire.
	if sf.RecodeLoop != RecodeLoopDisallow {
		t.Errorf("RecodeLoop = %d, want %d (libvpx vp9_speed_features.c:1056)",
			sf.RecodeLoop, RecodeLoopDisallow)
	}
	if sf.OptimizeCoefficients != 0 {
		t.Errorf("OptimizeCoefficients = %d, want 0 (libvpx vp9_speed_features.c:1057)",
			sf.OptimizeCoefficients)
	}

	// libvpx: vp9_speed_features.c:928-1029 — best-quality defaults that
	// survive the GOOD baseline because GOOD never overwrites them at speed=0.
	if sf.Mv.SearchMethod != SearchMethodNStep {
		t.Errorf("Mv.SearchMethod = %d, want %d (libvpx vp9_speed_features.c:930)",
			sf.Mv.SearchMethod, SearchMethodNStep)
	}
	if sf.Mv.SubpelSearchMethod != SubpelTree {
		t.Errorf("Mv.SubpelSearchMethod = %d, want %d (libvpx vp9_speed_features.c:932)",
			sf.Mv.SubpelSearchMethod, SubpelTree)
	}
	if sf.Mv.SubpelSearchLevel != 2 {
		t.Errorf("Mv.SubpelSearchLevel = %d, want 2 (libvpx vp9_speed_features.c:933)",
			sf.Mv.SubpelSearchLevel)
	}
	if sf.Mv.SubpelForceStop != EighthPel {
		t.Errorf("Mv.SubpelForceStop = %d, want %d (libvpx vp9_speed_features.c:934)",
			sf.Mv.SubpelForceStop, EighthPel)
	}
	if sf.Mv.FullpelSearchStepParam != 6 {
		t.Errorf("Mv.FullpelSearchStepParam = %d, want 6 (libvpx vp9_speed_features.c:939)",
			sf.Mv.FullpelSearchStepParam)
	}
	if sf.TxSizeSearchMethod != UseFullRD {
		t.Errorf("TxSizeSearchMethod = %d, want %d (libvpx vp9_speed_features.c:942)",
			sf.TxSizeSearchMethod, UseFullRD)
	}
	if sf.EnhancedFullPixelMotionSearch != 1 {
		t.Errorf("EnhancedFullPixelMotionSearch = %d, want 1 (libvpx vp9_speed_features.c:945)",
			sf.EnhancedFullPixelMotionSearch)
	}
	if sf.PartitionSearchType != SearchPartition {
		t.Errorf("PartitionSearchType = %d, want %d (libvpx vp9_speed_features.c:956)",
			sf.PartitionSearchType, SearchPartition)
	}
	if sf.AutoMinMaxPartitionSize != AutoMinMaxNotInUse {
		t.Errorf("AutoMinMaxPartitionSize = %d, want %d (libvpx vp9_speed_features.c:961)",
			sf.AutoMinMaxPartitionSize, AutoMinMaxNotInUse)
	}
	if sf.DefaultMaxPartitionSize != common.Block64x64 {
		t.Errorf("DefaultMaxPartitionSize = %d, want %d (libvpx vp9_speed_features.c:963)",
			sf.DefaultMaxPartitionSize, common.Block64x64)
	}
	if sf.DefaultMinPartitionSize != common.Block4x4 {
		t.Errorf("DefaultMinPartitionSize = %d, want %d (libvpx vp9_speed_features.c:964)",
			sf.DefaultMinPartitionSize, common.Block4x4)
	}
	if sf.LpfPick != LpfPickFromFullImage {
		t.Errorf("LpfPick = %d, want %d (libvpx vp9_speed_features.c:992)",
			sf.LpfPick, LpfPickFromFullImage)
	}
	if sf.MaxIntraBsize != common.Block64x64 {
		t.Errorf("MaxIntraBsize = %d, want %d (libvpx vp9_speed_features.c:999)",
			sf.MaxIntraBsize, common.Block64x64)
	}
	if sf.DefaultInterpFilter != vp9dec.InterpSwitchable {
		t.Errorf("DefaultInterpFilter = %d, want %d (libvpx vp9_speed_features.c:1008)",
			sf.DefaultInterpFilter, vp9dec.InterpSwitchable)
	}
	if sf.FrameParameterUpdate != 1 {
		t.Errorf("FrameParameterUpdate = %d, want 1 (libvpx vp9_speed_features.c:929)",
			sf.FrameParameterUpdate)
	}
	if sf.UseNonrdPickMode != 0 {
		t.Errorf("UseNonrdPickMode = %d, want 0 (libvpx vp9_speed_features.c:997)",
			sf.UseNonrdPickMode)
	}
	if sf.UseFastCoefUpdates != TwoLoop {
		t.Errorf("UseFastCoefUpdates = %d, want %d (libvpx vp9_speed_features.c:993)",
			sf.UseFastCoefUpdates, TwoLoop)
	}
	if sf.AllowAcl != 1 {
		t.Errorf("AllowAcl = %d, want 1 (libvpx vp9_speed_features.c:978)",
			sf.AllowAcl)
	}
	if sf.RecodeToleranceLow != 12 {
		t.Errorf("RecodeToleranceLow = %d, want 12 (libvpx vp9_speed_features.c:1006)",
			sf.RecodeToleranceLow)
	}
	// libvpx: vp9_speed_features.c:1007 — best-quality default RecodeToleranceHigh = 25.
	// The is_480p_or_larger branch at vp9_speed_features.c:85 would lift this to 45,
	// but we are sub-480p, so the default stays.
	if sf.RecodeToleranceHigh != 25 {
		t.Errorf("RecodeToleranceHigh = %d, want 25 (libvpx vp9_speed_features.c:1007 sub-480p)",
			sf.RecodeToleranceHigh)
	}
	if sf.TxSizeSearchDepth != 2 {
		t.Errorf("TxSizeSearchDepth = %d, want 2 (libvpx vp9_speed_features.c:1025)",
			sf.TxSizeSearchDepth)
	}
	if sf.UseAccurateSubpelSearch != Use8Taps {
		t.Errorf("UseAccurateSubpelSearch = %d, want %d (libvpx vp9_speed_features.c:1020)",
			sf.UseAccurateSubpelSearch, Use8Taps)
	}
}
