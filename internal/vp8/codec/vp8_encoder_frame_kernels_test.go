package codec

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"reflect"
	"testing"
)

var frame02framePRSink int

func frame02frameRefsmoothSkinMap(rows int, cols int, skinMap []uint8) {
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

func TestFramePR02(t *testing.T) {
	for _, shape := range [][2]int{{0, 0}, {1, 1}, {3, 3}, {7, 9}, {45, 80}} {
		for seed := range 10 {
			r, c := shape[0], shape[1]
			a := make([]uint8, r*c)
			for i := range a {
				a[i] = uint8((i*37 + seed*23) % (seed + 2))
			}
			z := append([]uint8{}, a...)
			smoothSkinMap(r, c, a)
			frame02frameRefsmoothSkinMap(r, c, z)
			for i := range a {
				if a[i] != z[i] {
					t.Fatalf("smooth %v seed=%d at=%d", shape, seed, i)
				}
			}
		}
	}
}

func BenchmarkFramePR02(b *testing.B) {
	for _, kind := range []string{"empty", "skin", "mixed"} {
		b.Run(kind, func(b *testing.B) {
			const r, c = 45, 80
			raw := make([]uint8, r*c)
			m := make([]uint8, r*c)
			for i := range raw {
				if kind == "skin" || kind == "mixed" && i%7 < 4 {
					raw[i] = 1
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				copy(m, raw)
				smoothSkinMap(r, c, m)
			}
			frame02framePRSink = int(m[1+c])
		})
	}
}

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

var frame05framePRSink int

func frame05frameRefdotArtifactCornerCheck(srcMB []byte, srcStride int, refMB []byte, refStride int, shift int) bool {
	corners := [4][4]int{
		{0, 0, 1, 1},
		{0, shift, 1, -1},
		{shift, 0, -1, 1},
		{shift, shift, -1, -1},
	}
	for _, c := range corners {
		gradLast := frame05frameRefmacroblockCornerGradient(refMB, refStride, c[0], c[1], c[2], c[3])
		gradSrc := frame05frameRefmacroblockCornerGradient(srcMB, srcStride, c[0], c[1], c[2], c[3])
		if gradLast >= dotArtifactCornerGradLast && gradSrc <= dotArtifactCornerGradSource {
			return true
		}
	}
	return false
}

func frame05frameRefmacroblockCornerGradient(plane []byte, stride int, offRow int, offCol int, sgnRow int, sgnCol int) int {
	y1 := int(plane[offRow*stride+offCol])
	y2 := int(plane[offRow*stride+offCol+sgnCol])
	y3 := int(plane[(offRow+sgnRow)*stride+offCol])
	y4 := int(plane[(offRow+sgnRow)*stride+offCol+sgnCol])
	// Branchless |delta| via sign-mask XOR plus max() to fold the
	// per-neighbor d{1,2,3} max ladder into builtin calls.
	d1 := y1 - y2
	m1 := d1 >> mvKernelSignShift
	d1 = (d1 ^ m1) - m1
	d2 := y1 - y3
	m2 := d2 >> mvKernelSignShift
	d2 = (d2 ^ m2) - m2
	d3 := y1 - y4
	m3 := d3 >> mvKernelSignShift
	d3 = (d3 ^ m3) - m3
	return max(max(d1, d2), d3)
}

func TestFramePR05(t *testing.T) {
	for seed := range 20 {
		a, z := make([]byte, 256), make([]byte, 256)
		for i := range a {
			a[i] = byte(i*seed + seed*7)
			z[i] = byte(i * (seed + 1))
		}
		for _, shift := range []int{7, 15} {
			if dotArtifactCornerCheck(a, 16, z, 16, shift) != frame05frameRefdotArtifactCornerCheck(a, 16, z, 16, shift) {
				t.Fatal("corner", seed, shift)
			}
		}
	}
}

func BenchmarkFramePR05(b *testing.B) {
	for _, kind := range []string{"flat-last", "gradient-last", "mixed"} {
		b.Run(kind, func(b *testing.B) {
			a, z := make([]byte, 256), make([]byte, 256)
			for i := range a {
				a[i] = 100
				z[i] = 100
				if kind == "gradient-last" {
					z[i] = byte(i * 7)
				}
				if kind == "mixed" {
					a[i] = byte(i * 29)
					z[i] = byte(i * 31)
				}
			}
			sum := 0
			b.ReportAllocs()
			for b.Loop() {
				if dotArtifactCornerCheck(a, 16, z, 16, 15) {
					sum++
				}
			}
			frame05framePRSink = sum
		})
	}
}

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
