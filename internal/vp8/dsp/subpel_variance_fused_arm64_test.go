//go:build arm64 && !purego

package dsp

import "testing"

func TestSubpelVariance16x16FusedARM64WindowGuards(t *testing.T) {
	const (
		srcStride = 32
		refStride = 16
	)
	ref := make([]byte, 16*refStride)
	srcHorizontal := make([]byte, 16*srcStride)
	srcVertical := make([]byte, 16*srcStride+16)
	srcBilinear := make([]byte, 17*srcStride)

	valid := []struct {
		name string
		fn   func() bool
	}{
		{
			name: "horizontal",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Horizontal(srcHorizontal, srcStride, 3, ref, refStride)
				return ok
			},
		},
		{
			name: "vertical",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Vertical(srcVertical, srcStride, 5, ref, refStride)
				return ok
			},
		},
		{
			name: "bilinear",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Bilinear(srcBilinear, srcStride, 3, 5, ref, refStride)
				return ok
			},
		},
	}
	for _, tc := range valid {
		if !tc.fn() {
			t.Fatalf("%s: valid fused arm64 window was rejected", tc.name)
		}
	}

	short := []struct {
		name string
		fn   func() bool
	}{
		{
			name: "horizontal-short-src",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Horizontal(srcHorizontal[:len(srcHorizontal)-1], srcStride, 3, ref, refStride)
				return ok
			},
		},
		{
			name: "horizontal-short-ref",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Horizontal(srcHorizontal, srcStride, 3, ref[:len(ref)-1], refStride)
				return ok
			},
		},
		{
			name: "vertical-short-src",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Vertical(srcVertical[:len(srcVertical)-1], srcStride, 5, ref, refStride)
				return ok
			},
		},
		{
			name: "vertical-short-ref",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Vertical(srcVertical, srcStride, 5, ref[:len(ref)-1], refStride)
				return ok
			},
		},
		{
			name: "bilinear-short-src",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Bilinear(srcBilinear[:len(srcBilinear)-1], srcStride, 3, 5, ref, refStride)
				return ok
			},
		},
		{
			name: "bilinear-short-ref",
			fn: func() bool {
				_, _, ok := subpelVariance16x16Bilinear(srcBilinear, srcStride, 3, 5, ref[:len(ref)-1], refStride)
				return ok
			},
		},
	}
	for _, tc := range short {
		if tc.fn() {
			t.Fatalf("%s: short fused arm64 window was accepted", tc.name)
		}
	}
}
