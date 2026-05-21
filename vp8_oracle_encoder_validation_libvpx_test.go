//go:build govpx_oracle_trace

package govpx

import (
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

func encodeLibvpxValidationCorpus(t *testing.T, vpxenc string, tc encoderValidationCase, sources []Image) []byte {
	t.Helper()
	extraArgs := []string{
		"--end-usage=cbr",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
	}
	extraArgs = append(extraArgs, tc.libvpxArgs...)
	cfg := coracle.VpxencVP8Config{
		BinaryPath:        vpxenc,
		Width:             tc.width,
		Height:            tc.height,
		Frames:            len(sources),
		Deadline:          libvpxValidationDeadline(tc.opts.Deadline),
		CPUUsed:           tc.opts.CpuUsed,
		LagInFrames:       0,
		AutoAltRef:        false,
		TargetBitrateKbps: tc.targetKbps,
		MinQ:              4,
		MaxQ:              56,
		FPS:               strconv.Itoa(tc.fps) + "/1",
		KeyFrameDistSet:   true,
		KeyFrameMinDist:   999,
		KeyFrameMaxDist:   999,
		ExtraArgs:         extraArgs,
	}
	ivf, diag, err := coracle.VpxencVP8EncodeI420(encoderValidationI420Bytes(t, sources), cfg)
	if err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, diag)
	}
	return ivf
}

func libvpxValidationDeadline(deadline Deadline) string {
	switch deadline {
	case DeadlineBestQuality:
		return "best"
	case DeadlineRealtime:
		return "rt"
	default:
		return "good"
	}
}
