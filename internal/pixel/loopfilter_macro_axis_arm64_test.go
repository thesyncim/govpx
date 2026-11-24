//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestMacroAxisCompleteWindows(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		for _, pitch := range []int{16, 17, 19, 20, 21, 32, 1312} {
			step, outer := pitch, 1
			if vertical {
				step, outer = 1, pitch
			}
			span := 19*step + 15*outer + 1
			for align := range 8 {
				for _, cut := range []int{-1, 0, 1} {
					for _, clips := range []bool{false, true} {
						for _, thresholds := range [][4]byte{{0, 0, 0, 0}, {64, 32, 9, 0}, {254, 254, 255, 255}, {255, 32, 9, 0}, {64, 255, 9, 0}} {
							n := span + cut
							got := bytes.Repeat([]byte{0xa7}, align+n+23)
							for i := range n {
								got[align+i] = byte(112 + (i*7+i/17)%23)
							}
							want := bytes.Clone(got)
							gs, ws := got[align:align+n], want[align:align+n]
							if clips {
								gs, ws = gs[:n:n], ws[:n:n]
							}
							mb, bl, lim, thr := thresholds[0], thresholds[1], thresholds[2], thresholds[3]
							ref := LoopFilterMacroInternal16(ws, step, outer, int(mb), int(bl), int(lim), int(thr))
							var ok bool
							if vertical {
								ok = CanLoopFilterMacroVertical16(gs, pitch, mb, bl)
								if ok {
									LoopFilterMacroVertical16Unchecked(gs, pitch, mb, bl, lim, thr)
								}
							} else {
								ok = CanLoopFilterMacroHorizontal16(gs, pitch, mb, bl)
								if ok {
									LoopFilterMacroHorizontal16Unchecked(gs, pitch, mb, bl, lim, thr)
								}
							}
							if ref != ok || !bytes.Equal(got, want) {
								t.Fatalf("vertical%v pitch%d align%d cut%d clipped%v thresholds%v", vertical, pitch, align, cut, clips, thresholds)
							}
						}
					}
				}
			}
		}
	}
}
func TestMacroAxisInvalidNoWrites(t *testing.T) {
	for _, pitch := range []int{-1, 0, 1, 15, 19, 1<<30 + 32, int(^uint(0) >> 1), -int(^uint(0)>>1) - 1} {
		for _, vertical := range []bool{false, true} {
			p := bytes.Repeat([]byte{123}, 600)
			before := bytes.Clone(p)
			var ok bool
			if vertical {
				ok = CanLoopFilterMacroVertical16(p[:1:1], pitch, 64, 32)
				if ok {
					LoopFilterMacroVertical16Unchecked(p[:1:1], pitch, 64, 32, 9, 0)
				}
			} else {
				ok = CanLoopFilterMacroHorizontal16(p[:1:1], pitch, 64, 32)
				if ok {
					LoopFilterMacroHorizontal16Unchecked(p[:1:1], pitch, 64, 32, 9, 0)
				}
			}
			if ok || !bytes.Equal(p, before) {
				t.Fatalf("invalid pitch%d vertical%v wrote", pitch, vertical)
			}
		}
	}
}
