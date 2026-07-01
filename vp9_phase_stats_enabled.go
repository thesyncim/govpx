//go:build govpx_phase_stats

package govpx

import (
	"sync/atomic"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

const vp9PhaseStatsEnabled = true

type vp9DecoderPhaseStatsOptions struct {
	phaseStats *EncoderPhaseStats
}

func (e *VP9Encoder) vp9PhaseStats() *EncoderPhaseStats {
	if e == nil {
		return nil
	}
	return e.opts.PhaseStats
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

func (e *VP9Encoder) vp9PhaseCountVarPartChoose(copied bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.VP9VarPartChooseCalls, 1)
	if copied {
		atomic.AddInt64(&stats.VP9VarPartCopyHits, 1)
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

func (e *VP9Encoder) vp9PhaseAddFullPelSAD(candidates int64, batch bool) {
	stats := e.vp9PhaseStats()
	if stats == nil {
		return
	}
	atomic.AddInt64(&stats.FullPelSADCalls, 1)
	atomic.AddInt64(&stats.FullPelSADCandidates, candidates)
	if batch {
		atomic.AddInt64(&stats.FullPelBatchCalls, 1)
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
