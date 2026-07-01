package benchcmd

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	govpx "github.com/thesyncim/govpx"
)

func formatEncodeReport(r benchReport) string {
	var b bytes.Buffer
	codec := r.Codec
	if codec == "" {
		codec = "vp8"
	}
	fmt.Fprintf(&b, "govpx-bench  encode  %s  %s  %dx%d @%dfps  target=%d kbps  frames=%d\n\n",
		codec, r.Mode, r.Width, r.Height, r.FPS, r.TargetBitrateKbps, r.Frames)

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	if r.Reference != nil {
		ref := r.Reference
		cmp := r.Comparison
		fmt.Fprintln(tw, "metric\tgovpx\tlibvpx\tdelta")
		fmt.Fprintln(tw, "------\t-----\t------\t-----")
		fmt.Fprintf(tw, "ns/frame\t%s\t%s\t%s\n",
			formatDuration(r.NSPerFrame), formatDuration(ref.NSPerFrame), formatRatio(cmp.NSPerFrameRatio, "x"))
		fmt.Fprintf(tw, "encode fps\t%s\t%s\t%s\n",
			formatFloat(r.EncodeFPS, 1), formatFloat(ref.EncodeFPS, 1), formatRatio(cmp.EncodeFPSRatio, "x"))
		fmt.Fprintf(tw, "MB/s (mblocks)\t%s\t%s\t-\n",
			formatFloat(r.MacroblocksPerSec/1e6, 2), formatFloat(ref.MacroblocksPerSec/1e6, 2))
		fmt.Fprintf(tw, "output kbps\t%.2f\t%.2f\t%s\n",
			r.OutputBitrateKbps, ref.OutputBitrateKbps, formatRatio(cmp.BitrateRatioVsReference, "x"))
		fmt.Fprintf(tw, "bitrate err %%\t%+.2f\t%+.2f\t%+.2f pp\n",
			r.BitrateErrorPct, ref.BitrateErrorPct, cmp.BitrateErrorPctDelta)
		fmt.Fprintf(tw, "frames encoded/dropped\t%d/%d\t%d/%d\t%+d dropped\n",
			r.EncodedFrames, r.DroppedFrames, ref.EncodedFrames, ref.DroppedFrames, r.DroppedFrames-ref.DroppedFrames)
		if r.QualitySkipped || ref.QualitySkipped {
			fmt.Fprintln(tw, "quality\t(skipped)\t(skipped)\t-")
		} else {
			fmt.Fprintf(tw, "PSNR (dB)\t%.2f\t%.2f\t%+.2f\n",
				r.PSNR, ref.PSNR, cmp.PSNRDeltaDB)
			fmt.Fprintf(tw, "SSIM\t%.5f\t%.5f\t%+.5f\n",
				r.SSIM, ref.SSIM, cmp.SSIMDelta)
		}
		fmt.Fprintf(tw, "output bytes\t%s\t%s\t%s\n",
			formatBytes(int64(r.OutputBytes)), formatBytes(int64(ref.OutputBytes)), formatRatio(cmp.OutputBytesRatio, "x"))
		fmt.Fprintf(tw, "keyframe bytes\t%s\t%s\t%s\n",
			formatBytes(int64(r.KeyframeBytes)), formatBytes(int64(ref.KeyframeBytes)), formatRatio(cmp.KeyframeBytesRatio, "x"))
		fmt.Fprintf(tw, "avg interframe\t%s\t%s\t%s\n",
			formatBytes(int64(r.AvgInterBytes)), formatBytes(int64(ref.AvgInterBytes)), formatRatio(cmp.AvgInterBytesRatio, "x"))
	} else {
		fmt.Fprintln(tw, "metric\tgovpx")
		fmt.Fprintln(tw, "------\t-----")
		fmt.Fprintf(tw, "ns/frame\t%s\n", formatDuration(r.NSPerFrame))
		fmt.Fprintf(tw, "encode fps\t%s\n", formatFloat(r.EncodeFPS, 1))
		fmt.Fprintf(tw, "MB/s (mblocks)\t%s\n", formatFloat(r.MacroblocksPerSec/1e6, 2))
		fmt.Fprintf(tw, "output kbps\t%.2f\n", r.OutputBitrateKbps)
		fmt.Fprintf(tw, "bitrate err %%\t%+.2f\n", r.BitrateErrorPct)
		if r.QualitySkipped {
			fmt.Fprintln(tw, "quality\t(skipped)")
		} else {
			fmt.Fprintf(tw, "PSNR (dB)\t%.2f\n", r.PSNR)
			fmt.Fprintf(tw, "SSIM\t%.5f\n", r.SSIM)
		}
		fmt.Fprintf(tw, "output bytes\t%s\n", formatBytes(int64(r.OutputBytes)))
		fmt.Fprintf(tw, "keyframe bytes\t%s\n", formatBytes(int64(r.KeyframeBytes)))
		fmt.Fprintf(tw, "avg interframe\t%s\n", formatBytes(int64(r.AvgInterBytes)))
	}
	tw.Flush()

	fmt.Fprintf(&b, "\nquantizers      min=%d max=%d mean=%.2f  (encoded=%d dropped=%d)\n",
		r.Quantizers.Min, r.Quantizers.Max, r.Quantizers.Mean, r.EncodedFrames, r.DroppedFrames)
	fmt.Fprintf(&b, "govpx latency   p50=%s  p95=%s  p99=%s\n",
		formatDuration(r.LatencyNS.P50), formatDuration(r.LatencyNS.P95), formatDuration(r.LatencyNS.P99))
	if r.PhaseNS != nil {
		appendEncodePhaseReport(&b, *r.PhaseNS, r.Frames)
	}
	if r.Reference != nil {
		ref := r.Reference
		fmt.Fprintf(&b, "libvpx timing   source=%s  wall/frame=%s  subprocess=%s\n",
			ref.TimingSource, formatDuration(ref.WallNSPerFrame), formatDuration(ref.SubprocessOverheadNS))
		if ref.VP9CallStats != nil {
			appendVP9CallStatsReport(&b, *ref.VP9CallStats)
		}
		if len(ref.ParityFlags) > 0 {
			fmt.Fprintf(&b, "libvpx parity   %s\n",
				strings.Join(ref.ParityFlags, " "))
		}
		if ref.QualityError != "" {
			fmt.Fprintf(&b, "libvpx quality  warn: %s\n", ref.QualityError)
		}
	}
	if r.AllocsPerFrame > 0 {
		fmt.Fprintf(&b, "allocs/frame    %.2f\n", r.AllocsPerFrame)
	}
	return b.String()
}

func appendVP9CallStatsReport(b *bytes.Buffer, stats vp9CallStats) {
	fmt.Fprintf(b, "libvpx topology mode_blocks=%d  inter_picks=%d  fullpel=%d  sad_calls=%d  sad_candidates=%d\n",
		stats.ModeBlocks(),
		stats.InterModePicks,
		stats.FullpelSearches,
		stats.SADCalls,
		stats.SADCandidates)
	fmt.Fprintf(b, "libvpx blocks   64=%d  32=%d  16=%d  8=%d  sub8=%d  varpart=%d  copy=%d\n",
		stats.ModeBlock64x64,
		stats.ModeBlock32x32+stats.ModeBlock32x16+stats.ModeBlock16x32,
		stats.ModeBlock16x16+stats.ModeBlock16x8+stats.ModeBlock8x16,
		stats.ModeBlock8x8,
		stats.ModeBlockSub8,
		stats.VarpartChooseCalls,
		stats.VarpartCopyHits)
	fmt.Fprintf(b, "libvpx predictor copy=%d  avg=%d  horiz=%d  vert=%d  2d=%d  avg_h=%d  avg_v=%d  avg_2d=%d\n",
		stats.PredictorCopy,
		stats.PredictorAvg,
		stats.PredictorHoriz,
		stats.PredictorVert,
		stats.Predictor2D,
		stats.PredictorAvgHoriz,
		stats.PredictorAvgVert,
		stats.PredictorAvg2D)
	if stats.VarpartContentStates() > 0 {
		fmt.Fprintf(b, "libvpx content  invalid=%d  low_ll=%d  low_lh=%d  high_hl=%d  high_hh=%d  lowvar=%d  very_high=%d\n",
			stats.VarpartContentStateInvalid,
			stats.VarpartContentStateLowSadLowSumdiff,
			stats.VarpartContentStateLowSadHighSumdiff,
			stats.VarpartContentStateHighSadLowSumdiff,
			stats.VarpartContentStateHighSadHighSumdiff,
			stats.VarpartContentStateLowVarHighSumdiff,
			stats.VarpartContentStateVeryHighSad)
	}
	if stats.VarpartSetVTCalls > 0 || stats.VarpartForceSplit64 > 0 ||
		stats.VarpartForceSplit32 > 0 || stats.VarpartForceSplit16 > 0 {
		fmt.Fprintf(b, "libvpx varpart y_sad=%d  y_sad64=%d  copy_select=%d  force64=%d  force32=%d  force16=%d\n",
			stats.VarpartYSADValid,
			stats.VarpartYSADSelect64x64,
			stats.VarpartCopyPartitionSelect,
			stats.VarpartForceSplit64,
			stats.VarpartForceSplit32,
			stats.VarpartForceSplit16)
		fmt.Fprintf(b, "libvpx setvt   calls=%d  blocks=%d/%d/%d/%d  force=%d/%d/%d/%d  select=%d  split=%d\n",
			stats.VarpartSetVTCalls,
			stats.VarpartSetVT64x64,
			stats.VarpartSetVT32x32,
			stats.VarpartSetVT16x16,
			stats.VarpartSetVT8x8,
			stats.VarpartSetVTForceSplit,
			stats.VarpartSetVTForceSplit64x64,
			stats.VarpartSetVTForceSplit32x32,
			stats.VarpartSetVTForceSplit16x16,
			stats.VarpartSetVTSelect,
			stats.VarpartSetVTSplit)
		if stats.VarpartVar16Samples > 0 || stats.VarpartThreshold2Count > 0 {
			fmt.Fprintf(b, "libvpx var16   samples=%d  var_avg=%d  th2_avg=%d  force_var=%d  force_minmax=%d  force_var_avg=%d  force_th2_avg=%d\n",
				stats.VarpartVar16Samples,
				avgUint64(stats.VarpartVar16Sum, stats.VarpartVar16Samples),
				avgUint64(stats.VarpartThreshold2Sum, stats.VarpartThreshold2Count),
				stats.VarpartForceSplit16Variance,
				stats.VarpartForceSplit16Minmax,
				avgUint64(stats.VarpartForce16VarianceSum, stats.VarpartForceSplit16Variance),
				avgUint64(stats.VarpartForce16ThresholdSum, stats.VarpartForceSplit16Variance))
		}
	}
}

func appendEncodePhaseReport(b *bytes.Buffer, stats govpx.EncoderPhaseStats, frames int) {
	if frames <= 0 {
		frames = 1
	}
	fmt.Fprintf(b, "phase/frame     inter_recon=%s  key_recon=%s  lf_pick=%s  lf_apply=%s  packet=%s\n",
		formatDuration(stats.InterReconstructNS/int64(frames)),
		formatDuration(stats.KeyReconstructNS/int64(frames)),
		formatDuration(stats.LoopFilterPickNS/int64(frames)),
		formatDuration(stats.LoopFilterApplyNS/int64(frames)),
		formatDuration(stats.PacketWriteNS/int64(frames)))
	fmt.Fprintf(b, "phase attempts  inter=%d  key=%d\n", stats.InterAttempts, stats.KeyAttempts)
	if stats.LoopFilterTrials > 0 {
		fmt.Fprintf(b, "lf trials       count=%d  copy=%s  filter=%s  sse=%s\n",
			stats.LoopFilterTrials,
			formatDuration(stats.LoopFilterTrialCopyNS/stats.LoopFilterTrials),
			formatDuration(stats.LoopFilterTrialFilterNS/stats.LoopFilterTrials),
			formatDuration(stats.LoopFilterTrialSSENS/stats.LoopFilterTrials))
	}
	if stats.InterRDCoeffCacheRequests > 0 || stats.InterCoefTokenRecords > 0 {
		fmt.Fprintf(b, "coeff pipeline  rd_cache=%d  dct_hits=%d  token_records=%d\n",
			stats.InterRDCoeffCacheRequests,
			stats.InterRDCoeffCacheDCTHits,
			stats.InterCoefTokenRecords)
	}
	if stats.FullPelSADCalls > 0 || stats.SubpelCandidates > 0 {
		fmt.Fprintf(b, "motion search   sad_calls=%d  sad_candidates=%d  sad4=%d  subpel=%d  variance=%d  cache_hits=%d\n",
			stats.FullPelSADCalls,
			stats.FullPelSADCandidates,
			stats.FullPelBatchCalls,
			stats.SubpelCandidates,
			stats.SubpelVarianceCalls,
			stats.SubpelCacheHits)
	}
	if stats.VP9FullPelSearches > 0 || stats.VP9FullPelSearchSkipMVPart > 0 ||
		stats.VP9FullPelSearchSkipIntPro > 0 {
		fmt.Fprintf(b, "vp9 fullpel    searches=%d  skip_mvpart=%d  skip_intpro=%d  blocks=%d/%d/%d/%d/%d/%d/%d/%d\n",
			stats.VP9FullPelSearches,
			stats.VP9FullPelSearchSkipMVPart,
			stats.VP9FullPelSearchSkipIntPro,
			stats.VP9FullPelSearch64x64,
			stats.VP9FullPelSearch32x32,
			stats.VP9FullPelSearch32x16,
			stats.VP9FullPelSearch16x32,
			stats.VP9FullPelSearch16x16,
			stats.VP9FullPelSearch16x8,
			stats.VP9FullPelSearch8x16,
			stats.VP9FullPelSearch8x8)
	}
	if stats.VP9ModeBlocks > 0 || stats.VP9InterPredictionBlocks > 0 {
		fmt.Fprintf(b, "vp9 topology    mode_blocks=%d  inter_picks=%d  pred_blocks=%d  pred_planes=%d  pred_var=%d\n",
			stats.VP9ModeBlocks,
			stats.VP9InterModePicks,
			stats.VP9InterPredictionBlocks,
			stats.VP9InterPredictPlaneCalls,
			stats.VP9InterPredictionVarianceCalls)
		fmt.Fprintf(b, "vp9 mode pass   count=%d  write=%d  blocks=%d/%d/%d/%d/%d/%d/%d/%d/%d\n",
			stats.VP9ModeBlocksCountPass,
			stats.VP9ModeBlocksWritePass,
			stats.VP9ModeBlock64x64,
			stats.VP9ModeBlock32x32,
			stats.VP9ModeBlock32x16,
			stats.VP9ModeBlock16x32,
			stats.VP9ModeBlock16x16,
			stats.VP9ModeBlock16x8,
			stats.VP9ModeBlock8x16,
			stats.VP9ModeBlock8x8,
			stats.VP9ModeBlockSub8)
		fmt.Fprintf(b, "vp9 inter pass  picks_count=%d  picks_write=%d  replay_store=%d  replay_hit=%d  replay_miss=%d\n",
			stats.VP9InterModePicksCountPass,
			stats.VP9InterModePicksWritePass,
			stats.VP9InterLeafCacheStores,
			stats.VP9InterLeafCacheReplayHits,
			stats.VP9InterLeafCacheReplayMisses)
		fmt.Fprintf(b, "vp9 predictor   copy=%d  avg=%d  horiz=%d  vert=%d  2d=%d  avg_h=%d  avg_v=%d  avg_2d=%d\n",
			stats.VP9InterPredictorCopy,
			stats.VP9InterPredictorAvg,
			stats.VP9InterPredictorHoriz,
			stats.VP9InterPredictorVert,
			stats.VP9InterPredictor2D,
			stats.VP9InterPredictorAvgHoriz,
			stats.VP9InterPredictorAvgVert,
			stats.VP9InterPredictorAvg2D)
	}
	if stats.VP9VarPartSetVTCalls > 0 || stats.VP9VarPartForceSplit64 > 0 ||
		stats.VP9VarPartForceSplit32 > 0 || stats.VP9VarPartForceSplit16 > 0 {
		fmt.Fprintf(b, "vp9 varpass     choose=%d  count=%d  write=%d  cache=%d/%d  copy_hits=%d  merged=%d\n",
			stats.VP9VarPartChooseCalls,
			stats.VP9VarPartChooseCountPass,
			stats.VP9VarPartChooseWritePass,
			stats.VP9VarPartCacheHits,
			stats.VP9VarPartCacheMisses,
			stats.VP9VarPartCopyHits,
			stats.VP9VarPartMergedSBs)
		fmt.Fprintf(b, "vp9 content     invalid=%d  low_ll=%d  low_lh=%d  high_hl=%d  high_hh=%d  lowvar=%d  very_high=%d\n",
			stats.VP9VarPartContentStateInvalid,
			stats.VP9VarPartContentStateLowSadLowSumdiff,
			stats.VP9VarPartContentStateLowSadHighSumdiff,
			stats.VP9VarPartContentStateHighSadLowSumdiff,
			stats.VP9VarPartContentStateHighSadHighSumdiff,
			stats.VP9VarPartContentStateLowVarHighSumdiff,
			stats.VP9VarPartContentStateVeryHighSad)
		fmt.Fprintf(b, "vp9 varpart     y_sad=%d  y_sad64=%d  copy_select=%d  force64=%d  force32=%d  force16=%d\n",
			stats.VP9VarPartYSADValid,
			stats.VP9VarPartYSADSelect64x64,
			stats.VP9VarPartCopyPartitionSelect,
			stats.VP9VarPartForceSplit64,
			stats.VP9VarPartForceSplit32,
			stats.VP9VarPartForceSplit16)
		fmt.Fprintf(b, "vp9 setvt       calls=%d  blocks=%d/%d/%d/%d  force=%d/%d/%d/%d  select=%d  split=%d\n",
			stats.VP9VarPartSetVTCalls,
			stats.VP9VarPartSetVT64x64,
			stats.VP9VarPartSetVT32x32,
			stats.VP9VarPartSetVT16x16,
			stats.VP9VarPartSetVT8x8,
			stats.VP9VarPartSetVTForceSplit,
			stats.VP9VarPartSetVTForceSplit64x64,
			stats.VP9VarPartSetVTForceSplit32x32,
			stats.VP9VarPartSetVTForceSplit16x16,
			stats.VP9VarPartSetVTSelect,
			stats.VP9VarPartSetVTSplit)
		if stats.VP9VarPartVar16Samples > 0 || stats.VP9VarPartThreshold2Count > 0 {
			fmt.Fprintf(b, "vp9 var16       samples=%d  var_avg=%d  th2_avg=%d  force_var=%d  force_minmax=%d  force_var_avg=%d  force_th2_avg=%d\n",
				stats.VP9VarPartVar16Samples,
				avgInt64(stats.VP9VarPartVar16Sum, stats.VP9VarPartVar16Samples),
				avgInt64(stats.VP9VarPartThreshold2Sum, stats.VP9VarPartThreshold2Count),
				stats.VP9VarPartForceSplit16Variance,
				stats.VP9VarPartForceSplit16Minmax,
				avgInt64(stats.VP9VarPartForce16VarianceSum, stats.VP9VarPartForceSplit16Variance),
				avgInt64(stats.VP9VarPartForce16ThresholdSum, stats.VP9VarPartForceSplit16Variance))
		}
	}
}

func avgUint64(sum, count uint64) uint64 {
	if count == 0 {
		return 0
	}
	return sum / count
}

func avgInt64(sum, count int64) int64 {
	if count == 0 {
		return 0
	}
	return sum / count
}

func formatSuiteReport(r suiteReport) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "govpx-bench  suite  %s  runs=%d  selector=%s\n", r.Name, r.Runs, r.Selector)
	if r.LibvpxVpxenc != "" {
		fmt.Fprintf(&b, "libvpx       %s\n", r.LibvpxVpxenc)
	}
	if r.GeomeanNSGap > 0 {
		fmt.Fprintf(&b, "geomean      ns/frame=%s  encode_fps=%s\n",
			formatRatio(r.GeomeanNSGap, "x"), formatRatio(r.GeomeanFPSGap, "x"))
	}
	fmt.Fprintln(&b)

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "case\tmode\tsize\tframes\ttarget\tgovpx\tlibvpx\tgap\tfps\trate\tPSNR\tSSIM\tdrop")
	fmt.Fprintln(tw, "----\t----\t----\t------\t------\t-----\t------\t---\t---\t----\t----\t----\t----")
	for _, c := range r.Cases {
		rep := c.Report
		ref := rep.Reference
		cmp := rep.Comparison
		if ref == nil || cmp == nil {
			continue
		}
		psnr := "skip"
		ssim := "skip"
		if !rep.QualitySkipped && !ref.QualitySkipped {
			psnr = fmt.Sprintf("%+.2f", cmp.PSNRDeltaDB)
			ssim = fmt.Sprintf("%+.5f", cmp.SSIMDelta)
		}
		fmt.Fprintf(tw, "%s\t%s\t%dx%d\t%d\t%d\t%s\t%s\t%s\t%s/%s\t%s\t%s\t%s\t%d/%d\n",
			c.Name,
			rep.Mode,
			rep.Width,
			rep.Height,
			rep.Frames,
			rep.TargetBitrateKbps,
			formatDuration(rep.NSPerFrame),
			formatDuration(ref.NSPerFrame),
			formatRatio(cmp.NSPerFrameRatio, "x"),
			formatFloat(rep.EncodeFPS, 1),
			formatFloat(ref.EncodeFPS, 1),
			formatRatio(cmp.BitrateRatioVsReference, "x"),
			psnr,
			ssim,
			rep.DroppedFrames,
			ref.DroppedFrames)
	}
	tw.Flush()

	if r.PhaseTiming {
		fmt.Fprintln(&b, "\ngovpx phase/frame")
		tw = tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "case\tinter_recon\tkey_recon\tlf_pick\tlf_apply\tpacket")
		fmt.Fprintln(tw, "----\t-----------\t---------\t-------\t--------\t------")
		for _, c := range r.Cases {
			rep := c.Report
			if rep.PhaseNS == nil {
				continue
			}
			frames := int64(rep.Frames)
			if frames <= 0 {
				frames = 1
			}
			stats := rep.PhaseNS
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				c.Name,
				formatDuration(stats.InterReconstructNS/frames),
				formatDuration(stats.KeyReconstructNS/frames),
				formatDuration(stats.LoopFilterPickNS/frames),
				formatDuration(stats.LoopFilterApplyNS/frames),
				formatDuration(stats.PacketWriteNS/frames))
		}
		tw.Flush()
	}
	return b.String()
}

func formatDecodeReport(r decodeBenchReport) string {
	var b bytes.Buffer
	codec := r.Codec
	if codec == "" {
		codec = codecVP8
	}
	fmt.Fprintf(&b, "govpx-bench  decode  %s  %s  %dx%d @%dfps  frames=%d  input=%s\n\n",
		codec, r.Mode, r.Width, r.Height, r.FPS, r.Frames, formatBytes(int64(r.InputBytes)))

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	if r.Reference != nil {
		ref := r.Reference
		cmp := r.Comparison
		relativeSpeed := r.RelativeSpeedVsReference
		decodeFPSRatio := 0.0
		decodedDelta := r.DecodedFrames - ref.DecodedFrames
		if cmp != nil {
			if cmp.DecodeFPSRatio > 0 {
				relativeSpeed = cmp.DecodeFPSRatio
			}
			decodeFPSRatio = cmp.DecodeFPSRatio
			decodedDelta = cmp.DecodedFramesDelta
		}
		fmt.Fprintln(tw, "metric\tgovpx\tlibvpx\trelative")
		fmt.Fprintln(tw, "------\t-----\t------\t--------")
		fmt.Fprintf(tw, "ns/frame\t%s\t%s\t%s\n",
			formatDuration(r.NSPerFrame), formatDuration(ref.NSPerFrame), formatRatio(relativeSpeed, "x faster"))
		fmt.Fprintf(tw, "decode fps\t%s\t%s\t%s\n",
			formatFloat(r.DecodeFPS, 1), formatFloat(ref.DecodeFPS, 1), formatRatio(decodeFPSRatio, "x"))
		fmt.Fprintf(tw, "MB/s (mblocks)\t%s\t%s\t-\n",
			formatFloat(r.MacroblocksPerSec/1e6, 2), formatFloat(ref.MacroblocksPerSec/1e6, 2))
		fmt.Fprintf(tw, "coded MB/s\t%s\t%s\t-\n",
			formatFloat(r.CodedMegabytesPerSec, 2), formatFloat(ref.CodedMegabytesPerSec, 2))
		fmt.Fprintf(tw, "frames decoded\t%d/%d\t%d/%d\t%+d\n",
			r.DecodedFrames, r.Frames, ref.DecodedFrames, r.Frames, decodedDelta)
	} else {
		fmt.Fprintln(tw, "metric\tgovpx")
		fmt.Fprintln(tw, "------\t-----")
		fmt.Fprintf(tw, "ns/frame\t%s\n", formatDuration(r.NSPerFrame))
		fmt.Fprintf(tw, "decode fps\t%s\n", formatFloat(r.DecodeFPS, 1))
		fmt.Fprintf(tw, "MB/s (mblocks)\t%s\n", formatFloat(r.MacroblocksPerSec/1e6, 2))
		fmt.Fprintf(tw, "coded MB/s\t%s\n", formatFloat(r.CodedMegabytesPerSec, 2))
	}
	tw.Flush()

	fmt.Fprintf(&b, "\ngovpx latency   p50=%s  p95=%s  p99=%s  (decoded=%d/%d)\n",
		formatDuration(r.LatencyNS.P50), formatDuration(r.LatencyNS.P95), formatDuration(r.LatencyNS.P99),
		r.DecodedFrames, r.Frames)
	if r.Reference != nil {
		ref := r.Reference
		fmt.Fprintf(&b, "libvpx latency  p50=%s  p95=%s  p99=%s\n",
			formatDuration(ref.LatencyNS.P50), formatDuration(ref.LatencyNS.P95), formatDuration(ref.LatencyNS.P99))
		fmt.Fprintf(&b, "libvpx timing   source=%s  wall/frame=%s  subprocess=%s\n",
			ref.TimingSource, formatDuration(ref.WallNSPerFrame), formatDuration(ref.SubprocessOverheadNS))
	}
	if r.AllocsPerFrame > 0 {
		fmt.Fprintf(&b, "allocs/frame    %.2f\n", r.AllocsPerFrame)
	}
	return b.String()
}

func formatDuration(ns int64) string {
	switch {
	case ns <= 0:
		return "-"
	case ns < 1_000:
		return fmt.Sprintf("%d ns", ns)
	case ns < 1_000_000:
		return fmt.Sprintf("%.2f µs", float64(ns)/1_000)
	case ns < 1_000_000_000:
		return fmt.Sprintf("%.2f ms", float64(ns)/1_000_000)
	default:
		return fmt.Sprintf("%.2f s", float64(ns)/1_000_000_000)
	}
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.2f KiB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.2f GiB", float64(n)/(1024*1024*1024))
	}
}

func formatFloat(v float64, digits int) string {
	return strconv.FormatFloat(v, 'f', digits, 64)
}

func formatRatio(ratio float64, suffix string) string {
	if ratio <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f%s", ratio, suffix)
}

// buildComparisonReport derives govpx-vs-libvpx ratios and deltas from a
// completed govpx benchReport plus its libvpx referenceReport.
func buildComparisonReport(report benchReport, reference referenceReport) *comparisonReport {
	cmp := &comparisonReport{
		BitrateDeltaKbps:     report.OutputBitrateKbps - reference.OutputBitrateKbps,
		BitrateErrorPctDelta: report.BitrateErrorPct - reference.BitrateErrorPct,
		PSNRDeltaDB:          report.PSNR - reference.PSNR,
		SSIMDelta:            report.SSIM - reference.SSIM,
		EncodedFramesDelta:   report.EncodedFrames - reference.EncodedFrames,
		DroppedFramesDelta:   report.DroppedFrames - reference.DroppedFrames,
	}
	if reference.OutputBitrateKbps > 0 {
		cmp.BitrateRatioVsReference = report.OutputBitrateKbps / reference.OutputBitrateKbps
	}
	if reference.NSPerFrame > 0 {
		cmp.NSPerFrameRatio = float64(report.NSPerFrame) / float64(reference.NSPerFrame)
	}
	if reference.EncodeFPS > 0 {
		cmp.EncodeFPSRatio = report.EncodeFPS / reference.EncodeFPS
	}
	if reference.OutputBytes > 0 {
		cmp.OutputBytesRatio = float64(report.OutputBytes) / float64(reference.OutputBytes)
	}
	if reference.AvgInterBytes > 0 {
		cmp.AvgInterBytesRatio = report.AvgInterBytes / reference.AvgInterBytes
	}
	if reference.KeyframeBytes > 0 {
		cmp.KeyframeBytesRatio = float64(report.KeyframeBytes) / float64(reference.KeyframeBytes)
	}
	return cmp
}

// buildDecodeComparisonReport derives govpx-vs-libvpx decode ratios and
// decoded-frame deltas from a completed govpx decodeBenchReport plus its
// libvpx decodeReferenceReport.
func buildDecodeComparisonReport(report decodeBenchReport, reference decodeReferenceReport) *decodeComparisonReport {
	cmp := &decodeComparisonReport{
		DecodedFramesDelta: report.DecodedFrames - reference.DecodedFrames,
	}
	if reference.NSPerFrame > 0 {
		cmp.NSPerFrameRatio = float64(report.NSPerFrame) / float64(reference.NSPerFrame)
	}
	if reference.DecodeFPS > 0 {
		cmp.DecodeFPSRatio = report.DecodeFPS / reference.DecodeFPS
	}
	if reference.CodedMegabytesPerSec > 0 {
		cmp.CodedMegabytesPerSecRatio = report.CodedMegabytesPerSec / reference.CodedMegabytesPerSec
	}
	return cmp
}
