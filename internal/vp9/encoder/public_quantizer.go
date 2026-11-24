package encoder

const MaxPublicQuantizer = 63

// PublicQuantizerToQIndex maps govpx's public VP9 0..63 quantizer scale to
// libvpx's 0..255 qindex scale.
func PublicQuantizerToQIndex(q int) int {
	return publicQuantizerToQIndex[min(max(q, 0), MaxPublicQuantizer)]
}

// QIndexToPublicQuantizer maps a VP9 qindex back to the first public
// quantizer whose table entry is at least qindex.
func QIndexToPublicQuantizer(qIndex int) int {
	for q, translated := range publicQuantizerToQIndex {
		if translated >= qIndex {
			return q
		}
	}
	return MaxPublicQuantizer
}

// PublicQModeInterRate returns the inter-frame q-delta ratio used by public
// quantizer mode's fixed cadence.
func PublicQModeInterRate(frameIndex int) (num int, den int) {
	switch frameIndex & 7 {
	case 0:
		return 1, 2
	case 2, 6:
		return 85, 100
	case 4:
		return 7, 10
	default:
		return 1, 1
	}
}

var publicQuantizerToQIndex = [MaxPublicQuantizer + 1]int{
	0, 4, 8, 12, 16, 20, 24, 28,
	32, 36, 40, 44, 48, 52, 56, 60,
	64, 68, 72, 76, 80, 84, 88, 92,
	96, 100, 104, 108, 112, 116, 120, 124,
	128, 132, 136, 140, 144, 148, 152, 156,
	160, 164, 168, 172, 176, 180, 184, 188,
	192, 196, 200, 204, 208, 212, 216, 220,
	224, 228, 232, 236, 240, 244, 249, 255,
}
