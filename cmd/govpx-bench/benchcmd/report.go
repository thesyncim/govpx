package benchcmd

import (
	"bytes"
	"fmt"
	"strconv"
	"text/tabwriter"

	govpx "github.com/thesyncim/govpx"
)

func formatEncodeReport(r benchReport) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "govpx-bench  encode  %s  %dx%d @%dfps  target=%d kbps  frames=%d\n\n",
		r.Mode, r.Width, r.Height, r.FPS, r.TargetBitrateKbps, r.Frames)

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
		if ref.QualityError != "" {
			fmt.Fprintf(&b, "libvpx quality  warn: %s\n", ref.QualityError)
		}
	}
	if r.AllocsPerFrame > 0 {
		fmt.Fprintf(&b, "allocs/frame    %.2f\n", r.AllocsPerFrame)
	}
	return b.String()
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
}

func formatDecodeReport(r decodeBenchReport) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "govpx-bench  decode  %s  %dx%d @%dfps  frames=%d  input=%s\n\n",
		r.Mode, r.Width, r.Height, r.FPS, r.Frames, formatBytes(int64(r.InputBytes)))

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	if r.Reference != nil {
		ref := r.Reference
		fmt.Fprintln(tw, "metric\tgovpx\tlibvpx\trelative")
		fmt.Fprintln(tw, "------\t-----\t------\t--------")
		fmt.Fprintf(tw, "ns/frame\t%s\t%s\t%s\n",
			formatDuration(r.NSPerFrame), formatDuration(ref.NSPerFrame), formatRatio(r.RelativeSpeedVsReference, "x faster"))
		fmt.Fprintf(tw, "decode fps\t%s\t%s\t-\n",
			formatFloat(r.DecodeFPS, 1), formatFloat(ref.DecodeFPS, 1))
		fmt.Fprintf(tw, "MB/s (mblocks)\t%s\t%s\t-\n",
			formatFloat(r.MacroblocksPerSec/1e6, 2), formatFloat(ref.MacroblocksPerSec/1e6, 2))
		fmt.Fprintf(tw, "coded MB/s\t%s\t%s\t-\n",
			formatFloat(r.CodedMegabytesPerSec, 2), formatFloat(ref.CodedMegabytesPerSec, 2))
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
