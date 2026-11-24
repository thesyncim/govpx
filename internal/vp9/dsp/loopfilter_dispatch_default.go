package dsp

import "github.com/thesyncim/govpx/internal/pixel"

func vpxLpfHorizontal4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit), int(limit), int(thresh), 4) {
		return
	}

	vpxLpfHorizontal4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit), int(limit), int(thresh), 4) {
		return
	}

	vpxLpfVertical4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit0), int(limit0), int(thresh0), 4) {
		pixel.LoopFilter(plane, s+8*1, pitch, 1, 8, int(blimit1), int(limit1), int(thresh1), 4)
		return
	}

	vpxLpfHorizontal4DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit0), int(limit0), int(thresh0), 4) {
		pixel.LoopFilter(plane, s+8*pitch, 1, pitch, 8, int(blimit1), int(limit1), int(thresh1), 4)
		return
	}

	vpxLpfVertical4DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit), int(limit), int(thresh), 8) {
		return
	}

	vpxLpfHorizontal8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit), int(limit), int(thresh), 8) {
		return
	}

	vpxLpfVertical8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if blimit0 == blimit1 && limit0 == limit1 && thresh0 == thresh1 &&
		pixel.LoopFilter(plane, s, pitch, 1, 16, int(blimit0), int(limit0), int(thresh0), 8) {
		return
	}
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit0), int(limit0), int(thresh0), 8) {
		pixel.LoopFilter(plane, s+8*1, pitch, 1, 8, int(blimit1), int(limit1), int(thresh1), 8)
		return
	}

	vpxLpfHorizontal8DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if blimit0 == blimit1 && limit0 == limit1 && thresh0 == thresh1 &&
		pixel.LoopFilter(plane, s, 1, pitch, 16, int(blimit0), int(limit0), int(thresh0), 8) {
		return
	}
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit0), int(limit0), int(thresh0), 8) {
		pixel.LoopFilter(plane, s+8*pitch, 1, pitch, 8, int(blimit1), int(limit1), int(thresh1), 8)
		return
	}

	vpxLpfVertical8DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal16(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit), int(limit), int(thresh), 16) {
		return
	}

	mbLpfHorizontalEdgeW(plane, s, pitch, blimit, limit, thresh, 1)
}

func vpxLpfHorizontal16Dual(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, pitch, 1, 16, int(blimit), int(limit), int(thresh), 16) {
		return
	}

	mbLpfHorizontalEdgeW(plane, s, pitch, blimit, limit, thresh, 2)
}

func vpxLpfVertical16(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit), int(limit), int(thresh), 16) {
		return
	}

	mbLpfVerticalEdgeW(plane, s, pitch, blimit, limit, thresh, 8)
}

func vpxLpfVertical16Dual(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, 1, pitch, 16, int(blimit), int(limit), int(thresh), 16) {
		return
	}

	mbLpfVerticalEdgeW(plane, s, pitch, blimit, limit, thresh, 16)
}
