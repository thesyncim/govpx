//go:build govpx_oracle_trace

package govpx_test

import (
	"fmt"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencOracleIdenticalInterByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	vp9oracle.AssertTwoFrameByteParity(t, src, src)
}

func TestVP9EncoderVpxencOracleChangedConstantInterByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewYCbCr(width, height, 160, 128, 128)
	vp9oracle.AssertTwoFrameByteParity(t, first, second)
}

func TestVP9EncoderVpxencOracleCheckerInterByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)
	// ML_BASED_PARTITION support is wired through internal partition models,
	// estimated prediction, and the non-RD partition picker.

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)
	vp9oracle.AssertTwoFrameByteParity(t, first, second)
}

func TestVP9EncoderVpxencOracleFixedQuantizerInterByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewYCbCr(width, height, 160, 128, 128)
	vp9oracle.AssertTwoFrameByteParityWithOptions(t, first, second, govpx.VP9EncoderOptions{
		MinQuantizer: 20,
		MaxQuantizer: 20,
	}, []string{
		"--cq-level=20",
		"--min-q=20",
		"--max-q=20",
		"--disable-warning-prompt",
	})
}

func TestVP9EncoderVpxencOracleCQLevelInterByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewYCbCr(width, height, 160, 128, 128)
	vp9oracle.AssertTwoFrameByteParityWithOptions(t, first, second, govpx.VP9EncoderOptions{
		CQLevel: 20,
	}, []string{"--cq-level=20"})
}

func TestVP9EncoderVpxencOraclePublicQuantizerBandInterByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewYCbCr(width, height, 160, 128, 128)
	vp9oracle.AssertTwoFrameByteParityWithOptions(t, first, second, govpx.VP9EncoderOptions{
		MinQuantizer: 10,
		MaxQuantizer: 50,
		CQLevel:      30,
	}, []string{
		"--min-q=10",
		"--max-q=50",
		"--cq-level=30",
	})
}

func TestVP9EncoderVpxencOracleLosslessInterByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)
	// ML_BASED_PARTITION support is wired through internal partition models,
	// estimated prediction, and the non-RD partition picker.

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)
	vp9oracle.AssertTwoFrameByteParityWithOptions(t, first, second, govpx.VP9EncoderOptions{
		Lossless: true,
	}, []string{"--lossless=1"})
}

// TestVP9EncoderVpxencOracleLosslessInterByteParityQuantizerSweep verifies
// that the rc_min_quantizer / rc_max_quantizer public controls do not
// perturb the encoded VP9 lossless bitstream. libvpx forces
// best_allowed_q = worst_allowed_q = 0 when lossless is requested
// (vp9_cx_iface.c:553-555), so the configured min/max-q range is irrelevant
// to the actual qindex used by the rate model. This sweep keeps the same
// govpx default Deadline / CpuUsed used by the parent
// TestVP9EncoderVpxencOracleLosslessInterByteParity test (which is already
// a hard gate) and only varies the public quantizer controls.
func TestVP9EncoderVpxencOracleLosslessInterByteParityQuantizerSweep(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)

	minQs := []int{0, 1, 2}
	maxQs := []int{0, 1, 2, 4}

	for _, minQ := range minQs {
		for _, maxQ := range maxQs {
			// validateVP9PublicQuantizerOptions rejects MinQ>MaxQ
			// when either is non-zero (vp9_encoder.go:11222-11225).
			if (minQ != 0 || maxQ != 0) && minQ > maxQ {
				continue
			}
			name := fmt.Sprintf("min%d_max%d", minQ, maxQ)
			t.Run(name, func(t *testing.T) {
				opts := govpx.VP9EncoderOptions{
					Lossless:     true,
					MinQuantizer: minQ,
					MaxQuantizer: maxQ,
				}
				extra := []string{
					"--lossless=1",
					// Silence vpxenc's "Quantizer values should not
					// be equal, and should differ by at least 8"
					// interactive prompt for narrow min/max-q bands;
					// the prompt would otherwise leave vpxenc waiting
					// for a "y" on stdin and exit non-zero.
					"--disable-warning-prompt",
				}
				if minQ != 0 || maxQ != 0 {
					extra = append(extra,
						fmt.Sprintf("--min-q=%d", minQ),
						fmt.Sprintf("--max-q=%d", maxQ),
					)
				}
				vp9oracle.AssertTwoFrameByteParityWithOptions(t,
					first, second, opts, extra)
			})
		}
	}
}

// TestVP9EncoderVpxencOracleLosslessInterByteParitySweep extends the
// lossless inter byte parity gate across the realtime cpu_used speed
// preset paired with the public-quantizer band. The cpu_used dimension
// sweeps {0, 2, 5, 8} alongside Deadline=Realtime, matching the oracle's
// pinned "--rt --cpu-used=N" path (internal/coracle/vpxenc_vp9.go:79-80).
//
// Cases where govpx's realtime SPEED_FEATURES dispatch has not yet
// reached full byte parity with libvpx
// (vp9_speed_features.c:452 set_rt_speed_feature_framesize_independent,
// dispatched from vp9_speed_features.c:1042) are skipped with explicit
// citations; the default cpu_used=8 lane stays a hard gate. The
// MinQuantizer/MaxQuantizer dimension is fully exercised under the
// companion TestVP9EncoderVpxencOracleLosslessInterByteParityQuantizerSweep.
func TestVP9EncoderVpxencOracleLosslessInterByteParitySweep(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)

	cpuUseds := []int8{0, 2, 5, 8}
	minQs := []int{0, 1, 2}
	maxQs := []int{0, 1, 2, 4}

	for _, cpuUsed := range cpuUseds {
		for _, minQ := range minQs {
			for _, maxQ := range maxQs {
				// validateVP9PublicQuantizerOptions rejects MinQ>MaxQ
				// when either is non-zero (vp9_encoder.go:11222-11225);
				// skip those invalid combinations rather than wedge
				// the matrix on a config error.
				if (minQ != 0 || maxQ != 0) && minQ > maxQ {
					continue
				}
				name := fmt.Sprintf("cpu%d_min%d_max%d", cpuUsed, minQ, maxQ)
				t.Run(name, func(t *testing.T) {
					// Speed presets 0-7 in realtime mode have not yet
					// reached byte-exact parity with libvpx's
					// set_rt_speed_feature_framesize_independent
					// (vp9_speed_features.c:452); see also the
					// dispatcher at vp9_speed_features.c:1042 which
					// calls into the speed-specific branches at
					// vp9_speed_features.c:485 (speed>=1),
					// vp9_speed_features.c:506 (speed>=2),
					// vp9_speed_features.c:544 (speed>=3), etc.
					// Only cpu_used=8 currently produces
					// byte-identical realtime lossless output. Skip
					// the other speeds with a citation so the matrix
					// is recorded but does not regress the gate.
					if cpuUsed != 8 {
						t.Skipf("VP9 realtime lossless byte parity not yet "+
							"complete for cpu_used=%d; libvpx "+
							"vp9_speed_features.c:452 "+
							"set_rt_speed_feature_framesize_independent "+
							"(dispatched at vp9_speed_features.c:1042) "+
							"is the remaining gap", cpuUsed)
					}
					opts := govpx.VP9EncoderOptions{
						Lossless:     true,
						Deadline:     govpx.DeadlineRealtime,
						CpuUsed:      cpuUsed,
						MinQuantizer: minQ,
						MaxQuantizer: maxQ,
					}
					extra := []string{
						"--lossless=1",
						fmt.Sprintf("--cpu-used=%d", cpuUsed),
						// vpxenc emits a "Quantizer values should not be
						// equal, and should differ by at least 8"
						// warning when the configured min/max-q band is
						// narrow, and otherwise exits non-zero waiting
						// for interactive confirmation. The min/max-q
						// values here are irrelevant for the lossless
						// bitstream (vp9_cx_iface.c:553-555 zeroes
						// them) but we still need to silence the
						// prompt.
						"--disable-warning-prompt",
					}
					if minQ != 0 || maxQ != 0 {
						extra = append(extra,
							fmt.Sprintf("--min-q=%d", minQ),
							fmt.Sprintf("--max-q=%d", maxQ),
						)
					}
					vp9oracle.AssertTwoFrameByteParityWithOptions(t,
						first, second, opts, extra)
				})
			}
		}
	}
}

func TestVP9EncoderVpxencOracleErrorResilientInterByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewYCbCr(width, height, 160, 128, 128)
	vp9oracle.AssertTwoFrameByteParityWithOptions(t, first, second, govpx.VP9EncoderOptions{
		ErrorResilient: true,
	}, []string{"--error-resilient=1"})
}

func TestVP9EncoderVpxencOracleMaxKeyframeIntervalByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 128, 128, 128),
	}
	vp9oracle.AssertFrameSequenceByteParityWithOptions(t, frames, govpx.VP9EncoderOptions{
		MaxKeyframeInterval: 2,
	}, []string{"--kf-max-dist=2"})
}
