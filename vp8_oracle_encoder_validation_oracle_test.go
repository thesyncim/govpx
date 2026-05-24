//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestVP8OracleEncoderCorpusValidation(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle validation")
	}
	oracle := vp8test.NewChecksumOracle(t)
	vpxenc := vp8test.Vpxenc(t)

	cases := []encoderValidationCase{
		{
			name:       "motion-eight-token-partitions",
			width:      64,
			height:     128,
			frames:     18,
			fps:        30,
			targetKbps: 700,
			pattern:    encoderValidationMotion,
			opts: encoderValidationOptions(64, 128, 30, 700, func(opts *EncoderOptions) {
				opts.TokenPartitions = int(vp8common.EightPartition)
			}),
			libvpxArgs: []string{"--token-parts=3"},
			// Realtime mode follows the cheaper libvpx pick_inter path; the
			// remaining gap is mostly mode-loop and rate-control parity.
			minPSNR:                       48.0,
			minSSIM:                       0.999,
			minFramePSNR:                  48.25,
			minFrameSSIM:                  0.999,
			maxPSNRGap:                    0.8,
			maxSSIMGap:                    0.001,
			maxFramePSNRGap:               1.5,
			maxFrameSSIMGap:               0.002,
			maxRateHigh:                   250.0,
			maxRateLow:                    95.0,
			maxRateGapPct:                 35.0,
			wantTokenPartition:            vp8common.EightPartition,
			checkTokenPartition:           true,
			checkAllTokenPartitionsActive: true,
			checkBPredModes:               true,
			checkInterFrames:              true,
		},
		{
			name:       "static-segmentation",
			width:      64,
			height:     64,
			frames:     18,
			fps:        30,
			targetKbps: 500,
			pattern:    encoderValidationSegmented,
			opts: encoderValidationOptions(64, 64, 30, 500, func(opts *EncoderOptions) {
				opts.StaticThreshold = 1
				opts.MaxQuantizer = 56
			}),
			libvpxArgs: []string{"--static-thresh=1"},
			// Static-threshold encode-breakout is bounded by the libvpx oracle,
			// while full mode RD and rate parity remain open.
			minPSNR:                 49.0,
			minSSIM:                 0.999,
			minFramePSNR:            48.75,
			minFrameSSIM:            0.999,
			maxPSNRGap:              0.5,
			maxSSIMGap:              0.001,
			maxFramePSNRGap:         0.55,
			maxFrameSSIMGap:         0.002,
			maxRateHigh:             250.0,
			maxRateLow:              95.0,
			maxRateGapPct:           5.0,
			checkSegmentationHeader: true,
			checkInterFrames:        true,
		},
		qualityValidationCase("best-quality-panning", DeadlineBestQuality, 0, 47.4, 47.1, 0.6, 0.7, 8.0),
		qualityValidationCase("good-quality-rd-panning", DeadlineGoodQuality, 3, 47.9, 47.6, 1.0, 1.2, 5.0),
		qualityValidationCase("good-quality-fast-panning", DeadlineGoodQuality, 4, 47.9, 47.6, 1.0, 1.2, 5.0),
		realtimeSpeedValidationCase(0, 47.9, 47.6, 0.8, 0.8, 12.0),
		realtimeSpeedValidationCase(3, 47.9, 47.6, 0.8, 0.8, 8.0),
		realtimeSpeedValidationCase(4, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(5, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(8, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(9, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(15, 47.9, 47.6, 0.4, 0.4, 5.0),
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := encoderValidationFrames(tc)
			got := encodeGopvxValidationCorpus(t, tc, sources)
			wantChecksums := oracle.Frames(t, got.ivf)
			gotChecksums := decodeIVFChecksums(t, got.ivf)
			assertFrameChecksumsEqual(t, "govpx encode decoded by libvpx", gotChecksums, wantChecksums)
			assertGopvxEncoderValidationFeatures(t, got.ivf, tc)

			libvpxIVF := encodeLibvpxValidationCorpus(t, vpxenc, tc, sources)
			libvpxWantChecksums := oracle.Frames(t, libvpxIVF)
			libvpxGotChecksums := decodeIVFChecksums(t, libvpxIVF)
			assertFrameChecksumsEqual(t, "libvpx encode decoded by govpx", libvpxGotChecksums, libvpxWantChecksums)
			libvpxQuality := qualityMetricsForIVF(t, libvpxIVF, sources)
			libvpxOutputKbps := encoderValidationOutputKbps(len(libvpxIVF)-testutil.IVFFileHeaderSize-len(sources)*testutil.IVFFrameHeaderSize, tc.fps, len(sources))
			logEncoderValidationQuality(t, got.quality, got.outputKbps, libvpxQuality, libvpxOutputKbps)

			assertEncoderValidationQuality(t, "govpx", got.quality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "govpx", got.outputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQuality(t, "libvpx", libvpxQuality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "libvpx", libvpxOutputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQualityGap(t, got.quality, libvpxQuality, tc)
			assertEncoderValidationRateGap(t, got.outputKbps, libvpxOutputKbps, tc)
		})
	}
}
