//go:build goexperiment.simd && arm64 && !purego

package dsp

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

var lfValuesAxisNativeSink uint64

// Actual public pixel axis entry points used by decoder loopfilter.go, with
// independent original scalar DSP macro+three internal edge oracle.
func BenchmarkLFMacroAxisNative(b *testing.B) {
	if !pixel.Enabled || !pixel.NativeLFByAxis {
		b.Fatal("native ARM axis required")
	}
	for _, vertical := range []bool{false, true} {
		for _, pitch := range []int{32, 1312} {
			for _, pattern := range []string{"flat", "ramp", "smooth", "mixed", "noop", "macroOnly"} {
				b.Run(fmt.Sprintf("vertical%v/pitch%d/%s", vertical, pitch, pattern), func(b *testing.B) {
					rows, width, step := 20, 16, pitch
					if vertical {
						rows, width, step = 16, 20, 1
					}
					span := (rows-1)*pitch + width
					const align = 7
					original := bytes.Repeat([]byte{0xa7}, align+span+17)
					for y := range rows {
						for x := range width {
							v := byte(112 + (x*3+y*5)%17)
							if pattern == "macroOnly" {
								v = 120
								if (vertical && x < 4) || (!vertical && y < 4) {
									v = 112
								}
							}
							if pattern == "flat" {
								v = 128
							}
							if pattern == "ramp" {
								v = byte(32 + x + y)
							}
							if pattern == "noop" || (pattern == "mixed" && x%7 == 0) {
								v = byte((y*32+x)*37 + 11)
							}
							original[align+y*pitch+x] = v
						}
					}
					got, want := bytes.Clone(original), bytes.Clone(original)
					view := got[align : align+span : align+span]
					const mb, bl, lim, thr = 64, 33, 32, 8
					if vertical {
						mbLoopFilterVerticalEdgeScalar(want[align:], pitch, mb, lim, thr, 2)
					} else {
						mbLoopFilterHorizontalEdgeScalar(want[align:], pitch, mb, lim, thr, 2)
					}
					for edge := 1; edge <= 3; edge++ {
						q := want[align+4*edge*step:]
						if vertical {
							loopFilterVerticalEdgeScalar(q, pitch, bl, lim, thr, 2)
						} else {
							loopFilterHorizontalEdgeScalar(q, pitch, bl, lim, thr, 2)
						}
					}
					if vertical {
						if !pixel.CanLoopFilterMacroVertical16(view, pitch, mb, bl) {
							b.Fatal("eligible vertical declined")
						}
						pixel.LoopFilterMacroVertical16Unchecked(view, pitch, mb, bl, lim, thr)
					} else {
						if !pixel.CanLoopFilterMacroHorizontal16(view, pitch, mb, bl) {
							b.Fatal("eligible horizontal declined")
						}
						pixel.LoopFilterMacroHorizontal16Unchecked(view, pitch, mb, bl, lim, thr)
					}
					if !bytes.Equal(got, want) {
						b.Fatal("pre-timing scalar/full-backing mismatch")
					}
					b.ReportAllocs()
					for b.Loop() {
						for y := range rows {
							copy(view[y*pitch:y*pitch+width], original[align+y*pitch:align+y*pitch+width])
						}
						if vertical {
							if !pixel.CanLoopFilterMacroVertical16(view, pitch, mb, bl) {
								b.Fatal("vertical declined")
							}
							pixel.LoopFilterMacroVertical16Unchecked(view, pitch, mb, bl, lim, thr)
						} else {
							if !pixel.CanLoopFilterMacroHorizontal16(view, pitch, mb, bl) {
								b.Fatal("horizontal declined")
							}
							pixel.LoopFilterMacroHorizontal16Unchecked(view, pitch, mb, bl, lim, thr)
						}
					}
					if !bytes.Equal(got, want) {
						b.Fatal("post-timing scalar/full-backing mismatch")
					}
					var sum uint64
					for _, v := range got {
						sum = sum*131 + uint64(v)
					}
					lfValuesAxisNativeSink = sum
				})
			}
		}
	}
}
