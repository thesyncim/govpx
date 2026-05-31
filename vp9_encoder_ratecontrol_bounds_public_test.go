package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
)

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
