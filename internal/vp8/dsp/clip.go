package dsp

// Ported from libvpx v1.16.0 vpx_dsp/vpx_dsp_common.h.

func ClipPixel(v int) uint8 {
	if v > 255 {
		return 255
	}
	if v < 0 {
		return 0
	}
	return uint8(v)
}

func ClipPixelAdd(dst uint8, delta int) uint8 {
	return ClipPixel(int(dst) + delta)
}

func Clamp(v int, low int, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
