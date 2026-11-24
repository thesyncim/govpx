// SPDX-License-Identifier: BSD-2-Clause
// Byte-domain dataflow extracted from goav1 ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

// loopFilterNonFlat uses the byte-domain narrow filter only when no lane
// requires flat filtering. It never writes a rejected packet.
func loopFilterNonFlat(p []byte, off, step, outer, blimit, limit, thresh int) bool {
	if blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 || !((outer == 1 && step >= 8) || (step == 1 && outer >= 8)) {
		return false
	}
	if off < 0 || off >= len(p) || step > off/4 {
		return false
	}
	room := len(p) - 1 - off
	if step > room/3 || outer > (room-3*step)/7 {
		return false
	}
	return loopFilterNonFlatBytes(p, off, step, outer, blimit, limit, thresh)
}
