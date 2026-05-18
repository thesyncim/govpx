package benchcmd

import (
	"fmt"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// TestVP9FeatureBDRateDiagnostics is an exploratory diagnostic that
// prints BD-rate numbers per feature toggle on synthetic content.
// It is skipped under `-short` because each measurement takes ~30s.
// The assertion tests in this package pin the tolerance bands;
// this test is informational only and exists so a reviewer can
// re-derive the numbers without instrumenting the gate tests.
func TestVP9FeatureBDRateDiagnostics(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BD-rate diagnostic under -short")
	}
	if !FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	t.Parallel()
	type scenario struct {
		name     string
		content  FeatureGateContent
		width    int
		height   int
		frames   int
		qladder  []int
		rladder  []int
		hookA    func(*govpx.VP9EncoderOptions)
		hookB    func(*govpx.VP9EncoderOptions)
		lookhd   int
		fallback bool
	}
	scenarios := []scenario{
		{
			name:    "AltRef on vs off (panning)",
			content: PanningContent,
			width:   64, height: 64,
			frames:   12,
			qladder:  []int{16, 24, 32, 40},
			lookhd:   8,
			fallback: true,
			hookA: func(o *govpx.VP9EncoderOptions) {
				// baseline: AltRef OFF, no ARNR
				o.AutoAltRef = false
				o.LookaheadFrames = 0
			},
			hookB: func(o *govpx.VP9EncoderOptions) {
				o.AutoAltRef = true
				o.ARNRMaxFrames = 0
			},
		},
		{
			name:    "ARNR on vs off (texture+noise)",
			content: TextureNoise,
			width:   64, height: 64,
			frames:   12,
			qladder:  []int{16, 24, 32, 40},
			rladder:  []int{80, 160, 320, 640},
			lookhd:   8,
			fallback: true,
			hookA: func(o *govpx.VP9EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				o.CpuUsed = 4
				o.RateControlModeSet = true
				o.RateControlMode = govpx.RateControlVBR
				o.AutoAltRef = true
				o.ARNRMaxFrames = 0
			},
			hookB: func(o *govpx.VP9EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				o.CpuUsed = 4
				o.RateControlModeSet = true
				o.RateControlMode = govpx.RateControlVBR
				o.AutoAltRef = true
				o.ARNRMaxFrames = 5
				o.ARNRStrength = 3
				o.ARNRType = 3
			},
		},
		{
			name:    "TPL on vs off (sharp edges)",
			content: SharpEdgesContent,
			width:   64, height: 64,
			frames:   12,
			qladder:  []int{16, 24, 32, 40},
			lookhd:   8,
			fallback: true,
			hookA: func(o *govpx.VP9EncoderOptions) {
				o.AutoAltRef = true
				o.EnableTPL = false
			},
			hookB: func(o *govpx.VP9EncoderOptions) {
				o.AutoAltRef = true
				o.EnableTPL = true
			},
		},
		{
			name:    "VarianceAQ on vs off",
			content: VarianceHeavyContent,
			width:   64, height: 64,
			frames:  8,
			qladder: []int{16, 24, 32, 40},
			lookhd:  0,
			hookA: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQNone
			},
			hookB: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQVariance
			},
		},
		{
			name:    "Equator360 AQ on vs off",
			content: PanningContent,
			width:   64, height: 64,
			frames:  8,
			qladder: []int{16, 24, 32, 40},
			lookhd:  0,
			hookA: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQNone
			},
			hookB: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQEquator360
			},
		},
		{
			name:    "Perceptual AQ on vs off",
			content: PerceptualContent,
			width:   64, height: 64,
			frames:  8,
			qladder: []int{16, 24, 32, 40},
			lookhd:  0,
			hookA: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQNone
			},
			hookB: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQPerceptual
			},
		},
		{
			name:    "AltRefAQ on vs off (panning)",
			content: PanningContent,
			width:   64, height: 64,
			frames:   12,
			qladder:  []int{16, 24, 32, 40},
			lookhd:   8,
			fallback: true,
			hookA: func(o *govpx.VP9EncoderOptions) {
				o.AutoAltRef = true
				o.AltRefAQ = false
			},
			hookB: func(o *govpx.VP9EncoderOptions) {
				o.AutoAltRef = true
				o.AltRefAQ = true
			},
		},
	}
	rows := make([][3]string, 0, len(scenarios)+1)
	rows = append(rows, [3]string{"scenario", "BD-rate %", "BD-PSNR dB"})
	for _, sc := range scenarios {
		gen := FeatureGateGenerator(sc.content, sc.width, sc.height)
		res, err := ComputeBDRate(t, BDRateOptions{
			Codec:                "vp9",
			Width:                sc.width,
			Height:               sc.height,
			FPS:                  30,
			Frames:               sc.frames,
			Source:               func(i int) *image.YCbCr { return gen(i) },
			QLadder:              sc.qladder,
			RateLadderKbps:       sc.rladder,
			Lookahead:            sc.lookhd,
			Baseline:             sc.hookA,
			Test:                 sc.hookB,
			AllowDecoderFallback: sc.fallback,
		})
		if err != nil {
			t.Logf("%s: err %v", sc.name, err)
			rows = append(rows, [3]string{sc.name, "ERR", err.Error()})
			continue
		}
		rows = append(rows, [3]string{
			sc.name,
			f64ToStr(res.BDRate),
			f64ToStr(res.BDPSNR),
		})
		t.Logf("%s: BD-rate=%.3f%% BD-PSNR=%.3f dB ref=%v test=%v",
			sc.name, res.BDRate, res.BDPSNR, res.Reference, res.Govpx)
	}
	t.Logf("BD-rate diagnostics table:\n%s", FormatFeatureGateNumbers(rows))
}

func f64ToStr(v float64) string {
	return fmt.Sprintf("%.3f", v)
}
