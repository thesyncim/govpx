//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestMacroInternalFusionOracle(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	seed := uint32(0x73101)
	next := func() byte { seed = seed*1664525 + 1013904223; return byte(seed >> 24) }
	for _, vertical := range []bool{false} {
		for _, extra := range []int{0, 1, 7, 31, 61} {
			step, outer := 16+extra, 1
			if vertical {
				step, outer = 1, 20+extra
			}
			for align := range 8 {
				for trial := range 18 {
					p := make([]byte, align+19*step+15*outer+1+13)
					for i := range p {
						p[i] = next()
						if trial%3 == 0 {
							p[i] = 120 + p[i]%9
						}
					}
					for _, limits := range [][4]int{{0, 0, 0, 0}, {1, 1, 1, 0}, {32, 17, 9, 0}, {64, 33, 32, 8}, {254, 254, 255, 255}} {
						got, want := bytes.Clone(p), bytes.Clone(p)
						mb, bl, lim, thr := limits[0], limits[1], limits[2], limits[3]
						if !LoopFilterMacroInternal16(got[align:len(got)-13], step, outer, mb, bl, lim, thr) {
							t.Fatal("eligible fusion declined")
						}
						for _, edge := range []int{4, 8, 12, 16} {
							kind, b := 4, bl
							if edge == 4 {
								kind, b = 6, mb
							}
							for lane := 0; lane < 16; lane += 8 {
								loopFilterNarrow8(want, align+edge*step+lane*outer, step, outer, b, lim, thr, kind)
							}
						}
						if !bytes.Equal(got, want) {
							t.Fatalf("vertical%v extra%d align%d trial%d limits%v", vertical, extra, align, trial, limits)
						}
					}
				}
			}
		}
	}
	for _, shape := range [][2]int{{0, 1}, {1, 0}, {1, 19}, {15, 1}, {int(^uint(0) >> 1), 1}, {1, int(^uint(0) >> 1)}} {
		p := bytes.Repeat([]byte{123}, 512)
		before := bytes.Clone(p)
		if LoopFilterMacroInternal16(p, shape[0], shape[1], 64, 32, 9, 0) {
			t.Fatal("invalid geometry accepted")
		}
		if !bytes.Equal(p, before) {
			t.Fatal("declined path wrote")
		}
	}
}

// A declined fast path must preserve the entire allocation so callers can
// safely retry the ordinary edge implementation.
func TestMacroInternalDeclinesWithoutStores(t *testing.T) {
	const maxInt = int(^uint(0) >> 1)
	for _, shape := range [][2]int{{16, 1}, {17, 1}, {1, 20}, {1, 21}, {maxInt, 1}, {1, maxInt}, {-1, 1}, {1, -1}} {
		step, outer := shape[0], shape[1]
		for _, thresholds := range [][4]int{{-1, 32, 9, 0}, {255, 32, 9, 0}, {32, -1, 9, 0}, {32, 255, 9, 0}, {32, 32, -1, 0}, {32, 32, 256, 0}, {32, 32, 9, -1}, {32, 32, 9, 256}, {32, 32, 9, 0}} {
			for _, size := range []int{0, 1, 318} {
				p := bytes.Repeat([]byte{127}, size+32)
				before := bytes.Clone(p)
				if LoopFilterMacroInternal16(p[16:16+size], step, outer, thresholds[0], thresholds[1], thresholds[2], thresholds[3]) {
					t.Fatalf("accepted shape=%v thresholds=%v size=%d", shape, thresholds, size)
				}
				if !bytes.Equal(p, before) {
					t.Fatal("declined path changed its allocation")
				}
			}
		}
	}
}
