//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Intra4x4DCFill follows libvpx v1.16.0 vpx_dsp/intrapred.c DC rounding.
// The exact four-byte references are completely consumed before output stores.
func Intra4x4DCFill(dst []byte, ds int, above, left []byte) bool {
	if !Enabled || uint(ds)>>30 != 0 || 3*ds+4 > len(dst) || len(above) < 4 || len(left) < 4 {
		return false
	}
	a := binary.LittleEndian.Uint32(above)
	l := binary.LittleEndian.Uint32(left)
	v := archsimd.Uint32x4{}.SetElem(0, a).SetElem(1, l).ReshapeToUint8s().ExtendLo8ToUint16()
	value := byte((int(v.ReduceSum()) + 4) >> 3)
	word := archsimd.BroadcastUint8x16(value).ReshapeToUint32s().GetElem(0)
	binary.LittleEndian.PutUint32(dst, word)
	binary.LittleEndian.PutUint32(dst[ds:], word)
	binary.LittleEndian.PutUint32(dst[2*ds:], word)
	binary.LittleEndian.PutUint32(dst[3*ds:], word)
	return true
}
