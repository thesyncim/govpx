package encoder

// SBStateMiBlock is the libvpx mi-units-per-content-state-SB constant.
// libvpx writes counters at SB boundaries with stride (mi_stride >> 3) and
// row stride (mi_rows >> 3), which equals 8 mi units per SB step.
//
// libvpx: vp9_speed_features.c:680, vp9_encodeframe.c:5367.
const SBStateMiBlock = 8

// ContentStateBufferSize computes the libvpx allocation size for per-SB
// content-state and ARF-usage byte buffers:
//
//	(mi_stride >> 3) * ((mi_rows >> 3) + 1) * sizeof(uint8_t)
//
// libvpx: vp9_speed_features.c:680.
func ContentStateBufferSize(miStride, miRows int) int {
	if miStride <= 0 || miRows < 0 {
		return 0
	}
	return (miStride >> 3) * ((miRows >> 3) + 1)
}

// CalcMiSize ports libvpx's calc_mi_size helper.
//
// libvpx: vp9_onyxc_int.h:416 calc_mi_size.
func CalcMiSize(length int) int {
	return length + 8
}

// MiDimensionsForFrame returns (miCols, miRows, miStride) for the frame
// dimensions, mirroring libvpx's common allocation path.
//
// libvpx: vp9_alloccommon.c:21-27 set_mb_mi.
func MiDimensionsForFrame(width, height int) (miCols, miRows, miStride int) {
	miCols = (width + 7) >> 3
	miRows = (height + 7) >> 3
	miStride = CalcMiSize(miCols)
	return
}

// SBOffsetForMi returns the per-SB index libvpx uses to address
// count_arf_frame_usage, count_lastgolden_frame_usage, and
// content_state_sb_fd.
//
// libvpx: vp9_encodeframe.c:5367 sboffset,
// vp9_encodeframe.c:1232 sb_offset.
func SBOffsetForMi(miRow, miCol, miCols int) int {
	return ((miCols+7)>>3)*(miRow>>3) + (miCol >> 3)
}

// ResetContentStateBuffer zeroes a content-state byte slab.
func ResetContentStateBuffer(buf []uint8) {
	for i := range buf {
		buf[i] = 0
	}
}

// UpdateContentStateBuffer ports the content_state_sb_fd increment/reset body.
//
// libvpx: vp9_encodeframe.c:1238-1244.
func UpdateContentStateBuffer(buf []uint8, sbOffset int, lowSourceSAD bool) {
	if sbOffset < 0 || sbOffset >= len(buf) {
		return
	}
	if lowSourceSAD {
		if buf[sbOffset] < 255 {
			buf[sbOffset]++
		}
		return
	}
	buf[sbOffset] = 0
}

// ContentStateAt returns a content-state byte, or zero when the buffer is
// absent or the index is outside the slab.
func ContentStateAt(buf []uint8, sbOffset int) uint8 {
	if sbOffset < 0 || sbOffset >= len(buf) {
		return 0
	}
	return buf[sbOffset]
}

type AltRefUsageUpdate struct {
	PreviousPercAltRef float64

	AltRefGFGroup      bool
	IsSrcFrameAltRef   bool
	RefreshGoldenFrame bool
	RefreshAltRefFrame bool

	MiCols int
	MiRows int

	CountAltRefFrameUsage     []uint8
	CountLastGoldenFrameUsage []uint8
}

// UpdateAltRefUsage ports libvpx's update_altref_usage accumulator.
//
// libvpx: vp9_ratectrl.c:1802-1819.
func UpdateAltRefUsage(in AltRefUsageUpdate) float64 {
	if len(in.CountAltRefFrameUsage) == 0 ||
		len(in.CountLastGoldenFrameUsage) == 0 {
		return in.PreviousPercAltRef
	}

	sumRefFrameUsage := 0
	altRefFrameUsage := 0
	if in.AltRefGFGroup && !in.IsSrcFrameAltRef &&
		!in.RefreshGoldenFrame && !in.RefreshAltRefFrame {
		for miRow := 0; miRow < in.MiRows; miRow += SBStateMiBlock {
			for miCol := 0; miCol < in.MiCols; miCol += SBStateMiBlock {
				sbOffset := SBOffsetForMi(miRow, miCol, in.MiCols)
				if sbOffset < 0 || sbOffset >= len(in.CountAltRefFrameUsage) ||
					sbOffset >= len(in.CountLastGoldenFrameUsage) {
					continue
				}
				sumRefFrameUsage += int(in.CountAltRefFrameUsage[sbOffset]) +
					int(in.CountLastGoldenFrameUsage[sbOffset])
				altRefFrameUsage += int(in.CountAltRefFrameUsage[sbOffset])
			}
		}
	}
	if sumRefFrameUsage <= 0 {
		return in.PreviousPercAltRef
	}
	altRefCount := 100.0 * float64(altRefFrameUsage) / float64(sumRefFrameUsage)
	return 0.75*in.PreviousPercAltRef + 0.25*altRefCount
}
