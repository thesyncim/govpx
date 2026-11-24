package govpx

import (
	"testing"
)

var frame04framePRSink int

func frame04frameRefskinPixel(y int, cb int, cr int, motion int) bool {
	if y < skinYLow || y > skinYHigh {
		return false
	}
	if cb == 128 && cr == 128 {
		return false
	}
	if cb > 150 && cr < 110 {
		return false
	}
	for i := range len(skinMean) {
		diff := frame04frameRefevaluateSkinColorDifference(cb, cr, i)
		threshold := skinThreshold[i+1]
		if diff < threshold {
			if y < 60 && diff > 3*(threshold>>2) {
				return false
			}
			if motion == 0 && diff > threshold>>1 {
				return false
			}
			return true
		}
		if diff > threshold<<3 {
			return false
		}
	}
	return false
}

func frame04frameRefevaluateSkinColorDifference(cb int, cr int, index int) int {
	cbQ6 := cb << 6
	crQ6 := cr << 6
	cbDelta := cbQ6 - skinMean[index][0]
	crDelta := crQ6 - skinMean[index][1]
	cbDiffQ12 := cbDelta * cbDelta
	cbcrDiffQ12 := cbDelta * crDelta
	crDiffQ12 := crDelta * crDelta
	cbDiffQ2 := (cbDiffQ12 + (1 << 9)) >> 10
	cbcrDiffQ2 := (cbcrDiffQ12 + (1 << 9)) >> 10
	crDiffQ2 := (crDiffQ12 + (1 << 9)) >> 10
	return skinInvCov[0]*cbDiffQ2 + skinInvCov[1]*cbcrDiffQ2 + skinInvCov[2]*cbcrDiffQ2 + skinInvCov[3]*crDiffQ2
}

func TestFramePR04(t *testing.T) {
	for cb := range 256 {
		for cr := range 256 {
			for _, y := range []int{39, 40, 59, 60, 220, 221} {
				for _, motion := range []int{0, 1, -1} {
					if skinPixel(y, cb, cr, motion) != frame04frameRefskinPixel(y, cb, cr, motion) {
						t.Fatalf("skin y=%d cb=%d cr=%d m=%d", y, cb, cr, motion)
					}
				}
			}
		}
	}
	for _, cb := range []int{-10000, -1, 256, 10000} {
		for _, cr := range []int{-10000, -1, 256, 10000} {
			for _, y := range []int{40, 59, 60, 220} {
				if skinPixel(y, cb, cr, 1) != frame04frameRefskinPixel(y, cb, cr, 1) {
					t.Fatal("nonbyte")
				}
			}
		}
	}
}

func BenchmarkFramePR04(b *testing.B) {
	for _, kind := range []string{"skin", "mixed", "gray"} {
		b.Run(kind, func(b *testing.B) {
			i, sum := 0, 0
			b.ReportAllocs()
			for b.Loop() {
				u, v := 100, 150
				if kind == "mixed" {
					u = (i * 37) & 255
					v = (i * 61) & 255
				}
				if kind == "gray" {
					u = 128
					v = 128
				}
				if skinPixel(100+(i&31), u, v, i&1) {
					sum++
				}
				i++
			}
			frame04framePRSink = sum
		})
	}
}
