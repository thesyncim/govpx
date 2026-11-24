//go:build goexperiment.simd && amd64 && !purego

package pixel

import "encoding/binary"

// Copy8Rows preserves libvpx v1.16.0 vp8/common/reconinter.c row order.
// Exact eight-byte row dataflow is adapted from goav1 recon_gosimd.go.
// Each full row is loaded before its store, preserving per-row memmove aliases.
func Copy8Rows(src []byte, ss int, dst []byte, ds int) bool {
	if !Enabled || (uint(ss)|uint(ds))>>30 != 0 || 7*ss+8 > len(src) || 7*ds+8 > len(dst) {
		return false
	}
	if &src[0] != &dst[0] || ss != ds {
		for row := range 8 {
			v := binary.LittleEndian.Uint64(src[row*ss:])
			binary.LittleEndian.PutUint64(dst[row*ds:], v)
		}
	}
	return true
}
