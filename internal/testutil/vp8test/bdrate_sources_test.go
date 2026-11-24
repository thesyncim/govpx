package vp8test

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestBDRateSourcesAreDeterministicAndTextured(t *testing.T) {
	const (
		width  = 64
		height = 48
		frameA = 3
		frameB = 5
	)
	tests := []struct {
		name string
		new  func(int, int, int) *image.YCbCr
	}{
		{"textured_noise", NewBDRateTexturedNoiseYCbCr},
		{"sports_motion", NewSportsMotionYCbCr},
		{"static_then_motion", NewStaticThenMotionYCbCr},
		{"mixed_motion", NewMixedMotionYCbCr},
		{"bpred_edge_grid", NewBPredEdgeGridYCbCr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := tt.new(width, height, frameA)
			second := tt.new(width, height, frameA)
			if first.Rect.Dx() != width || first.Rect.Dy() != height {
				t.Fatalf("dimensions = %dx%d, want %dx%d",
					first.Rect.Dx(), first.Rect.Dy(), width, height)
			}
			if !testutil.PlaneEqual(first.Y, first.YStride,
				second.Y, second.YStride, width, height) {
				t.Fatal("same frame produced different luma samples")
			}
			if lumaIsFlat(first, width, height) {
				t.Fatal("luma collapsed to a flat field")
			}

			later := tt.new(width, height, frameB)
			if testutil.PlaneEqual(first.Y, first.YStride,
				later.Y, later.YStride, width, height) {
				t.Fatal("different frame indexes produced identical luma")
			}
			if first.Cb[0] == 0 || first.Cr[0] == 0 {
				t.Fatal("chroma was not initialized")
			}
		})
	}
}

func lumaIsFlat(img *image.YCbCr, width, height int) bool {
	if width == 0 || height == 0 {
		return true
	}
	first := img.Y[0]
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			if row[x] != first {
				return false
			}
		}
	}
	return true
}
