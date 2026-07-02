//go:build !amd64 && !arm64

package bitstream

import "encoding/binary"

// load64BE reads 8 bytes big-endian from b at offset i.
func load64BE(b []byte, i int) uint64 {
	return binary.BigEndian.Uint64(b[i:])
}
