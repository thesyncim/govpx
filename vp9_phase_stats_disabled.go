//go:build !govpx_phase_stats

package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

const vp9PhaseStatsEnabled = false

type vp9FullPelSADSource uint8

const (
	vp9FullPelSADSourceOther vp9FullPelSADSource = iota
	vp9FullPelSADSourceZero
	vp9FullPelSADSourceSeed
	vp9FullPelSADSourceHint
	vp9FullPelSADSourcePattern
	vp9FullPelSADSourceFullRD
)

type vp9DecoderPhaseStatsOptions struct{}

type vp9TileWorkerPhaseStatsOptions struct{}

type vp9EncoderPhase uint8

const (
	vp9EncoderPhaseCount vp9EncoderPhase = iota
	vp9EncoderPhaseHeaderWrite
	vp9EncoderPhaseTileWrite
	vp9EncoderPhaseLoopFilterPick
	vp9EncoderPhaseLoopFilterApply
)

func (e *VP9Encoder) vp9PhaseStats() *EncoderPhaseStats {
	return nil
}

func (e *VP9Encoder) vp9PhaseStart() int64 {
	return 0
}

func (e *VP9Encoder) vp9PhaseEnd(vp9EncoderPhase, int64) {}

func (e *VP9Encoder) vp9PhaseStatsActive() bool {
	return false
}

func (e *VP9Encoder) vp9PhaseCountAttempt(bool) {}

func (e *VP9Encoder) vp9PhaseCountPreEncodeDrop(vp9DropReason) {}

func (e *VP9Encoder) vp9PhaseCountPostEncodeDrop(int) {}

func (e *VP9Encoder) vp9PhaseIncFrameTile(bool) {}

func (e *VP9Encoder) vp9PhaseIncModeSB(bool) {}

func (e *VP9Encoder) vp9PhaseIncModeBlock(common.BlockSize, bool) {}

func (e *VP9Encoder) vp9PhaseIncInterModePick(bool) {}

func (e *VP9Encoder) vp9PhaseIncInterLeafCacheStore() {}

func (e *VP9Encoder) vp9PhaseCountInterLeafReplay(bool) {}

func (e *VP9Encoder) vp9PhaseCountVarPartChoose(bool, bool) {}

func (e *VP9Encoder) vp9PhaseCountVarPartCacheHit(bool) {}

func (e *VP9Encoder) vp9PhaseAddVarPartMergedSBs(int64) {}

func (e *VP9Encoder) vp9PhaseCountVarPartContentState(encoder.ContentStateSB) {}

func (e *VP9Encoder) vp9PhaseAddChoosePartitioningStats(encoder.ChoosePartitioningStats) {}

func (e *VP9Encoder) vp9PhaseIncInterPredictionBlock() {}

func (e *VP9Encoder) vp9PhaseIncInterPredictionVariance() {}

func (e *VP9Encoder) vp9PhaseCountFullPelSearch(common.BlockSize, bool, bool) {}

func (e *VP9Encoder) vp9PhaseAddFullPelSAD(int64, bool, vp9FullPelSADSource) {}

func (e *VP9Encoder) vp9PhaseIncTileWorkerJob(vp9TileWorkerJobKind) {}

func (p *vp9TileWorkerPool) setVP9TileWorkerPhaseStats(*EncoderPhaseStats) {}

func (p *vp9TileWorkerPool) vp9TileWorkerPhaseStatsActive() bool {
	return false
}

func (p *vp9TileWorkerPool) vp9PhaseStartTileWorkerEpoch(vp9TileWorkerJobKind) {}

func (p *vp9TileWorkerPool) vp9PhaseAddTileWorkerWakeSignals(int64) {}

func (p *vp9TileWorkerPool) vp9PhaseIncTileWorkerPark() {}

func (p *vp9TileWorkerPool) vp9PhaseAddTileWorkerWait(int64, int64) {}

func (d *VP9Decoder) setVP9PhaseStats(*EncoderPhaseStats) {}

func (d *VP9Decoder) vp9PhaseIncInterPredictPlane() {}

func (d *VP9Decoder) vp9PhaseCountInterPredictor(int) {}
