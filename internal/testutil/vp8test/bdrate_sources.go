package vp8test

import (
	"image"
	"math/rand"

	"github.com/thesyncim/govpx/internal/testutil"
)

// NewBDRateTexturedNoiseYCbCr builds a textured, slowly translating 4:2:0
// frame for VP8 BD-rate gates. The source is deterministic for a given frame
// index while still carrying enough luma and chroma variation to exercise
// inter prediction across short ladders.
func NewBDRateTexturedNoiseYCbCr(width, height, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	r := rand.New(rand.NewSource(int64(frame) + 7919))
	shift := frame * 2
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			base := ((x + shift) ^ (y * 5)) & 0xFF
			noise := r.Intn(33) - 16
			row[x] = testutil.ClampByte(base + noise)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			cb[x] = byte(128 + ((x+frame)*3)&0x3F)
			cr[x] = byte(128 + ((y+frame*2)*5)&0x3F)
		}
	}
	return img
}

// NewSportsMotionYCbCr returns a deterministic high-motion 4:2:0 frame:
// textured background plus a fast-moving foreground disc that crosses the
// frame and triggers larger motion vectors.
func NewSportsMotionYCbCr(width, height, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	r := rand.New(rand.NewSource(int64(frame)*131 + 17))

	camShift := frame * 3
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			base := ((x+camShift)*7 ^ (y * 11)) & 0xFF
			noise := r.Intn(25) - 12
			row[x] = testutil.ClampByte(96 + base/4 + noise)
		}
	}

	radius := max(width/8, 8)
	cx := (frame * width / 6) % (width + radius*2)
	cx -= radius
	cy := height/2 + testutil.TriangleByte(frame*16, 64)*(height/4)/255 - height/8
	r2 := radius * radius
	for y := max(0, cy-radius); y < min(height, cy+radius); y++ {
		row := img.Y[y*img.YStride:]
		dy := y - cy
		for x := max(0, cx-radius); x < min(width, cx+radius); x++ {
			dx := x - cx
			if dx*dx+dy*dy <= r2 {
				row[x] = 232
			}
		}
	}

	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			cb[x] = byte(112 + ((x+frame)*5)&0x1F)
			cr[x] = byte(144 + ((y+frame*3)*7)&0x1F)
		}
	}
	return img
}

// NewStaticThenMotionYCbCr returns a deterministic 4:2:0 frame that is still
// for the first few frames and then translates quickly. This exercises
// rate-control handling for a sudden inter-prediction residual ramp.
func NewStaticThenMotionYCbCr(width, height, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	shift := 0
	if frame >= 4 {
		shift = (frame - 4) * 8
	}
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			sx := x + shift
			gradient := 80 + testutil.TriangleByte(sx+y, 192)/4
			tri := testutil.TriangleByte(sx, 48) / 5
			texture := ((sx*1103515245+y*12345)>>4)&0x0F - 8
			row[x] = testutil.ClampByte(gradient + tri + texture)
		}
	}

	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			sx := 2*x + shift
			cb[x] = testutil.ClampByte(128 + (testutil.TriangleByte(sx, 96)-128)/8)
			cr[x] = testutil.ClampByte(128 + (testutil.TriangleByte(2*y, 96)-128)/8)
		}
	}
	return img
}

// NewMixedMotionYCbCr returns a deterministic frame that alternates between
// near-static panning and high-motion phases. The repeated phase boundaries
// exercise rate-control adaptation beyond a single scene-transition fixture.
func NewMixedMotionYCbCr(width, height, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	phase := (frame / 4) & 1
	shiftX := frame
	shiftY := frame / 2
	if phase == 1 {
		shiftX = frame * 6
		shiftY = frame * 3
	}

	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			sx := x + shiftX
			sy := y + shiftY
			gradient := 64 + testutil.TriangleByte(sx+sy, 192)/4
			tri := testutil.TriangleByte(sx, 48)/5 +
				testutil.TriangleByte(sy, 96)/6
			texture := ((sx*1103515245+sy*12345)>>4)&0x0F - 8
			row[x] = testutil.ClampByte(gradient + tri + texture)
		}
	}

	if phase == 1 {
		radius := max(width/10, 6)
		cx := (frame * width / 5) % (width + radius*2)
		cx -= radius
		cy := height/2 + (frame%5)*(height/12) - height/8
		r2 := radius * radius
		for y := max(0, cy-radius); y < min(height, cy+radius); y++ {
			row := img.Y[y*img.YStride:]
			dy := y - cy
			for x := max(0, cx-radius); x < min(width, cx+radius); x++ {
				dx := x - cx
				if dx*dx+dy*dy <= r2 {
					row[x] = 220
				}
			}
		}
	}

	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			sx := 2*x + shiftX
			sy := 2*y + shiftY
			cb[x] = testutil.ClampByte(128 + (testutil.TriangleByte(sx, 128)-128)/8)
			cr[x] = testutil.ClampByte(128 + (testutil.TriangleByte(sy, 128)-128)/8)
		}
	}
	return img
}

// NewBPredEdgeGridYCbCr returns a deterministic 4:2:0 frame designed to make
// the VP8 picker consider B_PRED heavily: alternating directional-edge bands
// and flat regions, shifted by one pixel diagonally per frame.
func NewBPredEdgeGridYCbCr(width, height, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	r := rand.New(rand.NewSource(int64(frame)*9973 + 113))
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			noise := r.Intn(3) - 1
			row[x] = testutil.ClampByte(112 + noise)
		}
	}

	xoff := frame
	yoff := frame
	const block = 4
	renderBlock := func(dir, x0, y0 int, lumaHi, lumaLo byte) {
		for dy := range block {
			y := y0 + dy
			if y < 0 || y >= height {
				continue
			}
			row := img.Y[y*img.YStride:]
			for dx := range block {
				x := x0 + dx
				if x < 0 || x >= width {
					continue
				}
				on := false
				switch dir & 0x07 {
				case 0:
					on = dy < 2
				case 1:
					on = dx < 2
				case 2:
					on = dx+dy < 3
				case 3:
					on = dx >= dy
				case 4:
					on = 2*dx+dy < 5
				case 5:
					on = 2*dx-dy < 3
				case 6:
					on = dx+2*dy < 5
				case 7:
					on = dx-2*dy < 1
				}
				if on {
					row[x] = lumaHi
				} else {
					row[x] = lumaLo
				}
			}
		}
	}

	const bandHeight = 64
	for gy := 0; gy < height; gy += block {
		if (gy/bandHeight)&1 != 0 {
			continue
		}
		for gx := 0; gx < width; gx += block {
			cx := gx / block
			cy := gy / block
			dir := (cx*3 + cy*5) & 0x07
			hash := cx*1103515245 + cy*12345
			lumaHi := byte(128 + (hash>>3)&0x0F)
			lumaLo := byte(112 - (hash>>11)&0x0F)
			renderBlock(dir, gx+xoff, gy+yoff, lumaHi, lumaLo)
		}
	}

	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			cb[x] = byte(128 + ((x+frame)*3)&0x07)
			cr[x] = byte(128 + ((y+frame*2)*3)&0x07)
		}
	}
	return img
}
