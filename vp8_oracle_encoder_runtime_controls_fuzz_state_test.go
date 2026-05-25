//go:build govpx_oracle_trace

package govpx

import vp8common "github.com/thesyncim/govpx/internal/vp8/common"

func vp8OracleRuntimeCurrentDropConfig(e *VP8Encoder) (bool, int) {
	if e == nil || !e.opts.DropFrameAllowed {
		return false, 0
	}
	return true, e.opts.DropFrameWaterMark
}

func vp8OracleRuntimeCurrentRateControlConfig(e *VP8Encoder) RateControlConfig {
	if e == nil {
		return RateControlConfig{}
	}
	return RateControlConfig{
		Mode:                e.rc.mode,
		TargetBitrateKbps:   e.rc.targetBitrateKbps,
		MinBitrateKbps:      e.rc.minBitrateKbps,
		MaxBitrateKbps:      e.rc.maxBitrateKbps,
		MinQuantizer:        vp8common.QIndexToPublicQuantizer(e.rc.minQuantizer),
		MaxQuantizer:        vp8common.QIndexToPublicQuantizer(e.rc.maxQuantizer),
		CQLevel:             e.opts.CQLevel,
		UndershootPct:       e.rc.undershootPct,
		OvershootPct:        e.rc.overshootPct,
		BufferSizeMs:        e.rc.bufferSizeMs,
		BufferInitialSizeMs: e.rc.bufferInitialSizeMs,
		BufferOptimalSizeMs: e.rc.bufferOptimalSizeMs,
		DropFrameAllowed:    e.rc.dropFramesWaterMark > 0,
		DropFrameWaterMark:  e.rc.dropFramesWaterMark,
		MaxIntraBitratePct:  e.rc.maxIntraBitratePct,
		GFCBRBoostPct:       e.rc.gfCBRBoostPct,
	}
}
