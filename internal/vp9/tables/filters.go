package tables

// VP9 subpel interpolation filter tables ported byte-for-byte from
// libvpx v1.16.0 vp9/common/vp9_filter.c. Each filter is an 8-tap
// kernel sampled at 16 subpel positions (1/16-pel precision). The
// taps sum to 128 = 2^FILTER_BITS so a convolve normalizes by >>7.

// Subpel constants from vpx_dsp/vpx_filter.h. These ARE wire-stable —
// they are part of how the VP9 motion compensator interprets MVs.
const (
	FilterBits   = 7
	SubpelBits   = 4
	SubpelMask   = (1 << SubpelBits) - 1
	SubpelShifts = 1 << SubpelBits
	SubpelTaps   = 8
)

// BilinearFilters is the 8-tap bilinear filter (only the inner two
// taps are non-zero). Used by the predictor when bilinear interp is
// configured.
var BilinearFilters = [SubpelShifts][SubpelTaps]int16{
	{0, 0, 0, 128, 0, 0, 0, 0},
	{0, 0, 0, 120, 8, 0, 0, 0},
	{0, 0, 0, 112, 16, 0, 0, 0},
	{0, 0, 0, 104, 24, 0, 0, 0},
	{0, 0, 0, 96, 32, 0, 0, 0},
	{0, 0, 0, 88, 40, 0, 0, 0},
	{0, 0, 0, 80, 48, 0, 0, 0},
	{0, 0, 0, 72, 56, 0, 0, 0},
	{0, 0, 0, 64, 64, 0, 0, 0},
	{0, 0, 0, 56, 72, 0, 0, 0},
	{0, 0, 0, 48, 80, 0, 0, 0},
	{0, 0, 0, 40, 88, 0, 0, 0},
	{0, 0, 0, 32, 96, 0, 0, 0},
	{0, 0, 0, 24, 104, 0, 0, 0},
	{0, 0, 0, 16, 112, 0, 0, 0},
	{0, 0, 0, 8, 120, 0, 0, 0},
}

// SubPelFilters8 is the regular Lagrangian 8-tap subpel filter. This is
// the default for VP9 inter prediction.
var SubPelFilters8 = [SubpelShifts][SubpelTaps]int16{
	{0, 0, 0, 128, 0, 0, 0, 0},
	{0, 1, -5, 126, 8, -3, 1, 0},
	{-1, 3, -10, 122, 18, -6, 2, 0},
	{-1, 4, -13, 118, 27, -9, 3, -1},
	{-1, 4, -16, 112, 37, -11, 4, -1},
	{-1, 5, -18, 105, 48, -14, 4, -1},
	{-1, 5, -19, 97, 58, -16, 5, -1},
	{-1, 6, -19, 88, 68, -18, 5, -1},
	{-1, 6, -19, 78, 78, -19, 6, -1},
	{-1, 5, -18, 68, 88, -19, 6, -1},
	{-1, 5, -16, 58, 97, -19, 5, -1},
	{-1, 4, -14, 48, 105, -18, 5, -1},
	{-1, 4, -11, 37, 112, -16, 4, -1},
	{-1, 3, -9, 27, 118, -13, 4, -1},
	{0, 2, -6, 18, 122, -10, 3, -1},
	{0, 1, -3, 8, 126, -5, 1, 0},
}

// SubPelFilters8s is the sharp DCT-based 8-tap subpel filter.
var SubPelFilters8s = [SubpelShifts][SubpelTaps]int16{
	{0, 0, 0, 128, 0, 0, 0, 0},
	{-1, 3, -7, 127, 8, -3, 1, 0},
	{-2, 5, -13, 125, 17, -6, 3, -1},
	{-3, 7, -17, 121, 27, -10, 5, -2},
	{-4, 9, -20, 115, 37, -13, 6, -2},
	{-4, 10, -23, 108, 48, -16, 8, -3},
	{-4, 10, -24, 100, 59, -19, 9, -3},
	{-4, 11, -24, 90, 70, -21, 10, -4},
	{-4, 11, -23, 80, 80, -23, 11, -4},
	{-4, 10, -21, 70, 90, -24, 11, -4},
	{-3, 9, -19, 59, 100, -24, 10, -4},
	{-3, 8, -16, 48, 108, -23, 10, -4},
	{-2, 6, -13, 37, 115, -20, 9, -4},
	{-2, 5, -10, 27, 121, -17, 7, -3},
	{-1, 3, -6, 17, 125, -13, 5, -2},
	{0, 1, -3, 8, 127, -7, 3, -1},
}

// SubPelFilters8lp is the smooth (low-pass) 8-tap subpel filter
// (freqmultiplier = 0.5 in libvpx's comment).
var SubPelFilters8lp = [SubpelShifts][SubpelTaps]int16{
	{0, 0, 0, 128, 0, 0, 0, 0},
	{-3, -1, 32, 64, 38, 1, -3, 0},
	{-2, -2, 29, 63, 41, 2, -3, 0},
	{-2, -2, 26, 63, 43, 4, -4, 0},
	{-2, -3, 24, 62, 46, 5, -4, 0},
	{-2, -3, 21, 60, 49, 7, -4, 0},
	{-1, -4, 18, 59, 51, 9, -4, 0},
	{-1, -4, 16, 57, 53, 12, -4, -1},
	{-1, -4, 14, 55, 55, 14, -4, -1},
	{-1, -4, 12, 53, 57, 16, -4, -1},
	{0, -4, 9, 51, 59, 18, -4, -1},
	{0, -4, 7, 49, 60, 21, -3, -2},
	{0, -4, 5, 46, 62, 24, -3, -2},
	{0, -4, 4, 43, 63, 26, -2, -2},
	{0, -3, 2, 41, 63, 29, -2, -2},
	{0, -3, 1, 38, 64, 32, -1, -3},
}

// SubPelFilters4 is the 4-tap subpel filter (inner taps only).
var SubPelFilters4 = [SubpelShifts][SubpelTaps]int16{
	{0, 0, 0, 128, 0, 0, 0, 0},
	{0, 0, -4, 126, 8, -2, 0, 0},
	{0, 0, -6, 120, 18, -4, 0, 0},
	{0, 0, -8, 114, 28, -6, 0, 0},
	{0, 0, -10, 108, 36, -6, 0, 0},
	{0, 0, -12, 102, 46, -8, 0, 0},
	{0, 0, -12, 94, 56, -10, 0, 0},
	{0, 0, -12, 84, 66, -10, 0, 0},
	{0, 0, -12, 76, 76, -12, 0, 0},
	{0, 0, -10, 66, 84, -12, 0, 0},
	{0, 0, -10, 56, 94, -12, 0, 0},
	{0, 0, -8, 46, 102, -12, 0, 0},
	{0, 0, -6, 36, 108, -10, 0, 0},
	{0, 0, -6, 28, 114, -8, 0, 0},
	{0, 0, -4, 18, 120, -6, 0, 0},
	{0, 0, -2, 8, 126, -4, 0, 0},
}

// FilterKernels mirrors vp9_filter_kernels: ordered as
// EIGHTTAP, EIGHTTAP_SMOOTH, EIGHTTAP_SHARP, BILINEAR, FOURTAP.
// Decoder uses the first 4; the FOURTAP variant supports the
// frame scaling path.
var FilterKernels = [5]*[SubpelShifts][SubpelTaps]int16{
	&SubPelFilters8,
	&SubPelFilters8lp,
	&SubPelFilters8s,
	&BilinearFilters,
	&SubPelFilters4,
}
