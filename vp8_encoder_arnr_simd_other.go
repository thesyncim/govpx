//go:build (!amd64 && !arm64) || purego

package govpx

func applyTemporalFilterSIMD(_ []byte, _ int, _ []byte, _ int, _ int, _ int, _ int, _ []uint32, _ []uint32) bool {
	return false
}
