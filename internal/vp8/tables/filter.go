package tables

// Ported interpolation filter tables from libvpx v1.16.0
// vp8/common/filter.c and constants from vp8/common/filter.h.

const (
	FilterWeight = 128
	FilterShift  = 7
)

var BilinearFilters = [8][2]int16{
	{128, 0},
	{112, 16},
	{96, 32},
	{80, 48},
	{64, 64},
	{48, 80},
	{32, 96},
	{16, 112},
}

var SubPelFilters = [8][6]int16{
	{0, 0, 128, 0, 0, 0},
	{0, -6, 123, 12, -1, 0},
	{2, -11, 108, 36, -8, 1},
	{0, -9, 93, 50, -6, 0},
	{3, -16, 77, 77, -16, 3},
	{0, -6, 50, 93, -9, 0},
	{1, -8, 36, 108, -11, 2},
	{0, -1, 12, 123, -6, 0},
}
