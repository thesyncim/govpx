//go:build goexperiment.simd && (arm64 || amd64) && !purego

package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// libvpx v1.16.0 reconintra4x4.c uses avg2/avg3/copies. Four quarter
// samples represent all three exactly, including the single rounding.
func intraDirectionalGo(mode int, dst []byte, ds int, above, left []byte, top byte) bool {
	if !pixel.Enabled {
		return false
	}
	var border [16]byte
	copy(border[:8], above)
	copy(border[8:12], left)
	border[12] = top
	pixel.Intra4(dst, ds, &border, &intra4Selectors[mode])
	return true
}
