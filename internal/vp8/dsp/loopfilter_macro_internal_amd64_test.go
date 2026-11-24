//go:build goexperiment.simd && amd64 && !purego

package dsp

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
)

// Compare the fused public kernel with the independent scalar libvpx v1.16.0
// port, including every byte outside the exact accepted window.
func TestMacroInternalMatchesScalarEdges(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("native SIMD unavailable")
	}
	seed := uint32(0x92713)
	next := func() byte { seed = seed*1664525 + 1013904223; return byte(seed >> 24) }
	changed := 0
	for _, vertical := range []bool{false, true} {
		for _, extra := range []int{0, 1, 31} {
			step, outer := 16+extra, 1
			if vertical {
				step, outer = 1, 20+extra
			}
			stride := max(step, outer)
			for align := range 8 {
				for trial := range 32 {
					original := make([]byte, align+19*step+15*outer+1+13)
					for i := range original {
						original[i] = next()
						if trial%2 == 0 {
							original[i] = byte(trial*8) + original[i]%9
						}
					}
					for _, limits := range [][4]byte{{0, 0, 0, 0}, {1, 1, 1, 0}, {32, 17, 9, 0}, {64, 33, 32, 8}, {128, 96, 64, 1}, {254, 254, 255, 255}} {
						got, want := bytes.Clone(original), bytes.Clone(original)
						mb, bl, lim, thr := limits[0], limits[1], limits[2], limits[3]
						if !pixel.LoopFilterMacroInternal16(got[align:len(got)-13], step, outer, int(mb), int(bl), int(lim), int(thr)) {
							t.Fatal("eligible fusion declined")
						}
						if vertical {
							mbLoopFilterVerticalEdgeScalar(want[align:], stride, mb, lim, thr, 2)
						} else {
							mbLoopFilterHorizontalEdgeScalar(want[align:], stride, mb, lim, thr, 2)
						}
						for edge := 1; edge <= 3; edge++ {
							p := want[align+4*edge*step:]
							if vertical {
								loopFilterVerticalEdgeScalar(p, stride, bl, lim, thr, 2)
							} else {
								loopFilterHorizontalEdgeScalar(p, stride, bl, lim, thr, 2)
							}
						}
						if !bytes.Equal(got, want) {
							t.Fatalf("vertical=%v pitch=%d align=%d trial=%d limits=%v", vertical, stride, align, trial, limits)
						}
						if !bytes.Equal(got, original) {
							changed++
						}
					}
				}
			}
		}
	}
	if changed == 0 {
		t.Fatal("oracle fixtures did not exercise any active filter")
	}
}
