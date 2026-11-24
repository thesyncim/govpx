package testutil

import (
	"image"
	"math/rand"

	"github.com/thesyncim/govpx/internal/vpx/arith"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// NewYCbCr returns a full-frame 4:2:0 image filled with one Y/Cb/Cr sample.
func NewYCbCr(width, height int, y, cb, cr byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for yy := range height {
		row := img.Y[yy*img.YStride:]
		for xx := range width {
			row[xx] = y
		}
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	for yy := range uvHeight {
		cbRow := img.Cb[yy*img.CStride:]
		crRow := img.Cr[yy*img.CStride:]
		for xx := range uvWidth {
			cbRow[xx] = cb
			crRow[xx] = cr
		}
	}
	return img
}

// NewMotionYCbCr returns deterministic textured luma content with neutral
// chroma. The pattern is stable across tests but contains enough gradients
// and edges to exercise simple motion and variance paths.
func NewMotionYCbCr(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for yy := range height {
		row := img.Y[yy*img.YStride:]
		for xx := range width {
			row[xx] = byte((xx*3 + yy*5 + (xx/8)*17 + (yy/8)*29) & 0xff)
		}
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	for yy := range uvHeight {
		cbRow := img.Cb[yy*img.CStride:]
		crRow := img.Cr[yy*img.CStride:]
		for xx := range uvWidth {
			cbRow[xx] = 128
			crRow[xx] = 128
		}
	}
	return img
}

// TriangleByte returns a deterministic [0,255] triangle wave with the given
// period. It is useful for synthetic video fixtures that need smooth
// gradients without floating-point math.
func TriangleByte(x, period int) int {
	if period <= 0 {
		period = 32
	}
	half := period / 2
	r := ((x % period) + period) % period
	if r < half {
		return r * 255 / half
	}
	return (period - r) * 255 / half
}

// ClampByte saturates an integer into the uint8 sample range.
func ClampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// NewTexturedPanningYCbCr returns a deterministic "panning camera" 4:2:0
// frame: low-frequency luma gradient plus mid-frequency triangle harmonics
// translating by (+2,+1) per frame, with deterministic high-frequency texture
// layered on top.
func NewTexturedPanningYCbCr(width, height, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	xoff := frame * 2
	yoff := frame
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			sx := x + xoff
			sy := y + yoff
			gradient := 64 + TriangleByte(sx+sy, 256)/4
			triX := TriangleByte(sx, 64) / 4
			triY := TriangleByte(sy, 64) / 4
			texture := ((sx*1103515245+sy*12345)>>4)&0x0F - 8
			row[x] = ClampByte(gradient + triX + triY + texture)
		}
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			sx := 2*x + xoff
			sy := 2*y + yoff
			cb[x] = ClampByte(128 + (TriangleByte(sx, 128)-128)/8)
			cr[x] = ClampByte(128 + (TriangleByte(sy, 128)-128)/8)
		}
	}
	return img
}

// NewScreenTextWindowYCbCr returns a deterministic screen-content frame with
// a textured dark background and translating 8x8 glyph blocks.
func NewScreenTextWindowYCbCr(width, height, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	r := rand.New(rand.NewSource(int64(frame)*4099 + 31))
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			noise := r.Intn(7) - 3
			row[x] = ClampByte(28 + noise)
		}
	}

	const cell = 16
	const glyph = 8
	xoff := (frame * glyph) % cell
	for gy := 0; gy < height; gy += cell {
		for gx := 0; gx < width; gx += cell {
			cellHash := (gx/cell)*1103515245 + (gy/cell)*12345
			if cellHash&0x07 >= 5 {
				continue
			}
			lumaHi := byte(208 + (cellHash>>3)&0x1F)
			lumaLo := byte(168 + (cellHash>>11)&0x1F)
			x0 := gx + xoff
			y0 := gy
			for dy := range glyph {
				y := y0 + dy
				if y < 0 || y >= height {
					continue
				}
				row := img.Y[y*img.YStride:]
				for dx := range glyph {
					x := x0 + dx
					if x < 0 || x >= width {
						continue
					}
					if (dx^dy)&1 == 0 {
						row[x] = lumaHi
					} else {
						row[x] = lumaLo
					}
				}
			}
		}
	}

	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			cb[x] = byte(128 + ((x+frame)*3)&0x03)
			cr[x] = byte(128 + ((y+frame*2)*3)&0x03)
		}
	}
	return img
}

// ShiftYCbCrCopy shifts src by dy/dx pixels into a new image. Samples that
// would read outside the source repeat the nearest edge sample.
func ShiftYCbCrCopy(src *image.YCbCr, dy, dx int) *image.YCbCr {
	width := src.Rect.Dx()
	height := src.Rect.Dy()
	out := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for yy := range height {
		for xx := range width {
			sy := arith.ClampCoord(yy-dy, height)
			sx := arith.ClampCoord(xx-dx, width)
			out.Y[yy*out.YStride+xx] = src.Y[sy*src.YStride+sx]
		}
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	for yy := range uvHeight {
		for xx := range uvWidth {
			sy := arith.ClampCoord(yy-dy/2, uvHeight)
			sx := arith.ClampCoord(xx-dx/2, uvWidth)
			out.Cb[yy*out.CStride+xx] = src.Cb[sy*src.CStride+sx]
			out.Cr[yy*out.CStride+xx] = src.Cr[sy*src.CStride+sx]
		}
	}
	return out
}

// AppendYCbCrI420 appends the visible I420 planes from img, ignoring stride
// padding.
func AppendYCbCrI420(out []byte, img *image.YCbCr) []byte {
	width := img.Rect.Dx()
	height := img.Rect.Dy()
	return AppendI420Planes(out, width, height,
		img.Y, img.YStride,
		img.Cb, img.CStride,
		img.Cr, img.CStride)
}

// AppendI420Planes appends visible Y, U, and V samples from strided 4:2:0
// planes.
func AppendI420Planes(out []byte, width int, height int,
	y []byte, yStride int,
	u []byte, uStride int,
	v []byte, vStride int,
) []byte {
	out = AppendPlane(out, y, yStride, width, height)
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	out = AppendPlane(out, u, uStride, uvWidth, uvHeight)
	out = AppendPlane(out, v, vStride, uvWidth, uvHeight)
	return out
}

// AppendPlane appends visible samples from a strided plane.
func AppendPlane(out []byte, plane []byte, stride int, width int, height int) []byte {
	for row := range height {
		start := row * stride
		out = append(out, plane[start:start+width]...)
	}
	return out
}

// PlaneEqual reports whether two strided planes have identical visible
// samples.
func PlaneEqual(a []byte, aStride int, b []byte, bStride int, width int, height int) bool {
	for row := range height {
		aRow := a[row*aStride:]
		bRow := b[row*bStride:]
		for col := range width {
			if aRow[col] != bRow[col] {
				return false
			}
		}
	}
	return true
}
