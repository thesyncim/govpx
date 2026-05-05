package testutil

import (
	"crypto/md5"
	"encoding/hex"
	"hash"
)

type PlaneMD5 struct {
	Y [16]byte
	U [16]byte
	V [16]byte
}

func MD5Plane(plane []byte, stride int, width int, height int) [16]byte {
	if width <= 0 || height <= 0 || stride < width {
		return [16]byte{}
	}
	if stride == width && len(plane) >= width*height {
		return md5.Sum(plane[:width*height])
	}

	h := md5.New()
	writePlaneHash(h, plane, stride, width, height)
	var out [16]byte
	copy(out[:], h.Sum(nil))
	return out
}

func MD5Planes(y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, width int, height int) PlaneMD5 {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return PlaneMD5{
		Y: MD5Plane(y, yStride, width, height),
		U: MD5Plane(u, uStride, uvWidth, uvHeight),
		V: MD5Plane(v, vStride, uvWidth, uvHeight),
	}
}

func MD5Hex(sum [16]byte) string {
	dst := make([]byte, 32)
	hex.Encode(dst, sum[:])
	return string(dst)
}

func writePlaneHash(h hash.Hash, plane []byte, stride int, width int, height int) {
	for y := 0; y < height; y++ {
		off := y * stride
		if off+width > len(plane) {
			return
		}
		_, _ = h.Write(plane[off : off+width])
	}
}
