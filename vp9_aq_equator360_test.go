package govpx

import (
	"errors"
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9Equator360AQSegmentID(t *testing.T) {
	cases := []struct {
		miRow, miRows int
		want          uint8
	}{
		// Polar (top 1/8 and bottom 1/8): segment 2.
		{0, 64, 2},
		{7, 64, 2},
		{63, 64, 2},
		{57, 64, 2},
		// Temperate (1/8..1/4 from edges): segment 1.
		{8, 64, 1},
		{15, 64, 1},
		{49, 64, 1},
		{56, 64, 1},
		// Equator (middle 1/2, inclusive of 16..48): segment 0.
		{16, 64, 0},
		{32, 64, 0},
		{47, 64, 0},
		{48, 64, 0},
		// Degenerate.
		{0, 0, 0},
		{-1, 64, 2},
	}
	for _, tc := range cases {
		if got := vp9Equator360AQSegmentID(tc.miRow, tc.miRows); got != tc.want {
			t.Fatalf("vp9Equator360AQSegmentID(%d,%d) = %d, want %d",
				tc.miRow, tc.miRows, got, tc.want)
		}
	}
}

func TestVP9Equator360AQSegmentationParamsEmitsDeltasOnIntra(t *testing.T) {
	const baseQ = 96
	seg := vp9Equator360AQSegmentationParams(baseQ, true)
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Fatalf("seg flags = enabled:%t updateMap:%t updateData:%t, want all true",
			seg.Enabled, seg.UpdateMap, seg.UpdateData)
	}
	// Segments where the ratio is 1.0 should keep the AltQ mask clear; non-1.0
	// ratios should populate it.
	for i, ratio := range vp9Equator360AQRateRatios {
		hasAltQ := seg.FeatureMask[i]&(1<<uint(vp9dec.SegLvlAltQ)) != 0
		if ratio.num == ratio.den {
			if hasAltQ {
				t.Fatalf("segment %d unit ratio has unexpected AltQ", i)
			}
			continue
		}
		if !hasAltQ {
			t.Fatalf("segment %d non-unit ratio missing AltQ mask", i)
		}
	}
}

func TestVP9Equator360AQSegmentationParamsSkipsDataUpdateOnInter(t *testing.T) {
	seg := vp9Equator360AQSegmentationParams(96, false)
	if !seg.Enabled || !seg.UpdateMap {
		t.Fatalf("seg flags = enabled:%t updateMap:%t, want both true",
			seg.Enabled, seg.UpdateMap)
	}
	if seg.UpdateData {
		t.Fatalf("inter frame must inherit segment data; got UpdateData=true")
	}
}

func TestVP9EncoderEquator360AQValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*VP9EncoderOptions)
		err  error
	}{
		{"lossless", func(o *VP9EncoderOptions) {
			o.Lossless = true
		}, ErrInvalidConfig},
		{"static segmentation", func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
		}, ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := VP9EncoderOptions{
				Width:  64,
				Height: 64,
				FPS:    30,
				AQMode: VP9AQEquator360,
			}
			tc.mut(&opts)
			if _, err := NewVP9Encoder(opts); !errors.Is(err, tc.err) {
				t.Fatalf("NewVP9Encoder err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestVP9EncoderEquator360AQAcceptsConfiguration(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		AQMode: VP9AQEquator360,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.opts.AQMode != VP9AQEquator360 {
		t.Fatalf("opts.AQMode = %d, want %d", e.opts.AQMode, VP9AQEquator360)
	}
	dst := make([]byte, 65536)
	src := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	if _, err := e.EncodeInto(src, dst); err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
}
