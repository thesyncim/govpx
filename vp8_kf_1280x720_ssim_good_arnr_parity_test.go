//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8KF1280x720SSIMGoodARNRParity pins a high-resolution VP8 option mix
// that exercises the GoodQuality keyframe picker, VBR rate control,
// screen-content tuning, four-thread encode setup, TuneSSIM, and a short ARNR
// filter. The first packet covers the keyframe path; the second packet covers
// the inter-frame path that consumes the keyframe reconstruction.
//
// The test intentionally checks packet sizes and first-partition sizes against
// a reproducible libvpx oracle run before logging short SHA prefixes. That
// keeps the assertion objective while leaving enough detail to diagnose a
// future parity drift.
func TestVP8KF1280x720SSIMGoodARNRParity(t *testing.T) {
	vp8test.RequireOracle(t, "the VP8 GoodQuality ARNR parity replay")
	vpxencOracle := vp8test.VpxencOracle(t)

	opts := EncoderOptions{
		Width:             1280,
		Height:            720,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
		Tuning:            TuneSSIM,
		ScreenContentMode: 1,
		TokenPartitions:   1,
		Threads:           4,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}
	extraArgs := libvpxEndUsageArgs([]string{
		"--end-usage=vbr",
		"--screen-content-mode=1",
		"--token-parts=1",
		"--threads=4",
		"--tune=ssim",
		"--arnr-maxframes=1",
		"--arnr-strength=1",
		"--arnr-type=2",
	})

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(1280, 720, i)
	}

	govpxFrames := encodeFramesWithGovpx(t, opts, sources)
	// The --threads=4 cohort is routed through the libvpx threading
	// quarantine wrapper so oracle-side non-determinism is caught as a SHA
	// mismatch before the byte comparison below.
	libvpxFrames := encodeVP8FramesWithLibvpxOracleReproducible(t, vpxencOracle, "kf-1280x720-ssim-good-arnr", opts, 700, sources, extraArgs, VP8OracleReproducibleRuns)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Pin both the total packet length and first-partition length for the
	// first two frames. These values are intentionally equal between govpx and
	// libvpx; a drift means this option mix is no longer byte-aligned at the
	// packet boundary.
	wantFrame0GovpxLen := 145534
	wantFrame0LibvpxLen := 145534
	wantFrame0GovpxFirstPart := 20463
	wantFrame0LibvpxFirstPart := 20463
	wantFrame1GovpxLen := 6134
	wantFrame1LibvpxLen := 6134
	wantFrame1GovpxFirstPart := 2169
	wantFrame1LibvpxFirstPart := 2169

	if got := len(govpxFrames[0]); got != wantFrame0GovpxLen {
		t.Fatalf("frame 0 govpx len drift: got=%d want=%d", got, wantFrame0GovpxLen)
	}
	if got := len(libvpxFrames[0]); got != wantFrame0LibvpxLen {
		t.Fatalf("frame 0 libvpx len drift: got=%d want=%d", got, wantFrame0LibvpxLen)
	}
	if got := len(govpxFrames[1]); got != wantFrame1GovpxLen {
		t.Fatalf("frame 1 govpx len drift: got=%d want=%d", got, wantFrame1GovpxLen)
	}
	if got := len(libvpxFrames[1]); got != wantFrame1LibvpxLen {
		t.Fatalf("frame 1 libvpx len drift: got=%d want=%d", got, wantFrame1LibvpxLen)
	}

	if got, _ := parseVP8FramePartitionSizes(govpxFrames[0]); got != wantFrame0GovpxFirstPart {
		t.Fatalf("frame 0 govpx first_partition_size drift: got=%d want=%d", got, wantFrame0GovpxFirstPart)
	}
	if got, _ := parseVP8FramePartitionSizes(libvpxFrames[0]); got != wantFrame0LibvpxFirstPart {
		t.Fatalf("frame 0 libvpx first_partition_size drift: got=%d want=%d", got, wantFrame0LibvpxFirstPart)
	}
	if got, _ := parseVP8FramePartitionSizes(govpxFrames[1]); got != wantFrame1GovpxFirstPart {
		t.Fatalf("frame 1 govpx first_partition_size drift: got=%d want=%d", got, wantFrame1GovpxFirstPart)
	}
	if got, _ := parseVP8FramePartitionSizes(libvpxFrames[1]); got != wantFrame1LibvpxFirstPart {
		t.Fatalf("frame 1 libvpx first_partition_size drift: got=%d want=%d", got, wantFrame1LibvpxFirstPart)
	}

	govpxSHA0 := sha256.Sum256(govpxFrames[0])
	libvpxSHA0 := sha256.Sum256(libvpxFrames[0])
	govpxSHA1 := sha256.Sum256(govpxFrames[1])
	libvpxSHA1 := sha256.Sum256(libvpxFrames[1])

	t.Logf("GoodQuality ARNR parity pin: frame 0 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame0GovpxLen, wantFrame0LibvpxLen,
		wantFrame0GovpxFirstPart, wantFrame0LibvpxFirstPart,
		hex.EncodeToString(govpxSHA0[:8]), hex.EncodeToString(libvpxSHA0[:8]))
	t.Logf("GoodQuality ARNR parity pin: frame 1 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame1GovpxLen, wantFrame1LibvpxLen,
		wantFrame1GovpxFirstPart, wantFrame1LibvpxFirstPart,
		hex.EncodeToString(govpxSHA1[:8]), hex.EncodeToString(libvpxSHA1[:8]))
}
