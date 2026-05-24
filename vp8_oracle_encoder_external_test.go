//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8corpus"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8OracleExternalEncoderTestDataValidation(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run external encoder source tests")
	}
	root, ok := vp8corpus.SourceRoot(t)
	if !ok {
		return
	}
	oracle := vp8test.NewChecksumOracle(t)
	vpxenc := vp8test.Vpxenc(t)
	paths := vp8corpus.FindSources(t, root)
	if len(paths) == 0 {
		t.Fatalf("no encoder source files found under %s", root)
	}
	vp8corpus.AssertSourceMinimum(t, paths)

	maxFrames := vp8corpus.SourceFrameLimit(t)
	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			clip, ok := vp8corpus.ReadSourceClip(t, path, maxFrames)
			if !ok {
				t.Skipf("%s is not a supported 8-bit 4:2:0 source clip", path)
			}
			frames := vp8SourceClipImages(clip)
			targetKbps := vp8corpus.SourceTargetKbps(clip.Width, clip.Height, clip.FPS)
			tc := encoderValidationCase{
				name:       clip.Name,
				width:      clip.Width,
				height:     clip.Height,
				frames:     len(frames),
				fps:        clip.FPS,
				targetKbps: targetKbps,
				opts: encoderValidationOptions(clip.Width, clip.Height, clip.FPS, targetKbps, func(opts *EncoderOptions) {
					opts.KeyFrameInterval = 120
				}),
				minPSNR:          20.0,
				minSSIM:          0.75,
				minFramePSNR:     18.0,
				minFrameSSIM:     0.65,
				maxPSNRGap:       2.0,
				maxSSIMGap:       0.01,
				maxFramePSNRGap:  4.0,
				maxFrameSSIMGap:  0.02,
				maxRateHigh:      250.0,
				maxRateLow:       100.0,
				checkInterFrames: len(frames) > 1,
			}

			got := encodeGopvxValidationCorpus(t, tc, frames)
			gotChecksums := decodeIVFChecksums(t, got.ivf)
			wantChecksums := oracle.Frames(t, got.ivf)
			assertFrameChecksumsEqual(t, "external govpx encode decoded by libvpx", gotChecksums, wantChecksums)
			assertGopvxEncoderValidationFeatures(t, got.ivf, tc)
			assertEncoderValidationQuality(t, "external govpx", got.quality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "external govpx", got.outputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)

			libvpxIVF := encodeLibvpxValidationCorpus(t, vpxenc, tc, frames)
			libvpxGotChecksums := decodeIVFChecksums(t, libvpxIVF)
			libvpxWantChecksums := oracle.Frames(t, libvpxIVF)
			assertFrameChecksumsEqual(t, "external libvpx encode decoded by govpx", libvpxGotChecksums, libvpxWantChecksums)
			libvpxQuality := qualityMetricsForIVF(t, libvpxIVF, frames)
			libvpxOutputKbps := encoderValidationOutputKbps(len(libvpxIVF)-testutil.IVFFileHeaderSize-len(frames)*testutil.IVFFrameHeaderSize, tc.fps, len(frames))
			logEncoderValidationQuality(t, got.quality, got.outputKbps, libvpxQuality, libvpxOutputKbps)
			assertEncoderValidationQuality(t, "external libvpx", libvpxQuality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "external libvpx", libvpxOutputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQualityGap(t, got.quality, libvpxQuality, tc)
		})
	}
}
