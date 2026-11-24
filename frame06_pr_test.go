package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"reflect"
	"testing"
)

var frame06framePRSink int

func frame06framePRSource(kind int) vp8enc.SourceImage {
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

func frame06frameRefcomputeSkinMap(src vp8enc.SourceImage, rows int, cols int, consecZeroLast []uint8, skinMap []uint8) {
	count := rows * cols
	if count <= 0 || len(skinMap) < count || min(src.Width, src.Height) <= 0 {
		return
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	if src.Width*src.Height > skinDetectionMaxSmallFrame {
		computeSkinMap16x16(src, rows, cols, uvWidth, uvHeight, consecZeroLast, skinMap[:count])
		frame06frameRefsmoothSkinMap(rows, cols, skinMap[:count])
		return
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			consecutive := 0
			if len(consecZeroLast) > index {
				consecutive = int(consecZeroLast[index])
			}
			skin := frame06frameRefcomputeSkin8x8Block(src, uvWidth, uvHeight, row, col, consecutive)
			if skin {
				skinMap[index] = 1
			} else {
				skinMap[index] = 0
			}
		}
	}
	frame06frameRefsmoothSkinMap(rows, cols, skinMap[:count])
}

func frame06frameRefcomputeSkin8x8Block(src vp8enc.SourceImage, uvWidth int, uvHeight int, mbRow int, mbCol int, consecZeroLast int) bool {
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
		if frame06frameRefskinPixel(ySample, uSample, vSample, motion) {
			numSkin++
			if numSkin >= 2 {
				return true
			}
		}
	}
	return false
}

func frame06frameRefskinPixel(y int, cb int, cr int, motion int) bool {
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
		diff := frame06frameRefevaluateSkinColorDifference(cb, cr, i)
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

func frame06frameRefevaluateSkinColorDifference(cb int, cr int, index int) int {
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

func frame06frameRefsmoothSkinMap(rows int, cols int, skinMap []uint8) {
	if rows < 3 || cols < 3 || len(skinMap) < rows*cols {
		return
	}
	for row := 1; row < rows-1; row++ {
		for col := 1; col < cols-1; col++ {
			index := row*cols + col
			neighbors := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if skinMap[(row+dy)*cols+col+dx] != 0 {
						neighbors++
					}
				}
			}
			if skinMap[index] != 0 && neighbors < 2 {
				skinMap[index] = 0
			}
			if skinMap[index] == 0 && neighbors == 8 {
				skinMap[index] = 1
			}
		}
	}
}

func TestFramePR06(t *testing.T) {
	for _, size := range []int{64, 320, 720} {
		cols, rows := (size+15)/16, (size+15)/16
		src := frame06framePRSource(2)
		src.Width = size
		src.Height = size
		src.YStride = size
		src.UStride = (size + 1) / 2
		src.VStride = (size + 1) / 2
		src.Y = make([]byte, size*size)
		src.U = make([]byte, src.UStride*src.UStride)
		src.V = make([]byte, src.UStride*src.UStride)
		for kind := range 3 {
			counter := make([]uint8, rows*cols)
			for i := range counter {
				counter[i] = 61
				if kind == 1 && i == len(counter)-1 {
					counter[i] = 0
				}
				if kind == 2 {
					counter[i] = uint8(i)
				}
			}
			a, z := make([]uint8, len(counter)+3), make([]uint8, len(counter)+3)
			for i := range a {
				a[i] = 9
				z[i] = 9
			}
			computeSkinMap(src, rows, cols, counter, a)
			frame06frameRefcomputeSkinMap(src, rows, cols, counter, z)
			if !reflect.DeepEqual(a, z) {
				t.Fatal("static map", size, kind)
			}
		}
	}
}

func BenchmarkFramePR06(b *testing.B) {
	for _, kind := range []string{"static-720p", "moving-720p"} {
		b.Run(kind, func(b *testing.B) {
			const w, h = 1280, 720
			src := vp8enc.SourceImage{Width: w, Height: h, YStride: w, UStride: w / 2, VStride: w / 2, Y: make([]byte, w*h), U: make([]byte, w*h/4), V: make([]byte, w*h/4)}
			counter := make([]uint8, 80*45)
			m := make([]uint8, len(counter))
			if kind == "static-720p" {
				for i := range counter {
					counter[i] = 100
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				computeSkinMap(src, 45, 80, counter, m)
			}
			frame06framePRSink = int(m[0])
		})
	}
}
