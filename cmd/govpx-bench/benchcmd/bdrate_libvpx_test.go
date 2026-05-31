package benchcmd

import (
	"errors"
	"math"
	"strings"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// TestLibvpxVP9FrameFlagsCLIArgsMapping exercises the govpx ->
// vpxenc-vp9-frameflags CLI translation. It does not invoke the
// helper binary; it only asserts the argument list contains the
// expected per-field tokens given a representative test callback.
//
// The mapping must keep parity with the `// libvpx token:` comments
// inside libvpxVP9FrameFlagsCLIArgs — a regression here means a
// govpx feature toggle silently dropped its libvpx flag and the
// absolute-reference assertion will then compare govpx-with-feature
// against libvpx-without-feature.
func TestLibvpxVP9FrameFlagsCLIArgsMapping(t *testing.T) {
	opts := BDRateOptions{Width: 64, Height: 64, FPS: 30, Frames: 8, Lookahead: 8}
	cases := []struct {
		name   string
		apply  func(*govpx.VP9EncoderOptions)
		wants  []string
		absent []string
	}{
		{
			name: "AltRef on, ARNR off, AQ none",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.AutoAltRef = true
				o.LookaheadFrames = 8
			},
			wants: []string{
				"--auto-alt-ref=1", "--lag-in-frames=8", "--aq-mode=0",
				"--arnr-maxframes=0", "--arnr-strength=0",
			},
			absent: []string{"--alt-ref-aq=1"},
		},
		{
			name: "ARNR full",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				o.CpuUsed = 4
				o.AutoAltRef = true
				o.LookaheadFrames = 8
				o.ARNRMaxFrames = 5
				o.ARNRStrength = 3
				o.ARNRType = 3
			},
			wants: []string{
				"--deadline=rt", "--cpu-used=4", "--auto-alt-ref=1", "--arnr-maxframes=5",
				"--arnr-strength=3", "--arnr-type=3",
			},
		},
		{
			name: "Variance AQ",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQVariance
			},
			wants: []string{"--aq-mode=1"},
		},
		{
			name: "PerceptualAQ + AltRefAQ + FramePeriodicBoost",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQPerceptual
				o.AltRefAQ = true
				o.FramePeriodicBoost = true
			},
			wants: []string{"--aq-mode=5", "--alt-ref-aq=1", "--frame-boost=1"},
		},
		{
			name: "Cyclic refresh CBR",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				o.CpuUsed = 8
				o.RateControlModeSet = true
				o.RateControlMode = govpx.RateControlCBR
				o.TargetBitrateKbps = 320
				o.AQMode = govpx.VP9AQCyclicRefresh
			},
			wants: []string{
				"--deadline=rt", "--cpu-used=8",
				"--end-usage=cbr", "--target-bitrate=320", "--aq-mode=3",
			},
			absent: []string{"--cq-level="},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eo := govpx.VP9EncoderOptions{}
			tc.apply(&eo)
			args := libvpxVP9FrameFlagsCLIArgs(opts, eo, 32)
			line := strings.Join(args, " ")
			for _, w := range tc.wants {
				if !strings.Contains(line, w) {
					t.Errorf("missing %q in args: %s", w, line)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(line, a) {
					t.Errorf("unexpected %q in args: %s", a, line)
				}
			}
			if strings.Contains(line, "--end-usage=q") || strings.Contains(line, "--end-usage=cq") {
				if !strings.Contains(line, "--cq-level=32") {
					t.Errorf("missing --cq-level=32: %s", line)
				}
			} else if strings.Contains(line, "--cq-level=") {
				t.Errorf("unexpected --cq-level for non-Q mode: %s", line)
			}
			if !strings.Contains(line, "--end-usage=") {
				t.Errorf("missing --end-usage: %s", line)
			}
		})
	}
}

// TestFormatBDRateObservationsRendering pins the column layout of the
// BD-rate observation table so a maintainer can rely on its grep-ability
// in CI logs.
func TestFormatBDRateObservationsRendering(t *testing.T) {
	rows := []LibvpxBDRateObservation{
		{
			Case:                   "AltRef",
			GovpxBDRatePct:         -3.6,
			LibvpxBDRatePct:        math.NaN(),
			GovpxVsLibvpxBDRatePct: 4.2,
			GovpxVsLibvpxBDPSNRdB:  -0.3,
		},
		{
			Case:                   "ARNR",
			GovpxBDRatePct:         -1.5,
			LibvpxBDRatePct:        math.NaN(),
			GovpxVsLibvpxBDRatePct: math.NaN(),
			GovpxVsLibvpxBDPSNRdB:  math.NaN(),
			LibvpxErr:              errors.New("binary not built"),
		},
	}
	out := FormatBDRateObservations(rows)
	for _, want := range []string{
		"Case", "govpx BD-rate", "libvpx BD-rate", "govpx-vs-libvpx",
		"AltRef", "-3.600%", "+4.200%", "-0.300 dB",
		"ARNR", "binary not built",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
