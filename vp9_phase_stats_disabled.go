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

func (e *VP9Encoder) vp9PhaseStats() *EncoderPhaseStats {
	return nil
}

func (e *VP9Encoder) vp9PhaseCountAttempt(bool) {}

func (e *VP9Encoder) vp9PhaseCountPreEncodeDrop(vp9DropReason) {}

func (e *VP9Encoder) vp9PhaseCountPostEncodeDrop(int) {}

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

func (d *VP9Decoder) setVP9PhaseStats(*EncoderPhaseStats) {}

func (d *VP9Decoder) vp9PhaseIncInterPredictPlane() {}

func (d *VP9Decoder) vp9PhaseCountInterPredictor(int) {}
