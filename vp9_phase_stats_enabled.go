//go:build govpx_phase_stats

package govpx

import (
	"sync/atomic"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

const vp9PhaseStatsEnabled = true

type vp9FullPelSADSource uint8

const (
	vp9FullPelSADSourceOther vp9FullPelSADSource = iota
	vp9FullPelSADSourceZero
	vp9FullPelSADSourceSeed
	vp9FullPelSADSourceHint
	vp9FullPelSADSourcePattern
	vp9FullPelSADSourceFullRD
)

type vp9DecoderPhaseStatsOptions struct {
	phaseStats *EncoderPhaseStats
}

type vp9TileWorkerPhaseStatsOptions struct {
	phaseStats *EncoderPhaseStats
}

type vp9EncoderPhase uint8

const (
	vp9EncoderPhaseCount vp9EncoderPhase = iota
	vp9EncoderPhaseHeaderWrite
	vp9EncoderPhaseTileWrite
	vp9EncoderPhaseLoopFilterPick
	vp9EncoderPhaseLoopFilterApply
)

func (e *VP9Encoder) vp9PhaseStats() *EncoderPhaseStats {
	if e == nil {
		return nil
	}
	return e.opts.PhaseStats
}

func (e *VP9Encoder) vp9PhaseStart() int64 {
	if e.vp9PhaseStats() == nil {
		return 0
	}
	return nanotime()
}

func (e *VP9Encoder) vp9PhaseEnd(phase vp9EncoderPhase, start int64) {
	if start == 0 {
		return
	}
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	elapsed := nanotime() - start
	switch phase {
	case vp9EncoderPhaseCount:
		atomic.AddInt64(&stats.VP9CountNS, elapsed)
	case vp9EncoderPhaseHeaderWrite:
		atomic.AddInt64(&stats.VP9HeaderWriteNS, elapsed)
	case vp9EncoderPhaseTileWrite:
		atomic.AddInt64(&stats.VP9TileWriteNS, elapsed)
	case vp9EncoderPhaseLoopFilterPick:
		atomic.AddInt64(&stats.VP9LoopFilterPickNS, elapsed)
	case vp9EncoderPhaseLoopFilterApply:
		atomic.AddInt64(&stats.VP9LoopFilterApplyNS, elapsed)
	}
}

func (e *VP9Encoder) vp9PhaseStatsActive() bool {
	return e != nil && e.opts.PhaseStats != nil
}

func (e *VP9Encoder) vp9PhaseCountAttempt(keyFrame bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	if keyFrame {
		atomic.AddInt64(&stats.KeyAttempts, 1)
	} else {
		atomic.AddInt64(&stats.InterAttempts, 1)
	}
}

func (e *VP9Encoder) vp9PhaseCountPreEncodeDrop(reason vp9DropReason) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.VP9PreEncodeDrops, 1)
	switch reason {
	case vp9DropNegativeBuffer:
		atomic.AddInt64(&stats.VP9PreEncodeDropNegativeBuffer, 1)
	case vp9DropWatermarkDecimation:
		atomic.AddInt64(&stats.VP9PreEncodeDropWatermarkDecimation, 1)
	}
}

func (e *VP9Encoder) vp9PhaseCountPostEncodeDrop(encodedBits int) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.VP9PostEncodeDrops, 1)
	atomic.AddInt64(&stats.VP9PostEncodeDropBits, int64(encodedBits))
}

func (e *VP9Encoder) vp9PhaseIncFrameTile(countPass bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.VP9FrameTiles, 1)
	if countPass {
		atomic.AddInt64(&stats.VP9FrameTilesCountPass, 1)
	} else {
		atomic.AddInt64(&stats.VP9FrameTilesWritePass, 1)
	}
}

func (e *VP9Encoder) vp9PhaseIncModeSB(countPass bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.VP9ModeSBs, 1)
	if countPass {
		atomic.AddInt64(&stats.VP9ModeSBsCountPass, 1)
	} else {
		atomic.AddInt64(&stats.VP9ModeSBsWritePass, 1)
	}
}

func (e *VP9Encoder) vp9PhaseIncModeBlock(bsize common.BlockSize, countPass bool) {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9ModeBlocks, 1)
		if countPass {
			atomic.AddInt64(&stats.VP9ModeBlocksCountPass, 1)
		} else {
			atomic.AddInt64(&stats.VP9ModeBlocksWritePass, 1)
		}
		switch bsize {
		case common.Block64x64:
			atomic.AddInt64(&stats.VP9ModeBlock64x64, 1)
		case common.Block32x32:
			atomic.AddInt64(&stats.VP9ModeBlock32x32, 1)
		case common.Block32x16:
			atomic.AddInt64(&stats.VP9ModeBlock32x16, 1)
		case common.Block16x32:
			atomic.AddInt64(&stats.VP9ModeBlock16x32, 1)
		case common.Block16x16:
			atomic.AddInt64(&stats.VP9ModeBlock16x16, 1)
		case common.Block16x8:
			atomic.AddInt64(&stats.VP9ModeBlock16x8, 1)
		case common.Block8x16:
			atomic.AddInt64(&stats.VP9ModeBlock8x16, 1)
		case common.Block8x8:
			atomic.AddInt64(&stats.VP9ModeBlock8x8, 1)
		default:
			if bsize < common.Block8x8 {
				atomic.AddInt64(&stats.VP9ModeBlockSub8, 1)
			}
		}
	}
}

func (e *VP9Encoder) vp9PhaseIncInterModePick(countPass bool) {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9InterModePicks, 1)
		if countPass {
			atomic.AddInt64(&stats.VP9InterModePicksCountPass, 1)
		} else {
			atomic.AddInt64(&stats.VP9InterModePicksWritePass, 1)
		}
	}
}

func (e *VP9Encoder) vp9PhaseIncInterLeafCacheStore() {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9InterLeafCacheStores, 1)
	}
}

func (e *VP9Encoder) vp9PhaseCountInterLeafReplay(hit bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	if hit {
		atomic.AddInt64(&stats.VP9InterLeafCacheReplayHits, 1)
	} else {
		atomic.AddInt64(&stats.VP9InterLeafCacheReplayMisses, 1)
	}
}

func (e *VP9Encoder) vp9PhaseCountVarPartChoose(copied, countPass bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.VP9VarPartChooseCalls, 1)
	if countPass {
		atomic.AddInt64(&stats.VP9VarPartChooseCountPass, 1)
	} else {
		atomic.AddInt64(&stats.VP9VarPartChooseWritePass, 1)
	}
	if copied {
		atomic.AddInt64(&stats.VP9VarPartCopyHits, 1)
	}
}

func (e *VP9Encoder) vp9PhaseCountVarPartCacheHit(hit bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	if hit {
		atomic.AddInt64(&stats.VP9VarPartCacheHits, 1)
	} else {
		atomic.AddInt64(&stats.VP9VarPartCacheMisses, 1)
	}
}

func (e *VP9Encoder) vp9PhaseAddVarPartMergedSBs(n int64) {
	stats := e.vp9PhaseStats()
	if stats != nil && n != 0 {
		atomic.AddInt64(&stats.VP9VarPartMergedSBs, n)
	}
}

func (e *VP9Encoder) vp9PhaseCountVarPartContentState(state encoder.ContentStateSB) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	switch state {
	case encoder.ContentStateInvalid:
		atomic.AddInt64(&stats.VP9VarPartContentStateInvalid, 1)
	case encoder.ContentStateLowSadLowSumdiff:
		atomic.AddInt64(&stats.VP9VarPartContentStateLowSadLowSumdiff, 1)
	case encoder.ContentStateLowSadHighSumdiff:
		atomic.AddInt64(&stats.VP9VarPartContentStateLowSadHighSumdiff, 1)
	case encoder.ContentStateHighSadLowSumdiff:
		atomic.AddInt64(&stats.VP9VarPartContentStateHighSadLowSumdiff, 1)
	case encoder.ContentStateHighSadHighSumdiff:
		atomic.AddInt64(&stats.VP9VarPartContentStateHighSadHighSumdiff, 1)
	case encoder.ContentStateLowVarHighSumdiff:
		atomic.AddInt64(&stats.VP9VarPartContentStateLowVarHighSumdiff, 1)
	case encoder.ContentStateVeryHighSad:
		atomic.AddInt64(&stats.VP9VarPartContentStateVeryHighSad, 1)
	}
}

func (e *VP9Encoder) vp9PhaseAddChoosePartitioningStats(s encoder.ChoosePartitioningStats) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.VP9VarPartYSADValid, int64(s.YSADValid))
	atomic.AddInt64(&stats.VP9VarPartYSADSelect64x64, int64(s.YSADSelect64x64))
	atomic.AddInt64(&stats.VP9VarPartCopyPartitionSelect, int64(s.CopyPartitionSelect))
	atomic.AddInt64(&stats.VP9VarPartForceSplit64, int64(s.ForceSplit64))
	atomic.AddInt64(&stats.VP9VarPartForceSplit32, int64(s.ForceSplit32))
	atomic.AddInt64(&stats.VP9VarPartForceSplit16, int64(s.ForceSplit16))
	atomic.AddInt64(&stats.VP9VarPartForceSplit16Variance, int64(s.ForceSplit16Variance))
	atomic.AddInt64(&stats.VP9VarPartForceSplit16Minmax, int64(s.ForceSplit16Minmax))
	atomic.AddInt64(&stats.VP9VarPartThreshold2Count, int64(s.Threshold2Count))
	atomic.AddInt64(&stats.VP9VarPartThreshold2Sum, int64(s.Threshold2Sum))
	atomic.AddInt64(&stats.VP9VarPartVar16Samples, int64(s.Var16Samples))
	atomic.AddInt64(&stats.VP9VarPartVar16Sum, int64(s.Var16Sum))
	atomic.AddInt64(&stats.VP9VarPartForce16VarianceSum, int64(s.Force16VarianceSum))
	atomic.AddInt64(&stats.VP9VarPartForce16ThresholdSum, int64(s.Force16ThresholdSum))
	atomic.AddInt64(&stats.VP9VarPartSetVTCalls, int64(s.SetVTCalls))
	atomic.AddInt64(&stats.VP9VarPartSetVT64x64, int64(s.SetVT64x64))
	atomic.AddInt64(&stats.VP9VarPartSetVT32x32, int64(s.SetVT32x32))
	atomic.AddInt64(&stats.VP9VarPartSetVT16x16, int64(s.SetVT16x16))
	atomic.AddInt64(&stats.VP9VarPartSetVT8x8, int64(s.SetVT8x8))
	atomic.AddInt64(&stats.VP9VarPartSetVTForceSplit, int64(s.SetVTForceSplit))
	atomic.AddInt64(&stats.VP9VarPartSetVTForceSplit64x64, int64(s.SetVTForceSplit64x64))
	atomic.AddInt64(&stats.VP9VarPartSetVTForceSplit32x32, int64(s.SetVTForceSplit32x32))
	atomic.AddInt64(&stats.VP9VarPartSetVTForceSplit16x16, int64(s.SetVTForceSplit16x16))
	atomic.AddInt64(&stats.VP9VarPartSetVTSelect, int64(s.SetVTSelect))
	atomic.AddInt64(&stats.VP9VarPartSetVTSplit, int64(s.SetVTSplit))
}

func (e *VP9Encoder) vp9PhaseIncInterPredictionBlock() {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9InterPredictionBlocks, 1)
	}
}

func (e *VP9Encoder) vp9PhaseIncInterPredictionVariance() {
	stats := e.vp9PhaseStats()
	if stats != nil {
		atomic.AddInt64(&stats.VP9InterPredictionVarianceCalls, 1)
	}
}

func (e *VP9Encoder) vp9PhaseCountFullPelSearch(bsize common.BlockSize,
	skipMVPart, skipIntPro bool,
) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	if skipMVPart {
		atomic.AddInt64(&stats.VP9FullPelSearchSkipMVPart, 1)
		return
	}
	if skipIntPro {
		atomic.AddInt64(&stats.VP9FullPelSearchSkipIntPro, 1)
		return
	}
	atomic.AddInt64(&stats.VP9FullPelSearches, 1)
	switch bsize {
	case common.Block64x64:
		atomic.AddInt64(&stats.VP9FullPelSearch64x64, 1)
	case common.Block32x32:
		atomic.AddInt64(&stats.VP9FullPelSearch32x32, 1)
	case common.Block32x16:
		atomic.AddInt64(&stats.VP9FullPelSearch32x16, 1)
	case common.Block16x32:
		atomic.AddInt64(&stats.VP9FullPelSearch16x32, 1)
	case common.Block16x16:
		atomic.AddInt64(&stats.VP9FullPelSearch16x16, 1)
	case common.Block16x8:
		atomic.AddInt64(&stats.VP9FullPelSearch16x8, 1)
	case common.Block8x16:
		atomic.AddInt64(&stats.VP9FullPelSearch8x16, 1)
	case common.Block8x8:
		atomic.AddInt64(&stats.VP9FullPelSearch8x8, 1)
	}
}

func (e *VP9Encoder) vp9PhaseAddFullPelSAD(candidates int64, batch bool,
	source vp9FullPelSADSource,
) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.FullPelSADCalls, 1)
	atomic.AddInt64(&stats.FullPelSADCandidates, candidates)
	if batch {
		atomic.AddInt64(&stats.FullPelBatchCalls, 1)
	}
	switch source {
	case vp9FullPelSADSourceZero:
		atomic.AddInt64(&stats.VP9FullPelSADZeroCalls, 1)
		atomic.AddInt64(&stats.VP9FullPelSADZeroCandidates, candidates)
	case vp9FullPelSADSourceSeed:
		atomic.AddInt64(&stats.VP9FullPelSADSeedCalls, 1)
		atomic.AddInt64(&stats.VP9FullPelSADSeedCandidates, candidates)
	case vp9FullPelSADSourceHint:
		atomic.AddInt64(&stats.VP9FullPelSADHintCalls, 1)
		atomic.AddInt64(&stats.VP9FullPelSADHintCandidates, candidates)
	case vp9FullPelSADSourcePattern:
		atomic.AddInt64(&stats.VP9FullPelSADPatternCalls, 1)
		atomic.AddInt64(&stats.VP9FullPelSADPatternCandidates, candidates)
	case vp9FullPelSADSourceFullRD:
		atomic.AddInt64(&stats.VP9FullPelSADFullRDCalls, 1)
		atomic.AddInt64(&stats.VP9FullPelSADFullRDCandidates, candidates)
	default:
		atomic.AddInt64(&stats.VP9FullPelSADOtherCalls, 1)
		atomic.AddInt64(&stats.VP9FullPelSADOtherCandidates, candidates)
	}
}

func (e *VP9Encoder) vp9PhaseIncTileWorkerJob(kind vp9TileWorkerJobKind) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	switch kind {
	case vp9TileWorkerJobCount:
		atomic.AddInt64(&stats.VP9TileWorkerCountJobRuns, 1)
	default:
		atomic.AddInt64(&stats.VP9TileWorkerEncodeJobRuns, 1)
	}
}

func (e *VP9Encoder) vp9PhaseAddRowWorkerCountEpoch(jobs int) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.VP9RowWorkerCountEpochs, 1)
	atomic.AddInt64(&stats.VP9RowWorkerCountJobs, int64(jobs))
}

func (p *vp9TileWorkerPool) setVP9TileWorkerPhaseStats(stats *EncoderPhaseStats) {
	if p != nil {
		p.phaseStats = stats
	}
}

func (p *vp9TileWorkerPool) vp9TileWorkerPhaseStatsActive() bool {
	return p != nil && p.phaseStats != nil
}

func (p *vp9TileWorkerPool) vp9PhaseStartTileWorkerEpoch(kind vp9TileWorkerJobKind) {
	if p == nil || p.phaseStats == nil {
		return
	}
	switch kind {
	case vp9TileWorkerJobCount:
		atomic.AddInt64(&p.phaseStats.VP9TileWorkerCountEpochs, 1)
	case vp9TileWorkerJobEncodePrep, vp9TileWorkerJobLoopFilter:
		// Prep and loop-filter epochs piggyback on the encode pass; only
		// their wake signals are interesting, so they are not counted as
		// encode epochs.
	default:
		atomic.AddInt64(&p.phaseStats.VP9TileWorkerEncodeEpochs, 1)
	}
}

func (p *vp9TileWorkerPool) vp9PhaseAddTileWorkerWakeSignals(n int64) {
	if p != nil && p.phaseStats != nil && n != 0 {
		atomic.AddInt64(&p.phaseStats.VP9TileWorkerWakeSignals, n)
	}
}

func (p *vp9TileWorkerPool) vp9PhaseIncTileWorkerPark() {
	if p != nil && p.phaseStats != nil {
		atomic.AddInt64(&p.phaseStats.VP9TileWorkerParks, 1)
	}
}

func (p *vp9TileWorkerPool) vp9PhaseAddTileWorkerWait(spins, goscheds int64) {
	if p == nil || p.phaseStats == nil {
		return
	}
	if spins != 0 {
		atomic.AddInt64(&p.phaseStats.VP9TileWorkerWaitSpins, spins)
	}
	if goscheds != 0 {
		atomic.AddInt64(&p.phaseStats.VP9TileWorkerWaitGoscheds, goscheds)
	}
}

func (d *VP9Decoder) setVP9PhaseStats(stats *EncoderPhaseStats) {
	if d != nil {
		d.phaseStats = stats
	}
}

func (d *VP9Decoder) vp9PhaseIncInterPredictPlane() {
	if d == nil || d.phaseStats == nil {
		return
	}
	atomic.AddInt64(&d.phaseStats.VP9InterPredictPlaneCalls, 1)
}

func (d *VP9Decoder) vp9PhaseCountInterPredictor(key int) {
	if d == nil || d.phaseStats == nil {
		return
	}
	stats := d.phaseStats
	switch key {
	case 0:
		atomic.AddInt64(&stats.VP9InterPredictorCopy, 1)
	case 1:
		atomic.AddInt64(&stats.VP9InterPredictorAvg, 1)
	case 2:
		atomic.AddInt64(&stats.VP9InterPredictorVert, 1)
	case 3:
		atomic.AddInt64(&stats.VP9InterPredictorAvgVert, 1)
	case 4:
		atomic.AddInt64(&stats.VP9InterPredictorHoriz, 1)
	case 5:
		atomic.AddInt64(&stats.VP9InterPredictorAvgHoriz, 1)
	case 6:
		atomic.AddInt64(&stats.VP9InterPredictor2D, 1)
	case 7:
		atomic.AddInt64(&stats.VP9InterPredictorAvg2D, 1)
	}
}
