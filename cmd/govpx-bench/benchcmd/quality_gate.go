package benchcmd

import (
	"flag"
	"fmt"
	"strings"
)

// QualityGate captures absolute and relative PSNR/SSIM regression
// thresholds applied to a completed bench run. When Enabled is true the
// CLI exits non-zero on any violation so CI can block perceptual
// regressions even when byte-parity tests pass.
//
// Default values are tuned to the canonical synthetic fixtures shipped
// with the bench (panning-360p / checker-360p) and govpx VP9's observed
// realtime-CBR operating point on this branch:
//   - panning-360p-2m-60f reaches ~23 dB PSNR / 0.73 SSIM
//   - checker-360p-600k-120f reaches ~32 dB PSNR / 0.95 SSIM
//
// We set the floors well below the easier (checker) fixture and just
// below the harder (panning) fixture so the gate catches a catastrophic
// govpx regression -- a quantizer / DC misconfig that drops PSNR several
// dB -- while leaving headroom for run-to-run variance:
//   - MinPSNR=20 dB: catches "encoded but unrecognizable" output. The
//     observed govpx PSNR on the panning fixture is 23 dB, so this leaves
//     ~3 dB of headroom.
//   - MinSSIM=0.70: similar headroom on the SSIM axis (observed 0.73).
//   - MaxPSNRBehindLibvpx=2.0 dB: govpx and libvpx VP9 typically track to
//     within ~1 dB on these fixtures at realtime CBR. A 2 dB gap is
//     outside expected variance.
//   - MaxSSIMBehindLibvpx=0.03: similar headroom on the SSIM gap.
type QualityGate struct {
	Enabled bool

	MinPSNR             float64
	MinSSIM             float64
	MaxPSNRBehindLibvpx float64
	MaxSSIMBehindLibvpx float64
}

// defaultQualityGate returns the project's recommended thresholds. The
// values are documented on the QualityGate type.
func defaultQualityGate() QualityGate {
	return QualityGate{
		MinPSNR:             20.0,
		MinSSIM:             0.70,
		MaxPSNRBehindLibvpx: 2.0,
		MaxSSIMBehindLibvpx: 0.03,
	}
}

func registerQualityGateFlags(fs *flag.FlagSet, gate *QualityGate) {
	def := defaultQualityGate()
	gate.MinPSNR = def.MinPSNR
	gate.MinSSIM = def.MinSSIM
	gate.MaxPSNRBehindLibvpx = def.MaxPSNRBehindLibvpx
	gate.MaxSSIMBehindLibvpx = def.MaxSSIMBehindLibvpx

	fs.BoolVar(&gate.Enabled, "quality-gate", false, "fail the bench run when PSNR/SSIM gates are violated (see -quality-min-psnr etc.)")
	fs.Float64Var(&gate.MinPSNR, "quality-min-psnr", gate.MinPSNR, "absolute govpx PSNR floor in dB applied by -quality-gate")
	fs.Float64Var(&gate.MinSSIM, "quality-min-ssim", gate.MinSSIM, "absolute govpx SSIM floor applied by -quality-gate")
	fs.Float64Var(&gate.MaxPSNRBehindLibvpx, "quality-max-psnr-gap", gate.MaxPSNRBehindLibvpx, "max libvpx_psnr-govpx_psnr (dB) tolerated by -quality-gate")
	fs.Float64Var(&gate.MaxSSIMBehindLibvpx, "quality-max-ssim-gap", gate.MaxSSIMBehindLibvpx, "max libvpx_ssim-govpx_ssim tolerated by -quality-gate")
}

// QualityGateViolation describes a single threshold breach. Numeric fields
// hold both the observed measurement and the configured limit so the bench
// CLI's gate output can read like "PSNR 23.4 dB < min 25 dB" without the
// caller re-pulling values from the report.
type QualityGateViolation struct {
	Metric    string
	Observed  float64
	Threshold float64
	Limit     string // "min", "max-gap"
}

func (v QualityGateViolation) String() string {
	switch v.Limit {
	case "max-gap":
		return fmt.Sprintf("%s %.4f exceeds max %.4f", v.Metric, v.Observed, v.Threshold)
	default:
		return fmt.Sprintf("%s %.4f below floor %.4f", v.Metric, v.Observed, v.Threshold)
	}
}

// Evaluate applies the gate against an encode-mode bench report and
// returns every violation found. Reports with quality measurement
// skipped (cfg.SkipQuality) are exempt because there is no signal to
// gate on.
func (g QualityGate) Evaluate(report benchReport) []QualityGateViolation {
	if !g.Enabled || report.QualitySkipped {
		return nil
	}
	var violations []QualityGateViolation
	if g.MinPSNR > 0 && report.PSNR < g.MinPSNR {
		violations = append(violations, QualityGateViolation{
			Metric:    "PSNR (dB)",
			Observed:  report.PSNR,
			Threshold: g.MinPSNR,
			Limit:     "min",
		})
	}
	if g.MinSSIM > 0 && report.SSIM < g.MinSSIM {
		violations = append(violations, QualityGateViolation{
			Metric:    "SSIM",
			Observed:  report.SSIM,
			Threshold: g.MinSSIM,
			Limit:     "min",
		})
	}
	if report.Reference != nil && !report.Reference.QualitySkipped {
		psnrGap := report.Reference.PSNR - report.PSNR
		if g.MaxPSNRBehindLibvpx > 0 && psnrGap > g.MaxPSNRBehindLibvpx {
			violations = append(violations, QualityGateViolation{
				Metric:    "PSNR (dB) gap vs libvpx",
				Observed:  psnrGap,
				Threshold: g.MaxPSNRBehindLibvpx,
				Limit:     "max-gap",
			})
		}
		ssimGap := report.Reference.SSIM - report.SSIM
		if g.MaxSSIMBehindLibvpx > 0 && ssimGap > g.MaxSSIMBehindLibvpx {
			violations = append(violations, QualityGateViolation{
				Metric:    "SSIM gap vs libvpx",
				Observed:  ssimGap,
				Threshold: g.MaxSSIMBehindLibvpx,
				Limit:     "max-gap",
			})
		}
	}
	return violations
}

// formatQualityGateViolations renders a human-readable multi-line
// description of the violations for stderr output.
func formatQualityGateViolations(name string, violations []QualityGateViolation) string {
	var b strings.Builder
	if name != "" {
		fmt.Fprintf(&b, "quality gate FAILED for %s:\n", name)
	} else {
		fmt.Fprintln(&b, "quality gate FAILED:")
	}
	for _, v := range violations {
		fmt.Fprintf(&b, "  - %s\n", v.String())
	}
	return b.String()
}
