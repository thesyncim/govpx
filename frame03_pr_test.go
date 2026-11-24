package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

var frame03framePRSink int

func frame03framePRSource(kind int) vp8enc.SourceImage {
	const w, h = 64, 64
	s := vp8enc.SourceImage{Width: w, Height: h, YStride: w, UStride: w / 2, VStride: w / 2, Y: make([]byte, w*h), U: make([]byte, w*h/4), V: make([]byte, w*h/4)}
	for i := range s.Y {
		switch kind {
		case 0:
			s.Y[i] = 15
		case 1:
			s.Y[i] = 100
		default:
			s.Y[i] = byte(i*31 + i/13)
		}
	}
	for i := range s.U {
		s.U[i] = 100
		s.V[i] = 150
		if kind == 2 {
			s.U[i] = byte(i * 29)
			s.V[i] = byte(i * 47)
		}
	}
	return s
}

func frame03frameRefcomputeSkin8x8Block(src vp8enc.SourceImage, uvWidth int, uvHeight int, mbRow int, mbCol int, consecZeroLast int) bool {
	if consecZeroLast > 60 {
		return false
	}
	motion := 1
	if consecZeroLast > 25 {
		motion = 0
	}
	numSkin := 0
	for sb := range 4 {
		yRow := mbRow*16 + (sb>>1)*8 + 3
		yCol := mbCol*16 + (sb&1)*8 + 3
		uvRow := mbRow*8 + (sb>>1)*4 + 1
		uvCol := mbCol*8 + (sb&1)*4 + 1
		ySample := average2x2Clamped(src.Y, src.YStride, src.Width, src.Height, yRow, yCol)
		uSample := average2x2Clamped(src.U, src.UStride, uvWidth, uvHeight, uvRow, uvCol)
		vSample := average2x2Clamped(src.V, src.VStride, uvWidth, uvHeight, uvRow, uvCol)
		if frame03frameRefskinPixel(ySample, uSample, vSample, motion) {
			numSkin++
			if numSkin >= 2 {
				return true
			}
		}
	}
	return false
}

func frame03frameRefskinPixel(y int, cb int, cr int, motion int) bool {
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
		diff := frame03frameRefevaluateSkinColorDifference(cb, cr, i)
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

func frame03frameRefevaluateSkinColorDifference(cb int, cr int, index int) int {
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

func TestFramePR03(t *testing.T) {
	for kind := range 3 {
		s := frame03framePRSource(kind)
		for _, w := range []int{1, 17, 63, 64} {
			s.Width = w
			s.Height = w
			for r := range 4 {
				for c := range 4 {
					for _, n := range []int{0, 26, 60, 61} {
						got := computeSkin8x8Block(s, (w+1)/2, (w+1)/2, r, c, n)
						want := frame03frameRefcomputeSkin8x8Block(s, (w+1)/2, (w+1)/2, r, c, n)
						if got != want {
							t.Fatalf("skin vote k=%d w=%d r=%d c=%d n=%d", kind, w, r, c, n)
						}
					}
				}
			}
		}
	}
}

func BenchmarkFramePR03(b *testing.B) {
	for kind, name := range []string{"dark-reject", "skin-accept", "mixed"} {
		b.Run(name, func(b *testing.B) {
			s := frame03framePRSource(kind)
			i := 0
			sum := 0
			b.ReportAllocs()
			for b.Loop() {
				if computeSkin8x8Block(s, 32, 32, (i>>2)&3, i&3, 0) {
					sum++
				}
				i++
			}
			frame03framePRSink = sum
		})
	}
}
