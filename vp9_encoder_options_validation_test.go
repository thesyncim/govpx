package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderAcceptsMinimalOptions(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 320, Height: 240})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}
	if got := e.Codec(); got != govpx.CodecVP9 {
		t.Fatalf("Codec = %v, want CodecVP9", got)
	}
}

func TestVP9EncoderRejectsInvalidOptions(t *testing.T) {
	base := govpx.VP9EncoderOptions{Width: 320, Height: 240}
	tests := []struct {
		name string
		edit func(*govpx.VP9EncoderOptions)
		want error
	}{
		{name: "zero width", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Width = 0
		}, want: govpx.ErrInvalidConfig},
		{name: "zero height", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Height = 0
		}, want: govpx.ErrInvalidConfig},
		{name: "negative width", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Width = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "width above vp9 limit", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Width = 65537
		}, want: govpx.ErrInvalidConfig},
		{name: "height above vp9 limit", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Height = 65537
		}, want: govpx.ErrInvalidConfig},
		{name: "negative threads", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Threads = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "row mt without worker threads", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.RowMT = true
		}, want: govpx.ErrInvalidConfig},
		{name: "row mt with one worker", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.RowMT = true
			opts.Threads = 1
		}, want: govpx.ErrInvalidConfig},
		{name: "negative tile rows", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Log2TileRows = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "too many tile rows", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Log2TileRows = 3
		}, want: govpx.ErrInvalidConfig},
		{name: "tile rows exceed height", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Height = 64
			opts.Log2TileRows = 1
		}, want: govpx.ErrInvalidConfig},
		{name: "invalid deadline", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Deadline = govpx.Deadline(-1)
		}, want: govpx.ErrInvalidConfig},
		{name: "cpu below range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.CpuUsed = -10
		}, want: govpx.ErrInvalidConfig},
		{name: "cpu above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.CpuUsed = 10
		}, want: govpx.ErrInvalidConfig},
		{name: "negative bitrate", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.TargetBitrateKbps = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "negative fixed quantizer", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Quantizer = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "fixed quantizer above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Quantizer = 256
		}, want: govpx.ErrInvalidQuantizer},
		{name: "min quantizer below range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.MinQuantizer = -1
		}, want: govpx.ErrInvalidQuantizer},
		{name: "max quantizer above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.MaxQuantizer = 64
		}, want: govpx.ErrInvalidQuantizer},
		{name: "cq level above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.CQLevel = 64
		}, want: govpx.ErrInvalidQuantizer},
		{name: "min quantizer above max", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.MinQuantizer = 40
			opts.MaxQuantizer = 20
		}, want: govpx.ErrInvalidQuantizer},
		{name: "cq level below min", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.MinQuantizer = 20
			opts.MaxQuantizer = 40
			opts.CQLevel = 10
		}, want: govpx.ErrInvalidQuantizer},
		{name: "fixed quantizer conflicts with cq level", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Quantizer = 64
			opts.CQLevel = 32
		}, want: govpx.ErrInvalidQuantizer},
		{name: "temporal layering missing bitrate", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.TemporalScalability = govpx.TemporalScalabilityConfig{
				Enabled: true,
				Mode:    govpx.TemporalLayeringTwoLayers,
			}
		}, want: govpx.ErrInvalidBitrate},
		{name: "unknown temporal layering mode", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.TargetBitrateKbps = 300
			opts.TemporalScalability = govpx.TemporalScalabilityConfig{
				Enabled: true,
				Mode:    govpx.TemporalLayeringMode(99),
			}
		}, want: govpx.ErrInvalidConfig},
		{name: "spatial svc missing layer count", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.SpatialScalability = govpx.VP9SpatialScalabilityConfig{Enabled: true}
		}, want: govpx.ErrInvalidConfig},
		{name: "spatial svc too many layers", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.SpatialScalability = govpx.VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: govpx.VP9MaxSpatialLayers + 1,
			}
		}, want: govpx.ErrInvalidConfig},
		{name: "spatial svc layer id out of range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.SpatialScalability = govpx.VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: 2,
				LayerID:    2,
			}
		}, want: govpx.ErrInvalidConfig},
		{name: "spatial svc inter-layer dependency", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.SpatialScalability = govpx.VP9SpatialScalabilityConfig{
				Enabled:              true,
				LayerCount:           2,
				InterLayerDependency: true,
			}
		}, want: govpx.ErrInvalidConfig},
		{name: "spatial svc missing resolution flag", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.SpatialScalability = govpx.VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: 2,
				LayerID:    1,
				Width:      [govpx.VP9RTPMaxSpatialLayers]uint16{32, 64},
				Height:     [govpx.VP9RTPMaxSpatialLayers]uint16{32, 64},
			}
		}, want: govpx.ErrInvalidConfig},
		{name: "spatial svc non-increasing resolution", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.SpatialScalability = govpx.VP9SpatialScalabilityConfig{
				Enabled:           true,
				LayerCount:        2,
				LayerID:           1,
				ResolutionPresent: true,
				Width:             [govpx.VP9RTPMaxSpatialLayers]uint16{32, 32},
				Height:            [govpx.VP9RTPMaxSpatialLayers]uint16{32, 32},
			}
		}, want: govpx.ErrInvalidConfig},
		{name: "lossless with nonzero quantizer", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Lossless = true
			opts.Quantizer = 1
		}, want: govpx.ErrInvalidQuantizer},
		{name: "lossless with alt q segmentation", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Lossless = true
			opts.Segmentation.Enabled = true
			opts.Segmentation.AltQEnabled[0] = true
			opts.Segmentation.AltQ[0] = 1
		}, want: govpx.ErrInvalidQuantizer},
		{name: "lossless with alt loop-filter segmentation", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Lossless = true
			opts.Segmentation.Enabled = true
			opts.Segmentation.AltLFEnabled[0] = true
			opts.Segmentation.AltLF[0] = 1
		}, want: govpx.ErrInvalidConfig},
		{name: "negative min keyframe interval", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.MinKeyframeInterval = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "negative max keyframe interval", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.MaxKeyframeInterval = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "min keyframe interval above max", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.MinKeyframeInterval = 3
			opts.MaxKeyframeInterval = 2
		}, want: govpx.ErrInvalidConfig},
		{name: "min keyframe interval too high", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.MinKeyframeInterval = 129
		}, want: govpx.ErrInvalidConfig},
		{name: "negative lookahead", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.LookaheadFrames = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "lookahead above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.LookaheadFrames = 26
		}, want: govpx.ErrInvalidConfig},
		{name: "negative arnr max frames", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.ARNRMaxFrames = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "arnr max frames above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.ARNRMaxFrames = 16
		}, want: govpx.ErrInvalidConfig},
		{name: "negative arnr strength", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.ARNRStrength = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "arnr strength above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.ARNRStrength = 7
		}, want: govpx.ErrInvalidConfig},
		{name: "negative arnr type", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.ARNRType = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "arnr type above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.ARNRType = 4
		}, want: govpx.ErrInvalidConfig},
		{name: "negative screen content mode", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.ScreenContentMode = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "screen content mode above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.ScreenContentMode = 3
		}, want: govpx.ErrInvalidConfig},
		{name: "negative noise sensitivity", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.NoiseSensitivity = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "noise sensitivity above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.NoiseSensitivity = 7
		}, want: govpx.ErrInvalidConfig},
		{name: "sharpness above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Sharpness = 8
		}, want: govpx.ErrInvalidConfig},
		{name: "negative static threshold", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.StaticThreshold = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "auto alt-ref without lookahead", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AutoAltRef = true
		}, want: govpx.ErrInvalidConfig},
		{name: "auto alt-ref with too little lookahead", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AutoAltRef = true
			opts.LookaheadFrames = 1
		}, want: govpx.ErrInvalidConfig},
		{name: "auto alt-ref with error resilient", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AutoAltRef = true
			opts.LookaheadFrames = 2
			opts.ErrorResilient = true
		}, want: govpx.ErrInvalidConfig},
		{name: "unknown aq mode", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQMode(99)
		}, want: govpx.ErrInvalidConfig},
		{name: "complexity aq without vbr", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQComplexity
		}, want: govpx.ErrInvalidConfig},
		{name: "complexity aq with lossless", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQComplexity
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlVBR
			opts.TargetBitrateKbps = 300
			opts.Lossless = true
		}, want: govpx.ErrInvalidConfig},
		{name: "complexity aq with segmentation", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQComplexity
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlVBR
			opts.TargetBitrateKbps = 300
			opts.Segmentation.Enabled = true
		}, want: govpx.ErrInvalidConfig},
		{name: "cyclic aq without cbr", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQCyclicRefresh
		}, want: govpx.ErrInvalidConfig},
		{name: "cyclic aq with vbr", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQCyclicRefresh
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlVBR
			opts.TargetBitrateKbps = 300
		}, want: govpx.ErrInvalidConfig},
		{name: "cyclic aq with lossless", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQCyclicRefresh
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlCBR
			opts.TargetBitrateKbps = 300
			opts.Lossless = true
		}, want: govpx.ErrInvalidConfig},
		{name: "cyclic aq with segmentation", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQCyclicRefresh
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlCBR
			opts.TargetBitrateKbps = 300
			opts.Segmentation.Enabled = true
		}, want: govpx.ErrInvalidConfig},
		{name: "variance aq with lossless", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQVariance
			opts.Lossless = true
		}, want: govpx.ErrInvalidConfig},
		{name: "variance aq with segmentation", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.AQMode = govpx.VP9AQVariance
			opts.Segmentation.Enabled = true
		}, want: govpx.ErrInvalidConfig},
		{name: "unknown rate control mode", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlMode(99)
			opts.TargetBitrateKbps = 300
		}, want: govpx.ErrInvalidConfig},
		{name: "vbr missing bitrate", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlVBR
		}, want: govpx.ErrInvalidBitrate},
		{name: "vbr with frame dropping", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlVBR
			opts.TargetBitrateKbps = 300
			opts.DropFrameAllowed = true
		}, want: govpx.ErrInvalidConfig},
		{name: "negative two-pass vbr bias", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.TwoPassVBRBiasPct = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "negative two-pass min pct", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.TwoPassMinPct = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "negative two-pass max pct", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.TwoPassMaxPct = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "lookahead with cyclic aq", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.LookaheadFrames = 2
			opts.AQMode = govpx.VP9AQCyclicRefresh
			opts.RateControlModeSet = true
			opts.RateControlMode = govpx.RateControlCBR
			opts.TargetBitrateKbps = 300
		}, want: govpx.ErrInvalidConfig},
		{name: "negative fps", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.FPS = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "timebase missing denominator", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.TimebaseNum = 1
		}, want: govpx.ErrInvalidConfig},
		{name: "timebase missing numerator", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.TimebaseDen = 1
		}, want: govpx.ErrInvalidConfig},
		{name: "segmentation id above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Segmentation.Enabled = true
			opts.Segmentation.SegmentID = govpx.VP9MaxSegments
		}, want: govpx.ErrInvalidConfig},
		{name: "segmentation id without feature", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Segmentation.Enabled = true
			opts.Segmentation.SegmentID = 1
		}, want: govpx.ErrInvalidConfig},
		{name: "segmentation alt q below range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Segmentation.Enabled = true
			opts.Segmentation.AltQEnabled[0] = true
			opts.Segmentation.AltQ[0] = -256
		}, want: govpx.ErrInvalidQuantizer},
		{name: "segmentation alt loop-filter above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Segmentation.Enabled = true
			opts.Segmentation.AltLFEnabled[0] = true
			opts.Segmentation.AltLF[0] = 64
		}, want: govpx.ErrInvalidConfig},
		{name: "segmentation ref below range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Segmentation.Enabled = true
			opts.Segmentation.RefFrameEnabled[0] = true
			opts.Segmentation.RefFrame[0] = govpx.VP9RefFrameIntra - 1
		}, want: govpx.ErrInvalidConfig},
		{name: "segmentation ref above range", edit: func(opts *govpx.VP9EncoderOptions) {
			opts.Segmentation.Enabled = true
			opts.Segmentation.RefFrameEnabled[0] = true
			opts.Segmentation.RefFrame[0] = vp9dec.AltrefFrame + 1
		}, want: govpx.ErrInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := base
			tt.edit(&opts)
			_, err := govpx.NewVP9Encoder(opts)
			if !errors.Is(err, tt.want) {
				t.Fatalf("NewVP9Encoder error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestVP9EncoderRejectsInvalidRateControlBounds(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*govpx.VP9EncoderOptions)
		err  error
	}{
		{"min>max", func(o *govpx.VP9EncoderOptions) {
			o.MinBitrateKbps = 1500
			o.MaxBitrateKbps = 800
		}, govpx.ErrInvalidBitrate},
		{"target<min", func(o *govpx.VP9EncoderOptions) {
			o.MinBitrateKbps = 2000
		}, govpx.ErrInvalidBitrate},
		{"target>max", func(o *govpx.VP9EncoderOptions) {
			o.MaxBitrateKbps = 200
		}, govpx.ErrInvalidBitrate},
		{"negative min", func(o *govpx.VP9EncoderOptions) {
			o.MinBitrateKbps = -1
		}, govpx.ErrInvalidBitrate},
		{"undershoot>100", func(o *govpx.VP9EncoderOptions) {
			o.UndershootPct = 200
		}, govpx.ErrInvalidConfig},
		{"overshoot>100", func(o *govpx.VP9EncoderOptions) {
			o.OvershootPct = 200
		}, govpx.ErrInvalidConfig},
		{"negative max-intra", func(o *govpx.VP9EncoderOptions) {
			o.MaxIntraBitratePct = -1
		}, govpx.ErrInvalidConfig},
		{"non-cbr gfboost", func(o *govpx.VP9EncoderOptions) {
			o.RateControlMode = govpx.RateControlVBR
			o.GFCBRBoostPct = 20
		}, govpx.ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := govpx.VP9EncoderOptions{
				Width:              64,
				Height:             64,
				FPS:                30,
				RateControlModeSet: true,
				RateControlMode:    govpx.RateControlCBR,
				TargetBitrateKbps:  1000,
			}
			tc.mut(&opts)
			if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, tc.err) {
				t.Fatalf("NewVP9Encoder err = %v, want %v", err, tc.err)
			}
		})
	}
}
