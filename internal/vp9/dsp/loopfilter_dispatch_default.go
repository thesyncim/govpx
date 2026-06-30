//go:build !arm64 || purego

package dsp

func vpxLpfHorizontal4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfHorizontal4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfVertical4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfHorizontal4DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfVertical4DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfHorizontal8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfVertical8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfHorizontal8DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfVertical8DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}
